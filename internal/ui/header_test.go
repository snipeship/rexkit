package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// cellText returns the plain text of a rendered line at rect r.
func cellText(line string, r Rect) string {
	return ansi.Strip(ansi.Cut(line, r.X, r.X+r.W))
}

func filesHeader() Header {
	return Header{
		Crumbs: []string{"~", "Coding", "snipeship", "rexkit"},
		Scope:  "studio › web",
		Query:  Query{Value: "mod", Cursor: 3, Focused: true},
		Chips: []Chip{
			{Kind: ChipToggle, Label: "all files", Key: "ctrl+f"},
			{Kind: ChipToggle, Label: "hidden", Key: "alt+.", On: true},
		},
	}
}

func TestHeaderHitsMatchText(t *testing.T) {
	th := NewTheme(true)
	h := filesHeader()
	compact, compactHits := th.Header(h, 120)
	bandLines, bandHits := th.HeaderBand(h, 140)
	tests := []struct {
		name         string
		line         string
		hits         HeaderHits
		scope, query string
	}{
		{"compact", compact, compactHits, "in studio › web ×", GlyphPrompt + " mod"},
		{"band", bandLines[1], bandHits, "▐ in studio › web × ▌", "▐ mod"},
	}
	for _, tt := range tests {
		for i, c := range h.Crumbs {
			if got := cellText(tt.line, tt.hits.Crumbs[i]); got != c {
				t.Errorf("%s: crumb %d rect %v shows %q, want %q", tt.name, i, tt.hits.Crumbs[i], got, c)
			}
		}
		if got := cellText(tt.line, tt.hits.Scope); got != tt.scope {
			t.Errorf("%s: scope rect shows %q", tt.name, got)
		}
		for i, c := range h.Chips {
			if got := cellText(tt.line, tt.hits.Chips[i]); !strings.Contains(got, c.Label) {
				t.Errorf("%s: chip %d rect %v shows %q", tt.name, i, tt.hits.Chips[i], got)
			}
		}
		if got := cellText(tt.line, Rect{tt.hits.Cursor, 0, 1, 1}); got != GlyphCaret {
			t.Errorf("%s: cursor column %d shows %q, want the caret", tt.name, tt.hits.Cursor, got)
		}
		if got := cellText(tt.line, tt.hits.Query); !strings.HasPrefix(got, tt.query) {
			t.Errorf("%s: query rect shows %q", tt.name, got)
		}
	}
}

// The band's edge rows sit over exactly the shapes of its text row.
func TestHeaderBandShapes(t *testing.T) {
	th := NewTheme(true)
	lines, hits := th.HeaderBand(filesHeader(), 140)
	if len(lines) != HeaderRows {
		t.Fatalf("%d rows", len(lines))
	}
	// the field, the scope and the one toggle that is on
	if len(hits.Shapes) != 3 {
		t.Fatalf("%d shapes: %+v", len(hits.Shapes), hits.Shapes)
	}
	for _, s := range hits.Shapes {
		row := []rune(ansi.Strip(lines[1]))
		if row[s.X] != '▐' || row[s.X+s.W()-1] != '▌' {
			t.Errorf("shape at %d+%d is not between ends: %q", s.X, s.W(), string(row[s.X:s.X+s.W()]))
		}
		for _, y := range []int{0, 2} {
			edge := []rune(ansi.Strip(lines[y]))
			if edge[s.X] == ' ' || edge[s.X+s.W()/2] == ' ' {
				t.Errorf("row %d: no edge over the shape at %d", y, s.X)
			}
		}
	}
	if strings.TrimSpace(ansi.Strip(lines[3])) != "" {
		t.Error("the last row is air")
	}
}

func TestHeaderPlaceholderNeverUnderCursor(t *testing.T) {
	th := NewTheme(true)
	for _, real := range []bool{false, true} {
		q := Query{Focused: true, Placeholder: "Search files", RealCursor: real}
		line, hits := th.Header(Header{Query: q}, 60)
		under := cellText(line, Rect{hits.Cursor, 0, 1, 1})
		if under == "S" {
			t.Errorf("real=%v: placeholder starts under the cursor: %q", real, ansi.Strip(line))
		}
		if !strings.Contains(ansi.Strip(line), "Search files") {
			t.Errorf("real=%v: placeholder missing: %q", real, ansi.Strip(line))
		}
	}
}

func TestHeaderDegradesGracefully(t *testing.T) {
	th := NewTheme(true)
	files := filesHeader()
	files.Scope = ""
	files.Title = "Files"
	files.Info = Line{{GlyphBranch + " main", th.Muted}}
	goTo := Header{
		Title: "Go to",
		Scope: "studio › web",
		Query: Query{Value: "mod", Cursor: 3, Focused: true},
		Chips: []Chip{{Kind: ChipTab, Label: "All", On: true}, {Kind: ChipTab, Label: "Agents", Badge: "2"}},
	}
	tests := []struct {
		name string
		h    Header
		keep []string // visible at every width ≥ 40
		wide []string // visible at 140
	}{
		{"files", files, []string{"rexkit", "mod"}, []string{"Files", "~ › Coding › snipeship › rexkit", "⎇ main", "⌃F"}},
		{"go to", goTo, []string{"web ×", "mod"}, []string{"Go to", "in studio › web ×", "Agents²"}},
	}
	for _, tt := range tests {
		for w := 0; w <= 140; w++ {
			line, hits := th.Header(tt.h, w)
			band, bandHits := th.HeaderBand(tt.h, w)
			for _, l := range band {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("%s width %d: band row is %d cells: %q", tt.name, w, got, ansi.Strip(l))
				}
			}
			if w >= 50 && bandHits.Query.W < minField {
				t.Errorf("%s width %d: band field squeezed to %d cells", tt.name, w, bandHits.Query.W)
			}
			if got := ansi.StringWidth(line); got != w {
				t.Fatalf("%s width %d: header is %d cells: %q", tt.name, w, got, ansi.Strip(line))
			}
			if w < 40 {
				continue
			}
			plain := ansi.Strip(line)
			for _, want := range tt.keep {
				if !strings.Contains(plain, want) {
					t.Errorf("%s width %d: lost %q: %q", tt.name, w, want, plain)
				}
			}
			if hits.Query.W < minQuery {
				t.Errorf("%s width %d: query squeezed to %d cells", tt.name, w, hits.Query.W)
			}
		}
		plain := ansi.Strip(must(th.Header(tt.h, 140)))
		for _, want := range tt.wide {
			if !strings.Contains(plain, want) {
				t.Errorf("%s: wide header lacks %q: %q", tt.name, want, plain)
			}
		}
	}
}

