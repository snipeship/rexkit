package popup

import (
	"context"
	"errors"
	"flag"
	"slices"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
)

var ctx = context.Background()

// world is a session with tiled blocks a | b in one window, a zoomed origin
// optionally, and a tool float opened from a.
type world struct {
	f             *rexfake.Fake
	s, w, a, b    string
	tool, toolLyr string
}

func newWorld(t *testing.T, zoomA bool) world {
	t.Helper()
	f := rexfake.New()
	s, w, a := f.AddSession("work")
	split, err := f.NewSplit(ctx, s, rex.NewSplitRequest{AnchorID: a, Direction: rex.Horizontal, Side: "after", Layout: rex.Shell("b", "")})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ZoomBlock(ctx, s, a, zoomA); err != nil {
		t.Fatal(err)
	}
	layer, err := f.NewLayer(ctx, s, rex.NewLayerRequest{WindowID: w, Bounds: rex.Rect{0.1, 0.1, 0.8, 0.8}, Focus: true,
		Layout: rex.Command("Go to", "", "rexkit", "goto")})
	if err != nil {
		t.Fatal(err)
	}
	f.ResetCalls()
	return world{f: f, s: s, w: w, a: a, b: split.BlockIDs[0], tool: layer.BlockIDs[0], toolLyr: layer.LayerID}
}

func (w world) focus(t *testing.T) (focused, zoomed string, layers int) {
	t.Helper()
	v, err := w.f.View(ctx, w.s)
	if err != nil {
		t.Fatal(err)
	}
	win, _ := v.Window(w.w)
	return win.FocusedBlockID, win.ZoomedBlockID, len(win.Layers)
}

func noSighup(t *testing.T) *int {
	calls := 0
	old := beforeSelfClose
	beforeSelfClose = func() { calls++ }
	t.Cleanup(func() { beforeSelfClose = old })
	return &calls
}

func TestRegisterFlags(t *testing.T) {
	tests := []struct {
		args []string
		want Options
	}{
		{nil, Options{DismissOnBlur: true}},
		{
			[]string{"--session", "s", "--origin-block", "b", "--origin-window", "w", "--zoomed", "--client", "c", "--state", "/d", "--dismiss-on-blur=false"},
			Options{Session: "s", OriginBlock: "b", OriginWindow: "w", Zoomed: true, Client: "c", State: "/d"},
		},
	}
	for _, tt := range tests {
		var o Options
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		o.RegisterFlags(fs)
		if err := fs.Parse(tt.args); err != nil || o != tt.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", tt.args, o, err, tt.want)
		}
	}
}

func TestSelfFindsTheOwnLayer(t *testing.T) {
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.tool)
	t.Setenv("REX_SESSION", w.s)
	self, err := New(w.f, Options{}).Self(ctx)
	want := Self{SessionID: w.s, WindowID: w.w, LayerID: w.toolLyr, BlockID: w.tool, Floating: true}
	if err != nil || self != want {
		t.Fatalf("Self = %+v, %v; want %+v", self, err, want)
	}
	t.Setenv("REX_BLOCK", "")
	if _, err := New(w.f, Options{}).Self(ctx); !errors.Is(err, ErrOutsideRex) {
		t.Errorf("outside Rex: %v", err)
	}
	t.Setenv("REX_BLOCK", "block:missing")
	if _, err := New(w.f, Options{}).Self(ctx); !errors.Is(err, rex.ErrNotFound) {
		t.Errorf("missing block: %v", err)
	}
}

