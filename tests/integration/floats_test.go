//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rexkit/tests/integration/harness"
)

// TestRetiredSettings: a settings.lua from before Rex drew floats natively
// (backdrop) or from before floating panes (scratch_label) still loads,
// with one line saying the setting is no longer used.
func TestRetiredSettings(t *testing.T) {
	for _, setting := range []string{`backdrop = "dim"`, `scratch_label = "Parked"`} {
		name := strings.Fields(setting)[0]
		t.Run(name, func(t *testing.T) {
			s := startWith(t, strings.Replace(settings, "return {", "return { "+setting+",", 1))
			sess, blocks := s.CreateSession(t, "retired", cat("A", ""))
			res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_diagnostics", nil))
			warnings := fmt.Sprint(res["warnings"])
			if strings.Count(warnings, "no longer used") != 1 || !strings.Contains(warnings, name+" is no longer used") ||
				strings.Contains(warnings, "unknown setting") {
				t.Errorf("warnings %s, want the one line about %s", warnings, name)
			}
			if failed := fmt.Sprint(res["failed"]); failed != "[]" && failed != "map[]" && failed != "<nil>" {
				t.Errorf("modules failed: %s", failed)
			}
		})
	}
}

// TestToastFloat: a toast is a float of its own in the window's top-right
// corner, a row tall and as wide as `rexkit toast --measure` says, titled,
// and it leaves focus where it was.
func TestToastFloat(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "toast", hsplit(0.5, cat("A", ""), cat("B", "")))
	focus(t, s, sess.ID, blocks[0])
	res := result(t, act(t, s, sess.ID, blocks[0], "rexkit_notify", map[string]string{"message": "Copied a3f9c21", "kind": "ok", "title": "Hints"}))
	bounds, _ := res["bounds"].([]any)
	if len(bounds) != 4 || bounds[1].(float64) != 0 || bounds[0].(float64)+bounds[2].(float64) < 0.9999 {
		t.Fatalf("bounds %v, want the top-right corner", res["bounds"])
	}
	var toast float
	harness.WaitFor(t, 5*time.Second, "the toast", func() bool {
		for _, f := range floats(t, s, sess.ID) {
			if f.Block.Label == "rexkit:toast" {
				toast = f
				return true
			}
		}
		return false
	})
	if got := fmt.Sprint(toast.Layer.Bounds); got != fmt.Sprint(bounds) {
		t.Errorf("layer bounds %s, want %s", got, fmt.Sprint(bounds))
	}
	waitScreen(t, s, sess.ID, toast.Block.ID, "Copied a3f9c21")
	if got := title(t, s, sess.ID, toast.Block.ID); got != "Hints" {
		t.Errorf("title %q, want Hints", got)
	}
	if f, _ := s.Focus(t, sess.ID); f != blocks[0] {
		t.Errorf("focus %s, want A's", f)
	}
}
