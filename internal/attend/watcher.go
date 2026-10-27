package attend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// handshakeFor is how long a new watcher waits for the server to confirm
// it: the config that started it is still loading, and until it is done
// the server answers with the previous load's token (or none).
var handshakeFor = 20 * time.Second

// Watcher timing.
const (
	// settle is how long the watcher waits after an event for more before
	// it re-reads everything: a burst of events is one snapshot.
	settle = 200 * time.Millisecond
	// maxSettle bounds that wait: events that never pause (an agent
	// streaming its status) still get a snapshot this often.
	maxSettle = time.Second
	// resnap re-reads everything now and then, events or not.
	resnap = time.Minute
	// recheck is how often the watcher checks it is still wanted.
	recheck = time.Minute
	// steady is how long the event stream must have run for its loss to
	// count as the first in a row (the wait before following it again
	// starts over).
	steady = time.Minute
	// callTimeout bounds one call to Rex.
	callTimeout = 5 * time.Second
	// maxLog is the log's size beyond which a new watcher starts it afresh.
	maxLog = 1 << 20
)

// Lua actions the watcher calls (lua/attend.lua).
const (
	actionToken = "rexkit_attend_token" // the token of the config that runs
	actionToast = "rexkit_attend_toast" // a toast in its session's active window
)

// API is what the watcher needs of Rex (a *rex.Client).
type API interface {
	Call(ctx context.Context, method string, args, out any) error
	Snapshot(ctx context.Context) (rex.Inventory, error)
	Watch(ctx context.Context) (<-chan rex.Event, error)
	Action(ctx context.Context, session, name string, args, out any) error
	Clients(ctx context.Context) ([]rex.ClientInfo, error)
}

// WatchOptions configure the watcher.
type WatchOptions struct {
	Token   string   // the token of the config load that started it (lua/attend.lua)
	NextKey string   // the key toasts name for "go" (ctrl+shift+a)
	Toasts  []string // the OSC 7501 states that toast
}

// errSuperseded ends a watcher the server no longer wants: its config was
// reloaded (a new watcher takes over) or no longer loads rexkit.
var errSuperseded = errors.New("superseded")

// watcher follows one Rex server for as long as it runs the config that
// started it.
type watcher struct {
	api      API
	dirs     state.Dirs
	opts     WatchOptions
	log      *log.Logger
	exe      string // its own executable: gone means rexkit was removed
	instance string // the server's run
	tracker  *Tracker
	now      func() time.Time

	inv    rex.Inventory
	looked bool              // a snapshot was taken
	known  map[string]bool   // the blocks of the last complete snapshot
	focus  map[string]string // each session's tab and focused pane, as of the last snapshot
	active string            // the session where the focus last moved: a guess at what the app shows
}

// Watch runs the watcher until the server goes away or no longer wants it.
// It holds a lock per server run, so a second watcher for the same run
// waits for the first to go (or ends it, when it belongs to an older
// config load).
func Watch(ctx context.Context, api API, dirs state.Dirs, opts WatchOptions) error {
	w := &watcher{api: api, dirs: dirs, opts: opts, now: time.Now,
		tracker: NewTracker(opts.NextKey, opts.Toasts)}
	logFile := w.openLog()
	if logFile != nil {
		defer logFile.Close()
	}
	w.exe, _ = os.Executable()
	err := w.run(ctx)
	switch {
	case errors.Is(err, errSuperseded), errors.Is(err, context.Canceled):
		w.log.Printf("stopped: %v", err)
		return nil
	case err != nil:
		w.log.Printf("stopped: %v", err)
	}
	return err
}

