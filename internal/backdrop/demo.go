package backdrop

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
)

// Demo is a backdrop of three made-up panes in a w×h window, for demos and
// snapshots (no Rex needed): Neovim editing Go on the left, a shell with git
// and ls output top right, and a Claude Code session bottom right.
func Demo(theme rextheme.Theme, w, h int) *Backdrop {
	panes := []struct {
		rect           rex.Rect
		draw           func(cols, rows int) string
		program, title string
	}{
		{rex.Rect{0, 0, 0.5, 1}, demoEditor, "nvim", "backdrop/look.go"},
		{rex.Rect{0.5, 0, 0.5, 0.45}, demoShell, "nu", "~/code/rexkit"},
		{rex.Rect{0.5, 0.45, 0.5, 0.55}, demoAgent, "claude", "make the popups feel elevated"},
	}
	var screens rex.Screens
	for i, p := range panes {
		cols, rows := paneSize(p.rect, w, h)
		screens.Panes = append(screens.Panes, rex.PaneScreen{
			BlockID: "block:demo-" + string(rune('a'+i)),
			Rect:    p.rect,
			Size:    rex.Size{Columns: cols, Rows: rows},
			Screen:  p.draw(cols, rows),
			Program: p.program,
			Title:   p.title,
		})
	}
	return New(theme, screens)
}

// span is text in one SGR style ("" for the default).
type span struct{ sgr, text string }

// vtLine encodes spans as Rex's vt format does: each run restates its style.
func vtLine(spans ...span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString("\x1b[0")
		if s.sgr != "" {
			b.WriteString(";" + s.sgr)
		}
		b.WriteString("m" + s.text)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// fill is a span of n spaces in a style (a background bar).
func fill(sgr string, n int) span { return span{sgr, strings.Repeat(" ", max(n, 0))} }

// top keeps the first rows lines, padding with empty ones.
func top(lines []string, rows int) string {
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines[:rows], "\n")
}

// bottom keeps the last rows lines (a shell scrolls), padding at the end.
func bottom(lines []string, rows int) string {
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	return top(lines, rows)
}

func demoShell(cols, rows int) string {
	prompt := func(cmd string) string {
		return vtLine(span{"36", "~/code/rexkit"}, span{"", " "}, span{"35", "main"}, span{"", " "},
			span{"1;32", "❯"}, span{"", " " + cmd})
	}
	commit := func(graph, hash, refs, msg string) string {
		spans := []span{{"31", graph}, {"33", hash}, {"", " "}}
		if refs != "" {
			spans = append(spans, span{"33", "("}, span{"1;36", "HEAD -> "}, span{"1;32", "main"},
				span{"33", ", "}, span{"1;31", refs}, span{"33", ")"}, span{"", " "})
		}
		return vtLine(append(spans, span{"", msg})...)
	}
	lines := []string{
		prompt("git log --oneline --graph -9"),
		commit("* ", "4eaf457", "origin/main", "Open every tool over the whole window"),
		commit("* ", "cabad71", "", "Paint an opaque surface under every tool"),
		commit("* ", "d0cd502", "", "Mark handshake commits, hold an early ⏎"),
		commit("*   ", "4985d3c", "", "Merge branch 'review-fixes'"),
		vtLine(span{"31", "|"}, span{"32", "\\"}),
		vtLine(span{"31", "| "}, span{"32", "* "}, span{"33", "9c1e2aa"}, span{"", " Fix 92 confirmed review findings"}),
		vtLine(span{"31", "| "}, span{"32", "* "}, span{"33", "77b0d41"}, span{"", " Clamp the list scroll on resize"}),
		vtLine(span{"31", "|"}, span{"32", "/"}),
		commit("* ", "b57d48e", "", "Build rexkit: one Go binary and a thin Lua layer"),
		prompt("ls --color"),
		vtLine(span{"1;34", "cmd"}, span{"", "  "}, span{"1;34", "docs"}, span{"", "  go.mod  go.sum  "},
			span{"1;34", "internal"}, span{"", "  "}, span{"1;34", "lua"}, span{"", "  "}, span{"1;34", "nvim"},
			span{"", "  README.md  "}, span{"1;34", "tests"}),
		prompt("go test ./internal/..."),
		vtLine(span{"32", "ok  "}, span{"", "\trexkit/internal/backdrop\t0.412s"}),
		vtLine(span{"32", "ok  "}, span{"", "\trexkit/internal/popup\t0.538s"}),
		vtLine(span{"32", "ok  "}, span{"", "\trexkit/internal/rextheme\t0.217s"}),
		vtLine(span{"32", "ok  "}, span{"", "\trexkit/internal/ui\t1.204s"}),
		prompt("") + "\x1b[7m \x1b[0m",
	}
	return bottom(lines, rows)
}

