package switcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui/uitest"
)

func TestNavigation(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	start := h.m.list.Cursor()
	h.key("down")
	if h.m.list.Cursor() != start+1 {
		t.Fatalf("↓ moved from %d to %d", start, h.m.list.Cursor())
	}
	h.key("up")
	h.key("up")
	if h.m.list.Cursor() != start-1 {
		t.Fatalf("↑↑ moved to %d, want %d", h.m.list.Cursor(), start-1)
	}
	h.key("pgdown")
	h.key("pgdown")
	h.key("pgdown")
	h.key("pgdown")
	if last := h.m.list.Len() - 1; h.m.list.Cursor() != last {
		t.Errorf("paging down stops at the last row: at %d, want %d", h.m.list.Cursor(), last)
	}
	h.key("down")
	if h.m.list.Cursor() != 0 {
		t.Errorf("↓ on the last row wraps to the first, got %d", h.m.list.Cursor())
	}
}

func TestDrillAndBackRestoreQueryAndSelection(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.typeText("stu")
	h.selectEntry("studio")
	h.key("right")
	if h.m.filter.scope != h.find("studio").key() || h.m.input.Value() != "" {
		t.Fatalf("→ on studio: scope %q query %q", h.m.filter.scope, h.m.input.Value())
	}
	if got, want := h.rowNames(), []string{"web", "Navigation polish", "Fix navbar overflow on small screens", "api", "zsh", "migrations"}; !slices.Equal(got, want) {
		t.Fatalf("studio lists %q, want %q", got, want)
	}
	if !strings.Contains(h.screen(), "in studio ×") {
		t.Errorf("no breadcrumb for the scope:\n%s", h.screen())
	}
	h.selectEntry("api")
	h.key("right")
	if got := h.rowNames(); !slices.Equal(got, []string{"zsh", "migrations"}) {
		t.Fatalf("studio › api lists %q", got)
	}
	if !strings.Contains(h.screen(), "in studio › api ×") {
		t.Errorf("breadcrumb does not show the tab:\n%s", h.screen())
	}
	h.typeText("mig")
	h.key("backspace") // edits the query: it is not empty
	if h.m.input.Value() != "mi" || h.m.filter.scope == "" {
		t.Fatalf("⌫ with a query edited it: %q, scope %q", h.m.input.Value(), h.m.filter.scope)
	}
	h.key("backspace")
	h.key("backspace") // empties the query
	h.key("backspace") // at once: the same ⌫ still held, swallowed
	if h.m.filter.scope != h.find("api").key() || h.m.input.Value() != "" {
		t.Fatalf("a held ⌫ that emptied the query backed out: scope %q query %q", h.m.filter.scope, h.m.input.Value())
	}
	h.m.emptiedAt = time.Time{} // let go (the test clock does not move)
	h.key("backspace")          // a fresh ⌫ on the empty query: back to studio
	if h.m.filter.scope != h.find("studio").key() || h.selected() != "api" {
		t.Fatalf("⌫ on an empty query: scope %q selected %q, want studio with api selected", h.m.filter.scope, h.selected())
	}
	h.key("left")
	if h.m.filter.scope != "" || h.m.input.Value() != "stu" || h.selected() != "studio" {
		t.Fatalf("← restored scope %q query %q selected %q, want the stu search with studio selected",
			h.m.filter.scope, h.m.input.Value(), h.selected())
	}
}

func TestScopeClosesWhileDrilledIn(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.selectEntry("infrastructure-and-deployment-pipelines")
	h.key("right")
	inv := h.inventory()
	inv.Sessions = slices.DeleteFunc(inv.Sessions, func(s rex.SessionView) bool {
		return s.Label == "infrastructure-and-deployment-pipelines"
	})
	h.send(inventoryMsg{inv: inv})
	if h.m.filter.scope != "" || !h.m.switcherView() {
		t.Errorf("the scope's session closed but the scope %q stayed", h.m.filter.scope)
	}
}

