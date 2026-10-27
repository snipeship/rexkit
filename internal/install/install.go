// Package install puts rexkit into Rex and takes it out again. Install builds
// bin/rexkit (or, packaged, checks the one it came with) and makes Rex's
// init.lua a one-line loader of the checkout's or the package's Lua
// layer; it checks the new config before touching the live one, keeps one
// backup per distinct config it replaces, reloads Rex and rolls everything
// back if the reload fails. It migrates the rex-goto and rex-zellij installs
// rexkit replaces. Uninstall restores the config from before rexkit, and
// Doctor explains what is wrong with an installation.
//
// Every path comes from Env, so tests run against temp dirs and a fake rex.
package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"rexkit/internal/ui"
)

// Options configure Install.
type Options struct {
	Root   string // the rexkit checkout or packaged tree; found from the binary or the working directory when empty
	DryRun bool   // report what would change and change nothing
}

// ErrEdited means init.lua carries rexkit's marker but was changed by hand.
var ErrEdited = errors.New("init.lua was edited after rexkit installed it")

// Install installs or upgrades rexkit. It is idempotent: a second run with
// nothing new changes no file. It returns a *ProblemsError when rexkit is
// installed but the doctor run that follows finds something broken.
func (e *Env) Install(ctx context.Context, opts Options) error {
	if !opts.DryRun {
		unlock, err := e.lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	in, err := e.newInstaller(opts)
	if err != nil {
		return err
	}
	in.p.heading(pick(opts.DryRun, "rexkit install (dry run)", "rexkit install"), e.Paths.Tilde(in.root))
	in.announce()
	if err := in.build(ctx); err != nil {
		return err
	}
	in.announceLegacy()
	if err := in.migrate(); err != nil {
		return in.fail(err)
	}
	if err := in.check(ctx); err != nil {
		return in.fail(fmt.Errorf("the new config does not load, so your config was not changed: %w", err))
	}
	if err := in.checkRex(ctx); err != nil {
		return in.fail(err)
	}
	if opts.DryRun {
		in.plan()
		return nil
	}
	if err := in.write(); err != nil {
		return in.fail(err)
	}
	if err := e.reload(ctx, in.p, in.undo, in.verify); err != nil {
		return err
	}
	e.cleanupNote(in.p)
	fmt.Fprintln(e.Out)
	in.p.heading("rexkit doctor")
	if n := countFailures(e.diagnose(ctx, in.p, in.root)); n > 0 {
		return &ProblemsError{N: n, Installed: true}
	}
	return nil
}

// installer is one run of Install.
type installer struct {
	env    *Env
	opts   Options
	p      printer
	root   string
	loader []byte
	cur    current
	rec    Record // as found
	next   Record // as it will be written
	found  bool   // a record existed
	leg    legacy
	undo   *undo // the writes made so far; a dry run makes none
	notes  []Line
}

func (e *Env) newInstaller(opts Options) (*installer, error) {
	if e.Rex == "" {
		return nil, ErrNoRex
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	root, err := findRoot(opts.Root, cwd, exe, Packaged)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, "lua", "init.lua")); err != nil {
		return nil, fmt.Errorf("%s/lua/init.lua is missing: are rexkit's files complete?", root)
	}
	in := &installer{env: e, opts: opts, p: e.printer(), root: root, loader: e.loader(root), undo: e.newUndo()}
	if in.cur, err = e.readCurrent(); err != nil {
		return nil, err
	}
	if in.rec, in.found, err = e.readRecord(); err != nil {
		return nil, err
	}
	if in.cur.kind == configEdited {
		return nil, e.editedError(in.rec)
	}
	in.leg = e.findLegacy(in.cur)
	in.next = in.rec
	in.next.Root = root
	if !in.found {
		in.next.InstalledAt = e.Now().UTC()
		in.next.NoConfig = !in.cur.exists()
		if in.cur.kind == configManaged {
			in.recoverBackup()
		}
	}
	if prev := pick(in.cur.kind == configManaged, in.cur.root, in.rec.Root); prev != "" && prev != root {
		in.notes = append(in.notes, Line{Mark: MarkWarn, Name: "location",
			Detail: "switching from " + e.Paths.Tilde(prev) + " to " + e.Paths.Tilde(root),
			Fix:    "to keep the old one: rexkit install --root " + e.Paths.Tilde(prev)})
	}
	return in, nil
}

