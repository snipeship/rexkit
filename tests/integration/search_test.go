//go:build integration

package integration

import (
	"path/filepath"
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
