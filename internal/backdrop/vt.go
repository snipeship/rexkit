package backdrop

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rextheme"
)

// Cell attributes the backdrop keeps. Colour attributes (faint, reverse,
// conceal) are folded into the colours when the screen is parsed; blink is
// dropped.
const (
	attrBold uint8 = 1 << iota
	attrItalic
	attrUnderline
	attrStrike
)

// cell is one cell of a pane or of the window, with resolved colours.
type cell struct {
	content string // one grapheme; "" for the second column of a wide one
	width   uint8  // 1 or 2; 0 for the second column of a wide one
	attrs   uint8
	fg, bg  rextheme.Color
}

// blank is an empty cell in the given colours.
func blank(fg, bg rextheme.Color) cell { return cell{content: " ", width: 1, fg: fg, bg: bg} }

// grid is a rectangle of cells, row by row.
type grid struct {
	w, h  int
	cells []cell
}

func newGrid(w, h int, fill cell) grid {
	g := grid{w: max(w, 0), h: max(h, 0)}
	g.cells = make([]cell, g.w*g.h)
	for i := range g.cells {
		g.cells[i] = fill
	}
	return g
}

func (g grid) at(x, y int) *cell { return &g.cells[y*g.w+x] }

// parseScreen reads a terminal screen in Rex's vt format (rows joined by
// "\n", each restating its styles) into a cols×rows grid, resolving colours
// against the terminal palette. It understands what Rex writes: printable
// text, SGR, cursor-forward (CUF, Rex's encoding of a run of blank cells),
// erase-characters (ECH) and erase-in-line (EL, which paint the current
// background), CHA, CUB and tabs. Everything else is ignored.
func parseScreen(vt string, cols, rows int, term rextheme.Terminal) grid {
	g := newGrid(cols, rows, blank(term.Foreground, term.Background))
	lines := strings.Split(vt, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	var pen uv.Style
	for y, line := range lines {
		x := 0
		var state byte
		for len(line) > 0 {
			seq, width, n, next := ansi.DecodeSequence(line, state, p)
			state, line = next, line[n:]
			if width > 0 {
				put(g, x, y, seq, width, &pen, term)
				x += width
				continue
			}
			switch {
			case seq == "\t":
				x = min((x/8+1)*8, cols)
			case ansi.HasCsiPrefix(seq):
				cmd := ansi.Cmd(p.Command())
				if cmd.Prefix() != 0 || cmd.Intermediate() != 0 {
					break
				}
				count := func() int { n, _ := p.Param(0, 1); return max(n, 1) }
				switch cmd.Final() {
				case 'm':
					uv.ReadStyle(p.Params(), &pen)
				case 'C':
					x += count()
				case 'D':
					x = max(x-count(), 0)
				case 'G':
					x = count() - 1
				case 'X':
					erase(g, x, x+count(), y, &pen, term)
				case 'K':
					if mode, _ := p.Param(0, 0); mode == 0 {
						erase(g, x, cols, y, &pen, term)
					}
				}
			}
		}
	}
	return g
}

// put writes one grapheme at (x, y). A wide grapheme that does not fit is
// dropped, as a terminal would wrap it.
func put(g grid, x, y int, s string, width int, pen *uv.Style, term rextheme.Terminal) {
	if x < 0 || y >= g.h || x+width > g.w {
		return
	}
	fg, bg, attrs := resolve(pen, term)
	*g.at(x, y) = cell{content: s, width: uint8(width), attrs: attrs, fg: fg, bg: bg}
	for i := 1; i < width; i++ {
		*g.at(x+i, y) = cell{fg: fg, bg: bg, attrs: attrs}
	}
}

// erase blanks cells [from, to) of row y in the pen's background.
func erase(g grid, from, to, y int, pen *uv.Style, term rextheme.Terminal) {
	_, bg, _ := resolve(pen, term)
	for x := max(from, 0); x < min(to, g.w); x++ {
		*g.at(x, y) = blank(term.Foreground, bg)
	}
}

// resolve turns a pen into the colours and attributes a cell shows.
func resolve(pen *uv.Style, term rextheme.Terminal) (fg, bg rextheme.Color, attrs uint8) {
	fg, bg = term.Foreground, term.Background
	if pen.Fg != nil {
		fg = paletteColor(pen.Fg, term)
	}
	if pen.Bg != nil {
		bg = paletteColor(pen.Bg, term)
	}
	if pen.Attrs&uv.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if pen.Attrs&uv.AttrFaint != 0 {
		fg = fg.Mix(bg, 0.5)
	}
	if pen.Attrs&uv.AttrConceal != 0 {
		fg = bg
	}
	if pen.Attrs&uv.AttrBold != 0 {
		attrs |= attrBold
	}
	if pen.Attrs&uv.AttrItalic != 0 {
		attrs |= attrItalic
	}
	if pen.Attrs&uv.AttrStrikethrough != 0 {
		attrs |= attrStrike
	}
	if pen.Underline != uv.UnderlineNone {
		attrs |= attrUnderline
	}
	return fg, bg, attrs
}

// paletteColor resolves an SGR colour: ANSI and 256-colour indexes go
// through the theme's palette (Rex generates its own 16–255), direct colours
// stay as they are.
func paletteColor(c color.Color, term rextheme.Terminal) rextheme.Color {
	switch c := c.(type) {
	case ansi.BasicColor:
		return term.Indexed(int(c))
	case ansi.IndexedColor: // SGR 38;5;N, which is how Rex writes ANSI colours too
		return term.Indexed(int(c))
	}
	return rextheme.Of(c)
}
