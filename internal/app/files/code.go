package files

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"rexkit/internal/ui"
)

// Highlighting bounds. Chroma's lexers are backtracking regular
// expressions: on crafted input (a 16 KB line in a .desktop or .htaccess
// file) they take minutes, and nothing inside them can be cancelled. The
// work is cut into tokens and stops at highlightBudget; very long lines,
// where the cost explodes, are not highlighted at all.
const (
	highlightBudget  = 200 * time.Millisecond
	highlightMaxLine = 2 << 10
	budgetCheckEvery = 64 // tokens between clock reads
)

// highlight colours sanitized source text with Chroma in the theme's code
// style and returns one rendered line per input line, plus the language
// name ("" for plain text). Every line opens and closes its own styles, so
// lines can be cut and placed independently (a multi-line comment stays
// coloured on every line). Text that can't be highlighted within the budget
// comes back plain; err is ctx's error when the preview is no longer
// wanted.
func highlight(ctx context.Context, text, filename string, t ui.Theme) ([]string, string, error) {
	plain := func() []string { return strings.Split(text, "\n") }
	lexer := lexers.Match(filename)
	if lexer == nil {
		return plain(), "", nil
	}
	language := lexer.Config().Name
	if language == "plaintext" {
		language = ""
	}
	if longestLine(text) > highlightMaxLine {
		return plain(), language, nil
	}
	tokens, err := lexer.Tokenise(nil, text)
	if err != nil {
		return plain(), language, nil
	}
	deadline := time.Now().Add(highlightBudget)
	style := styles.Get(t.CodeStyle())
	w := codeWriter{sgr: map[chroma.TokenType]string{}, style: style}
	for n := 1; ; n++ {
		tok := tokens()
		if n%budgetCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, language, err
			}
			if time.Now().After(deadline) {
				return plain(), language, nil
			}
		}
		if tok == chroma.EOF {
			break
		}
		w.token(tok)
	}
	lines := w.finish()
	if n := strings.Count(text, "\n") + 1; len(lines) > n {
		lines = lines[:n] // lexers may add a trailing newline
	}
	return lines, language, nil
}

func longestLine(text string) int {
	longest := 0
	for line := range strings.SplitSeq(text, "\n") {
		longest = max(longest, len(line))
	}
	return longest
}

// codeWriter renders tokens into lines, merging runs of the same token
// type into one styled span (as chroma.Coalesce would, but token by token,
// so the budget can be checked between them).
type codeWriter struct {
	style   *chroma.Style
	sgr     map[chroma.TokenType]string
	run     chroma.TokenType
	pending strings.Builder // the current run's text
	lines   []string
	b       strings.Builder // the current line
}

func (w *codeWriter) token(tok chroma.Token) {
	if tok.Value == "" {
		return
	}
	if tok.Type != w.run {
		w.flush()
		w.run = tok.Type
	}
	w.pending.WriteString(tok.Value)
}

// flush writes the pending run: each of its lines opens and closes the
// run's style.
func (w *codeWriter) flush() {
	if w.pending.Len() == 0 {
		return
	}
	code, ok := w.sgr[w.run]
	if !ok {
		code = entrySGR(w.style.Get(w.run))
		w.sgr[w.run] = code
	}
	for i, part := range strings.Split(w.pending.String(), "\n") {
		if i > 0 {
			w.lines = append(w.lines, w.b.String())
			w.b.Reset()
		}
		if part == "" {
			continue
		}
		if code == "" {
			w.b.WriteString(part)
			continue
		}
		w.b.WriteString(code)
		w.b.WriteString(part)
		w.b.WriteString("\x1b[m")
	}
	w.pending.Reset()
}

func (w *codeWriter) finish() []string {
	w.flush()
	return append(w.lines, w.b.String())
}

// entrySGR is the escape sequence for a Chroma style entry: foreground and
// attributes only; the preview keeps the float's own background.
func entrySGR(e chroma.StyleEntry) string {
	var params []string
	if e.Bold == chroma.Yes {
		params = append(params, "1")
	}
	if e.Italic == chroma.Yes {
		params = append(params, "3")
	}
	if e.Underline == chroma.Yes {
		params = append(params, "4")
	}
	if e.Colour.IsSet() {
		params = append(params, fmt.Sprintf("38;2;%d;%d;%d", e.Colour.Red(), e.Colour.Green(), e.Colour.Blue()))
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}
