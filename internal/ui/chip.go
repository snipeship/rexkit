package ui

import "slices"

// ChipKind selects how a chip looks and behaves.
type ChipKind int

// Chip kinds.
const (
	ChipToggle ChipKind = iota // ○ hidden ⌥. (off) / ◉ hidden (on)
	ChipTab                    // a category; On marks the active one
	ChipScope                  // in studio › web ×  (removable)
	ChipInfo                   // custom content, such as ⎇ main ↑1 ●3
)

// Chip is a small clickable state marker in the header.
type Chip struct {
	Kind  ChipKind
	Label string
	On    bool   // toggles: on; tabs: active
	Key   string // toggles: shortcut, in config notation ("alt+.")
	Badge string // tabs: a count drawn as a superscript (Agents⁵)
	// Badged keeps a cell for the badge while there is none, so the chip
	// (and the field beside it) keeps its width when a count arrives.
	Badged bool
	Line   Line // info chips: the content
}

// Filled reports whether the chip is drawn as a shape: an active tab, a
// toggle that is on (on the selection fill), or a scope (on the field fill).
func (c Chip) Filled() bool {
	return (c.Kind == ChipToggle || c.Kind == ChipTab) && c.On || c.Kind == ChipScope
}

// Chip renders a chip as the header band draws it: an active chip is a
// Control shape on the selection fill with its label in bold accent, a
// scope a Control on the field fill; inactive chips are plain words in the
// secondary colour (a toggle's key in the tertiary one), so on and off
// differ in more than hue. Draw a filled chip's edge rows with EdgeRow
// (HeaderHits.Shapes does it for the header).
func (t Theme) Chip(c Chip) Line { return t.chip(c, true, true) }

// chip renders a chip with or without its toggle key, and as a shape
// (shaped) or, in the compact header, as plain words (an active one in bold
// accent).
func (t Theme) chip(c Chip, withKey, shaped bool) Line {
	var line Line
	switch c.Kind {
	case ChipToggle:
		if c.On {
			line = Line{{GlyphOn + " " + c.Label, t.Accent.Bold(true)}}
		} else {
			line = Line{{GlyphOff + " " + c.Label, t.Muted}}
		}
		// The key stays on an active toggle (it turns it off again), so
		// turning it on does not move the chips or resize the field.
		if withKey && c.Key != "" {
			line = append(line, Space(1), Span{KeyLabel(c.Key), t.Subtle})
		}
	case ChipTab:
		line = Line{{c.Label, t.Muted}}
		if c.On {
			line = Line{{c.Label, t.Accent.Bold(true)}}
		}
		switch {
		case c.Badge != "":
			line = append(line, Span{Superscript(c.Badge), t.Warn})
		case c.Badged:
			line = append(line, Space(1))
		}
	case ChipScope:
		line = Line{{"in ", t.Muted}, {c.Label, t.Text}, {" " + GlyphRemove, t.Subtle}}
	default:
		line = slices.Clone(c.Line)
	}
	if !shaped || !c.Filled() {
		return line
	}
	fill := t.chipFill(c)
	return Control(t.StepUp(line, fill), fill)
}
