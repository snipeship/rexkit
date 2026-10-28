package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const userConfig = "rex.bind(\"ctrl+x\", \"pane.zoom\")\n"

func TestFreshInstall(t *testing.T) {
	w := newWorld(t)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if got, want := w.read(w.env.Paths.RexConfig), string(w.env.loader(w.root)); got != want {
		t.Errorf("init.lua =\n%s\nwant\n%s", got, want)
	}
	rec := w.record()
	if !rec.NoConfig || rec.Backup != "" || rec.Root != w.root {
		t.Errorf("record = %+v, want NoConfig, no backup, root %s", rec, w.root)
	}
	if b := w.backups(); len(b) != 0 {
		t.Errorf("backups = %v, want none (there was nothing to back up)", b)
	}
	if !w.exists(filepath.Join(w.root, "bin", "rexkit")) {
		t.Error("bin/rexkit was not built")
	}
	if w.reloads != 1 {
		t.Errorf("reloads = %d, want 1", w.reloads)
	}
	for _, want := range []string{"built", "installed", "reloaded", "rexkit doctor"} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, w.out)
		}
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	rec := w.record()
	if rec.Backup == "" || w.read(rec.Backup) != userConfig {
		t.Fatalf("backup %q does not hold the user's config", rec.Backup)
	}
	config, record := w.read(w.env.Paths.RexConfig), w.read(w.env.Paths.Record())
	for range 2 {
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if w.read(w.env.Paths.RexConfig) != config || w.read(w.env.Paths.Record()) != record {
		t.Error("a repeated install changed init.lua or the record")
	}
	if b := w.backups(); len(b) != 1 {
		t.Errorf("backups = %v, want exactly one", b)
	}
	if !strings.Contains(w.out.String(), "up to date") {
		t.Errorf("a repeated install should say the config is up to date:\n%s", w.out)
	}
}

func TestInstallFromAnotherCheckoutKeepsFirstBackup(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	first := w.record().Backup
	moved := w.checkout("elsewhere/rexkit")
	if err := w.install(Options{Root: moved}); err != nil {
		t.Fatal(err)
	}
	if got := w.read(w.env.Paths.RexConfig); got != string(w.env.loader(moved)) {
		t.Errorf("init.lua does not load the new checkout:\n%s", got)
	}
	if rec := w.record(); rec.Root != moved || rec.Backup != first {
		t.Errorf("record = %+v, want root %s and the first backup %s", rec, moved, first)
	}
	if len(w.backups()) != 1 {
		t.Errorf("our own loader was backed up: %v", w.backups())
	}
	if !strings.Contains(w.out.String(), "switching from ~/code/rexkit to ~/elsewhere/rexkit") {
		t.Errorf("install did not point out the switch of checkout:\n%s", w.out)
	}
}

func TestBackupOncePerContent(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	// The user puts their old config back by hand, then installs again.
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if len(w.backups()) != 1 {
		t.Errorf("identical content was backed up twice: %v", w.backups())
	}
	w.write(w.env.Paths.RexConfig, "-- different\n")
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if len(w.backups()) != 2 {
		t.Errorf("new content was not backed up: %v", w.backups())
	}
}

// rexZellij lays out a rex-zellij checkout with the user's settings and the
// bootstrap its installer wrote, and returns the bootstrap.
func (w *world) rexZellij() string {
	root := filepath.Join(w.home, "code", "rex-zellij")
	w.write(filepath.Join(root, "init.lua"), "return {}\n")
	w.write(filepath.Join(root, "settings.lua"), `return {
  -- Rex's sequence-hint preference has not hidden persistent mode panels.
  pane_key_sequences = false,
  tiny_navigation = false,
  compact_labels = true,
  compact_menus = true,
  shell = "/opt/homebrew/bin/nu",
  python = "/opt/homebrew/bin/python3",
  editor = { "/opt/homebrew/bin/nvim" },
  resize_step = 0.05,
  scratch_label = "Scratch",
  alt_shortcuts = true,
}
`)
	w.write(filepath.Join(w.env.Paths.ConfigHome, "rex-zellij-backup-20261025-190746-870299", "init.lua"), "-- the original\n")
	return rexZellijMarker + "\nreturn dofile(" + longBracket(filepath.Join(root, "init.lua")) + ")\n"
}

