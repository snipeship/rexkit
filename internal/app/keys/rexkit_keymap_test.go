package keys

import (
	"encoding/json"
	"os"
	"rexkit/internal/ui/uitest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// rexkitKeymap is `rex keymap --json` from a server running rexkit's own
// lua/init.lua, trimmed as the cache trims it, with sources made relative.
// Regenerate it whenever lua/ changes the keymap.
func rexkitKeymap(t *testing.T) rex.Keymap {
	t.Helper()
	b, err := os.ReadFile("testdata/rexkit-keymap.json")
	if err != nil {
		t.Fatal(err)
	}
	var km rex.Keymap
	if err := json.Unmarshal(b, &km); err != nil {
		t.Fatal(err)
	}
	return km
}

func group(t *testing.T, g Guide, mode string) Group {
	t.Helper()
	for _, grp := range g.Groups {
		if grp.Mode == mode {
			return grp
		}
	}
	t.Fatalf("no %q group", mode)
	return Group{}
}

func rowsOf(grp Group) []string {
	var out []string
	for _, e := range grp.Entries {
		out = append(out, e.KeysLabel()+"  "+e.Title)
	}
	return out
}

func TestRexkitKeymap(t *testing.T) {
	g := Build(rexkitKeymap(t))
	if len(g.Conflicts) != 0 {
		t.Errorf("rexkit's own keymap has conflicts: %v", g.Conflicts)
	}

	// One action bound with different arguments: every row reads apart.
	for _, mode := range []string{"resize", "tab", "scroll"} {
		seen := map[string]bool{}
		for _, e := range group(t, g, mode).Entries {
			if seen[e.Title] {
				t.Errorf("%s: two rows read %q", mode, e.Title)
			}
			seen[e.Title] = true
		}
	}
	resize := rowsOf(group(t, g, "resize"))
	for _, want := range []string{"h ←  Grow left", "⇧H ⇧←  Shrink left", ui.KeyLabel("=") + " " + ui.KeyLabel("shift+=") + "  Grow pane"} {
		if !slices.Contains(resize, want) {
			t.Errorf("resize lacks %q: %q", want, resize)
		}
	}
	scroll := rowsOf(group(t, g, "scroll"))
	for _, want := range []string{"b  Scrollback from top", "u  Scrollback, half page up", "PgUp  Scrollback, page up"} {
		if !slices.Contains(scroll, want) {
			t.Errorf("scroll lacks %q: %q", want, scroll)
		}
	}

	// The other modes' prefixes fold into one row, ahead of the exits; the
	// mode's own guide key is gone, since that guide is what is showing.
	want := []string{
		"h ←  Grow left", "⇧H ⇧←  Shrink left", "j ↓  Grow down", "⇧J ⇧↓  Shrink down",
		"k ↑  Grow up", "⇧K ⇧↑  Shrink up", "l →  Grow right", "⇧L ⇧→  Shrink right",
		ui.KeyLabel("=") + " " + ui.KeyLabel("shift+=") + "  Grow pane", "-  Shrink pane", "0  Balance splits",
		"⌃P ⌃T ⌃H ⌃S ⌃O ⌃B ⌃G  Switch mode", "⎋ ⏎ ⌃C ⌃N  Leave mode",
	}
	if !slices.Equal(resize, want) {
		t.Errorf("resize =\n%q\nwant\n%q", resize, want)
	}
	prefixes := map[string]bool{}
	for _, grp := range g.Groups {
		prefixes[grp.Prefix.Label()] = grp.Prefix != nil
	}
	for _, grp := range g.Groups {
		for _, e := range grp.Entries {
			if grp.Mode != "" && strings.HasPrefix(e.Action, "rexkit_mode_") && prefixes[e.KeysLabel()] {
				t.Errorf("%s still lists %s %s", grp.Mode, e.KeysLabel(), e.Title)
			}
		}
	}
	if s := rowsOf(group(t, g, "session")); !slices.Contains(s, ui.KeyLabel("shift+/")+"  Key guide") {
		t.Errorf("session's ? opens the whole guide and stays listed: %q", s)
	}

	// The way into a mode reads the same everywhere.
	if tmux := rowsOf(group(t, g, "tmux")); !slices.Contains(tmux, "[ ⌃S  Scroll mode") {
		t.Errorf("tmux = %q", tmux)
	}
	global := rowsOf(group(t, g, ""))
	for _, want := range []string{"⌃B  Tmux mode (one key)", "⌃G  Locked mode", "⌥N  Split pane best fit"} {
		if !slices.Contains(global, want) {
			t.Errorf("global lacks %q: %q", want, global)
		}
	}

	// Hints opens from Scroll mode and the tmux prefix; its own mode, which
	// only carries ⌥ + a letter to it, is never shown.
	scroll, tmux := rowsOf(group(t, g, "scroll")), rowsOf(group(t, g, "tmux"))
	if !slices.Contains(scroll, "f  Hints…") || !slices.Contains(tmux, "⇧F  Hints…") {
		t.Errorf("Hints is not at ⌃S f and ⌃B ⇧F: %q %q", scroll, tmux)
	}
	for _, grp := range g.Groups {
		if grp.Mode == "hints" {
			t.Error("the hints mode is listed")
		}
	}

	// Locked passes every key on; only its way out is listed.
	if locked := rowsOf(group(t, g, "locked")); !slices.Equal(locked, []string{"⌃G  Leave mode"}) {
		t.Errorf("locked = %q", locked)
	}
}

func TestSwitchModeRowRunsNothing(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{canRun: true, stateDir: t.TempDir(), mode: "resize"}, Build(rexkitKeymap(t)), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	press(t, m, "up", "up") // the exits are last: Leave mode, then Switch mode
	if got := selectedTitle(m); got != "Switch mode" {
		t.Fatalf("selected %q", got)
	}
	if _, cmd := m.Update(uitest.Key(t, "enter")); quits(cmd) || m.chosen != nil {
		t.Error("the summary row has nothing to run")
	}
	if !strings.Contains(m.toasts.Current().Text, "Nothing to run") {
		t.Errorf("toast %q", m.toasts.Current().Text)
	}
}

func TestSearchMatchesArguments(t *testing.T) {
	km := keymap(`ctrl+n client.mode.enter {"name":"resize"}`, `alt+n pane.split {"direction":"auto"}`)
	g := Build(km)
	// "Split pane best fit" says auto in other words; the argument still finds it.
	if v := filter(g, "auto", 0); count(v) != 1 {
		t.Errorf("searching \"auto\" found %d entries", count(v))
	}
}

func TestGoldenRexkit(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(options{}, Build(rexkitKeymap(t)), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	uitest.Golden(t, "rexkit-150x45", screen(m))
}