func (w *watcher) run(ctx context.Context) error {
	if err := w.handshake(ctx); err != nil {
		return err
	}
	var status struct {
		InstanceID string `json:"instance_id"`
	}
	if err := w.call(ctx, "server.status", nil, &status); err != nil {
		return err
	}
	w.instance = status.InstanceID
	release, err := claim(ctx, w.dirs, w.instance, w.opts.Token, w.log)
	if err != nil {
		return err
	}
	defer release()
	w.log.Printf("watching server run %s (pid %d)", w.instance, os.Getpid())
	for failures := 0; ; failures++ {
		began := time.Now()
		err := w.follow(ctx)
		if errors.Is(err, errSuperseded) || ctx.Err() != nil {
			return err
		}
		if !w.alive(ctx) {
			return fmt.Errorf("the server is gone (%v)", err)
		}
		if time.Since(began) >= steady {
			failures = 0 // it ran a good while: this loss is not one of a series
		}
		pause := min(time.Duration(1<<min(failures, 5))*time.Second, 30*time.Second)
		w.log.Printf("lost the event stream (%v); again in %s", err, pause)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
}

// handshake waits until the server's config confirms this watcher's token.
// A config check (rex config check) starts a watcher the running config
// never knows, and so does a load that a newer one replaced: they end here
// once handshakeFor is up, before they claim anything.
func (w *watcher) handshake(ctx context.Context) error {
	deadline := w.now().Add(handshakeFor)
	pause := 250 * time.Millisecond
	for {
		token, err := w.token(ctx)
		switch {
		case err == nil && token == w.opts.Token:
			return nil
		case w.now().After(deadline) && err == nil:
			return fmt.Errorf("%w: not confirmed by the server (a config check, or a load a newer one replaced)", errSuperseded)
		case w.now().After(deadline):
			return fmt.Errorf("the server did not confirm the watcher: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
		pause = min(2*pause, 2*time.Second)
	}
}

func (w *watcher) token(ctx context.Context) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := w.api.Action(ctx, "", actionToken, nil, &out)
	return out.Token, err
}

// wanted checks the watcher is still the one the server's config started,
// and that rexkit is still there.
func (w *watcher) wanted(ctx context.Context) error {
	if w.exe != "" {
		if _, err := os.Stat(w.exe); errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s is gone", errSuperseded, w.exe)
		}
	}
	token, err := w.token(ctx)
	switch {
	case err == nil && token != w.opts.Token:
		return fmt.Errorf("%w: the config was reloaded", errSuperseded)
	case err != nil && strings.Contains(err.Error(), "no action named"):
		return fmt.Errorf("%w: the config no longer runs rexkit", errSuperseded)
	case err != nil && !w.alive(ctx):
		return fmt.Errorf("the server is gone (%v)", err)
	}
	return nil // a slow answer is no reason to stop
}

// alive reports the server still runs, the same run.
func (w *watcher) alive(ctx context.Context) bool {
	var status struct {
		InstanceID string `json:"instance_id"`
	}
	return w.call(ctx, "server.status", nil, &status) == nil && status.InstanceID == w.instance
}

// follow reads the event stream until it ends, re-reading everything a
// little after each burst of events (settle after the last, maxSettle after
// the first at most), and every resnap in any case.
func (w *watcher) follow(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := w.api.Watch(ctx)
	if err != nil {
		return err
	}
	snap := time.NewTimer(0) // a first snapshot at once
	defer snap.Stop()
	var burst time.Time // the first event since the last snapshot
	again := time.NewTicker(resnap)
	defer again.Stop()
	tick := time.NewTicker(SeenDwell / 2)
	defer tick.Stop()
	check := time.NewTicker(recheck)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return errors.New("the event stream ended")
			}
			if ev.Type == rex.EventKeymapChanged {
				if err := w.wanted(ctx); err != nil {
					return err
				}
			}
			now := time.Now()
			if burst.IsZero() {
				burst = now
			}
			snap.Reset(max(0, min(settle, burst.Add(maxSettle).Sub(now))))
		case <-snap.C:
			burst = time.Time{}
			w.refresh(ctx)
		case <-again.C:
			w.refresh(ctx)
		case <-tick.C:
			w.tick(ctx)
		case <-check.C:
			if err := w.wanted(ctx); err != nil {
				return err
			}
		}
	}
}

// refresh re-reads every session, tells the tracker, and writes what
// changed to the attention state.
func (w *watcher) refresh(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	inv, err := w.api.Snapshot(cctx)
	cancel()
	if err != nil {
		w.log.Printf("snapshot: %v", err)
		return
	}
	now := w.now()
	first := !w.looked
	w.inv, w.looked = inv, true
	w.follows(inv, first)
	changes := w.tracker.Update(Panes(inv, Recall(w.dirs), now), now)
	w.tracker.Here(w.guess().block, now)
	// Panes closed since the last complete snapshot are forgotten. Only
	// those: the attention state is shared with the watchers of other
	// servers, whose panes this one never sees.
	var gone []string
	if len(inv.Errors) == 0 {
		blocks := map[string]bool{}
		for _, s := range inv.Sessions {
			for _, win := range s.Windows {
				for _, l := range win.Layers {
					for _, b := range l.Blocks {
						blocks[b.ID] = true
					}
				}
			}
		}
		for b := range w.known {
			if !blocks[b] {
				gone = append(gone, b)
			}
		}
		w.known = blocks
	}
	w.remember(now, changes, gone)
	if first {
		w.log.Printf("first look: %s", ui.Plural(len(w.tracker.Panes()), "agent pane"))
	}
}

// follows notes the session where the focus moved since the last snapshot
// (another tab, another pane, a new session): the user's doing, mostly,
// and so the best guess at the session the app shows. Toasts never take
// the focus, so the watcher's own toasts do not move the guess.
func (w *watcher) follows(inv rex.Inventory, first bool) {
	focus := make(map[string]string, len(inv.Sessions))
	for _, s := range inv.Sessions {
		at := s.ActiveWindowID
		if win, ok := s.Window(at); ok {
			at += "/" + win.FocusedBlockID
		}
		focus[s.SessionID] = at
		if was, ok := w.focus[s.SessionID]; !first && (!ok || was != at) {
			w.active = s.SessionID
		}
	}
	w.focus = focus
}

