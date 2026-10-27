package search

import (
	"context"
	"fmt"
	"slices"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// unreadable is the demo Rex with one pane whose capture fails.
type unreadable struct {
	rex.API
	block string
}

func (u unreadable) Capture(ctx context.Context, sessionID, blockID string, opts rex.CaptureOptions) (string, error) {
	if blockID == u.block {
		return "", fmt.Errorf("capture %s: %w", blockID, context.DeadlineExceeded)
	}
	return u.API.Capture(ctx, sessionID, blockID, opts)
}

// SnapStates are the states Snap can show.
var SnapStates = []string{
	"start", "default", "pane", "narrowed", "folded", "scoped", "regex", "badregex", "scanning", "unreadable", "nomatch", "copied",
	"help", "narrow", "stacked",
}

// snapNow is the clock of snapshots.
var snapNow = time.Date(2026, 10, 26, 12, 40, 0, 0, time.UTC)

// Snap returns the tool on the demo world in a named state, for `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	if !slices.Contains(SnapStates, name) {
		return snap.Scene{}, fmt.Errorf("search has no state %q (try %v)", name, SnapStates)
	}
	fake, o := demoWorld(snapNow)
	cfg := config{api: fake, origin: o, home: demoHome, demo: true, now: func() time.Time { return snapNow }}
	blocks := map[string]string{} // label → block
	inv, _ := fake.Snapshot(context.Background())
	for _, s := range inv.Sessions {
		for _, w := range s.Windows {
			for _, l := range w.Layers {
				for _, b := range l.Blocks {
					blocks[b.Label] = b.ID
				}
			}
		}
	}
	switch name {
	case "scanning":
		// The biggest panes are still being read.
		cfg.slow = map[string]bool{}
		for _, label := range []string{"tail -f production.log", "kubectl", "fish", "aider"} {
			cfg.slow[blocks[label]] = true
		}
	case "unreadable":
		cfg.api = unreadable{fake, blocks["kubectl"]}
	}
	m := newModel(context.Background(), cfg)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	// The spinner has been turning for a while.
	m.spinner.Start(snapNow.Add(-time.Second))
	s := snap.Scene{Model: m, Script: func(*snap.Driver) {}}
	// default selects the go test panic in rexkit › spark › zsh.
	panic := func(d *snap.Driver) {
		d.Type("panic")
		d.Key("alt+down")
	}
	switch name {
	case "default":
		s.Script = panic
	case "narrowed":
		s.Script = func(d *snap.Driver) {
			d.Type(hotRequest)
			d.Key("down")
		}
	case "pane":
		// production.log's own row: the preview follows the end of the
		// pane, every panic on the find fill.
		s.Script = func(d *snap.Driver) {
			d.Type("panic")
			for range 4 {
				d.Key("alt+down")
			}
			d.Key("up")
			d.Key("up")
		}
	case "scoped":
		s.Script = func(d *snap.Driver) {
			d.Type("panic")
			d.Key("tab")
			d.Key("alt+down")
		}
	case "badregex":
		s.Script = func(d *snap.Driver) {
			d.Key("alt+r")
			d.Type(`status=(5\d\d`)
		}
	case "regex":
		s.Script = func(d *snap.Driver) {
			d.Key("alt+r")
			d.Type(`status=5\d\d`)
		}
	case "folded":
		// production.log's fold: ⏎ lists its earlier matches.
		s.Script = func(d *snap.Driver) {
			d.Type("panic")
			for range 4 {
				d.Key("alt+down")
			}
			d.Key("up")
		}
	case "scanning":
		s.Script = func(d *snap.Driver) { d.Type("panic") }
	case "unreadable":
		s.Script = panic
	case "nomatch":
		s.Script = func(d *snap.Driver) { d.Type("segfault") }
	case "copied":
		s.Script = func(d *snap.Driver) {
			panic(d)
			d.Key("ctrl+y")
		}
	case "help":
		s.Script = func(d *snap.Driver) {
			panic(d)
			d.Key("f1") // ? would go into the query
		}
	case "narrow":
		s.W, s.H = 76, 30
		s.Script = panic
	case "stacked":
		s.W, s.H = 112, 40
		s.Script = panic
	}
	return s, nil
}
