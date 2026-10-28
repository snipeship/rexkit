package attend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// fakeAPI is a Rex server for the watcher: its config's token, its
// sessions (set changes them), an event stream, and the actions the
// watcher ran.
type fakeAPI struct {
	mu       sync.Mutex
	token    string
	instance string
	inv      rex.Inventory
	events   chan rex.Event
	actions  []string // name and session of each action run: "rexkit_attend_toast s2"
	args     []map[string]any
	clients  []rex.ClientInfo
	gone     bool
}

func newFakeAPI(token string) *fakeAPI {
	return &fakeAPI{token: token, instance: "run-1", events: make(chan rex.Event, 16)}
}

func (f *fakeAPI) set(inv rex.Inventory) {
	f.mu.Lock()
	f.inv = inv
	f.mu.Unlock()
	f.events <- rex.Event{Type: "terminal.program_status_changed"}
}

func (f *fakeAPI) Call(_ context.Context, method string, _, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return rex.ErrUnavailable
	}
	if method == "server.status" {
		data, _ := json.Marshal(map[string]string{"instance_id": f.instance})
		return json.Unmarshal(data, out)
	}
	return nil
}

func (f *fakeAPI) Snapshot(context.Context) (rex.Inventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inv, nil
}

func (f *fakeAPI) Watch(context.Context) (<-chan rex.Event, error) { return f.events, nil }

func (f *fakeAPI) Action(_ context.Context, session, name string, args, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, strings.TrimSpace(name+" "+session))
	if m, ok := args.(map[string]any); ok {
		f.args = append(f.args, m)
	}
	if name == actionToken {
		if f.token == "" {
			return errors.New("rex do: no action named " + name)
		}
		data, _ := json.Marshal(map[string]string{"token": f.token})
		return json.Unmarshal(data, out)
	}
	return nil
}

func (f *fakeAPI) Clients(context.Context) ([]rex.ClientInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clients, nil
}

// toasts are the args of the toasts the watcher showed.
func (f *fakeAPI) toasts() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, a := range f.args {
		if _, ok := a["message"]; ok {
			out = append(out, a)
		}
	}
	return out
}

func (f *fakeAPI) ran(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.actions {
		if strings.HasPrefix(a, name) {
			out = append(out, a)
		}
	}
	return out
}

// twoSessions: the user works in s1 (where the layout last changed); an
// agent in s2 is in state st.
func twoSessions(st string) rex.Inventory {
	inv := rex.Inventory{Sessions: rex.List[rex.SessionView]{
		{Order: 0, View: rex.View{SessionID: "s1", Label: "work", ActiveWindowID: "w1", Windows: rex.List[rex.Window]{
			{ID: "w1", FocusedBlockID: "me", Layers: rex.List[rex.Layer]{{Kind: rex.LayerTiled,
				Blocks: rex.List[rex.Block]{{ID: "me", Terminal: &rex.Terminal{Process: proc("zsh", "/bin/zsh", "")}}}}}}}}},
		{Order: 1, View: rex.View{SessionID: "s2", Label: "agents", ActiveWindowID: "w2", Windows: rex.List[rex.Window]{
			{ID: "w2", Label: "api", FocusedBlockID: "agent", Layers: rex.List[rex.Layer]{{Kind: rex.LayerTiled,
				Blocks: rex.List[rex.Block]{{ID: "agent", Terminal: &rex.Terminal{Process: proc("claude", "/bin/claude", ""),
					Status: rex.List[rex.StatusRecord]{{App: "claude", State: st, Msg: "Tests pass", UpdatedAt: time.Now()}}}}}}}}}}},
	}}
	return inv
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// watch runs a watcher until the test ends, and waits for its pidfile.
func watch(t *testing.T, api *fakeAPI, d state.Dirs, opts WatchOptions) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, api, d, opts) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Watch = %v", err)
		}
	})
	eventually(t, "the pidfile", func() bool { return len(Running(d)) == 1 })
}

// lookedOnce waits for the watcher's first snapshot (its log says so).
func lookedOnce(t *testing.T, d state.Dirs) {
	t.Helper()
	eventually(t, "the first look", func() bool {
		data, _ := os.ReadFile(LogPath(d))
		return strings.Contains(string(data), "first look")
	})
}