// tick marks the pane in front seen once it has been there long enough,
// and shows the toast that is due.
func (w *watcher) tick(ctx context.Context) {
	now := w.now()
	seen, saw, toast, due := w.tracker.Tick(now)
	if saw {
		w.remember(now, []Change{seen}, nil)
	}
	if due {
		w.show(ctx, toast)
	}
}

// remember writes changes to the attention state, and forgets the panes
// that are gone. It takes the state's lock only when that changes
// something.
func (w *watcher) remember(now time.Time, changes []Change, gone []string) {
	apply := func(a *state.Attention) bool {
		changed := false
		for _, c := range changes {
			if c.Entered && a.Enter(c.Block, c.State, c.Kind, c.At) {
				changed = true
			}
			if c.Seen && a.See(c.Block, c.At) {
				changed = true
			}
		}
		if len(gone) > 0 && a.Forget(func(block string) bool { return !slices.Contains(gone, block) }) {
			changed = true
		}
		return changed
	}
	if probe := w.dirs.Attention(); !apply(&probe) {
		return
	}
	if err := w.dirs.UpdateAttention(now, func(a *state.Attention) { apply(a) }); err != nil {
		w.log.Printf("attention state: %v", err)
	}
}

// place is a block in a session.
type place struct{ session, block string }

// guess is the focused pane of the session where the focus last moved, or
// of the first session before it moved anywhere. Rex does not say which
// session the app shows (client.perform runs only the app's own actions,
// not the config's), so this is a guess.
func (w *watcher) guess() place {
	s, ok := w.inv.Session(w.active)
	if !ok {
		if len(w.inv.Sessions) == 0 {
			return place{}
		}
		s = slices.MinFunc(w.inv.Sessions, func(a, b rex.SessionView) int { return a.Order - b.Order })
	}
	p := place{session: s.SessionID}
	if win, ok := s.Window(s.ActiveWindowID); ok {
		p.block = win.FocusedBlockID
	}
	return p
}

// show puts a toast in the active window of every session but one whose
// focused pane is the agent it is about: the watcher cannot tell which
// session the app shows, and a toast where it is not seen just goes. A
// click on it goes to the pane it is about. The toast action runs once in
// each session, the guess first: run by `rex do`, an action reaches only
// its own session.
func (w *watcher) show(ctx context.Context, t Toast) {
	var sessions []string
	for _, s := range w.inv.Sessions {
		if win, ok := s.Window(s.ActiveWindowID); ok && len(t.About) == 1 && win.FocusedBlockID == t.About[0] {
			continue // the user, if there, is looking at it
		}
		sessions = append(sessions, s.SessionID)
	}
	if len(sessions) == 0 {
		return
	}
	if i := slices.Index(sessions, w.guess().session); i > 0 {
		sessions[0], sessions[i] = sessions[i], sessions[0]
	}
	args := map[string]any{"message": t.Text, "kind": spec(t).Kind, "status": spec(t).Status}
	for _, p := range w.tracker.Panes() {
		if p.Block == t.Block {
			args["go_session"], args["go_window"], args["go_block"] = p.Session, p.Window, p.Block
		}
	}
	if app := w.app(ctx); app != "" {
		args["client"] = app // a click switches it to the agent's session
	}
	shown := 0
	for _, s := range sessions {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		err := w.api.Action(cctx, s, actionToast, args, nil)
		cancel()
		if err != nil {
			w.log.Printf("toast in %s: %v", s, err)
			continue
		}
		shown++
	}
	if shown > 0 {
		w.log.Printf("toast in %s: %s", ui.Plural(shown, "session"), t.Text)
	}
}

// app is the app's client when exactly one is connected; with several,
// which one is in front is anyone's guess.
func (w *watcher) app(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	clients, err := w.api.Clients(ctx)
	if err != nil {
		return ""
	}
	var apps []string
	for _, c := range clients {
		if c.Info.Kind == rex.ClientApp {
			apps = append(apps, c.ID)
		}
	}
	if len(apps) != 1 {
		return ""
	}
	return apps[0]
}

func (w *watcher) call(ctx context.Context, method string, args, out any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.api.Call(ctx, method, args, out)
}

// LogPath is the watchers' log: what each did and why it stopped.
func LogPath(d state.Dirs) string { return filepath.Join(d.State, attendDir, "attend.log") }

// openLog points the watcher's log at LogPath, started afresh once it grows
// past maxLog.
func (w *watcher) openLog() io.Closer {
	w.log = log.New(io.Discard, "", 0)
	path := LogPath(w.dirs)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > maxLog {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil
	}
	w.log = log.New(f, fmt.Sprintf("attend[%d] ", os.Getpid()), log.LstdFlags)
	return f
}
