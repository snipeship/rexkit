//go:build integration

package integration

import (
	"context"
	"fmt"
	"slices"
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

func newWindow(t *testing.T, s *harness.Server, session, label string, layout rex.Node) rex.Created {
	t.Helper()
	var created rex.Created
	call(t, s, "session.new_window", map[string]any{"session_id": session, "window_label": label, "focus": false, "layout": layout}, &created)
	return created
}

// newFloat opens a floating layer the way Rex's own Float Pane leaves one:
// a user's floating pane, not one of rexkit's.
func newFloat(t *testing.T, s *harness.Server, session, window string, bounds rex.Rect, layout rex.Node) []string {
	t.Helper()
	var created rex.Created
	call(t, s, "session.new_layer", map[string]any{"session_id": session, "window_id": window, "bounds": bounds,
		"focus": false, "layout": layout}, &created)
	return created.BlockIDs
}

// userFloats lists a window's floating layers that are not rexkit's own
// (tools, toasts), back to front.
func userFloats(t *testing.T, s *harness.Server, session, window string) []rex.Layer {
	t.Helper()
	w, ok := s.View(t, session).Window(window)
	if !ok {
		t.Fatalf("no window %s", window)
	}
	var out []rex.Layer
	for _, l := range w.Layers {
		mine := l.Floating() && len(l.Blocks) > 0
		for _, b := range l.Blocks {
			if strings.HasPrefix(b.Label, "rexkit:") {
				mine = false
			}
		}
		if mine {
			out = append(out, l)
		}
	}
	return out
}

// placement is where session.list_blocks says block is ("placed",
// "detached"), or "" once it is closed.
func placement(t *testing.T, s *harness.Server, session, block string) string {
	t.Helper()
	var res struct {
		Blocks []struct {
			BlockID   string `json:"block_id"`
			Placement string `json:"placement"`
		} `json:"blocks"`
	}
	call(t, s, "session.list_blocks", map[string]any{"session_id": session}, &res)
	for _, b := range res.Blocks {
		if b.BlockID == block {
			return b.Placement
		}
	}
	return ""
}

// floatRect is a block's rect in the window, wherever its layer is.
func floatRect(t *testing.T, s *harness.Server, session, block string) rex.Rect {
	t.Helper()
	_, l, _, ok := s.View(t, session).FindBlock(block)
	if !ok {
		t.Fatalf("block %s is not placed", block)
	}
	r, _ := l.Rect(block)
	return r
}

// TestFloatsHideAndShow: ⌃P w hides the tab's floating panes, their
// programs running on, and shows them again with the same bounds, layout,
// stacking order and focus.
func TestFloatsHideAndShow(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "floats", hsplit(0.5, cat("A", ""), cat("B", "")))
	back := newFloat(t, s, sess.ID, sess.WindowID, rex.Rect{0.1, 0.1, 0.4, 0.3}, cat("F1", ""))[0]
	front := newFloat(t, s, sess.ID, sess.WindowID, rex.Rect{0.3, 0.4, 0.6, 0.5}, vsplit(0.3, cat("F2", ""), cat("F3", "")))
	panes := []string{back, front[0], front[1]}
	rects, pids := map[string]rex.Rect{}, map[string]int{}
	for _, b := range panes {
		rects[b], pids[b] = floatRect(t, s, sess.ID, b), pid(t, s, sess.ID, b)
	}
	focus(t, s, sess.ID, front[1])

	if res := result(t, act(t, s, sess.ID, front[1], "rexkit_floats", nil)); res["hidden"] != float64(3) {
		t.Fatalf("hide: %v", res)
	}
	if got := userFloats(t, s, sess.ID, sess.WindowID); len(got) != 0 {
		t.Fatalf("%d floating panes after hiding, want none", len(got))
	}
	for _, b := range panes {
		if p := placement(t, s, sess.ID, b); p != "detached" {
			t.Errorf("%s is %q, want detached", b, p)
		}
	}
	f, _ := s.Focus(t, sess.ID)
	if f != blocks[0] && f != blocks[1] {
		t.Errorf("focus on %s, want a pane of the layout", f)
	}

	if res := result(t, act(t, s, sess.ID, f, "rexkit_floats", nil)); res["shown"] == nil {
		t.Fatalf("show: %v", res)
	}
	layers := userFloats(t, s, sess.ID, sess.WindowID)
	if len(layers) != 2 || layers[0].Blocks[0].ID != back || layers[0].Bounds != (rex.Rect{0.1, 0.1, 0.4, 0.3}) ||
		layers[1].Bounds != (rex.Rect{0.3, 0.4, 0.6, 0.5}) {
		t.Fatalf("layers %+v, want F1 behind F2|F3, where they were", layers)
	}
	for _, b := range panes {
		if got := floatRect(t, s, sess.ID, b); got != rects[b] {
			t.Errorf("%s at %v, want %v", b, got, rects[b])
		}
		if got := pid(t, s, sess.ID, b); got != pids[b] {
			t.Errorf("%s runs %d, want the same %d", b, got, pids[b])
		}
	}
	assertFocus(t, s, sess.ID, front[1], "")
}