var wantSettings = []string{"shell", "editor", "resize_step"}

func TestMigrateRexGotoWrapper(t *testing.T) {
	w := newWorld(t)
	gotoRoot := filepath.Join(w.home, "code", "rex-goto")
	w.write(filepath.Join(gotoRoot, "rex.lua"), "return {}\n")
	inner := filepath.Join(filepath.Dir(w.env.Paths.RexConfig), "init.before-rex-goto-20261025-211100.lua")
	w.write(inner, w.rexZellij())
	wrapper := rexGotoMarker + "\nlocal previous = dofile(" + longBracket(inner) + ")\nlocal picker = dofile(" +
		longBracket(filepath.Join(gotoRoot, "rex.lua")) + ")\nreturn previous\n"
	w.write(w.env.Paths.RexConfig, wrapper)
	w.write(w.env.Paths.LegacyRecord(), `{"backup": "`+inner+`", "root": "`+gotoRoot+`"}`)

	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	rec := w.record()
	if w.read(rec.Backup) != wrapper {
		t.Error("the rex-goto wrapper was not backed up verbatim")
	}
	if got := migratedFrom(rec); !slices.Equal(got, []string{"settings", "rex-goto", "rex-zellij"}) {
		t.Errorf("migrated = %v", got)
	}
	if len(rec.Originals) != 1 || !strings.Contains(rec.Originals[0], "rex-zellij-backup-") {
		t.Errorf("originals = %v, want the rex-zellij backup", rec.Originals)
	}
	assertMigratedSettings(t, w)
	if !w.exists(inner) || !w.exists(w.env.Paths.LegacyRecord()) {
		t.Error("install deleted a legacy file the backup still needs")
	}
}

func TestMigrateRexZellijBootstrap(t *testing.T) {
	w := newWorld(t)
	bootstrap := w.rexZellij()
	w.write(w.env.Paths.RexConfig, bootstrap)
	stateDir := filepath.Join(w.env.Paths.StateHome, "rex-zellij")
	w.write(filepath.Join(stateDir, "state.json"), zellijState)

	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	rec := w.record()
	if w.read(rec.Backup) != bootstrap {
		t.Error("the bootstrap was not backed up verbatim")
	}
	if got := migratedFrom(rec); !slices.Equal(got, []string{"settings", "rex-zellij"}) {
		t.Errorf("migrated = %v", got)
	}
	assertMigratedSettings(t, w)
	if w.exists(workspacePath(w)) {
		t.Error("rex-zellij's Scratch state was carried over; rexkit has no Scratch")
	}
	if !w.exists(stateDir) {
		t.Error("install deleted the legacy state")
	}
	if !strings.Contains(w.out.String(), "rm -r ~/.local/state/rex-zellij") {
		t.Errorf("install did not offer to delete the legacy state:\n%s", w.out)
	}
}

// zellijState is rex-zellij's state.json: its Scratch tab and parked panes.
const zellijState = `{"session:1":{"parked":{"block:5":{"window":"window:2","ratio":0.5}},` +
	`"previous":"window:2","scratch":"window:9","tools":{"files":"block:7"}},"session:2":{"parked":{}}}`

func TestExistingSettingsAreKept(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	w.write(w.env.Paths.Dirs.Settings(), "return { shell = \"/bin/zsh\" }\n")
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if got := w.read(w.env.Paths.Dirs.Settings()); !strings.Contains(got, "/bin/zsh") {
		t.Errorf("install replaced the user's settings:\n%s", got)
	}
}

func migratedFrom(rec Record) []string {
	var from []string
	for _, m := range rec.Migrated {
		from = append(from, m.From)
	}
	return from
}

func assertMigratedSettings(t *testing.T, w *world) {
	t.Helper()
	settings, err := readLuaData(w.env.Paths.Dirs.Settings())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(settings.Keys, wantSettings) {
		t.Errorf("settings.lua keys = %v, want %v", settings.Keys, wantSettings)
	}
	if v, _ := settings.Get("shell"); v != "/opt/homebrew/bin/nu" {
		t.Errorf("shell = %v", v)
	}
}

