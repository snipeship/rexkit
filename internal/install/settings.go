package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// settingsPlan is the settings.lua install writes from rex-zellij's settings:
// only the values that differ from rexkit's defaults.
type settingsPlan struct {
	sources []string // files read, in merge order
	keep    []string // keys carried over, in the defaults' order
	dropped []string // keys rexkit has no setting for
	data    []byte   // the new file; nil when every value is a default
}

// legacySettings are rex-zellij's settings files in the checkout the
// bootstrap loaded: settings.lua, then settings.local.lua over it.
func legacySettings(zellijRoot string) []string {
	var found []string
	for _, name := range []string{"settings.lua", "settings.local.lua"} {
		if _, err := os.Stat(filepath.Join(zellijRoot, name)); err == nil {
			found = append(found, filepath.Join(zellijRoot, name))
		}
	}
	return found
}

// planSettings merges the legacy settings files and keeps what differs from
// the defaults file.
func planSettings(sources []string, defaultsPath string, now time.Time) (settingsPlan, error) {
	plan := settingsPlan{sources: sources}
	defaults, err := readDefaults(defaultsPath)
	if err != nil {
		return plan, err
	}
	merged := &luaTable{}
	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			return plan, err
		}
		t, err := parseLuaData(string(data))
		if err != nil {
			return plan, &legacyCodeError{file: src, err: err}
		}
		for _, k := range t.Keys {
			merged.set(k, t.Fields[k])
		}
	}
	out := &luaTable{}
	for _, k := range merged.Keys {
		def, known := defaults.Get(k)
		switch {
		case !known:
			plan.dropped = append(plan.dropped, k)
		case !luaEqual(merged.Fields[k], def):
			out.set(k, merged.Fields[k])
		}
	}
	slices.SortStableFunc(out.Keys, func(a, b string) int {
		return slices.Index(defaults.Keys, a) - slices.Index(defaults.Keys, b)
	})
	plan.keep = out.Keys
	if len(out.Keys) > 0 {
		plan.data = settingsFile(out, sources, plan.dropped, now)
	}
	return plan, nil
}

// readDefaults reads lua/defaults.lua, a list of `{ name, default,
// description, ... }` entries, as a table of name = default.
func readDefaults(path string) (*luaTable, error) {
	list, err := readLuaData(path)
	if err != nil {
		return nil, err
	}
	defaults := &luaTable{}
	for i, item := range list.Items {
		entry, ok := item.(*luaTable)
		var name string
		if ok && len(entry.Items) >= 2 {
			name, ok = entry.Items[0].(string)
		}
		if !ok {
			return nil, fmt.Errorf("%s: entry %d is not { name, default, description }", path, i+1)
		}
		defaults.set(name, entry.Items[1])
	}
	return defaults, nil
}

func readLuaData(path string) (*luaTable, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := parseLuaData(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

func settingsFile(t *luaTable, sources, dropped []string, now time.Time) []byte {
	var b strings.Builder
	b.WriteString("-- rexkit settings. Only values that differ from lua/defaults.lua belong here;\n")
	b.WriteString("-- `rexkit doctor` checks the file and Rex reloads it with its config.\n")
	fmt.Fprintf(&b, "-- Carried over from rex-zellij on %s:\n", now.Format("2006-01-02"))
	for _, src := range sources {
		b.WriteString("--   " + luaComment(src) + "\n")
	}
	if len(dropped) > 0 {
		shown := make([]string, len(dropped))
		for i, k := range dropped {
			shown[i] = luaComment(k)
		}
		b.WriteString("-- Not carried over (rexkit has no such setting): " + strings.Join(shown, ", ") + ".\n")
	}
	b.WriteString("return " + formatLua(t, 0) + "\n")
	return []byte(b.String())
}

// legacyCodeError is a rex-zellij settings file that is Lua code, not the
// plain data rexkit reads without running Lua. rex-zellij ran its settings
// with dofile, so code was legal there; install skips the migration.
type legacyCodeError struct {
	file string
	err  error
}

func (e *legacyCodeError) Error() string { return e.file + ": " + e.err.Error() }
func (e *legacyCodeError) Unwrap() error { return e.err }

// migrateSettings plans carrying rex-zellij's non-default settings over into
// a new settings.lua, and returns the file to write (nil for none). It never
// replaces an existing settings file. A legacy file that is code is skipped
// with the reason in Migration.Skipped; a broken lua/defaults.lua or an I/O
// error fails.
func (e *Env) migrateSettings(zellijRoot, root string) (*Migration, []byte, error) {
	dest := e.Paths.Dirs.Settings()
	if exists, err := settingsExist(dest); exists || err != nil {
		return nil, nil, err
	}
	sources := legacySettings(zellijRoot)
	if len(sources) == 0 {
		return nil, nil, nil
	}
	plan, err := planSettings(sources, filepath.Join(root, "lua", "defaults.lua"), e.Now())
	var code *legacyCodeError
	if errors.As(err, &code) {
		return &Migration{From: "settings", Files: sources, Skipped: code.Error(), At: e.Now().UTC()}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading rex-zellij's settings: %w", err)
	}
	m := &Migration{From: "settings", Files: sources, Settings: plan.keep, Dropped: plan.dropped, At: e.Now().UTC()}
	if plan.data != nil {
		m.Wrote = dest
	}
	return m, plan.data, nil
}

// settingsExist reports whether the user already has a settings file, which
// install never overwrites.
func settingsExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
