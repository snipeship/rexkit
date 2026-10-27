package hints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

// tool is a hints model over src in a w×h tool, loaded. Its picks flash for
// no time, so run returns at once.
func tool(t *testing.T, src Source, w, h int, dark bool) *model {
	t.Helper()
	m := newModel(options{dismissOnBlur: true}, ui.FixedScheme(dark), func() tea.Msg { return loadedMsg{src: src} }, nil)
	m.flashFor = 0
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(loadedMsg{src: src})
	return m
}

// run runs cmd and what it batches, and returns the messages.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, run(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func quits(cmd tea.Cmd) bool {
	for _, msg := range run(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func press(t *testing.T, m *model, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(uitest.Key(t, k))
	}
	return cmd
}

func label(t *testing.T, m *model, text string) string {
	t.Helper()
	for _, mt := range m.matches {
		if mt.Text == text {
			return mt.Label
		}
	}
	t.Fatalf("%q is not a match", text)
	return ""
}

func TestPickActions(t *testing.T) {
	tests := []struct {
		name string
		keys func(label string) []string
		want Action
	}{
		{"label copies", func(l string) []string { return strings.Split(l, "") }, ActCopy},
		{"shift on the first key opens", func(l string) []string { return []string{"shift+" + l[:1], l[1:]} }, ActOpen},
		{"shift on the last key opens", func(l string) []string { return []string{l[:1], "shift+" + l[1:]} }, ActOpen},
		{"alt on the last key pastes", func(l string) []string { return []string{l[:1], "alt+" + l[1:]} }, ActPaste},
		{"alt on the first key pastes", func(l string) []string { return []string{"alt+" + l[:1], l[1:]} }, ActPaste},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tool(t, demoSource(demoShell), 119, 66, true)
			l := label(t, m, "10.42.3.17:8080")
			if len(l) != 2 {
				t.Fatalf("label %q, want two letters", l)
			}
			press(t, m, tt.keys(l)...)
			p := m.Picked()
			if p == nil || p.Action != tt.want || p.Match.Text != "10.42.3.17:8080" || p.Match.Kind != KindIP {
				t.Fatalf("picked %+v, want %s 10.42.3.17:8080", p, tt.want)
			}
		})
	}
}

func TestPodPickCarriesNamespace(t *testing.T) {
	m := tool(t, demoSource(demoShell), 119, 66, true)
	press(t, m, strings.Split(label(t, m, "studio-api-7d9f8b6c5d-q8m4z"), "")...)
	if p := m.Picked(); p == nil || p.Namespace != "staging" {
		t.Fatalf("picked %+v, want the namespace staging", p)
	}
}

func TestTypingNarrowsAndBacksOut(t *testing.T) {
	m := tool(t, demoSource(demoShell), 119, 66, true)
	all := m.left()
	press(t, m, "shift+y")
	if m.typed != "y" || !m.shift || m.left() >= all || m.left() < 2 {
		t.Fatalf("after Y: typed %q shift %v left %d of %d", m.typed, m.shift, m.left(), all)
	}
	if bar := ansi.Strip(m.View().Content); !strings.Contains(bar, "Y…") || !strings.Contains(bar, "to open") {
		t.Errorf("the bar does not say Y… to open")
	}
	press(t, m, "backspace", "alt+y")
	if bar := ansi.Strip(m.View().Content); m.shift || !m.alt || !strings.Contains(bar, "to paste") {
		t.Errorf("after ⌫ ⌥Y: shift %v alt %v, and the bar does not say to paste", m.shift, m.alt)
	}
	press(t, m, "backspace", "shift+y")
	press(t, m, "y")
	if m.miss != 'y' || m.typed != "y" {
		t.Fatalf("after yy: miss %q typed %q", m.miss, m.typed)
	}
	if bar := ansi.Strip(m.View().Content); !strings.Contains(bar, "no label yy") {
		t.Errorf("the bar does not say no label yy")
	}
	press(t, m, "backspace")
	if m.typed != "" || m.shift || m.alt || m.miss != 0 || m.left() != all {
		t.Fatalf("after backspace: typed %q shift %v alt %v miss %q", m.typed, m.shift, m.alt, m.miss)
	}
	press(t, m, "5", "space", "ctrl+a")
	if m.picked != nil || m.typed != "" {
		t.Fatalf("keys that are no letters picked %+v, typed %q", m.picked, m.typed)
	}
}

