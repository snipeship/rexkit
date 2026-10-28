package rexfake

import (
	"context"
	"encoding/json"
	"fmt"

	"rexkit/internal/rex"
)

func (f *Fake) BlockCall(_ context.Context, sessionID, blockID, method string, args, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("block."+method, sessionID, blockID); err != nil {
		return err
	}
	if _, _, _, err := f.find(sessionID, blockID); err != nil && !f.isDetached(sessionID, blockID) {
		return err // a detached block answers, as in Rex: its program runs on
	}
	t := f.term(blockID)
	var result any
	switch method {
	case "process":
		result = t.state.Process
	case "program_status":
		result = map[string]any{"records": t.state.Status}
	case "title":
		result = map[string]string{"title": t.state.Title}
	case "size":
		result = t.size
	case "format":
		result = map[string]string{"content": t.capture}
	case "write":
		var in struct {
			Data string `json:"data"`
		}
		data, _ := json.Marshal(args)
		_ = json.Unmarshal(data, &in)
		t.written += in.Data
		result = map[string]int{"bytes_written": len(in.Data)}
	default:
		return fmt.Errorf("%w: block method %s", ErrUnsupported, method)
	}
	if out == nil {
		return nil
	}
	data, _ := json.Marshal(result)
	return json.Unmarshal(data, out)
}

func (f *Fake) Process(ctx context.Context, sessionID, blockID string) (rex.Process, error) {
	var p rex.Process
	err := f.BlockCall(ctx, sessionID, blockID, "process", nil, &p)
	return p, err
}

func (f *Fake) ProgramStatus(ctx context.Context, sessionID, blockID string) ([]rex.StatusRecord, error) {
	var res struct {
		Records []rex.StatusRecord `json:"records"`
	}
	err := f.BlockCall(ctx, sessionID, blockID, "program_status", nil, &res)
	return res.Records, err
}

func (f *Fake) Size(ctx context.Context, sessionID, blockID string) (rex.Size, error) {
	var s rex.Size
	err := f.BlockCall(ctx, sessionID, blockID, "size", nil, &s)
	return s, err
}

func (f *Fake) Write(ctx context.Context, sessionID, blockID, data string) error {
	return f.BlockCall(ctx, sessionID, blockID, "write", map[string]string{"data": data}, nil)
}

// Capture returns the SetCapture content of any block, whatever the options.
// The fake does not model session attachment, formats or tails: what the real
// client must do to read another session's block (attach the tail script with
// -s) is covered by internal/rex's argv tests and the integration harness.
func (f *Fake) Capture(ctx context.Context, sessionID, blockID string, _ rex.CaptureOptions) (string, error) {
	var res struct {
		Content string `json:"content"`
	}
	err := f.BlockCall(ctx, sessionID, blockID, "format", nil, &res)
	return res.Content, err
}

// Screens reads the panes of a window's tiled layer like the real client's
// script (window is a window ID, a block ID or empty): rects from the
// layout (the whole window for a zoomed block), sizes from SetSize and
// screens from the last rows of SetCapture.
func (f *Fake) Screens(ctx context.Context, sessionID, windowID string) (rex.Screens, error) {
	view, err := f.View(ctx, sessionID)
	if err != nil {
		return rex.Screens{}, err
	}
	if windowID == "" {
		windowID = view.ActiveWindowID
	}
	if bw, _, _, ok := view.FindBlock(windowID); ok {
		windowID = bw.ID
	}
	w, ok := view.Window(windowID)
	if !ok {
		return rex.Screens{}, notFound("window", windowID)
	}
	out := rex.Screens{WindowID: w.ID, Zoomed: w.ZoomedBlockID}
	add := func(l rex.Layer, id string, rect rex.Rect) {
		for _, b := range l.Blocks {
			if b.ID != id || !b.IsTerminal() {
				continue
			}
			p := rex.PaneScreen{BlockID: id, Rect: rect}
			var res struct {
				Content string `json:"content"`
			}
			var err error
			if p.Size, err = f.Size(ctx, sessionID, id); err == nil {
				err = f.BlockCall(ctx, sessionID, id, "format", nil, &res)
			}
			if err != nil {
				p.Error = err.Error()
			}
			p.Screen = rex.LastLines(res.Content, p.Size.Rows)
			out.Panes = append(out.Panes, p)
		}
	}
	for _, l := range w.Layers {
		if l.Floating() || l.Layout == nil {
			continue
		}
		if w.ZoomedBlockID != "" {
			add(l, w.ZoomedBlockID, rex.Rect{0, 0, 1, 1})
			continue
		}
		var walk func(n rex.LayoutNode)
		walk = func(n rex.LayoutNode) {
			if n.Split != nil {
				walk(n.Split.Before)
				walk(n.Split.After)
				return
			}
			add(l, n.BlockID, n.Rect)
		}
		walk(*l.Layout)
	}
	return out, nil
}
