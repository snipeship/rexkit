package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/panes"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// TestSnapEveryState drives every tool into every state, alone and in its
// window, and checks the screen is drawn and fits its float. (The theme
// only changes colours; one is enough here.)
func TestSnapEveryState(t *testing.T) {
	shots, err := allShots([]bool{true}, false)
	if err != nil {
		t.Fatal(err)
	}
	theme := snap.Theme(true)
	seen := map[string]bool{}
	for _, s := range shots {
		pic, err := s.take(theme)
		name := fmt.Sprintf("%s-%s-%dx%d-window=%t", s.tool.name, s.state, pic.w, pic.h, s.window)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if seen[name] {
			t.Errorf("%s is drawn twice", name)
		}
		seen[name] = true
		lines := strings.Split(pic.content, "\n")
		// Hints over a pane with nothing to pick draws nothing at all: only
		// the toast it leaves shows.
		blankOK := s.tool.name == "hints" && s.state == "nothing"
		if strings.TrimSpace(ansi.Strip(pic.content)) == "" && !blankOK || len(lines) > pic.rows {
			t.Errorf("%s: %d lines of %d, blank=%v", name, len(lines), pic.rows, strings.TrimSpace(ansi.Strip(pic.content)) == "")
		}
		for i, l := range lines {
			if ansi.StringWidth(l) > pic.cols {
				t.Errorf("%s: line %d is %d cells wide, over %d", name, i, ansi.StringWidth(l), pic.cols)
			}
		}
		if s.window != (pic.window != nil) {
			t.Errorf("%s: window %v", name, pic.window != nil)
		}
	}
}

// Each tool's float is where Lua opens it: the float size by the window's
// width, a dialog centred over its pane, a toast in the top-right corner,
// and the pane tools exactly over their pane.
func TestSnapPlacement(t *testing.T) {
	tools := map[string]snapTool{}
	for _, tool := range snapTools() {
		tools[tool.name] = tool
	}
	l := panes.LayoutFor(241, 70, snap.CellW, snap.CellH)
	left := rex.Rect{0, 0, 0.5, 1}
	at := tools["goto"].placeIn(l, 241, left, snap.Scene{})
	if cols, _, _ := panes.Content(at, snap.CellW, snap.CellH); cols < 185 || cols > 189 || at.Min.X <= 0 {
		t.Errorf("wide float %v holds %d columns, want about 78%% of 241, centred", at, cols)
	}
	for _, w := range []int{70, 110} {
		small := panes.LayoutFor(w, 34, snap.CellW, snap.CellH)
		if at := tools["goto"].placeIn(small, w, left, snap.Scene{}); at != (image.Rectangle{Max: image.Pt(small.W, small.H)}) {
			t.Errorf("the float of a window %d columns wide %v is not the whole window", w, at)
		}
	}
	right := rex.Rect{0.5, 0, 0.5, 1}
	for _, over := range []rex.Rect{left, right} {
		dialog := tools["rename"].placeIn(l, 241, over, snap.Scene{Cols: 56, Rows: ui.DialogRows})
		pane := l.Pane(over)
		if cols, rows, _ := panes.Content(dialog, snap.CellW, snap.CellH); cols != 56 || rows != ui.DialogRows {
			t.Errorf("dialog holds %dx%d", cols, rows)
		}
		d := dialog.Min.Add(dialog.Max).Div(2).Sub(pane.Min.Add(pane.Max).Div(2))
		if abs(d.X) > 1 || abs(d.Y) > 1 {
			t.Errorf("dialog %v is %v off the middle of its pane %v", dialog, d, pane)
		}
	}
	pane := l.Pane(left)
	toastAt := tools["toast"].placeIn(l, 241, left, snap.Scene{Rows: 1, FitCols: func(int) int { return 30 }})
	if toastAt.Max.X != l.W || toastAt.Min.Y != 0 {
		t.Errorf("toast %v is not in the top-right corner of %v", toastAt, l)
	}
	if at := tools["hints"].placeIn(l, 241, left, snap.Scene{}); at != pane {
		t.Errorf("hints %v is not over its pane %v", at, pane)
	}
}

func TestSnapWritesPNG(t *testing.T) {
	if _, err := os.Stat(snap.DefaultFontPaths().Primary[0]); err != nil {
		t.Skip("Berkeley Mono is not installed")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "rename.png")
	if err := snapCmd([]string{"--tool", "rename", "--state", "error", "--window", "--scale", "1", "--out", out}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Errorf("no PNG: %v", err)
	}
	for _, bad := range [][]string{
		{"--tool", "nope", "--out", out},
		{"--tool", "goto", "--state", "nope", "--out", out},
		{"--theme", "both", "--out", out},
		{"--size", "0x5", "--out", out},
		{"--all"},
		{"--backdrop", "dim", "--out", out},
	} {
		if err := snapCmd(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestSnapAllWindowOnly(t *testing.T) {
	all, err := allShots([]bool{true, false}, false)
	if err != nil {
		t.Fatal(err)
	}
	windows, err := allShots([]bool{true, false}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows)*2 != len(all) {
		t.Errorf("%d window shots of %d", len(windows), len(all))
	}
	for _, s := range windows {
		if !s.window {
			t.Fatalf("%s-%s is not in its window", s.tool.name, s.state)
		}
	}
}

// Hints leaves a toast once it picks: the window shows it in the top-right
// corner, titled as hints, beside hints over its pane.
func TestSnapHintsToast(t *testing.T) {
	var tool snapTool
	for _, tt := range snapTools() {
		if tt.name == "hints" {
			tool = tt
		}
	}
	pic, err := shot{tool: tool, state: "copied", dark: true, w: 241, h: 70, window: true}.take(snap.Theme(true))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(pic.window.Floats); n != 2 {
		t.Fatalf("%d floats", n)
	}
	toast := pic.window.Floats[1]
	if toast.Title != "Hints" || toast.At.Max.X != pic.window.Layout.W || !strings.Contains(ansi.Strip(toast.Content), "Copied a3f9c21") {
		t.Errorf("toast %q at %v: %q", toast.Title, toast.At, ansi.Strip(toast.Content))
	}
}

// Hints over a pane with nothing to pick closes at once: the window shows
// only the toast it leaves, not an empty float over the pane.
func TestSnapHintsNothing(t *testing.T) {
	var tool snapTool
	for _, tt := range snapTools() {
		if tt.name == "hints" {
			tool = tt
		}
	}
	pic, err := shot{tool: tool, state: "nothing", dark: true, w: 241, h: 70, window: true}.take(snap.Theme(true))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(pic.window.Floats); n != 1 || !strings.Contains(ansi.Strip(pic.window.Floats[0].Content), "Nothing to pick") {
		t.Errorf("%d floats, want only the toast", n)
	}
}

func abs(n int) int { return max(n, -n) }
