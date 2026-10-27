package attend

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui/sanitize"
)

// DoneFor is how long a result nobody looked at keeps needing the user.
const DoneFor = 24 * time.Hour

// Unseen reports whether a pane in state since its time since is news the
// user has not looked at (seen is when they last did): blocked, failed or
// done, and not looked at since. A result is news for DoneFor.
func Unseen(st string, since, seen, now time.Time) bool {
	switch st {
	case rex.StateBlocked, rex.StateError:
	case rex.StateDone:
		if now.Sub(since) >= DoneFor {
			return false
		}
	default:
		return false
	}
	return seen.Before(since)
}

// NeedsYou reports whether a pane in state needs the user: a blocked or
// failed agent does until it goes on, seen or not; a result only while it
// is unseen.
func NeedsYou(st string, unseen bool) bool {
	return st == rex.StateBlocked || st == rex.StateError || st == rex.StateDone && unseen
}

// Memory is what rexkit remembers about panes the user looked at: the
// attention state (the watcher's and the tools' marks) and the visit
// history (every jump is a look).
type Memory struct {
	Attention state.Attention
	History   state.History
}

// Recall loads the memory; what cannot be read is empty.
func Recall(d state.Dirs) Memory {
	h, _ := d.History()
	return Memory{Attention: d.Attention(), History: h}
}

// Since is when a pane entered state and kind: as the watcher saw it, or
// else updated, the time of its status record.
func (m Memory) Since(block, st, kind string, updated time.Time) time.Time {
	return m.Attention.Since(block, st, kind, updated)
}

// Seen is when the user last looked at a pane, or zero.
func (m Memory) Seen(block string) time.Time {
	a, h := m.Attention.SeenAt(block), m.History.SeenAt(block)
	if h.After(a) {
		return h
	}
	return a
}

// Pane is an agent pane that reports its state.
type Pane struct {
	Session, Window, Block string
	Where                  string // "session › tab", sanitized, for toasts
	App                    string
	State, Kind            string // OSC 7501 state and, when blocked, why
	Msg                    string // what it says, first line, sanitized
	Since                  time.Time
	Seen                   time.Time
}

// Unseen reports news the user has not looked at.
func (p Pane) Unseen(now time.Time) bool { return Unseen(p.State, p.Since, p.Seen, now) }

// NeedsYou reports whether the pane needs the user.
func (p Pane) NeedsYou(now time.Time) bool { return NeedsYou(p.State, p.Unseen(now)) }

// Rank orders what needs the user: an agent that cannot go on (blocked)
// first, then one that stopped (failed), then a result to read (done).
func Rank(st string) int {
	switch st {
	case rex.StateBlocked:
		return 0
	case rex.StateError:
		return 1
	case rex.StateDone:
		return 2
	}
	return 3
}

// Panes lists the agent panes of an inventory that report a state, in the
// server's session order, tab by tab: rexkit's own tools are left out.
func Panes(inv rex.Inventory, mem Memory, now time.Time) []Pane {
	sessions := slices.Clone([]rex.SessionView(inv.Sessions))
	slices.SortStableFunc(sessions, func(a, b rex.SessionView) int { return cmp.Compare(a.Order, b.Order) })
	var out []Pane
	for _, s := range sessions {
		session := sanitize.Label(s.Label)
		if session == "" {
			session = "Session"
		}
		for n, w := range s.Windows {
			for _, l := range w.Layers {
				for _, b := range l.Blocks {
					if strings.HasPrefix(b.Label, ToolLabelPrefix) || b.Terminal == nil {
						continue
					}
					a := Read(b.Terminal, now)
					if a.App == "" || a.Since.IsZero() || a.Guessed {
						continue
					}
					p := Pane{Session: s.SessionID, Window: w.ID, Block: b.ID,
						Where: session + " › " + tabName(w.Label, b.Terminal, n+1),
						App:   sanitize.Label(a.App), State: a.State, Kind: a.Kind, Msg: sanitize.Label(a.Msg)}
					p.Since = mem.Since(b.ID, a.State, a.Kind, a.Since)
					p.Seen = mem.Seen(b.ID)
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// ToolLabelPrefix labels rexkit's own floats (Go to, toasts…), which are
// never agents.
const ToolLabelPrefix = "rexkit:"

// defaultWindowLabel is the label Rex gives a tab nobody named.
var defaultWindowLabel = regexp.MustCompile(`^Window [0-9a-f]{8}$`)

// tabName names a tab for a toast: its label, else the agent's folder,
// else its number.
func tabName(label string, t *rex.Terminal, number int) string {
	if label = sanitize.Label(label); label != "" && !defaultWindowLabel.MatchString(label) {
		return label
	}
	if t.Process != nil {
		if p := t.Process.Current(); p != nil && p.Cwd != "" && p.Cwd != "/" {
			return sanitize.Label(filepath.Base(p.Cwd))
		}
	}
	return "Tab " + strconv.Itoa(number)
}

// Waiting lists the panes that need the user, in ⌥A's order: blocked,
// failed, done; in each, whoever has waited longest first.
func Waiting(panes []Pane, now time.Time) []Pane {
	var out []Pane
	for _, p := range panes {
		if p.NeedsYou(now) {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b Pane) int {
		if c := cmp.Compare(Rank(a.State), Rank(b.State)); c != 0 {
			return c
		}
		return a.Since.Compare(b.Since)
	})
	return out
}

// Next is where ⌥A goes from the pane at block: the first pane that needs
// the user and that they have not seen since it began to, in Waiting's
// order; when they have seen them all, the one after block in that order,
// round the end. It is never block itself.
func Next(panes []Pane, block string, now time.Time) (Pane, bool) {
	list := Waiting(panes, now)
	for _, p := range list {
		if p.Block != block && p.Unseen(now) {
			return p, true
		}
	}
	at := slices.IndexFunc(list, func(p Pane) bool { return p.Block == block })
	for step := 1; step <= len(list); step++ {
		if p := list[(at+step)%len(list)]; p.Block != block {
			return p, true
		}
	}
	return Pane{}, false
}
