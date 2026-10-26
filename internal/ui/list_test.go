package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func newList(n, h int, sections ...Section) *List {
	var l List
	l.SetItems(n, sections)
	l.SetHeight(h)
	return &l
}

func TestListNavigation(t *testing.T) {
	tests := []struct {
		name  string
		start int
		moves func(l *List)
		want  int
	}{
		{"down", 0, (*List).Down, 1},
		{"up wraps to last", 0, (*List).Up, 19},
		{"down wraps to first", 19, (*List).Down, 0},
		{"page down", 0, (*List).PageDown, 4},
		{"page down stops at end", 18, (*List).PageDown, 19},
		{"page up stops at start", 2, (*List).PageUp, 0},
		{"half down", 0, (*List).HalfDown, 2},
		{"bottom", 3, (*List).Bottom, 19},
		{"top", 7, (*List).Top, 0},
		{"wheel does not wrap", 18, func(l *List) { l.Wheel(3) }, 19},
		{"wheel up", 5, func(l *List) { l.Wheel(-3) }, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newList(20, 5)
			l.Select(tt.start)
			tt.moves(l)
			if got := l.Cursor(); got != tt.want {
				t.Errorf("cursor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestListEmpty(t *testing.T) {
	l := newList(0, 5)
	l.Down()
	l.Up()
	l.PageDown()
	if l.Cursor() != -1 {
		t.Errorf("cursor = %d", l.Cursor())
	}
	if _, ok := l.ItemAt(0); ok {
		t.Error("ItemAt on an empty list")
	}
}

// visibleRows returns the item drawn on each row (-1 for headers/blank).
func visibleRows(l *List) []int {
	out := make([]int, l.Height())
	for y := range out {
		if item, ok := l.ItemAt(y); ok {
			out[y] = item
		} else {
			out[y] = -1
		}
	}
	return out
}

func cursorVisible(l *List) bool {
	for _, item := range visibleRows(l) {
		if item == l.Cursor() {
			return true
		}
	}
	return false
}

// TestListResizeKeepsSelectionVisible is the Files bug: after a resize the
// selected row must still be on screen.
func TestListResizeKeepsSelectionVisible(t *testing.T) {
	sections := []Section{{Title: "Recent", Start: 0}, {Title: "Folders", Start: 3}, {Title: "Files", Start: 10}}
	for _, n := range []int{1, 5, 40} {
		for sel := 0; sel < n; sel += 3 {
			for _, heights := range [][]int{{20, 3}, {3, 20, 1}, {10, 2, 7}, {1, 1, 30}} {
				l := newList(n, heights[0], sections...)
				l.Select(sel)
				for _, h := range heights[1:] {
					l.SetHeight(h)
					if !cursorVisible(l) {
						t.Fatalf("n=%d sel=%d heights=%v: cursor %d not in %v", n, sel, heights, l.Cursor(), visibleRows(l))
					}
				}
			}
		}
	}
}

func TestListShrinkingContentClampsScroll(t *testing.T) {
	l := newList(100, 10)
	l.Bottom()
	l.SetItems(12, nil)
	if l.Cursor() != 11 || !cursorVisible(l) {
		t.Fatalf("cursor %d rows %v", l.Cursor(), visibleRows(l))
	}
	if rows := visibleRows(l); rows[len(rows)-1] != 11 {
		t.Errorf("list should fill the view after shrinking, got %v", rows)
	}
}

func TestListSections(t *testing.T) {
	// Empty sections (same Start as the next, or past the end) are skipped.
	l := newList(5, 10,
		Section{Title: "Files", Start: 2},
		Section{Title: "Empty", Start: 2},
		Section{Title: "Recent", Start: 0},
		Section{Title: "Gone", Start: 9},
	)
	want := []int{-1, 0, 1, -1, -1, 2, 3, 4, -1, -1} // a blank row before the second section
	got := visibleRows(l)
	if !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	out := l.View(NewTheme(true), 20, func(i, w int, _ bool) Row { return Row{Line: Plain("item " + strconv.Itoa(i))} }, nil)
	if !strings.Contains(ansi.Strip(out[0]), "Recent") || !strings.Contains(ansi.Strip(out[4]), "Empty") {
		t.Errorf("headers: %q / %q", ansi.Strip(out[0]), ansi.Strip(out[4]))
	}
}

func TestListScrollShowsSectionHeader(t *testing.T) {
	l := newList(20, 4, Section{Title: "A", Start: 0}, Section{Title: "B", Start: 10})
	l.Bottom()
	for l.Cursor() > 10 {
		l.Up()
	}
	if rows := visibleRows(l); rows[0] != -1 || rows[1] != 10 {
		t.Errorf("moving up onto the first item of a section should reveal its header: %v", rows)
	}
}

func TestListClick(t *testing.T) {
	l := newList(10, 5, Section{Title: "S", Start: 0})
	now := time.Now()
	if _, _, ok := l.Click(0, now); ok {
		t.Error("clicking a header selected something")
	}
	item, double, ok := l.Click(2, now)
	if !ok || item != 1 || double || l.Cursor() != 1 {
		t.Fatalf("click: item=%d double=%v ok=%v", item, double, ok)
	}
	if _, double, _ = l.Click(2, now.Add(200*time.Millisecond)); !double {
		t.Error("second click within the interval should be a double-click")
	}
	if _, double, _ = l.Click(2, now.Add(300*time.Millisecond)); double {
		t.Error("a third click starts over")
	}
	if _, double, _ = l.Click(2, now.Add(time.Second)); double {
		t.Error("slow clicks are not a double-click")
	}
	l.Click(3, now)
	if _, double, _ = l.Click(4, now.Add(10*time.Millisecond)); double {
		t.Error("clicks on different rows are not a double-click")
	}
}

// A background refresh between the two clicks of a double-click must not
// swallow it; the double-click follows the entry, not the row.
func TestListDoubleClickSurvivesRefresh(t *testing.T) {
	now := time.Now()
	l := newList(10, 5)
	l.Click(2, now)
	l.SetItems(10, nil) // a refresh with the same rows…
	l.Select(2)         // …that re-selects the same entry
	if _, double, _ := l.Click(2, now.Add(150*time.Millisecond)); !double {
		t.Error("a refresh between the clicks swallowed the double-click")
	}

	// The clicked entry moved to row 4: a click on row 2 is another entry.
	l = newList(10, 5)
	l.Click(2, now)
	l.SetItems(10, nil)
	l.Select(4)
	if _, double, _ := l.Click(2, now.Add(150*time.Millisecond)); double {
		t.Error("a click on another entry is not a double-click")
	}
	// …and a click on its new row is.
	l = newList(10, 5)
	l.Click(2, now)
	l.SetItems(10, nil)
	l.Select(4)
	if _, double, _ := l.Click(4, now.Add(150*time.Millisecond)); !double {
		t.Error("the double-click should follow the entry to its new row")
	}

	// The entry is gone: the caller resets.
	l = newList(10, 5)
	l.Click(2, now)
	l.SetItems(9, nil)
	l.ResetClick()
	if _, double, _ := l.Click(2, now.Add(150*time.Millisecond)); double {
		t.Error("ResetClick must forget the first click")
	}

	// Keyboard navigation in between cancels it.
	l = newList(10, 5)
	l.Click(2, now)
	l.Down()
	l.Up()
	if _, double, _ := l.Click(2, now.Add(150*time.Millisecond)); double {
		t.Error("navigating between the clicks must cancel the double-click")
	}
}

func TestListOverflowCounts(t *testing.T) {
	l := newList(30, 5)
	l.Select(15)
	above, below := l.Above(), l.Below()
	rows := visibleRows(l)
	if above != rows[0] || below != 30-rows[len(rows)-1]-1 {
		t.Errorf("above=%d below=%d rows=%v", above, below, rows)
	}
	out := l.View(NewTheme(true), 30, func(i, w int, _ bool) Row { return Row{Line: Plain("row")} }, nil)
	if !strings.Contains(ansi.Strip(out[0]), "▲ "+strconv.Itoa(above)) ||
		!strings.Contains(ansi.Strip(out[4]), "▼ "+strconv.Itoa(below)) {
		t.Errorf("markers missing: %q %q", ansi.Strip(out[0]), ansi.Strip(out[4]))
	}
}

// The ▲/▼ markers get a column of their own: they never cover a row's last
// cells (an age, a size), and every row is as wide as the others.
func TestListMarkersDoNotCoverRows(t *testing.T) {
	th := NewTheme(true)
	l := newList(30, 5)
	l.Select(15)
	const w = 40
	widths := map[int]bool{}
	out := l.View(th, w, func(i, width int, _ bool) Row {
		widths[width] = true
		name := "row " + strconv.Itoa(i)
		return Row{Line: Line{{name, th.Text}, Space(width - len(name) - 3), {"12m", th.Subtle}}}
	}, nil)
	if len(widths) != 1 || !widths[l.RowWidth(w)] || l.RowWidth(w) >= RowWidth(w) {
		t.Fatalf("row widths %v, want only l.RowWidth = %d (< %d)", widths, l.RowWidth(w), RowWidth(w))
	}
	first, last := ansi.Strip(out[0]), ansi.Strip(out[4])
	if !strings.Contains(first, "12m") || !strings.Contains(first, "▲ "+strconv.Itoa(l.Above())) {
		t.Errorf("first row %q lost its age or marker", first)
	}
	if !strings.Contains(last, "12m") || !strings.Contains(last, "▼ "+strconv.Itoa(l.Below())) {
		t.Errorf("last row %q lost its age or marker", last)
	}
	ages := map[int]bool{}
	for _, line := range out {
		checkSize(t, "row", line, w, 1)
		plain := ansi.Strip(line)
		ages[ansi.StringWidth(plain[:strings.Index(plain, "12m")])] = true
	}
	if len(ages) != 1 {
		t.Errorf("ages are not aligned: %v", out)
	}

	// Without overflow the column stays, so filtering moves no column.
	if short := newList(3, 5); short.RowWidth(w) != l.RowWidth(w) {
		t.Errorf("RowWidth without overflow = %d, with it %d", short.RowWidth(w), l.RowWidth(w))
	}

	// A row drawn for the plain RowWidth still gets its markers.
	l = newList(30, 5)
	l.Select(15)
	out = l.View(th, w, func(i, _ int, _ bool) Row { return Row{Line: Plain(strings.Repeat("x", RowWidth(w)))} }, nil)
	if !strings.Contains(ansi.Strip(out[0]), "▲") || !strings.Contains(ansi.Strip(out[4]), "▼") {
		t.Errorf("legacy rows lost their markers: %q", out)
	}
}

// TestSelectionSpansFullRow is the Go-to bug: inner resets cut the bar before
// the meta column. Every cell of the selected row must carry selBg.
func TestSelectionSpansFullRow(t *testing.T) {
	th := NewTheme(true)
	cols := Columns{{Width: 1}, {Flex: true, Min: 4, Gap: 1}, {Gap: 2}, {Width: 1, Gap: 2}, {Gap: 1, Right: true}}
	row := func(i, w int, selected bool) Row {
		cells := []Line{
			{th.Glyph(KindAgent)},
			Highlight("Navigation polish", []int{0, 1, 2}, th.Text, th.Match),
			{{"studio", th.Muted}, {" › ", th.Subtle}, {"web", th.Muted}},
			{th.StatusGlyph(StatusBlocked, time.Time{})},
			Styled("12m", th.Subtle),
		}
		return Row{Line: cols.Fit(w, []int{1, 17, 12, 1, 3}).Row(cells...)}
	}
	for _, w := range []int{20, 44, 80, 133} {
		l := newList(3, 3)
		l.Select(1)
		out := l.View(th, w, row, nil)
		if got := cellsWithBg(t, out[1], th.C.SelBg); got != w {
			t.Errorf("width %d: %d cells on the selection bar: %q", w, got, out[1])
		}
		if got := cellsWithBg(t, out[0], th.C.SelBg); got != 0 {
			t.Errorf("width %d: unselected row has %d highlighted cells", w, got)
		}
	}
}

func TestCurrentMarkKeepsKindGlyph(t *testing.T) {
	th := NewTheme(true)
	l := newList(2, 2)
	l.Select(1)
	out := l.View(th, 20, func(i, w int, _ bool) Row {
		return Row{Line: Line{th.Glyph(KindAgent), {" api", th.Text}}, Current: i == 0}
	}, nil)
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, GlyphCurrent+"✦ api") {
		t.Errorf("current row = %q", got)
	}
}

// TestListHideCursor: with the cursor hidden no row looks selected (no
// selection wash, the row func is told it is not selected), and the cursor
// keeps its place for the next move.
func TestListHideCursor(t *testing.T) {
	th := NewTheme(true)
	l := newList(5, 5)
	l.Select(2)
	l.HideCursor(true)
	sel := false
	out := l.View(th, 30, func(i, width int, selected bool) Row {
		sel = sel || selected
		return Row{Line: Line{{"row " + strconv.Itoa(i), th.Text}}}
	}, nil)
	if sel {
		t.Error("a row was drawn as selected")
	}
	for _, line := range out {
		if strings.Contains(line, GlyphSelection) || strings.Contains(line, "48;2;") {
			t.Errorf("row %q looks selected", ansi.Strip(line))
		}
	}
	if l.Cursor() != 2 {
		t.Errorf("cursor %d, want it kept at 2", l.Cursor())
	}
	l.HideCursor(false)
	out = l.View(th, 30, func(i, width int, selected bool) Row {
		return Row{Line: Line{{"row " + strconv.Itoa(i), th.Text}}}
	}, nil)
	if cellsWithBg(t, out[2], th.C.SelBg) != 30 {
		t.Errorf("shown again, row 2 is %q", ansi.Strip(out[2]))
	}
}

// A ▲/▼ marker never sits alone on a section's blank gap row, away from
// the rows it counts: where the view starts or ends on a gap, it moves to
// the row next to it.
func TestListMarkersSkipGaps(t *testing.T) {
	l := newList(30, 10, Section{Title: "A", Start: 0}, Section{Title: "B", Start: 8}, Section{Title: "C", Start: 17})
	draw := func(i, w int, _ bool) Row { return Row{Line: Plain("item " + strconv.Itoa(i))} }
	cut := 0
	for i := range 30 {
		l.Select(i)
		out := l.View(NewTheme(true), 30, draw, nil)
		for y, line := range out {
			plain := ansi.Strip(line)
			marked := strings.Contains(plain, "▲") || strings.Contains(plain, "▼")
			if marked && strings.TrimSpace(strings.NewReplacer("▲", "", "▼", "").Replace(plain[:strings.LastIndexAny(plain, "▲▼")])) == "" {
				t.Fatalf("cursor %d: a marker alone on row %d: %q", i, y, out)
			}
		}
		if l.gapAt(l.offset) || l.gapAt(l.offset+l.Height()-1) {
			cut++
		}
	}
	if cut == 0 {
		t.Fatal("no view started or ended on a gap: the test checks nothing")
	}
}
