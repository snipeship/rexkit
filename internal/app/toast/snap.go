package toast

import (
	"fmt"
	"time"

	"rexkit/internal/attend"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show: one per kind of news, then the
// agent-attention toasts.
var SnapStates = []string{"ok", "info", "warn", "err",
	"agent-blocked", "agent-question", "agent-done", "agent-failed", "agent-many", "agent-next"}

// snapToasts are the sample messages of Snap, by state.
var snapToasts = map[string]struct {
	kind ui.ToastKind
	text string
}{
	"ok":   {ui.ToastOK, "Renamed the pane to api-server"},
	"info": {ui.ToastInfo, "Parked api in Scratch · {key:ctrl+p e} brings it back"},
	"warn": {ui.ToastWarn, "zoxide not found · projects are hidden"},
	"err":  {ui.ToastErr, "Could not open the editor: nvim exited with status 1"},
}

// snapAgents are the agents of the attention toasts: Go to's attention
// demo (switcher's attentionWorld), as the watcher would know them.
var snapAgents = map[string]struct {
	p   attend.Pane
	ago time.Duration // since the state began
}{
	"agent-blocked": {attend.Pane{Session: "studio", Block: "web-codex", Where: "studio › web", App: "codex",
		State: rex.StateBlocked, Kind: "permission", Msg: "Allow Bash(pnpm test --filter nav)?"}, 12 * time.Minute},
	"agent-question": {attend.Pane{Session: "studio", Block: "api-claude", Where: "studio › api", App: "claude",
		State: rex.StateBlocked, Kind: "question", Msg: "Use Redis or the in-memory limiter for /api/projects?"}, 4 * time.Minute},
	"agent-failed": {attend.Pane{Session: "infrastructure-and-deployment-pipelines", Block: "deploy-gemini",
		Where: "infrastructure-and-deployment-pipelines › deploy", App: "gemini",
		State: rex.StateError, Msg: "Error acquiring the state lock (held by ci@runner-17)"}, 41 * time.Minute},
	"agent-done": {attend.Pane{Session: "studio", Block: "web-claude", Where: "studio › web", App: "claude",
		State: rex.StateDone, Msg: "Navbar wraps cleanly at 375px · 3 files changed, 48 tests pass"}, 2 * time.Minute},
}

// agentToast is the toast the attention tracker words for a state: one
// agent starting to wait, two more joining one that already waits (a
// burst), or the toast after ⌥A lands on the permission ask with three more
// waiting.
func agentToast(name string) (attend.Toast, bool) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	pane := func(k string) attend.Pane {
		a := snapAgents[k]
		a.p.Since = now.Add(-a.ago)
		return a.p
	}
	tr := attend.NewTracker("alt+a", []string{rex.StateBlocked, rex.StateError, rex.StateDone})
	tr.Here("rexkit-spark-zsh", now.Add(-time.Hour))
	var toast attend.Toast
	ok := false
	switch name {
	case "agent-many":
		tr.Update([]attend.Pane{pane("agent-blocked")}, now.Add(-time.Minute))
		tr.Update([]attend.Pane{pane("agent-blocked"), pane("agent-done"), pane("agent-question")}, now)
		_, _, toast, ok = tr.Tick(now.Add(attend.Burst))
	case "agent-next":
		var panes []attend.Pane
		for _, k := range []string{"agent-blocked", "agent-question", "agent-failed", "agent-done"} {
			p := pane(k)
			if k == "agent-blocked" {
				p.Seen = now // ⌥A landed there
			}
			panes = append(panes, p)
		}
		toast, ok = attend.Landed(panes, snapAgents["agent-blocked"].p.Block, "alt+a", "alt+shift+a", now), true
	default:
		if _, known := snapAgents[name]; known {
			tr.Update(nil, now.Add(-time.Minute))
			tr.Update([]attend.Pane{pane(name)}, now)
			_, _, toast, ok = tr.Tick(now.Add(attend.Burst))
		}
	}
	return toast, ok
}

// Snap returns a toast of the named kind, for `rexkit snap`.
func Snap(dark bool, name string) (snap.Scene, error) {
	t, ok := snapToasts[name]
	var status ui.Status
	if !ok {
		var a attend.Toast
		if a, ok = agentToast(name); ok {
			t.kind, t.text, status = a.Kind, a.Text, a.Status
		}
	}
	if !ok {
		return snap.Scene{}, fmt.Errorf("toast has no state %q (try %v)", name, SnapStates)
	}
	m := newModel(t.kind, t.text, 0)
	m.status = status
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	return snap.Scene{Model: m, Script: func(*snap.Driver) {}, Rows: Rows}, nil
}
