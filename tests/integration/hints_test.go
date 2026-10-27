//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// hintsWorld is a server whose $PATH starts with a pbcopy and an open that
// write what they got into files, a docker and a kubectl that print what
// they were asked, and a git repository with one commit.
type hintsWorld struct {
	s         *harness.Server
	dir       string // the repository, where the origin pane runs
	sha       string // its commit, short
	clip, url string // what pbcopy and open got
}

func newHintsWorld(t *testing.T) *hintsWorld {
	t.Helper()
	w := &hintsWorld{}
	bin := t.TempDir()
	out := t.TempDir()
	w.clip, w.url = filepath.Join(out, "clip"), filepath.Join(out, "url")
	for name, script := range map[string]string{
		"pbcopy":  "#!/bin/sh\ncat > " + w.clip + "\n",
		"open":    "#!/bin/sh\nprintf '%s' \"$*\" > " + w.url + "\n",
		"kubectl": "#!/bin/sh\necho KUBECTL \"$*\"\n",
		// every id is a container, whose logs end at once (it is stopped)
		"docker": "#!/bin/sh\n[ \"$1\" = container ] && exit 0\necho DOCKER \"$*\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixed dates make the commit, and so its SHA, the same every run.
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2026-01-02T03:04:05Z")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v\n%s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	git("add", "notes.txt")
	git("commit", "-q", "-m", "notes")
	w.sha = git("rev-parse", "--short=7", "HEAD")
	// Hints labels hex with a digit and a letter only.
	if !strings.ContainsAny(w.sha, "0123456789") || !strings.ContainsAny(w.sha, "abcdef") {
		t.Fatalf("the test commit's SHA %s would get no label: change its dates", w.sha)
	}
	w.s = startWith(t, settings, "PATH="+bin+":"+os.Getenv("PATH"))
	return w
}

// pane is a pane in the repository that shows lines, then waits with the
// cursor back on its first row.
func (w *hintsWorld) pane(label string, lines ...string) rex.Node {
	script := `printf '%s\n' "$@"; printf '\033[H'; exec /bin/cat`
	n := rex.Command(label, w.dir, append([]string{"/bin/sh", "-c", script, "sh"}, lines...)...)
	n.Block.Options.Exit = nil
	login := false
	n.Block.Options.Login = &login
	return n
}

// labels opens Hints over origin and reads each text's label off its float:
// the letters drawn right before the text, in the blank cells there (the
// panes leave two blanks before each text, so a label never touches a word).
// It reads the float until every text has one: a capture can catch a frame
// half drawn. When it fails it closes the float, so the next test starts
// without it.
func (w *hintsWorld) labels(t *testing.T, session, origin string, texts ...string) (opened, map[string]string) {
	t.Helper()
	o := open(t, w.s, session, origin, "rexkit_hints", nil, texts[0])
	var got map[string]string
	var screen string
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen = w.s.Capture(t, session, o.Block)
		if got = labelsOn(screen, texts); len(got) == len(texts) {
			return o, got
		}
		if time.Now().After(deadline) {
			w.s.Drive(t, session, harness.Press(t, o.Block, "esc"))
			t.Fatalf("labels %v of %q:\n%s", got, texts, screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// labelsOn reads the labels of texts off a capture.
func labelsOn(screen string, texts []string) map[string]string {
	got := map[string]string{}
	for _, text := range texts {
		for _, line := range strings.Split(screen, "\n") {
			i := strings.Index(line, text)
			if i < 1 {
				continue
			}
			head := strings.ReplaceAll(line[:i], "▐", " ") // ▐ rounds a chip's left end
			if fields := strings.Fields(head); len(fields) > 0 && !strings.HasSuffix(head, " ") {
				got[text] = fields[len(fields)-1]
			}
		}
	}
	return got
}

// hintsFloat is the open Hints float, if any.
func hintsFloat(t *testing.T, s *harness.Server, session string) bool {
	t.Helper()
	for _, f := range floats(t, s, session) {
		if f.Block.Label == "rexkit:hints" {
			return true
		}
	}
	return false
}

// waitToast waits for a toast that says want.
func waitToast(t *testing.T, s *harness.Server, session, want string) {
	t.Helper()
	var last string
	harness.WaitFor(t, 10*time.Second, "a toast saying "+want, func() bool {
		for _, f := range floats(t, s, session) {
			if f.Block.Label == "rexkit:toast" {
				last = s.Capture(t, session, f.Block.ID)
				if strings.Contains(last, want) {
					return true
				}
			}
		}
		return false
	})
}

func waitFile(t *testing.T, path, want string) {
	t.Helper()
	var got []byte
	harness.WaitFor(t, 10*time.Second, path+" to hold "+want, func() bool {
		got, _ = os.ReadFile(path)
		return string(got) == want
	})
}

// TestHints labels a pane, then picks with each modifier: a label copies, ⌥
// and a label pastes into the pane, ⇧ and a label opens a URL, a file at
// its line in the editor setting, a commit in git show, a container's logs
// and a pod in kubectl describe (with the namespace its listing was for),
// each in a split in the pane's folder. Labels go nearest the pane's cursor
// first (Rex's cursor, through rex.API.Screens), and after each pick the
// float closes and focus is back on the pane.
func TestHints(t *testing.T) {
	w := newHintsWorld(t)
	s := w.s
	url, path, ip := "https://example.com/pull/482", "notes.txt:2", "10.42.3.17:8080"
	pod, container := "web-7d9f8b6c5d-x2k4p", "f3a9b2c4d1e8"
	sess, blocks := s.CreateSession(t, "hints", hsplit(0.6,
		w.pane("origin", "  "+url, "  M  "+path, "  ok  "+w.sha, "  on  "+ip,
			"$ kubectl -n staging get pods", "  "+pod, "  ctr  "+container), cat("other", "")))
	origin := blocks[0]
	waitScreen(t, s, sess.ID, origin, container)
	texts := []string{url, path, w.sha, ip}

	t.Run("labels go nearest the cursor first", func(t *testing.T) {
		focus(t, s, sess.ID, origin)
		o, got := w.labels(t, sess.ID, origin, texts...)
		if got[url] != "a" || got[path] != "s" || got[w.sha] != "d" || got[ip] != "f" {
			t.Errorf("labels %v, want a s d f from the top (the cursor's row)", got)
		}
		if _, ok := queued(o.Outcome, "client.mode.enter"); !ok {
			t.Errorf("no mode entered (queued %s)", names(o.Outcome))
		}
		s.Drive(t, sess.ID, harness.Press(t, o.Block, "esc"))
		waitNoFloats(t, s, sess.ID)
		assertFocus(t, s, sess.ID, origin, "")
	})

	t.Run("a label copies", func(t *testing.T) {
		focus(t, s, sess.ID, origin)
		o, got := w.labels(t, sess.ID, origin, ip)
		s.Drive(t, sess.ID, harness.Type(t, o.Block, got[ip])...)
		waitFile(t, w.clip, ip)
		waitToast(t, s, sess.ID, "Copied "+ip)
		harness.WaitFor(t, 5*time.Second, "Hints to close", func() bool { return !hintsFloat(t, s, sess.ID) })
		assertFocus(t, s, sess.ID, origin, "")
	})

	t.Run("shift opens a URL", func(t *testing.T) {
		focus(t, s, sess.ID, origin)
		o, got := w.labels(t, sess.ID, origin, url)
		s.Drive(t, sess.ID, harness.Press(t, o.Block, "shift+"+got[url]))
		waitFile(t, w.url, url)
		harness.WaitFor(t, 5*time.Second, "Hints to close", func() bool { return !hintsFloat(t, s, sess.ID) })
		assertFocus(t, s, sess.ID, origin, "")
	})

	// The editor's file is absolute, so vi is asked where it runs and on
	// which line it is; git show finds the commit only in the repository. A
	// stopped container's logs, which end at once, stay in a pager.
	for _, tt := range []struct {
		name, text, shows string
		pwd               bool
	}{
		{"shift opens a file at its line in a split", path, "second line", true},
		{"shift opens a commit in git show", w.sha, "notes", false},
		{"shift opens a container's logs", container, "DOCKER logs -f " + container, false},
		{"shift describes a pod in its namespace", pod, "KUBECTL describe pod " + pod + " --namespace=staging", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := newBlocks(t, s, sess.ID, nil)
			focus(t, s, sess.ID, origin)
			o, got := w.labels(t, sess.ID, origin, tt.text)
			s.Drive(t, sess.ID, harness.Press(t, o.Block, "shift+"+got[tt.text]))
			var created []string
			harness.WaitFor(t, 10*time.Second, "the split", func() bool {
				created = newBlocks(t, s, sess.ID, append(before, o.Block))
				return len(created) == 1 && !hintsFloat(t, s, sess.ID)
			})
			split := created[0]
			defer call(t, s, "block.close", map[string]string{"session_id": sess.ID, "block_id": split}, nil)
			waitScreen(t, s, sess.ID, split, tt.shows)
			if tt.pwd {
				if err := s.Rex.Write(context.Background(), sess.ID, split, ":.=\r"); err != nil {
					t.Fatal(err)
				}
				harness.WaitFor(t, 5*time.Second, "vi to say it is on line 2", func() bool {
					rows := strings.Split(strings.TrimRight(s.Capture(t, sess.ID, split), "\n "), "\n")
					return strings.TrimSpace(rows[len(rows)-1]) == "2"
				})
				if err := s.Rex.Write(context.Background(), sess.ID, split, ":pwd\r"); err != nil {
					t.Fatal(err)
				}
				waitScreen(t, s, sess.ID, split, w.dir)
			}
			o0, _ := tiledRect(t, s, sess.ID, origin)
			if r, window := tiledRect(t, s, sess.ID, split); window != sess.WindowID || r[0] <= o0[0] || r[1] != o0[1] {
				t.Errorf("the split is at %v in %s, want right of the pane at %v", r, window, o0)
			}
			assertFocus(t, s, sess.ID, split, "")
		})
	}

	// Last: the paste lands on the pane's first row, where its cursor is.
	t.Run("alt and a label pastes into the pane", func(t *testing.T) {
		focus(t, s, sess.ID, origin)
		o, got := w.labels(t, sess.ID, origin, path)
		out := act(t, s, sess.ID, o.Block, "rexkit_hints_key", map[string]string{"key": got[path]})
		if res := result(t, out); res["hints"] != true {
			t.Fatalf("⌥ %s went elsewhere: %v", got[path], res)
		}
		harness.WaitFor(t, 5*time.Second, "Hints to close", func() bool { return !hintsFloat(t, s, sess.ID) })
		harness.WaitFor(t, 5*time.Second, "the paste", func() bool {
			return strings.HasPrefix(s.Capture(t, sess.ID, origin), path)
		})
		assertFocus(t, s, sess.ID, origin, "")
		if _, err := os.Stat(w.clip); err == nil {
			if b, _ := os.ReadFile(w.clip); string(b) == path {
				t.Error("a paste went to the clipboard")
			}
		}
	})
}

// TestScreensCursor: Rex reports a pane's cursor even while its program
// hides it; rex.API.Screens leaves a hidden one out, so Hints counts from
// the last row instead of wherever a TUI left it.
func TestScreensCursor(t *testing.T) {
	s := start(t)
	pane := func(label, script string) rex.Node {
		n := rex.Command(label, "/tmp", "/bin/sh", "-c", script)
		n.Block.Options.Exit = nil
		login := false
		n.Block.Options.Login = &login
		return n
	}
	sess, blocks := s.CreateSession(t, "screens-cursor", hsplit(0.5,
		pane("hidden", `printf 'one\ntwo\n\033[?25l'; exec /bin/cat`),
		pane("shown", `printf 'one\ntwo\n'; exec /bin/cat`)))
	var screens rex.Screens
	harness.WaitFor(t, 10*time.Second, "both panes", func() bool {
		var err error
		screens, err = s.Rex.Screens(context.Background(), sess.ID, blocks[0])
		if err != nil || len(screens.Panes) != 2 {
			return false
		}
		for _, p := range screens.Panes {
			if !strings.Contains(p.Screen, "two") {
				return false
			}
		}
		return true
	})
	for _, p := range screens.Panes {
		hidden := p.BlockID == blocks[0]
		if hidden && p.Cursor != nil || !hidden && (p.Cursor == nil || *p.Cursor != (rex.Cursor{Row: 2})) {
			t.Errorf("pane %s (hidden %v): cursor %+v", p.BlockID, hidden, p.Cursor)
		}
	}
}

// TestHintsNothingToPick: over a pane with nothing to pick Hints never
// draws; it closes and says so, and a zoomed pane stays zoomed.
func TestHintsNothingToPick(t *testing.T) {
	w := newHintsWorld(t)
	s := w.s
	sess, blocks := s.CreateSession(t, "hints-empty", hsplit(0.5, w.pane("origin", "hello", "world"), cat("other", "")))
	origin := blocks[0]
	waitScreen(t, s, sess.ID, origin, "world")
	focus(t, s, sess.ID, origin)
	zoom(t, s, sess.ID, origin)
	act(t, s, sess.ID, origin, "rexkit_hints", nil)
	waitToast(t, s, sess.ID, "Nothing to pick in this pane")
	harness.WaitFor(t, 5*time.Second, "Hints to close", func() bool { return !hintsFloat(t, s, sess.ID) })
	assertFocus(t, s, sess.ID, origin, origin)
}

// TestHintsZoomedCancel: ⎋ closes Hints over a zoomed pane and leaves it
// zoomed and focused.
func TestHintsZoomedCancel(t *testing.T) {
	w := newHintsWorld(t)
	s := w.s
	sess, blocks := s.CreateSession(t, "hints-zoom", hsplit(0.5, cat("other", ""), w.pane("origin", "  "+w.sha)))
	origin := blocks[1]
	waitScreen(t, s, sess.ID, origin, w.sha)
	focus(t, s, sess.ID, origin)
	zoom(t, s, sess.ID, origin)
	o, got := w.labels(t, sess.ID, origin, w.sha)
	if got[w.sha] != "a" {
		t.Errorf("label %q, want a", got[w.sha])
	}
	s.Drive(t, sess.ID, harness.Press(t, o.Block, "ctrl+c"))
	waitNoFloats(t, s, sess.ID)
	assertFocus(t, s, sess.ID, origin, origin)
	if _, err := os.Stat(w.clip); err == nil {
		t.Errorf("a cancel copied: %s", w.clip)
	}
}

// TestHintsKey: the hints_key setting binds a global key to Hints; a key Rex
// can't read is left out and reported in the keymap's diagnostics, which
// rexkit doctor shows.
//
// A key another global key used opens Hints instead, with a warning.
func TestHintsKey(t *testing.T) {
	for _, tt := range []struct {
		key  string
		ok   bool
		warn string
	}{
		{"ctrl+shift+f", true, ""},
		{"no+such+key", false, ""},
		{"alt+a", true, "hints_key alt+a was rexkit_next_agent; it opens Hints instead"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			s := startWith(t, fmt.Sprintf("return { shell = %q, binary = %q, hints_key = %q }", "/bin/sh", rexkitBin, tt.key))
			km, err := s.Rex.Keymap(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			bound := "" // the keymap lists the binding that wins first
			for _, b := range km.Bindings {
				if b.Mode == "" && strings.Join(b.Keys, ">") == tt.key && bound == "" {
					bound = b.Action
				}
			}
			if (bound == "rexkit_hints") != tt.ok || (len(km.Diagnostics) == 0) != tt.ok {
				t.Errorf("bound to %q, diagnostics %+v", bound, km.Diagnostics)
			}
			var diag struct {
				Warnings json.RawMessage `json:"warnings"` // {} when there are none
			}
			var warnings []string
			out := s.CLI(t, "do", "rexkit_diagnostics", "--json")
			if err := json.Unmarshal([]byte(out), &diag); err != nil {
				t.Fatalf("diagnostics: %v\n%s", err, out)
			}
			_ = json.Unmarshal(diag.Warnings, &warnings)
			if tt.warn != "" && !slices.Contains(warnings, tt.warn) || tt.warn == "" && len(warnings) > 0 {
				t.Errorf("warnings %s, want %q", diag.Warnings, tt.warn)
			}
		})
	}
}
