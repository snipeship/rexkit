package backdrop

import (
	"math"
	"time"

	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

// How far the backdrop recedes at full strength. One strength from 0 (the
// panes as they are) to 1 (fully dimmed) drives every amount, so a fade
// only has to interpolate it.
const (
	// DimText and DimFill are how far text and backgrounds move towards
	// the scrim. Text moves further, so it loses contrast and recedes.
	DimText = 0.68
	DimFill = 0.55
	// Desaturation takes colour out (towards grey of the same brightness),
	// so syntax colours and prompts stop competing with the card.
	Desaturation = 0.45

	// The scrim is the theme's canvas moved towards black in a dark theme
	// (a deeper night) and towards a dark grey in a light one (white turns
	// to a calm mid grey instead of a muddy black).
	ScrimDark  = 0.55
	ScrimLight = 0.42

	// ShadowDark and ShadowLight are the shadow's peak strength (its
	// shape is ui.ShadeAt's): a dark theme's shadow is deep, a light
	// theme's soft.
	ShadowDark  = ui.ShadowDark
	ShadowLight = ui.ShadowLight

	// EdgeDark and EdgeLight are how far the hairline beside the card's
	// straight sides moves from the card's colour towards the shadow's ink.
	// A dark theme's dim sets the card apart by itself: no hairline.
	EdgeDark  = 0
	EdgeLight = 0.30
	// ContactLight is the least shadow a light theme puts in the clear
	// part of the card's top lip: a soft line where the card begins.
	ContactLight = ui.ContactLight
)

// Fade-in: after the backdrop is loaded, the stage draws FadeFrames frames
// over FadeDuration, at strengths Fade(1) … Fade(FadeFrames) = 1.
const (
	FadeDuration = 140 * time.Millisecond
	FadeFrames   = 5
)

// Fade is the strength of frame i of the fade-in: an ease-out (fast first,
// settling softly) from Fade(0) = 0 to Fade(FadeFrames) = 1.
func Fade(i int) float64 {
	t := math.Max(0, math.Min(1, float64(i)/FadeFrames))
	return 1 - math.Pow(1-t, 3)
}

// look is what the backdrop blends towards, from the theme.
type look struct {
	scrim, shadow rextheme.Color
	shadowAmount  float64
	edge          rextheme.Color // the hairline beside the card's sides; zero for none
	contact       float64        // the least shade in the top lip's clear part
}

func lookFor(t rextheme.Theme) look {
	pal := ui.FromRex(t).C
	card, ink := rextheme.Of(pal.Surface), rextheme.Of(pal.Shadow)
	if t.Dark {
		lk := look{
			scrim:        t.UI.Canvas.Mix(rextheme.Color{}, ScrimDark),
			shadow:       ink,
			shadowAmount: ShadowDark,
		}
		if EdgeDark > 0 {
			lk.edge = card.Mix(t.UI.Content, EdgeDark)
		}
		return lk
	}
	return look{
		scrim:        t.UI.Canvas.Mix(ink, ScrimLight),
		shadow:       ink,
		shadowAmount: ShadowLight,
		edge:         card.Mix(ink, EdgeLight),
		contact:      ContactLight,
	}
}

// edgeGlyph is the hairline on cell (x, y) beside a card's straight sides:
// ▕ left of the card, ▏ right of it, on the rows between its lips.
func edgeGlyph(x, y int, card ui.Rect) string {
	if card.Empty() || y <= card.Y || y >= card.Y+card.H-1 {
		return ""
	}
	switch x {
	case card.X - 1:
		return "▕"
	case card.X + card.W:
		return "▏"
	}
	return ""
}
