package rextheme

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// keyCustomThemes holds the themes the user made in Rex (one imported from a
// Ghostty config, say): a JSON array in a data value. Their IDs start with
// customPrefix.
const (
	keyCustomThemes = "Workspace.customThemes"
	customPrefix    = "custom-"
)

// customTheme is one entry of Workspace.customThemes; only the fields rexkit
// reads. Unlike a bundled theme it has no semantic tokens: Rex generates
// them, and derive does the same.
type customTheme struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Appearance string `json:"appearance"`
	Palette    struct {
		Background  tokenColor            `json:"background"`
		Foreground  tokenColor            `json:"foreground"`
		Cursor      tokenColor            `json:"cursor"`
		ANSI        []tokenColor          `json:"ansi"`
		Generate    bool                  `json:"paletteGenerate"`
		PaletteBase map[string]tokenColor `json:"paletteBase"`
	} `json:"palette"`
}

// custom resolves custom theme id from Rex's preferences.
func custom(p Paths, id string) (Theme, error) {
	data, err := os.ReadFile(p.Prefs)
	if err != nil {
		return Theme{}, err
	}
	prefs, err := plistStrings(data, keyCustomThemes)
	if err != nil {
		return Theme{}, err
	}
	var themes []customTheme
	if err := json.Unmarshal([]byte(prefs[keyCustomThemes]), &themes); err != nil {
		return Theme{}, fmt.Errorf("rextheme: %s: %w", keyCustomThemes, err)
	}
	for _, c := range themes {
		if c.ID == id {
			return c.resolve()
		}
	}
	return Theme{}, fmt.Errorf("%w: %s", ErrNoTheme, id)
}

// resolve turns the custom theme into a bundled theme's shape (its terminal
// colours as the source, a 256-colour palette, derived semantic tokens) and
// resolves that.
func (c customTheme) resolve() (Theme, error) {
	pal := c.Palette
	if len(pal.ANSI) != 16 {
		return Theme{}, fmt.Errorf("rextheme: theme %s has %d ANSI colours, want 16", c.ID, len(pal.ANSI))
	}
	r := rawTheme{ID: c.ID, Name: c.Name, Appearance: c.Appearance}
	bg, fg, cursor := pal.Background.rgba().Color, pal.Foreground.rgba().Color, pal.Cursor.rgba().Color
	var ansi [16]Color
	for i, a := range pal.ANSI {
		ansi[i] = a.rgba().Color
		r.Source.ANSI = append(r.Source.ANSI, ansi[i].Hex())
	}
	r.Source.Background, r.Source.Foreground, r.Source.Cursor = bg.Hex(), fg.Hex(), cursor.Hex()
	// Rex draws 16–255 from paletteBase unless it generates them; rexkit
	// cannot generate them as Rex does, so those stay the xterm cube.
	if !pal.Generate {
		for i, c := range xterm256(ansi) {
			if base, ok := pal.PaletteBase[strconv.Itoa(i)]; ok && i >= 16 {
				c = base.rgba().Color
			}
			r.Palette = append(r.Palette, token(c, 1))
		}
	}
	r.Semantic = derive(c.Appearance != "light", bg, fg, cursor, ansi)
	return r.resolve()
}

// Derivation rules: what Rex's generated themes (the bundled ones with
// paletteGenerate, all but Merino and Silver Point) hold, fitted to them.
// Surfaces move the background towards the colour named by a fixed share;
// text tiers and strokes move it towards the foreground until they reach a
// contrast on the surface. TestDeriveBundled holds how close that comes.
var (
	derivedDark = derivation{
		surface: 0.021, subtle: 0.036, raised: 0.086, onOverlay: 0.105, // towards the foreground
		secondary: 5.15, tertiary: 3.44, stroke: 1.95, strokeSubtle: 1.43,
		container: 0.138,
		dim:       0.106, dimSubtle: 0.055, ring: 0.148,
		control: 0.020, hover: 0.042, pressed: 0.075,
	}
	derivedLight = derivation{
		surface: 0.23, raised: 0.37, // towards white
		subtle:    0.04, // towards black
		secondary: 5.0, tertiary: 3.34, stroke: 1.44, strokeSubtle: 1.17,
		container: 0.073,
		dim:       0.128, dimSubtle: 0.060, ring: 0.188,
		control: 0.021, hover: 0.047, pressed: 0.090,
	}
)

type derivation struct {
	surface, subtle, raised, onOverlay float64 // shares
	secondary, tertiary                float64 // contrasts on the surface
	stroke, strokeSubtle               float64 // contrasts on the surface
	container                          float64 // a status or accent container: the background towards the colour
	dim, dimSubtle, ring               float64 // alphas of the accent
	control, hover, pressed            float64 // alphas of the foreground
}

