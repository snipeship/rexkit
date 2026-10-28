package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"rexkit/internal/app/hints"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// ProblemsError is a doctor run that found broken things.
type ProblemsError struct {
	N         int
	Installed bool // the doctor ran after an install, which stays in place
}

func (e *ProblemsError) Error() string {
	if e.Installed {
		return "rexkit is installed, but doctor found " + ui.Plural(e.N, "problem")
	}
	return "doctor found " + ui.Plural(e.N, "problem")
}

// ExitCode makes the process exit 1.
func (e *ProblemsError) ExitCode() int { return 1 }

// Doctor checks the installation and prints a checklist. It returns a
// *ProblemsError when any check fails.
func (e *Env) Doctor(ctx context.Context, rootFlag string) error {
	root, err := e.doctorRoot(rootFlag)
	if err != nil {
		return err
	}
	p := e.printer()
	about := []string{runtime.GOOS + "/" + runtime.GOARCH}
	if v := e.rexVersion(ctx); v != "" {
		about = append([]string{strings.Replace(v, "rex ", "Rex ", 1)}, about...)
	}
	p.heading("rexkit doctor", about...)
	if n := countFailures(e.diagnose(ctx, p, root)); n > 0 {
		return &ProblemsError{N: n}
	}
	return nil
}

func countFailures(lines []Line) int {
	n := 0
	for _, l := range lines {
		if l.Mark == MarkFail {
			n++
		}
	}
	return n
}

// doctorRoot is the rexkit tree to check: --root, else the installed one,
// else the running binary's own (packaged) or the checkout around the working
// directory or the running binary. It is empty
// when none is found, which the binary check reports.
func (e *Env) doctorRoot(flag string) (string, error) {
	if flag == "" {
		if rec, ok, _ := e.readRecord(); ok && rec.Root != "" {
			return rec.Root, nil
		}
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	root, err := findRoot(flag, cwd, exe, Packaged)
	if flag == "" {
		return root, nil
	}
	return root, err
}

// diagnose runs every check, prints each line as it is known, then a summary.
func (e *Env) diagnose(ctx context.Context, p printer, root string) []Line {
	var lines []Line
	add := func(ls ...Line) {
		for _, l := range ls {
			p.line(l)
			lines = append(lines, l)
		}
	}
	add(e.checkBinary(root))
	add(e.checkChain(root))
	add(e.checkSettings(root))
	add(e.checkConfigFile(ctx))
	add(e.checkServer(ctx)...)
	add(e.checkLuaLayer(ctx)...)
	add(e.checkRemoteControl(ctx))
	add(e.checkLayers(ctx))
	add(e.checkApp(ctx))
	add(e.checkState()...)
	add(e.checkTools()...)
	p.summary(lines)
	return lines
}

func (e *Env) checkBinary(root string) Line {
	l := Line{Name: "binary"}
	if root == "" {
		l.Mark, l.Detail, l.Fix = MarkFail, "rexkit's files were not found", "rexkit doctor --root DIR"
		return l
	}
	bin := filepath.Join(root, "bin", "rexkit")
	info, err := os.Stat(bin)
	if err != nil {
		l.Mark, l.Detail, l.Fix = MarkFail, e.Paths.Tilde(bin)+" is missing", pick(isCheckout(root), "rexkit install", "reinstall rexkit")
		return l
	}
	if !isCheckout(root) {
		l.Detail = e.Paths.Tilde(bin) + " · packaged"
		return l
	}
	built := info.ModTime()
	l.Detail = fmt.Sprintf("%s · built %s", e.Paths.Tilde(bin), ago(built, e.Now()))
	if file, changed := newestSource(root); changed.After(built) {
		l.Mark, l.Fix = MarkWarn, "rexkit install"
		l.Detail += fmt.Sprintf(", but %s changed %s", file, ago(changed, e.Now()))
		return l
	}
	l.Detail += ", up to date"
	return l
}

// newestSource finds the most recently changed file that goes into the
// binary: Go sources (not tests), embedded Lua, go.mod and go.sum.
func newestSource(root string) (string, time.Time) {
	var newest time.Time
	var name string
	consider := func(path string, info fs.FileInfo) {
		if info.ModTime().After(newest) {
			newest = info.ModTime()
			name, _ = filepath.Rel(root, path)
		}
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		if info, err := os.Stat(filepath.Join(root, f)); err == nil {
			consider(filepath.Join(root, f), info)
		}
	}
	for _, dir := range []string{"cmd", "internal"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			ext := filepath.Ext(path)
			if d.IsDir() || strings.HasSuffix(path, "_test.go") || (ext != ".go" && ext != ".lua") {
				return nil
			}
			if info, err := d.Info(); err == nil {
				consider(path, info)
			}
			return nil
		})
	}
	return name, newest
}

