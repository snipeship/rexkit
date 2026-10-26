package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// FileTabWidth is the tab stop used for file previews.
	FileTabWidth = 4
	replacement  = '\uFFFD'
)

// FileText makes file contents displayable without interpreting anything:
// CRLF becomes LF; tabs expand to FileTabWidth stops; other C0 controls and
// DEL become their control pictures (ESC → ␛, NUL → ␀, a lone CR → ␍);
// C1 controls, bidi and zero-width format characters, separators,
// noncharacters, kitty placeholders and invalid UTF-8 become U+FFFD; a leading
// byte-order mark is dropped. Newlines are kept, so the result can be handed to
// a syntax highlighter and split into lines.
func FileText(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	prev := '\n'
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\n':
			b.WriteByte('\n')
			col = 0
		case r == '\r' && i < len(s) && s[i] == '\n':
			// CRLF: the LF ends the line.
		case r == '\t':
			n := FileTabWidth - col%FileTabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f:
			b.WriteString(visible(r))
			col++
		case r == utf8.RuneError && size == 1:
			b.WriteRune(replacement)
			col++
		case isJoiner(r):
			next, _ := utf8.DecodeRuneInString(s[i:])
			if keepJoiner(prev, next) {
				b.WriteRune(r)
			} else {
				b.WriteRune(replacement)
				col++
			}
		case isHidden(r):
			b.WriteRune(replacement)
			col++
		default:
			b.WriteRune(r)
			if !unicode.Is(unicode.Mn, r) {
				col++ // tab stops only need to be approximately right for wide runes
			}
		}
		prev = r
	}
	return b.String()
}

// MarkControls dims the stand-ins FileText produced (control pictures and
// U+FFFD) inside an already styled line, so they read as markers rather than
// content. Faint (SGR 2/22) is used because it composes with any colour a
// syntax highlighter applied.
func MarkControls(line string) string {
	if !strings.ContainsFunc(line, isMarker) {
		return line
	}
	var b strings.Builder
	b.Grow(len(line) + 16)
	for _, r := range line {
		if isMarker(r) {
			b.WriteString("\x1b[2m")
			b.WriteRune(r)
			b.WriteString("\x1b[22m")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isMarker(r rune) bool {
	return (r >= 0x2400 && r <= 0x2421) || r == replacement
}
