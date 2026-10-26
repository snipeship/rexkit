package switcher

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{"default", "search", "drill", "help", "empty", "narrow", "loading", "error", "busy", "failed",
	"rename", "close", "new"}

// snapNow is the demo world's clock in snapshots, so ages never change.
var snapNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// Snap returns the switcher on the demo world in a named state, for
// `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	fake, origin, history := demoWorld(snapNow)
	var api rex.API = fake
	at := snapNow // scripts move it on, so spinners have waited long enough to show
	// Timers are held, like the tests do, and fired around the script: a
	// preview debounce that fired before the first WindowSizeMsg would find
	// no preview to fill.
	var timers []tea.Msg
	e := env{
		close: func(context.Context) error { return nil },
		now:   func() time.Time { return at },
		after: func(_ time.Duration, msg tea.Msg) tea.Cmd {
			switch msg.(type) {
			case clockMsg, tailMsg, pollMsg: // endless: the frame needs none of them
			default:
				timers = append(timers, msg)
			}
			return nil
		},
		demo: true,
	}
	fire := func(d *snap.Driver) {
		for range 10 { // a timer may set another; none sets them forever
			held := timers
			timers = nil
			d.Send(held...)
		}
	}
	switch name {
	case "loading":
		api = stalledRex{fake}
	case "error":
		fake.Fail("snapshot", &rex.Error{Op: "snapshot", Message: "the Rex server is not running"})
	}
	e.api = api
	m := newModel(context.Background(), e, options{origin: origin, home: demoHome}, nil, history)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	s := snap.Scene{Model: m}
	switch name {
	case "default":
	case "search":
		s.Script = func(d *snap.Driver) { d.Type("nav") }
	case "drill":
		s.Script = func(d *snap.Driver) { selectNamed(m, d, "studio"); d.Key("right") }
	case "help":
		s.Script = func(d *snap.Driver) { d.Key("?") }
	case "empty":
		s.Script = func(d *snap.Driver) { d.Type("zqxj") }
	case "narrow":
		s.W, s.H = 70, 24
	case "loading", "error":
		s.Script = func(*snap.Driver) { at = at.Add(time.Second) }
	case "busy":
		s.Script = func(*snap.Driver) {
			m.busy = true // a jump in flight, as act starts it
			m.going.Start(at)
			at = at.Add(time.Second)
		}
	case "failed":
		s.Script = func(d *snap.Driver) {
			d.Key("down")
			d.Send(doneMsg{err: fmt.Errorf("focus: %w", rex.ErrNotFound), what: "jump", subject: "pane"})
		}
	case "rename":
		s.Script = func(d *snap.Driver) { selectNamed(m, d, "migrations"); d.Key("ctrl+e") }
	case "close":
		s.Script = func(d *snap.Driver) { selectNamed(m, d, "studio"); d.Key("ctrl+x") }
	case "new":
		s.Script = func(d *snap.Driver) { d.Key("ctrl+a"); d.Type("demo") }
	default:
		return snap.Scene{}, fmt.Errorf("goto has no state %q (try %v)", name, SnapStates)
	}
	script := s.Script
	s.Script = func(d *snap.Driver) {
		fire(d)
		if script != nil {
			script(d)
		}
		fire(d)
	}
	return s, nil
}

// stalledRex is a Rex that never answers a snapshot, for the loading state.
type stalledRex struct{ *rexfake.Fake }

func (stalledRex) Snapshot(ctx context.Context) (rex.Inventory, error) {
	<-ctx.Done()
	return rex.Inventory{}, ctx.Err()
}

// selectNamed selects the first row named name.
func selectNamed(m *model, d *snap.Driver, name string) {
	for i, r := range m.listing.rows {
		if m.cat.entries[r.entry].name == name {
			m.list.Select(i)
			d.Run(m.selectionChanged())
			return
		}
	}
}
