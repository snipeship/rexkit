package popup

import (
	"context"
	"flag"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/backdrop"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

func TestCardRect(t *testing.T) {
	float := [4]float64{0.11, 0.13, 0.78, 0.74}
	pane := [4]float64{0.5, 0, 0.5, 1}
	tests := []struct {
		name string
		card Card
		w, h int
		want ui.Rect
	}{
		{"zero is the window", Card{}, 241, 70, ui.Rect{X: 0, Y: 0, W: 241, H: 70}},
		{"area", Card{Area: float}, 241, 70, ui.Rect{X: 27, Y: 9, W: 188, H: 52}},
		{"cells centred in the area", Card{Area: pane, Cols: 56, Rows: 7}, 240, 70, ui.Rect{X: 152, Y: 31, W: 56, H: 7}},
		{"cells centred in the window", Card{Cols: 56, Rows: 7}, 100, 30, ui.Rect{X: 22, Y: 11, W: 56, H: 7}},
		{"cells clamped to the window", Card{Area: pane, Cols: 56, Rows: 7}, 40, 5, ui.Rect{X: 0, Y: 0, W: 40, H: 5}},
		{"cells kept inside the window", Card{Area: [4]float64{0.9, 0, 0.1, 1}, Cols: 30, Rows: 3}, 100, 30, ui.Rect{X: 70, Y: 13, W: 30, H: 3}},
		{"top right", Card{Cols: 20, Rows: 1, TopRight: true}, 100, 2, ui.Rect{X: 80, Y: 0, W: 20, H: 1}},
		{"area rounding stays inside", Card{Area: [4]float64{0.5, 0.5, 0.5, 0.5}}, 7, 7, ui.Rect{X: 3, Y: 3, W: 4, H: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.card.Rect(tt.w, tt.h); got != tt.want {
				t.Errorf("Rect(%d, %d) = %+v, want %+v", tt.w, tt.h, got, tt.want)
			}
		})
	}
}

func TestCardFlags(t *testing.T) {
	parse := func(args ...string) (Options, error) {
		var o Options
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(new(strings.Builder))
		o.RegisterFlags(fs)
		return o, fs.Parse(args)
	}
	o, err := parse("--card", "0.1100,0.13,0.78,0.7400", "--card-cells", "56x7")
	if err != nil {
		t.Fatal(err)
	}
	want := Card{Area: [4]float64{0.11, 0.13, 0.78, 0.74}, Cols: 56, Rows: 7}
	if o.Card != want {
		t.Errorf("card = %+v, want %+v", o.Card, want)
	}
	if o, _ := parse(); !o.Card.IsZero() {
		t.Errorf("no flags: card = %+v, want the zero card", o.Card)
	}
	for _, bad := range [][]string{
		{"--card", "0.1,0.1,0.8"},
		{"--card", "0.1,0.1,0.8,x"},
		{"--card", "0.1,0.1,1.2,0.5"},
		{"--card", "0.1,0.1,0,0.5"},
		{"--card-cells", "56"},
		{"--card-cells", "0x7"},
	} {
		if _, err := parse(bad...); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// recorder is a model that keeps the messages it gets and draws a fixed view.
type recorder struct {
	msgs []tea.Msg
	view tea.View
}

func (r *recorder) Init() tea.Cmd { return nil }
func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	r.msgs = append(r.msgs, msg)
	return r, nil
}
func (r *recorder) View() tea.View { return r.view }

func (r *recorder) last() tea.Msg { return r.msgs[len(r.msgs)-1] }

func TestStage(t *testing.T) {
	inner := &recorder{view: tea.NewView("ab\ncd")}
	inner.view.Cursor = tea.NewCursor(1, 1)
	m := Stage(inner, Card{Cols: 2, Rows: 2})
	if Unstage(m) != inner {
		t.Fatal("Unstage does not return the inner model")
	}

	m.Update(tea.WindowSizeMsg{Width: 6, Height: 4})
	if got, want := inner.last(), (tea.WindowSizeMsg{Width: 2, Height: 2}); got != want {
		t.Errorf("size = %v, want %v", got, want)
	}

	v := m.View()
	if want := "\n" + "  ab\n  cd"; v.Content != want {
		t.Errorf("content = %q, want %q", v.Content, want)
	}
	if v.Cursor == nil || v.Cursor.X != 3 || v.Cursor.Y != 2 {
		t.Errorf("cursor = %+v, want (3, 2)", v.Cursor)
	}
	if inner.view.Cursor.X != 1 {
		t.Error("View changed the inner model's cursor")
	}

	m.Update(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft})
	if got, want := inner.last(), (tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft}); got != want {
		t.Errorf("click inside = %v, want %v", got, want)
	}
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if key, ok := inner.last().(tea.KeyPressMsg); !ok || key.String() != "ctrl+c" {
		t.Errorf("click outside = %v, want ctrl+c", inner.last())
	}
	n := len(inner.msgs)
	m.Update(tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown})
	if len(inner.msgs) != n {
		t.Errorf("a wheel outside reached the tool: %v", inner.last())
	}
	m.Update(tea.MouseMotionMsg{X: 0, Y: 3})
	if got, want := inner.last(), (tea.MouseMotionMsg{X: -2, Y: 2}); got != want {
		t.Errorf("drag outside = %v, want %v (out of range)", got, want)
	}
}

