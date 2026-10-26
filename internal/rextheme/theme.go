// Package rextheme reads the theme the user picked in Rex: the terminal
// palette Rex draws panes with and Rex's own semantic design tokens (canvas,
// surfaces, strokes, content, accent, status colours). It finds the user's
// choice in Rex's preferences, the theme in the installed app's design-token
// bundle, and falls back to built-in copies of Merino Dark and Merino Light,
// so it works without the app. Translucent tokens come pre-blended onto the
// surface they sit on.
package rextheme

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// Theme is one of Rex's themes, resolved to opaque colours.
type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dark bool   `json:"dark"`

	Terminal Terminal `json:"terminal"`
	UI       UI       `json:"ui"`
}

// Terminal is the palette Rex draws terminal panes with.
type Terminal struct {
	Background Color     `json:"background"`
	Foreground Color     `json:"foreground"`
	Cursor     Color     `json:"cursor"`
	ANSI       [16]Color `json:"ansi"`
	// Palette is the whole 256-colour palette. Rex generates entries 16–255
	// from the theme ("harmonious" palettes), so they are not the xterm
	// cube; entries 0–15 are ANSI.
	Palette [256]Color `json:"palette"`
}

// Indexed returns palette colour i (0–255).
func (t Terminal) Indexed(i int) Color {
	if i < 0 || i > 255 {
		return t.Foreground
	}
	return t.Palette[i]
}

// UI holds Rex's semantic design tokens. Translucent tokens (Control,
// ControlHover, AccentDim) are blended onto Surface; Alpha keeps them as
// they are for other backgrounds.
type UI struct {
	Canvas        Color `json:"canvas"`         // behind everything: the window
	Surface       Color `json:"surface"`        // panes, panels
	SurfaceRaised Color `json:"surface_raised"` // menus, popovers, cards
	SurfaceSubtle Color `json:"surface_subtle"` // sidebars, wells

	Stroke       Color `json:"stroke"`        // borders
	StrokeSubtle Color `json:"stroke_subtle"` // hairlines, dividers

	Content          Color `json:"content"`           // primary text
	ContentSecondary Color `json:"content_secondary"` // secondary text
	ContentTertiary  Color `json:"content_tertiary"`  // meta, placeholders

	Accent          Color `json:"accent"`
	AccentContainer Color `json:"accent_container"` // a filled accent area (chips, badges)
	AccentDim       Color `json:"accent_dim"`       // a faint accent wash

	Selection           Color `json:"selection"`
	SelectionForeground Color `json:"selection_foreground"`

	Control      Color `json:"control"`       // resting control fill
	ControlHover Color `json:"control_hover"` // hovered control fill
	// ControlPressed is a pressed control's fill, and ControlOnOverlay a
	// control's fill on a raised surface (a field or key on a card).
	ControlPressed   Color `json:"control_pressed"`
	ControlOnOverlay Color `json:"control_on_overlay"`

	AccentSolid   Color `json:"accent_solid"`    // a solid accent fill (a primary button)
	AccentOnSolid Color `json:"accent_on_solid"` // text on AccentSolid

	Success Status `json:"success"`
	Warning Status `json:"warning"`
	Danger  Status `json:"danger"`
	Info    Status `json:"info"`

	Alpha Alpha `json:"alpha"`
}

// Status is a status colour with its container (a faint filled area), the
// text on that container, its border, and a solid fill with the text on it.
type Status struct {
	Color       Color `json:"color"`
	Container   Color `json:"container"`
	OnContainer Color `json:"on_container"`
	Border      Color `json:"border"`
	Solid       Color `json:"solid"`
	OnSolid     Color `json:"on_solid"`
}

// Alpha are the translucent tokens before blending.
type Alpha struct {
	AccentDim       RGBA `json:"accent_dim"`
	AccentDimSubtle RGBA `json:"accent_dim_subtle"`
	AccentRingOuter RGBA `json:"accent_ring_outer"`
	Control         RGBA `json:"control"`
	ControlHover    RGBA `json:"control_hover"`
	ControlPressed  RGBA `json:"control_pressed"`
}

// rawTheme is one entry of Rex's themes.json; only the fields rexkit reads.
type rawTheme struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Appearance string `json:"appearance"`
	Source     struct {
		Background string   `json:"background"`
		Foreground string   `json:"foreground"`
		Cursor     string   `json:"cursor"`
		ANSI       []string `json:"ansi"`
	} `json:"source"`
	Palette  []tokenColor          `json:"palette"`
	Semantic map[string]tokenColor `json:"semantic"`
}

