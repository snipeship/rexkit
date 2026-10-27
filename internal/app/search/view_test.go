package search

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/ui"
)

// unreadableKubectl fails the capture of the kubectl pane (another session).
func unreadableKubectl(f *rexfake.Fake) rex.API {
	inv, _ := f.Snapshot(context.Background())
	for _, p := range panesOf(inv, origin{}, "") {
		if p.name == "kubectl" {
			return failing{f, p.block}
		}
	}
	panic("no kubectl pane")
}

// The tray's note on a pane that could not be read, and a toast, keep
// their place at any width: the hints give way to them.
func TestTrayKeepsWarningsAndToasts(t *testing.T) {
	for _, w := range []int{80, 100, 120} {
		h := newHarness(t, w, 30, true, withAPI(unreadableKubectl))
		h.typeText("panic")
		if s := h.screen(); !strings.Contains(s, "1 pane could not be read") {
			t.Errorf("%d columns: no warning in the tray:\n%s", w, s)
		}
		h.selectHit("zsh", 1233)
		h.key("ctrl+y")
		if s := h.screen(); !strings.Contains(s, "Copied line 1233 of zsh") {
			t.Errorf("%d columns: no copied toast:\n%s", w, s)
		}
	}
}

// The page with no matches counts only the panes it could read.
func TestNoMatchesCountsPanesRead(t *testing.T) {
	h := newHarness(t, 160, 40, true, withAPI(unreadableKubectl))
	h.typeText("segfault")
	panes, _ := h.m.searchedPanes()
	if panes != len(h.m.panes)-1 {
		t.Errorf("searched %d panes of %d, one unreadable", panes, len(h.m.panes))
	}
	if s := h.screen(); !strings.Contains(s, "in "+plural(len(h.m.panes)-1, "pane")+" across") {
		t.Errorf("page:\n%s", s)
	}
}

// ⌥C is offered only when it would ignore case: match case on and no
// capital typed.
func TestIgnoreCaseHint(t *testing.T) {
	h := newHarness(t, 120, 30, true)
	h.typeText("PANICKED") // a capital matches case: ⌥C cannot change that
	if s := h.screen(); !strings.Contains(s, "No matches for") || strings.Contains(s, "ignore case") {
		t.Errorf("smart case:\n%s", s)
	}
	h.key("alt+c")
	if s := h.screen(); strings.Contains(s, "ignore case") {
		t.Errorf("match case and a capital:\n%s", s)
	}
	for range len("PANICKED") {
		h.key("backspace")
	}
	h.typeText("fix panic in refresh") // only "Fix panic in refresh" is there
	if s := h.screen(); !strings.Contains(s, "ignore case") {
		t.Errorf("match case on a lower-case query offers no way out:\n%s", s)
	}
	h.key("alt+c")
	if h.m.total == 0 {
		t.Error("⌥C did not ignore case")
	}
}

func TestEmptyPageOffersTheOtherSyntax(t *testing.T) {
	h := newHarness(t, 120, 30, true)
	if s := h.screen(); !strings.Contains(s, "alt r▌ regex") {
		t.Errorf("plain:\n%s", s)
	}
	h.key("alt+r")
	if s := h.screen(); !strings.Contains(s, "alt r▌ plain text") {
		t.Errorf("regex on:\n%s", s)
	}
}

// A regex that needs the line's indentation still highlights its match in
// the list, which drops the indentation.
func TestHitRowHighlightsIndentedMatches(t *testing.T) {
	f := rexfake.New()
	sid, w, a := f.AddSession("s")
	f.SetCapture(a, "Error: boom\n    at handler (server.js:12)\nok\n")
	h := newHarness(t, 160, 40, true, withWorld(f, origin{session: sid, window: w, block: a}))
	h.key("alt+r")
	h.typeText(`^\s+at `)
	r, ok := h.m.selected()
	if !ok || r.kind != rowHit {
		t.Fatalf("selected %q", h.selectedRow())
	}
	var lit []string
	for _, sp := range h.m.hitRow(r, 1, 100) {
		if reflect.DeepEqual(sp.Style, h.m.theme.Match) {
			lit = append(lit, sp.Text)
		}
	}
	if strings.Join(lit, "") != "at " {
		t.Errorf("highlighted %q, want the match without its indentation", lit)
	}
}

// While panes are read, the tray counts them and the matches so far; then
// it counts the matches.
func TestTrayStreamsTheReading(t *testing.T) {
	h, c, msgs := holdReads(t, context.Background())
	now := testNow
	h.m.cfg.now = func() time.Time { return now }
	h.typeText("panic")
	for range 2 {
		c.release <- struct{}{}
		_, next := h.m.Update(captures(t, msgs, 1)[0])
		startAll(next, msgs)
	}
	now = now.Add(ui.SpinnerDelay)
	want := "Reading 2 of " + plural(len(h.m.panes), "pane") + " · " + plural(h.m.total, "match") + " so far"
	if s := h.screen(); !strings.Contains(s, want) {
		t.Errorf("no %q in the tray:\n%s", want, s)
	}
	close(c.release)
	for deadline := time.After(5 * time.Second); h.m.scanning(); {
		select {
		case msg := <-msgs:
			if cm, ok := msg.(captureMsg); ok {
				_, next := h.m.Update(cm)
				startAll(next, msgs)
			}
		case <-deadline:
			t.Fatal("the reads did not end")
		}
	}
	if s := h.screen(); strings.Contains(s, "Reading") || !strings.Contains(s, "92 matches in 6 panes") {
		t.Errorf("after reading:\n%s", s)
	}
}
