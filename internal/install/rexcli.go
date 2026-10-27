package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"rexkit/internal/rex"
)

// ErrRexNotRunning means the Rex server could not be reached. A config that
// passed `rex config check` is kept: Rex loads it when it starts.
var ErrRexNotRunning = errors.New("the Rex server is not running")

// ErrNoRex means the rex CLI is not installed.
var ErrNoRex = errors.New("the rex command was not found (is Rex installed?)")

// cliExitUnavailable is the rex CLI's exit status when it cannot connect.
const cliExitUnavailable = 4

// ConfigReport is what `rex config check --json` and `rex config reload
// --json` report about a config file.
type ConfigReport struct {
	Path        string                   `json:"path"`
	Exists      bool                     `json:"exists"`
	Bindings    int                      `json:"bindings"`
	Modes       int                      `json:"modes"`
	Actions     rex.List[map[string]any] `json:"actions"`
	Generation  int                      `json:"generation"`
	Diagnostics rex.List[rex.Diagnostic] `json:"diagnostics"`
}

// Problems returns the diagnostics of one severity ("error" or "warning").
func (r ConfigReport) Problems(severity string) []rex.Diagnostic {
	var out []rex.Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == severity {
			out = append(out, d)
		}
	}
	return out
}

// ConfigError is a config that Rex rejected.
type ConfigError struct {
	Op     string // "check" or "reload"
	Report ConfigReport
	Detail string
}

func (e *ConfigError) Error() string {
	msg := "rex config " + e.Op + " failed"
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	for i, d := range e.Report.Problems("error") {
		if i == 3 {
			msg += "\n    …"
			break
		}
		msg += "\n    " + describeDiagnostic(d)
	}
	return msg
}

func describeDiagnostic(d rex.Diagnostic) string {
	if d.Source == "" {
		return d.Message
	}
	return d.Source + ": " + d.Message
}

// rexCLI runs the rex CLI with rexkit's global flags.
func (e *Env) rexCLI(ctx context.Context, args ...string) (rex.Output, error) {
	if e.Rex == "" {
		return rex.Output{}, ErrNoRex
	}
	argv := append([]string{e.Rex, "--autostart=false", "--color=never", "--timeout", "20s"}, args...)
	return e.Run(ctx, argv, nil)
}

// interrupted reports a run that ended because ctx was cancelled (Ctrl-C,
// SIGTERM), so that a killed child is not mistaken for a rex failure.
func interrupted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("interrupted: %w", err)
	}
	return nil
}

// stderrMessage is the error a rex run printed, or its exit status when it
// printed none.
func stderrMessage(out rex.Output) string {
	if strings.TrimSpace(string(out.Stderr)) == "" {
		return fmt.Sprintf("rex exited with status %d", out.ExitCode)
	}
	return rex.CleanMessage(string(out.Stderr))
}

// checkConfig loads a config file with `rex config check` without touching
// the running server. It fails on any error diagnostic, and on a missing file
// (which rex itself reports as fine).
func (e *Env) checkConfig(ctx context.Context, path string) (ConfigReport, error) {
	out, err := e.rexCLI(ctx, "config", "check", "--json", path)
	if err != nil {
		return ConfigReport{}, err
	}
	if err := interrupted(ctx); err != nil {
		return ConfigReport{}, err
	}
	var r ConfigReport
	if jerr := json.Unmarshal(out.Stdout, &r); jerr != nil {
		return r, &ConfigError{Op: "check", Detail: stderrMessage(out)}
	}
	if !r.Exists {
		return r, &ConfigError{Op: "check", Report: r, Detail: path + " does not exist"}
	}
	if out.ExitCode != 0 || len(r.Problems("error")) > 0 {
		return r, &ConfigError{Op: "check", Report: r}
	}
	return r, nil
}

// checkAt checks content as a config file in dir, then removes the file.
func (e *Env) checkAt(ctx context.Context, dir string, content []byte) (ConfigReport, error) {
	f, err := os.CreateTemp(dir, ".rexkit-check-*.lua")
	if err != nil {
		return ConfigReport{}, err
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(content)
	if err := errors.Join(werr, f.Close()); err != nil {
		return ConfigReport{}, err
	}
	return e.checkConfig(ctx, f.Name())
}

// reloadConfig makes the running server read its config file again.
func (e *Env) reloadConfig(ctx context.Context) (ConfigReport, error) {
	out, err := e.rexCLI(ctx, "config", "reload", "--json")
	if err != nil {
		return ConfigReport{}, err
	}
	if err := interrupted(ctx); err != nil {
		return ConfigReport{}, err
	}
	var r ConfigReport
	jerr := json.Unmarshal(out.Stdout, &r)
	switch {
	case out.ExitCode == cliExitUnavailable || bytes.Contains(out.Stderr, []byte("connecting to Rex server")):
		return r, ErrRexNotRunning
	case jerr != nil || out.ExitCode != 0 || len(r.Problems("error")) > 0:
		detail := ""
		if len(r.Problems("error")) == 0 {
			detail = stderrMessage(out)
		}
		return r, &ConfigError{Op: "reload", Report: r, Detail: detail}
	}
	return r, nil
}

// apiMethods lists the server's API methods.
func (e *Env) apiMethods(ctx context.Context) ([]string, error) {
	out, err := e.rexCLI(ctx, "api", "list", "--json")
	if err != nil {
		return nil, err
	}
	if out.ExitCode != 0 {
		return nil, rexFailure(out)
	}
	var list struct {
		Methods rex.List[struct {
			Name string `json:"name"`
		}] `json:"methods"`
	}
	if err := json.Unmarshal(out.Stdout, &list); err != nil {
		return nil, fmt.Errorf("rex api list: %w", err)
	}
	names := make([]string, len(list.Methods))
	for i, m := range list.Methods {
		names[i] = m.Name
	}
	slices.Sort(names)
	return names, nil
}

// keymap is the running server's keymap.
func (e *Env) keymap(ctx context.Context) (rex.Keymap, error) {
	if e.Rex == "" {
		return rex.Keymap{}, ErrNoRex
	}
	client, err := rex.New(rex.WithBinary(e.Rex), rex.WithRunner(e.Run))
	if err != nil {
		return rex.Keymap{}, err
	}
	km, err := client.Keymap(ctx)
	if errors.Is(err, rex.ErrUnavailable) {
		return km, ErrRexNotRunning
	}
	return km, err
}

// rexVersion is the first line of `rex version`, such as "rex 0.0.17…".
func (e *Env) rexVersion(ctx context.Context) string {
	out, err := e.rexCLI(ctx, "version")
	if err != nil || out.ExitCode != 0 {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out.Stdout)), "\n")
	return first
}

