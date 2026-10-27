package rex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recorder is a Runner that records invocations and replies with a canned
// output.
type recorder struct {
	mu    sync.Mutex
	argv  [][]string
	stdin [][]byte
	reply func(argv []string, stdin []byte) Output
}

func (r *recorder) run(_ context.Context, argv []string, stdin []byte) (Output, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argv = append(r.argv, argv)
	r.stdin = append(r.stdin, stdin)
	if r.reply == nil {
		return Output{}, nil
	}
	return r.reply(argv, stdin), nil
}

func newTestClient(t *testing.T, reply func(argv []string, stdin []byte) Output, opts ...Option) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{reply: reply}
	opts = append([]Option{WithBinary("/bin/rex"), WithRunner(rec.run)}, opts...)
	c, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

func stdout(s string) func([]string, []byte) Output {
	return func([]string, []byte) Output { return Output{Stdout: []byte(s)} }
}

func TestCallPassesGlobalFlagsAndArgsOnStdin(t *testing.T) {
	c, rec := newTestClient(t, stdout(`{"revision":3}`), WithServer("http://127.0.0.1:9"))
	var out struct{ Revision int }
	if err := c.Call(context.Background(), "session.focus_block", map[string]string{"session_id": "s1"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Revision != 3 {
		t.Fatalf("decoded %+v", out)
	}
	argv := rec.argv[0]
	for _, want := range [][]string{{"--autostart=false"}, {"-S", "http://127.0.0.1:9"}, {"api", "call", "session.focus_block", "-"}} {
		if !containsRun(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	if i := slices.Index(argv, "--timeout"); i < 0 || !strings.HasSuffix(argv[i+1], "ms") {
		t.Errorf("argv %q lacks --timeout <n>ms", argv)
	}
	if got := string(rec.stdin[0]); got != `{"session_id":"s1"}` {
		t.Errorf("stdin = %s", got)
	}
}

func containsRun(argv, run []string) bool {
	for i := 0; i+len(run) <= len(argv); i++ {
		if slices.Equal(argv[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func TestTypedMethodsSendTheRightRequests(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		call   func(c *Client) error
		method string
		args   string
	}{
		{"view", func(c *Client) error { _, err := c.View(ctx, "s"); return err },
			"session.view", `{"session_id":"s"}`},
		{"focus block", func(c *Client) error { return c.FocusBlock(ctx, "s", "b") },
			"session.focus_block", `{"block_id":"b","session_id":"s"}`},
		{"focus window", func(c *Client) error { return c.FocusWindow(ctx, "s", "w") },
			"session.focus_window", `{"session_id":"s","window_id":"w"}`},
		{"zoom", func(c *Client) error { return c.ZoomBlock(ctx, "s", "b", true) },
			"session.zoom_block", `{"block_id":"b","session_id":"s","zoom":true}`},
		{"close layer", func(c *Client) error { return c.CloseLayer(ctx, "s", "l") },
			"session.close_layer", `{"layer_id":"l","session_id":"s"}`},
		{"block label", func(c *Client) error { return c.SetBlockLabel(ctx, "s", "b", "api") },
			"session.set_block_label", `{"block_id":"b","label":"api","session_id":"s"}`},
		{"window label", func(c *Client) error { return c.SetWindowLabel(ctx, "s", "w", "") },
			"session.set_window_label", `{"label":"","session_id":"s","window_id":"w"}`},
		{"session label", func(c *Client) error { return c.SetSessionLabel(ctx, "s", "work") },
			"session.set_label", `{"label":"work","session_id":"s"}`},
		{"move", func(c *Client) error {
			return c.MoveBlock(ctx, "s", MoveBlockRequest{BlockID: "b", AnchorID: "a", Side: "after"})
		}, "session.move_block", `{"anchor_block_id":"a","block_id":"b","session_id":"s","side":"after"}`},
		{"new layer", func(c *Client) error {
			_, err := c.NewLayer(ctx, "s", NewLayerRequest{Bounds: Rect{0.1, 0.1, 0.8, 0.8}, Focus: true,
				Layout: Command("Go to", "", "/bin/rexkit", "goto")})
			return err
		}, "session.new_layer", `{"bounds":[0.1,0.1,0.8,0.8],"focus":true,"layout":{"block":{"flavor":"com.superlogical.terminal.shell",` +
			`"label":"Go to","options":{"command":["/bin/rexkit","goto"],"exit":{"on_completion":true,"quick_exit_threshold_ms":0},"shell":"none"}}},"session_id":"s"}`},
		{"block call", func(c *Client) error { return c.Write(ctx, "s", "b", "\r") },
			"block.method", `{"args":{"data":"\r"},"block_id":"b","method":"write","session_id":"s"}`},
		{"perform", func(c *Client) error {
			return c.ClientPerform(ctx, "c", "session.select", map[string]string{"session_id": "s"})
		},
			"client.perform", `{"action":"session.select","args":{"session_id":"s"},"client_id":"c","origin":"api"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newTestClient(t, stdout(`{}`))
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			argv := rec.argv[0]
			if got := argv[len(argv)-2]; got != tt.method {
				t.Errorf("method = %s, want %s", got, tt.method)
			}
			if got := canonical(t, rec.stdin[0]); got != canonical(t, []byte(tt.args)) {
				t.Errorf("args = %s\nwant   %s", got, tt.args)
			}
		})
	}
}

func canonical(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("bad JSON %s: %v", data, err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestFailuresBecomeTypedErrors(t *testing.T) {
	tests := []struct {
		name   string
		output Output
		want   error
		msg    string
	}{
		{"not found", Output{ExitCode: 3, Stderr: []byte(`Error: stream response "control-3" failed: block block:1 not found in any window (status=404 code=not_found)` + "\n")},
			ErrNotFound, "session.focus_block: block block:1 not found in any window"},
		{"session not found", Output{ExitCode: 3, Stderr: []byte(`Error: stream response "control-1" failed: session session:1 not found (status=404 code=session_not_found)`)},
			ErrNotFound, "session.focus_block: session session:1 not found"},
		{"target not found", Output{ExitCode: 3, Stderr: []byte(`Error: session target "work" not found`)},
			ErrNotFound, `session.focus_block: session target "work" not found`},
		{"refused", Output{ExitCode: 3, Stderr: []byte(`Error: stream response "control-2" failed: Rex.app is not accepting actions. Turn on Remote Control in its Settings under Rex Server. (status=403 code=refused)`)},
			ErrRefused, "session.focus_block: Rex.app is not accepting actions. Turn on Remote Control in its Settings under Rex Server."},
		{"unavailable", Output{ExitCode: 4, Stderr: []byte("Error: connecting to Rex server http://127.0.0.1:1: dial tcp: connection refused")},
			ErrUnavailable, "session.focus_block: connecting to Rex server http://127.0.0.1:1: dial tcp: connection refused"},
		{"method not found is not ErrNotFound", Output{ExitCode: 3, Stderr: []byte(`Error: stream response "control-3" failed: netapi: no handler for method "x" (status=404 code=method_not_found)`)},
			nil, `session.focus_block: no handler for method "x"`},
		{"control characters are cleaned", Output{ExitCode: 1, Stderr: []byte("Error: bad \x1b[31mlabel\x1b[0m\x07 here\n")},
			nil, "session.focus_block: bad label here"},
		{"empty stderr", Output{ExitCode: 2}, nil, "session.focus_block: rex exited with status 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func([]string, []byte) Output { return tt.output })
			err := c.FocusBlock(context.Background(), "s", "b")
			var rexErr *Error
			if !errors.As(err, &rexErr) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.want)
			}
			for _, kind := range []error{ErrNotFound, ErrRefused, ErrUnavailable} {
				if kind != tt.want && errors.Is(err, kind) {
					t.Errorf("error also matches %v", kind)
				}
			}
			if err.Error() != tt.msg {
				t.Errorf("message = %q\nwant      %q", err.Error(), tt.msg)
			}
		})
	}
}

func TestRunnerFailureIsUnavailable(t *testing.T) {
	c, err := New(WithBinary("/nonexistent/rex"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FocusBlock(context.Background(), "s", "b"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestContextDeadlineIsReported(t *testing.T) {
	c, _ := newTestClient(t, nil, WithRunner(func(ctx context.Context, _ []string, _ []byte) (Output, error) {
		<-ctx.Done()
		return Output{ExitCode: -1}, ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := c.FocusBlock(ctx, "s", "b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestCLITimeoutFollowsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ms, err := strconv.Atoi(strings.TrimSuffix(cliTimeout(ctx), "ms"))
	if err != nil || ms < 1900 || ms >= 2000 {
		t.Errorf("cliTimeout = %dms (%v), want just under 2000ms", ms, err)
	}
	if got := cliTimeout(context.Background()); got != "5000ms" {
		t.Errorf("without deadline = %s", got)
	}
}

func TestKeymapToleratesErrorDiagnostics(t *testing.T) {
	c, _ := newTestClient(t, func([]string, []byte) Output {
		return Output{ExitCode: 1, Stdout: []byte(`{"bindings":[{"keys":["ctrl+p"],"action":"client.mode.enter","mode":""}],"modes":[{"name":"pane","exclusive":true}],"diagnostics":[{"severity":"error","message":"no such action"}],"actions":{}}`)}
	})
	km, err := c.Keymap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(km.Bindings) != 1 || km.Modes[0].Name != "pane" || km.Diagnostics[0].Severity != "error" {
		t.Fatalf("keymap = %+v", km)
	}
}

func TestCaptureTailDropsThePartialFirstLine(t *testing.T) {
	tests := []struct {
		name, reply, want string
	}{
		{"whole", `{"content":"a\nb\n"}`, "a\nb\n"},
		{"partial", `{"content":"\u001b[3tail of line\nnext\n","partial":true}`, "next\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newTestClient(t, stdout(tt.reply))
			got, err := c.Capture(context.Background(), "s", "b", CaptureOptions{Format: FormatVT, TailBytes: 1024})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if argv := rec.argv[0]; !containsRun(argv, []string{"do", "-", "--json"}) || !strings.Contains(string(rec.stdin[0]), "blob_read") {
				t.Errorf("tail should run the embedded script: %q", argv)
			}
			// block.method only reaches a session the script is attached to.
			if argv := rec.argv[0]; !containsRun(argv, []string{"-s", "s"}) {
				t.Errorf("tail must attach to the block's session: %q", argv)
			}
		})
	}
}

func TestCaptureTailReportsScriptErrors(t *testing.T) {
	c, _ := newTestClient(t, stdout(`{"error":"stream response \"control-2\" failed: block not found: b (status=404 code=not_found)"}`))
	_, err := c.Capture(context.Background(), "s", "b", CaptureOptions{TailBytes: 10})
	if !errors.Is(err, ErrNotFound) || err.Error() != "capture: block not found: b" {
		t.Fatalf("err = %v", err)
	}
}

func TestCaptureUsesTheCLI(t *testing.T) {
	c, rec := newTestClient(t, stdout("screen\n"))
	got, err := c.Capture(context.Background(), "s", "b", CaptureOptions{Trim: true})
	if err != nil || got != "screen\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !containsRun(rec.argv[0], []string{"capture", "-s", "s", "-b", "b", "--format", "text", "--trim"}) {
		t.Errorf("argv = %q", rec.argv[0])
	}
}

// The CLI silently switches to the local server when $REX_SERVER cannot be
// reached; naming it with -S makes that an ErrUnavailable instead.
func TestServerFromEnvironmentIsPassedExplicitly(t *testing.T) {
	t.Setenv("REX_SERVER", "http://127.0.0.1:47899")
	c, rec := newTestClient(t, stdout(`{}`))
	if err := c.Call(context.Background(), "session.list", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !containsRun(rec.argv[0], []string{"-S", "http://127.0.0.1:47899"}) || c.Server() != "http://127.0.0.1:47899" {
		t.Errorf("argv = %q, Server() = %q", rec.argv[0], c.Server())
	}

	// WithServer wins over the environment.
	c, rec = newTestClient(t, stdout(`{}`), WithServer("unix:///tmp/rex.sock"))
	_ = c.Call(context.Background(), "session.list", nil, nil)
	if !containsRun(rec.argv[0], []string{"-S", "unix:///tmp/rex.sock"}) || slices.Contains(rec.argv[0], "http://127.0.0.1:47899") {
		t.Errorf("argv = %q", rec.argv[0])
	}

	// WithEnv replaces the process environment, REX_SERVER included.
	c, rec = newTestClient(t, stdout(`{}`), WithEnv([]string{"REX_SERVER=http://a:1", "HOME=/x", "REX_SERVER=http://b:2"}))
	_ = c.Call(context.Background(), "session.list", nil, nil)
	if !containsRun(rec.argv[0], []string{"-S", "http://b:2"}) {
		t.Errorf("argv = %q", rec.argv[0])
	}
	c, rec = newTestClient(t, stdout(`{}`), WithEnv([]string{"HOME=/x"}))
	_ = c.Call(context.Background(), "session.list", nil, nil)
	if slices.Contains(rec.argv[0], "-S") {
		t.Errorf("no server configured, yet argv = %q", rec.argv[0])
	}
}

func TestUserMessage(t *testing.T) {
	limit := &Error{Op: "session.new_window", Message: "window limit reached"}
	gone := parseError("session.focus_block", 1, "Error: block not found: block:1 (status=404 code=not_found)")
	down := parseError("session.list", 4, "Error: connecting to Rex server http://x: connection refused")
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{limit, "window limit reached"},
		{fmt.Errorf("couldn't open: %w", limit), "couldn't open: window limit reached"},
		{gone, "that target is gone"},
		{down, "Rex is not reachable"},
		{fmt.Errorf("rex capture: %w", context.DeadlineExceeded), "Rex did not answer in time"},
		{errors.New("⌃E edits files · choose a file"), "⌃E edits files · choose a file"},
		{errors.New("stream response \"control-1\" failed: refused (status=403 code=refused)"), "refused"},
	}
	for _, tt := range tests {
		if got := UserMessage(tt.err); got != tt.want {
			t.Errorf("UserMessage(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

// Start leaves the action running in a session of its own: it returns
// before the CLI is done, and the CLI shares no process group with the
// caller (whose float may close, ending the processes in it). With a
// client the action runs as a key there would: its client actions go to it.
func TestStartLeavesTheActionRunning(t *testing.T) {
	for _, c := range []struct{ client, want string }{
		{"", `-S http://127.0.0.1:1 -s s do rexkit_search --json --args {"query":"panic"}`},
		{"client:app", `-S http://127.0.0.1:1 -C client:app -s s do rexkit_search --json --args {"query":"panic"} --origin key`},
	} {
		dir := t.TempDir()
		out := filepath.Join(dir, "out")
		bin := filepath.Join(dir, "rex")
		script := "#!/bin/sh\n/bin/sleep 0.3\n{ echo \"$*\"; /bin/ps -o pgid= -p $$; } > " + out + ".tmp\n/bin/mv " + out + ".tmp " + out + "\n"
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cl, err := New(WithBinary(bin), WithServer("http://127.0.0.1:1"))
		if err != nil {
			t.Fatal(err)
		}
		if err := cl.Start("s", c.client, "rexkit_search", map[string]string{"query": "panic"}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(out); err == nil {
			t.Fatal("Start waited for the CLI")
		}
		var data []byte
		deadline := time.Now().Add(5 * time.Second)
		for {
			if data, err = os.ReadFile(out); err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("the CLI never ran: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "--autostart=false --color=never --timeout ") || !strings.HasSuffix(lines[0], c.want) {
			t.Fatalf("CLI ran with %q, want the global flags then %q", data, c.want)
		}
		if pgid, _ := strconv.Atoi(strings.TrimSpace(lines[1])); pgid == syscall.Getpgrp() {
			t.Errorf("the CLI is in the caller's process group %d", pgid)
		}
	}
}

func TestStartReportsAMissingBinary(t *testing.T) {
	c, err := New(WithBinary(filepath.Join(t.TempDir(), "rex")))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start("s", "", "rexkit_search", nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
