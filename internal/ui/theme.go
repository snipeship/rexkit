// Package ui is rexkit's design system: colour tokens taken from Rex's own
// theme, one glyph per meaning, one key grammar, the rounded shapes every
// control is drawn with, and pure components (input → string + hit rects)
// that every tool composes the same way.
//
// Components never use raw colours; they take a Theme. Text inside components
// is plain (sanitized by the caller with ui/sanitize) and styled through Span
// and Line, so a row can carry one background across all of its segments.
package ui

import (
	"image/color"
	"math"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"rexkit/internal/rextheme"
)

// Palette holds the semantic colour tokens. Every one is derived from the
// Rex theme (FromRex), so the tools look like Rex's own panes and follow
// the user's theme. Translucent Rex tokens are blended onto the surface the
// token is drawn on: the float for chrome, the terminal background for the
// Well tokens.
type Palette struct {
	// Surface is the float a tool fills: surface, which Rex draws panes
	// on, floating or not. Tools do not paint it: they leave it Clear, and
	// Rex draws the float itself there. It is what the other tokens are
	// blended onto and measured against.
	Surface  color.Color
	Raised   color.Color // a card on a page (the scrollback loader's message): surfaceRaised
	Fg       color.Color // primary text: content
	FgMuted  color.Color // secondary text: contentSecondary, lifted to 4.5:1 on every chrome surface
	FgSubtle color.Color // tertiary text (ages, counts, placeholders, section labels): contentTertiary, lifted
	Line     color.Color // hairlines, never text: strokeSubtle
	Stroke   color.Color // the key panel's side edges in light themes: stroke

	Field       color.Color // the search field, the dialog field, an inactive scope: controlPressed over the float
	ErrField    color.Color // a field whose value was refused (= ErrBg)
	Tray        color.Color // the footer tray: surfaceSubtle
	KeyCap      color.Color // a keycap on the float
	TrayKeyCap  color.Color // a keycap in the tray
	PanelKeyCap color.Color // a keycap in the key panel: a step off Popover
	WellKeyCap  color.Color // a keycap in a well: a step off Well
	Well        color.Color // the preview well: surfaceSubtle, a step off the float
	WellFg      color.Color // text on the Well that is not chrome (Neovim's Normal): the terminal's foreground
	Popover     color.Color // the key panel

	Accent        color.Color // caret, active chip text, selection mark, folders, links
	Match         color.Color // matched characters (drawn bold)
	AccentSolid   color.Color // a solid accent fill (Neovim's current match)
	AccentOnSolid color.Color // text on AccentSolid
	SelBg         color.Color // the selection wash, the active chip: accentDim over the float
	SelField      color.Color // selected text in a field: accentDim over Field, lifted towards the accent to stand off it

	OK, Warn, Err, Info         color.Color // status text (≥4.5:1 on the float and on their own …Bg)
	OKBg, WarnBg, ErrBg, InfoBg color.Color // status fills: the status tag, the error field, toast tiles
	WarnSolid, WarnOnSolid      color.Color // the warn toast's tile and its glyph
	ErrSolid, ErrOnSolid        color.Color // the err toast's tile and its glyph

	WellSelBg   color.Color // a selection on the Well (Neovim's Visual): Rex's own terminal selection
	WellSelFg   color.Color // text in it
	WellFindBg  color.Color // a search match on the Well (Neovim's Search)
	WellFlashBg color.Color // a yank flash on the Well

	Agent  color.Color // the agent glyph: terminal ANSI 5
	Shadow color.Color // what shadows darken towards: black, or a blue-grey ink in light themes
}

// Clear is the background of cells that are the float itself: everything a
// tool does not fill, and the corners and lips of a rounded shape. Bleed
// turns it into the terminal's default background, which Rex draws as the
// float's own surface, so a tool's background is exactly Rex's in every
// theme and interface style. Raise composites a panel's Clear cells over
// what is under it. No palette colour is ever equal to it (FromRex nudges
// one that would be).
var Clear color.Color = rextheme.Color{R: 1, G: 2, B: 3}

// clearSGR is how Clear appears in a rendered view.
const clearSGR = "48;2;1;2;3"

