//go:build integration

package integration

import (
	"testing"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// TestRenameSetsLabels renames a pane, a tab and a session through the
// dialog: the prefill is selected, so typing replaces it, and ⏎ (through the
// picker mode) saves and closes, returning focus — and zoom — to the pane.
func TestRenameSetsLabels(t *testing.T) {
	s := start(t)
	tests := []struct {
		action string
		ready  string
		zoomed bool
		label  func(v rex.View, sess harness.Session, origin string) string
	}{
		{"rexkit_rename_pane", "Rename pane", false, func(v rex.View, _ harness.Session, origin string) string {
			_, _, b, _ := v.FindBlock(origin)
			return b.Label
		}},
		{"rexkit_rename_pane", "Rename pane", true, func(v rex.View, _ harness.Session, origin string) string {
			_, _, b, _ := v.FindBlock(origin)
			return b.Label
		}},
		{"rexkit_rename_tab", "Rename tab", false, func(v rex.View, sess harness.Session, _ string) string {
			w, _ := v.Window(sess.WindowID)
			return w.Label
		}},
		{"rexkit_rename_session", "Rename session", false, func(v rex.View, _ harness.Session, _ string) string {
			return v.Label
		}},
	}
	for _, tt := range tests {
		name := tt.action
		if tt.zoomed {
			name += "/zoomed"
		}
		t.Run(name, func(t *testing.T) {
			sess, blocks := s.CreateSession(t, "before", hsplit(0.5, cat("left", ""), cat("right", "")))
			origin := blocks[1]
			focus(t, s, sess.ID, origin)
			wantZoom := ""
			if tt.zoomed {
				zoom(t, s, sess.ID, origin)
				wantZoom = origin
			}
			o := open(t, s, sess.ID, origin, tt.action, nil, tt.ready)
			in := append(harness.Type(t, o.Block, "renamed"), harness.Act(t, o.Block, "rexkit_commit", map[string]string{"key": "enter"}))
			outs := s.Drive(t, sess.ID, in...)
			if res := result(t, outs[len(outs)-1]); res["action"] != "rename" {
				t.Fatalf("result %v, want rename", res)
			}
			waitNoFloats(t, s, sess.ID)
			if got := tt.label(s.View(t, sess.ID), sess, origin); got != "renamed" {
				t.Errorf("label %q, want renamed", got)
			}
			assertFocus(t, s, sess.ID, origin, wantZoom)
		})
	}
}
