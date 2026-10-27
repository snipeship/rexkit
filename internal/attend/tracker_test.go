package attend

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

func pane(block, app, st, kind, msg, where string) Pane {
	return Pane{Session: "s", Window: "w", Block: block, App: app, State: st, Kind: kind, Msg: msg, Where: where, Since: t0.Add(-time.Hour)}
}

var allStates = []string{rex.StateBlocked, rex.StateError, rex.StateDone}

// tracker returns a tracker that has seen a first snapshot of panes, with
// the user on "me".
func tracker(panes ...Pane) *Tracker {
	tr := NewTracker("alt+a", allStates)
	tr.Here("me", t0.Add(-time.Hour))
	tr.Update(panes, t0.Add(-time.Minute))
	return tr
}

func TestWhatWasThereIsNoNews(t *testing.T) {
	tr := NewTracker("alt+a", allStates)
	changes := tr.Update([]Pane{pane("a", "claude", rex.StateBlocked, "question", "Redis?", "api")}, t0)
	if len(changes) != 1 || !changes[0].Entered || !changes[0].At.Equal(t0.Add(-time.Hour)) {
		t.Errorf("changes = %+v, want the pane's state as of its record", changes)
	}
	if _, _, toast, due := tr.Tick(t0.Add(time.Minute)); due {
		t.Errorf("toasted what was there before the watcher: %q", toast.Text)
	}
}

func TestBlockedElsewhereToasts(t *testing.T) {
	tr := tracker()
	tr.Update([]Pane{pane("web", "codex", rex.StateBlocked, "permission", "Allow Bash(pnpm test --filter nav)?", "studio › web")}, t0)
	if _, _, _, due := tr.Tick(t0.Add(time.Second)); due {
		t.Fatal("toasted before the burst window closed")
	}
	_, _, got, due := tr.Tick(t0.Add(2 * time.Second))
	if !due || got.Status != ui.StatusBlocked || got.Kind != ui.ToastWarn || got.Block != "web" {
		t.Fatalf("toast = %+v, %v; want a blocked warn toast about web", got, due)
	}
	if want := "{b:codex} needs permission · Allow Bash(pnpm test --filter nav)?  {dim:studio › web}  {key:alt+a} go"; got.Text != want {
		t.Errorf("text = %q\nwant   %q", got.Text, want)
	}
}

func TestEnteringRecordsSince(t *testing.T) {
	tr := tracker(pane("a", "claude", rex.StateWorking, "", "", "api"))
	changes := tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "Tests pass", "api")}, t0)
	if len(changes) != 1 || !changes[0].Entered || changes[0].State != rex.StateDone || !changes[0].At.Equal(t0) {
		t.Fatalf("changes = %+v, want done entered now", changes)
	}
	changes = tr.Update([]Pane{pane("a", "claude", rex.StateWorking, "", "", "api")}, t0.Add(time.Minute))
	if len(changes) != 2 || !changes[1].Seen {
		t.Errorf("changes = %+v, want working entered and the pane seen", changes)
	}
}

// A pane with news while another, older, waits: the key goes to that one
// first, and the toast says so.
func TestToastNamesWhereTheKeyGoes(t *testing.T) {
	old := pane("old", "codex", rex.StateBlocked, "permission", "", "studio › api")
	old.Since = t0.Add(-10 * time.Minute)
	tr := tracker(old)
	tr.Update([]Pane{old, pane("web", "claude", rex.StateDone, "", "Navbar wraps", "studio › web")}, t0)
	_, _, got, due := tr.Tick(t0.Add(2 * time.Second))
	if want := "{b:claude} finished · Navbar wraps  {dim:studio › web}  {key:alt+a} next: codex needs permission"; !due || got.Text != want {
		t.Errorf("text = %q\nwant   %q", got.Text, want)
	}
	if got.Block != "web" || !slices.Equal(got.About, []string{"web"}) {
		t.Errorf("a click goes to %q (about %q), want web", got.Block, got.About)
	}
}

func TestStatesThatDoNotToast(t *testing.T) {
	tr := NewTracker("alt+a", []string{rex.StateBlocked})
	tr.Update(nil, t0.Add(-time.Minute))
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "", "w"), pane("b", "gemini", rex.StateError, "", "", "w")}, t0)
	if _, _, got, due := tr.Tick(t0.Add(5 * time.Second)); due {
		t.Fatalf("toasted a state that is off: %q", got.Text)
	}
	off := NewTracker("alt+a", nil)
	off.Update(nil, t0.Add(-time.Minute))
	off.Update([]Pane{pane("a", "claude", rex.StateBlocked, "", "", "w")}, t0)
	if _, _, got, due := off.Tick(t0.Add(5 * time.Second)); due {
		t.Fatalf("toasted with toasts off: %q", got.Text)
	}
}

