package attend

import (
	"context"
	"slices"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// Jump is where ⌥A or ⌥⇧A goes, for the Lua layer to carry out (focus the
// block, switch the app to the session), and the toast to show there. With
// no SessionID it goes nowhere and the toast says why.
type Jump struct {
	SessionID string     `json:"session_id,omitempty"`
	WindowID  string     `json:"window_id,omitempty"`
	BlockID   string     `json:"block_id,omitempty"`
	Toast     *ToastSpec `json:"toast,omitempty"`
}

// ToastSpec is a toast as `rexkit toast` takes it.
type ToastSpec struct {
	Message string `json:"message"`
	Kind    string `json:"kind"`
	Status  string `json:"status,omitempty"`
}

// spec turns a Toast into flags for `rexkit toast`.
func spec(t Toast) *ToastSpec {
	s := &ToastSpec{Message: t.Text, Kind: map[ui.ToastKind]string{
		ui.ToastInfo: "info", ui.ToastOK: "ok", ui.ToastWarn: "warn", ui.ToastErr: "err"}[t.Kind]}
	switch t.Status {
	case ui.StatusBlocked:
		s.Status = "blocked"
	case ui.StatusFailed:
		s.Status = "error"
	case ui.StatusDone:
		s.Status = "done"
	}
	return s
}

// Keys are the keys toasts name: ⌥A and ⌥⇧A, or what stands in for them.
type Keys struct {
	Next, Back string
}

// NextJump is ⌥A from here: the next agent that needs the user (Next). The
// pane the user is on counts as seen, and so does the one they go to.
// Where they were is kept for ⌥⇧A, unless they were where the last ⌥A
// went: a walk of several presses goes back to where it began.
func NextJump(d state.Dirs, inv rex.Inventory, here state.Visit, keys Keys, now time.Time) (Jump, error) {
	panes := Panes(inv, Recall(d), now)
	see := func(block string) {
		for i := range panes {
			if panes[i].Block == block {
				panes[i].Seen = now
			}
		}
	}
	see(here.BlockID)
	to, ok := Next(panes, here.BlockID, now)
	if !ok {
		err := d.UpdateAttention(now, func(a *state.Attention) {
			if here.BlockID != "" {
				a.See(here.BlockID, now)
			}
		})
		msg := "No agent needs you"
		if slices.ContainsFunc(Waiting(panes, now), func(p Pane) bool { return p.Block == here.BlockID }) {
			msg = "No other agent needs you" // the one here still does
		}
		return Jump{Toast: &ToastSpec{Message: msg, Kind: "ok"}}, err
	}
	see(to.Block)
	landed := state.Visit{Kind: "agent", SessionID: to.Session, WindowID: to.Window, BlockID: to.Block, At: now}
	err := d.UpdateAttention(now, func(a *state.Attention) {
		if here.BlockID != "" {
			a.See(here.BlockID, now)
		}
		a.See(to.Block, now)
		if a.Landed == nil || !a.Landed.Same(here) {
			back := here
			a.Back = &back
		}
		a.Landed = &landed
	})
	return Jump{SessionID: to.Session, WindowID: to.Window, BlockID: to.Block,
		Toast: spec(Landed(panes, to.Block, keys.Next, keys.Back, now))}, err
}

// BackJump is ⌥⇧A from here: back to where the last walk of ⌥A presses
// began, or to the nearest place of it that is still there (its tab, its
// session). Pressed again, it returns to here.
func BackJump(d state.Dirs, inv rex.Inventory, here state.Visit, now time.Time) (Jump, error) {
	a := d.Attention()
	if a.Back == nil {
		return Jump{Toast: &ToastSpec{Message: "Nothing to go back to", Kind: "info"}}, nil
	}
	to, ok := resolve(inv, *a.Back)
	if !ok {
		return Jump{Toast: &ToastSpec{Message: "The place you left is gone", Kind: "info"}}, nil
	}
	err := d.UpdateAttention(now, func(a *state.Attention) {
		if to.BlockID != "" {
			a.See(to.BlockID, now)
		}
		back := here
		a.Back, a.Landed = &back, nil
	})
	return Jump{SessionID: to.SessionID, WindowID: to.WindowID, BlockID: to.BlockID}, err
}

// resolve finds a place in the inventory: the block if it is still there,
// else its window, else its session.
func resolve(inv rex.Inventory, v state.Visit) (state.Visit, bool) {
	s, ok := inv.Session(v.SessionID)
	if !ok {
		return state.Visit{}, false
	}
	out := state.Visit{Kind: "session", SessionID: s.SessionID}
	if w, _, _, ok := s.FindBlock(v.BlockID); ok && v.BlockID != "" {
		out.Kind, out.WindowID, out.BlockID = "pane", w.ID, v.BlockID
		return out, true
	}
	if w, ok := s.Window(v.WindowID); ok {
		out.Kind, out.WindowID, out.BlockID = "tab", w.ID, w.FocusedBlockID
	}
	return out, true
}

// GoAPI is what going to a pane from a toast needs of Rex.
type GoAPI interface {
	FocusBlock(ctx context.Context, session, block string) error
	ClientPerform(ctx context.Context, client, action string, args any) error
}

// Visit goes to the pane a toast is about (a click on it): the app
// (client) switches to its session when it shows another one (from), the
// pane gets the focus, and what the agent says counts as seen. Switching
// needs Remote Control. Without a client, or with the switch refused, the
// click only closes the toast: the focus moving over there would look like
// the user went (the watcher's guess), and the agent would count as seen.
func Visit(ctx context.Context, api GoAPI, d state.Dirs, to state.Visit, client, from string) error {
	if to.SessionID != from {
		if client == "" {
			return nil
		}
		if err := api.ClientPerform(ctx, client, "session.select", map[string]string{"session_id": to.SessionID, "window_id": to.WindowID}); err != nil {
			return err
		}
	}
	if err := api.FocusBlock(ctx, to.SessionID, to.BlockID); err != nil {
		return err
	}
	now := time.Now()
	return d.UpdateAttention(now, func(a *state.Attention) { a.See(to.BlockID, now) })
}