// Bleed turns Clear back into the terminal's default background: the last
// step of every view a tool draws in its float.
func Bleed(content string) string { return strings.ReplaceAll(content, clearSGR, "49") }

// clearNudge replaces a theme colour that happens to equal Clear.
var clearNudge = rextheme.Color{R: 1, G: 2, B: 4}

// IsClear reports whether c is Clear.
func IsClear(c color.Color) bool {
	if c == nil {
		return false
	}
	r, g, b, a := c.RGBA()
	return a == 0xffff && r>>8 == 1 && g>>8 == 2 && b>>8 == 3
}

// Theme is a palette plus the styles derived from it. The zero value is not
// useful; use NewTheme or FromRex.
type Theme struct {
	Dark bool
	C    Palette

	Text, Muted, Subtle lipgloss.Style
	Accent, Match       lipgloss.Style
	OK, Warn, Err, Info lipgloss.Style
	Agent               lipgloss.Style
	Section             lipgloss.Style // section labels: "Needs you", "Recent"
	Key                 lipgloss.Style // a key in glyph notation (dense key columns)
	Selected            lipgloss.Style // the SelBg fill
}

// RexTheme returns the Rex theme the tools draw with for an appearance.
// It is Rex's default (Merino) unless a caller installs the user's own
// (UseUserThemes) at start-up.
var RexTheme = rextheme.Builtin

// UseUserThemes makes the tools draw with the user's own Rex themes: their
// dark or light pick (rextheme.ForAppearance), each read the first time it
// is asked for.
func UseUserThemes() {
	pick := func(dark bool) func() rextheme.Theme {
		return sync.OnceValue(func() rextheme.Theme {
			t, _ := rextheme.ForAppearance(rextheme.DefaultPaths(), dark)
			return t
		})
	}
	dark, light := pick(true), pick(false)
	RexTheme = func(d bool) rextheme.Theme {
		if d {
			return dark()
		}
		return light()
	}
}

// NewTheme returns the dark or light theme: FromRex(RexTheme(dark)).
func NewTheme(dark bool) Theme { return FromRex(RexTheme(dark)) }

// Contrast floors (WCAG ratios) that legible lifts the text tiers to.
const (
	minBodyContrast = 4.5  // secondary text, status text, tag text
	minMetaContrast = 3.0  // tertiary text on every chrome surface
	minCardContrast = 3.9  // tertiary text on the float and the well
	minCapContrast  = 1.25 // a keycap's fill on the key panel
	minSelContrast  = 1.3  // selected text's fill in a field, against the field
)

// Derivation constants.
const (
	// containerMinContrast is how far a Rex status container must stand
	// off the float to be used as a fill; below it the fill is the float
	// moved containerMix towards the status colour.
	containerMinContrast = 1.10
	containerMix         = 0.14
	// popoverLift lifts the key panel off the float in dark themes (the float
	// moved towards the content colour).
	popoverLift = 0.07
	// wellLight mixes the light well from surfaceSubtle towards the
	// terminal background.
	wellLight = 0.30
	// wellTint is how far the Neovim search and yank fills move from the
	// terminal background towards warning and success.
	wellTint = 0.25
)

// lightInk is what light themes shade towards: a blue-grey, never black,
// so a shadow on white reads as depth rather than dirt.
var lightInk = rextheme.Color{R: 0x2a, G: 0x2e, B: 0x35}