func demoEditor(cols, rows int) string {
	const (
		kw, typ, fn, str, cmt, num = "35", "36", "34", "32", "3;90", "33"
	)
	code := [][]span{
		{{kw, "package"}, {"", " backdrop"}},
		{},
		{{kw, "import"}, {"", " ("}},
		{{"", "\t"}, {str, `"math"`}},
		{{"", "\t"}, {str, `"time"`}},
		{{"", ")"}},
		{},
		{{cmt, "// Fade is the strength of frame i of the fade-in: an ease-out"}},
		{{cmt, "// from Fade(0) = 0 to Fade(FadeFrames) = 1."}},
		{{kw, "func"}, {"", " "}, {fn, "Fade"}, {"", "(i "}, {typ, "int"}, {"", ") "}, {typ, "float64"}, {"", " {"}},
		{{"", "\tt := math."}, {fn, "Max"}, {"", "("}, {num, "0"}, {"", ", math."}, {fn, "Min"}, {"", "("}, {num, "1"},
			{"", ", "}, {typ, "float64"}, {"", "(i)/FadeFrames))"}},
		{{"", "\t"}, {kw, "return"}, {"", " "}, {num, "1"}, {"", " - math."}, {fn, "Pow"}, {"", "("}, {num, "1"},
			{"", "-t, "}, {num, "3"}, {"", ")"}},
		{{"", "}"}},
		{},
		{{cmt, "// shade is how much shadow falls on cell (x, y) next to the card."}},
		{{kw, "func"}, {"", " "}, {fn, "shade"}, {"", "(x, y "}, {typ, "int"}, {"", ", card ui."}, {typ, "Rect"},
			{"", ") "}, {typ, "float64"}, {"", " {"}},
		{{"", "\t"}, {kw, "if"}, {"", " card."}, {fn, "Empty"}, {"", "() || card."}, {fn, "Contains"}, {"", "(x, y) {"}},
		{{"", "\t\t"}, {kw, "return"}, {"", " "}, {num, "0"}},
		{{"", "\t}"}},
		{{"", "\tkey := card."}, {fn, "Offset"}, {"", "(shadowDX, shadowDY)"}},
		{{"", "\ts := "}, {num, "0.0"}},
		{{"", "\t"}, {kw, "switch"}, {"", " "}, {fn, "distance"}, {"", "(x, y, key) {"}},
		{{"", "\t"}, {kw, "case"}, {"", " "}, {num, "0"}, {"", ":"}},
		{{"", "\t\ts = "}, {num, "1"}},
		{{"", "\t"}, {kw, "case"}, {"", " "}, {num, "1"}, {"", ":"}},
		{{"", "\t\ts = "}, {num, "0.45"}},
		{{"", "\t}"}},
		{{"", "\t"}, {kw, "return"}, {"", " s"}},
		{{"", "}"}},
	}
	const cursor = 11 // the line the cursor is on (0-based)
	var lines []string
	for i := range rows - 2 {
		if i >= len(code) {
			lines = append(lines, vtLine(span{"34", "~"}))
			continue
		}
		nr, bg := "90", ""
		if i == cursor {
			nr, bg = "1;33", ";48;5;236"
		}
		spans := []span{{nr + bg, fmt.Sprintf("%4d ", i+1)}}
		width := 5
		for _, s := range code[i] {
			text := strings.ReplaceAll(s.text, "\t", "    ")
			spans = append(spans, span{strings.TrimPrefix(s.sgr+bg, ";"), text})
			width += ansi.StringWidth(text)
		}
		if bg != "" {
			spans = append(spans, fill(bg[1:], cols-width))
		}
		lines = append(lines, vtLine(spans...))
	}
	status := []span{{"1;30;44", " NORMAL "}, {"37;48;5;238", " backdrop/look.go "}, {"90;48;5;236", " go "}}
	right := " utf-8  12:9  41% "
	used := 0
	for _, s := range status {
		used += ansi.StringWidth(s.text)
	}
	status = append(status, fill("48;5;236", cols-used-ansi.StringWidth(right)), span{"30;44", right})
	lines = append(lines, vtLine(status...), vtLine(span{"", ""}))
	return top(lines, rows)
}

func demoAgent(cols, rows int) string {
	const claude = "38;2;215;119;87"
	box := func(sgr string, inner []string) []string {
		w := max(cols-2, 4)
		out := []string{vtLine(span{sgr, "╭" + strings.Repeat("─", w-2) + "╮"})}
		for _, s := range inner {
			pad := w - 3 - ansi.StringWidth(s)
			out = append(out, vtLine(span{sgr, "│"}, span{"", " " + s + strings.Repeat(" ", max(pad, 0))}, span{sgr, "│"}))
		}
		return append(out, vtLine(span{sgr, "╰" + strings.Repeat("─", w-2) + "╯"}))
	}
	var lines []string
	lines = append(lines, box(claude, []string{"✻ Welcome to Claude Code!", "", "  cwd: ~/code/rexkit"})...)
	lines = append(lines,
		"",
		vtLine(span{"90", "> "}, span{"", "make the popups feel elevated above the panes"}),
		"",
		vtLine(span{"", "⏺ I'll redraw the window's panes behind the card, dimmed,"}),
		vtLine(span{"", "  with a soft shadow under it, and fade it in."}),
		"",
		vtLine(span{"32", "⏺ "}, span{"1", "Read"}, span{"", "(internal/popup/stage.go)"}),
		vtLine(span{"90", "  ⎿  Read 228 lines"}),
		"",
		vtLine(span{"32", "⏺ "}, span{"1", "Update"}, span{"", "(internal/backdrop/look.go)"}),
		vtLine(span{"90", "  ⎿  Updated with 2 additions and 2 removals"}),
		vtLine(span{"90", "      18  "}, span{"30;48;2;255;180;180", "-  DimText = 0.5"}),
		vtLine(span{"90", "      18  "}, span{"30;48;2;170;230;170", "+  DimText = 0.62"}),
		vtLine(span{"90", "      19  "}, span{"30;48;2;170;230;170", "+  DimFill = 0.55"}),
		"",
		vtLine(span{claude, "✻ "}, span{claude, "Fading in… "}, span{"90", "(esc to interrupt)"}),
		"",
	)
	tail := append(box("90", []string{"> \x1b[7m \x1b[0m"}), vtLine(span{"90", "  ? for shortcuts"}))
	for len(lines)+len(tail) < rows {
		lines = append(lines, "")
	}
	return bottom(append(lines, tail...), rows)
}
