//go:build integration

// Package integration tests rexkit end to end: the real lua/ config loaded
// by an isolated Rex server, with the tools running from a rexkit freshly
// built into a temporary directory. Every test starts its own server; none
// touches the user's, and none writes the checkout's bin/rexkit, which may be
// the live install's binary.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

var (
	// root is the rexkit checkout; the loader dofiles its lua/init.lua.
	root string
	// rexkitBin is the rexkit built for this run, outside the checkout.
	rexkitBin string
	// settings keep tests independent of the machine: a plain shell, an
	// editor that is always there, and the binary built for this run.
	settings string
)

func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go env GOMOD:", err)
		return 1
	}
	root = filepath.Dir(strings.TrimSpace(string(out)))
	dir, err := os.MkdirTemp("", "rexkit-it-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	rexkitBin = filepath.Join(dir, "rexkit")
	build := exec.Command("go", "build", "-o", rexkitBin, "./cmd/rexkit")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building rexkit: %v\n%s", err, out)
		return 1
	}
	// Pay macOS's first-launch check once, not inside a test's timeout.
	_ = exec.Command(rexkitBin, "version").Run()
	settings = fmt.Sprintf("return { shell = %q, editor = { %q }, binary = %q }", "/bin/sh", "/usr/bin/vi", rexkitBin)
	return m.Run()
}

// start runs an isolated server whose config is rexkit's loader.
func start(t *testing.T) *harness.Server {
	t.Helper()
	return startWith(t, settings)
}

// startWith is start with other settings.lua source and extra environment
// for the server and its blocks.
func startWith(t *testing.T, settings string, env ...string) *harness.Server {
	t.Helper()
	return harness.Start(t, harness.Options{
		Config: fmt.Sprintf("return dofile(%q)\n", filepath.Join(root, "lua", "init.lua")),
		Files:  map[string]string{"xdg/config_home/rexkit/settings.lua": settings},
		Env:    env,
	})
}

// cat is a pane running /bin/cat in dir (home when empty).
func cat(label, dir string) rex.Node {
	n := rex.Command(label, dir, "/bin/cat")
	n.Block.Options.Exit = nil // a pane that never closes by itself
	login := false             // else /usr/bin/login is its foreground process, with no cwd to read
	n.Block.Options.Login = &login
	return n
}

func hsplit(ratio float64, before, after rex.Node) rex.Node {
	return rex.Node{Split: &rex.SplitSpec{Direction: rex.Horizontal, Ratio: ratio, Before: before, After: after}}
}

func vsplit(ratio float64, before, after rex.Node) rex.Node {
	return rex.Node{Split: &rex.SplitSpec{Direction: rex.Vertical, Ratio: ratio, Before: before, After: after}}
}

// call runs a server method and fails the test on an error.
func call(t *testing.T, s *harness.Server, method string, args, out any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Rex.Call(ctx, method, args, out); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func focus(t *testing.T, s *harness.Server, session, block string) {
	t.Helper()
	call(t, s, "session.focus_block", map[string]string{"session_id": session, "block_id": block}, nil)
}

func zoom(t *testing.T, s *harness.Server, session, block string) {
	t.Helper()
	call(t, s, "session.zoom_block", map[string]any{"session_id": session, "block_id": block, "zoom": true}, nil)
}

// act runs an action as a key in the app would and returns the outcome.
func act(t *testing.T, s *harness.Server, session, block, action string, args any) harness.Outcome {
	t.Helper()
	return s.Drive(t, session, harness.Act(t, block, action, args))[0]
}

// float is an open floating layer and its tool block.
type float struct {
	Layer rex.Layer
	Block rex.Block
}

func floats(t *testing.T, s *harness.Server, session string) []float {
	t.Helper()
	var out []float
	for _, w := range s.View(t, session).Windows {
		for _, l := range w.Layers {
			if l.Floating() && len(l.Blocks) > 0 {
				out = append(out, float{l, l.Blocks[0]})
			}
		}
	}
	return out
}

func waitNoFloats(t *testing.T, s *harness.Server, session string) {
	t.Helper()
	harness.WaitFor(t, 5*time.Second, "the float to close", func() bool { return len(floats(t, s, session)) == 0 })
}

// opened is a tool float opened through its action.
type opened struct {
	Block   string
	Layer   string
	Outcome harness.Outcome
}

// open runs a tool's action from block and waits until the tool shows want.
func open(t *testing.T, s *harness.Server, session, block, action string, args any, want string) opened {
	t.Helper()
	out := act(t, s, session, block, action, args)
	var res struct {
		BlockID string `json:"block_id"`
		LayerID string `json:"layer_id"`
	}
	if err := json.Unmarshal(out.Result, &res); err != nil || res.BlockID == "" {
		t.Fatalf("%s opened nothing: %s (%v)", action, out.Result, err)
	}
	waitScreen(t, s, session, res.BlockID, want)
	return opened{Block: res.BlockID, Layer: res.LayerID, Outcome: out}
}

// result decodes an outcome's result.
func result(t *testing.T, o harness.Outcome) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(o.Result, &m); err != nil {
		t.Fatalf("result %s: %v", o.Result, err)
	}
	return m
}

// queued returns the args of the first queued client action named name.
func queued(o harness.Outcome, name string) (map[string]any, bool) {
	for _, a := range o.ClientActions {
		if a.Action == name {
			var args map[string]any
			_ = json.Unmarshal(a.Args, &args)
			return args, true
		}
	}
	return nil, false
}

func names(o harness.Outcome) string { return strings.Join(o.Names(), " ") }

// assertFocus checks the active window's focused and zoomed blocks.
func assertFocus(t *testing.T, s *harness.Server, session, wantFocus, wantZoom string) {
	t.Helper()
	harness.WaitFor(t, 3*time.Second, "focus on "+wantFocus, func() bool {
		f, _ := s.Focus(t, session)
		return f == wantFocus
	})
	if f, z := s.Focus(t, session); f != wantFocus || z != wantZoom {
		t.Errorf("focus %s zoom %q, want focus %s zoom %q", f, z, wantFocus, wantZoom)
	}
}

// waitScreen waits until a block shows want; on a timeout it reports what
// the block showed instead.
func waitScreen(t *testing.T, s *harness.Server, session, block, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen := s.Capture(t, session, block)
		if strings.Contains(screen, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never showed %q; it shows:\n%s", block, want, screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
