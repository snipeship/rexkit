package popup

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

// Card is the part of its float a tool draws in.
//
// Rex's app draws a floating layer at [x, y, w, h] as if w and h were its
// right and bottom edges, while the server sizes the layer's terminal from
// the documented [x, y, w, h]. A float that does
// not start at the window's top-left corner therefore loses its right and
// bottom edges; only one at [0, 0, w, h] is drawn whole. So Lua opens every
// tool over the whole window and passes the card the tool should draw in.
// Stage leaves the margin around the card at the terminal's default
// background, which Rex draws see-through: the card floats over the panes.
// With Dim (Lua's backdrop setting) it draws the panes there instead,
// dimmed, so the card reads as a layer above them (internal/backdrop).
type Card struct {
	Area       [4]float64 // x, y, w, h as fractions of the window; zero is the whole window
	Cols, Rows int        // a fixed size placed in Area; zero fills Area
	TopRight   bool       // place a fixed size in Area's top-right corner, not its centre
	// Dim draws the window's own panes, dimmed, around the card (and a
	// shadow under it) instead of leaving the margin see-through
	// (--backdrop dim; see dimmer). It needs Rex ($REX_SESSION, $REX_BLOCK).
	Dim bool
}

// IsZero reports whether the card is the whole window.
func (c Card) IsZero() bool {
	return c.Area == [4]float64{} && c.Cols == 0 && c.Rows == 0 && !c.TopRight
}

// Rect returns the card in a w×h window, clamped to it.
func (c Card) Rect(w, h int) ui.Rect {
	r := ui.Rect{W: w, H: h}
	if c.Area != [4]float64{} {
		r = ui.Rect{X: frac(c.Area[0], w), Y: frac(c.Area[1], h), W: frac(c.Area[2], w), H: frac(c.Area[3], h)}
	}
	if c.Cols > 0 {
		if c.TopRight {
			r.X += r.W - c.Cols
		} else {
			r.X += (r.W - c.Cols) / 2
		}
		r.W = c.Cols
	}
	if c.Rows > 0 {
		if !c.TopRight {
			r.Y += (r.H - c.Rows) / 2
		}
		r.H = c.Rows
	}
	r.W, r.H = min(max(r.W, 0), w), min(max(r.H, 0), h)
	r.X, r.Y = min(max(r.X, 0), w-r.W), min(max(r.Y, 0), h-r.H)
	return r
}

func frac(f float64, n int) int { return int(math.Round(f * float64(n))) }

// cardArea is the --card flag: "x,y,w,h" fractions of the window.
type cardArea struct{ c *Card }

func (a cardArea) String() string {
	if a.c == nil || a.c.Area == [4]float64{} {
		return ""
	}
	parts := make([]string, 4)
	for i, f := range a.c.Area {
		parts[i] = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.Join(parts, ",")
}

func (a cardArea) Set(s string) error {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return errors.New("want x,y,w,h")
	}
	var area [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("%q is not a fraction from 0 to 1", p)
		}
		area[i] = f
	}
	if area[2] == 0 || area[3] == 0 {
		return errors.New("w and h must be above 0")
	}
	a.c.Area = area
	return nil
}

// cardCells is the --card-cells flag: "COLSxROWS".
type cardCells struct{ c *Card }

func (s cardCells) String() string {
	if s.c == nil || s.c.Cols == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", s.c.Cols, s.c.Rows)
}

func (s cardCells) Set(v string) error {
	cols, rows, ok := strings.Cut(v, "x")
	c, err1 := strconv.Atoi(cols)
	r, err2 := strconv.Atoi(rows)
	if !ok || err1 != nil || err2 != nil || c <= 0 || r <= 0 {
		return errors.New("want COLSxROWS")
	}
	s.c.Cols, s.c.Rows = c, r
	return nil
}

// Backdrop draws what shows around the card: a w×h view of the whole
// window, over which the card (with silhouette card, which its shadow
// follows) is drawn. Without one the margin is left see-through.
type Backdrop func(w, h int, card ui.Silhouette) string

// CardMsg tells a staged tool that it is drawn in a card: Stage sends it
// before the first size. A tool frames its view with ui.FloatFrame(w, h,
// carded), carded once it got this; a tool that never gets one fills its
// window.
type CardMsg struct{}

// StageOption tunes Stage.
type StageOption func(*staged)

// WithBackdrop draws b behind the card, whatever the card's Dim; nil keeps
// the margin see-through. Snapshots pass a made-up window this way.
func WithBackdrop(b Backdrop) StageOption {
	return func(s *staged) { s.backdrop, s.ownBackdrop = b, true }
}

// Stage runs m in card c: m gets a CardMsg, then sees the card's size and
// mouse events in card coordinates, and its view is drawn at the card's
// place. A click outside
// the card reaches m as ⌃C, which closes every tool. With the zero card
// Stage returns m itself.
func Stage(m tea.Model, c Card, opts ...StageOption) tea.Model {
	if c.IsZero() {
		return m
	}
	s := &staged{Model: m, card: c}
	for _, o := range opts {
		o(s)
	}
	if c.Dim && !s.ownBackdrop {
		if d := dimInRex(); d != nil {
			s.backdrop, s.anim = d.draw, d
		}
	}
	return s
}

// Unstage returns the model Stage wrapped (a final model from tea.Program.Run).
func Unstage(m tea.Model) tea.Model {
	if s, ok := m.(*staged); ok {
		return s.Model
	}
	return m
}

