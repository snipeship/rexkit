package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Every rounded thing in rexkit (the card, the field, a chip, the preview
// well, a keycap, the toast) is a filled shape drawn with block elements and
// the sextants and smooth-mosaic wedges of Unicode's Symbols for Legacy
// Computing, which Ghostty (and so Rex) draws as pixel-exact sprites, like
// box drawing. A one-row control becomes a shape two rows and a third tall:
// an edge row above (filled in its lower two thirds), the text row with a
// half block at each end, and an edge row below (its upper two thirds); the
// corners are cut on a diagonal.

// ShapeMode is the set of glyphs shapes are drawn with. Every mode keeps the
// same rows and columns, so layout and hit rects do not depend on it.
type ShapeMode int

// Shape modes.
const (
	// ShapesSprite draws two-thirds lips and diagonal corners: Ghostty's
	// sprites (Rex).
	ShapesSprite ShapeMode = iota
	// ShapesQuadrant draws half-row lips and quadrant corners, which every
	// font has.
	ShapesQuadrant
	// ShapesFlat draws no edges at all: edge rows are blank and a shape is
	// a plain fill (REXKIT_SHAPES=flat, NO_COLOR).
	ShapesFlat
)

// EnvShapes overrides the detected shape mode: sprite, quadrant or flat.
const EnvShapes = "REXKIT_SHAPES"

var shapeNames = [...]string{"sprite", "quadrant", "flat"}

func (m ShapeMode) String() string {
	if m < 0 || int(m) >= len(shapeNames) {
		return "sprite"
	}
	return shapeNames[m]
}

// shapeMode is the mode every shape is drawn in; tests and goldens use the
// default, sprites.
var shapeMode = ShapesSprite

// Shapes returns the mode shapes are drawn in.
func Shapes() ShapeMode { return shapeMode }

// SetShapes sets the mode shapes are drawn in. Call it once at start-up
// (with DetectShapes), before anything is drawn.
func SetShapes(m ShapeMode) { shapeMode = m }

// DetectShapes picks the shape mode for the terminal the environment
// describes: EnvShapes when it names a mode, flat under NO_COLOR, sprites
// in Rex ($REX_BLOCK) or Ghostty, and quadrants anywhere else.
func DetectShapes(getenv func(string) string) ShapeMode {
	for i, name := range shapeNames {
		if strings.EqualFold(strings.TrimSpace(getenv(EnvShapes)), name) {
			return ShapeMode(i)
		}
	}
	switch {
	case getenv("NO_COLOR") != "":
		return ShapesFlat
	case getenv("REX_BLOCK") != "", strings.EqualFold(getenv("TERM_PROGRAM"), "ghostty"):
		return ShapesSprite
	}
	return ShapesQuadrant
}

// Lip is how much of an edge row the mode leaves clear on the side away
// from the shape: a third for sprites, half for quadrants, none when flat.
func (m ShapeMode) Lip() float64 {
	switch m {
	case ShapesQuadrant:
		return 0.5
	case ShapesFlat:
		return 0
	}
	return 1.0 / 3
}

// Profile is how a shape's ends and corners are cut.
type Profile int

// Shape profiles.
const (
	// ProfileCard is the card, the dialog and the hold card: corners cut
	// across a whole cell, the sides full cells.
	ProfileCard Profile = iota
	// ProfileControl is the field, the active chip, the preview well, the
	// key panel and the toast: corners cut across half a cell, landing on
	// the half-block ends of the text rows.
	ProfileControl
	// ProfileTag is a one-row shape with no edge rows: keycaps and the
	// status tag.
	ProfileTag
)

// The glyphs of the shapes, by mode. A sprite card corner cuts a whole
// cell (14×19 px at Rex's size), which reads as a radius on the card; a
// control corner cuts half a cell, so a one-row control reads as a stadium
// and not an octagon.
const (
	edgeTop    = "🬹" // U+1FB39 BLOCK SEXTANT-3456: the lower two thirds
	edgeBottom = "🬎" // U+1FB0E BLOCK SEXTANT-1234: the upper two thirds

	cardTL = "🭊" // U+1FB4A lower right: lower left → upper middle right
	cardTR = "🬿" // U+1FB3F lower left: upper middle left → lower right
	cardBL = "🭥" // U+1FB65 upper right: upper left → lower middle right
	cardBR = "🭚" // U+1FB5A upper left: lower middle left → upper right

	controlTL = "🭉" // U+1FB49 lower right: lower centre → upper middle right
	controlTR = "🬾" // U+1FB3E lower left: upper middle left → lower centre
	controlBL = "🭤" // U+1FB64 upper right: upper centre → lower middle right
	controlBR = "🭙" // U+1FB59 upper left: lower middle left → upper centre

	endLeft  = "▐" // U+2590: a shape's text row starts in a cell's right half
	endRight = "▌" // U+258C: and ends in a cell's left half
)

// edgeGlyphs are the left corner, run and right corner of an edge row.
type edgeGlyphs struct{ left, run, right string }

// edges returns the glyphs of a top or bottom edge of a profile in mode m.
func (m ShapeMode) edges(p Profile, top bool) edgeGlyphs {
	switch {
	case m == ShapesFlat:
		return edgeGlyphs{" ", " ", " "}
	case m == ShapesQuadrant && top:
		return edgeGlyphs{"▗", "▄", "▖"}
	case m == ShapesQuadrant:
		return edgeGlyphs{"▝", "▀", "▘"}
	case p == ProfileCard && top:
		return edgeGlyphs{cardTL, edgeTop, cardTR}
	case p == ProfileCard:
		return edgeGlyphs{cardBL, edgeBottom, cardBR}
	case top:
		return edgeGlyphs{controlTL, edgeTop, controlTR}
	}
	return edgeGlyphs{controlBL, edgeBottom, controlBR}
}

// Seg is one stretch of a shape's fill: a shape can change colour along its
// length (the toast's tile) without inner corners.
type Seg struct {
	W    int
	Fill color.Color
}

// Shape is a rounded shape on a row: its profile, its first column, and its
// fill from left to right. Two shapes on one row must leave at least one
// cell between them.
type Shape struct {
	Profile Profile
	X       int
	Segs    []Seg
}

// ControlShape is a one-colour Control shape w cells wide at column x.
func ControlShape(x, w int, fill color.Color) Shape {
	return Shape{Profile: ProfileControl, X: x, Segs: []Seg{{w, fill}}}
}

// W is the shape's width.
func (s Shape) W() int {
	w := 0
	for _, g := range s.Segs {
		w += max(0, g.W)
	}
	return w
}

// EdgeRow draws the top (or bottom) edge row of shapes across w cells, over
// on: each shape's lip and corners in its fill, on elsewhere. The row above
// a shape's text row gets EdgeRow(top), the row below EdgeRow(bottom). Tag
// shapes have no edge rows and are skipped. In flat mode a card's edge rows
// are its fill (a plain rectangle); every other edge row is blank.
func EdgeRow(w int, top bool, on color.Color, shapes ...Shape) string {
	var line Line
	x := 0
	bg := lipgloss.NewStyle().Background(on)
	mode := Shapes()
	for _, s := range shapes {
		sw := s.W()
		if s.Profile == ProfileTag || sw <= 0 || s.X < x || s.X >= w {
			continue
		}
		line = append(line, Span{strings.Repeat(" ", s.X-x), bg})
		n := min(sw, w-s.X)
		g := mode.edges(s.Profile, top)
		at := 0
		for _, seg := range s.Segs {
			st := lipgloss.NewStyle().Foreground(seg.Fill).Background(on)
			switch {
			case mode == ShapesFlat && s.Profile == ProfileCard:
				st = lipgloss.NewStyle().Background(seg.Fill)
			case mode == ShapesFlat:
				st = bg
			}
			from, to := at, min(at+max(0, seg.W), n)
			var b strings.Builder
			for i := from; i < to; i++ {
				switch {
				case i == 0 && sw > 1:
					b.WriteString(g.left)
				case i == sw-1 && sw > 1:
					b.WriteString(g.right)
				default:
					b.WriteString(g.run)
				}
			}
			if b.Len() > 0 {
				line = append(line, Span{b.String(), st})
			}
			at = to
		}
		x = s.X + n
	}
	if x < w {
		line = append(line, Span{strings.Repeat(" ", w-x), bg})
	}
	return line.Render()
}

// straightEdge is an edge row of fill across w cells with no corners, over
// on: the lip of a surface that runs on past both sides (the tray).
func straightEdge(w int, top bool, fill, on color.Color) string {
	if w <= 0 {
		return ""
	}
	st := lipgloss.NewStyle().Foreground(fill).Background(on)
	if Shapes() == ShapesFlat {
		st = lipgloss.NewStyle().Background(on)
	}
	return st.Render(strings.Repeat(Shapes().edges(ProfileControl, top).run, w))
}

// Ends are the two cells that close a shape's text row in fill: half blocks
// in the fill (their other half shows what is around the shape), or, when
// flat, blank cells of the fill.
func Ends(fill color.Color) (left, right Span) {
	if Shapes() == ShapesFlat {
		st := lipgloss.NewStyle().Background(fill)
		return Span{" ", st}, Span{" ", st}
	}
	st := lipgloss.NewStyle().Foreground(fill)
	return Span{endLeft, st}, Span{endRight, st}
}

// Pill is a Tag shape: line on fill between the two ends, "▐ctrl /▌". It is
// line.Width()+2 cells wide.
func Pill(line Line, fill color.Color) Line {
	left, right := Ends(fill)
	return append(Line{left}.Append(OnBg(line, fill)...), right)
}

// Control is the text row of a Control shape: line on fill, with a cell of
// air inside each end, "▐ Search ▌". It is line.Width()+4 cells wide; draw
// its edge rows with EdgeRow and ControlShape(x, width, fill).
func Control(line Line, fill color.Color) Line {
	return Pill(Line{Space(1)}.Append(line...).Append(Space(1)), fill)
}

// OnBg puts every span of a line that has no background of its own on bg
// (a selection inside a field keeps its fill).
func OnBg(l Line, bg color.Color) Line {
	out := make(Line, len(l))
	for i, s := range l {
		out[i] = s
		if !hasBackground(s.Style) {
			out[i].Style = s.Style.Background(bg)
		}
	}
	return out
}
