package switcher

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// actionTimeout bounds a jump: focus, result, history and closing the float.
const actionTimeout = 4 * time.Second

// doneMsg ends an action: the float is closed (nil err) or the action failed
// and the switcher stays open.
type doneMsg struct {
	err     error
	what    string // the action, for the toast: "jump", "bring it here"
	subject string // what it acted on: "pane"
}

// trigger is how a jump was asked for.
type trigger int

const (
	byHandshake trigger = iota // ⏎ written by Lua's handshake (popup.TakeCommit): Lua reads the result
	byKey                      // ⏎ typed while the picker mode was not active: nobody reads the result
	byClick                    // double-click: no handshake either
)

// goTo jumps to e. Within the origin session the jump is done here through
// the server; across sessions only the app can switch, which the Lua layer
// does after reading the result (the handshake), or Remote Control allows.
func (m *model) goTo(e entry, how trigger) tea.Cmd {
	if m.demo {
		return m.toast.Show(ui.ToastInfo, "Demo: this would go to "+describe(e))
	}
	local := e.sessionID == m.opts.origin.sessionID
	handshake := how == byHandshake
	switch {
	case local, handshake:
		return m.act("jump", e.kindName(), func(ctx context.Context) error { return m.jump(ctx, e, handshake) })
	case how == byKey: // ⌃⇧Space reaches rexkit_goto even when the picker mode is not active
		return m.toast.Show(ui.ToastWarn, e.sessionName()+" is another session", ui.Hint{Key: "ctrl+shift+space", Label: "to jump there"})
	}
	return m.act("jump", e.kindName(), func(ctx context.Context) error {
		if m.opts.client == "" {
			return errRemoteControl
		}
		err := m.api.ClientPerform(ctx, m.opts.client, "session.select",
			map[string]string{"session_id": e.sessionID, "window_id": e.windowID})
		if errors.Is(err, rex.ErrRefused) {
			return errRemoteControl
		}
		if err != nil {
			return err
		}
		return m.jump(ctx, e, handshake)
	})
}

// errRemoteControl means the app refused to switch sessions for a click.
var errRemoteControl = errors.New("double-click across sessions needs Remote Control")

// jump focuses e, hands the result to the Lua layer, records the visit and
// closes the float. handshake says whether Lua awaits the result (see finish).
func (m *model) jump(ctx context.Context, e entry, handshake bool) error {
	if err := m.focus(ctx, e); err != nil {
		return err
	}
	return m.finish(ctx, e, e.windowID, handshake)
}

// focus moves server-side focus to e. Going back to the origin needs nothing:
// closing the float restores focus (and zoom) exactly.
func (m *model) focus(ctx context.Context, e entry) error {
	o := m.opts.origin
	switch {
	case e.kind == ui.KindSession, o.at(e):
		return nil
	case e.kind == ui.KindTab:
		if e.windowID == o.windowID {
			return nil
		}
		return m.api.FocusWindow(ctx, e.sessionID, e.windowID)
	}
	return m.api.FocusBlock(ctx, e.sessionID, e.blockID)
}

// finish writes the result for the handshake, records the visit and closes
// the float. window is where the app should land. Without a handshake (a
// double-click, ⌥⏎, a ⏎ typed while the picker mode was not active or held
// past popup.HandshakeGrace) nobody reads the result, so nobody leaves the
// picker mode Lua entered for the tool either; finish leaves it itself.
func (m *model) finish(ctx context.Context, e entry, window string, handshake bool) error {
	if m.opts.stateDir != "" {
		r := popup.Result{Action: popup.ActionGoto, SessionID: e.sessionID, WindowID: window, BlockID: e.blockID}
		if _, err := popup.WriteResult(m.opts.stateDir, r); err != nil {
			return err
		}
	}
	if m.record != nil {
		now := m.now()
		origin := m.opts.origin.visit(now.Add(-time.Millisecond))
		// History is a convenience; a jump never fails over it.
		_ = m.record(func(h *state.History) {
			h.Visit(origin)
			h.Visit(e.visit(now))
		})
	}
	if !handshake {
		m.leaveModes(ctx)
	}
	return m.close(ctx)
}

