package ui

import (
	"slices"
	"sort"
	"time"
)

// DoubleClick is the longest interval between two clicks on the same row
// that counts as a double-click.
const DoubleClick = 400 * time.Millisecond

// Section is a header row (NEEDS YOU, RECENT, FOLDERS) shown before item
// Start. Sections without items are not shown.
type Section struct {
	Title string // shown upper-case
	Note  string // optional, muted, after the title ("2", "updated 3s ago")
	Start int
}

// List is the selection and scroll state of a virtualized list. Items are
// addressed by index; section headers are display-only rows between them.
// The zero value is an empty list.
type List struct {
	n        int
	sections []Section
	cursor   int
	offset   int // first visible display row
	height   int

	// clickArmed: the last click selected the cursor's item at lastClickAt,
	// so a second click on the cursor's item soon after is a double-click.
	// It follows the selected entry across SetItems and Select (a refresh
	// that re-selects the same entry keeps it armed) and is disarmed by
	// navigation, a miss, a double-click and ResetClick.
	clickArmed  bool
	lastClickAt time.Time

	// hideCursor: no item is selected. The cursor keeps its place (moves
	// still start from it) but View draws its row as an ordinary row.
	hideCursor bool
}

// SetItems replaces the content: n items and their section headers (any
// order; empty sections are skipped). The cursor stays on the same index,
// clamped, and stays visible. A pending click stays armed: a caller that
// re-selects the same entry after a refresh keeps a double-click working,
// and one that cannot (the entry is gone) calls ResetClick.
func (l *List) SetItems(n int, sections []Section) {
	l.n = max(0, n)
	l.sections = l.sections[:0]
	sorted := slices.Clone(sections)
	slices.SortStableFunc(sorted, func(a, b Section) int { return a.Start - b.Start })
	for i, s := range sorted {
		empty := i+1 < len(sorted) && sorted[i+1].Start == s.Start
		if s.Start >= 0 && s.Start < l.n && !empty {
			l.sections = append(l.sections, s)
		}
	}
	l.Select(l.cursor)
}

// HideCursor sets whether the list shows no selection: View then draws the
// cursor's row as an ordinary row, with no ▍ and no selection bar. Files
// uses it while it rests on the folder itself, where no row is what ⏎ acts
// on.
func (l *List) HideCursor(hide bool) { l.hideCursor = hide }

// ResetClick forgets a pending click, so the next click is a single click.
// Call it when the selected entry changed under the cursor, for example when
// a refresh removed it.
func (l *List) ResetClick() { l.clickArmed = false }

// SetHeight sets the number of visible rows (call it on every resize); the
// selection stays visible.
func (l *List) SetHeight(h int) {
	l.height = max(0, h)
	l.scrollToCursor()
}

// Len is the number of items.
func (l *List) Len() int { return l.n }

// Rows is the number of rows the list draws in full at its height: items,
// section labels and the gaps before them.
func (l *List) Rows() int { return l.rows() }

// Height is the number of visible rows.
func (l *List) Height() int { return l.height }

// Cursor is the selected item, or -1 when the list is empty.
func (l *List) Cursor() int {
	if l.n == 0 {
		return -1
	}
	return l.cursor
}

// Select moves the selection to item i (clamped) and scrolls it into view.
func (l *List) Select(i int) {
	l.cursor = max(0, min(i, l.n-1))
	l.scrollToCursor()
}

// Up moves one item up, wrapping from the first item to the last.
func (l *List) Up() {
	l.clickArmed = false
	if l.n == 0 {
		return
	}
	if l.cursor == 0 {
		l.Select(l.n - 1)
		return
	}
	l.Select(l.cursor - 1)
}

// Down moves one item down, wrapping from the last item to the first.
func (l *List) Down() {
	l.clickArmed = false
	if l.n == 0 {
		return
	}
	if l.cursor == l.n-1 {
		l.Select(0)
		return
	}
	l.Select(l.cursor + 1)
}

// PageUp and PageDown move by a page, stopping at the ends (no wrap).
func (l *List) PageUp() { l.move(l.cursor - l.page()) }

// PageDown moves down by a page, stopping at the last item.
func (l *List) PageDown() { l.move(l.cursor + l.page()) }

// HalfUp and HalfDown move by half a page (⌃U / ⌃D).
func (l *List) HalfUp() { l.move(l.cursor - max(1, l.page()/2)) }

