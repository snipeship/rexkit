package ui

import "testing"

func TestShadow(t *testing.T) {
	card := Rect{X: 10, Y: 5, W: 20, H: 10}
	sil := CardSilhouette(card)
	at := func(x, y int) float64 {
		s, _ := ShadeCell(x, y, sil, false)
		return s.Fg
	}
	// Little climbs over the top: nothing beyond the row above the card.
	for x := card.X; x < card.X+card.W; x++ {
		if s := at(x, card.Y-2); s != 0 {
			t.Errorf("shade %v two rows above the card at column %d", s, x)
		}
	}
	// Under the card it is deepest and falls off row by row.
	mid := card.X + card.W/2
	prev := 2.0
	for y := card.Y + card.H; y < card.Y+card.H+6; y++ {
		s := at(mid, y)
		if s > prev || s < 0 {
			t.Errorf("row %d: shade %v after %v (not monotone)", y, s, prev)
		}
		prev = s
	}
	if at(mid, card.Y+card.H) < 0.9 {
		t.Errorf("the row under the card: %v", at(mid, card.Y+card.H))
	}
	if at(mid, card.Y+card.H+5) != 0 {
		t.Error("the shadow never ends")
	}
	// A rounded corner: the diagonal cell is lighter than the cells beside
	// it straight below and straight right.
	corner := at(card.X+card.W, card.Y+card.H)
	if below, right := at(card.X+card.W-1, card.Y+card.H), at(card.X+card.W, card.Y+card.H-1); corner >= below || corner >= right {
		t.Errorf("square corner: %v at the corner, %v below, %v right", corner, below, right)
	}
	if at(0, 0) != 0 {
		t.Error("far away is in the shadow")
	}
	if _, ok := ShadeCell(5, 5, Silhouette{}, true); ok {
		t.Error("no card, no shadow")
	}
}

// A blank cell where the shadow changes fast draws its halves apart.
func TestShadowSplitsBlankCells(t *testing.T) {
	card := Rect{X: 10, Y: 5, W: 20, H: 10}
	sil := CardSilhouette(card)
	if s, _ := ShadeCell(20, card.Y+card.H+1, sil, true); s.Glyph != "▀" || s.Fg <= s.Bg {
		t.Errorf("below the card: %+v, want ▀ darker on top", s)
	}
	if s, _ := ShadeCell(card.X+card.W+1, 10, sil, true); s.Glyph != "▐" {
		t.Errorf("beside the card: %+v, want a split", s)
	}
	if s, _ := ShadeCell(20, card.Y+card.H+1, sil, false); s.Glyph != "" {
		t.Errorf("a cell with text was split: %+v", s)
	}
}

// Every shape mode's silhouette casts a shadow that starts under the card,
// and a flat card's has square corners.
func TestShadowModes(t *testing.T) {
	defer SetShapes(Shapes())
	card := Rect{X: 10, Y: 5, W: 20, H: 10}
	for _, m := range []ShapeMode{ShapesSprite, ShapesQuadrant, ShapesFlat} {
		SetShapes(m)
		sil := CardSilhouette(card)
		if sil.LipTop != m.Lip() {
			t.Errorf("%v: lip %v", m, sil.LipTop)
		}
		if s := ShadeAt(20.5, float64(card.Y+card.H)+0.5, sil); s < 0.9 {
			t.Errorf("%v: %v under the card", m, s)
		}
	}
}
