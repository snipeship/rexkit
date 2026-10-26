package switcher

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/app/rename"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// selectBlock moves the selection to the pane row of block.
func (h *harness) selectBlock(block string) {
	h.t.Helper()
	for i, r := range h.m.listing.rows {
		if e := h.m.cat.entries[r.entry]; e.pane() && e.blockID == block {
			h.m.list.Select(i)
			h.run(h.m.selectionChanged())
			return
		}
	}
	h.t.Fatalf("no row for block %s", block)
}

func TestRename(t *testing.T) {
	t.Run("saves through the server and stays open", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		target := h.find("migrations")
		h.selectEntry("migrations")
		h.key("ctrl+e")
		if h.m.ask == nil || !strings.Contains(h.screen(), "Rename pane") {
			t.Fatalf("no rename sheet:\n%s", h.screen())
		}
		if got := h.m.ask.editor.Label(); got != "migrations" {
			t.Errorf("field = %q, want the label", got)
		}
		h.fake.ResetCalls()
		h.typeText("schema") // replaces the selected label
		h.commit()
		if h.closed != 0 || h.m.ask != nil {
			t.Fatalf("closed=%d ask=%v, want Go to open and the sheet gone", h.closed, h.m.ask)
		}
		calls := h.fake.Calls()
		if len(calls) == 0 || calls[0].Method != "session.set_block_label" || calls[0].Args[1] != target.blockID || calls[0].Args[2] != "schema" {
			t.Fatalf("calls = %+v, want set_block_label schema first", calls)
		}
		if res := h.result(); res.Action != popup.ActionStay {
			t.Errorf("⏎ answered %q, want stay", res.Action)
		}
		if h.find("schema").blockID != target.blockID {
			t.Error("the list did not refresh to the new name")
		}
		if got := h.m.toast.Current().Text; got != "Renamed" {
			t.Errorf("toast = %q", got)
		}
	})
	t.Run("an automatic label starts empty", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("nvim")
		h.key("ctrl+e")
		if got := h.m.ask.editor.Label(); got != "" {
			t.Errorf("field = %q, want empty for Rex's own label", got)
		}
	})
	t.Run("unchanged closes the sheet without a call", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("studio")
		h.key("ctrl+e")
		h.fake.ResetCalls()
		h.key("enter")
		if h.m.ask != nil || len(h.fake.Calls()) != 0 {
			t.Errorf("ask=%v calls=%v", h.m.ask, h.fake.Methods())
		}
	})
	t.Run("a refused rename keeps the sheet with the reason", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("docs")
		h.key("ctrl+e")
		h.typeText("guide")
		h.fake.Fail("session.set_window_label", fmt.Errorf("gone: %w", rex.ErrNotFound))
		h.key("enter")
		if h.m.ask == nil || h.m.ask.problem != "That tab is gone" || h.m.ask.busy {
			t.Fatalf("ask = %+v, want it open with the problem", h.m.ask)
		}
		h.typeText("s")
		if h.m.ask.problem != "" {
			t.Error("typing kept the old problem")
		}
	})
	t.Run("⎋ backs out of the sheet only", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		h.key("ctrl+e")
		h.key("esc")
		if h.m.ask != nil || h.closed != 0 || h.result().Action != popup.ActionStay {
			t.Errorf("ask=%v closed=%d", h.m.ask, h.closed)
		}
	})
	t.Run("⌃C still closes Go to", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		h.key("ctrl+e")
		h.key("ctrl+c")
		if h.closed != 1 || h.result().Action != popup.ActionClose {
			t.Errorf("closed=%d result=%q", h.closed, h.result().Action)
		}
	})
}

func TestClosePlan(t *testing.T) {
	tests := []struct {
		pick string
		kind ui.Kind
		why  string
	}{
		{"migrations", ui.KindPane, ""},
		{"nvim", ui.KindTab, "nvim"},    // rexkit › docs
		{"要約を書く", ui.KindSession, "論文"}, // an agent in 研究ノート › 論文
		{"logs", ui.KindTab, ""},
		{"web", ui.KindTab, ""},
		{"studio", ui.KindSession, ""},
	}
	for _, tt := range tests {
		h := newHarness(t, 100, 30, true)
		got, via := h.m.closePlan(h.find(tt.pick))
		why := ""
		if via != nil {
			why = via.name
		}
		if got.kind != tt.kind || why != tt.why {
			t.Errorf("closing %s closes a %v (via %q), want a %v (via %q)", tt.pick, got.kind, why, tt.kind, tt.why)
		}
	}
}

