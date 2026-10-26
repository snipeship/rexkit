package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runner runs a helper program (git, zoxide, pbcopy, open) and returns its
// standard output. Arguments are argv elements, never shell text. Tests
// substitute fakes.
type runner func(ctx context.Context, dir, stdin, name string, args ...string) ([]byte, error)

// runCommand is the real runner. Git never takes optional locks, so a
// status refresh can't block the user's own git commands.
func runCommand(ctx context.Context, dir, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(stdin)
	cmd.WaitDelay = 200 * time.Millisecond
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && stderr.Len() > 0 {
			msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
			return out, fmt.Errorf("%s: %s", name, msg)
		}
		return out, err
	}
	return out, nil
}
