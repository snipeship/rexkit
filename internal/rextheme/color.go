package rextheme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// Color is an opaque sRGB colour. It implements color.Color, so it can be
// handed to Lip Gloss as is.
type Color struct{ R, G, B uint8 }

// RGBA implements color.Color.
func (c Color) RGBA() (r, g, b, a uint32) {
	return uint32(c.R) * 0x101, uint32(c.G) * 0x101, uint32(c.B) * 0x101, 0xffff
}

// Hex returns the colour as #rrggbb.
func (c Color) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// String implements fmt.Stringer.
func (c Color) String() string { return c.Hex() }

// MarshalText writes the colour as #rrggbb.
func (c Color) MarshalText() ([]byte, error) { return []byte(c.Hex()), nil }

// UnmarshalText reads #rrggbb.
func (c *Color) UnmarshalText(b []byte) error {
	v, err := ParseHex(string(b))
	*c = v
	return err
}

// Of converts any colour to an opaque Color, dropping its alpha.
func Of(c color.Color) Color {
	if c, ok := c.(Color); ok {
		return c
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return Color{}
	}
	// Un-premultiply: color.Color values are alpha-premultiplied.
	return Color{uint8(r * 0xff / a), uint8(g * 0xff / a), uint8(b * 0xff / a)}
}

// ParseHex reads #rgb or #rrggbb.
func ParseHex(s string) (Color, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return Color{}, fmt.Errorf("rextheme: %q is not a #rrggbb colour", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Color{}, fmt.Errorf("rextheme: %q is not a #rrggbb colour", s)
	}
	return Color{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

// Mix returns c moved t of the way (0..1) towards o, in sRGB.
func (c Color) Mix(o Color, t float64) Color {
	if t <= 0 {
		return c
	}
	if t >= 1 {
		return o
	}
	return Color{mix(c.R, o.R, t), mix(c.G, o.G, t), mix(c.B, o.B, t)}
}

func mix(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

// Luma is the colour's perceived brightness from 0 to 1 (Rec. 709 weights on
// the sRGB values; good enough to order and grey colours, not to measure
// contrast).
func (c Color) Luma() float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// Desaturate moves c t of the way towards the grey of the same luma.
func (c Color) Desaturate(t float64) Color {
	y := uint8(math.Round(c.Luma() * 255))
	return c.Mix(Color{y, y, y}, t)
}

// RGBA is a colour with straight (not premultiplied) alpha, as Rex's design
// tokens store translucent colours such as accentDim.
type RGBA struct {
	Color Color   `json:"color"`
	A     float64 `json:"a"` // 0 transparent … 1 opaque
}

// Over composites the colour onto an opaque background: what the token
// looks like on the surface it sits on.
func (c RGBA) Over(bg Color) Color { return bg.Mix(c.Color, c.A) }

// tokenColor is a colour as Rex's themes.json stores it: sRGB components
// from 0 to 1.
type tokenColor struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

func (t tokenColor) rgba() RGBA {
	return RGBA{Color: Color{unit(t.Red), unit(t.Green), unit(t.Blue)}, A: math.Max(0, math.Min(1, t.Alpha))}
}

func unit(f float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, f)) * 255)) }
