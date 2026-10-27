//go:build integration

package integration

import (
	"testing"

	"rexkit/tests/integration/harness"
)

// tool is how to open a floating tool and how to back out of it.
type tool struct {
	action string
	args   any
	ready  string // text the tool shows once it is up
	label  string // its float's block label
	// dismiss backs out: through the picker mode's ⎋ when it has one, else
	// keys typed into the tool.
	picker bool
	keys   []string
}

var tools = []tool{
	{action: "rexkit_goto", ready: "Search sessions", label: "rexkit:goto", picker: true},
	{action: "rexkit_files", ready: "Search this folder", label: "rexkit:files", picker: true},
	{action: "rexkit_keys", ready: "Search keys", label: "rexkit:keys", picker: true},
	{action: "rexkit_rename_pane", ready: "Rename pane", label: "rexkit:rename", picker: true},
	{action: "rexkit_rename_tab", ready: "Rename tab", label: "rexkit:rename", picker: true},
	{action: "rexkit_rename_session", ready: "Rename session", label: "rexkit:rename", picker: true},
	{action: "rexkit_scrollback", ready: "Scrollback", label: "rexkit:scrollback", keys: []string{"q"}},
	{action: "rexkit_search", ready: "Search every pane", label: "rexkit:search", picker: true},
}

// TestToolsOpenAsFloatsAndRestoreFocus opens every tool from the second of
// three panes and backs out again: the float is labelled, focused, enters
// the picker mode when it has a handshake, and on the way out focus returns
// to exactly the pane it came from, zoomed again if it was zoomed.
func TestToolsOpenAsFloatsAndRestoreFocus(t *testing.T) {
	s := start(t)
	for _, zoomed := range []bool{false, true} {
		for _, tl := range tools {
			name := tl.action
			if zoomed {
				name += "/zoomed"
			}
			t.Run(name, func(t *testing.T) {
				sess, blocks := s.CreateSession(t, "tools", hsplit(0.5, cat("A", ""), vsplit(0.5, cat("B", ""), cat("C", ""))))
				origin := blocks[1]
				focus(t, s, sess.ID, origin)
				wantZoom := ""
				if zoomed {
					zoom(t, s, sess.ID, origin)
					wantZoom = origin
				}
				o := open(t, s, sess.ID, origin, tl.action, tl.args, tl.ready)

				fl := floats(t, s, sess.ID)
				if len(fl) != 1 || fl[0].Block.ID != o.Block || fl[0].Block.Label != tl.label {
					t.Fatalf("floats = %+v, want one labelled %s", fl, tl.label)
				}
				if f, _ := s.Focus(t, sess.ID); f != o.Block {
					t.Errorf("focus %s, want the tool %s", f, o.Block)
				}
				_, picker := queued(o.Outcome, "client.mode.enter")
				if picker != tl.picker {
					t.Errorf("entered the picker mode: %v, want %v (queued %s)", picker, tl.picker, names(o.Outcome))
				}

				if tl.picker {
					out := act(t, s, sess.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
					if got := names(out); got != "client.mode.exit_all" {
						t.Errorf("dismiss queued %q", got)
					}
				} else {
					var in []harness.Input
					for _, k := range tl.keys {
						in = append(in, harness.Press(t, o.Block, k))
					}
					s.Drive(t, sess.ID, in...)
				}
				waitNoFloats(t, s, sess.ID)
				assertFocus(t, s, sess.ID, origin, wantZoom)
			})
		}
	}
}