// readableContrast is the WCAG contrast Rex's generated themes give accent
// and status text on the surface: a colour below it is moved towards the
// foreground until it reaches it (their solids keep the colour as is).
const readableContrast = 5.1

// solidContrast is the contrast of white text on a light theme's accent
// fill.
const solidContrast = 4.5

// neutralChroma is how grey a cursor can be and still be the accent; a
// greyer one (white, say) gives way to ANSI blue, as in Whitby Bay.
const neutralChroma = 0.08

// derive returns the semantic tokens Rex generates for a theme with these
// terminal colours.
func derive(dark bool, bg, fg, cursor Color, ansi [16]Color) map[string]tokenColor {
	d, white, black := derivedLight, Color{255, 255, 255}, Color{}
	surface, raised := bg.Mix(white, d.surface), bg.Mix(white, d.raised)
	subtle, onOverlay := bg.Mix(black, d.subtle), bg
	if dark {
		d = derivedDark
		surface, raised = bg.Mix(fg, d.surface), bg.Mix(fg, d.raised)
		subtle, onOverlay = bg.Mix(fg, d.subtle), bg.Mix(fg, d.onOverlay)
	}
	away := black // where text moves to read on the surface
	if dark {
		away = white
	}
	// Text on a solid fill: the background on a dark theme; white or black
	// on a light one, whichever reads better (white on the accent).
	onSolid := func(solid Color) Color {
		switch {
		case dark:
			return bg
		case contrast(white, solid) >= contrast(black, solid):
			return white
		}
		return black
	}

	m := map[string]tokenColor{
		"canvas":             token(bg, 1),
		"surface":            token(surface, 1),
		"terminalBackground": token(surface, 1),
		"surfaceSubtle":      token(subtle, 1),
		"surfaceRaised":      token(raised, 1),
		"controlOnOverlay":   token(onOverlay, 1),
		"stroke":             token(towards(bg, fg, surface, d.stroke), 1),
		"strokeSubtle":       token(towards(bg, fg, surface, d.strokeSubtle), 1),
		"content":            token(fg, 1),
		"contentSecondary":   token(towards(bg, fg, surface, d.secondary), 1),
		"contentTertiary":    token(towards(bg, fg, surface, d.tertiary), 1),
		"control":            token(fg, d.control),
		"controlHover":       token(fg, d.hover),
		"controlPressed":     token(fg, d.pressed),
	}
	set := func(name string, base Color) {
		text := towards(base, away, surface, readableContrast)
		m[name] = token(text, 1)
		m[name+"Solid"] = token(base, 1)
		m[name+"OnSolid"] = token(onSolid(base), 1)
		m[name+"Container"] = token(bg.Mix(text, d.container), 1)
		m[name+"OnContainer"] = token(text, 1)
		m[name+"Border"] = token(text, 1)
	}
	accent := cursor
	if chroma(cursor) < neutralChroma {
		accent = ansi[4]
	}
	set("accent", accent)
	if !dark { // white text on the accent's fill, so the fill darkens until it reads
		m["accentSolid"] = token(towards(accent, black, white, solidContrast), 1)
		m["accentOnSolid"] = token(white, 1)
	}
	text := m["accent"].rgba().Color
	m["accentDim"] = token(text, d.dim)
	m["accentDimSubtle"] = token(text, d.dimSubtle)
	m["accentRingOuter"] = token(text, d.ring)
	m["selection"] = m["accentSolid"]
	m["selectionForeground"] = m["accentOnSolid"]
	set("danger", ansi[1])
	set("success", ansi[2])
	set("warning", ansi[3])
	set("info", ansi[4])
	return m
}

// towards moves c towards to until it has contrast least on surface.
func towards(c, to, surface Color, least float64) Color {
	for t := 0.0; t <= 1; t += 0.01 {
		if m := c.Mix(to, t); contrast(m, surface) >= least {
			return m
		}
	}
	return to
}

// contrast is the WCAG contrast ratio of two colours (1 to 21).
func contrast(a, b Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is WCAG relative luminance.
func luminance(c Color) float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// chroma is how far from grey c is: its largest component minus its
// smallest, from 0 to 1.
func chroma(c Color) float64 {
	return float64(max(c.R, c.G, c.B)-min(c.R, c.G, c.B)) / 255
}

// opaque is c with alpha a, as themes.json stores it.
func token(c Color, a float64) tokenColor {
	return tokenColor{Red: float64(c.R) / 255, Green: float64(c.G) / 255, Blue: float64(c.B) / 255, Alpha: a}
}

// isCustom reports whether id names one of the user's own themes.
func isCustom(id string) bool { return strings.HasPrefix(id, customPrefix) }
