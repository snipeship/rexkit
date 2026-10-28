//go:build integration

package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"rexkit/internal/attend"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/tests/integration/harness"
)

const terminal = "com.superlogical.terminal"

// report makes the program in block (a cat, which echoes it back) report an
// OSC 7501 state, as an agent does.
func report(t *testing.T, s *harness.Server, session, block, pairs, msg string) {
	t.Helper()
	if msg != "" {
		pairs += ":msg=" + base64.StdEncoding.EncodeToString([]byte(msg))
	}
	call(t, s, terminal+".write", map[string]any{"session_id": session, "block_id": block,
		"args": map[string]string{"data": "\x1b]7501;" + pairs + "\x07\n"}}, nil)
	harness.WaitFor(t, 5*time.Second, "the report", func() bool {
		var st struct {
			Records []rex.StatusRecord `json:"records"`
		}
		_ = s.Rex.Call(t.Context(), terminal+".program_status", map[string]any{"session_id": session, "block_id": block, "args": map[string]any{}}, &st)
		return len(st.Records) > 0 && strings.Contains(pairs, "state="+st.Records[0].State)
	})
}

// dirs are rexkit's directories of the server's blocks.
func dirs(s *harness.Server) state.Dirs {
	return state.Dirs{State: s.XDG("XDG_STATE_HOME") + "/rexkit", Config: s.XDG("XDG_CONFIG_HOME") + "/rexkit"}
}

// diagToken is the watcher token the server's config load made.
func diagToken(t *testing.T, s *harness.Server) string {
	t.Helper()
	var diag struct {
		Attend struct {
			Token string `json:"token"`
		} `json:"attend"`
	}
	out := s.CLI(t, "do", "rexkit_diagnostics", "--json")
	if err := json.Unmarshal([]byte(out), &diag); err != nil || diag.Attend.Token == "" {
		t.Fatalf("diagnostics: %v\n%s", err, out)
	}
	return diag.Attend.Token
}

// watcher waits for the one watcher of the server's config load.
func watcher(t *testing.T, s *harness.Server) attend.Info {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			log, _ := os.ReadFile(attend.LogPath(dirs(s)))
			t.Logf("watcher log:\n%s", log)
		}
	})
	token := diagToken(t, s)
	var found attend.Info
	harness.WaitFor(t, 15*time.Second, "the agent watcher", func() bool {
		running := attend.Running(dirs(s))
		i := slices.IndexFunc(running, func(w attend.Info) bool { return w.Token == token })
		if i >= 0 && len(running) == 1 {
			found = running[0]
			return true
		}
		return false
	})
	return found
}

// firstLook waits for the watcher's first snapshot: what is there by then
// is the state of things, and anything after it is news.
func firstLook(t *testing.T, s *harness.Server, w attend.Info) {
	t.Helper()
	mark := fmt.Sprintf("attend[%d] ", w.PID)
	harness.WaitFor(t, 15*time.Second, "the watcher's first look", func() bool {
		data, _ := os.ReadFile(attend.LogPath(dirs(s)))
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, mark) && strings.Contains(line, "first look") {
				return true
			}
		}
		return false
	})
}

func toasts(t *testing.T, s *harness.Server, session string) []float {
	var out []float
	for _, f := range floats(t, s, session) {
		if f.Block.Label == "rexkit:toast" {
			out = append(out, f)
		}
	}
	return out
}