// TestFloatsOpen: ⌃P w with no floating pane opens a shell, half the
// window, centred; ⌃P n in it opens the next one a step down, and ⌃P n in a
// pane of the layout splits it.
func TestFloatsOpen(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "open", cat("A", ""))
	res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_floats", nil))
	first, _ := res["opened"].(string)
	layers := userFloats(t, s, sess.ID, sess.WindowID)
	if first == "" || len(layers) != 1 || layers[0].Bounds != (rex.Rect{0.25, 0.25, 0.5, 0.5}) {
		t.Fatalf("opened %v, layers %+v: want one floating pane, centred", res, layers)
	}
	assertFocus(t, s, sess.ID, first, "")

	res = result(t, act(t, s, sess.ID, first, "rexkit_new_pane", nil))
	next, _ := res["opened"].(string)
	layers = userFloats(t, s, sess.ID, sess.WindowID)
	if next == "" || len(layers) != 2 || layers[1].Blocks[0].ID != next || layers[1].Bounds != (rex.Rect{0.27, 0.29, 0.5, 0.5}) {
		t.Fatalf("n in a floating pane: %v, layers %+v", res, layers)
	}
	assertFocus(t, s, sess.ID, next, "")

	split := act(t, s, sess.ID, blocks[0], "rexkit_new_pane", nil)
	if args, ok := queued(split, "pane.split"); !ok || args["direction"] != "auto" {
		t.Errorf("n in the layout queued %s, want pane.split auto", names(split))
	}
}

// TestFloatEmbedRoundTrip: ⌃P e floats a pane and puts it back exactly
// where it was, its program running on and focus staying with it.
func TestFloatEmbedRoundTrip(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "embed", hsplit(0.4, cat("A", ""), vsplit(0.3, cat("B", ""), cat("C", ""))))
	rects := map[string]rex.Rect{}
	for _, b := range blocks {
		rects[b], _ = tiledRect(t, s, sess.ID, b)
	}
	for _, b := range blocks[:2] { // A's sibling is a split, B's a pane
		before := pid(t, s, sess.ID, b)
		focus(t, s, sess.ID, b)
		if res := result(t, act(t, s, sess.ID, b, "rexkit_float", nil)); res["floated"] != b {
			t.Fatalf("float %s: %v", b, res)
		}
		if layers := userFloats(t, s, sess.ID, sess.WindowID); len(layers) != 1 || layers[0].Blocks[0].ID != b {
			t.Fatalf("layers %+v, want %s floating", layers, b)
		}
		assertFocus(t, s, sess.ID, b, "")
		if res := result(t, act(t, s, sess.ID, b, "rexkit_float", nil)); res["embedded"] != b {
			t.Fatalf("embed %s: %v", b, res)
		}
		for _, other := range blocks {
			if got, _ := tiledRect(t, s, sess.ID, other); got != rects[other] {
				t.Errorf("after %s's round trip %s is at %v, want %v", b, other, got, rects[other])
			}
		}
		assertFocus(t, s, sess.ID, b, "")
		if after := pid(t, s, sess.ID, b); after != before {
			t.Errorf("%s runs %d, want the same %d", b, after, before)
		}
	}
}

