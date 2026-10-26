package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// doctor installs rexkit into a fresh world, applies a change, and returns
// the doctor's checks by name.
func doctor(t *testing.T, change func(w *world)) (map[string]Line, *world, error) {
	t.Helper()
	w := newWorld(t)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	// A binary newer than every source, as after a real build.
	bin := filepath.Join(w.root, "bin", "rexkit")
	if err := os.Chtimes(bin, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(w)
	}
	w.out.Reset()
	err := w.env.Doctor(context.Background(), "")
	lines := map[string]Line{}
	for _, name := range []string{"binary", "config", "settings", "config check", "loaded", "keymap", "conflicts",
		"remote control", "floating layers", "state", "old state", "nvim", "git", "zoxide", "lua layer", "setting"} {
		for _, row := range strings.Split(w.out.String(), "\n") {
			if l, ok := parseLine(row, name); ok {
				lines[name] = l
			}
		}
	}
	return lines, w, err
}

// parseLine reads one printed checklist line back.
func parseLine(row, name string) (Line, bool) {
	row = strings.TrimSpace(row)
	for m := MarkOK; m <= MarkPlan; m++ {
		rest, ok := strings.CutPrefix(row, m.glyph()+" "+name+" ")
		if ok && (len(rest) == 0 || rest[0] == ' ' || len(name) >= nameWidth-1) {
			return Line{Mark: m, Name: name, Detail: strings.TrimSpace(rest)}, true
		}
	}
	return Line{}, false
}

func TestDoctorHealthy(t *testing.T) {
	lines, w, err := doctor(t, nil)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, w.out)
	}
	for name, l := range lines {
		if l.Mark == MarkFail || l.Mark == MarkWarn {
			t.Errorf("%s: %v %s", name, l.Mark, l.Detail)
		}
	}
	if _, ok := lines["old state"]; ok {
		t.Error("reported legacy state that does not exist")
	}
	if strings.Contains(w.out.String(), "\x1b[") {
		t.Error("plain output contains escape sequences")
	}
}

