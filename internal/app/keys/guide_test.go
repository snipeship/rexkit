package keys

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// keymap builds a keymap from bindings written oldest first, as a config
// file would bind them; the server lists them newest first.
func keymap(binds ...string) rex.Keymap {
	var km rex.Keymap
	for _, b := range binds {
		key, rest, _ := strings.Cut(b, " ")
		action, args, _ := strings.Cut(rest, " ")
		mode, k, ok := strings.Cut(key, "/")
		if !ok {
			mode, k = "", key
		}
		rb := rex.Binding{Keys: strings.Split(k, ">"), Action: action, Mode: mode}
		if args != "" {
			rb.Args = json.RawMessage(args)
		}
		km.Bindings = append(rex.List[rex.Binding]{rb}, km.Bindings...)
	}
	return km
}

func TestBuildGroups(t *testing.T) {
	km := keymap(
		`ctrl+t client.mode.enter {"name":"tab"}`,
		`ctrl+p client.mode.enter {"name":"pane"}`,
		`pane/h pane.focus {"direction":"left"}`,
		`pane/esc client.mode.exit_all`,
		`pane/left pane.focus {"direction":"left"}`,
		`tab/n client.tab.new`,
		`scroll/k client.prompt.previous`,
		`locked/ctrl+p pane.send_key {"key":"ctrl+p"}`,
		`picker/enter rexkit_commit`,
		`f1 rexkit_keys`,
	)
	km.Actions = rex.List[rex.Action]{{Name: "rexkit_keys", Title: "Key guide", Description: "What every key does"}}
	g := Build(km)

	var names []string
	for _, grp := range g.Groups {
		names = append(names, grp.Name()+":"+grp.Prefix.Config())
	}
	if want := []string{"Global:", "Tab:ctrl+t", "Pane:ctrl+p", "Scroll:"}; !slices.Equal(names, want) {
		t.Fatalf("groups = %v, want %v (locked has only a pass-through, picker is internal)", names, want)
	}
	pane := g.Groups[2]
	if len(pane.Entries) != 2 {
		t.Fatalf("pane entries = %+v", pane.Entries)
	}
	if e := pane.Entries[0]; e.KeysLabel() != "h ←" || e.Title != "Focus pane left" || e.Exit {
		t.Errorf("first pane entry = %q %q exit=%v, want h ← merged into one entry", e.KeysLabel(), e.Title, e.Exit)
	}
	if e := pane.Entries[1]; !e.Exit || e.Title != "Leave mode" {
		t.Errorf("the mode exit should come last: %+v", e)
	}
	if e := g.Groups[0].Entries[2]; e.Title != "Key guide" || e.Description != "What every key does" {
		t.Errorf("lua action entry = %+v", e)
	}
	if len(g.Conflicts) != 0 {
		t.Errorf("unexpected conflicts %v", g.Conflicts)
	}
}

func TestBuildConflicts(t *testing.T) {
	tests := []struct {
		name  string
		binds []string
		want  []string // Conflict.String()
		runs  string   // title of the entry flagged with the conflict
	}{
		{"same key, newest wins", []string{"ctrl+o pane.zoom", "ctrl+o pane.close"}, []string{"⌃O runs Close pane, not Zoom pane"}, "Close pane"},
		{"same binding twice is harmless", []string{"ctrl+o pane.zoom", "ctrl+o pane.zoom"}, nil, ""},
		{"sequence hidden by a newer key", []string{"ctrl+b>c client.tab.new", "ctrl+b pane.zoom"}, []string{"⌃B runs Zoom pane, not New tab (" + ui.KeyLabel("ctrl+b c") + ")"}, "Zoom pane"},
		{"key hidden by a newer sequence", []string{"cmd+k pane.zoom", "cmd+k>s session.switch"}, []string{ui.KeyLabel("cmd+k s") + " runs Change session…, not Zoom pane (⌘K)"}, "Change session…"},
		{"modes are separate", []string{"o pane.zoom", "pane/o pane.close"}, nil, ""},
		{"three bindings", []string{"pane/o pane.zoom", "pane/o rz_files", "pane/o rexkit_files"}, []string{"o runs Files, not Files, Zoom pane"}, "Files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := keymap(tt.binds...)
			km.Actions = rex.List[rex.Action]{{Name: "rz_files", Title: "Files"}, {Name: "rexkit_files", Title: "Files"}}
			g := Build(km)
			var got []string
			for _, c := range g.Conflicts {
				got = append(got, c.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("conflicts = %q, want %q", got, tt.want)
			}
			flagged := ""
			for _, grp := range g.Groups {
				for _, e := range grp.Entries {
					if e.Conflict >= 0 {
						flagged = e.Title
					}
				}
			}
			if flagged != tt.runs {
				t.Errorf("flagged entry = %q, want %q", flagged, tt.runs)
			}
		})
	}
}

