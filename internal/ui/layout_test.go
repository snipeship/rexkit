package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// rowColumns is the Go-to row: glyph, name, context, status glyph, status
// word, age.
var rowColumns = Columns{
	{Width: 1},
	{Flex: true, Min: 18, Gap: 1},
	{Shrink: true, Min: 8, Max: 24, Gap: 2, Drop: 1},
	{Width: 1, Gap: 2},
	{Max: 8, Gap: 1, Drop: 3},
	{Max: 6, Gap: 2, Right: true, Drop: 2},
}

func TestColumnsDropOrder(t *testing.T) {
	natural := []int{1, 20, 16, 1, 7, 3}
	tests := []struct {
		w       int
		visible []bool
	}{
		{80, []bool{true, true, true, true, true, true}},
		{50, []bool{true, true, true, true, true, true}},  // context shrinks first
		{40, []bool{true, true, false, true, true, true}}, // names beat context
		{30, []bool{true, true, false, true, false, false}},
		{22, []bool{true, true, false, true, false, false}}, // status glyph stays
	}
	for _, tt := range tests {
		g := rowColumns.Fit(tt.w, natural)
		var got []bool
		for i := range rowColumns {
			got = append(got, g.Visible(i))
		}
		if !slices.Equal(got, tt.visible) {
			t.Errorf("width %d: visible %v, want %v (widths %v)", tt.w, got, tt.visible, g.widths)
		}
		if row := g.Row(); row.Width() != tt.w {
			t.Errorf("width %d: row is %d cells", tt.w, row.Width())
		}
	}
}

// The flex column takes the room left over, so the fixed columns sit at
// the right edge whatever the names are.
func TestColumnsPinRight(t *testing.T) {
	short := rowColumns.Fit(120, []int{1, 10, 8, 1, 7, 6})
	long := rowColumns.Fit(120, []int{1, 30, 8, 1, 7, 6})
	if short.total() != 120 || short.Width(1) != long.Width(1) {
		t.Errorf("name widths %d and %d, rows %d wide: the age is not at the right edge", short.Width(1), long.Width(1), short.total())
	}
}

// TestColumnsAlignWideRunes checks that CJK and emoji names keep later
// columns aligned.
func TestColumnsAlignWideRunes(t *testing.T) {
	names := []string{"zsh", "漢字 notes 📝", "👨\u200D👩\u200D👧 family", "e\u0301clair", "an-extremely-long-name-that-overflows"}
	rows := make([][]Line, len(names))
	for i, n := range names {
		rows[i] = []Line{Plain("·"), Plain(n), Plain("ctx"), Plain("●"), Plain("idle"), Plain("3h")}
	}
	for _, w := range []int{30, 45, 80} {
		g := rowColumns.Fit(w, Measure(rows, len(rowColumns)))
		col := -1
		for _, r := range rows {
			line := g.Row(r...)
			plain := line.String()
			if line.Width() != w {
				t.Fatalf("width %d: row %q is %d cells", w, plain, line.Width())
			}
			idx := strings.Index(plain, "●")
			at := ansi.StringWidth(plain[:idx])
			if col >= 0 && at != col {
				t.Errorf("width %d: status column at %d, earlier rows at %d: %q", w, at, col, plain)
			}
			col = at
		}
	}
}

