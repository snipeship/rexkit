package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// The Rex app keeps its Remote Control switch in this preference; absent means off.
const (
	appDomain         = "com.superlogical.rex"
	remoteControlPref = "rex.client.remoteControl"
)

// checkConfigFile runs `rex config check` on init.lua. rexkit's own loader is
// checked in strict mode, so a Lua module that fails to load is an error
// rather than a runtime warning.
func (e *Env) checkConfigFile(ctx context.Context) Line {
	l := Line{Name: "config check"}
	var report ConfigReport
	var err error
	if cur, _ := e.readCurrent(); cur.kind == configManaged {
		report, err = e.checkAt(ctx, os.TempDir(), strictLoader(cur.data))
	} else {
		report, err = e.checkConfig(ctx, e.Paths.RexConfig)
	}
	var cfgErr *ConfigError
	switch {
	case errors.As(err, &cfgErr) && len(cfgErr.Report.Problems("error")) > 0:
		l.Mark, l.Fix = MarkFail, "rex config check"
		l.Detail = describeDiagnostic(cfgErr.Report.Problems("error")[0])
	case err != nil:
		l.Mark, l.Detail = MarkFail, err.Error()
	default:
		l.Mark, l.Detail = MarkOK, strings.TrimPrefix(describeReport(report), "rex config check: ")
		if w := report.Problems("warning"); len(w) > 0 {
			l.Mark, l.Fix = MarkWarn, "rex config check"
			l.Detail += ": " + describeDiagnostic(w[0])
		}
	}
	return l
}

// checkServer asks the running server which config it loaded and what its
// keymap makes of it.
func (e *Env) checkServer(ctx context.Context) []Line {
	km, err := e.keymap(ctx)
	if err != nil {
		l := Line{Mark: MarkWarn, Name: "loaded", Detail: "cannot ask Rex: " + err.Error()}
		if errors.Is(err, ErrRexNotRunning) {
			l.Detail = "Rex is not running, so the keymap was not checked"
		}
		return []Line{l}
	}
	for i := range km.Bindings {
		km.Bindings[i].Source = e.Paths.Tilde(km.Bindings[i].Source)
	}
	for i := range km.Diagnostics {
		km.Diagnostics[i].Source = e.Paths.Tilde(km.Diagnostics[i].Source)
	}
	return []Line{e.checkLoaded(km), checkKeymap(km), checkConflicts(km)}
}

// checkLuaLayer asks the running server's Lua layer which modules loaded
// and what it thinks of the settings. Without rexkit's loader installed, or
// with Rex down, the lines above already say what is wrong.
func (e *Env) checkLuaLayer(ctx context.Context) []Line {
	l := Line{Name: "lua layer"}
	d, err := e.luaDiagnostics(ctx)
	switch {
	case errors.Is(err, ErrRexNotRunning) || errors.Is(err, ErrNoRex):
		return nil
	case errors.Is(err, errNoLuaLayer):
		if cur, _ := e.readCurrent(); cur.kind != configManaged {
			return nil
		}
		l.Mark, l.Detail, l.Fix = MarkFail, "Rex is not running rexkit's Lua layer ("+err.Error()+")", "rex config reload, then rexkit doctor"
		return []Line{l}
	case err != nil:
		l.Mark, l.Detail = MarkWarn, "cannot ask Rex: "+err.Error()
		return []Line{l}
	}
	if problems := d.problems(); len(problems) > 0 {
		l.Mark, l.Detail, l.Fix = MarkFail, strings.ReplaceAll(problems[0], e.Paths.Home+"/", "~/"), "fix the checkout's lua/ directory, then: rex config reload"
		if len(problems) > 1 {
			l.Detail += fmt.Sprintf("; %d more", len(problems)-1)
		}
		return []Line{l}
	}
	l.Mark, l.Detail = MarkOK, ui.Plural(len(d.Modules), "module")+" loaded"
	return append([]Line{l}, d.warningLines(e)...)
}

func (e *Env) checkLoaded(km rex.Keymap) Line {
	l := Line{Mark: MarkOK, Name: "loaded"}
	data, err := os.ReadFile(e.Paths.RexConfig)
	switch {
	case km.Config.Path != "" && km.Config.Path != e.Paths.RexConfig:
		l.Mark, l.Detail = MarkWarn, "Rex reads "+e.Paths.Tilde(km.Config.Path)+", not "+e.Paths.Tilde(e.Paths.RexConfig)
		l.Fix = "set REX_CONFIG the same way for Rex and your shell"
	case err != nil:
		l.Mark, l.Detail = MarkWarn, "Rex has no config file loaded"
	case km.Config.Hash != "" && km.Config.Hash != sha256Hex(string(data)):
		l.Mark, l.Detail, l.Fix = MarkWarn, "Rex is running an older version of init.lua", "rex config reload"
	default:
		l.Detail = "Rex is running this config"
	}
	return l
}

