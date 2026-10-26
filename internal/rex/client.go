// Package rex is a typed client for the Rex server. It drives the public rex
// CLI (the server socket speaks an undocumented protocol), always with
// --autostart=false and a timeout, and turns failures into typed errors.
package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout bounds a call whose context has no deadline.
const DefaultTimeout = 5 * time.Second

// Output is what one run of the rex CLI produced.
type Output struct {
	Stdout, Stderr []byte
	ExitCode       int
}

// Runner runs the rex CLI. It returns an error only when the program could
// not run at all; a non-zero exit is reported in Output.ExitCode.
type Runner func(ctx context.Context, argv []string, stdin []byte) (Output, error)

// Client calls Rex through the CLI. The zero value is not usable; use New.
type Client struct {
	bin     string
	server  string
	env     []string
	timeout time.Duration
	run     Runner
}

// Option configures a Client.
type Option func(*Client)

// WithServer targets a server address or host label (rex -S). Without it (or
// with an empty addr) the client targets $REX_SERVER — read from the WithEnv
// environment when one is given — and passes it on as an explicit -S, because
// the CLI silently falls back to the local server when $REX_SERVER cannot be
// reached. With neither, the CLI uses the local server.
func WithServer(addr string) Option { return func(c *Client) { c.server = addr } }

// WithBinary uses the given rex executable instead of locating one.
func WithBinary(path string) Option { return func(c *Client) { c.bin = path } }

// WithEnv runs the CLI with exactly this environment instead of inheriting
// the process environment (the isolated test harness uses it).
func WithEnv(env []string) Option { return func(c *Client) { c.env = env } }

// WithRunner replaces process execution (for tests).
func WithRunner(r Runner) Option { return func(c *Client) { c.run = r } }

// New returns a client, locating the rex binary unless WithBinary is given.
func New(opts ...Option) (*Client, error) {
	c := &Client{timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(c)
	}
	if c.server == "" {
		c.server = c.envValue("REX_SERVER")
	}
	if c.run == nil {
		c.run = c.execRunner
	}
	if c.bin == "" {
		bin, err := Locate()
		if err != nil {
			return nil, err
		}
		c.bin = bin
	}
	return c, nil
}

// Binary is the rex executable the client runs.
func (c *Client) Binary() string { return c.bin }

// Server is the server address the client targets (passed as -S): the
// WithServer address, else $REX_SERVER, else empty for the local server.
func (c *Client) Server() string { return c.server }

// envValue looks name up in the CLI's environment: the WithEnv one when given
// (the last entry wins, as in exec), otherwise the process environment.
func (c *Client) envValue(name string) string {
	if c.env == nil {
		return os.Getenv(name)
	}
	value := ""
	for _, kv := range c.env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value = v
		}
	}
	return value
}

// Call runs any server API method. A top-level session_id in args attaches
// the call to that session. out may be nil.
func (c *Client) Call(ctx context.Context, method string, args, out any) error {
	if args == nil {
		args = struct{}{}
	}
	in, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("rex %s: %w", method, err)
	}
	stdout, err := c.exec(ctx, method, in, "api", "call", method, "-")
	if err != nil {
		return err
	}
	return decode(method, stdout, out)
}

// script runs Lua through `rex do -`. Its return value is decoded into out.
func (c *Client) script(ctx context.Context, op string, source []byte, args, out any, flags ...string) error {
	argv := append([]string{"do", "-", "--json"}, flags...)
	if args != nil {
		in, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("rex %s: %w", op, err)
		}
		argv = append(argv, "--args", string(in))
	}
	stdout, err := c.exec(ctx, op, source, argv...)
	if err != nil {
		return err
	}
	return decode(op, stdout, out)
}

// exec runs the CLI with the global flags and maps failures to errors. On a
// non-zero exit it still returns whatever the CLI printed.
func (c *Client) exec(ctx context.Context, op string, stdin []byte, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	out, err := c.run(ctx, c.argv(ctx, args), stdin)
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("rex %s: %w", op, ctx.Err())
	case err != nil:
		return nil, &Error{Op: op, Message: cleanText(err.Error(), 300), kind: ErrUnavailable}
	case out.ExitCode != 0:
		return out.Stdout, parseError(op, out.ExitCode, string(out.Stderr))
	}
	return out.Stdout, nil
}

func (c *Client) argv(ctx context.Context, args []string) []string {
	argv := []string{c.bin, "--autostart=false", "--color=never", "--timeout", cliTimeout(ctx)}
	if c.server != "" {
		argv = append(argv, "-S", c.server)
	}
	return append(argv, args...)
}

// cliTimeout passes the context's remaining time on to the CLI so it gives
// up (with a clean message) just before the context does.
func cliTimeout(ctx context.Context) string {
	d := DefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		d = time.Until(deadline) - 20*time.Millisecond
	}
	return strconv.FormatInt(max(d.Milliseconds(), 50), 10) + "ms"
}

func decode(op string, data []byte, out any) error {
	data = bytes.TrimSpace(data)
	if out == nil || len(data) == 0 || string(data) == "null" {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("rex %s: decoding result: %w", op, err)
	}
	return nil
}

func (c *Client) command(ctx context.Context, argv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = c.env
	return cmd
}

func (c *Client) execRunner(ctx context.Context, argv []string, stdin []byte) (Output, error) {
	cmd := c.command(ctx, argv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		out.ExitCode = exit.ExitCode()
		return out, nil
	}
	return out, err
}
