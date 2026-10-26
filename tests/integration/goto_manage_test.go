//go:build integration

package integration

import (
	"encoding/json"
	"testing"
	"time"

	"rexkit/internal/popup"
	"rexkit/tests/integration/harness"
)

// lastQueued returns the args of the last queued client action named name.
func lastQueued(o harness.Outcome, name string) (map[string]any, bool) {
	var args map[string]any
	found := false
	for _, a := range o.ClientActions {
		if a.Action == name {
			args, found = nil, true
			_ = json.Unmarshal(a.Args, &args)
		}
	}
	return args, found
}

type listed struct {
	Sessions []struct {
		SessionID string `json:"session_id"`
		Label     string `json:"label"`
	} `json:"sessions"`
}

func sessionsOf(t *testing.T, s *harness.Server) listed {
	t.Helper()
	var l listed
	call(t, s, "session.list", map[string]any{}, &l)
	return l
}

func hasBlock(t *testing.T, s *harness.Server, session, block string) bool {
	t.Helper()
	_, _, _, ok := s.View(t, session).FindBlock(block)
	return ok
}

// ask opens Go to from origin, selects what query finds, presses key and
// waits for the sheet to show want.
func ask(t *testing.T, s *harness.Server, session, origin, query, key, want string) opened {
	t.Helper()
	focus(t, s, session, origin)
	o := open(t, s, session, origin, "rexkit_goto", nil, "Search sessions")
	s.Drive(t, session, harness.Type(t, o.Block, query)...)
	waitScreen(t, s, session, o.Block, query)
	s.Drive(t, session, harness.Press(t, o.Block, key))
	waitScreen(t, s, session, o.Block, want)
	return o
}

func commitIn(t *testing.T, s *harness.Server, session string, o opened) harness.Outcome {
	t.Helper()
	return s.Drive(t, session, harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))[0]
}

// TestGotoRenamesAndClosesElsewhere renames and closes a pane Go to is not
// in: both go through the server and Go to stays open on the new list.
func TestGotoRenamesAndClosesElsewhere(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", hsplit(0.5, cat("alpha", ""), cat("beta", "")))

	o := ask(t, s, work.ID, blocks[0], "beta", "ctrl+e", "Rename pane")
	s.Drive(t, work.ID, harness.Type(t, o.Block, "gamma")...)
	if res := result(t, commitIn(t, s, work.ID, o)); res["action"] != popup.ActionStay {
		t.Fatalf("result %v, want stay", res)
	}
	harness.WaitFor(t, 5*time.Second, "the new label", func() bool {
		_, _, b, _ := s.View(t, work.ID).FindBlock(blocks[1])
		return b.Label == "gamma"
	})
	waitScreen(t, s, work.ID, o.Block, "Renamed")

	// the query no longer matches: search for the new name
	in := []harness.Input{}
	for range len("beta") {
		in = append(in, harness.Press(t, o.Block, "backspace"))
	}
	in = append(in, harness.Type(t, o.Block, "gamma")...)
	s.Drive(t, work.ID, append(in, harness.Press(t, o.Block, "ctrl+x"))...)
	waitScreen(t, s, work.ID, o.Block, "Close pane gamma?")
	if res := result(t, commitIn(t, s, work.ID, o)); res["action"] != popup.ActionStay {
		t.Fatalf("result %v, want stay", res)
	}
	harness.WaitFor(t, 5*time.Second, "beta to close", func() bool { return !hasBlock(t, s, work.ID, blocks[1]) })
	waitScreen(t, s, work.ID, o.Block, "Closed the pane") // cut short in a small float
	if n := len(floats(t, s, work.ID)); n != 1 {
		t.Fatalf("%d floats, want Go to still open", n)
	}
	act(t, s, work.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
	waitNoFloats(t, s, work.ID)
}

// TestGotoClosesWhereItIs closes the pane Go to was opened from: Go to
// closes and Lua closes the pane after it.
func TestGotoClosesWhereItIs(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	o := ask(t, s, work.ID, blocks[0], "alpha", "ctrl+x", "You are in it")
	done := commitIn(t, s, work.ID, o)
	if res := result(t, done); res["action"] != popup.ActionInvoke || res["invoke"] != "rexkit_close_place" {
		t.Fatalf("result %v, want the close handed to Lua", res)
	}
	waitNoFloats(t, s, work.ID)
	if hasBlock(t, s, work.ID, blocks[0]) || !hasBlock(t, s, work.ID, blocks[1]) {
		t.Fatalf("alpha there %v, beta there %v: want alpha closed and beta kept",
			hasBlock(t, s, work.ID, blocks[0]), hasBlock(t, s, work.ID, blocks[1]))
	}
	if f, _ := s.Focus(t, work.ID); f != blocks[1] {
		t.Errorf("focus %s, want beta", f)
	}
}

// TestGotoClosesItsSession closes the session Go to is in: the client is
// sent to another session first.
func TestGotoClosesItsSession(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", cat("alpha", ""))
	other, _ := s.CreateSession(t, "other", cat("delta", ""))
	o := ask(t, s, work.ID, blocks[0], "work", "ctrl+x", "Close session work?")
	done := commitIn(t, s, work.ID, o)
	if sel, ok := lastQueued(done, "session.select"); !ok || sel["session_id"] != other.ID {
		t.Errorf("queued %s with select %v, want a select of %s", names(done), sel, other.ID)
	}
	for _, l := range sessionsOf(t, s).Sessions {
		if l.SessionID == work.ID {
			t.Fatalf("session work is still there")
		}
	}
}

// TestGotoNewSession makes a session through Go to and switches to it.
func TestGotoNewSession(t *testing.T) {
	s := start(t)
	work, blocks := s.CreateSession(t, "work", cat("alpha", ""))
	focus(t, s, work.ID, blocks[0])
	o := open(t, s, work.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	s.Drive(t, work.ID, harness.Press(t, o.Block, "ctrl+a"))
	waitScreen(t, s, work.ID, o.Block, "New session")
	s.Drive(t, work.ID, harness.Type(t, o.Block, "fresh")...)
	waitScreen(t, s, work.ID, o.Block, "fresh")
	done := commitIn(t, s, work.ID, o)
	if res := result(t, done); res["action"] != popup.ActionInvoke || res["invoke"] != "rexkit_new_session" {
		t.Fatalf("result %v", res)
	}
	var fresh string
	for _, l := range sessionsOf(t, s).Sessions {
		if l.Label == "fresh" {
			fresh = l.SessionID
		}
	}
	if fresh == "" {
		t.Fatalf("no session named fresh in %+v", sessionsOf(t, s))
	}
	sel, ok := lastQueued(done, "session.select")
	if !ok || sel["session_id"] != fresh {
		t.Errorf("queued %s with select %v, want a select of %s", names(done), sel, fresh)
	}
	if v := s.View(t, fresh); len(v.Windows) != 1 || len(v.Windows[0].Layers) == 0 || len(v.Windows[0].Layers[0].Blocks) != 1 {
		t.Errorf("the new session is %+v, want one window with a shell", v.Windows)
	}
	waitNoFloats(t, s, work.ID)
}