func TestClose(t *testing.T) {
	tests := []struct {
		name        string
		zoomed      bool // origin a was zoomed before the tool opened
		setup       func(t *testing.T, w world)
		wantMethods []string
		wantFocus   func(w world) string
		wantZoom    func(w world) string
	}{
		{
			name:        "focus returns to the origin",
			wantMethods: []string{"session.view", "session.close_layer"},
			wantFocus:   func(w world) string { return w.a },
			wantZoom:    func(world) string { return "" },
		},
		{
			name:        "zoomed origin is re-zoomed before the layer closes",
			zoomed:      true,
			wantMethods: []string{"session.view", "session.zoom_block", "session.close_layer"},
			wantFocus:   func(w world) string { return w.a },
			wantZoom:    func(w world) string { return w.a },
		},
		{
			name:   "a tool that lost focus does not take it back",
			zoomed: true,
			setup: func(t *testing.T, w world) {
				if err := w.f.FocusBlock(ctx, w.s, w.b); err != nil {
					t.Fatal(err)
				}
			},
			wantMethods: []string{"session.view", "session.close_layer"},
			wantFocus:   func(w world) string { return w.b },
			wantZoom:    func(world) string { return "" },
		},
		{
			name:   "origin closed meanwhile",
			zoomed: true,
			setup: func(t *testing.T, w world) {
				w.f.Fail("session.zoom_block", rex.ErrNotFound)
			},
			wantMethods: []string{"session.view", "session.zoom_block", "session.close_layer"},
			wantFocus:   func(w world) string { return w.a },
			wantZoom:    func(world) string { return "" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hups := noSighup(t)
			w := newWorld(t, tt.zoomed)
			if tt.setup != nil {
				tt.setup(t, w)
				w.f.ResetCalls()
			}
			t.Setenv("REX_BLOCK", w.tool)
			p := New(w.f, Options{Session: w.s, OriginBlock: w.a, OriginWindow: w.w, Zoomed: tt.zoomed})
			if err := p.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := p.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if got := w.f.Methods(); !slices.Equal(got, tt.wantMethods) {
				t.Errorf("calls = %v, want %v (Close twice must act once)", got, tt.wantMethods)
			}
			focused, zoomed, layers := w.focus(t)
			if focused != tt.wantFocus(w) || zoomed != tt.wantZoom(w) || layers != 1 {
				t.Errorf("focus %s zoom %s layers %d; want focus %s zoom %s layers 1",
					focused, zoomed, layers, tt.wantFocus(w), tt.wantZoom(w))
			}
			if *hups != 1 {
				t.Errorf("SIGHUP guard ran %d times", *hups)
			}
		})
	}
}

func TestCloseOutsideAFloatDoesNothing(t *testing.T) {
	noSighup(t)
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.b) // a tool started in an ordinary pane
	if err := New(w.f, Options{Session: w.s}).Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REX_BLOCK", "")
	if err := New(w.f, Options{Session: w.s}).Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.f.Methods(); !slices.Equal(got, []string{"session.view"}) {
		t.Errorf("calls = %v", got)
	}
}

func TestCloseReportsViewErrors(t *testing.T) {
	noSighup(t)
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.tool)
	boom := errors.New("boom")
	w.f.Fail("session.view", boom)
	if err := New(w.f, Options{Session: w.s}).Close(ctx); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestGuardClosesOnPanic(t *testing.T) {
	noSighup(t)
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.tool)
	p := New(w.f, Options{Session: w.s})
	defer func() {
		if r := recover(); r != "kaboom" {
			t.Errorf("recovered %v, want the original panic", r)
		}
		if _, _, layers := w.focus(t); layers != 1 {
			t.Error("the layer was not closed")
		}
	}()
	defer p.Guard()
	panic("kaboom")
}

func TestGuardWithoutPanicIsInert(t *testing.T) {
	w := newWorld(t, false)
	func() {
		defer New(w.f, Options{Session: w.s}).Guard()
	}()
	if got := w.f.Methods(); len(got) != 0 {
		t.Errorf("calls = %v", got)
	}
}

// A failed Close is not remembered: the next call tries again and closes the
// layer with the exact focus and zoom restore.
func TestCloseRetriesAfterAFailure(t *testing.T) {
	for _, method := range []string{"session.view", "session.zoom_block", "session.close_layer"} {
		t.Run(method, func(t *testing.T) {
			noSighup(t)
			w := newWorld(t, true)
			t.Setenv("REX_BLOCK", w.tool)
			p := New(w.f, Options{Session: w.s, OriginBlock: w.a, OriginWindow: w.w, Zoomed: true})
			transient := errors.New("transient")
			w.f.Fail(method, transient)
			if err := p.Close(ctx); !errors.Is(err, transient) {
				t.Fatalf("first Close = %v", err)
			}
			w.f.Fail(method, nil)
			if err := p.Close(ctx); err != nil {
				t.Fatalf("second Close = %v", err)
			}
			focused, zoomed, layers := w.focus(t)
			if focused != w.a || zoomed != w.a || layers != 1 {
				t.Errorf("focus %s zoom %s layers %d; want focus and zoom on %s, the float gone", focused, zoomed, layers, w.a)
			}
			w.f.ResetCalls()
			if err := p.Close(ctx); err != nil || len(w.f.Methods()) != 0 {
				t.Errorf("Close after success = %v, calls %v", err, w.f.Methods())
			}
		})
	}
}
