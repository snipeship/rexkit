package rex

import (
	"context"
	"encoding/json"
)

type object = map[string]any

func (c *Client) sessionCall(ctx context.Context, session, method string, args object, out any) error {
	args["session_id"] = session
	return c.Call(ctx, method, args, out)
}

// Sessions lists the sessions in the server's order.
func (c *Client) Sessions(ctx context.Context) ([]SessionInfo, error) {
	var res struct {
		Sessions List[SessionInfo] `json:"sessions"`
	}
	err := c.Call(ctx, "session.list", nil, &res)
	return res.Sessions, err
}

// View returns a session's windows, layers and blocks.
func (c *Client) View(ctx context.Context, session string) (View, error) {
	var v View
	err := c.sessionCall(ctx, session, "session.view", object{}, &v)
	return v, err
}

// FocusBlock focuses a block and makes its window active.
func (c *Client) FocusBlock(ctx context.Context, session, block string) error {
	return c.sessionCall(ctx, session, "session.focus_block", object{"block_id": block}, nil)
}

// FocusWindow makes a window active; focus goes to its last focused block.
func (c *Client) FocusWindow(ctx context.Context, session, window string) error {
	return c.sessionCall(ctx, session, "session.focus_window", object{"window_id": window}, nil)
}

// ZoomBlock zooms a block (which also focuses it) or restores the layout.
func (c *Client) ZoomBlock(ctx context.Context, session, block string, zoom bool) error {
	return c.sessionCall(ctx, session, "session.zoom_block", object{"block_id": block, "zoom": zoom}, nil)
}

// NewWindow creates a window.
func (c *Client) NewWindow(ctx context.Context, session string, req NewWindowRequest) (Created, error) {
	var res Created
	err := c.Call(ctx, "session.new_window", withSession(session, req), &res)
	return res, err
}

// NewSplit creates blocks beside an existing block or split.
func (c *Client) NewSplit(ctx context.Context, session string, req NewSplitRequest) (Created, error) {
	var res Created
	err := c.Call(ctx, "session.new_split", withSession(session, req), &res)
	return res, err
}

// NewLayer creates a floating layer.
func (c *Client) NewLayer(ctx context.Context, session string, req NewLayerRequest) (Created, error) {
	var res Created
	err := c.Call(ctx, "session.new_layer", withSession(session, req), &res)
	return res, err
}

// CloseLayer closes a floating layer and its blocks. If the layer had focus,
// focus returns exactly where it was before the layer opened.
func (c *Client) CloseLayer(ctx context.Context, session, layer string) error {
	return c.sessionCall(ctx, session, "session.close_layer", object{"layer_id": layer}, nil)
}

// MoveBlock places a block beside an anchor block.
func (c *Client) MoveBlock(ctx context.Context, session string, req MoveBlockRequest) error {
	return c.Call(ctx, "session.move_block", withSession(session, req), nil)
}

// SetBlockLabel sets a block's label; empty restores the automatic one.
func (c *Client) SetBlockLabel(ctx context.Context, session, block, label string) error {
	return c.sessionCall(ctx, session, "session.set_block_label", object{"block_id": block, "label": label}, nil)
}

// SetWindowLabel sets a window's label; empty restores the automatic one.
func (c *Client) SetWindowLabel(ctx context.Context, session, window, label string) error {
	return c.sessionCall(ctx, session, "session.set_window_label", object{"window_id": window, "label": label}, nil)
}

// SetSessionLabel sets a session's label; empty restores the automatic one.
func (c *Client) SetSessionLabel(ctx context.Context, session, label string) error {
	return c.sessionCall(ctx, session, "session.set_label", object{"label": label}, nil)
}

// ClientPerform asks a client (the app) to perform one of its actions. It
// fails with ErrRefused while the app's Remote Control setting is off.
func (c *Client) ClientPerform(ctx context.Context, client, action string, args any) error {
	req := object{"client_id": client, "action": action, "origin": "api"}
	if args != nil {
		req["args"] = args
	}
	return c.Call(ctx, "client.perform", req, nil)
}

// withSession turns a request struct into an object with session_id set.
func withSession(session string, req any) object {
	data, _ := json.Marshal(req) // request structs always marshal
	args := object{}
	_ = json.Unmarshal(data, &args)
	args["session_id"] = session
	return args
}
