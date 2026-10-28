package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func lines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i)
	}
	return out
}

// wellText is a preview row's text without the well's ends.
func wellText(line string) string { return strings.Trim(ansi.Strip(line), " ▐▌") }

func TestPreviewRevisionGuard(t *testing.T) {
	var p Preview
	first := p.Begin()
	second := p.Begin()
	if p.Set(first, Plain("old"), lines(1)) {
		t.Fatal("a stale load was accepted")
	}
	if !p.Loading() {
		t.Fatal("should be loading")
	}
	if !p.Set(second, Plain("new"), lines(2)) || p.Loading() || p.Title.String() != "new" {
		t.Fatal("the current load was rejected")
	}
	p.Begin()
	if p.Title.String() != "new" || len(p.Lines) != 2 {
		t.Fatal("starting a load must keep the previous content")
	}
}

func TestPreviewScrollAndMarkers(t *testing.T) {
	th := NewTheme(true)
	var p Preview
	p.Set(p.Begin(), Plain("title"), lines(30))
	// Title, air, the well's top edge, 4 content rows, its bottom edge.
	out := p.View(th, 30, 8)
	checkSize(t, "preview", strings.Join(out, "\n"), 30, 8)
	first, last := func() string { return wellText(out[3]) }, func() string { return wellText(out[6]) }
	if !strings.Contains(last(), "▼ 26") {
		t.Errorf("bottom marker: %q", last())
	}
	p.Scroll(10, 8)
	out = p.View(th, 30, 8)
	if !strings.HasPrefix(first(), "line 10") || !strings.Contains(first(), "▲ 10") {
		t.Errorf("after scrolling: %q", first())
	}
	p.Scroll(1000, 8)
	if out = p.View(th, 30, 8); !strings.HasPrefix(last(), "line 29") {
		t.Errorf("scroll clamps at the end: %q", last())
	}
	p.Scroll(-1000, 8)
	if out = p.View(th, 30, 8); !strings.HasPrefix(first(), "line 0") {
		t.Errorf("scroll clamps at the start: %q", first())
	}
}

func TestPreviewTail(t *testing.T) {
	th := NewTheme(true)
	p := Preview{Follow: true}
	p.Set(p.Begin(), nil, lines(30))
	last := func() string { return strings.TrimSpace(ansi.Strip(p.View(th, 20, 4)[3])) }
	if got := last(); !strings.HasPrefix(got, "line 29") {
		t.Errorf("tail shows %q", got)
	}
	p.Scroll(-2, 4)
	if got := last(); !strings.HasPrefix(got, "line 27") {
		t.Errorf("scrolling up leaves the end: %q", got)
	}
	p.Refresh(p.Begin(), nil, lines(40))
	if got := last(); !strings.HasPrefix(got, "line 27") {
		t.Errorf("a refresh must keep the reader's place: %q", got)
	}
	p.Scroll(100, 4)
	p.Refresh(p.Begin(), nil, lines(50))
	if got := last(); !strings.HasPrefix(got, "line 49") {
		t.Errorf("back at the end, new output is followed: %q", got)
	}
	p.Scroll(-5, 4)
	p.Set(p.Begin(), nil, lines(10))
	if got := last(); !strings.HasPrefix(got, "line 9") {
		t.Errorf("a new subject starts at the end: %q", got)
	}
}

func TestPreviewEmpty(t *testing.T) {
	th := NewTheme(true)
	p := Preview{Empty: Plain("Empty file")}
	p.Set(p.Begin(), th.Meta("go.mod", "1.1 KB", "", "3h"), nil)
	out := p.View(th, 40, 6)
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, " go.mod · 1.1 KB · 3h") {
		t.Errorf("title: %q", got)
	}
	if got := wellText(out[3]); got != "Empty file" {
		t.Errorf("empty: %q", got)
	}
	checkSize(t, "empty", strings.Join(out, "\n"), 40, 6)
}

