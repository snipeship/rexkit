package switcher

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/match"
	"rexkit/internal/ui"
)

// Row columns.
const (
	colName = iota // indent, kind glyph and name
	colContext
	colStatusGlyph
	colStatusWord
	colAge
)

// columns lay rows out: the name flexes; context gives way first, then the
// age, then the status word (its glyph stays).
var columns = ui.Columns{
	colName:        {Flex: true, Min: 18},
	colContext:     {Shrink: true, Min: 10, Max: 34, Gap: 2, Drop: 1},
	colStatusGlyph: {Max: 1, Gap: 2},
	colStatusWord:  {Max: 12, Gap: 1, Drop: 3},
	colAge:         {Max: 6, Gap: 2, Right: true, Drop: 2},
}

// drawRow returns the list's row renderer for a list w cells wide. Columns
// are resolved once over every row, so they line up while scrolling.
func (m *model) drawRow(w int) ui.RowFunc {
	now := m.now()
	all := make([][]ui.Line, len(m.listing.rows))
	for i, r := range m.listing.rows {
		all[i] = m.cells(r, false, now)
	}
	natural := ui.Measure(all, len(columns))
	natural[colStatusGlyph], natural[colStatusWord], natural[colAge] = 1, m.statusWidth(now), columns[colAge].Max
	grid := fitColumns(m.list.RowWidth(w), natural)
	cw := grid.Width(colContext)
	return func(i, _ int, selected bool) ui.Row {
		r := m.listing.rows[i]
		cells := all[i]
		if selected || cells[colContext].Width() > cw {
			cells = m.cells(r, selected, now)
			cells[colContext] = m.context(r, m.cat.entries[r.entry], cw)
		}
		e := m.cat.entries[r.entry]
		return ui.Row{Line: grid.Row(cells...), Current: e.pane() && m.opts.origin.at(e)}
	}
}

// statusWidth is the widest status word of every entry, shown or not: the
// status and age columns keep their place as the list is searched or
// drilled into.
func (m *model) statusWidth(now time.Time) int {
	w := 0
	for _, e := range m.cat.entries {
		status := m.theme.StatusText(e.status, now)
		if !e.pane() {
			s, word := e.summary()
			status = m.theme.StatusSummary(s, word, now, true)
		}
		_, word := splitStatus(status)
		w = max(w, word.Width())
	}
	return w
}

// fitColumns resolves the columns for rows w cells wide. Long names (agent
// tasks) are capped so the context next to them stays readable. The context's
// Max is where it starts, so it is the first to give way when rows are tight;
// room the names do not use goes back to it, so "session › tab" is shortened
// only when it has to be. What is left pads the names: status and age keep
// to the right edge.
func fitColumns(w int, natural []int) ui.Grid {
	cols := slices.Clone(columns)
	cols[colName].Max = max(cols[colName].Min, (w-nameReserve)*3/5)
	grid := cols.Fit(w, natural)
	cw := grid.Width(colContext)
	spare := grid.Width(colName) - min(natural[colName], cols[colName].Max)
	if cw > 0 && spare > 0 && natural[colContext] > cw {
		cols[colContext].Max = min(natural[colContext], cw+spare)
		grid = cols.Fit(w, natural)
	}
	return grid
}

// nameReserve is roughly what the status and age columns take.
const nameReserve = 20

// cells draws one row: name, context, status and age.
func (m *model) cells(r row, selected bool, now time.Time) []ui.Line {
	t := m.theme
	e := m.cat.entries[r.entry]
	var pos [][]int
	if r.match != nil {
		pos = r.match.Positions
	}
	at := func(field int) []int {
		if field < len(pos) {
			return pos[field]
		}
		return nil
	}

	nameStyle := t.Text
	if e.status == ui.StatusExited {
		nameStyle = t.Subtle
	}
	if selected || e.kind == ui.KindSession {
		nameStyle = nameStyle.Bold(true)
	}
	name := ui.Line{ui.Space(2 * r.depth), t.Glyph(e.kind), ui.Space(1)}
	name = append(name, ui.HighlightEscaped(e.name, at(fieldName), e.nameEsc, nameStyle, t.Match, t.EscapeStyle())...)

	status := t.StatusText(e.status, now)
	if !e.pane() {
		s, word := e.summary()
		status = t.StatusSummary(s, word, now, true)
	}
	glyph, word := splitStatus(status)

	age := r.visited
	if age.IsZero() && e.kind == ui.KindAgent {
		age = e.since
	}
	return []ui.Line{
		colName:        name,
		colContext:     m.context(r, e, 0),
		colStatusGlyph: glyph,
		colStatusWord:  word,
		colAge:         ui.Styled(ui.RelTimeAt(age, now), t.Subtle),
	}
}

