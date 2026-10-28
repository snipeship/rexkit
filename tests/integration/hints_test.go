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
	return newHintsWorldWith(t, settings)
}

// newHintsWorldWith is newHintsWorld with other settings.lua source.
func newHintsWorldWith(t *testing.T, settings string) *hintsWorld {
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

// TestHints labels a pane, then picks each way: a label copies, ⇥ then a
// label pastes into the pane, ⇧ and a label opens a URL, a file at its line
// in the editor setting, a commit in git show, a container's logs and a pod
// in kubectl describe (with the namespace its listing was for), each in a
// split in the pane's folder. Labels go nearest the pane's cursor first
// (Rex's cursor, through rex.API.Screens), and after each pick the float
// closes and focus is back on the pane. Hints enters no mode, so the keys
// it reads (⇥, ⏎, ⎋, letters) reach it as keys; that no binding takes them
// is lua/tests/check.lua's.
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
		if _, ok := queued(o.Outcome, "client.mode.enter"); ok {
			t.Errorf("Hints entered a mode (queued %s)", names(o.Outcome))
		}
		// ⏎ picks nothing and Hints stays; ⎋ closes it.
		s.Drive(t, sess.ID, harness.Press(t, o.Block, "enter"))
		time.Sleep(300 * time.Millisecond)
		if !hintsFloat(t, s, sess.ID) {
			t.Fatal("⏎ closed Hints")
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
	t.Run("tab then a label pastes into the pane", func(t *testing.T) {
		focus(t, s, sess.ID, origin)
		o, got := w.labels(t, sess.ID, origin, path)
		if err := os.Remove(w.clip); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		s.Drive(t, sess.ID, harness.Press(t, o.Block, "tab"))
		harness.WaitFor(t, 5*time.Second, "the key bar to say a paste · tab copy", func() bool {
			bar := strings.NewReplacer("▐", "", "▌", "").Replace(s.Capture(t, sess.ID, o.Block))
			return strings.Contains(bar, "a paste") && strings.Contains(bar, "tab copy")
		})
		s.Drive(t, sess.ID, harness.Type(t, o.Block, got[path])...)
		waitToast(t, s, sess.ID, "Pasted "+path)
		harness.WaitFor(t, 5*time.Second, "Hints to close", func() bool { return !hintsFloat(t, s, sess.ID) })
		harness.WaitFor(t, 5*time.Second, "the paste", func() bool {
			return strings.HasPrefix(s.Capture(t, sess.ID, origin), path)
		})
		assertFocus(t, s, sess.ID, origin, "")
		if _, err := os.Stat(w.clip); err == nil {
			t.Error("a paste went to the clipboard")
		}
		// It lands over the first row; a ⏎ would make cat print it again
		// over the second.
		assertNoEnter(t, s, sess.ID, origin, "  M  "+path)
	})
}

// TestHintsCommitViaGlobal: with commit_via = "global" ⏎ is a global key;
// in Hints it goes through the handshake, which Hints answers "stay", so it
// stays open, and ⇥ and a label still paste, without a ⏎. ⎋, ⌃C, ⇥ and ⌫
// stay keys no global binding takes.
func TestHintsCommitViaGlobal(t *testing.T) {
	w := newHintsWorldWith(t, fmt.Sprintf("return { shell = %q, editor = { %q }, binary = %q, commit_via = %q }",
		"/bin/sh", "/usr/bin/vi", rexkitBin, "global"))
	s := w.s
	sess, blocks := s.CreateSession(t, "hints-global", hsplit(0.5, w.pane("origin", "  "+w.sha), cat("other", "")))
	origin := blocks[0]
	waitScreen(t, s, sess.ID, origin, w.sha)
	focus(t, s, sess.ID, origin)
	o, got := w.labels(t, sess.ID, origin, w.sha)
	if _, ok := queued(o.Outcome, "client.mode.enter"); ok {
		t.Errorf("Hints entered a mode (queued %s)", names(o.Outcome))
	}
	// The app runs the global ⏎'s binding in the tool, as here.
	km, err := s.Rex.Keymap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var enter *rex.Binding
	for i, b := range km.Bindings {
		if b.Mode == "" && strings.Join(b.Keys, ">") == "enter" {
			enter = &km.Bindings[i]
			break
		}
	}
	if enter == nil || enter.Action != "rexkit_commit" {
		t.Fatalf("the global ⏎ is %+v, want rexkit_commit", enter)
	}
	// Hints reads these itself, with no mode on: no global binding may take
	// them (Press can't tell, as client.key runs no bindings).
	for _, b := range km.Bindings {
		if k := strings.Join(b.Keys, ">"); b.Mode == "" && slices.Contains([]string{"tab", "shift+tab", "escape", "esc", "ctrl+c", "backspace"}, k) {
			t.Errorf("the global %s is %s", k, b.Action)
		}
	}
	if res := result(t, act(t, s, sess.ID, o.Block, enter.Action, enter.Args)); res["action"] != "stay" {
		t.Errorf("⏎ in Hints answered %v, want stay", res)
	}
	time.Sleep(300 * time.Millisecond)
	if !hintsFloat(t, s, sess.ID) {
		t.Fatal("⏎ closed Hints")
	}
	s.Drive(t, sess.ID, append([]harness.Input{harness.Press(t, o.Block, "tab")}, harness.Type(t, o.Block, got[w.sha])...)...)
	waitToast(t, s, sess.ID, "Pasted "+w.sha)
	harness.WaitFor(t, 5*time.Second, "the paste", func() bool {
		return strings.HasPrefix(s.Capture(t, sess.ID, origin), w.sha)
	})
	assertFocus(t, s, sess.ID, origin, "")
	assertNoEnter(t, s, sess.ID, origin, "")
}

// TestHintsBracketedPaste: a pane whose program turned on bracketed paste
// (as Neovim, zsh and agents do) gets the paste between the paste marks,
// so it reads the text as text and not as keys. The pane's terminal echoes
// what arrives, the marks' ⎋ as ^[.
func TestHintsBracketedPaste(t *testing.T) {
	w := newHintsWorld(t)
	s := w.s
	script := `printf '  %s\n' "$1"; printf '\033[?2004h\033[H'; exec /bin/cat`
	origin := rex.Command("origin", w.dir, "/bin/sh", "-c", script, "sh", w.sha)
	origin.Block.Options.Exit = nil
	login := false
	origin.Block.Options.Login = &login
	sess, blocks := s.CreateSession(t, "hints-bracketed", hsplit(0.5, origin, cat("other", "")))
	pane := blocks[0]
	waitScreen(t, s, sess.ID, pane, w.sha)
	focus(t, s, sess.ID, pane)
	o, got := w.labels(t, sess.ID, pane, w.sha)
	s.Drive(t, sess.ID, append([]harness.Input{harness.Press(t, o.Block, "tab")}, harness.Type(t, o.Block, got[w.sha])...)...)
	waitToast(t, s, sess.ID, "Pasted "+w.sha)
	harness.WaitFor(t, 5*time.Second, "the paste between the marks", func() bool {
		return strings.HasPrefix(s.Capture(t, sess.ID, pane), "^[[200~"+w.sha+"^[[201~")
	})
	assertNoEnter(t, s, sess.ID, pane, "")
}

// assertNoEnter checks that a paste over a pane's first row came without a
// ⏎: its second row is still second, a while after the paste showed.
func assertNoEnter(t *testing.T, s *harness.Server, session, pane, second string) {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	if rows := strings.Split(s.Capture(t, session, pane), "\n"); len(rows) < 2 || strings.TrimRight(rows[1], " ") != second {
		t.Errorf("the paste was typed with a ⏎: the second row is %q, want %q", rows[min(1, len(rows)-1)], second)
	}
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

// TestHintsZoomedReadsTheZoomedScreen: Hints over a zoomed pane reads the
// pane before the zoom ends. Focusing a float ends the zoom, and the app then
// gives the pane its tiled size at once, which rewraps its screen. Here a
// client that owns the pane plays the app (a terminal takes another client's
// size only once that client owns it): it halves the pane's columns as soon
// as the zoom ends. Hints draws the pane's long line as the zoomed pane
// showed it, unwrapped.
func TestHintsZoomedReadsTheZoomedScreen(t *testing.T) {
	w := newHintsWorld(t)
	s := w.s
	long := strings.Repeat("x", 60) + "  " + w.sha
	ids := filepath.Join(t.TempDir(), "ids.json")
	args, err := json.Marshal(map[string]any{"out": ids, "layout": hsplit(0.5, cat("other", ""), w.pane("origin", long))})
	if err != nil {
		t.Fatal(err)
	}
	app := exec.Command(s.Bin, "--autostart=false", "-S", s.Addr, "do", "-", "--json", "--args", string(args))
	app.Env, app.Stdin = s.Env, strings.NewReader(halveOnUnzoom)
	var played strings.Builder
	app.Stdout, app.Stderr = &played, &played
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Process.Kill(); _ = app.Wait() })
	var created struct {
		SessionID string `json:"session_id"`
		Windows   []struct {
			BlockIDs rex.List[string] `json:"block_ids"`
		} `json:"initial_windows"`
	}
	harness.WaitFor(t, 10*time.Second, "the app's session", func() bool {
		b, err := os.ReadFile(ids)
		return err == nil && json.Unmarshal(b, &created) == nil && len(created.Windows) > 0
	})
	sess, origin := created.SessionID, created.Windows[0].BlockIDs[1]
	waitScreen(t, s, sess, origin, w.sha)
	focus(t, s, sess, origin)
	zoom(t, s, sess, origin)
	o, _ := w.labels(t, sess, origin, w.sha)
	for _, line := range strings.Split(w.s.Capture(t, sess, o.Block), "\n") {
		if i := strings.Index(line, w.sha); i >= 0 && i < 60 {
			t.Errorf("Hints drew the pane rewrapped at its tiled size:\n%s", line)
		}
	}
	s.Drive(t, sess, harness.Press(t, o.Block, "ctrl+c"))
	waitNoFloats(t, s, sess)
	assertFocus(t, s, sess, origin, origin)
	if err := app.Wait(); err != nil || !strings.Contains(played.String(), `"resized": true`) {
		t.Errorf("the zoom never ended while Hints was open: %v\n%s", err, played.String())
	}
}