func TestEnterSameSessionFocusesWritesResultAndCloses(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	target := h.find("nvim")
	h.selectEntry("nvim")
	h.fake.ResetCalls()
	h.commit()
	if !h.quit || h.closed != 1 {
		t.Fatalf("quit=%v closed=%d, want the float closed once", h.quit, h.closed)
	}
	if got := h.fake.Calls(); len(got) != 1 || got[0].Method != "session.focus_block" || got[0].Args[1] != target.blockID {
		t.Errorf("calls = %+v, want one focus_block of nvim", got)
	}
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil {
		t.Fatal(err)
	}
	want := popup.Result{Seq: res.Seq, Action: popup.ActionGoto, SessionID: target.sessionID, WindowID: target.windowID, BlockID: target.blockID}
	if res.Seq == 0 || !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if v := h.recorded.Visits; len(v) < 2 || v[0].BlockID != target.blockID || v[1].BlockID != h.origin.blockID {
		t.Errorf("visits = %+v, want nvim then the origin (so the next open bounces back)", v)
	}
}

func TestEnterOnOriginOnlyCloses(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.selectEntry("spark")
	h.fake.ResetCalls()
	h.key("enter")
	if h.closed != 1 || len(h.fake.Calls()) != 0 {
		t.Errorf("closed=%d calls=%v: going where you are should just close the float", h.closed, h.fake.Methods())
	}
}

func TestEnterAcrossSessions(t *testing.T) {
	t.Run("without the handshake explains ⌃⇧Space", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		h.selectEntry("migrations")
		h.key("enter")
		if h.closed != 0 || h.quit {
			t.Fatal("closed without a way to switch sessions")
		}
		if got := h.m.toast.Current(); !strings.Contains(got.Text, "studio") || len(got.Ways) == 0 || got.Ways[0].Key != "ctrl+shift+space" {
			t.Errorf("toast = %+v, want studio and the ctrl shift space way", got)
		}
	})
	t.Run("with the handshake focuses there and hands over", func(t *testing.T) {
		dir := t.TempDir()
		h := newHarness(t, 100, 30, true, withStateDir(dir))
		target := h.find("migrations")
		h.selectEntry("migrations")
		h.fake.ResetCalls()
		h.commit()
		if h.closed != 1 || !slices.Equal(h.fake.Methods(), []string{"session.focus_block"}) {
			t.Fatalf("closed=%d calls=%v", h.closed, h.fake.Methods())
		}
		if c := h.fake.Calls()[0]; c.Args[0] != target.sessionID {
			t.Errorf("focused in session %s, want %s", c.Args[0], target.sessionID)
		}
		res, err := popup.AwaitResult(context.Background(), dir, 0)
		if err != nil || res.SessionID != target.sessionID || res.WindowID != target.windowID {
			t.Errorf("result = %+v, %v", res, err)
		}
	})
}

func TestEnterOnClosedPaneKeepsSwitcherOpen(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.selectEntry("nvim")
	h.fake.Fail("session.focus_block", fmt.Errorf("block gone: %w", rex.ErrNotFound))
	h.key("enter")
	if h.closed != 0 || h.m.busy {
		t.Fatalf("closed=%d busy=%v after a failed jump", h.closed, h.m.busy)
	}
	if got := h.m.toast.Current().Text; !strings.Contains(got, "That pane closed") {
		t.Errorf("toast = %q", got)
	}
}

func TestDoubleClick(t *testing.T) {
	clickRow := func(h *harness, name string) {
		h.t.Helper()
		h.click(name)
		h.click(name)
	}
	t.Run("same session goes there", func(t *testing.T) {
		h := newHarness(t, 100, 30, true)
		clickRow(h, "Revamp the switcher UI")
		if h.closed != 1 || !slices.Equal(h.fake.Methods()[len(h.fake.Methods())-1:], []string{"session.focus_block"}) {
			t.Errorf("closed=%d calls=%v", h.closed, h.fake.Methods())
		}
	})
	t.Run("across sessions needs Remote Control", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withClient("client:1"))
		clickRow(h, "Navigation polish")
		if h.closed != 0 || !strings.Contains(h.m.toast.Current().Text, "Remote Control") {
			t.Errorf("closed=%d toast=%q", h.closed, h.m.toast.Current().Text)
		}
	})
	t.Run("across sessions with Remote Control on", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withClient("client:1"))
		h.fake.RemoteControl = true
		clickRow(h, "Navigation polish")
		if h.closed != 1 || !slices.Contains(h.fake.Methods(), "client.perform") {
			t.Errorf("closed=%d calls=%v", h.closed, h.fake.Methods())
		}
	})
}