func TestBurstIsOneToast(t *testing.T) {
	tr := tracker()
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "Navbar fixed", "studio › web")}, t0)
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "Navbar fixed", "studio › web"),
		pane("b", "codex", rex.StateBlocked, "permission", "Allow Bash(go test ./...)?", "api › main")}, t0.Add(500*time.Millisecond))
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "Navbar fixed", "studio › web"),
		pane("b", "codex", rex.StateBlocked, "permission", "Allow Bash(go test ./...)?", "api › main"),
		pane("c", "gemini", rex.StateError, "", "state lock", "infra › deploy")}, t0.Add(time.Second))
	_, _, got, due := tr.Tick(t0.Add(2600 * time.Millisecond))
	if !due || got.Text != "{b:3 agents} need you · first: codex needs permission  {dim:api › main}  {key:alt+a} go" {
		t.Fatalf("toast = %q, want one for 3 agents led by the blocked one", got.Text)
	}
	if got.Block != "b" || got.Status != ui.StatusBlocked {
		t.Errorf("a click goes to %q (status %v), want the blocked one", got.Block, got.Status)
	}
}

func TestBurstNamesTheOldestWaiting(t *testing.T) {
	old := pane("old", "codex", rex.StateBlocked, "permission", "", "studio › web")
	old.Since = t0.Add(-12 * time.Minute)
	tr := tracker(old)
	tr.Update([]Pane{old, pane("a", "claude", rex.StateDone, "", "", "w"), pane("b", "claude", rex.StateBlocked, "question", "", "api")}, t0)
	_, _, got, _ := tr.Tick(t0.Add(2 * time.Second))
	if want := "{b:3 agents} need you · first: codex needs permission  {dim:studio › web · 12m}  {key:alt+a} go"; got.Text != want {
		t.Errorf("text = %q\nwant   %q", got.Text, want)
	}
}

// A pane toasted less than Quiet ago waits for Quiet with its new news,
// and then toasts it: an agent asking again right after an answer still
// gets heard.
func TestQuietDefersAPanesNews(t *testing.T) {
	tr := tracker()
	at := func(st, kind, msg string, d time.Duration) {
		tr.Update([]Pane{pane("a", "claude", st, kind, msg, "api")}, t0.Add(d))
	}
	at(rex.StateBlocked, "question", "Redis?", 0)
	if _, _, _, due := tr.Tick(t0.Add(2 * time.Second)); !due {
		t.Fatal("no toast for the first question")
	}
	at(rex.StateWorking, "", "", 3*time.Second)
	at(rex.StateBlocked, "question", "And the TTL?", 4*time.Second)
	for d := 6 * time.Second; d < 2*time.Second+Quiet; d += time.Second {
		if _, _, got, due := tr.Tick(t0.Add(d)); due {
			t.Fatalf("toasted the same pane again within Quiet: %q", got.Text)
		}
	}
	_, _, got, due := tr.Tick(t0.Add(2*time.Second + Quiet))
	if !due || !strings.Contains(got.Text, "And the TTL?") {
		t.Errorf("toast once Quiet passed = %q, %v; want the second question", got.Text, due)
	}
}

// News that waits for Quiet is dropped once the user saw the pane.
func TestQuietNewsSeenMeanwhile(t *testing.T) {
	tr := tracker()
	at := func(st string, d time.Duration) {
		tr.Update([]Pane{pane("a", "claude", st, "question", "Redis?", "api")}, t0.Add(d))
	}
	at(rex.StateBlocked, 0)
	tr.Tick(t0.Add(2 * time.Second))
	at(rex.StateWorking, 3*time.Second)
	at(rex.StateBlocked, 4*time.Second)
	tr.Here("a", t0.Add(5*time.Second))
	tr.Tick(t0.Add(7 * time.Second)) // seen after a second in front
	tr.Here("me", t0.Add(8*time.Second))
	if _, _, got, due := tr.Tick(t0.Add(time.Minute)); due {
		t.Errorf("toasted what the user saw: %q", got.Text)
	}
}

