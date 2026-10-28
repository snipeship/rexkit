package keys

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// keymapMsg delivers the live keymap.
type keymapMsg struct {
	km  rex.Keymap
	err error
}

// selfMsg reports whether the guide runs in a floating layer.
type selfMsg struct{ floating bool }

// options are what Main decides before the model starts.
type options struct {
	mode          string // initial mode chip ("pane"), "" for all
	canRun        bool   // ⏎ can hand an action to the Lua layer
	demo          bool
	dismissOnBlur bool     // close when another pane takes focus
	stateDir      string   // where the Lua layer waits for answers; "" when it did not open the guide
	first         []string // global keys listed first in their rows: the key settings' (lua/core.lua)
}

// model is the key guide.
type model struct {
	opts   options
	scheme ui.Scheme
	theme  ui.Theme
	w, h   int

	guide  Guide
	loaded bool // the guide came from the live keymap, not the cache
	load   tea.Cmd
	self   tea.Cmd

	input    textinput.Model
	mode     int  // chip: 0 all, i Guide.Groups[i-1]
	userChip bool // the user picked the chip (tab or a click), not --mode
	view     []shown
	sheet    sheet
	vp       viewport
	sel      int // index into sheet.order, -1 when nothing shows

	toasts   ui.Toaster
	spinner  ui.Spinner
	help     bool // the key panel is open
	helpAt   int  // the key panel's scroll position, when it is cut short
	focused  bool
	floating bool
	click    struct {
		entry int
		at    time.Time
	}

	started time.Time // when the guide started, for ui.EarlyEnterSince
	chosen  *Entry    // the action ⏎ runs
	pending bool      // ⏎ was pressed before the live keymap arrived
	// pendingCommit is when Lua wrote the commit marker of the held ⏎
	// (popup.TakeCommitAt); zero when it was typed, or once its grace is
	// over and the guide answered "stay" (lapsed).
	pendingCommit time.Time
	pendingRev    int  // counts held handshake ⏎s, so a late stillPendingMsg is ignored
	lapsed        bool // the held ⏎'s handshake was answered "stay": the ⏎ is dropped
}

// holdEnter keeps a ⏎ typed before the live keymap arrived. A held
// handshake ⏎ answers "stay" popup.GraceLeft after Lua wrote it, before Lua
// gives up waiting (stillPending).
func (m *model) holdEnter(commit time.Time) tea.Cmd {
	m.pending = true
	if commit.IsZero() {
		return nil
	}
	m.pendingCommit, m.lapsed = commit, false
	m.pendingRev++
	rev := m.pendingRev
	return tea.Tick(popup.GraceLeft(commit), func(time.Time) tea.Msg { return stillPendingMsg{rev} })
}

// stillPendingMsg fires when the grace of a held handshake ⏎ is over.
type stillPendingMsg struct{ rev int }

func (m *model) stillPending(msg stillPendingMsg) tea.Cmd {
	if msg.rev != m.pendingRev || !m.pending || m.pendingCommit.IsZero() {
		return nil // it ran, or was answered already
	}
	m.pendingCommit, m.lapsed = time.Time{}, true
	return tea.Batch(m.stay(), m.toasts.Show(ui.ToastInfo, "Still reading the keymap"))
}

// runPending runs a ⏎ that waited for the live keymap. Unlike Go to's, a ⏎
// whose handshake lapsed is dropped: without the handshake only Lua can run
// an action, so it would only say how to reopen the guide. The selection
// stays, and a fresh ⏎ runs it.
func (m *model) runPending(ready bool) (tea.Model, tea.Cmd) {
	if !m.pending {
		return m, nil
	}
	commit, lapsed := !m.pendingCommit.IsZero(), m.lapsed
	m.pending, m.pendingCommit, m.lapsed = false, time.Time{}, false
	switch {
	case lapsed && ready:
		return m, m.toasts.Show(ui.ToastInfo, "Keymap ready", ui.Hint{Key: "enter", Label: "runs it"})
	case lapsed:
		return m, nil
	}
	return m.enter(commit)
}

