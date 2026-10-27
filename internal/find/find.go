// Package find is the text search that Search and Scrollback share: a
// query, literal or a regular expression, with smart case (it turns
// case-sensitive once the query has a capital), compiled for Go and
// translated for Neovim, so ⏎ lands on a screen that finds what the list
// showed.
package find

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Query is what was typed and how to read it.
type Query struct {
	Text      string
	Regex     bool // a regular expression (Go's syntax); else literal text
	MatchCase bool // always case-sensitive; else smart case
}

// Sensitive reports whether the query matches case: with MatchCase, or
// once it has a capital. In a regex an escape (\D, \S, \W, \B) is not a
// capital, nor is what it names (\x{1F600}, \x4A, \pL, \p{Greek}), nor a
// group's name ((?P<Name>…)).
func (q Query) Sensitive() bool {
	if q.MatchCase {
		return true
	}
	s := q.Text
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case q.Regex && r == '\\':
			n += escapeArg(s[i+n:])
		case q.Regex && (strings.HasPrefix(s[i:], "(?P<") || strings.HasPrefix(s[i:], "(?<")):
			if end := strings.IndexByte(s[i:], '>'); end > 0 {
				n = end + 1
			}
		case unicode.IsUpper(r):
			return true
		}
		i += n
	}
	return false
}

// escapeArg is how many bytes of s, which follows a backslash, belong to
// the escape: its letter and what it names (\x{…}, \xHH, \p{…}, \pX).
func escapeArg(s string) int {
	if s == "" {
		return 0
	}
	_, n := utf8.DecodeRuneInString(s)
	switch s[0] {
	case 'x', 'p', 'P':
		if strings.HasPrefix(s[1:], "{") {
			if end := strings.IndexByte(s, '}'); end > 0 {
				return end + 1
			}
			return len(s)
		}
		if s[0] == 'x' {
			return min(len(s), 3) // two hex digits
		}
		if len(s) > 1 {
			_, m := utf8.DecodeRuneInString(s[1:])
			return 1 + m
		}
	}
	return n
}

// Matcher finds a compiled query in lines.
type Matcher struct {
	kind kind
	lit  string         // exact and folded: the text to look for (folded: lower-case)
	re   *regexp.Regexp // regex, and literal text whose case folds beyond ASCII
}

type kind int

const (
	exact  kind = iota // literal, case-sensitive
	folded             // literal, ASCII case ignored: searched in Fold(line)
	regex              // a regular expression (or literal text Go folds)
)

// Compile compiles q. An empty query compiles to a matcher that finds
// nothing. A regex that does not compile is an error that quotes it as typed.
func (q Query) Compile() (*Matcher, error) {
	if q.Text == "" {
		return nil, nil
	}
	if !q.Regex {
		switch {
		case q.Sensitive():
			return &Matcher{kind: exact, lit: q.Text}, nil
		case asciiCase(q.Text):
			return &Matcher{kind: folded, lit: Fold(q.Text)}, nil
		}
		return &Matcher{kind: regex, re: regexp.MustCompile("(?i)" + regexp.QuoteMeta(q.Text))}, nil
	}
	// Compile what was typed first, so an error quotes it without the (?i).
	re, err := regexp.Compile(q.Text)
	if err != nil {
		return nil, err
	}
	if !q.Sensitive() {
		re = regexp.MustCompile("(?i)" + q.Text)
	}
	return &Matcher{kind: regex, re: re}, nil
}

// asciiCase reports whether only ASCII letters in s have another case, so
// folding ASCII alone is enough to ignore case.
func asciiCase(s string) bool {
	for _, r := range s {
		if r >= utf8.RuneSelf && unicode.SimpleFold(r) != r {
			return false
		}
	}
	return true
}

// Fold lower-cases the ASCII letters of s and leaves every other byte as it
// is, so byte offsets in Fold(s) are offsets in s.
func Fold(s string) string {
	i := strings.IndexFunc(s, func(r rune) bool { return 'A' <= r && r <= 'Z' })
	if i < 0 {
		return s
	}
	b := []byte(s)
	for ; i < len(b); i++ {
		if 'A' <= b[i] && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// Folds reports whether the matcher reads folded lines: In wants
// Fold(line) as well as the line.
func (m *Matcher) Folds() bool { return m != nil && m.kind == folded }

// Slow reports a matcher worth running away from the UI: a regular
// expression, which can be a hundred times slower than a literal search.
func (m *Matcher) Slow() bool { return m != nil && m.kind == regex }

// In reports whether line holds a match that is not empty. fold is
// Fold(line) for a matcher that Folds, and is ignored otherwise.
func (m *Matcher) In(line, fold string) bool {
	switch {
	case m == nil:
		return false
	case m.kind == exact:
		return strings.Contains(line, m.lit)
	case m.kind == folded:
		return strings.Contains(fold, m.lit)
	}
	loc := m.re.FindStringIndex(line)
	if loc == nil {
		return false
	}
	return loc[1] > loc[0] || len(m.Spans(line)) > 0
}

// Spans returns the byte ranges of the matches in line, empty ones left out.
func (m *Matcher) Spans(line string) [][2]int {
	if m == nil {
		return nil
	}
	if m.kind == regex {
		var out [][2]int
		for _, r := range m.re.FindAllStringIndex(line, -1) {
			if r[1] > r[0] {
				out = append(out, [2]int{r[0], r[1]})
			}
		}
		return out
	}
	text := line
	if m.kind == folded {
		text = Fold(line)
	}
	var out [][2]int
	for at := 0; ; {
		i := strings.Index(text[at:], m.lit)
		if i < 0 {
			return out
		}
		out = append(out, [2]int{at + i, at + i + len(m.lit)})
		at += i + len(m.lit)
	}
}

// Reason is a regexp error without its "error parsing regexp: " lead,
// capitalised: "Missing closing ): `status=(5\d\d`".
func Reason(err error) string {
	s := err.Error()
	if _, rest, ok := strings.Cut(s, "regexp: "); ok {
		s = rest
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
