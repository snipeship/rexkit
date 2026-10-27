package scrollback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
)

// Main runs `rexkit scrollback` with the arguments after the subcommand.
func Main(args []string) error {
	c, err := parseArgs(args)
	if err != nil {
		return err
	}
	// The float closing under us (SIGHUP) or a kill must still clean up.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	sys := osSystem()
	sys.tempDir = c.popup.State // GC'd with the tool if it is ever killed
	if _, err := rex.New(); err != nil && c.demo {
		return show(ctx, c, demoSource, sys) // no Rex: nothing to close
	}
	return run(ctx, c, sys, popup.RunOptions{LazySelf: true})
}

// run shows the scrollback in the tool's float (popup.Run): closing the
// float afterwards, whatever happens, returns focus to the pane it came from
// (exiting alone would focus another pane). The float's own layer is only
// looked up to claim the state directory, so a viewer started by hand in an
// ordinary pane runs too.
func run(ctx context.Context, c config, sys system, ro popup.RunOptions) error {
	return popup.Run("scrollback", c.popup, ro, func(env *popup.Env) error {
		if c.popup.State != "" {
			_, _ = env.ResolveSelf(ctx) // best effort: GC also removes dirs by age
		}
		src, err := sourceFor(c, env.API, env.Popup.Options().Session, toolsDir(env))
		if err != nil {
			return report(ctx, sys, err)
		}
		return show(ctx, c, src, sys)
	})
}

// sourceFor picks what to show: a capture handed over (--page), the demo
// sample, or the origin pane (the pane the tool runs in when started by
// hand). tools is where Search hands pages over.
func sourceFor(c config, api rex.API, session, tools string) (source, error) {
	if c.page != "" {
		return pageSource(c.page, tools), nil
	}
	if c.demo {
		return demoSource, nil
	}
	block := c.popup.OriginBlock
	if block == "" {
		block = os.Getenv("REX_BLOCK")
	}
	if session == "" || block == "" {
		return nil, errors.New("no pane to read: run it inside Rex or pass --session and --origin-block")
	}
	return paneSource(api, session, block, c.plain), nil
}

// toolsDir is where Search hands pages over, "" when rexkit's state dir
// cannot be found.
func toolsDir(env *popup.Env) string {
	if env.DirsErr != nil {
		return ""
	}
	return env.Dirs.ToolsDir()
}

// system is what show needs from the process, as a seam for tests.
type system struct {
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	tempDir string              // parent of the spool: the state dir, or "" for os.TempDir()
	screen  []tea.ProgramOption // extra Bubble Tea options (tests set a size)
	find    finder
	run     func(ctx context.Context, c command) error
}

func osSystem() system {
	s := system{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, find: findProgram}
	s.run = func(ctx context.Context, c command) error { return runCommand(ctx, c, s.stdin, s.stdout, s.stderr) }
	return s
}

// show loads the capture and runs the viewer on it. The spool is removed
// before show returns, so before the float closes.
func show(ctx context.Context, c config, src source, sys system) error {
	sp, err := newSpool(sys.tempDir)
	if err != nil {
		return report(ctx, sys, err)
	}
	defer sp.remove()

	ready, theme, err := load(ctx, prepare(src, sp, c.nvim, sys.find, target{c.line, c.search}), sys.stdin, sys.stdout, sys.screen...)
	switch {
	case errors.Is(err, errCancelled):
		return nil
	case err != nil:
		return err
	}
	err = sys.run(ctx, ready.command(c.start, theme))
	var notStarted startError
	if errors.As(err, &notStarted) {
		return report(ctx, sys, err)
	}
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("viewer: %w", err)
	}
	return nil
}

// report shows err on an error card until a key, then returns it.
func report(ctx context.Context, sys system, err error) error {
	_, _, _ = load(ctx, failed(err), sys.stdin, sys.stdout, sys.screen...)
	return err
}
