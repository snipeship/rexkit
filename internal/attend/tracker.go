package attend

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Tunables.
const (
	// SeenDwell is how long a pane must stay in front before what it says
	// counts as seen: passing through it on the way elsewhere does not.
	SeenDwell = time.Second
	// Burst is how long the tracker waits for more news before it toasts:
	// agents that stop together get one toast.
	Burst = 1500 * time.Millisecond
	// Quiet is the least time between two toasts about the same pane.
	Quiet = 30 * time.Second
	// msgCells bounds an agent's message in a toast, so what follows it
	// (where, the key) stays.
	msgCells = 64
	// sessionCells bounds the session in a toast's place: a long name
	// ("infrastructure-and-deployment-pipelines") keeps its start.
	sessionCells = 16
)

// Toast is a toast the watcher shows: rexkit toast --kind --status
// --message.
type Toast struct {
	Kind   ui.ToastKind
	Status ui.Status
	Text   string
	Block  string   // the pane a click goes to: the one it is about, or the first of several
	About  []string // the panes that began to need the user
}

// Tracker is the watcher's memory between snapshots: what each agent pane
// was doing, which one is likely in front, and what is still to be toasted.
type Tracker struct {
	// NextKey is the key that goes to the next agent (agent_key, else
	// alt+a, else ctrl+o a); toasts name it. Empty: they name none.
	NextKey string
	// Toasts are the states that toast (rex.StateBlocked, StateError,
	// StateDone); nil toasts none.
	Toasts map[string]bool
	// Held are the panes that are in no tab now and that Panes does not
	// list (a hidden floating pane whose record is gone, a pane on its way
	// back): the tracker keeps what it knew of them for when they show
	// again, and toasts none of them meanwhile.
	Held map[string]bool

	panes   map[string]Pane // by block, as of the last Update
	started bool            // the first Update is the state of things, not news
	here    string          // the pane likely in front: the watcher's guess
	hereAt  time.Time       // since when
	news    []string        // panes that began to need the user, not toasted yet
	newsAt  time.Time
	later   []string // news about panes toasted less than Quiet ago
	toasted map[string]time.Time
}

// NewTracker returns a tracker that toasts the given states.
func NewTracker(nextKey string, toasts []string) *Tracker {
	t := &Tracker{NextKey: nextKey, Toasts: map[string]bool{}, panes: map[string]Pane{}, toasted: map[string]time.Time{}}
	for _, s := range toasts {
		t.Toasts[s] = true
	}
	return t
}

// Change is something to remember about a pane (state.Attention): it
// entered its state (Entered) or the user saw it (Seen).
type Change struct {
	Block   string
	Entered bool // State and Kind began at At
	State   string
	Kind    string
	Seen    bool // the user looked at it at At
	At      time.Time
}

// Update takes a snapshot's panes (Panes) and returns what to remember:
// the panes that changed state, and those seen because their agent went
// back to work. A pane that begins to need the user is news for the next
// toast (the watcher leaves it out of the session where that pane is in
// front). A pane in Held that is missing keeps its entry.
func (t *Tracker) Update(panes []Pane, now time.Time) []Change {
	var changes []Change
	current := make(map[string]Pane, len(panes))
	for _, p := range panes {
		old, had := t.panes[p.Block]
		switch {
		case !had && !t.started:
			// what was there before the watcher: keep its time
			changes = append(changes, Change{Block: p.Block, Entered: true, State: p.State, Kind: p.Kind, At: p.Since})
		case !had || old.State != p.State || old.Kind != p.Kind:
			p.Since = now
			changes = append(changes, Change{Block: p.Block, Entered: true, State: p.State, Kind: p.Kind, At: now})
			if p.State == rex.StateWorking {
				p.Seen = now
				changes = append(changes, Change{Block: p.Block, Seen: true, At: now})
			}
			if NeedsYou(p.State, true) && t.Toasts[p.State] {
				t.addNews(p.Block, now)
			}
		default:
			p.Since = old.Since // as the tracker saw it begin
			if old.Seen.After(p.Seen) {
				p.Seen = old.Seen // seen here before the store caught up
			}
		}
		current[p.Block] = p
	}
	for b, old := range t.panes {
		if _, ok := current[b]; !ok && t.Held[b] {
			current[b] = old
		}
	}
	t.panes, t.started = current, true
	gone := func(b string) bool { _, ok := current[b]; return !ok || t.Held[b] }
	t.news, t.later = slices.DeleteFunc(t.news, gone), slices.DeleteFunc(t.later, gone)
	for b := range t.toasted {
		if _, ok := current[b]; !ok {
			delete(t.toasted, b)
		}
	}
	return changes
}

