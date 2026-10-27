package ui

import "testing"

// TestListTallRows checks that items marked tall take two display rows, and
// that rows, headers and clicks map back to the right items.
func TestListTallRows(t *testing.T) {
	var l List
	l.SetHeight(20) // tall enough for gaps between sections
	l.SetItems(5, []Section{{Title: "Needs you", Start: 0}, {Title: "Recent", Start: 2}})
	l.SetTall(func(i int) bool { return i < 2 })
	// title, 0, 0′, 1, 1′, gap, title, 2, 3, 4
	if got, want := l.Rows(), 10; got != want {
		t.Fatalf("Rows() = %d, want %d", got, want)
	}
	want := []struct{ item, header, sub int }{
		{-1, 0, 0}, {0, -1, 0}, {0, -1, 1}, {1, -1, 0}, {1, -1, 1},
		{-1, 1, 0}, {-1, 1, 0}, {2, -1, 0}, {3, -1, 0}, {4, -1, 0},
	}
	for row, w := range want {
		item, header, sub := l.at3(row)
		if item != w.item && w.header < 0 || header != w.header || sub != w.sub {
			t.Errorf("at3(%d) = %d, %d, %d; want %d, %d, %d", row, item, header, sub, w.item, w.header, w.sub)
		}
	}
	for i, row := range []int{1, 3, 7, 8, 9} {
		if got := l.rowOf(i); got != row {
			t.Errorf("rowOf(%d) = %d, want %d", i, got, row)
		}
	}
	if item, ok := l.ItemAt(4); !ok || item != 1 {
		t.Errorf("ItemAt(4) = %d, %v; want item 1 (its detail row)", item, ok)
	}
}

// TestListTallScroll keeps a selected tall item's detail row in view.
func TestListTallScroll(t *testing.T) {
	var l List
	l.SetItems(6, nil)
	l.SetTall(func(i int) bool { return true })
	l.SetHeight(4)
	l.Select(2) // rows 4 and 5
	if l.offset != 2 {
		t.Errorf("offset = %d, want 2 so both rows of item 2 show", l.offset)
	}
}