// TestToolFloatsAreNotFloatingPanes: rexkit's tools and toasts float, but
// ⌃P w, e and n leave them alone.
func TestToolFloatsAreNotFloatingPanes(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "tools", hsplit(0.5, cat("A", ""), cat("B", "")))
	focus(t, s, sess.ID, blocks[0])
	tool := open(t, s, sess.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	assertFocus(t, s, sess.ID, tool.Block, "")
	err := s.Rex.Action(context.Background(), sess.ID, "rexkit_float", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "close the open tool first") {
		t.Errorf("e in a tool: %v, want close the open tool first", err)
	}
	if split := act(t, s, sess.ID, tool.Block, "rexkit_new_pane", nil); !hasQueued(split, "pane.split") {
		t.Errorf("n in a tool queued %s, want a split (a tool is no floating pane)", names(split))
	}
	act(t, s, sess.ID, tool.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
	waitNoFloats(t, s, sess.ID)

	act(t, s, sess.ID, blocks[0], "rexkit_notify", map[string]string{"message": "Copied", "kind": "ok"})
	var own string
	for _, f := range floats(t, s, sess.ID) {
		if f.Block.Label == "rexkit:toast" {
			own = f.Block.ID
		}
	}
	if own == "" {
		t.Fatal("no toast")
	}
	if res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)); res["opened"] == nil {
		t.Fatalf("w with only a toast floating: %v, want a new floating pane", res)
	}
	if res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)); res["hidden"] != float64(1) {
		t.Fatalf("w: %v, want the one floating pane hidden", res)
	}
	if p := placement(t, s, sess.ID, own); p == "detached" { // or gone: a toast does not last
		t.Error("the toast was hidden, want it left alone")
	}
}

func hasQueued(o harness.Outcome, name string) bool { _, ok := queued(o, name); return ok }

