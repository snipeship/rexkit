package keys

import (
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

// areas are where the parts of the guide go, in the view's coordinates.
type areas struct {
	frame                ui.Frame
	header, body, footer ui.Rect
	line                 int // the header's text row
}

func (m *model) layout() areas {
	f := ui.FloatFrame(m.w, m.h)
	in := f.Inner(m.w, m.h)
	a := ui.Arrange(in.W, in.H, ui.LayoutOptions{NoPreview: true})
	return areas{
		frame:  f,
		header: a.Header.Offset(in.X, in.Y),
		body:   a.Body.Offset(in.X, in.Y),
		footer: a.Footer.Offset(in.X, in.Y),
		line:   in.Y + a.HeaderLine(),
	}
}

func (m *model) bodyW() int { return m.layout().body.W }
func (m *model) bodyH() int { return m.layout().body.H }

// sheetW is the sheet's width: the body less a cell of air on the right, so
// text ends where the header's and the tray's do.
func (m *model) sheetW() int { return max(0, m.bodyW()-1) }

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	if !m.scheme.Settled() || m.w <= 0 || m.h <= 0 {
		return v // hold the first frame until the palette is known
	}
	v.Content, v.Cursor = m.render(time.Now())
	v.Content = ui.Bleed(v.Content)
	return v
}

// render draws the whole guide and returns where the caret goes.
func (m *model) render(now time.Time) (string, *tea.Cursor) {
	t := m.theme
	lay := m.layout()
	header, hits := m.headerLines()
	body := m.body(lay.body.W, lay.body.H, now)
	if panel, at, _ := m.keyPanel(); m.help && panel != nil {
		body = t.Raise(body, panel, at)
	}
	tray := t.Tray(m.tray(now), lay.footer.W, lay.footer.H)
	out := lay.frame.Render(t, m.w, m.h, header, body, tray)
	if m.help || hits.Cursor < 0 || lay.header.Empty() {
		return out, nil
	}
	c := tea.NewCursor(lay.header.X+hits.Cursor, lay.line)
	c.Shape, c.Color = tea.CursorBar, t.C.Accent
	return out, c
}

// headerLines draws the header: a band, or one row when the guide is small.
func (m *model) headerLines() ([]string, ui.HeaderHits) {
	lay := m.layout()
	if lay.header.H >= ui.HeaderRows {
		return m.theme.HeaderBand(m.header(), lay.header.W)
	}
	line, hits := m.theme.Header(m.header(), lay.header.W)
	return []string{line}, hits
}

func (m *model) header() ui.Header {
	q := ui.QueryOf(m.input, "Search keys and actions")
	q.RealCursor = true
	chips := []ui.Chip{{Kind: ui.ChipTab, Label: "All", On: m.mode == 0}}
	for i, g := range m.guide.Groups {
		chips = append(chips, ui.Chip{Kind: ui.ChipTab, Label: g.Name(), On: m.mode == i+1})
	}
	return ui.Header{Query: q, Chips: chips}
}

// keyPanel is the ? panel, where it goes in the body and how far it
// scrolls.
func (m *model) keyPanel() ([]string, ui.Rect, int) {
	lay := m.layout()
	return m.theme.KeyPanel(helpGroups(m.sheet.visible > 1), lay.body.W, lay.body.H, ui.PanelCorner, m.helpAt)
}

// tray is the guide's tray: the count, or the selected entry's whole title
// when its row cuts it, then its conflict, or else what it does; a toast
// or the first load's spinner in their place. With nothing to run, the
// primary hint edits the search instead.
func (m *model) tray(now time.Time) ui.Tray {
	t := m.theme
	tr := ui.Tray{
		Meta:  t.MetaCount(count(m.view), m.guide.total(), "key"),
		Toast: m.toasts.Current(),
	}
	switch {
	case len(m.sheet.order) > 0:
		tr.Hints = []ui.Hint{{Key: "enter", Label: "run", Primary: true}, {Key: "tab", Label: "mode"}}
	case m.input.Value() != "": // nothing to run: the way out is the search
		tr.Hints = []ui.Hint{{Key: "backspace", Label: "edit", Primary: true}}
	}
	if m.sheet.visible > 1 {
		tr.Hints = append(tr.Hints, ui.Hint{Key: "left right", Label: "column"})
	}
	tr.Hints = append(tr.Hints, ui.Hint{Key: "?", Label: "keys"})
	if m.spinner.Visible(now) {
		tr.Busy = t.Spinning("Updating", now)
	}
	if m.sel < 0 || m.sel >= len(m.sheet.order) {
		return m.conflictNote(tr)
	}
	e := m.selected()
	if m.titleRoom(m.sel, e) < ansi.StringWidth(e.Title) {
		tr.Meta = t.Escaped(e.Title, e.Escapes, t.Text)
	}
	switch {
	case e.Conflict >= 0:
		tr.Meta = append(tr.Meta, ui.Space(3), ui.Span{Text: ui.GlyphWarn + " " + m.guide.Conflicts[e.Conflict].String(), Style: t.Warn})
	case e.Description != "":
		tr.Meta = append(tr.Meta, ui.Space(3), ui.Span{Text: e.Description, Style: t.Subtle})
	default:
		return m.conflictNote(tr)
	}
	return tr
}

