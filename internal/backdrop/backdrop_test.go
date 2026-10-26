package backdrop

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

var dark = rextheme.Builtin(true)

// The window measured with rexkit calibrate (geometry.go): 199×57, a 97×55
// pane on the left, two 97×26 panes stacked on the right.
func TestPlace(t *testing.T) {
	tests := []struct {
		name       string
		rect       rex.Rect
		cols, rows int
		want       ui.Rect
	}{
		{"left", rex.Rect{0, 0, 0.5, 1}, 97, 55, ui.Rect{X: 0, Y: 2, W: 97, H: 55}},
		{"top right", rex.Rect{0.5, 0, 0.5, 0.5}, 97, 26, ui.Rect{X: 101, Y: 2, W: 97, H: 26}},
		{"bottom right", rex.Rect{0.5, 0.5, 0.5, 0.5}, 97, 26, ui.Rect{X: 101, Y: 31, W: 97, H: 26}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Place(tt.rect, tt.cols, tt.rows, 199, 57); got != tt.want {
				t.Errorf("Place = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPaneSize(t *testing.T) {
	tests := []struct {
		rect       rex.Rect
		w, h       int
		cols, rows int
	}{
		{rex.Rect{0, 0, 1, 1}, 241, 70, 241, 68},
		{rex.Rect{0, 0, 0.5, 1}, 199, 57, 98, 55},
		{rex.Rect{0.5, 0, 0.5, 0.5}, 199, 57, 98, 26},
	}
	for _, tt := range tests {
		if c, r := paneSize(tt.rect, tt.w, tt.h); c != tt.cols || r != tt.rows {
			t.Errorf("paneSize(%v) in %dx%d = %dx%d, want %dx%d", tt.rect, tt.w, tt.h, c, r, tt.cols, tt.rows)
		}
	}
}

// screen parses vt and returns its rows as plain text, for asserts.
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

func TestEdgeGlyph(t *testing.T) {
	card := ui.Rect{X: 10, Y: 5, W: 20, H: 10}
	tests := []struct {
		x, y int
		want string
	}{
		{9, 8, "▕"}, {30, 8, "▏"}, {9, 5, ""}, {9, 14, ""}, {15, 4, ""}, {15, 15, ""}, {8, 8, ""},
	}
	for _, tt := range tests {
		if got := edgeGlyph(tt.x, tt.y, card); got != tt.want {
			t.Errorf("edgeGlyph(%d, %d) = %q, want %q", tt.x, tt.y, got, tt.want)
		}
	}
	if lookFor(dark).edge != (rextheme.Color{}) {
		t.Error("a dark theme draws a hairline")
	}
	if lookFor(rextheme.Builtin(false)).edge == (rextheme.Color{}) {
		t.Error("a light theme draws no hairline")
	}
}

func TestFade(t *testing.T) {
	if Fade(0) != 0 || Fade(FadeFrames) != 1 || Fade(FadeFrames+3) != 1 {
		t.Errorf("Fade ends: %v %v", Fade(0), Fade(FadeFrames))
	}
	for i := 1; i <= FadeFrames; i++ {
		if Fade(i) <= Fade(i-1) {
			t.Errorf("Fade(%d) = %v is not above Fade(%d) = %v", i, Fade(i), i-1, Fade(i-1))
		}
	}
	if Fade(1) < 0.4 {
		t.Errorf("Fade(1) = %v: an ease-out starts fast", Fade(1))
	}
}

// draw renders a frame into cells, as the stage's compositor does.
func draw(t *testing.T, s string, w, h int) *uv.ScreenBuffer {
	t.Helper()
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(s).Draw(buf, buf.Bounds())
	return &buf
}

func colorAt(buf *uv.ScreenBuffer, x, y int) (fg, bg rextheme.Color) {
	c := buf.CellAt(x, y)
	return rextheme.Of(c.Style.Fg), rextheme.Of(c.Style.Bg)
}

func TestFrame(t *testing.T) {
	screens := rex.Screens{Panes: []rex.PaneScreen{
		{Rect: rex.Rect{0, 0, 0.5, 1}, Size: rex.Size{Columns: 7, Rows: 7}, Screen: "left\n\x1b[31mred\x1b[0m"},
		{Rect: rex.Rect{0.5, 0, 0.5, 1}, Size: rex.Size{Columns: 7, Rows: 7}, Screen: "right界"},
		{Rect: rex.Rect{0, 0, 1, 1}, Error: "gone"},
	}}
	b := New(dark, screens)
	const w, h = 20, 10
	card := ui.Rect{X: 8, Y: 3, W: 4, H: 3}
	sil := ui.CardSilhouette(card)

	clear := draw(t, b.Frame(w, h, sil, 0), w, h)
	if got := clear.CellAt(0, HeaderRows).Content; got != "l" {
		t.Errorf("left pane starts with %q", got)
	}
	if fg, bg := colorAt(clear, 1, HeaderRows); fg != dark.Terminal.Foreground || bg != dark.Terminal.Background {
		t.Errorf("strength 0 keeps the colours: %s on %s", fg, bg)
	}
	if fg, _ := colorAt(clear, 1, HeaderRows+1); fg != dark.Terminal.Indexed(1) {
		t.Errorf("red: %s", fg)
	}
	if _, bg := colorAt(clear, 0, 0); bg != dark.Terminal.Background {
		t.Errorf("a pane's title bar is the pane's: %s", bg)
	}
	if _, bg := colorAt(clear, 9, 0); bg != dark.UI.Canvas {
		t.Errorf("between the panes is the canvas: %s", bg)
	}
	if c := clear.CellAt(9, 4); c.Content != " " || c.Style.Bg != nil {
		t.Errorf("under the card: %+v", c)
	}

	dim := draw(t, b.Frame(w, h, sil, 1), w, h)
	fg0, bg0 := colorAt(clear, 1, HeaderRows)
	fg1, bg1 := colorAt(dim, 1, HeaderRows)
	if dim.CellAt(0, HeaderRows).Content != "l" {
		t.Error("dimming keeps the glyphs")
	}
	if fg1.Luma() >= fg0.Luma() || bg1.Luma() > bg0.Luma() {
		t.Errorf("dimmed %s on %s is not darker than %s on %s", fg1, bg1, fg0, bg0)
	}
	contrast := func(fg, bg rextheme.Color) float64 { return fg.Luma() - bg.Luma() }
	if contrast(fg1, bg1) > contrast(fg0, bg0)*0.5 {
		t.Errorf("text keeps too much contrast: %v of %v", contrast(fg1, bg1), contrast(fg0, bg0))
	}
	// The shadow: under the card is darker than the same pane far away.
	_, near := colorAt(dim, card.X+1, card.Y+card.H)
	_, far := colorAt(dim, w-1, card.Y+card.H)
	if near.Luma() >= far.Luma() {
		t.Errorf("no shadow: %s next to the card, %s away from it", near, far)
	}
	if got := b.Frame(w, h, sil, 1); got != b.Frame(w, h, sil, 1) {
		t.Error("frames differ")
	}
	// Every row is w cells wide, and a wide glyph cut by the card is a space.
	for y, row := range strings.Split(b.Frame(w, h, ui.CardSilhouette(ui.Rect{X: 16, Y: 0, W: 4, H: 10}), 1), "\n") {
		if ansi.StringWidth(row) != w {
			t.Errorf("row %d is %d wide", y, ansi.StringWidth(row))
		}
	}
	// A resize lays the panes out again.
	if rows := strings.Split(b.Frame(30, 12, sil, 1), "\n"); len(rows) != 12 || ansi.StringWidth(rows[0]) != 30 {
		t.Errorf("after a resize: %d rows of %d", len(rows), ansi.StringWidth(rows[0]))
	}
}

func TestLight(t *testing.T) {
	light := rextheme.Builtin(false)
	lk := lookFor(light)
	if lk.scrim.Luma() > light.UI.Canvas.Luma()-0.2 || lk.scrim.Luma() < 0.25 {
		t.Errorf("light scrim %s should be a mid grey (canvas %s)", lk.scrim, light.UI.Canvas)
	}
}

func TestDemo(t *testing.T) {
	for _, theme := range []rextheme.Theme{dark, rextheme.Builtin(false)} {
		b := Demo(theme, 241, 70)
		if len(b.panes) != 3 {
			t.Fatalf("%d panes", len(b.panes))
		}
		out := b.Frame(241, 70, ui.CardSilhouette(ui.Rect{X: 27, Y: 9, W: 188, H: 52}), 1)
		for _, want := range []string{"git log", "func", "Claude Code"} {
			if !strings.Contains(ansi.Strip(b.Frame(241, 70, ui.Silhouette{}, 0)), want) {
				t.Errorf("%s: demo lacks %q", theme.ID, want)
			}
		}
		if rows := strings.Split(out, "\n"); len(rows) != 70 {
			t.Errorf("%d rows", len(rows))
		}
	}
}

func BenchmarkFrame(b *testing.B) {
	d := Demo(dark, 241, 70)
	card := ui.CardSilhouette(ui.Rect{X: 27, Y: 9, W: 188, H: 52})
	i := 0
	for b.Loop() {
		i++
		d.Frame(241, 70, card, float64(i%100)/100) // a new strength every time: no cache
	}
}

func BenchmarkParse(b *testing.B) {
	vt := strings.Repeat("\x1b[0m\x1b[38;5;2mok\x1b[0m \x1b[1;34mdir\x1b[0m some plain text here \x1b[38;2;1;2;3mtrue\x1b[0m\n", 67)
	for b.Loop() {
		parseScreen(vt, 241, 67, dark.Terminal)
	}
}

// A pane's title bar shows its program and title above its content, as
// Rex's does: the program in the content colour, the title in the secondary.
func TestHeader(t *testing.T) {
	screens := rex.Screens{Panes: []rex.PaneScreen{
		{Rect: rex.Rect{0, 0, 1, 1}, Size: rex.Size{Columns: 30, Rows: 3}, Screen: "$", Program: "nu", Title: "~/code\x1b[31m\x07"},
	}}
	const w, h = 30, 5
	g := draw(t, New(dark, screens).Frame(w, h, ui.Silhouette{}, 0), w, h)
	var row strings.Builder
	for x := range w {
		row.WriteString(g.CellAt(x, 0).Content)
	}
	if got := strings.TrimRight(row.String(), " "); got != "›_ nu ~/code␇" {
		t.Errorf("title bar %q", got)
	}
	if fg, _ := colorAt(g, 3, 0); fg != dark.UI.Content {
		t.Errorf("program in %s, want the content colour", fg)
	}
	if fg, _ := colorAt(g, 6, 0); fg != dark.UI.ContentSecondary {
		t.Errorf("title in %s, want the secondary colour", fg)
	}
	if got := g.CellAt(0, HeaderRows).Content; got != "$" {
		t.Errorf("content starts with %q", got)
	}
	claude := screens
	claude.Panes = []rex.PaneScreen{{Rect: rex.Rect{0, 0, 1, 1}, Size: rex.Size{Columns: 30, Rows: 3}, Program: "claude", Title: "Plan"}}
	g = draw(t, New(dark, claude).Frame(w, h, ui.Silhouette{}, 0), w, h)
	if got := g.CellAt(0, 0).Content + g.CellAt(2, 0).Content; got != "✻P" {
		t.Errorf("claude's title bar starts %q", got)
	}
}
