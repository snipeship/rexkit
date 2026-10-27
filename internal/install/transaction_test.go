package install

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"rexkit/internal/rex"
)

func TestFlagsRefuseArguments(t *testing.T) {
	// Were the parse to fall through, DefaultEnv would fail on the missing
	// $HOME before anything is touched, with a different error.
	t.Setenv("HOME", "")
	tests := []struct {
		name string
		main func([]string) error
		args []string
		want string
	}{
		{"install", Main, []string{"extra-arg", "--dry-run", "--root", "/x"}, `install: unexpected argument "extra-arg"`},
		{"uninstall", UninstallMain, []string{"./old.lua", "--dry-run"}, "use --to ./old.lua"},
		{"doctor", DoctorMain, []string{"x"}, `doctor: unexpected argument "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.main(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// A failed upgrade must not take back what the user saved while it ran.
func TestFailedUpgradeKeepsConcurrentEdits(t *testing.T) {
	w := newWorld(t)
	settings, workspace := w.env.Paths.Dirs.Settings(), w.env.workspacePath()
	w.write(settings, "return { resize_step = 0.1 }\n")
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	loader := w.read(w.env.Paths.RexConfig)
	edited := "return { resize_step = 0.2, shell = \"/bin/zsh\" }\n"
	parked := `{"session:1":{"parked":{"block:5":{"window":"window:2"}}}}`
	w.onReload = func() { // the user saves settings and parks a pane meanwhile
		w.write(settings, edited)
		w.write(workspace, parked)
	}
	w.reloadFails = 1
	if err := w.install(Options{Root: w.checkout("elsewhere/rexkit")}); err == nil || !strings.Contains(err.Error(), "previous config was restored") {
		t.Fatalf("err = %v, want a rolled-back reload", err)
	}
	if w.read(w.env.Paths.RexConfig) != loader {
		t.Error("init.lua was not rolled back")
	}
	if got := w.read(settings); got != edited {
		t.Errorf("settings.lua = %q, want the edit made during the install", got)
	}
	if got := w.read(workspace); got != parked {
		t.Errorf("workspace.json = %q, want the park made during the install", got)
	}
}

// A migrated file the user changed after install wrote it survives the
// rollback, and the error says so.
func TestRollbackLeavesFilesChangedAfterTheWrite(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	settings := w.env.Paths.Dirs.Settings()
	w.onReload = func() { w.write(settings, "return { shell = \"/bin/bash\" }\n") }
	w.reloadFails = 1
	err := w.install(Options{})
	if err == nil || !strings.Contains(err.Error(), "settings.lua changed after rexkit wrote it") {
		t.Fatalf("err = %v, want the changed settings.lua reported", err)
	}
	if !strings.Contains(w.read(settings), "/bin/bash") {
		t.Error("rollback replaced the user's settings")
	}
	if w.exists(w.env.Paths.Record()) {
		t.Error("the record was not rolled back")
	}
}

// symlinkConfig makes init.lua a 0644 symlink into a dotfiles checkout and
// returns the target.
func symlinkConfig(t *testing.T, w *world) string {
	t.Helper()
	target := filepath.Join(w.home, "dotfiles", "init.lua")
	w.write(target, userConfig)
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(w.env.Paths.RexConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, w.env.Paths.RexConfig); err != nil {
		t.Fatal(err)
	}
	return target
}

func assertSymlink(t *testing.T, w *world, target string) {
	t.Helper()
	if got, err := os.Readlink(w.env.Paths.RexConfig); err != nil || got != target {
		t.Errorf("init.lua links to %q (%v), want %q", got, err, target)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 || w.read(target) != userConfig {
		t.Errorf("the dotfiles config changed: %v %v", info.Mode(), err)
	}
}

func TestFailedDryRunWritesNothing(t *testing.T) {
	w := newWorld(t)
	target := symlinkConfig(t, w)
	w.checkFails = true
	err := w.install(Options{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "config was not changed") {
		t.Fatalf("err = %v", err)
	}
	assertSymlink(t, w, target)
	for _, path := range []string{w.env.Paths.Record(), w.env.Paths.Dirs.Settings(), w.env.Paths.Backups()} {
		if w.exists(path) {
			t.Errorf("dry run created %s", path)
		}
	}
}

func TestSymlinkedConfigComesBack(t *testing.T) {
	w := newWorld(t)
	target := symlinkConfig(t, w)

	w.reloadFails = 1
	if err := w.install(Options{}); err == nil {
		t.Fatal("install succeeded although the reload failed")
	}
	assertSymlink(t, w, target)

	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "init.lua is a symlink") {
		t.Errorf("install did not explain what happens to the symlink:\n%s", w.out)
	}
	if info, _ := os.Lstat(w.env.Paths.RexConfig); info.Mode()&fs.ModeSymlink != 0 {
		t.Error("install wrote through the symlink instead of replacing it")
	}
	if rec := w.record(); rec.Link != target {
		t.Errorf("record link = %q", rec.Link)
	}
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	assertSymlink(t, w, target)
}

func TestUninstallKeepsTheConfigsMode(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	os.Chmod(w.env.Paths.RexConfig, 0o644)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(w.env.Paths.RexConfig); err != nil || info.Mode() != 0o644 {
		t.Errorf("init.lua mode = %v (%v), want 0644", info.Mode(), err)
	}
}

// An interrupt while Rex reloads must not stop the rollback.
func TestInterruptedReloadStillRollsBack(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.onReload = cancel // Ctrl-C arrives during the reload
	w.reloadFails = 1
	err := w.env.Install(ctx, Options{Root: w.root})
	if err == nil || !strings.Contains(err.Error(), "previous config was restored and reloaded") {
		t.Fatalf("err = %v, want a completed rollback", err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig {
		t.Error("init.lua was not restored")
	}
	if len(w.reloadCtx) != 2 || w.reloadCtx[1] != nil {
		t.Errorf("reloads saw %v, want two, the second with a live context", w.reloadCtx)
	}
}

func TestInterruptedCheckSaysSo(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.env.Run = func(context.Context, []string, []byte) (rex.Output, error) {
		return rex.Output{ExitCode: -1}, nil // killed
	}
	_, err := w.env.checkConfig(ctx, w.env.Paths.RexConfig)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("err = %v, want an interruption", err)
	}
}

func TestSilentRexFailureNamesItsStatus(t *testing.T) {
	w := newWorld(t)
	w.env.Run = func(context.Context, []string, []byte) (rex.Output, error) {
		return rex.Output{ExitCode: 3}, nil
	}
	_, err := w.env.reloadConfig(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 3") {
		t.Errorf("err = %v, want the real exit status", err)
	}
}

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// With output gone (`rexkit install | head`), install still finishes.
func TestInstallFinishesWithoutOutput(t *testing.T) {
	w := newWorld(t)
	w.env.Out = brokenPipe{}
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if w.reloads != 1 {
		t.Errorf("reloads = %d, want 1", w.reloads)
	}
}

func TestCheckCatchesModulesThatFailToLoad(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	w.strictFails = true
	err := w.install(Options{})
	var cfg *ConfigError
	if !errors.As(err, &cfg) || !strings.Contains(err.Error(), "config was not changed") {
		t.Fatalf("err = %v, want a failed strict check", err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig || w.reloads != 0 {
		t.Error("a config whose modules fail went live")
	}
}

func TestReloadChecksTheLuaLayer(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	w.luaDiag = `{"modules":["core","layout","workspace","tools","modes"],"warnings":["bindings.lua failed to load: boom"]}`
	err := w.install(Options{})
	if err == nil || !strings.Contains(err.Error(), "previous config was restored") || !strings.Contains(err.Error(), "bindings.lua failed to load: boom") {
		t.Fatalf("err = %v, want a rollback naming the module", err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig || w.reloads != 2 {
		t.Errorf("not rolled back: reloads = %d", w.reloads)
	}

	w.out.Reset()
	w.luaDiag = `{"modules":["core","layout","workspace","tools","attend","bindings","modes"],"warnings":["unknown setting \"colour\" is ignored"]}`
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), `⚠ setting`) || !strings.Contains(w.out.String(), `unknown setting "colour"`) {
		t.Errorf("settings warnings are not shown:\n%s", w.out)
	}
}

func TestInstallRefusesARexWithoutLayers(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	w.methods = []string{"session.view"}
	err := w.install(Options{})
	if err == nil || !strings.Contains(err.Error(), "no session.new_layer") {
		t.Fatalf("err = %v", err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig || w.exists(w.env.Paths.Record()) {
		t.Error("install went ahead on a Rex without layers")
	}
}

func TestInstallFailsWhenDoctorFindsProblems(t *testing.T) {
	w := newWorld(t)
	w.keymap = `{"diagnostics":[{"severity":"error","source":"modes.lua:2","message":"bad mode"}]}`
	err := w.install(Options{})
	var problems *ProblemsError
	if !errors.As(err, &problems) || !problems.Installed || problems.ExitCode() != 1 {
		t.Fatalf("err = %v, want installed with problems", err)
	}
	if !strings.Contains(err.Error(), "rexkit is installed") {
		t.Errorf("err = %v", err)
	}
	if w.read(w.env.Paths.RexConfig) != string(w.env.loader(w.root)) {
		t.Error("the checked config should stay installed")
	}
}

func TestDefaultEnvReadsThemeLikeTheTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REXKIT_THEME", " light\n")
	e, err := DefaultEnv(os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if e.Dark {
		t.Error("a padded REXKIT_THEME=light gave a dark report")
	}
}
