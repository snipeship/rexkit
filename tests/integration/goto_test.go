//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// TestGotoTypeThenEnter types a query and presses ⏎ at once, on one
// connection, the way a fast typist does: the commit must use the typed
// query, never a selection from before it, and the client must be told to
// show the target.
func TestGotoTypeThenEnter(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	var logs rex.Created
	call(t, s, "session.new_window", map[string]any{"session_id": work.ID, "window_label": "logs", "focus": false,
		"layout": cat("gamma", "")}, &logs)
	far, _ := s.CreateSession(t, "faraway", cat("delta", ""))
	origin := blocks[0]

	tests := []struct {
		name        string
		query       string
		wantSession string
		wantBlock   string // "" when only the session is checked
		wantWindow  string
	}{
		{"pane in another tab", "gamma", work.ID, logs.BlockIDs[0], logs.WindowID},
		{"pane beside", "beta", work.ID, blocks[1], work.WindowID},
		{"another session", "faraway", far.ID, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			focus(t, s, work.ID, origin)
			o := open(t, s, work.ID, origin, "rexkit_goto", nil, "Search sessions")
			in := append(harness.Type(t, o.Block, tt.query), harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
			outs := s.Drive(t, work.ID, in...)
			done := outs[len(outs)-1]

			res := result(t, done)
			if res["action"] != "goto" || res["session_id"] != tt.wantSession {
				t.Fatalf("result %v, want a goto to %s", res, tt.wantSession)
			}
			if tt.wantBlock != "" && res["block_id"] != tt.wantBlock {
				t.Errorf("went to %v, want %s (typed %q)", res["block_id"], tt.wantBlock, tt.query)
			}
			sel, ok := queued(done, "session.select")
			if !ok || sel["session_id"] != tt.wantSession {
				t.Errorf("queued %s with select %v, want a select of %s", names(done), sel, tt.wantSession)
			}
			if tt.wantWindow != "" && sel["window_id"] != tt.wantWindow {
				t.Errorf("select window %v, want %s", sel["window_id"], tt.wantWindow)
			}
			if _, ok := queued(done, "client.mode.exit_all"); !ok {
				t.Errorf("the picker mode stays: %s", names(done))
			}
			waitNoFloats(t, s, work.ID)
			if tt.wantBlock != "" {
				v := s.View(t, work.ID)
				w, _ := v.Window(tt.wantWindow)
				if v.ActiveWindowID != tt.wantWindow || w.FocusedBlockID != tt.wantBlock {
					t.Errorf("server focus: window %s block %s, want %s %s", v.ActiveWindowID, w.FocusedBlockID, tt.wantWindow, tt.wantBlock)
				}
			}
		})
	}
}

// TestGotoTypedBeforeItIsUp opens Go to and types the query and ⏎ on the
// same connection straight away, before the tool has drawn anything: the keys
// wait in its terminal, and the jump still goes to the typed target.
func TestGotoTypedBeforeItIsUp(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	for i := range 3 {
		focus(t, s, work.ID, blocks[i%2])
		target := blocks[(i+1)%2]
		label := map[string]string{blocks[0]: "alpha", blocks[1]: "beta"}[target]
		in := []harness.Input{harness.Act(t, blocks[i%2], "rexkit_goto", nil)}
		in = append(in, harness.Type(t, "", label)...) // "": whatever has focus, i.e. the new float
		in = append(in, harness.Act(t, "", "rexkit_commit", map[string]string{"key": "enter"}))
		outs := s.Drive(t, work.ID, in...)
		res := result(t, outs[len(outs)-1])
		switch {
		case res["action"] == popup.ActionStay:
			// A slow start: the catalog was not live popup.HandshakeGrace
			// after Lua wrote ⏎, so Go to answered "stay" and jumps by
			// itself once it is (a same-session jump needs no handshake).
			t.Logf("run %d: Go to answered stay; it jumps when its catalog is live", i)
		case res["action"] != popup.ActionGoto || res["block_id"] != target:
			t.Fatalf("run %d: result %v, want a goto to %s (%s)", i, res, label, target)
		}
		waitNoFloats(t, s, work.ID)
		assertFocus(t, s, work.ID, target, "")
	}
}

// TestGotoEnterWithoutTheHandshake writes ⏎ straight into Go to's terminal,
// as a ⏎ typed while the picker mode is not active (after a config reload)
// arrives: no rexkit_commit, so no commit marker and nobody reading the
// result. A cross-session target must not be dropped silently: Go to stays
// open and says to press ⌃⇧Space. The same ⏎ through rexkit_commit then
// jumps, and the client is told to show the other session.
func TestGotoEnterWithoutTheHandshake(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", cat("alpha", ""))
	far, _ := s.CreateSession(t, "faraway", cat("delta", ""))
	focus(t, s, work.ID, blocks[0])
	o := open(t, s, work.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	var opened struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(o.Outcome.Result, &opened); err != nil || opened.State == "" {
		t.Fatalf("no state dir in %s (%v)", o.Outcome.Result, err)
	}

	s.Drive(t, work.ID, harness.Type(t, o.Block, "faraway")...)
	waitScreen(t, s, work.ID, o.Block, "faraway")
	if err := s.Rex.Write(context.Background(), work.ID, o.Block, "\r"); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, s, work.ID, o.Block, "to jump there")
	time.Sleep(300 * time.Millisecond) // a jump would have closed the float by now
	if n := len(floats(t, s, work.ID)); n != 1 {
		t.Fatalf("%d floats: Go to closed on a ⏎ nobody could act on", n)
	}
	var res popup.Result
	data, err := os.ReadFile(filepath.Join(opened.State, popup.ResultFile))
	if err == nil {
		_ = json.Unmarshal(data, &res)
	}
	if res.Action == popup.ActionGoto {
		t.Fatalf("Go to wrote a jump nobody reads: %s", data)
	}

	done := s.Drive(t, work.ID, harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))[0]
	got := result(t, done)
	if got["action"] != popup.ActionGoto || got["session_id"] != far.ID {
		t.Fatalf("result %v, want a goto to %s", got, far.ID)
	}
	if sel, ok := queued(done, "session.select"); !ok || sel["session_id"] != far.ID {
		t.Errorf("queued %s with select %v, want a select of %s", names(done), sel, far.ID)
	}
	waitNoFloats(t, s, work.ID)
	if _, err := os.Stat(filepath.Join(opened.State, popup.CommitFile)); !os.IsNotExist(err) {
		t.Errorf("the commit marker outlived the handshake: %v", err)
	}
}