func TestPreviewRule(t *testing.T) {
	th := NewTheme(true)
	p := Preview{Rule: true}
	p.Set(p.Begin(), Plain("rexkit/"), lines(3))
	out := p.View(th, 30, 8)
	checkSize(t, "rule", strings.Join(out, "\n"), 30, 8)
	if got := ansi.Strip(out[1]); got != " "+strings.Repeat("─", 28)+" " {
		t.Errorf("rule row: %q", got)
	}
	if got := wellText(out[3]); got != "line 0" {
		t.Errorf("content under the rule: %q", got)
	}
	p.Rule = false
	if got := ansi.Strip(p.View(th, 30, 8)[1]); strings.TrimSpace(got) != "" {
		t.Errorf("no rule, air: %q", got)
	}
}

func TestPreviewGrid(t *testing.T) {
	th := NewTheme(true)
	p := Preview{Context: Plain("~/code/web"), Grid: []MetaRow{{"Directory", Plain("~/code/web")}, {"Program", Plain("codex")}}}
	p.Set(p.Begin(), Plain("Fix navbar"), lines(40))
	p.Right = Plain("2m")
	tall := p.View(th, 50, MetaGridMinRows)
	checkSize(t, "grid", strings.Join(tall, "\n"), 50, MetaGridMinRows)
	if got := ansi.Strip(tall[0]); !strings.HasPrefix(got, " Fix navbar") || !strings.HasSuffix(got, "2m ") {
		t.Errorf("title row: %q", got)
	}
	if got := ansi.Strip(tall[3]); got != " "+Fit("Directory", MetaLabelW)+Fit("~/code/web", 50-2-MetaLabelW)+" " {
		t.Errorf("grid row: %q", got)
	}
	r := p.ContentRect(50, MetaGridMinRows)
	if got := ansi.Strip(tall[r.Y]); !strings.HasPrefix(got[r.X:], "line 0") && !strings.Contains(got, " line 0") {
		t.Errorf("content rect %v: row %q", r, got)
	}
	short := p.View(th, 50, MetaGridMinRows-1)
	if strings.Contains(ansi.Strip(strings.Join(short, "\n")), "Directory") {
		t.Error("a shorter preview drops the grid")
	}
}

func TestKeyPanel(t *testing.T) {
	th := NewTheme(true)
	groups := []KeyGroup{
		{"Navigate", []Binding{{[]string{"up", "down"}, "move"}, {[]string{"enter"}, "open"}}},
		{"View", []Binding{{[]string{"ctrl+/"}, "preview"}, {[]string{"?"}, "keys"}}},
	}
	wide, at, _ := th.KeyPanel(groups, 120, 20, PanelCorner, 0)
	// Rows: the top edge, the heading, air, then the group labels.
	if row := ansi.Strip(wide[3]); !strings.Contains(row, "NAVIGATE") && !strings.Contains(row, "Navigate") || !strings.Contains(strings.ToLower(row), "view") {
		t.Errorf("a wide panel has two columns: %q", row)
	}
	if at.X+at.W != 119 || at.Y+at.H != 20 {
		t.Errorf("anchored bottom-right, its right end at column w-2: %v", at)
	}
	narrow, _, _ := th.KeyPanel(groups, 50, 20, PanelCorner, 0)
	if strings.Contains(strings.ToLower(ansi.Strip(narrow[3])), "view") {
		t.Errorf("a narrow panel has one column: %q", ansi.Strip(narrow[3]))
	}
	if row := ansi.Strip(wide[1]); !strings.Contains(row, "Keys") || !strings.Contains(row, "▐esc▌") {
		t.Errorf("heading: %q", row)
	}
	for w := 0; w < 60; w += 3 {
		for h := 0; h < 14; h += 2 {
			box, at, _ := th.KeyPanel(groups, w, h, PanelCorner, 0)
			if box == nil {
				continue
			}
			if len(box) != at.H || at.X < 0 || at.Y < 0 || at.X+at.W > w || at.Y+at.H > h {
				t.Fatalf("%s: panel %v of %d lines", size(w, h), at, len(box))
			}
			for _, l := range box {
				if ansi.StringWidth(l) != at.W {
					t.Fatalf("%s: panel line %d cells, want %d", size(w, h), ansi.StringWidth(l), at.W)
				}
			}
		}
	}
}

