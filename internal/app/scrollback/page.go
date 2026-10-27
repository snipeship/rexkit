package scrollback

import (
	"context"
	"os"
	"path/filepath"
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

// pageSource reads a capture to show (Search spools the one it searched,
// so the lines it numbered are the lines shown). A page Search handed over,
// a page.txt in a folder of its own under tools, is the viewer's once read:
// it is removed, and its folder with it when that is left empty. Any other
// file is only read.
func pageSource(path, tools string) source {
	return func(context.Context) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if handedOver(path, tools) {
			_ = os.Remove(path)
			_ = os.Remove(filepath.Dir(path)) // only goes when empty
		}
		return string(data), nil
	}
}

// handedOver reports whether path is a page Search handed over: tools/X/page.txt.
func handedOver(path, tools string) bool {
	if tools == "" || !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	return filepath.Base(path) == "page.txt" && filepath.Dir(filepath.Dir(path)) == filepath.Clean(tools)
}

// page is captured output made safe to show: text and SGR colours only.
type page struct {
	text    string // lines, each ending in "\n"
	lines   int
	dropped int // oldest lines left out to stay within the limit
	newer   int // newest lines left out to keep the line to open at (the last line notes them)
}

// newPage sanitizes raw terminal output into at most limit lines: the last
// ones, the first of which notes how many were left out. When line (from 1)
// is not among them, it keeps the lines around it instead, with a note at
// either end.
func newPage(raw string, limit, line int) page {
	lines := sanitize.TerminalLines(raw, 0)
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	var p page
	if limit > 0 && len(lines) > limit {
		from, to := len(lines)-limit+1, len(lines) // the lines kept, after the note
		if line > 0 && line-1 < from {
			keep := limit - 2 // two notes
			from = max(0, line-1-keep/2)
			to = from + keep
		}
		p.dropped, p.newer = from, len(lines)-to
		var kept []string
		if from > 0 {
			kept = append(kept, omitted(from))
		}
		kept = append(kept, lines[from:to]...)
		if p.newer > 0 {
			kept = append(kept, omittedNewer(p.newer))
		}
		lines = kept
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

// at is where line n (from 1) of the capture is in the page, 0 when it
// was left out (or there is no such line).
func (p page) at(n int) int {
	if p.dropped > 0 {
		n -= p.dropped - 1 // the note stands in for the lines dropped
		if n < 2 {
			return 0
		}
	}
	last := p.lines
	if p.newer > 0 {
		last-- // the note
	}
	if n < 1 || n > last {
		return 0
	}
	return n
}

// plainLine is line n (from 1) of the page, without colours.
func (p page) plainLine(n int) string {
	text := p.text
	for ; n > 1; n-- {
		_, text, _ = strings.Cut(text, "\n")
	}
	line, _, _ := strings.Cut(text, "\n")
	return ansi.Strip(line)
}

func blank(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }

// omitted is the dim first line that stands in for n dropped lines.
func omitted(n int) string {
	return "\x1b[2;3m… " + ui.Plural(n, "line") + " of older output not shown\x1b[m"
}

// omittedNewer is the dim last line that stands in for n newer lines left
// out.
func omittedNewer(n int) string {
	return "\x1b[2;3m… " + ui.Plural(n, "line") + " of newer output not shown\x1b[m"
}
