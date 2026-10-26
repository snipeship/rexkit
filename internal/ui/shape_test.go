package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestEdgeRow(t *testing.T) {
	defer SetShapes(Shapes())
	fill := NewTheme(true).C.Field
	card := Shape{ProfileCard, 1, []Seg{{6, fill}}}
	control := ControlShape(1, 6, fill)
	tag := Shape{ProfileTag, 1, []Seg{{6, fill}}}
	tests := []struct {
		mode ShapeMode
		s    Shape
		top  bool
		want string
	}{
		{ShapesSprite, card, true, " 🭊🬹🬹🬹🬹🬿 "},
		{ShapesSprite, card, false, " 🭥🬎🬎🬎🬎🭚 "},
		{ShapesSprite, control, true, " 🭉🬹🬹🬹🬹🬾 "},
		{ShapesSprite, control, false, " 🭤🬎🬎🬎🬎🭙 "},
		{ShapesQuadrant, card, true, " ▗▄▄▄▄▖ "},
		{ShapesQuadrant, control, false, " ▝▀▀▀▀▘ "},
		{ShapesFlat, card, true, "        "},
		{ShapesFlat, control, true, "        "},
		{ShapesSprite, tag, true, "        "},
	}
	for _, tt := range tests {
		SetShapes(tt.mode)
		got := EdgeRow(8, tt.top, Clear, tt.s)
		if plain := ansi.Strip(got); plain != tt.want {
			t.Errorf("%v profile %d top=%v: %q, want %q", tt.mode, tt.s.Profile, tt.top, plain, tt.want)
		}
		if ansi.StringWidth(got) != 8 {
			t.Errorf("%v: %d cells", tt.mode, ansi.StringWidth(got))
		}
	}
	// A flat card's lips are its fill: a plain rectangle.
	SetShapes(ShapesFlat)
	if n := cellsWithBg(t, EdgeRow(8, true, Clear, card), fill); n != 6 {
		t.Errorf("flat card lip: %d cells of fill, want 6", n)
	}
	if n := cellsWithBg(t, EdgeRow(8, true, Clear, control), fill); n != 0 {
		t.Errorf("flat control edge: %d cells of fill, want none", n)
	}
}

// Each mode draws only glyphs its terminals have: sprites from Legacy
// Computing and block elements, quadrants from block elements alone.
func TestShapeGlyphSets(t *testing.T) {
	inBlocks := func(r rune) bool { return r >= 0x2580 && r <= 0x259f }
	inLegacy := func(r rune) bool { return r >= 0x1fb00 && r <= 0x1fb3b || r >= 0x1fb3c && r <= 0x1fb67 }
	for _, m := range []ShapeMode{ShapesSprite, ShapesQuadrant, ShapesFlat} {
		for _, p := range []Profile{ProfileCard, ProfileControl} {
			for _, top := range []bool{true, false} {
				g := m.edges(p, top)
				for _, r := range g.left + g.run + g.right {
					ok := r == ' '
					switch m {
					case ShapesSprite:
						ok = inLegacy(r)
					case ShapesQuadrant:
						ok = inBlocks(r)
					}
					if !ok {
						t.Errorf("%v profile %d: %q (%U) is not in the mode's set", m, p, r, r)
					}
				}
			}
		}
	}
}

func TestDetectShapes(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want ShapeMode
	}{
		{map[string]string{}, ShapesQuadrant},
		{map[string]string{"REX_BLOCK": "block:1"}, ShapesSprite},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, ShapesSprite},
		{map[string]string{"REX_BLOCK": "block:1", "NO_COLOR": "1"}, ShapesFlat},
		{map[string]string{"REX_BLOCK": "block:1", EnvShapes: "quadrant"}, ShapesQuadrant},
		{map[string]string{"NO_COLOR": "1", EnvShapes: " Sprite "}, ShapesSprite},
		{map[string]string{EnvShapes: "round"}, ShapesQuadrant},
	}
	for _, tt := range tests {
		if got := DetectShapes(func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("%v: %v, want %v", tt.env, got, tt.want)
		}
	}
}

// Shapes keep their rows and columns in every mode: only glyphs change.
func TestShapesKeepLayout(t *testing.T) {
	defer SetShapes(Shapes())
	th := NewTheme(true)
	for _, m := range []ShapeMode{ShapesSprite, ShapesQuadrant, ShapesFlat} {
		SetShapes(m)
		f := FloatFrame(60, 20, true)
		out := f.Render(th, 60, 20, []string{"head"}, []string{"body"}, th.Tray(Tray{Meta: Plain("3")}, 58, TrayRows))
		checkSize(t, m.String(), out, 60, 20)
		if line := th.Keycap("enter"); line.Width() != 7 {
			t.Errorf("%v: keycap %d cells", m, line.Width())
		}
		if !strings.Contains(ansi.Strip(out), "head") {
			t.Errorf("%v: header lost", m)
		}
	}
}
