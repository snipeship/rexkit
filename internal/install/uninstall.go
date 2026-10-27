package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// UninstallOptions configure Uninstall.
type UninstallOptions struct {
	To     string // restore this file instead of the recorded backup
	DryRun bool
}

// Uninstall puts back the init.lua that rexkit's most recent takeover
// replaced (a symlink comes back as the same symlink), with the same care
// as Install: the old config must pass `rex config check` first, and a
// failed reload restores the loader.
func (e *Env) Uninstall(ctx context.Context, opts UninstallOptions) error {
	if !opts.DryRun {
		unlock, err := e.lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	if e.Rex == "" {
		return ErrNoRex
	}
	rec, ok, err := e.readRecord()
	if err != nil {
		return err
	}
	cur, err := e.readCurrent()
	if err != nil {
		return err
	}
	// Without a record, --to still works while rexkit's loader is in place.
	if !ok && (opts.To == "" || cur.kind != configManaged) {
		return fmt.Errorf("rexkit is not installed: %s does not exist%s", e.Paths.Tilde(e.Paths.Record()), e.restoreHint(rec))
	}
	switch cur.kind {
	case configManaged, configAbsent:
	case configEdited:
		return e.editedError(rec)
	default:
		return fmt.Errorf("%s is no longer rexkit's loader, so there is nothing to uninstall%s",
			e.Paths.Tilde(e.Paths.RexConfig), e.backupHint(rec))
	}

	next, from, err := e.restoreTarget(rec, opts.To)
	if err != nil {
		return err
	}

	p := e.printer()
	p.heading(pick(opts.DryRun, "rexkit uninstall (dry run)", "rexkit uninstall"))
	restored := Line{Mark: MarkOK, Name: "restored", Detail: from + " → " + e.Paths.Tilde(e.Paths.RexConfig)}
	switch {
	case !next.exists:
		restored = Line{Mark: MarkOK, Name: "removed", Detail: e.Paths.Tilde(e.Paths.RexConfig) + " (there was none before rexkit)"}
	case next.link != "":
		restored.Detail = e.Paths.Tilde(e.Paths.RexConfig) + " → " + from + " (the symlink again)"
	}
	if next.exists {
		content := next.data
		if next.link != "" {
			content, _ = os.ReadFile(linkTarget(e.Paths.RexConfig, next.link))
		}
		// Checked beside init.lua: a config may find its own files relative to itself.
		report, err := e.checkAt(ctx, filepath.Dir(e.Paths.RexConfig), content)
		if err != nil {
			return fmt.Errorf("%s no longer loads, so nothing was changed: %w%s", from, err, e.restoreHint(rec))
		}
		p.line(Line{Mark: MarkOK, Name: "checked", Detail: describeReport(report)})
	}
	if opts.DryRun {
		restored.Mark, restored.Name = MarkPlan, "would "+strings.TrimSuffix(restored.Name, "d")
		p.line(restored)
		p.line(Line{Mark: MarkPlan, Name: "would reload", Detail: "rex config reload"})
		return nil
	}

	u := e.newUndo()
	if err := u.change(e.Paths.RexConfig, next); err != nil {
		return errors.Join(err, u.run())
	}
	p.line(restored)
	if err := e.reload(ctx, p, u, nil); err != nil {
		return err
	}
	if err := removeIfExists(e.Paths.Record()); err != nil {
		return err
	}
	kept := "Kept: the backups in " + e.Paths.Tilde(e.Paths.Backups())
	if exists, _ := settingsExist(e.Paths.Dirs.Settings()); exists {
		kept += ", your settings in " + e.Paths.Tilde(e.Paths.Dirs.Settings())
	}
	if rec.Root != "" && !isCheckout(rec.Root) {
		p.note(kept + ". rexkit itself is still installed: brew uninstall rexkit")
	} else {
		p.note(kept + " and your clone.")
	}
	return nil
}

// restoreTarget is what init.lua becomes: the file --to names, the symlink
// or backup the record names, or nothing when there was no config before
// rexkit. from shows where it comes from.
func (e *Env) restoreTarget(rec Record, to string) (next fileState, from string, err error) {
	switch {
	case to != "":
		path, err := filepath.Abs(to)
		if err != nil {
			return next, "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return next, "", fmt.Errorf("cannot read the config to restore: %w%s", err, e.restoreHint(rec))
		}
		return fileState{exists: true, data: data}, e.Paths.Tilde(path), nil
	case rec.Link != "":
		// The file the link points to is the user's (a dotfiles repository,
		// say): rexkit never wrote it, and uninstall only puts the link back.
		target := linkTarget(e.Paths.RexConfig, rec.Link)
		if _, err := os.ReadFile(target); err != nil {
			return next, "", fmt.Errorf("%s was a symlink to %s, which cannot be read now: %w%s",
				e.Paths.Tilde(e.Paths.RexConfig), e.Paths.Tilde(rec.Link), err, e.restoreHint(rec))
		}
		return fileState{exists: true, link: rec.Link}, e.Paths.Tilde(rec.Link), nil
	case rec.Backup != "":
		data, err := os.ReadFile(rec.Backup)
		if err != nil {
			return next, "", fmt.Errorf("cannot read the config to restore: %w%s", err, e.restoreHint(rec))
		}
		return fileState{exists: true, data: data, mode: fs.FileMode(rec.Mode).Perm()}, e.Paths.Tilde(rec.Backup), nil
	case rec.NoConfig:
		return fileState{}, "", nil
	}
	return next, "", fmt.Errorf("the install record names no backup to restore%s", e.restoreHint(rec))
}

// linkTarget resolves a symlink's raw target the way the system does:
// relative to the link's directory.
func linkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(link), target)
}

func (e *Env) backupHint(rec Record) string {
	if rec.Backup == "" {
		return ""
	}
	return ".\n  Your config from before rexkit is " + e.Paths.Tilde(rec.Backup)
}

// restoreHint lists the configs that could be restored instead: the older
// ones the record names and every backup install made, newest first.
func (e *Env) restoreHint(rec Record) string {
	var files []string
	for _, f := range append(slices.Clone(rec.Originals), e.listBackups()...) {
		if _, err := os.Stat(f); err == nil && !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n  Configs rexkit kept:")
	for _, f := range files {
		b.WriteString("\n    " + e.Paths.Tilde(f))
	}
	b.WriteString("\n  Restore one with: rexkit uninstall --to FILE")
	return b.String()
}