// recoverBackup rebuilds the record's restore target when the record is gone
// (~/.config/rexkit was deleted) but rexkit's loader is still installed.
// Install never backs up its own loader, so every backup is a config rexkit
// replaced; the newest one is what the latest takeover replaced.
func (in *installer) recoverBackup() {
	e := in.env
	backups := e.listBackups()
	if len(backups) == 0 {
		in.notes = append(in.notes, Line{Mark: MarkWarn, Name: "record",
			Detail: e.Paths.Tilde(e.Paths.Record()) + " is missing and " + e.Paths.Tilde(e.Paths.Backups()) + " holds no backup",
			Fix:    "uninstall will need: rexkit uninstall --to FILE"})
		return
	}
	in.next.Backup, in.next.Originals = backups[0], backups[1:]
	in.notes = append(in.notes, Line{Mark: MarkWarn, Name: "record",
		Detail: e.Paths.Tilde(e.Paths.Record()) + " was missing; uninstall will restore " + e.Paths.Tilde(backups[0]),
		Fix:    "to restore another: rexkit uninstall --to FILE"})
}

// fail undoes a real run's writes after err. A dry run wrote nothing, so it
// has nothing to undo.
func (in *installer) fail(err error) error {
	if in.opts.DryRun {
		return err
	}
	return errors.Join(err, in.undo.run())
}

// verb picks the name for a step: what happened, or what a dry run would do.
func (in *installer) verb(done, planned string) (Mark, string) {
	if in.opts.DryRun {
		return MarkPlan, planned
	}
	return MarkOK, done
}

func (in *installer) step(done, planned, detail string) {
	mark, name := in.verb(done, planned)
	in.p.line(Line{Mark: mark, Name: name, Detail: detail})
}

func (in *installer) build(ctx context.Context) error {
	if !isCheckout(in.root) {
		// a packaged tree comes with its binary: run it once, as a new build
		version, err := in.env.probe(ctx, in.root)
		if err != nil {
			return err
		}
		in.p.line(Line{Mark: MarkOK, Name: "binary", Detail: "bin/rexkit · " + version + " (packaged)"})
		return nil
	}
	if in.opts.DryRun {
		in.step("", "build", "would build bin/rexkit and run it once")
		return nil
	}
	version, err := in.env.build(ctx, in.root)
	if err != nil {
		return err
	}
	in.step("built", "", "bin/rexkit · "+version)
	return nil
}

// announce prints what the run noticed before doing anything.
func (in *installer) announce() {
	for _, l := range in.notes {
		in.p.line(l)
	}
}

func (in *installer) announceLegacy() {
	for _, m := range in.leg.migrations() {
		in.step("replacing", "would replace", m.From+" · "+in.env.Paths.Tilde(m.Root))
	}
}

// migrate carries rex-zellij's settings and Scratch state over.
func (in *installer) migrate() error {
	if in.leg.zellijRoot == "" {
		return nil
	}
	e := in.env
	settings, data, err := e.migrateSettings(in.leg.zellijRoot, in.root)
	if err != nil {
		return err
	}
	switch {
	case settings == nil:
	case settings.Skipped != "":
		in.next.Migrated = append(in.next.Migrated, *settings)
		in.p.line(Line{Mark: MarkWarn, Name: "settings",
			Detail: "not carried over: " + e.Paths.Tilde(settings.Skipped) + " (rex-zellij's settings are Lua code, not plain values)",
			Fix:    "copy the values you changed into " + e.Paths.Tilde(e.Paths.Dirs.Settings()) + " by hand"})
	default:
		if data != nil && !in.opts.DryRun {
			if err := in.undo.write(settings.Wrote, data, 0); err != nil {
				return err
			}
		}
		in.next.Migrated = append(in.next.Migrated, *settings)
		if settings.Wrote == "" {
			in.p.line(Line{Mark: MarkOK, Name: "settings", Detail: "every rex-zellij setting matches a default"})
		} else {
			in.step("settings", "would write", e.Paths.Tilde(settings.Wrote)+" · "+strings.Join(settings.Settings, ", "))
		}
		if len(settings.Dropped) > 0 {
			in.p.line(Line{Mark: MarkPlan, Detail: "left behind: " + strings.Join(settings.Dropped, ", ")})
		}
	}
	scratch, data, err := e.migrateScratch()
	if err != nil {
		return err
	}
	if scratch != nil {
		if !in.opts.DryRun {
			if err := in.undo.write(scratch.Wrote, data, 0); err != nil {
				return err
			}
		}
		in.next.Migrated = append(in.next.Migrated, *scratch)
		in.step("scratch", "would write", e.Paths.Tilde(scratch.Wrote)+" · from "+e.Paths.Tilde(scratch.Files[0]))
	}
	return nil
}

