package search

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// actionTimeout bounds a jump: the page, focus, result and closing the float.
const actionTimeout = 4 * time.Second

// jumpAction is the Lua action that opens Scrollback over the pane a jump
// went to (lua/tools.lua), with jumpArgs.
const jumpAction = "rexkit_search_jump"

// jumpArgs is what rexkit_search_jump needs: the pane, the capture that was
// searched, the line and the search.
type jumpArgs struct {
	SessionID string `json:"session_id"`
	WindowID  string `json:"window_id"`
	BlockID   string `json:"block_id"`
	Page      string `json:"page"`
	Line      int    `json:"line"`
	Search    string `json:"search"`
	Regex     bool   `json:"regex"`
	MatchCase bool   `json:"match_case"`
}

// doneMsg ends an action: the float is closed (nil err) or the action failed
// and the tool stays open.
type doneMsg struct {
	err  error
	what string // for the toast: "jump", "go there"
}

// errRemoteControl means the app refused to switch sessions for a key that
// came without Lua's handshake.
var errRemoteControl = errors.New("going to another session needs Remote Control")

// jump is ⏎ on a match: go to its pane and open Scrollback there, on the
// capture that was searched (it cannot drift from the line shown), at the
// line, with the query as its search. Only Lua can open Scrollback over
// another pane, so it needs the handshake.
func (m *model) jump(r row, handshake bool) tea.Cmd {
	g := m.groups[r.group]
	p, line := m.panes[g.pane], g.hits[r.hit]+1
	switch {
	case m.cfg.demo:
		return m.toasts.Show(ui.ToastInfo, "Demo: this would open line "+strconv.Itoa(line)+" of "+p.name+" in Scrollback")
	case !handshake:
		m.stay("enter")
		return m.toasts.Show(ui.ToastWarn, "Reopen Search to open Scrollback from it", ui.Hint{Key: "alt+enter", Label: "go to the pane"})
	}
	tx, q := p.text, m.shown
	return m.act("jump", func(ctx context.Context) error {
		dir, err := m.cfg.spool()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "page.txt")
		args, _ := json.Marshal(jumpArgs{SessionID: p.session, WindowID: p.window, BlockID: p.block, Page: path,
			Line: line, Search: q.Text, Regex: q.Regex, MatchCase: q.MatchCase})
		r := popup.Result{Action: popup.ActionGoto, SessionID: p.session, WindowID: p.window, BlockID: p.block,
			Invoke: jumpAction, Args: args}
		if err := m.focus(ctx, p); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
		// The result goes first, as Lua waits for it only so long; the page
		// (tens of MB, maybe) is read only once the float is gone.
		if _, err := popup.WriteResult(m.cfg.stateDir, r); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
		if err := writePage(path, tx); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
		return m.cfg.close(ctx)
	})
}

// writePage writes the capture for Scrollback whole or not at all: it is
// written aside, then renamed into place.
func writePage(path string, t text) error {
	part := path + ".part"
	if err := os.WriteFile(part, []byte(t.page()), 0o600); err != nil {
		return err
	}
	return os.Rename(part, path)
}

// goPane goes to the selected row's pane. Within the origin session the
// server does it; across sessions only the app can switch, which Lua does
// after reading the result (the handshake), or Remote Control allows.
func (m *model) goPane(r row, handshake bool) tea.Cmd {
	p := m.panes[m.groups[r.group].pane]
	if m.cfg.demo {
		return m.toasts.Show(ui.ToastInfo, "Demo: this would go to "+p.sessionLabel+" "+ui.GlyphCrumb+" "+p.tabLabel+" "+ui.GlyphCrumb+" "+p.name)
	}
	local := p.session == m.cfg.origin.session
	return m.act("go there", func(ctx context.Context) error {
		if !local && !handshake {
			if m.cfg.client == "" {
				return errRemoteControl
			}
			err := m.cfg.api.ClientPerform(ctx, m.cfg.client, "session.select",
				map[string]string{"session_id": p.session, "window_id": p.window})
			if errors.Is(err, rex.ErrRefused) {
				return errRemoteControl
			}
			if err != nil {
				return err
			}
		}
		if err := m.focus(ctx, p); err != nil {
			return err
		}
		if m.cfg.stateDir != "" {
			res := popup.Result{Action: popup.ActionGoto, SessionID: p.session, WindowID: p.window, BlockID: p.block}
			if _, err := popup.WriteResult(m.cfg.stateDir, res); err != nil {
				return err
			}
		}
		if !handshake {
			m.leaveModes(ctx)
		}
		return m.cfg.close(ctx)
	})
}

// focus moves the server's focus to p. Going back to the origin needs
// nothing: closing the float restores focus (and zoom) exactly.
func (m *model) focus(ctx context.Context, p *pane) error {
	if p.block == m.cfg.origin.block {
		return nil
	}
	return m.cfg.api.FocusBlock(ctx, p.session, p.block)
}

// leaveModes ends the app client's key modes, as Lua's handshake does after
// applying a result: nobody reads a result written without one. Only a tool
// Lua opened (it has a state dir) is in the picker mode.
func (m *model) leaveModes(ctx context.Context) {
	if m.cfg.client != "" && m.cfg.stateDir != "" {
		_ = m.cfg.api.ClientPerform(ctx, m.cfg.client, "client.mode.exit_all", nil)
	}
}

// act runs an action that ends by closing the float. Keys wait until it is
// done.
func (m *model) act(what string, fn func(context.Context) error) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true // the reads go on: the action may fail, and closing stops them
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		return doneMsg{err: fn(ctx), what: what}
	}
}

func (m *model) done(msg doneMsg) tea.Cmd {
	if msg.err == nil {
		return tea.Quit
	}
	m.busy = false
	_ = popup.Answer(m.cfg.stateDir, popup.ActionStay) // the tool stays open
	switch {
	case errors.Is(msg.err, rex.ErrNotFound):
		return m.toasts.Show(ui.ToastErr, "That pane closed", ui.Hint{Key: "ctrl+r", Label: "read again"})
	case errors.Is(msg.err, errRemoteControl):
		way := ui.Hint{Key: "enter", Label: "go"} // ⏎ goes with Lua's handshake
		if r, ok := m.selected(); ok && r.kind == rowHit {
			way.Label = "jump" // and on a match opens Scrollback
		}
		return m.toasts.Show(ui.ToastWarn, "Going to another session needs Remote Control", way)
	}
	return m.toasts.Show(ui.ToastErr, "Can't "+msg.what+": "+rex.UserMessage(msg.err))
}

// copyLine puts the selected match's line on the clipboard.
func (m *model) copyLine() tea.Cmd {
	r, ok := m.selected()
	if !ok || r.kind != rowHit {
		return nil
	}
	g := m.groups[r.group]
	p, n := m.panes[g.pane], g.hits[r.hit]
	text, what := p.plain[n], "Copied line "+strconv.Itoa(n+1)+" of "+p.name
	if m.cfg.copy == nil {
		return m.toasts.Show(ui.ToastOK, what)
	}
	copyText := m.cfg.copy
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		if err := copyText(ctx, text); err != nil {
			return copiedMsg{err: err}
		}
		return copiedMsg{what: what}
	}
}

// copiedMsg says how a copy went.
type copiedMsg struct {
	what string
	err  error
}

func (m *model) copied(msg copiedMsg) tea.Cmd {
	if msg.err != nil {
		return m.toasts.Show(ui.ToastErr, "Couldn't copy: "+msg.err.Error())
	}
	return m.toasts.Show(ui.ToastOK, msg.what)
}