func TestClose(t *testing.T) {
	t.Run("a place elsewhere closes through the server", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		target := h.find("migrations")
		h.selectEntry("migrations")
		h.key("ctrl+x")
		if !strings.Contains(h.screen(), "Close pane migrations?") {
			t.Fatalf("no close sheet:\n%s", h.screen())
		}
		h.typeText("y") // a close has no field: typing does nothing
		h.fake.ResetCalls()
		h.commit()
		if calls := h.fake.Calls(); len(calls) == 0 || calls[0].Method != "block.close" || calls[0].Args[1] != target.blockID {
			t.Fatalf("calls = %+v", calls)
		}
		if h.closed != 0 || h.m.ask != nil || h.result().Action != popup.ActionStay {
			t.Errorf("closed=%d ask=%v", h.closed, h.m.ask)
		}
		if slices.Contains(h.rowNames(), "migrations") {
			t.Error("the closed pane is still listed")
		}
		if got := h.m.toast.Current().Text; got != "Closed the pane migrations" {
			t.Errorf("toast = %q", got)
		}
	})
	t.Run("an only pane closes its tab", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		target := h.find("docs")
		h.selectEntry("nvim")
		h.key("ctrl+x")
		if s := h.screen(); !strings.Contains(s, "Close tab docs?") || !strings.Contains(s, "nvim is its only pane, so the tab closes.") {
			t.Fatalf("sheet:\n%s", s)
		}
		h.fake.ResetCalls()
		h.key("enter")
		if calls := h.fake.Calls(); len(calls) == 0 || calls[0].Method != "session.close_window" || calls[0].Args[1] != target.windowID {
			t.Fatalf("calls = %+v", calls)
		}
	})
	t.Run("a session elsewhere", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		target := h.find("studio")
		h.selectEntry("studio")
		h.key("ctrl+x")
		if s := h.screen(); !strings.Contains(s, "2 agents are at work in it") {
			t.Errorf("no warning of studio's agents:\n%s", s)
		}
		h.fake.ResetCalls()
		h.key("enter")
		if calls := h.fake.Calls(); len(calls) == 0 || calls[0].Method != "session.destroy" || calls[0].Args[0] != target.sessionID {
			t.Fatalf("calls = %+v", calls)
		}
	})
	t.Run("a pane that already closed", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("migrations")
		h.key("ctrl+x")
		h.fake.Fail("block.close", fmt.Errorf("gone: %w", rex.ErrNotFound))
		h.key("enter")
		if h.m.ask != nil || h.m.toast.Current().Text != "The pane migrations was already closed" {
			t.Errorf("ask=%v toast=%q", h.m.ask, h.m.toast.Current().Text)
		}
	})
	t.Run("the only session is kept", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		for _, e := range h.m.cat.entries {
			if e.kind == ui.KindSession && e.sessionID != h.origin.sessionID {
				_ = h.fake.DestroySession(context.Background(), e.sessionID)
			}
		}
		h.send(inventoryMsg{inv: h.inventory()})
		h.selectEntry("rexkit")
		h.key("ctrl+x")
		if h.m.ask != nil || h.m.toast.Current().Text != "That is the only session" {
			t.Errorf("ask=%v toast=%q", h.m.ask, h.m.toast.Current().Text)
		}
	})
}

func TestCloseWhereGoToIs(t *testing.T) {
	t.Run("hands the close to Lua with the handshake", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		h.selectBlock(h.origin.blockID)
		h.key("ctrl+x")
		if !strings.Contains(h.screen(), "You are in it") {
			t.Errorf("the sheet does not say Go to is in it:\n%s", h.screen())
		}
		h.fake.ResetCalls()
		h.commit()
		if h.closed != 1 || len(h.fake.Calls()) != 0 {
			t.Fatalf("closed=%d calls=%v, want Go to closed and nothing closed from here", h.closed, h.fake.Methods())
		}
		res := h.result()
		if res.Action != popup.ActionInvoke || res.Invoke != luaClosePlace || res.SessionID != "" {
			t.Errorf("result = %+v, want an invoke with no place to go back to", res)
		}
		var args map[string]string
		if err := json.Unmarshal(res.Args, &args); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"kind": "pane", "session_id": h.origin.sessionID, "window_id": h.origin.windowID, "block_id": h.origin.blockID}
		if !maps.Equal(args, want) {
			t.Errorf("args = %v, want %v", args, want)
		}
	})
	t.Run("without the handshake closes a tab, then itself", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("spark")
		h.key("ctrl+x")
		h.fake.ResetCalls()
		h.key("enter")
		if h.closed != 1 || !slices.Equal(h.fake.Methods(), []string{"session.close_window"}) {
			t.Errorf("closed=%d calls=%v", h.closed, h.fake.Methods())
		}
	})
	t.Run("without the handshake keeps its session", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("rexkit")
		h.key("ctrl+x")
		h.fake.ResetCalls()
		h.key("enter")
		got := h.m.toast.Current()
		if h.closed != 0 || len(h.fake.Calls()) != 0 || h.m.ask == nil || len(got.Ways) == 0 || got.Ways[0].Key != "ctrl+shift+space" {
			t.Errorf("closed=%d calls=%v ask open %v toast=%+v", h.closed, h.fake.Methods(), h.m.ask != nil, got)
		}
	})
	t.Run("a failed close from here keeps Go to open", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("spark")
		h.key("ctrl+x")
		h.fake.Fail("session.close_window", fmt.Errorf("refused"))
		h.key("enter")
		if h.closed != 0 || h.quit || !strings.HasPrefix(h.m.toast.Current().Text, "Can't close it") {
			t.Errorf("closed=%d quit=%v toast=%q", h.closed, h.quit, h.m.toast.Current().Text)
		}
	})
}

