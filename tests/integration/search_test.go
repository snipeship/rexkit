//go:build integration

package integration

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// printer is a pane that prints a script's output and then waits.
func printer(label, script string) rex.Node {
	n := rex.Command(label, "", "/bin/sh", "-c", script+"; exec /bin/cat")
	n.Block.Options.Exit = nil
	login := false
	n.Block.Options.Login = &login
	return n
}

// TestSearch searches two sessions from a third. ⏎ on a match in another
// session goes there and opens Scrollback over its pane on the capture
// that was searched, at the match's line (far from the bottom, where
// Scrollback would open by itself), with the query as Neovim's search. F1
// lists Search's keys; ⌥⏎ goes to a pane of the origin's session; ⎋
// closes.
func TestSearch(t *testing.T) {
	s := start(t)
	logs, logBlocks := s.CreateSession(t, "logs", hsplit(0.5,
		printer("server", "seq 1 200; echo 'ERROR needle in the haystack'; seq 201 600"), cat("idle", "")))
	work, workBlocks := s.CreateSession(t, "work", hsplit(0.5,
		printer("origin", "echo 'a needle at home'"), cat("other", "")))
	origin, server := workBlocks[0], logBlocks[0]
	waitScreen(t, s, logs.ID, server, "600")
	waitScreen(t, s, work.ID, origin, "a needle at home")
	focus(t, s, work.ID, origin)

	t.Run("F1 lists the keys, ⎋ closes", func(t *testing.T) {
		o := open(t, s, work.ID, origin, "rexkit_search", nil, "Search every pane")
		out := act(t, s, work.ID, o.Block, "rexkit_keys", nil)
		if got := result(t, out)["forwarded"]; got != "search" {
			t.Fatalf("F1 opened %s, want it forwarded", out.Result)
		}
		waitScreen(t, s, work.ID, o.Block, "Open the line in Scrollback")
		if fl := floats(t, s, work.ID); len(fl) != 1 {
			t.Errorf("%d floats: the key guide opened over Search", len(fl))
		}
		act(t, s, work.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"}) // closes the key panel
		act(t, s, work.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
		waitNoFloats(t, s, work.ID)
		assertFocus(t, s, work.ID, origin, "")
	})

	t.Run("⏎ opens Scrollback at the match in another session", func(t *testing.T) {
		o := open(t, s, work.ID, origin, "rexkit_search", nil, "Search every pane")
		s.Drive(t, work.ID, harness.Type(t, o.Block, "haystack")...)
		waitScreen(t, s, work.ID, o.Block, "jump to line 201") // the one match
		done := act(t, s, work.ID, o.Block, "rexkit_commit", map[string]string{"key": "enter"})
		sel, ok := queued(done, "session.select")
		if !ok || sel["session_id"] != logs.ID {
			t.Fatalf("queued %s, want the logs session selected", names(done))
		}
		res := result(t, done)
		if res["invoke"] != "rexkit_search_jump" || res["block_id"] != server {
			t.Errorf("result %s", done.Result)
		}
		waitNoFloats(t, s, work.ID)
		var sb float
		harness.WaitFor(t, 10*time.Second, "Scrollback over the server pane", func() bool {
			for _, f := range floats(t, s, logs.ID) {
				if f.Block.Label == "rexkit:scrollback" {
					sb = f
					return true
				}
			}
			return false
		})
		screen := ""
		harness.WaitFor(t, 15*time.Second, "Neovim at the match", func() bool {
			screen = s.Capture(t, logs.ID, sb.Block.ID)
			return strings.Contains(screen, "ERROR needle in the haystack") && strings.Contains(screen, "next, previous")
		})
		if strings.Contains(screen, "\n600") || strings.Contains(screen, " 600\n") {
			t.Errorf("Scrollback opened at the bottom:\n%s", screen)
		}
		if f, _ := s.Focus(t, logs.ID); f != sb.Block.ID {
			t.Errorf("focus %s, want Scrollback", f)
		}
		// Scrollback took the page Search handed it
		pages, _ := filepath.Glob(filepath.Join(s.XDG("XDG_STATE_HOME"), "rexkit", "tools", "*", "page.txt"))
		if len(pages) != 0 {
			t.Errorf("pages left behind: %q", pages)
		}
		s.Drive(t, logs.ID, harness.Press(t, sb.Block.ID, "q"))
		waitNoFloats(t, s, logs.ID)
		assertFocus(t, s, logs.ID, server, "")
	})

	t.Run("⌥⏎ goes to a pane of the origin's session", func(t *testing.T) {
		focus(t, s, work.ID, workBlocks[1])
		o := open(t, s, work.ID, workBlocks[1], "rexkit_search", nil, "Search every pane")
		s.Drive(t, work.ID, harness.Type(t, o.Block, "at home")...)
		waitScreen(t, s, work.ID, o.Block, "jump to line 1")
		s.Drive(t, work.ID, harness.Press(t, o.Block, "alt+enter"))
		waitNoFloats(t, s, work.ID)
		assertFocus(t, s, work.ID, origin, "")
	})
}

// TestGotoHandsItsQueryToSearch: ⌃F in Go to closes it and opens Search over
// the same pane, with Go to's query typed and searched, as if Search had
// been opened there: the origin's session comes first, ⏎ on a match opens
// it in Scrollback, and ⎋ goes back to the origin, zoomed as it was. A ⌃F
// that reaches Go to without Lua's handshake (no picker mode binds it, as
// with commit_via = "global") gets Search all the same.
func TestGotoHandsItsQueryToSearch(t *testing.T) {
	s := start(t)
	logs, logBlocks := s.CreateSession(t, "logs", hsplit(0.5,
		printer("server", "seq 1 50; echo 'ERROR needle in the haystack'; seq 51 100"), cat("idle", "")))
	work, workBlocks := s.CreateSession(t, "work", hsplit(0.5,
		printer("origin", "echo 'a haystack at home'"), cat("other", "")))
	origin, server := workBlocks[0], logBlocks[0]
	waitScreen(t, s, logs.ID, server, "100")
	waitScreen(t, s, work.ID, origin, "a haystack at home")
	focus(t, s, work.ID, origin)

	// search waits for Search's float in the work session and returns it.
	search := func(t *testing.T) float {
		t.Helper()
		var sf float
		harness.WaitFor(t, 10*time.Second, "Search's float", func() bool {
			fl := floats(t, s, work.ID)
			if len(fl) == 1 && fl[0].Block.Label == "rexkit:search" {
				sf = fl[0]
				return true
			}
			return false
		})
		return sf
	}

	t.Run("⌃F with the handshake", func(t *testing.T) {
		zoom(t, s, work.ID, origin)
		o := open(t, s, work.ID, origin, "rexkit_goto", nil, "Search sessions")
		s.Drive(t, work.ID, harness.Type(t, o.Block, "haystack")...)
		waitScreen(t, s, work.ID, o.Block, "No place matches “haystack”")
		waitScreen(t, s, work.ID, o.Block, "search the text of every pane")
		done := act(t, s, work.ID, o.Block, "rexkit_search_text", nil)
		if res := result(t, done); res["action"] != "invoke" || res["invoke"] != "rexkit_search" {
			t.Fatalf("result %s, want Search invoked", done.Result)
		}
		if _, ok := queued(done, "session.select"); ok {
			t.Errorf("queued %s: the hand-off stays where it is", names(done))
		}
		if mode, ok := queued(done, "client.mode.enter"); !ok || mode["name"] != "picker" {
			t.Errorf("queued %s, want Search's picker mode", names(done))
		}
		sf := search(t)
		// The panes come in in any order: wait for both matches, then the
		// origin's comes first, and is selected.
		var screen string
		harness.WaitFor(t, 10*time.Second, "both matches", func() bool {
			screen = s.Capture(t, work.ID, sf.Block.ID)
			return strings.Contains(screen, "a haystack at home") && strings.Contains(screen, "ERROR needle in the haystack")
		})
		home, there := strings.Index(screen, "a haystack at home"), strings.Index(screen, "ERROR needle in the haystack")
		if home < 0 || there < home {
			t.Errorf("want the origin's session first:\n%s", screen)
		}
		if !strings.Contains(screen, "haystack") || !regexp.MustCompile(`jump to line 1\b`).MatchString(screen) {
			t.Errorf("no query or not the origin's match selected:\n%s", screen)
		}
		if f, z := s.Focus(t, work.ID); f != sf.Block.ID || z != "" {
			t.Errorf("focus %s zoom %q, want Search focused", f, z)
		}
		act(t, s, work.ID, sf.Block.ID, "rexkit_dismiss", map[string]string{"key": "escape"})
		waitNoFloats(t, s, work.ID)
		assertFocus(t, s, work.ID, origin, origin)
		call(t, s, "session.zoom_block", map[string]any{"session_id": work.ID, "block_id": origin, "zoom": false}, nil)
	})

	t.Run("⏎ in that Search opens Scrollback at the match", func(t *testing.T) {
		o := open(t, s, work.ID, origin, "rexkit_goto", nil, "Search sessions")
		s.Drive(t, work.ID, harness.Type(t, o.Block, "needle")...)
		act(t, s, work.ID, o.Block, "rexkit_search_text", nil)
		sf := search(t)
		waitScreen(t, s, work.ID, sf.Block.ID, "jump to line 51")
		done := act(t, s, work.ID, sf.Block.ID, "rexkit_commit", map[string]string{"key": "enter"})
		res := result(t, done)
		if res["invoke"] != "rexkit_search_jump" || res["block_id"] != server {
			t.Fatalf("result %s, want a jump to the server pane", done.Result)
		}
		waitNoFloats(t, s, work.ID)
		var sb float
		harness.WaitFor(t, 10*time.Second, "Scrollback over the server pane", func() bool {
			for _, f := range floats(t, s, logs.ID) {
				if f.Block.Label == "rexkit:scrollback" {
					sb = f
					return true
				}
			}
			return false
		})
		s.WaitScreen(t, logs.ID, sb.Block.ID, "ERROR needle in the haystack", 15*time.Second)
		call(t, s, "session.close_layer", map[string]string{"session_id": logs.ID, "layer_id": sb.Layer.ID}, nil)
		waitNoFloats(t, s, logs.ID)
		focus(t, s, work.ID, origin)
	})

	t.Run("⌃F typed without the handshake", func(t *testing.T) {
		zoom(t, s, work.ID, origin)
		o := open(t, s, work.ID, origin, "rexkit_goto", nil, "Search sessions")
		s.Drive(t, work.ID, harness.Type(t, o.Block, "needle")...)
		waitScreen(t, s, work.ID, o.Block, "search the text of every pane")
		s.Drive(t, work.ID, harness.Press(t, o.Block, "ctrl+f"))
		sf := search(t)
		screen := s.WaitScreen(t, work.ID, sf.Block.ID, "ERROR needle in the haystack", 10*time.Second)
		if !strings.Contains(screen, "needle") || !strings.Contains(screen, "jump to line 51") {
			t.Errorf("no query or no selected match:\n%s", screen)
		}
		if f, z := s.Focus(t, work.ID); f != sf.Block.ID || z != "" {
			t.Errorf("focus %s zoom %q, want Search focused", f, z)
		}
		act(t, s, work.ID, sf.Block.ID, "rexkit_dismiss", map[string]string{"key": "escape"})
		waitNoFloats(t, s, work.ID)
		assertFocus(t, s, work.ID, origin, origin)
		call(t, s, "session.zoom_block", map[string]any{"session_id": work.ID, "block_id": origin, "zoom": false}, nil)
	})
}

// TestGotoHandsItsQueryToSearchGlobal: with commit_via = "global" no mode
// binds ⌃F, so it reaches Go to as a key, and Go to starts Search itself.
// The global ⏎ in that Search then opens Scrollback at the match, over the
// pane in the other session.
func TestGotoHandsItsQueryToSearchGlobal(t *testing.T) {
	s := startWith(t, fmt.Sprintf("return { shell = %q, editor = { %q }, binary = %q, commit_via = %q }",
		"/bin/sh", "/usr/bin/vi", rexkitBin, "global"))
	logs, logBlocks := s.CreateSession(t, "logs", hsplit(0.5,
		printer("server", "seq 1 50; echo 'ERROR needle in the haystack'; seq 51 100"), cat("idle", "")))
	work, workBlocks := s.CreateSession(t, "work", hsplit(0.5,
		printer("origin", "echo 'a haystack at home'"), cat("other", "")))
	origin, server := workBlocks[0], logBlocks[0]
	waitScreen(t, s, logs.ID, server, "100")
	waitScreen(t, s, work.ID, origin, "a haystack at home")
	focus(t, s, work.ID, origin)

	o := open(t, s, work.ID, origin, "rexkit_goto", nil, "Search sessions")
	if _, ok := queued(o.Outcome, "client.mode.enter"); ok {
		t.Errorf("Go to entered a mode (queued %s)", names(o.Outcome))
	}
	s.Drive(t, work.ID, harness.Type(t, o.Block, "needle")...)
	waitScreen(t, s, work.ID, o.Block, "No place matches “needle”")
	s.Drive(t, work.ID, harness.Press(t, o.Block, "ctrl+f"))
	var sf float
	harness.WaitFor(t, 10*time.Second, "Search's float", func() bool {
		fl := floats(t, s, work.ID)
		if len(fl) == 1 && fl[0].Block.Label == "rexkit:search" {
			sf = fl[0]
			return true
		}
		return false
	})
	waitScreen(t, s, work.ID, sf.Block.ID, "jump to line 51")
	if f, _ := s.Focus(t, work.ID); f != sf.Block.ID {
		t.Errorf("focus %s, want Search focused", f)
	}
	done := pressGlobal(t, s, work.ID, sf.Block.ID, "enter")
	if res := result(t, done); res["invoke"] != "rexkit_search_jump" || res["block_id"] != server {
		t.Fatalf("result %s, want a jump to the server pane", done.Result)
	}
	waitNoFloats(t, s, work.ID)
	var sb float
	harness.WaitFor(t, 10*time.Second, "Scrollback over the server pane", func() bool {
		for _, f := range floats(t, s, logs.ID) {
			if f.Block.Label == "rexkit:scrollback" {
				sb = f
				return true
			}
		}
		return false
	})
	s.WaitScreen(t, logs.ID, sb.Block.ID, "ERROR needle in the haystack", 15*time.Second)
}