// halveOnUnzoom creates a session with rex.args.layout, writes what
// session.create returned to rex.args.out, and once the session's second
// block has been zoomed and is no longer, halves its columns, as the app
// does when it lays the pane out in its tile again.
const halveOnUnzoom = `
local a = rex.args
local created = assert(rex.call("session.create", {label = "hints-zoom-read",
  initial_windows = {{window_label = "home", layout = a.layout}}}))
rex.call("session.attach", {session_id = created.session_id})
local file = assert(io.open(a.out, "w"))
file:write(rex.json.encode(created))
file:close()
local sid, block = created.session_id, created.initial_windows[1].block_ids[2]
local zoomed = false
for _ = 1, 1000 do
  local v = rex.call("session.view", {session_id = sid}) or {}
  local now
  for _, w in ipairs(v.windows or {}) do
    if w.window_id == v.active_window_id then now = w.zoomed_block_id end
  end
  if now == block then
    zoomed = true
  elseif zoomed then
    local size = rex.call("com.superlogical.terminal.size", {session_id = sid, block_id = block, args = {}})
    rex.call("com.superlogical.terminal.resize", {session_id = sid, block_id = block,
      args = {columns = math.floor(size.columns / 2), rows = size.rows}})
    return {resized = true}
  end
  rex.sleep(0.005)
end
return {resized = false}
`

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
		// ⇥ is a key of Hints itself: bound globally, it would never reach it
		{"tab", true, "hints_key tab is a key you type; it opens Hints instead"},
		// so is ⎋, which closes Hints: no picker mode takes it from the binding
		{"esc", true, "hints_key esc is a key you type; it opens Hints instead"},
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
