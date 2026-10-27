package hints

import (
	"strings"
	"testing"
)

func TestLabelsPrefixFree(t *testing.T) {
	for _, n := range []int{1, 9, 26, 27, 33, 120, 676, 700} {
		ls := Labels(n, Alphabet)
		if len(ls) != n {
			t.Fatalf("Labels(%d) gave %d", n, len(ls))
		}
		seen := map[string]bool{}
		for _, a := range ls {
			if seen[a] {
				t.Fatalf("Labels(%d): %q twice", n, a)
			}
			seen[a] = true
			for _, b := range ls {
				if a != b && strings.HasPrefix(b, a) {
					t.Fatalf("Labels(%d): %q starts %q", n, a, b)
				}
			}
		}
		if n <= len(Alphabet) && ls[0] != "a" {
			t.Errorf("Labels(%d) starts with %q, want a", n, ls[0])
		}
	}
	// Past the singles, the pairs start with the least easy letters: the
	// home row stays single.
	if ls := Labels(30, Alphabet); ls[0] != "a" || !strings.HasPrefix(ls[29], "y") {
		t.Errorf("Labels(30) = %v", ls)
	}
}

func TestAssignNearestFirstAndShared(t *testing.T) {
	ms := []Match{
		{Row: 0, Col: 0, Text: "far"},
		{Row: 8, Col: 4, Text: "near"},
		{Row: 2, Col: 0, Text: "near"},
		{Row: 9, Col: 0, Text: "nearest"},
	}
	Assign(ms, 9, 0)
	if ms[3].Label != "a" || ms[1].Label != "s" || ms[2].Label != "s" || ms[0].Label != "d" {
		t.Errorf("labels %q %q %q %q, want d s s a", ms[0].Label, ms[1].Label, ms[2].Label, ms[3].Label)
	}
}

// TestPlace: a label goes in the cells before its match, taking the end of
// the text there when the blank cells are too few; it covers the first cells
// of the match only where nothing before it can be taken, never another
// match.
func TestPlace(t *testing.T) {
	c := cells(plainScreen(
		"a3f9c21 Drain the queue",
		"  M internal/runs/pool.go",
		"   9a8d3e6..c2b77f0  main",
		"listening on 10.42.3.17:8080",
		"漢a3f9c21 x",
	), 40, 5)
	ms := Find(c)
	for i := range ms {
		ms[i].Label = "a"
		switch ms[i].Text {
		case "10.42.3.17:8080", "c2b77f0":
			ms[i].Label = "yf"
		}
	}
	place(ms, c)
	want := map[[2]int]int{
		{0, 0}:  0,  // a3f9c21 at column 0: over the match
		{1, 4}:  3,  // internal/runs/pool.go: the blank cell before it
		{2, 3}:  2,  // 9a8d3e6: the cell right before it
		{2, 12}: 10, // c2b77f0, two letters: over the ".."
		{3, 13}: 11, // 10.42.3.17:8080, two letters: the blank and the "n" before it
		{4, 2}:  2,  // a3f9c21 after a wide glyph: over the match, not half the glyph
	}
	for _, m := range ms {
		at := [2]int{m.Row, m.Col}
		if w, ok := want[at]; ok && m.Chip != w {
			t.Errorf("%q at %v: chip at %d, want %d", m.Text, at, m.Chip, w)
		}
		delete(want, at)
	}
	if len(want) > 0 {
		t.Errorf("not found: %v", want)
	}
	// Two matches one cell apart: the second chip takes that cell and its
	// own match's first, not the first match.
	c = cells(plainScreen("a3f9c21 b4e8d10"), 20, 1)
	ms = Find(c)
	ms[0].Label, ms[1].Label = "ab", "cd"
	place(ms, c)
	if ms[1].Chip != 7 {
		t.Errorf("chip at %d, want 7", ms[1].Chip)
	}
}
