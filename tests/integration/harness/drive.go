//go:build integration

package harness

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
)

// Session is a session created by NewSession.
type Session struct {
	ID       string
	WindowID string
	BlockID  string // the first window's first block
}

// NewSession creates a session whose first block runs argv directly (no
// shell); with no argv it runs /bin/cat, which echoes what it is sent.
func (s *Server) NewSession(t testing.TB, label string, argv ...string) Session {
	t.Helper()
	if len(argv) == 0 {
		argv = []string{"/bin/cat"}
	}
	out := s.CLI(t, append([]string{"new", label, "--json", "--shell", "none", "--"}, argv...)...)
	var created struct {
		SessionID string `json:"session_id"`
		Windows   []struct {
			WindowID string   `json:"window_id"`
			BlockIDs []string `json:"block_ids"`
		} `json:"initial_windows"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || len(created.Windows) == 0 {
		t.Fatalf("rex new: %v: %s", err, out)
	}
	w := created.Windows[0]
	return Session{ID: created.SessionID, WindowID: w.WindowID, BlockID: w.BlockIDs[0]}
}

// CreateSession creates a session whose first tab, labelled "home", holds
// layout, and attaches it. Blocks lists the tab's blocks in layout order.
func (s *Server) CreateSession(t testing.TB, label string, layout rex.Node) (Session, []string) {
	t.Helper()
	ctx := context.Background()
	var created struct {
		SessionID string `json:"session_id"`
		Windows   []struct {
			WindowID string           `json:"window_id"`
			BlockIDs rex.List[string] `json:"block_ids"`
		} `json:"initial_windows"`
	}
	args := map[string]any{"label": label, "initial_windows": []any{map[string]any{"window_label": "home", "layout": layout}}}
	if err := s.Rex.Call(ctx, "session.create", args, &created); err != nil || len(created.Windows) == 0 {
		t.Fatalf("session.create: %v", err)
	}
	if err := s.Rex.Call(ctx, "session.attach", map[string]string{"session_id": created.SessionID}, nil); err != nil {
		t.Fatalf("session.attach: %v", err)
	}
	w := created.Windows[0]
	return Session{ID: created.SessionID, WindowID: w.WindowID, BlockID: w.BlockIDs[0]}, w.BlockIDs
}

// View returns a session's view.
func (s *Server) View(t testing.TB, session string) rex.View {
	t.Helper()
	v, err := s.Rex.View(context.Background(), session)
	if err != nil {
		t.Fatalf("view %s: %v", session, err)
	}
	return v
}

// Focus reports the active window's focused and zoomed block.
func (s *Server) Focus(t testing.TB, session string) (focused, zoomed string) {
	t.Helper()
	v := s.View(t, session)
	w, _ := v.Window(v.ActiveWindowID)
	return w.FocusedBlockID, w.ZoomedBlockID
}

// Capture returns a block's screen and scrollback as trimmed text.
func (s *Server) Capture(t testing.TB, session, block string) string {
	t.Helper()
	out, err := s.Rex.Capture(context.Background(), session, block, rex.CaptureOptions{Trim: true})
	if err != nil {
		t.Fatalf("capture %s: %v", block, err)
	}
	return out
}

// WaitFor polls cond until it holds or timeout passes.
func WaitFor(t testing.TB, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WaitScreen waits until a block's text contains want and returns the text.
func (s *Server) WaitScreen(t testing.TB, session, block, want string, timeout time.Duration) string {
	t.Helper()
	var screen string
	WaitFor(t, timeout, "screen to show "+want, func() bool {
		screen = s.Capture(t, session, block)
		return strings.Contains(screen, want)
	})
	return screen
}

// Input is one step for Drive: a key (client.key) or an action
// (client.action). Steps of one Drive call share a connection, so the server
// runs them strictly in order, as it does for the app.
type Input struct {
	Block  string          `json:"block,omitempty"` // default: the focused block
	Key    *KeyEvent       `json:"key,omitempty"`
	Prefix []string        `json:"prefix,omitempty"` // earlier keys of a sequence
	Action string          `json:"action,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	Origin string          `json:"origin,omitempty"` // default "key"
}

// Press is the Input for a binding-style key such as "ctrl+c".
func Press(t testing.TB, block, spec string) Input {
	t.Helper()
	ev, err := Key(spec)
	if err != nil {
		t.Fatal(err)
	}
	return Input{Block: block, Key: &ev}
}

// Type is the Inputs for typed text.
func Type(t testing.TB, block, text string) []Input {
	t.Helper()
	events, err := Text(text)
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]Input, len(events))
	for i := range events {
		inputs[i] = Input{Block: block, Key: &events[i]}
	}
	return inputs
}

// Act is the Input for an action with optional arguments.
func Act(t testing.TB, block, action string, args any) Input {
	t.Helper()
	in := Input{Block: block, Action: action}
	if args != nil {
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		in.Args = data
	}
	return in
}

// ClientAction is a client action an action queued with rex.client.queue.
type ClientAction struct {
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// Outcome is the server's answer to one Input.
type Outcome struct {
	Handled       bool                   `json:"handled"`        // key: a Lua function took it
	ClientActions rex.List[ClientAction] `json:"client_actions"` // action: queued client actions
	Result        json.RawMessage        `json:"result"`         // action: its return value
}

// Names lists the queued client actions' names.
func (o Outcome) Names() []string {
	var names []string
	for _, a := range o.ClientActions {
		names = append(names, a.Action)
	}
	return names
}

const driveScript = `
local a = rex.args
local _, err = rex.call("session.attach", {session_id = a.session})
if err then return {error = tostring(err)} end
local out = {}
for i, step in ipairs(a.steps) do
  local res, serr
  if step.key then
    res, serr = rex.call("client.key", {session_id = a.session, block_id = step.block, key = step.key, prefix = step.prefix})
  else
    res, serr = rex.call("client.action", {session_id = a.session, block_id = step.block, action = step.action,
      args = step.args, origin = step.origin or "key"})
  end
  if not res then return {error = tostring(serr), step = i} end
  out[i] = res
end
return {results = out}
`

// Drive performs inputs in order over one connection attached to session,
// the way the app sends keys and actions, and returns one Outcome per input.
func (s *Server) Drive(t testing.TB, session string, inputs ...Input) []Outcome {
	t.Helper()
	args, err := json.Marshal(map[string]any{"session": session, "steps": inputs})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(s.Bin, "--autostart=false", "-S", s.Addr, "do", "-", "--json", "--args", string(args))
	cmd.Env = s.Env
	cmd.Stdin = strings.NewReader(driveScript)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("drive: %v\n%s", err, stderr)
	}
	var res struct {
		Results rex.List[Outcome] `json:"results"`
		Error   string            `json:"error"`
		Step    int               `json:"step"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("drive: %v: %s", err, out)
	}
	if res.Error != "" {
		t.Fatalf("drive step %d: %s", res.Step, rex.CleanMessage(res.Error))
	}
	return res.Results
}
