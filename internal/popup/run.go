package popup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// SelfTimeout bounds finding the tool's own layer.
const SelfTimeout = 3 * time.Second

// titleOut is where Run writes the title: the tool's terminal.
var titleOut io.Writer = os.Stdout

// gcTimeout bounds the launch-time garbage collection of tool directories.
const gcTimeout = 10 * time.Second

// RunOptions tune Run for one tool.
type RunOptions struct {
	// Title is the float's title, which Rex shows in its header ("Go to").
	Title string
	// NoKeys skips HoldKeys, for a tool that never has focus (toast).
	NoKeys bool
	// LazySelf skips finding the tool's layer before body runs: the tool
	// calls Env.ResolveSelf itself (usually from a tea.Cmd) so the first
	// frame is not held up by a round trip to Rex. A tool outside Rex then
	// runs anyway.
	LazySelf bool
	// API replaces the rex CLI client (for tests).
	API rex.API
}

// Env is what Run hands a tool's body.
type Env struct {
	Ctx   context.Context // cancelled when body returns
	API   rex.API
	Popup *Popup
	// Self is the tool's own block and layer; the zero Self with LazySelf.
	Self Self
	// Dirs are rexkit's state and cache dirs, or the zero Dirs (and DirsErr)
	// when they cannot be found.
	Dirs    state.Dirs
	DirsErr error
}

// ResolveSelf finds the tool's own layer and claims the state directory for
// it (best effort: without a claim GC still removes the directory after
// state.ToolDirMaxAge). It is what Run does up front unless LazySelf is set.
func (e *Env) ResolveSelf(ctx context.Context) (Self, error) {
	ctx, cancel := context.WithTimeout(ctx, SelfTimeout)
	defer cancel()
	self, err := e.Popup.Self(ctx)
	if err == nil {
		_ = e.Popup.Claim(self)
	}
	return self, err
}

// Run is the lifecycle every floating tool shares, so that each tool's Main is
// only flag parsing plus building its model:
//
//   - set the title (RunOptions.Title) before anything is drawn, so the
//     float's header never shows the program and its folder first;
//   - hold the keys (raw mode) at once, so keys Lua writes are not echoed;
//   - connect to Rex and guard against panics (the float closes either way);
//   - find and claim the tool's own layer (see RunOptions.LazySelf);
//   - collect stale tool directories in the background;
//   - run body;
//   - on every exit path, error or not: answer ActionClose when the tool
//     answered nothing since it started (Lua may be waiting on a ⏎ or ⎋),
//     then Close the float within CloseTimeout.
//
// Errors come back as "<name>: …"; outside Rex (unless LazySelf) the error
// suggests the demo. Errors that arrive after the float was closed (closing
// it hangs up the tool's terminal) are dropped.
func Run(name string, po Options, ro RunOptions, body func(*Env) error) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", name, err)
		}
	}()
	if ro.Title != "" && os.Getenv("REX_BLOCK") != "" {
		_, _ = io.WriteString(titleOut, ansi.SetWindowTitle(ro.Title))
	}
	if !ro.NoKeys {
		defer HoldKeys()()
	}
	api := ro.API
	if api == nil {
		client, err := rex.New()
		if err != nil {
			return err
		}
		api = client
	}
	p := New(api, po)
	defer p.Guard()
	started := time.Now().UnixNano()
	defer func() { err = errors.Join(err, p.finish(started)) }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env := &Env{Ctx: ctx, API: api, Popup: p}
	env.Dirs, env.DirsErr = state.Default()
	if !ro.LazySelf {
		self, err := env.ResolveSelf(ctx)
		if errors.Is(err, ErrOutsideRex) {
			return OutsideRexError(name)
		}
		if err != nil {
			return err
		}
		env.Self = self
	}
	if env.DirsErr == nil {
		go func() {
			gctx, cancel := context.WithTimeout(ctx, gcTimeout)
			defer cancel()
			_, _ = GC(gctx, api, env.Dirs)
		}()
	}
	err = body(env)
	if p.isClosed() {
		return nil
	}
	return err
}

// OutsideRexError is the one hint every tool gives when started outside Rex:
// "run it inside Rex, or try `rexkit demo goto`". It matches ErrOutsideRex.
func OutsideRexError(name string) error { return outsideRex{name} }

type outsideRex struct{ tool string }

func (e outsideRex) Error() string {
	return "run it inside Rex, or try `rexkit demo " + e.tool + "`"
}

func (e outsideRex) Unwrap() error { return ErrOutsideRex }

// finish answers ActionClose unless the tool answered after started, then
// closes the float.
func (p *Popup) finish(started int64) error {
	var errs []error
	if dir := p.opts.State; dir != "" && !answeredSince(dir, started) {
		errs = append(errs, Answer(dir, ActionClose))
	}
	ctx, cancel := context.WithTimeout(context.Background(), CloseTimeout)
	defer cancel()
	errs = append(errs, p.Close(ctx))
	return errors.Join(errs...)
}

// answeredSince reports a result in dir written after seq. Lua deletes the
// old result before it forwards a key, so a result that is still there
// answers the last key Lua forwarded.
func answeredSince(dir string, seq int64) bool {
	var r Result
	return state.ReadJSON(filepath.Join(dir, ResultFile), &r) == nil && r.Seq > seq
}

func (p *Popup) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
