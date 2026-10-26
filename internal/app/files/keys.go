package files

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/ui"
)

// heldBackspace is how long after ⌫ empties the query a further ⌫ still
// counts as part of the same press (a held key must not walk up folders).
const heldBackspace = 400 * time.Millisecond

// verbTimeout bounds a verb's calls to Rex and helper programs.
const verbTimeout = 10 * time.Second

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	// Every ⏎ takes Lua's commit marker, whatever it then does: only a ⏎
	// that took one came from the handshake, which waits for an answer.
	var commit time.Time // when Lua wrote the marker; zero: not the handshake's ⏎
	if k == "enter" {
		commit, _ = popup.TakeCommitAt(m.cfg.launcher.stateDir)
	}
	if m.busy {
		return nil // a terminal is being opened; Files ends when it is
	}
	switch {
	case k == "ctrl+c":
		return m.quit()
	case m.help:
		// A key panel cut short scrolls; ? and ⎋ close it; any other key
		// closes it and does its job.
		_, _, more := m.keyPanel(m.layout().areas)
		if at, ok := ui.ScrollKeyPanel(m.helpAt, more, k); ok {
			m.helpAt = at
			return nil
		}
		m.setHelp(false)
		if k == "?" || k == "esc" {
			m.stay(k)
			return nil
		}
	}
	switch {
	case m.path.active:
		return m.pathKey(msg)
	}
	switch k {
	case "esc":
		return m.quit()
	case "?":
		m.setHelp(true)
		return nil
	case "enter":
		m.answerFailure = !commit.IsZero() // Lua waits for this ⏎: a failed verb must answer
		m.commitAt = commit
		return m.act(verbTab)
	case "alt+enter":
		return m.act(verbRight)
	case "shift+enter":
		return m.act(verbBelow)
	case "ctrl+e":
		return m.act(verbEdit)
	case "ctrl+y":
		return m.act(verbCopy)
	case "ctrl+x":
		return m.act(verbOpen)
	case "ctrl+f":
		return m.toggleRecursive()
	case "alt+.":
		return m.toggleHidden()
	case "ctrl+l":
		return m.openPath()
	case "ctrl+r":
		return m.refresh()
	case "right":
		if m.query.Position() < len([]rune(m.query.Value())) {
			break // move the text cursor
		}
		if r, ok := m.selected(); ok && r.e.isDir() {
			return m.navigate(r.e.path, "")
		}
		return nil // a file, or the folder itself: there is nothing to enter
	case "left":
		if m.query.Value() == "" {
			return m.goBack()
		}
	case "backspace":
		if m.query.Value() == "" {
			if m.cfg.now().Sub(m.emptiedAt) < heldBackspace {
				m.emptiedAt = m.cfg.now() // still held: keep swallowing
				return nil
			}
			return m.goBack()
		}
	}
	if cmd, ok := m.common(k); ok {
		return cmd
	}
	return m.edit(msg)
}

// common handles the keys that work the same in the list and the path
// field: moving, paging, scrolling the preview and toggling it.
func (m *model) common(k string) (tea.Cmd, bool) {
	a := m.layout().areas
	switch k {
	case "up", "down", "pgup", "pgdown":
		m.move(k)
	case "ctrl+k", "ctrl+j": // the fzf habit (an early ⌃J is ⏎, see ui.EarlyKeys)
		m.move(map[string]string{"ctrl+k": "up", "ctrl+j": "down"}[k])
	case "ctrl+u":
		m.preview.Scroll(-max(1, a.Preview.H/2), a.Preview.H)
		return nil, true
	case "ctrl+d":
		m.preview.Scroll(max(1, a.Preview.H/2), a.Preview.H)
		return nil, true
	case "ctrl+/", "ctrl+_", "ctrl+7":
		m.previewPref = m.previewPref.Toggle(a.PreviewVisible())
		return m.relayout(), true
	default:
		return nil, false
	}
	return m.selectionChanged(), true
}

// move moves the selection. Resting on the folder itself (nothing
// selected) is the position above the first row: ↓ leaves it for the first
// row, ↑ wraps to the last, and the ends of the list lead back to it.
func (m *model) move(k string) {
	n, i := m.list.Len(), m.list.Cursor()
	switch {
	case n == 0:
		return
	case !m.canRestHere():
		m.here = false
	case m.here && k == "pgup":
		return // already at the top
	case m.here:
		m.here = false
		switch k {
		case "up":
			m.list.Bottom()
		case "pgdown":
			m.list.PageDown()
		}
		return
	case (k == "down" && i == n-1) || ((k == "up" || k == "pgup") && i == 0):
		m.rest()
		return
	}
	switch k {
	case "up":
		m.list.Up()
	case "down":
		m.list.Down()
	case "pgup":
		m.list.PageUp()
	case "pgdown":
		m.list.PageDown()
	}
}

func (m *model) pathKey(msg tea.KeyPressMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc":
		m.stay(k) // ⎋ only leaves the path field
		return m.closePath()
	case "enter":
		m.stay(k)
		return m.goPath()
	case "tab":
		return m.tabComplete()
	case "alt+.":
		return m.toggleHidden()
	case "?":
		m.setHelp(true) // as in the list: "? keys" is always there
		return nil
	default:
		if cmd, ok := m.common(k); ok {
			return cmd
		}
	}
	return m.edit(msg)
}

