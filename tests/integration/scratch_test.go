//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

func pid(t *testing.T, s *harness.Server, session, block string) int {
	t.Helper()
	var p int
	harness.WaitFor(t, 5*time.Second, "a program in "+block, func() bool {
		proc, err := s.Rex.Process(context.Background(), session, block)
		if err == nil && proc.Current() != nil {
			p = proc.Current().PID
		}
		return p != 0
	})
	return p
}

// park runs Pane mode E on block and returns the action's result.
func park(t *testing.T, s *harness.Server, session, block string) map[string]any {
	t.Helper()
	return result(t, act(t, s, session, block, "rexkit_scratch_park", nil))
}

// windowOf is the window holding block ("" when it is gone).
func windowOf(t *testing.T, s *harness.Server, session, block string) string {
	t.Helper()
	w, _, _, ok := s.View(t, session).FindBlock(block)
	if !ok {
		return ""
	}
	return w.ID
}

func newWindow(t *testing.T, s *harness.Server, session, label string, layout rex.Node) rex.Created {
	t.Helper()
	var created rex.Created
	call(t, s, "session.new_window", map[string]any{"session_id": session, "window_label": label, "focus": false, "layout": layout}, &created)
	return created
}

// TestScratchRoundTrip parks a pane that has a split sibling and brings it
// back: every pane is exactly where it was and the program never restarted.
func TestScratchRoundTrip(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "scratch", hsplit(0.4, cat("A", ""), vsplit(0.3, cat("B", ""), cat("C", ""))))
	a := blocks[0]
	rects := map[string]rex.Rect{}
	for _, b := range blocks {
		rects[b], _ = tiledRect(t, s, sess.ID, b)
	}
	before := pid(t, s, sess.ID, a)

	if res := park(t, s, sess.ID, a); res["parked"] != true {
		t.Fatalf("park: %v", res)
	}
	scratch := windowOf(t, s, sess.ID, a)
	if w, _ := s.View(t, sess.ID).Window(scratch); scratch == sess.WindowID || w.Label != "Scratch" {
		t.Fatalf("A is in %s %q, want the Scratch tab", scratch, w.Label)
	}
	if res := park(t, s, sess.ID, a); res["restored"] != true {
		t.Fatalf("restore: %v", res)
	}
	for _, b := range blocks {
		if got, _ := tiledRect(t, s, sess.ID, b); got != rects[b] {
			t.Errorf("%s at %v, want %v", b, got, rects[b])
		}
	}
	if after := pid(t, s, sess.ID, a); after != before {
		t.Errorf("A's program is %d, want the same %d", after, before)
	}
}

// TestScratchSinglePaneTab parks the only pane of a tab: a `rexkit hold`
// pane keeps the tab, swallows ⌃C, ⌃Z and ⌃\, and E on it brings the pane
// back with the same program.
func TestScratchSinglePaneTab(t *testing.T) {
	s := start(t)
	sess, _ := s.CreateSession(t, "single", cat("home", ""))
	single := newWindow(t, s, sess.ID, "Single", cat("S", ""))
	only := single.BlockIDs[0]
	before := pid(t, s, sess.ID, only)

	park(t, s, sess.ID, only)
	w, _ := s.View(t, sess.ID).Window(single.WindowID)
	var hold string
	for _, l := range w.Layers {
		for _, b := range l.Blocks {
			if b.Label == "rexkit:hold" {
				hold = b.ID
			}
		}
	}
	if hold == "" {
		t.Fatalf("no hold pane in the tab: %+v", w)
	}
	screen := waitHold(t, s, sess.ID, hold)
	if !strings.Contains(screen, "Scratch") {
		t.Errorf("hold card lacks the Scratch hint:\n%s", screen)
	}

	var keys []harness.Input
	for _, k := range []string{"ctrl+c", "ctrl+z", "ctrl+\\", "q", "enter"} {
		keys = append(keys, harness.Press(t, hold, k))
	}
	for i, o := range s.Drive(t, sess.ID, keys...) {
		if o.Handled {
			t.Errorf("key %d was taken by a binding, want it to reach the hold pane", i)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if _, _, _, ok := s.View(t, sess.ID).FindBlock(hold); !ok {
		t.Fatal("the hold pane died from a key")
	}
	if got := s.Capture(t, sess.ID, hold); got != screen {
		t.Errorf("the hold card changed after keys:\n%s\nwas:\n%s", got, screen)
	}

	if res := park(t, s, sess.ID, hold); res["restored"] != true {
		t.Fatalf("E on the hold pane: %v", res)
	}
	if got := windowOf(t, s, sess.ID, only); got != single.WindowID {
		t.Errorf("S is in %s, want back in its tab %s", got, single.WindowID)
	}
	if _, _, _, ok := s.View(t, sess.ID).FindBlock(hold); ok {
		t.Error("the hold pane is still there")
	}
	if after := pid(t, s, sess.ID, only); after != before {
		t.Errorf("S's program is %d, want the same %d", after, before)
	}
}

// waitHold waits for a hold pane's card and returns it.
func waitHold(t *testing.T, s *harness.Server, session, hold string) string {
	t.Helper()
	waitScreen(t, s, session, hold, "parked in")
	time.Sleep(300 * time.Millisecond) // its first status poll redraws the card
	return s.Capture(t, session, hold)
}

// TestScratchClosedHomeTab parks the only pane of the middle tab, closes
// that tab, and brings the pane back: the tab is recreated in its old place
// with its old name, and the program is the same.
func TestScratchClosedHomeTab(t *testing.T) {
	s := start(t)
	sess, _ := s.CreateSession(t, "closedhome", cat("first", ""))
	mid := newWindow(t, s, sess.ID, "Mid", cat("M", ""))
	newWindow(t, s, sess.ID, "Last", cat("L", ""))
	m := mid.BlockIDs[0]
	before := pid(t, s, sess.ID, m)

	park(t, s, sess.ID, m)
	call(t, s, "session.close_window", map[string]string{"session_id": sess.ID, "window_id": mid.WindowID}, nil)
	if res := park(t, s, sess.ID, m); res["restored"] != true {
		t.Fatalf("restore: %v", res)
	}
	v := s.View(t, sess.ID)
	if len(v.Windows) < 3 || v.Windows[1].ID != windowOf(t, s, sess.ID, m) || v.Windows[1].Label != "Mid" {
		var tabs []string
		for _, w := range v.Windows {
			tabs = append(tabs, w.Label)
		}
		t.Errorf("tabs %v, want M back in a second tab named Mid", tabs)
	}
	if after := pid(t, s, sess.ID, m); after != before {
		t.Errorf("M's program is %d, want the same %d", after, before)
	}
}
