package switcher

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// sectionRows returns the names listed under each section title.
func sectionRows(h *harness) map[string][]string {
	out := map[string][]string{}
	for i, r := range h.m.listing.rows {
		title := h.m.sectionOf(i)
		out[title] = append(out[title], h.m.cat.entries[r.entry].name)
	}
	return out
}

func TestSwitcherViewSections(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	var titles []string
	for _, s := range h.m.listing.sections {
		titles = append(titles, s.Title)
	}
	if want := []string{secNeedsYou, secWorking, secRecent, secSessions}; !slices.Equal(titles, want) {
		t.Fatalf("sections = %q, want %q", titles, want)
	}
	rows := sectionRows(h)
	tests := []struct {
		section string
		want    []string
	}{
		// Blocked before failed, though the failed task has waited longer:
		// a blocked agent cannot go on without you.
		{secNeedsYou, []string{"Navigation polish", "Migrate terraform state to the new bucket"}},
		{secWorking, []string{"Revamp the switcher UI", "Fix navbar overflow on small screens"}},
		// The origin (rexkit › spark › zsh) is never "recent".
		{secRecent, []string{"zsh", "要約を書く", "kubectl"}},
	}
	for _, tt := range tests {
		if got := rows[tt.section]; !slices.Equal(got, tt.want) {
			t.Errorf("%s = %q, want %q", tt.section, got, tt.want)
		}
	}
	if got := rows[secSessions][0]; got != "rexkit" {
		t.Errorf("first session = %q, want the origin's", got)
	}
	_, e, _ := h.m.selected()
	if h.m.sectionOf(h.m.list.Cursor()) != secRecent || e.session != "studio" || e.name != "zsh" {
		t.Errorf("selected %q in %q, want the previous location studio › api › zsh", describe(e), h.m.selectedSection())
	}
}

func TestSearchRanking(t *testing.T) {
	tests := []struct {
		query string
		first string
		none  []string // must not be listed
	}{
		{"nav", "Navigation polish", []string{"tail -f production.log (eu-west-1, all services)"}},
		{"fix nav", "Fix navbar overflow on small screens", []string{"Navigation polish"}},
		{"kube", "kubectl", nil},
		{"論文", "論文", nil},
		{"blocked", "Navigation polish", nil},
		{"migrations", "migrations", nil},
		{"notes", "要約を書く", []string{"infrastructure-and-deployment-pipelines", "kubectl", "tail -f production.log (eu-west-1, all services)"}},
		{"kctl", "kubectl", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			h := newHarness(t, 100, 30, true)
			h.typeText(tt.query)
			names := h.rowNames()
			if len(names) == 0 || names[0] != tt.first {
				t.Fatalf("results = %q, want %q first", names, tt.first)
			}
			for _, n := range tt.none {
				if slices.Contains(names, n) {
					t.Errorf("results = %q, should not list %q", names, n)
				}
			}
			if h.m.listing.sections != nil {
				t.Errorf("search results have sections %v", h.m.listing.sections)
			}
		})
	}
}

func TestSearchExplainsHiddenMatches(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	h.typeText("notes")
	r, e, ok := h.m.selected()
	if !ok || e.name != "要約を書く" || r.match.Via != fieldCwd {
		t.Fatalf("selected %q via %d, want the aider pane matched through its cwd", e.name, r.match.Via)
	}
	if got := h.m.context(r, e, 0).String(); got != "in ~/notes/論文" {
		t.Errorf("context = %q, want the hint", got)
	}
	screen := h.screen()
	if !strings.Contains(screen, "in ~/notes/論文") {
		t.Errorf("hint not on screen:\n%s", screen)
	}
}

