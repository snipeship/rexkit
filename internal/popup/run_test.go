package popup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// flakyView fails the first n View calls.
type flakyView struct {
	rex.API
	n atomic.Int32
}

func (f *flakyView) View(ctx context.Context, session string) (rex.View, error) {
	if f.n.Add(-1) >= 0 {
		return rex.View{}, errors.New("transient")
	}
	return f.API.View(ctx, session)
}

func TestRunClosesAndAnswersOnEveryPath(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		viewFails int32
		body      func(*Env) error
		wantErr   string
		wantBody  bool
		wantClaim bool
	}{
		{name: "success", body: func(*Env) error { return nil }, wantBody: true, wantClaim: true},
		{name: "body fails", body: func(*Env) error { return boom }, wantErr: "tool: boom", wantBody: true, wantClaim: true},
		{
			name: "claim fails",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "owner.json"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			body:     func(*Env) error { return nil },
			wantBody: true, // a claim only helps GC
		},
		{name: "self fails", viewFails: 1, body: func(*Env) error { return nil }, wantErr: "tool: transient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noSighup(t)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			w := newWorld(t, false)
			t.Setenv("REX_BLOCK", w.tool)
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			api := &flakyView{API: w.f}
			api.n.Store(tt.viewFails)
			ran := false
			err := Run("tool", Options{Session: w.s, OriginBlock: w.a, State: dir}, RunOptions{API: api}, func(env *Env) error {
				ran = true
				if env.Self.BlockID != w.tool || env.Popup == nil || env.Dirs.State == "" {
					t.Errorf("env = %+v", env)
				}
				return tt.body(env)
			})
			if got := errString(err); got != tt.wantErr {
				t.Errorf("err = %q, want %q", got, tt.wantErr)
			}
			if ran != tt.wantBody {
				t.Errorf("body ran = %v", ran)
			}
			var r Result
			if err := state.ReadJSON(filepath.Join(dir, ResultFile), &r); err != nil || r.Action != ActionClose {
				t.Errorf("answer = %+v, %v; want close", r, err)
			}
			var o state.Owner
			if claimed := state.ReadJSON(filepath.Join(dir, "owner.json"), &o) == nil; claimed != tt.wantClaim {
				t.Errorf("claimed = %v", claimed)
			}
			focused, _, layers := w.focus(t)
			if layers != 1 || focused != w.a {
				t.Errorf("float not closed: layers %d focus %s", layers, focused)
			}
			if n := countOf(w.f.Methods(), "session.close_layer"); n != 1 {
				t.Errorf("close_layer called %d times", n)
			}
		})
	}
}

func TestRunKeepsTheToolsOwnAnswer(t *testing.T) {
	noSighup(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.tool)
	dir := t.TempDir()
	err := Run("tool", Options{Session: w.s, State: dir}, RunOptions{API: w.f}, func(env *Env) error {
		_, err := WriteResult(dir, Result{Action: ActionGoto, SessionID: w.s})
		return err
	})
	var r Result
	if err != nil || state.ReadJSON(filepath.Join(dir, ResultFile), &r) != nil || r.Action != ActionGoto {
		t.Errorf("err %v, answer %+v; the tool's own answer must stay", err, r)
	}
}

func TestRunOutsideRex(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("REX_BLOCK", "")
	f := newWorld(t, false).f
	err := Run("goto", Options{}, RunOptions{API: f}, func(*Env) error {
		t.Error("body must not run outside Rex")
		return nil
	})
	if !errors.Is(err, ErrOutsideRex) || err.Error() != "goto: run it inside Rex, or try `rexkit demo goto`" {
		t.Errorf("err = %v", err)
	}
	ran := false
	err = Run("keys", Options{}, RunOptions{API: f, LazySelf: true}, func(env *Env) error {
		ran = true
		_, err := env.ResolveSelf(env.Ctx)
		if !errors.Is(err, ErrOutsideRex) {
			t.Errorf("ResolveSelf = %v", err)
		}
		return nil
	})
	if err != nil || !ran {
		t.Errorf("a lazy tool runs outside Rex: ran %v, err %v", ran, err)
	}
	if m := f.Methods(); len(m) != 0 {
		t.Errorf("calls outside Rex: %v", m)
	}
}

// An error after the tool closed its own float is noise (the terminal hung up).
func TestRunDropsErrorsAfterTheFloatClosed(t *testing.T) {
	noSighup(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	w := newWorld(t, false)
	t.Setenv("REX_BLOCK", w.tool)
	err := Run("tool", Options{Session: w.s}, RunOptions{API: w.f}, func(env *Env) error {
		if err := env.Popup.Close(env.Ctx); err != nil {
			t.Fatal(err)
		}
		return errors.New("read /dev/tty: input/output error")
	})
	if err != nil {
		t.Errorf("err = %v", err)
	}
	if n := countOf(w.f.Methods(), "session.close_layer"); n != 1 {
		t.Errorf("close_layer called %d times", n)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func countOf(s []string, v string) int {
	n := 0
	for i := slices.Index(s, v); i >= 0; i = slices.Index(s, v) {
		n++
		s = s[i+1:]
	}
	return n
}

// Run sets the float's title before the body draws anything, and only in
// Rex (a demo in another terminal keeps that terminal's title).
func TestRunSetsTitle(t *testing.T) {
	for _, inRex := range []bool{true, false} {
		noSighup(t)
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		w := newWorld(t, false)
		t.Setenv("REX_BLOCK", "")
		if inRex {
			t.Setenv("REX_BLOCK", w.tool)
		}
		var out strings.Builder
		titleOut = &out
		t.Cleanup(func() { titleOut = os.Stdout })
		before := ""
		_ = Run("tool", Options{Session: w.s, State: t.TempDir()}, RunOptions{API: w.f, Title: "Go to", LazySelf: true, NoKeys: true}, func(*Env) error {
			before = out.String()
			return nil
		})
		want := ""
		if inRex {
			want = "\x1b]2;Go to\x07"
		}
		if before != want {
			t.Errorf("in Rex %v: wrote %q before the body, want %q", inRex, before, want)
		}
	}
}

// A tool Lua opened finds the tools dir where Lua made its state dir, even
// when its own XDG_STATE_HOME points elsewhere (the lab moves Lua's only).
func TestEnvToolsDirFollowsLua(t *testing.T) {
	lua := filepath.Join(t.TempDir(), "tools")
	own := state.Dirs{State: t.TempDir()}
	env := &Env{Popup: New(nil, Options{State: filepath.Join(lua, "abc123")}), Dirs: own}
	if got := env.ToolsDir(); got != lua {
		t.Fatalf("ToolsDir = %q, want Lua's %q", got, lua)
	}
	dir, err := env.NewToolDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != lua {
		t.Fatalf("NewToolDir = %q, want it under %q", dir, lua)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("NewToolDir = %v, %v; want a 0700 dir", info, err)
	}

	byHand := &Env{Popup: New(nil, Options{}), Dirs: own}
	if got := byHand.ToolsDir(); got != own.ToolsDir() {
		t.Fatalf("ToolsDir by hand = %q, want %q", got, own.ToolsDir())
	}
	lost := &Env{Popup: New(nil, Options{}), DirsErr: errors.New("no home")}
	if got := lost.ToolsDir(); got != "" {
		t.Fatalf("ToolsDir without dirs = %q, want \"\"", got)
	}
	if _, err := lost.NewToolDir(); err == nil {
		t.Fatal("NewToolDir without dirs succeeded")
	}
}
