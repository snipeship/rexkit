package scrollback

import (
	"context"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// maxLines caps the capture at the newest 100k lines. It is only a coarse
// upper bound: a Neovim terminal buffer keeps 100k *rows* ('scrollback'), and
// a line wider than the window takes several, so the profile
// (nvim/scrollback.lua) trims to its real row budget and replaces the note.
const maxLines = 100_000

// source returns the raw terminal text to show: Rex's VT export, plain text,
// or a generated sample.
type source func(ctx context.Context) (string, error)

// paneSource captures a pane's screen and scrollback. Soft-wrapped lines
// are joined so the viewer re-wraps them at its own width; trailing blank
// lines are dropped so "bottom" means the last line of output.
func paneSource(api rex.API, session, block string, plain bool) source {
	opts := rex.CaptureOptions{Format: rex.FormatVT, Trim: true, Unwrap: true}
	if plain {
		opts.Format = rex.FormatText
	}
	return func(ctx context.Context) (string, error) {
		return api.Capture(ctx, session, block, opts)
	}
}

// page is captured output made safe to show: text and SGR colours only.
type page struct {
	text    string // lines, each ending in "\n"
	lines   int
	dropped int // oldest lines left out to stay within the limit
}

// newPage sanitizes raw terminal output into at most limit lines (the last
// ones), the first of which notes how many were left out.
func newPage(raw string, limit int) page {
	lines := sanitize.TerminalLines(raw, 0)
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	var p page
	if limit > 0 && len(lines) > limit {
		p.dropped = len(lines) - limit + 1
		lines = append([]string{omitted(p.dropped)}, lines[p.dropped:]...)
	}
	p.lines = len(lines)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	p.text = b.String()
	return p
}

func blank(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }

// omitted is the dim first line that stands in for n dropped lines.
func omitted(n int) string {
	return "\x1b[2;3m… " + ui.Plural(n, "line") + " of older output not shown\x1b[m"
}