func rexFailure(out rex.Output) error {
	if out.ExitCode == cliExitUnavailable {
		return ErrRexNotRunning
	}
	return errors.New(stderrMessage(out))
}

// luaModules are the modules lua/init.lua loads. Each one must load for
// rexkit to work.
var luaModules = []string{"core", "layout", "workspace", "tools", "attend", "bindings", "modes"}

// luaDiagnostics is what the Lua layer reports about itself through its
// internal rexkit_diagnostics action.
type luaDiagnostics struct {
	Modules  rex.List[string] `json:"modules"`   // the modules that loaded
	Failed   rex.List[string] `json:"failed"`    // why modules failed, when the layer lists them apart
	Warnings rex.List[string] `json:"warnings"`  // settings problems, and module failures
	StateDir string           `json:"state_dir"` // rexkit's state dir as the server sees it
	Attend   *struct {
		Token string `json:"token"` // of the agent watcher this load started (lua/attend.lua)
	} `json:"attend"`
}

// errNoLuaLayer means the server has no rexkit_diagnostics action: it is not
// running rexkit's Lua layer, or lua/tools.lua (which defines it) failed.
var errNoLuaLayer = errors.New("the Rex server has no rexkit_diagnostics action")

// luaDiagnostics asks the running server's Lua layer about itself.
func (e *Env) luaDiagnostics(ctx context.Context) (luaDiagnostics, error) {
	var d luaDiagnostics
	out, err := e.rexCLI(ctx, "do", "rexkit_diagnostics", "--json")
	if err != nil {
		return d, err
	}
	if err := interrupted(ctx); err != nil {
		return d, err
	}
	switch {
	case out.ExitCode == cliExitUnavailable || bytes.Contains(out.Stderr, []byte("connecting to Rex server")):
		return d, ErrRexNotRunning
	case out.ExitCode != 0 && bytes.Contains(out.Stderr, []byte("no action named")):
		return d, errNoLuaLayer
	case out.ExitCode != 0:
		return d, rexFailure(out)
	}
	if err := json.Unmarshal(out.Stdout, &d); err != nil {
		return d, fmt.Errorf("rex do rexkit_diagnostics: %w", err)
	}
	return d, nil
}

// problems lists the modules that did not load, each with the reason the
// layer gave when it gave one.
func (d luaDiagnostics) problems() []string {
	out := slices.Clone(d.Failed)
	known := append(slices.Clone(d.Failed), d.Warnings...)
	for _, m := range luaModules {
		if slices.Contains(d.Modules, m) || slices.ContainsFunc(out, isAbout(m)) {
			continue
		}
		reason := m + ".lua did not load"
		if i := slices.IndexFunc(known, isAbout(m)); i >= 0 {
			reason = known[i]
		}
		out = append(out, reason)
	}
	return out
}

// settingsWarnings are the warnings that are not about a module.
func (d luaDiagnostics) settingsWarnings() []string {
	var out []string
	for _, w := range d.Warnings {
		if !slices.Contains(d.Failed, w) && !slices.ContainsFunc(luaModules, func(m string) bool { return isAbout(m)(w) }) {
			out = append(out, w)
		}
	}
	return out
}

// isAbout matches a message about module m, such as "tools.lua failed to load: …".
func isAbout(m string) func(string) bool {
	return func(s string) bool { return strings.HasPrefix(s, m+".lua ") }
}

// warningLines shows the Lua layer's settings warnings as checklist lines.
func (d luaDiagnostics) warningLines(e *Env) []Line {
	var lines []Line
	for _, w := range d.settingsWarnings() {
		lines = append(lines, Line{Mark: MarkWarn, Name: "setting", Detail: w, Fix: "edit " + e.Paths.Tilde(e.Paths.Dirs.Settings())})
	}
	return lines
}
