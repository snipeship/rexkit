//go:build integration

package harness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
)

func TestMain(m *testing.M) { os.Exit(Run(m)) }

func TestKey(t *testing.T) {
	tests := []struct {
		spec string
		want KeyEvent
	}{
		{"enter", KeyEvent{Key: "enter", Text: "\r", UnshiftedCodepoint: 13, Action: "press"}},
		{"a", KeyEvent{Key: "key_a", Text: "a", UnshiftedCodepoint: 'a', Action: "press"}},
		{"shift+a", KeyEvent{Key: "key_a", Mods: "shift", ConsumedMods: "shift", Text: "A", UnshiftedCodepoint: 'a', Action: "press"}},
		{"ctrl+c", KeyEvent{Key: "key_c", Mods: "ctrl", Text: "\x03", UnshiftedCodepoint: 'c', Action: "press"}},
		{"ctrl+shift+space", KeyEvent{Key: "space", Mods: "shift+ctrl", UnshiftedCodepoint: 32, Action: "press"}},
		{"cmd+k", KeyEvent{Key: "key_k", Mods: "super", UnshiftedCodepoint: 'k', Action: "press"}},
		{"esc", KeyEvent{Key: "escape", Text: "\x1b", UnshiftedCodepoint: 27, Action: "press"}},
		{"7", KeyEvent{Key: "digit_7", Text: "7", UnshiftedCodepoint: '7', Action: "press"}},
		{"/", KeyEvent{Key: "slash", Text: "/", UnshiftedCodepoint: '/', Action: "press"}},
		{"f1", KeyEvent{Key: "f1", Action: "press"}},
		{"down", KeyEvent{Key: "arrow_down", Action: "press"}},
	}
	for _, tt := range tests {
		got, err := Key(tt.spec)
		if err != nil || got != tt.want {
			t.Errorf("Key(%q) = %+v, %v\nwant %+v", tt.spec, got, err, tt.want)
		}
	}
	for _, bad := range []string{"hyper+a", "nope", "ctrl+"} {
		if _, err := Key(bad); err == nil {
			t.Errorf("Key(%q) should fail", bad)
		}
	}
}

func TestIsolatedServer(t *testing.T) {
	s := Start(t, Options{Config: `rex.action{name = "echo", title = "Echo", run = function(ctx, args)
  rex.client.queue("client.mode.exit_all")
  return {block = ctx.block_id, origin = ctx.origin, n = args and args.n}
end}`})
	sess := s.NewSession(t, "harness")

	out := s.Drive(t, sess.ID, append(Type(t, sess.BlockID, "hello"), Press(t, sess.BlockID, "enter"),
		Act(t, sess.BlockID, "echo", map[string]int{"n": 2}))...)
	if out[0].Handled {
		t.Error("an unbound key must reach the block")
	}
	last := out[len(out)-1]
	var result struct {
		Block, Origin string
		N             int
	}
	if err := json.Unmarshal(last.Result, &result); err != nil || result.Block != sess.BlockID || result.Origin != "key" || result.N != 2 {
		t.Errorf("echo result = %s (%v)", last.Result, err)
	}
	if !slices.Equal(last.Names(), []string{"client.mode.exit_all"}) {
		t.Errorf("queued = %v", last.Names())
	}
	s.WaitScreen(t, sess.ID, sess.BlockID, "hello", 3*time.Second)

	inv, err := s.Rex.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := inv.Session(sess.ID)
	if !ok {
		t.Fatalf("snapshot lacks the session: %+v", inv)
	}
	_, _, b, _ := got.FindBlock(sess.BlockID)
	if b.Terminal == nil || b.Terminal.Process.Current().Name != "cat" {
		t.Errorf("terminal = %+v", b.Terminal)
	}
}

func TestWatch(t *testing.T) {
	s := Start(t, Options{})
	sess := s.NewSession(t, "watched")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := s.Rex.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-events; ev.Type != rex.EventReady {
		t.Fatalf("first event = %+v", ev)
	}
	if err := s.Rex.SetBlockLabel(ctx, sess.ID, sess.BlockID, "renamed"); err != nil {
		t.Fatal(err)
	}
	for ev := range events {
		if ev.Type == "session_view_changed" && ev.SessionID == sess.ID {
			return
		}
	}
	t.Fatal("no session_view_changed event")
}

// commitConfig is the tool handshake (lua/tools.lua) reduced to its essentials: a
// non-exclusive picker mode whose enter commits through the tool's pty.
const commitConfig = `
local pickers = {}
rex.mode("picker", {})
rex.action{name = "register", title = "register", run = function(ctx, args)
  pickers[args.block] = {dir = args.dir}
end}
rex.action{name = "commit", title = "commit", run = function(ctx)
  local p = pickers[ctx.block_id]
  if not p then
    rex.client.queue("client.key.forward")
    rex.client.queue("client.mode.exit_all")
    return {miss = true}
  end
  os.remove(p.dir .. "/result.json")
  rex.block.call("com.superlogical.terminal", "write", {data = "\r"})
  local res = rex.exec({os.getenv("REXKIT_BIN"), "await", "--state", p.dir, "--timeout", "1500ms"})
  if res.code ~= 0 then return {timeout = true, stderr = res.stderr} end
  local r = rex.json.decode(res.stdout)
  rex.client.queue("session.select", {session_id = r.session_id})
  rex.client.queue("client.mode.exit_all")
  pickers[ctx.block_id] = nil
  return r
end}
rex.bind("picker/enter", "commit")
`

