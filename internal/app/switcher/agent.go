package switcher

import (
	"strconv"
	"time"

	"rexkit/internal/attend"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// terminalState is what the switcher reads from a terminal block.
type terminalState struct {
	program string
	cwd     string // as the process reports it: not sanitized yet
	agent   bool
	state   string // the agent's OSC 7501 state, for attention (attend)
	status  ui.Status
	guess   bool // status is guessed (attend.Agent.Guessed)
	task    string
	ask     string // what the agent says to the user, first line (attend.Message)
	askKind string // why a blocked agent waits: permission, question or auth
	title   string
	since   time.Time
	exit    string
	// where the sanitizer wrote stand-ins into program, task and title
	programEsc, taskEsc, titleEsc escapes
}

// readTerminal interprets a terminal's process and program status.
func readTerminal(t *rex.Terminal, now time.Time) terminalState {
	var s terminalState
	if t == nil {
		return s
	}
	s.title, s.titleEsc = sanitize.LabelSpans(t.Title)
	if t.Process != nil {
		if p := t.Process.Current(); p != nil {
			name, agent := attend.Program(p)
			s.program, s.programEsc = sanitize.LabelSpans(name)
			s.cwd = p.Cwd
			s.agent = agent
		}
		if e := t.Process.LastExit; e != nil {
			s.status = ui.StatusExited
			s.exit = "exit " + strconv.Itoa(e.Code)
		}
	}
	a := attend.Read(t, now)
	switch {
	case !a.Since.IsZero(): // it reports
		s.agent = true
		s.program, s.programEsc = sanitize.LabelSpans(a.App)
		s.task, s.taskEsc = sanitize.LabelSpans(firstNonEmpty(a.Title, a.Msg))
		s.since = a.Since
		s.ask, s.askKind = sanitize.Label(a.Msg), a.Kind
	case a.State == "" || s.status == ui.StatusExited:
		return s
	default: // guessed from the title, which says what it works on
		s.task, s.taskEsc = sanitize.LabelSpans(a.Title)
	}
	s.state, s.status, s.guess = a.State, attend.StatusOf(a.State), a.Guessed
	return s
}

// kindWords are the words a blocked agent's status column shows for the
// kinds OSC 7501 defines.
var kindWords = map[string]string{"permission": "permission", "question": "question", "auth": "sign-in"}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
