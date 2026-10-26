package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Rect is a cell rectangle, used for layout areas and mouse hit testing.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Offset moves r by (dx, dy).
func (r Rect) Offset(dx, dy int) Rect { return Rect{r.X + dx, r.Y + dy, r.W, r.H} }

// Empty reports whether r has no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Frame is how a tool's view sits in its float. On a card it
// draws the card's silhouette: a lip row above and below (the Card profile:
// the card's visible edge a third of a row inside its cells, with cut
// corners), and a column of plain card at each side. Without a card the
// content fills the whole window, flat. The title lives in the header and
// the meta in the tray: the frame carries no text of its own.
type Frame struct {
	// Card draws the silhouette; without it the content fills every cell.
	Card bool
	// Banded is the full chrome: a header band (HeaderBand) and a tray with
	// a lip (Tray), whose colour the card's bottom lip takes. Without it the
	// chrome is compact: one header row and one tray row.
	Banded bool
	// Fill is the card's colour (default the theme's Surface) and On what
	// it sits on (default Clear: the window behind the card).
	Fill, On color.Color
}

// FloatFrame is the frame of a resizable tool (Go to, Files, Keys) at w×h:
// on a card when the stage draws it in one (popup.CardMsg), banded when its
// content is tall and wide enough (Banded). Every resizable tool uses it, so
// the same size looks the same in every tool.
func FloatFrame(w, h int, carded bool) Frame {
	f := Frame{Card: carded && w >= 4 && h >= 3}
	in := f.Inner(w, h)
	f.Banded = Banded(in.W, in.H)
	return f
}

// DialogRows is the height of a dialog with its full chrome (Rename: title,
// field with its edge rows, helper, tray with its lip).
const DialogRows = 9

// DialogFrame is the frame of a fixed-size dialog (Rename) at w×h: always
// a card, banded at DialogRows rows and more.
func DialogFrame(w, h int) Frame {
	return Frame{Card: w >= 4 && h >= 3, Banded: h >= DialogRows}
}

// Inner returns the content area of a w×h frame, relative to the frame: on
// a card, everything between the lips and the side columns.
func (f Frame) Inner(w, h int) Rect {
	if !f.Card || w < 4 || h < 3 {
		return Rect{0, 0, max(0, w), max(0, h)}
	}
	return Rect{1, 1, w - 2, h - 2}
}

func (f Frame) colours(t Theme) (fill, on color.Color) {
	fill, on = f.Fill, f.On
	if fill == nil {
		fill = t.C.Surface
	}
	if on == nil {
		on = Clear
	}
	return fill, on
}

// Render draws a w×h view: header lines at the top of the content area,
// tray lines at its bottom and body lines between them (missing lines are
// blank; extra lines and overlong lines are cut), inside the card's
// silhouette when there is one. The result is exactly h lines of w cells,
// every cell with a background (Paint).
func (f Frame) Render(t Theme, w, h int, header, body, tray []string) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	in := f.Inner(w, h)
	fill, on := f.colours(t)
	header = header[:min(len(header), in.H)]
	tray = tray[:min(len(tray), in.H-len(header))]
	bodyH := in.H - len(header) - len(tray)
	content := make([]string, 0, in.H)
	content = append(content, header...)
	for y := range bodyH {
		line := ""
		if y < len(body) {
			line = body[y]
		}
		content = append(content, line)
	}
	content = append(content, tray...)

	carded := in.X > 0
	trayed := f.Banded && len(tray) > 0
	lines := make([]string, 0, h)
	if carded {
		lines = append(lines, EdgeRow(w, true, on, Shape{ProfileCard, 0, []Seg{{w, fill}}}))
	}
	side := lipgloss.NewStyle().Background(fill).Render(" ")
	traySide := lipgloss.NewStyle().Background(t.C.Tray).Render(" ")
	trayLip := straightEdge(1, true, t.C.Tray, fill)
	for y, line := range content {
		line = Paint(Fit(line, in.W), fill)
		if carded {
			s := side
			switch {
			case trayed && y == in.H-len(tray):
				s = trayLip
			case trayed && y > in.H-len(tray):
				s = traySide
			}
			line = s + line + s
		}
		lines = append(lines, line)
	}
	if carded {
		bottom := fill
		if trayed {
			bottom = t.C.Tray
		}
		lines = append(lines, EdgeRow(w, false, on, Shape{ProfileCard, 0, []Seg{{w, bottom}}}))
	}
	return strings.Join(lines, "\n")
}

// Edged frames a card that sits straight on a page of colour on (the hold
// screen, the scrollback loader's error) with a column each side: in a
// light theme a hairline in the Stroke colour beside the card's straight
// sides, which a near-white card on a near-white page
// needs for an edge; in a dark one plain page. card lines must be equally
// wide; the result is two cells wider.
func (t Theme) Edged(card []string, on color.Color) []string {
	page := lipgloss.NewStyle().Background(on)
	left, right := page.Render(" "), page.Render(" ")
	out := make([]string, len(card))
	for i, line := range card {
		l, r := left, right
		if !t.Dark && i > 0 && i < len(card)-1 {
			edge := page.Foreground(t.C.Stroke)
			l, r = edge.Render("▕"), edge.Render("▏")
		}
		out[i] = l + line + r
	}
	return out
}