// A panel too short for its keys sets them in two narrower columns when it
// spans the body, then drops its blank rows, and last scrolls, with the
// rows it hides counted on its last row.
func TestKeyPanelCutShort(t *testing.T) {
	th := NewTheme(true)
	var groups []KeyGroup
	for _, title := range []string{"Open", "Browse", "Show"} {
		g := KeyGroup{Title: title}
		for i := range 5 {
			g.Bindings = append(g.Bindings, Binding{[]string{"ctrl+" + string(rune('a'+i))}, title + " a thing"})
		}
		groups = append(groups, g)
	}
	text := func(box []string) string { return ansi.Strip(strings.Join(box, "\n")) }

	// 66 cells is under KeyPanelTwoCols, but one column would be cut.
	box, _, more := th.KeyPanel(groups, 66, 20, PanelSpan, 0)
	if more != 0 || !strings.Contains(text(box), "Show") || !strings.Contains(ansi.Strip(box[2]), "Browse") && !strings.Contains(ansi.Strip(box[3]), "Browse") {
		t.Errorf("two narrow columns show every key:\n%s", text(box))
	}
	if _, _, more := th.KeyPanel(groups, 66, 20, PanelCorner, 0); more == 0 {
		t.Error("a corner panel stays one column (it keeps off the list) and scrolls")
	}

	// Too short for even that: it scrolls, ▼ counts what is below.
	box, at, more := th.KeyPanel(groups, 66, 10, PanelSpan, 0)
	if more <= 0 || len(box) != at.H {
		t.Fatalf("a short panel scrolls: more=%d, %d lines for %v", more, len(box), at)
	}
	if last := ansi.Strip(box[len(box)-2]); !strings.Contains(last, GlyphDown+" "+strconv.Itoa(more)) || strings.Contains(last, GlyphUp) {
		t.Errorf("at the top the last row counts the rows below: %q", last)
	}
	if strings.TrimSpace(ansi.Strip(box[2])) == "▐ ▌" {
		t.Errorf("a cut panel keeps no blank row under its title:\n%s", text(box))
	}
	end, _, _ := th.KeyPanel(groups, 66, 10, PanelSpan, more+5)
	if last := ansi.Strip(end[len(end)-2]); !strings.Contains(last, GlyphUp+" "+strconv.Itoa(more)) || strings.Contains(last, GlyphDown) {
		t.Errorf("scrolled to its end (clamped) the last row counts the rows above: %q", last)
	}
	if !strings.Contains(text(end), "Show a thing") {
		t.Errorf("scrolled to its end, the last group shows:\n%s", text(end))
	}

	for _, tt := range []struct {
		key      string
		from, to int
		ok       bool
	}{
		{"down", 0, 1, true}, {"down", 3, 3, true}, {"up", 0, 0, true}, {"up", 2, 1, true},
		{"pgdown", 0, 3, true}, {"pgup", 2, 0, true}, {"enter", 1, 1, false},
	} {
		if to, ok := ScrollKeyPanel(tt.from, 3, tt.key); to != tt.to || ok != tt.ok {
			t.Errorf("ScrollKeyPanel(%d, 3, %q) = %d, %v; want %d, %v", tt.from, tt.key, to, ok, tt.to, tt.ok)
		}
	}
	if _, ok := ScrollKeyPanel(0, 0, "down"); ok {
		t.Error("a panel that shows every key does not scroll")
	}
}

