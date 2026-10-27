package scrollback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rexkit/internal/find"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
)

func TestNewPage(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		limit int
		want  string
	}{
		{"empty", "", 10, ""},
		{"plain text", "a\r\nb\r\n", 10, "a\nb\n"},
		{"colours kept, reset at line end", "\x1b[31mred\r\nstill\x1b[0m\r\n", 10,
			"\x1b[31mred\x1b[m\n\x1b[31mstill\x1b[0m\x1b[m\n"},
		{"blank cells become spaces", "a\x1b[3Cb\r\n", 10, "a   b\n"},
		{"tabs expand", "a\tb\r\n", 10, "a       b\n"},
		{"titles, links, cursor moves and modes dropped",
			"\x1b]0;title\x07\x1b]8;;file:///etc\x1b\\link\x1b]8;;\x1b\\\x1b[2J\x1b[H\x1b[?1049hok\r\n", 10, "linkok\n"},
		{"kitty graphics dropped", "\x1b_Gf=100;AAAA\x1b\\img\r\n", 10, "img\n"},
		{"bidi override dropped", "a\u202eb\r\n", 10, "ab\n"},
		{"trailing blank lines trimmed", "a\r\n\r\n   \r\n\x1b[0m\r\n", 10, "a\n"},
		{"blank lines inside kept", "a\r\n\r\nb\r\n", 10, "a\n\nb\n"},
		{"over the limit: newest kept, notice first", "1\r\n2\r\n3\r\n4\r\n5\r\n", 3,
			omitted(3) + "\n4\n5\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPage(tt.raw, tt.limit, 0)
			if p.text != tt.want {
				t.Errorf("text = %q\n want %q", p.text, tt.want)
			}
			if lines := strings.Count(p.text, "\n"); p.lines != lines {
				t.Errorf("lines = %d, text has %d", p.lines, lines)
			}
		})
	}
}

func TestNewPageNeverLetsControlsThrough(t *testing.T) {
	raw := "x\x1b[5;5Hy\x1bP1$r\x1b\\\x1b[6n\x07\x00\x1bc\x1b[38;2;1;2;3mz\x1b[m\r\n"
	p := newPage(raw, 10, 0)
	text := strings.ReplaceAll(p.text, "\x1b[38;2;1;2;3m", "")
	text = strings.ReplaceAll(text, "\x1b[m", "")
	if strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 && r != '\n' }) {
		t.Errorf("control left in %q", p.text)
	}
	if !strings.Contains(p.text, "\x1b[38;2;1;2;3mz") {
		t.Errorf("true colour lost: %q", p.text)
	}
}

func TestOmittedNotice(t *testing.T) {
	for n, want := range map[int]string{1: "1 line of older output", 12345: "12345 lines of older output"} {
		if got := omitted(n); !strings.Contains(got, want) || !strings.HasPrefix(got, "\x1b[") {
			t.Errorf("omitted(%d) = %q, want a dim %q", n, got, want)
		}
	}
}

// captures records the options of every Capture call.
type captures struct {
	*rexfake.Fake
	opts []rex.CaptureOptions
}

func (c *captures) Capture(ctx context.Context, session, block string, opts rex.CaptureOptions) (string, error) {
	c.opts = append(c.opts, opts)
	return c.Fake.Capture(ctx, session, block, opts)
}

func TestPaneSource(t *testing.T) {
	tests := []struct {
		plain bool
		want  rex.CaptureOptions
	}{
		{false, rex.CaptureOptions{Format: rex.FormatVT, Trim: true, Unwrap: true}},
		{true, rex.CaptureOptions{Format: rex.FormatText, Trim: true, Unwrap: true}},
	}
	for _, tt := range tests {
		api := &captures{Fake: rexfake.New()}
		s, _, b := api.AddSession("work")
		api.SetCapture(b, "\x1b[32mok\x1b[m\r\n")
		got, err := paneSource(api, s, b, tt.plain)(context.Background())
		if err != nil || got != "\x1b[32mok\x1b[m\r\n" {
			t.Errorf("plain=%v: capture = %q, %v", tt.plain, got, err)
		}
		if len(api.opts) != 1 || api.opts[0] != tt.want {
			t.Errorf("plain=%v: options = %+v, want %+v", tt.plain, api.opts, tt.want)
		}
	}
}

