// Package rexfake is an in-memory Rex for unit tests. It models sessions,
// windows, tiled and floating layers, focus, zoom and terminal state closely
// enough to test tools against, records every call, and can inject errors.
package rexfake

import (
	"fmt"
	"slices"
	"sync"

	"rexkit/internal/rex"
)

// Call is one recorded API call.
type Call struct {
	Method string // API method name, such as session.close_layer
	Args   []string
}

// Fake implements rex.API in memory. Its zero value is not usable; use New.
type Fake struct {
	mu        sync.Mutex
	next      int
	sessions  []*session
	terminals map[string]*terminal
	fail      map[string]error
	calls     []Call

	// KeymapResult is returned by Keymap.
	KeymapResult rex.Keymap
	// RemoteControl makes ClientPerform succeed instead of returning ErrRefused.
	RemoteControl bool
	// Events, if set, is returned by Watch.
	Events chan rex.Event
}

type session struct {
	view rex.View
	// detached are blocks in no window: running, but shown nowhere.
	detached []rex.Block
	// restore remembers, per floating layer, the block focused before it opened.
	restore map[string]string
}

type terminal struct {
	state   rex.Terminal
	size    rex.Size
	written string
	capture string
}

// New returns an empty fake server.
func New() *Fake {
	return &Fake{terminals: map[string]*terminal{}, fail: map[string]error{}}
}

func (f *Fake) id(kind string) string {
	f.next++
	return fmt.Sprintf("%s:%04d", kind, f.next)
}

// AddSession creates a session with one window holding one shell block and
// returns their IDs.
func (f *Fake) AddSession(label string) (sessionID, windowID, blockID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &session{restore: map[string]string{}}
	s.view = rex.View{SessionID: f.id("session"), Label: label}
	f.sessions = append(f.sessions, s)
	w := f.addWindow(s, "", []string{"shell"}, true)
	return s.view.SessionID, w.ID, w.FocusedBlockID
}

// AddWindow adds a window whose tiled layer holds blocks with the given
// labels side by side, and returns the window and block IDs.
func (f *Fake) AddWindow(sessionID, label string, blockLabels ...string) (string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.session(sessionID)
	if s == nil {
		panic("rexfake: no session " + sessionID)
	}
	w := f.addWindow(s, label, blockLabels, false)
	var ids []string
	for _, b := range w.Layers[0].Blocks {
		ids = append(ids, b.ID)
	}
	return w.ID, ids
}

func (f *Fake) addWindow(s *session, label string, blockLabels []string, activate bool) *rex.Window {
	var blocks []rex.Block
	for _, l := range blockLabels {
		blocks = append(blocks, f.newBlock(l))
	}
	w := rex.Window{ID: f.id("window"), Label: label, FocusedBlockID: blocks[0].ID}
	if w.Label == "" {
		w.Label = "Window " + w.ID
	}
	w.Layers = rex.List[rex.Layer]{{ID: f.id("layer"), Kind: rex.LayerTiled, Bounds: rex.Rect{0, 0, 1, 1},
		Layout: row(blocks), Blocks: blocks}}
	s.view.Windows = append(s.view.Windows, w)
	if activate || s.view.ActiveWindowID == "" {
		s.view.ActiveWindowID = w.ID
	}
	s.view.Revision++
	return &s.view.Windows[len(s.view.Windows)-1]
}

func (f *Fake) newBlock(label string) rex.Block {
	b := rex.Block{ID: f.id("block"), Label: label, Creator: rex.TerminalCreator, Flavor: rex.ShellFlavor}
	f.terminals[b.ID] = &terminal{size: rex.Size{Columns: 80, Rows: 24}}
	return b
}

// Detach takes a block out of its window, the way session.detach_block
// does: its program runs on, in no tab, until it is moved back.
func (f *Fake) Detach(blockID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.sessions {
		for wi := range s.view.Windows {
			w := &s.view.Windows[wi]
			for li := range w.Layers {
				l := &w.Layers[li]
				bi := slices.IndexFunc(l.Blocks, func(b rex.Block) bool { return b.ID == blockID })
				if bi < 0 {
					continue
				}
				s.detached = append(s.detached, l.Blocks[bi])
				l.Blocks = slices.Delete(l.Blocks, bi, bi+1)
				l.Layout = remove(l.Layout, blockID)
				if w.FocusedBlockID == blockID {
					w.FocusedBlockID = ""
				}
				pruneEmpty(s)
				s.view.Revision++
				return
			}
		}
	}
	panic("rexfake: no block " + blockID)
}

// SetTerminal sets what Process, ProgramStatus, title and Snapshot report
// for a block.
func (f *Fake) SetTerminal(blockID string, t rex.Terminal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.term(blockID).state = t
}

// SetSize sets a block's terminal size.
func (f *Fake) SetSize(blockID string, size rex.Size) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.term(blockID).size = size
}

// SetCapture sets what Capture returns for a block.
func (f *Fake) SetCapture(blockID, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.term(blockID).capture = content
}

// Written returns everything written into a block with Write.
func (f *Fake) Written(blockID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.term(blockID).written
}

func (f *Fake) term(blockID string) *terminal {
	t, ok := f.terminals[blockID]
	if !ok {
		t = &terminal{}
		f.terminals[blockID] = t
	}
	return t
}

// Fail makes every later call of method (an API name such as
// session.close_layer, or block.<name> for block methods) return err.
// A nil err clears it.
func (f *Fake) Fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.fail, method)
		return
	}
	f.fail[method] = err
}

// Calls returns the recorded calls, oldest first.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Methods returns the method names of the recorded calls, oldest first.
func (f *Fake) Methods() []string {
	var names []string
	for _, c := range f.Calls() {
		names = append(names, c.Method)
	}
	return names
}

// ResetCalls forgets the recorded calls.
func (f *Fake) ResetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// record logs a call and returns the injected error for it, if any. The
// caller must hold f.mu.
func (f *Fake) record(method string, args ...string) error {
	f.calls = append(f.calls, Call{Method: method, Args: args})
	return f.fail[method]
}

func (f *Fake) session(id string) *session {
	for _, s := range f.sessions {
		if s.view.SessionID == id {
			return s
		}
	}
	return nil
}

func notFound(what, id string) error {
	return fmt.Errorf("rexfake: %s %s: %w", what, id, rex.ErrNotFound)
}
