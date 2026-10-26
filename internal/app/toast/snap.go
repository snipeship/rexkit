package toast

import (
	"fmt"

	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show: one per kind of news.
var SnapStates = []string{"ok", "info", "warn", "err"}

// snapToasts are the sample messages of Snap, by state.
var snapToasts = map[string]struct {
	kind ui.ToastKind
	text string
}{
	"ok":   {ui.ToastOK, "Renamed the pane to api-server"},
	"info": {ui.ToastInfo, "Parked api in Scratch · {key:ctrl+p e} brings it back"},
	"warn": {ui.ToastWarn, "zoxide not found · projects are hidden"},
	"err":  {ui.ToastErr, "Could not open the editor: nvim exited with status 1"},
}

// Snap returns a toast of the named kind, for `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	t, ok := snapToasts[name]
	if !ok {
		return snap.Scene{}, fmt.Errorf("toast has no state %q (try %v)", name, SnapStates)
	}
	m := newModel(t.kind, t.text, 0)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	return snap.Scene{Model: m, Script: func(*snap.Driver) {}, Rows: Rows}, nil
}
