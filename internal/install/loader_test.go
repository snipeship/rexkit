package install

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestManagedRoot(t *testing.T) {
	tests := []struct {
		name, src string
		want      string // "" = not managed
	}{
		{"current loader", string(Loader("/code/rexkit", "~/.config/rexkit/settings.lua")), "/code/rexkit"},
		{"older wording", Marker + ". Do not edit.\nreturn dofile([[/code/rexkit/lua/init.lua]])\n", "/code/rexkit"},
		{"comments anywhere", Marker + "\n--[[ block ]]\nreturn dofile(\"/code/rexkit/lua/init.lua\"); -- end\n", "/code/rexkit"},
		{"no marker", "return dofile([[/code/rexkit/lua/init.lua]])\n", ""},
		{"extra code", Marker + "\nrex.bind(\"x\", \"y\")\nreturn dofile([[/code/rexkit/lua/init.lua]])\n", ""},
		{"code after", Marker + "\nreturn dofile([[/code/rexkit/lua/init.lua]])\nprint(1)\n", ""},
		{"another file", Marker + "\nreturn dofile([[/code/rexkit/other.lua]])\n", ""},
		{"relative path", Marker + "\nreturn dofile([[rexkit/lua/init.lua]])\n", ""},
		{"not a call", Marker + "\nreturn [[/code/rexkit/lua/init.lua]]\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := managedRoot(tt.src)
			if ok != (tt.want != "") || got != tt.want {
				t.Errorf("managedRoot = %q, %v; want %q", got, ok, tt.want)
			}
		})
	}
}

// A loader from an earlier rexkit, or written under other XDG dirs, is
// still rexkit's: install rewrites it and uninstall restores the backup.
func TestOlderLoaderIsStillManaged(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	older := Marker + ". An earlier note.\n-- Settings: /elsewhere/settings.lua\nreturn dofile([[" + w.root + "/lua/init.lua]])\n"
	w.write(w.env.Paths.RexConfig, older)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if got := w.read(w.env.Paths.RexConfig); got != string(w.env.loader(w.root)) {
		t.Errorf("install did not rewrite the older loader:\n%s", got)
	}
	if len(w.backups()) != 1 {
		t.Errorf("rexkit's own loader was backed up: %v", w.backups())
	}
	w.write(w.env.Paths.RexConfig, older)
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig {
		t.Error("uninstall did not restore the backup")
	}
}

func TestGeneratedCommentsStayComments(t *testing.T) {
	loader := string(Loader("/code/rexkit", "/x\nos.execute('boom')"))
	if root, ok := managedRoot(loader); !ok || root != "/code/rexkit" || strings.Contains(loader, "\nos.execute") {
		t.Errorf("a newline in the settings path escaped the comment:\n%s", loader)
	}

	dir := t.TempDir()
	defaults, legacy := dir+"/defaults.lua", dir+"/settings.lua"
	writeFile(t, defaults, testDefaults)
	writeFile(t, legacy, `return { ["x\nos.execute('boom')"] = 1, shell = "/bin/bash" }`)
	plan, err := planSettings([]string{legacy}, defaults, testNow)
	if err != nil {
		t.Fatal(err)
	}
	src := string(plan.data)
	head, _, _ := strings.Cut(src, "return ")
	for _, line := range strings.Split(strings.TrimSpace(head), "\n") {
		if !strings.HasPrefix(line, "--") {
			t.Errorf("line %q before return is not a comment:\n%s", line, src)
		}
	}
	got, err := parseLuaData(src)
	if err != nil || !slices.Equal(got.Keys, []string{"shell"}) {
		t.Errorf("parsed %v, %v; want only shell", got, err)
	}
}

// rex-zellij ran its settings with dofile, so they may be code; install
// carries on without them.
func TestLegacySettingsThatAreCode(t *testing.T) {
	for _, dry := range []bool{true, false} {
		w := newWorld(t)
		w.write(w.env.Paths.RexConfig, w.rexZellij())
		local := w.home + "/code/rex-zellij/settings.local.lua"
		w.write(local, `return { shell = os.getenv("SHELL") }`)
		if err := w.install(Options{DryRun: dry}); err != nil {
			t.Fatalf("dry run %v: %v", dry, err)
		}
		if !strings.Contains(w.out.String(), "Lua code, not plain values") || !strings.Contains(w.out.String(), "settings.local.lua") {
			t.Errorf("no warning about the skipped settings:\n%s", w.out)
		}
		if w.exists(w.env.Paths.Dirs.Settings()) {
			t.Error("settings.lua was written")
		}
		if dry {
			continue
		}
		var skipped string
		for _, m := range w.record().Migrated {
			if m.From == "settings" {
				skipped = m.Skipped
			}
		}
		if !strings.Contains(skipped, "os is not a plain value") {
			t.Errorf("record skipped = %q", skipped)
		}
	}
}

func TestBrokenDefaultsStillFailInstall(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	w.write(w.root+"/lua/defaults.lua", "return { broken(")
	if err := w.install(Options{}); err == nil || !strings.Contains(err.Error(), "defaults.lua") {
		t.Fatalf("err = %v, want the broken defaults reported", err)
	}
	if _, err := os.Stat(w.env.Paths.Record()); err == nil {
		t.Error("install went ahead")
	}
}
