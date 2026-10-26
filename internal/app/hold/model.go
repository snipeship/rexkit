package hold

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/ui"
)

// clockEvery redraws the "parked 3m ago" line.
const clockEvery = 30 * time.Second

// Hint is one "key → what it does" line of the card. Gone is what the key
// does once the parked pane has exited; a hint without it is left out then.
type Hint struct{ Key, Label, Gone string }

// parseHint reads "⌃P e|bring it back" (or "ctrl+p e|bring it back"), and
// what the key does once the pane has exited after a second "|".
func parseHint(s string) Hint {
	key, label, _ := strings.Cut(s, "|")
	label, gone, _ := strings.Cut(label, "|")
	return Hint{Key: strings.TrimSpace(key), Label: strings.TrimSpace(label), Gone: strings.TrimSpace(gone)}
}

// Card is what the hold screen says. None of its text is trusted: the
// message carries the parked pane's name, which can fall back to its process
// name, so the view sanitizes all of it.
type Card struct {
	Message []string // the first line is the headline
	Hints   []Hint
	Since   time.Time // when the pane was parked; zero hides the line
}

// clockMsg redraws relative times.
type clockMsg struct{}

func clockAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clockMsg{} })
}

// model is the hold screen. It swallows every key: the pane exists only to
// keep its slot in the layout until the parked pane comes back.
type model struct {
	card   Card
	scheme ui.Scheme
	theme  ui.Theme
	w, h   int

	watch    watcher // nil: no live status
	status   parked
	every    time.Duration // the wait before the next look
	failed   bool          // the last look failed
	spinning bool
	demo     bool // q quits, so a demo is not a trap
	now      func() time.Time
}

func newModel(c Card, w watcher, demo bool) *model {
	m := &model{card: c, scheme: ui.NewScheme(), watch: w, demo: demo, now: time.Now, every: pollEvery}
	m.theme = m.scheme.Theme()
	return m
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.scheme.Init(), clockAfter(clockEvery)}
	if m.watch != nil {
		cmds = append(cmds, pollAfter(firstPoll))
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case clockMsg:
		return m, clockAfter(clockEvery)
	case pollMsg:
		w := m.watch
		return m, func() tea.Msg { return w(context.Background()) }
	case statusMsg:
		// Back off while nothing changes: the card sits in the background
		// for hours, and every look starts rex processes.
		// Back off while Rex is unreachable too, and look again soon once
		// it answers.
		switch {
		case msg.err != nil:
			m.every, m.failed = min(2*m.every, maxPollEvery), true
		case msg.p != m.status || m.failed:
			m.every, m.failed = pollEvery, false
		default:
			m.every = min(2*m.every, maxPollEvery)
		}
		if msg.err == nil {
			m.status = msg.p
		}
		next := pollAfter(m.every)
		if m.status.gone {
			next = nil // it will not come back
		}
		return m, tea.Batch(next, m.spin())
	case spinMsg:
		m.spinning = false
		return m, m.spin()
	case tea.KeyPressMsg:
		if m.demo && msg.String() == "q" {
			return m, tea.Quit
		}
	}
	return m, nil // keys, paste and mouse are swallowed
}

// spin keeps the working spinner turning while the parked pane works, at
// the card's slow pace.
func (m *model) spin() tea.Cmd {
	if m.spinning || m.status.state != ui.StatusWorking || m.status.gone {
		return nil
	}
	m.spinning = true
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
}

// spinMsg turns the card's spinner.
type spinMsg struct{}

// spinEvery is the card's spinner pace: the card is ambient, so it turns
// at a third of the speed of the tools' spinners.
const spinEvery = 3 * ui.SpinnerInterval

// ambient is the clock the card's spinner turns by: slowed down to spinEvery
// a frame.
func ambient(now time.Time) time.Time {
	return time.UnixMilli(now.UnixMilli() / int64(spinEvery/ui.SpinnerInterval))
}