func TestEntersMode(t *testing.T) {
	actions := map[string]rex.Action{}
	for _, n := range []string{"rz_mode_pane", "rexkit_mode_tab", "scroll_mode", "rz_exit_mode", "rz_files"} {
		actions[n] = rex.Action{Name: n}
	}
	tests := []struct {
		action, args string
		mode         string
		once         bool
	}{
		{"client.mode.enter", `{"name":"pane"}`, "pane", false},
		{"client.mode.enter", `{"name":"tmux","once":true}`, "tmux", true},
		{"rz_mode_pane", "", "pane", false},
		{"rexkit_mode_tab", "", "tab", false},
		{"scroll_mode", "", "scroll", false},
		{"rz_exit_mode", "", "", false},
		{"rz_files", "", "", false},
		{"pane.mode_unknown", "", "", false}, // not a config action
	}
	for _, tt := range tests {
		b := rex.Binding{Action: tt.action, Args: json.RawMessage(tt.args)}
		if mode, once := entersMode(b, actions); mode != tt.mode || once != tt.once {
			t.Errorf("entersMode(%s %s) = %q, %v; want %q, %v", tt.action, tt.args, mode, once, tt.mode, tt.once)
		}
	}
}

func TestTitles(t *testing.T) {
	actions := map[string]rex.Action{
		"rz_rename_pane":    {Name: "rz_rename_pane", Title: "Rename pane"},
		"rz_bare":           {Name: "rz_bare"},
		"evil":              {Name: "evil", Title: "Evil\x1b[31m red\u202e"},
		"rexkit_grow":       {Name: "rexkit_grow", Title: "Grow pane"},
		"rexkit_tab_goto":   {Name: "rexkit_tab_goto", Title: "Go to tab"},
		"rexkit_scrollback": {Name: "rexkit_scrollback", Title: "Scrollback"},
		"rexkit_commit":     {Name: "rexkit_commit", Title: "Confirm in tool", Category: internalCategory},
	}
	tests := []struct{ action, args, want string }{
		{"client.mode.enter", `{"name":"pane"}`, "Pane mode"},
		{"client.mode.enter", `{"name":"tmux","once":true}`, "Tmux mode (one key)"},
		{"pane.send_key", `{"key":"ctrl+b"}`, "Send ⌃B"},
		{"rz_rename_pane", "", "Rename pane"},
		{"rz_bare", "", "Bare"},
		{"rz_split_auto", "", "Split auto"},
		{"pane.split", `{"direction":"right"}`, "Split pane right"},
		{"pane.resize", `{"amount":15,"direction":"left"}`, "Resize pane left 15"},
		{"client.tab.goto", `{"index":3}`, "Go to tab 3"},
		{"pane.split", `{"direction":"auto"}`, "Split pane best fit"},
		{"pane.focus", `{"direction":"left","wrap":false}`, "Focus pane left"},
		// Lua actions bound with different arguments read apart.
		{"rexkit_grow", `{"direction":"left"}`, "Grow pane left"},
		{"rexkit_grow", "", "Grow pane"},
		{"rexkit_tab_goto", `{"index":3}`, "Go to tab 3"},
		{"rexkit_scrollback", `{"start":"top"}`, "Scrollback top"},
		{"rexkit_scrollback", `{"start":"half-up"}`, "Scrollback half up"},
		{"rexkit_scrollback", `{"start":"page_up"}`, "Scrollback page up"},
		{"rexkit_commit", `{"key":"enter","global":true}`, "Confirm in tool"}, // plumbing: args only route a key
		{"com.example.do_thing", "", "Com example do thing"},
		{"evil", "", "Evil red<U+202E>"},
	}
	for _, tt := range tests {
		b := rex.Binding{Action: tt.action, Args: json.RawMessage(tt.args)}
		if got := titleOf(b, actions); got != tt.want {
			t.Errorf("titleOf(%s %s) = %q, want %q", tt.action, tt.args, got, tt.want)
		}
	}
}

