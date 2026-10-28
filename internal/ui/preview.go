package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Preview layout.
const (
	// MetaLabelW is the width of the metadata grid's label column.
	MetaLabelW = 11
	// MetaGridMinRows is the shortest preview that shows its grid.
	MetaGridMinRows = 16
	// wellMinRows is the shortest preview that draws its content in a
	// well; a shorter one sets it flat on the float.
	wellMinRows = 5
)

// Preview is the state of a preview pane: a title row, an optional context
// row and metadata grid, and bounded, already sanitized content drawn in a
// well (a Control shape in the terminal's own background, so a pane's tail
// looks like the pane).
//
// Loads are revision-guarded: Begin before starting an async load, Set with
// the revision it returned when it completes. A late result for an older
// selection is ignored, and the previous content stays up until the new one
// is ready, so moving through a list never flashes an empty preview.
type Preview struct {
	Title   Line      // kind glyph, name in bold, context: set by Set and Refresh
	Right   Line      // right-aligned on the title row: the status Tag, the age
	Context Line      // the row under the title (cwd · task), usually tertiary
	Grid    []MetaRow // the metadata grid (MetaGrid), shown from MetaGridMinRows
	Lines   []string  // sanitized (ui/sanitize) and optionally styled
	Empty   Line      // shown in the well when there are no lines ("Empty file")
	// Rule draws the air under the title as a hairline in Line: the one
	// rule left, under a Files folder listing's title.
	Rule bool
	// Follow keeps the end in view as content grows (live pane output),
	// until the user scrolls up; scrolling back to the end resumes it.
	Follow bool
	// Decorate, when set, draws line i of Lines as it is shown (a gutter,
	// highlights), so a long capture is never decorated whole.
	Decorate func(i int, line string) string

	offset      int
	away        bool // scrolled up from the end while following
	want, shown uint64
}

// MetaRow is one row of a metadata grid: "Directory  ~/code/studio/web".
type MetaRow struct {
	Label string
	Value Line
}

// Begin starts a load and returns its revision.
func (p *Preview) Begin() uint64 {
	p.want++
	return p.want
}

// Set installs a load's result if it is the latest one requested; it reports
// whether it did. The scroll position resets.
func (p *Preview) Set(rev uint64, title Line, lines []string) bool {
	if rev != p.want {
		return false
	}
	p.shown, p.Title, p.Lines, p.offset, p.away = rev, title, lines, 0, false
	return true
}

// Refresh is Set for new content of the same subject (a live pane tail): the
// scroll position is kept, so reading back through output is not interrupted.
func (p *Preview) Refresh(rev uint64, title Line, lines []string) bool {
	if rev != p.want {
		return false
	}
	p.shown, p.Title, p.Lines = rev, title, lines
	return true
}

// Loading reports whether a requested load has not arrived yet.
func (p *Preview) Loading() bool { return p.want != p.shown }

// Scroll moves the content by delta lines (⌃U/⌃D, wheel) for a preview of
// height rows. It counts the rows View draws.
func (p *Preview) Scroll(delta, height int) {
	rows := p.layout(height).rows
	limit := max(0, len(p.Lines)-rows)
	from := p.top(rows)
	p.offset = max(0, min(from+delta, limit))
	p.away = p.offset < limit
}

// previewLayout is how a preview of some height is split: head rows (title,
// context, air, grid and its air), then the content rows, in a well (with
// its two edge rows) or flat.
type previewLayout struct {
	head, rows int
	grid, well bool
}

func (p *Preview) layout(h int) previewLayout {
	l := previewLayout{head: 1}
	if len(p.Context) > 0 && h >= 3 {
		l.head++
	}
	if h < wellMinRows {
		l.rows = max(1, h-l.head)
		return l
	}
	l.head++ // air
	if len(p.Grid) > 0 && h >= MetaGridMinRows {
		l.grid = true
		l.head += len(p.Grid) + 1
	}
	l.well = true
	l.rows = max(1, h-l.head-2)
	return l
}

func (p *Preview) top(rows int) int {
	if p.Follow && !p.away {
		return max(0, len(p.Lines)-rows)
	}
	return max(0, min(p.offset, len(p.Lines)-rows))
}

// The well's padding: its end and a cell of air on the left, its end on the
// right (with the marker column inside it).
const (
	wellPadLeft  = 2
	wellPadRight = 1
)

// ContentRect is where a preview of w×h cells draws its content lines,
// relative to the preview: inside the well's padding, or under the title
// when flat. Files fits pictures to it.
func (p *Preview) ContentRect(w, h int) Rect {
	l := p.layout(h)
	if !l.well {
		return Rect{1, l.head, max(0, w-2), max(0, min(l.rows, h-l.head))}
	}
	return Rect{1 + wellPadLeft, l.head + 1, max(0, w-2-wellPadLeft-wellPadRight), l.rows}
}

