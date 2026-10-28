package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/find"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/ui/uitest"
)

func TestGroupsByPaneOriginSessionFirst(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	want := []string{
		"claude rexkit › spark", "zsh rexkit › spark", "zsh studio › api",
		"kubectl infrastructure-and-deployment-pipelines › deploy",
		"tail -f production.log infrastructure-and-deployment-pipelines › logs", "fish scratch › probe",
	}
	if got := h.groupNames(); !slices.Equal(got, want) {
		t.Fatalf("groups =\n%q\nwant\n%q", got, want)
	}
	if h.m.total != 92 {
		t.Errorf("total = %d, want 92", h.m.total)
	}
	if got := h.selectedRow(); got != "hit 6" {
		t.Errorf("selected %q, want the first match", got)
	}
	if s := h.screen(); !strings.Contains(s, "92 matches in 6 panes") {
		t.Errorf("no count in the tray:\n%s", s)
	}
}

// A world of two sessions whose origin is the second: it is searched and
// listed first.
func TestOriginSessionComesFirst(t *testing.T) {
	f := rexfake.New()
	first, _, a := f.AddSession("first")
	second, w, b := f.AddSession("second")
	f.SetCapture(a, "needle in first\n")
	f.SetCapture(b, "needle in second\n")
	h := newHarness(t, 120, 30, true, withWorld(f, origin{session: second, window: w, block: b}))
	h.typeText("needle")
	if len(h.m.groups) != 2 || h.m.panes[h.m.groups[0].pane].session != second || h.m.panes[h.m.groups[1].pane].session != first {
		t.Fatalf("groups %q", h.groupNames())
	}
}

// rexkit's own blocks are never searched, floating or not; a float of the
// user's is, after the tab's tiled panes, as Go to lists it.
func TestSkipsItselfAndTools(t *testing.T) {
	f := rexfake.New()
	sid, w, a := f.AddSession("s")
	_, blocks := f.AddWindow(sid, "tools", "rexkit:toast", "")
	f.SetCapture(a, "needle\n")
	f.SetCapture(blocks[0], "needle\n")
	f.SetCapture(blocks[1], "needle\n")
	float, _ := f.NewLayer(context.Background(), sid, rex.NewLayerRequest{WindowID: w, Layout: rex.Shell("repl", "")})
	tool, _ := f.NewLayer(context.Background(), sid, rex.NewLayerRequest{WindowID: w, Layout: rex.Shell("rexkit:goto", "")})
	f.SetCapture(float.BlockIDs[0], "needle\n")
	f.SetCapture(tool.BlockIDs[0], "needle\n")
	h := newHarness(t, 120, 30, true, withWorld(f, origin{session: sid, window: w, block: a}))
	h.m.cfg.self = blocks[1]
	h.key("ctrl+r")
	h.typeText("needle")
	var got []string
	for _, p := range h.m.panes {
		got = append(got, p.block)
	}
	if want := []string{a, float.BlockIDs[0]}; !slices.Equal(got, want) {
		t.Fatalf("panes %q, want %q", got, want)
	}
	if h.m.total != 2 {
		t.Errorf("%d matches, want the float's too", h.m.total)
	}
}

func TestFoldsOlderMatches(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	h.selectKind("tail -f production.log", rowMore)
	if got := h.selectedRow(); got != "more 60" {
		t.Fatalf("selected %q", got)
	}
	shown := len(h.m.rows)
	h.commit() // ⏎ on the fold unfolds it, and the tool stays
	if len(h.m.rows) != shown+59 {
		t.Errorf("rows %d → %d, want 60 hits for the fold", shown, len(h.m.rows))
	}
	if got := h.selectedRow(); got != "hit "+strconv.Itoa(h.blockOf("tail -f production.log", "logs").text.firstHit("panic")) {
		// the fold's place now holds the pane's first (oldest) match
		t.Errorf("after unfolding %q is selected", got)
	}
	if r := h.result(); r.Action != popup.ActionStay {
		t.Errorf("result %+v, want stay", r)
	}
	if h.quit {
		t.Error("unfolding closed the tool")
	}
	// the unfolded pane stays unfolded as the query changes
	h.typeText(":")
	for _, g := range h.m.groups {
		if p := h.m.panes[g.pane]; p.name == "tail -f production.log" && !g.expanded {
			t.Error("the fold came back")
		}
	}
}

func TestRightUnfoldsElseMovesTheCursor(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	h.key("left")
	h.key("right")
	if h.m.input.Position() != 5 {
		t.Errorf("→ on a match moves the cursor: %d", h.m.input.Position())
	}
	h.selectKind("zsh", rowMore) // studio › api: 7 earlier
	h.key("right")
	r, _ := h.m.selected()
	if g := h.m.groups[r.group]; r.kind != rowHit || r.hit != 0 || !g.expanded {
		t.Errorf("→ on the fold: %q", h.selectedRow())
	}
}

