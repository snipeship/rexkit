package rexfake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"rexkit/internal/rex"
)

var _ rex.API = (*Fake)(nil)

// ErrUnsupported is returned by Call for methods the fake does not model.
var ErrUnsupported = errors.New("rexfake: method not modelled")

// clone deep-copies through JSON so callers never share the fake's state.
func clone[T any](v T) T {
	var out T
	data, _ := json.Marshal(v)
	_ = json.Unmarshal(data, &out)
	return out
}

// Call records the call and returns ErrUnsupported.
func (f *Fake) Call(_ context.Context, method string, _, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record(method); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrUnsupported, method)
}

func (f *Fake) Sessions(context.Context) ([]rex.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.list"); err != nil {
		return nil, err
	}
	var out []rex.SessionInfo
	for _, s := range f.sessions {
		out = append(out, rex.SessionInfo{ID: s.view.SessionID, Label: s.view.Label})
	}
	return out, nil
}

func (f *Fake) View(_ context.Context, sessionID string) (rex.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.view", sessionID); err != nil {
		return rex.View{}, err
	}
	s := f.session(sessionID)
	if s == nil {
		return rex.View{}, notFound("session", sessionID)
	}
	return clone(s.view), nil
}

func (f *Fake) Snapshot(context.Context) (rex.Inventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("snapshot"); err != nil {
		return rex.Inventory{}, err
	}
	inv := rex.Inventory{CollectedAt: time.Now()}
	for i, s := range f.sessions {
		sv := rex.SessionView{View: clone(s.view), Order: i}
		for wi := range sv.Windows {
			for li := range sv.Windows[wi].Layers {
				blocks := sv.Windows[wi].Layers[li].Blocks
				for bi := range blocks {
					t := clone(f.term(blocks[bi].ID).state)
					blocks[bi].Terminal = &t
				}
			}
		}
		inv.Sessions = append(inv.Sessions, sv)
	}
	return inv, nil
}

func (f *Fake) Watch(context.Context) (<-chan rex.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("watch"); err != nil {
		return nil, err
	}
	if f.Events == nil {
		return nil, fmt.Errorf("%w: watch", ErrUnsupported)
	}
	return f.Events, nil
}

func (f *Fake) Keymap(context.Context) (rex.Keymap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("keymap"); err != nil {
		return rex.Keymap{}, err
	}
	return clone(f.KeymapResult), nil
}

func (f *Fake) FocusBlock(_ context.Context, sessionID, blockID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.focus_block", sessionID, blockID); err != nil {
		return err
	}
	s, w, _, err := f.find(sessionID, blockID)
	if err != nil {
		return err
	}
	focus(s, w, blockID)
	return nil
}

func (f *Fake) FocusWindow(_ context.Context, sessionID, windowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.focus_window", sessionID, windowID); err != nil {
		return err
	}
	s := f.session(sessionID)
	if s == nil {
		return notFound("session", sessionID)
	}
	if _, ok := s.view.Window(windowID); !ok {
		return notFound("window", windowID)
	}
	s.view.ActiveWindowID = windowID
	s.view.Revision++
	return nil
}

func (f *Fake) ZoomBlock(_ context.Context, sessionID, blockID string, zoom bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.zoom_block", sessionID, blockID, fmt.Sprint(zoom)); err != nil {
		return err
	}
	s, w, _, err := f.find(sessionID, blockID)
	if err != nil {
		return err
	}
	focus(s, w, blockID)
	w.ZoomedBlockID = ""
	if zoom {
		w.ZoomedBlockID = blockID
	}
	return nil
}

func (f *Fake) NewWindow(_ context.Context, sessionID string, req rex.NewWindowRequest) (rex.Created, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.new_window", sessionID, req.Label); err != nil {
		return rex.Created{}, err
	}
	s := f.session(sessionID)
	if s == nil {
		return rex.Created{}, notFound("session", sessionID)
	}
	var labels []string
	for _, b := range leaves(req.Layout) {
		labels = append(labels, b.Label)
	}
	if len(labels) == 0 {
		return rex.Created{}, errors.New("rexfake: new_window needs a layout")
	}
	w := f.addWindow(s, req.Label, labels, req.Focus == nil || *req.Focus)
	created := rex.Created{WindowID: w.ID, Revision: s.view.Revision}
	for _, b := range w.Layers[0].Blocks {
		created.BlockIDs = append(created.BlockIDs, b.ID)
	}
	return created, nil
}

