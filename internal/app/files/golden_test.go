package files

import (
	"os"
	"path/filepath"
	"rexkit/internal/popup"
	"rexkit/internal/ui/uitest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// goldenEnv is the fixture with a git status and a zoxide project, as the
// launcher shows it at the start.
func goldenEnv(t *testing.T, w, h int) *testEnv {
	t.Helper()
	home, root := fixtureTree(t)
	other := filepath.Join(home, "Coding", "website")
	mustMkdir(t, other)
	stamp := testNow.Add(-26 * time.Hour)
	_ = os.Chtimes(other, stamp, stamp)
	env := newEnv(t, home, root, w, h)
	project, err := statEntry(other, "~/Coding/website")
	if err != nil {
		t.Fatal(err)
	}
	env.send(t, projectsMsg{[]entry{project}})
	status := parseStatus([]byte(strings.Join([]string{
		"# branch.oid 1234567890abcdef", "# branch.head main", "# branch.ab +2 -0",
		"1 .M N... 100644 100644 100644 abc abc README.md",
		"1 A. N... 000000 100644 100644 000 abc cmd/rexkit/main.go",
		"? docs/", "",
	}, "\x00")))
	env.send(t, gitMsg{dir: root, status: status, ok: true})
	return env
}

var goldenSizes = []struct {
	name string
	w, h int
}{{"wide", 140, 36}, {"medium", 100, 28}, {"narrow", 56, 20}}

func TestGoldenViews(t *testing.T) {
	for _, size := range goldenSizes {
		t.Run(size.name, func(t *testing.T) {
			env := goldenEnv(t, size.w, size.h)
			uitest.Golden(t, "start-"+size.name, env.m.View().Content)

			// Search all files below for a Go file: highlighted matches, the
			// code preview, and the footer naming the folder ⏎ opens.
			env.press(t, "ctrl+f")
			env.typeText(t, "main")
			uitest.Golden(t, "search-"+size.name, env.m.View().Content)
		})
	}
}

// TestGoldenStates covers the states Files draws: the key panel
// (two columns on a card, beside the list when narrow), the empty pages, a
// refused path, and the compact chrome.
func TestGoldenStates(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		carded bool
		script func(t *testing.T, e *testEnv)
	}{
		{"help-card", 140, 36, true, func(t *testing.T, e *testEnv) { e.press(t, "?") }},
		{"help-narrow", 56, 20, false, func(t *testing.T, e *testEnv) { e.press(t, "?") }},
		{"nomatch-card", 100, 28, true, func(t *testing.T, e *testEnv) { e.typeText(t, "zqxj") }},
		{"empty-card", 140, 36, true, func(t *testing.T, e *testEnv) {
			e.do(t, e.m.navigate(filepath.Join(e.m.cfg.home, "Coding", "website"), ""))
		}},
		{"refused-card", 140, 36, true, func(t *testing.T, e *testEnv) {
			e.press(t, "ctrl+l")
			e.typeText(t, "zz")
		}},
		{"compact", 60, 12, false, func(t *testing.T, e *testEnv) { e.press(t, "down") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := goldenEnv(t, tt.w, tt.h)
			if tt.carded {
				e.send(t, popup.CardMsg{})
				e.send(t, tea.WindowSizeMsg{Width: tt.w, Height: tt.h})
			}
			tt.script(t, e)
			uitest.Golden(t, tt.name, e.m.View().Content)
		})
	}
}

// TestViewFitsEverySize checks that every frame fills the terminal exactly,
// from tiny to huge, in each mode.
func TestViewFitsEverySize(t *testing.T) {
	home, root := fixtureTree(t)
	for _, size := range [][2]int{{300, 100}, {180, 50}, {80, 24}, {60, 18}, {44, 18}, {30, 8}, {12, 4}} {
		e := newEnv(t, home, root, size[0], size[1])
		for _, keys := range [][]string{nil, {"ctrl+f"}, {"ctrl+l"}, {"?"}} {
			e.press(t, keys...)
			lines := e.screen()
			if len(lines) != size[1] {
				t.Fatalf("%v %v: %d lines", size, keys, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Fatalf("%v %v: line %d is %d wide: %q", size, keys, i, w, l)
				}
			}
			e.press(t, "esc")
			if e.m.done {
				e = newEnv(t, home, root, size[0], size[1])
			}
		}
	}
}
