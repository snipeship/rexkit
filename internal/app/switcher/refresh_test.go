package switcher

import (
	"path/filepath"
	"testing"
	"time"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// event delivers one change event from a watcher that then goes quiet.
func (h *harness) event(typ string) {
	h.t.Helper()
	ch := make(chan rex.Event)
	close(ch) // the next read reports the watcher gone, so nothing blocks
	h.send(watchMsg{ev: rex.Event{Type: typ, BlockID: "block:other"}, events: ch, ok: true})
}

// lastDelay is the duration of the newest timer for a message like msg.
func (h *harness) lastDelay(match func(any) bool) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.delays) - 1; i >= 0; i-- {
		if match(h.delays[i].msg) {
			return h.delays[i].d, true
		}
	}
	return 0, false
}

func isRefreshDue(msg any) bool { _, ok := msg.(refreshDueMsg); return ok }

// A pane that animates its title (or an agent streaming status) sends events
// many times a second; each snapshot is a burst of RPCs on the user's server.
func TestEventsRefreshAtMostOncePerGap(t *testing.T) {
	h := newHarness(t, 100, 30, true) // Init's snapshot just arrived
	h.event("terminal.program_status_changed")
	if d, ok := h.lastDelay(isRefreshDue); !ok || d != minRefreshGap {
		t.Errorf("an event right after a snapshot refreshes in %v (set %v), want %v", d, ok, minRefreshGap)
	}
	h.event("terminal.program_status_changed")
	h.mu.Lock()
	n := 0
	for _, d := range h.delays {
		if isRefreshDue(d.msg) {
			n++
		}
	}
	h.mu.Unlock()
	if n != 1 {
		t.Errorf("a second event while one refresh is due set %d refresh timers, want 1", n)
	}
	h.fire() // the trailing refresh still runs
	if h.m.refresh.due || h.m.refresh.running {
		t.Fatalf("due=%v running=%v after the trailing refresh", h.m.refresh.due, h.m.refresh.running)
	}
	h.m.refresh.lastDone = testNow.Add(-time.Minute)
	h.event("session.window_added")
	if d, _ := h.lastDelay(isRefreshDue); d != refreshDebounce {
		t.Errorf("an event long after the last snapshot refreshes in %v, want the %v debounce", d, refreshDebounce)
	}
}

func TestUnchangedSnapshotIsNotCachedAgain(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	saves := 0
	h.m.save = func(rex.Inventory) { saves++ }
	h.key("ctrl+r")
	h.key("ctrl+r")
	if saves != 1 {
		t.Errorf("two identical snapshots wrote the cache %d times, want once", saves)
	}
	h.fake.AddWindow(h.find("studio").sessionID, "docs2", "less")
	h.key("ctrl+r")
	if saves != 2 {
		t.Errorf("a changed snapshot was not cached (%d writes)", saves)
	}
}

func TestCacheOutdated(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	inv := h.inventory()
	same := inv
	same.CollectedAt = inv.CollectedAt.Add(time.Second)
	aged := inv
	aged.CollectedAt = inv.CollectedAt.Add(cacheMaxAge)
	if cacheOutdated(same, &inv) {
		t.Error("an identical snapshot a second later counts as changed")
	}
	if !cacheOutdated(aged, &inv) || !cacheOutdated(inv, nil) {
		t.Error("an old or unknown cache is kept")
	}
	h.fake.AddWindow(h.find("studio").sessionID, "docs2", "less")
	if changed := h.inventory(); !cacheOutdated(changed, &inv) {
		t.Error("a new tab does not count as a change")
	}
}

// Only the first load and ⌃R say "Updating"; event-driven refreshes update
// rows in place without waking the spinner clock.
func TestBackgroundRefreshIsQuiet(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	cmd := h.m.collect()
	if h.m.loading.Active() {
		t.Error("a background refresh started the loading spinner")
	}
	h.run(cmd)
	cmd = h.m.reload()
	if !h.m.loading.Active() {
		t.Error("⌃R does not show that it is working")
	}
	h.run(cmd)
	if h.m.loading.Active() {
		t.Error("the spinner kept going after the snapshot arrived")
	}
}

// The snapshot cache paints the first frame; a tab opened since is not in
// it. A query typed and committed before the live snapshot arrives must go
// to the best match of the live data, not to the cached frame's top row.
func TestHeldEnterOnCachedFrameRanksFreshData(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	h.m.live = false // as if the catalog were the cached first frame
	_, blocks := h.fake.AddWindow(h.find("studio").sessionID, "doc", "doc")
	h.typeText("doc")
	cached := h.selected()
	h.commit()
	if h.closed != 0 || !h.m.pending {
		t.Fatalf("closed=%d pending=%v: ⏎ must wait for the live catalog", h.closed, h.m.pending)
	}
	h.send(inventoryMsg{inv: h.inventory()})
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil || res.BlockID != blocks[0] {
		t.Errorf("result = %+v (%v), want the new doc pane %s, not the cached top row %q", res, err, blocks[0], cached)
	}
}

func TestHeldEnterKeepsAPickedRow(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	h.m.live = false
	h.fake.AddWindow(h.find("studio").sessionID, "doc", "doc")
	h.typeText("doc")
	h.key("down") // the user picked a row: the fresh ranking must not move it
	picked := h.m.selectedKey()
	h.commit()
	h.send(inventoryMsg{inv: h.inventory()})
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil {
		t.Fatal(err)
	}
	want, ok := h.m.cat.find(picked)
	if !ok || res.BlockID != want.blockID {
		t.Errorf("result %+v; want the picked row %s (%s)", res, describe(want), want.blockID)
	}
}