func TestSmartCaseAndMatchCase(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	all := h.m.total
	h.typeText(":")
	h.key("backspace")
	h.key("backspace", "backspace", "backspace", "backspace", "backspace")
	h.typeText("PANIC") // a capital asks for that case
	if h.m.total != 0 {
		t.Errorf("PANIC found %d", h.m.total)
	}
	h.key("backspace", "backspace", "backspace", "backspace", "backspace")
	h.typeText("panic")
	h.key("alt+c") // match case: only lower case
	if h.m.total == 0 || h.m.total > all {
		t.Errorf("match case found %d of %d", h.m.total, all)
	}
	if !strings.Contains(h.screen(), "● match case") && !strings.Contains(h.screen(), "◉ match case") {
		t.Errorf("the chip is off:\n%s", h.screen())
	}
}

func TestRegex(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("alt+r")
	h.typeText(`status=5\d\d`)
	if h.m.total == 0 {
		t.Fatal("no 5xx found")
	}
	for _, g := range h.m.groups {
		for _, n := range g.hits {
			if l := h.m.panes[g.pane].plain[n]; !strings.Contains(l, "status=5") {
				t.Errorf("%q matched", l)
			}
		}
	}
	// ? is part of a regular expression; F1 still lists the keys
	h.typeText("?")
	if h.m.help || !strings.HasSuffix(h.m.input.Value(), "?") {
		t.Errorf("? did not go into the query: %q", h.m.input.Value())
	}
	h.key("f1")
	if !h.m.help {
		t.Error("F1 did not open the keys")
	}
	h.key("f1")
	if h.m.help {
		t.Error("F1 did not close the keys")
	}
}

func TestBadRegex(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("alt+r")
	h.typeText(`status=(5`)
	s := h.screen()
	if !strings.Contains(s, "Not a regular expression") || !strings.Contains(s, "Missing closing )") {
		t.Errorf("no error page:\n%s", s)
	}
	h.key("alt+r") // the same text, literally: nothing has it
	if !strings.Contains(h.screen(), "No matches for") {
		t.Errorf("literal search:\n%s", h.screen())
	}
}

func TestQuestionMarkOpensKeys(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("?")
	if !h.m.help || h.m.input.Value() != "" {
		t.Fatalf("? typed %q, help %v", h.m.input.Value(), h.m.help)
	}
	if !strings.Contains(h.screen(), "Open the line in Scrollback") {
		t.Errorf("no key panel:\n%s", h.screen())
	}
	h.typeText("p") // any other key closes it and still does its job
	if h.m.help || h.m.input.Value() != "p" {
		t.Errorf("help %v, query %q", h.m.help, h.m.input.Value())
	}
}

func TestThisSessionOnly(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	h.key("tab")
	if got := h.groupNames(); !slices.Equal(got, []string{"claude rexkit › spark", "zsh rexkit › spark"}) {
		t.Errorf("this session: %q", got)
	}
	h.key("tab")
	if len(h.m.groups) != 6 {
		t.Errorf("all sessions again: %q", h.groupNames())
	}
}

func TestStepPanesAndSkipGaps(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	h.key("alt+down")
	if got := h.selectedRow(); got != "hit 1233" {
		t.Errorf("⌥↓: %q", got)
	}
	h.key("alt+up")
	if got := h.selectedRow(); got != "hit 6" {
		t.Errorf("⌥↑: %q", got)
	}
	h.key("up") // claude's pane row
	if got := h.selectedRow(); got != "pane claude" {
		t.Errorf("top: %q", got)
	}
	for range 5 {
		h.key("down")
	}
	// past claude's four matches: over the air between panes onto zsh
	if got := h.selectedRow(); got != "pane zsh" {
		t.Errorf("down over the gap: %q", got)
	}
	h.key("up")
	if got := h.selectedRow(); got != "hit 32" {
		t.Errorf("up over the gap: %q", got)
	}
	h.key("up", "up", "up", "up", "up") // past the top: round to the last match
	if got := h.selectedRow(); got != "hit 7" {
		t.Errorf("round the top: %q", got)
	}
}