// TestAttendToastsWhereTheAgentIsNot: the config load starts one watcher;
// an agent that starts to wait toasts in every session but its own, where
// its pane has the focus, naming the agent, and the attention state
// remembers it. Rex does not say which session the app shows. Three
// sessions: each other session gets its own toast, not just the first.
func TestAttendToastsWhereTheAgentIsNot(t *testing.T) {
	s := startToasts(t)
	here, _ := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	notes, _ := s.CreateSession(t, "notes", cat("notes", ""))
	focus(t, s, agents.ID, ablocks[1]) // whoever looks at that session sees the agent
	w := watcher(t, s)
	firstLook(t, s, w)

	report(t, s, agents.ID, ablocks[1], "state=blocked:kind=question:app=claude", "Use Redis or memory?")
	var toast float
	harness.WaitFor(t, 10*time.Second, "a toast in each other session", func() bool {
		ts := toasts(t, s, here.ID)
		if len(ts) > 0 {
			toast = ts[0]
		}
		return len(ts) > 0 && len(toasts(t, s, notes.ID)) > 0
	})
	screen := s.WaitScreen(t, here.ID, toast.Block.ID, "claude", 5*time.Second)
	// in a narrow window (this one is 80 columns) the question and where the
	// agent is give way; who it is, what it wants and the key ⌃⇧A do not
	for _, want := range []string{"claude asks", "ctrl shift a▌ go"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the toast says %q, want %q in it", screen, want)
		}
	}
	if len(toasts(t, s, agents.ID)) > 0 {
		t.Error("a toast in the session where the agent is in front")
	}
	m := dirs(s).Attention().Panes[ablocks[1]]
	if m.State != rex.StateBlocked || m.Kind != "question" || m.Since.IsZero() {
		t.Errorf("attention = %+v, want blocked on a question", m)
	}
	if running := attend.Running(dirs(s)); len(running) != 1 || running[0].PID != w.PID {
		t.Errorf("watchers %+v, want the one", running)
	}
}

// TestAttendSeenWhenFocused: a finished agent counts as seen once the focus
// moves to its pane, in a session other than the first, and stays a second;
// not before.
func TestAttendSeenWhenFocused(t *testing.T) {
	s := startToasts(t)
	s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	focus(t, s, agents.ID, ablocks[0])
	firstLook(t, s, watcher(t, s))
	report(t, s, agents.ID, ablocks[1], "state=done:app=claude", "Tests pass")
	harness.WaitFor(t, 5*time.Second, "the watcher to note it", func() bool {
		return dirs(s).Attention().Panes[ablocks[1]].State == rex.StateDone
	})
	time.Sleep(2 * time.Second)
	if seen := dirs(s).Attention().SeenAt(ablocks[1]); !seen.IsZero() {
		t.Fatal("seen while the focus was elsewhere")
	}
	focus(t, s, agents.ID, ablocks[1])
	harness.WaitFor(t, 10*time.Second, "the agent seen", func() bool {
		return !dirs(s).Attention().SeenAt(ablocks[1]).IsZero()
	})
}

// TestAttendToastClickGoes: a click on an agent's toast in the agent's own
// session focuses the agent, and it counts as seen. From another session,
// with no app to switch, the click only closes the toast: nothing moves,
// and nobody saw the agent (not even the watcher's guess).
func TestAttendToastClickGoes(t *testing.T) {
	s := startToasts(t)
	here, hblocks := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), hsplit(0.5, cat("agent", ""), cat("other", ""))))
	focus(t, s, agents.ID, ablocks[0])
	w := watcher(t, s)
	firstLook(t, s, w)
	// click clicks the toast in session as soon as it shows.
	click := func(session string) {
		t.Helper()
		var toast float
		harness.WaitFor(t, 10*time.Second, "the toast", func() bool {
			ts := toasts(t, s, session)
			if len(ts) > 0 {
				toast = ts[0]
			}
			return len(ts) > 0
		})
		screen := s.WaitScreen(t, session, toast.Block.ID, "claude", 5*time.Second)
		x, y := -1, -1
		for i, line := range strings.Split(screen, "\n") {
			if at := strings.Index(line, "claude"); at >= 0 {
				x, y = len([]rune(line[:at]))+1, i+1
				break
			}
		}
		report := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y)
		call(t, s, terminal+".write", map[string]any{"session_id": session, "block_id": toast.Block.ID,
			"args": map[string]string{"data": report}}, nil)
		harness.WaitFor(t, 5*time.Second, "the toast to go", func() bool {
			return !slices.ContainsFunc(toasts(t, s, session), func(f float) bool { return f.Block.ID == toast.Block.ID })
		})
	}

	act(t, s, agents.ID, ablocks[0], "rexkit_attend_toast", map[string]string{"message": "{b:claude} finished",
		"kind": "warn", "status": "done", "go_session": agents.ID, "go_window": agents.WindowID, "go_block": ablocks[1]})
	click(agents.ID)
	assertFocus(t, s, agents.ID, ablocks[1], "")
	harness.WaitFor(t, 5*time.Second, "the agent seen", func() bool {
		return !dirs(s).Attention().SeenAt(ablocks[1]).IsZero()
	})

	report(t, s, agents.ID, ablocks[2], "state=done:app=claude", "Docs updated") // the watcher toasts
	click(here.ID)
	time.Sleep(3 * attend.SeenDwell) // what the watcher would take for a look
	assertFocus(t, s, agents.ID, ablocks[1], "")
	if seen := dirs(s).Attention().SeenAt(ablocks[2]); !seen.IsZero() {
		t.Errorf("a click in another session, with no app to switch, marked the agent seen at %v", seen)
	}

	// The app does not follow a tab focused on the server, so an agent in
	// another tab of the session needs the app to switch tabs too.
	tab := newWindow(t, s, here.ID, "lint", cat("linter", ""))
	report(t, s, here.ID, tab.BlockIDs[0], "state=done:app=claude", "Lint clean")
	click(here.ID)
	time.Sleep(3 * attend.SeenDwell)
	assertFocus(t, s, here.ID, hblocks[0], "")
	if seen := dirs(s).Attention().SeenAt(tab.BlockIDs[0]); !seen.IsZero() {
		t.Errorf("a click on an agent in another tab, with no app to switch, marked it seen at %v", seen)
	}
}

