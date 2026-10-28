package keys

import (
	"fmt"
	"rexkit/internal/ui/uitest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestGolden(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		keys []string
	}{
		{"wide-150x40", 150, 40, nil},
		{"medium-100x30", 100, 30, []string{"down", "down", "down", "down", "down"}},
		{"narrow-60x20", 60, 20, nil},
		{"tiny-44x8", 44, 8, nil},
		{"search-120x24", 120, 24, []string{"s", "p", "l", "i", "t"}},
		{"mode-100x24", 100, 24, []string{"tab", "tab"}},
		{"nomatch-80x10", 80, 10, []string{"q", "q", "q"}},
		{"help-100x30", 100, 30, []string{"?"}},
		{"help-narrow-60x20", 60, 20, []string{"down", "?"}},
		{"empty-120x30", 120, 30, []string{"z", "q", "x", "j"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := demoModel(t, tt.w, tt.h)
			press(t, m, tt.keys...)
			uitest.Golden(t, tt.name, screen(m))
		})
	}
}

// TestGoldenFloat: at the float sizes Lua opens it at, banded and compact.
func TestGoldenFloat(t *testing.T) {
	for _, size := range [][2]int{{188, 52}, {103, 30}, {60, 12}} {
		name := fmt.Sprintf("float-%dx%d", size[0], size[1])
		t.Run(name, func(t *testing.T) {
			t.Setenv("REXKIT_THEME", "dark")
			m := newModel(options{demo: true}, Build(demoKeymap()), nil, nil)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			uitest.Golden(t, name, screen(m))
		})
	}
}

func TestGoldenLight(t *testing.T) {
	t.Setenv("REXKIT_THEME", "light")
	m := newModel(options{demo: true}, Build(demoKeymap()), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	press(t, m, "down", "down", "down", "down", "down")
	uitest.Golden(t, "light-100x24", screen(m))
}
