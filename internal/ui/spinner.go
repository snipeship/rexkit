package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// SpinnerDelay is how long loading must last before a spinner appears, so
// fast loads never flash one.
const SpinnerDelay = 120 * time.Millisecond

// SpinnerTickMsg advances spinners; every spinner derives its frame from the
// clock, so one tick serves all of them.
type SpinnerTickMsg time.Time

// TickSpinner schedules the next spinner frame.
func TickSpinner() tea.Cmd {
	return tea.Tick(SpinnerInterval, func(t time.Time) tea.Msg { return SpinnerTickMsg(t) })
}

// Spinner tracks one loading state and owns the tick chain that animates it,
// so overlapping loads (or a Stop and Start within one frame) never run more
// than one chain:
//
//	cmd := m.spinner.Start(now)               // nil when a tick is in flight
//	case ui.SpinnerTickMsg: return m, m.spinner.Tick()
type Spinner struct {
	since  time.Time
	active bool

	inflight int  // ticks scheduled and not yet handed to Tick
	owned    bool // the model drives the chain through Tick
}

// Start marks loading as begun (a no-op while already loading) and returns
// the tick that drives the animation, or nil when one is already in flight.
// (Until the model has called Tick once, Start always returns a tick, for
// models that re-arm with TickSpinner themselves.)
func (s *Spinner) Start(now time.Time) tea.Cmd {
	if !s.active {
		s.since, s.active = now, true
	}
	if s.owned && s.inflight > 0 {
		return nil
	}
	s.inflight++
	return TickSpinner()
}

// Tick handles a SpinnerTickMsg: it returns the next tick while loading, and
// ends the chain (nil) once stopped. Of several chains in flight (started
// before the first Tick) only the last to arrive continues, so they merge.
func (s *Spinner) Tick() tea.Cmd {
	s.owned = true
	if s.inflight > 0 {
		s.inflight--
	}
	if !s.active || s.inflight > 0 {
		return nil
	}
	s.inflight++
	return TickSpinner()
}

// Stop marks loading as done. A tick in flight still arrives; Tick then ends
// the chain (or carries on, if loading started again meanwhile).
func (s *Spinner) Stop() { s.active = false }

// Active reports whether loading is in progress (shown or not).
func (s *Spinner) Active() bool { return s.active }

// Visible reports whether the spinner should be drawn: loading for at least
// SpinnerDelay.
func (s *Spinner) Visible(now time.Time) bool {
	return s.active && now.Sub(s.since) >= SpinnerDelay
}

// View returns the spinner glyph, or nothing while it is not visible.
func (s *Spinner) View(t Theme, now time.Time) Line {
	if !s.Visible(now) {
		return nil
	}
	return Line{{SpinnerFrame(now), t.OK}}
}