func TestBringHere(t *testing.T) {
	tests := []struct {
		name  string
		row   string
		moved bool
		toast string
	}{
		{"a pane of this session moves next to the origin", "nvim", true, ""},
		{"another session is out of reach", "kubectl", false, "panes of this session"},
		{"sessions are not panes", "studio", false, "not sessions"},
		{"the origin stays put", "zsh", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, 100, 30, true)
			if tt.row == "zsh" {
				h.key("tab")
				h.key("tab")
				h.key("tab") // Panes: the origin's zsh comes first
			}
			h.selectEntry(tt.row)
			target := h.find(tt.row)
			h.fake.ResetCalls()
			h.key("alt+enter")
			moved := slices.Contains(h.fake.Methods(), "session.move_block")
			if moved != tt.moved || (h.closed == 1) != tt.moved {
				t.Fatalf("moved=%v closed=%d calls=%v", moved, h.closed, h.fake.Methods())
			}
			if tt.toast != "" && !strings.Contains(h.m.toast.Current().Text, tt.toast) {
				t.Errorf("toast = %q, want %q", h.m.toast.Current().Text, tt.toast)
			}
			if !tt.moved {
				return
			}
			view, _ := h.fake.View(context.Background(), target.sessionID)
			w, _, _, _ := view.FindBlock(target.blockID)
			if w.ID != h.origin.windowID || w.FocusedBlockID != target.blockID {
				t.Errorf("nvim is in %s focused %s, want the origin's window, focused", w.ID, w.FocusedBlockID)
			}
		})
	}
}

func TestDismiss(t *testing.T) {
	for _, msg := range []tea.Msg{uitest.Key(t, "esc"), uitest.Key(t, "ctrl+c"), tea.BlurMsg{}} {
		h := newHarness(t, 100, 30, true)
		h.typeText("query")
		h.fake.ResetCalls()
		h.send(msg)
		if !h.quit || h.closed != 1 || len(h.fake.Calls()) != 0 {
			t.Errorf("%T: quit=%v closed=%d calls=%v, want just the float closed", msg, h.quit, h.closed, h.fake.Methods())
		}
	}
}

func TestKeyPanel(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.key("?")
	if !strings.Contains(h.screen(), "Bring pane here") {
		t.Fatalf("? did not show the key panel:\n%s", h.screen())
	}
	// Stacked under the list, the panel covers the list's bottom: the
	// selection scrolls above it and its shadow.
	s := h.m.layout()
	at := h.m.panelRect(s)
	if y := rowOnScreen(h, "› zsh"); y < 0 || y >= s.inner.Y+at.Y-1 {
		t.Errorf("the selection is on row %d, under the panel from row %d:\n%s", y, s.inner.Y+at.Y, h.screen())
	}
	before := h.selected()
	h.key("down")
	if h.m.help || strings.Contains(h.screen(), "Bring pane here") {
		t.Fatal("a key did not close the panel")
	}
	if h.selected() == before {
		t.Error("↓ closed the panel without moving")
	}
	h.key("?")
	h.key("?")
	if h.m.help {
		t.Error("? did not close the panel")
	}
}

// rowOnScreen is the row of the selected line showing text, or -1.
func rowOnScreen(h *harness, text string) int {
	lines := strings.Split(h.m.View().Content, "\n")
	sel := washed(h.m.View().Content, h.m.theme.C.SelBg)
	for y, l := range lines {
		plain := ansi.Strip(l)
		if strings.Contains(plain, text) && slices.Contains(sel, plain) {
			return y
		}
	}
	return -1
}

