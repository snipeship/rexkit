package ui

import (
	"slices"
	"strconv"
	"strings"
)

// Row is what a list draws for one item.
type Row struct {
	Line    Line
	Current bool // the location the user came from: ▎ in the gutter
}

// RowFunc draws item i in width cells: l.RowWidth of the list's width (the
// list minus the gutter, and the margin or the overflow markers). selected
// lets the row set its name in bold; the selection itself (the wash, the
// mark, tertiary text stepped up) is drawn by the list.
type RowFunc func(item, width int, selected bool) Row

// View renders the visible rows, each exactly w cells wide, as l.Height()
// lines. The selected row is washed in SelBg across its
// full width, through every segment, with ▍ in the accent colour in the
// gutter (column 0) and its tertiary text stepped up a tier (Theme.OnFill);
// ▎ in the gutter marks Row.Current, the place the user came from. Sections
// are a sentence-case label and its note, set apart by a blank row when the
// list is GapMinRows tall. A column at the right edge is reserved for the
// ▲/▼ counts of rows hidden above or below, on the list's first and last
// rows, so the markers never cover a row's own cells, and the columns stay
// where they are from row to row and as the list is filtered. empty is shown when there are no items; tools
// draw an EmptyState over the body instead.
func (l *List) View(t Theme, w int, row RowFunc, empty Line) []string {
	out := make([]string, l.height)
	if w <= 0 {
		return out
	}
	blank := strings.Repeat(" ", w)
	for y := range out {
		out[y] = blank
	}
	if l.n == 0 {
		if l.height > 0 {
			out[0] = Line{Space(1)}.Append(empty...).Fit(w).Render()
		}
		return out
	}
	above, below := l.Above(), l.Below()
	// The markers sit on the first and last rows, or next to them when
	// those are a section's gap: never alone on a blank row.
	upY, downY := 0, l.height-1
	if l.gapAt(l.offset) {
		upY++
	}
	if l.gapAt(l.offset + downY) {
		downY--
	}
	mw := l.markerWidth(w)
	rowW := l.RowWidth(w)
	for y := range out {
		r := l.offset + y
		if r >= l.rows() {
			break
		}
		var line Line
		var bg = t.C.SelBg
		item, header := l.at(r)
		selected := header < 0 && item == l.cursor && !l.hideCursor
		legacy := false
		switch {
		case header >= 0 && l.gapAt(r):
			line = Line{Space(w - mw)}
			bg = nil
		case header >= 0:
			line = l.sectionLine(t, header, w-mw)
			bg = nil
		default:
			drawn := row(item, rowW, selected)
			gutter := Span{" ", t.Text}
			switch {
			case selected:
				gutter = Span{GlyphSelection, t.Accent}
			case drawn.Current:
				gutter = Span{GlyphCurrent, t.Muted}
			}
			body := drawn.Line
			if selected {
				body = t.StepUp(body, t.C.SelBg)
			}
			// A row drawn for the plain RowWidth(w) instead of the width it
			// was given has no room for the markers: overlay them as before.
			legacy = mw > 0 && body.Width() > rowW
			width := rowW
			if legacy {
				width = RowWidth(w)
			}
			line = Line{gutter}.Append(body.Fit(width)...)
			if !selected {
				bg = nil
			}
		}
		marker := ""
		switch {
		case y == upY && above > 0:
			marker = GlyphUp + " " + strconv.Itoa(above)
		case y == downY && below > 0:
			marker = GlyphDown + " " + strconv.Itoa(below)
		}
		switch {
		case legacy && marker != "":
			line = overlayEnd(line, w, Span{marker, t.Subtle})
		case !legacy && mw > 0:
			line = line.Fit(w - mw).Append(markerCell(t, marker, mw)...)
		}
		out[y] = line.Fit(w).RenderBg(bg)
	}
	return out
}

// RowWidth is the room a row gets in this list when it is w cells wide: the
// plain RowWidth(w), less the overflow marker column. RowFuncs get it as
// their width; fit row Columns to it.
func (l *List) RowWidth(w int) int {
	if mw := l.markerWidth(w); mw > 0 {
		return max(0, w-1-mw)
	}
	return RowWidth(w)
}

// markerWidth is the width of the right-edge column that holds "▲ 12" in
// a list w cells wide, after the air that sets it apart from the row's last
// column, or 0 when the list is too narrow to spare it. It is kept when
// every row fits, and sized for at least two digits, so the columns before
// it stay put as the list is filtered or scrolled.
func (l *List) markerWidth(w int) int {
	mw := markerWidth(l.n)
	if l.height <= 0 || w < 2*mw {
		return 0
	}
	return mw
}

// markerAir is the air before the marker column: the space between any two
// columns, so "▼ 26" never reads as the age's neighbour.
const markerAir = 3

// markerWidth fits the air, "▼ n" and a one-cell margin for every count up
// to n.
func markerWidth(n int) int { return markerAir + 3 + max(2, len(strconv.Itoa(n))) }

// markerCell renders marker (or blanks) right-aligned in a mw-cell column,
// after its air and with a one-cell margin on the right.
func markerCell(t Theme, marker string, mw int) Line {
	if marker == "" {
		return Line{Space(mw)}
	}
	m := Line{{marker, t.Subtle}}
	return Line{Space(max(markerAir, mw-1-m.Width()))}.Append(m...).Append(Space(1))
}

func (l *List) sectionLine(t Theme, k int, w int) Line {
	s := l.sections[k]
	line := Line{Space(1), {sectionTitle(s.Title), t.Section}}
	if s.Note != "" {
		line = append(line, Space(1), Span{s.Note, t.Subtle})
	}
	return line.Fit(w)
}

// overlayEnd places marker at the right edge of line, over trailing blanks
// when there are enough, otherwise over the end of the content. Only rows
// drawn wider than List.RowWidth (for the plain RowWidth) still need it.
func overlayEnd(line Line, w int, marker Span) Line {
	m := Line{Space(1), marker, Space(1)}
	room := w - m.Width()
	if room < 1 {
		return line
	}
	body := line.trimEnd()
	if body.Width() > room {
		body = body.clip(room)
	}
	return body.pad(room).Append(m...)
}

// RowWidth is the room a row gets in a list w cells wide when every row fits
// (the gutter on the left and a one-cell margin on the right belong to the
// list). Rows should be fitted to List.RowWidth (the width a RowFunc is
// given), which also leaves room for the overflow markers.
func RowWidth(w int) int { return max(0, w-2) }

// trimEnd drops trailing blanks.
func (l Line) trimEnd() Line {
	out := l[:len(l):len(l)]
	for len(out) > 0 {
		last := out[len(out)-1]
		trimmed := strings.TrimRight(last.Text, " ")
		if trimmed != "" {
			return append(slices.Clone(out[:len(out)-1]), Span{trimmed, last.Style})
		}
		out = out[:len(out)-1]
	}
	return out
}

// sectionTitle sets a section title in sentence case ("NEEDS YOU" → "Needs
// you"), as Rex labels its own groups.
func sectionTitle(s string) string {
	if strings.ToUpper(s) != s {
		return s
	}
	r := []rune(strings.ToLower(s))
	if len(r) > 0 {
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	}
	return string(r)
}
