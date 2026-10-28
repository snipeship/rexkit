package hints

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/panes"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

// Action is what picking a label does.
type Action string

// Actions.
const (
	ActCopy  Action = "copy"  // the label: copy the text
	ActOpen  Action = "open"  // ⇧ on a key of the label: open it (browser, editor, git show …)
	ActPaste Action = "paste" // the label after ⇥: type the text into the pane
	// ActCommand runs the user's command with the text as its last
	// argument: only a key of the hints setting does.
	ActCommand Action = "command"
)

// Pick is what was picked and what to do with it.
type Pick struct {
	Action Action
	Match  Match
	// Namespace is the Kubernetes namespace a pod was listed in, read off
	// the kubectl command above it ("" for none).
	Namespace string
}

// Source is the pane the tool covers.
type Source struct {
	// Screen is the pane's screen in Rex's vt format at cols×rows (a demo
	// pane is drawn at the size it is asked for).
	Screen func(cols, rows int) string
	// Size is the pane's size; zero for a pane drawn at the tool's size.
	Size rex.Size
	// Cursor is where the pane's cursor is; nil for its last row. The
	// nearest things get the shortest labels.
	Cursor *rex.Cursor
	// Dir is the pane's working directory and Home the user's: every prompt
	// repeats Dir (often as ~/…), so it is never a hint.
	Dir, Home string
}

// skip is what Find leaves out: the pane's own folder, as a prompt shows it.
func (s Source) skip() []string {
	if s.Dir == "" {
		return nil
	}
	out := []string{s.Dir}
	if s.Home != "" && s.Dir != s.Home && strings.HasPrefix(s.Dir, s.Home+string(filepath.Separator)) {
		out = append(out, "~"+strings.TrimPrefix(s.Dir, s.Home))
	}
	return out
}

// flashFor is how long a picked match flashes before the tool closes, as
// Neovim flashes a yank. A pick whose work takes longer flashes until it
// is done.
const flashFor = 220 * time.Millisecond

type (
	// loadedMsg brings the pane (the tool reads it once it is up).
	loadedMsg struct {
		src Source
		err error
	}
	flashedMsg struct{}
	// doneMsg ends a pick's work.
	doneMsg struct{}
)

// options are what Main decides before the tool starts.
type options struct {
	demo          bool
	dismissOnBlur bool   // close when another pane takes focus
	stateDir      string // where the Lua layer waits for answers; "" when it did not open the tool
	// fixed is what every label does when a key of the hints setting
	// opened the tool ("" for Hints as it comes: copy, ⇥ paste, ⇧ open);
	// label is what the key bar calls it.
	fixed    Action
	label    string
	alphabet string   // what labels are made of; "" for Alphabet
	patterns Patterns // what to label; nil for the built-in patterns
}

type model struct {
	opts   options
	scheme ui.Scheme
	theme  ui.Theme
	rex    rextheme.Theme
	w, h   int // the tool's size: the window, or the pane alone

	load func() tea.Msg // reads the pane
	work func(Pick)     // carries out a pick (off the UI goroutine); nil in a demo

	src     *Source           // nil until loaded
	err     error             // why the pane could not be read
	early   []tea.KeyPressMsg // keys typed before the pane was there
	area    ui.Rect           // where the pane is in the tool
	cells   [][]panes.Cell
	matches []Match

	typed  string
	shift  bool   // ⇧ was held on a key of the label being typed: it opens
	paste  bool   // ⇥ switched a label from copying its text to pasting it
	miss   string // what was typed when the last key started no label ("yy", "⌥y"; "" when it did): the bar says so
	picked *Pick
	done   bool // the pick's work is done
	flash  bool // the flash is over

	focused, floating bool
	self              tea.Cmd // finds the tool's own layer
	started           time.Time
	flashFor          time.Duration // how long a pick flashes (tests shorten it)
}

func newModel(opts options, scheme ui.Scheme, load func() tea.Msg, work func(Pick)) *model {
	m := &model{opts: opts, scheme: scheme, load: load, work: work, started: time.Now(), flashFor: flashFor}
	m.setTheme()
	return m
}

func (m *model) setTheme() {
	m.theme = m.scheme.Theme()
	m.rex = ui.RexTheme(m.scheme.Dark())
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), m.self, m.load)
}

// ready reports whether the pane is read and laid out.
func (m *model) ready() bool { return m.cells != nil }

// layout parses the pane at its place in the tool and finds what is on it.
// It waits for the size, the pane and the colour scheme (the pane's
// colours are its theme's).
func (m *model) layout() {
	if m.src == nil || m.w <= 0 || m.h <= 0 || !m.scheme.Settled() {
		return
	}
	// The float is the pane's size, give or take a cell of rounding: the
	// pane is drawn from its top-left corner.
	cols, rows := m.src.Size.Columns, m.src.Size.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = m.w, m.h
	}
	m.area = ui.Rect{W: cols, H: rows}
	m.cells = panes.Cells(m.src.Screen(cols, rows), cols, rows, m.rex.Terminal)
	m.matches = Find(m.cells, m.opts.patterns, m.src.skip()...)
	row, col := rows-1, 0
	if c := m.src.Cursor; c != nil {
		row, col = c.Row, c.Column
	}
	Assign(m.matches, row, col, m.opts.alphabet)
	place(m.matches, m.cells)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.setTheme()
		m.layout()
		if cmd := m.settle(); cmd != nil {
			return m, cmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, m.settle()
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.src = &msg.src
		m.layout()
		return m, m.settle()
	case selfMsg:
		m.floating = msg.floating
	case tea.FocusMsg:
		m.focused = true
	case tea.BlurMsg:
		// A pick that opens a split moves the focus there: that is no
		// reason to stop its work.
		if m.focused && m.floating && m.opts.dismissOnBlur && m.picked == nil {
			return m, tea.Quit
		}
	case flashedMsg:
		m.flash = true
		return m, m.finished()
	case doneMsg:
		m.done = true
		return m, m.finished()
	case tea.KeyPressMsg:
		if m.picked != nil {
			// A ⏎ Lua forwarded (commit_via = "global") still gets its
			// answer while the pick's work runs.
			if msg.String() == "enter" {
				return m, m.stay()
			}
			return m, nil
		}
		if !m.ready() && !isCancel(msg) && msg.String() != "enter" {
			m.early = append(m.early, msg)
			return m, nil
		}
		return m.key(msg)
	}
	return m, nil
}