// Raise shades the cells around the panel and keeps the base elsewhere.
func TestRaise(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := NewTheme(dark)
		base := make([]string, 16)
		for i := range base {
			base[i] = Paint(strings.Repeat(" ", 80), Clear) // the float
		}
		panel, at, _ := th.KeyPanel([]KeyGroup{{"Go", []Binding{{[]string{"enter"}, "go"}}}}, 80, 16, PanelCorner, 0)
		out := th.Raise(base, panel, at)
		checkSize(t, "raise", strings.Join(out, "\n"), 80, 16)
		if cellsWithBg(t, out[0], Clear) != 80 {
			t.Errorf("dark=%v: the row far above the panel is shaded", dark)
		}
		if left := ansi.Cut(out[at.Y+2], 0, at.X); cellsWithBg(t, left, Clear) == at.X {
			t.Errorf("dark=%v: no shadow beside the panel", dark)
		}
		if got := ansi.Strip(out[at.Y+1]); !strings.Contains(got, "Keys") {
			t.Errorf("dark=%v: the panel is not drawn: %q", dark, got)
		}
		side := []rune(ansi.Strip(out[at.Y+2]))[at.X-1]
		if !dark && side != '▕' {
			t.Errorf("light: left of the panel is %q, want the hairline", side)
		}
		// The shadow falls left and below, strongest beside the panel;
		// right of it is plain float.
		if right := ansi.Cut(out[at.Y+2], at.X+at.W, 80); cellsWithBg(t, right, Clear) != 80-at.X-at.W {
			t.Errorf("dark=%v: right of the panel is shaded: %q", dark, ansi.Strip(right))
		}
		next, far := shadeOf(t, th, out[at.Y+3], at.X-1), shadeOf(t, th, out[at.Y+3], at.X-6)
		if next <= far {
			t.Errorf("dark=%v: the shade beside the panel (%.3f) is not the strongest (%.3f further out)", dark, next, far)
		}
		// In light the panel's sides are full cells, so the hairline
		// beside them touches its edge; its top lip shows a contact line.
		if end := []rune(ansi.Strip(out[at.Y+2]))[at.X]; !dark && end != ' ' {
			t.Errorf("light: the panel's side is %q, want a full cell", end)
		}
		if lip := shadeOf(t, th, out[at.Y], at.X+at.W/2); !dark && lip < 1.01 {
			t.Errorf("light: no contact line on the panel's top lip (%.3f)", lip)
		}
		// Left of the bottom lip the shade ends where the panel does:
		// its lower part, past the panel's edge, is plain card.
		if got := []rune(ansi.Strip(out[at.Y+at.H-1]))[at.X-1]; got != []rune(Shapes().edges(ProfileControl, false).run)[0] {
			t.Errorf("dark=%v: left of the bottom lip is %q, want the lip's split", dark, got)
		}
	}
}

// A panel that spans the body blanks the gutter beside it: a row's marks
// there do not show beside the panel.
func TestRaiseSpanGutter(t *testing.T) {
	th := NewTheme(true)
	base := make([]string, 16)
	for i := range base {
		base[i] = Paint(Line{{GlyphCurrent, th.Muted}}.Fit(60).Render(), th.C.Surface)
	}
	panel, at, _ := th.KeyPanel([]KeyGroup{{"Go", []Binding{{[]string{"enter"}, "go"}}}}, 60, 16, PanelSpan, 0)
	out := th.Raise(base, panel, at)
	for y := at.Y; y < at.Y+at.H; y++ {
		if strings.Contains(ansi.Strip(out[y]), GlyphCurrent) {
			t.Errorf("row %d keeps its gutter mark beside the panel: %q", y, ansi.Strip(out[y]))
		}
	}
	if !strings.Contains(ansi.Strip(out[at.Y-1]), GlyphCurrent) {
		t.Error("a row above the panel lost its mark")
	}
}