func TestCategories(t *testing.T) {
	tests := []struct {
		presses int
		kinds   []ui.Kind
	}{
		{1, []ui.Kind{ui.KindSession}},
		{2, []ui.Kind{ui.KindTab}},
		{3, []ui.Kind{ui.KindPane, ui.KindAgent}},
		{4, []ui.Kind{ui.KindAgent}},
	}
	for _, tt := range tests {
		h := newHarness(t, 100, 30, true)
		for range tt.presses {
			h.key("tab")
		}
		for _, r := range h.m.listing.rows {
			if k := h.m.cat.entries[r.entry].kind; !slices.Contains(tt.kinds, k) {
				t.Errorf("%s lists a %v", categoryNames[h.m.filter.cat], k)
			}
		}
	}
	h := newHarness(t, 100, 30, true)
	h.key("shift+tab")
	if h.m.filter.cat != catAgents {
		t.Fatalf("⇧⇥ from All = %s, want Agents", categoryNames[h.m.filter.cat])
	}
	var titles []string
	for _, s := range h.m.listing.sections {
		titles = append(titles, s.Title)
	}
	if want := []string{secNeedsYou, secWorking, secIdle}; !slices.Equal(titles, want) {
		t.Errorf("agent sections = %q, want %q", titles, want)
	}
	if got := sectionRows(h)[secNeedsYou]; !slices.Equal(got, []string{"Navigation polish", "Migrate terraform state to the new bucket"}) {
		t.Errorf("agents needing you = %q, want blocked before failed", got)
	}
	if !strings.Contains(h.screen(), "Agents²") {
		t.Errorf("Agents chip lacks the waiting badge:\n%s", h.screen())
	}
}

func TestRecentSkipsClosedPlacesAndFallsBack(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	kubectl := h.find("kubectl")
	logs := h.find("logs")
	h.m.history.Visits = append(h.m.history.Visits[:1], // the origin
		h.m.history.Visits[3], // kubectl
		entry{sessionID: logs.sessionID, windowID: logs.windowID, blockID: "block:gone"}.visit(testNow), // a closed pane of logs
		entry{sessionID: "session:gone", windowID: "w", blockID: "b"}.visit(testNow),
	)
	h.m.rebuild(selectDefault)
	if got := sectionRows(h)[secRecent]; !slices.Equal(got, []string{kubectl.name, "logs"}) {
		t.Errorf("recent = %q, want kubectl then the tab whose pane closed", got)
	}
}

// The next-agent key (Lua hands it over as ⌥A) walks Needs you's rows,
// round its end, and its header shows the key.
func TestAltAWalksNeedsYou(t *testing.T) {
	h := newHarness(t, 120, 40, true)
	if !strings.Contains(h.screen(), "ctrl shift a▌ next") {
		t.Errorf("Needs you's header lacks the ⌃⇧A hint:\n%s", h.screen())
	}
	var got []string
	for range 3 {
		h.key("alt+a")
		got = append(got, h.selected())
	}
	want := []string{"Navigation polish", "Migrate terraform state to the new bucket", "Navigation polish"}
	if !slices.Equal(got, want) {
		t.Errorf("⌥A selected %q, want %q", got, want)
	}
}

// Needs you's header and the key panel name the next-agent key Lua found
// bound: agent_key, ⌥A or Session mode's a; none without one.
func TestNextKeyNamed(t *testing.T) {
	for _, tt := range []struct{ key, cap string }{
		{"ctrl+shift+a", "ctrl shift a▌"},
		{"alt+a", "alt a▌"},
		{"ctrl+o a", "ctrl o▌ ▐a▌"},
		{"", ""},
	} {
		t.Run(tt.key, func(t *testing.T) {
			h := newHarness(t, 120, 40, true, withNextKey(tt.key))
			header := ""
			for _, line := range strings.Split(h.screen(), "\n") {
				if strings.Contains(line, "Needs you") {
					header = line
				}
			}
			if header == "" {
				t.Fatalf("no Needs you header:\n%s", h.screen())
			}
			if want := tt.cap + " next"; tt.key == "" && strings.Contains(header, "next") ||
				tt.key != "" && !strings.Contains(header, want) {
				t.Errorf("header %q, want %q", strings.TrimSpace(header), want)
			}
			h.key("?")
			panel := h.screen()
			listed := strings.Contains(panel, "Next agent that needs you")
			if listed != (tt.key != "") || !strings.Contains(panel, tt.cap) {
				t.Errorf("key panel with %q:\n%s", tt.key, panel)
			}
		})
	}
}