// resolve turns a raw theme into opaque, typed colours.
func (r rawTheme) resolve() (Theme, error) {
	t := Theme{ID: r.ID, Name: r.Name, Dark: r.Appearance != "light"}
	var err error
	hex := func(s string) Color {
		c, e := ParseHex(s)
		if e != nil && err == nil {
			err = fmt.Errorf("theme %s: %w", r.ID, e)
		}
		return c
	}
	if len(r.Source.ANSI) != 16 {
		return Theme{}, fmt.Errorf("rextheme: theme %s has %d ANSI colours, want 16", r.ID, len(r.Source.ANSI))
	}
	term := &t.Terminal
	term.Background = hex(r.Source.Background)
	term.Foreground = hex(r.Source.Foreground)
	term.Cursor = hex(r.Source.Cursor)
	for i, s := range r.Source.ANSI {
		term.ANSI[i] = hex(s)
	}
	if err != nil {
		return Theme{}, err
	}
	copy(term.Palette[:], xterm256(term.ANSI))
	if len(r.Palette) == 256 {
		for i, c := range r.Palette {
			term.Palette[i] = c.rgba().Over(term.Background)
		}
	}

	sem := func(name string, fallback Color) RGBA {
		if c, ok := r.Semantic[name]; ok {
			return c.rgba()
		}
		return RGBA{Color: fallback, A: 1}
	}
	ui := &t.UI
	ui.Canvas = sem("canvas", term.Background).Color
	ui.Surface = sem("surface", term.Background).Over(ui.Canvas)
	on := func(name string, fallback Color) Color { return sem(name, fallback).Over(ui.Surface) }
	ui.SurfaceRaised = on("surfaceRaised", ui.Surface)
	ui.SurfaceSubtle = on("surfaceSubtle", ui.Surface)
	ui.Content = on("content", term.Foreground)
	ui.ContentSecondary = on("contentSecondary", ui.Content.Mix(ui.Surface, 0.4))
	ui.ContentTertiary = on("contentTertiary", ui.Content.Mix(ui.Surface, 0.55))
	ui.Stroke = on("stroke", ui.Content.Mix(ui.Surface, 0.75))
	ui.StrokeSubtle = on("strokeSubtle", ui.Content.Mix(ui.Surface, 0.85))
	ui.Accent = on("accent", term.ANSI[4])
	ui.AccentContainer = on("accentContainer", ui.Accent.Mix(ui.Surface, 0.8))
	ui.Selection = on("selection", ui.Accent)
	ui.SelectionForeground = on("selectionForeground", term.Background)
	ui.Alpha = Alpha{
		AccentDim:       sem("accentDim", ui.AccentContainer),
		AccentDimSubtle: sem("accentDimSubtle", ui.AccentContainer),
		AccentRingOuter: sem("accentRingOuter", ui.Accent),
		Control:         sem("control", ui.Surface),
		ControlHover:    sem("controlHover", ui.SurfaceRaised),
		ControlPressed:  sem("controlPressed", ui.SurfaceRaised),
	}
	ui.AccentDim = ui.Alpha.AccentDim.Over(ui.Surface)
	ui.Control = ui.Alpha.Control.Over(ui.Surface)
	ui.ControlHover = ui.Alpha.ControlHover.Over(ui.Surface)
	ui.ControlPressed = ui.Alpha.ControlPressed.Over(ui.Surface)
	ui.ControlOnOverlay = on("controlOnOverlay", ui.SurfaceRaised)
	ui.AccentSolid = on("accentSolid", ui.Accent)
	ui.AccentOnSolid = on("accentOnSolid", term.Background)
	status := func(name string, fallback Color) Status {
		c := on(name, fallback)
		return Status{
			Color:       c,
			Container:   on(name+"Container", c.Mix(ui.Surface, 0.85)),
			OnContainer: on(name+"OnContainer", c),
			Border:      on(name+"Border", c),
			Solid:       on(name+"Solid", c),
			OnSolid:     on(name+"OnSolid", term.Background),
		}
	}
	ui.Success = status("success", term.ANSI[2])
	ui.Warning = status("warning", term.ANSI[3])
	ui.Danger = status("danger", term.ANSI[1])
	ui.Info = status("info", term.ANSI[4])
	return t, nil
}

// xterm256 is the standard 256-colour palette around the given ANSI colours,
// for a theme that does not list its own.
func xterm256(ansi [16]Color) []Color {
	p := make([]Color, 256)
	copy(p, ansi[:])
	level := func(i int) uint8 {
		if i == 0 {
			return 0
		}
		return uint8(55 + 40*i)
	}
	for i := range 216 {
		p[16+i] = Color{level(i / 36), level(i / 6 % 6), level(i % 6)}
	}
	for i := range 24 {
		v := uint8(8 + 10*i)
		p[232+i] = Color{v, v, v}
	}
	return p
}

//go:embed builtin.json
var builtinJSON []byte

// Built-in theme IDs: Rex's defaults, embedded so rexkit works without the
// app (or with a theme it cannot find).
const (
	DefaultDark  = "merino-dark"
	DefaultLight = "merino-light"
)

var builtins = sync.OnceValue(func() map[string]Theme {
	var raw []rawTheme
	if err := json.Unmarshal(builtinJSON, &raw); err != nil {
		panic("rextheme: builtin.json: " + err.Error())
	}
	m := map[string]Theme{}
	for _, r := range raw {
		t, err := r.resolve()
		if err != nil {
			panic("rextheme: builtin.json: " + err.Error())
		}
		m[t.ID] = t
	}
	return m
})

// Builtins returns every built-in theme, dark first.
func Builtins() []Theme { return []Theme{Builtin(true), Builtin(false)} }

// Builtin returns the built-in dark or light theme (Merino).
func Builtin(dark bool) Theme {
	if dark {
		return builtins()[DefaultDark]
	}
	return builtins()[DefaultLight]
}