func TestTrimKeepsWhatTheGuideReads(t *testing.T) {
	km := demoKeymap()
	km.Actions = append(km.Actions, rex.Action{Name: "com.superlogical.terminal.format", Kind: "block"})
	km.Modes = append(km.Modes, rex.Mode{Name: "big", Blocked: [][]string{{"a"}, {"b"}}})
	km.Diagnostics = rex.List[rex.Diagnostic]{{Message: "x"}}
	small := trim(km)
	if len(small.Diagnostics) != 0 || slices.ContainsFunc(small.Actions, func(a rex.Action) bool { return a.Kind == "block" }) {
		t.Errorf("trim kept unused data: %+v", small)
	}
	for _, m := range small.Modes {
		if m.Blocked != nil {
			t.Errorf("trim kept blocked keys of %s", m.Name)
		}
	}
	a, _ := json.Marshal(Build(km))
	b, _ := json.Marshal(Build(small))
	if string(a) != string(b) {
		t.Error("the trimmed keymap builds a different guide")
	}
}

// TestHiddenBindingsStillConflict: pass-throughs and rexkit's plumbing are
// not listed, but they can still be what runs on a key, or lose it.
func TestHiddenBindingsStillConflict(t *testing.T) {
	actions := rex.List[rex.Action]{
		{Name: "rexkit_commit", Title: "Confirm in tool", Category: internalCategory},
		{Name: "user_enter", Title: "User enter"},
	}
	type entry struct{ keys, title string }
	tests := []struct {
		name      string
		binds     []string
		conflicts []string
		listed    []entry // every listed entry, with ⚠ after the title when flagged
	}{
		{"plumbing wins over an older user key", []string{
			`enter user_enter`, `enter rexkit_commit {"key":"enter","global":true}`,
		}, []string{"⏎ runs Confirm in tool, not User enter"}, []entry{{"⏎", "User enter ⚠"}}},
		{"a newer user key replaces the plumbing", []string{
			`enter rexkit_commit {"key":"enter","global":true}`, `enter user_enter`,
		}, []string{"⏎ runs User enter, not Confirm in tool"}, []entry{{"⏎", "User enter ⚠"}}},
		{"a pass-through wins over an older locked key", []string{
			`ctrl+g client.mode.enter {"name":"locked"}`, `locked/ctrl+p client.tab.new`, `locked/ctrl+p pane.send_key {"key":"ctrl+p"}`,
		}, []string{"⌃P runs Send ⌃P, not New tab"}, []entry{{"⌃G", "Locked mode"}, {"⌃P", "New tab ⚠"}}},
		{"pass-throughs and plumbing alone", []string{
			`locked/ctrl+p pane.send_key {"key":"ctrl+p"}`, `locked/ctrl+p pane.send_key {"key":"ctrl+p"}`,
			`enter rexkit_commit {"key":"enter"}`, `f1 user_enter`,
		}, nil, []entry{{"F1", "User enter"}}},
		{"two hidden bindings on one key", []string{
			`enter rexkit_commit {"key":"enter"}`, `enter rexkit_commit {"key":"enter","global":true}`,
		}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := keymap(tt.binds...)
			km.Actions = actions
			g := Build(km)
			var got []string
			for _, c := range g.Conflicts {
				got = append(got, c.String())
			}
			if !slices.Equal(got, tt.conflicts) {
				t.Errorf("conflicts = %q, want %q", got, tt.conflicts)
			}
			var listed []entry
			for _, grp := range g.Groups {
				for _, e := range grp.Entries {
					title := e.Title
					if e.Conflict >= 0 {
						title += " ⚠"
					}
					listed = append(listed, entry{e.KeysLabel(), title})
				}
			}
			if !slices.Equal(listed, tt.listed) {
				t.Errorf("listed = %q, want %q", listed, tt.listed)
			}
		})
	}
}

