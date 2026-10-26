package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	// Never reach a real server: tools must see that they run outside Rex.
	for _, name := range []string{"REX_BLOCK", "REX_SESSION", "REX_SERVER"} {
		t.Setenv(name, "")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"no command prints usage", nil, 2, "Usage: rexkit"},
		{"help", []string{"help"}, 0, "Internal (used by the Lua layer)"},
		{"unknown", []string{"nope"}, 2, `unknown command "nope"`},
		{"tool outside Rex", []string{"goto"}, 1, "rexkit: goto: run it inside Rex"},
		{"demo needs a tool", []string{"demo"}, 1, "demo: name a tool"},
		{"demo rejects non-tools", []string{"demo", "install"}, 1, `"install" is not a tool`},
		{"await timeout exit code", []string{"await", "--state", t.TempDir(), "--timeout", "10ms"}, 3, "no result within 10ms"},
		{"await usage exit code", []string{"await"}, 2, "--state is required"},
	}
	bad := []struct {
		name string
		args []string
	}{
		{"await bad flag", []string{"await", "-badflag"}},
		{"collect bad flag", []string{"collect", "-badflag"}},
	}
	for _, name := range []string{"goto", "files", "keys", "rename", "scrollback", "hold", "toast", "snap",
		"install", "uninstall", "doctor"} {
		bad = append(bad, struct {
			name string
			args []string
		}{name + " bad flag", []string{name, "-badflag"}})
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(tt.args, &stderr); code != 2 {
				t.Errorf("exit = %d, want the usage status 2", code)
			}
			// The flag package already printed the error and the usage.
			if strings.Contains(stderr.String(), "not defined") {
				t.Errorf("the flag error was printed again: %q", stderr.String())
			}
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(tt.args, &stderr); code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	for _, name := range []string{"goto", "files", "keys", "rename", "scrollback", "hold", "toast", "snap",
		"install", "uninstall", "doctor", "demo", "version", "await", "collect"} {
		if !strings.Contains(out.String(), "  "+name+" ") {
			t.Errorf("usage lacks %s", name)
		}
	}
}
