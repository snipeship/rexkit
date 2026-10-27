package install

import (
	"context"
	"testing"
)

// The installer's and doctor's `rex config check` must not start an agent
// watcher: lua/attend.lua starts none when REXKIT_CHECK is set.
func TestExecRunnerMarksConfigChecks(t *testing.T) {
	out, err := execRunner(context.Background(), []string{"/bin/sh", "-c", `printf %s "$REXKIT_CHECK"`}, nil)
	if err != nil || string(out.Stdout) != "1" {
		t.Errorf("REXKIT_CHECK = %q (%v), want 1", out.Stdout, err)
	}
}
