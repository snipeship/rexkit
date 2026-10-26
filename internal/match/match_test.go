package match

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

func visible(texts ...string) []Field {
	out := make([]Field, len(texts))
	for i, t := range texts {
		out[i] = Field{Text: t}
	}
	return out
}

func TestPositions(t *testing.T) {
	tests := []struct {
		query, text string
		want        []int
	}{
		{"nav", "Navigation polish", []int{0, 1, 2}},
		{"nav", "Fix navbar overflow", []int{4, 5, 6}},
		{"gomod", "go.mod", []int{0, 1, 3, 4, 5}},
		{"mod", "internal/navigator/model.go", []int{19, 20, 21}},
		{"api", "~/code/api", []int{7, 8, 9}},
		{"fb", "foo_bar", []int{0, 4}},
		{"fb", "FooBar", []int{0, 3}},
		{"漢字", "a漢b字", []int{1, 3}},
		{"ss", "rex session", []int{4, 6}}, // the word start wins over the inner "ss",
	}
	for _, tt := range tests {
		t.Run(tt.query+"/"+tt.text, func(t *testing.T) {
			res, ok := Compile(tt.query).Match(visible(tt.text)...)
			if !ok {
				t.Fatalf("no match")
			}
			if !slices.Equal(res.Positions[0], tt.want) {
				t.Errorf("positions = %v, want %v", res.Positions[0], tt.want)
			}
		})
	}
}

func TestNoMatch(t *testing.T) {
	tests := []struct{ query, text string }{
		{"xyz", "Navigation"},
		{"navv", "nav"},
		{"Nav", "navigation"}, // smart case
		{"api main", "api"},   // every term must match
	}
	for _, tt := range tests {
		if _, ok := Compile(tt.query).Score(visible(tt.text)...); ok {
			t.Errorf("%q matched %q", tt.query, tt.text)
		}
	}
}

func TestRanking(t *testing.T) {
	tests := []struct {
		query         string
		better, worse string
	}{
		{"nav", "nav", "navigation"},                  // exact field
		{"nav", "navigation", "fix navbar"},           // prefix beats inner word
		{"nav", "fix navbar", "can avoid"},            // word beats scattered
		{"mod", "go.mod", "my old dir"},               // boundary run beats gaps
		{"gm", "go.mod", "gamma"},                     // boundaries beat consecutive mid-word
		{"rk", "rexkit", "r-----------------k"},       // shorter gap
		{"api", "api-server", "rapid"},                // word start beats inner
		{"code", "~/code/api", "c/o/d/e"},             // contiguous beats delimited singles
		{"sess", "session", "s e s s"},                // contiguous beats spaced
		{"fbr", "foo_bar_baz", "fxxxxxbxxxxxxrxxxxx"}, // word starts
	}
	for _, tt := range tests {
		p := Compile(tt.query)
		b, okb := p.Score(visible(tt.better)...)
		w, okw := p.Score(visible(tt.worse)...)
		if !okb || !okw {
			t.Errorf("%q: expected both %q and %q to match", tt.query, tt.better, tt.worse)
			continue
		}
		if b <= w {
			t.Errorf("%q: %q scored %d, not above %q at %d", tt.query, tt.better, b, tt.worse, w)
		}
	}
}

func TestMultiTermAcrossFields(t *testing.T) {
	p := Compile("api work")
	fields := []Field{
		{Text: "claude"},
		{Text: "studio › api"},
		{Text: "working", Hidden: true},
	}
	res, ok := p.Match(fields...)
	if !ok {
		t.Fatal("expected a match")
	}
	if res.Positions[0] != nil {
		t.Errorf("name should have no positions, got %v", res.Positions[0])
	}
	if !slices.Equal(res.Positions[1], []int{9, 10, 11}) {
		t.Errorf("context positions = %v", res.Positions[1])
	}
	if res.Via != 2 {
		t.Errorf("Via = %d, want 2 (hidden status field)", res.Via)
	}
}

func TestVisibleFieldPreferred(t *testing.T) {
	// "api" scores higher in the hidden cwd, but the visible name matches too:
	// highlight what the user can see and report no hidden hint.
	res, ok := Compile("api").Match(
		Field{Text: "rapid-ui"},
		Field{Text: "~/code/api", Hidden: true, Weight: 300},
	)
	if !ok || res.Via != -1 || res.Positions[0] == nil || res.Positions[1] != nil {
		t.Fatalf("got %+v ok=%v", res, ok)
	}
}

func TestHiddenFieldsAreStrict(t *testing.T) {
	hidden := Field{Text: "/Users/brain/Coding/snipeship", Hidden: true}
	if _, ok := Compile("bns").Score(Field{Text: "zsh"}, hidden); ok {
		t.Error("scattered letters should not match a hidden field")
	}
	if _, ok := Compile("snip").Score(Field{Text: "zsh"}, hidden); !ok {
		t.Error("a word should match a hidden field")
	}
}

