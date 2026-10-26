package rex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// collectOutput is shaped like the collector's real output: Lua re-encodes
// every empty array as {}.
const collectOutput = `{
  "errors": {},
  "sessions": [
    {"session_id": "session:1", "label": "work", "revision": 9, "order": 0, "active_window_id": "window:1",
     "windows": [{"window_id": "window:1", "label": "api", "focused_block_id": "block:1",
       "layers": [{"layer_id": "layer:1", "kind": "tiled", "bounds": [0,0,1,1],
         "layout": {"rect": [0,0,1,1], "block_id": "block:1"},
         "blocks": [{"block_id": "block:1", "label": "shell", "creator_name": "com.superlogical.terminal",
           "flavor": "com.superlogical.terminal.shell",
           "terminal": {
             "process": {"foreground": {"pid": 7, "name": "claude", "invoked_path": "/opt/bin/claude", "cwd": "/src/api"},
                         "child": {"pid": 6, "name": "zsh"}, "home": "/Users/me"},
             "status": [{"app": "claude", "state": "blocked", "kind": "question", "msg": "hello",
                         "updated_at": "2026-10-25T23:52:56.761356-03:00"}],
             "title": "claude task"}}]}]}]},
    {"session_id": "session:2", "label": "empty", "revision": 1, "order": 1, "windows": {}}
  ]
}`

func TestSnapshotDecodesCollectorOutput(t *testing.T) {
	c, rec := newTestClient(t, stdout(collectOutput))
	inv, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !containsRun(rec.argv[0], []string{"do", "-", "--json"}) || !strings.Contains(string(rec.stdin[0]), "session.attach") {
		t.Fatalf("Snapshot should run the embedded collector: %q", rec.argv[0])
	}
	if len(inv.Sessions) != 2 || len(inv.Errors) != 0 || inv.CollectedAt.IsZero() {
		t.Fatalf("inventory = %+v", inv)
	}
	work, ok := inv.Session("session:1")
	if !ok || work.Order != 0 || work.Label != "work" {
		t.Fatalf("session:1 = %+v %v", work, ok)
	}
	_, _, b, _ := work.FindBlock("block:1")
	term := b.Terminal
	if term == nil || term.Title != "claude task" || term.Process.Current().Name != "claude" {
		t.Fatalf("terminal = %+v", term)
	}
	if s := term.Status[0]; s.State != StateBlocked || s.Kind != "question" || s.UpdatedAt.IsZero() {
		t.Errorf("status = %+v", s)
	}
	if empty, _ := inv.Session("session:2"); empty.Windows != nil {
		t.Errorf("empty session windows = %v", empty.Windows)
	}
}

func TestSnapshotCleansErrors(t *testing.T) {
	c, _ := newTestClient(t, stdout(`{"sessions":{},"errors":[{"session_id":"session:9","error":"stream response \"control-4\" failed: stream is not attached (status=409 code=conflict)"}]}`))
	inv, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Errors) != 1 || inv.Errors[0] != "session:9: stream is not attached" {
		t.Errorf("errors = %q", inv.Errors)
	}
}

// A session.list that fails must not read as "nothing is running": the
// collector raises, rex do exits 1, and Snapshot returns the error.
func TestSnapshotFailsWhenSessionsCannotBeListed(t *testing.T) {
	c, _ := newTestClient(t, func([]string, []byte) Output {
		return Output{ExitCode: 1, Stderr: []byte("Error: stream response \"control-1\" failed: refused (status=403 code=refused)\n")}
	})
	inv, err := c.Snapshot(context.Background())
	if err == nil || !errors.Is(err, ErrRefused) || len(inv.Sessions) != 0 {
		t.Fatalf("inv = %+v, err = %v", inv, err)
	}
	if !strings.Contains(string(collectScript), `if not list then error(`) {
		t.Error("collect.lua must raise when session.list fails")
	}
}
