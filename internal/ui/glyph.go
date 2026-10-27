package ui

import (
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Glyphs. Each has exactly one meaning across every tool; Unicode only, no
// Nerd Font assumption.
const (
	GlyphSession   = "◆"
	GlyphTab       = "▭"
	GlyphPane      = "›"
	GlyphAgent     = "✦"
	GlyphFolder    = "▸"
	GlyphFile      = "·"
	GlyphSymlink   = "↗"
	GlyphCurrent   = "▎" // gutter: where you came from
	GlyphSelection = "▍" // gutter: the selected row
	GlyphBlocked   = "●"
	GlyphIdle      = "○"
	GlyphFailed    = "✗"
	GlyphExited    = "◌"
	GlyphOn        = "◉"
	GlyphOff       = "○"
	GlyphOK        = "✓"
	GlyphWarn      = "⚠"
	GlyphInfo      = "•"
	GlyphPrompt    = "❯"
	GlyphCaret     = "▏"
	GlyphCrumb     = "›"
	GlyphRemove    = "×"
	GlyphUp        = "▲"
	GlyphDown      = "▼"
	GlyphBranch    = "⎇"
	Ellipsis       = "…"
	Dot            = "·"
)

// SpinnerFrames animate "working".
var SpinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerInterval is the time per spinner frame.
const SpinnerInterval = 80 * time.Millisecond

// SpinnerFrame returns the frame for a moment in time, so every spinner on
// screen turns in step from a single tick.
func SpinnerFrame(now time.Time) string {
	return SpinnerFrames[(now.UnixMilli()/SpinnerInterval.Milliseconds())%int64(len(SpinnerFrames))]
}

// Kind is what a row is.
type Kind int

// Row kinds.
const (
	KindSession Kind = iota
	KindTab
	KindPane
	KindAgent
	KindFolder
	KindFile
)

// Glyph returns the kind's glyph in its colour.
func (t Theme) Glyph(k Kind) Span {
	switch k {
	case KindSession:
		return Span{GlyphSession, t.Accent}
	case KindTab:
		return Span{GlyphTab, t.Muted}
	case KindAgent:
		return Span{GlyphAgent, t.Agent}
	case KindFolder:
		return Span{GlyphFolder, t.Accent}
	case KindFile:
		return Span{GlyphFile, t.Subtle}
	default:
		return Span{GlyphPane, t.Subtle}
	}
}

// Status is the state of a pane or agent.
type Status int

// Statuses, in the order "needs you" sorts them.
const (
	StatusNone Status = iota
	StatusBlocked
	StatusFailed
	StatusWorking
	StatusIdle
	StatusExited
	// StatusDone is a finished turn: an agent's result, ready to look at.
	// Unseen, it needs the user like blocked and failed do.
	StatusDone
)

// Word is the status in plain words.
func (s Status) Word() string {
	return [...]string{"", "blocked", "failed", "working", "idle", "exited", "done"}[s]
}

// StatusGlyph returns the status glyph in its colour; working spins.
func (t Theme) StatusGlyph(s Status, now time.Time) Span {
	switch s {
	case StatusBlocked:
		return Span{GlyphBlocked, t.Warn.Bold(true)}
	case StatusFailed:
		return Span{GlyphFailed, t.Err}
	case StatusWorking:
		return Span{SpinnerFrame(now), t.OK}
	case StatusIdle:
		return Span{GlyphIdle, t.Subtle}
	case StatusExited:
		return Span{GlyphExited, t.Subtle}
	case StatusDone:
		return Span{GlyphOK, t.OK}
	}
	return Span{}
}

var superscripts = strings.NewReplacer(
	"0", "⁰", "1", "¹", "2", "²", "3", "³", "4", "⁴",
	"5", "⁵", "6", "⁶", "7", "⁷", "8", "⁸", "9", "⁹",
)

// Superscript renders digits as superscripts, for chip badges (Agents⁵).
func Superscript(s string) string { return superscripts.Replace(s) }

// statusStyle is the colour a status is written in, bold for the states
// that need the user (failed, blocked) unless quiet.
func (t Theme) statusStyle(s Status, quiet bool) lipgloss.Style {
	switch s {
	case StatusBlocked:
		if quiet {
			return t.Warn
		}
		return t.Warn.Bold(true)
	case StatusFailed:
		if quiet {
			return t.Err
		}
		return t.Err.Bold(true)
	case StatusWorking:
		return t.OK
	case StatusDone:
		if quiet {
			return t.OK
		}
		return t.OK.Bold(true)
	}
	return t.Subtle
}

// StatusText is a status as lists show it: its glyph and
// word in the status colour, bold for failed and blocked, never a fill, so
// rows of "needs you" never merge into a block and the selection wash has
// no holes ("✗ failed", "● blocked", "⠋ working", "○ idle").
func (t Theme) StatusText(s Status, now time.Time) Line {
	return t.StatusSummary(s, s.Word(), now, false)
}

// StatusSummary is StatusText with a word of its own ("1 failed", "2
// working", "exited 1"); quiet drops the bold, for roll-ups on session and
// tab rows.
func (t Theme) StatusSummary(s Status, word string, now time.Time, quiet bool) Line {
	if s == StatusNone {
		return nil
	}
	style := t.statusStyle(s, quiet)
	return Line{{t.StatusGlyph(s, now).Text + " " + word, style}}
}

// Tag is the one filled status shape, for where it is the
// single status on screen (the preview title, the hold card): a Tag shape on
// the status container, "▐● blocked▌". word replaces the status word when
// set ("claude needs you"). Idle and exited states are not tagged: they
// come back as plain StatusSummary text.
func (t Theme) Tag(s Status, word string, now time.Time) Line {
	if word == "" {
		word = s.Word()
	}
	var fill color.Color
	switch s {
	case StatusBlocked:
		fill = t.C.WarnBg
	case StatusFailed:
		fill = t.C.ErrBg
	case StatusWorking, StatusDone:
		fill = t.C.OKBg
	default:
		return t.StatusSummary(s, word, now, true)
	}
	return Pill(t.StatusSummary(s, word, now, false), fill)
}
