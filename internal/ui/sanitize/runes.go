// Package sanitize turns untrusted text (pane output, file contents, names and
// titles) into text that is safe to put on screen: it never moves the cursor,
// never changes terminal modes and never hides what is really there.
package sanitize

import (
	"fmt"
	"unicode"
)

// kittyPlaceholder is the kitty graphics Unicode placeholder. Untrusted text
// must not be able to reference images that we (or anyone) uploaded.
const kittyPlaceholder = '\U0010EEEE'

// isJoiner reports runes that are only meaningful inside a grapheme cluster
// (emoji ZWJ sequences, Indic conjuncts).
func isJoiner(r rune) bool { return r == '\u200D' || r == '\u200C' }

// isNoncharacter reports Unicode noncharacters (U+FDD0..U+FDEF and the last two
// code points of every plane).
func isNoncharacter(r rune) bool {
	return (r >= 0xfdd0 && r <= 0xfdef) || r&0xfffe == 0xfffe
}

// isHidden reports runes that are invisible or reorder text: control
// characters, format characters (bidi overrides, zero-width), line/paragraph
// separators, noncharacters and the kitty placeholder.
func isHidden(r rune) bool {
	return unicode.IsControl(r) ||
		unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) ||
		isNoncharacter(r) ||
		r == kittyPlaceholder
}

// visible returns a printable stand-in for a hidden rune: the Unicode control
// picture for C0 and DEL (ESC → ␛), and <U+XXXX> for everything else.
func visible(r rune) string {
	switch {
	case r < 0x20:
		return string(0x2400 + r)
	case r == 0x7f:
		return "␡"
	default:
		return fmt.Sprintf("<U+%04X>", r)
	}
}

// keepJoiner reports whether a ZWJ/ZWNJ sits between two visible runes, where
// it legitimately glues a grapheme cluster together.
func keepJoiner(prev, next rune) bool {
	ok := func(r rune) bool { return r > ' ' && !isHidden(r) && !unicode.IsSpace(r) }
	return ok(prev) && ok(next)
}
