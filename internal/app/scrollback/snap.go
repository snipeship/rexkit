package scrollback

import (
	"fmt"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show of the screen shown while the
// scrollback loads (Neovim itself is not drawn).
var SnapStates = []string{"default", "error"}

// Snap returns the loading screen in a named state, for `rexkit snap`:
// "default" is the spinner of a slow load, "error" the card of a failed one.
func Snap(dark bool, name string) (snap.Scene, error) {
	m := &loader{scheme: ui.FixedScheme(dark)}
	switch name {
	case "default":
		m.spin.Start(time.Now().Add(-time.Second)) // slow enough to show the spinner
	case "error":
		m.err = fmt.Errorf("reading the pane: %w", rex.ErrNotFound)
	default:
		return snap.Scene{}, fmt.Errorf("scrollback-loader has no state %q (try %v)", name, SnapStates)
	}
	return snap.Scene{Model: m, Script: func(*snap.Driver) {}}, nil
}
