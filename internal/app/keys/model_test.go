package keys

import (
	"errors"
	"rexkit/internal/popup"
	"rexkit/internal/ui/uitest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func selectedTitle(m *model) string {
	if e := m.selected(); e != nil {
		return e.Title
	}
	return ""
}

func TestNavigation(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		keys  []string
		want  string
		chips int // active mode chip
	}{
		{"starts on the first entry", 150, 40, nil, "Pane mode", 0},
		{"down", 150, 40, []string{"down", "down"}, "Resize mode", 0},
		{"up wraps to the last entry", 150, 40, []string{"up"}, "Leave mode", 0},
		{"right moves to the next column, same row", 150, 40, []string{"down", "right"}, "Focus pane down", 0},
		{"left from the first column stays", 150, 40, []string{"left"}, "Pane mode", 0},
		{"left and right edit the query in one column", 60, 20, []string{"right"}, "Pane mode", 0},
		{"tab picks the next mode", 150, 40, []string{"tab", "tab"}, "Focus pane left", 2},
		{"shift+tab wraps to the last mode", 150, 40, []string{"shift+tab"}, "Leave mode", 9},
		{"typing selects the best match", 150, 40, []string{"z", "o", "o", "m"}, "Zoom pane", 0},
		{"esc clears the query and keeps the selection", 150, 40, []string{"z", "o", "o", "m", "esc"}, "Zoom pane", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := demoModel(t, tt.w, tt.h)
			press(t, m, tt.keys...)
			if got := selectedTitle(m); got != tt.want {
				t.Errorf("selected %q, want %q", got, tt.want)
			}
			if m.mode != tt.chips {
				t.Errorf("mode chip %d, want %d", m.mode, tt.chips)
			}
		})
	}
}

func TestSelectionStaysVisible(t *testing.T) {
	for _, size := range [][2]int{{60, 14}, {150, 20}} {
		m := demoModel(t, size[0], size[1])
		for range len(m.sheet.order) + 3 {
			press(t, m, "down")
			if !slices.ContainsFunc(washed(screen(m), m.theme.C.SelBg), func(l string) bool { return strings.Contains(l, selectedTitle(m)) }) {
				t.Fatalf("%v: selection %d (%s) scrolled out of view:\n%s", size, m.sel, selectedTitle(m), plain(screen(m)))
			}
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		opts   options
		quit   bool
		toast  string
		chosen string
	}{
		{"demo explains", options{demo: true}, false, "Would run Pane mode", ""},
		{"without the handshake", options{}, false, "Open the guide with its key (F1) to run actions from it", ""},
		{"hands the action to Lua", options{canRun: true}, true, "", "client.mode.enter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REXKIT_THEME", "dark")
			if tt.opts.canRun { // as Main: only Lua's handshake can run it
				tt.opts.stateDir = t.TempDir()
				mark(t, tt.opts.stateDir)
			}
			m := newModel(tt.opts, Build(demoKeymap()), nil, nil)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			_, cmd := m.Update(uitest.Key(t, "enter"))
			if tt.quit && !quits(cmd) { // (a toast's cmd is a timer: don't wait for it)
				t.Error("enter did not close the guide")
			}
			if got := m.toasts.Current().Text; got != tt.toast {
				t.Errorf("toast %q, want %q", got, tt.toast)
			}
			got := ""
			if m.chosen != nil {
				got = m.chosen.Action + " " + string(m.chosen.Args)
			}
			if tt.chosen != "" && got != tt.chosen+` {"name":"pane"}` {
				t.Errorf("chosen %q", got)
			}
		})
	}
}