// A staged tool learns that it is in a card before its first size, once.
func TestStageSendsCardMsg(t *testing.T) {
	inner := &recorder{}
	m := Stage(inner, Card{Cols: 2, Rows: 2})
	m.Update(tea.WindowSizeMsg{Width: 6, Height: 4})
	m.Update(tea.WindowSizeMsg{Width: 8, Height: 4})
	want := []tea.Msg{CardMsg{}, tea.WindowSizeMsg{Width: 2, Height: 2}, tea.WindowSizeMsg{Width: 2, Height: 2}}
	if len(inner.msgs) != len(want) {
		t.Fatalf("the tool got %v, want %v", inner.msgs, want)
	}
	for i := range want {
		if inner.msgs[i] != want[i] {
			t.Errorf("message %d = %v, want %v", i, inner.msgs[i], want[i])
		}
	}
}

func TestStageZeroCard(t *testing.T) {
	inner := &recorder{}
	if Stage(inner, Card{}) != tea.Model(inner) {
		t.Error("the zero card wraps the model")
	}
}

func TestStageBackdrop(t *testing.T) {
	inner := &recorder{view: tea.NewView("ab\ncd")}
	var got ui.Silhouette
	backdrop := func(w, h int, card ui.Silhouette) string {
		got = card
		return strings.TrimSuffix(strings.Repeat(strings.Repeat(".", w)+"\n", h), "\n")
	}
	m := Stage(inner, Card{Cols: 2, Rows: 2}, WithBackdrop(backdrop))
	m.Update(tea.WindowSizeMsg{Width: 6, Height: 4})
	v := m.View()
	if want := "......\n..ab..\n..cd..\n......"; v.Content != want {
		t.Errorf("content = %q, want %q", v.Content, want)
	}
	if want := ui.CardSilhouette(ui.Rect{X: 2, Y: 1, W: 2, H: 2}); got != want {
		t.Errorf("backdrop saw the card at %+v, want %+v", got, want)
	}
}

func TestBackdropFlag(t *testing.T) {
	parse := func(args ...string) (Options, error) {
		var o Options
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(new(strings.Builder))
		o.RegisterFlags(fs)
		return o, fs.Parse(args)
	}
	if o, _ := parse(); o.Card.Dim {
		t.Error("clear is the default (Lua passes the setting)")
	}
	if o, _ := parse("--backdrop", "dim"); !o.Card.Dim {
		t.Error("--backdrop dim does not dim")
	}
	if o, _ := parse("--backdrop", "dim", "--backdrop", "clear"); o.Card.Dim {
		t.Error("--backdrop clear still dims")
	}
	if _, err := parse("--backdrop", "fog"); err == nil {
		t.Error("--backdrop fog: no error")
	}
	if !(Card{Dim: true}).IsZero() {
		t.Error("Dim alone is still the whole window")
	}
}