func TestResizeKeepsSelectionVisible(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("up") // wrap to the last row: deep in the SESSIONS tree
	last := h.rowNames()[h.m.list.Cursor()]
	shown := string([]rune(last)[:min(10, len([]rune(last)))]) // names are cut to fit narrow columns
	for _, size := range [][2]int{{100, 30}, {70, 20}, {70, 9}, {160, 40}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !slices.ContainsFunc(washed(h.m.View().Content, h.m.theme.C.SelBg), func(l string) bool { return strings.Contains(l, shown) }) || h.selected() != last {
			t.Errorf("at %dx%d the selection (%s) is off screen:\n%s", size[0], size[1], last, h.screen())
		}
	}
}

func TestPreviewShowsSelectedPane(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	if !strings.Contains(h.screen(), "Ready on http://localhost:3000") {
		t.Fatalf("preview of the preselected studio › api › zsh is missing:\n%s", h.screen())
	}
	h.selectEntry("kubectl")
	h.fire()
	if s := h.screen(); !strings.Contains(s, "CrashLoopBackOff") || strings.Contains(s, "Ready on") {
		t.Errorf("preview did not follow the selection:\n%s", s)
	}
}

func TestPreviewIgnoresStaleCaptures(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	kubectl, logs := h.find("kubectl"), h.find("tail -f production.log (eu-west-1, all services)")
	h.selectEntry("kubectl")
	staleRev := h.m.pv.rev
	h.selectEntry(logs.name)
	h.fire()
	h.send(previewMsg{rev: staleRev, block: kubectl.blockID, capture: capture{raw: "STALE OUTPUT", at: testNow}})
	if s := h.screen(); strings.Contains(s, "STALE OUTPUT") || !strings.Contains(s, "panic: nil map assignment") {
		t.Errorf("a late capture of a previous selection replaced the preview:\n%s", s)
	}
}

func TestPreviewKeepsPreviousUntilReady(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.selectEntry("kubectl")
	h.fire()
	h.selectEntry("migrations") // not captured yet: the debounce is held
	if s := h.screen(); !strings.Contains(s, "CrashLoopBackOff") {
		t.Errorf("the preview went blank before the next capture arrived:\n%s", s)
	}
	h.fire()
	if s := h.screen(); !strings.Contains(s, "select count(*)") {
		t.Errorf("the new capture did not arrive:\n%s", s)
	}
}

func TestPreviewLiveTail(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	zsh := h.find("zsh")
	for _, e := range h.m.cat.entries {
		if e.name == "zsh" && e.session == "studio" {
			zsh = e
		}
	}
	h.fake.SetCapture(zsh.blockID, "fresh line from the server\r\n")
	h.send(tailMsg{})
	if !strings.Contains(h.screen(), "fresh line from the server") {
		t.Errorf("the live tail did not refresh the preview:\n%s", h.screen())
	}
}

func TestRefreshKeepsSelectionAndUpdatesStatus(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.selectEntry("Navigation polish")
	review := h.find("Navigation polish")
	h.fake.SetTerminal(review.blockID, rex.Terminal{Status: rex.List[rex.StatusRecord]{
		{App: "codex", State: rex.StateWorking, Title: "Navigation polish", UpdatedAt: testNow},
	}})
	ch := make(chan rex.Event, 1)
	ch <- rex.Event{Type: "terminal.program_status_changed", SessionID: review.sessionID, BlockID: review.blockID}
	h.send(watchMsg{ev: <-ch, events: ch, ok: true})
	if !h.m.refresh.due {
		t.Fatal("a change event did not schedule a refresh")
	}
	h.fire() // the debounce, then the snapshot
	if got := sectionRows(h)[secNeedsYou]; slices.Contains(got, "Navigation polish") {
		t.Errorf("needs you = %q after the agent went back to work", got)
	}
	if h.selected() != "Navigation polish" || h.m.selectedSection() != secWorking {
		t.Errorf("selected %q in %q, want the same agent, now in WORKING", h.selected(), h.m.selectedSection())
	}
}

func TestWatcherLossFallsBackToPolling(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.send(watchMsg{ok: false})
	h.mu.Lock()
	defer h.mu.Unlock()
	if !slices.ContainsFunc(h.timers, func(m tea.Msg) bool { _, ok := m.(pollMsg); return ok }) {
		t.Errorf("no poll scheduled after the watcher died: %v", h.timers)
	}
}