func TestCancel(t *testing.T) {
	for _, k := range []string{"esc", "ctrl+c"} {
		m := tool(t, demoSource(demoShell), 119, 66, true)
		press(t, m, "y")
		if !quits(press(t, m, k)) || m.Picked() != nil {
			t.Errorf("%s: no cancel", k)
		}
	}
}

// TestEnterStays: a ⏎ Lua forwards picks nothing; the tool takes the
// commit marker and answers stay.
func TestEnterStays(t *testing.T) {
	dir := t.TempDir()
	m := tool(t, demoSource(demoShell), 119, 66, true)
	m.opts.stateDir = dir
	if err := os.WriteFile(filepath.Join(dir, popup.CommitFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if quits(press(t, m, "enter")) {
		t.Fatal("enter closed the tool")
	}
	var r popup.Result
	b, _ := os.ReadFile(filepath.Join(dir, popup.ResultFile))
	if !strings.Contains(string(b), `"stay"`) {
		t.Errorf("result %s %+v, want stay", b, r)
	}
	if _, err := os.Stat(filepath.Join(dir, popup.CommitFile)); err == nil {
		t.Error("the commit marker outlived its ⏎")
	}

	// While a pick's work runs, a forwarded ⏎ still gets its answer.
	if err := os.Remove(filepath.Join(dir, popup.ResultFile)); err != nil {
		t.Fatal(err)
	}
	m.work = func(Pick) {}
	press(t, m, strings.Split(label(t, m, "a3f9c21"), "")...)
	run(press(t, m, "enter"))
	if b, _ := os.ReadFile(filepath.Join(dir, popup.ResultFile)); !strings.Contains(string(b), `"stay"`) {
		t.Errorf("a ⏎ during a pick: result %s, want stay", b)
	}
}

// TestKeysBeforeTheScreen: what is typed while the pane is read counts once
// it is there.
func TestKeysBeforeTheScreen(t *testing.T) {
	src := demoSource(demoShell)
	m := newModel(options{}, ui.FixedScheme(true), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 119, Height: 66})
	want := tool(t, src, 119, 66, true)
	l := label(t, want, "a3f9c21")
	press(t, m, strings.Split(l, "")...)
	if m.picked != nil {
		t.Fatal("picked before the pane was there")
	}
	m.Update(loadedMsg{src: src})
	if p := m.Picked(); p == nil || p.Match.Text != "a3f9c21" {
		t.Fatalf("picked %+v, want a3f9c21", p)
	}
	m = newModel(options{}, ui.FixedScheme(true), nil, nil)
	if !quits(press(t, m, "esc")) {
		t.Error("esc before the pane does not close")
	}
}

func TestNothingToPick(t *testing.T) {
	src := Source{Screen: plainScreen("$ echo hello", "hello", "$")}
	m := newModel(options{}, ui.FixedScheme(true), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	_, cmd := m.Update(loadedMsg{src: src})
	if !quits(cmd) || !m.ready() || len(m.matches) != 0 || m.View().Content != "" {
		t.Fatalf("a pane with nothing on it: quit %v, drawn %q", quits(cmd), m.View().Content)
	}
}

func TestLoadError(t *testing.T) {
	m := newModel(options{}, ui.FixedScheme(true), nil, nil)
	if _, cmd := m.Update(loadedMsg{err: errNotPane}); !quits(cmd) || m.err != errNotPane {
		t.Fatal("a pane that can't be read does not end the tool")
	}
}

// TestPickWaitsForItsWork: the tool closes once the flash is over and the
// pick is done, in either order; losing the focus to a split the pick
// opened does not cut it short.
func TestPickWaitsForItsWork(t *testing.T) {
	var did []Pick
	m := tool(t, demoSource(demoShell), 119, 66, true)
	m.work = func(p Pick) { did = append(did, p) }
	m.floating = true
	m.Update(tea.FocusMsg{})
	cmd := press(t, m, strings.Split(label(t, m, "a3f9c21"), "")...)
	var done, flashed []tea.Msg
	for _, msg := range run(cmd) {
		switch msg.(type) {
		case doneMsg:
			done = append(done, msg)
		case flashedMsg:
			flashed = append(flashed, msg)
		}
	}
	if len(did) != 1 || len(done) != 1 || len(flashed) != 1 {
		t.Fatalf("work ran %d times, %d done, %d flashed", len(did), len(done), len(flashed))
	}
	if _, c := m.Update(tea.BlurMsg{}); quits(c) {
		t.Fatal("blur during a pick closed the tool")
	}
	if _, c := m.Update(done[0]); quits(c) {
		t.Fatal("closed before the flash was over")
	}
	if _, c := m.Update(flashed[0]); !quits(c) {
		t.Fatal("not closed after the flash and the work")
	}
	if !strings.Contains(m.View().Content, "a3f9c21") {
		t.Error("the flash does not show the pick")
	}
}

func TestBlurDismisses(t *testing.T) {
	m := tool(t, demoSource(demoShell), 119, 66, true)
	m.floating = true
	m.Update(tea.FocusMsg{})
	if _, cmd := m.Update(tea.BlurMsg{}); !quits(cmd) {
		t.Error("blur does not close")
	}
	m = tool(t, demoSource(demoShell), 119, 66, true)
	m.floating, m.opts.dismissOnBlur = true, false
	m.Update(tea.FocusMsg{})
	if _, cmd := m.Update(tea.BlurMsg{}); quits(cmd) {
		t.Error("blur closes with dismiss-on-blur off")
	}
}

// TestDrawnOverThePane: in a whole-window float the pane is drawn at its
// place in the window, and every other cell is left see-through.
func TestDrawnOverThePane(t *testing.T) {
	src := Source{Screen: plainScreen("$ git log", "a3f9c21 Drain the queue"), Size: rex.Size{Columns: 40, Rows: 10},
		Rect: rex.Rect{0.5, 0, 0.5, 1}}
	m := tool(t, src, 81, 12, true)
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != 12 {
		t.Fatalf("%d lines, want the window's 12", len(lines))
	}
	// The pane starts after half the gap, under its title bar (backdrop.Place).
	at := strings.Index(ansi.Strip(lines[2]), "$ git log")
	if at != 42 {
		t.Errorf("the pane's first row is at column %d, want 42:\n%s", at, ansi.Strip(m.View().Content))
	}
	for _, y := range []int{0, 1} {
		if strings.Contains(lines[y], "48;2") {
			t.Errorf("row %d paints a background over the window: %q", y, lines[y])
		}
	}
	if row := ansi.Strip(lines[3]); strings.TrimSpace(row[:42]) != "" || !strings.Contains(row[42:], "3f9c21 Drain") {
		t.Errorf("row 3 is %q, want the pane's second row from column 42", row)
	}
}

// TestEveryLineFits: at any size the tool draws exactly its rows, none
// wider than it.
func TestEveryLineFits(t *testing.T) {
	for _, size := range [][2]int{{20, 4}, {44, 10}, {60, 20}, {119, 66}, {200, 30}} {
		m := tool(t, demoSource(demoShell), size[0], size[1], true)
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Fatalf("%v: line %d is %d wide", size, i, w)
			}
		}
	}
}
