package rename

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/uitest"
)

// dialog returns a dialog for target pane with current label, sized w×h.
func dialog(t *testing.T, current string, apply func(string) tea.Cmd, w, h int) *model {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{target: Target{Kind: "pane", ID: "block:01a1763b-f56c-72ce-b5e2-fb233bccb71a"}, dismissOnBlur: true}, current, apply, nil, nil)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func plain(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func screen(m *model) string {
	out, _ := m.render(time.Now())
	return out
}

// typeKeys presses keys (config notation, uitest.Key) and returns the last
// command.
func typeKeys(t *testing.T, m *model, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(uitest.Key(t, k))
	}
	return cmd
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
