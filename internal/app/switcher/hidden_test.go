package switcher

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// hide takes the pane named name out of its tab the way ⌃P w hides a
// floating pane, tells the switcher (through env.hidden) and refreshes.
func (h *harness) hide(tab, program string) entry {
	h.t.Helper()
	e := h.pane(tab, program)
	h.fake.Detach(e.blockID)
	h.mu.Lock()
	h.hidden = append(h.hidden, state.HiddenFloat{SessionID: e.sessionID, WindowID: e.windowID, BlockID: e.blockID})
	h.mu.Unlock()
	h.send(inventoryMsg{inv: h.inventory()})
	return e
}

// pane returns the pane running program in the tab named tab.
func (h *harness) pane(tab, program string) entry {
	h.t.Helper()
	for _, e := range h.m.cat.entries {
		if e.pane() && e.tab == tab && e.program == program {
			return e
		}
	}
	h.t.Fatalf("no %s pane in %s", program, tab)
	return entry{}
}

func TestHiddenFloatsAreListedUnderTheirTab(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	hidden := h.hide("api", "psql")
	e := h.pane("api", "psql")
	if !e.hidden || e.windowID != hidden.windowID {
		t.Fatalf("entry = %+v, want migrations hidden in its tab", e)
	}
	tab, _ := h.m.cat.find(hidden.windowID)
	if tab.blockID == hidden.blockID {
		t.Error("the tab jumps to its hidden pane, want a pane it shows")
	}
	if got := tab.counts(); got != "1 pane · 1 hidden" {
		t.Errorf("tab counts = %q", got)
	}
	names := h.rowNames()
	if i, j := slices.Index(names, "zsh"), slices.Index(names, "migrations"); i < 0 || j < 0 {
		t.Fatalf("rows = %v, want the api tab's panes", names)
	}
	h.selectEntry("migrations")
	if !strings.Contains(h.screen(), "hidden · ~/code/studio/api") {
		t.Errorf("no hidden marker on the row:\n%s", h.screen())
	}
}

func TestHiddenPanesOfOtherProgramsAreNotListed(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	e := h.find("migrations")
	h.fake.Detach(e.blockID) // detached by something else: rexkit did not hide it
	h.send(inventoryMsg{inv: h.inventory()})
	if slices.Contains(h.rowNames(), "migrations") {
		t.Errorf("rows = %v, want only the panes ⌃P w hid", h.rowNames())
	}
}

func TestEnterOnAHiddenFloatShowsItsTabFirst(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	hidden := h.hide("spark", "claude") // rexkit › spark, the origin's tab
	// what lua/floats.lua does: put it back in its tab
	h.act = func(string, any) error {
		return h.fake.MoveBlock(context.Background(), hidden.sessionID, rex.MoveBlockRequest{
			BlockID: hidden.blockID, AnchorID: h.origin.blockID, Direction: rex.Horizontal, Side: "after"})
	}
	h.selectBlock(hidden.blockID)
	h.fake.ResetCalls()
	h.key("enter")
	want := []started{{luaShowFloats, map[string]string{"session_id": hidden.sessionID, "window_id": hidden.windowID}, 0}}
	if !reflect.DeepEqual(h.actions, want) {
		t.Fatalf("actions = %+v, want %+v", h.actions, want)
	}
	if m := h.fake.Methods(); len(m) == 0 || m[len(m)-1] != "session.focus_block" || h.closed != 1 {
		t.Errorf("calls = %v closed=%d, want the pane focused and the float closed", m, h.closed)
	}
}

func TestHiddenFloatThatCannotBeShownKeepsGoToOpen(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	hidden := h.hide("spark", "claude")
	h.act = func(string, any) error { return errors.New("this tab is gone") }
	h.selectBlock(hidden.blockID)
	h.fake.ResetCalls()
	h.key("enter")
	if h.closed != 0 || slices.Contains(h.fake.Methods(), "session.focus_block") {
		t.Errorf("closed=%d calls=%v, want Go to open and nothing focused", h.closed, h.fake.Methods())
	}
}

func TestHiddenFloatsDoNotKeepATabOpen(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.hide("api", "psql")
	got, via := h.m.closePlan(h.pane("api", "zsh"))
	if got.kind != ui.KindTab || via == nil {
		t.Errorf("closing api's last shown pane closes %+v, want the tab", got)
	}
	hidden := h.pane("api", "psql")
	if got, via := h.m.closePlan(hidden); got.blockID != hidden.blockID || via != nil {
		t.Errorf("closing a hidden pane closes %+v, want only it", got)
	}
}

func TestHandshakeLeavesAHiddenFloatToLua(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	hidden := h.hide("api", "psql")
	h.selectBlock(hidden.blockID)
	h.fake.ResetCalls()
	h.commit()
	if len(h.actions) != 0 || len(h.fake.Calls()) != 0 {
		t.Errorf("actions %+v, calls %v: Lua awaits the result, so it shows the pane", h.actions, h.fake.Methods())
	}
	if res := h.result(); res.BlockID != hidden.blockID || res.WindowID != hidden.windowID || h.closed != 1 {
		t.Errorf("result %+v closed=%d, want a goto to the hidden pane", res, h.closed)
	}
}

func TestCloseSheetNamesHiddenFloats(t *testing.T) {
	h := newHarness(t, 120, 30, true)
	h.hide("api", "psql")
	h.selectBlock(h.pane("api", "zsh").blockID)
	h.key("ctrl+x")
	s := h.screen()
	for _, want := range []string{"zsh is its only pane in the layout, so the tab closes.",
		"Ends what runs in its 2 panes, 1 of them hidden."} {
		if !strings.Contains(s, want) {
			t.Errorf("the sheet does not say %q:\n%s", want, s)
		}
	}
}