// edit sends a key or paste to the field being typed in and refreshes the
// rows when the text changed.
func (m *model) edit(msg tea.Msg) tea.Cmd {
	in := &m.query
	if m.path.active {
		in = &m.path.input
	}
	before := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if in.Value() == before {
		return cmd
	}
	if m.path.active {
		return tea.Batch(cmd, m.complete(""))
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "backspace" && in.Value() == "" {
		m.emptiedAt = m.cfg.now()
	}
	// While searching the best match leads; clearing the search keeps what
	// was found selected, now shown among its neighbours.
	keep := ""
	if in.Value() == "" {
		keep = m.selectedPath()
	}
	return tea.Batch(cmd, m.rebuild(keep))
}

// target is what verbs act on now.
func (m *model) target() target {
	if r, ok := m.selected(); ok {
		return target{path: r.e.path, dir: folderOf(r.e), file: r.e.isRegular()}
	}
	return target{path: m.dir, dir: m.dir}
}

// act runs a verb in the background. Terminal verbs end the tool when they
// succeed; the others report with a toast.
func (m *model) act(v verb) tea.Cmd {
	if m.busy {
		return nil
	}
	if !m.settled() {
		m.pending = &v // typed before the rows caught up: act once they have
		if !m.answerFailure {
			return nil
		}
		m.pendingRev++
		rev := m.pendingRev
		return tea.Tick(popup.GraceLeft(m.commitAt), func(time.Time) tea.Msg { return stillPendingMsg{rev} })
	}
	t := m.target()
	if v == verbEdit && !t.file {
		return m.toast.Show(ui.ToastInfo, "Choose a file to edit")
	}
	var busy tea.Cmd
	if v.opens() {
		busy = m.setBusy(true)
	}
	l := m.cfg.launcher
	return tea.Batch(busy, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), verbTimeout)
		defer cancel()
		return l.do(ctx, v, t)
	})
}

// stillPendingMsg fires popup.HandshakeGrace after Lua wrote a handshake ⏎
// that is held for the rows: a large listing can take seconds, longer than
// Lua waits, so Files answers "stay" then and runs the verb by itself once
// the rows settle.
type stillPendingMsg struct{ rev int }

func (m *model) stillPending(msg stillPendingMsg) tea.Cmd {
	if msg.rev != m.pendingRev || m.pending == nil || !m.answerFailure {
		return nil // the verb ran, or was answered already
	}
	m.answerFailure = false
	m.cfg.launcher.answer(popup.ActionStay)
	return m.toast.Show(ui.ToastInfo, "Still listing", ui.Hint{Key: "enter", Label: "runs when it's ready"})
}

// activate is a double-click: folders open, files are edited. In the path
// field it accepts the completion first, as ⏎ does.
func (m *model) activate(i int) tea.Cmd {
	e := m.rows[i].e
	if m.path.active && (e.isDir() || e.isRegular()) {
		m.leavePath()
	}
	switch {
	case e.isDir():
		return m.navigate(e.path, "")
	case e.isRegular():
		return m.act(verbEdit)
	}
	return nil
}

func (m *model) click(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	s := m.layout()
	x, y := msg.X-s.inner.X, msg.Y-s.inner.Y
	a := s.areas
	if m.help {
		// A click on the key panel stays there; one outside it closes it.
		if !m.keyPanelAt(a).Contains(x, y) {
			m.setHelp(false)
		}
		return nil
	}
	switch {
	case a.Header.Contains(x, y):
		return m.clickHeader(x-a.Header.X, y-a.HeaderLine(), a)
	case a.List.Contains(x, y):
		i, double, ok := m.list.Click(y-a.List.Y, m.cfg.now())
		if !ok {
			return nil
		}
		m.here = false
		if double {
			return m.activate(i)
		}
		return m.selectionChanged()
	}
	return nil
}

// clickHeader handles a click at column x of the header, dy rows from its
// text row (a shape's edge rows above and below it count as the shape): a
// breadcrumb goes to that folder; the current one selects the folder
// itself (so ⏎ opens it), and a click on it once selected opens the path
// field. A chip toggles.
func (m *model) clickHeader(x, dy int, a ui.Areas) tea.Cmd {
	_, hits, actions := m.headerView(a.Header.W, a.Banded())
	if dy == 0 {
		for i, r := range hits.Crumbs {
			if !r.Contains(x, 0) {
				continue
			}
			cs := crumbs(m.dir, m.cfg.home)
			if i == len(cs)-1 {
				if m.here || !m.canRestHere() {
					return m.openPath()
				}
				m.rest()
				return m.selectionChanged()
			}
			return m.navigate(cs[i].path, cs[i+1].path)
		}
	}
	if dy < -1 || dy > 1 {
		return nil // the band's row of air
	}
	for i, r := range hits.Chips {
		if r.Contains(x, 0) && actions[i] != nil {
			return actions[i]()
		}
	}
	return nil
}

func (m *model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := 3
	if msg.Button == tea.MouseWheelUp {
		delta = -3
	} else if msg.Button != tea.MouseWheelDown {
		return nil
	}
	s := m.layout()
	if s.areas.Preview.Contains(msg.X-s.inner.X, msg.Y-s.inner.Y) {
		m.preview.Scroll(delta, s.areas.Preview.H)
		return nil
	}
	if m.here && delta < 0 {
		return nil // already at the top
	}
	m.here = false
	m.list.Wheel(delta)
	return m.selectionChanged()
}
