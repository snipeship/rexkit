package sanitize

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestLabel(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "api-server", "api-server"},
		{"strips sgr", "\x1b[31mred\x1b[0m", "red"},
		{"strips osc title", "\x1b]0;evil\x07zsh", "zsh"},
		{"strips hyperlink", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"newline and tab", "a\nb\tc", "a␊b␉c"},
		{"nul and del", "a\x00b\x7f", "a␀b␡"},
		{"c1 control", "a\u009bb", "a<U+009B>b"},
		{"bidi override", "abc\u202Etxt.exe", "abc<U+202E>txt.exe"},
		{"zero width space", "a\u200Bb", "a<U+200B>b"},
		{"line separator", "a\u2028b", "a<U+2028>b"},
		{"zwj inside emoji kept", "👨\u200D👩\u200D👧", "👨\u200D👩\u200D👧"},
		{"lone zwj escaped", "\u200Dx", "<U+200D>x"},
		{"kitty placeholder", "x\U0010EEEE\u0305\u030Dy", "x<U+10EEEE>y"},
		{"noncharacter", "a\uFFFEb", "a<U+FFFE>b"},
		{"invalid utf8", "a\xffb", "a\uFFFDb"},
		{"cjk kept", "漢字.txt", "漢字.txt"},
		{"combining kept", "e\u0301", "e\u0301"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Label(tt.in); got != tt.want {
				t.Errorf("Label(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNameKeepsEscapeVisible(t *testing.T) {
	if got, want := Name("\x1b[31mred"), "␛[31mred"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestLabelCapsWidth(t *testing.T) {
	got := Label(strings.Repeat("漢", 400))
	if w := ansi.StringWidth(got); w > MaxLabel {
		t.Fatalf("width %d > %d", w, MaxLabel)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated label should end with an ellipsis: %q", got[len(got)-8:])
	}
}

func TestTerminalLines(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"plain", "hello", 0, []string{"hello"}},
		{"keeps sgr and resets", "\x1b[31mred\x1b[0m", 0, []string{"\x1b[31mred\x1b[0m\x1b[m"}},
		{"keeps colon sgr", "\x1b[38:2::1:2:3mx", 0, []string{"\x1b[38:2::1:2:3mx\x1b[m"}},
		{"drops private sgr", "\x1b[?1mx", 0, []string{"x"}},
		{"cuf becomes spaces", "a\x1b[3Cb", 0, []string{"a   b"}},
		{"cuf default 1", "a\x1b[Cb", 0, []string{"a b"}},
		{"cuf blanks have default bg", "\x1b[41ma\x1b[2Cb", 0, []string{"\x1b[41ma\x1b[m  \x1b[41mb\x1b[m"}},
		{"huge cuf bounded by width", "a\x1b[999999999999Cb", 10, []string{"a        …"}},
		{"huge trailing cuf", "a\x1b[999999999999C", 10, []string{"a         "}},
		{"tab to stop 8", "ab\tc", 0, []string{"ab      c"}},
		{"drops cursor moves", "a\x1b[2J\x1b[H\x1b[5;5Hb\x1b[Ac", 0, []string{"abc"}},
		{"drops modes", "\x1b[?1000h\x1b[?1049hx", 0, []string{"x"}},
		{"drops osc 52", "\x1b]52;c;ZXZpbA==\x07x", 0, []string{"x"}},
		{"drops hyperlink keeps text", "\x1b]8;;file:///etc\x1b\\t\x1b]8;;\x1b\\", 0, []string{"t"}},
		{"drops dcs", "\x1bP1$r0m\x1b\\x", 0, []string{"x"}},
		{"drops kitty graphics apc", "\x1b_Ga=T,f=100;AAAA\x1b\\x", 0, []string{"x"}},
		{"drops pm and sos", "\x1b^pm\x1b\\\x1bXsos\x1b\\x", 0, []string{"x"}},
		{"osc with newline does not leak", "\x1b]0;a\nb\x07c", 0, []string{"c"}},
		{"drops c0 and bell", "a\x07\x08\x00b", 0, []string{"ab"}},
		{"drops c1 rune", "a\u009b31mb", 0, []string{"a31mb"}},
		{"drops 8-bit csi", "a\x9b2Jb", 0, []string{"ab"}},
		{"drops bidi", "a\u202Eb", 0, []string{"ab"}},
		{"keeps zwj emoji", "👨\u200D👩\u200D👧", 0, []string{"👨\u200D👩\u200D👧"}},
		{"drops kitty placeholder", "a\U0010EEEE\u0305b", 0, []string{"ab"}},
		{"crlf lines", "a\r\nb\r\n", 0, []string{"a", "b"}},
		{"colour carried across lines", "\x1b[32ma\nb\x1b[m", 0, []string{"\x1b[32ma\x1b[m", "\x1b[32mb\x1b[m\x1b[m"}},
		{"truncates content with ellipsis", "abcdefgh", 5, []string{"abcd…"}},
		{"blank overflow cut silently", "abc\x1b[20C", 5, []string{"abc  "}},
		{"wide chars respect width", "漢字漢字", 5, []string{"漢字…"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TerminalLines(tt.in, tt.width)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("TerminalLines(%q, %d)\n got %q\nwant %q", tt.in, tt.width, got, tt.want)
			}
		})
	}
}

func TestTerminalLineJoins(t *testing.T) {
	if got := TerminalLine("a\nb", 0); got != "ab" {
		t.Errorf("TerminalLine = %q", got)
	}
}