func TestFirstFrameFromCache(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	fake, origin, history := demoWorld(testNow)
	inv, err := fake.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	never := func(time.Duration, tea.Msg) tea.Cmd { return nil }
	m := newModel(context.Background(), env{api: fake, now: func() time.Time { return testNow }, after: never}, options{origin: origin}, &inv, history)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// No Init has run, so nothing was collected: the frame comes from the cache.
	sel := washed(m.View().Content, m.theme.C.SelBg)
	if s := ansi.Strip(m.View().Content); !strings.Contains(s, "Navigation polish") || !slices.ContainsFunc(sel, func(l string) bool { return strings.Contains(l, "› zsh") }) {
		t.Errorf("first frame does not show the cached inventory with the previous location selected:\n%s", s)
	}
}

// A pane moved to another tab while the switcher is open: after a refresh the
// commit must name the tab it is in now, not the stale one.
func TestCommitAfterRefreshUsesTheNewTab(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	nvim, claude := h.find("nvim"), h.find("Revamp the switcher UI")
	h.selectEntry("nvim")
	if err := h.fake.MoveBlock(context.Background(), nvim.sessionID, rex.MoveBlockRequest{BlockID: nvim.blockID, AnchorID: claude.blockID, Direction: "horizontal", Side: "after"}); err != nil {
		t.Fatal(err)
	}
	h.key("ctrl+r")
	if h.selected() != "nvim" {
		t.Fatalf("selected %q after the refresh, want nvim still", h.selected())
	}
	h.key("enter")
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil {
		t.Fatal(err)
	}
	if res.BlockID != nvim.blockID || res.WindowID != claude.windowID {
		t.Errorf("result = %+v, want nvim in its new tab %s", res, claude.windowID)
	}
}

// Rex exports blank cells as cursor-forward; the preview must show them as
// spaces, colours intact.
func TestPreviewRestoresBlankCells(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	nvim := h.find("nvim")
	h.fake.SetCapture(nvim.blockID, "\x1b[38;2;255;255;255m⏺\x1b[0m The\x1b[1Cimprovement\x1b[Cworkflow\x1b[2Cis running.\x1b[80C\n")
	h.selectEntry("nvim")
	h.fire()
	if s := h.screen(); !strings.Contains(s, "⏺ The improvement workflow  is running.") {
		t.Errorf("blank cells collapsed:\n%s", s)
	}
}

// Every ⏎, ⎋ and ⌃C the Lua handshake writes gets a result, so Lua never
// waits out its timeout: a jump, "close", or "stay" when the switcher
// consumed the key.
func TestEveryHandshakeKeyIsAnswered(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*harness)
		key      string
		want     string
		wantOpen bool
	}{
		{"⎋ closes", nil, "esc", popup.ActionClose, false},
		{"⌃C closes", nil, "ctrl+c", popup.ActionClose, false},
		{"⎋ over the key panel only closes the panel", func(h *harness) { h.key("?") }, "esc", popup.ActionStay, true},
		{"⌃C over the key panel still closes", func(h *harness) { h.key("?") }, "ctrl+c", popup.ActionClose, false},
		{"⏎ over the key panel closes it and goes", func(h *harness) { h.key("?") }, "enter", popup.ActionGoto, false},
		{"⏎ with nothing listed", func(h *harness) { h.typeText("zzzzzz") }, "enter", popup.ActionStay, true},
		{"⏎ on a pane that closed", func(h *harness) {
			h.selectEntry("nvim")
			h.fake.Fail("session.focus_block", fmt.Errorf("gone: %w", rex.ErrNotFound))
		}, "enter", popup.ActionStay, true},
		{"⏎ jumps", func(h *harness) { h.selectEntry("nvim") }, "enter", popup.ActionGoto, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			h := newHarness(t, 100, 30, true, withStateDir(dir))
			if tt.setup != nil {
				tt.setup(h)
			}
			if tt.key == "enter" {
				h.mark() // Lua writes ⏎ with the commit marker
			}
			h.key(tt.key)
			if h.marked() {
				t.Error("the commit marker outlived its ⏎")
			}
			var res popup.Result
			if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil {
				t.Fatalf("no result: %v", err)
			}
			if res.Action != tt.want || (h.closed == 0) != tt.wantOpen {
				t.Errorf("result %q, closed %d; want %q, open %v", res.Action, h.closed, tt.want, tt.wantOpen)
			}
		})
	}
}

