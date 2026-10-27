package search

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/ui"
)

// key handles a key press. Every ⏎ consumes Lua's commit marker first,
// whatever it then does: only a ⏎ that took one is the handshake's, whose
// result Lua reads (and so can open Scrollback).
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	var commit time.Time
	k := msg.String()
	if k == "enter" {
		commit, _ = popup.TakeCommitAt(m.cfg.stateDir)
	}
	if m.busy {
		return nil
	}
	if m.help {
		_, _, more := m.keyPanel(m.layout().areas)
		if at, ok := ui.ScrollKeyPanel(m.helpAt, more, k); ok {
			m.helpAt = at // a key panel cut short scrolls
			return nil
		}
		m.setHelp(false)
		switch k {
		case "?", "f1", "esc":
			m.stay(k) // they only close the key panel
			return nil
		}
		// any other key closes it and still does its job
	}
	switch k {
	case "esc", "ctrl+c":
		return tea.Quit
	case "f1":
		m.setHelp(true)
		return nil
	case "?":
		if !m.questionTyped() {
			m.setHelp(true)
			return nil
		}
		return m.edit(msg)
	case "enter":
		return m.enter(commit)
	case "alt+enter":
		var cmd tea.Cmd
		if r, ok := m.selected(); ok && r.kind != rowMore {
			cmd = m.goPane(r, false)
		}
		if !m.busy {
			m.stay(k)
		}
		return cmd
	case "right":
		if r, ok := m.selected(); ok && r.kind == rowMore {
			m.unfold(r)
			return nil
		}
		return m.edit(msg)
	case "up", "ctrl+k", "ctrl+p":
		m.list.Up()
		m.skipGap(true)
	case "down", "ctrl+j", "ctrl+n":
		m.list.Down()
		m.skipGap(false)
	case "pgup":
		m.list.PageUp()
		m.skipGap(true)
	case "pgdown":
		m.list.PageDown()
		m.skipGap(false)
	case "alt+up", "alt+down":
		m.stepPane(k == "alt+down")
	case "ctrl+u":
		m.preview.Scroll(-m.previewPage(), m.layout().areas.Preview.H)
		return nil
	case "ctrl+d":
		m.preview.Scroll(m.previewPage(), m.layout().areas.Preview.H)
		return nil
	case "tab":
		m.sessionOnly = !m.sessionOnly
		m.rebuild()
		return nil
	case "alt+r":
		m.regex = !m.regex
		return m.research(false)
	case "alt+c":
		m.matchCase = !m.matchCase
		return m.research(false)
	case "ctrl+r":
		return m.read()
	case "ctrl+/", "ctrl+_", "ctrl+7":
		m.previewPref = m.previewPref.Toggle(m.layout().areas.PreviewVisible())
		m.relayout()
		return nil
	case "ctrl+y":
		return m.copyLine()
	default:
		return m.edit(msg)
	}
	m.syncPreview()
	return nil
}

// edit passes a message to the query field and searches again when the
// query changed.
func (m *model) edit(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return tea.Batch(cmd, m.research(false))
}

// enter acts on the selected row: a match opens Scrollback there, a pane
// row goes to the pane, a fold unfolds. A ⏎ with nothing to act on while
// panes are still read or searched waits for them.
func (m *model) enter(commit time.Time) tea.Cmd {
	r, ok := m.selected()
	switch {
	case !ok && m.input.Value() != "" && (m.scanning() || !m.ready):
		return m.holdEnter(commit)
	case !ok:
		m.stay("enter")
		return nil
	case r.kind == rowMore:
		m.unfold(r)
		m.stay("enter")
		return nil
	case r.kind == rowPane:
		return m.goPane(r, !commit.IsZero())
	}
	return m.jump(r, !commit.IsZero())
}

// holdEnter keeps a ⏎ until there are results (runPending). A held
// handshake ⏎ answers "stay" popup.GraceLeft after Lua wrote it, before Lua
// gives up waiting; it is then dropped, as only the handshake can open
// Scrollback.
func (m *model) holdEnter(commit time.Time) tea.Cmd {
	m.pending, m.pendingCommit = true, commit
	m.pendingRev++
	if commit.IsZero() {
		return nil
	}
	rev := m.pendingRev
	return tea.Tick(popup.GraceLeft(commit), func(time.Time) tea.Msg { return stillPendingMsg{rev} })
}

// stillPendingMsg fires when the grace of a held handshake ⏎ is over.
type stillPendingMsg struct{ rev int }

func (m *model) stillPending(msg stillPendingMsg) tea.Cmd {
	if msg.rev != m.pendingRev || !m.pending {
		return nil // it ran already
	}
	m.pending = false
	m.stay("enter")
	return m.toasts.Show(ui.ToastInfo, "Still reading the panes", ui.Hint{Key: "enter", Label: "again when they are in"})
}