// sha256Hex is how Rex identifies the config file it loaded.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func checkKeymap(km rex.Keymap) Line {
	l := Line{Mark: MarkOK, Name: "keymap"}
	var errs, warns []string
	for _, d := range km.Diagnostics {
		if d.Severity == "error" {
			errs = append(errs, describeDiagnostic(d))
		} else {
			warns = append(warns, describeDiagnostic(d))
		}
	}
	lua := map[string]bool{}
	for _, a := range km.Actions {
		lua[a.Name] = true
	}
	for _, b := range km.Bindings {
		// Config actions never contain a dot; client and block actions do.
		if !strings.Contains(b.Action, ".") && !lua[b.Action] {
			warns = append(warns, fmt.Sprintf("%s binds %s to the missing action %s", b.Source, keyName(b), b.Action))
		}
	}
	l.Detail = fmt.Sprintf("%s in %s", ui.Plural(len(km.Bindings), "binding"), ui.Plural(len(km.Modes), "mode"))
	switch {
	case len(errs) > 0:
		l.Mark, l.Fix = MarkFail, "rex keymap"
		l.Detail = fmt.Sprintf("%s: %s", ui.Plural(len(errs), "error"), errs[0])
	case len(warns) > 0:
		l.Mark, l.Fix = MarkWarn, "rex keymap"
		l.Detail += fmt.Sprintf(" · %s: %s", ui.Plural(len(warns), "warning"), warns[0])
	default:
		l.Detail += ", no warnings"
	}
	return l
}

// checkConflicts finds keys bound more than once in the same mode. Rex lists
// the binding that wins first.
func checkConflicts(km rex.Keymap) Line {
	type slot struct{ mode, keys string }
	seen := map[slot][]rex.Binding{}
	var order []slot
	for _, b := range km.Bindings {
		s := slot{b.Mode, strings.Join(b.Keys, " ")}
		if len(seen[s]) == 0 {
			order = append(order, s)
		}
		seen[s] = append(seen[s], b)
	}
	var found []string
	for _, s := range order {
		if bs := seen[s]; len(bs) > 1 {
			found = append(found, fmt.Sprintf("%s bound %d× (%s wins)", keyName(bs[0]), len(bs), bs[0].Source))
		}
	}
	l := Line{Mark: MarkOK, Name: "conflicts", Detail: "none"}
	if len(found) > 0 {
		l.Mark, l.Fix = MarkWarn, "rexkit keys"
		l.Detail = strings.Join(found[:min(2, len(found))], "; ")
		if len(found) > 2 {
			l.Detail += fmt.Sprintf("; %d more", len(found)-2)
		}
	}
	return l
}

func keyName(b rex.Binding) string {
	keys := strings.Join(b.Keys, " ")
	if b.Mode != "" {
		return b.Mode + "/" + keys
	}
	return keys
}

// checkRemoteControl reads the app's preference; it never asks the app to
// do anything.
func (e *Env) checkRemoteControl(ctx context.Context) Line {
	l := Line{Mark: MarkOff, Name: "remote control"}
	if e.Defaults == "" {
		l.Detail = "unknown on this system"
		return l
	}
	out, err := e.Run(ctx, []string{e.Defaults, "read", appDomain, remoteControlPref}, nil)
	if err == nil && out.ExitCode == 0 && strings.TrimSpace(string(out.Stdout)) == "1" {
		l.Mark, l.Detail = MarkOK, "on · double-click jumps across sessions"
		return l
	}
	l.Detail = "off · ⏎ still jumps anywhere; double-click only within a session"
	l.Fix = "optional: Rex Settings › Rex Server › Remote Control"
	return l
}

func (e *Env) checkLayers(ctx context.Context) Line {
	l := Line{Name: "floating layers"}
	methods, err := e.apiMethods(ctx)
	switch {
	case err != nil:
		l.Mark, l.Detail = MarkWarn, "unknown: "+err.Error()
	case !slices.Contains(methods, "session.new_layer") || !slices.Contains(methods, "session.close_layer"):
		l.Mark, l.Detail, l.Fix = MarkFail, "this Rex has no session.new_layer, which every tool needs", "update Rex"
	default:
		l.Mark, l.Detail = MarkOK, "supported"
	}
	return l
}