func buildPopupTool(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "popuptool")
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/popuptool").CombinedOutput(); err != nil {
		t.Fatalf("build popuptool: %v\n%s", err, out)
	}
	return bin
}

func TestCommitHandshakeAndPopupClose(t *testing.T) {
	tool := buildPopupTool(t)
	s := Start(t, Options{Config: commitConfig, Env: []string{"REXKIT_BIN=" + Rexkit(t)}})
	ctx := context.Background()
	sess := s.NewSession(t, "origin")
	split, err := s.Rex.NewSplit(ctx, sess.ID, rex.NewSplitRequest{AnchorID: sess.BlockID, Direction: rex.Horizontal, Side: "after",
		Layout: rex.Command("right", "", "/bin/cat")})
	if err != nil {
		t.Fatal(err)
	}
	origin := sess.BlockID
	if err := s.Rex.ZoomBlock(ctx, sess.ID, origin, true); err != nil {
		t.Fatal(err)
	}
	dir, marker := t.TempDir(), filepath.Join(t.TempDir(), "marker")
	layer, err := s.Rex.NewLayer(ctx, sess.ID, rex.NewLayerRequest{Bounds: rex.Rect{0.1, 0.1, 0.8, 0.8}, Focus: true,
		Layout: rex.Command("tool", "", tool, "--session", sess.ID, "--origin-block", origin, "--zoomed", "--state", dir, "--marker", marker)})
	if err != nil {
		t.Fatal(err)
	}
	block := layer.BlockIDs[0]
	if focused, zoomed := s.Focus(t, sess.ID); focused != block || zoomed != "" {
		t.Fatalf("popup opened: focus %s zoom %s", focused, zoomed)
	}
	WaitFor(t, 5*time.Second, "the tool to claim its dir", func() bool {
		_, err := os.Stat(filepath.Join(dir, "owner.json"))
		return err == nil
	})

	start := time.Now()
	inputs := append([]Input{Act(t, block, "register", map[string]string{"block": block, "dir": dir})}, Type(t, block, "typed-before-enter")...)
	out := s.Drive(t, sess.ID, append(inputs, Act(t, block, "commit", nil))...)
	t.Logf("register + type + commit took %s", time.Since(start).Round(time.Millisecond))
	commit := out[len(out)-1]
	if !slices.Equal(commit.Names(), []string{"session.select", "client.mode.exit_all"}) {
		t.Fatalf("commit queued %v, result %s", commit.Names(), commit.Result)
	}
	var result struct {
		BlockID string `json:"block_id"`
	}
	if err := json.Unmarshal(commit.Result, &result); err != nil || result.BlockID != "typed-before-enter" {
		t.Errorf("the commit must see every key typed before it: %s (%v)", commit.Result, err)
	}

	WaitFor(t, 5*time.Second, "popup.Close to return", func() bool {
		data, _ := os.ReadFile(marker)
		return len(data) > 0
	})
	if data, _ := os.ReadFile(marker); string(data) != "closed err=<nil>" {
		t.Errorf("tool reported %q", data)
	}
	focused, zoomed := s.Focus(t, sess.ID)
	if focused != origin || zoomed != origin {
		t.Errorf("after close: focus %s zoom %s, want both %s", focused, zoomed, origin)
	}
	if _, l, _, ok := s.View(t, sess.ID).FindBlock(block); ok {
		t.Errorf("tool block still in layer %s", l.ID)
	}

	miss := s.Drive(t, sess.ID, Act(t, split.BlockIDs[0], "commit", nil), Press(t, split.BlockIDs[0], "enter"))
	if !slices.Equal(miss[0].Names(), []string{"client.key.forward", "client.mode.exit_all"}) {
		t.Errorf("miss queued %v", miss[0].Names())
	}
	if miss[1].Handled {
		t.Error("client.key ignores modes: a mode-bound enter must reach the block")
	}
}

// A tail capture (the goto preview) must read a pane in another session than
// the one the caller runs in: the tail script has to attach to the target's
// session. The client here gets the server only from $REX_SERVER, as every
// tool does, and runs "inside" the other session.
func TestTailCaptureAcrossSessions(t *testing.T) {
	s := Start(t, Options{})
	caller := s.NewSession(t, "caller")
	target := s.NewSession(t, "target", "/bin/sh", "-c", "seq 1 5000; echo tail-marker; exec cat")
	s.WaitScreen(t, target.ID, target.BlockID, "tail-marker", 5*time.Second)

	for _, session := range []string{caller.ID, ""} {
		env := slices.Clone(s.Env)
		if session != "" {
			env = append(env, "REX_SESSION="+session, "REX_BLOCK="+caller.BlockID)
		}
		c, err := rex.New(rex.WithBinary(s.Bin), rex.WithEnv(env))
		if err != nil {
			t.Fatal(err)
		}
		if c.Server() != s.Addr {
			t.Fatalf("client targets %q, want $REX_SERVER %q", c.Server(), s.Addr)
		}
		got, err := c.Capture(context.Background(), target.ID, target.BlockID, rex.CaptureOptions{Trim: true, TailBytes: 64})
		if err != nil {
			t.Fatalf("REX_SESSION=%q: tail of another session: %v", session, err)
		}
		if !strings.HasSuffix(strings.TrimSpace(got), "5000\ntail-marker") || len(got) > 64 || strings.Contains(got, "\n1\n") {
			t.Errorf("REX_SESSION=%q: tail = %q", session, got)
		}
	}
}
