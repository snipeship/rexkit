//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// TestPlainKeysReachPanes: rexkit binds no global ⏎, ⎋ or ⌃C, so in an
// ordinary pane they are not handled by any binding and their bytes reach
// the program.
func TestPlainKeysReachPanes(t *testing.T) {
	s := start(t)
	sess, _ := s.CreateSession(t, "keys", cat("A", ""))
	b := sess.BlockID
	in := append(harness.Type(t, b, "hello"), harness.Press(t, b, "enter"), harness.Press(t, b, "escape"), harness.Press(t, b, "ctrl+c"))
	outs := s.Drive(t, sess.ID, in...)
	for i, k := range []string{"enter", "escape", "ctrl+c"} {
		if outs[len(outs)-3+i].Handled {
			t.Errorf("%s was handled by a binding", k)
		}
	}
	s.WaitScreen(t, sess.ID, b, "hello", 3*time.Second)
}

// TestKeymap checks the loaded keymap: no global ⏎/⎋/⌃C, the modes and
// their prefixes, locked mode passing every global key to the pane with
// only ⌃G leaving it, and the picker mode binding only the handshake keys.
func TestKeymap(t *testing.T) {
	s := start(t)
	km, err := s.Rex.Keymap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(km.Diagnostics) > 0 {
		t.Errorf("diagnostics: %+v", km.Diagnostics)
	}
	bound := map[string]rex.Binding{}
	for _, b := range km.Bindings {
		key := b.Mode + "/" + strings.Join(b.Keys, " ")
		if _, dup := bound[key]; dup {
			t.Errorf("%s is bound twice", key)
		}
		bound[key] = b
	}
	for _, k := range []string{"enter", "esc", "escape", "ctrl+c"} {
		if b, ok := bound["/"+k]; ok {
			t.Errorf("global %s is bound to %s", k, b.Action)
		}
	}
	modes := map[string]rex.Mode{}
	for _, m := range km.Modes {
		modes[m.Name] = m
	}
	for name, prefix := range map[string]string{"pane": "ctrl+p", "tab": "ctrl+t", "resize": "ctrl+n", "move": "ctrl+h",
		"scroll": "ctrl+s", "session": "ctrl+o", "tmux": "ctrl+b", "locked": "ctrl+g"} {
		if _, ok := modes[name]; !ok {
			t.Errorf("no %s mode", name)
		}
		if b := bound["/"+prefix]; b.Action != "client.mode.enter" {
			t.Errorf("%s runs %q, want client.mode.enter{%s}", prefix, b.Action, name)
		}
	}
	for key, b := range bound {
		mode, k, _ := strings.Cut(key, "/")
		if mode != "" {
			continue
		}
		locked := bound["locked/"+k]
		if k == "ctrl+g" {
			if locked.Action != "client.mode.exit_all" {
				t.Errorf("locked/ctrl+g runs %q, want it to leave locked mode", locked.Action)
			}
		} else if locked.Action != "pane.send_key" {
			t.Errorf("locked mode lets %s run %s, want it passed to the pane", k, b.Action)
		}
	}
	var picker []string
	for key, b := range bound {
		if strings.HasPrefix(key, "picker/") {
			picker = append(picker, strings.TrimPrefix(key, "picker/")+"→"+b.Action)
		}
	}
	if len(picker) != 4 || bound["picker/enter"].Action != "rexkit_commit" || bound["picker/esc"].Action != "rexkit_dismiss" ||
		bound["picker/ctrl+c"].Action != "rexkit_dismiss" || bound["picker/ctrl+shift+space"].Action != "rexkit_goto" {
		t.Errorf("picker mode binds %v, want only the handshake keys", picker)
	}
}