func TestCheckFailureWritesNothing(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	before := w.read(w.env.Paths.RexConfig)
	w.checkFails = true

	err := w.install(Options{})
	var cfg *ConfigError
	if !errors.As(err, &cfg) || !strings.Contains(err.Error(), "config was not changed") {
		t.Fatalf("err = %v, want a config check error", err)
	}
	if w.read(w.env.Paths.RexConfig) != before {
		t.Error("init.lua changed")
	}
	for _, path := range []string{w.env.Paths.Record(), w.env.Paths.Dirs.Settings(), w.env.Paths.Backups()} {
		if w.exists(path) {
			t.Errorf("%s was left behind", path)
		}
	}
	if w.reloads != 0 {
		t.Errorf("reloaded %d times, want 0", w.reloads)
	}
}

func TestReloadFailureRollsBack(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	w.write(filepath.Join(w.env.Paths.StateHome, "rex-zellij", "state.json"), zellijState)
	before := w.read(w.env.Paths.RexConfig)
	w.reloadFails = 1

	err := w.install(Options{})
	if err == nil || !strings.Contains(err.Error(), "previous config was restored") {
		t.Fatalf("err = %v, want a rolled-back reload", err)
	}
	if w.read(w.env.Paths.RexConfig) != before {
		t.Error("init.lua was not restored")
	}
	for _, path := range []string{w.env.Paths.Record(), w.env.Paths.Dirs.Settings()} {
		if w.exists(path) {
			t.Errorf("%s was not rolled back", path)
		}
	}
	if b := w.backups(); len(b) != 0 {
		t.Errorf("rollback left backups behind: %v", b)
	}
	if w.reloads != 2 {
		t.Errorf("reloads = %d, want 2 (the new config, then the restored one)", w.reloads)
	}
}

func TestInstallWhileRexIsDown(t *testing.T) {
	w := newWorld(t)
	w.down = true
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != string(w.env.loader(w.root)) {
		t.Error("a checked config should stay installed while Rex is down")
	}
	if !strings.Contains(w.out.String(), "Rex is not running") {
		t.Errorf("output does not explain the skipped reload:\n%s", w.out)
	}
}

func TestEditedLoaderIsRefused(t *testing.T) {
	w := newWorld(t)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	edited := w.read(w.env.Paths.RexConfig) + "rex.bind(\"ctrl+y\", \"pane.zoom\")\n"
	w.write(w.env.Paths.RexConfig, edited)
	for name, err := range map[string]error{
		"install":   w.install(Options{}),
		"uninstall": w.uninstall(UninstallOptions{}),
	} {
		if !errors.Is(err, ErrEdited) || !strings.Contains(err.Error(), "settings.lua") {
			t.Errorf("%s: err = %v, want ErrEdited with a way out", name, err)
		}
	}
	if w.read(w.env.Paths.RexConfig) != edited {
		t.Error("the edited loader was changed")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	before := w.read(w.env.Paths.RexConfig)
	if err := w.install(Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != before {
		t.Error("init.lua changed")
	}
	for _, path := range []string{w.env.Paths.Record(), w.env.Paths.Dirs.Settings(), w.env.Paths.Backups(), filepath.Join(w.root, "bin")} {
		if w.exists(path) {
			t.Errorf("dry run created %s", path)
		}
	}
	if w.reloads != 0 || w.count("go") != 0 {
		t.Error("dry run built or reloaded")
	}
	for _, want := range []string{"would replace", "would write", "would back up", "would install", "checked"} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("dry run output lacks %q:\n%s", want, w.out)
		}
	}
}

func TestBuildFailureChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.env.Go = ""
	if err := w.install(Options{}); err == nil || !strings.Contains(err.Error(), "go tool") {
		t.Fatalf("err = %v, want a missing go tool", err)
	}
	if w.exists(w.env.Paths.RexConfig) || w.exists(w.env.Paths.Record()) {
		t.Error("a failed build still installed")
	}
}