func TestJumpOpensScrollbackThroughLua(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	h.selectHit("tail -f production.log", 2380)
	target := h.blockOf("tail -f production.log", "logs")
	h.commit()
	if !h.quit || h.closed != 1 {
		t.Fatalf("quit %v, closed %d", h.quit, h.closed)
	}
	r := h.result()
	if r.Action != popup.ActionGoto || r.BlockID != target.block || r.SessionID != target.session || r.WindowID != target.window {
		t.Errorf("result %+v", r)
	}
	if r.Invoke != jumpAction {
		t.Errorf("invoke %q", r.Invoke)
	}
	var args jumpArgs
	if err := json.Unmarshal(r.Args, &args); err != nil {
		t.Fatal(err)
	}
	if args.BlockID != target.block || args.Line != 2380 || args.Search != "panic" || args.Regex || args.MatchCase {
		t.Errorf("args %+v", args)
	}
	page, err := os.ReadFile(args.Page)
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != target.text.page() {
		t.Error("the page is not the capture that was searched")
	}
	if lines := strings.Split(string(page), "\n"); !strings.Contains(lines[2379], "panic") {
		t.Errorf("line 2380 of the page: %q", lines[2379])
	}
	if info, _ := os.Stat(args.Page); info.Mode().Perm() != 0o600 {
		t.Errorf("page mode %v", info.Mode())
	}
	if !slices.Contains(h.fake.Methods(), "session.focus_block") {
		t.Errorf("no focus: %q", h.fake.Methods())
	}
	if _, err := os.Stat(args.Page + ".part"); !os.IsNotExist(err) {
		t.Errorf("the page was left half-written: %v", err)
	}
}

func TestJumpCarriesTheQuery(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.key("alt+r", "alt+c")
	h.typeText(`status=5\d\d`)
	h.commit()
	var args jumpArgs
	if err := json.Unmarshal(h.result().Args, &args); err != nil {
		t.Fatal(err)
	}
	if args.Search != `status=5\d\d` || !args.Regex || !args.MatchCase {
		t.Errorf("args %+v", args)
	}
}

// A ⏎ that came without Lua's handshake (typed after a config reload, or
// in a pane) cannot open Scrollback over another pane: the tool says so.
func TestJumpWithoutHandshakeStays(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	h.key("enter")
	if h.quit {
		t.Fatal("closed")
	}
	if r := h.result(); r.Action != popup.ActionStay {
		t.Errorf("result %+v", r)
	}
	if toast := h.m.toasts.Current(); !strings.Contains(toast.Text, "Reopen Search") {
		t.Errorf("toast %q", toast.Text)
	}
	h.key("alt+enter") // that still goes there
	if !h.quit {
		t.Error("⌥⏎ did not go to the pane")
	}
}

func TestAltEnterGoesToThePane(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()), withClient("client-1"))
	h.typeText("panic")
	h.selectHit("claude", 12) // the origin's session: no Remote Control needed
	h.key("alt+enter")
	if !h.quit {
		t.Fatal("did not close")
	}
	r := h.result()
	if r.Action != popup.ActionGoto || r.Invoke != "" || r.BlockID != h.blockOf("claude", "spark").block {
		t.Errorf("result %+v", r)
	}
	var performed []string
	for _, c := range h.fake.Calls() {
		if c.Method == "client.perform" {
			performed = append(performed, c.Args[1])
		}
	}
	if !slices.Equal(performed, []string{"client.mode.exit_all"}) {
		t.Errorf("client actions %q: only leaving the picker mode", performed)
	}
}

func TestAltEnterToAnotherSessionNeedsRemoteControl(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()), withClient("client-1"))
	h.typeText("panic")
	h.selectHit("kubectl", 9)
	h.key("alt+enter")
	if h.quit {
		t.Fatal("closed without going")
	}
	// ⏎ goes there with Lua's help; on a match it opens Scrollback too.
	if toast := h.m.toasts.Current(); !strings.Contains(toast.Text, "Remote Control") || len(toast.Ways) != 1 || toast.Ways[0].Label != "jump" {
		t.Errorf("toast %q %+v", toast.Text, toast.Ways)
	}
	h.selectKind("kubectl", rowPane)
	h.key("alt+enter")
	if toast := h.m.toasts.Current(); len(toast.Ways) != 1 || toast.Ways[0].Label != "go" {
		t.Errorf("on a pane row: %+v", toast.Ways)
	}
	h.fake.RemoteControl = true
	h.key("alt+enter")
	if !h.quit {
		t.Error("with Remote Control it goes")
	}
}

func TestEnterOnAPaneRowGoesThere(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	h.selectKind("kubectl", rowPane)
	h.commit() // another session: Lua selects it
	r := h.result()
	if !h.quit || r.Action != popup.ActionGoto || r.Invoke != "" || r.BlockID != h.blockOf("kubectl", "deploy").block {
		t.Errorf("quit %v, result %+v", h.quit, r)
	}
}

