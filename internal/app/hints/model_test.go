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
	return toolWith(t, options{dismissOnBlur: true}, src, w, h, dark)
}

// toolWith is tool with options of its own (a key of the hints setting).
func toolWith(t *testing.T, opts options, src Source, w, h int, dark bool) *model {
	t.Helper()
	m := newModel(opts, ui.FixedScheme(dark), func() tea.Msg { return loadedMsg{src: src} }, nil)
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
		{"tab, then the label pastes", func(l string) []string { return []string{"tab", l[:1], l[1:]} }, ActPaste},
		{"tab after the first key pastes", func(l string) []string { return []string{l[:1], "tab", l[1:]} }, ActPaste},
		{"tab twice copies", func(l string) []string { return []string{"tab", l[:1], "tab", l[1:]} }, ActCopy},
		{"shift opens after tab", func(l string) []string { return []string{"tab", "shift+" + l[:1], l[1:]} }, ActOpen},
		{"tab after shift still opens", func(l string) []string { return []string{"shift+" + l[:1], "tab", l[1:]} }, ActOpen},
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
	press(t, m, "backspace", "tab", "y")
	if bar := ansi.Strip(m.View().Content); m.shift || !m.paste || !strings.Contains(bar, "to paste") {
		t.Errorf("after ⌫ ⇥ y: shift %v paste %v, and the bar does not say to paste", m.shift, m.paste)
	}
	press(t, m, "backspace", "tab", "shift+y")
	press(t, m, "y")
	if m.miss != "yy" || m.typed != "y" {
		t.Fatalf("after yy: miss %q typed %q", m.miss, m.typed)
	}
	if bar := ansi.Strip(m.View().Content); !strings.Contains(bar, "no label yy") {
		t.Errorf("the bar does not say no label yy")
	}
	press(t, m, "backspace")
	if m.typed != "" || m.shift || m.paste || m.miss != "" || m.left() != all {
		t.Fatalf("after backspace: typed %q shift %v paste %v miss %q", m.typed, m.shift, m.paste, m.miss)
	}
	press(t, m, "5", "space", "ctrl+a")
	if m.picked != nil || m.typed != "" {
		t.Fatalf("keys that are no letters picked %+v, typed %q", m.picked, m.typed)
	}
	// ⌥ + a letter is no key of Hints: it neither types nor picks, and
	// the bar says so as for any key that starts no label.
	press(t, m, "backspace")
	l := label(t, m, "a3f9c21")
	press(t, m, "alt+"+l[:1])
	if len(l) > 1 {
		press(t, m, "alt+"+l[1:])
	}
	if m.picked != nil || m.typed != "" || m.miss != "⌥"+l[len(l)-1:] {
		t.Fatalf("⌥ letters picked %+v, typed %q, missed %q", m.picked, m.typed, m.miss)
	}
	if bar := ansi.Strip(m.View().Content); !strings.Contains(bar, "no label ⌥"+l[len(l)-1:]) {
		t.Errorf("the bar does not say no label ⌥%s", l[len(l)-1:])
	}
}