func newModel(opts options, g Guide, load, self tea.Cmd) *model {
	if opts.demo && opts.first == nil {
		opts.first = demoFirst
	}
	m := &model{opts: opts, scheme: ui.NewScheme(), guide: g.lead(opts.first), load: load, self: self, sel: -1, started: time.Now()}
	m.theme = m.scheme.Theme()
	m.input = ui.NewInput()
	m.mode = m.modeChip(opts.mode)
	if load != nil {
		m.spinner.Start(time.Now()) // counts the one tick Init sends
	}
	m.refilter(nil)
	return m
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.scheme.Init(), m.self, textinput.Blink}
	if m.load != nil {
		cmds = append(cmds, m.load, ui.TickSpinner())
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	if m.toasts.Update(msg) {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.relayout()
	case keymapMsg:
		m.spinner.Stop()
		m.load = nil // nothing more to wait for
		if msg.err != nil {
			toast := m.toasts.Show(ui.ToastErr, "Could not read the keymap: "+rex.UserMessage(msg.err))
			_, run := m.runPending(false)
			return m, tea.Batch(toast, run)
		}
		// The chip is an index into the groups, whose order can differ
		// between the cached and the live keymap: keep it by mode name,
		// and keep a chip the user picked over the one asked for.
		want := m.opts.mode
		if m.userChip {
			want = m.chipName()
		}
		keep := m.selected()
		m.guide, m.loaded = Build(msg.km).lead(m.opts.first), true
		m.mode = m.modeChip(want)
		m.refilter(keep)
		return m.runPending(true)
	case stillPendingMsg:
		return m, m.stillPending(msg)
	case ui.SpinnerTickMsg:
		return m, m.spinner.Tick() // one tick chain, however often Start ran
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
		return m.edit(msg)
	case tea.MouseClickMsg:
		return m.mouseClick(msg)
	case tea.MouseWheelMsg:
		m.wheel(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Every ⏎ takes Lua's commit marker, whatever it then does: only a ⏎
	// that took one came from the handshake, whose result Lua runs.
	var commit time.Time
	if msg.String() == "enter" {
		commit, _ = popup.TakeCommitAt(m.opts.stateDir)
	}
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.help {
		// A key panel cut short scrolls; ? and esc close it; any other
		// key closes it and does its job.
		_, _, more := m.keyPanel()
		if at, ok := ui.ScrollKeyPanel(m.helpAt, more, msg.String()); ok {
			m.helpAt = at
			return m, nil
		}
		m.help = false
		switch msg.String() {
		case "?":
			return m, nil
		case "esc":
			return m, m.stay()
		}
	}
	switch msg.String() {
	case "esc":
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.refilter(m.selected())
			return m, m.stay()
		}
		return m, tea.Quit
	case "enter":
		if m.load != nil {
			return m, m.holdEnter(commit) // typed before the live keymap arrived: run once it has
		}
		return m.enter(!commit.IsZero())
	case "up", "ctrl+k":
		m.move(-1, true)
	case "down", "ctrl+j":
		m.move(1, true)
	case "pgup":
		m.move(-max(1, m.bodyH()-1), false)
	case "pgdown":
		m.move(max(1, m.bodyH()-1), false)
	case "left", "right":
		if m.sheet.visible > 1 {
			m.sel = m.sheet.beside(m.sel, map[string]int{"left": -1, "right": 1}[msg.String()])
			m.follow()
			return m, nil
		}
		return m.edit(msg)
	case "tab", "shift+tab":
		step := 1
		if msg.String() == "shift+tab" {
			step = -1
		}
		m.setMode((m.mode + step + len(m.guide.Groups) + 1) % (len(m.guide.Groups) + 1))
	case "?":
		if m.input.Value() == "" {
			m.help, m.helpAt = true, 0
			m.follow() // over a single column, the panel covers its bottom
			return m, nil
		}
		return m.edit(msg)
	default:
		return m.edit(msg)
	}
	return m, nil
}

// edit passes a message to the query field and refilters when it changed.
func (m *model) edit(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.refilter(nil)
	}
	return m, cmd
}

// enter runs the selected action for ⏎. Only Lua runs it, and only for a
// ⏎ its handshake wrote (commit): one typed while the picker mode was not
// active (after a config reload, say) would close the guide with a result
// nobody reads.
func (m *model) enter(commit bool) (tea.Model, tea.Cmd) {
	if e := m.selected(); !commit && m.opts.canRun && !m.opts.demo && e != nil && e.Action != "" {
		return m, tea.Batch(m.toasts.Show(ui.ToastWarn, "Reopen the guide (F1) to run actions from it"), m.stay())
	}
	return m.run()
}

// run hands the selected action to the Lua layer and closes the guide.
func (m *model) run() (tea.Model, tea.Cmd) {
	e := m.selected()
	switch {
	case e == nil:
		return m, m.stay()
	case e.Action == "":
		return m, tea.Batch(m.toasts.Show(ui.ToastInfo, "Nothing to run: in the mode, these keys switch to another one"), m.stay())
	case m.opts.demo:
		return m, m.toasts.Show(ui.ToastOK, "Would run "+e.Title)
	case !m.opts.canRun:
		return m, m.toasts.Show(ui.ToastWarn, "Open the guide with its key (F1) to run actions from it")
	}
	m.chosen = e
	return m, tea.Quit
}

// setMode switches to a chip the user picked.
func (m *model) setMode(mode int) {
	m.mode, m.userChip = mode, true
	m.refilter(m.selected())
}

