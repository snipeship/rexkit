package switcher

import (
	"context"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

const (
	// collectTimeout bounds one snapshot.
	collectTimeout = 5 * time.Second
	// refreshDebounce gathers a burst of change events into one snapshot.
	refreshDebounce = 120 * time.Millisecond
	// minRefreshGap is the least time between the end of one event-driven
	// snapshot and the start of the next. A pane that animates its title, or
	// an agent streaming status, sends events many times a second; each
	// snapshot is a burst of RPCs on the user's server.
	minRefreshGap = time.Second
	// pollInterval refreshes while no watcher reports changes.
	pollInterval = 2 * time.Second
	// cacheMaxAge rewrites an unchanged snapshot cache now and then, so its
	// collected_at stays honest for the "From … ago" of a later failed open.
	cacheMaxAge = time.Minute
)

// refresher tracks snapshot collection.
type refresher struct {
	running  bool      // a snapshot is being collected
	again    bool      // collect once more when it finishes
	shown    bool      // …and show that one in the footer (⌃R while running)
	due      bool      // a debounced refresh is scheduled
	lastDone time.Time // when the last snapshot arrived
	// saved is the inventory the snapshot cache holds (nil: unknown), so an
	// unchanged snapshot is not written again.
	saved *rex.Inventory
}

type (
	inventoryMsg struct {
		inv   rex.Inventory
		err   error
		saved bool // inv was written to the snapshot cache
	}
	// watchMsg is a change event; ok is false when the watcher is gone.
	watchMsg struct {
		ev     rex.Event
		events <-chan rex.Event
		ok     bool
	}
	refreshDueMsg struct{}
	pollMsg       struct{}
)

// collect starts a snapshot in the background unless one is running. Only
// the first load (nothing to show yet) says so; later ones update the rows in
// place.
func (m *model) collect() tea.Cmd { return m.snapshot(m.cat == nil) }

// reload is ⌃R: a snapshot the user asked for, so the footer shows it working.
func (m *model) reload() tea.Cmd { return m.snapshot(true) }

func (m *model) snapshot(shown bool) tea.Cmd {
	if shown {
		m.loading.Start(m.now())
	}
	if m.refresh.running {
		m.refresh.again = true
		m.refresh.shown = m.refresh.shown || shown
		return m.clock.wake(m)
	}
	m.refresh.running = true
	api, save, ctx, saved := m.api, m.save, m.ctx, m.refresh.saved
	return tea.Batch(m.clock.wake(m), func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, collectTimeout)
		defer cancel()
		inv, err := api.Snapshot(ctx)
		msg := inventoryMsg{inv: inv, err: err}
		if err == nil && save != nil && cacheOutdated(inv, saved) {
			save(inv)
			msg.saved = true
		}
		return msg
	})
}

// cacheOutdated reports whether a fresh inventory should replace the cached
// one: it differs (collected_at aside), or the cached one is getting old.
func cacheOutdated(fresh rex.Inventory, saved *rex.Inventory) bool {
	if saved == nil || fresh.CollectedAt.Sub(saved.CollectedAt) >= cacheMaxAge {
		return true
	}
	old := *saved
	old.CollectedAt = fresh.CollectedAt
	return !reflect.DeepEqual(fresh, old)
}

