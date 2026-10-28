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

// model is a notification on a float of its own in the window's top-right
// corner (ui.ToastRow): Rex draws the float, its header (the toast's title)
// and its shadow. It quits when its time is up, on a key, or on a click,
// which first goes to the agent the toast is about, if any.
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
	going  bool // jump runs: the toast quits once it is done
}

func newModel(kind ui.ToastKind, text string, ttl time.Duration) *model {
	if ttl <= 0 {
		ttl = lifetimes[kind]
	}
	m := &model{kind: kind, text: sanitize.Label(text), ttl: ttl, scheme: ui.NewScheme()}
	m.theme = m.scheme.Theme()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), tea.Tick(m.ttl, func(time.Time) tea.Msg { return expiredMsg{} }))
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case jumpedMsg:
		return m, tea.Quit
	case tea.MouseClickMsg:
		// A click on an agent's toast goes to the agent; any other click
		// dismisses it early.
		if m.going {
			return m, nil
		}
		if m.jump != nil && msg.Mouse().Button == tea.MouseLeft {
			m.going = true
			return m, m.goThere()
		}
		return m, tea.Quit
	case expiredMsg, tea.KeyPressMsg:
		// A key (if it ever gets focus) dismisses it early.
		if !m.going {
			return m, tea.Quit
		}
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

// render draws the toast on its first row, with the rest of the float left
// to Rex: a float can come out a row taller than asked.
func (m *model) render() string {
	rows := make([]string, max(1, m.h))
	rows[0] = m.theme.ToastRow(m.toast(), m.w)
	for i := 1; i < len(rows); i++ {
		rows[i] = strings.Repeat(" ", m.w)
	}
	return ui.Bleed(strings.Join(rows, "\n"))
}

func (m *model) toast() ui.Toast { return ui.Toast{Kind: m.kind, Text: m.text, Status: m.status} }

// jumpTimeout bounds going to the agent.
const jumpTimeout = 4 * time.Second

// goThere goes to the toast's agent. A failure is not worth a word: the
// toast was only a pointer.
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