func TestFindRoot(t *testing.T) {
	w := newWorld(t)
	other := filepath.Join(w.home, "other")
	w.write(filepath.Join(other, "go.mod"), "module example.com/other\n")
	packaged := w.packaged("brew/opt/rexkit/libexec")
	keg := w.packaged("brew/Cellar/rexkit/1.0/libexec") // and opt/ leading to it, as Homebrew links them
	opt := filepath.Join(w.home, "brew", "opt", "rexkit-linked")
	if err := os.Symlink(filepath.Dir(keg), opt); err != nil {
		t.Fatal(err)
	}
	optLibexec := filepath.Join(opt, "libexec")
	tests := []struct {
		name                         string
		explicit, cwd, exe, packaged string
		want                         string
		wantErr                      bool
	}{
		{"explicit", w.root, "/", "/", "", w.root, false},
		{"explicit, not rexkit", other, "/", "/", "", "", true},
		{"explicit packaged tree", packaged, "/", "/", "", packaged, false},
		{"working directory below the root", "", filepath.Join(w.root, "lua"), "/", "", w.root, false},
		{"the binary's checkout", "", other, filepath.Join(w.root, "bin", "rexkit"), "", w.root, false},
		{"a packaged binary's own tree, even in a checkout", "", w.root, "/brew/bin/rexkit", packaged, packaged, false},
		{"a packaged tree that is gone", "", w.root, "/brew/bin/rexkit", other, "", true},
		{"explicit beats packaged", w.root, "/", "/", packaged, w.root, false},
		{"explicit versioned path of the packaged tree", keg, "/", "/", optLibexec, optLibexec, false},
		{"nowhere", "", other, "/usr/local/bin/rexkit", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findRoot(tt.explicit, tt.cwd, tt.exe, tt.packaged)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("findRoot = %q, %v; want %q (error %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestLoaderQuotesAwkwardPaths(t *testing.T) {
	for _, root := range []string{"/plain/rexkit", "/a]]b/rexkit", "/a]=]b]/rex kit", "/tail]"} {
		loader := string(Loader(root, "~/.config/rexkit/settings.lua"))
		paths := dofilePaths(loader)
		if want := filepath.Join(root, "lua", "init.lua"); len(paths) != 1 || paths[0] != want {
			t.Errorf("Loader(%q) loads %q, want %q", root, paths, want)
		}
		if !strings.HasPrefix(loader, Marker) {
			t.Errorf("Loader(%q) lacks the marker", root)
		}
	}
}

func TestExistingWorkspaceIsKept(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	w.write(filepath.Join(w.env.Paths.StateHome, "rex-zellij", "state.json"), zellijState)
	w.write(workspacePath(w), "{}")
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if got := w.read(workspacePath(w)); got != "{}" {
		t.Errorf("install replaced rexkit's own state: %s", got)
	}
}

// workspacePath is the Lua layer's state (lua/core.lua), which install
// leaves alone.
func workspacePath(w *world) string { return filepath.Join(w.env.Paths.Dirs.State, "workspace.json") }

// TestPackagedInstall installs from a packaged tree (Homebrew's): its binary
// is run, never built, and the loader names the tree's stable path.
func TestPackagedInstall(t *testing.T) {
	w := newWorld(t)
	root := w.packaged("brew/opt/rexkit/libexec")
	w.env.Go = "" // no Go toolchain needed
	if err := w.install(Options{Root: root}); err != nil {
		t.Fatalf("%v\n%s", err, w.out)
	}
	if got, want := w.read(w.env.Paths.RexConfig), string(w.env.loader(root)); got != want {
		t.Errorf("init.lua =\n%s\nwant\n%s", got, want)
	}
	for _, c := range w.calls {
		if c[0] == "go" {
			t.Errorf("ran %q for a packaged tree", c)
		}
	}
	for _, want := range []string{"(packaged)", "installed", "reloaded", "bin/rexkit · packaged"} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, w.out)
		}
	}
	// an upgrade replaces the tree's files behind the same path: install
	// then only reloads Rex
	w.out.Reset()
	if err := w.install(Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "up to date") || w.reloads != 2 {
		t.Errorf("reloads = %d, output:\n%s", w.reloads, w.out)
	}
}
