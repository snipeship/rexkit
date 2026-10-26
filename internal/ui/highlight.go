package ui

import (
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Highlight styles text with base, and the runes at positions (rune indexes,
// ascending, as returned by internal/match) with match. A grapheme cluster is
// highlighted whole when any of its runes matched, so emoji and combining
// sequences are never split by an escape sequence.
func Highlight(text string, positions []int, base, match lipgloss.Style) Line {
	return HighlightEscaped(text, positions, nil, base, match, base)
}

// EscapeStyle is how a sanitized name's escapes (␛, <U+202E>, U+FFFD; see
// sanitize.NameSpans) are drawn: subtle, so they read as markers and not
// as part of the name. Not faint: chrome tiers are real colours, so their
// contrast can be measured.
func (t Theme) EscapeStyle() lipgloss.Style { return t.Subtle }

// Escaped styles a sanitized name or label: the escape ranges (byte offsets,
// from sanitize.NameSpans or LabelSpans) in EscapeStyle, the rest in base.
func (t Theme) Escaped(text string, escapes [][2]int, base lipgloss.Style) Line {
	return HighlightEscaped(text, nil, escapes, base, base, t.EscapeStyle())
}

// HighlightEscaped is Highlight for a sanitized name: matched clusters in
// match, other clusters inside an escape range in escape, the rest in base.
func HighlightEscaped(text string, positions []int, escapes [][2]int, base, match, escape lipgloss.Style) Line {
	if len(positions) == 0 && len(escapes) == 0 {
		return Line{{text, base}}
	}
	styles := [...]lipgloss.Style{base, match, escape}
	const isBase, isMatch, isEscape = 0, 1, 2
	var out Line
	runStart, runKind := 0, isBase
	rune0, next, esc := 0, 0, 0 // rune index of the current cluster; indexes into positions and escapes
	for i := 0; i < len(text); {
		cluster, _ := ansi.FirstGraphemeCluster(text[i:], ansi.GraphemeWidth)
		n := utf8.RuneCountInString(cluster)
		for next < len(positions) && positions[next] < rune0 {
			next++
		}
		for esc < len(escapes) && escapes[esc][1] <= i {
			esc++
		}
		kind := isBase
		switch {
		case next < len(positions) && positions[next] < rune0+n:
			kind = isMatch
		case esc < len(escapes) && escapes[esc][0] <= i:
			kind = isEscape
		}
		if i > runStart && kind != runKind {
			out = append(out, Span{text[runStart:i], styles[runKind]})
			runStart = i
		}
		runKind = kind
		rune0 += n
		i += len(cluster)
	}
	return append(out, Span{text[runStart:], styles[runKind]})
}