// The toast shows in every session but the one where the agent is the
// focused pane: Rex does not say which session the app shows.
func TestWatcherToastsWhereTheAgentIsNotInFront(t *testing.T) {
	api := newFakeAPI("tok")
	api.inv = twoSessions(rex.StateWorking)
	d := state.Dirs{State: t.TempDir()}
	watch(t, api, d, WatchOptions{Token: "tok", NextKey: "alt+a", Toasts: []string{rex.StateDone}})
	lookedOnce(t, d)
	api.set(twoSessions(rex.StateDone))
	eventually(t, "a toast", func() bool { return len(api.toasts()) == 1 })
	if got := api.ran(actionToast); !slices.Equal(got, []string{actionToast + " s1"}) {
		t.Errorf("toast ran as %q, want in s1 only: the agent is in front in s2", got)
	}
	args := api.toasts()[0]
	if args["go_block"] != "agent" || !strings.Contains(args["message"].(string), "{b:claude} finished · Tests pass") {
		t.Errorf("toast args = %v", args)
	}
	if _, ok := args["client"]; ok {
		t.Errorf("toast names a client with no app connected: %v", args)
	}
	if m := d.Attention().Panes["agent"]; m.State != rex.StateDone || m.Since.IsZero() {
		t.Errorf("attention = %+v, want done entered", m)
	}
}

// An agent in a floating pane ⌃P w hid toasts as any other, and says it is
// hidden; one detached by something else is held: no news.
func TestWatcherToastsAHiddenAgent(t *testing.T) {
	api := newFakeAPI("tok")
	hide := func(st string) rex.Inventory {
		inv := twoSessions(st)
		s := &inv.Sessions[1]
		agent := s.Windows[0].Layers[0].Blocks[0]
		s.Detached = rex.List[rex.Block]{agent}
		s.Windows = slices.Clone(s.Windows)
		s.Windows[0].FocusedBlockID = "shell"
		s.Windows[0].Layers = rex.List[rex.Layer]{{Kind: rex.LayerTiled, Blocks: rex.List[rex.Block]{{ID: "shell"}}}}
		return inv
	}
	api.inv = hide(rex.StateWorking)
	d := state.Dirs{State: t.TempDir()}
	recordHidden(t, d, "s2", "w2", "agent")
	watch(t, api, d, WatchOptions{Token: "tok", NextKey: "alt+a", Toasts: []string{rex.StateDone}})
	lookedOnce(t, d)
	api.set(hide(rex.StateDone))
	eventually(t, "a toast", func() bool { return len(api.toasts()) > 0 })
	args := api.toasts()[0]
	if args["go_block"] != "agent" || !strings.Contains(args["message"].(string), "agents › api (hidden)") {
		t.Errorf("toast args = %v", args)
	}
}

// With the agent behind another pane of its session, that session gets the
// toast too, and so does every other session: the toast action runs once in
// each, since one run by `rex do` reaches only its own session. A click
// switches the one app connected.
func TestWatcherToastsEverySessionButTheAgents(t *testing.T) {
	api := newFakeAPI("tok")
	inv := func(st string) rex.Inventory {
		inv := twoSessions(st)
		inv.Sessions[1].Windows[0].FocusedBlockID = "shell"
		inv.Sessions[1].Windows[0].Layers[0].Blocks = append(inv.Sessions[1].Windows[0].Layers[0].Blocks,
			rex.Block{ID: "shell", Terminal: &rex.Terminal{Process: proc("zsh", "/bin/zsh", "")}})
		return inv
	}
	inv3 := func(st string) rex.Inventory {
		inv := inv(st)
		third := inv.Sessions[0]
		third.Order, third.SessionID, third.Label = 2, "s3", "notes"
		inv.Sessions = append(inv.Sessions, third)
		return inv
	}
	api.inv = inv3(rex.StateWorking)
	app := rex.ClientInfo{ID: "client:app"}
	app.Info.Kind = rex.ClientApp
	api.clients = []rex.ClientInfo{app, {ID: "client:cli"}}
	d := state.Dirs{State: t.TempDir()}
	watch(t, api, d, WatchOptions{Token: "tok", Toasts: []string{rex.StateBlocked}})
	lookedOnce(t, d)
	api.set(inv3(rex.StateBlocked))
	eventually(t, "the toasts", func() bool { return len(api.toasts()) == 3 })
	ran := api.ran(actionToast)
	slices.Sort(ran)
	if want := []string{actionToast + " s1", actionToast + " s2", actionToast + " s3"}; !slices.Equal(ran, want) {
		t.Errorf("toasts ran as %q, want one in each session %q", ran, want)
	}
	for _, args := range api.toasts() {
		if _, ok := args["sessions"]; ok || args["client"] != "client:app" {
			t.Errorf("toast args = %v, want the app's client and no session list", args)
		}
	}
}