func (t *Tracker) addNews(block string, now time.Time) {
	if slices.Contains(t.news, block) {
		return
	}
	if len(t.news) == 0 {
		t.newsAt = now
	}
	t.news = append(t.news, block)
}

// Here records the pane the watcher guesses is in front: the focused pane
// of the session where the focus last moved. Rex does not say which
// session the app shows, so it is only a guess, and it keeps the news.
func (t *Tracker) Here(block string, now time.Time) {
	if block == t.here {
		return
	}
	t.here, t.hereAt = block, now
}

// Tick returns the pane in front if it has now been seen, and the toast
// due, if any. A pane counts as seen after SeenDwell in front, and only if
// it came to the front after it began to need the user: an agent that
// stops in the pane last used may have stopped behind the user's back (and
// one seen to stop and come to the front at once may have, too).
// News about a pane toasted less than Quiet ago waits for Quiet to pass,
// and is dropped if the user saw the pane meanwhile.
func (t *Tracker) Tick(now time.Time) (seen Change, saw bool, toast Toast, due bool) {
	if p, ok := t.panes[t.here]; ok && p.Unseen(now) && now.Sub(t.hereAt) >= SeenDwell && t.hereAt.After(p.Since) {
		p.Seen = now
		t.panes[t.here] = p
		seen, saw = Change{Block: p.Block, Seen: true, At: now}, true
	}
	var ready []string
	if len(t.news) > 0 && now.Sub(t.newsAt) >= Burst {
		ready, t.news = t.news, nil
	}
	var fresh []Pane
	var later []string
	looked := map[string]bool{}
	for _, b := range slices.Concat(t.later, ready) {
		if looked[b] {
			continue
		}
		looked[b] = true
		p, ok := t.panes[b]
		switch {
		case !ok || !p.Unseen(now): // gone, or the user saw it meanwhile
		case now.Sub(t.toasted[b]) < Quiet:
			later = append(later, b)
		default:
			fresh = append(fresh, p)
			t.toasted[b] = now
		}
	}
	t.later = later
	if len(fresh) == 0 {
		return seen, saw, Toast{}, false
	}
	return seen, saw, t.toastFor(fresh, now), true
}

// Panes is every agent pane as of the last Update, with what the tracker
// saw since.
func (t *Tracker) Panes() []Pane {
	out := make([]Pane, 0, len(t.panes))
	for _, p := range t.panes {
		out = append(out, p)
	}
	return out
}

// toastFor words the toast for the panes that just began to need the user.
func (t *Tracker) toastFor(fresh []Pane, now time.Time) Toast {
	slices.SortStableFunc(fresh, func(a, b Pane) int { return Rank(a.State) - Rank(b.State) })
	p := fresh[0]
	if len(fresh) > 1 {
		// The key goes to the first of them all (Next), so say who that is,
		// and how long it has waited when that is a while.
		all := Waiting(t.Panes(), now)
		first, _ := Next(all, t.here, now)
		place := esc(where(first.Where))
		if now.Sub(first.Since) >= time.Minute {
			place += " · " + ui.RelTimeAt(first.Since, now)
		}
		about := make([]string, len(fresh))
		for i, f := range fresh {
			about[i] = f.Block
		}
		return Toast{Kind: ui.ToastWarn, Status: StatusOf(p.State), Block: first.Block, About: about, Text: fmt.Sprintf(
			"{b:%d agents} need you · first: %s %s  {dim:%s}%s", len(all), esc(first.App), verb(first),
			place, keyHint(t.NextKey, "go"))}
	}
	text := "{b:" + esc(p.App) + "} " + verb(p)
	if msg := clip(p.Msg); msg != "" {
		text += " · " + esc(msg)
	}
	text += "  {dim:" + esc(where(p.Where)) + "}"
	// The key goes to the first that needs the user, which may be another
	// agent that has waited longer: then say which.
	if first, ok := Next(Waiting(t.Panes(), now), t.here, now); ok && first.Block != p.Block {
		text += keyHint(t.NextKey, "next: "+esc(first.App)+" "+verb(first))
	} else {
		text += keyHint(t.NextKey, "go")
	}
	return Toast{Kind: toastKind(p.State), Status: StatusOf(p.State), Text: text, Block: p.Block, About: []string{p.Block}}
}