type staged struct {
	tea.Model
	card     Card
	rect     ui.Rect // the card in the window; empty until the first WindowSizeMsg
	win      ui.Rect // the whole window
	backdrop Backdrop
	anim     animated // the backdrop, when it loads and animates itself
	// ownBackdrop: the caller chose the backdrop (WithBackdrop), so Dim
	// does not load the window's panes.
	ownBackdrop bool
	told        bool // the CardMsg went out
}

func (s *staged) Init() tea.Cmd {
	if s.anim == nil {
		return s.Model.Init()
	}
	return tea.Batch(s.Model.Init(), s.anim.Init())
}

func (s *staged) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var bcmd tea.Cmd
	if s.anim != nil {
		var own bool
		if bcmd, own = s.anim.Update(msg); own {
			return s, bcmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.rect = s.card.Rect(msg.Width, msg.Height)
		s.win = ui.Rect{W: msg.Width, H: msg.Height}
		var told tea.Cmd
		if !s.told {
			s.told = true
			_, told = s.forward(CardMsg{})
		}
		m, cmd := s.forward(tea.WindowSizeMsg{Width: s.rect.W, Height: s.rect.H})
		return m, tea.Batch(told, cmd, bcmd)
	case tea.MouseClickMsg:
		mouse, in := s.local(msg.Mouse())
		if !in {
			return s.forward(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		}
		return s.forward(tea.MouseClickMsg(mouse))
	case tea.MouseWheelMsg:
		if mouse, in := s.local(msg.Mouse()); in {
			return s.forward(tea.MouseWheelMsg(mouse))
		}
		return s, nil
	case tea.MouseMotionMsg: // a drag may leave the card: pass it on, out of range
		mouse, _ := s.local(msg.Mouse())
		return s.forward(tea.MouseMotionMsg(mouse))
	case tea.MouseReleaseMsg:
		mouse, _ := s.local(msg.Mouse())
		return s.forward(tea.MouseReleaseMsg(mouse))
	}
	return s.forward(msg)
}

func (s *staged) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	return s, cmd
}

// local moves a mouse event into card coordinates and reports whether it is
// inside the card. Before the card is known every event counts as inside.
func (s *staged) local(m tea.Mouse) (tea.Mouse, bool) {
	if s.rect.Empty() {
		return m, true
	}
	in := s.rect.Contains(m.X, m.Y)
	m.X -= s.rect.X
	m.Y -= s.rect.Y
	return m, in
}

func (s *staged) View() tea.View {
	v := s.Model.View()
	switch {
	case s.backdrop != nil && !s.win.Empty():
		v.Content = s.overBackdrop(v.Content)
	case s.rect.X == 0 && s.rect.Y == 0:
		v.Content = seeThrough(v.Content)
		return v
	default:
		v.Content = place(seeThrough(v.Content), s.rect.X, s.rect.Y)
	}
	if v.Cursor != nil {
		c := *v.Cursor
		c.X += s.rect.X
		c.Y += s.rect.Y
		v.Cursor = &c
	}
	if on := v.OnMouse; on != nil {
		v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
			mouse, _ := s.local(msg.Mouse())
			switch msg.(type) {
			case tea.MouseClickMsg:
				return on(tea.MouseClickMsg(mouse))
			case tea.MouseReleaseMsg:
				return on(tea.MouseReleaseMsg(mouse))
			case tea.MouseWheelMsg:
				return on(tea.MouseWheelMsg(mouse))
			case tea.MouseMotionMsg:
				return on(tea.MouseMotionMsg(mouse))
			}
			return on(msg)
		}
	}
	return v
}

// overBackdrop draws content at the card's place over the backdrop. Cells
// the card leaves ui.Clear (its rounded corners and edges) keep the
// backdrop's colour: a blank one shows the backdrop's cell whole, an edge
// glyph is drawn on the backdrop's background.
func (s *staged) overBackdrop(content string) string {
	buf := uv.NewScreenBuffer(s.win.W, s.win.H)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(s.backdrop(s.win.W, s.win.H, ui.CardSilhouette(s.rect))).Draw(buf, buf.Bounds())
	if content == "" {
		return buf.Render()
	}
	area := uv.Rect(s.rect.X, s.rect.Y, s.rect.W, s.rect.H)
	under := buf.CloneArea(area)
	uv.NewStyledString(content).Draw(buf, area)
	for y := range s.rect.H {
		for x := range s.rect.W {
			c := buf.CellAt(s.rect.X+x, s.rect.Y+y)
			if c == nil || !ui.IsClear(c.Style.Bg) {
				continue
			}
			b := under.CellAt(x, y)
			if b == nil {
				c.Style.Bg = nil
				continue
			}
			if c.Content == " " || c.Content == "" {
				buf.SetCell(s.rect.X+x, s.rect.Y+y, b.Clone())
				continue
			}
			c.Style.Bg = b.Style.Bg
		}
	}
	return buf.Render()
}

// clearSGR is how ui.Clear appears in a rendered view.
const clearSGR = "48;2;1;2;3"

// seeThrough turns ui.Clear back into the default background, which Rex
// draws see-through, when there is no backdrop to show there.
func seeThrough(content string) string { return strings.ReplaceAll(content, clearSGR, "49") }

// place moves content x cells right and y rows down. The cells it adds have
// no attributes, so they keep the default background and stay see-through.
func place(content string, x, y int) string {
	if content == "" {
		return ""
	}
	pad := strings.Repeat(" ", x)
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Repeat("\n", y) + strings.Join(lines, "\n")
}
