package state

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// Attention bounds.
const (
	// MaxMarks bounds the panes remembered; the least recently touched go.
	MaxMarks = 500
	// MarkMaxAge is how long a pane nobody touched is remembered.
	MarkMaxAge = 7 * 24 * time.Hour
)

// Attention is what rexkit remembers about the agents that need the user
// (internal/attend): when each pane entered the state it is in, when the
// user last looked at it, and where ⌥A walks from and back to.
type Attention struct {
	Panes map[string]Mark `json:"panes,omitempty"` // by block ID
	// Back is where the user was when a walk of ⌥A presses began: ⌥⇧A
	// goes there. Landed is where the last ⌥A or ⌥⇧A went; a press from
	// anywhere else begins a new walk.
	Back   *Visit `json:"back,omitempty"`
	Landed *Visit `json:"landed,omitempty"`
}

// Mark is one pane's attention state.
type Mark struct {
	State string    `json:"state,omitempty"` // OSC 7501 state it was last seen in
	Kind  string    `json:"kind,omitempty"`  // and its kind (blocked: permission, question…)
	Since time.Time `json:"since,omitempty"` // when it entered State and Kind
	Seen  time.Time `json:"seen,omitempty"`  // when the user last looked at it
	At    time.Time `json:"at"`              // when the mark last changed
}

// Enter records that a pane is in state and kind, as of at. It reports
// whether that is news: a change of state or kind, from which the pane's
// Since counts.
func (a *Attention) Enter(block, state, kind string, at time.Time) bool {
	m := a.Panes[block]
	if m.State == state && m.Kind == kind && !m.Since.IsZero() {
		return false
	}
	m.State, m.Kind, m.Since, m.At = state, kind, at, at
	a.set(block, m)
	return true
}

// See records that the user looked at a pane at time at. It reports
// whether that is later than the last look.
func (a *Attention) See(block string, at time.Time) bool {
	m := a.Panes[block]
	if !at.After(m.Seen) {
		return false
	}
	m.Seen, m.At = at, at
	a.set(block, m)
	return true
}

func (a *Attention) set(block string, m Mark) {
	if a.Panes == nil {
		a.Panes = map[string]Mark{}
	}
	a.Panes[block] = m
}

// Since is when a pane entered state and kind: the watcher's record of it
// when it saw the change, else fallback (the status record's update time).
func (a Attention) Since(block, state, kind string, fallback time.Time) time.Time {
	if m, ok := a.Panes[block]; ok && m.State == state && m.Kind == kind && !m.Since.IsZero() {
		return m.Since
	}
	return fallback
}

// SeenAt is when the user last looked at a pane, or zero.
func (a Attention) SeenAt(block string) time.Time { return a.Panes[block].Seen }

// Forget drops the panes keep rejects (closed ones) and walk places in
// them. It reports whether it dropped anything.
func (a *Attention) Forget(keep func(block string) bool) bool {
	n := len(a.Panes)
	maps.DeleteFunc(a.Panes, func(block string, _ Mark) bool { return !keep(block) })
	dropped := len(a.Panes) < n
	if a.Back != nil && a.Back.BlockID != "" && !keep(a.Back.BlockID) {
		a.Back, dropped = nil, true
	}
	if a.Landed != nil && a.Landed.BlockID != "" && !keep(a.Landed.BlockID) {
		a.Landed, dropped = nil, true
	}
	return dropped
}

// prune bounds the marks: none untouched for MarkMaxAge, at most MaxMarks.
func (a *Attention) prune(now time.Time) {
	maps.DeleteFunc(a.Panes, func(_ string, m Mark) bool { return now.Sub(m.At) > MarkMaxAge })
	if len(a.Panes) <= MaxMarks {
		return
	}
	blocks := slices.SortedFunc(maps.Keys(a.Panes), func(x, y string) int {
		return a.Panes[y].At.Compare(a.Panes[x].At)
	})
	for _, b := range blocks[MaxMarks:] {
		delete(a.Panes, b)
	}
}

const attentionFile = "attention.json"

// Attention loads the attention state; a missing or corrupt file is an
// empty one (it only ever costs a toast or a "seen").
func (d Dirs) Attention() Attention {
	var a Attention
	if err := ReadJSON(filepath.Join(d.State, attentionFile), &a); err != nil {
		return Attention{}
	}
	return a
}

// UpdateAttention applies fn to the stored attention state under an
// exclusive lock, so the watcher and the tools never lose each other's
// updates. now bounds what is kept.
func (d Dirs) UpdateAttention(now time.Time, fn func(*Attention)) error {
	unlock, err := d.lock(attentionFile)
	if err != nil {
		return err
	}
	defer unlock()
	a := d.Attention()
	fn(&a)
	a.prune(now)
	return WriteJSON(filepath.Join(d.State, attentionFile), a)
}

// lock takes the exclusive lock that guards the state file name.
func (d Dirs) lock(name string) (unlock func(), err error) {
	if err := os.MkdirAll(d.State, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d.State, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