// TestAttendToastOverZoomedPane: a zoomed pane fills its window, so a toast
// over it is as tall as over a window of that one pane, not sized by the
// pane's half of the layout.
func TestAttendToastOverZoomedPane(t *testing.T) {
	s := startToasts(t)
	whole, wblocks := s.CreateSession(t, "whole", cat("me", ""))
	split, sblocks := s.CreateSession(t, "split", vsplit(0.5, cat("top", ""), cat("bottom", "")))
	zoom(t, s, split.ID, sblocks[0])
	height := func(session, block string) float64 {
		act(t, s, session, block, "rexkit_attend_toast", map[string]string{"message": "{b:claude} finished", "kind": "ok"})
		var h float64
		harness.WaitFor(t, 5*time.Second, "the toast", func() bool {
			ts := toasts(t, s, session)
			if len(ts) > 0 {
				h = ts[0].Layer.Bounds[3]
			}
			return len(ts) > 0
		})
		return h
	}
	if got, want := height(split.ID, sblocks[0]), height(whole.ID, wblocks[0]); got != want {
		t.Errorf("the toast over the zoomed pane is %.4f of the window, want %.4f as over a whole one", got, want)
	}
}

// TestAttendOneWatcherPerLoad: a config reload starts a new watcher and the
// old one ends; never two.
func TestAttendOneWatcherPerLoad(t *testing.T) {
	s := harness.Start(t, harness.Options{
		Config: fmt.Sprintf("return dofile(%q)\n", root+"/lua/init.lua"),
		Files:  map[string]string{"xdg/config_home/rexkit/settings.lua": settings},
		Unix:   true,
	})
	old := watcher(t, s)
	s.CLI(t, "config", "reload")
	now := watcher(t, s)
	if now.PID == old.PID || now.Token == old.Token {
		t.Fatalf("after a reload the watcher is %+v, want a new one", now)
	}
	harness.WaitFor(t, 10*time.Second, "the old watcher to end", func() bool {
		return syscall.Kill(old.PID, 0) != nil
	})
	if running := attend.Running(dirs(s)); len(running) != 1 {
		t.Errorf("watchers %+v after a reload, want one", running)
	}
}