func TestClose(t *testing.T) {
	tests := []struct {
		name     string
		floating bool
		keep     bool
		msgs     []tea.Msg
		quit     bool
	}{
		{"ctrl+c", false, false, []tea.Msg{uitest.Key(t, "ctrl+c")}, true},
		{"esc with an empty query", false, false, []tea.Msg{uitest.Key(t, "esc")}, true},
		{"esc with a query clears it", false, false, []tea.Msg{uitest.Key(t, "x"), uitest.Key(t, "esc")}, false},
		{"blur closes a float", true, false, []tea.Msg{tea.FocusMsg{}, tea.BlurMsg{}}, true},
		{"blur before focus is ignored", true, false, []tea.Msg{tea.BlurMsg{}}, false},
		{"blur keeps a pane", false, false, []tea.Msg{tea.FocusMsg{}, tea.BlurMsg{}}, false},
		{"dismiss-on-blur=false", true, true, []tea.Msg{tea.FocusMsg{}, tea.BlurMsg{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REXKIT_THEME", "dark")
			m := newModel(options{dismissOnBlur: !tt.keep}, Build(demoKeymap()), nil, nil)
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m.Update(selfMsg{floating: tt.floating})
			var cmd tea.Cmd
			for _, msg := range tt.msgs {
				_, cmd = m.Update(msg)
			}
			if quits(cmd) != tt.quit {
				t.Errorf("quit = %v, want %v", !tt.quit, tt.quit)
			}
		})
	}
}

func TestLiveKeymapReplacesCache(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{mode: "tab"}, Guide{}, func() tea.Msg { return nil }, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if out, _ := m.render(time.Now()); strings.Contains(plain(out), "Reading the keymap…") {
		t.Errorf("the loading page flashed before ui.SpinnerDelay:\n%s", plain(out))
	}
	if out, _ := m.render(time.Now().Add(ui.SpinnerDelay)); !strings.Contains(plain(out), "Reading the keymap…") {
		t.Errorf("an empty cache should say it is loading:\n%s", plain(out))
	}
	m.Update(keymapMsg{km: demoKeymap()})
	if m.mode == 0 || m.guide.Groups[m.mode-1].Mode != "tab" {
		t.Errorf("--mode tab not applied once the keymap arrived (chip %d)", m.mode)
	}
	press(t, m, "down")
	m.Update(keymapMsg{km: demoKeymap()})
	if got := selectedTitle(m); got != "Next tab" {
		t.Errorf("a refreshed keymap lost the selection: %q", got)
	}
	m.Update(keymapMsg{err: errors.New("rex keymap: server unavailable")})
	if got := m.toasts.Current().Text; !strings.Contains(got, "server unavailable") {
		t.Errorf("error toast %q", got)
	}
	if selectedTitle(m) != "Next tab" {
		t.Error("a failed refresh must keep what is shown")
	}
}

// entryAt returns where the entry titled title is drawn: a cell of its
// title, in the view's coordinates.
func entryAt(t *testing.T, m *model, title string) (x, y int) {
	t.Helper()
	lay := m.layout()
	for _, p := range m.sheet.order {
		c, _ := m.sheet.at(p)
		s := m.view[c.group]
		e := &m.guide.Groups[s.group].Entries[s.entries[c.entry].entry]
		if e.Title == title {
			x = lay.body.X + (p.col-m.vp.col)*(m.sheet.colW+colGap) + 1 + m.keyCell(p.col, e)
			return x, lay.body.Y + p.row - m.vp.row
		}
	}
	t.Fatalf("no entry %q on screen", title)
	return 0, 0
}

func TestMouse(t *testing.T) {
	for _, carded := range []bool{false, true} {
		m := demoModel(t, 150, 40)
		if carded {
			m.Update(popup.CardMsg{})
			m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
		}
		x, y := entryAt(t, m, "Focus pane up")
		if row := strings.Split(plain(screen(m)), "\n")[y]; ansi.Cut(row, x, x+len("Focus pane up")) != "Focus pane up" {
			t.Fatalf("carded=%v: row %d does not show the entry at %d:\n%s", carded, y, x, plain(screen(m)))
		}
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if got := selectedTitle(m); got != "Focus pane up" {
			t.Errorf("carded=%v: click selected %q", carded, got)
		}
		m.opts.canRun, m.opts.demo = true, false
		_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if !quits(cmd) || m.chosen == nil {
			t.Errorf("carded=%v: double-click should run the action", carded)
		}
	}
	m := demoModel(t, 150, 40)
	lay := m.layout()
	_, hits := m.headerLines()
	chip := hits.Chips[3].Offset(lay.header.X, lay.line)
	m.Update(tea.MouseClickMsg{X: chip.X, Y: chip.Y, Button: tea.MouseLeft})
	if m.mode != 3 {
		t.Errorf("chip click picked mode %d, want 3", m.mode)
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := selectedTitle(m); got != "Next tab" {
		t.Errorf("wheel moved to %q", got)
	}
}

func TestEveryLineFits(t *testing.T) {
	for w := 0; w <= 130; w += 7 {
		for h := 0; h <= 34; h += 3 {
			m := demoModel(t, w, h)
			press(t, m, "down", "right")
			out := screen(m)
			lines := strings.Split(out, "\n")
			if w == 0 || h == 0 {
				continue
			}
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("%dx%d line %d is %d wide: %q", w, h, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

func TestEmptyGuide(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{}, Build(rex.Keymap{}), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	press(t, m, "down", "right", "enter", "tab")
	if !strings.Contains(plain(screen(m)), "No key bindings") {
		t.Errorf("empty guide:\n%s", plain(screen(m)))
	}
}

// TestChipSurvivesTheLiveKeymap: the chip names a mode, whatever position
// the mode has in the live keymap, and a chip the user picked while the
// keymap loaded stays picked.
func TestChipSurvivesTheLiveKeymap(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	live := demoKeymap()
	// The cache predates a config change: no pane mode yet.
	var old rex.Keymap
	for _, b := range live.Bindings {
		if b.Mode != "pane" && !strings.Contains(string(b.Args), `"pane"`) {
			old.Bindings = append(old.Bindings, b)
		}
	}
	old.Actions, old.Modes = live.Actions, live.Modes
	chipName := func(m *model) string {
		if m.mode == 0 {
			return "All"
		}
		return m.guide.Groups[m.mode-1].Name()
	}

	m := newModel(options{mode: "locked"}, Build(old), func() tea.Msg { return nil }, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if chipName(m) != "Locked" {
		t.Fatalf("cached chip %q", chipName(m))
	}
	m.Update(keymapMsg{km: live})
	if chipName(m) != "Locked" {
		t.Errorf("--mode locked shows %q once the live keymap arrived", chipName(m))
	}

	m = newModel(options{mode: "locked"}, Build(old), func() tea.Msg { return nil }, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	press(t, m, "tab") // Locked is last: tab wraps to All
	if chipName(m) != "All" {
		t.Fatalf("tab picked %q", chipName(m))
	}
	m.Update(keymapMsg{km: live})
	if chipName(m) != "All" {
		t.Errorf("the user's All became %q", chipName(m))
	}

	m = newModel(options{}, Build(old), func() tea.Msg { return nil }, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	press(t, m, "tab", "tab") // Global, Tab
	m.Update(keymapMsg{km: live})
	if chipName(m) != "Tab" {
		t.Errorf("the user's Tab chip became %q", chipName(m))
	}
}

func TestCacheIsKeyedByServer(t *testing.T) {
	if cacheName("") != "keymap" {
		t.Errorf("no server: %q", cacheName(""))
	}
	a, b := cacheName("/tmp/a.sock"), cacheName("127.0.0.1:47391")
	if a == b || !strings.HasPrefix(a, "keymap-") || a != cacheName("/tmp/a.sock") {
		t.Errorf("cache names %q %q", a, b)
	}
}

// TestMarkerKeepsTheSelection: an overflow marker drawn over the selected
// entry stays on the selection fill, padded a cell from the border.
func TestMarkerKeepsTheSelection(t *testing.T) {
	m := demoModel(t, 100, 30)
	last := m.sheet.visible - 1
	for i, p := range m.sheet.order {
		if p.col != m.vp.col+last {
			continue
		}
		m.sel = i
		w := m.bodyW()
		row := ui.Line{ui.Space(w)}
		mark := m.overlayEnd(row, p.row-m.vp.row, w, ui.Span{Text: "▶ 3", Style: m.theme.Subtle})
		if mark.Width() != w || !strings.HasSuffix(mark.String(), " ▶ 3 ") {
			t.Fatalf("marker row %q", mark.String())
		}
		for _, s := range mark[len(mark)-3:] {
			if _, none := s.Style.GetBackground().(lipgloss.NoColor); none {
				t.Fatalf("marker span %q lost the selection fill", s.Text)
			}
		}
		m.sel = 0 // in the first column: the marker is not over it
		mark = m.overlayEnd(row, p.row-m.vp.row, w, ui.Span{Text: "▶ 3", Style: m.theme.Subtle})
		if _, none := mark[len(mark)-2].Style.GetBackground().(lipgloss.NoColor); !none {
			t.Fatal("a marker away from the selection has a fill")
		}
		return
	}
	t.Fatal("no entry in the last column")
}

// TestCtrlJMovesAfterStart: ⌃J is ⏎ only in the first moments (a CR the
// cooked tty turned into LF); later ⌃J and ⌃K move like ↓ and ↑.
func TestCtrlJMovesAfterStart(t *testing.T) {
	m := demoModel(t, 100, 30)
	m.started = time.Now().Add(-time.Second)
	first := m.sel
	m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if m.sel == first || m.chosen != nil {
		t.Fatalf("⌃J: sel %d → %d, chosen %v; want a move down", first, m.sel, m.chosen)
	}
	m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if m.sel != first {
		t.Fatalf("⌃K: sel %d, want %d", m.sel, first)
	}
}

// TestKeyPanel: ? opens the panel; ? and esc only close it; any other key
// closes it and does its job; a click outside closes it, one inside does
// not.
func TestKeyPanel(t *testing.T) {
	m := demoModel(t, 150, 40)
	press(t, m, "?")
	if !m.help || !strings.Contains(plain(screen(m)), "Clear, then close") {
		t.Fatalf("? did not open the panel:\n%s", plain(screen(m)))
	}
	press(t, m, "?")
	if m.help || m.input.Value() != "" {
		t.Fatalf("? closed nothing (help %v) or typed %q", m.help, m.input.Value())
	}
	press(t, m, "?", "down")
	if m.help || selectedTitle(m) != "Tab mode" {
		t.Errorf("down: help %v, selected %q; want closed and moved", m.help, selectedTitle(m))
	}
	press(t, m, "?")
	lay := m.layout()
	_, at, _ := m.keyPanel()
	at = at.Offset(lay.body.X, lay.body.Y)
	m.Update(tea.MouseClickMsg{X: at.X + at.W/2, Y: at.Y + at.H/2, Button: tea.MouseLeft})
	if !m.help {
		t.Error("a click inside the panel closed it")
	}
	x, y := entryAt(t, m, "Pane mode")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.help || selectedTitle(m) != "Tab mode" {
		t.Errorf("a click outside: help %v, selected %q; want closed, selection kept", m.help, selectedTitle(m))
	}
}

// TestPanelKeepsTheSelectionInView: over a single column the panel covers
// the bottom of the list, which scrolls so the selection stays above it.
func TestPanelKeepsTheSelectionInView(t *testing.T) {
	m := demoModel(t, 60, 40)
	for range 20 {
		press(t, m, "down")
	}
	press(t, m, "?")
	_, at, _ := m.keyPanel()
	if at.Empty() || m.sheet.height != 0 {
		t.Fatalf("want one column under a panel (panel %v, height %d)", at, m.sheet.height)
	}
	if row := m.sheet.order[m.sel].row - m.vp.row; row < 0 || row >= at.Y {
		t.Errorf("selection on body row %d, under the panel from row %d", row, at.Y)
	}
}
