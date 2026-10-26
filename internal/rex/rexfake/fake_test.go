package rexfake

import (
	"context"
	"errors"
	"slices"
	"testing"

	"rexkit/internal/rex"
)

var ctx = context.Background()

func focused(t *testing.T, f *Fake, s string) (block, zoomed string) {
	t.Helper()
	v, err := f.View(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := v.Window(v.ActiveWindowID)
	return w.FocusedBlockID, w.ZoomedBlockID
}

func TestFloatingLayerFocusAndZoom(t *testing.T) {
	f := New()
	s, w, a := f.AddSession("work")
	split, err := f.NewSplit(ctx, s, rex.NewSplitRequest{AnchorID: a, Direction: rex.Horizontal, Side: "after", Layout: rex.Shell("b", "")})
	if err != nil {
		t.Fatal(err)
	}
	b := split.BlockIDs[0]
	if err := f.ZoomBlock(ctx, s, a, true); err != nil {
		t.Fatal(err)
	}
	if got, zoomed := focused(t, f, s); got != a || zoomed != a {
		t.Fatalf("after zoom: focus %s zoom %s", got, zoomed)
	}
	layer, err := f.NewLayer(ctx, s, rex.NewLayerRequest{WindowID: w, Bounds: rex.Rect{0.1, 0.1, 0.8, 0.8}, Focus: true, Layout: rex.Command("tool", "", "/bin/true")})
	if err != nil {
		t.Fatal(err)
	}
	if got, zoomed := focused(t, f, s); got != layer.BlockIDs[0] || zoomed != "" {
		t.Fatalf("a focused popup must end the zoom: focus %s zoom %s", got, zoomed)
	}
	if err := f.CloseLayer(ctx, s, layer.LayerID); err != nil {
		t.Fatal(err)
	}
	if got, zoomed := focused(t, f, s); got != a || zoomed != "" {
		t.Fatalf("close_layer restores focus only: focus %s zoom %s", got, zoomed)
	}
	if err := f.FocusBlock(ctx, s, b); err != nil {
		t.Fatal(err)
	}
	if got, _ := focused(t, f, s); got != b {
		t.Fatalf("focus %s, want %s", got, b)
	}
	if err := f.CloseLayer(ctx, s, layer.LayerID); !errors.Is(err, rex.ErrNotFound) {
		t.Errorf("closing twice: %v", err)
	}
}

func TestMoveBlockAndLabels(t *testing.T) {
	f := New()
	s, _, a := f.AddSession("work")
	w2, blocks := f.AddWindow(s, "logs", "x", "y")
	if err := f.MoveBlock(ctx, s, rex.MoveBlockRequest{BlockID: blocks[1], AnchorID: a, Side: "after"}); err != nil {
		t.Fatal(err)
	}
	v, _ := f.View(ctx, s)
	win, _, _, _ := v.FindBlock(blocks[1])
	if win.ID == w2 {
		t.Fatal("block did not move")
	}
	if _, l, _, _ := v.FindBlock(a); len(l.Blocks) != 2 || l.Layout.Split == nil {
		t.Fatalf("anchor layer = %+v", l)
	}
	if err := f.SetWindowLabel(ctx, s, w2, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetBlockLabel(ctx, s, a, "api"); err != nil {
		t.Fatal(err)
	}
	v, _ = f.View(ctx, s)
	if w, _ := v.Window(w2); w.Label != "renamed" {
		t.Errorf("window label = %q", w.Label)
	}
	if _, _, b, _ := v.FindBlock(a); b.Label != "api" {
		t.Errorf("block label = %q", b.Label)
	}
}

func TestTerminalStateAndSnapshot(t *testing.T) {
	f := New()
	s, _, a := f.AddSession("work")
	f.SetTerminal(a, rex.Terminal{Title: "claude", Process: &rex.Process{Foreground: &rex.ProcessInfo{Name: "claude", Cwd: "/src"}},
		Status: rex.List[rex.StatusRecord]{{State: rex.StateWorking}}})
	if err := f.Write(ctx, s, a, "ab"); err != nil {
		t.Fatal(err)
	}
	_ = f.Write(ctx, s, a, "\r")
	if got := f.Written(a); got != "ab\r" {
		t.Errorf("written = %q", got)
	}
	p, err := f.Process(ctx, s, a)
	if err != nil || p.Current().Cwd != "/src" {
		t.Errorf("process = %+v, %v", p, err)
	}
	inv, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, b, _ := inv.Sessions[0].FindBlock(a)
	if b.Terminal == nil || b.Terminal.Title != "claude" || b.Terminal.Status[0].State != rex.StateWorking {
		t.Errorf("snapshot terminal = %+v", b.Terminal)
	}
}

func TestFailAndCalls(t *testing.T) {
	f := New()
	s, _, a := f.AddSession("work")
	boom := errors.New("boom")
	f.Fail("session.focus_block", boom)
	if err := f.FocusBlock(ctx, s, a); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	f.Fail("session.focus_block", nil)
	if err := f.FocusBlock(ctx, s, a); err != nil {
		t.Fatal(err)
	}
	if err := f.ClientPerform(ctx, "client:1", "session.select", nil); !errors.Is(err, rex.ErrRefused) {
		t.Errorf("perform without Remote Control: %v", err)
	}
	want := []string{"session.focus_block", "session.focus_block", "client.perform"}
	if got := f.Methods(); !slices.Equal(got, want) {
		t.Errorf("methods = %v", got)
	}
}

func TestScreens(t *testing.T) {
	f := New()
	s, w, a := f.AddSession("work")
	split, err := f.NewSplit(ctx, s, rex.NewSplitRequest{AnchorID: a, Direction: rex.Horizontal, Side: "after", Layout: rex.Shell("b", "")})
	if err != nil {
		t.Fatal(err)
	}
	b := split.BlockIDs[0]
	f.SetSize(a, rex.Size{Columns: 10, Rows: 2})
	f.SetCapture(a, "gone\r\none\r\ntwo")
	if _, err := f.NewLayer(ctx, s, rex.NewLayerRequest{WindowID: w, Bounds: rex.Rect{0, 0, 1, 1}, Layout: rex.Command("tool", "", "/bin/true")}); err != nil {
		t.Fatal(err)
	}
	got, err := f.Screens(ctx, s, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowID != w || len(got.Panes) != 2 {
		t.Fatalf("screens = %+v, want the two tiled panes of %s", got, w)
	}
	if p := got.Panes[0]; p.BlockID != a || p.Screen != "one\ntwo" || p.Rect != (rex.Rect{0, 0, 0.5, 1}) {
		t.Errorf("pane a = %+v", p)
	}
	if p := got.Panes[1]; p.BlockID != b || p.Rect != (rex.Rect{0.5, 0, 0.5, 1}) {
		t.Errorf("pane b = %+v", p)
	}
	if err := f.ZoomBlock(ctx, s, b, true); err != nil {
		t.Fatal(err)
	}
	got, _ = f.Screens(ctx, s, b)
	if len(got.Panes) != 1 || got.Panes[0].BlockID != b || got.Panes[0].Rect != (rex.Rect{0, 0, 1, 1}) || got.Zoomed != b {
		t.Errorf("zoomed: %+v", got)
	}
	if _, err := f.Screens(ctx, s, "window:nope"); !errors.Is(err, rex.ErrNotFound) {
		t.Errorf("unknown window: %v", err)
	}
}