// runPending runs a held ⏎ once there is a row to act on. A handshake ⏎
// whose grace is over is dropped instead: Lua would stop waiting for the
// result before the action is done.
func (m *model) runPending() tea.Cmd {
	if !m.pending {
		return nil
	}
	if _, ok := m.selected(); !ok {
		if !m.scanning() && m.ready {
			m.pending = false // nothing found: there is nothing to do
			m.stay("enter")
		}
		return nil
	}
	m.pending = false
	if !m.pendingCommit.IsZero() && popup.GraceLeft(m.pendingCommit) == 0 {
		m.stay("enter")
		return m.toasts.Show(ui.ToastInfo, "The matches are in", ui.Hint{Key: "enter", Label: "again"})
	}
	return m.enter(m.pendingCommit)
}

// questionTyped reports whether ? goes into the query rather than opening
// the keys: in a regular expression, and once something is typed (a URL's
// query, "Continue?"). F1 always opens them.
func (m *model) questionTyped() bool { return m.regex || m.input.Value() != "" }

// unfold lists every hit of the selected fold's pane; the cursor stays in
// the fold's place, on the oldest of them.
func (m *model) unfold(r row) {
	m.groups[r.group].expanded = true
	cur := m.list.Cursor()
	m.buildRows()
	m.list.Select(cur)
	m.fitList()
	m.syncPreview()
}

// skipGap moves the cursor off the air between panes, on in the direction
// it was going; at either end of the list it turns back.
func (m *model) skipGap(up bool) {
	r, ok := m.selected()
	if !ok || r.kind != rowGap {
		return
	}
	if up {
		m.list.Up()
	} else {
		m.list.Down()
	}
	if r, ok := m.selected(); ok && r.kind == rowGap { // the end of the list
		m.list.Select(m.list.Cursor() + map[bool]int{true: 1, false: -1}[up])
	}
}

// stepPane moves to the next (or previous) pane's newest shown hit.
func (m *model) stepPane(down bool) {
	r, ok := m.selected()
	if !ok {
		return
	}
	target := r.group - 1
	if down {
		target = r.group + 1
	}
	if target < 0 || target >= len(m.groups) {
		return
	}
	for i, rr := range m.rows {
		if rr.group == target && rr.kind == rowHit {
			m.list.Select(i)
			return
		}
	}
}

// stay answers a handshake key the tool consumed without closing, so Lua
// keeps it open (the handshake in popup/result.go).
func (m *model) stay(key string) {
	switch key {
	case "enter", "alt+enter", "esc", "ctrl+c":
		_ = popup.Answer(m.cfg.stateDir, popup.ActionStay)
	}
}

// previewPage is how far ⌃U and ⌃D scroll the preview: half of it.
func (m *model) previewPage() int { return max(3, m.previewRows()/2) }

// wheelStep is how many rows a wheel notch moves.
const wheelStep = 3

// click handles a left click: a chip toggles, a row is selected and a
// double-click goes to its pane.
func (m *model) click(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft || m.busy {
		return nil
	}
	s := m.layout()
	x, y := mouse.X-s.inner.X, mouse.Y-s.inner.Y
	if m.help {
		if !m.keyPanelAt(s.areas).Contains(x, y) {
			m.setHelp(false) // a click outside the key panel closes it
		}
		return nil
	}
	if s.areas.Header.Contains(x, y) {
		for i, r := range s.hits.Chips {
			if r.Contains(x-s.areas.Header.X, 0) {
				return m.toggleChip(i)
			}
		}
		return nil
	}
	list := listRect(s.areas, ui.Rect{})
	if len(m.rows) == 0 || !list.Contains(x, y) {
		return nil
	}
	_, double, ok := m.list.Click(y-list.Y, m.cfg.now())
	r, sel := m.selected()
	switch {
	case !ok || !sel:
		return nil
	case r.kind == rowGap:
		m.skipGap(false)
	case double && r.kind == rowMore:
		m.unfold(r)
		return nil
	case double:
		return m.goPane(r, false)
	}
	m.syncPreview()
	return nil
}

// toggleChip is a click on chip i: match case, regex, this session.
func (m *model) toggleChip(i int) tea.Cmd {
	switch i {
	case 0:
		m.matchCase = !m.matchCase
	case 1:
		m.regex = !m.regex
	default:
		m.sessionOnly = !m.sessionOnly
		m.rebuild()
		return nil
	}
	return m.research(false)
}

// wheel scrolls whatever is under the pointer.
func (m *model) wheel(mouse tea.Mouse) {
	delta := wheelStep
	if mouse.Button == tea.MouseWheelUp {
		delta = -wheelStep
	} else if mouse.Button != tea.MouseWheelDown {
		return
	}
	s := m.layout()
	x, y := mouse.X-s.inner.X, mouse.Y-s.inner.Y
	switch {
	case s.areas.PreviewVisible() && s.areas.Preview.Contains(x, y):
		m.preview.Scroll(delta, s.areas.Preview.H)
	case s.areas.List.Contains(x, y) && len(m.rows) > 0:
		m.list.Wheel(delta)
		m.skipGap(delta < 0)
		m.syncPreview()
	}
}