// conflictNote adds the keymap's standing conflict note to the tray's meta:
// "⚠ ctrl+o bound twice", or "⚠ 3 conflicts".
func (m *model) conflictNote(tr ui.Tray) ui.Tray {
	cs := m.guide.Conflicts
	if len(cs) == 0 {
		return tr
	}
	note := ui.Plural(len(cs), "conflict")
	if len(cs) == 1 {
		note = ui.CapLabel(cs[0].Keys.Chord()) + " bound twice"
	}
	tr.Meta = append(tr.Meta, ui.Space(3), ui.Span{Text: ui.GlyphWarn + " " + note, Style: m.theme.Warn})
	return tr
}

// body draws the visible part of the sheet as h lines of w cells, or the
// page that stands in for an empty one.
func (m *model) body(w, h int, now time.Time) []string {
	if len(m.sheet.order) == 0 {
		return m.empty(w, h, now)
	}
	t := m.theme
	lines := make([]string, h)
	if h <= 0 || w <= 0 {
		return lines
	}
	sw := m.sheetW()
	sh := m.sheet
	before, after := sh.hidden(m.vp, h)
	rows := make([]ui.Line, h)
	for y := range rows {
		var row ui.Line
		for c := range sh.visible {
			col := m.vp.col + c
			if c > 0 {
				row = append(row, ui.Space(colGap))
			}
			p := pos{col, m.vp.row + y}
			cl, ok := sh.at(p)
			if !ok || cl.group < 0 {
				row = append(row, ui.Space(sh.colW))
				continue
			}
			row = append(row, m.cell(cl, col, sh.index(p) == m.sel, sh.colW)...)
		}
		rows[y] = row
	}
	// Overflow markers, as every list draws them: "▲ 12" and "▼ 340" for
	// rows, "◀ 3  ▶ 34" for columns, at the sheet's right edge.
	marker := func(y int, parts ...string) {
		var text []string
		for _, p := range parts {
			if p != "" {
				text = append(text, p)
			}
		}
		if len(text) == 0 {
			return
		}
		rows[y] = m.overlayEnd(rows[y], y, sw, ui.Span{Text: strings.Join(text, "  "), Style: t.Subtle})
	}
	if sh.height == 0 {
		marker(0, count0(ui.GlyphUp, before))
		marker(h-1, count0(ui.GlyphDown, after))
	} else {
		marker(h-1, count0(glyphLeft, before), count0(glyphRight, after))
	}
	for y, row := range rows {
		lines[y] = row.Fit(w).Render()
	}
	return lines
}

// Column overflow glyphs, from the same filled family as ▲/▼ (▸ is a
// folder, › a pane or crumb).
const (
	glyphLeft  = "◀"
	glyphRight = "▶"
)

// overlayEnd puts marker at the right edge of a sheet row y, w cells wide,
// with a blank cell on each side. Over the selected entry the marker keeps
// the selection fill.
func (m *model) overlayEnd(row ui.Line, y, w int, marker ui.Span) ui.Line {
	mark := ui.Line{ui.Space(1), marker, ui.Space(1)}
	room := w - mark.Width()
	if room < 1 {
		return row
	}
	if m.sel >= 0 && m.sel < len(m.sheet.order) {
		if p := m.sheet.order[m.sel]; p.row == m.vp.row+y && p.col == m.vp.col+m.sheet.visible-1 &&
			(m.sheet.visible-1)*(m.sheet.colW+colGap)+m.sheet.colW > room {
			mark = withBg(mark, m.theme.C.SelBg)
		}
	}
	return clip(row, room).Fit(room).Append(mark...)
}

// clip keeps the start of a line within w cells, with no ellipsis: the
// marker beside it already says there is more.
func clip(l ui.Line, w int) ui.Line {
	var out ui.Line
	for _, s := range l {
		if w <= 0 {
			break
		}
		text := ansi.Truncate(s.Text, w, "")
		if text != "" {
			out = append(out, ui.Span{Text: text, Style: s.Style})
		}
		w -= ansi.StringWidth(text)
		if text != s.Text {
			break
		}
	}
	return out
}