func (e *Env) checkChain(root string) Line {
	l := Line{Name: "config", Mark: MarkFail}
	cur, err := e.readCurrent()
	if err != nil {
		l.Detail = err.Error()
		return l
	}
	file := e.Paths.Tilde(e.Paths.RexConfig)
	switch cur.kind {
	case configManaged:
		l.Mark, l.Detail = MarkOK, file+" → "+e.Paths.Tilde(filepath.Join(cur.root, "lua", "init.lua"))
		if _, err := os.Stat(filepath.Join(cur.root, "lua", "init.lua")); err != nil {
			l.Mark, l.Fix = MarkFail, "rexkit install --root DIR"
			l.Detail += " (missing)"
		} else if root != "" && cur.root != root {
			l.Mark, l.Fix = MarkWarn, "rexkit install --root "+e.Paths.Tilde(root)
			l.Detail += " (not this rexkit)"
		}
	case configAbsent:
		l.Detail, l.Fix = file+" does not exist", "rexkit install"
	case configEdited:
		l.Detail, l.Fix = file+" was edited after rexkit installed it", "move the edits to "+e.Paths.Tilde(e.Paths.Dirs.Settings())+", delete the file, rexkit install"
	case configRexGoto, configRexZellij:
		l.Detail, l.Fix = file+" still loads the old rex-goto/rex-zellij setup", "rexkit install (migrates it)"
	default:
		l.Detail, l.Fix = file+" is your own config; rexkit is not installed", "rexkit install (backs it up first)"
	}
	return l
}

// hintProblems checks the regular expressions and alphabets of
// hints_patterns, hints_alphabet and the hints setting's keys.
func hintProblems(settings *luaTable) []string {
	var out []string
	patterns := func(where string, v any) {
		t, _ := v.(*luaTable)
		if t == nil {
			return
		}
		var exprs []string
		for _, item := range t.Items {
			if e, ok := item.(string); ok {
				exprs = append(exprs, e)
			}
		}
		if _, err := hints.Compile(exprs, false); err != nil {
			out = append(out, where+": "+err.Error())
		}
	}
	alphabet := func(where string, v any) {
		if a, _ := v.(string); a != "" {
			if err := hints.CheckAlphabet(a); err != nil {
				out = append(out, where+": "+err.Error())
			}
		}
	}
	patterns("hints_patterns", settings.Fields["hints_patterns"])
	alphabet("hints_alphabet", settings.Fields["hints_alphabet"])
	if keys, _ := settings.Fields["hints"].(*luaTable); keys != nil {
		for i, item := range keys.Items {
			if hint, _ := item.(*luaTable); hint != nil {
				where := fmt.Sprintf("hints[%d]", i+1)
				patterns(where, hint.Fields["patterns"])
				alphabet(where, hint.Fields["alphabet"])
			}
		}
	}
	return out
}