// An agent streaming its status sends events many times a second: the
// watcher still looks, and toasts, while they go on.
func TestWatcherLooksWhileEventsNeverPause(t *testing.T) {
	api := newFakeAPI("tok")
	api.inv = twoSessions(rex.StateWorking)
	d := state.Dirs{State: t.TempDir()}
	watch(t, api, d, WatchOptions{Token: "tok", Toasts: []string{rex.StateDone}})
	lookedOnce(t, d)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				select {
				case api.events <- rex.Event{Type: "terminal.program_status_changed", SessionID: "s2"}:
				default:
				}
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	api.mu.Lock()
	api.inv = twoSessions(rex.StateDone)
	api.mu.Unlock()
	start := time.Now()
	eventually(t, "a toast while the events go on", func() bool { return len(api.toasts()) == 1 })
	if took := time.Since(start); took > maxSettle+Burst+2*time.Second {
		t.Errorf("the toast took %s", took)
	}
}

// The attention state is shared by the watchers of every server: a watcher
// forgets only panes of its own server that closed, and writes the state
// only when something changed.
func TestWatcherForgetsOnlyItsOwnClosedPanes(t *testing.T) {
	api := newFakeAPI("tok")
	api.inv = twoSessions(rex.StateDone)
	d := state.Dirs{State: t.TempDir()}
	elsewhere := state.Visit{Kind: "pane", SessionID: "other-server", BlockID: "theirs"}
	_ = d.UpdateAttention(time.Now(), func(a *state.Attention) {
		a.See("theirs", time.Now()) // a pane of another server
		a.Back = &elsewhere
	})
	watch(t, api, d, WatchOptions{Token: "tok"})
	lookedOnce(t, d)
	path := filepath.Join(d.State, "attention.json")
	before, _ := os.Stat(path)
	for range 3 {
		api.events <- rex.Event{Type: "terminal.process_changed", SessionID: "s2"}
		time.Sleep(2 * settle)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("rewrote the attention state with nothing new")
	}
	closed := twoSessions(rex.StateDone)
	closed.Sessions = closed.Sessions[:1] // the agent's session closed
	api.set(closed)
	eventually(t, "the closed agent forgotten", func() bool {
		_, ok := d.Attention().Panes["agent"]
		return !ok
	})
	a := d.Attention()
	if a.SeenAt("theirs").IsZero() || a.Back == nil || a.Back.BlockID != "theirs" {
		t.Errorf("attention = %+v, want another server's pane and walk kept", a)
	}
}