// ⌥A says why it selects nothing: nothing needs you, or the search or the
// filter hides Needs you.
func TestAltAWithNoNeedsYou(t *testing.T) {
	h := newHarness(t, 120, 40, true, withWorld(attentionWorld))
	h.typeText("terraform")
	h.key("alt+a")
	if got := h.m.toast.Current().Text; got != "Clear the search to see Needs you" {
		t.Errorf("⌥A in a search: toast %q", got)
	}
	for range len("terraform") {
		h.key("backspace")
	}
	h.key("tab") // Sessions
	h.key("alt+a")
	if got := h.m.toast.Current().Text; got != "Needs you is under All and Agents" {
		t.Errorf("⌥A under Sessions: toast %q", got)
	}

	calm := newHarness(t, 120, 40, true, withWorld(func(now time.Time) (*rexfake.Fake, place, state.History) {
		return buildDemo(now, []demoSession{{"work", []demoTab{{"main", []demoPane{
			{program: "claude", cwd: "/tmp", state: rex.StateWorking, task: "Port it", ago: time.Second, output: demoClaude},
			{program: "zsh", cwd: "/tmp", output: demoShell},
		}}}}}, nil)
	}))
	calm.key("alt+a")
	if got := calm.m.toast.Current().Text; got != "No agent needs you" {
		t.Errorf("⌥A with nothing waiting: toast %q", got)
	}
}

// ⌃⌫ on a result takes it out of Needs you, and the selection stays there,
// on the row that takes its place: the list does not follow the result
// down into Sessions. With nothing left, it goes to the top.
func TestCtrlBackspaceStaysInNeedsYou(t *testing.T) {
	h := newHarness(t, 80, 24, true, withWorld(attentionWorld))
	h.selectEntry("Fix navbar overflow on small screens")
	h.key("ctrl+backspace")
	if e := h.find("Fix navbar overflow on small screens"); e.unseen {
		t.Fatal("⌃⌫ did not mark the result seen")
	}
	if got, sec := h.selected(), h.m.selectedSection(); sec != secNeedsYou || got != "Migrate terraform state to the new bucket" {
		t.Errorf("after ⌃⌫ the selection is %q in %q, want the last row of Needs you", got, sec)
	}
	if screen := h.screen(); !strings.Contains(screen, "Needs you") || !strings.Contains(screen, "Allow Bash") {
		t.Errorf("after ⌃⌫ the list scrolled Needs you away:\n%s", screen)
	}

	calm := newHarness(t, 80, 24, true, withWorld(func(now time.Time) (*rexfake.Fake, place, state.History) {
		return buildDemo(now, []demoSession{{"work", []demoTab{{"main", []demoPane{
			{program: "claude", cwd: "/tmp", state: rex.StateDone, task: "Port it", ago: time.Minute, output: demoClaude},
			{program: "zsh", cwd: "/tmp", output: demoShell}, // Go to opens here
		}}}}}, nil)
	}))
	calm.selectEntry("Port it")
	if sec := calm.m.selectedSection(); sec != secNeedsYou {
		t.Fatalf("the result is in %q, want Needs you", sec)
	}
	calm.key("ctrl+backspace")
	if c := calm.m.list.Cursor(); c != 0 || calm.m.firstRowIn(secNeedsYou) >= 0 {
		t.Errorf("after the last ⌃⌫ the cursor is on row %d, want the top and no Needs you", c)
	}
}

// ⌃⌫ in a search deletes a word of the query, as in any field: it marks
// nothing seen.
func TestCtrlBackspaceInASearchEdits(t *testing.T) {
	h := newHarness(t, 120, 40, true, withWorld(attentionWorld))
	h.typeText("fix navbar")
	h.key("ctrl+backspace")
	if got := h.m.input.Value(); got != "fix " {
		t.Errorf("query = %q, want the last word gone", got)
	}
	if e := h.find("Fix navbar overflow on small screens"); !e.unseen {
		t.Error("⌃⌫ in a search marked the result seen")
	}
}

// An agent only guessed to work (from its title, or a stale report in
// doubt) is no working agent in any total, nor in Working.
func TestGuessedWorkIsNotCounted(t *testing.T) {
	h := newHarness(t, 120, 40, true, withWorld(attentionWorld))
	if rows := sectionRows(h)[secWorking]; slices.Contains(rows, "Port the webhook handlers") {
		t.Errorf("Working lists a guess: %q", rows)
	}
	for _, name := range []string{"payments", "webhooks"} {
		if e := h.find(name); e.working != 0 || e.guessed != 1 {
			t.Errorf("%s: working %d, guessed %d; want the guess apart", name, e.working, e.guessed)
		}
	}
	if got := agentsAt(h.find("webhooks")); got != "1 agent is at work in it" {
		t.Errorf("closing its tab warns %q, want the agent at work", got)
	}
}