// leaveModes ends the app client's key modes, as Lua's handshake does after
// applying a result. Only a tool Lua opened (it has a state dir) is in the
// picker mode. It needs Remote Control; when the app refuses, the stale mode
// still heals on the next key it binds (Lua's miss path).
func (m *model) leaveModes(ctx context.Context) {
	if m.opts.client != "" && m.opts.stateDir != "" {
		_ = m.api.ClientPerform(ctx, m.opts.client, "client.mode.exit_all", nil)
	}
}

// bringHere moves the selected pane next to the origin pane and focuses it.
func (m *model) bringHere(e entry) tea.Cmd {
	o := m.opts.origin
	switch {
	case !e.pane():
		return m.toast.Show(ui.ToastWarn, "Only panes can be brought here, not "+e.kindName()+"s")
	case e.sessionID != o.sessionID:
		return m.toast.Show(ui.ToastWarn, "Only panes of this session can be brought here")
	case e.blockID == o.blockID:
		return m.toast.Show(ui.ToastInfo, "That is where you are")
	}
	if m.demo {
		return m.toast.Show(ui.ToastInfo, "Demo: this would bring "+e.name+" here")
	}
	return m.act("bring it here", e.kindName(), func(ctx context.Context) error {
		req := rex.MoveBlockRequest{BlockID: e.blockID, AnchorID: o.blockID, Direction: m.splitDirection(ctx), Side: "after"}
		if err := m.api.MoveBlock(ctx, e.sessionID, req); err != nil {
			return err
		}
		if err := m.api.FocusBlock(ctx, e.sessionID, e.blockID); err != nil {
			return err
		}
		return m.finish(ctx, e, o.windowID, false)
	})
}

// splitDirection places a brought pane beside a wide origin and below a tall
// one (cells are about twice as tall as wide).
func (m *model) splitDirection(ctx context.Context) string {
	size, err := m.api.Size(ctx, m.opts.origin.sessionID, m.opts.origin.blockID)
	if err == nil && size.Columns < 2*size.Rows {
		return rex.Vertical
	}
	return rex.Horizontal
}

// dismiss closes the float without going anywhere; focus returns exactly to
// where it was.
func (m *model) dismiss() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.demo {
		return tea.Quit
	}
	return m.act("close", "", func(ctx context.Context) error {
		_ = popup.Answer(m.opts.stateDir, popup.ActionClose)
		return m.close(ctx)
	})
}

// act runs an action that ends by closing the float. Input is ignored until
// it is done; a jump or a move that takes a while says "Going…" in the tray.
func (m *model) act(what, subject string, fn func(context.Context) error) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	m.stopCapture()
	var going tea.Cmd
	if what != "close" {
		m.going.Start(m.now())
		going = m.clock.wake(m)
	}
	return tea.Batch(going, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		return doneMsg{err: fn(ctx), what: what, subject: subject}
	})
}

func (m *model) done(msg doneMsg) tea.Cmd {
	if msg.err == nil || msg.what == "close" {
		return tea.Quit // a failed close still ends the tool: there is nothing left to do
	}
	m.busy = false
	m.going.Stop()
	_ = popup.Answer(m.opts.stateDir, popup.ActionStay) // the switcher stays open
	refresh := ui.Hint{Key: "ctrl+r", Label: "refresh"}
	var toast tea.Cmd
	switch {
	case errors.Is(msg.err, rex.ErrNotFound):
		toast = m.toast.Show(ui.ToastErr, "That "+msg.subject+" closed", refresh)
	case errors.Is(msg.err, errRemoteControl):
		return m.toast.Show(ui.ToastWarn, "Double-click across sessions needs Remote Control", ui.Hint{Key: "enter", Label: "go"})
	default:
		toast = m.toast.Show(ui.ToastErr, "Can't "+msg.what+": "+rex.UserMessage(msg.err))
	}
	return tea.Batch(toast, m.collect())
}

// describe names an entry with its place: "studio › web › api".
func describe(e entry) string {
	if p := e.path(); p != "" {
		return p + " " + ui.GlyphCrumb + " " + e.name
	}
	return e.name
}

// stay answers a handshake key the switcher consumed without acting on it
// (the handshake in popup/result.go): Lua waits
// for a result after every ⏎, ⎋ and ⌃C it writes (and ⌥⏎, should it bind
// that).
func (m *model) stay(key string) {
	switch key {
	case "enter", "alt+enter", "esc", "ctrl+c":
		_ = popup.Answer(m.opts.stateDir, popup.ActionStay)
	}
}