// View renders the preview as h lines of exactly w cells, with a cell of
// margin on each side: the title row (Title, and Right at its end), the
// Context row, a row of air, the Grid when the preview is MetaGridMinRows
// tall, and the content in a well down to the last row, with ▲ ▼ counts on
// its first and last rows when it scrolls. Under five rows the content is
// flat under the title. Wherever the preview sits (beside the list, under
// it, a bottom sheet) it is drawn the same: Arrange sets it apart.
func (p *Preview) View(t Theme, w, h int) []string {
	out := make([]string, 0, max(0, h))
	if w <= 0 || h <= 0 {
		return out
	}
	blank := strings.Repeat(" ", w)
	margin := func(l Line) string { return Line{Space(1)}.Append(l.Fit(max(0, w-2))...).Fit(w).Render() }
	title := p.Title
	if len(p.Right) > 0 {
		room := max(0, w-2-p.Right.Width()-2)
		title = title.Truncate(room).Fit(w - 2 - p.Right.Width()).Append(p.Right...)
	}
	out = append(out, margin(title))
	l := p.layout(h)
	if len(p.Context) > 0 && len(out) < l.head {
		out = append(out, margin(p.Context))
	}
	if !l.well || w < 8 {
		return p.flat(t, w, h, out)
	}
	if p.Rule {
		out = append(out, margin(Line{{strings.Repeat("─", w-2), lipgloss.NewStyle().Foreground(t.C.Line)}}))
	} else {
		out = append(out, blank)
	}
	if l.grid {
		for _, row := range t.MetaGrid(p.Grid, w-2) {
			out = append(out, " "+row+" ")
		}
		out = append(out, blank)
	}
	ww := w - 2
	well := ControlShape(1, ww, t.C.Well)
	out = append(out, EdgeRow(w, true, Clear, well))
	from := p.top(l.rows)
	above, below := from, max(0, len(p.Lines)-from-l.rows)
	cw := ww - wellPadLeft - wellPadRight
	mw := 0
	if (above > 0 || below > 0) && cw >= 2*markerWidth(len(p.Lines)) {
		mw = markerWidth(len(p.Lines))
	}
	left, right := Ends(t.C.Well)
	wellBg := Line{left}.Render()
	end := Line{right}.Render()
	for i := range l.rows {
		line := ""
		switch {
		case len(p.Lines) == 0 && i == 0:
			line = p.Empty.Render()
		case from+i < len(p.Lines):
			line = p.line(from + i)
		}
		if mw > 0 {
			marker := ""
			switch {
			case i == 0 && above > 0:
				marker = GlyphUp + " " + strconv.Itoa(above)
			case i == l.rows-1 && below > 0:
				marker = GlyphDown + " " + strconv.Itoa(below)
			}
			line = Fit(line, cw-mw) + markerCell(t.OnFill(t.C.Well), marker, mw).Render()
		}
		out = append(out, " "+wellBg+Paint(" "+Fit(line, cw), t.C.Well)+end+" ")
	}
	out = append(out, EdgeRow(w, false, Clear, well))
	return out
}

// line is line i of Lines as it is drawn.
func (p *Preview) line(i int) string {
	if p.Decorate != nil {
		return p.Decorate(i, p.Lines[i])
	}
	return p.Lines[i]
}

// flat is the preview without a well, for tiny sizes: content under the
// title.
func (p *Preview) flat(t Theme, w, h int, out []string) []string {
	if len(p.Lines) == 0 && len(out) < h {
		out = append(out, Line{Space(1)}.Append(p.Empty...).Fit(w).Render())
	}
	for i := p.top(h - len(out)); len(out) < h && i < len(p.Lines); i++ {
		out = append(out, " "+Fit(p.line(i), w-1))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out[:h]
}

// Meta joins title parts with " · " in muted text after a primary name:
// Meta(t, "go.mod", "1.1 KB", "3h") → go.mod · 1.1 KB · 3h.
func (t Theme) Meta(name string, parts ...string) Line {
	line := Line{{name, t.Text.Bold(true)}}
	for _, p := range parts {
		if p != "" {
			line = append(line, Span{" " + Dot + " " + p, t.Muted})
		}
	}
	return line
}

// MetaGrid renders a metadata grid as lines of w cells:
// each label tertiary in a MetaLabelW column, its value after it.
func (t Theme) MetaGrid(rows []MetaRow, w int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		label := Line{{r.Label, t.Subtle}}.Fit(MetaLabelW)
		out[i] = label.Append(r.Value...).Fit(w).Render()
	}
	return out
}
