package hold

import (
	"fmt"
	"time"

	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "blocked", "closed"}

// Snap returns the Scratch placeholder of a parked pane in a named state,
// for `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	card := demoCard(Card{}, nil)
	card.Since = now.Add(-3 * time.Minute)
	m := newModel(card, nil, false)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	m.now = func() time.Time { return now }
	switch name {
	case "default":
		m.status = parked{known: true, state: ui.StatusWorking, app: "claude"}
	case "blocked":
		m.status = parked{known: true, state: ui.StatusBlocked, app: "claude"}
	case "closed":
		m.status = parked{known: true, gone: true}
	default:
		return snap.Scene{}, fmt.Errorf("hold has no state %q (try %v)", name, SnapStates)
	}
	return snap.Scene{Model: m, Script: func(*snap.Driver) {}}, nil
}