func TestArrange(t *testing.T) {
	tests := []struct {
		w, h  int
		pref  PreviewPref
		share int
		card  bool
		mode  PreviewMode
		listW int // checked in PreviewSide mode
		tray  int
	}{
		{160, 40, PreviewAuto, 0, false, PreviewSide, 70, TrayAirRows},
		{130, 40, PreviewAuto, 55, true, PreviewSide, 71, TrayRows},
		{200, 40, PreviewAuto, 55, false, PreviewSide, MaxListCols, TrayAirRows},
		{160, 40, PreviewOff, 0, false, PreviewNone, 0, TrayAirRows},
		{110, 30, PreviewAuto, 0, false, PreviewStacked, 0, TrayAirRows},
		{110, 18, PreviewAuto, 0, false, PreviewNone, 0, TrayRows},
		{110, 18, PreviewOn, 0, false, PreviewSplit, 0, TrayRows},
		{70, 24, PreviewAuto, 0, true, PreviewNone, 0, TrayRows},
		{70, 24, PreviewOn, 0, false, PreviewSheet, 0, TrayAirRows},
		{70, 12, PreviewAuto, 0, false, PreviewNone, 0, 1}, // compact chrome
		{70, 6, PreviewOn, 0, false, PreviewNone, 0, 1},    // too short for a sheet
	}
	for _, tt := range tests {
		a := Arrange(tt.w, tt.h, LayoutOptions{Preview: tt.pref, ListShare: tt.share, Card: tt.card})
		if a.Mode != tt.mode {
			t.Errorf("%dx%d pref %d: mode %d, want %d", tt.w, tt.h, tt.pref, a.Mode, tt.mode)
		}
		header := 1
		if Banded(tt.w, tt.h) {
			header = HeaderRows
		}
		if a.Header != (Rect{0, 0, tt.w, header}) || a.Footer != (Rect{0, tt.h - tt.tray, tt.w, tt.tray}) {
			t.Errorf("%dx%d: chrome %v %v", tt.w, tt.h, a.Header, a.Footer)
		}
		if a.Body != (Rect{0, header, tt.w, tt.h - header - tt.tray}) {
			t.Errorf("%dx%d: body %v", tt.w, tt.h, a.Body)
		}
		if a.Mode == PreviewSide && a.List.W != tt.listW {
			t.Errorf("%dx%d share %d: side list width %d, want %d", tt.w, tt.h, tt.share, a.List.W, tt.listW)
		}
		switch a.Mode {
		case PreviewSide, PreviewSplit:
			if a.Preview.X != a.List.W+PreviewGap || a.Preview.X+a.Preview.W != tt.w {
				t.Errorf("%dx%d: preview %v beside list %v", tt.w, tt.h, a.Preview, a.List)
			}
		case PreviewStacked, PreviewSheet:
			if a.List.H+1+a.Preview.H != a.Body.H {
				t.Errorf("%dx%d: list %v and preview %v leave rows of the body %v", tt.w, tt.h, a.List, a.Preview, a.Body)
			}
		}
	}
}

func TestArrangeTinyNeverNegative(t *testing.T) {
	for w := 0; w < 20; w++ {
		for h := 0; h < 8; h++ {
			a := Arrange(w, h, LayoutOptions{Preview: PreviewOn, SubRow: true})
			for _, r := range []Rect{a.Header, a.Sub, a.Body, a.List, a.Preview, a.Footer} {
				if r.W < 0 || r.H < 0 || r.X+r.W > w || r.Y+r.H > h {
					t.Fatalf("%dx%d: bad rect %v in %+v", w, h, r, a)
				}
			}
		}
	}
}

func TestPreviewToggle(t *testing.T) {
	if PreviewAuto.Toggle(true) != PreviewOff || PreviewAuto.Toggle(false) != PreviewOn {
		t.Error("toggle")
	}
}

func TestCompose(t *testing.T) {
	got := Compose(10, 2,
		Part{Rect{0, 0, 3, 2}, []string{"abcdef", "x"}},
		Part{Rect{5, 0, 5, 1}, []string{"\x1b[31mred\x1b[0m"}},
		Part{Rect{12, 0, 3, 1}, []string{"off"}},
	)
	want := []string{"ab…  red  ", "x         "}
	for i := range got {
		if ansi.Strip(got[i]) != want[i] || ansi.StringWidth(got[i]) != 10 {
			t.Errorf("row %d = %q, want %q", i, ansi.Strip(got[i]), want[i])
		}
	}
}

func TestFloatFrame(t *testing.T) {
	tests := []struct {
		w, h         int
		carded       bool
		card, banded bool
	}{
		{100, 30, true, true, true},
		{100, 30, false, false, true},
		{70, 18, true, true, true},
		{70, 17, true, true, false}, // 15 rows inside the card
		{70, 16, false, false, true},
		{45, 20, true, true, false}, // 43 columns inside the card
		{3, 2, true, false, false},
	}
	for _, tt := range tests {
		f := FloatFrame(tt.w, tt.h, tt.carded)
		if f.Card != tt.card || f.Banded != tt.banded {
			t.Errorf("FloatFrame(%d, %d, %v) = %+v, want card %v banded %v", tt.w, tt.h, tt.carded, f, tt.card, tt.banded)
		}
	}
	if d := DialogFrame(56, DialogRows); !d.Card || !d.Banded {
		t.Errorf("a dialog of %d rows is a banded card: %+v", DialogRows, d)
	}
	if d := DialogFrame(56, DialogRows-1); d.Banded {
		t.Error("a shorter dialog is compact")
	}
}
