package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// The toast on its own (rexkit toast): one row on a float of its own in the
// window's top-right corner. A tile carries the kind's glyph and the
// message follows it. Attention kinds get a solid tile, so an error reads
// as one at a glance.
const (
	// ToastMarginCols is the least of the window's width the toast leaves
	// beside it: its float's frame and some air.
	ToastMarginCols = 6
	// ToastMaxShare is the widest the toast gets, as a share of the window,
	// unless the keys it names need more.
	ToastMaxShare = 0.60
	// toastChrome is everything but the message: the tile (its two ends,
	// the glyph and a space each side of it) and two spaces before the
	// message.
	toastChrome = 7
)

// toastTile is the tile's fill and its glyph: the kind's, or the agent
// state's when the toast is about an agent.
func (t Theme) toastTile(to Toast) (color.Color, Span) {
	switch to.Status {
	case StatusBlocked:
		return t.C.WarnSolid, Span{GlyphBlocked, lipgloss.NewStyle().Foreground(t.C.WarnOnSolid)}
	case StatusFailed:
		return t.C.ErrSolid, Span{GlyphFailed, lipgloss.NewStyle().Foreground(t.C.ErrOnSolid)}
	case StatusDone:
		return t.C.OKBg, Span{GlyphOK, t.OK}
	}
	return t.kindTile(to.Kind)
}

// kindTile is the tile's fill and the glyph's style for a kind.
func (t Theme) kindTile(k ToastKind) (color.Color, Span) {
	g := t.toastGlyph(k)
	switch k {
	case ToastOK:
		return t.C.OKBg, g
	case ToastWarn:
		return t.C.WarnSolid, Span{g.Text, lipgloss.NewStyle().Foreground(t.C.WarnOnSolid)}
	case ToastErr:
		return t.C.ErrSolid, Span{g.Text, lipgloss.NewStyle().Foreground(t.C.ErrOnSolid)}
	}
	return t.C.InfoBg, g
}

// Marks in a toast's message: "{key:ctrl+p e}" is drawn as keycaps, one a
// step (a key inside a toast's sentence is a keycap); "{b:codex}" is the
// subject, in bold; "{dim:studio › web}" is context, a tier down. Text from
// outside (an agent's message) must have its braces escaped (ToastEscape)
// before it goes in, so it cannot draw keys of its own.
var toastMarks = []string{"{key:", "{b:", "{dim:"}

// ToastEscape makes text safe to put in a toast's message: braces become
// brackets, so no mark can start in it.
func ToastEscape(text string) string {
	return strings.NewReplacer("{", "[", "}", "]").Replace(text)
}

// toastPart is a run of a toast's message and how soon it gives way when
// the message is too wide.
type toastPart struct {
	line Line
	give int  // toastKey … toastAside
	more bool // it goes on from the run before, in the same style
}

// How soon a run gives way, from never to first: a keycap, the word after
// it (what the key does) and the gaps around keys never do; the aside (the
// detail after a " · ", and the context, {dim:…}) goes first, cut and then
// dropped whole; then the text, and the subject ({b:…}) last, down to "…".
const (
	toastKey = iota
	toastKeeps
	toastSubject
	toastText
	toastAside
)

// toastLeastRun is the fewest cells a run that can be dropped is cut to:
// below that it says nothing, and goes.
const toastLeastRun = 8

// plainParts is a run of a toast's text: the words, which may be cut, and
// the spaces after them, which keep apart what follows. What follows a
// " · " in the words is detail. After a key, the first word is what the
// key does ("go"), and stays whole with the key.
func plainParts(text string, style lipgloss.Style, afterKey bool) []toastPart {
	var parts []toastPart
	if afterKey {
		lead := len(text) - len(strings.TrimLeft(text, " "))
		end := len(text)
		if i := strings.IndexByte(text[lead:], ' '); i >= 0 {
			end = lead + i
		}
		parts = append(parts, toastPart{line: Line{{text[:end], style}}, give: toastKeeps})
		text = text[end:]
	}
	more := afterKey
	words := strings.TrimRight(text, " ")
	head, detail, ok := strings.Cut(words, " · ")
	switch {
	case ok:
		parts = append(parts, toastPart{Line{{head, style}}, toastText, more},
			toastPart{Line{{" · " + detail, style}}, toastAside, true})
	case words != "":
		parts = append(parts, toastPart{Line{{words, style}}, toastText, more})
	}
	if gap := text[len(words):]; gap != "" {
		parts = append(parts, toastPart{line: Line{{gap, style}}, give: toastKeeps})
	}
	return parts
}

// toastParts are the runs of a toast's message, in style, its keys as
// keycaps. Context set off by a gap takes the gap with
// it when it goes; context inside a sentence ("asks in {dim:api}") goes
// with the words before it.
func (t Theme) toastParts(text string, style lipgloss.Style) []toastPart {
	var parts []toastPart
	afterKey := false
	for text != "" {
		i, mark := -1, ""
		for _, m := range toastMarks {
			if j := strings.Index(text, m); j >= 0 && (i < 0 || j < i) {
				i, mark = j, m
			}
		}
		end := strings.Index(text[max(0, i):], "}")
		if i < 0 || end < 0 {
			break
		}
		if i > 0 {
			parts = append(parts, plainParts(text[:i], style, afterKey)...)
		}
		body := text[i+len(mark) : i+end]
		switch mark {
		case "{key:":
			parts = append(parts, toastPart{line: t.Keycap(CapLabel(body)), give: toastKey})
		case "{b:":
			parts = append(parts, toastPart{line: Line{{body, style.Bold(true)}}, give: toastSubject})
		case "{dim:":
			dim := toastPart{give: toastAside}
			if n := len(parts); n > 0 && parts[n-1].give == toastKeeps && strings.TrimSpace(parts[n-1].line.String()) == "" {
				gap := parts[n-1].line
				dim.line, parts = gap, parts[:n-1]
				if n > 1 && gap.Width() == 1 && parts[n-2].give > toastKeeps {
					dim = parts[n-2]
					dim.line = append(append(Line{}, dim.line...), gap...)
					parts = parts[:n-2]
				}
			}
			dim.line = append(dim.line, Span{body, t.Muted})
			parts = append(parts, dim)
		}
		afterKey = mark == "{key:"
		text = text[i+end+1:]
	}
	if text != "" {
		parts = append(parts, plainParts(text, style, afterKey)...)
	}
	return parts
}

