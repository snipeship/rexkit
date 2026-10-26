package popup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"rexkit/internal/state"
)

// ResultFile is the name of the result handed to the Lua layer.
const ResultFile = "result.json"

// The handshake vocabulary (lua/tools.lua apply()). Lua forwards
// ⏎, ⎋ (and the switcher's ⌃C) into the tool's pty and waits for a Result:
// ActionStay means the tool used the key and stays open, ActionClose that it
// closed without going anywhere; any other action names a commit that
// switches the app to SessionID/WindowID.
const (
	ActionStay      = "stay"
	ActionClose     = "close"
	ActionGoto      = "goto"       // goto jumped
	ActionInvoke    = "invoke"     // keys: run Invoke with Args after closing
	ActionRename    = "rename"     // rename: back to the origin pane
	ActionFilesOpen = "files.open" // files opened a tab, split or editor
)

// The handshake's timing. Lua waits AwaitTimeout for a result
// (the default of `rexkit await`, which Lua does not override); a tool that
// holds a handshake key back (its data is not ready yet) answers ActionStay
// after HandshakeGrace, well inside that, and acts on the key by itself
// once it can, as if typed without the handshake.
const (
	AwaitTimeout   = 1500 * time.Millisecond
	HandshakeGrace = time.Second
)

// CommitFile is the one-shot commit marker. Lua's handshake creates
// <state>/commit right before it writes ⏎ into the tool, and removes it
// again after the await. A tool cannot tell that ⏎ from one typed while the
// picker mode is not active (after a config reload, say), and only for the
// former does anyone read its result: see TakeCommit.
const CommitFile = "commit"

// TakeCommit consumes the commit marker and reports whether there was one,
// i.e. whether the ⏎ being handled came from Lua's handshake, which reads
// the result (and so can switch the app to another session). A tool calls
// it once for every ⏎ it reads, whatever it then does with the key, so a
// marker never outlives its ⏎. Removing is the atomic test-and-clear: of
// two callers only one sees true.
func TakeCommit(dir string) bool {
	_, ok := TakeCommitAt(dir)
	return ok
}

// TakeCommitAt is TakeCommit that also reports when Lua wrote the marker,
// which is when it started to wait for the result (see GraceLeft).
func TakeCommitAt(dir string) (time.Time, bool) {
	if dir == "" {
		return time.Time{}, false
	}
	path := filepath.Join(dir, CommitFile)
	info, err := os.Stat(path)
	if err != nil || os.Remove(path) != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// GraceLeft is how long a tool may still hold a handshake key whose marker
// Lua wrote at `at` before it must answer ActionStay: HandshakeGrace counts
// from Lua's write, not from when a slow-starting tool read the key.
func GraceLeft(at time.Time) time.Duration {
	return min(HandshakeGrace, max(0, HandshakeGrace-time.Since(at)))
}

// IsHandshakeKey reports the keys (in Bubble Tea notation) that Lua may be
// waiting on an answer for. A tool that consumes one of them without
// committing or closing answers ActionStay.
func IsHandshakeKey(key string) bool {
	return key == "enter" || key == "esc" || key == "ctrl+c"
}

// Answer writes a bare Result{Action: action} for the Lua layer. It does
// nothing (and reports no error) without a state directory: then no one is
// waiting.
func Answer(dir, action string) error {
	if dir == "" {
		return nil
	}
	_, err := WriteResult(dir, Result{Action: action})
	return err
}

// Result is what a committing tool asks the Lua layer to do: switch the app
// to SessionID/WindowID (a client action only Lua can queue), then optionally
// run Invoke with Args.
type Result struct {
	Seq       int64           `json:"seq"`
	Action    string          `json:"action"`
	SessionID string          `json:"session_id"`
	WindowID  string          `json:"window_id,omitempty"`
	BlockID   string          `json:"block_id,omitempty"`
	Invoke    string          `json:"invoke,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
}

// WriteResult atomically writes r to dir/result.json, stamping a sequence
// number if r has none, and returns what it wrote.
func WriteResult(dir string, r Result) (Result, error) {
	if dir == "" {
		return r, errors.New("popup: no state directory to write the result to")
	}
	if r.Seq == 0 {
		r.Seq = time.Now().UnixNano()
	}
	return r, state.WriteJSON(filepath.Join(dir, ResultFile), r)
}

// pollInterval is how often AwaitResult looks for the result.
const pollInterval = 5 * time.Millisecond

// AwaitResult waits until dir/result.json holds a result with Seq > after.
// Lua deletes any old result before asking the tool to commit, so after is
// usually 0.
func AwaitResult(ctx context.Context, dir string, after int64) (Result, error) {
	path := filepath.Join(dir, ResultFile)
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		var r Result
		err := state.ReadJSON(path, &r)
		switch {
		case err == nil && r.Seq > after:
			return r, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			// A torn read cannot happen (writes are atomic), so this is a
			// real problem such as a permission error.
			return Result{}, err
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-tick.C:
		}
	}
}

// ExitTimeout is the await exit status when no result arrived in time.
const ExitTimeout = 3

// ExitError carries a process exit status.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
func (e *ExitError) ExitCode() int { return e.Code }

// Await implements `rexkit await --state DIR [--timeout D] [--after SEQ]`:
// it prints the result as JSON and returns nil, or returns an *ExitError with
// ExitTimeout when none arrives in time.
func Await(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("await", flag.ContinueOnError)
	dir := fs.String("state", "", "tool state `DIR` to watch")
	timeout := fs.Duration("timeout", AwaitTimeout, "give up after `DURATION`")
	after := fs.Int64("after", 0, "only accept a result with a sequence number above `SEQ`")
	if err := ParseFlags(fs, args); err != nil {
		return err
	}
	if *dir == "" {
		return &ExitError{Code: ExitUsage, Err: errors.New("await: --state is required")}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	r, err := AwaitResult(ctx, *dir, *after)
	if err != nil {
		if ctx.Err() != nil {
			return &ExitError{Code: ExitTimeout, Err: fmt.Errorf("await: no result within %s", *timeout)}
		}
		return err
	}
	return json.NewEncoder(stdout).Encode(r)
}