func (m *model) inventoryArrived(msg inventoryMsg) tea.Cmd {
	m.refresh.running = false
	m.refresh.lastDone = m.now()
	m.loading.Stop()
	if msg.saved {
		inv := msg.inv
		m.refresh.saved = &inv
	}
	if msg.err != nil {
		m.stale = true
		reason := rex.UserMessage(msg.err)
		if m.cat == nil {
			m.loadErr = reason // the error page says it; no toast repeats it
			return tea.Batch(m.collectAgain(), m.goLive())
		}
		return tea.Batch(m.collectAgain(), m.toast.Show(ui.ToastErr, "Can't read Rex: "+reason, ui.Hint{Key: "ctrl+r", Label: "retry"}), m.goLive())
	}
	m.stale, m.loadErr = false, ""
	prev, first := m.selection(), m.listing.rows == nil
	inv := msg.inv
	m.cat, m.inv, m.updatedAt = buildCatalog(inv, m.catalogOptions()), &inv, inv.CollectedAt
	switch {
	case first:
		m.rebuild(selectDefault) // the first data: preselect the previous location
	case m.pending && !m.moved:
		// ⏎ waits on this snapshot and the row it would act on is only the
		// cached frame's ranking: rank the fresh data, so type-then-⏎ goes to
		// what the query names now. (Outside the switcher view selectDefault
		// is the top row.)
		m.rebuild(selectDefault)
	default:
		m.rebuildFrom(selectKeep, "", prev) // rows must not move under the cursor
	}
	return tea.Batch(m.collectAgain(), m.goLive())
}

// collectAgain starts the snapshot asked for while the last one ran.
func (m *model) collectAgain() tea.Cmd {
	if !m.refresh.again {
		return nil
	}
	shown := m.refresh.shown || m.cat == nil
	m.refresh.again, m.refresh.shown = false, false
	return m.snapshot(shown)
}

// goLive marks the catalog live and runs a ⏎ that was pressed before it was,
// so a query typed and committed at once goes to what it names, not to a
// row of the cached first frame (or to nothing).
func (m *model) goLive() tea.Cmd {
	m.live = true
	if !m.pending {
		return nil
	}
	commit := m.pendingCommit
	m.pending, m.pendingCommit = false, time.Time{}
	return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}, commit)
}

// startWatch subscribes to change events; without them it polls.
func (m *model) startWatch() tea.Cmd {
	api, ctx := m.api, m.ctx
	return func() tea.Msg {
		events, err := api.Watch(ctx)
		if err != nil {
			return watchMsg{}
		}
		return nextEvent(events)()
	}
}

func nextEvent(events <-chan rex.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		return watchMsg{ev: ev, events: events, ok: ok}
	}
}

func (m *model) watchEvent(msg watchMsg) tea.Cmd {
	if !msg.ok {
		return m.after(pollInterval, pollMsg{}) // the watcher died: poll instead
	}
	next := nextEvent(msg.events)
	if msg.ev.Type == rex.EventReady || msg.ev.BlockID == m.opts.self || m.refresh.due {
		return next
	}
	m.refresh.due = true
	delay := refreshDebounce
	if gap := m.refresh.lastDone.Add(minRefreshGap).Sub(m.now()); gap > delay {
		delay = gap // the trailing refresh still comes, just not sooner
	}
	return tea.Batch(next, m.after(delay, refreshDueMsg{}))
}

// clockMsg redraws for spinners and relative ages.
type clockMsg struct{ gen int }

// clock drives redraws: fast while something spins, once a second otherwise.
type clock struct {
	gen  int
	fast bool
}

func (c *clock) start(m *model) tea.Cmd {
	c.gen++
	c.fast = m.animating()
	return m.after(c.interval(), clockMsg{c.gen})
}

// wake switches a slow clock to fast when something starts spinning.
func (c *clock) wake(m *model) tea.Cmd {
	if c.fast || !m.animating() {
		return nil
	}
	return c.start(m)
}

func (c *clock) tick(m *model, msg clockMsg) tea.Cmd {
	if msg.gen != c.gen {
		return nil // superseded by a faster clock
	}
	return c.start(m)
}

func (c *clock) interval() time.Duration {
	if c.fast {
		return ui.SpinnerInterval
	}
	return time.Second
}

// animating reports whether anything on screen spins.
func (m *model) animating() bool {
	if m.loading.Active() || m.going.Active() || m.pv.spin.Active() || (m.ask != nil && m.ask.spin.Active()) {
		return true
	}
	if m.cat == nil {
		return false
	}
	for _, r := range m.listing.rows {
		e := m.cat.entries[r.entry]
		if e.status == ui.StatusWorking || e.working > 0 && e.failed+e.blocked == 0 {
			return true
		}
	}
	return false
}
