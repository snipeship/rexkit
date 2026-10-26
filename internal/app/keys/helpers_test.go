package keys

import (
	"rexkit/internal/ui/uitest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// demoModel returns the demo guide at w×h in the dark theme.
func demoModel(t *testing.T, w, h int) *model {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{demo: true}, Build(demoKeymap()), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

var fixedNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func screen(m *model) string {
	out, _ := m.render(fixedNow)
	return out
}

func plain(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func press(t *testing.T, m *model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		m.Update(uitest.Key(t, k))
	}
}