func TestDoctorFindsProblems(t *testing.T) {
	tests := []struct {
		name   string
		change func(w *world)
		check  string
		want   Mark
		detail string
	}{
		{"binary missing", func(w *world) { os.Remove(filepath.Join(w.root, "bin", "rexkit")) }, "binary", MarkFail, "missing"},
		{"binary stale", func(w *world) {
			future := time.Now().Add(2 * time.Hour)
			os.Chtimes(filepath.Join(w.root, "cmd", "rexkit", "main.go"), future, future)
		}, "binary", MarkWarn, "cmd/rexkit/main.go changed"},
		{"not installed", func(w *world) { w.write(w.env.Paths.RexConfig, userConfig) }, "config", MarkFail, "your own config"},
		{"legacy installer", func(w *world) { w.write(w.env.Paths.RexConfig, w.rexZellij()) }, "config", MarkFail, "rex-goto/rex-zellij"},
		{"edited loader", func(w *world) {
			w.write(w.env.Paths.RexConfig, w.read(w.env.Paths.RexConfig)+"rex.bind(\"ctrl+y\", \"pane.zoom\")\n")
		}, "config", MarkFail, "edited"},
		{"a comment added to the loader", func(w *world) {
			w.write(w.env.Paths.RexConfig, w.read(w.env.Paths.RexConfig)+"-- mine\n")
		}, "config", MarkOK, "code/rexkit/lua/init.lua"},
		{"loader written for other settings dirs", func(w *world) {
			w.write(w.env.Paths.RexConfig, string(Loader(w.root, "/elsewhere/rexkit/settings.lua")))
		}, "config", MarkOK, "code/rexkit/lua/init.lua"},
		{"module fails in a strict check", func(w *world) { w.strictFails = true }, "config check", MarkFail, "boom"},
		{"module did not load", func(w *world) {
			w.luaDiag = `{"modules":["core","layout","workspace","tools","modes"],"warnings":["bindings.lua failed to load: bindings.lua:1: boom"]}`
		}, "lua layer", MarkFail, "bindings.lua failed to load: bindings.lua:1: boom"},
		{"module missing without a reason", func(w *world) {
			w.luaDiag = `{"modules":["core","layout","workspace","tools","bindings"],"warnings":{}}`
		}, "lua layer", MarkFail, "modes.lua did not load"},
		{"server lacks the lua layer", func(w *world) { w.diagMissing = true }, "lua layer", MarkFail, "not running rexkit's Lua layer"},
		{"settings warning", func(w *world) {
			w.luaDiag = `{"modules":["core","layout","workspace","tools","bindings","modes"],"warnings":["unknown setting \"colour\" is ignored"]}`
		}, "setting", MarkWarn, `unknown setting "colour"`},
		{"lua layer gone", func(w *world) { os.RemoveAll(filepath.Join(w.root, "lua")) }, "config", MarkFail, "missing"},
		{"config error", func(w *world) { w.checkFails = true }, "config check", MarkFail, "boom"},
		{"unknown setting", func(w *world) {
			w.write(w.env.Paths.Dirs.Settings(), `return { shell = "/bin/zsh", colour = "red" }`)
		}, "settings", MarkWarn, "unknown: colour"},
		{"server has an old config", func(w *world) {
			w.keymap = keymapJSON(w.env.Paths.RexConfig, "-- old\n")
		}, "loaded", MarkWarn, "older version"},
		{"server reads another file", func(w *world) {
			w.keymap = keymapJSON("/elsewhere/init.lua", "")
		}, "loaded", MarkWarn, "/elsewhere/init.lua"},
		{"rex missing", func(w *world) { w.env.Rex = "" }, "config check", MarkFail, "rex command was not found"},
		{"rex not running", func(w *world) { w.down = true }, "loaded", MarkWarn, "Rex is not running"},
		{"layers unknown while rex is down", func(w *world) { w.down = true }, "floating layers", MarkWarn, "not running"},
		{"keymap warning", func(w *world) {
			w.keymap = `{"bindings":[{"keys":["ctrl+x"],"action":"gone","source":"bindings.lua:4"}],"diagnostics":[]}`
		}, "keymap", MarkWarn, "missing action gone"},
		{"keymap error", func(w *world) {
			w.keymap = `{"diagnostics":[{"severity":"error","source":"modes.lua:2","message":"bad mode"}]}`
		}, "keymap", MarkFail, "modes.lua:2: bad mode"},
		{"conflict", func(w *world) {
			w.keymap = `{"bindings":[
				{"keys":["o"],"mode":"pane","action":"pane.zoom","source":"b.lua:9"},
				{"keys":["o"],"mode":"pane","action":"pane.close","source":"a.lua:1"}]}`
		}, "conflicts", MarkWarn, "pane/o bound 2× (b.lua:9 wins)"},
		{"remote control on", func(w *world) { w.remote = "1" }, "remote control", MarkOK, "on"},
		{"no floating layers", func(w *world) { w.methods = []string{"session.view"} }, "floating layers", MarkFail, "no session.new_layer"},
		{"stale tool dirs", func(w *world) {
			dir := filepath.Join(w.env.Paths.Dirs.ToolsDir(), "abc")
			os.MkdirAll(dir, 0o700)
			old := testNow.Add(-48 * time.Hour)
			os.Chtimes(dir, old, old)
		}, "state", MarkWarn, "1 tool dir older than a day"},
		{"legacy state", func(w *world) { os.MkdirAll(filepath.Join(w.env.Paths.StateHome, "rex-goto"), 0o700) }, "old state", MarkWarn, "rex-goto"},
		{"no nvim", func(w *world) { delete(w.tools, "nvim") }, "nvim", MarkWarn, "less"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, w, err := doctor(t, tt.change)
			l, ok := lines[tt.check]
			if !ok {
				t.Fatalf("no %q line:\n%s", tt.check, w.out)
			}
			if l.Mark != tt.want || !strings.Contains(l.Detail, tt.detail) {
				t.Errorf("%s = %s %q, want %s containing %q", tt.check, l.Mark.glyph(), l.Detail, tt.want.glyph(), tt.detail)
			}
			var problems *ProblemsError
			if failed := errors.As(err, &problems); failed != (tt.want == MarkFail) {
				t.Errorf("doctor err = %v, want failure %v", err, tt.want == MarkFail)
			} else if failed && problems.ExitCode() != 1 {
				t.Errorf("exit code %d", problems.ExitCode())
			}
		})
	}
}

func TestDoctorColoursOnlyTerminals(t *testing.T) {
	w := newWorld(t)
	w.env.Color = true
	w.env.Doctor(context.Background(), w.root)
	if !strings.Contains(w.out.String(), "\x1b[") {
		t.Error("terminal output has no colour")
	}
}

func TestAgo(t *testing.T) {
	tests := map[time.Duration]string{
		time.Second:         "just now",
		3 * time.Minute:     "3m ago",
		5 * time.Hour:       "5h ago",
		30 * 24 * time.Hour: "on Sep 26",
	}
	for d, want := range tests {
		if got := ago(testNow.Add(-d), testNow); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDoctorKeepsLegacyStateWhileLegacyToolsRun(t *testing.T) {
	lines, _, _ := doctor(t, func(w *world) {
		w.write(w.env.Paths.RexConfig, w.rexZellij())
		os.MkdirAll(filepath.Join(w.env.Paths.StateHome, "rex-zellij"), 0o700)
	})
	if l, ok := lines["old state"]; ok {
		t.Errorf("suggested deleting state the old tools still use: %+v", l)
	}
}