func must(s string, _ HeaderHits) string { return s }

func TestQueryScrollsToCursor(t *testing.T) {
	th := NewTheme(true)
	long := "a-very-long-query-that-does-not-fit-at-all"
	tests := []struct {
		cursor int
		want   string // must be visible next to the caret
	}{
		{len(long), "at-all" + GlyphCaret},
		{0, GlyphCaret + "a-very"},
		{20, "th" + GlyphCaret + "at"},
	}
	for _, tt := range tests {
		line, x := th.Query(Query{Value: long, Cursor: tt.cursor, Focused: true}, 20, true)
		plain := line.String()
		if line.Width() != 20 || !strings.Contains(plain, tt.want) {
			t.Errorf("cursor %d: %q (width %d), want it to contain %q", tt.cursor, plain, line.Width(), tt.want)
		}
		if got := ansi.Cut(plain, x, x+1); got != GlyphCaret {
			t.Errorf("cursor %d: column %d holds %q", tt.cursor, x, got)
		}
	}
	if _, x := th.Query(Query{Value: "x"}, 20, false); x != -1 {
		t.Error("an unfocused field reports no cursor")
	}
}

func TestNewInputHasNoChrome(t *testing.T) {
	in := NewInput()
	if in.Prompt != "" || in.Placeholder != "" || in.VirtualCursor() || !in.Focused() {
		t.Errorf("NewInput: prompt=%q placeholder=%q virtual=%v focused=%v", in.Prompt, in.Placeholder, in.VirtualCursor(), in.Focused())
	}
	in.SetValue("héllo")
	q := QueryOf(in, "Search")
	if q.Value != "héllo" || q.Cursor != 5 || !q.Focused || q.Placeholder != "Search" {
		t.Errorf("QueryOf = %+v", q)
	}
}

// The field keeps its size whichever chip is active, while a toggle is on
// and before a badge's count arrives: nothing in the header moves. (Two
// shapes side by side need air of their own: two toggles on at once take
// two cells more.)
func TestHeaderFieldKeepsItsSize(t *testing.T) {
	th := NewTheme(true)
	tabs := func(active int, badge string) []Chip {
		var chips []Chip
		for i, label := range []string{"All", "Sessions", "Agents"} {
			chips = append(chips, Chip{Kind: ChipTab, Label: label, On: i == active})
		}
		chips[2].Badged, chips[2].Badge = true, badge
		return chips
	}
	toggles := func(all, hidden bool) []Chip {
		return []Chip{
			{Kind: ChipToggle, Label: "all files", Key: "ctrl+f", On: all},
			{Kind: ChipToggle, Label: "hidden", Key: "alt+.", On: hidden},
		}
	}
	for _, states := range [][][]Chip{
		{tabs(0, ""), tabs(0, "2"), tabs(1, "2"), tabs(2, "")},
		{toggles(false, false), toggles(true, false), toggles(false, true)},
	} {
		want := -1
		for _, chips := range states {
			_, hits := th.HeaderBand(Header{Title: "Go to", Chips: chips}, 120)
			if want < 0 {
				want = hits.Query.W
			}
			if hits.Query.W != want {
				t.Errorf("%+v: field %d cells wide, want %d", chips, hits.Query.W, want)
			}
		}
	}
}

func TestChips(t *testing.T) {
	th := NewTheme(true)
	tests := []struct {
		chip Chip
		want string
		fill color.Color // nil: plain words
	}{
		{Chip{Kind: ChipToggle, Label: "hidden", Key: "alt+.", On: true}, "▐ ◉ hidden ⌥. ▌", th.C.SelBg},
		{Chip{Kind: ChipToggle, Label: "hidden", Key: "alt+."}, "○ hidden ⌥.", nil},
		{Chip{Kind: ChipTab, Label: "Agents", Badge: "5"}, "Agents⁵", nil},
		{Chip{Kind: ChipTab, Label: "Agents", Badged: true}, "Agents ", nil},
		{Chip{Kind: ChipTab, Label: "All", On: true}, "▐ All ▌", th.C.SelBg},
		{Chip{Kind: ChipScope, Label: "studio › web"}, "▐ in studio › web × ▌", th.C.Field},
		{Chip{Kind: ChipInfo, Line: Plain("⎇ main")}, "⎇ main", nil},
	}
	for _, tt := range tests {
		line := th.Chip(tt.chip)
		if line.String() != tt.want {
			t.Errorf("chip = %q, want %q", line.String(), tt.want)
		}
		if tt.fill == nil {
			continue
		}
		if got := cellsWithBg(t, line.Render(), tt.fill); got != line.Width()-2 {
			t.Errorf("%q: %d cells on the fill, want all but the ends", tt.want, got)
		}
	}
}
