package install

import (
	"errors"
	"path/filepath"
	"strings"

	"rexkit/internal/state"
)

// Paths are every location the installer reads or writes. They all come
// from the environment (PathsFrom), so tests point them at temp dirs.
type Paths struct {
	Home       string     // for display (~) only
	ConfigHome string     // $XDG_CONFIG_HOME or ~/.config
	RexConfig  string     // the file Rex loads: $REX_CONFIG or <ConfigHome>/rex/init.lua
	StateHome  string     // $XDG_STATE_HOME or ~/.local/state, home of the legacy tools' state
	Dirs       state.Dirs // rexkit's own directories
}

// PathsFrom resolves the paths the way Rex and rexkit do.
func PathsFrom(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")
	if home == "" {
		return Paths{}, errors.New("$HOME is not set")
	}
	xdg := func(env, fallback string) string {
		if v := getenv(env); filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(home, fallback)
	}
	dirs, err := state.DirsFrom(getenv)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Home:       home,
		ConfigHome: xdg("XDG_CONFIG_HOME", ".config"),
		StateHome:  xdg("XDG_STATE_HOME", ".local/state"),
		Dirs:       dirs,
	}
	p.RexConfig = filepath.Join(p.ConfigHome, "rex", "init.lua")
	if v := getenv("REX_CONFIG"); filepath.IsAbs(v) {
		p.RexConfig = v
	}
	return p, nil
}

// Backups holds copies of every init.lua the installer replaced.
func (p Paths) Backups() string { return filepath.Join(filepath.Dir(p.RexConfig), "backups") }

// Record is the installation record.
func (p Paths) Record() string { return filepath.Join(p.Dirs.Config, "install.json") }

// lock serializes installer runs.
func (p Paths) lock() string { return filepath.Join(p.Dirs.Config, "install.lock") }

// LegacyRecord is the old rex-goto installer's record.
func (p Paths) LegacyRecord() string {
	return filepath.Join(filepath.Dir(p.RexConfig), "rex-goto-install.json")
}

// Tilde shortens a path under the home directory for display.
func (p Paths) Tilde(path string) string {
	if path == p.Home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, p.Home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}
