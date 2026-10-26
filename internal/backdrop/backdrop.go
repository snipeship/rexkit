// Package backdrop draws what a floating tool shows around its card, so the
// card reads as a layer above the panes rather than another pane among them.
//
// A terminal has no per-cell transparency, and a cell left at the default
// background shows the real panes at full strength. So the backdrop is drawn:
// the tool's whole-window float redraws the window's own tiled panes, with
// their real text and colours, blended towards a scrim colour and a little
// desaturated, with a soft shadow under the card (ui.ShadeAt). One strength
// (0 the panes as they are, 1 fully dimmed) drives every amount, so the
// stage can fade it in.
//
// The pane screens come from one round trip to Rex (rex.API.Screens) and are
// parsed once; a frame only blends and encodes (well under 5 ms for a
// 241×70 window), and the last frame is cached.
package backdrop

import (
	"context"
	"fmt"
	"strconv"

	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// Backdrop is a window's panes, parsed and ready to draw at any window size
// and strength. It is not safe for concurrent use.
type Backdrop struct {
	theme rextheme.Theme
	look  look
	panes []pane

	base  grid  // the panes placed in the window, at the size last drawn
	frame frame // the last frame drawn
}

// pane is one pane's screen, its title bar and where it sits.
type pane struct {
	rect   rex.Rect
	grid   grid
	header grid // one row, drawn on the first of the HeaderRows above grid
}

type frame struct {
	w, h     int
	card     ui.Silhouette
	strength float64
	out      string
}

// New parses the panes' screens in the theme's terminal colours. A pane
// that could not be read is drawn empty.
func New(theme rextheme.Theme, screens rex.Screens) *Backdrop {
	b := &Backdrop{theme: theme, look: lookFor(theme)}
	for _, p := range screens.Panes {
		cols, rows := p.Size.Columns, p.Size.Rows
		if cols <= 0 || rows <= 0 {
			continue
		}
		b.panes = append(b.panes, pane{
			rect:   p.Rect,
			grid:   parseScreen(p.Screen, cols, rows, theme.Terminal),
			header: parseScreen(header(p, theme.UI), cols, 1, theme.Terminal),
		})
	}
	return b
}

// Load reads the panes of the window holding block (a float's own block) in
// session and parses them.
func Load(ctx context.Context, api rex.API, theme rextheme.Theme, session, block string) (*Backdrop, error) {
	screens, err := api.Screens(ctx, session, block)
	if err != nil {
		return nil, err
	}
	return New(theme, screens), nil
}

// Theme is the theme the backdrop draws in.
func (b *Backdrop) Theme() rextheme.Theme { return b.theme }

// Frame draws the w×h window around a card with silhouette card: every cell
// keeps its glyph, its colours are blended towards the scrim by strength
// (0–1), and cells around the card get its shadow. Cells the card covers
// are left blank (the stage draws the card over them); its lip rows show
// the backdrop, for the stage to put under the card's Clear cells. Rows are
// joined by "\n" and each ends with a reset.
func (b *Backdrop) Frame(w, h int, card ui.Silhouette, strength float64) string {
	strength = max(0, min(1, strength))
	if f := b.frame; f.out != "" && f.w == w && f.h == h && f.card == card && f.strength == strength {
		return f.out
	}
	if b.base.w != w || b.base.h != h {
		b.layout(w, h)
	}
	out := b.render(card, strength)
	b.frame = frame{w: w, h: h, card: card, strength: strength, out: out}
	return out
}

// layout places every pane in a w×h window over the canvas.
func (b *Backdrop) layout(w, h int) {
	canvas := b.theme.UI.Canvas
	g := newGrid(w, h, blank(canvas, canvas))
	term := b.theme.Terminal
	for _, p := range b.panes {
		r := Place(p.rect, p.grid.w, p.grid.h, w, h)
		bar := ui.Rect{X: r.X, Y: r.Y - HeaderRows, W: r.W, H: HeaderRows}
		g.put(newGrid(bar.W, bar.H, blank(term.Foreground, term.Background)), bar.X, bar.Y)
		g.put(p.header, bar.X, bar.Y)
		g.put(p.grid, r.X, r.Y)
	}
	b.base = g
}

// put draws src with its top-left corner at (x, y), clipped to g.
func (g grid) put(src grid, x0, y0 int) {
	for y := max(y0, 0); y < min(y0+src.h, g.h); y++ {
		for x := max(x0, 0); x < min(x0+src.w, g.w); x++ {
			c := *src.at(x-x0, y-y0)
			if c.width == 2 && x+1 >= g.w || c.width == 0 && x == 0 {
				c = blank(c.fg, c.bg) // a wide glyph cut by the window's edge
			}
			*g.at(x, y) = c
		}
	}
}

// header is a pane's title bar as Rex draws it, in vt: the program's icon
// and name in the content colour, then the title in the secondary one.
// Claude Code shows as Rex shows it, as ✻ before its title.
func header(p rex.PaneScreen, u rextheme.UI) string {
	program, title := sanitize.Label(p.Program), sanitize.Label(p.Title)
	if program == "" && title == "" {
		return ""
	}
	name := "›_ " + program
	if program == "claude" {
		name = "✻"
	} else if program == "" {
		name = "›_"
	}
	return vtLine(span{"1;" + sgrFg(u.Content), name}, span{"", " "}, span{sgrFg(u.ContentSecondary), title})
}

// sgrFg is the SGR parameters of a truecolor foreground.
func sgrFg(c rextheme.Color) string { return fmt.Sprintf("38;2;%d;%d;%d", c.R, c.G, c.B) }

// render encodes the base grid at a strength around a card.
func (b *Backdrop) render(sil ui.Silhouette, s float64) string {
	g, lk, card := b.base, b.look, sil.Rect
	fgs, bgs := map[rextheme.Color]rextheme.Color{}, map[rextheme.Color]rextheme.Color{}
	dim := func(memo map[rextheme.Color]rextheme.Color, c rextheme.Color, amount float64) rextheme.Color {
		if d, ok := memo[c]; ok {
			return d
		}
		d := c.Desaturate(Desaturation*s).Mix(lk.scrim, amount*s)
		memo[c] = d
		return d
	}
	shadow := func(c rextheme.Color, shade float64) rextheme.Color {
		return c.Mix(lk.shadow, shade*lk.shadowAmount*s)
	}
	// covered reports whether the card hides cell (x, y) whole. Its lip
	// rows show the backdrop through the part of the row the lip leaves
	// clear; in flat mode the lips are solid.
	covered := func(x, y int) bool {
		if !card.Contains(x, y) {
			return false
		}
		return y > card.Y && y < card.Y+card.H-1 || sil.LipTop == 0 && y == card.Y || sil.LipBottom == 0 && y == card.Y+card.H-1
	}
	out := make([]byte, 0, g.w*g.h*4+g.h*64)
	for y := range g.h {
		if y > 0 {
			out = append(out, '\n')
		}
		var prev style
		styled := false
		for x := range g.w {
			if covered(x, y) {
				if styled {
					out = append(out, "\x1b[m"...)
					styled = false
				}
				out = append(out, ' ')
				continue
			}
			c := g.at(x, y)
			content := c.content
			switch {
			case c.width == 0 && !covered(x-1, y):
				continue // drawn by the wide glyph on its left
			case c.width == 0:
				content = " " // the card covers its left half
			case c.width == 2 && covered(x+1, y):
				content = " " // the card covers its right half
			}
			st := style{fg: dim(fgs, c.fg, DimText), bg: dim(bgs, c.bg, DimFill), attrs: c.attrs}
			switch {
			case card.Contains(x, y):
				// A lip cell: the shade at the centre of its clear part,
				// the contact shadow right at the card's edge.
				sy := float64(card.Y) + sil.LipTop/2
				if y != card.Y {
					sy = float64(card.Y+card.H) - sil.LipBottom/2
				}
				shade := ui.ShadeAt(float64(x)+0.5, sy, sil)
				if y == card.Y {
					shade = max(shade, lk.contact)
				}
				st.fg, st.bg = shadow(st.fg, shade), shadow(st.bg, shade)
			default:
				blank := content == " " && c.attrs&(attrUnderline|attrStrike) == 0
				if sh, ok := ui.ShadeCell(x, y, sil, blank); ok {
					base := st.bg
					if sh.Glyph != "" {
						content, st.attrs = sh.Glyph, 0
						st.fg = shadow(base, sh.Fg)
					} else {
						st.fg = shadow(st.fg, sh.Fg)
					}
					st.bg = shadow(base, sh.Bg)
				}
			}
			if e := edgeGlyph(x, y, card); e != "" && lk.edge != (rextheme.Color{}) && c.width == 1 && s > 0 {
				content, st.attrs = e, 0
				st.fg = st.bg.Mix(lk.edge, s)
			}
			if !styled || st != prev {
				out = st.appendSGR(out)
				prev, styled = st, true
			}
			out = append(out, content...)
		}
		if styled {
			out = append(out, "\x1b[m"...)
		}
	}
	return string(out)
}

// style is what one SGR sequence sets.
type style struct {
	fg, bg rextheme.Color
	attrs  uint8
}

// appendSGR appends the sequence that sets s from scratch.
func (s style) appendSGR(out []byte) []byte {
	out = append(out, "\x1b[0"...)
	for _, a := range [...]struct {
		bit  uint8
		code string
	}{{attrBold, ";1"}, {attrItalic, ";3"}, {attrUnderline, ";4"}, {attrStrike, ";9"}} {
		if s.attrs&a.bit != 0 {
			out = append(out, a.code...)
		}
	}
	out = appendRGB(append(out, ";38;2"...), s.fg)
	out = appendRGB(append(out, ";48;2"...), s.bg)
	return append(out, 'm')
}

func appendRGB(out []byte, c rextheme.Color) []byte {
	for _, v := range [...]uint8{c.R, c.G, c.B} {
		out = strconv.AppendUint(append(out, ';'), uint64(v), 10)
	}
	return out
}