// TestTabSwitchesTheAction: ⇥ switches a label between copy and paste, and
// the bar says which, and what ⇥ switches to; ⌫ keeps the choice. Once ⇧
// says the label opens, ⇥ changes nothing and the bar leaves it out.
func TestTabSwitchesTheAction(t *testing.T) {
	m := tool(t, demoSource(demoShell), 119, 66, true)
	// the bar's text, its keycaps and chips without their round ends
	bar := func() string { return strings.NewReplacer("▐", "", "▌", "").Replace(ansi.Strip(m.View().Content)) }
	if b := bar(); !strings.Contains(b, "a copy") || !strings.Contains(b, "tab paste") {
		t.Fatalf("the bar does not say a copy · tab paste:\n%s", b)
	}
	press(t, m, "tab")
	if b := bar(); !m.paste || !strings.Contains(b, "a paste") || !strings.Contains(b, "tab copy") {
		t.Fatalf("after ⇥ the bar does not say a paste · tab copy:\n%s", b)
	}
	press(t, m, "y")
	if b := bar(); !strings.Contains(b, "left to paste") || !strings.Contains(b, "tab copy") {
		t.Fatalf("after ⇥ y the bar does not say left to paste · tab copy:\n%s", b)
	}
	// A miss still says what a label does.
	press(t, m, "y")
	if b := bar(); !strings.Contains(b, "no label yy") || !strings.Contains(b, "paste") || !strings.Contains(b, "tab copy") {
		t.Fatalf("after ⇥ y y the bar does not say no label yy · paste · tab copy:\n%s", b)
	}
	press(t, m, "backspace")
	if !m.paste {
		t.Fatal("⌫ undid the ⇥")
	}
	press(t, m, "tab")
	if b := bar(); m.paste || !strings.Contains(b, "a copy") {
		t.Fatalf("a second ⇥ does not go back to copy:\n%s", b)
	}
	press(t, m, "shift+y")
	if b := bar(); strings.Contains(b, "tab paste") || strings.Contains(b, "tab copy") || !strings.Contains(b, "to open") {
		t.Fatalf("with ⇧ the bar still offers ⇥, or does not say open:\n%s", b)
	}
	press(t, m, "tab")
	if m.paste {
		t.Error("⇥ switched to paste a label that opens")
	}
}

// TestPasteWordStandsOut: the bar draws paste in the accent, copy in the
// text colour, so a label that types into the pane is not taken for a copy.
func TestPasteWordStandsOut(t *testing.T) {
	m := tool(t, demoSource(demoShell), 119, 66, true)
	t0 := m.barTheme()
	if v := m.verb(t0); v.Text != "copy" || v.Style.GetForeground() != t0.Text.GetForeground() {
		t.Errorf("copy is %+v", v)
	}
	press(t, m, "tab")
	if v := m.verb(t0); v.Text != "paste" || v.Style.GetForeground() != t0.Accent.GetForeground() || v.Style.GetForeground() == t0.Text.GetForeground() {
		t.Errorf("paste is %+v, not in the accent", v)
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
	if p := m.Picked(); p == nil || p.Match.Text != "a3f9c21" || p.Action != ActCopy {
		t.Fatalf("picked %+v, want a3f9c21 copied", p)
	}
	// ⇥ typed that early counts too: the label pastes.
	m = newModel(options{}, ui.FixedScheme(true), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 119, Height: 66})
	press(t, m, append([]string{"tab"}, strings.Split(l, "")...)...)
	m.Update(loadedMsg{src: src})
	if p := m.Picked(); p == nil || p.Match.Text != "a3f9c21" || p.Action != ActPaste {
		t.Fatalf("after an early ⇥ picked %+v, want a3f9c21 pasted", p)
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

// TestDrawnOverThePane: the float is over the pane, so the pane's cells
// are drawn where they are, from the float's first cell, and plain cells
// leave Rex's surface to show.
func TestDrawnOverThePane(t *testing.T) {
	src := Source{Screen: plainScreen("$ git log", "a3f9c21 Drain the queue"), Size: rex.Size{Columns: 40, Rows: 10}}
	m := tool(t, src, 40, 10, true)
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != 10 {
		t.Fatalf("%d lines, want the pane's 10", len(lines))
	}
	if !strings.HasPrefix(ansi.Strip(lines[0]), "$ git log") {
		t.Errorf("row 0 is %q, want the pane's first row from column 0", ansi.Strip(lines[0]))
	}
	if !strings.Contains(ansi.Strip(lines[1]), "3f9c21 Drain") {
		t.Errorf("row 1 is %q, want the pane's second row", ansi.Strip(lines[1]))
	}
	for _, y := range []int{0, 2, 9} {
		if strings.Contains(lines[y], "48;2") {
			t.Errorf("row %d paints a background over the surface: %q", y, lines[y])
		}
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