func TestFileText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "package main\n", "package main\n"},
		{"crlf", "a\r\nb", "a\nb"},
		{"lone cr", "a\rb", "a␍b"},
		{"tabs", "\tx\ty", "    x   y"},
		{"escape shown", "\x1b[31mred", "␛[31mred"},
		{"nul", "a\x00b", "a␀b"},
		{"c1", "a\u0085b", "a\uFFFDb"},
		{"bidi", "a\u2067b", "a\uFFFDb"},
		{"bom dropped", "\uFEFFx", "x"},
		{"zwj kept in cluster", "👨\u200D👩\u200D👧", "👨\u200D👩\u200D👧"},
		{"invalid utf8", "a\xc3(", "a\uFFFD("},
		{"line separator", "a\u2028b", "a\uFFFDb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileText(tt.in); got != tt.want {
				t.Errorf("FileText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarkControls(t *testing.T) {
	got := MarkControls("\x1b[31ma␛b\x1b[m")
	if want := "\x1b[31ma\x1b[2m␛\x1b[22mb\x1b[m"; got != want {
		t.Errorf("MarkControls = %q, want %q", got, want)
	}
	if MarkControls("plain") != "plain" {
		t.Error("MarkControls changed a line without markers")
	}
}

var sgr = regexp.MustCompile(`^\x1b\[[0-9;:]*m`)

// checkDisplaySafe asserts the invariants every sanitizer output must hold.
func checkDisplaySafe(t *testing.T, in, out string, allowSGR bool) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Fatalf("invalid UTF-8 output for %q: %q", in, out)
	}
	for i := 0; i < len(out); {
		if out[i] == 0x1b {
			m := sgr.FindString(out[i:])
			if !allowSGR || m == "" {
				t.Fatalf("stray ESC in output for %q: %q", in, out)
			}
			i += len(m)
			continue
		}
		r, size := utf8.DecodeRuneInString(out[i:])
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || unicode.In(r, unicode.Zl, unicode.Zp) {
			t.Fatalf("control %U in output for %q: %q", r, in, out)
		}
		if unicode.Is(unicode.Bidi_Control, r) || r == kittyPlaceholder {
			t.Fatalf("hidden rune %U in output for %q: %q", r, in, out)
		}
		i += size
	}
}

func fuzzSeeds(f *testing.F) {
	for _, s := range []string{
		"", "plain", "\x1b[31mred\x1b[0m", "a\x1b[5Cb", "\x1b]52;c;AAAA\x07",
		"\x1b_Gf=100;AAAA\x1b\\", "\x1bP1$r\x1b\\", "a\tb\r\nc", "\x9b31m", "\u202E",
		"👨\u200D👩\u200D👧", "\U0010EEEE\u0305", "\x1b[38:2::1:2:3m", "\x1b[999999999C", "\xff\xfe",
	} {
		f.Add(s, 20)
	}
}

func FuzzTerminalLines(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, in string, width int) {
		width = width%200 + 1
		if width < 1 {
			width = -width + 1
		}
		for _, line := range TerminalLines(in, width) {
			checkDisplaySafe(t, in, line, true)
			if strings.Contains(line, "\n") {
				t.Fatalf("line contains a newline: %q", line)
			}
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d > %d for %q: %q", w, width, in, line)
			}
		}
	})
}

func FuzzLabel(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, in string, _ int) {
		for _, out := range []string{Label(in), Name(in)} {
			checkDisplaySafe(t, in, out, false)
			if w := ansi.StringWidth(out); w > MaxLabel {
				t.Fatalf("width %d > %d", w, MaxLabel)
			}
			if strings.ContainsAny(out, "\n\r\t") {
				t.Fatalf("label is not a single line: %q", out)
			}
		}
	})
}

func FuzzFileText(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, in string, _ int) {
		out := FileText(in)
		for _, line := range strings.Split(out, "\n") {
			checkDisplaySafe(t, in, line, false)
			checkDisplaySafe(t, in, MarkControls(line), true)
		}
	})
}

func TestNameSpansMarkEveryEscape(t *testing.T) {
	text, escapes := NameSpans("a\x1bb\u202ec\xffd")
	if text != "a␛b<U+202E>c�d" {
		t.Fatalf("text = %q", text)
	}
	var got []string
	for _, e := range escapes {
		got = append(got, text[e[0]:e[1]])
	}
	if want := []string{"␛", "<U+202E>", "�"}; !slices.Equal(got, want) {
		t.Errorf("escapes = %q, want %q", got, want)
	}
	if text, escapes := NameSpans("plain.txt"); text != "plain.txt" || escapes != nil {
		t.Errorf("plain = %q %v", text, escapes)
	}
	// A name that only looks like an escape has none.
	if _, escapes := NameSpans("bidi-<U+202E>cod.exe"); escapes != nil {
		t.Errorf("literal text marked as an escape: %v", escapes)
	}
	if text, escapes := LabelSpans("\x1b[31mred\x1b[0m\u200b!"); text != "red<U+200B>!" || len(escapes) != 1 || text[escapes[0][0]:escapes[0][1]] != "<U+200B>" {
		t.Errorf("label = %q %v", text, escapes)
	}
}

func TestNameSpansSurviveTruncation(t *testing.T) {
	long := strings.Repeat("x", MaxLabel-3) + "\u202e" + strings.Repeat("y", 50)
	text, escapes := NameSpans(long)
	if !strings.HasSuffix(text, "…") || ansi.StringWidth(text) > MaxLabel {
		t.Fatalf("not truncated: %d cells", ansi.StringWidth(text))
	}
	kept := len(strings.TrimSuffix(text, "…"))
	if len(escapes) != 1 || escapes[0][0] != MaxLabel-3 || escapes[0][1] > kept {
		t.Errorf("escapes = %v (kept %d bytes)", escapes, kept)
	}
	if got := text[escapes[0][0]:escapes[0][1]]; !strings.HasPrefix("<U+202E>", got) || got == "" {
		t.Errorf("clipped escape = %q", got)
	}
}