// selfMsg reports whether the tool runs in a floating layer.
type selfMsg struct{ floating bool }

// settle runs once the pane is laid out: with nothing on it the tool ends
// (Main says so in a toast), else the keys typed meanwhile are replayed.
func (m *model) settle() tea.Cmd {
	if !m.ready() {
		return nil
	}
	if len(m.matches) == 0 {
		return tea.Quit
	}
	early := m.early
	m.early = nil
	var cmds []tea.Cmd
	for _, k := range early {
		_, cmd := m.key(k)
		cmds = append(cmds, cmd)
		if m.picked != nil {
			break
		}
	}
	return tea.Batch(cmds...)
}

// isCancel reports ⎋ and ⌃C, which close the tool even before it shows.
func isCancel(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == "esc" || s == "ctrl+c"
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m, m.stay()
	case "tab":
		// ⇥ switches what a label does between copy and paste, also after
		// its first letter. A label typed with ⇧ opens either way. A key of
		// the hints setting does one thing.
		if !m.shift && m.opts.fixed == "" {
			m.paste, m.miss = !m.paste, ""
		}
		return m, nil
	case "backspace":
		if m.typed != "" {
			m.typed = m.typed[:len(m.typed)-1]
		}
		if m.typed == "" {
			m.shift = false
		}
		m.miss = ""
		return m, nil
	}
	if msg.Mod&^(tea.ModShift|tea.ModAlt) != 0 || msg.Code > unicode.MaxRune || !unicode.IsPrint(msg.Code) || unicode.IsSpace(msg.Code) {
		return m, nil
	}
	r := unicode.ToLower(msg.Code)
	// ⌥ + a letter is no key of Hints (paste is ⇥, then the label), but it
	// gets the answer any other key that starts no label gets.
	if msg.Mod&tea.ModAlt != 0 {
		m.miss = "⌥" + string(r)
		return m, nil
	}
	// ⇧ on any key of a label opens it: "Y" then "s", or "y" then "S".
	shift := m.shift
	if t := []rune(msg.Text); m.opts.fixed == "" && (len(t) == 1 && unicode.IsUpper(t[0]) || msg.Mod&tea.ModShift != 0 || unicode.IsUpper(msg.Code)) {
		shift = true
	}
	typed := m.typed + string(r)
	live := false
	for _, mt := range m.matches {
		if mt.Label == typed {
			m.typed, m.shift, m.miss = typed, shift, ""
			return m, m.pick(Pick{Action: m.action(), Match: mt, Namespace: namespace(m.cells, mt.Row)})
		}
		live = live || strings.HasPrefix(mt.Label, typed)
	}
	if live {
		m.typed, m.shift, m.miss = typed, shift, ""
	} else {
		m.miss = typed
	}
	return m, nil
}

// action is what typing a label does now.
func (m *model) action() Action {
	switch {
	case m.opts.fixed != "":
		return m.opts.fixed
	case m.shift:
		return ActOpen
	case m.paste:
		return ActPaste
	}
	return ActCopy
}

// pick flashes what was picked and does it; the tool closes once both are
// over.
func (m *model) pick(p Pick) tea.Cmd {
	m.picked = &p
	flash := tea.Tick(m.flashFor, func(time.Time) tea.Msg { return flashedMsg{} })
	if m.work == nil {
		m.done = true
		return flash
	}
	return tea.Batch(flash, func() tea.Msg {
		m.work(p)
		return doneMsg{}
	})
}

func (m *model) finished() tea.Cmd {
	if m.flash && m.done {
		return tea.Quit
	}
	return nil
}

// stay answers a ⏎ that Lua forwarded: it picks nothing, and the tool stays.
func (m *model) stay() tea.Cmd {
	dir := m.opts.stateDir
	if dir == "" {
		return nil
	}
	_ = popup.TakeCommit(dir)
	return func() tea.Msg {
		_ = popup.Answer(dir, popup.ActionStay)
		return nil
	}
}

// Picked is what the tool closed with: nil for a cancel.
func (m *model) Picked() *Pick { return m.picked }

// left counts the distinct things whose label starts with what was typed.
func (m *model) left() int {
	seen := map[string]bool{}
	for _, mt := range m.matches {
		if strings.HasPrefix(mt.Label, m.typed) {
			seen[mt.Label] = true
		}
	}
	return len(seen)
}

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	if !m.ready() || len(m.matches) == 0 {
		return v
	}
	v.Content = m.render()
	return v
}