// TestModeSettings turns the Zellij-style modes and the tmux prefix off, one
// at a time and together: the modes that are off are gone with their
// prefixes, which reach the pane again; locked mode stays while either is
// on; the global keys and every tool action stay.
func TestModeSettings(t *testing.T) {
	zellij := map[string]string{"pane": "ctrl+p", "tab": "ctrl+t", "resize": "ctrl+n", "move": "ctrl+h",
		"scroll": "ctrl+s", "session": "ctrl+o"}
	for _, tc := range []struct {
		name             string
		zellijOn, tmuxOn bool
	}{
		{"no zellij modes", false, true},
		{"no tmux prefix", true, false},
		{"neither", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startWith(t, strings.Replace(settings, "return {",
				fmt.Sprintf("return { zellij_modes = %v, tmux_prefix = %v,", tc.zellijOn, tc.tmuxOn), 1))
			km, err := s.Rex.Keymap(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(km.Diagnostics) > 0 {
				t.Errorf("diagnostics: %+v", km.Diagnostics)
			}
			bound, modes, actions := map[string]string{}, map[string]bool{}, map[string]bool{}
			for _, b := range km.Bindings {
				bound[b.Mode+"/"+strings.Join(b.Keys, " ")] = b.Action
			}
			for _, m := range km.Modes {
				modes[m.Name] = true
			}
			for _, a := range km.Actions {
				actions[a.Name] = true
			}
			check := func(name, prefix string, on bool) {
				t.Helper()
				if modes[name] != on {
					t.Errorf("%s mode there: %v, want %v", name, modes[name], on)
				}
				if got := bound["/"+prefix] == "client.mode.enter"; got != on {
					t.Errorf("%s enters a mode: %v, want %v", prefix, got, on)
				}
			}
			for name, prefix := range zellij {
				check(name, prefix, tc.zellijOn)
			}
			check("tmux", "ctrl+b", tc.tmuxOn)
			check("locked", "ctrl+g", tc.zellijOn || tc.tmuxOn)
			for key, action := range bound {
				mode, _, _ := strings.Cut(key, "/")
				if _, ok := zellij[mode]; ok && !tc.zellijOn || mode == "tmux" && !tc.tmuxOn {
					t.Errorf("%s is bound (to %s) with its mode off", key, action)
				}
			}
			if tc.tmuxOn && !tc.zellijOn && bound["tmux/["] != "rexkit_tmux_unbound" {
				t.Errorf("tmux/[ runs %s with no scroll mode to enter, want it to leave the prefix", bound["tmux/["])
			}
			if bound["/ctrl+shift+space"] != "rexkit_goto" || bound["/f1"] != "rexkit_keys" || bound["/alt+n"] == "" {
				t.Errorf("global keys: ⌃⇧Space %q, F1 %q, ⌥N %q", bound["/ctrl+shift+space"], bound["/f1"], bound["/alt+n"])
			}
			for _, a := range []string{"rexkit_files", "rexkit_rename_pane", "rexkit_scrollback", "rexkit_scratch", "rexkit_next_agent", "rexkit_settings"} {
				if !actions[a] {
					t.Errorf("no %s action for the palette", a)
				}
			}
			if tc.zellijOn || tc.tmuxOn {
				return
			}
			// With no modes the key guide lists the global keys alone.
			sess, _ := s.CreateSession(t, "guide", cat("A", ""))
			o := open(t, s, sess.ID, sess.BlockID, "rexkit_keys", nil, "Global")
			screen := s.Capture(t, sess.ID, o.Block)
			for _, want := range []string{"Go to", "F1"} {
				if !strings.Contains(screen, want) {
					t.Errorf("the guide lacks %q:\n%s", want, screen)
				}
			}
			for _, gone := range []string{"Pane", "Tab"} { // the mode chips TestKeyGuide sees
				if strings.Contains(screen, gone) {
					t.Errorf("the guide lists %q with no modes:\n%s", gone, screen)
				}
			}
		})
	}
}

// TestKeyGuide opens the key guide (F1): it lists the live bindings by mode,
// and ⏎ on a search result runs that action about the pane it came from.
func TestKeyGuide(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "guide", hsplit(0.5, cat("A", ""), cat("B", "")))
	origin := blocks[0]
	focus(t, s, sess.ID, origin)
	o := open(t, s, sess.ID, origin, "rexkit_keys", nil, "Global")
	screen := s.Capture(t, sess.ID, o.Block)
	for _, want := range []string{"Pane", "Tab", "Go to", "⌥H", "Focus left or previous tab", "F1", "Key guide"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the guide lacks %q:\n%s", want, screen)
		}
	}
	in := append(harness.Type(t, o.Block, "split right"), harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
	outs := s.Drive(t, sess.ID, in...)
	done := outs[len(outs)-1]
	res := result(t, done)
	if res["action"] != "invoke" || res["block_id"] != origin {
		t.Fatalf("result %v, want an invoke about the origin", res)
	}
	split, ok := queued(done, "pane.split")
	if !ok || split["direction"] != "right" {
		t.Errorf("queued %s (split %v), want pane.split right", names(done), split)
	}
	waitNoFloats(t, s, sess.ID)
	assertFocus(t, s, sess.ID, origin, "")
}
