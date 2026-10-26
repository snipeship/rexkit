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
	// ToastMaxShare is the widest the toast gets, as a share of the window.
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

// toastTile is the tile's fill and the glyph's style for a kind.
func (t Theme) toastTile(k ToastKind) (color.Color, Span) {
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

// toastKey marks a key in a toast's message: "{key:ctrl+p e}" is drawn as
// keycaps, one a step (a key inside a toast's sentence is a
// keycap).
const toastKey = "{key:"

// toastMessage draws a toast's message in style, its keys as keycaps on
// the toast's body.
func (t Theme) toastMessage(text string, style lipgloss.Style) Line {
	caps := t
	caps.C.KeyCap = t.C.ToastKeyCap
	var out Line
	for text != "" {
		i := strings.Index(text, toastKey)
		end := strings.Index(text[max(0, i):], "}")
		if i < 0 || end < 0 {
			break
		}
		if i > 0 {
			out = append(out, Span{text[:i], style})
		}
		out = append(out, caps.Keycap(CapLabel(text[i+len(toastKey):i+end]))...)
		text = text[i+end+1:]
	}
	if text != "" {
		out = append(out, Span{text, style})
	}
	return out
}

// ToastStrip draws a toast in a w×h strip: the toast's right end
// ToastMarginCols from the right edge, on rows 1–3 (row 0 is air), and every
// other cell Clear, so the panes show through. shown is how many of its
// rows are drawn, from the top (1…ToastRows: the enter and leave frames).
// The message is cut with "…" to keep the toast within ToastMaxShare of the
// width; keys in it ("{key:ctrl+p e}") are keycaps. A strip under four rows keeps the rows it can, the text row
// first; under three, only the text row.
func (t Theme) ToastStrip(to Toast, w, h, shown int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	clear := lipgloss.NewStyle().Background(Clear).Render(strings.Repeat(" ", w))
	out := make([]string, h)
	for y := range out {
		out[y] = clear
	}
	maxW := min(int(float64(w)*ToastMaxShare), w-ToastMarginCols)
	style := t.Text
	if to.Kind == ToastErr {
		style = style.Bold(true)
	}
	msg := t.toastMessage(to.Text, style).Truncate(max(1, maxW-toastChrome))
	tw := toastChrome + msg.Width()
	if to.Text == "" || maxW < toastChrome+1 {
		return out
	}
	x := w - ToastMarginCols - tw
	tile, glyph := t.toastTile(to.Kind)
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
	top := 1
	switch {
	case h < 3:
		rows, top, shown = rows[1:2], 0, min(shown, 1)
	case h < ToastRows+1:
		top = 0
	}
	for i := range min(shown, len(rows)) {
		if y := top + i; y < h {
			out[y] = rows[i]
		}
	}
	return out
}