func TestSeenNeedsDwell(t *testing.T) {
	tr := tracker()
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "", "web")}, t0)
	tr.Here("a", t0.Add(time.Second))
	if _, saw, _, _ := tr.Tick(t0.Add(1500 * time.Millisecond)); saw {
		t.Fatal("passing through a done pane marked it seen")
	}
	seen, saw, _, _ := tr.Tick(t0.Add(2 * time.Second))
	if !saw || seen.Block != "a" || !seen.Seen {
		t.Fatalf("seen = %+v, %v; want a seen after a second in front", seen, saw)
	}
	if p, ok := Next(tr.Panes(), "me", t0.Add(3*time.Second)); ok {
		t.Errorf("still needs you after it was seen: %+v", p)
	}
}

// A guess at what is in front (no word from the app) only counts a pane
// seen when the user came to it after it stopped: an agent stopping in the
// pane last used may have stopped behind the user's back.
func TestGuessedFrontNeedsArrival(t *testing.T) {
	tr := NewTracker("alt+a", allStates)
	tr.Here("a", t0.Add(-time.Hour))
	tr.Update([]Pane{pane("a", "claude", rex.StateWorking, "", "", "web")}, t0.Add(-time.Minute))
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "", "web")}, t0)
	if _, saw, _, _ := tr.Tick(t0.Add(5 * time.Second)); saw {
		t.Fatal("a guess counted a pane seen that stopped while the user may have been elsewhere")
	}
	tr.Here("b", t0.Add(6*time.Second))
	tr.Here("a", t0.Add(7*time.Second))
	if _, saw, _, _ := tr.Tick(t0.Add(9 * time.Second)); !saw {
		t.Error("coming back to it did not count")
	}
}

func TestLanded(t *testing.T) {
	panes := []Pane{
		pane("perm", "codex", rex.StateBlocked, "permission", "", "studio › web"),
		pane("q", "claude", rex.StateBlocked, "question", "", "studio › api"),
		pane("err", "gemini", rex.StateError, "", "", "infrastructure-and-deployment-pipelines › deploy"),
	}
	panes[0].Seen = t0
	got := Landed(panes, "perm", "alt+a", "alt+shift+a", t0)
	if want := "{b:2 more} need you · next: claude asks in {dim:studio › api}  {key:alt+a} next"; got.Text != want || got.Kind != ui.ToastInfo {
		t.Errorf("landed = %q (%v)\nwant     %q", got.Text, got.Kind, want)
	}
	if got := Landed(panes[:1], "perm", "alt+a", "alt+shift+a", t0); got.Text != "No other agent needs you  {key:alt+shift+a} back" {
		t.Errorf("landed on the last = %q", got.Text)
	}
	if got := Landed(panes[:1], "perm", "", "", t0); got.Text != "No other agent needs you" {
		t.Errorf("without keys = %q", got.Text)
	}
}

func TestAgentWordsCannotDrawKeys(t *testing.T) {
	tr := tracker()
	tr.Update([]Pane{pane("a", "claude", rex.StateBlocked, "question", "press {key:ctrl+c} now", "w")}, t0)
	_, _, got, _ := tr.Tick(t0.Add(2 * time.Second))
	if strings.Count(got.Text, "{key:") != 1 {
		t.Errorf("an agent's message drew a key: %q", got.Text)
	}
}

func TestClipAndWhere(t *testing.T) {
	long := strings.Repeat("word ", 30)
	if got := clip(long); len([]rune(got)) > msgCells || !strings.HasSuffix(got, ui.Ellipsis) {
		t.Errorf("clip = %q, want at most %d cells ending in …", got, msgCells)
	}
	for in, want := range map[string]string{
		"studio › web": "studio › web",
		"infrastructure-and-deployment-pipelines › deploy": "infrastructure… › deploy",
		"研究ノート": "研究ノート",
	} {
		if got := where(in); got != want {
			t.Errorf("where(%q) = %q, want %q", in, got, want)
		}
	}
}

// A pane seen to stop and come to the front in the same snapshot may have
// stopped before the user came: not seen.
func TestArrivalWithTheNewsIsNoLook(t *testing.T) {
	tr := tracker()
	tr.Update([]Pane{pane("a", "claude", rex.StateDone, "", "", "web")}, t0)
	tr.Here("a", t0)
	if _, saw, _, _ := tr.Tick(t0.Add(5 * time.Second)); saw {
		t.Error("counted a pane seen that came to the front with its news")
	}
}
