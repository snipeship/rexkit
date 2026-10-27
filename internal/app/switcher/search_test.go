package switcher

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// searchFlag presses ⌃F the way Lua's handshake writes it (the picker mode
// binds it): with the commit marker in the state dir.
func (h *harness) searchFlag() {
	h.t.Helper()
	h.mark()
	h.key("ctrl+f")
}

// ⌃F hands the query over to Search, which opens once Go to's float is
// gone: with the handshake, in an invoke result Lua runs (no place to go
// first: Search opens over the pane Go to restores); without it, in an
// action Go to starts itself before it closes.
func TestCtrlFHandsTheQueryToSearch(t *testing.T) {
	for _, query := range []string{"panic", ""} {
		t.Run("handshake "+query, func(t *testing.T) {
			h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
			h.m.opts.self = "block:goto"
			h.typeText(query)
			h.searchFlag()
			if h.marked() {
				t.Error("the commit marker outlived its ⌃F")
			}
			res := h.result()
			if h.closed != 1 || !h.quit || res.Action != popup.ActionInvoke || res.Invoke != luaSearch {
				t.Fatalf("closed=%d quit=%v result=%+v", h.closed, h.quit, res)
			}
			if res.SessionID != "" || res.BlockID != "" {
				t.Errorf("result goes to %s %s: closing Go to restores the origin itself", res.SessionID, res.BlockID)
			}
			var args map[string]string
			if err := json.Unmarshal(res.Args, &args); err != nil || args["query"] != query || args["after"] != "block:goto" {
				t.Errorf("args = %s (%v), want query %q after block:goto", res.Args, err, query)
			}
			if len(h.started) != 0 {
				t.Errorf("started %+v: the handshake carried the query", h.started)
			}
		})
	}
	t.Run("typed without the handshake", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:app"))
		h.m.opts.self = "block:goto"
		h.typeText("panic")
		h.key("ctrl+f")
		want := map[string]string{"query": "panic", "after": "block:goto", "client": "client:app"}
		if len(h.started) != 1 || h.started[0].name != luaSearch || !reflect.DeepEqual(h.started[0].args, want) {
			t.Fatalf("started %+v, want %s with %v", h.started, luaSearch, want)
		}
		if h.started[0].closed != 0 || h.closed != 1 || !h.quit {
			t.Errorf("closed=%d (%d at the start) quit=%v: start Search, then close", h.closed, h.started[0].closed, h.quit)
		}
		if res := h.result(); res.Action == popup.ActionInvoke {
			t.Errorf("result %+v: nobody reads it", res)
		}
	})
	t.Run("a failed start stays", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
		h.startErr = &rex.Error{Op: luaSearch, Message: "rex: not found"}
		h.typeText("panic")
		h.key("ctrl+f")
		if h.closed != 0 || h.quit || h.m.busy {
			t.Errorf("closed=%d quit=%v busy=%v: Go to stays when Search cannot start", h.closed, h.quit, h.m.busy)
		}
		if h.m.toast.Current().Text == "" {
			t.Error("no toast says why")
		}
	})
	t.Run("demo", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.m.demo = true
		h.typeText("panic")
		h.key("ctrl+f")
		if got := h.m.toast.Current().Text; h.closed != 0 || !strings.Contains(got, "“panic”") {
			t.Errorf("closed=%d toast %q", h.closed, got)
		}
	})
}

// The key panel closes and ⌃F still goes; over a rename's sheet ⌃F stays a
// field's key, and the handshake is answered.
func TestCtrlFOverPanels(t *testing.T) {
	t.Run("key panel", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
		h.typeText("nav")
		h.key("?")
		h.searchFlag()
		if res := h.result(); res.Invoke != luaSearch || h.closed != 1 {
			t.Errorf("closed=%d result=%+v", h.closed, res)
		}
	})
	t.Run("rename sheet", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
		h.selectEntry("migrations")
		h.key("ctrl+e")
		h.key("home")
		h.searchFlag()
		if res := h.result(); res.Action != popup.ActionStay || h.closed != 0 || h.m.ask == nil {
			t.Fatalf("closed=%d ask %v result=%+v", h.closed, h.m.ask != nil, res)
		}
		if h.marked() {
			t.Error("the commit marker outlived its ⌃F")
		}
		if got := h.m.ask.editor.Field("").Query.Cursor; got != 1 {
			t.Errorf("cursor at %d, want ⌃F to move it one on", got)
		}
	})
}

