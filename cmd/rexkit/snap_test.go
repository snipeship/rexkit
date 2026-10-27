package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// TestSnapEveryState drives every tool into every state, alone and in its
// float over the demo backdrop, and checks the screen is drawn and fits its
// size. (The theme only changes colours; one is enough here.)
func TestSnapEveryState(t *testing.T) {
	shots, err := allShots([]bool{true}, false)
	if err != nil {
		t.Fatal(err)
	}
	demo := demoBackdrop(snap.Theme(true), 1)
	seen := map[string]bool{}
	for _, s := range shots {
		pic, err := s.take(demo, nil)
		content, w, h := pic.content, pic.w, pic.h
		name := fmt.Sprintf("%s-%s-%dx%d-window=%t", s.tool.name, s.state, w, h, s.window)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if seen[name] {
			t.Errorf("%s is drawn twice", name)
		}
		seen[name] = true
		lines := strings.Split(content, "\n")
		// Hints over a pane with nothing to pick draws nothing at all: only
		// the window around it shows the toast it leaves.
		blankOK := s.tool.name == "hints" && s.state == "nothing" && !s.window
		if strings.TrimSpace(ansi.Strip(content)) == "" && !blankOK || len(lines) > h {
			t.Errorf("%s: %d lines of %d, blank=%v", name, len(lines), h, strings.TrimSpace(ansi.Strip(content)) == "")
		}
		for i, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Errorf("%s: line %d is %d cells wide, over %d", name, i, ansi.StringWidth(l), w)
			}
		}
	}
}

func TestSnapCard(t *testing.T) {
	tools := map[string]snapTool{}
	for _, tool := range snapTools() {
		tools[tool.name] = tool
	}
	float := tools["goto"]
	if got := float.card(241, snap.Scene{}).Rect(241, 70); got.W != 188 || got.H != 52 {
		t.Errorf("wide float card %+v, want 188x52", got)
	}
	if got := float.card(100, snap.Scene{}).Rect(100, 30); got.W != 94 || got.H != 26 {
		t.Errorf("medium float card %+v, want 94x26", got)
	}
	if !float.card(70, snap.Scene{}).IsZero() {
		t.Error("a narrow window has a card")
	}
	if got := tools["rename"].card(241, snap.Scene{Cols: 56, Rows: 7}); got != (popup.Card{Cols: 56, Rows: 7}) {
		t.Errorf("dialog card %+v", got)
	}
	if got := tools["toast"].card(241, snap.Scene{Cols: 30, Rows: 1}).Rect(241, 70); got.X != 211 || got.Y != 0 {
		t.Errorf("toast card %+v, want the top-right corner", got)
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
		{"--backdrop", "fog", "--out", out},
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
			t.Fatalf("%s-%s is not in its float", s.tool.name, s.state)
		}
	}
}

// The demo backdrop is drawn only where Lua dims: around a float's or a
// dialog's card, never a toast's, a tool's without a card, or a window too
// narrow for one.
func TestSnapBackdrop(t *testing.T) {
	demo := demoBackdrop(snap.Theme(true), 1)
	tools := map[string]snapTool{}
	for _, tool := range snapTools() {
		tools[tool.name] = tool
	}
	for _, tt := range []struct {
		tool string
		w, h int
		want bool
	}{
		{"goto", 241, 70, true}, {"goto", 70, 24, false}, {"rename", 120, 40, true},
		{"toast", 241, 70, false}, {"hold", 241, 70, false},
	} {
		tool := tools[tt.tool]
		s := shot{tool: tool, state: tool.states[0], dark: true, w: tt.w, h: tt.h, window: true}
		pic, err := s.take(demo, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pic.backdrop != tt.want {
			t.Errorf("%s at %dx%d: backdrop %t, want %t", tt.tool, tt.w, tt.h, pic.backdrop, tt.want)
		}
		if !pic.backdrop {
			continue
		}
		if lines := strings.Split(pic.content, "\n"); len(lines) != tt.h || ansi.StringWidth(lines[0]) != tt.w {
			t.Errorf("%s: the backdrop does not fill the window: %d lines", tt.tool, len(lines))
		}
	}
	s := shot{tool: tools["goto"], state: "default", dark: true, w: 241, h: 70}
	if pic, _ := s.take(demo, nil); pic.backdrop {
		t.Error("a tool alone has a backdrop")
	}
}

// A toast's strip is Clear around the toast: the panes under it show whole,
// as they do in Rex, and only the toast itself covers them.
func TestSnapToastShowsPanes(t *testing.T) {
	theme := snap.Theme(true)
	var tool snapTool
	for _, tt := range snapTools() {
		if tt.name == "toast" {
			tool = tt
		}
	}
	s := shot{tool: tool, state: "info", dark: true, w: 241, h: 70, window: true}
	pic, err := s.take(demoBackdrop(theme, 1), demoBackdrop(theme, 0))
	if err != nil {
		t.Fatal(err)
	}
	panes := strings.Split(ansi.Strip(demoBackdrop(theme, 0)(241, 70, ui.Silhouette{})), "\n")
	got := strings.Split(ansi.Strip(pic.content), "\n")
	for y := range 4 {
		// Left of the toast (it takes at most 60 % of the width) the
		// strip shows the panes' text.
		want, line := ansi.Cut(panes[y], 0, 90), ansi.Cut(got[y], 0, 90)
		if line != want {
			t.Errorf("strip row %d: %q, want the panes' %q", y, line, want)
		}
	}
	if !strings.Contains(got[2], "Parked api") {
		t.Errorf("no toast on the strip's text row: %q", got[2])
	}
}