func TestPassThroughSpellings(t *testing.T) {
	// The server stores the key "escape" as "esc" but keeps the args as sent.
	km := keymap(
		`ctrl+g client.mode.enter {"name":"locked"}`,
		`locked/esc pane.send_key {"key":"escape"}`,
		`locked/enter pane.send_key {"key":"return"}`,
		`locked/ctrl+b pane.send_key {"key":"control+b"}`,
		`locked/ctrl+g client.mode.exit_all`,
	)
	g := Build(km)
	locked := g.Groups[len(g.Groups)-1]
	if locked.Mode != "locked" || len(locked.Entries) != 1 || locked.Entries[0].Title != "Leave mode" {
		t.Errorf("locked = %+v, want only Leave mode", locked.Entries)
	}
}

// TestColumns: groups stay whole, in order, in balanced columns; only a
// group taller than the body is split, into near equal parts under a
// repeated header.
func TestColumns(t *testing.T) {
	entries := func(n int) shown {
		s := shown{}
		for i := range n {
			s.entries = append(s.entries, hit{entry: i})
		}
		return s
	}
	for _, tt := range []struct {
		name string
		view []shown
		h    int
		want [][]int // per column, entries per group part
	}{
		{"fits in one", []shown{entries(4)}, 5, [][]int{{4}}},
		{"small groups share a column", []shown{entries(2), entries(3), entries(2)}, 20, [][]int{{2, 3, 2}}},
		{"one over splits even", []shown{entries(5)}, 5, [][]int{{3}, {2}}}, // not 4 + 1
		{"long", []shown{entries(9)}, 5, [][]int{{3}, {3}, {3}}},            // not 4 + 4 + 1
		{"whole groups, balanced", []shown{entries(3), entries(3), entries(3)}, 10, [][]int{{3, 3}, {3}}},
		{"balanced, not greedy", []shown{entries(5), entries(1), entries(1), entries(5)}, 14, [][]int{{5, 1}, {1, 5}}},
		{"a group is never split to fill a column", []shown{entries(2), entries(4)}, 6, [][]int{{2}, {4}}},
		{"more columns than fit", []shown{entries(4), entries(4), entries(4)}, 6, [][]int{{4}, {4}, {4}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cols := balance(units(tt.view, tt.h), tt.h)
			var got [][]int
			for _, col := range cols {
				var parts []int
				for _, c := range col {
					switch {
					case c.group < 0:
					case c.entry < 0:
						parts = append(parts, 0)
					default:
						parts[len(parts)-1]++
					}
				}
				got = append(got, parts)
				if len(col) > tt.h {
					t.Errorf("column of %d rows in %d", len(col), tt.h)
				}
			}
			if !slices.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("columns = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTitleEscapes: a Lua title's stand-ins (sanitize.LabelSpans) are kept
// with the entry, so the guide draws them apart from the text.
func TestTitleEscapes(t *testing.T) {
	km := rex.Keymap{
		Actions:  []rex.Action{{Name: "evil", Kind: "lua", Title: "a\u202eb"}},
		Bindings: []rex.Binding{{Keys: []string{"f2"}, Action: "evil"}},
	}
	e := Build(km).Groups[0].Entries[0]
	if len(e.Escapes) != 1 || e.Title[e.Escapes[0][0]:e.Escapes[0][1]] != "<U+202E>" {
		t.Errorf("title %q, escapes %v", e.Title, e.Escapes)
	}
	if plain := Build(keymap("f3 pane.zoom")).Groups[0].Entries[0]; plain.Escapes != nil {
		t.Errorf("a plain title has escapes %v", plain.Escapes)
	}
}
