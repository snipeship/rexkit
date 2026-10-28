package panes

import (
	"image"
	"math"

	"rexkit/internal/rex"
)

// Where Rex draws panes and floats in a window, in pixels on a Retina
// screen, measured on screenshots of Rex Beta 0.1.0 (1541) (a 14×28 px
// cell). Panes and floats share it: a float at a pane's rect holds the
// pane's cells.
//
//   - Below the tab bar, the window keeps Margin on its other three sides:
//     the rest is the layout area, which rects (fractions) are of.
//   - Panes side by side are Gap apart: each loses half of it at an edge
//     that is not the layout area's. A float does not.
//   - A pane or float is a rounded surface with a header (its title)
//     across its top: its cells start PadLeft in and Header down, and take
//     all but PadX of its width and PadY of its height.
const (
	TabBar  = 92
	Margin  = 16
	Gap     = 16
	PadLeft = 20
	Header  = 76
	PadX    = 40
	PadY    = 91
	Radius  = 20
)

// Layout is a window's layout area, in pixels.
type Layout struct{ W, H int }

// LayoutFor is the layout area of a window whose one pane holds cols×rows
// cells of cw×ch pixels.
func LayoutFor(cols, rows, cw, ch int) Layout {
	return Layout{W: cols*cw + PadX, H: rows*ch + PadY}
}

// Pane is where a tiled pane with rect r is in the layout area: it loses
// half a gap at each edge inside the area.
func (l Layout) Pane(r rex.Rect) image.Rectangle {
	f := l.Float(r)
	if r[0] > 0 {
		f.Min.X += Gap / 2
	}
	if r[1] > 0 {
		f.Min.Y += Gap / 2
	}
	if r[0]+r[2] < 1 {
		f.Max.X -= Gap / 2
	}
	if r[1]+r[3] < 1 {
		f.Max.Y -= Gap / 2
	}
	return f
}

// Float is where a float with rect r is in the layout area.
func (l Layout) Float(r rex.Rect) image.Rectangle {
	x0, y0 := px(r[0], l.W), px(r[1], l.H)
	return image.Rect(x0, y0, px(r[0]+r[2], l.W), px(r[1]+r[3], l.H))
}

func px(f float64, n int) int { return int(math.Round(f * float64(n))) }

// Content is how many cw×ch cells a pane or float at r holds, and where the
// first one is.
func Content(r image.Rectangle, cw, ch int) (cols, rows int, at image.Point) {
	cols = max((r.Dx()-PadX)/cw, 1)
	rows = max((r.Dy()-PadY)/ch, 1)
	return cols, rows, r.Min.Add(image.Pt(PadLeft, Header))
}

// Size is the size of a float that holds cols×rows cells of cw×ch pixels.
func Size(cols, rows, cw, ch int) image.Point {
	return image.Pt(cols*cw+PadX, rows*ch+PadY)
}
