package switcher

import (
	"path/filepath"
	"strconv"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// agentPrograms are executables known to be coding agents, matched by
// basename (never by substring of a path: /Users/campbell/bin/zsh is no amp).
var agentPrograms = map[string]bool{
	"claude": true, "codex": true, "aider": true, "opencode": true, "gemini": true,
	"goose": true, "amp": true, "cursor-agent": true,
}

// terminalState is what the switcher reads from a terminal block.
type terminalState struct {
	program string
	cwd     string // as the process reports it: not sanitized yet
	agent   bool
	status  ui.Status
	task    string
	title   string
	since   time.Time
	exit    string
	// where the sanitizer wrote stand-ins into program, task and title
	programEsc, taskEsc, titleEsc escapes
}

// readTerminal interprets a terminal's process and program status.
func readTerminal(t *rex.Terminal) terminalState {
	var s terminalState
	if t == nil {
		return s
	}
	s.title, s.titleEsc = sanitize.LabelSpans(t.Title)
	if t.Process != nil {
		if p := t.Process.Current(); p != nil {
			s.program, s.programEsc = sanitize.LabelSpans(programName(p))
			s.cwd = p.Cwd
			s.agent = agentPrograms[s.program]
		}
		if e := t.Process.LastExit; e != nil {
			s.status = ui.StatusExited
			s.exit = "exit " + strconv.Itoa(e.Code)
		}
	}
	if rec, ok := primaryRecord(t.Status); ok {
		s.agent = true
		if app := firstNonEmpty(rec.EffectiveApp, rec.App); app != "" {
			s.program, s.programEsc = sanitize.LabelSpans(app)
		}
		s.status = statusOf(rec.State)
		s.task, s.taskEsc = sanitize.LabelSpans(firstNonEmpty(rec.Title, rec.Msg))
		s.since = rec.UpdatedAt
	}
	return s
}

// programName is the basename of what was run: the invoked path keeps the
// name a user typed (claude) when the binary is a shared runtime (node).
func programName(p *rex.ProcessInfo) string {
	return filepath.Base(firstNonEmpty(p.InvokedPath, p.Path, p.Name))
}

// urgency orders program states: what needs the user first.
var urgency = map[string]int{
	rex.StateBlocked: 0, rex.StateError: 1, rex.StateWorking: 2, rex.StateDone: 3, rex.StateIdle: 4,
}

// primaryRecord picks the record that best describes a terminal: the most
// urgent state, then the most recent update.
func primaryRecord(records []rex.StatusRecord) (rex.StatusRecord, bool) {
	best, found := rex.StatusRecord{}, false
	rank := func(r rex.StatusRecord) int {
		if u, ok := urgency[r.State]; ok {
			return u
		}
		return len(urgency)
	}
	for _, r := range records {
		if !found || rank(r) < rank(best) || rank(r) == rank(best) && r.UpdatedAt.After(best.UpdatedAt) {
			best, found = r, true
		}
	}
	return best, found
}

func statusOf(state string) ui.Status {
	switch state {
	case rex.StateBlocked:
		return ui.StatusBlocked
	case rex.StateError:
		return ui.StatusFailed
	case rex.StateWorking:
		return ui.StatusWorking
	}
	return ui.StatusIdle
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
