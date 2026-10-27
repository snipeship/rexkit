package find

import (
	"regexp"
	"slices"
	"testing"
)

func TestSensitive(t *testing.T) {
	tests := []struct {
		q    Query
		want bool
	}{
		{Query{Text: "panic"}, false},
		{Query{Text: "Panic"}, true},
		{Query{Text: "panic", MatchCase: true}, true},
		{Query{Text: `status=5\d\d`, Regex: true}, false},
		{Query{Text: `\D\S\W\B`, Regex: true}, false}, // escapes are not capitals
		{Query{Text: `\DX`, Regex: true}, true},
		{Query{Text: `\D`}, true}, // literal text: a capital is a capital
		// what an escape names is not typed in capitals
		{Query{Text: `error\pL`, Regex: true}, false},
		{Query{Text: `\p{L}+error`, Regex: true}, false},
		{Query{Text: `\P{Greek}`, Regex: true}, false},
		{Query{Text: `\x{1F600}k`, Regex: true}, false},
		{Query{Text: `\x4Ak`, Regex: true}, false},
		{Query{Text: `\x4AK`, Regex: true}, true},
		{Query{Text: `\pLX`, Regex: true}, true},
		{Query{Text: `(?P<Name>x)`, Regex: true}, false},
		{Query{Text: `(?<Name>x)Y`, Regex: true}, true},
		{Query{Text: `[A-Z]`, Regex: true}, true},
		{Query{Text: "Ärger"}, true},
		{Query{Text: "研究"}, false},
	}
	for _, tt := range tests {
		if got := tt.q.Sensitive(); got != tt.want {
			t.Errorf("%+v.Sensitive() = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestCompile(t *testing.T) {
	tests := []struct {
		q     Query
		kind  kind
		line  string
		spans [][2]int
	}{
		{Query{Text: "panic"}, folded, "PANIC: Panic panic", [][2]int{{0, 5}, {7, 12}, {13, 18}}},
		{Query{Text: "Panic"}, exact, "PANIC: Panic panic", [][2]int{{7, 12}}},
		{Query{Text: "panic", MatchCase: true}, exact, "PANIC: Panic panic", [][2]int{{13, 18}}},
		{Query{Text: "aa"}, folded, "aaaaa", [][2]int{{0, 2}, {2, 4}}}, // matches do not overlap
		{Query{Text: "日本"}, folded, "x 日本 y", [][2]int{{2, 8}}},
		{Query{Text: "ärger"}, regex, "ÄRGER", [][2]int{{0, 6}}}, // folds beyond ASCII: Go's (?i)
		{Query{Text: `status=5\d\d`, Regex: true}, regex, "status=200 STATUS=503", [][2]int{{11, 21}}},
		{Query{Text: `x*`, Regex: true}, regex, "abxxc", [][2]int{{2, 4}}}, // empty matches are left out
		{Query{Text: `a.c`}, folded, "abc a.c", [][2]int{{4, 7}}},          // literal: no regex
	}
	for _, tt := range tests {
		m, err := tt.q.Compile()
		if err != nil {
			t.Fatalf("%+v: %v", tt.q, err)
		}
		if m.kind != tt.kind {
			t.Errorf("%+v: kind %d, want %d", tt.q, m.kind, tt.kind)
		}
		if got := m.Spans(tt.line); !slices.Equal(got, tt.spans) {
			t.Errorf("%+v in %q: spans %v, want %v", tt.q, tt.line, got, tt.spans)
		}
		if got := m.In(tt.line, Fold(tt.line)); got != (len(tt.spans) > 0) {
			t.Errorf("%+v in %q: In = %v", tt.q, tt.line, got)
		}
		if m.Folds() != (tt.kind == folded) || m.Slow() != (tt.kind == regex) {
			t.Errorf("%+v: Folds %v Slow %v", tt.q, m.Folds(), m.Slow())
		}
	}
	if m, err := (Query{}).Compile(); m != nil || err != nil || m.In("x", "x") || m.Spans("x") != nil {
		t.Errorf("empty query: %v %v", m, err)
	}
	if m, _ := (Query{Text: "x*", Regex: true}).Compile(); m.In("abc", "abc") {
		t.Error("a line with only empty matches matches")
	}
}

func TestCompileError(t *testing.T) {
	_, err := Query{Text: `status=(5\d\d`, Regex: true}.Compile()
	if err == nil {
		t.Fatal("no error")
	}
	if got, want := Reason(err), "Missing closing ): `status=(5\\d\\d`"; got != want {
		t.Errorf("Reason = %q, want %q (as typed, without (?i))", got, want)
	}
}

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "abc", "ABC xYz": "abc xyz", "ÄRGER Ö": "Ärger Ö"} {
		got := Fold(in)
		if got != want || len(got) != len(in) {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
	if !regexp.MustCompile(`^[a-z ]+$`).MatchString(Fold("HELLO World")) {
		t.Error("Fold leaves capitals")
	}
}

func TestVimPattern(t *testing.T) {
	tests := []struct {
		q    Query
		want string
		ok   bool
	}{
		{Query{Text: "panic"}, `\V\cpanic`, true},
		{Query{Text: "Panic"}, `\V\CPanic`, true},
		{Query{Text: `a\b.*[x]`}, `\V\ca\\b.*[x]`, true},
		{Query{Text: `status=5\d\d`, Regex: true}, `\V\cstatus=5\[0-9]\[0-9]`, true},
		{Query{Text: `^(GET|POST) /api`, Regex: true}, `\V\C\_^\%(GET\|POST\) /api`, true},
		{Query{Text: `ab+c*d?`, Regex: true}, `\V\cab\+c\*d\=`, true},
		{Query{Text: `(?:ab)+?x{2,3}y{2,}z{4}`, Regex: true}, `\V\c\%(ab\)\{-1,}x\{2,3}y\{2,}z\{4}`, true},
		{Query{Text: `\bid\b`, Regex: true}, `\V\c\%(\<\|\>\)id\%(\<\|\>\)`, true},
		{Query{Text: `[^a-c\]]x$`, Regex: true}, `\V\c\[^\]a-c]x\_$`, true},
		{Query{Text: `a\Bb`, Regex: true}, "", false},
		{Query{Text: `(?i)Error`, Regex: true}, "", false}, // (?i) in a query that matches case
		{Query{Text: `x(?-i:ab)`, Regex: true}, "", false},
		{Query{Text: `(`, Regex: true}, "", false},
		{Query{}, "", false},
	}
	for _, tt := range tests {
		got, ok := tt.q.Vim()
		if got != tt.want || ok != tt.ok {
			t.Errorf("%+v.Vim() = %q, %v; want %q, %v", tt.q, got, ok, tt.want, tt.ok)
		}
	}
}
