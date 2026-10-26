package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Span is a run of plain (already sanitized) text in one style.
type Span struct {
	Text  string
	Style lipgloss.Style
}

// Line is one row of styled text, kept as spans until it is rendered so that
// truncation never cuts an escape sequence and a row background can be
// applied to every span (inner resets never cut a selection bar).
type Line []Span

// Plain returns a single unstyled span.
func Plain(s string) Line { return Line{{Text: s}} }

// Styled returns a single span.
func Styled(s string, style lipgloss.Style) Line { return Line{{s, style}} }

// Width is the display width in cells.
func (l Line) Width() int {
	w := 0
	for _, s := range l {
		w += ansi.StringWidth(s.Text)
	}
	return w
}

// String is the plain text.
func (l Line) String() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Append adds spans and returns the line (for chaining).
func (l Line) Append(spans ...Span) Line { return append(l, spans...) }

// Space returns n spaces as a span.
func Space(n int) Span { return Span{Text: strings.Repeat(" ", max(0, n))} }

// Render renders the line with each span's own style.
func (l Line) Render() string { return l.RenderBg(nil) }

// RenderBg renders the line with bg behind every span that has no background
// of its own, so the background is continuous across the whole row.
func (l Line) RenderBg(bg color.Color) string {
	var b strings.Builder
	for _, s := range l {
		if s.Text == "" {
			continue
		}
		style := s.Style
		if bg != nil && !hasBackground(style) {
			style = style.Background(bg)
		}
		b.WriteString(style.Render(s.Text))
	}
	return b.String()
}

func hasBackground(s lipgloss.Style) bool {
	_, none := s.GetBackground().(lipgloss.NoColor)
	return !none
}

// Truncate keeps the start of the line within w cells, ending with "…" in the
// style of the last span it cut.
func (l Line) Truncate(w int) Line {
	if w <= 0 {
		return nil
	}
	if l.Width() <= w {
		return l
	}
	out := make(Line, 0, len(l))
	room := w - 1 // for the ellipsis
	for _, s := range l {
		text, cw := cutWidth(s.Text, room)
		if text != "" {
			out = append(out, Span{text, s.Style})
		}
		room -= cw
		if text != s.Text {
			out = append(out, Span{Ellipsis, s.Style})
			break
		}
	}
	return out.pad(w)
}

// TruncateStart keeps the end of the line within w cells, starting with "…".
// Use it for paths, where the meaningful part is at the end.
func (l Line) TruncateStart(w int) Line {
	if w <= 0 {
		return nil
	}
	if l.Width() <= w {
		return l
	}
	room := w - 1
	var rev Line
	for i := len(l) - 1; i >= 0; i-- {
		s := l[i]
		text, cw := cutWidthEnd(s.Text, room)
		if text != "" {
			rev = append(rev, Span{text, s.Style})
		}
		room -= cw
		if text != s.Text {
			rev = append(rev, Span{Ellipsis, s.Style})
			break
		}
	}
	out := make(Line, 0, len(rev)+1)
	if gap := w - rev.Width(); gap > 0 {
		out = append(out, Space(gap)) // a wide rune did not fit
	}
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

// Fit truncates (keeping the start) and pads the line to exactly w cells.
func (l Line) Fit(w int) Line { return l.Truncate(w).pad(w) }

// FitStart truncates keeping the end and pads to exactly w cells.
func (l Line) FitStart(w int) Line { return l.TruncateStart(w).pad(w) }

// FitRight truncates keeping the start and right-aligns within w cells.
func (l Line) FitRight(w int) Line {
	l = l.Truncate(w)
	if gap := w - l.Width(); gap > 0 {
		return append(Line{Space(gap)}, l...)
	}
	return l
}

func (l Line) pad(w int) Line {
	if gap := w - l.Width(); gap > 0 {
		return append(l[:len(l):len(l)], Space(gap))
	}
	return l
}

// cutWidth returns the longest prefix of s (whole grapheme clusters) that fits
// in w cells, and its width.
func cutWidth(s string, w int) (string, int) {
	used, i := 0, 0
	for i < len(s) {
		cluster, cw := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		if used+cw > w {
			break
		}
		used += cw
		i += len(cluster)
	}
	return s[:i], used
}

// cutWidthEnd returns the longest suffix of s that fits in w cells.
func cutWidthEnd(s string, w int) (string, int) {
	var widths, sizes []int
	for i := 0; i < len(s); {
		cluster, cw := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		widths, sizes = append(widths, cw), append(sizes, len(cluster))
		i += len(cluster)
	}
	used, start := 0, len(s)
	for k := len(widths) - 1; k >= 0; k-- {
		if used+widths[k] > w {
			break
		}
		used += widths[k]
		start -= sizes[k]
	}
	return s[start:], used
}

// Fit pads or truncates an already rendered (ANSI) string to exactly w cells.
// Use it for content that is not a Line, such as sanitized terminal output or
// highlighted code. The result never leaks attributes past its end.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, Ellipsis)
	}
	if strings.Contains(s, "\x1b") {
		s += "\x1b[m"
	}
	if gap := w - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// TruncatePath shortens a path to w cells keeping its end, cutting at a
// separator when possible: "~/Coding/snipeship/rexkit" → "…/snipeship/rexkit".
func TruncatePath(path string, w int) string { return truncateKeepEnd(path, "/", w) }

// TruncateMiddle shortens a path to w cells keeping its last segment and as
// much of its start as fits, with "…" between: "~/Coding/snipeship/website"
// → "~/Co…/website", "~/Coding/rexkit/docs" → "~/…/docs". When even the last
// segment does not fit it keeps the end, as TruncatePath.
func TruncateMiddle(path string, w int) string {
	if ansi.StringWidth(path) <= w {
		return path
	}
	if w <= 0 {
		return ""
	}
	tail := path
	if i := strings.LastIndex(path, "/"); i > 0 {
		tail = path[i:]
	}
	room := w - 1 - ansi.StringWidth(tail)
	if room < 1 {
		return TruncatePath(path, w)
	}
	head, _ := cutWidth(path, room)
	return head + Ellipsis + tail
}

// truncateKeepEnd shortens s to w cells keeping its end, starting at a sep
// boundary when one is in reach.
func truncateKeepEnd(s, sep string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	tail, _ := cutWidthEnd(s, w-1)
	if i := strings.Index(tail, sep); i >= 0 && i < len(tail)-len(sep) {
		tail = tail[i:]
	}
	return Ellipsis + tail
}

// clip keeps the start of the line within w cells without an ellipsis, for
// when something else (an overflow marker) already says there is more.
func (l Line) clip(w int) Line {
	var out Line
	for _, s := range l {
		text, cw := cutWidth(s.Text, w)
		if text != "" {
			out = append(out, Span{text, s.Style})
		}
		w -= cw
		if text != s.Text {
			break
		}
	}
	return out
}