func TestEnterWaitsForThePanes(t *testing.T) {
	h := newIdle(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	mark(t, h.m.cfg.stateDir)
	h.m.Update(uitest.Key(t, "enter")) // nothing is read yet; its grace never ends here
	if !h.m.pending || h.quit {
		t.Fatalf("pending %v, quit %v", h.m.pending, h.quit)
	}
	h.run(h.m.read())
	if !h.quit {
		t.Fatal("the held ⏎ did not jump once the panes were in")
	}
	if r := h.result(); r.Invoke != jumpAction {
		t.Errorf("result %+v: the handshake's ⏎ opens Scrollback", r)
	}
}

func TestHeldEnterAnswersStayInTime(t *testing.T) {
	h := newIdle(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	mark(t, h.m.cfg.stateDir)
	h.m.Update(uitest.Key(t, "enter"))
	h.m.Update(stillPendingMsg{rev: h.m.pendingRev}) // the grace is over (its toast's timer is not run)
	if h.m.pending {
		t.Error("still pending after the grace")
	}
	if r := h.result(); r.Action != popup.ActionStay {
		t.Errorf("result %+v", r)
	}
	h.run(h.m.read())
	if h.quit {
		t.Error("a dropped ⏎ acted later")
	}
}

// Editing the query drops a ⏎ held for the one before.
func TestEditingDropsAHeldEnter(t *testing.T) {
	for _, handshake := range []bool{true, false} {
		h := newIdle(t, 160, 40, true, withStateDir(t.TempDir()))
		h.typeText("panic")
		if handshake {
			mark(t, h.m.cfg.stateDir)
		}
		h.m.Update(uitest.Key(t, "enter"))
		if !h.m.pending {
			t.Fatal("not held")
		}
		h.typeText(":")
		if h.m.pending {
			t.Errorf("handshake %v: still held after an edit", handshake)
		}
		if handshake {
			if r := h.result(); r.Action != popup.ActionStay {
				t.Errorf("result %+v, want stay", r)
			}
		}
		h.run(h.m.read())
		if h.quit {
			t.Errorf("handshake %v: the dropped ⏎ acted", handshake)
		}
	}
}

// A held ⏎ ends when the last pane to come in fails and nothing was found.
func TestHeldEnterEndsWhenTheLastReadFails(t *testing.T) {
	f := rexfake.New()
	sid, w, a := f.AddSession("s")
	h := newIdle(t, 120, 30, true, withStateDir(t.TempDir()), withWorld(f, origin{session: sid, window: w, block: a}),
		withAPI(func(f *rexfake.Fake) rex.API { return failing{f, a} }))
	h.typeText("needle")
	mark(t, h.m.cfg.stateDir)
	h.m.Update(uitest.Key(t, "enter"))
	h.run(h.m.read())
	if h.m.pending || h.quit {
		t.Errorf("pending %v, quit %v", h.m.pending, h.quit)
	}
	if r := h.result(); r.Action != popup.ActionStay {
		t.Errorf("result %+v, want stay", r)
	}
}

// A handshake ⏎ whose grace ran out while the panes were read answers stay
// rather than acting after Lua stopped waiting.
func TestHeldEnterPastItsGraceStays(t *testing.T) {
	h := newIdle(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	mark(t, h.m.cfg.stateDir)
	old := time.Now().Add(-2 * popup.HandshakeGrace)
	if err := os.Chtimes(filepath.Join(h.m.cfg.stateDir, popup.CommitFile), old, old); err != nil {
		t.Fatal(err)
	}
	h.m.Update(uitest.Key(t, "enter")) // its grace tick is not run
	if !h.m.pending {
		t.Fatal("not held")
	}
	// The panes come in (their toast's timer is not run).
	pending := h.m.pending
	h.m.pending = false
	h.run(h.m.read())
	h.m.pending = pending
	h.m.runPending()
	if h.quit || h.m.pending {
		t.Fatalf("quit %v, pending %v", h.quit, h.m.pending)
	}
	if r := h.result(); r.Action != popup.ActionStay {
		t.Errorf("result %+v, want stay", r)
	}
	if toast := h.m.toasts.Current(); toast.Text != "The matches are in" {
		t.Errorf("toast %q", toast.Text)
	}
}

// A search replaced by a newer one is dropped when it comes in late.
func TestStaleSearchesAreDropped(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("alt+r")
	_, old := h.m.Update(uitest.Type("err")[0]) // e: a regex runs off the UI goroutine
	h.typeText("rror")
	total, rows := h.m.total, len(h.m.rows)
	h.run(old) // the search for "e" ends last
	if h.m.total != total || len(h.m.rows) != rows || h.m.shown.Text != "error" {
		t.Fatalf("a late search for %q replaced %q: %d → %d matches", "e", "error", total, h.m.total)
	}
	for _, g := range h.m.groups {
		for _, n := range g.hits {
			if l := strings.ToLower(h.m.panes[g.pane].plain[n]); !strings.Contains(l, "error") {
				t.Errorf("%q listed for error", l)
			}
		}
	}
	// One that slips past the stale check is dropped as it comes in.
	every := make([]int, len(h.m.panes[0].plain))
	for i := range every {
		every[i] = i
	}
	h.send(searchedMsg{gen: h.m.gen - 1, panes: []int{0}, hits: [][]int{every}, all: true})
	if h.m.total != total || len(h.m.rows) != rows {
		t.Errorf("a stale result was shown: %d → %d matches", total, h.m.total)
	}
	m, _ := find.Query{Text: "x"}.Compile()
	if _, ok := searchTexts(m, []text{newText("x\n")}, func() bool { return true }); ok {
		t.Error("a stale search ran to its end")
	}
}

// A literal search over many lines runs off the UI goroutine too.
func TestLongLiteralSearchesRunAside(t *testing.T) {
	f := rexfake.New()
	sid, w, a := f.AddSession("s")
	var b strings.Builder
	for i := range syncLines + 10 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	b.WriteString("the needle\n")
	f.SetCapture(a, b.String())
	h := newHarness(t, 120, 30, true, withWorld(f, origin{session: sid, window: w, block: a}))
	var cmd tea.Cmd
	for _, msg := range uitest.Type("needle") {
		_, cmd = h.m.Update(msg)
	}
	if h.m.ready || cmd == nil {
		t.Fatalf("ready %v: the search ran on the UI goroutine", h.m.ready)
	}
	h.run(cmd)
	if h.m.total != 1 || h.selectedRow() != "hit "+strconv.Itoa(syncLines+11) {
		t.Errorf("%d matches, selected %q", h.m.total, h.selectedRow())
	}
}

func TestCopyLine(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	h.selectHit("zsh", 1233)
	h.key("ctrl+y")
	if len(h.copied) != 1 || !strings.HasPrefix(h.copied[0], "panic: runtime error") {
		t.Errorf("copied %q", h.copied)
	}
	if toast := h.m.toasts.Current(); toast.Text != "Copied line 1233 of zsh" {
		t.Errorf("toast %q", toast.Text)
	}
}

// failing fails the capture of one block.
type failing struct {
	rex.API
	block string
}

func (f failing) Capture(ctx context.Context, s, b string, o rex.CaptureOptions) (string, error) {
	if b == f.block {
		return "", errors.New("capture timed out")
	}
	return f.API.Capture(ctx, s, b, o)
}

func TestUnreadablePane(t *testing.T) {
	var kubectl string
	h := newHarness(t, 160, 40, true, withAPI(func(f *rexfake.Fake) rex.API {
		inv, _ := f.Snapshot(context.Background())
		for _, p := range panesOf(inv, origin{}, "") {
			if p.name == "kubectl" {
				kubectl = p.block
			}
		}
		return failing{f, kubectl}
	}))
	h.typeText("panic")
	if s := h.screen(); !strings.Contains(s, "90 matches in 5 panes · 1 pane could not be read") {
		t.Errorf("tray:\n%s", s)
	}
	h.key("tab") // kubectl is in another session
	if strings.Contains(h.screen(), "could not be read") {
		t.Error("this session's panes were all read")
	}
}

func TestReadAgain(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("segfault")
	if h.m.total != 0 {
		t.Fatal("found segfault")
	}
	p := h.blockOf("fish", "probe")
	h.fake.SetCapture(p.block, "a segfault\n")
	h.key("ctrl+r")
	if h.m.total != 1 || h.m.panes[h.m.groups[0].pane].block != p.block {
		t.Errorf("after ⌃R: %d matches %q", h.m.total, h.groupNames())
	}
}

func TestEscCloses(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("esc")
	if !h.quit {
		t.Error("⎋ did not close")
	}
}

func TestBlurDismisses(t *testing.T) {
	h := newHarness(t, 160, 40, true, withFloat(true))
	h.send(tea.FocusMsg{})
	h.send(tea.BlurMsg{})
	if !h.quit {
		t.Error("blur did not close")
	}
	h = newHarness(t, 160, 40, true, withFloat(false))
	h.send(tea.FocusMsg{})
	h.send(tea.BlurMsg{})
	if h.quit {
		t.Error("closed with dismiss-on-blur off")
	}
}

// counting holds every capture until released and counts those running.
type counting struct {
	rex.API
	running, most atomic.Int32
	release       chan struct{}
}

func (c *counting) Capture(ctx context.Context, s, b string, o rex.CaptureOptions) (string, error) {
	n := c.running.Add(1)
	defer c.running.Add(-1)
	for {
		m := c.most.Load()
		if n <= m || c.most.CompareAndSwap(m, n) {
			break
		}
	}
	select {
	case <-c.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return c.API.Capture(ctx, s, b, o)
}

func TestReadsFourPanesAtATime(t *testing.T) {
	c := &counting{release: make(chan struct{})}
	h := newIdle(t, 160, 40, true, withAPI(func(f *rexfake.Fake) rex.API { c.API = f; return c }))
	h.m.read()
	inv, _ := h.fake.Snapshot(context.Background())
	_, cmd := h.m.Update(inventoryMsg{gen: h.m.readGen, inv: inv})
	if h.m.reading != maxReads || len(h.m.queue) != len(h.m.panes)-maxReads {
		t.Fatalf("reading %d, queued %d of %d", h.m.reading, len(h.m.queue), len(h.m.panes))
	}
	// run every capture command at once, as Bubble Tea does
	msgs := make(chan tea.Msg, 16)
	var start func(tea.Cmd)
	start = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					start(c)
				}
				return
			}
			msgs <- msg
		}()
	}
	start(cmd)
	// All four are in flight together before any is let go.
	for wait := time.Now().Add(5 * time.Second); c.running.Load() < maxReads; {
		if time.Now().After(wait) {
			t.Fatalf("%d captures running, want %d", c.running.Load(), maxReads)
		}
		time.Sleep(time.Millisecond)
	}
	close(c.release)
	read := 0
	deadline := time.After(5 * time.Second)
	for read < len(h.m.panes) {
		select {
		case msg := <-msgs:
			cm, ok := msg.(captureMsg)
			if !ok {
				continue
			}
			read++
			_, next := h.m.Update(cm)
			start(next)
		case <-deadline:
			t.Fatalf("read %d of %d panes", read, len(h.m.panes))
		}
	}
	if most := c.most.Load(); most != maxReads {
		t.Errorf("at most %d captures ran at once, want %d", most, maxReads)
	}
}

