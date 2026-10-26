// Package state owns rexkit's files: XDG directories, atomic JSON writes,
// per-tool-instance directories, the snapshot cache and the visit history.
package state

import (
	"errors"
	"os"
	"path/filepath"
)

// Dirs are rexkit's directories.
type Dirs struct {
	State  string // $XDG_STATE_HOME/rexkit, default ~/.local/state/rexkit
	Cache  string // $XDG_CACHE_HOME/rexkit, default ~/.cache/rexkit
	Config string // $XDG_CONFIG_HOME/rexkit, default ~/.config/rexkit
}

// Default returns the directories for the current environment.
func Default() (Dirs, error) { return DirsFrom(os.Getenv) }

// DirsFrom resolves the directories from an environment lookup.
func DirsFrom(getenv func(string) string) (Dirs, error) {
	home := getenv("HOME")
	dir := func(env, fallback string) (string, error) {
		if v := getenv(env); filepath.IsAbs(v) {
			return filepath.Join(v, "rexkit"), nil
		}
		if home == "" {
			return "", errors.New("state: $HOME is not set")
		}
		return filepath.Join(home, fallback, "rexkit"), nil
	}
	var d Dirs
	var err error
	if d.State, err = dir("XDG_STATE_HOME", ".local/state"); err != nil {
		return Dirs{}, err
	}
	if d.Cache, err = dir("XDG_CACHE_HOME", ".cache"); err != nil {
		return Dirs{}, err
	}
	if d.Config, err = dir("XDG_CONFIG_HOME", ".config"); err != nil {
		return Dirs{}, err
	}
	return d, nil
}

// Settings is the user's settings file, read by the Lua layer.
func (d Dirs) Settings() string { return filepath.Join(d.Config, "settings.lua") }