func TestPaneSourceReportsMissingPanes(t *testing.T) {
	api := rexfake.New()
	s, _, _ := api.AddSession("work")
	if _, err := paneSource(api, s, "block:gone", false)(context.Background()); !errors.Is(err, rex.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDemoSourceIsColourfulAndClean(t *testing.T) {
	raw, err := demoSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := newPage(raw, maxLines, 0)
	if p.lines < 200 {
		t.Errorf("demo has %d lines; it should be long enough to scroll", p.lines)
	}
	for _, want := range []string{"\x1b[48;5;", "\x1b[48;2;", "\x1b[1;31m", "日本語", "❯"} {
		if !strings.Contains(p.text, want) {
			t.Errorf("demo lacks %q", want)
		}
	}
}

func TestPageAt(t *testing.T) {
	whole := newPage("1\n2\n3\n4\n5\n", 0, 0)
	cut := newPage("1\n2\n3\n4\n5\n", 3, 0) // the note, then 4 and 5
	tests := []struct {
		p       page
		n, want int
	}{
		{whole, 1, 1}, {whole, 5, 5}, {whole, 0, 0}, {whole, 6, 0},
		{cut, 4, 2}, {cut, 5, 3},
		{cut, 3, 0}, {cut, 1, 0}, // left out
	}
	for _, tt := range tests {
		if got := tt.p.at(tt.n); got != tt.want {
			t.Errorf("at(%d) of %q = %d, want %d", tt.n, tt.p.text, got, tt.want)
		}
	}
	if got := cut.plainLine(3); got != "5" {
		t.Errorf("plainLine(3) = %q", got)
	}
}

func TestOpen(t *testing.T) {
	pg := newPage("ok\n\x1b[31mGET /a status=200\x1b[m\nPOST /b status=503\n", 0, 0)
	tests := []struct {
		name string
		tg   target
		want opening
	}{
		{"no line", target{}, opening{}},
		{"literal", target{2, find.Query{Text: "status"}}, opening{2, `\V\cstatus`}},
		{"regex", target{3, find.Query{Text: `status=5\d\d`, Regex: true}}, opening{3, `\V\cstatus=5\[0-9]\[0-9]`}},
		// \B has no Vim twin: search for what it matched on the line.
		{"untranslatable regex", target{3, find.Query{Text: `\Bt\w+=5`, Regex: true}}, opening{3, `\V\Ctatus=5`}},
		{"untranslatable, line gone", target{9, find.Query{Text: `\Bt`, Regex: true}}, opening{}},
	}
	for _, tt := range tests {
		if got := open(pg, tt.tg); got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestPageSourceTakesTheFile(t *testing.T) {
	tools := t.TempDir()
	dir := filepath.Join(tools, "handed")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "page.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := pageSource(path, tools)(context.Background())
	if err != nil || got != "a\nb\n" {
		t.Fatalf("read %q, %v", got, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the page's folder is still there: %v", err)
	}
	if _, err := pageSource(path, tools)(context.Background()); err == nil {
		t.Error("a page read twice")
	}
}

// --page on any other file only reads it.
func TestPageSourceLeavesOtherFiles(t *testing.T) {
	tools := filepath.Join(t.TempDir(), "tools")
	elsewhere := t.TempDir()
	for _, path := range []string{
		filepath.Join(elsewhere, "build.log"),
		filepath.Join(elsewhere, "x", "page.txt"),  // a page.txt, not under tools
		filepath.Join(tools, "page.txt"),           // in tools itself
		filepath.Join(tools, "a", "b", "page.txt"), // too deep
		filepath.Join(tools, "a", "notes.txt"),     // not a page
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := pageSource(path, tools)(context.Background()); err != nil || got != "keep\n" {
			t.Fatalf("%s: read %q, %v", path, got, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed", path)
		}
	}
	// Without the state dir nothing is taken.
	path := filepath.Join(tools, "c", "page.txt")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("x\n"), 0o600)
	if _, err := pageSource(path, "")(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("a page was taken without knowing the tools folder")
	}
}

// A line older than the newest limit lines keeps the lines around it, with
// a note for those left out at either end.
func TestPageKeepsTheLineToOpenAt(t *testing.T) {
	var raw strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&raw, "line %d\n", i)
	}
	p := newPage(raw.String(), 10, 30) // 8 lines and two notes
	lines := strings.Split(strings.TrimSuffix(p.text, "\n"), "\n")
	if p.lines != 10 || len(lines) != 10 || p.dropped != 25 || p.newer != 67 {
		t.Fatalf("lines %d, dropped %d, newer %d: %q", p.lines, p.dropped, p.newer, lines)
	}
	if !strings.Contains(lines[0], "25 lines of older output") || !strings.Contains(lines[9], "67 lines of newer output") {
		t.Errorf("notes %q, %q", lines[0], lines[9])
	}
	if at := p.at(30); at == 0 || p.plainLine(at) != "line 30" {
		t.Errorf("line 30 is at %d: %q", at, p.plainLine(at))
	}
	for _, n := range []int{25, 34, 100} { // left out, before and after
		if at := p.at(n); at != 0 {
			t.Errorf("at(%d) = %d, want 0", n, at)
		}
	}
	if at := p.at(33); p.plainLine(at) != "line 33" {
		t.Errorf("the last line kept: %q", p.plainLine(at))
	}
	// Near the start: no note before.
	p = newPage(raw.String(), 10, 2)
	if p.dropped != 0 || p.plainLine(p.at(2)) != "line 2" || !strings.Contains(p.plainLine(p.lines), "newer output") {
		t.Errorf("dropped %d, at %d: %q", p.dropped, p.at(2), p.text)
	}
	// A line among the newest: the newest are kept, as without one.
	if p := newPage(raw.String(), 10, 95); p.newer != 0 || p.dropped != 91 || p.plainLine(p.at(95)) != "line 95" {
		t.Errorf("dropped %d newer %d", p.dropped, p.newer)
	}
}
