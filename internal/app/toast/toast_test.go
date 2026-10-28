package toast

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

// toastIn is a toast in its float as Lua opens it in a window cols wide:
// ui.ToastWidth wide, a row tall.
func toastIn(t *testing.T, kind ui.ToastKind, text string, cols int) *model {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(kind, text, 0)
	m.Update(tea.WindowSizeMsg{Width: m.theme.ToastWidth(m.toast(), cols), Height: 1})
	return m
}

// The goldens are named for the window the toast is in.
func TestGolden(t *testing.T) {
	tests := []struct {
		name string
		kind ui.ToastKind
		text string
		cols int
	}{
		{"ok-90", ui.ToastOK, "Renamed the pane to api", 90},
		{"err-120", ui.ToastErr, "Could not open the editor: nvim exited with status 1", 120},
		{"warn-60", ui.ToastWarn, "Nothing to search", 60},
		{"info-60-cut", ui.ToastInfo, "A message that is far too long for sixty percent of this window", 60},
		{"info-20-cut", ui.ToastInfo, "A message that is far too long for this window", 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, tt.name, toastIn(t, tt.kind, tt.text, tt.cols).render())
		})
	}
}

// TestEveryLineFits: at any float size, the toast draws exactly its rows,
// none wider than it (Rex can make a float a row taller than asked).
func TestEveryLineFits(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	for w := 1; w <= 60; w += 5 {
		for h := 1; h <= 3; h++ {
			m := newModel(ui.ToastOK, "Parked api ✓ 漢字", 0)
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			lines := strings.Split(m.render(), "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("%dx%d line %d is %d wide", w, h, i, got)
				}
			}
		}
	}
}

// TestCloses: the toast's time running out, a key or a click quits it.
func TestCloses(t *testing.T) {
	for _, msg := range []tea.Msg{expiredMsg{}, tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.MouseClickMsg{Button: tea.MouseLeft}} {
		m := toastIn(t, ui.ToastOK, "hi", 60)
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatalf("%T did not close the toast", msg)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%T: not a quit", msg)
		}
	}
}

// TestWidth: the float fits a short message, and a long one never takes
// more than ToastMaxShare of the window.
func TestWidth(t *testing.T) {
	m := toastIn(t, ui.ToastOK, "hi", 100)
	if row := strings.TrimRight(ansi.Strip(m.render()), " "); !strings.HasSuffix(row, "hi") || m.w > 12 {
		t.Errorf("%d wide for %q", m.w, row)
	}
	for _, cols := range []int{60, 100, 200} {
		m := toastIn(t, ui.ToastWarn, strings.Repeat("long words ", 20), cols)
		if m.w > int(float64(cols)*ui.ToastMaxShare) {
			t.Errorf("%d columns: %d wide", cols, m.w)
		}
		if row := ansi.Strip(m.render()); !strings.Contains(row, "…") {
			t.Errorf("%d columns: not cut: %q", cols, row)
		}
	}
}

func TestKindsAndLifetimes(t *testing.T) {
	tests := []struct {
		in   string
		kind ui.ToastKind
		ttl  time.Duration
	}{
		{"ok", ui.ToastOK, ui.ToastOKTTL},
		{"", ui.ToastInfo, ui.ToastInfoTTL},
		{"WARN", ui.ToastWarn, 6 * time.Second},
		{"error", ui.ToastErr, 8 * time.Second},
	}
	for _, tt := range tests {
		kind, err := parseKind(tt.in)
		if err != nil || kind != tt.kind {
			t.Errorf("parseKind(%q) = %v, %v", tt.in, kind, err)
		}
		if got := newModel(kind, "x", 0).ttl; got != tt.ttl {
			t.Errorf("%q lifetime %v, want %v", tt.in, got, tt.ttl)
		}
	}
	if _, err := parseKind("loud"); err == nil {
		t.Error("unknown kind accepted")
	}
	if got := newModel(ui.ToastOK, "x", time.Second).ttl; got != time.Second {
		t.Errorf("--ttl ignored: %v", got)
	}
	if got := newModel(ui.ToastOK, "a\x1b[2Jb\nc", 0).text; strings.ContainsAny(got, "\x1b\n") {
		t.Errorf("message not sanitized: %q", got)
	}
	if err := Main(nil); err == nil || !strings.Contains(err.Error(), "--message") {
		t.Errorf("missing message: %v", err)
	}
}

// agentAt is the attention toast of a snap state in a window cols wide.
func agentAt(t *testing.T, state string, cols int) *model {
	t.Helper()
	a, ok := agentToast(state)
	if !ok {
		t.Fatalf("no agent toast %q", state)
	}
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(a.Kind, a.Text, 0)
	m.status = a.Status
	m.Update(tea.WindowSizeMsg{Width: m.theme.ToastWidth(m.toast(), cols), Height: 1})
	return m
}