func TestNewSession(t *testing.T) {
	t.Run("hands it to Lua with the handshake", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		h.key("ctrl+a")
		if !strings.Contains(h.screen(), "New session") {
			t.Fatalf("no sheet:\n%s", h.screen())
		}
		h.typeText("demo")
		h.commit()
		res := h.result()
		if h.closed != 1 || res.Action != popup.ActionInvoke || res.Invoke != luaNewSession || res.BlockID != h.origin.blockID {
			t.Fatalf("closed=%d result=%+v", h.closed, res)
		}
		var args map[string]string
		if err := json.Unmarshal(res.Args, &args); err != nil || args["label"] != "demo" {
			t.Errorf("args = %s (%v)", res.Args, err)
		}
	})
	t.Run("without the handshake offers ⌃⇧Space", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.key("ctrl+a")
		h.key("enter")
		got := h.m.toast.Current()
		if h.closed != 0 || h.m.ask == nil || len(got.Ways) == 0 || got.Ways[0].Key != "ctrl+shift+space" {
			t.Errorf("closed=%d ask open %v toast=%+v", h.closed, h.m.ask != nil, got)
		}
	})
}

func TestCloseChecksAgainOnEnter(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	nvim := h.find("nvim")
	h.selectEntry("nvim")
	h.key("ctrl+x") // nvim is docs' only pane: it asks to close the tab
	if h.m.ask.target.kind != ui.KindTab {
		t.Fatalf("target %v, want the tab", h.m.ask.target.kind)
	}
	splitDemo(h.fake, nvim.sessionID, nvim.blockID, "more") // docs gets a second pane meanwhile
	h.send(inventoryMsg{inv: h.inventory()})
	h.fake.ResetCalls()
	h.key("enter")
	if len(h.fake.Calls()) != 0 || h.m.ask == nil || h.m.ask.target.kind != ui.KindPane {
		t.Fatalf("calls=%v ask=%+v: want nothing closed and the sheet asking about the pane now", h.fake.Methods(), h.m.ask)
	}
	if got := h.m.toast.Current().Text; got != "That changed: check it again" {
		t.Errorf("toast = %q", got)
	}
	h.key("enter")
	if m := h.fake.Methods(); len(m) == 0 || m[0] != "block.close" {
		t.Errorf("calls = %v, want the pane closed on the second ⏎", m)
	}
}

func TestLateAskDone(t *testing.T) {
	t.Run("leaves a newer ask alone", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("docs")
		h.key("ctrl+e")
		newer := h.m.ask
		h.send(askDoneMsg{ask: &ask{kind: askRename}, kind: askRename, target: rename.Target{Kind: "pane"}, name: "nvim"})
		if h.m.ask != newer {
			t.Error("a backed-out rename's result closed the newer sheet")
		}
	})
	t.Run("a failed rename is reported as a rename", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.send(askDoneMsg{ask: &ask{kind: askRename}, kind: askRename, target: rename.Target{Kind: "tab"},
			name: "web", err: fmt.Errorf("gone: %w", rex.ErrNotFound)})
		if got := h.m.toast.Current().Text; got != "The tab web is gone" {
			t.Errorf("toast = %q", got)
		}
	})
}

func TestAskDropsAHeldEnter(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.m.pending = true // a ⏎ waiting for the catalog
	h.key("ctrl+x")
	if h.m.pending {
		t.Error("the held ⏎ would confirm the sheet once the catalog is live")
	}
}

func TestAskNeedsRoom(t *testing.T) {
	h := newHarness(t, 24, 12, true)
	h.key("ctrl+x")
	if h.m.ask != nil || h.m.toast.Current().Kind != ui.ToastWarn {
		t.Errorf("ask=%v toast=%+v: an ask that can't be shown must not open", h.m.ask, h.m.toast.Current())
	}
	h = newHarness(t, 100, 30, true)
	h.selectEntry("migrations")
	h.key("ctrl+x")
	h.send(tea.WindowSizeMsg{Width: 24, Height: 12})
	if h.m.ask != nil {
		t.Error("the sheet no longer shows but its ask is still open")
	}
}

func TestSheetKeepsItsHeightWhileTyping(t *testing.T) {
	h := newHarness(t, 70, 20, true)
	h.selectEntry("migrations")
	h.key("ctrl+e")
	_, before, _, _ := h.m.askPanel(h.m.layout().areas)
	h.typeText("x") // "Was migrations" appears under the field
	_, after, _, _ := h.m.askPanel(h.m.layout().areas)
	if before != after {
		t.Errorf("the sheet moved from %+v to %+v", before, after)
	}
}
