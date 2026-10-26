package rename

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "edited", "cleared", "error"}

// The dialog's size, as the Lua layer opens it.
const (
	snapCols = 56
	snapRows = 9
)

// Snap returns the rename dialog for a pane in a named state, for
// `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	target := Target{Kind: "pane", ID: "block:01a1763b-f56c-72ce-b5e2-fb233bccb71a"}
	var apply func(string) tea.Cmd
	var script func(*snap.Driver)
	switch name {
	case "default":
		script = func(*snap.Driver) {}
	case "edited":
		script = func(d *snap.Driver) {
			d.Key("right")
			d.Type("-2")
		}
	case "cleared":
		script = func(d *snap.Driver) { d.Key("ctrl+u") }
	case "error":
		apply = func(string) tea.Cmd {
			return func() tea.Msg { return savedMsg{problem: problem(target.Kind, rex.ErrNotFound)} }
		}
		script = func(d *snap.Driver) {
			d.Key("right")
			d.Type("-2")
			d.Key("enter")
		}
	default:
		return snap.Scene{}, fmt.Errorf("rename has no state %q (try %v)", name, SnapStates)
	}
	m := newModel(options{target: target}, "api-server", apply, nil, nil)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	return snap.Scene{Model: m, Script: script, Cols: snapCols, Rows: snapRows}, nil
}