func (f *Fake) NewSplit(_ context.Context, sessionID string, req rex.NewSplitRequest) (rex.Created, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.new_split", sessionID, req.AnchorID); err != nil {
		return rex.Created{}, err
	}
	s, w, l, err := f.find(sessionID, req.AnchorID)
	if err != nil {
		return rex.Created{}, err
	}
	created := rex.Created{}
	anchor := req.AnchorID
	for _, spec := range leaves(req.Layout) {
		b := f.newBlock(spec.Label)
		l.Blocks = append(l.Blocks, b)
		insert(l.Layout, anchor, b.ID, req.Direction, req.Side)
		created.BlockIDs = append(created.BlockIDs, b.ID)
		anchor = b.ID
	}
	w.ZoomedBlockID = ""
	if req.Focus {
		focus(s, w, created.BlockIDs[0])
	}
	s.view.Revision++
	created.Revision = s.view.Revision
	return created, nil
}

func (f *Fake) NewLayer(_ context.Context, sessionID string, req rex.NewLayerRequest) (rex.Created, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.new_layer", sessionID, req.WindowID); err != nil {
		return rex.Created{}, err
	}
	s := f.session(sessionID)
	if s == nil {
		return rex.Created{}, notFound("session", sessionID)
	}
	windowID := req.WindowID
	if windowID == "" {
		windowID = s.view.ActiveWindowID
	}
	w := f.window(s, windowID)
	if w == nil {
		return rex.Created{}, notFound("window", windowID)
	}
	var blocks []rex.Block
	for _, spec := range leaves(req.Layout) {
		blocks = append(blocks, f.newBlock(spec.Label))
	}
	if len(blocks) == 0 {
		return rex.Created{}, errors.New("rexfake: new_layer needs a layout")
	}
	layer := rex.Layer{ID: f.id("layer"), Kind: rex.LayerFloating, Bounds: req.Bounds, Layout: row(blocks), Blocks: blocks}
	w.Layers = append(w.Layers, layer)
	created := rex.Created{LayerID: layer.ID}
	for _, b := range blocks {
		created.BlockIDs = append(created.BlockIDs, b.ID)
	}
	if req.Focus {
		s.restore[layer.ID] = w.FocusedBlockID
		focus(s, w, blocks[0].ID)
	}
	s.view.Revision++
	created.Revision = s.view.Revision
	return created, nil
}

func (f *Fake) CloseLayer(_ context.Context, sessionID, layerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.close_layer", sessionID, layerID); err != nil {
		return err
	}
	s := f.session(sessionID)
	if s == nil {
		return notFound("session", sessionID)
	}
	for wi := range s.view.Windows {
		w := &s.view.Windows[wi]
		li := slices.IndexFunc(w.Layers, func(l rex.Layer) bool { return l.ID == layerID })
		if li < 0 {
			continue
		}
		if !w.Layers[li].Floating() {
			return fmt.Errorf("rexfake: layer %s is tiled", layerID)
		}
		hadFocus := slices.ContainsFunc(w.Layers[li].Blocks, func(b rex.Block) bool { return b.ID == w.FocusedBlockID })
		w.Layers = slices.Delete(w.Layers, li, li+1)
		if hadFocus {
			w.FocusedBlockID = s.restore[layerID]
		}
		delete(s.restore, layerID)
		s.view.Revision++
		return nil
	}
	return notFound("layer", layerID)
}

func (f *Fake) MoveBlock(_ context.Context, sessionID string, req rex.MoveBlockRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.move_block", sessionID, req.BlockID, req.AnchorID); err != nil {
		return err
	}
	s, from, fromLayer, err := f.find(sessionID, req.BlockID)
	if err != nil {
		return err
	}
	if _, _, _, err := f.find(sessionID, req.AnchorID); err != nil {
		return err
	}
	bi := slices.IndexFunc(fromLayer.Blocks, func(b rex.Block) bool { return b.ID == req.BlockID })
	block := fromLayer.Blocks[bi]
	fromLayer.Blocks = slices.Delete(fromLayer.Blocks, bi, bi+1)
	fromLayer.Layout = remove(fromLayer.Layout, req.BlockID)
	if from.FocusedBlockID == req.BlockID {
		from.FocusedBlockID = ""
	}
	pruneEmpty(s)
	_, to, toLayer, _ := f.find(sessionID, req.AnchorID)
	toLayer.Blocks = append(toLayer.Blocks, block)
	insert(toLayer.Layout, req.AnchorID, req.BlockID, req.Direction, req.Side)
	to.ZoomedBlockID = ""
	s.view.Revision++
	return nil
}

func (f *Fake) SetBlockLabel(_ context.Context, sessionID, blockID, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.set_block_label", sessionID, blockID, label); err != nil {
		return err
	}
	s, _, l, err := f.find(sessionID, blockID)
	if err != nil {
		return err
	}
	for i := range l.Blocks {
		if l.Blocks[i].ID == blockID {
			l.Blocks[i].Label = label
		}
	}
	s.view.Revision++
	return nil
}