// FromRex derives the palette and styles from a Rex theme. The appearance
// is the theme's own (rt.Dark).
func FromRex(rt rextheme.Theme) Theme {
	dark := rt.Dark
	u := rt.UI
	term := rt.Terminal.Background
	card := u.Surface // the float: Rex draws a pane, floating or not, on its surface
	container := func(s rextheme.Status) rextheme.Color {
		if Contrast(s.Container, card) >= containerMinContrast {
			return s.Container
		}
		return card.Mix(s.Color, containerMix)
	}
	p := Palette{
		Surface:       card,
		Raised:        u.SurfaceRaised,
		Fg:            u.Content,
		Line:          u.StrokeSubtle,
		Stroke:        u.Stroke,
		Field:         u.Alpha.ControlPressed.Over(card),
		Tray:          u.SurfaceSubtle,
		KeyCap:        u.StrokeSubtle,
		TrayKeyCap:    u.StrokeSubtle,
		Well:          u.SurfaceSubtle,
		WellFg:        rt.Terminal.Foreground,
		Popover:       card.Mix(u.Content, popoverLift),
		Accent:        u.Accent,
		Match:         u.Accent,
		AccentSolid:   u.AccentSolid,
		AccentOnSolid: u.AccentOnSolid,
		SelBg:         u.Alpha.AccentDim.Over(card),
		OKBg:          container(u.Success),
		WarnBg:        container(u.Warning),
		ErrBg:         container(u.Danger),
		InfoBg:        container(u.Info),
		WarnSolid:     u.Warning.Solid,
		WarnOnSolid:   onFill(u.Warning.OnSolid, u.Warning.Solid),
		ErrSolid:      u.Danger.Solid,
		ErrOnSolid:    onFill(u.Danger.OnSolid, u.Danger.Solid),
		WellSelBg:     u.Selection,
		WellSelFg:     onFill(u.SelectionForeground, u.Selection),
		WellFindBg:    term.Mix(u.Warning.Color, wellTint),
		WellFlashBg:   term.Mix(u.Success.Color, wellTint),
		Agent:         rt.Terminal.ANSI[5],
		Shadow:        rextheme.Color{},
	}
	if !dark {
		p.KeyCap, p.TrayKeyCap = u.SurfaceSubtle, card
		p.Well = u.SurfaceSubtle.Mix(term, wellLight)
		p.Popover = term
		p.Shadow = lightInk
	}
	p.ErrField = p.ErrBg
	// A preselected value must read as selected: its fill stands off the
	// field, which accentDim alone barely does (≈1.06:1 in dark themes).
	p.SelField = legible(u.Alpha.AccentDim.Over(rextheme.Of(p.Field)), u.Accent, minSelContrast, p.Field)
	// A keycap in the key panel stands a fill step off it: KeyCap is
	// about the Popover's own colour in dark themes.
	p.PanelKeyCap = legible(rextheme.Of(p.KeyCap), u.Content, minCapContrast, p.Popover)
	p.WellKeyCap = legible(rextheme.Of(p.KeyCap), u.Content, minCapContrast, p.Well)

	// Text tiers hold their contrast on every surface they are set on.
	chrome := []color.Color{p.Surface, p.SelBg, p.Field, p.Tray, p.Popover}
	p.FgMuted = legible(u.ContentSecondary, u.Content, minBodyContrast, chrome...)
	subtle := legible(u.ContentTertiary, u.Content, minMetaContrast, chrome...)
	p.FgSubtle = legible(subtle, u.Content, minCardContrast, p.Surface, p.Well)
	p.OK = legible(u.Success.Color, u.Content, minBodyContrast, p.Surface, p.OKBg)
	p.Warn = legible(u.Warning.Color, u.Content, minBodyContrast, p.Surface, p.WarnBg)
	p.Err = legible(u.Danger.Color, u.Content, minBodyContrast, p.Surface, p.ErrBg)
	p.Info = legible(u.Info.Color, u.Content, minBodyContrast, p.Surface, p.InfoBg)
	p.guardClear()

	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }
	return Theme{
		Dark:     dark,
		C:        p,
		Text:     fg(p.Fg),
		Muted:    fg(p.FgMuted),
		Subtle:   fg(p.FgSubtle),
		Accent:   fg(p.Accent),
		Match:    fg(p.Match).Bold(true),
		OK:       fg(p.OK),
		Warn:     fg(p.Warn),
		Err:      fg(p.Err),
		Info:     fg(p.Info),
		Agent:    fg(p.Agent),
		Section:  fg(p.FgSubtle).Bold(true),
		Key:      fg(p.Fg),
		Selected: lipgloss.NewStyle().Background(p.SelBg),
	}
}

// guardClear nudges every token that equals Clear, so no real colour is
// ever composited away by the stage.
func (p *Palette) guardClear() {
	for _, c := range p.tokens() {
		if IsClear(*c) {
			*c = clearNudge
		}
	}
}

