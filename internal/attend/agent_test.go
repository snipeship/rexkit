package attend

import (
	"testing"
	"time"

	"rexkit/internal/rex"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func proc(name, path, invoked string) *rex.Process {
	return &rex.Process{Foreground: &rex.ProcessInfo{Name: name, Path: path, InvokedPath: invoked}}
}

func TestProgramKnowsAgents(t *testing.T) {
	tests := []struct {
		p     *rex.ProcessInfo
		name  string
		agent bool
	}{
		{&rex.ProcessInfo{Name: "node", Path: "/usr/local/bin/node", InvokedPath: "/opt/homebrew/bin/claude"}, "claude", true},
		{&rex.ProcessInfo{Name: "2.1.310", Path: "/Users/me/.local/share/claude/versions/2.1.310"}, "claude", true},
		{&rex.ProcessInfo{Name: "pi", Path: "/usr/local/bin/pi"}, "pi", true},
		{&rex.ProcessInfo{Name: "grok-cli", Path: "/usr/local/bin/grok-cli"}, "grok-cli", true},
		{&rex.ProcessInfo{Name: "zsh", Path: "/Users/campbell/bin/zsh"}, "zsh", false},
		{&rex.ProcessInfo{Name: "python3", Path: "/usr/bin/python3.12.1"}, "python3.12.1", false},
		{&rex.ProcessInfo{Name: "v1.2", Path: "/tmp/v1.2"}, "v1.2", false},
	}
	for _, tt := range tests {
		if name, agent := Program(tt.p); name != tt.name || agent != tt.agent {
			t.Errorf("Program(%+v) = %q, %v; want %q, %v", *tt.p, name, agent, tt.name, tt.agent)
		}
	}
}

func TestPrimaryRecord(t *testing.T) {
	old, recent := t0.Add(-time.Hour), t0
	tests := []struct {
		name    string
		records []rex.StatusRecord
		want    string
	}{
		{"the pane's own record wins over an urgent child",
			[]rex.StatusRecord{{ID: "task/1", State: "blocked", Title: "child", UpdatedAt: recent}, {State: "working", Title: "own", UpdatedAt: old}}, "own"},
		{"else the most urgent",
			[]rex.StatusRecord{{ID: "a", State: "working", Title: "w", UpdatedAt: recent}, {ID: "b", State: "blocked", Title: "b", UpdatedAt: old}}, "b"},
		{"then the most recent",
			[]rex.StatusRecord{{ID: "a", State: "idle", Title: "old", UpdatedAt: old}, {ID: "b", State: "idle", Title: "new", UpdatedAt: recent}}, "new"},
		{"an unknown state ranks last",
			[]rex.StatusRecord{{ID: "a", State: "mystery", Title: "m", UpdatedAt: recent}, {ID: "b", State: "done", Title: "d", UpdatedAt: old}}, "d"},
		{"the turn record is no state",
			[]rex.StatusRecord{{ID: "turn", State: "blocked", Title: "t", UpdatedAt: recent}, {ID: "x", State: "done", Title: "d", UpdatedAt: old}}, "d"},
	}
	for _, tt := range tests {
		if got, _ := Primary(tt.records); got.Title != tt.want {
			t.Errorf("%s: Primary = %q, want %q", tt.name, got.Title, tt.want)
		}
	}
	if _, ok := Primary(nil); ok {
		t.Error("Primary(nil) found a record")
	}
	if _, ok := Primary([]rex.StatusRecord{{ID: "turn", State: "done"}}); ok {
		t.Error("a turn record alone made a state")
	}
}

func TestMessageDropsFiller(t *testing.T) {
	for in, want := range map[string]string{
		"Done":                                 "",
		"Working…":                             "",
		"  needs input ":                       "",
		"Allow Bash?":                          "",
		"Allow mcp__github__create_issue?":     "",
		"Allow Bash(pnpm test --filter nav)?":  "Allow Bash(pnpm test --filter nav)?",
		"Tests pass ⏎ 3 files changed":         "Tests pass",
		"First line\nsecond":                   "First line",
		"Use Redis or the in-memory limiter?":  "Use Redis or the in-memory limiter?",
		"Done: navbar wraps cleanly at 375px.": "Done: navbar wraps cleanly at 375px.",
	} {
		if got := Message(in); got != want {
			t.Errorf("Message(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRead(t *testing.T) {
	now := t0
	rec := func(st, kind, msg string, ago time.Duration) rex.StatusRecord {
		return rex.StatusRecord{App: "claude", State: st, Kind: kind, Msg: msg, UpdatedAt: now.Add(-ago)}
	}
	tests := []struct {
		name string
		term rex.Terminal
		want Agent
	}{
		{"a report", rex.Terminal{Process: proc("node", "/bin/node", "/bin/claude"),
			Status: rex.List[rex.StatusRecord]{rec("blocked", "question", "Redis or memory?", time.Minute)}},
			Agent{App: "claude", State: "blocked", Kind: "question", Msg: "Redis or memory?", Since: now.Add(-time.Minute)}},
		{"Rex's own report of a question: the question", rex.Terminal{Process: proc("2.1.311", "/x/versions/2.1.311", ""), Title: "Tabs or spaces question",
			Status: rex.List[rex.StatusRecord]{{App: "claude-code", EffectiveApp: "claude-code", State: "blocked", Kind: "question",
				Msg: "answer: Tabs or spaces? (Tabs · Spaces)", UpdatedAt: now.Add(-time.Minute)}}},
			Agent{App: "claude-code", State: "blocked", Kind: "question", Msg: "Tabs or spaces? (Tabs · Spaces)", Since: now.Add(-time.Minute)}},
		{"answer: stays where it is no question", rex.Terminal{
			Status: rex.List[rex.StatusRecord]{rec("done", "", "answer: 42", time.Minute)}},
			Agent{App: "claude", State: "done", Msg: "answer: 42", Since: now.Add(-time.Minute)}},
		{"a kind only counts while blocked", rex.Terminal{
			Status: rex.List[rex.StatusRecord]{rec("done", "question", "", time.Minute)}},
			Agent{App: "claude", State: "done", Since: now.Add(-time.Minute)}},
		{"the turn's message when the pane's record says nothing", rex.Terminal{
			Status: rex.List[rex.StatusRecord]{rec("done", "", "Done", time.Minute),
				{ID: "turn", State: "done", Msg: "Navbar fixed ⏎ more", UpdatedAt: now}}},
			Agent{App: "claude", State: "done", Msg: "Navbar fixed", Since: now.Add(-time.Minute)}},
		{"the pane's own message first", rex.Terminal{
			Status: rex.List[rex.StatusRecord]{rec("done", "", "Tests pass", time.Minute),
				{ID: "turn", State: "done", Msg: "Navbar fixed", UpdatedAt: now}}},
			Agent{App: "claude", State: "done", Msg: "Tests pass", Since: now.Add(-time.Minute)}},
		{"a stale working report with an idle title is doubted", rex.Terminal{Title: "✳ Claude Code",
			Status: rex.List[rex.StatusRecord]{rec("working", "", "", 3*time.Minute)}},
			Agent{App: "claude", State: "idle", Since: now.Add(-3 * time.Minute), Guessed: true}},
		{"a stale working report with a shell in front is doubted", rex.Terminal{Process: proc("-zsh", "/bin/zsh", ""),
			Status: rex.List[rex.StatusRecord]{rec("working", "", "", 3*time.Minute)}},
			Agent{App: "claude", State: "idle", Since: now.Add(-3 * time.Minute), Guessed: true}},
		{"a fresh working report is trusted", rex.Terminal{Title: "✳ Claude Code",
			Status: rex.List[rex.StatusRecord]{rec("working", "", "", time.Minute)}},
			Agent{App: "claude", State: "working", Since: now.Add(-time.Minute)}},
		{"no report: idle from the title", rex.Terminal{Process: proc("2.1.310", "/x/versions/2.1.310", ""), Title: "✳ Fix the navbar"},
			Agent{App: "claude", State: "idle", Title: "Fix the navbar", Guessed: true}},
		{"no report: working behind a spinner", rex.Terminal{Process: proc("codex", "/bin/codex", ""), Title: "⠙ Port the handlers"},
			Agent{App: "codex", State: "working", Title: "Port the handlers", Guessed: true}},
		{"no report: a half moon spins too", rex.Terminal{Process: proc("pi", "/bin/pi", ""), Title: "◑ thinking"},
			Agent{App: "pi", State: "working", Title: "thinking", Guessed: true}},
		{"no report, no glyph: unknown", rex.Terminal{Process: proc("codex", "/bin/codex", ""), Title: "codex"},
			Agent{App: "codex"}},
		{"no agent at all", rex.Terminal{Process: proc("zsh", "/bin/zsh", ""), Title: "✳ looks idle"}, Agent{}},
	}
	for _, tt := range tests {
		if got := Read(&tt.term, now); got != tt.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
}