// TestHiddenFloatsInGoTo: Go to lists a hidden floating pane under its tab,
// and going there shows the tab's floating panes and focuses it.
func TestHiddenFloatsInGoTo(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "goto", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	notes := newFloat(t, s, sess.ID, sess.WindowID, rex.Rect{0.2, 0.2, 0.5, 0.4}, cat("notes", ""))[0]
	act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)
	if p := placement(t, s, sess.ID, notes); p != "detached" {
		t.Fatalf("notes is %q, want hidden", p)
	}
	focus(t, s, sess.ID, blocks[0])
	o := open(t, s, sess.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	s.WaitScreen(t, sess.ID, o.Block, "hidden", 10*time.Second)
	in := append(harness.Type(t, o.Block, "notes"), harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
	outs := s.Drive(t, sess.ID, in...)
	if res := result(t, outs[len(outs)-1]); res["block_id"] != notes {
		t.Fatalf("result %v, want a goto to notes", res)
	}
	waitNoFloatsBut(t, s, sess.ID, notes)
	layers := userFloats(t, s, sess.ID, sess.WindowID)
	if len(layers) != 1 || layers[0].Blocks[0].ID != notes || layers[0].Bounds != (rex.Rect{0.2, 0.2, 0.5, 0.4}) {
		t.Fatalf("layers %+v, want notes shown where it was", layers)
	}
	assertFocus(t, s, sess.ID, notes, "")
}

// waitNoFloatsBut waits until the only floating layer, toasts aside,
// holds block.
func waitNoFloatsBut(t *testing.T, s *harness.Server, session, block string) {
	t.Helper()
	harness.WaitFor(t, 5*time.Second, "the tool to close", func() bool {
		f := slices.DeleteFunc(floats(t, s, session), func(f float) bool { return f.Block.Label == "rexkit:toast" })
		return len(f) == 1 && f[0].Block.ID == block
	})
}

// TestHiddenFloatsInGoToWithoutHandshake: with commit_via = "global" Go to
// shows the hidden pane's tab itself, then goes there.
func TestHiddenFloatsInGoToWithoutHandshake(t *testing.T) {
	s := startWith(t, fmt.Sprintf("return { shell = %q, editor = { %q }, binary = %q, commit_via = %q }",
		"/bin/sh", "/usr/bin/vi", rexkitBin, "global"))
	sess, blocks := s.CreateSession(t, "global", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	notes := newFloat(t, s, sess.ID, sess.WindowID, rex.Rect{0.2, 0.2, 0.5, 0.4}, cat("notes", ""))[0]
	act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)
	focus(t, s, sess.ID, blocks[0])
	o := open(t, s, sess.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	s.WaitScreen(t, sess.ID, o.Block, "hidden", 10*time.Second)
	s.Drive(t, sess.ID, append(harness.Type(t, o.Block, "notes"), harness.Press(t, o.Block, "enter"))...)
	waitNoFloatsBut(t, s, sess.ID, notes)
	layers := userFloats(t, s, sess.ID, sess.WindowID)
	if len(layers) != 1 || layers[0].Blocks[0].ID != notes || layers[0].Bounds != (rex.Rect{0.2, 0.2, 0.5, 0.4}) {
		t.Fatalf("layers %+v, want notes shown where it was", layers)
	}
	assertFocus(t, s, sess.ID, notes, "")
}

// TestHiddenFloatsInGoToAcrossSessions: Go to shows a hidden pane of
// another session and goes there.
func TestHiddenFloatsInGoToAcrossSessions(t *testing.T) {
	s := start(t)
	other, oblocks := s.CreateSession(t, "other", hsplit(0.5, cat("alpha", ""), cat("beta", "")))
	notes := newFloat(t, s, other.ID, other.WindowID, rex.Rect{0.2, 0.2, 0.5, 0.4}, cat("notes", ""))[0]
	act(t, s, other.ID, oblocks[0], "rexkit_floats", nil)
	here, blocks := s.CreateSession(t, "here", cat("home", ""))
	focus(t, s, here.ID, blocks[0])
	o := open(t, s, here.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	s.WaitScreen(t, here.ID, o.Block, "hidden", 10*time.Second)
	in := append(harness.Type(t, o.Block, "notes"), harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
	outs := s.Drive(t, here.ID, in...)
	if res := result(t, outs[len(outs)-1]); res["block_id"] != notes || res["session_id"] != other.ID {
		t.Fatalf("result %v, want a goto to notes in the other session", res)
	}
	waitNoFloats(t, s, here.ID)
	layers := userFloats(t, s, other.ID, other.WindowID)
	if len(layers) != 1 || layers[0].Blocks[0].ID != notes || layers[0].Bounds != (rex.Rect{0.2, 0.2, 0.5, 0.4}) {
		t.Fatalf("layers %+v, want notes shown where it was", layers)
	}
	assertFocus(t, s, other.ID, notes, "")
}

// TestHiddenFloatsOfAClosedTab: Rex keeps a hidden pane running when its
// tab closes; the agent watcher's sweep closes it, as nothing could show it
// again. (The Lua checks, which run with no watcher, pin Rex keeping it.)
func TestHiddenFloatsOfAClosedTab(t *testing.T) {
	s := start(t)
	sess, _ := s.CreateSession(t, "closed", cat("home", ""))
	firstLook(t, s, watcher(t, s))
	two := newWindow(t, s, sess.ID, "two", cat("T2", ""))
	hidden := newFloat(t, s, sess.ID, two.WindowID, rex.Rect{0.1, 0.1, 0.4, 0.4}, cat("F1", ""))[0]
	act(t, s, sess.ID, two.BlockIDs[0], "rexkit_floats", nil)
	if p := placement(t, s, sess.ID, hidden); p != "detached" {
		t.Fatalf("F1 is %q, want it hidden", p)
	}
	call(t, s, "session.close_window", map[string]any{"session_id": sess.ID, "window_id": two.WindowID}, nil)
	harness.WaitFor(t, 10*time.Second, "the watcher to close F1", func() bool { return placement(t, s, sess.ID, hidden) == "" })
}

// TestHiddenFloatsOutliveAReload: a config reload with panes hidden says
// nothing (the config cannot sweep while it loads), and ⌃P w shows them
// again after it.
func TestHiddenFloatsOutliveAReload(t *testing.T) {
	s := harness.Start(t, harness.Options{
		Config: fmt.Sprintf("return dofile(%q)\n", root+"/lua/init.lua"),
		Files:  map[string]string{"xdg/config_home/rexkit/settings.lua": settings},
		Unix:   true,
	})
	sess, blocks := s.CreateSession(t, "reload", hsplit(0.5, cat("A", ""), cat("B", "")))
	f1 := newFloat(t, s, sess.ID, sess.WindowID, rex.Rect{0.1, 0.1, 0.4, 0.4}, cat("F1", ""))[0]
	act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)
	if out := s.CLI(t, "config", "reload"); strings.Contains(out, "warning") {
		t.Errorf("reload says:\n%s", out)
	}
	focus(t, s, sess.ID, blocks[0])
	if res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_floats", nil)); res["shown"] == nil {
		t.Fatalf("w after a reload: %v, want F1 shown", res)
	}
	layers := userFloats(t, s, sess.ID, sess.WindowID)
	if len(layers) != 1 || layers[0].Blocks[0].ID != f1 || layers[0].Bounds != (rex.Rect{0.1, 0.1, 0.4, 0.4}) {
		t.Fatalf("layers %+v, want F1 where it was", layers)
	}
}

// TestFloatsHandFocusBack: hiding a floating pane gives the focus back to
// the pane of the layout it was opened from.
func TestFloatsHandFocusBack(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "back", hsplit(0.3, cat("A", ""), hsplit(0.5, cat("B", ""), cat("C", ""))))
	for _, b := range blocks {
		focus(t, s, sess.ID, b)
		res := result(t, act(t, s, sess.ID, b, "rexkit_floats", nil))
		f, _ := res["opened"].(string)
		if f == "" {
			f, _ = s.Focus(t, sess.ID) // the one hidden before, shown again
		}
		assertFocus(t, s, sess.ID, f, "")
		if res := result(t, act(t, s, sess.ID, f, "rexkit_floats", nil)); res["hidden"] != float64(1) {
			t.Fatalf("hide from %s: %v", b, res)
		}
		assertFocus(t, s, sess.ID, b, "")
	}
}

// TestFloatsKeepAZoom: floating panes show over a zoomed pane and leave it
// zoomed when they hide, as over a fullscreen pane in Zellij.
func TestFloatsKeepAZoom(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "zoom", hsplit(0.5, cat("A", ""), cat("B", "")))
	a := blocks[0]
	zoom(t, s, sess.ID, a)
	assertFocus(t, s, sess.ID, a, a)
	g, _ := result(t, act(t, s, sess.ID, a, "rexkit_floats", nil))["opened"].(string)
	if g == "" {
		t.Fatal("w opened nothing")
	}
	act(t, s, sess.ID, g, "rexkit_floats", nil) // hide
	assertFocus(t, s, sess.ID, a, a)
	act(t, s, sess.ID, a, "rexkit_floats", nil) // show
	assertFocus(t, s, sess.ID, g, "")
	act(t, s, sess.ID, g, "rexkit_floats", nil) // hide
	assertFocus(t, s, sess.ID, a, a)
}