// With a query the tray offers ⌃F after ⏎, which keeps its place in a
// narrow window; with no query it offers none. When no place matches it
// comes after ⌫, unless a toast needs the room.
func TestTrayOffersSearchText(t *testing.T) {
	for _, w := range []int{160, 70} {
		h := newHarness(t, w, 30, true)
		if l := lineWith(h, "keys"); strings.Contains(l, "ctrl f") {
			t.Errorf("%d: tray %q offers ⌃F with no query", w, l)
		}
		h.typeText("nav")
		l := lineWith(h, "keys")
		enter, search := strings.Index(l, "enter▌ go"), strings.Index(l, "ctrl f▌ search text")
		if enter < 0 || search < enter {
			t.Errorf("%d: tray %q, want ⏎ then ⌃F search text", w, l)
		}
	}
	h := newHarness(t, 62, 30, true)
	h.typeText("zqxj")
	l := lineWith(h, "keys")
	edit, search := strings.Index(l, "bksp▌ edit"), strings.Index(l, "ctrl f▌ search text")
	if edit < 0 || search < edit {
		t.Errorf("no place matches: tray %q, want ⌫ then ⌃F search text", l)
	}
	h.run(h.m.toast.Show(ui.ToastOK, "Closed the pane gamma"))
	if l := lineWith(h, "keys"); !strings.Contains(l, "Closed the pane") || strings.Contains(l, "ctrl f") {
		t.Errorf("tray %q, want the toast to keep its room", l)
	}
}

// With a query in the card of a medium window, ⌃/ preview makes way for
// the count, which ⌃F would otherwise push out; where both fit it stays.
func TestTrayKeepsTheCountWithAQuery(t *testing.T) {
	for _, w := range []int{100, 160} {
		h := newHarness(t, w, 30, true, withCard())
		h.typeText("nav")
		l := lineWith(h, "keys")
		if !strings.Contains(l, "of 26") || !strings.Contains(l, "ctrl f▌ search text") {
			t.Errorf("%d: tray %q, want the count and ⌃F", w, l)
		}
		if preview := strings.Contains(l, "ctrl /▌ preview"); preview != (w > 100) {
			t.Errorf("%d: tray %q: ⌃/ preview shown %v", w, l, preview)
		}
	}
}

// When no place matches, the page offers ⌃F first, also in the one-line
// page of a short window: "No place matches “panic” · ⌃F search the text
// of every pane". Short of room its label shortens, then ⌫'s and ⇥'s,
// before a way is left out: ⌃F and ⌫ are both there in a narrow or short
// window with a long query, and ⇥ when a kind is filtered.
func TestNoPlaceMatchesOffersSearchText(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {100, 7}} {
		h := newHarness(t, size[0], size[1], true)
		h.typeText("panic")
		l := lineWith(h, "No place matches “panic”")
		if size[1] < 30 {
			if !strings.Contains(l, "· ▐ctrl f▌ search the text of every pane") {
				t.Errorf("%v: %q, want the offer on the same line", size, l)
			}
			continue
		}
		if l == "" || !strings.Contains(h.screen(), "▐ctrl f▌ search the text of every pane   ▐bksp▌ edit the search") {
			t.Errorf("%v: no offer:\n%s", size, h.screen())
		}
	}
}

// The key panel lists ⌃F.
func TestKeyPanelListsSearchText(t *testing.T) {
	var found bool
	for _, g := range keySheet("") {
		for _, b := range g.Bindings {
			found = found || (len(b.Keys) == 1 && b.Keys[0] == "ctrl+f" && b.Desc == "search pane text")
		}
	}
	if !found {
		t.Error("the key panel does not list ⌃F")
	}
}

func TestNoPlaceMatchesShortOfRoom(t *testing.T) {
	for _, c := range []struct {
		w, h   int
		filter bool
		want   []string
	}{
		{70, 7, false, []string{"No place matches “kubernetes” · ", "ctrl f▌ search text", "bksp▌ edit"}},
		{36, 20, false, []string{"ctrl f▌ search text", "bksp▌ edit"}},
		{70, 20, true, []string{"ctrl f▌ search text", "bksp▌ edit", "tab▌ all kinds", "esc▌ close"}},
		{100, 20, true, []string{"ctrl f▌ search text", "bksp▌ edit the search", "tab▌ search all kinds"}},
	} {
		h := newHarness(t, c.w, c.h, true)
		if c.filter {
			h.key("tab")
		}
		h.typeText("kubernetes")
		s := h.screen()
		for _, want := range c.want {
			if !strings.Contains(s, want) {
				t.Errorf("%dx%d filter %v: no %q:\n%s", c.w, c.h, c.filter, want, s)
			}
		}
	}
}