// startAll runs a command and every command of its batches at once, as
// Bubble Tea does, and sends their messages to msgs.
func startAll(cmd tea.Cmd, msgs chan<- tea.Msg) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				startAll(c, msgs)
			}
			return
		}
		msgs <- msg
	}()
}

// holdReads starts the tool on a Rex whose captures wait to be released, and
// waits until maxReads of them are running.
func holdReads(t *testing.T, ctx context.Context) (*harness, *counting, chan tea.Msg) {
	t.Helper()
	c := &counting{release: make(chan struct{})}
	h := newIdle(t, 160, 40, true, withAPI(func(f *rexfake.Fake) rex.API { c.API = f; return c }))
	h.m = newModel(ctx, h.m.cfg)
	_, _ = h.m.Update(teaSize(160, 40))
	h.m.read()
	inv, _ := h.fake.Snapshot(context.Background())
	_, cmd := h.m.Update(inventoryMsg{gen: h.m.readGen, inv: inv})
	msgs := make(chan tea.Msg, 64)
	startAll(cmd, msgs)
	for wait := time.Now().Add(5 * time.Second); c.running.Load() < maxReads; {
		if time.Now().After(wait) {
			t.Fatalf("%d captures running, want %d", c.running.Load(), maxReads)
		}
		time.Sleep(time.Millisecond)
	}
	return h, c, msgs
}