// recordHidden writes workspace.json as lua/floats.lua does: block hidden
// in session's tab window.
func recordHidden(t *testing.T, d state.Dirs, session, window, block string) {
	t.Helper()
	data := `{"` + session + `":{"hidden":{"` + window + `":{"layers":[{"bounds":[0.1,0.1,0.5,0.5],"tree":"` + block + `"}]}}}}`
	if err := os.WriteFile(filepath.Join(d.State, "workspace.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A tab that closes may leave floating panes hidden in it running: the
// watcher asks the Lua layer to sweep that session, and only that one.
func TestWatcherSweepsWhereATabClosed(t *testing.T) {
	api := newFakeAPI("tok")
	inv := twoSessions(rex.StateWorking)
	inv.Sessions[1].Windows = append(slices.Clone(inv.Sessions[1].Windows), rex.Window{ID: "w3", FocusedBlockID: "x",
		Layers: rex.List[rex.Layer]{{Kind: rex.LayerTiled, Blocks: rex.List[rex.Block]{{ID: "x"}}}}})
	inv.Sessions[1].Detached = rex.List[rex.Block]{{ID: "hidden"}}
	api.inv = inv
	d := state.Dirs{State: t.TempDir()}
	recordHidden(t, d, "s2", "w3", "hidden")
	watch(t, api, d, WatchOptions{Token: "tok"})
	lookedOnce(t, d)
	if got := api.ran(actionSweep); len(got) != 0 {
		t.Fatalf("swept %v with no tab closed", got)
	}
	closed := twoSessions(rex.StateWorking) // s2's second tab closed
	closed.Sessions[1].Detached = inv.Sessions[1].Detached
	api.set(closed)
	eventually(t, "a sweep", func() bool { return len(api.ran(actionSweep)) > 0 })
	if got := api.ran(actionSweep); got[0] != actionSweep+" s2" {
		t.Errorf("swept %v, want s2", got)
	}
}

// A tab may have closed while no watcher ran: the watcher sweeps where a
// hidden pane's tab is gone from its first look on, and nowhere else.
func TestWatcherSweepsHiddenPanesOfATabClosedBeforeIt(t *testing.T) {
	api := newFakeAPI("tok")
	inv := twoSessions(rex.StateWorking)
	inv.Sessions[0].Detached = rex.List[rex.Block]{{ID: "kept"}}   // its tab is open
	inv.Sessions[1].Detached = rex.List[rex.Block]{{ID: "orphan"}} // its tab is gone
	api.inv = inv
	d := state.Dirs{State: t.TempDir()}
	data := `{"s1":{"hidden":{"w1":{"layers":[{"bounds":[0.1,0.1,0.5,0.5],"tree":"kept"}]}}},` +
		`"s2":{"hidden":{"gone":{"layers":[{"bounds":[0.1,0.1,0.5,0.5],"tree":"orphan"}]}}}}`
	if err := os.WriteFile(filepath.Join(d.State, "workspace.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	watch(t, api, d, WatchOptions{Token: "tok"})
	lookedOnce(t, d)
	eventually(t, "a sweep", func() bool { return len(api.ran(actionSweep)) > 0 })
	if got := slices.Compact(api.ran(actionSweep)); !slices.Equal(got, []string{actionSweep + " s2"}) {
		t.Errorf("swept %v, want s2 only", got)
	}
}

// The guess at the session the app shows follows the focus: a session
// where another tab or pane got it. A toast (a layer that never takes the
// focus) does not move it.
func TestGuessFollowsTheFocus(t *testing.T) {
	w := &watcher{}
	three := func() rex.Inventory {
		inv := twoSessions(rex.StateWorking)
		third := inv.Sessions[1]
		third.Order, third.SessionID = 2, "s3"
		third.Windows = slices.Clone(third.Windows)
		third.Windows[0].ID, third.Windows[0].FocusedBlockID = "w3", "x"
		third.ActiveWindowID = "w3"
		inv.Sessions = append(inv.Sessions, third)
		return inv
	}
	look := func(inv rex.Inventory) {
		w.inv = inv
		w.follows(inv, !w.looked)
		w.looked = true
	}
	inv := three()
	look(inv)
	if g := w.guess(); g.session != "s1" || g.block != "me" {
		t.Fatalf("guess = %+v before the focus moved, want the first session", g)
	}
	toasted := three()
	toasted.Sessions[0].Windows[0].Layers = append(slices.Clone(toasted.Sessions[0].Windows[0].Layers),
		rex.Layer{Kind: rex.LayerFloating, Blocks: rex.List[rex.Block]{{ID: "toast", Label: "rexkit:toast"}}})
	look(toasted)
	if g := w.guess(); g.session != "s1" {
		t.Errorf("a toast moved the guess to %q", g.session)
	}
	tab := three()
	tab.Sessions[2].Windows = append(slices.Clone(tab.Sessions[2].Windows), rex.Window{ID: "w4", FocusedBlockID: "y"})
	tab.Sessions[2].ActiveWindowID = "w4" // a new tab in the third session
	look(tab)
	if g := w.guess(); g.session != "s3" || g.block != "y" {
		t.Errorf("guess = %+v, want the new tab of s3", g)
	}
}

// A reload starts the new watcher while the server still runs the old
// config: it waits for its own token.
func TestWatcherWaitsForItsLoad(t *testing.T) {
	api := newFakeAPI("old")
	d := state.Dirs{State: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, api, d, WatchOptions{Token: "new"}) }()
	time.Sleep(400 * time.Millisecond)
	api.mu.Lock()
	api.token = "new" // the load finished
	api.mu.Unlock()
	eventually(t, "the pidfile", func() bool { return len(Running(d)) == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Watch = %v", err)
	}
}

func TestWatcherEndsWhenSuperseded(t *testing.T) {
	api := newFakeAPI("tok")
	api.inv = twoSessions(rex.StateWorking)
	d := state.Dirs{State: t.TempDir()}
	done := make(chan error, 1)
	go func() { done <- Watch(context.Background(), api, d, WatchOptions{Token: "tok"}) }()
	eventually(t, "the pidfile", func() bool { return len(Running(d)) == 1 })
	api.mu.Lock()
	api.token = "newer" // the config was reloaded
	api.mu.Unlock()
	api.events <- rex.Event{Type: rex.EventKeymapChanged}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Watch = %v, want a clean end", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher kept running after a reload")
	}
	if len(Running(d)) != 0 {
		t.Error("the pidfile outlived the watcher")
	}
}

func TestWatcherOfAnotherLoadEnds(t *testing.T) {
	defer func(d time.Duration) { handshakeFor = d }(handshakeFor)
	handshakeFor = 600 * time.Millisecond
	api := newFakeAPI("live")
	d := state.Dirs{State: t.TempDir()}
	start := time.Now()
	if err := Watch(context.Background(), api, d, WatchOptions{Token: "from a config check"}); err != nil {
		t.Errorf("Watch = %v, want a clean end", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("a watcher the config does not know kept waiting")
	}
	if _, err := os.Stat(filepath.Join(d.State, attendDir, "run-1.json")); err == nil {
		t.Error("it wrote a pidfile")
	}
}

func TestWatcherEndsWhenTheServerGoes(t *testing.T) {
	api := newFakeAPI("tok")
	d := state.Dirs{State: t.TempDir()}
	done := make(chan error, 1)
	go func() { done <- Watch(context.Background(), api, d, WatchOptions{Token: "tok"}) }()
	eventually(t, "the pidfile", func() bool { return len(Running(d)) == 1 })
	api.mu.Lock()
	api.gone = true
	api.mu.Unlock()
	close(api.events)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "gone") {
			t.Errorf("Watch = %v, want the server gone", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher outlived its server")
	}
}

func TestClaimTakesOverFromAnOlderLoad(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	quiet := testLogger()
	release, err := claim(context.Background(), d, "run-1", "old", quiet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claim(context.Background(), d, "run-1", "old", quiet); !errors.Is(err, errSuperseded) {
		t.Errorf("a second watcher of the same load: %v, want superseded", err)
	}
	got := make(chan error, 1)
	go func() {
		r, err := claim(context.Background(), d, "run-1", "new", quiet)
		if err == nil {
			defer r()
		}
		got <- err
	}()
	time.Sleep(300 * time.Millisecond)
	release() // the older watcher goes on its own
	if err := <-got; err != nil {
		t.Errorf("the newer watcher did not take over: %v", err)
	}
}

func TestSweepRemovesOldRuns(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	dir := filepath.Join(d.State, attendDir)
	_ = os.MkdirAll(dir, 0o700)
	for _, name := range []string{"old-run.lock", "old-run.json"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(`{"pid": 1}`), 0o600)
	}
	release, err := claim(context.Background(), d, "run-2", "tok", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := os.Stat(filepath.Join(dir, "old-run.json")); err == nil {
		t.Error("kept the files of a server run nobody watches")
	}
	if infos := Running(d); len(infos) != 1 || infos[0].PID != os.Getpid() || infos[0].Token != "tok" {
		t.Errorf("Running = %+v", infos)
	}
}

func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// A lock file swept away between the open and the lock is not the lock: the
// next try takes the new one.
func TestLockFileSweptMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	l := &lockFile{path: path}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	l.f = f // opened; then a sweep removes it and another watcher makes it anew
	os.Remove(path)
	other := &lockFile{path: path}
	if !other.try() {
		t.Fatal("the other watcher did not get the new lock file")
	}
	defer other.close()
	if l.try() {
		t.Fatal("took the lock of a removed file")
	}
	if l.try() {
		t.Error("took the lock another watcher holds")
	}
	if !held(path) {
		t.Error("the lock file at the path is not held")
	}
}

// askToEnd signals only the watcher that holds the lock now, of another load.
func TestAskSignalsOnlyTheHolder(t *testing.T) {
	dir := t.TempDir()
	lockPath, infoPath := filepath.Join(dir, "run.lock"), filepath.Join(dir, "run.json")
	sleeper := exec.Command("/bin/sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(exited) }()
	defer sleeper.Process.Kill()
	_ = state.WriteJSON(infoPath, Info{PID: sleeper.Process.Pid, Token: "old"})
	taken := func() bool { return true }
	askToEnd(context.Background(), taken, infoPath, lockPath, "new", testLogger()) // nobody holds the lock: a stale pidfile
	askToEnd(context.Background(), taken, infoPath, lockPath, "old", testLogger()) // our own load
	select {
	case <-exited:
		t.Fatal("signalled the PID of a stale pidfile")
	case <-time.After(200 * time.Millisecond):
	}
	holder := &lockFile{path: lockPath}
	if !holder.try() {
		t.Fatal("cannot take the lock")
	}
	defer holder.close()
	askToEnd(context.Background(), taken, infoPath, lockPath, "new", testLogger())
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Error("did not ask the holder to end")
	}
}