// TestAttendEndsWhenRexkitLeaves: a config without rexkit, reloaded, ends
// the watcher.
func TestAttendEndsWhenRexkitLeaves(t *testing.T) {
	s := harness.Start(t, harness.Options{
		Config: fmt.Sprintf("return dofile(%q)\n", root+"/lua/init.lua"),
		Files:  map[string]string{"xdg/config_home/rexkit/settings.lua": settings},
		Unix:   true,
	})
	w := watcher(t, s)
	if err := os.WriteFile(s.ConfigPath(), []byte("return {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.CLI(t, "config", "reload")
	harness.WaitFor(t, 10*time.Second, "the watcher to end", func() bool {
		return syscall.Kill(w.PID, 0) != nil
	})
	log, _ := os.ReadFile(attend.LogPath(dirs(s)))
	if !strings.Contains(string(log), "the config no longer runs rexkit") {
		t.Errorf("the watcher's log says:\n%s", log)
	}
}

// startToasts is start with agent toasts on (they are off by default).
func startToasts(t *testing.T) *harness.Server {
	t.Helper()
	return startWith(t, strings.TrimSuffix(settings, " }")+", agent_toasts = true }")
}

// TestAttendToastsOff: toasts are off by default, and the watcher still
// runs (it keeps what was seen), with no states to toast.
func TestAttendToastsOff(t *testing.T) {
	s := start(t)
	here, _ := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	focus(t, s, agents.ID, ablocks[0])
	firstLook(t, s, watcher(t, s))
	report(t, s, agents.ID, ablocks[1], "state=done:app=claude", "Tests pass")
	harness.WaitFor(t, 5*time.Second, "the watcher to note it", func() bool {
		return dirs(s).Attention().Panes[ablocks[1]].State == rex.StateDone
	})
	time.Sleep(2 * time.Second) // past the burst window
	if ts := toasts(t, s, here.ID); len(ts) > 0 {
		t.Errorf("a toast with agent_toasts off: %+v", ts[0].Block)
	}
}

// TestAttendNextAndBack: ⌥A goes to the agent that waits in another
// session (and tells the client to show it), toasting there; ⌥⇧A comes
// back to where the walk began.
func TestAttendNextAndBack(t *testing.T) {
	s := startToasts(t)
	here, hblocks := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	focus(t, s, agents.ID, ablocks[0])
	report(t, s, agents.ID, ablocks[1], "state=blocked:kind=permission:app=codex", "Allow Bash(go test ./...)?")
	focus(t, s, here.ID, hblocks[0])

	out := act(t, s, here.ID, hblocks[0], "rexkit_next_agent", nil)
	res := result(t, out)
	if res["block_id"] != ablocks[1] || res["session_id"] != agents.ID {
		t.Fatalf("⌥A = %v, want the agent %s", res, ablocks[1])
	}
	if sel, ok := queued(out, "session.select"); !ok || sel["session_id"] != agents.ID {
		t.Errorf("queued %s, want a select of the agent's session", names(out))
	}
	assertFocus(t, s, agents.ID, ablocks[1], "")
	harness.WaitFor(t, 5*time.Second, "the landing toast", func() bool { return len(toasts(t, s, agents.ID)) > 0 })
	if seen := dirs(s).Attention().SeenAt(ablocks[1]); seen.IsZero() {
		t.Error("landing on the agent did not count as seeing it")
	}

	back := result(t, act(t, s, agents.ID, ablocks[1], "rexkit_agent_back", nil))
	if back["block_id"] != hblocks[0] || back["session_id"] != here.ID {
		t.Fatalf("⌥⇧A = %v, want back to %s", back, hblocks[0])
	}
	assertFocus(t, s, here.ID, hblocks[0], "")
}

// TestAttendNextInGoto: ⌃⇧A (agent_key) in Go to moves its selection to the
// next agent that needs you, as ⌥A does; ⌃⌫ there, as the app sends it,
// marks that one seen.
func TestAttendNextInGoto(t *testing.T) {
	s := startToasts(t)
	work, blocks := s.CreateSession(t, "work", hsplit(0.5, cat("me", ""), hsplit(0.5, cat("a1", ""), cat("a2", ""))))
	report(t, s, work.ID, blocks[1], "state=done:app=claude", "Navbar fixed")
	time.Sleep(1100 * time.Millisecond) // a2 finishes later: it comes second
	report(t, s, work.ID, blocks[2], "state=done:app=codex", "Tests pass")
	focus(t, s, work.ID, blocks[0])
	o := open(t, s, work.ID, blocks[0], "rexkit_goto", nil, "Needs you")
	s.WaitScreen(t, work.ID, o.Block, "Tests pass", 5*time.Second)

	res := result(t, pressGlobal(t, s, work.ID, o.Block, "ctrl+shift+a"))
	if res["go_to"] != true {
		t.Fatalf("⌃⇧A in Go to = %v, want it handed to Go to", res)
	}
	time.Sleep(300 * time.Millisecond) // Go to reads the ⎋a Lua typed
	s.Drive(t, work.ID, harness.Press(t, o.Block, "ctrl+backspace"))
	harness.WaitFor(t, 5*time.Second, "the second agent marked seen", func() bool {
		return !dirs(s).Attention().SeenAt(blocks[2]).IsZero()
	})
	if !dirs(s).Attention().SeenAt(blocks[1]).IsZero() {
		t.Error("⌃⌫ marked the first agent seen: ⌃⇧A did not move the selection")
	}
	act(t, s, work.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "ctrl+c"})
	harness.WaitFor(t, 5*time.Second, "Go to to close", func() bool { // the watcher's toasts may come and go
		return !slices.ContainsFunc(floats(t, s, work.ID), func(f float) bool { return f.Block.ID == o.Block })
	})
}

// TestAttendKeysWithoutAlt: without agent_key and the ⌥ shortcuts, the
// toasts name Session mode's keys.
func TestAttendKeysWithoutAlt(t *testing.T) {
	s := startWith(t, strings.TrimSuffix(settings, " }")+`, agent_toasts = true, alt_shortcuts = false, agent_key = "", agent_back_key = "" }`)
	here, hblocks := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	report(t, s, agents.ID, ablocks[1], "state=blocked:kind=permission:app=codex", "Allow Bash(go test ./...)?")
	focus(t, s, here.ID, hblocks[0])
	res := result(t, act(t, s, here.ID, hblocks[0], "rexkit_next_agent", nil))
	if res["block_id"] != ablocks[1] {
		t.Fatalf("⌃O a = %v, want the agent", res)
	}
	var toast float
	harness.WaitFor(t, 5*time.Second, "the landing toast", func() bool {
		ts := toasts(t, s, agents.ID)
		if len(ts) > 0 {
			toast = ts[0]
		}
		return len(ts) > 0
	})
	if screen := s.WaitScreen(t, agents.ID, toast.Block.ID, "back", 5*time.Second); !strings.Contains(screen, "ctrl o") {
		t.Errorf("the toast says %q, want Session mode's key", screen)
	}
}

// TestAttendAgentKeys: ⌃⇧A (agent_key), pressed as the app does, goes to
// the agent that waits in another session, where the toast names ⌃⌥⇧A
// (agent_back_key) to go back; ⌃⌥⇧A comes back to where the walk began.
func TestAttendAgentKeys(t *testing.T) {
	s := startToasts(t)
	here, hblocks := s.CreateSession(t, "here", cat("me", ""))
	agents, ablocks := s.CreateSession(t, "agents", hsplit(0.5, cat("shell", ""), cat("agent", "")))
	focus(t, s, agents.ID, ablocks[0])
	report(t, s, agents.ID, ablocks[1], "state=blocked:kind=question:app=claude", "Redis or in-memory?")
	focus(t, s, here.ID, hblocks[0])

	res := result(t, pressGlobal(t, s, here.ID, hblocks[0], "ctrl+shift+a"))
	if res["block_id"] != ablocks[1] || res["session_id"] != agents.ID {
		t.Fatalf("⌃⇧A = %v, want the agent %s", res, ablocks[1])
	}
	assertFocus(t, s, agents.ID, ablocks[1], "")
	var toast float
	harness.WaitFor(t, 5*time.Second, "the landing toast", func() bool {
		ts := toasts(t, s, agents.ID)
		if len(ts) > 0 {
			toast = ts[0]
		}
		return len(ts) > 0
	})
	if screen := s.WaitScreen(t, agents.ID, toast.Block.ID, "back", 5*time.Second); !strings.Contains(screen, "ctrl alt shift a") {
		t.Errorf("the toast says %q, want agent_back_key", screen)
	}

	back := result(t, pressGlobal(t, s, agents.ID, ablocks[1], "ctrl+alt+shift+a"))
	if back["block_id"] != hblocks[0] || back["session_id"] != here.ID {
		t.Fatalf("⌃⌥⇧A = %v, want back to %s", back, hblocks[0])
	}
	assertFocus(t, s, here.ID, hblocks[0], "")
}

// TestAttendKeysNamed: the keys that toasts, Go to and the key panels name
// are the first of agent_key (agent_back_key), ⌥A (⌥⇧A) and Session mode's
// a (⇧A) that the keymap still runs: never a key that is off, that Rex
// can't read or that another key took, nor Session mode's once its prefix
// runs something else. A key setting another key shadows, or one that
// shadows another, is a warning; one that names a key bound to it already,
// in any spelling, is neither a warning nor a duplicate in the keymap.
func TestAttendKeysNamed(t *testing.T) {
	for _, tt := range []struct {
		name, settings string
		next, back     string
		warn           string
		diagnostics    bool
	}{
		{"defaults", "", "ctrl+shift+a", "ctrl+alt+shift+a", "", false},
		{"in Rex's spelling", `agent_key = "shift+ctrl+n", agent_back_key = "ctrl+shift+alt+b"`,
			"ctrl+shift+n", "ctrl+alt+shift+b", "", false},
		{"agent keys off", `agent_key = "", agent_back_key = ""`, "alt+a", "alt+shift+a", "", false},
		{"no alt", `agent_key = "", agent_back_key = "", alt_shortcuts = false`, "ctrl+o a", "ctrl+o shift+a", "", false},
		{"none", `agent_key = "", agent_back_key = "", alt_shortcuts = false, zellij_modes = false`, "", "", "", false},
		{"unreadable", `agent_key = "no+such+key"`, "alt+a", "ctrl+alt+shift+a", "", true},
		{"taken by hints_key", `hints_key = "ctrl+shift+a"`, "alt+a", "ctrl+alt+shift+a",
			"hints_key ctrl+shift+a was rexkit_next_agent; it opens Hints instead", false},
		{"shadows a mode prefix", `agent_key = "ctrl+o"`, "ctrl+o", "ctrl+alt+shift+a",
			"agent_key ctrl+o was client.mode.enter; it goes to the next agent instead", false},
		{"typed", `agent_back_key = "tab"`, "ctrl+shift+a", "tab",
			"agent_back_key tab is a key you type; it goes back instead", false},
		{"bound already, spelled another way", `agent_key = "Option+A", agent_back_key = "shift+alt+a"`,
			"alt+a", "alt+shift+a", "", false},
		{"Session mode's prefix taken", `agent_key = "ctrl+o", agent_back_key = "", alt_shortcuts = false`, "ctrl+o", "",
			"agent_key ctrl+o was client.mode.enter; it goes to the next agent instead", false},
		{"Session mode's prefix taken by hints_key", `hints_key = "ctrl+o", agent_key = "", agent_back_key = "", alt_shortcuts = false`,
			"", "", "hints_key ctrl+o was client.mode.enter; it opens Hints instead", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conf := settings
			if tt.settings != "" {
				conf = strings.TrimSuffix(settings, " }") + ", " + tt.settings + " }"
			}
			s := startWith(t, conf)
			var diag struct {
				Warnings json.RawMessage `json:"warnings"` // {} when there are none
				Attend   struct {
					Keys struct{ Next, Back string } `json:"keys"`
				} `json:"attend"`
			}
			out := s.CLI(t, "do", "rexkit_diagnostics", "--json")
			if err := json.Unmarshal([]byte(out), &diag); err != nil {
				t.Fatalf("diagnostics: %v\n%s", err, out)
			}
			if k := diag.Attend.Keys; k.Next != tt.next || k.Back != tt.back {
				t.Errorf("keys named: next %q, back %q; want %q, %q", k.Next, k.Back, tt.next, tt.back)
			}
			var warnings []string
			_ = json.Unmarshal(diag.Warnings, &warnings)
			if tt.warn != "" && !slices.Contains(warnings, tt.warn) || tt.warn == "" && len(warnings) > 0 {
				t.Errorf("warnings %s, want %q", diag.Warnings, tt.warn)
			}
			km, err := s.Rex.Keymap(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if (len(km.Diagnostics) > 0) != tt.diagnostics {
				t.Errorf("keymap diagnostics %+v", km.Diagnostics)
			}
		})
	}
}

// TestAttendSettingsChecked: a state agent_toast_states does not know is a
// warning, and the default applies.
func TestAttendSettingsChecked(t *testing.T) {
	s := startWith(t, strings.TrimSuffix(settings, " }")+`, agent_toast_states = { "blocked", "sometimes" } }`)
	var diag struct {
		Warnings []string `json:"warnings"`
		Settings struct {
			States []string `json:"agent_toast_states"`
		} `json:"settings"`
	}
	out := s.CLI(t, "do", "rexkit_diagnostics", "--json")
	if err := json.Unmarshal([]byte(out), &diag); err != nil {
		t.Fatalf("diagnostics: %v\n%s", err, out)
	}
	want := `agent_toast_states: "sometimes" should be one of blocked, failed, done; using the default`
	if !slices.Contains(diag.Warnings, want) {
		t.Errorf("warnings %q, want %q", diag.Warnings, want)
	}
	if !slices.Equal(diag.Settings.States, []string{"blocked", "failed", "done"}) {
		t.Errorf("agent_toast_states = %q, want the default", diag.Settings.States)
	}
}