// captures waits for n capture messages.
func captures(t *testing.T, msgs <-chan tea.Msg, n int) []captureMsg {
	t.Helper()
	var out []captureMsg
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case msg := <-msgs:
			if cm, ok := msg.(captureMsg); ok {
				out = append(out, cm)
			}
		case <-deadline:
			t.Fatalf("%d of %d captures came back", len(out), n)
		}
	}
	return out
}

func TestClosingStopsTheReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, _, msgs := holdReads(t, ctx)
	cancel() // the tool closes
	for _, cm := range captures(t, msgs, maxReads) {
		if !errors.Is(cm.err, context.Canceled) {
			t.Errorf("pane %d: %v, want cancelled", cm.pane, cm.err)
		}
	}
}

// ⌃R cancels the reads still running; what they bring back late is ignored.
func TestReadAgainStopsTheReads(t *testing.T) {
	h, _, msgs := holdReads(t, context.Background())
	gen := h.m.readGen
	h.m.read() // ⌃R
	late := captures(t, msgs, maxReads)
	for _, cm := range late {
		if !errors.Is(cm.err, context.Canceled) || cm.gen != gen {
			t.Errorf("pane %d: %v (read %d), want the first read cancelled", cm.pane, cm.err, cm.gen)
		}
		h.m.Update(cm)
	}
	if h.m.failed != 0 || h.m.readCount() != 0 {
		t.Errorf("the cancelled reads counted: %d failed, %d read", h.m.failed, h.m.readCount())
	}
}

// A ⌥⏎ that fails leaves the tool open and its reads running: the panes
// not read yet are still read.
func TestFailedEnterKeepsReading(t *testing.T) {
	h, c, msgs := holdReads(t, context.Background())
	h.m.cfg.close = func(context.Context) error { return errors.New("the float would not close") }
	h.typeText("panic")
	for len(h.m.rows) == 0 { // let reads in one by one until there is a match
		c.release <- struct{}{}
		_, next := h.m.Update(captures(t, msgs, 1)[0])
		startAll(next, msgs)
	}
	h.key("alt+enter")
	if h.quit || h.m.busy || !h.m.scanning() {
		t.Fatalf("quit %v, busy %v, scanning %v", h.quit, h.m.busy, h.m.scanning())
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
			t.Fatalf("read %d of %d panes", h.m.readCount(), len(h.m.panes))
		}
	}
	if h.m.failed != 0 || h.m.total != 92 {
		t.Errorf("%d panes could not be read, %d matches, want every pane's 92", h.m.failed, h.m.total)
	}
}

