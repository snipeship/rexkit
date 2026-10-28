package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
)

// DemoPane is one made-up pane, for demos and snapshots (no Rex needed):
// where it sits in the window, what it shows at a size, and what its header
// says.
type DemoPane struct {
	Rect           rex.Rect
	Draw           func(cols, rows int) string
	Program, Title string
}

// DemoPanes are a made-up window of three panes: Neovim editing Go on the
// left, a shell with git and ls output top right, and a Claude Code session
// bottom right.
func DemoPanes() []DemoPane {
	return []DemoPane{
		{rex.Rect{0, 0, 0.5, 1}, demoEditor, "nvim", "panes/geometry.go"},
		{rex.Rect{0.5, 0, 0.5, 0.45}, demoShell, "nu", "~/code/rexkit"},
		{rex.Rect{0.5, 0.45, 0.5, 0.55}, demoAgent, "claude", "make the floats native"},
	}
}

// DemoScreens lays made-up panes out in layout area l, each drawn at the
// size Rex would give it with cw×ch-pixel cells, as rex.API.Screens
// reports a real window.
func DemoScreens(panes []DemoPane, l Layout, cw, ch int) rex.Screens {
	var screens rex.Screens
	for i, p := range panes {
		cols, rows, _ := Content(l.Pane(p.Rect), cw, ch)
		screens.Panes = append(screens.Panes, rex.PaneScreen{
			BlockID: "block:demo-" + string(rune('a'+i)),
			Rect:    p.Rect,
			Size:    rex.Size{Columns: cols, Rows: rows},
			Screen:  p.Draw(cols, rows),
			Program: p.Program,
			Title:   p.Title,
		})
	}
	return screens
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
		vtLine(span{"32", "ok  "}, span{"", "\trexkit/internal/panes\t0.412s"}),
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
		{{kw, "package"}, {"", " panes"}},
		{},
		{{kw, "import"}, {"", " ("}},
		{{"", "\t"}, {str, `"image"`}},
		{{"", "\t"}, {str, `"math"`}},
		{{"", ")"}},
		{},
		{{cmt, "// Content is how many cw×ch cells a pane or float at r holds, and"}},
		{{cmt, "// where the first one is."}},
		{{kw, "func"}, {"", " "}, {fn, "Content"}, {"", "(r image."}, {typ, "Rectangle"}, {"", ", cw, ch "}, {typ, "int"}, {"", ") {"}},
		{{"", "\tcols = "}, {fn, "max"}, {"", "((r."}, {fn, "Dx"}, {"", "()-PadX)/cw, "}, {num, "1"}, {"", ")"}},
		{{"", "\trows = "}, {fn, "max"}, {"", "((r."}, {fn, "Dy"}, {"", "()-PadY)/ch, "}, {num, "1"}, {"", ")"}},
		{{"", "\t"}, {kw, "return"}, {"", " cols, rows, r.Min."}, {fn, "Add"}, {"", "(image."}, {fn, "Pt"}, {"", "(PadLeft, Header))"}},
		{{"", "}"}},
		{},
		{{cmt, "// Float is where a float with rect r is in the layout area."}},
		{{kw, "func"}, {"", " (l Layout) "}, {fn, "Float"}, {"", "(r rex."}, {typ, "Rect"}, {"", ") image."}, {typ, "Rectangle"}, {"", " {"}},
		{{"", "\tx0, y0 := "}, {fn, "px"}, {"", "(r["}, {num, "0"}, {"", "], l.W), "}, {fn, "px"}, {"", "(r["}, {num, "1"}, {"", "], l.H)"}},
		{{"", "\t"}, {kw, "return"}, {"", " image."}, {fn, "Rect"}, {"", "(x0, y0, "}, {fn, "px"}, {"", "(r["}, {num, "0"}, {"", "]+r["}, {num, "2"}, {"", "], l.W),"}},
		{{"", "\t\t"}, {fn, "px"}, {"", "(r["}, {num, "1"}, {"", "]+r["}, {num, "3"}, {"", "], l.H))"}},
		{{"", "}"}},
		{},
		{{kw, "func"}, {"", " "}, {fn, "px"}, {"", "(f "}, {typ, "float64"}, {"", ", n "}, {typ, "int"}, {"", ") "}, {typ, "int"}, {"", " {"}},
		{{"", "\t"}, {kw, "return"}, {"", " "}, {typ, "int"}, {"", "(math."}, {fn, "Round"}, {"", "(f * "}, {typ, "float64"}, {"", "(n)))"}},
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
	status := []span{{"1;30;44", " NORMAL "}, {"37;48;5;238", " panes/geometry.go "}, {"90;48;5;236", " go "}}
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
		vtLine(span{"90", "> "}, span{"", "make the floats native: Rex draws them now"}),
		"",
		vtLine(span{"", "⏺ I'll size each tool's float to its content and let Rex"}),
		vtLine(span{"", "  draw the frame, the header and the shadow."}),
		"",
		vtLine(span{"32", "⏺ "}, span{"1", "Read"}, span{"", "(lua/tools.lua)"}),
		vtLine(span{"90", "  ⎿  Read 612 lines"}),
		"",
		vtLine(span{"32", "⏺ "}, span{"1", "Update"}, span{"", "(internal/panes/geometry.go)"}),
		vtLine(span{"90", "  ⎿  Updated with 2 additions and 2 removals"}),
		vtLine(span{"90", "      31  "}, span{"30;48;2;255;180;180", "-  Header  = 72"}),
		vtLine(span{"90", "      31  "}, span{"30;48;2;170;230;170", "+  Header  = 76"}),
		vtLine(span{"90", "      32  "}, span{"30;48;2;170;230;170", "+  PadY    = 91"}),
		"",
		vtLine(span{claude, "✻ "}, span{claude, "Measuring… "}, span{"90", "(esc to interrupt)"}),
		"",
	)
	tail := append(box("90", []string{"> \x1b[7m \x1b[0m"}), vtLine(span{"90", "  ? for shortcuts"}))
	for len(lines)+len(tail) < rows {
		lines = append(lines, "")
	}
	return bottom(append(lines, tail...), rows)
}
