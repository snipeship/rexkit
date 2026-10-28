package hints

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/panes"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "typed", "open", "miss", "copied", "paste", "paste-typed", "pasted", "agent", "nothing"}

// demoPanes are the window the snapshots show: the shell on the left,
// the demo's Neovim top right and a coding agent's session in the same repo
// bottom right.
func demoPanes(shell func(cols, rows int) string) []panes.DemoPane {
	d := panes.DemoPanes()
	editor := d[0]
	editor.Rect = d[1].Rect
	return []panes.DemoPane{
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
	// pasted: the exited container's id, on the pane's last rows at any
	// size, after `docker start ` typed at the prompt.
	const started, id = "docker start ", "1c2d3e4f5a6b"
	switch name {
	case "agent":
		pane = 2
	case "nothing":
		shell = demoQuiet
	case "pasted":
		shell = func(cols, rows int) string { return demoShellAt(started, cols, rows) }
	}
	window := demoPanes(shell)
	src := demoSource(window[pane].Draw)
	m := newModel(options{demo: true}, ui.FixedScheme(dark), func() tea.Msg { return loadedMsg{src: src} }, nil)
	m.rex, m.theme = rt, ui.FromRex(rt)
	var toast ui.Toast
	s := snap.Scene{Model: m, Title: "Hints"}
	// pick types the label of text, which has to be on the pane: a state
	// that picks nothing would pass for one that did.
	pick := func(text string) func(*snap.Driver) {
		return func(d *snap.Driver) {
			for _, mt := range m.matches {
				if mt.Text == text {
					d.Type(mt.Label)
					return
				}
			}
			panic(fmt.Sprintf("hints: %q is not on the demo pane at %dx%d", text, m.w, m.h))
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
	case "paste":
		// ⇥: a label now types its text into the pane
		s.Script = func(d *snap.Driver) { d.Key("tab") }
	case "paste-typed":
		s.Script = func(d *snap.Driver) { d.Key("tab"); d.Type("y") }
	case "pasted":
		feed := pick(id)
		s.Script = func(d *snap.Driver) {
			d.Key("tab")
			feed(d)
			// The pane under the flash as the paste left it: the id at the
			// prompt, with no ⏎.
			rows, cols := len(m.cells), len(m.cells[0])
			m.cells = panes.Cells(demoShellAt(started+id, cols, rows), cols, rows, m.rex.Terminal)
		}
		toast = ui.Toast{Kind: ui.ToastOK, Text: "Pasted " + id}
	case "nothing":
		toast = ui.Toast{Kind: ui.ToastInfo, Text: nothingText}
	default:
		return snap.Scene{}, fmt.Errorf("hints has no state %q (try %v)", name, SnapStates)
	}
	s.Panes, s.Over, s.Toast = window, pane, toast
	return s, nil
}
