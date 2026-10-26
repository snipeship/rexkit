package switcher

import (
	"slices"
	"strings"
	"testing"

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
		// Oldest first: the failed task has waited 41m, the review 12m.
		{secNeedsYou, []string{"Migrate terraform state to the new bucket", "Navigation polish"}},
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
	if got := sectionRows(h)[secNeedsYou]; !slices.Equal(got, []string{"Migrate terraform state to the new bucket", "Navigation polish"}) {
		t.Errorf("agents needing you = %q, want the longest waiting first", got)
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
