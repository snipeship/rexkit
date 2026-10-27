package switcher

import (
	"strconv"
	"time"

	"rexkit/internal/attend"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// entry is one place the switcher can go to: a session, a tab, or a pane.
// An agent is a pane that runs a coding agent or reports program status.
type entry struct {
	kind                         ui.Kind
	sessionID, windowID, blockID string // where going there lands

	name    string // display name, sanitized
	label   string // the label in Rex, raw: what a rename starts from
	session string // name of the session it belongs to
	tab     string // name of the tab it belongs to; "" for sessions
	cwd     string // working directory, ~-abbreviated
	program string // foreground program, or the app reporting status
	task    string // what an agent says it is doing
	title   string // terminal title
	status  ui.Status
	since   time.Time // when status last changed; zero when unknown
	exit    string    // "exit 1" for a pane whose program ended
	state   string    // an agent's OSC 7501 state (status says it for rows)
	guess   bool      // status is a guess: from the title, or a stale working report doubted
	ask     string    // what the agent says to the user: its question, error or result (msg), first line
	askKind string    // why a blocked agent waits: permission, question or auth; "" when it does not say
	unseen  bool      // news the user has not looked at since it came (attend.Unseen)
	// where the sanitizer wrote stand-ins into the labels above
	nameEsc, cwdEsc, programEsc, taskEsc, titleEsc escapes

	depth                    int // tree depth: 0 session, 1 tab, 2 pane
	tabs, panes              int // children of sessions and tabs
	failed, blocked, working int // agents below a session or tab, by state
	done                     int // unseen results below a session or tab
	guessed                  int // agents below only guessed to work: no total counts them
}

// key identifies the entry across refreshes.
func (e entry) key() string {
	switch e.kind {
	case ui.KindSession:
		return e.sessionID
	case ui.KindTab:
		return e.windowID
	}
	return e.blockID
}

// sessionName is the name of the session the entry is in.
func (e entry) sessionName() string {
	if e.kind == ui.KindSession {
		return e.name
	}
	return e.session
}

// pane reports whether the entry is a pane (agents included).
func (e entry) pane() bool { return e.kind == ui.KindPane || e.kind == ui.KindAgent }

// needsYou reports an agent waiting for the user: blocked, failed, or done
// with a result the user has not seen (attend.NeedsYou). A guess never
// needs the user.
func (e entry) needsYou() bool {
	return e.kind == ui.KindAgent && !e.guess && attend.NeedsYou(e.state, e.unseen)
}

// statusWord is the word after the status glyph: a blocked agent says what
// it waits for (permission, question), as the glyph already says blocked;
// a guess says so with a question mark.
func (e entry) statusWord() string {
	word := e.status.Word()
	if w, ok := kindWords[e.askKind]; ok && e.status == ui.StatusBlocked {
		word = w
	}
	if e.guess {
		word += "?"
	}
	return word
}

// path is where the entry lives: "session › tab" for panes, "session" for
// tabs, nothing for sessions.
func (e entry) path() string {
	switch e.kind {
	case ui.KindSession:
		return ""
	case ui.KindTab:
		return e.session
	}
	return e.session + " " + ui.GlyphCrumb + " " + e.tab
}

// counts describes a session's or tab's contents: "2 tabs · 5 panes".
func (e entry) counts() string {
	switch e.kind {
	case ui.KindSession:
		return ui.Plural(e.tabs, "tab") + " " + ui.Dot + " " + ui.Plural(e.panes, "pane")
	case ui.KindTab:
		return ui.Plural(e.panes, "pane")
	}
	return ""
}

// summary is the most urgent agent state below a session or tab, as a status
// and a count in the agent rows' own words ("1 failed", "2 blocked",
// "1 working"); StatusNone when nothing is going on.
func (e entry) summary() (ui.Status, string) {
	switch {
	case e.failed > 0:
		return ui.StatusFailed, strconv.Itoa(e.failed) + " failed"
	case e.blocked > 0:
		return ui.StatusBlocked, strconv.Itoa(e.blocked) + " blocked"
	case e.done > 0:
		return ui.StatusDone, strconv.Itoa(e.done) + " done"
	case e.working > 0:
		return ui.StatusWorking, strconv.Itoa(e.working) + " working"
	}
	return ui.StatusNone, ""
}

// escapesOf returns the escapes of a match field's text (see fieldsOf).
func (e entry) escapesOf(field int) escapes {
	switch field {
	case fieldName:
		return e.nameEsc
	case fieldCwd:
		return e.cwdEsc
	case fieldProgram:
		return e.programEsc
	case fieldTask:
		return e.taskEsc
	case fieldTitle:
		return e.titleEsc
	}
	return nil
}

// kindName names the kind in the visit history.
func (e entry) kindName() string {
	return [...]string{ui.KindSession: "session", ui.KindTab: "tab", ui.KindPane: "pane", ui.KindAgent: "agent"}[e.kind]
}

// visit is the history record of going to the entry.
func (e entry) visit(at time.Time) state.Visit {
	return state.Visit{Kind: e.kindName(), SessionID: e.sessionID, WindowID: e.windowID, BlockID: e.blockID, At: at}
}

// place is a location in Rex, such as where the switcher was opened from.
type place struct {
	sessionID, windowID, blockID string
}

func (p place) visit(at time.Time) state.Visit {
	return state.Visit{Kind: "pane", SessionID: p.sessionID, WindowID: p.windowID, BlockID: p.blockID, At: at}
}

// at reports whether going to e would land exactly at p.
func (p place) at(e entry) bool {
	return p.blockID != "" && e.blockID == p.blockID && e.windowID == p.windowID
}
