package toast

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

// toastAt is a toast in a w×h strip, grown in whole.
func toastAt(t *testing.T, kind ui.ToastKind, text string, w, h int) *model {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(kind, text, 0)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for range ui.ToastRows {
		m.Update(frameMsg{})
	}
	return m
}

func TestGolden(t *testing.T) {
	tests := []struct {
		name string
		kind ui.ToastKind
		text string
		w, h int
	}{
		{"ok-90x4", ui.ToastOK, "Parked api in Scratch · ⌃P e brings it back", 90, 4},
		{"err-120x5", ui.ToastErr, "Could not open the editor: nvim exited with status 1", 120, 5},
		{"warn-60x4", ui.ToastWarn, "Scratch is full", 60, 4},
		{"info-60x4-cut", ui.ToastInfo, "A message that is far too long for sixty percent of this strip", 60, 4},
		{"ok-48x3", ui.ToastOK, "Renamed the pane", 48, 3},
		{"err-40x1", ui.ToastErr, "Could not park: the pane is gone", 40, 1},
		{"info-20x1-cut", ui.ToastInfo, "A message that is far too long for this float", 20, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, tt.name, toastAt(t, tt.kind, tt.text, tt.w, tt.h).render())
		})
	}
}

func TestEveryLineFits(t *testing.T) {
	for w := 1; w <= 60; w += 5 {
		for h := 1; h <= 5; h++ {
			lines := strings.Split(toastAt(t, ui.ToastOK, "Parked api ✓ 漢字", w, h).render(), "\n")
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

// TestCloses: the toast's time running out, a key or a click shrinks it
// away a row a frame, then quits.
func TestCloses(t *testing.T) {
	for _, msg := range []tea.Msg{expiredMsg{}, tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.MouseClickMsg{Button: tea.MouseLeft}} {
		m := toastAt(t, ui.ToastOK, "hi", 60, 4)
		_, cmd := m.Update(msg)
		for shown := ui.ToastRows; shown > 1; shown-- {
			if cmd == nil || m.shown != shown {
				t.Fatalf("%T: %d rows shown, want %d", msg, m.shown, shown)
			}
			_, cmd = m.Update(frameMsg{})
		}
		if _, cmd = m.Update(frameMsg{}); cmd == nil {
			t.Fatalf("%T did not close the toast", msg)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%T: not a quit", msg)
		}
	}
}

// TestGrowsIn: the toast enters a row a frame: its top edge, then its text,
// then its bottom edge, in rows 1–3 of the strip.
func TestGrowsIn(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(ui.ToastErr, "Could not park", 0)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: Rows})
	for shown := 1; shown <= ui.ToastRows; shown++ {
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		for y, l := range lines {
			if drawn := strings.TrimSpace(l) != ""; drawn != (y >= 1 && y <= shown) {
				t.Fatalf("frame %d: row %d drawn = %v:\n%s", shown, y, drawn, strings.Join(lines, "\n"))
			}
		}
		m.Update(frameMsg{})
	}
}

// TestPlacement: the toast's right end sits ToastMarginCols from the strip's
// right edge, and it never takes more than ToastMaxShare of the width.
func TestPlacement(t *testing.T) {
	for _, text := range []string{"hi", strings.Repeat("long words ", 20)} {
		m := toastAt(t, ui.ToastWarn, text, 100, Rows)
		row := []rune(strings.Split(ansi.Strip(m.render()), "\n")[2])
		end := strings.LastIndex(string(row), "▌")
		start := strings.Index(string(row), "▐")
		if end < 0 || start < 0 {
			t.Fatalf("no toast in %q", string(row))
		}
		right := len(row) - 1 - len([]rune(string(row)[:end]))
		width := len([]rune(string(row)[start:end])) + 1
		if right != ui.ToastMarginCols || width > int(100*ui.ToastMaxShare) {
			t.Errorf("%q: %d cells from the edge, %d wide", text, right, width)
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

// agentAt is the attention toast of a snap state in a w×Rows strip.
func agentAt(t *testing.T, state string, w int) *model {
	t.Helper()
	a, ok := agentToast(state)
	if !ok {
		t.Fatalf("no agent toast %q", state)
	}
	m := toastAt(t, a.Kind, a.Text, w, Rows)
	m.status = a.Status
	return m
}

func TestAgentGolden(t *testing.T) {
	for _, tt := range []struct {
		state string
		w     int
	}{
		{"agent-blocked", 160}, {"agent-question", 160}, {"agent-failed", 160}, {"agent-done", 160},
		{"agent-many", 160}, {"agent-next", 160}, {"agent-last", 160},
		{"agent-blocked", 90}, {"agent-question", 80}, {"agent-last", 90},
	} {
		name := fmt.Sprintf("%s-%dx%d", tt.state, tt.w, Rows)
		t.Run(name, func(t *testing.T) {
			uitest.Golden(t, name, agentAt(t, tt.state, tt.w).render())
		})
	}
}

// TestCutKeepsTheKey: a toast too long for its strip gives up words of the
// message, never the key it names or what the key does.
func TestCutKeepsTheKey(t *testing.T) {
	for w := 40; w <= 160; w += 10 {
		row := strings.Split(ansi.Strip(agentAt(t, "agent-failed", w).render()), "\n")[2]
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
			row := strings.Split(ansi.Strip(agentAt(t, state, w).render()), "\n")[2]
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
			row := strings.Split(ansi.Strip(agentAt(t, state, w).render()), "\n")[2]
			for _, bad := range []string{" …", "›…", "·…", "……"} {
				if strings.Contains(row, bad) {
					t.Errorf("%s %d wide: %q has %q", state, w, strings.TrimSpace(row), bad)
				}
			}
		}
	}
}

// TestClickGoes: a click on an agent's toast goes to the agent, and the
// toast waits for that before it quits; a click beside it only dismisses.
func TestClickGoes(t *testing.T) {
	m := agentAt(t, "agent-done", 120)
	went := 0
	m.jump = func(context.Context) error { went++; return nil }
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 2})
	if !m.leaving || went != 0 {
		t.Fatalf("a click beside the toast: leaving %v, went %d", m.leaving, went)
	}
	_ = cmd

	m = agentAt(t, "agent-done", 120)
	m.jump = func(context.Context) error { went++; return nil }
	_, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 110, Y: 2})
	if !m.going {
		t.Fatal("a click on the toast did not go")
	}
	for range ui.ToastRows {
		m.Update(frameMsg{})
	}
	if _, quit := m.Update(frameMsg{}); quit != nil {
		t.Fatal("quit before going to the agent was done")
	}
	var jumped tea.Msg
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if msg, ok := c().(jumpedMsg); ok {
			jumped = msg
		}
	}
	if went != 1 || jumped == nil {
		t.Fatalf("went %d times, jumped %v", went, jumped)
	}
	if _, quit := m.Update(jumped); quit == nil {
		t.Error("the toast stayed after going")
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
