package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// MaxLabel is the display width, in cells, that Label and Name never exceed.
const MaxLabel = 256

// Label returns a single display line for a title or label from an untrusted
// source (pane titles, tab and session labels, task names). Escape sequences
// are stripped; any remaining control, format or separator rune is shown as a
// visible escape (ESC → ␛, U+202E → <U+202E>); invalid UTF-8 becomes U+FFFD. The
// result is at most MaxLabel cells wide.
func Label(s string) string {
	text, _ := LabelSpans(s)
	return text
}

// LabelSpans is Label plus where its escapes are: the byte ranges [start,
// end) of every stand-in it wrote (␛, <U+202E>, U+FFFD), so the caller can
// style them apart from the text (see ui.Theme.Escaped). Without that a file
// literally named "a<U+202E>b" would look exactly like one holding U+202E.
func LabelSpans(s string) (text string, escapes [][2]int) {
	return NameSpans(ansi.Strip(strings.ToValidUTF8(s, string(utf8.RuneError))))
}

// Name is Label without stripping escape sequences: every byte of the input is
// accounted for on screen (ESC shows as ␛, followed by the rest of the
// sequence as text). Use it for file names, which are data and should be shown
// exactly as they are.
func Name(s string) string {
	text, _ := NameSpans(s)
	return text
}

// NameSpans is Name plus the byte ranges of its escapes (see LabelSpans).
func NameSpans(s string) (text string, escapes [][2]int) {
	if isPlain(s) {
		return truncate(s), nil
	}
	var b strings.Builder
	b.Grow(len(s))
	escape := func(stand string) {
		escapes = append(escapes, [2]int{b.Len(), b.Len() + len(stand)})
		b.WriteString(stand)
	}
	prev := ' '
	skipMarks := false
	for i, r := range s {
		if r == utf8.RuneError {
			escape(string(utf8.RuneError)) // invalid UTF-8, or a literal U+FFFD: both read as "unknown"
			prev, skipMarks = r, false
			continue
		}
		if skipMarks && unicode.Is(unicode.Mn, r) {
			continue // diacritics carrying a kitty placeholder's row/column
		}
		skipMarks = r == kittyPlaceholder
		switch {
		case isJoiner(r):
			next, _ := utf8.DecodeRuneInString(s[i+utf8.RuneLen(r):])
			if keepJoiner(prev, next) {
				b.WriteRune(r)
			} else {
				escape(visible(r))
			}
		case isHidden(r):
			escape(visible(r))
		default:
			b.WriteRune(r)
		}
		prev = r
	}
	text = b.String()
	cut := truncate(text)
	if cut == text {
		return text, escapes
	}
	kept := len(strings.TrimSuffix(cut, ellipsis))
	clipped := escapes[:0]
	for _, e := range escapes {
		if e[0] < kept {
			clipped = append(clipped, [2]int{e[0], min(e[1], kept)})
		}
	}
	return cut, clipped
}

// isPlain reports printable ASCII, the common case, which needs no rewriting.
func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

const ellipsis = "…"

func truncate(s string) string {
	if len(s) <= MaxLabel {
		return s
	}
	return ansi.Truncate(s, MaxLabel, ellipsis)
}
