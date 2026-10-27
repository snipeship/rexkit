// Package attend is rexkit's agent attention: which coding agents need the
// user (blocked, failed, or done with a result not seen yet), in what order
// ⌥A walks them, and what the watcher (rexkit attend) toasts about them.
//
// Agents report their state with OSC 7501 program status records. Agents
// that report nothing are recognised by their program and given a state
// guessed from their terminal title; a guess never makes an agent need you.
package attend

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"rexkit/internal/rex"
)

// DoubtAfter is how old a working report must be before an idle-looking
// terminal (an idle title, a shell back in front) makes it doubtful: the
// agent probably stopped without saying so.
const DoubtAfter = 120 * time.Second

// Agent is what a terminal says about the coding agent in it.
type Agent struct {
	App   string // claude, codex…; "" when the terminal runs no agent
	State string // an OSC 7501 state (rex.State*), or "" when unknown
	// Kind is why a blocked agent waits: permission, question or auth.
	Kind  string
	Msg   string    // the first line of what it says to the user, filler dropped; not sanitized
	Title string    // what it works on, as its record (or its title, after the glyph) says
	Since time.Time // when its record last changed; zero without a record
	// Guessed: State comes from the terminal's title, or is a working
	// report the terminal contradicts (shown as idle).
	Guessed bool
}

// agentPrograms are executables known to be coding agents, matched by
// basename (never by substring of a path: /Users/campbell/bin/zsh is no amp).
var agentPrograms = map[string]bool{
	"claude": true, "codex": true, "aider": true, "opencode": true, "gemini": true,
	"goose": true, "amp": true, "cursor-agent": true, "pi": true, "grok": true,
}

// claudeVersion is how Claude Code's process is named: its version.
var claudeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// shells are programs whose return to the front means the agent before
// them is gone.
var shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "nu": true}

// Program is the name of what a process runs, as the user knows it, and
// whether it is a coding agent. The invoked path keeps the name a user
// typed (claude) when the binary is a shared runtime (node).
func Program(p *rex.ProcessInfo) (name string, agent bool) {
	if p == nil {
		return "", false
	}
	for _, path := range []string{p.InvokedPath, p.Path, p.Name} {
		switch base := filepath.Base(path); {
		case path == "":
		case agentPrograms[base], strings.HasPrefix(base, "grok-"):
			return base, true
		case claudeVersion.MatchString(base):
			return "claude", true
		}
	}
	for _, path := range []string{p.InvokedPath, p.Path, p.Name} {
		if path != "" {
			return filepath.Base(path), false
		}
	}
	return "", false
}

// Read interprets a terminal: the agent in it, if any, and its state.
func Read(t *rex.Terminal, now time.Time) Agent {
	var a Agent
	if t == nil {
		return a
	}
	front := ""
	if t.Process != nil {
		name, agent := Program(t.Process.Current())
		front = strings.TrimPrefix(name, "-") // a login shell: -zsh
		if agent {
			a.App = name
		}
	}
	rec, ok := Primary(t.Status)
	if !ok {
		if a.App != "" {
			a.State = fromTitle(t.Title)
			if a.Guessed = a.State != ""; a.Guessed {
				_, size := utf8.DecodeRuneInString(t.Title)
				a.Title = strings.TrimSpace(t.Title[size:]) // after the glyph
			}
		}
		return a
	}
	a.App = firstNonEmpty(rec.EffectiveApp, rec.App, a.App, front)
	a.State, a.Title, a.Since = rec.State, rec.Title, rec.UpdatedAt
	if rec.State == rex.StateBlocked {
		a.Kind = rec.Kind
	}
	a.Msg = Message(rec.Msg)
	if turn, ok := record(t.Status, turnID); ok && a.Msg == "" {
		a.Msg = Message(turn.Msg) // a hook's final message for the turn
	}
	if a.Kind == "question" {
		a.Msg = questionOnly(a.Msg)
	}
	if rec.State == rex.StateWorking && now.Sub(rec.UpdatedAt) > DoubtAfter &&
		(strings.HasPrefix(t.Title, idleTitle) || shells[front]) {
		a.State, a.Guessed = rex.StateIdle, true
	}
	return a
}

// idleTitle starts the title of an agent waiting at its prompt (Claude
// Code's).
const idleTitle = "✳ "

// fromTitle guesses a state from a terminal title: idle at the prompt, or
// working behind a spinner (braille, or a half-filled circle).
func fromTitle(title string) string {
	if strings.HasPrefix(title, idleTitle) {
		return rex.StateIdle
	}
	switch r, _ := utf8.DecodeRuneInString(title); {
	case r >= 0x2800 && r <= 0x28ff, r == '◐', r == '◑':
		return rex.StateWorking
	}
	return ""
}

// turnID is the sub-record some hooks write with a turn's final message:
// words for the pane's own record, not a state of its own.
const turnID = "turn"

// urgency orders program states: what needs the user first.
var urgency = map[string]int{
	rex.StateBlocked: 0, rex.StateError: 1, rex.StateWorking: 2, rex.StateDone: 3, rex.StateIdle: 4,
}

// Primary picks the record that describes a terminal: the pane's own record
// (ID ""), else the most urgent of the others, then the most recent. The
// turn record never counts.
func Primary(records []rex.StatusRecord) (rex.StatusRecord, bool) {
	if own, ok := record(records, ""); ok {
		return own, true
	}
	best, found := rex.StatusRecord{}, false
	rank := func(r rex.StatusRecord) int {
		if u, ok := urgency[r.State]; ok {
			return u
		}
		return len(urgency)
	}
	for _, r := range records {
		if r.ID == turnID {
			continue
		}
		if !found || rank(r) < rank(best) || rank(r) == rank(best) && r.UpdatedAt.After(best.UpdatedAt) {
			best, found = r, true
		}
	}
	return best, found
}

func record(records []rex.StatusRecord, id string) (rex.StatusRecord, bool) {
	for _, r := range records {
		if r.ID == id {
			return r, true
		}
	}
	return rex.StatusRecord{}, false
}

// Message is the first line of a status message (hooks join lines with
// " ⏎ "), or "" for the filler hooks send when they have nothing to say.
func Message(msg string) string {
	msg, _, _ = strings.Cut(msg, "\n")
	msg, _, _ = strings.Cut(msg, " ⏎ ")
	msg = strings.TrimSpace(msg)
	if filler[strings.ToLower(strings.TrimRight(msg, ".!…"))] || bareAllow.MatchString(msg) {
		return ""
	}
	return msg
}

// questionOnly drops the "answer: " Rex puts before a question it reports
// for Claude Code ("answer: Tabs or spaces? (Tabs · Spaces)"): "asks" says
// that already.
func questionOnly(msg string) string {
	if len(msg) > len(answerPrefix) && strings.EqualFold(msg[:len(answerPrefix)], answerPrefix) {
		return strings.TrimSpace(msg[len(answerPrefix):])
	}
	return msg
}

const answerPrefix = "answer:"

// filler are messages that say no more than the state does.
var filler = map[string]bool{
	"": true, "done": true, "finished": true, "working": true, "thinking": true, "idle": true,
	"ready": true, "waiting": true, "needs input": true, "waiting for input": true,
}

// bareAllow is a hook's permission prompt that names only the tool
// ("Allow Bash?"), not what it would run.
var bareAllow = regexp.MustCompile(`^Allow [A-Za-z0-9_.-]+\?$`)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
