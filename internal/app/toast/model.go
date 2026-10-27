package toast

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// lifetimes are how long each kind stays when --ttl is not given: problems
// stay long enough to be read.
var lifetimes = map[ui.ToastKind]time.Duration{
	ui.ToastOK:   ui.ToastOKTTL,
	ui.ToastInfo: ui.ToastInfoTTL,
	ui.ToastWarn: 6 * time.Second,
	ui.ToastErr:  8 * time.Second,
}

// parseKind reads --kind.
func parseKind(s string) (ui.ToastKind, error) {
	switch strings.ToLower(s) {
	case "ok", "success":
		return ui.ToastOK, nil
	case "info", "":
		return ui.ToastInfo, nil
	case "warn", "warning":
		return ui.ToastWarn, nil
	case "err", "error":
		return ui.ToastErr, nil
	}
	return 0, fmt.Errorf("toast: --kind must be ok, info, warn or err, not %q", s)
}

// parseStatus reads --status: the state of the agent the toast is about.
func parseStatus(s string) (ui.Status, error) {
	switch strings.ToLower(s) {
	case "":
		return ui.StatusNone, nil
	case "blocked":
		return ui.StatusBlocked, nil
	case "error", "failed":
		return ui.StatusFailed, nil
	case "done":
		return ui.StatusDone, nil
	}
	return 0, fmt.Errorf("toast: --status must be blocked, error or done, not %q", s)
}

// expiredMsg ends the toast.
type expiredMsg struct{}

// frameMsg draws the next frame of the toast growing in or shrinking away.
type frameMsg struct{}

// frameEvery is the pace of the enter and leave frames.
const frameEvery = 16 * time.Millisecond

// model is a notification in the top-right corner of a strip along the
// window's top (ui.ToastStrip). It grows in a row at a time, and when its
// time is up (or on a click) shrinks the same way and quits.
type model struct {
	kind   ui.ToastKind
	status ui.Status // an agent's state, for an agent's toast (ui.Toast.Status)
	text   string
	// jump goes to the agent the toast is about (a click on it); nil when
	// it is about none.
	jump   func(context.Context) error
	ttl    time.Duration
	scheme ui.Scheme
	theme  ui.Theme
	w, h   int

	shown   int  // rows drawn, from the top: 1…ui.ToastRows
	leaving bool // shrinking away
	going   bool // jump runs: the toast quits once it is done
	ticking bool // a frameMsg is on its way
}

func newModel(kind ui.ToastKind, text string, ttl time.Duration) *model {
	if ttl <= 0 {
		ttl = lifetimes[kind]
	}
	m := &model{kind: kind, text: sanitize.Label(text), ttl: ttl, scheme: ui.NewScheme(), shown: 1}
	m.theme = m.scheme.Theme()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), tea.Tick(m.ttl, func(time.Time) tea.Msg { return expiredMsg{} }), m.frame())
}

// frame schedules the next frame, unless one is on its way.
func (m *model) frame() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(frameEvery, func(time.Time) tea.Msg { return frameMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case frameMsg:
		m.ticking = false
		switch {
		case m.leaving && m.shown <= 1 && m.going:
			return m, nil // jumpedMsg quits
		case m.leaving && m.shown <= 1:
			return m, tea.Quit
		case m.leaving:
			m.shown--
			return m, m.frame()
		case m.shown < ui.ToastRows:
			m.shown++
			return m, m.frame()
		}
	case jumpedMsg:
		m.going = false
		if m.leaving && m.shown <= 1 {
			return m, tea.Quit
		}
	case tea.MouseClickMsg:
		// A click on an agent's toast goes to the agent; any other click
		// dismisses it early.
		m.leaving = true
		mouse := msg.Mouse()
		if m.jump != nil && !m.going && mouse.Button == tea.MouseLeft && m.theme.ToastHit(m.toast(), m.w, m.h, mouse.X, mouse.Y) {
			m.going = true
			return m, tea.Batch(m.goThere(), m.frame())
		}
		return m, m.frame()
	case expiredMsg, tea.KeyPressMsg:
		// A key (if it ever gets focus) dismisses it early.
		m.leaving = true
		return m, m.frame()
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if !m.scheme.Settled() || m.w <= 0 || m.h <= 0 {
		return v
	}
	v.Content = m.render()
	return v
}

// Rows is the height of the strip the toast draws in: a row of air under
// the window's top edge, then the toast with its edge rows. Lua's
// TOAST_ROWS matches it.
const Rows = ui.ToastRows + 1

// render draws the toast in the strip, every other cell ui.Clear: the panes
// show around it.
func (m *model) render() string {
	return strings.Join(m.theme.ToastStrip(m.toast(), m.w, m.h, m.shown), "\n")
}

func (m *model) toast() ui.Toast { return ui.Toast{Kind: m.kind, Text: m.text, Status: m.status} }

// jumpTimeout bounds going to the agent.
const jumpTimeout = 4 * time.Second

// goThere goes to the toast's agent while the toast shrinks away. A failure
// is not worth a word: the toast was only a pointer.
func (m *model) goThere() tea.Cmd {
	jump := m.jump
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), jumpTimeout)
		defer cancel()
		_ = jump(ctx)
		return jumpedMsg{}
	}
}

// jumpedMsg: going to the agent is over.
type jumpedMsg struct{}
