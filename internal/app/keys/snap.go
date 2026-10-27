package keys

import (
	"fmt"

	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "filter", "agents", "help", "empty"}

// Snap returns the key guide on the demo keymap in a named state, for
// `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	m := newModel(options{demo: true}, Build(demoKeymap()), nil, nil)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	s := snap.Scene{Model: m, Script: func(*snap.Driver) {}}
	switch name {
	case "default":
	case "filter":
		s.Script = func(d *snap.Driver) { d.Type("split") }
	case "agents": // ⌃⇧A and ⌃⌥⇧A beside ⌥A and ⌥⇧A, and Session mode's a and ⇧A
		s.Script = func(d *snap.Driver) { d.Type("agent") }
	case "help":
		s.Script = func(d *snap.Driver) { d.Key("?") }
	case "empty":
		s.Script = func(d *snap.Driver) { d.Type("zqxj") }
	default:
		return snap.Scene{}, fmt.Errorf("keys has no state %q (try %v)", name, SnapStates)
	}
	return s, nil
}