// TestEnterBeforeTheCatalogIsLive covers a query typed and committed while
// the first snapshot is still loading (and the ⏎ that the terminal turned
// into ⌃J because it came before raw mode): the jump waits for the data and
// then goes to what the query names.
func TestEnterBeforeTheCatalogIsLive(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	target := h.find("nvim")
	h.m.live, h.m.cat, h.m.listing = false, nil, listing{} // as if the first snapshot had not arrived
	h.m.list.SetItems(0, nil)
	h.typeText("nvim")
	h.mark()
	h.send(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if h.closed != 0 || !h.m.pending {
		t.Fatalf("closed=%d pending=%v: ⏎ must wait for the catalog", h.closed, h.m.pending)
	}
	h.send(inventoryMsg{inv: h.inventory()})
	if h.closed != 1 {
		t.Fatalf("closed=%d after the catalog arrived, want the jump", h.closed)
	}
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil || res.BlockID != target.blockID {
		t.Errorf("result = %+v (%v), want a jump to nvim %s", res, err, target.blockID)
	}
}

// ⌃J reads as ⏎ only for the LF a cooked tty made of a CR before raw mode.
// Later it is the user's own ⌃J: it moves down (⌃K up), and never commits.
func TestCtrlJAfterStartMovesDown(t *testing.T) {
	h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
	h.m.started = testNow.Add(-time.Second)
	start := h.m.list.Cursor()
	h.key("ctrl+j")
	if h.m.list.Cursor() != start+1 || h.closed != 0 {
		t.Fatalf("⌃J: cursor %d (from %d) closed=%d, want a move down", h.m.list.Cursor(), start, h.closed)
	}
	h.send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if h.m.list.Cursor() != start {
		t.Errorf("⌃K: cursor %d, want back at %d", h.m.list.Cursor(), start)
	}
	// It must not commit across sessions with no Lua handshake to switch the app.
	h.selectEntry("migrations")
	h.fake.ResetCalls()
	h.key("ctrl+j")
	if h.closed != 0 || len(h.fake.Calls()) != 0 {
		t.Errorf("⌃J on another session's pane: closed=%d calls=%v", h.closed, h.fake.Methods())
	}
}

// With nothing matching, the preview must not keep describing the pane that
// was selected before, nor the footer offer ⏎.
func TestNoMatchesClearsThePreview(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.selectEntry("kubectl")
	h.fire()
	if !strings.Contains(h.screen(), "CrashLoopBackOff") {
		t.Fatalf("no kubectl preview to start from:\n%s", h.screen())
	}
	h.typeText("zzqxw")
	h.fire()
	s := h.screen()
	// Typing passes through a match (z: zsh), so any pane's preview is stale.
	if !strings.Contains(s, "No place matches") || h.m.pv.shown != "" || strings.Contains(s, "~/code") ||
		strings.Contains(s, "CrashLoopBackOff") || strings.Contains(s, "Ready on") {
		t.Errorf("no matches, yet the preview still shows %q:\n%s", h.m.pv.shown, s)
	}
	for _, hint := range h.m.hints() {
		if hint.Key == "enter" {
			t.Errorf("the footer offers ⏎ with nothing selected: %+v", h.m.hints())
		}
	}
	for range "zzqxw" {
		h.key("backspace")
	}
	h.fire()
	if s := h.screen(); !strings.Contains(s, "Ready on http://localhost:3000") {
		t.Errorf("clearing the query did not bring back the preview of the previous location:\n%s", s)
	}
}

// A double-click (or ⌥⏎) finishes without the Lua handshake, which is what
// would leave the picker mode; the switcher leaves it itself.
func TestNonHandshakeCommitsLeaveThePickerMode(t *testing.T) {
	exited := func(h *harness) bool {
		for _, c := range h.fake.Calls() {
			if c.Method == "client.perform" && len(c.Args) > 1 && c.Args[1] == "client.mode.exit_all" {
				return true
			}
		}
		return false
	}
	t.Run("double-click", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:1"))
		h.fake.RemoteControl = true
		h.click("Revamp the switcher UI")
		h.click("Revamp the switcher UI")
		if h.closed != 1 || !exited(h) {
			t.Errorf("closed=%d calls=%v, want the mode left and the float closed", h.closed, h.fake.Methods())
		}
	})
	t.Run("enter typed without the handshake", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:1"))
		h.fake.RemoteControl = true
		h.selectEntry("nvim")
		h.key("enter") // no commit marker: the picker mode was not active
		if h.closed != 1 || !exited(h) {
			t.Errorf("closed=%d calls=%v, want the same-session jump, the mode left and the float closed", h.closed, h.fake.Methods())
		}
	})
	t.Run("alt+enter", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:1"))
		h.fake.RemoteControl = true
		h.selectEntry("nvim")
		h.key("alt+enter")
		if h.closed != 1 || !exited(h) {
			t.Errorf("closed=%d calls=%v, want the mode left and the float closed", h.closed, h.fake.Methods())
		}
	})
	t.Run("enter leaves it to Lua", func(t *testing.T) {
		h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:1"))
		h.fake.RemoteControl = true
		h.selectEntry("nvim")
		h.commit()
		if h.closed != 1 || exited(h) {
			t.Errorf("closed=%d calls=%v: Lua's handshake leaves the mode after ⏎", h.closed, h.fake.Methods())
		}
	})
}

