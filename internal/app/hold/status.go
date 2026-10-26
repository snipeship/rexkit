package hold

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// pollEvery is how often the parked pane is looked at after a change;
// while nothing changes the wait doubles up to maxPollEvery, since the hold
// screen sits in the background for hours.
const (
	pollEvery    = 5 * time.Second
	maxPollEvery = 60 * time.Second
)

// firstPoll waits for the first frame before touching the network.
const firstPoll = 200 * time.Millisecond

// parked is what the hold screen knows about the parked pane.
type parked struct {
	known   bool
	gone    bool
	state   ui.Status
	app     string // program that reports the status ("claude"), or ""
	running string // foreground program when it is not the shell, or ""
}

// statusMsg is the result of one look at the parked pane. err is set when
// Rex could not tell (the last known state stays).
type statusMsg struct {
	p   parked
	err error
}

// pollMsg asks for the next look.
type pollMsg struct{}

func pollAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return pollMsg{} })
}

// watcher looks at one block.
type watcher func(ctx context.Context) statusMsg

// watch returns a watcher for block in session.
func watch(api rex.API, session, block string) watcher {
	return func(ctx context.Context) statusMsg {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		proc, err := api.Process(ctx, session, block)
		if errors.Is(err, rex.ErrNotFound) {
			return statusMsg{p: parked{known: true, gone: true}}
		}
		if err != nil {
			return statusMsg{err: err}
		}
		records, err := api.ProgramStatus(ctx, session, block)
		if err != nil && !errors.Is(err, rex.ErrNotFound) {
			return statusMsg{err: err}
		}
		return statusMsg{p: describe(proc, records)}
	}
}

// describe sums up a terminal: the most urgent program status record, or
// else whether something other than the shell is running.
func describe(proc rex.Process, records []rex.StatusRecord) parked {
	p := parked{known: true, state: ui.StatusNone}
	best := 0
	for _, r := range records {
		s, rank := classify(r.State)
		if rank > best {
			best, p.state = rank, s
			p.app = sanitize.Label(firstNonEmpty(r.EffectiveApp, r.App))
		}
	}
	if fg := proc.Foreground; fg != nil && (proc.Child == nil || fg.PID != proc.Child.PID) {
		p.running = sanitize.Label(fg.Name)
	}
	return p
}

// classify maps an OSC 7501 state to a status and its urgency.
func classify(state string) (ui.Status, int) {
	switch state {
	case rex.StateBlocked:
		return ui.StatusBlocked, 4
	case rex.StateError:
		return ui.StatusFailed, 3
	case rex.StateWorking:
		return ui.StatusWorking, 2
	case rex.StateIdle, rex.StateDone:
		return ui.StatusIdle, 1
	}
	return ui.StatusNone, 0
}

// line renders the parked pane's state. Blocked, failed and working are the
// one status tag on the card ("▐● claude needs you▌"); the rest is plain
// text ("○ claude idle", "› running nvim", "◌ closed").
func (p parked) line(t ui.Theme, now time.Time) ui.Line {
	switch {
	case !p.known:
		return nil
	case p.gone:
		return ui.Line{t.StatusGlyph(ui.StatusExited, now), {Text: " closed", Style: t.Subtle}}
	case p.state != ui.StatusNone:
		word := func(s ui.Status) string {
			w := s.Word()
			if s == ui.StatusBlocked {
				w = "needs you"
			}
			return strings.TrimSpace(p.app + " " + w)
		}
		tag := t.Tag(p.state, word(p.state), ambient(now))
		// Padded to the widest tag, so the card keeps its size (and place)
		// when the state changes.
		widest := t.Tag(ui.StatusBlocked, word(ui.StatusBlocked), now).Width()
		return tag.Append(ui.Space(max(0, widest-tag.Width())))
	case p.running != "":
		return ui.Line{{Text: ui.GlyphPane, Style: t.Subtle}, {Text: " running ", Style: t.Subtle}, {Text: p.running, Style: t.Muted}}
	}
	return ui.Line{t.StatusGlyph(ui.StatusIdle, now), {Text: " idle", Style: t.Subtle}}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