func TestPaneNames(t *testing.T) {
	for _, tt := range []struct{ label, program, want string }{
		{"", "zsh", "zsh"},
		{"shell", "nvim", "nvim"},
		{"Terminal", "", "Terminal"},
		{"api server", "node", "api server"},
		{"\x1b[31mred\x1b[0m", "", "red"},
	} {
		if got := paneName(tt.label, tt.program); got != tt.want {
			t.Errorf("paneName(%q, %q) = %q, want %q", tt.label, tt.program, got, tt.want)
		}
	}
	panes := []*pane{{block: "a", name: "zsh", cwd: "/Users/demo/code/api"}, {block: "b", name: "nvim"}}
	for _, tt := range []struct {
		w    rex.Window
		want string
	}{
		{rex.Window{Label: "deploy"}, "deploy"},
		{rex.Window{Label: "Window 726ac4f9", FocusedBlockID: "a"}, "api"},
		{rex.Window{FocusedBlockID: "b"}, "nvim"},
	} {
		if got := tabName(tt.w, panes, 2); got != tt.want {
			t.Errorf("tabName(%+v) = %q, want %q", tt.w, got, tt.want)
		}
	}
	if got := tabName(rex.Window{}, nil, 3); got != "Tab 3" {
		t.Errorf("an empty tab is %q", got)
	}
}

func TestNewTextMatchesScrollback(t *testing.T) {
	tx := newText("\x1b[31mred\x1b[0m line\r\n\x1b]52;c;Y2xpcA==\x07plain\n\n  \n")
	if !slices.Equal(tx.plain, []string{"red line", "plain"}) {
		t.Errorf("plain %q", tx.plain)
	}
	if tx.page() != tx.lines[0]+"\n"+tx.lines[1]+"\n" {
		t.Errorf("page %q", tx.page())
	}
	if !strings.Contains(tx.lines[0], "\x1b[") {
		t.Errorf("colours dropped: %q", tx.lines[0])
	}
}

// firstHit is the first line (from 1) that has q, ignoring case.
func (t text) firstHit(q string) int {
	for i, l := range t.fold {
		if strings.Contains(l, q) {
			return i + 1
		}
	}
	return 0
}