func count0(glyph string, n int) string {
	if n == 0 {
		return ""
	}
	return glyph + " " + strconv.Itoa(n)
}

// keyCell is the width an entry's keys take in column col, with the air
// after them: the column's key cells and keyGap, or a wider key and wideGap.
func (m *model) keyCell(col int, e *Entry) int {
	kw := m.sheet.keyW[col]
	if w := ansi.StringWidth(e.KeysLabel()); w > kw {
		return w + wideGap
	}
	return kw + keyGap
}

// titleRoom is the room entry i's title has in its row.
func (m *model) titleRoom(i int, e *Entry) int {
	room := m.sheet.colW - 1 - m.keyCell(m.sheet.order[i].col, e)
	if e.Conflict >= 0 {
		room -= 2 // " ⚠"
	}
	return max(0, room)
}

// cell draws a group header or an entry of column col into w cells: the
// keys in glyph notation, padded to the column's key cells, then the title;
// mode exits quiet. The selected entry is washed, marked ▍ and bold.
func (m *model) cell(cl cell, col int, selected bool, w int) ui.Line {
	t := m.theme
	s := m.view[cl.group]
	grp := m.guide.Groups[s.group]
	if cl.entry < 0 {
		return ui.Line{ui.Space(1)}.Append(head(t, grp, cl.cont)...).Fit(w)
	}
	h := s.entries[cl.entry]
	e := &grp.Entries[h.entry]
	gutter := ui.Span{Text: " "}
	titleStyle := t.Text
	switch {
	case selected:
		gutter = ui.Span{Text: ui.GlyphSelection, Style: t.Accent}
		titleStyle = t.Text.Bold(true)
	case e.Exit:
		titleStyle = t.Subtle
	}
	keys := ui.Highlight(e.KeysLabel(), h.keys, t.Text, t.Match)
	line := ui.Line{gutter}.Append(keys.Fit(m.keyCell(col, e))...)
	var warn ui.Line
	if e.Conflict >= 0 {
		warn = ui.Line{{Text: " " + ui.GlyphWarn, Style: t.Warn}}
	}
	title := ui.HighlightEscaped(e.Title, h.title, e.Escapes, titleStyle, t.Match, t.EscapeStyle())
	line = append(line, title.Truncate(max(0, w-line.Width()-warn.Width()))...)
	line = append(line, warn...).Fit(w)
	if selected {
		return withBg(t.StepUp(line, t.C.SelBg), t.C.SelBg)
	}
	return line
}

// empty is the body when no entry shows: what was searched and the ways
// out, the keymap still loading, or a keymap with nothing bound.
func (m *model) empty(w, h int, now time.Time) []string {
	t := m.theme
	switch q := m.input.Value(); {
	case q != "":
		ways := []ui.Hint{{Key: "backspace", Label: "edit"}, {Key: "esc", Label: "clear"}}
		return t.EmptyState(t.Quoted("No keys match ", q), ui.Styled("Keys, actions and modes are searched.", t.Muted), ways, w, h)
	case m.spinner.Visible(now):
		return t.LoadingState("Reading the keymap…", now, w, h)
	case m.spinner.Active():
		return ui.Compose(w, h) // a moment's blank, not a flash of "loading"
	}
	return t.EmptyState(ui.Styled("No key bindings", t.Text.Bold(true)), nil, nil, w, h)
}

// withBg puts every span of a line on bg.
func withBg(l ui.Line, bg color.Color) ui.Line {
	out := make(ui.Line, len(l))
	for i, s := range l {
		out[i] = ui.Span{Text: s.Text, Style: s.Style.Background(bg)}
	}
	return out
}

func helpGroups(columns bool) []ui.KeyGroup {
	move := []ui.Binding{{Keys: []string{"up", "down"}, Desc: "select"}}
	if columns {
		move = append(move, ui.Binding{Keys: []string{"left", "right"}, Desc: "column"})
	}
	move = append(move,
		ui.Binding{Keys: []string{"pgup", "pgdown"}, Desc: "page"},
		ui.Binding{Keys: []string{"tab", "shift+tab"}, Desc: "mode"},
	)
	return []ui.KeyGroup{
		{Title: "Move", Bindings: move},
		{Title: "Act", Bindings: []ui.Binding{
			{Keys: []string{"enter", "double-click"}, Desc: "run the action"},
			{Keys: []string{"esc"}, Desc: "clear, then close"},
			{Keys: []string{"ctrl+c"}, Desc: "close"},
		}},
	}
}
