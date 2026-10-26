package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// scratchSession is one session's Scratch state. rex-zellij's state.json and
// rexkit's workspace.json (lua/workspace.lua) share this shape; rex-zellij's
// extra "tools" ids pointed at its Python helpers and are dropped. Parked
// return addresses are carried over as they are.
type scratchSession struct {
	Scratch  string                     `json:"scratch,omitempty"`
	Previous string                     `json:"previous,omitempty"`
	Parked   map[string]json.RawMessage `json:"parked"`
}

func (e *Env) workspacePath() string { return filepath.Join(e.Paths.Dirs.State, "workspace.json") }

func (e *Env) legacyScratchPath() string {
	return filepath.Join(e.Paths.StateHome, "rex-zellij", "state.json")
}

// migrateScratch plans carrying rex-zellij's Scratch state over to rexkit,
// unless rexkit already has its own, and returns the workspace.json to write.
func (e *Env) migrateScratch() (*Migration, []byte, error) {
	src, dest := e.legacyScratchPath(), e.workspacePath()
	if _, err := os.Stat(dest); !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var sessions map[string]scratchSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, nil, fmt.Errorf("reading rex-zellij's Scratch state %s: %w", e.Paths.Tilde(src), err)
	}
	for id, s := range sessions {
		if s.Parked == nil {
			s.Parked = map[string]json.RawMessage{}
		}
		sessions[id] = s
	}
	out, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	m := &Migration{From: "scratch", Files: []string{src}, Wrote: dest, At: e.Now().UTC()}
	return m, append(out, '\n'), nil
}
