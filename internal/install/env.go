package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Env is the outside world the installer works in: paths, programs, time and
// output. Tests build one over temp dirs with a fake Run, so no test ever
// runs go, rex or a built binary, or touches the real ~/.config.
type Env struct {
	Paths    Paths
	Rex      string // the rex CLI; empty when Rex is not installed
	Go       string // the go tool; empty when it is missing
	Defaults string // macOS defaults(1), for reading app preferences; empty elsewhere
	Run      rex.Runner
	LookPath func(string) (string, error)
	Now      func() time.Time
	Out      io.Writer
	Color    bool // style output: Out is a terminal
	Dark     bool // the terminal has a dark background

	reloaded bool // Rex just reloaded the config: its new watcher may still be starting
}

// DefaultEnv is the real environment, printing to out.
func DefaultEnv(out *os.File) (*Env, error) {
	paths, err := PathsFrom(os.Getenv)
	if err != nil {
		return nil, err
	}
	e := &Env{
		Paths:    paths,
		Run:      execRunner,
		LookPath: lookPath,
		Now:      time.Now,
		Out:      out,
		Color:    isTerminal(out) && os.Getenv("NO_COLOR") == "",
		Dark:     ui.NewScheme().Dark(),
	}
	e.Rex, _ = rex.Locate()
	e.Go, _ = lookPath("go")
	if runtime.GOOS == "darwin" {
		e.Defaults = "/usr/bin/defaults"
	}
	return e, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// lookPath also searches the usual Homebrew and Go locations, because Rex
// may start rexkit with a PATH that lacks them.
func lookPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s: not found", name)
}

// checkEnv tells rexkit's Lua layer that a `rex config check` of the
// installer or doctor loads it (lua/attend.lua): it starts no agent watcher.
const checkEnv = "REXKIT_CHECK=1"

// execRunner runs a program in its own process group, so that Ctrl-C in the
// terminal reaches only rexkit: the installer decides what an interrupt
// stops (through ctx), and a reload or a rollback in flight runs to its end.
// Cancelling ctx kills the whole group, go build's compilers included. Only
// `rex config check` runs the config in the program it starts, and with
// checkEnv it starts no watcher there.
func execRunner(ctx context.Context, argv []string, stdin []byte) (rex.Output, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), checkEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	out := rex.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		out.ExitCode = exit.ExitCode()
		return out, nil
	}
	return out, err
}

// command runs a program and returns its stdout. A non-zero exit is an error
// that carries the end of its stderr; op names the step in errors.
func (e *Env) command(ctx context.Context, op string, argv ...string) ([]byte, error) {
	out, err := e.Run(ctx, argv, nil)
	if ierr := interrupted(ctx); ierr != nil {
		return nil, fmt.Errorf("%s: %w", op, ierr)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.ExitCode != 0 {
		return out.Stdout, fmt.Errorf("%s failed (exit status %d)%s", op, out.ExitCode, indentTail(out.Stderr, 12))
	}
	return out.Stdout, nil
}

// indentTail formats the last lines of a program's error output to follow
// an error message.
func indentTail(stderr []byte, lines int) string {
	text := strings.TrimSpace(string(stderr))
	if text == "" {
		return ""
	}
	all := strings.Split(text, "\n")
	all = all[max(0, len(all)-lines):]
	return ":\n    " + strings.Join(all, "\n    ")
}
