package ui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLineTruncate(t *testing.T) {
	tests := []struct {
		in    Line
		w     int
		want  string
		start string // TruncateStart
	}{
		{Plain("hello"), 10, "hello", "hello"},
		{Plain("hello world"), 8, "hello w…", "…o world"},
		{Line{{Text: "ab"}, {Text: "cdef"}}, 4, "abc…", "…def"},
		{Plain("漢字漢字"), 5, "漢字…", "…漢字"},
		{Plain("漢字漢字"), 4, "漢… ", " …字"}, // a wide rune that does not fit leaves a pad cell
		{Plain("👨\u200D👩\u200D👧 family"), 4, "👨\u200D👩\u200D👧 …", "…ily"},
		{Plain("abc"), 0, "", ""},
		{Plain("abcdef"), 1, "…", "…"},
	}
	for _, tt := range tests {
		got := tt.in.Truncate(tt.w).String()
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in.String(), tt.w, got, tt.want)
		}
		if w := ansi.StringWidth(got); w > tt.w {
			t.Errorf("Truncate(%q, %d) is %d cells", tt.in.String(), tt.w, w)
		}
		gotStart := tt.in.TruncateStart(tt.w).String()
		if gotStart != tt.start {
			t.Errorf("TruncateStart(%q, %d) = %q, want %q", tt.in.String(), tt.w, gotStart, tt.start)
		}
	}
}

func TestLineFitIsExact(t *testing.T) {
	for _, s := range []string{"", "a", "漢字 notes 📝", "e\u0301clair", strings.Repeat("x", 50)} {
		for w := 0; w < 20; w++ {
			l := Plain(s)
			for name, got := range map[string]Line{"Fit": l.Fit(w), "FitStart": l.FitStart(w), "FitRight": l.FitRight(w)} {
				if got.Width() != w {
					t.Errorf("%s(%q, %d) is %d cells", name, s, w, got.Width())
				}
			}
		}
	}
}

func TestLineDoesNotAliasInput(t *testing.T) {
	l := make(Line, 1, 4)
	l[0] = Span{Text: "abc"}
	_ = l.Fit(10)
	_ = l.trimEnd()
	if got := l[:cap(l)][1].Text; got != "" {
		t.Errorf("Fit wrote into the caller's backing array: %q", got)
	}
}

func TestRenderBgSpansEverySegment(t *testing.T) {
	th := NewTheme(true)
	line := Line{{"a", th.Accent}, {" mid ", th.Text}, {"b", th.Match}, Space(3)}
	got := line.RenderBg(th.C.SelBg)
	if n := cellsWithBg(t, got, th.C.SelBg); n != line.Width() {
		t.Errorf("%d of %d cells on the selection background: %q", n, line.Width(), got)
	}
	own := lipgloss.NewStyle().Background(th.C.Err)
	got = Line{{"x", own}}.RenderBg(th.C.SelBg)
	if cellsWithBg(t, got, th.C.SelBg) != 0 {
		t.Error("a span's own background must win over the row background")
	}
}

func TestTruncatePath(t *testing.T) {
	tests := []struct {
		path string
		w    int
		want string
	}{
		{"~/Coding/snipeship/rexkit", 30, "~/Coding/snipeship/rexkit"},
		{"~/Coding/snipeship/rexkit", 20, "…/snipeship/rexkit"},
		{"~/Coding/snipeship/rexkit", 10, "…/rexkit"},
		{"~/Coding/snipeship/rexkit", 5, "…xkit"},
		{"/a/very-long-directory-name", 8, "…ry-name"},
	}
	for _, tt := range tests {
		if got := TruncatePath(tt.path, tt.w); got != tt.want {
			t.Errorf("TruncatePath(%q, %d) = %q, want %q", tt.path, tt.w, got, tt.want)
		}
	}
}

func TestFitAnsi(t *testing.T) {
	got := Fit("\x1b[31mhello world\x1b[0m", 6)
	if w := ansi.StringWidth(got); w != 6 {
		t.Fatalf("width %d", w)
	}
	if !strings.HasSuffix(got, "\x1b[m") {
		t.Errorf("Fit must close attributes: %q", got)
	}
	if got := Fit("ab", 4); got != "ab  " {
		t.Errorf("Fit pads: %q", got)
	}
}

func TestHighlight(t *testing.T) {
	th := NewTheme(true)
	tests := []struct {
		text string
		pos  []int
		want []string // spans, matched ones in [brackets]
	}{
		{"navigation", []int{0, 1, 2}, []string{"[nav]", "igation"}},
		{"go.mod", []int{0, 3, 4, 5}, []string{"[g]", "o.", "[mod]"}},
		{"plain", nil, []string{"plain"}},
		{"a漢b字", []int{1, 3}, []string{"a", "[漢]", "b", "[字]"}},
		// A ZWJ family is one cluster: matching its second rune lights it whole.
		{"x👨\u200D👩\u200D👧y", []int{3}, []string{"x", "[👨\u200D👩\u200D👧]", "y"}},
		{"e\u0301te", []int{0}, []string{"[e\u0301]", "te"}},
	}
	for _, tt := range tests {
		var got []string
		for _, s := range Highlight(tt.text, tt.pos, th.Text, th.Match) {
			if s.Style.GetBold() {
				got = append(got, "["+s.Text+"]")
			} else {
				got = append(got, s.Text)
			}
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Highlight(%q, %v) = %q, want %q", tt.text, tt.pos, got, tt.want)
		}
	}
}

// Escapes in a sanitized name are drawn apart from the name's own text (in
// the subtle tier, never faint); a match inside one still shows as a match.
func TestHighlightEscaped(t *testing.T) {
	th := NewTheme(true)
	text := "a␛b<U+202E>c" // as sanitize.NameSpans("a\x1bb\u202ec") writes it
	escapes := [][2]int{{1, 4}, {5, 13}}
	render := func(l Line) []string {
		var got []string
		for _, s := range l {
			switch {
			case s.Style.GetBold():
				got = append(got, "["+s.Text+"]")
			case s.Style.GetFaint():
				t.Errorf("%q is drawn faint", s.Text) // tiers are real colours
			case s.Style.GetForeground() == th.Subtle.GetForeground():
				got = append(got, "("+s.Text+")")
			default:
				got = append(got, s.Text)
			}
		}
		return got
	}
	if got, want := render(th.Escaped(text, escapes, th.Text)), []string{"a", "(␛)", "b", "(<U+202E>)", "c"}; !slices.Equal(got, want) {
		t.Errorf("Escaped = %q, want %q", got, want)
	}
	got := render(HighlightEscaped(text, []int{0, 3}, escapes, th.Text, th.Match, th.EscapeStyle()))
	if want := []string{"[a]", "(␛)", "b", "[<]", "(U+202E>)", "c"}; !slices.Equal(got, want) {
		t.Errorf("HighlightEscaped = %q, want %q", got, want)
	}
}

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"~/Coding/snipeship/website", 30, "~/Coding/snipeship/website"},
		{"~/Coding/snipeship/website", 13, "~/Co…/website"},
		{"~/Coding/rexkit/docs", 8, "~/…/docs"},
		{"~/a/an-extremely-long-folder", 10, "…ng-folder"},
		{"plain", 0, ""},
	}
	for _, tt := range tests {
		if got := TruncateMiddle(tt.in, tt.w); got != tt.want {
			t.Errorf("TruncateMiddle(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}