// tokens lists every colour of the palette.
func (p *Palette) tokens() []*color.Color {
	return []*color.Color{
		&p.Surface, &p.Raised, &p.Fg, &p.FgMuted, &p.FgSubtle, &p.Line, &p.Stroke,
		&p.Field, &p.ErrField, &p.Tray, &p.KeyCap, &p.TrayKeyCap, &p.PanelKeyCap, &p.WellKeyCap, &p.Well, &p.WellFg, &p.Popover,
		&p.Accent, &p.Match, &p.AccentSolid, &p.AccentOnSolid, &p.SelBg, &p.SelField,
		&p.OK, &p.Warn, &p.Err, &p.Info, &p.OKBg, &p.WarnBg, &p.ErrBg, &p.InfoBg,
		&p.WarnSolid, &p.WarnOnSolid, &p.ErrSolid, &p.ErrOnSolid,
		&p.WellSelBg, &p.WellSelFg, &p.WellFindBg, &p.WellFlashBg, &p.Agent, &p.Shadow,
	}
}

// OnFill returns the theme for text drawn on the fill bg (the selection
// wash, a field, the key panel): tertiary text steps up to the secondary
// tier when it would fall under its card contrast there, so ages and counts
// stay readable on a fill. Secondary text already holds 4.5:1 on every fill.
func (t Theme) OnFill(bg color.Color) Theme {
	if !t.stepsUp(bg) {
		return t
	}
	t.C.FgSubtle = t.C.FgMuted
	t.Subtle = t.Muted
	t.Section = t.Muted.Bold(true)
	return t
}

// stepsUp reports whether tertiary text steps up a tier on bg.
func (t Theme) stepsUp(bg color.Color) bool {
	return bg != nil && Contrast(t.C.FgSubtle, bg) < minCardContrast
}

// StepUp applies OnFill to a line that was built with t: spans in the
// tertiary colour move to the secondary one when bg calls for it.
func (t Theme) StepUp(l Line, bg color.Color) Line {
	if !t.stepsUp(bg) {
		return l
	}
	out := make(Line, len(l))
	for i, s := range l {
		out[i] = s
		if sameColor(s.Style.GetForeground(), t.C.FgSubtle) {
			out[i].Style = s.Style.Foreground(t.C.FgMuted)
		}
	}
	return out
}

// InTray returns the theme for drawing in the footer tray: keycaps sit on
// TrayKeyCap, which stands off the tray rather than the float.
func (t Theme) InTray() Theme {
	t.C.KeyCap = t.C.TrayKeyCap
	return t
}

// onFill is text on a solid fill (a tile's glyph, selected text), lifted
// to minBodyContrast: towards white when it is the lighter of the two,
// else towards black, or the other of the two when that way cannot get
// there. Some themes pair them a shade too close.
func onFill(c, fill rextheme.Color) rextheme.Color {
	toward, other := rextheme.Color{R: 255, G: 255, B: 255}, rextheme.Color{}
	if luminance(c) < luminance(fill) {
		toward, other = other, toward
	}
	if l := legible(c, toward, minBodyContrast, fill); Contrast(l, fill) >= minBodyContrast || Contrast(toward, fill) >= Contrast(other, fill) {
		return l
	}
	return other
}

// legible moves c towards strong in 5 % steps, at most 20 times, until it
// reaches ratio against every bg.
func legible(c, strong rextheme.Color, ratio float64, bgs ...color.Color) rextheme.Color {
	worst := func() float64 {
		w := math.Inf(1)
		for _, bg := range bgs {
			w = min(w, Contrast(c, bg))
		}
		return w
	}
	for i := 0; i < 20 && worst() < ratio; i++ {
		c = c.Mix(strong, 0.05)
	}
	return c
}

// Contrast is the WCAG contrast ratio of two colours, from 1 to 21.
func Contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		f := float64(v>>8) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// sameColor reports whether two colours are the same opaque colour.
func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return false
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// CodeStyle is the Chroma style that matches the theme.
func (t Theme) CodeStyle() string {
	if t.Dark {
		return "github-dark"
	}
	return "github"
}
