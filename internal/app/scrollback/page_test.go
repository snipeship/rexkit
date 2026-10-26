package scrollback

import (
	"context"
	"errors"
	"strings"
	"testing"

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
			p := newPage(tt.raw, tt.limit)
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
	p := newPage(raw, 10)
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
	p := newPage(raw, maxLines)
	if p.lines < 200 {
		t.Errorf("demo has %d lines; it should be long enough to scroll", p.lines)
	}
	for _, want := range []string{"\x1b[48;5;", "\x1b[48;2;", "\x1b[1;31m", "日本語", "❯"} {
		if !strings.Contains(p.text, want) {
			t.Errorf("demo lacks %q", want)
		}
	}
}
