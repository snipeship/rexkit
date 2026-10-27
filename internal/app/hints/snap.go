package hints

import (
	"fmt"
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/backdrop"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "typed", "open", "miss", "copied", "agent", "nothing"}

// demoPanes are the window the snapshots show: the shell on the left,
// the demo's Neovim top right and a coding agent's session in the same repo
// bottom right.
func demoPanes(shell func(cols, rows int) string) []backdrop.DemoPane {
	d := backdrop.DemoPanes()
	editor := d[0]
	editor.Rect = d[1].Rect
	return []backdrop.DemoPane{
		{Rect: rex.Rect{0, 0, 0.5, 1}, Draw: shell, Program: "zsh", Title: "~/code/studio/api"},
		editor,
		{Rect: d[2].Rect, Draw: demoAgent, Program: "claude", Title: "fix the drain test"},
	}
}

// Snap returns hints over the demo shell in a named state, for `rexkit snap`
// ("agent": over the agent's pane instead; "nothing": a shell with
// nothing to pick, where the tool only toasts).
func Snap(dark bool, name string) (snap.Scene, error) {
	rt := snap.Theme(dark)
	pane, shell := 0, demoShell
	switch name {
	case "agent":
		pane = 2
	case "nothing":
		shell = demoQuiet
	}
	panes := demoPanes(shell)
	src := demoSource(panes[pane].Draw)
	m := newModel(options{demo: true}, ui.FixedScheme(dark), func() tea.Msg { return loadedMsg{src: src} }, nil)
	m.rex, m.theme = rt, ui.FromRex(rt)
	var toast ui.Toast
	s := snap.Scene{Model: m}
	pick := func(text string) func(*snap.Driver) {
		return func(d *snap.Driver) {
			for _, mt := range m.matches {
				if mt.Text == text {
					d.Type(mt.Label)
					return
				}
			}
		}
	}
	switch name {
	case "default", "agent":
	case "typed":
		s.Script = func(d *snap.Driver) { d.Type("y") }
	case "open":
		// ⇧ on the first key of a two-key label: it will open, not copy.
		s.Script = func(d *snap.Driver) { d.Type("Y") }
	case "miss":
		// a key no label starts (labels are prefix-free, so after "y" only
		// the pairs y… are left, and "yy" is not one of them)
		s.Script = func(d *snap.Driver) { d.Type("yy") }
	case "copied":
		s.Script = pick("a3f9c21")
		toast = ui.Toast{Kind: ui.ToastOK, Text: "Copied a3f9c21"}
	case "nothing":
		toast = ui.Toast{Kind: ui.ToastInfo, Text: nothingText}
	default:
		return snap.Scene{}, fmt.Errorf("hints has no state %q (try %v)", name, SnapStates)
	}
	s.Window = func(w, h int) (string, image.Rectangle) {
		screens := backdrop.DemoScreens(panes, w, h)
		panes := backdrop.New(rt, screens).Frame(w, h, ui.Silhouette{}, 0)
		if toast.Text != "" {
			panes = withToast(panes, ui.FromRex(rt), toast, w, h)
		}
		p := screens.Panes[pane]
		r := backdrop.Place(p.Rect, p.Size.Columns, p.Size.Rows, w, h)
		return panes, image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	}
	return s, nil
}

// withToast draws the toast Lua shows once hints closes (app.notify: the
// window's top-right corner) over the window.
func withToast(window string, t ui.Theme, to ui.Toast, w, h int) string {
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(window).Draw(buf, buf.Bounds())
	for y, row := range t.ToastStrip(to, w, ui.ToastRows+1, ui.ToastRows) {
		if strings.TrimSpace(ansi.Strip(row)) != "" {
			over(&buf, row, 0, y, w)
		}
	}
	return buf.Render()
}