func TestSqueezeKeepsSpans(t *testing.T) {
	s, spans := squeeze("a     panic  b", [][2]int{{6, 11}})
	if s != "a  panic  b" || s[spans[0][0]:spans[0][1]] != "panic" {
		t.Errorf("squeeze = %q %v", s, spans)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{1: "1 match", 2: "2 matches", 1204: "1,204 matches"} {
		if got := plural(n, "match"); got != want {
			t.Errorf("plural(%d) = %q", n, got)
		}
	}
	if got := plural(3, "pane"); got != "3 panes" {
		t.Errorf("%q", got)
	}
}

// Opened with a query (Go to's ⌃F), the field holds it with the cursor at
// its end, and it is searched as soon as the panes are read: the same
// results, in the same order, as typing it.
func TestOpensWithAQuery(t *testing.T) {
	typed := newHarness(t, 160, 40, true)
	typed.typeText("panic")
	h := newHarness(t, 160, 40, true, withQuery("panic"))
	if got := h.m.input.Value(); got != "panic" || h.m.input.Position() != len("panic") {
		t.Fatalf("field %q, cursor at %d", got, h.m.input.Position())
	}
	if !slices.Equal(h.groupNames(), typed.groupNames()) || h.m.total != typed.m.total || h.selectedRow() != typed.selectedRow() {
		t.Errorf("groups %q (%d), selected %q; typed: %q (%d), %q",
			h.groupNames(), h.m.total, h.selectedRow(), typed.groupNames(), typed.m.total, typed.selectedRow())
	}
	if s := h.screen(); !strings.Contains(s, "92 matches in 6 panes") {
		t.Errorf("no count in the tray:\n%s", s)
	}
	h.typeText("!") // typing goes on at the end
	if got := h.m.input.Value(); got != "panic!" {
		t.Errorf("field %q after typing, want panic!", got)
	}
}

// ⏎ pressed before the panes of a query from Go to are read waits for the
// matches, as a ⏎ typed fast after the query does, and then opens the
// first one in Scrollback.
func TestEnterRightAfterAQueryWaits(t *testing.T) {
	h := newIdle(t, 160, 40, true, withQuery("panic"), withStateDir(t.TempDir()))
	mark(t, h.m.cfg.stateDir)
	h.m.Update(uitest.Key(t, "enter")) // nothing is read yet; its grace never ends here
	if !h.m.pending || h.quit {
		t.Fatalf("pending %v, quit %v", h.m.pending, h.quit)
	}
	h.run(h.m.read())
	if r := h.result(); !h.quit || r.Invoke != jumpAction {
		t.Errorf("quit %v, result %+v: the held ⏎ opens the first match in Scrollback", h.quit, r)
	}
}

// A query handed over by Go to is searched as the panes come in, in any
// order. When another session's pane is read before the origin's, its
// match is selected only until the origin's arrives above it, and a ⏎
// pressed in between waits for that: it opens the origin's match, as if
// the panes had come in in order.
func TestFirstMatchIsTheOriginsWhateverIsReadFirst(t *testing.T) {
	f := rexfake.New()
	_, _, far := f.AddSession("logs")
	sid, w, home := f.AddSession("work")
	f.SetCapture(far, "1\n2\nERROR needle\n")
	f.SetCapture(home, "a needle at home\n")
	h := newIdle(t, 120, 30, true, withQuery("needle"), withStateDir(t.TempDir()),
		withWorld(f, origin{session: sid, window: w, block: home}))
	h.m.read()
	inv, _ := f.Snapshot(context.Background())
	h.m.Update(inventoryMsg{gen: h.m.readGen, inv: inv})
	if len(h.m.panes) != 2 || h.m.panes[0].block != home {
		t.Fatalf("panes %+v, want the origin's first", h.m.panes)
	}
	capture := func(i int) {
		raw, _ := f.Capture(context.Background(), h.m.panes[i].session, h.m.panes[i].block, captureOptions)
		_, cmd := h.m.Update(captureMsg{gen: h.m.readGen, pane: i, text: newText(raw)})
		h.run(cmd)
	}
	capture(1) // the other session's pane comes in first
	if got := h.selectedRow(); got != "hit 3" {
		t.Fatalf("selected %q, want the only match so far", got)
	}
	mark(t, h.m.cfg.stateDir)
	h.m.Update(uitest.Key(t, "enter"))
	if !h.m.pending || h.quit {
		t.Fatalf("pending %v, quit %v: ⏎ must wait for the origin's pane", h.m.pending, h.quit)
	}
	capture(0)
	if r, _ := h.m.selected(); h.m.panes[h.m.groups[r.group].pane].block != home {
		t.Errorf("selected %q in %s, want the origin's match", h.selectedRow(), h.m.panes[h.m.groups[r.group].pane].block)
	}
	if r := h.result(); !h.quit || r.Invoke != jumpAction || r.BlockID != home {
		t.Errorf("quit %v, result %+v: the held ⏎ opens the origin's match", h.quit, r)
	}
}

// A row the person moved to stays selected as more panes come in, and ⏎
// on it does not wait.
func TestAPickedMatchStays(t *testing.T) {
	f := rexfake.New()
	_, _, far := f.AddSession("logs")
	sid, w, home := f.AddSession("work")
	other, _ := f.NewSplit(context.Background(), sid, rex.NewSplitRequest{
		AnchorID: home, Direction: rex.Horizontal, Side: "after", Layout: rex.Shell("", ""),
	})
	f.SetCapture(far, "needle far\n")
	f.SetCapture(home, "needle one\nneedle two\n")
	f.SetCapture(other.BlockIDs[0], "needle other\n")
	h := newIdle(t, 120, 30, true, withQuery("needle"), withStateDir(t.TempDir()),
		withWorld(f, origin{session: sid, window: w, block: home}))
	h.m.read()
	inv, _ := f.Snapshot(context.Background())
	h.m.Update(inventoryMsg{gen: h.m.readGen, inv: inv})
	capture := func(i int) {
		raw, _ := f.Capture(context.Background(), h.m.panes[i].session, h.m.panes[i].block, captureOptions)
		_, cmd := h.m.Update(captureMsg{gen: h.m.readGen, pane: i, text: newText(raw)})
		h.run(cmd)
	}
	capture(0)
	h.key("down") // to "needle two"
	if got := h.selectedRow(); got != "hit 2" {
		t.Fatalf("selected %q, want hit 2", got)
	}
	capture(2)
	if got := h.selectedRow(); got != "hit 2" {
		t.Errorf("selected %q after another pane came in, want the picked hit 2", got)
	}
	mark(t, h.m.cfg.stateDir)
	_, cmd := h.m.Update(uitest.Key(t, "enter")) // the pane in between is not read yet
	h.run(cmd)
	if r := h.result(); h.m.pending || !h.quit || r.Invoke != jumpAction || r.BlockID != home {
		t.Errorf("pending %v quit %v result %+v: ⏎ on a picked match goes at once", h.m.pending, h.quit, r)
	}
}
