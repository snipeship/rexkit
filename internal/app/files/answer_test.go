package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui/uitest"
)

// answered returns the action of the result Lua would read, "" for none.
func (e *testEnv) answered(t *testing.T) string {
	t.Helper()
	var r popup.Result
	err := state.ReadJSON(filepath.Join(e.state, popup.ResultFile), &r)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return r.Action
}

// mark creates Lua's commit marker: the next ⏎ comes from the handshake.
func (e *testEnv) mark(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.state, popup.CommitFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) marked() bool {
	_, err := os.Stat(filepath.Join(e.state, popup.CommitFile))
	return err == nil
}

// TestHandshakeKeysAreAnswered checks that every ⏎ and ⎋ Files keeps open
// for is answered with "stay", so the Lua layer never waits it out.
func TestHandshakeKeysAreAnswered(t *testing.T) {
	home, root := fixtureTree(t)
	tests := []struct {
		name  string
		setup func(t *testing.T, e *testEnv)
		key   string
		want  string
	}{
		{"⎋ leaves the path field", func(t *testing.T, e *testEnv) { e.press(t, "ctrl+l") }, "esc", popup.ActionStay},
		{"⏎ in the path field", func(t *testing.T, e *testEnv) { e.press(t, "ctrl+l") }, "enter", popup.ActionStay},
		{"⏎ closes the key panel and opens", func(t *testing.T, e *testEnv) { e.press(t, "?") }, "enter", "files.open"},
		{"⎋ closes the key panel", func(t *testing.T, e *testEnv) { e.press(t, "?") }, "esc", popup.ActionStay},
		{"⏎ whose verb fails", func(t *testing.T, e *testEnv) {
			e.rex.Fail("session.new_window", &rex.Error{Op: "session.new_window", Message: "boom"})
		}, "enter", popup.ActionStay},
		{"⏎ that opens a tab", func(*testing.T, *testEnv) {}, "enter", "files.open"},
		{"a verb failing after another key answers nothing", func(t *testing.T, e *testEnv) {
			e.m.cfg.launcher.origin = "block:gone"
		}, "alt+enter", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, home, root, 120, 30)
			tt.setup(t, e)
			if tt.key == "enter" {
				e.mark(t)
			}
			e.press(t, tt.key)
			if got := e.answered(t); got != tt.want {
				t.Errorf("answered %q, want %q", got, tt.want)
			}
			if e.marked() {
				t.Error("the commit marker outlived its ⏎")
			}
		})
	}
}

// TestTypedEnterAnswersNothing: a ⏎ typed while the picker mode is not
// active has no commit marker and nobody waits for it, so a failing verb
// only shows its toast; a stale marker is taken even by a ⏎ Files ignores.
func TestTypedEnterAnswersNothing(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.rex.Fail("session.new_window", &rex.Error{Op: "session.new_window", Message: "boom"})
	e.press(t, "enter")
	if got := e.answered(t); got != "" {
		t.Errorf("answered %q to a ⏎ nobody waits for", got)
	}
	e.m.busy = true
	e.mark(t)
	e.press(t, "enter")
	if e.marked() {
		t.Error("a ⏎ while busy left the commit marker")
	}
}

// TestVerbWaitsForTheRows types a name and presses ⏎ before the listing has
// loaded: the verb runs once the rows show what was typed.
func TestVerbWaitsForTheRows(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	load := e.m.load() // a reload in flight, as when Files has just started
	e.typeText(t, "docs")
	e.press(t, "enter")
	if len(e.rex.windows) != 0 || e.m.pending == nil {
		t.Fatalf("windows=%d pending=%v: ⏎ must wait for the listing", len(e.rex.windows), e.m.pending)
	}
	e.do(t, load)
	if len(e.rex.windows) != 1 || e.rex.windows[0].Label != "docs" {
		t.Fatalf("windows = %+v, want one tab in docs", e.rex.windows)
	}
}

// TestBlurWhileOpeningDoesNotClose: the new terminal takes focus while the
// verb still writes its result; that blur must not end Files and overwrite
// the answer with "close".
func TestBlurWhileOpeningDoesNotClose(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30, func(c *config) { c.dismissOnBlur = true })
	e.m.busy = true // as while a verb runs
	e.send(t, tea.BlurMsg{})
	if e.m.done {
		t.Fatal("a blur caused by Files' own verb closed it")
	}
}

// TestHeldEnterAnswersBeforeLuaGivesUp: a ⏎ held for a slow listing must
// not outlast Lua's wait (popup.AwaitTimeout). After popup.HandshakeGrace
// Files answers "stay" (Lua keeps its mode, no error), and the verb still
// runs once the rows settle, writing its own result after.
func TestHeldEnterAnswersBeforeLuaGivesUp(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	load := e.m.load() // a slow listing in flight
	e.typeText(t, "docs")
	e.mark(t)
	_, cmd := e.m.Update(uitest.Key(t, "enter")) // the grace timer is not run here
	if e.m.pending == nil || cmd == nil || e.answered(t) != "" {
		t.Fatalf("pending %v, answered %q: ⏎ must wait without answering yet", e.m.pending, e.answered(t))
	}
	e.send(t, stillPendingMsg{rev: e.m.pendingRev})
	if got := e.answered(t); got != popup.ActionStay {
		t.Fatalf("after the grace answered %q, want stay", got)
	}
	if toast := ansi.Strip(e.m.theme.Toast(e.m.toast.Current()).Render()); !strings.Contains(toast, "enter▌ runs when it's ready") {
		t.Errorf("toast %q", toast)
	}
	e.do(t, load)
	if len(e.rex.windows) != 1 || e.rex.windows[0].Label != "docs" || e.answered(t) != "files.open" {
		t.Fatalf("windows %+v answered %q", e.rex.windows, e.answered(t))
	}
	// A timer from a verb that already ran answers nothing more.
	e.send(t, stillPendingMsg{rev: e.m.pendingRev})
	if got := e.answered(t); got != "files.open" {
		t.Fatalf("a late timer overwrote the result with %q", got)
	}
}