// strictPrefix makes lua/init.lua raise an error for a module that fails to
// load, instead of isolating it, so that `rex config check` reports it as an
// error diagnostic. Only checks use it; the installed loader keeps the
// runtime's isolation.
const strictPrefix = "rexkit_strict = true\n"

func strictLoader(loader []byte) []byte { return append([]byte(strictPrefix), loader...) }

// check runs `rex config check` on the new loader before anything goes live.
// The loader names its files by absolute path, so the temp file can live
// anywhere.
func (in *installer) check(ctx context.Context) error {
	report, err := in.env.checkAt(ctx, os.TempDir(), strictLoader(in.loader))
	if err != nil {
		return err
	}
	in.p.line(Line{Mark: MarkOK, Name: "checked", Detail: describeReport(report)})
	return nil
}

// checkRex refuses a running Rex that lacks the floating-layer API every
// tool needs. A Rex that cannot be asked is not held against the install.
func (in *installer) checkRex(ctx context.Context) error {
	methods, err := in.env.apiMethods(ctx)
	if err != nil || hasLayers(methods) {
		return nil
	}
	return errors.New("this Rex has no session.new_layer, which every rexkit tool needs: update Rex and run rexkit install again; your config was not changed")
}

func hasLayers(methods []string) bool {
	return slices.Contains(methods, "session.new_layer") && slices.Contains(methods, "session.close_layer")
}

// verify asks the reloaded server whether every module of the Lua layer
// loaded. lua/init.lua isolates a failing module at runtime, so Rex accepts
// the config either way; a missing module makes the reload a failure.
func (in *installer) verify(ctx context.Context) ([]Line, error) {
	d, err := in.env.luaDiagnostics(ctx)
	switch {
	case errors.Is(err, errNoLuaLayer):
		return nil, errors.New("rexkit's Lua layer did not load: " + err.Error())
	case err != nil:
		return []Line{{Mark: MarkWarn, Name: "lua layer", Detail: "not verified: " + err.Error(), Fix: "rexkit doctor"}}, nil
	}
	if problems := d.problems(); len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return d.warningLines(in.env), nil
}

func (in *installer) changed() bool { return !bytes.Equal(in.cur.data, in.loader) }

// needsBackup: a config that is not rexkit's own loader is kept before it is
// replaced.
func (in *installer) needsBackup() bool {
	return in.changed() && in.cur.exists() && in.cur.kind != configManaged
}

func (in *installer) target() string {
	return in.env.Paths.Tilde(in.env.Paths.RexConfig) + " → " + in.env.Paths.Tilde(filepath.Join(in.root, "lua", "init.lua"))
}

// plan reports what a real run would write.
func (in *installer) plan() {
	e := in.env
	if in.needsBackup() {
		in.step("", "would back up", e.Paths.Tilde(e.Paths.RexConfig)+" → "+e.Paths.Tilde(e.Paths.Backups()))
		in.linkNote()
	}
	if in.changed() {
		in.step("", "would install", in.target())
	}
	in.step("", "would reload", "rex config reload")
}

// linkNote explains what happens to a symlinked init.lua: install replaces
// the link, never the file it points to, and uninstall puts it back.
func (in *installer) linkNote() {
	if in.cur.link != "" {
		in.p.line(Line{Mark: MarkPlan, Detail: "init.lua is a symlink → " + in.env.Paths.Tilde(in.cur.link) +
			"; rexkit replaces the link, not that file, and uninstall puts the link back"})
	}
}

// takeOver makes the config being replaced the one uninstall restores. The
// previous restore target, if any, joins the older configs.
func (in *installer) takeOver(backup string) {
	next := &in.next
	originals := slices.DeleteFunc(slices.Clone(next.Originals), func(o string) bool { return o == backup })
	if prev := next.Backup; prev != "" && prev != backup && !slices.Contains(originals, prev) {
		originals = append([]string{prev}, originals...)
	}
	next.Originals = originals
	next.Backup, next.NoConfig = backup, false
	next.Link, next.Mode = in.cur.link, uint32(in.cur.mode)
}