func TestAgentGolden(t *testing.T) {
	for _, tt := range []struct {
		state string
		cols  int
	}{
		{"agent-blocked", 160}, {"agent-question", 160}, {"agent-failed", 160}, {"agent-done", 160},
		{"agent-many", 160}, {"agent-next", 160}, {"agent-last", 160},
		{"agent-blocked", 90}, {"agent-question", 80}, {"agent-last", 90},
	} {
		name := fmt.Sprintf("%s-%d", tt.state, tt.cols)
		t.Run(name, func(t *testing.T) {
			uitest.Golden(t, name, agentAt(t, tt.state, tt.cols).render())
		})
	}
}

// TestCutKeepsTheKey: a toast too long for its window gives up words of the
// message, never the key it names or what the key does.
func TestCutKeepsTheKey(t *testing.T) {
	for w := 40; w <= 160; w += 10 {
		row := strings.Split(ansi.Strip(agentAt(t, "agent-failed", w).render()), "\n")[0]
		if !strings.Contains(row, "ctrl shift a▌ go") {
			t.Errorf("%d wide: the key was cut: %q", w, strings.TrimSpace(row))
		}
	}
}

// TestCutKeepsWhatTheAgentWants: in a window 70 columns or wider, a toast
// gives up the agent's detail and whereabouts before it cuts who the agent
// is and what it wants, and the key's width costs it none of that.
func TestCutKeepsWhatTheAgentWants(t *testing.T) {
	for state, want := range map[string]string{"agent-blocked": "codex needs permission", "agent-question": "claude asks",
		"agent-failed": "gemini failed", "agent-done": "claude finished", "agent-many": "3 agents need you",
		"agent-next": "3 more need you", "agent-last": "No other agent needs you"} {
		for w := 70; w <= 160; w += 10 {
			row := strings.Split(ansi.Strip(agentAt(t, state, w).render()), "\n")[0]
			if !strings.Contains(row, want) {
				t.Errorf("%s %d wide: %q, want %q in it", state, w, strings.TrimSpace(row), want)
			}
		}
	}
}

// TestCutEndsOnAWord: a cut leaves no space, separator or "…" of its own
// before the "…" ("studio…", not "studio ›…" or "studio …").
func TestCutEndsOnAWord(t *testing.T) {
	for _, state := range []string{"agent-blocked", "agent-question", "agent-failed", "agent-done", "agent-many",
		"agent-next", "agent-last"} {
		for w := 60; w <= 160; w++ {
			row := strings.Split(ansi.Strip(agentAt(t, state, w).render()), "\n")[0]
			for _, bad := range []string{" …", "›…", "·…", "……"} {
				if strings.Contains(row, bad) {
					t.Errorf("%s %d wide: %q has %q", state, w, strings.TrimSpace(row), bad)
				}
			}
		}
	}
}

// TestClickGoes: a click on an agent's toast goes to the agent, and the
// toast waits for that before it quits.
func TestClickGoes(t *testing.T) {
	m := agentAt(t, "agent-done", 120)
	went := 0
	m.jump = func(context.Context) error { went++; return nil }
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 2})
	if !m.going || cmd == nil {
		t.Fatal("a click on the toast did not go")
	}
	if _, quit := m.Update(expiredMsg{}); quit != nil {
		t.Fatal("quit before going to the agent was done")
	}
	jumped := cmd()
	if _, ok := jumped.(jumpedMsg); !ok || went != 1 {
		t.Fatalf("went %d times, then %T", went, jumped)
	}
	if _, quit := m.Update(jumped); quit == nil {
		t.Error("the toast stayed after going")
	}
}

// TestMeasure: --measure prints the width the toast takes, and shows none.
func TestMeasure(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	var out strings.Builder
	stdout = &out
	t.Cleanup(func() { stdout = os.Stdout })
	if err := Main([]string{"--message", "Copied a3f9c21", "--kind", "ok", "--measure", "80"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "21" {
		t.Errorf("measured %q, want 21: the tile, a gap and the message", got)
	}
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]ui.Status{"": ui.StatusNone, "blocked": ui.StatusBlocked,
		"error": ui.StatusFailed, "failed": ui.StatusFailed, "done": ui.StatusDone} {
		if got, err := parseStatus(in); err != nil || got != want {
			t.Errorf("parseStatus(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := parseStatus("working"); err == nil {
		t.Error("parseStatus accepted working")
	}
}
