package snap

import (
	"image/color"

	"rexkit/internal/rextheme"
)

// Palette is a terminal's colours: what cells without a colour of their own
// get, its cursor's colour, and the 256 indexed colours (the 16 ANSI
// colours first).
type Palette struct {
	Fg, Bg  color.RGBA
	Cursor  color.RGBA
	Indexed [256]color.RGBA
}

// PaletteOf is the palette of a Rex theme's terminal, with Rex's own
// 256-colour palette (not the xterm cube).
func PaletteOf(t rextheme.Terminal) Palette {
	p := Palette{Fg: rgba(t.Foreground), Bg: rgba(t.Background), Cursor: rgba(t.Cursor)}
	for i := range p.Indexed {
		p.Indexed[i] = rgba(t.Indexed(i))
	}
	return p
}

// Theme is the theme snapshots draw in: Rex's default dark or light theme
// (Merino) as the installed app defines it, or rexkit's built-in copy.
func Theme(dark bool) rextheme.Theme {
	id := rextheme.DefaultDark
	if !dark {
		id = rextheme.DefaultLight
	}
	t, _ := rextheme.Named(rextheme.DefaultPaths(), id)
	return t
}

// DefaultPalette is the terminal palette of Theme(dark).
func DefaultPalette(dark bool) Palette { return PaletteOf(Theme(dark).Terminal) }

func rgba(c rextheme.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255} }
