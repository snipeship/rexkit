package rex

import (
	"context"
	_ "embed"
	"time"
)

// Inventory is everything Rex is running, collected in one round trip.
type Inventory struct {
	Sessions    List[SessionView] `json:"sessions"`
	Errors      List[string]      `json:"errors"`
	CollectedAt time.Time         `json:"collected_at"`
}

// SessionView is a session's view, with Terminal filled in on terminal
// blocks, and its position in the server's session order. Detached are the
// session's blocks that are in no window (a floating pane ⌃P w hid, say),
// with Terminal filled in too.
type SessionView struct {
	View
	Order    int         `json:"order"`
	Detached List[Block] `json:"detached"`
}

// Session returns the session with the given ID.
func (inv Inventory) Session(id string) (SessionView, bool) {
	for _, s := range inv.Sessions {
		if s.SessionID == id {
			return s, true
		}
	}
	return SessionView{}, false
}

//go:embed lua/collect.lua
var collectScript []byte

// collectError is a session the collector could not read.
type collectError struct {
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// Snapshot collects every session, window, layer and block, with process,
// program status and title for terminal blocks, through one embedded Lua
// script. When the sessions cannot be listed at all it fails; a session that
// cannot be read is reported in Errors ("<session id>: <why>") and skipped.
func (c *Client) Snapshot(ctx context.Context) (Inventory, error) {
	var out struct {
		Sessions List[SessionView]  `json:"sessions"`
		Errors   List[collectError] `json:"errors"`
	}
	if err := c.script(ctx, "collect", collectScript, nil, &out); err != nil {
		return Inventory{}, err
	}
	inv := Inventory{Sessions: out.Sessions, CollectedAt: time.Now()}
	for _, e := range out.Errors {
		msg := CleanMessage(e.Error)
		if e.SessionID != "" {
			msg = e.SessionID + ": " + msg
		}
		inv.Errors = append(inv.Errors, msg)
	}
	return inv, nil
}
