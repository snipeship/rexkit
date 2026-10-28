package snap

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"rexkit/internal/panes"
	"rexkit/internal/rextheme"
)

// palette is a test palette with colours easy to tell apart.
var palette = Palette{
	Fg: color.RGBA{200, 200, 200, 255}, Bg: color.RGBA{10, 10, 10, 255},
	Indexed: [256]color.RGBA{1: {255, 0, 0, 255}, 4: {0, 0, 255, 255}},
}

// renderer returns a renderer with the default fonts, or skips the test
// where they are not installed.
func renderer(t *testing.T, opts Options) *Renderer {
	t.Helper()
	if _, err := os.Stat(DefaultFontPaths().Primary[styleRegular]); err != nil {
		t.Skip("Berkeley Mono is not installed")
	}
	if opts.Palette == (Palette{}) {
		opts.Palette = palette
	}
	r, err := NewRenderer(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func at(img *image.RGBA, x, y int) color.RGBA { return img.RGBAAt(x, y) }

func TestCellSize(t *testing.T) {
	r := renderer(t, Options{})
	if b := r.Render("hi", 3, 2).Bounds(); b.Dx() != 3*CellW || b.Dy() != 2*CellH {
		t.Errorf("3×2 cells are %v, want %d×%d", b, 3*CellW, 2*CellH)
	}
	if b := renderer(t, Options{Half: true}).Render("", 4, 2).Bounds(); b.Dx() != 2*CellW || b.Dy() != CellH {
		t.Errorf("half size is %v", b)
	}
	if r.fonts.size < 20 || r.fonts.size > 27 || r.fonts.baseline < CellH/2 || r.fonts.baseline >= CellH {
		t.Errorf("font size %.2f px, baseline %d", r.fonts.size, r.fonts.baseline)
	}
}

func TestColoursLandInTheirCells(t *testing.T) {
	r := renderer(t, Options{})
	// Cell 0: default; cell 1: ANSI red background; cell 2: true-colour
	// background; cell 3: reverse video of the defaults.
	img := r.Render(" \x1b[41m \x1b[48;2;0;128;0m \x1b[0;7m \x1b[0m", 4, 1)
	tests := []struct {
		cell int
		want color.RGBA
	}{
		{0, palette.Bg},
		{1, palette.Indexed[1]},
		{2, color.RGBA{0, 128, 0, 255}},
		{3, palette.Fg},
	}
	for _, tt := range tests {
		for _, p := range []image.Point{{tt.cell*CellW + 1, 1}, {tt.cell*CellW + CellW - 1, CellH - 1}} {
			if got := at(img, p.X, p.Y); got != tt.want {
				t.Errorf("cell %d pixel %v = %v, want %v", tt.cell, p, got, tt.want)
			}
		}
	}
	// A glyph is drawn in its foreground colour, inside its cell.
	img = r.Render("\x1b[34m█M\x1b[0m", 2, 1)
	if got := at(img, CellW/2, CellH/2); got != palette.Indexed[4] {
		t.Errorf("full block = %v, want blue", got)
	}
	blue := 0
	for y := range CellH {
		for x := CellW; x < 2*CellW; x++ {
			if c := at(img, x, y); c.B > c.R+100 {
				blue++
			}
		}
	}
	if blue < 20 {
		t.Errorf("M has %d blue pixels", blue)
	}
}

// TestBoxDrawingConnects: lines run edge to edge without gaps, from cell to
// cell, as a terminal draws them.
func TestBoxDrawingConnects(t *testing.T) {
	r := renderer(t, Options{})
	fg := palette.Fg
	img := r.Render("╭─┬─╮\n│ │ │\n├─┼─┤\n╰─┴─╯", 5, 4)
	row := func(y int) {
		t.Helper()
		for x := CellW / 2; x < 5*CellW-CellW/2; x++ {
			if at(img, x, y) != fg {
				t.Fatalf("horizontal line broken at (%d, %d)", x, y)
			}
		}
	}
	col := func(x int) {
		t.Helper()
		for y := CellH / 2; y < 4*CellH-CellH/2; y++ {
			if at(img, x, y) != fg {
				t.Fatalf("vertical line broken at (%d, %d)", x, y)
			}
		}
	}
	thin := max(1, CellW/7)
	mid := (CellH-thin)/2 + thin/2
	row(2*CellH + mid) // ├─┼─┤
	col(2*CellW + (CellW-thin)/2 + thin/2)
	// The rounded corners reach the cells' edges where the lines go on.
	for _, p := range []image.Point{{CellW - 1, mid}, {(CellW - thin) / 2, CellH - 1}} {
		if c := at(img, p.X, p.Y); c != fg {
			t.Errorf("╭ does not reach the edge at %v: %v", p, c)
		}
	}
	if c := at(img, 0, 0); c != palette.Bg {
		t.Errorf("╭ fills its outer corner: %v", c)
	}
}

func TestBlocksFillTheirPart(t *testing.T) {
	r := renderer(t, Options{})
	img := r.Render("▀▐▖", 3, 1)
	fg, bg := palette.Fg, palette.Bg
	checks := []struct {
		x, y int
		want color.RGBA
	}{
		{1, 1, fg}, {1, CellH - 2, bg}, // ▀
		{CellW + 1, 1, bg}, {2*CellW - 2, CellH - 2, fg}, // ▐
		{2*CellW + 1, CellH - 2, fg}, {2*CellW + 1, 1, bg}, {3*CellW - 2, CellH - 2, bg}, // ▖
	}
	for _, c := range checks {
		if got := at(img, c.x, c.y); got != c.want {
			t.Errorf("(%d, %d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// ⏺ is in none of the fonts: it is drawn as a disc, not a hollow box.
func TestRecordBulletIsADisc(t *testing.T) {
	r := renderer(t, Options{})
	img := r.Render("⏺", 1, 1)
	if got := at(img, CellW/2, CellH*55/100); got != palette.Fg {
		t.Errorf("centre = %v, want the foreground", got)
	}
	if got := at(img, 0, 0); got != palette.Bg {
		t.Errorf("corner = %v, want the background", got)
	}
	if len(r.Missing()) > 0 {
		t.Errorf("missing %q", string(r.Missing()))
	}
}

func TestWideRunesTakeTwoCells(t *testing.T) {
	r := renderer(t, Options{})
	img := r.Render("\x1b[41m漢\x1b[0mx", 3, 1)
	if got := at(img, 2*CellW-1, 0); got != palette.Indexed[1] {
		t.Errorf("the second half of 漢 = %v, want its red background", got)
	}
	if got := at(img, 2*CellW+1, 0); got != palette.Bg {
		t.Errorf("x went into the second half of 漢: %v", got)
	}
	if missing := r.Missing(); len(missing) > 0 {
		t.Errorf("no glyph for %q", string(missing))
	}
}

func TestEncode(t *testing.T) {
	r := renderer(t, Options{})
	var buf bytes.Buffer
	if err := Encode(&buf, r.Render("ok", 2, 1)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil || img.Bounds().Dx() != 2*CellW {
		t.Errorf("decoded %v, %v", img.Bounds(), err)
	}
}

func TestPaletteOf(t *testing.T) {
	term := rextheme.Builtin(true).Terminal
	p := PaletteOf(term)
	if p.Bg != rgba(term.Background) || p.Indexed[1] != rgba(term.ANSI[1]) || p.Indexed[200] != rgba(term.Palette[200]) {
		t.Errorf("palette %v %v %v", p.Bg, p.Indexed[1], p.Indexed[200])
	}
	if Theme(false).Dark || !Theme(true).Dark {
		t.Error("Theme picks the wrong appearance")
	}
}

func BenchmarkRenderFullWindow(b *testing.B) {
	if _, err := os.Stat(DefaultFontPaths().Primary[styleRegular]); err != nil {
		b.Skip("Berkeley Mono is not installed")
	}
	r, err := NewRenderer(Options{Palette: palette})
	if err != nil {
		b.Fatal(err)
	}
	var line bytes.Buffer
	for i := range 241 {
		line.WriteString("\x1b[3" + string(rune('1'+i%6)) + "m" + string(rune('a'+i%26)))
	}
	screen := bytes.Repeat(append(line.Bytes(), '\n'), 70)
	for b.Loop() {
		r.Render(string(screen), 241, 70)
	}
}

// Each smooth mosaic wedge and its partner 22 code points on cover the cell
// between them, and fill the corner their name gives.
func TestWedges(t *testing.T) {
	fg := color.RGBA{255, 255, 255, 255}
	cover := func(r rune) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, CellW, CellH))
		if !drawSprite(pen{img: img, box: img.Bounds(), fg: fg}, r) {
			t.Fatalf("%U is not drawn", r)
		}
		return img
	}
	lit := func(img *image.RGBA, x, y int) bool { return img.RGBAAt(x, y).R > 127 }
	for r := wedgeFirst; r < wedgeFirst+22; r++ {
		a, b := cover(r), cover(r+22)
		for y := range CellH {
			for x := range CellW {
				sum := int(a.RGBAAt(x, y).R) + int(b.RGBAAt(x, y).R)
				if sum < 250 || sum > 260 {
					t.Fatalf("%U and %U cover pixel (%d, %d) %d/255", r, r+22, x, y, sum)
				}
			}
		}
	}
	corners := []struct {
		r          rune
		x, y       int
		lit, unlit bool
	}{
		{0x1fb4a, CellW - 1, CellH - 1, true, false}, // 🭊 lower right
		{0x1fb4a, 0, 0, false, true},
		{0x1fb3f, 0, CellH - 1, true, false}, // 🬿 lower left
		{0x1fb65, CellW - 1, 0, true, false}, // 🭥 upper right
		{0x1fb5a, 0, 0, true, false},         // 🭚 upper left
		{0x1fb5a, CellW - 1, CellH - 1, false, true},
	}
	for _, c := range corners {
		if got := lit(cover(c.r), c.x, c.y); got != c.lit {
			t.Errorf("%U at (%d, %d): lit %v, want %v", c.r, c.x, c.y, got, c.lit)
		}
	}
}

// TestWindowPadding: as Rex does, a float's padding takes the backgrounds
// at the edges of its cells: the sides those of each row's end cells, the
// bottom those of the last row's cells, column by column.
func TestWindowPadding(t *testing.T) {
	r := renderer(t, Options{})
	theme := rextheme.Builtin(true)
	l := panes.LayoutFor(40, 20, CellW, CellH)
	box := image.Rect(100, 100, 100+panes.PadX+10*CellW, 100+panes.PadY+3*CellH)
	// the last row: a red cell at column 2, and a red last cell
	content := "\n\n  \x1b[41m \x1b[0m      \x1b[41m \x1b[0m"
	img := r.Window(theme, Window{Layout: l, Floats: []Float{{At: box, Content: content}}})
	cells := box.Min.Add(origin()).Add(image.Pt(panes.PadLeft, panes.Header))
	below := cells.Y + 3*CellH + (panes.PadY-panes.Header)/2
	red := palette.Indexed[1]
	for _, tt := range []struct {
		name string
		p    image.Point
		want color.RGBA
	}{
		{"under the red cell", image.Pt(cells.X+2*CellW+CellW/2, below), red},
		{"under a plain cell", image.Pt(cells.X+5*CellW+CellW/2, below), SurfaceOf(theme)},
		{"beside the red last cell", image.Pt(cells.X+10*CellW+5, cells.Y+2*CellH+CellH/2), red},
	} {
		if got := at(img, tt.p.X, tt.p.Y); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}