// ⌥⏎ that only shows a toast still answers, should Lua write it.
func TestAltEnterToastAnswersStay(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 100, 30, true, withStateDir(dir))
	h.selectEntry("studio")
	h.key("alt+enter")
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(dir, popup.ResultFile), &res); err != nil || res.Action != popup.ActionStay || h.closed != 0 {
		t.Errorf("result %+v (%v) closed=%d, want stay and open", res, err, h.closed)
	}
}

// The context column starts capped, so it gives way first; room the row does
// not use goes back to it, so "session › tab" is cut only when it must be.
func TestContextTakesFreeRoom(t *testing.T) {
	natural := []int{colName: 20, colContext: 48, colStatusGlyph: 1, colStatusWord: 7, colAge: 3}
	if got := fitColumns(158, natural).Width(colContext); got != 48 {
		t.Errorf("at 158 cols the context gets %d cells, want its natural 48", got)
	}
	tight := []int{colName: 40, colContext: 48, colStatusGlyph: 1, colStatusWord: 7, colAge: 3}
	if got := fitColumns(80, tight).Width(colContext); got > columns[colContext].Max {
		t.Errorf("at 80 cols the context takes %d cells from a long name", got)
	}
	h := newHarness(t, 160, 40, true)
	h.key("ctrl+/")
	h.typeText("kubectl")
	if s := h.screen(); !strings.Contains(s, "infrastructure-and-deployment-pipelines › deploy") {
		t.Errorf("the context is cut with room to spare:\n%s", s)
	}
}

// The status and age columns keep their place as the list is searched or
// drilled into: they are laid out from the list's right edge, not from the
// rows shown.
func TestStatusColumnStays(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	col := func() int {
		for _, l := range strings.Split(h.screen(), "\n") {
			if i := strings.Index(l, "● blocked"); i >= 0 {
				return ansi.StringWidth(l[:i])
			}
		}
		t.Fatalf("no blocked row:\n%s", h.screen())
		return 0
	}
	before := col()
	h.typeText("nav")
	if got := col(); got != before {
		t.Errorf("searching moved the status column from %d to %d", before, got)
	}
}

// result reads the result the switcher wrote ("" action when none).
func (h *harness) result() popup.Result {
	h.t.Helper()
	var res popup.Result
	if err := state.ReadJSON(filepath.Join(h.m.opts.stateDir, popup.ResultFile), &res); err != nil && !errors.Is(err, fs.ErrNotExist) {
		h.t.Fatal(err)
	}
	return res
}