func (f *Fake) SetWindowLabel(_ context.Context, sessionID, windowID, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.set_window_label", sessionID, windowID, label); err != nil {
		return err
	}
	s := f.session(sessionID)
	if s == nil {
		return notFound("session", sessionID)
	}
	w := f.window(s, windowID)
	if w == nil {
		return notFound("window", windowID)
	}
	w.Label = label
	s.view.Revision++
	return nil
}

func (f *Fake) SetSessionLabel(_ context.Context, sessionID, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.set_label", sessionID, label); err != nil {
		return err
	}
	s := f.session(sessionID)
	if s == nil {
		return notFound("session", sessionID)
	}
	s.view.Label = label
	s.view.Revision++
	return nil
}

func (f *Fake) CloseBlock(_ context.Context, sessionID, blockID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("block.close", sessionID, blockID); err != nil {
		return err
	}
	s, w, l, err := f.find(sessionID, blockID)
	if err != nil {
		return err
	}
	l.Blocks = slices.DeleteFunc(l.Blocks, func(b rex.Block) bool { return b.ID == blockID })
	l.Layout = remove(l.Layout, blockID)
	if w.FocusedBlockID == blockID {
		w.FocusedBlockID = ""
		if len(w.Layers[0].Blocks) > 0 {
			w.FocusedBlockID = w.Layers[0].Blocks[0].ID
		}
	}
	if w.ZoomedBlockID == blockID {
		w.ZoomedBlockID = ""
	}
	delete(f.terminals, blockID)
	pruneEmpty(s)
	s.view.Revision++
	return nil
}

func (f *Fake) CloseWindow(_ context.Context, sessionID, windowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.close_window", sessionID, windowID); err != nil {
		return err
	}
	s := f.session(sessionID)
	if s == nil {
		return notFound("session", sessionID)
	}
	if f.window(s, windowID) == nil {
		return notFound("window", windowID)
	}
	s.view.Windows = slices.DeleteFunc(s.view.Windows, func(w rex.Window) bool { return w.ID == windowID })
	pruneEmpty(s)
	s.view.Revision++
	return nil
}

func (f *Fake) DestroySession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("session.destroy", sessionID); err != nil {
		return err
	}
	if f.session(sessionID) == nil {
		return notFound("session", sessionID)
	}
	f.sessions = slices.DeleteFunc(f.sessions, func(s *session) bool { return s.view.SessionID == sessionID })
	return nil
}

func (f *Fake) ClientPerform(_ context.Context, client, action string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("client.perform", client, action); err != nil {
		return err
	}
	if !f.RemoteControl {
		return fmt.Errorf("rexfake: Remote Control is off: %w", rex.ErrRefused)
	}
	return nil
}

// find locates a block; the caller must hold f.mu.
func (f *Fake) find(sessionID, blockID string) (*session, *rex.Window, *rex.Layer, error) {
	s := f.session(sessionID)
	if s == nil {
		return nil, nil, nil, notFound("session", sessionID)
	}
	for wi := range s.view.Windows {
		w := &s.view.Windows[wi]
		for li := range w.Layers {
			for _, b := range w.Layers[li].Blocks {
				if b.ID == blockID {
					return s, w, &w.Layers[li], nil
				}
			}
		}
	}
	return nil, nil, nil, notFound("block", blockID)
}

func (f *Fake) window(s *session, id string) *rex.Window {
	for wi := range s.view.Windows {
		if s.view.Windows[wi].ID == id {
			return &s.view.Windows[wi]
		}
	}
	return nil
}

// focus focuses a block like the server: its window becomes active, and a
// zoom of another block in that window ends.
func focus(s *session, w *rex.Window, blockID string) {
	if w.ZoomedBlockID != blockID {
		w.ZoomedBlockID = ""
	}
	w.FocusedBlockID = blockID
	s.view.ActiveWindowID = w.ID
	s.view.Revision++
}

// pruneEmpty drops empty floating layers and windows without blocks.
func pruneEmpty(s *session) {
	for wi := range s.view.Windows {
		w := &s.view.Windows[wi]
		w.Layers = slices.DeleteFunc(w.Layers, func(l rex.Layer) bool { return l.Floating() && len(l.Blocks) == 0 })
	}
	s.view.Windows = slices.DeleteFunc(s.view.Windows, func(w rex.Window) bool { return len(w.Layers[0].Blocks) == 0 })
	if s.view.ActiveWindowID != "" && !slices.ContainsFunc(s.view.Windows, func(w rex.Window) bool { return w.ID == s.view.ActiveWindowID }) {
		s.view.ActiveWindowID = ""
		if len(s.view.Windows) > 0 {
			s.view.ActiveWindowID = s.view.Windows[0].ID
		}
	}
}