// splitStatus parts a status line ("✗ failed") into its glyph and its
// word, so that narrow rows can give up the word and keep the glyph.
func splitStatus(status ui.Line) (glyph, word ui.Line) {
	if len(status) == 0 {
		return nil, nil
	}
	s := status[0]
	g, w, _ := strings.Cut(s.Text, " ")
	return ui.Line{{Text: g, Style: s.Style}}, ui.Line{{Text: w, Style: s.Style}}
}

// context is the muted text after the name: why the row matched when only a
// hidden field did, else where it lives or what is in it. A positive w fits
// it to w cells, giving up what matters least first.
func (m *model) context(r row, e entry, w int) ui.Line {
	t := m.theme
	if r.match != nil && r.match.Via >= 0 {
		return hint(t, r.match, fieldsOf(e)[r.match.Via].Text, e.escapesOf(r.match.Via))
	}
	switch {
	case e.kind == ui.KindSession, r.tree && e.kind == ui.KindTab:
		return ui.Styled(e.counts(), t.Subtle)
	case r.tree && w > 0 && ansi.StringWidth(e.cwd) > w:
		return ui.Styled(ui.TruncatePath(e.cwd, w), t.Subtle) // the end names the place
	case r.tree:
		return t.Escaped(e.cwd, e.cwdEsc, t.Subtle)
	}
	var pos []int
	if r.match != nil {
		pos = r.match.Positions[fieldPath]
	}
	text, pos := fitPath(e, pos, w)
	line := ui.Highlight(text, pos, t.Muted, t.Match)
	suffix := ui.Dot + " " + e.program
	if e.kind == ui.KindAgent && e.program != "" && e.program != e.name && (w <= 0 || line.Width()+1+ansi.StringWidth(suffix) <= w) {
		// After the path, so it is what truncation gives up first.
		line = append(line, ui.Span{Text: " " + ui.Dot + " ", Style: t.Subtle}, ui.Span{Text: e.program, Style: t.Subtle})
	}
	return line
}

// hintPrefix introduces each hidden field a row can match through.
var hintPrefix = map[int]string{
	fieldCwd:     "in",
	fieldProgram: "runs",
	fieldTask:    "task:",
	fieldTitle:   "title:",
}

// hint explains a match through a hidden field: "in ~/code/api" with the
// matched characters highlighted.
func hint(t ui.Theme, res *match.Result, text string, esc escapes) ui.Line {
	return append(ui.Line{{Text: hintPrefix[res.Via] + " ", Style: t.Subtle}},
		ui.HighlightEscaped(text, res.Positions[res.Via], esc, t.Muted, t.Match, t.EscapeStyle())...)
}

// fitPath fits "session › tab" in w cells (w <= 0: no limit) by shortening
// the session, down to a single character, so the tab, the more specific
// half, stays readable: "infrastructure-and-… › deploy", "i… › deploy".
// When even that does not fit, it keeps the end: "…› deploy". pos (rune
// indexes into the full path) are remapped to the shortened text.
func fitPath(e entry, pos []int, w int) (string, []int) {
	full := e.path()
	if w <= 0 || ansi.StringWidth(full) <= w || e.kind == ui.KindTab {
		return full, pos
	}
	tail := " " + ui.GlyphCrumb + " " + e.tab
	room := w - ansi.StringWidth(tail) - ansi.StringWidth(ui.Ellipsis)
	kept := ansi.Truncate(e.session, room, "")
	if kept == "" {
		end := ansi.TruncateLeft(full, ansi.StringWidth(full)-(w-ansi.StringWidth(ui.Ellipsis)), "")
		return ui.Ellipsis + end, shiftPositions(pos, utf8.RuneCountInString(full)-utf8.RuneCountInString(end), 1)
	}
	keptRunes, sessionRunes := utf8.RuneCountInString(kept), utf8.RuneCountInString(e.session)
	var out []int
	for _, p := range pos {
		switch {
		case p < keptRunes:
			out = append(out, p)
		case p >= sessionRunes:
			out = append(out, p-sessionRunes+keptRunes+1)
		}
	}
	return kept + ui.Ellipsis + tail, out
}

// shiftPositions drops the positions before the first cut runes and moves
// the rest to after a prefix of add runes.
func shiftPositions(pos []int, cut, add int) []int {
	var out []int
	for _, p := range pos {
		if p >= cut {
			out = append(out, p-cut+add)
		}
	}
	return out
}
