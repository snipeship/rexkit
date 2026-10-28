package panes

import (
	"image"
	"strings"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
)

var dark = rextheme.Builtin(true)

// The geometry holds what was measured on Rex 1541: a 1728 px wide window
// (1696 px of layout area) holds a 118-column pane, split in half two
// 57-column panes, with 14×28 px cells.
func TestGeometry(t *testing.T) {
	l := Layout{W: 1696, H: 2024}
	if cols, rows, at := Content(l.Pane(rex.Rect{0, 0, 1, 1}), 14, 28); cols != 118 || rows != 69 || at != image.Pt(20, 76) {
		t.Errorf("whole pane: %dx%d at %v", cols, rows, at)
	}
	left, right := l.Pane(rex.Rect{0, 0, 0.5, 1}), l.Pane(rex.Rect{0.5, 0, 0.5, 0.5})
	if c, _, _ := Content(left, 14, 28); c != 57 {
		t.Errorf("left half: %d columns", c)
	}
	if right.Min.X-left.Max.X != Gap || right.Max.X != l.W || right.Min.Y != 0 || right.Max.Y != l.H/2-Gap/2 {
		t.Errorf("right half %v beside %v", right, left)
	}
	// A float at a pane's rect holds the pane's cells, half a gap up and left
	// of an inner pane.
	if f := l.Float(rex.Rect{0.5, 0, 0.5, 0.5}); f.Min.X != right.Min.X-Gap/2 || f.Max.X != right.Max.X {
		t.Errorf("float %v over pane %v", f, right)
	}
	if got := Size(56, 6, 14, 28); got != image.Pt(56*14+PadX, 6*28+PadY) {
		t.Errorf("size %v", got)
	}
	if got := LayoutFor(118, 69, 14, 28); got.W != 118*14+PadX || got.H != 69*28+PadY {
		t.Errorf("layout %v", got)
	}
}

// TestLuaPlacement: the bounds lua/tests/check.lua's "placement in the
// window measured on Rex 1541" asserts lua/tools.lua opens floats at, in
// that window (two 57-column panes side by side), are where this geometry
// puts them: Hints exactly over the right pane, Rename's 56×7 cells (and a
// column to spare) centred over it. Lua and Go then measure the same Rex.
func TestLuaPlacement(t *testing.T) {
	l := Layout{W: 1696, H: 2024}
	right := l.Pane(rex.Rect{0.5, 0, 0.5, 1})
	if hints := l.Float(rex.Rect{0.5047, 0, 0.4953, 1}); hints != right {
		t.Errorf("hints %v, its pane %v", hints, right)
	}
	rename := l.Float(rex.Rect{0.5067, 0.4261, 0.4912, 0.1478})
	if cols, rows, _ := Content(rename, 14, 28); cols < 56 || cols > 57 || rows != 7 {
		t.Errorf("rename holds %dx%d", cols, rows)
	}
	d := rename.Min.Add(rename.Max).Div(2).Sub(right.Min.Add(right.Max).Div(2))
	if max(d.X, -d.X) > 1 || max(d.Y, -d.Y) > 1 {
		t.Errorf("rename %v is %v off the middle of its pane %v", rename, d, right)
	}
	if cols, _, _ := Content(l.Pane(rex.Rect{0, 0, 1, 1}), 14, 28); cols >= 120 {
		t.Errorf("the window holds %d columns: Go to would not be the whole window", cols)
	}
}

func TestDemoScreens(t *testing.T) {
	l := LayoutFor(241, 70, 14, 29)
	screens := DemoScreens(DemoPanes(), l, 14, 29)
	if len(screens.Panes) != 3 {
		t.Fatalf("%d panes", len(screens.Panes))
	}
	all := ""
	for _, p := range screens.Panes {
		cols, rows, _ := Content(l.Pane(p.Rect), 14, 29)
		if p.Size.Columns != cols || p.Size.Rows != rows {
			t.Errorf("%s: %+v, want %dx%d", p.Program, p.Size, cols, rows)
		}
		all += p.Screen
	}
	for _, want := range []string{"git log", "func", "Claude Code"} {
		if !strings.Contains(all, want) {
			t.Errorf("demo lacks %q", want)
		}
	}
}

func text(g grid) []string {
	var rows []string
	for y := range g.h {
		var b strings.Builder
		for x := range g.w {
			b.WriteString(g.at(x, y).content)
		}
		rows = append(rows, b.String())
	}
	return rows
}

func TestParseScreen(t *testing.T) {
	term := dark.Terminal
	vt := "\x1b[0m\x1b[48;5;4mtop\x1b[3X\x1b[3C\x1b[0m end\n" +
		"\x1b[4Cmid\x1b[0m\x1b[1;7m!\x1b[0m\n" +
		"a\tb\x1b[38;2;10;20;30m界\x1b[2;31mx\n" +
		"\x1b[0m\x1b[38;5;208mo\x1b[0mk"
	g := parseScreen(vt, 12, 4, term)
	want := []string{"top    end  ", "    mid!    ", "a       b界x", "ok          "}
	for y, row := range text(g) {
		if row != want[y] {
			t.Errorf("row %d = %q, want %q", y, row, want[y])
		}
	}
	if c := g.at(0, 0); c.bg != term.Indexed(4) || c.fg != term.Foreground {
		t.Errorf("SGR 48;5;4: %+v", c)
	}
	if c := g.at(4, 0); c.bg != term.Indexed(4) {
		t.Errorf("ECH keeps the background: %+v", c)
	}
	if c := g.at(7, 1); c.fg != term.Background || c.bg != term.Foreground || c.attrs != attrBold {
		t.Errorf("reverse bold: %+v", c)
	}
	if c := g.at(9, 2); c.width != 2 || c.fg != (rextheme.Color{R: 10, G: 20, B: 30}) || g.at(10, 2).width != 0 {
		t.Errorf("wide truecolour: %+v %+v", c, *g.at(10, 2))
	}
	if c := g.at(11, 2); c.content != "x" || c.fg != term.Indexed(1).Mix(term.Background, 0.5) {
		t.Errorf("faint red: %+v", c)
	}
	if c := g.at(0, 3); c.fg != term.Indexed(208) {
		t.Errorf("256 colours come from Rex's palette: %+v", c)
	}
	// Only the last rows lines are the screen.
	if got := text(parseScreen("old\nnew", 3, 1, term)); got[0] != "new" {
		t.Errorf("last rows: %q", got)
	}
}

func BenchmarkParse(b *testing.B) {
	vt := strings.Repeat("\x1b[0m\x1b[38;5;2mok\x1b[0m \x1b[1;34mdir\x1b[0m some plain text here \x1b[38;2;1;2;3mtrue\x1b[0m\n", 67)
	for b.Loop() {
		parseScreen(vt, 241, 67, dark.Terminal)
	}
}
