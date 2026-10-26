package switcher

import (
	"slices"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

func TestPaneEntry(t *testing.T) {
	at := testNow.Add(-5 * time.Minute)
	proc := func(path, cwd string) *rex.Process {
		return &rex.Process{Child: &rex.ProcessInfo{Name: "x", Path: path, Cwd: cwd}}
	}
	tests := []struct {
		name     string
		block    rex.Block
		wantKind ui.Kind
		wantName string
		wantStat ui.Status
		wantCwd  string
	}{
		{
			name:     "automatic label shows the program",
			block:    rex.Block{Label: "shell", Terminal: &rex.Terminal{Process: proc("/bin/zsh", "/Users/me/code")}},
			wantKind: ui.KindPane, wantName: "zsh", wantCwd: "~/code",
		},
		{
			name:     "a label the user chose wins",
			block:    rex.Block{Label: "api server", Terminal: &rex.Terminal{Process: proc("/bin/zsh", "/Users/me")}},
			wantKind: ui.KindPane, wantName: "api server", wantCwd: "~",
		},
		{
			name:     "agents are known by basename",
			block:    rex.Block{Label: "shell", Terminal: &rex.Terminal{Process: proc("/opt/bin/claude", "")}},
			wantKind: ui.KindAgent, wantName: "claude",
		},
		{
			name:     "a path merely containing an agent name is no agent",
			block:    rex.Block{Label: "", Terminal: &rex.Terminal{Process: proc("/Users/campbell/bin/zsh", "")}},
			wantKind: ui.KindPane, wantName: "zsh",
		},
		{
			name: "program status makes an agent named by its task",
			block: rex.Block{Label: "shell", Terminal: &rex.Terminal{
				Process: proc("/usr/bin/node", ""),
				Status: rex.List[rex.StatusRecord]{
					{App: "claude-code", State: rex.StateWorking, Title: "Old", UpdatedAt: at.Add(time.Minute)},
					{App: "claude-code", State: rex.StateBlocked, Title: "Review the plan", UpdatedAt: at},
				},
			}},
			wantKind: ui.KindAgent, wantName: "Review the plan", wantStat: ui.StatusBlocked,
		},
		{
			name:     "an ended program is exited",
			block:    rex.Block{Label: "probe", Terminal: &rex.Terminal{Process: &rex.Process{LastExit: &rex.Exit{Code: 2}}}},
			wantKind: ui.KindPane, wantName: "probe", wantStat: ui.StatusExited,
		},
		{
			name:     "labels are sanitized",
			block:    rex.Block{Label: "evil\x1b[31m\u202ename", Terminal: &rex.Terminal{}},
			wantKind: ui.KindPane, wantName: "evil<U+202E>name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := paneEntry(entry{sessionID: "s", windowID: "w", session: "S"}, tt.block, "/Users/me")
			if e.kind != tt.wantKind || e.name != tt.wantName || e.status != tt.wantStat || e.cwd != tt.wantCwd {
				t.Errorf("got kind=%v name=%q status=%v cwd=%q, want kind=%v name=%q status=%v cwd=%q",
					e.kind, e.name, e.status, e.cwd, tt.wantKind, tt.wantName, tt.wantStat, tt.wantCwd)
			}
		})
	}
}

func TestTabName(t *testing.T) {
	panes := []entry{{blockID: "b1", cwd: "~/code/api", program: "zsh"}, {blockID: "b2", cwd: "~", program: "nvim"}}
	tests := []struct {
		label, focused, want string
	}{
		{"web", "b1", "web"},
		{"Window 726ac4f9", "b1", "api"},
		{"", "b2", "nvim"},
		{"Window 726ac4f9", "missing", "Tab 3"},
		{"Window of opportunity", "b1", "Window of opportunity"},
	}
	for _, tt := range tests {
		if got, _ := tabName(tt.label, panes, tt.focused, 3); got != tt.want {
			t.Errorf("tabName(%q, focused %s) = %q, want %q", tt.label, tt.focused, got, tt.want)
		}
	}
}

func TestCatalogOrderAndExclusions(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	sid, win, _ := h.fake.AddSession("tools")
	_, blocks := h.fake.AddWindow(sid, "float host", "rexkit:files", "kept")
	h.fake.SetTerminal(blocks[1], rex.Terminal{})
	inv := h.inventory()

	cat := buildCatalog(inv, catalogOptions{origin: h.origin, self: blocks[1]})
	var sessions []string
	for _, e := range cat.entries {
		if e.kind == ui.KindSession {
			sessions = append(sessions, e.name)
		}
		if e.blockID == blocks[0] || e.blockID == blocks[1] {
			t.Errorf("listed %q, a rexkit tool or the switcher itself", e.name)
		}
	}
	want := []string{"rexkit", "studio", "infrastructure-and-deployment-pipelines", "研究ノート", "scratch", "tools"}
	if !slices.Equal(sessions, want) {
		t.Errorf("sessions = %q, want %q (origin first, then server order)", sessions, want)
	}
	if e, ok := cat.find(sid); !ok || e.tabs != 1 || e.windowID != win {
		t.Errorf("tools session = %+v, want only the window with a listable pane", e)
	}
}

