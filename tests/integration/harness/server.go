//go:build integration

// Package harness runs integration tests against an isolated Rex server: a
// private `rex server run` on a free localhost port with its own config,
// state and XDG directories. It never talks to the user's server: every CLI
// call names the isolated server with -S and runs without the caller's REX_*
// variables. Blocks started by the server inherit REX_SERVER and the XDG
// directories, so tools running inside them talk to the isolated server too.
package harness

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"rexkit/internal/rex"
)

// Options configure an isolated server.
type Options struct {
	Config string   // init.lua source; empty for no bindings
	Env    []string // extra KEY=VALUE for the server and every block it starts
	// Files are written under the server's private dir before it starts,
	// keyed by relative path, e.g. "xdg/config_home/rexkit/settings.lua" (see XDG).
	Files map[string]string
	// Unix listens on a Unix socket instead of TCP: Rex takes some methods
	// (server.config_reload) only from its local socket.
	Unix bool
}

// Server is a running isolated Rex server.
type Server struct {
	Addr string      // http://127.0.0.1:<port>, or unix://<path> (Options.Unix)
	Dir  string      // private root: init.lua, state/, xdg/, server.log
	Env  []string    // environment of the server and the CLI calls
	Rex  *rex.Client // client bound to this server
	Bin  string      // the rex CLI

	cmd  *exec.Cmd
	done chan struct{}
}

// Start launches an isolated server and stops it when the test ends. It
// skips the test when Rex is not installed.
func Start(t testing.TB, opts Options) *Server {
	t.Helper()
	bin, err := rex.Locate()
	if err != nil {
		t.Skip("rex is not installed:", err)
	}
	dir := t.TempDir()
	s := &Server{Dir: dir, Bin: bin, done: make(chan struct{})}
	if opts.Unix {
		// a short path: macOS caps socket paths at 104 bytes, and t.TempDir's are long
		sock, err := os.MkdirTemp("/tmp", "rexkit-it-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(sock) })
		s.Addr = "unix://" + filepath.Join(sock, "s.sock")
	} else {
		s.Addr = "http://" + freeAddr(t)
	}
	s.writeConfig(t, opts.Config)
	for name, content := range opts.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.Env = append(isolatedEnv(os.Environ(), dir, s.Addr), opts.Env...)

	log, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	s.cmd = exec.Command(bin, "server", "run", "--listen", s.Addr, "--allow-anonymous",
		"--config", s.ConfigPath(), "--state-dir", filepath.Join(dir, "state"))
	s.cmd.Env = s.Env
	s.cmd.Stdout, s.cmd.Stderr = log, log
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.cmd.Wait(); log.Close(); close(s.done) }()
	t.Cleanup(func() { s.stop(t) })

	s.Rex, err = rex.New(rex.WithBinary(bin), rex.WithServer(s.Addr), rex.WithEnv(s.Env))
	if err != nil {
		t.Fatal(err)
	}
	s.waitReady(t)
	return s
}

// ConfigPath is the server's init.lua.
func (s *Server) ConfigPath() string { return filepath.Join(s.Dir, "init.lua") }

// XDG returns the XDG base directory the server and its blocks use, such as
// XDG("XDG_STATE_HOME").
func (s *Server) XDG(name string) string {
	return filepath.Join(s.Dir, "xdg", strings.ToLower(strings.TrimPrefix(name, "XDG_")))
}

// Log returns the server log so far.
func (s *Server) Log() string {
	data, _ := os.ReadFile(filepath.Join(s.Dir, "server.log"))
	return string(data)
}

// CLI runs the rex CLI against the server and returns its stdout.
func (s *Server) CLI(t testing.TB, args ...string) string {
	t.Helper()
	argv := append([]string{"--autostart=false", "-S", s.Addr}, args...)
	cmd := exec.Command(s.Bin, argv...)
	cmd.Env = s.Env
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("rex %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return string(out)
}

func (s *Server) writeConfig(t testing.TB, config string) {
	t.Helper()
	if err := os.WriteFile(s.ConfigPath(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *Server) waitReady(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := s.Rex.Call(ctx, "server.status", nil, nil)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-s.done:
			t.Fatalf("rex server exited:\n%s", s.Log())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("rex server not ready: %v\n%s", err, s.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *Server) stop(t testing.TB) {
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
	}
	if t.Failed() {
		t.Logf("rex server log:\n%s", s.Log())
	}
}

// isolatedEnv drops the caller's REX_* variables (they name the user's
// server, session and block) and points Rex and rexkit at private dirs.
func isolatedEnv(environ []string, dir, addr string) []string {
	var env []string
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "REX_") && !strings.HasPrefix(kv, "XDG_") {
			env = append(env, kv)
		}
	}
	env = append(env, "REX_SERVER="+addr)
	for _, name := range []string{"XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"} {
		env = append(env, name+"="+filepath.Join(dir, "xdg", strings.ToLower(strings.TrimPrefix(name, "XDG_"))))
	}
	return env
}

func freeAddr(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr())
}
