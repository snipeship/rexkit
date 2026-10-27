package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// The toast on its own (rexkit toast): a Control shape of
// two segments in the top-right corner of a strip along the window's top. A
// tile that runs the toast's full height carries the kind's glyph; the body
// carries the message. Attention kinds get a solid tile, so an error reads
// as one at a glance.
const (
	// ToastMarginCols is the air between the toast and the window's right
	// edge.
	ToastMarginCols = 2
	// ToastMaxShare is the widest the toast gets, as a share of the window,
	// unless the keys it names need more.
	ToastMaxShare = 0.60
	// ToastRows is the toast's height with its edge rows; ToastFrames
	// draws it growing a row at a time.
	ToastRows = 3
	// toastTileW is the tile: its end, a space, the glyph and a space.
	toastTileW = 4
	// toastChrome is everything but the message: the tile, two spaces
	// before the message, one after it and the closing end.
	toastChrome = toastTileW + 4
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
// keycaps on the toast's body. Context set off by a gap takes the gap with
// it when it goes; context inside a sentence ("asks in {dim:api}") goes
// with the words before it.
func (t Theme) toastParts(text string, style lipgloss.Style) []toastPart {
	caps := t
	caps.C.KeyCap = t.C.ToastKeyCap
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
			parts = append(parts, toastPart{line: caps.Keycap(CapLabel(body)), give: toastKey})
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

// ToastStrip draws a toast in a w×h strip: the toast's right end
// ToastMarginCols from the right edge, on rows 1–3 (row 0 is air), and every
// other cell Clear, so the panes show through. shown is how many of its
// rows are drawn, from the top (1…ToastRows: the enter and leave frames).
// The message is cut with "…" to keep the toast within ToastMaxShare of the
// width; keys in it ("{key:ctrl+p e}") are keycaps, which are never cut,
// nor is the word after one. A
// strip under four rows keeps the rows it can, the text row first; under
// three, only the text row.
func (t Theme) ToastStrip(to Toast, w, h, shown int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	clear := lipgloss.NewStyle().Background(Clear).Render(strings.Repeat(" ", w))
	out := make([]string, h)
	for y := range out {
		out[y] = clear
	}
	msg, x, tw, ok := t.toastFit(to, w)
	if !ok {
		return out
	}
	tile, glyph := t.toastTile(to)
	body := t.C.Toast
	left, _ := Ends(tile)
	_, right := Ends(body)
	text := Line{left}.
		Append(OnBg(Line{Space(1), glyph, Space(1)}, tile)...).
		Append(OnBg(Line{Space(2)}.Append(msg...).Append(Space(1)), body)...).
		Append(right)
	shape := Shape{ProfileControl, x, []Seg{{toastTileW, tile}, {tw - toastTileW, body}}}
	rows := []string{
		EdgeRow(w, true, Clear, shape),
		Line{{strings.Repeat(" ", x), lipgloss.NewStyle().Background(Clear)}}.Append(text...).
			Append(Span{strings.Repeat(" ", ToastMarginCols), lipgloss.NewStyle().Background(Clear)}).RenderBg(Clear),
		EdgeRow(w, false, Clear, shape),
	}
	top, rows := toastRows(rows, h)
	if len(rows) == 1 {
		shown = min(shown, 1)
	}
	for i := range min(shown, len(rows)) {
		if y := top + i; y < h {
			out[y] = rows[i]
		}
	}
	return out
}

// toastFit is a toast's message fitted to a strip w wide, the toast's
// first column and its width; ok is false when nothing fits.
func (t Theme) toastFit(to Toast, w int) (msg Line, x, tw int, ok bool) {
	maxW := min(int(float64(w)*ToastMaxShare), w-ToastMarginCols)
	if to.Text == "" || maxW < toastChrome+1 {
		return nil, 0, 0, false
	}
	style := t.Text
	if to.Kind == ToastErr && to.Status == StatusNone {
		style = style.Bold(true) // an agent's toast has its subject in bold instead
	}
	// Keys need not cost the message its words: past its share, the toast
	// grows by up to the width of its keycaps to keep the message whole but
	// for its asides, and as far as it must to keep the keys whole, while
	// the window has room.
	parts := t.toastParts(to.Text, style)
	least, plain, keys := toastWidths(parts)
	room := max(maxW, min(max(least, min(plain, maxW-toastChrome+keys))+toastChrome, w-ToastMarginCols))
	msg = toastMessage(parts, room-toastChrome)
	tw = toastChrome + msg.Width()
	return msg, w - ToastMarginCols - tw, tw, true
}

// toastRows places the toast's rows (edge, text, edge) in a strip h tall:
// below a row of air, at the top when there is no room for it, or only the
// text row under three rows. It returns the first row's y and the rows
// kept.
func toastRows[T any](rows []T, h int) (int, []T) {
	switch {
	case h < 3:
		return 0, rows[1:2]
	case h < ToastRows+1:
		return 0, rows
	}
	return 1, rows
}

// ToastHit reports whether the cell (x, y) of a w×h strip is on the toast
// ToastStrip draws there: a click on it acts on the toast, a click beside
// it does not.
func (t Theme) ToastHit(to Toast, w, h, x, y int) bool {
	_, left, tw, ok := t.toastFit(to, w)
	if !ok || x < left || x >= left+tw {
		return false
	}
	top, rows := toastRows([]int{0, 1, 2}, h)
	return y >= top && y < top+len(rows)
}