func TestOriginTabJumpsToOriginWhenTheFloatHasFocus(t *testing.T) {
	w := rex.Window{ID: "w", FocusedBlockID: "float", Layers: rex.List[rex.Layer]{
		{Kind: rex.LayerTiled, Blocks: rex.List[rex.Block]{{ID: "a"}, {ID: "origin"}}},
		{Kind: rex.LayerFloating, Blocks: rex.List[rex.Block]{{ID: "float"}}},
	}}
	group := tabEntries(entry{sessionID: "s"}, w, 1, catalogOptions{origin: place{"s", "w", "origin"}, self: "float"})
	if len(group) != 3 || group[0].blockID != "origin" {
		t.Fatalf("tab = %+v, want 2 panes and a jump to the origin", group)
	}
}

func TestPrimaryRecordPrefersUrgentThenRecent(t *testing.T) {
	old, recent := testNow.Add(-time.Hour), testNow
	tests := []struct {
		records []rex.StatusRecord
		want    string
	}{
		{[]rex.StatusRecord{{State: "working", Title: "w", UpdatedAt: recent}, {State: "blocked", Title: "b", UpdatedAt: old}}, "b"},
		{[]rex.StatusRecord{{State: "idle", Title: "old", UpdatedAt: old}, {State: "idle", Title: "new", UpdatedAt: recent}}, "new"},
		{[]rex.StatusRecord{{State: "mystery", Title: "m", UpdatedAt: recent}, {State: "done", Title: "d", UpdatedAt: old}}, "d"},
	}
	for _, tt := range tests {
		if got, _ := primaryRecord(tt.records); got.Title != tt.want {
			t.Errorf("primaryRecord = %q, want %q", got.Title, tt.want)
		}
	}
	if _, ok := primaryRecord(nil); ok {
		t.Error("primaryRecord(nil) found a record")
	}
}

func TestFitPathKeepsTheTab(t *testing.T) {
	e := entry{kind: ui.KindPane, session: "infrastructure-and-deployment-pipelines", tab: "deploy"}
	tests := []struct {
		w       int
		pos     []int
		want    string
		wantPos []int
	}{
		{0, []int{0, 40}, "infrastructure-and-deployment-pipelines › deploy", []int{0, 40}},
		{30, []int{0, 1, 20, 42, 43}, "infrastructure-and-d… › deploy", []int{0, 1, 24, 25}},
		{15, []int{0, 5, 42}, "infra… › deploy", []int{0, 9}},
		{11, nil, "i… › deploy", nil},
		{9, []int{0, 42, 43}, "…› deploy", []int{3, 4}}, // too narrow for any of the session: the end
	}
	for _, tt := range tests {
		got, pos := fitPath(e, tt.pos, tt.w)
		if got != tt.want || !slices.Equal(pos, tt.wantPos) {
			t.Errorf("fitPath(w=%d) = %q %v, want %q %v", tt.w, got, pos, tt.want, tt.wantPos)
		}
	}
}

func TestCacheNameIsPerServer(t *testing.T) {
	a, b := cacheName("unix:///a.sock"), cacheName("http://127.0.0.1:4000")
	if a == b || a == cacheName("") || cacheName("") != state.SnapshotCache || a != cacheName("unix:///a.sock") {
		t.Errorf("cache names %q %q %q are not stable and distinct", a, b, cacheName(""))
	}
}

// A session's or tab's rollup names its most urgent agent in the agent rows'
// own words and colours: a failed agent is not "waiting".
func TestRollupUsesAgentWords(t *testing.T) {
	h := newHarness(t, 100, 30, true)
	tests := []struct {
		name   string
		status ui.Status
		word   string
	}{
		{"deploy", ui.StatusFailed, "1 failed"},
		{"infrastructure-and-deployment-pipelines", ui.StatusFailed, "1 failed"},
		{"web", ui.StatusBlocked, "1 blocked"},
	}
	for _, tt := range tests {
		if s, w := h.find(tt.name).summary(); s != tt.status || w != tt.word {
			t.Errorf("%s rolls up as (%v, %q), want (%v, %q)", tt.name, s, w, tt.status, tt.word)
		}
	}
}
