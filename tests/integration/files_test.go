//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// fixture is a small folder tree for Files to browse.
func fixture(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tiledRect is a block's rect in its window's tiled layer, and the window.
func tiledRect(t *testing.T, s *harness.Server, session, block string) (rex.Rect, string) {
	t.Helper()
	for _, w := range s.View(t, session).Windows {
		for _, l := range w.Layers {
			if r, ok := l.Rect(block); ok && !l.Floating() {
				return r, w.ID
			}
		}
	}
	t.Fatalf("block %s is not tiled in %s", block, session)
	return rex.Rect{}, ""
}

// newBlocks lists the blocks of session that are not in before.
func newBlocks(t *testing.T, s *harness.Server, session string, before []string) []string {
	t.Helper()
	var out []string
	for _, w := range s.View(t, session).Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if !slices.Contains(before, b.ID) {
					out = append(out, b.ID)
				}
			}
		}
	}
	return out
}

// cwd waits for a block's program and returns its working directory.
func cwd(t *testing.T, s *harness.Server, session, block string) string {
	t.Helper()
	var dir string
	harness.WaitFor(t, 5*time.Second, "a program in "+block, func() bool {
		p, err := s.Rex.Process(context.Background(), session, block)
		if err == nil {
			dir = p.Current().Cwd
		}
		return dir != ""
	})
	return dir
}

// TestFilesVerbs opens Files from a pane whose program runs in a fixture
// folder, picks a folder by typing its name, and checks where each verb puts
// the new terminal: ⏎ a new tab named after the folder, ⌥⏎ a split right of
// the origin pane, ⇧⏎ a split below it. Each lands in the folder, gets
// focus, and the float closes.
func TestFilesVerbs(t *testing.T) {
	s := start(t)
	dir := fixture(t)
	sess, blocks := s.CreateSession(t, "files", hsplit(0.5, cat("origin", dir), cat("other", "")))
	origin := blocks[0]

	tests := []struct {
		name   string
		folder string
		key    string // "" commits through the picker mode's ⏎
		check  func(t *testing.T, rect rex.Rect, window string)
	}{
		{"enter opens a tab", "alpha", "", func(t *testing.T, _ rex.Rect, window string) {
			w, _ := s.View(t, sess.ID).Window(window)
			if window == sess.WindowID || w.Label != "alpha" {
				t.Errorf("window %s %q, want a new tab labelled alpha", window, w.Label)
			}
		}},
		{"alt+enter splits right", "beta", "alt+enter", func(t *testing.T, rect rex.Rect, window string) {
			o, _ := tiledRect(t, s, sess.ID, origin)
			if window != sess.WindowID || rect[0] <= o[0] || rect[1] != o[1] {
				t.Errorf("new pane at %v in %s, want right of the origin %v in %s", rect, window, o, sess.WindowID)
			}
		}},
		{"shift+enter splits below", "alpha", "shift+enter", func(t *testing.T, rect rex.Rect, window string) {
			o, _ := tiledRect(t, s, sess.ID, origin)
			if window != sess.WindowID || rect[1] <= o[1] || rect[0] != o[0] {
				t.Errorf("new pane at %v in %s, want below the origin %v in %s", rect, window, o, sess.WindowID)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := newBlocks(t, s, sess.ID, nil)
			focus(t, s, sess.ID, origin)
			o := open(t, s, sess.ID, origin, "rexkit_files", nil, "alpha/")
			in := harness.Type(t, o.Block, tt.folder)
			if tt.key == "" {
				in = append(in, harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
			} else {
				in = append(in, harness.Press(t, o.Block, tt.key))
			}
			outs := s.Drive(t, sess.ID, in...)
			if tt.key == "" {
				if res := result(t, outs[len(outs)-1]); res["action"] != "files.open" {
					t.Fatalf("result %v, want files.open", res)
				}
			}
			waitNoFloats(t, s, sess.ID)

			var created []string
			harness.WaitFor(t, 5*time.Second, "the new terminal", func() bool {
				created = newBlocks(t, s, sess.ID, append(before, o.Block))
				return len(created) == 1
			})
			rect, window := tiledRect(t, s, sess.ID, created[0])
			tt.check(t, rect, window)
			if got := cwd(t, s, sess.ID, created[0]); got != filepath.Join(dir, tt.folder) {
				t.Errorf("new terminal runs in %s, want %s", got, filepath.Join(dir, tt.folder))
			}
			v := s.View(t, sess.ID)
			if w, _ := v.Window(window); v.ActiveWindowID != window || w.FocusedBlockID != created[0] {
				t.Errorf("focus %s in %s, want the new terminal %s in %s", w.FocusedBlockID, v.ActiveWindowID, created[0], window)
			}
		})
	}
}

// TestFilesTypedBeforeItIsUp opens Files and types a folder name and ⏎ at
// once: the new tab opens in that folder, not in the one Files started in.
func TestFilesTypedBeforeItIsUp(t *testing.T) {
	s := start(t)
	dir := fixture(t)
	sess, blocks := s.CreateSession(t, "files", cat("origin", dir))
	before := blocks
	in := []harness.Input{harness.Act(t, blocks[0], "rexkit_files", nil)}
	in = append(in, harness.Type(t, "", "beta")...)
	in = append(in, harness.Act(t, "", "rexkit_commit", map[string]string{"key": "enter"}))
	outs := s.Drive(t, sess.ID, in...)
	if res := result(t, outs[len(outs)-1]); res["action"] != "files.open" {
		t.Fatalf("result %v, want files.open", res)
	}
	waitNoFloats(t, s, sess.ID)
	var created []string
	harness.WaitFor(t, 5*time.Second, "the new terminal", func() bool {
		created = newBlocks(t, s, sess.ID, before)
		return len(created) == 1
	})
	if got := cwd(t, s, sess.ID, created[0]); got != filepath.Join(dir, "beta") {
		t.Errorf("new terminal runs in %s, want %s", got, filepath.Join(dir, "beta"))
	}
}