// write backs up the current config, then writes the record and the loader.
func (in *installer) write() error {
	e := in.env
	if in.needsBackup() {
		path, created, err := e.backup(in.cur.data)
		if err != nil {
			return err
		}
		if created {
			in.undo.remove(path)
		}
		in.takeOver(path)
		in.step("backed up", "", e.Paths.Tilde(e.Paths.RexConfig)+" → "+e.Paths.Tilde(path))
		in.linkNote()
	}
	if in.leg.found() {
		in.next.Migrated = append(in.next.Migrated, e.stamp(in.leg.migrations())...)
		for _, o := range in.leg.originals {
			if o != in.next.Backup && !slices.Contains(in.next.Originals, o) {
				in.next.Originals = append(slices.Clone(in.next.Originals), o)
			}
		}
	}
	if !in.found || !reflect.DeepEqual(in.rec, in.next) {
		if err := in.undo.writeJSON(e.Paths.Record(), in.next); err != nil {
			return err
		}
	}
	if !in.changed() {
		in.step("up to date", "", e.Paths.Tilde(e.Paths.RexConfig))
		return nil
	}
	if err := in.undo.write(e.Paths.RexConfig, in.loader, 0); err != nil {
		return err
	}
	in.step("installed", "", in.target())
	return nil
}

// Rex is asked to reload for at most this long, and as long again to reload
// the restored files after a failure. The rex CLI's own --timeout is shorter.
const reloadTimeout = 45 * time.Second

// reload makes Rex read the new config and, when verify is set, checks what
// Rex made of it. If Rex rejects it, undo restores the previous files and Rex
// reloads them. A Rex that is not running keeps the checked config for its
// next start. The files are already written, so an interrupt no longer
// cancels: the server and the files must end up agreeing.
func (e *Env) reload(ctx context.Context, p printer, u *undo, verify func(context.Context) ([]Line, error)) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reloadTimeout)
	defer cancel()
	report, err := e.reloadConfig(rctx)
	var extra []Line
	switch {
	case errors.Is(err, ErrRexNotRunning):
		p.line(Line{Mark: MarkWarn, Name: "not reloaded", Detail: "Rex is not running; it loads the new config when it starts"})
		return nil
	case err == nil && verify != nil:
		extra, err = verify(rctx)
	}
	if err != nil {
		undoErr := u.run()
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reloadTimeout)
		defer cancel()
		_, reloadErr := e.reloadConfig(bctx)
		if undoErr != nil || (reloadErr != nil && !errors.Is(reloadErr, ErrRexNotRunning)) {
			return fmt.Errorf("reloading the new config failed (%w), and so did restoring the previous one: %w", err, errors.Join(undoErr, reloadErr))
		}
		return fmt.Errorf("reloading the new config failed, so the previous config was restored and reloaded: %w", err)
	}
	p.line(Line{Mark: MarkOK, Name: "reloaded", Detail: fmt.Sprintf("Rex is running it (generation %d)", report.Generation)})
	for _, d := range report.Problems("warning") {
		p.line(Line{Mark: MarkWarn, Name: "warning", Detail: describeDiagnostic(d)})
	}
	for _, l := range extra {
		p.line(l)
	}
	return nil
}

// editedError explains why install and uninstall leave a hand-edited loader alone.
func (e *Env) editedError(rec Record) error {
	hint := ""
	if rec.Backup != "" {
		hint = "\n  Your config from before rexkit is " + e.Paths.Tilde(rec.Backup) + "."
	}
	return fmt.Errorf("%s: %w, so rexkit leaves it alone.\n"+
		"  Move your changes into %s (or a Lua file of your own),\n"+
		"  then delete %s and run rexkit install again.%s",
		e.Paths.Tilde(e.Paths.RexConfig), ErrEdited, e.Paths.Tilde(e.Paths.Dirs.Settings()), e.Paths.Tilde(e.Paths.RexConfig), hint)
}

// cleanupNote lists the legacy tools' state, which install leaves for the
// user to delete.
func (e *Env) cleanupNote(p printer) {
	dirs := e.legacyState()
	if len(dirs) == 0 {
		return
	}
	shown := make([]string, len(dirs))
	for i, d := range dirs {
		shown[i] = e.Paths.Tilde(d)
	}
	p.note("The old tools' state is no longer used. Delete it when you no longer need it:\n  rm -r " + strings.Join(shown, " "))
}

func (e *Env) stamp(ms []Migration) []Migration {
	for i := range ms {
		ms[i].At = e.Now().UTC()
	}
	return ms
}

func describeReport(r ConfigReport) string {
	s := fmt.Sprintf("rex config check: %s · %s · %s",
		ui.Plural(r.Bindings, "binding"), ui.Plural(r.Modes, "mode"), ui.Plural(len(r.Actions), "action"))
	if w := len(r.Problems("warning")); w > 0 {
		s += " · " + ui.Plural(w, "warning")
	}
	return s
}

func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