// Landed is the toast after ⌥A went to block: how many more need the user,
// and where the key goes next; or, when none is left, that none is (and
// backKey goes back).
func Landed(panes []Pane, block, nextKey, backKey string, now time.Time) Toast {
	var rest []Pane
	for _, p := range Waiting(panes, now) {
		if p.Block != block {
			rest = append(rest, p)
		}
	}
	if len(rest) == 0 {
		return Toast{Kind: ui.ToastOK, Text: "No other agent needs you" + keyHint(backKey, "back")}
	}
	n, _ := Next(panes, block, now)
	more := "{b:1 more} needs you"
	if len(rest) > 1 {
		more = fmt.Sprintf("{b:%d more} need you", len(rest))
	}
	return Toast{Kind: ui.ToastInfo, Text: more + " · next: " + esc(n.App) + " " + verb(n) +
		" in {dim:" + esc(where(n.Where)) + "}" + keyHint(nextKey, "next")}
}

// keyHint is "  {key:ctrl+shift+a} go", or nothing without a key.
func keyHint(key, label string) string {
	if key == "" {
		return ""
	}
	return "  {key:" + esc(key) + "} " + label
}

// verb says what happened, in a toast's words.
func verb(p Pane) string {
	switch {
	case p.State == rex.StateBlocked && p.Kind == "question":
		return "asks"
	case p.State == rex.StateBlocked && p.Kind == "auth":
		return "needs you to sign in"
	case p.State == rex.StateBlocked && p.Kind == "permission":
		return "needs permission"
	case p.State == rex.StateBlocked:
		return "waits for you"
	case p.State == rex.StateError:
		return "failed"
	}
	return "finished"
}

// StatusOf is the status a toast or row shows for an OSC 7501 state.
func StatusOf(st string) ui.Status {
	switch st {
	case rex.StateBlocked:
		return ui.StatusBlocked
	case rex.StateError:
		return ui.StatusFailed
	case rex.StateWorking:
		return ui.StatusWorking
	case rex.StateDone:
		return ui.StatusDone
	}
	return ui.StatusIdle
}

func toastKind(st string) ui.ToastKind {
	switch st {
	case rex.StateBlocked:
		return ui.ToastWarn
	case rex.StateError:
		return ui.ToastErr
	}
	return ui.ToastOK
}

// esc keeps an agent's words from drawing marks (keys) in a toast.
func esc(s string) string { return ui.ToastEscape(s) }

// clip cuts a message to msgCells, so what follows it in a toast stays.
func clip(msg string) string {
	r := []rune(strings.TrimSpace(msg))
	if len(r) > msgCells {
		return strings.TrimRight(string(r[:msgCells-1]), " ") + ui.Ellipsis
	}
	return string(r)
}

// where is a place for a toast, "session › tab", its session cut to
// sessionCells ("infrastructure… › deploy").
func where(w string) string {
	session, tab, ok := strings.Cut(w, " › ")
	if r := []rune(session); len(r) > sessionCells {
		session = strings.TrimRight(string(r[:sessionCells-1]), "-_. ") + ui.Ellipsis
	}
	if !ok {
		return session
	}
	return session + " › " + tab
}