// checkSettings looks for settings rexkit does not know, which the Lua layer
// ignores. Settings files that are code, not plain data, are not checked.
func (e *Env) checkSettings(root string) Line {
	path := e.Paths.Dirs.Settings()
	l := Line{Mark: MarkOK, Name: "settings", Detail: e.Paths.Tilde(path)}
	settings, err := readLuaData(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		l.Detail = "all defaults (no " + e.Paths.Tilde(path) + ")"
		return l
	case err != nil:
		l.Detail += " · not plain data, so not checked here"
		return l
	}
	l.Detail += " · " + ui.Plural(len(settings.Keys), "value")
	defaults, err := readDefaults(filepath.Join(root, "lua", "defaults.lua"))
	if err != nil {
		return l
	}
	var unknown, retired []string
	for _, k := range settings.Keys {
		if _, ok := defaults.Get(k); ok {
			continue
		}
		if slices.Contains(retiredSettings, k) {
			retired = append(retired, k)
		} else {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		l.Detail += " · unknown: " + strings.Join(unknown, ", ")
	}
	if len(retired) > 0 {
		l.Detail += " · no longer used: " + strings.Join(retired, ", ")
	}
	if len(unknown)+len(retired) > 0 {
		l.Mark, l.Fix = MarkWarn, "remove them from "+e.Paths.Tilde(path)
	}
	// Hints' patterns are Go regular expressions, which the Lua layer
	// can't check: one that won't compile only shows when Hints opens.
	if problems := hintProblems(settings); len(problems) > 0 {
		l.Detail += " · " + strings.Join(problems, " · ")
		if l.Mark == MarkOK {
			l.Mark, l.Fix = MarkWarn, "fix them in "+e.Paths.Tilde(path)
		}
	}
	return l
}

// retiredSettings are settings rexkit once had (lua/init.lua says so too):
// backdrop, from before Rex drew tools' floats itself, and scratch_label,
// from before ⌃P w and ⌃P e worked on floating panes.
var retiredSettings = []string{"backdrop", "scratch_label"}

func (e *Env) checkState() []Line {
	tools := e.Paths.Dirs.ToolsDir()
	l := Line{Mark: MarkOK, Name: "state", Detail: e.Paths.Tilde(e.Paths.Dirs.State)}
	entries, _ := os.ReadDir(tools)
	stale := 0
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && entry.IsDir() && e.Now().Sub(info.ModTime()) > state.ToolDirMaxAge {
			stale++
		}
	}
	if stale > 0 {
		l.Mark = MarkWarn
		l.Detail += fmt.Sprintf(" · %s older than a day", ui.Plural(stale, "tool dir"))
		l.Fix = "the next tool that opens removes them, or: rm -r " + e.Paths.Tilde(tools)
	} else {
		l.Detail += " · no stale tool dirs"
	}
	lines := []Line{l}
	// The old tools' state is in use until rexkit replaces them.
	if cur, _ := e.readCurrent(); cur.kind != configManaged {
		return lines
	}
	if legacy := e.legacyState(); len(legacy) > 0 {
		shown := make([]string, len(legacy))
		for i, d := range legacy {
			shown[i] = e.Paths.Tilde(d)
		}
		lines = append(lines, Line{Mark: MarkWarn, Name: "old state", Detail: strings.Join(shown, ", ") + " (unused)",
			Fix: "rm -r " + strings.Join(shown, " ")})
	}
	return lines
}

// checkTools looks for the programs the tools use.
func (e *Env) checkTools() []Line {
	tools := []struct {
		name    string
		missing Mark
		without string
		fix     string
	}{
		{"nvim", MarkWarn, "scrollback opens in less instead", "brew install neovim"},
		{"git", MarkWarn, "Files shows no git status", "xcode-select --install"},
		{"zoxide", MarkOff, "Files shows no PROJECTS", "optional: brew install zoxide"},
	}
	lines := make([]Line, len(tools))
	for i, t := range tools {
		if path, err := e.LookPath(t.name); err == nil {
			lines[i] = Line{Mark: MarkOK, Name: t.name, Detail: e.Paths.Tilde(path)}
			continue
		}
		lines[i] = Line{Mark: t.missing, Name: t.name, Detail: "not found · " + t.without, Fix: t.fix}
	}
	return lines
}