func TestWeights(t *testing.T) {
	p := Compile("api")
	name, _ := p.Score(Field{Text: "api"})
	ctx, _ := p.Score(Field{Text: "api", Weight: 50})
	if ctx*2 != name {
		t.Errorf("weight 50 score %d, want half of %d", ctx, name)
	}
}

func TestEmptyPattern(t *testing.T) {
	p := Compile("   ")
	if !p.Empty() {
		t.Fatal("expected empty pattern")
	}
	if res, ok := p.Match(visible("x")...); !ok || res.Via != -1 {
		t.Errorf("empty pattern should match everything: %+v %v", res, ok)
	}
	got := p.Rank(3, func(i int) []Field { return visible("x") })
	if len(got) != 3 || got[0].Index != 0 || got[2].Index != 2 {
		t.Errorf("Rank = %v", got)
	}
}

func TestRankOrder(t *testing.T) {
	rows := []string{"navbar overflow", "can avoid", "nav", "unrelated", "Navigation"}
	got := Compile("nav").Rank(len(rows), func(i int) []Field { return visible(rows[i]) })
	var names []string
	for _, r := range got {
		names = append(names, rows[r.Index])
	}
	want := []string{"nav", "Navigation", "navbar overflow", "can avoid"}
	if !slices.Equal(names, want) {
		t.Errorf("Rank = %v, want %v", names, want)
	}
}

func TestLongTextFallsBackToGreedy(t *testing.T) {
	text := strings.Repeat("x", 2000) + "/api/" + strings.Repeat("y", 100) + "main.go"
	res, ok := Compile("api main").Match(visible(text)...)
	if !ok {
		t.Fatal("no match")
	}
	want := []int{2001, 2002, 2003, 2105, 2106, 2107, 2108}
	if !slices.Equal(res.Positions[0], want) {
		t.Errorf("positions = %v, want %v", res.Positions[0], want)
	}
}

// FuzzPositions checks that positions always point at the query's runes, in
// order, inside the field.
func FuzzPositions(f *testing.F) {
	f.Add("nav", "Navigation polish")
	f.Add("a b", "ab ba")
	f.Add("ẞ", "straße")
	f.Fuzz(func(t *testing.T, query, text string) {
		p := Compile(query)
		res, ok := p.Match(visible(text)...)
		if _, sok := p.Score(visible(text)...); sok != ok {
			t.Fatalf("Score and Match disagree for %q in %q", query, text)
		}
		if !ok || p.Empty() {
			return
		}
		runes := []rune(text)
		for _, at := range res.Positions[0] {
			if at < 0 || at >= len(runes) {
				t.Fatalf("position %d out of range for %q", at, text)
			}
		}
		if !slices.IsSorted(res.Positions[0]) {
			t.Fatalf("positions not sorted: %v", res.Positions[0])
		}
		for _, tm := range p.terms {
			pos := make([]int, len(tm.runes))
			if _, ok := p.s.align(tm, runes, pos, false); !ok {
				t.Fatalf("term %q did not align", string(tm.runes))
			}
			for i, at := range pos {
				if !tm.eq(runes[at], tm.runes[i]) || (i > 0 && at <= pos[i-1]) {
					t.Fatalf("bad alignment %v of %q in %q", pos, string(tm.runes), text)
				}
			}
		}
	})
}

func benchRows(n int) [][]Field {
	r := rand.New(rand.NewPCG(1, 2))
	words := []string{"api", "studio", "web", "claude", "codex", "zsh", "server", "navigation",
		"polish", "rexkit", "internal", "model", "view", "main", "go", "spark", "audit", "fix"}
	pick := func(k int) string {
		parts := make([]string, k)
		for i := range parts {
			parts[i] = words[r.IntN(len(words))]
		}
		return strings.Join(parts, "-")
	}
	rows := make([][]Field, n)
	for i := range rows {
		rows[i] = []Field{
			{Text: pick(2)},
			{Text: pick(1) + " › " + pick(1), Weight: 70},
			{Text: "/Users/brain/Coding/" + pick(3), Hidden: true},
		}
	}
	return rows
}

func BenchmarkRank10k(b *testing.B) {
	rows := benchRows(10_000)
	for _, q := range []string{"nav", "api main", "rexkit view", "zz"} {
		b.Run(q, func(b *testing.B) {
			p := Compile(q)
			for b.Loop() {
				p.Rank(len(rows), func(i int) []Field { return rows[i] })
			}
		})
	}
}

func TestRankIsFast(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test")
	}
	rows := benchRows(10_000)
	res := testing.Benchmark(func(b *testing.B) {
		p := Compile("api main")
		for b.Loop() {
			p.Rank(len(rows), func(i int) []Field { return rows[i] })
		}
	})
	if per := time.Duration(res.NsPerOp()); per > 10*time.Millisecond {
		t.Errorf("ranking 10k rows took %s, want < 10ms", per)
	}
}
