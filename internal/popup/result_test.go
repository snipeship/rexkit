package popup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"rexkit/internal/state"
)

func TestResultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteResult(dir, Result{Action: "goto", SessionID: "s", WindowID: "w", BlockID: "b"})
	if err != nil || written.Seq == 0 {
		t.Fatalf("WriteResult = %+v, %v", written, err)
	}
	got, err := AwaitResult(ctx, dir, 0)
	if err != nil || !reflect.DeepEqual(got, written) {
		t.Fatalf("AwaitResult = %+v, %v; want %+v", got, err, written)
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := AwaitResult(short, dir, written.Seq); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a result that is not newer must not count: %v", err)
	}
	if _, err := WriteResult("", Result{}); err == nil {
		t.Error("WriteResult without a dir should fail")
	}
}

func TestAwaitResultWaitsForTheTool(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = WriteResult(dir, Result{Seq: 7, Action: "goto", SessionID: "s"})
	}()
	got, err := AwaitResult(ctx, dir, 0)
	if err != nil || got.Seq != 7 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAwaitCommand(t *testing.T) {
	ready := t.TempDir()
	if _, err := WriteResult(ready, Result{Seq: 5, Action: "invoke", SessionID: "s", Invoke: "rexkit_files", Args: json.RawMessage(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}
	unreadable := t.TempDir()
	if err := state.WriteFile(filepath.Join(unreadable, ResultFile), []byte("{oops")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		args     []string
		wantCode int // 0: success
		wantOut  string
	}{
		{"result", []string{"--state", ready, "--timeout", "1s"}, 0,
			`{"seq":5,"action":"invoke","session_id":"s","invoke":"rexkit_files","args":{"x":1}}` + "\n"},
		{"timeout", []string{"--state", t.TempDir(), "--timeout", "20ms"}, ExitTimeout, ""},
		{"stale result", []string{"--state", ready, "--timeout", "20ms", "--after", "5"}, ExitTimeout, ""},
		{"missing state", []string{"--timeout", "20ms"}, 2, ""},
		{"bad flag", []string{"--nope"}, ExitUsage, ""},
		{"corrupt result", []string{"--state", unreadable, "--timeout", "1s"}, -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := Await(ctx, tt.args, &out)
			var exit interface{ ExitCode() int }
			switch {
			case tt.wantCode == 0 && err != nil:
				t.Fatalf("err = %v", err)
			case tt.wantCode > 0 && (!errors.As(err, &exit) || exit.ExitCode() != tt.wantCode):
				t.Fatalf("err = %v, want exit %d", err, tt.wantCode)
			case tt.wantCode < 0 && (err == nil || errors.As(err, &exit)):
				t.Fatalf("err = %v, want a plain error", err)
			}
			if out.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestAwaitResultReportsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ResultFile)
	if err := os.Mkdir(path, 0o700); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	if _, err := AwaitResult(ctx, dir, 0); err == nil {
		t.Error("want an error")
	}
}

func TestAnswer(t *testing.T) {
	dir := t.TempDir()
	if err := Answer(dir, ActionStay); err != nil {
		t.Fatal(err)
	}
	got, err := AwaitResult(ctx, dir, 0)
	if err != nil || got.Action != ActionStay || got.Seq == 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := Answer("", ActionClose); err != nil {
		t.Errorf("no dir means no one waits: %v", err)
	}
	for key, want := range map[string]bool{"enter": true, "esc": true, "ctrl+c": true, "q": false, "alt+enter": false} {
		if IsHandshakeKey(key) != want {
			t.Errorf("IsHandshakeKey(%q) = %v", key, !want)
		}
	}
}

func TestParseFlags(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("x", false, "")
	if err := ParseFlags(fs, []string{"-x"}); err != nil {
		t.Fatal(err)
	}
	if err := ParseFlags(fs, []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h = %v", err)
	}
	err := ParseFlags(fs, []string{"-nope"})
	var usage *UsageError
	if !errors.As(err, &usage) || usage.ExitCode() != ExitUsage || !usage.Reported() || !strings.Contains(err.Error(), "-nope") {
		t.Errorf("bad flag = %#v", err)
	}
}

func TestTakeCommit(t *testing.T) {
	dir := t.TempDir()
	if TakeCommit(dir) {
		t.Fatal("no marker, yet a commit")
	}
	if TakeCommit("") {
		t.Fatal("no state dir, yet a commit")
	}
	if err := os.WriteFile(filepath.Join(dir, CommitFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !TakeCommit(dir) {
		t.Fatal("the marker was not taken")
	}
	if TakeCommit(dir) {
		t.Error("a marker counts once")
	}
	if _, err := os.Stat(filepath.Join(dir, CommitFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the marker is still there: %v", err)
	}
}

func TestTakeCommitAtAndGraceLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CommitFile)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	written := time.Now().Add(-400 * time.Millisecond)
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	at, ok := TakeCommitAt(dir)
	if !ok || !at.Equal(written) {
		t.Fatalf("TakeCommitAt = %v, %v; want %v", at, ok, written)
	}
	if left := GraceLeft(at); left > HandshakeGrace-400*time.Millisecond || left < HandshakeGrace/4 {
		t.Errorf("GraceLeft = %s, want about %s", left, HandshakeGrace-400*time.Millisecond)
	}
	if left := GraceLeft(time.Now().Add(-time.Hour)); left != 0 {
		t.Errorf("GraceLeft long after = %s, want 0", left)
	}
	if left := GraceLeft(time.Now().Add(time.Hour)); left != HandshakeGrace {
		t.Errorf("GraceLeft of a future marker = %s, want at most HandshakeGrace", left)
	}
}

// TestHandshakeTiming: a held key must be answered before Lua gives up.
func TestHandshakeTiming(t *testing.T) {
	if HandshakeGrace+250*time.Millisecond > AwaitTimeout {
		t.Errorf("HandshakeGrace %s leaves too little of Lua's %s await", HandshakeGrace, AwaitTimeout)
	}
}