// Raise blanks a wide character the panel's edge cuts in half, and shades
// one beside it whole: the row keeps its width and its other characters.
func TestRaiseWide(t *testing.T) {
	th := NewTheme(true)
	base := make([]string, 16)
	for i := range base {
		base[i] = Paint(Fit(strings.Repeat("要", 40), 80), th.C.Surface)
	}
	panel, at, _ := th.KeyPanel([]KeyGroup{{"Go", []Binding{{[]string{"enter"}, "go"}}}}, 80, 16, PanelCorner, 0)
	if at.X%2 == 0 {
		t.Fatalf("panel at column %d: the test wants it to cut a wide character", at.X)
	}
	out := th.Raise(base, panel, at)
	checkSize(t, "raise wide", strings.Join(out, "\n"), 80, 16)
	for y := at.Y + 1; y < at.Y+at.H-1; y++ {
		left := ansi.Strip(ansi.Cut(out[y], 0, at.X))
		if want := strings.Repeat("要", at.X/2) + " "; left != want {
			t.Fatalf("row %d left of the panel: %q, want %q", y, left, want)
		}
	}
}

func TestEmptyState(t *testing.T) {
	th := NewTheme(true)
	ways := []Hint{{Key: "esc", Label: "close"}}
	tall := th.EmptyState(Plain("No matches"), Plain("Try fewer letters."), ways, 40, 12)
	checkSize(t, "empty", strings.Join(tall, "\n"), 40, 12)
	if got := strings.TrimSpace(ansi.Strip(tall[3])); got != "No matches" {
		t.Errorf("title a third down: %q", got)
	}
	if got := strings.TrimSpace(ansi.Strip(tall[7])); got != "▐esc▌ close" {
		t.Errorf("ways: %q", got)
	}
	short := th.EmptyState(Plain("No matches"), Plain("Try fewer letters."), ways, 40, EmptyStateMinRows-1)
	if got := strings.TrimSpace(ansi.Strip(short[0])); got != "No matches · ▐esc▌ close" {
		t.Errorf("one-liner: %q", got)
	}
	typed := th.Quoted("No matches for ", "a\x1bb")
	if got := typed.String(); got != "No matches for “a␛b”" {
		t.Errorf("Quoted = %q", got)
	}
	short = th.EmptyState(typed, Plain("Try fewer letters."), ways, 40, EmptyStateMinRows-1)
	if got := strings.TrimSpace(ansi.Strip(short[0])); got != "No matches for “a␛b” · ▐esc▌ close" {
		t.Errorf("quoted one-liner: %q", got)
	}
	short = th.EmptyState(typed, nil, ways, 26, EmptyStateMinRows-1) // no room for any way
	if got := strings.TrimSpace(ansi.Strip(short[0])); got != "No matches for “a␛b”" {
		t.Errorf("one-liner without room for its ways: %q, want no dot", got)
	}
	errs := th.ErrorState("Could not read Rex", "Rex is not running", ways, 40, 12)
	if got := strings.TrimSpace(ansi.Strip(errs[3])); got != GlyphFailed+" Could not read Rex" {
		t.Errorf("error title: %q", got)
	}
}

func TestPreviewDecorate(t *testing.T) {
	th := NewTheme(true)
	var p Preview
	p.Set(p.Begin(), Plain("title"), lines(1000))
	var drawn []int
	p.Decorate = func(i int, line string) string {
		drawn = append(drawn, i)
		return "#" + line
	}
	p.Scroll(500, 8)
	out := p.View(th, 30, 8)
	// Only the four lines shown are decorated, as they are drawn.
	if len(drawn) != 4 || drawn[0] != 500 {
		t.Errorf("decorated %v, want lines 500-503", drawn)
	}
	if first := wellText(out[3]); !strings.HasPrefix(first, "#line 500") {
		t.Errorf("first row %q", first)
	}
	drawn = nil
	p.View(th, 30, 3) // flat
	if len(drawn) == 0 {
		t.Error("a flat preview is not decorated")
	}
}
