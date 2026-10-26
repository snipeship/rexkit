package ui

import "math"

// Shadow shape. A card's shadow is its visible silhouette
// moved a row down (light from above), blurred: a Gaussian of the distance
// to it, measured in rows. There is one implementation, ShadeAt: the
// backdrop shades the panes around the card with it, and the key panel the
// card around itself.
const (
	ShadowBlur  = 1.4  // sigma, in rows
	ColAspect   = 0.48 // a column's width in rows (14×29 px cells)
	ShadowAbove = 1.6  // distances above the silhouette count this much more
	shadowDY    = 1.0  // the silhouette's offset down, in rows
	shadowFloor = 0.04 // shade under this is none
)

// Peak strength of a shadow: how far a fully shaded cell moves towards the
// palette's Shadow colour. A dark theme's shadow is deep; a light theme's
// soft, so it lifts the card without a dirty slab.
const (
	ShadowDark  = 0.55
	ShadowLight = 0.14
)

// ContactLight is the least shadow a light theme puts in the clear part of
// a raised shape's top lip: a soft line where it begins,
// which a near-white shape on a near-white ground needs for a top edge.
const ContactLight = 0.5

// ShadowPeak is the peak strength of a shadow in a dark or light theme.
func ShadowPeak(dark bool) float64 {
	if dark {
		return ShadowDark
	}
	return ShadowLight
}

// Corner radii of a silhouette, in rows: a card's wedge cuts a whole cell,
// a control's half of one.
const (
	cardRadius    = 0.5
	controlRadius = 0.3
)

// Silhouette is the visible outline of a shape that casts a shadow: its cell
// rect, the part of the top and bottom rows its lips leave clear (in rows),
// and how its corners are cut.
type Silhouette struct {
	Rect              Rect
	LipTop, LipBottom float64
	Corner            Profile
}

// CardSilhouette is the silhouette of a card drawn in r in the current shape
// mode.
func CardSilhouette(r Rect) Silhouette {
	lip := Shapes().Lip()
	return Silhouette{Rect: r, LipTop: lip, LipBottom: lip, Corner: ProfileCard}
}

// ShadeAt is how much shadow sil casts on the point (x, y), in cells (the
// centre of cell (c, r) is c+0.5, r+0.5), from 0 to 1. Points the shadow
// covers fully get 1, including points under the shape itself; the caller
// decides which cells show it.
func ShadeAt(x, y float64, sil Silhouette) float64 {
	r := sil.Rect
	if r.Empty() {
		return 0
	}
	top := float64(r.Y) + sil.LipTop + shadowDY
	bottom := float64(r.Y+r.H) - sil.LipBottom + shadowDY
	if y < top {
		y = top - (top-y)*ShadowAbove // light from above: little climbs over the top
	}
	radius := 0.0
	if Shapes() != ShapesFlat {
		switch sil.Corner {
		case ProfileCard:
			radius = cardRadius
		case ProfileControl:
			radius = controlRadius
		}
	}
	// The signed distance to a rounded box, in rows.
	hx := float64(r.W) * ColAspect / 2
	hy := (bottom - top) / 2
	radius = min(radius, hx, hy)
	qx := math.Abs(x*ColAspect-(float64(r.X)*ColAspect+hx)) - hx + radius
	qy := math.Abs(y-(top+hy)) - hy + radius
	d := math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - radius
	if d <= 0 {
		return 1
	}
	s := math.Exp(-d * d / (2 * ShadowBlur * ShadowBlur))
	if s < shadowFloor {
		return 0
	}
	return s
}

// splitMin is the least difference in shade between a blank cell's halves
// that draws them apart.
const splitMin = 0.02

// CellShade is the shadow on one cell: Fg and Bg, from 0 to 1, for its
// foreground and background. With Glyph set the cell is drawn as that half
// block, its foreground half shaded Fg and the rest Bg; without it the
// cell keeps its content and Fg equals Bg.
type CellShade struct {
	Glyph  string
	Fg, Bg float64
}

// ShadeCell samples sil's shadow on cell (x, y) at the centres of its four
// halves, and reports whether there is any. A blank cell
// is split along the axis whose halves differ more, ▀ (the upper half in
// the foreground) or ▐ (the right half), so the falloff gets twice the
// resolution where it is steepest; a cell with text gets the shade at its
// centre.
func ShadeCell(x, y int, sil Silhouette, blank bool) (CellShade, bool) {
	cx, cy := float64(x)+0.5, float64(y)+0.5
	mid := ShadeAt(cx, cy, sil)
	up, down := ShadeAt(cx, cy-0.25, sil), ShadeAt(cx, cy+0.25, sil)
	left, right := ShadeAt(cx-0.25, cy, sil), ShadeAt(cx+0.25, cy, sil)
	if max(mid, up, down, left, right) == 0 {
		return CellShade{}, false
	}
	dv, dh := math.Abs(up-down), math.Abs(left-right)
	switch {
	case !blank || max(dv, dh) < splitMin:
		return CellShade{Fg: mid, Bg: mid}, true
	case dv >= dh:
		return CellShade{Glyph: "▀", Fg: up, Bg: down}, true
	}
	return CellShade{Glyph: "▐", Fg: right, Bg: left}, true
}