// chipName is the mode the chip shows, as modeChip reads it: "" for All,
// "global" for the global group.
func (m *model) chipName() string {
	if m.mode <= 0 || m.mode > len(m.guide.Groups) {
		return ""
	}
	if mode := m.guide.Groups[m.mode-1].Mode; mode != "" {
		return mode
	}
	return "global"
}

// modeChip returns the chip index of a mode name (0 when unknown).
func (m *model) modeChip(name string) int {
	if name == "" {
		return 0
	}
	for i, g := range m.guide.Groups {
		if g.Mode == name || (name == "global" && g.Mode == "") {
			return i + 1
		}
	}
	return 0
}

// refilter recomputes what shows. It keeps keep selected when it is still
// shown; otherwise it selects the best match (or the first entry).
func (m *model) refilter(keep *Entry) {
	m.view = filter(m.guide, m.input.Value(), m.mode)
	m.relayout()
	m.sel = -1
	best := -1
	for i, p := range m.sheet.order {
		c, _ := m.sheet.at(p)
		h := m.view[c.group].entries[c.entry]
		e := &m.guide.Groups[m.view[c.group].group].Entries[h.entry]
		if keep != nil && sameEntry(e, keep) {
			m.sel = i
			break
		}
		if best < 0 || h.score > m.hitAt(best).score {
			best = i
		}
	}
	if m.sel < 0 {
		m.sel = best
	}
	m.vp = viewport{}
	m.follow()
}

func sameEntry(a, b *Entry) bool {
	return a.Action == b.Action && string(compactJSON(a.Args)) == string(compactJSON(b.Args)) && a.KeysLabel() == b.KeysLabel()
}

func (m *model) relayout() {
	m.sheet = layOut(m.theme, m.guide, m.view, m.sheetW(), m.bodyH())
	m.follow()
}

// follow scrolls the sheet to the selection, which stays above the key
// panel where the panel covers the bottom of a single column.
func (m *model) follow() {
	h := m.bodyH()
	if _, at, _ := m.keyPanel(); m.help && m.sheet.height == 0 && !at.Empty() {
		h = max(1, at.Y)
	}
	m.vp = m.sheet.follow(m.vp, m.sel, h)
}

// move steps the selection through the entries in reading order; single
// steps wrap around, page steps stop at the ends.
func (m *model) move(d int, wrap bool) {
	n := len(m.sheet.order)
	if n == 0 {
		return
	}
	switch next := m.sel + d; {
	case wrap:
		m.sel = (next%n + n) % n
	default:
		m.sel = clamp(next, 0, n-1)
	}
	m.follow()
}

func (m *model) wheel(msg tea.MouseWheelMsg) {
	if m.help {
		return
	}
	switch msg.Button {
	case tea.MouseWheelUp, tea.MouseWheelLeft:
		m.move(-1, false)
	case tea.MouseWheelDown, tea.MouseWheelRight:
		m.move(1, false)
	}
}

func (m *model) mouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	lay := m.layout()
	if m.help {
		// A click outside the key panel closes it; one inside does nothing.
		if _, at, _ := m.keyPanel(); !at.Offset(lay.body.X, lay.body.Y).Contains(msg.X, msg.Y) {
			m.help = false
		}
		return m, nil
	}
	if lay.header.Contains(msg.X, msg.Y) {
		_, hits := m.headerLines()
		for i, r := range hits.Chips {
			if r.Contains(msg.X-lay.header.X, 0) { // any row of the header band
				m.setMode(i)
			}
		}
		return m, nil
	}
	if !lay.body.Contains(msg.X, msg.Y) {
		return m, nil
	}
	i := m.sheet.hitTest(m.vp, msg.X-lay.body.X, msg.Y-lay.body.Y)
	if i < 0 {
		return m, nil
	}
	now := time.Now()
	double := i == m.click.entry && i == m.sel && now.Sub(m.click.at) <= ui.DoubleClick
	m.sel, m.click.entry, m.click.at = i, i, now
	if double {
		return m.run()
	}
	return m, nil
}

// selected returns the selected entry, or nil.
func (m *model) selected() *Entry {
	if m.sel < 0 || m.sel >= len(m.sheet.order) {
		return nil
	}
	c, _ := m.sheet.at(m.sheet.order[m.sel])
	s := m.view[c.group]
	return &m.guide.Groups[s.group].Entries[s.entries[c.entry].entry]
}

func (m *model) hitAt(i int) hit {
	c, _ := m.sheet.at(m.sheet.order[i])
	return m.view[c.group].entries[c.entry]
}

// stay answers a key the Lua layer forwarded (⏎, ⎋ or ⌃C) that the guide
// handled without closing, so Lua keeps it open. Closing answers come from
// Main, after the program ends.
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
