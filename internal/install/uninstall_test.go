package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUninstallRestoresTheFirstBackup(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	for range 2 {
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
	}
	reloads := w.reloads
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := w.read(w.env.Paths.RexConfig); got != userConfig {
		t.Errorf("init.lua = %q, want the original", got)
	}
	if w.exists(w.env.Paths.Record()) {
		t.Error("the install record is still there")
	}
	if len(w.backups()) != 1 {
		t.Error("uninstall should keep the backup")
	}
	if w.reloads != reloads+1 {
		t.Errorf("uninstall reloaded %d times, want 1", w.reloads-reloads)
	}
}

func TestUninstallRemovesAConfigThatWasNotThere(t *testing.T) {
	w := newWorld(t)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if w.exists(w.env.Paths.RexConfig) {
		t.Error("init.lua should be gone again")
	}
}

func TestUninstallRollsBackAFailedReload(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	loader := w.read(w.env.Paths.RexConfig)
	w.reloadFails = 1
	if err := w.uninstall(UninstallOptions{}); err == nil {
		t.Fatal("uninstall succeeded although the reload failed")
	}
	if w.read(w.env.Paths.RexConfig) != loader || !w.exists(w.env.Paths.Record()) {
		t.Error("a failed uninstall did not put the loader and record back")
	}
}

func TestUninstallRefusesABackupThatNoLongerLoads(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, w.rexZellij())
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	loader := w.read(w.env.Paths.RexConfig)
	w.checkFails = true // as if the rex-zellij checkout were deleted

	err := w.uninstall(UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "no longer loads") || !strings.Contains(err.Error(), "rex-zellij-backup-") {
		t.Fatalf("err = %v, want a refusal that points at the original config", err)
	}
	if w.read(w.env.Paths.RexConfig) != loader {
		t.Error("init.lua changed")
	}

	w.checkFails = false
	original := w.record().Originals[0]
	if err := w.uninstall(UninstallOptions{To: original}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != w.read(original) {
		t.Error("--to did not restore the chosen file")
	}
}

func TestUninstallLeavesForeignConfigsAlone(t *testing.T) {
	w := newWorld(t)
	if err := w.uninstall(UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("without a record: err = %v", err)
	}
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.uninstall(UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "no longer rexkit's loader") {
		t.Errorf("over a foreign config: err = %v", err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig {
		t.Error("uninstall replaced a config rexkit did not write")
	}
}

func TestUninstallDryRun(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	loader := w.read(w.env.Paths.RexConfig)
	reloads := w.reloads
	if err := w.uninstall(UninstallOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != loader || !w.exists(w.env.Paths.Record()) || w.reloads != reloads {
		t.Error("dry run changed something")
	}
	if !strings.Contains(w.out.String(), "would restore") {
		t.Errorf("output:\n%s", w.out)
	}
}

// Uninstall restores what the most recent takeover replaced.
func TestUninstallRestoresTheLatestTakeover(t *testing.T) {
	t.Run("no config, then the user's", func(t *testing.T) {
		w := newWorld(t)
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
		w.write(w.env.Paths.RexConfig, userConfig)
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
		if err := w.uninstall(UninstallOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := w.read(w.env.Paths.RexConfig); got != userConfig {
			t.Errorf("init.lua = %q, want the config the second install replaced", got)
		}
	})
	t.Run("A, then B", func(t *testing.T) {
		w := newWorld(t)
		w.write(w.env.Paths.RexConfig, userConfig)
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
		first := w.record().Backup
		w.write(w.env.Paths.RexConfig, "-- newer\n")
		if err := w.install(Options{}); err != nil {
			t.Fatal(err)
		}
		if rec := w.record(); len(rec.Originals) != 1 || rec.Originals[0] != first {
			t.Errorf("originals = %v, want the first backup %s", rec.Originals, first)
		}
		if err := w.uninstall(UninstallOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := w.read(w.env.Paths.RexConfig); got != "-- newer\n" {
			t.Errorf("init.lua = %q, want the newer config", got)
		}
	})
}

// Deleting ~/.config/rexkit loses the record, not the way back.
func TestLostRecordKeepsTheBackup(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	backup := w.record().Backup
	os.RemoveAll(w.env.Paths.Dirs.Config)

	err := w.uninstall(UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), filepath.Base(backup)) {
		t.Errorf("err = %v, want the backup named", err)
	}

	w.out.Reset()
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "was missing; uninstall will restore") {
		t.Errorf("install did not say it recovered the backup:\n%s", w.out)
	}
	if rec := w.record(); rec.Backup != backup || rec.NoConfig {
		t.Errorf("record = %+v, want backup %s", rec, backup)
	}
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig {
		t.Error("uninstall did not restore the original")
	}
}

func TestUninstallToWorksWithoutARecord(t *testing.T) {
	w := newWorld(t)
	w.write(w.env.Paths.RexConfig, userConfig)
	if err := w.install(Options{}); err != nil {
		t.Fatal(err)
	}
	backup := w.record().Backup
	os.Remove(w.env.Paths.Record())
	if err := w.uninstall(UninstallOptions{To: backup}); err != nil {
		t.Fatal(err)
	}
	if w.read(w.env.Paths.RexConfig) != userConfig {
		t.Error("--to did not restore the file")
	}
}

func TestListBackupsNewestFirst(t *testing.T) {
	w := newWorld(t)
	dir := w.env.Paths.Backups()
	for _, name := range []string{"init.20261026-120000.lua", "init.20261026-120000-2.lua", "init.20261025-090000.lua", "init.20261026-120000-10.lua"} {
		w.write(filepath.Join(dir, name), name)
	}
	var got []string
	for _, f := range w.env.listBackups() {
		got = append(got, filepath.Base(f))
	}
	want := []string{"init.20261026-120000-10.lua", "init.20261026-120000-2.lua", "init.20261026-120000.lua", "init.20261025-090000.lua"}
	if !slices.Equal(got, want) {
		t.Errorf("listBackups = %v, want %v", got, want)
	}
}
