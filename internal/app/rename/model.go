package rename

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/ui"
)

// savedMsg reports how applying the label went: problem is "" on success.
type savedMsg struct{ problem string }

// currentMsg delivers the current label when the caller did not pass one.
type currentMsg struct{ label string }

// selfMsg reports whether the dialog runs in a floating layer.
type selfMsg struct{ floating bool }

// options are what Main decides before the dialog starts.
type options struct {
	target        Target
	demo          bool
	dismissOnBlur bool   // close when another pane takes focus
	stateDir      string // where the Lua layer waits for answers; "" when it did not open the dialog
}

// model is the rename dialog.
type model struct {
	opts   options
	scheme ui.Scheme
	theme  ui.Theme
	w, h   int

	Editor // the name field

	apply   func(label string) tea.Cmd // nil in demo mode
	current tea.Cmd                    // fetches the current label, or nil

	saving   bool
	spin     ui.Spinner // the tray's "Saving…", once saving takes a while
	saved    bool
	toasts   ui.Toaster
	focused  bool
	floating bool
	self     tea.Cmd
	started  time.Time // when the dialog started, for ui.EarlyEnterSince
}

func newModel(opts options, current string, apply func(string) tea.Cmd, fetch, self tea.Cmd) *model {
	m := &model{opts: opts, scheme: ui.NewScheme(), apply: apply, current: fetch, self: self, started: time.Now()}
	m.theme = m.scheme.Theme()
	m.Editor = NewEditor(opts.target, current)
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), m.current, m.self)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	m.toasts.Update(msg)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case currentMsg:
		if !m.edited {
			m.prefill(msg.label)
		}
	case ui.SpinnerTickMsg:
		return m, m.spin.Tick()
	case savedMsg:
		m.saving = false
		m.spin.Stop()
		if msg.problem != "" { // the dialog stays, so Lua keeps it
			return m, tea.Batch(m.toasts.Show(ui.ToastErr, msg.problem), m.stay())
		}
		m.saved = true
		return m, tea.Quit
	case selfMsg:
		m.floating = msg.floating
	case tea.FocusMsg:
		m.focused = true
	case tea.BlurMsg:
		if m.focused && m.floating && m.opts.dismissOnBlur {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		return m.key(ui.EarlyEnterSince(m.started, time.Now(), msg))
	case tea.PasteMsg:
		if m.saving {
			return m, nil
		}
		return m, m.Paste(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		// Take Lua's commit marker so it never outlives its ⏎. The rename
		// itself goes through the server, so it works with or without the
		// handshake; without one, nobody leaves the picker mode, which heals
		// on its next key (Lua's miss path).
		_ = popup.TakeCommit(m.opts.stateDir)
		return m.save()
	}
	if m.saving { // the label is on its way: only ⎋ and ⌃C still count
		return m, nil
	}
	return m, m.Key(msg)
}

// save applies the label; an empty label restores the automatic name.
func (m *model) save() (tea.Model, tea.Cmd) {
	if m.saving {
		return m, nil
	}
	label := m.Label()
	switch {
	case m.Unchanged():
		m.saved = true // nothing to change
		return m, tea.Quit
	case m.apply == nil:
		return m, tea.Batch(m.toasts.Show(ui.ToastOK, m.opts.target.Outcome(label)), m.stay())
	}
	m.saving = true
	return m, tea.Batch(m.apply(label), m.spin.Start(time.Now()))
}

// stay answers a ⏎ the Lua layer forwarded when the dialog stays open (the
// rename failed), so Lua keeps it. Closing answers come from Main.
func (m *model) stay() tea.Cmd {
	dir := m.opts.stateDir
	if dir == "" {
		return nil
	}
	return func() tea.Msg {
		_ = popup.Answer(dir, popup.ActionStay)
		return nil
	}
}