// HalfDown moves down by half a page.
func (l *List) HalfDown() { l.move(l.cursor + max(1, l.page()/2)) }

// Top selects the first item; Bottom the last.
func (l *List) Top() { l.move(0) }

// Bottom selects the last item.
func (l *List) Bottom() { l.move(l.n - 1) }

// Wheel moves the selection by delta rows without wrapping (mouse wheel).
func (l *List) Wheel(delta int) { l.move(l.cursor + delta) }

// move is navigation by the user: it selects i and disarms a pending click.
func (l *List) move(i int) {
	l.clickArmed = false
	l.Select(i)
}

func (l *List) page() int { return max(1, l.height-1) }

// ItemAt returns the item drawn at row y of the list (0 = first visible row).
func (l *List) ItemAt(y int) (int, bool) {
	if y < 0 || y >= l.height {
		return 0, false
	}
	item, header := l.at(l.offset + y)
	if header >= 0 || item >= l.n {
		return 0, false
	}
	return item, true
}

// Click selects the item at row y. double is true for the second click on
// the same entry within DoubleClick — the entry the first click selected,
// wherever a refresh in between moved it (see SetItems).
func (l *List) Click(y int, now time.Time) (item int, double, ok bool) {
	item, ok = l.ItemAt(y)
	if !ok {
		l.clickArmed = false
		return 0, false, false
	}
	double = l.clickArmed && item == l.cursor && now.Sub(l.lastClickAt) <= DoubleClick
	l.Select(item)
	l.clickArmed, l.lastClickAt = !double, now
	return item, double, true
}

// Above and Below count the items scrolled out of view.
func (l *List) Above() int {
	item, header := l.at(l.offset)
	if header >= 0 {
		item = l.sections[header].Start
	}
	return min(item, l.n)
}

// Below counts the items after the last visible row.
func (l *List) Below() int {
	last := l.offset + l.height - 1
	if last >= l.rows()-1 {
		return 0
	}
	item, header := l.at(last)
	if header >= 0 {
		return l.n - l.sections[header].Start
	}
	return l.n - item - 1
}

// GapMinRows is the shortest list whose sections are set apart by a blank
// row; a shorter one keeps every row for items.
const GapMinRows = 10

// gap is the number of blank rows before every section but the first:
// sections are set apart by air, not rules.
func (l *List) gap() int {
	if l.height >= GapMinRows {
		return 1
	}
	return 0
}

// rows is the number of display rows (items, visible headers and the gaps
// before them).
func (l *List) rows() int { return l.n + l.headerRows(len(l.sections)) }

// headerRows is the number of rows the first k section headers take.
func (l *List) headerRows(k int) int {
	if k <= 0 {
		return 0
	}
	return k + (k-1)*l.gap()
}

// titleRow is the display row of section k's title.
func (l *List) titleRow(k int) int { return l.sections[k].Start + k + k*l.gap() }

// rowOf returns the display row of item i.
func (l *List) rowOf(i int) int {
	headers := sort.Search(len(l.sections), func(k int) bool { return l.sections[k].Start > i })
	return i + l.headerRows(headers)
}

// at maps a display row to an item, or to a section header (header ≥ 0):
// its title or the gap before it.
func (l *List) at(row int) (item, header int) {
	// Find the last header whose rows (gap, then title) start at or before row.
	k := sort.Search(len(l.sections), func(k int) bool {
		top := l.titleRow(k)
		if k > 0 {
			top -= l.gap()
		}
		return top > row
	}) - 1
	if k >= 0 && row <= l.titleRow(k) {
		return -1, k
	}
	return row - l.headerRows(k+1), -1
}

// gapAt reports whether display row is the blank gap before a section.
func (l *List) gapAt(row int) bool {
	_, k := l.at(row)
	return k >= 0 && row < l.titleRow(k)
}

func (l *List) scrollToCursor() {
	if l.height <= 0 || l.n == 0 {
		l.offset = 0
		return
	}
	row := l.rowOf(l.cursor)
	top := row
	if _, header := l.at(row - 1); row > 0 && header >= 0 {
		top = row - 1 // keep the item's section header in view when scrolling up
	}
	if top < l.offset {
		l.offset = top
	}
	if row >= l.offset+l.height {
		l.offset = row - l.height + 1
	}
	l.offset = max(0, min(l.offset, l.rows()-l.height))
}