// A ⏎ typed while the picker mode is not active (after a config reload,
// say) reaches the tool without the commit marker: nobody would read a
// cross-session result, so the switcher must not close with one.
func TestTypedEnterAcrossSessionsNeedsTheHandshake(t *testing.T) {
	h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
	target := h.find("migrations")
	if target.sessionID == h.origin.sessionID {
		t.Fatal("migrations should be in another session")
	}
	h.selectEntry("migrations")
	h.fake.ResetCalls()
	h.key("enter")
	if h.closed != 0 || len(h.fake.Calls()) != 0 {
		t.Fatalf("closed=%d calls=%v: a cross-session ⏎ without the handshake was dropped", h.closed, h.fake.Methods())
	}
	if s := h.screen(); !strings.Contains(s, "to jump there") {
		t.Errorf("no ⌃⇧Space toast:\n%s", s)
	}
	h.commit() // ⌃⇧Space: rexkit_goto writes ⏎ with the marker
	if res := h.result(); h.closed != 1 || res.Action != popup.ActionGoto || res.SessionID != target.sessionID {
		t.Errorf("closed=%d result=%+v, want a jump to %s", h.closed, res, target.sessionID)
	}
}

// A stale marker must not turn a later typed ⏎ into a handshake: every ⏎
// takes it, whatever the ⏎ does.
func TestEveryEnterTakesTheMarker(t *testing.T) {
	for name, setup := range map[string]func(*harness){
		"key sheet":      func(h *harness) { h.key("?") },
		"nothing listed": func(h *harness) { h.typeText("zzzzzz") },
		"busy":           func(h *harness) { h.m.busy = true },
		"held":           func(h *harness) { h.m.live = false },
		"toast":          func(h *harness) { h.selectEntry("migrations"); h.key("enter") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
			setup(h)
			h.commit()
			if h.marked() {
				t.Error("the commit marker outlived its ⏎")
			}
		})
	}
}

// A ⏎ held for a fresh catalog answers "stay" after popup.HandshakeGrace,
// before Lua's await gives up. When the catalog then arrives, the ⏎ runs as
// one without the handshake: a same-session jump still goes, a cross-session
// one only shows the ⌃⇧Space toast.
func TestHeldEnterAnswersStayBeforeLuaGivesUp(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		wantClosed  int
	}{
		{"same session", "nvim", 1},
		{"another session", "migrations", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()), withClient("client:1"))
			h.fake.RemoteControl = true
			h.m.live = false
			h.typeText(tt.query)
			h.delays = nil
			h.commit()
			if h.closed != 0 || !h.m.pending || h.result().Action != "" {
				t.Fatalf("closed=%d pending=%v result=%+v: ⏎ must wait for the catalog", h.closed, h.m.pending, h.result())
			}
			// the grace counts from Lua's write of the marker, just now
			if len(h.delays) != 1 || h.delays[0].d > popup.HandshakeGrace || h.delays[0].d < popup.HandshakeGrace/2 {
				t.Fatalf("timers %+v, want one of about popup.HandshakeGrace", h.delays)
			}
			h.fire()
			if res := h.result(); res.Action != popup.ActionStay || h.closed != 0 {
				t.Fatalf("after the grace: result %+v closed=%d, want stay and open", res, h.closed)
			}
			h.send(inventoryMsg{inv: h.inventory()})
			if h.closed != tt.wantClosed {
				t.Errorf("closed=%d after the catalog arrived, want %d", h.closed, tt.wantClosed)
			}
			if tt.wantClosed == 0 && !strings.Contains(h.screen(), "to jump there") {
				t.Errorf("no ⌃⇧Space toast:\n%s", h.screen())
			}
		})
	}
}

// A held ⏎ that goes before the grace keeps its jump result: the late
// timer must not overwrite it with "stay".
func TestHeldEnterThatWentIgnoresTheGrace(t *testing.T) {
	h := newHarness(t, 100, 30, true, withStateDir(t.TempDir()))
	h.m.live = false
	h.typeText("migrations")
	h.commit()
	h.send(inventoryMsg{inv: h.inventory()})
	if res := h.result(); h.closed != 1 || res.Action != popup.ActionGoto {
		t.Fatalf("closed=%d result %+v, want the cross-session jump (the handshake reads it)", h.closed, res)
	}
	h.fire()
	if res := h.result(); res.Action != popup.ActionGoto {
		t.Errorf("the grace overwrote the jump: %+v", res)
	}
}