// least is the narrowest a run can be cut to.
func (p toastPart) least() int {
	switch p.give {
	case toastKey, toastKeeps:
		return p.line.Width()
	case toastAside:
		return 0
	}
	return min(1, p.line.Width())
}

// toastWidths are the widths of a toast's message: least, cut as far as it
// goes; plain, without its asides; and keys, of its keycaps.
func toastWidths(parts []toastPart) (least, plain, keys int) {
	for _, p := range parts {
		least += p.least()
		if p.give < toastAside {
			plain += p.line.Width()
		}
		if p.give == toastKey {
			keys += p.line.Width()
		}
	}
	return least, plain, keys
}

// cutRun cuts a run to w cells or fewer with "…", where what is left does
// not end in a space, a separator or a "…" of its own: "studio…", not
// "studio ›…" or "studio …".
func cutRun(l Line, w int) Line {
	if l.Width() <= w {
		return l
	}
	for k := w; k > 1; k-- {
		cut := l.Truncate(k)
		left := strings.TrimSuffix(strings.TrimRight(cut.String(), " "), Ellipsis)
		if left == strings.TrimRight(left, " ›·"+Ellipsis) {
			return cut
		}
	}
	return l.Truncate(1)
}

// toastMessage fits a toast's message in w cells: while it is too wide,
// the widest of the runs that give way soonest is cut with "…", or
// dropped, so the keys it names stay whole.
func toastMessage(parts []toastPart, w int) Line {
	width := func() int {
		n := 0
		for _, p := range parts {
			n += p.line.Width()
		}
		return n
	}
	for over := width() - w; over > 0; over = width() - w {
		next := -1
		for i, p := range parts {
			if p.line.Width() <= p.least() {
				continue
			}
			if next < 0 || p.give > parts[next].give ||
				p.give == parts[next].give && p.line.Width() > parts[next].line.Width() {
				next = i
			}
		}
		if next < 0 {
			break
		}
		p := &parts[next]
		was, keep := p.line.Width(), p.line.Width()-over
		p.line = cutRun(p.line, max(1, keep))
		if p.give == toastAside && p.line.Width() < toastLeastRun {
			p.line = nil
		}
		if p.line.Width() >= was {
			break
		}
	}
	var out Line
	for _, p := range parts {
		if n := len(out); p.more && n > 0 && len(p.line) > 0 {
			out[n-1].Text += p.line[0].Text // one span, as it was before it was split
			p.line = p.line[1:]
		}
		out = append(out, p.line...)
	}
	return out.Truncate(max(1, w))
}

// ToastRow draws a toast w cells wide: one row, on the toast's own float
// (rexkit toast; Rex draws the float's header with the toast's title). The
// tile carries the kind's glyph, the message follows it; every other cell is
// left to the float (Clear). The message is cut with "…" to fit; keys in it
// ("{key:ctrl+p e}") are keycaps, which are never cut, nor is the word
// after one.
func (t Theme) ToastRow(to Toast, w int) string {
	if w <= 0 {
		return ""
	}
	clear := lipgloss.NewStyle().Background(Clear)
	if to.Text == "" || w < toastChrome+1 {
		return clear.Render(strings.Repeat(" ", w))
	}
	tile, glyph := t.toastTile(to)
	msg := toastMessage(t.toastParts(to.Text, t.toastStyle(to)), w-toastChrome)
	return Pill(Line{Space(1), glyph, Space(1)}, tile).
		Append(Space(2)).
		Append(msg...).
		Fit(w).RenderBg(Clear)
}

// ToastWidth is how wide a toast's float is in a window cols wide: its
// message whole when it fits ToastMaxShare of the window, else cut as
// ToastRow cuts it. Keys need not cost the message its words: past its
// share, the toast grows by up to the width of its keycaps to keep the
// message whole but for its asides, and as far as it must to keep the keys
// whole, while the window has room. It is 0 when nothing fits.
func (t Theme) ToastWidth(to Toast, cols int) int {
	room := cols - ToastMarginCols
	maxW := min(int(float64(cols)*ToastMaxShare), room)
	if to.Text == "" || maxW < toastChrome+1 {
		return 0
	}
	parts := t.toastParts(to.Text, t.toastStyle(to))
	least, plain, keys := toastWidths(parts)
	fit := max(maxW, min(max(least, min(plain, maxW-toastChrome+keys))+toastChrome, room))
	return toastChrome + toastMessage(parts, fit-toastChrome).Width()
}

// toastStyle is the message's style: an error in bold, but for an agent's
// toast, which has its subject in bold instead.
func (t Theme) toastStyle(to Toast) lipgloss.Style {
	if to.Kind == ToastErr && to.Status == StatusNone {
		return t.Text.Bold(true)
	}
	return t.Text
}