// A dimmed stage outside Rex keeps the margin see-through.
func TestStageDimOutsideRex(t *testing.T) {
	t.Setenv("REX_BLOCK", "")
	m := Stage(&recorder{view: tea.NewView("ab")}, Card{Cols: 2, Rows: 1, Dim: true}).(*staged)
	if m.backdrop != nil || m.anim != nil {
		t.Error("a backdrop outside Rex")
	}
}

// A backdrop the caller passes wins over Dim, even nil (see-through): the
// stage never reads the window then.
func TestStageOwnBackdropWinsOverDim(t *testing.T) {
	t.Setenv("REX_SESSION", "session:test")
	t.Setenv("REX_BLOCK", "block:test")
	inner := &recorder{view: tea.NewView("ab")}
	if m := Stage(inner, Card{Cols: 2, Rows: 1, Dim: true}).(*staged); m.anim == nil {
		t.Fatal("Dim in Rex has no dim backdrop")
	}
	for _, b := range []Backdrop{nil, func(int, int, ui.Silhouette) string { return "" }} {
		if m := Stage(inner, Card{Cols: 2, Rows: 1, Dim: true}, WithBackdrop(b)).(*staged); m.anim != nil {
			t.Error("Dim replaced the caller's backdrop")
		}
	}
}

func TestDimmer(t *testing.T) {
	theme := rextheme.Builtin(true)
	screens := rex.Screens{Panes: []rex.PaneScreen{{Rect: rex.Rect{0, 0, 1, 1}, Size: rex.Size{Columns: 6, Rows: 1}, Screen: "pane"}}}
	loads := 0
	d := &dimmer{load: func(context.Context) (*backdrop.Backdrop, error) {
		loads++
		return backdrop.New(theme, screens), nil
	}}
	inner := &recorder{view: tea.NewView("ab")}
	s := Stage(inner, Card{Cols: 2, Rows: 1}, WithBackdrop(d.draw)).(*staged)
	s.anim = d

	load := s.Init() // the load runs as a command: the first frame does not wait
	s.Update(tea.WindowSizeMsg{Width: 6, Height: 5})
	if v := s.View().Content; strings.Contains(v, "pane") {
		t.Errorf("drawn before the panes are loaded: %q", v)
	}
	msg := load()
	if batch, ok := msg.(tea.BatchMsg); ok { // the inner Init (nil) batched with the load
		for _, cmd := range batch {
			if cmd != nil {
				msg = cmd()
			}
		}
	}
	n := len(inner.msgs)
	_, tick := s.Update(msg)
	if len(inner.msgs) != n {
		t.Error("the tool saw the backdrop's message")
	}
	if tick == nil || d.frame != 1 {
		t.Fatalf("loaded: frame %d, tick %v", d.frame, tick)
	}
	first := s.View().Content
	if !strings.Contains(ansi.Strip(first), "paab") { // the pane, with the card over its middle
		t.Errorf("frame 1 = %q", ansi.Strip(first))
	}
	for d.frame < backdrop.FadeFrames {
		before := d.frame
		if _, cmd := s.Update(dimFrameMsg{d}); cmd == nil && d.frame < backdrop.FadeFrames {
			t.Fatalf("the fade stopped at frame %d", d.frame)
		}
		if d.frame != before+1 {
			t.Fatalf("frame %d after %d", d.frame, before)
		}
	}
	if last := s.View().Content; last == first {
		t.Error("the fade did not change the backdrop")
	}
	// A resize reloads the panes (they reflow); the same size does not.
	if _, cmd := s.Update(tea.WindowSizeMsg{Width: 6, Height: 5}); cmd != nil {
		t.Error("the same size reloads")
	}
	_, cmd := s.Update(tea.WindowSizeMsg{Width: 8, Height: 4})
	if cmd == nil {
		t.Fatal("a resize does not reload")
	}
	if got := inner.last(); got != (tea.WindowSizeMsg{Width: 2, Height: 1}) {
		t.Errorf("the tool got %v", got)
	}
	if loads != 1 {
		t.Errorf("%d loads before the resize's command ran, want 1", loads)
	}
}
