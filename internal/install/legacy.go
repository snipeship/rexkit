package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// legacy is what the two tools rexkit replaces left behind, found by
// following the config chain from Rex's init.lua.
type legacy struct {
	gotoRecord string   // rex-goto-install.json, when the rex-goto wrapper is installed
	gotoRoot   string   // the rex-goto checkout
	zellijRoot string   // the rex-zellij checkout its bootstrap loads
	originals  []string // configs from before either tool, newest first
}

func (l legacy) found() bool { return l.gotoRoot != "" || l.zellijRoot != "" }

// findLegacy follows a rex-goto wrapper to the config it wraps, and a
// rex-zellij bootstrap to its checkout.
func (e *Env) findLegacy(c current) legacy {
	var l legacy
	data := c.data
	if c.kind == configRexGoto {
		var rec struct{ Backup, Root string }
		if raw, err := os.ReadFile(e.Paths.LegacyRecord()); err == nil && json.Unmarshal(raw, &rec) == nil {
			l.gotoRecord = e.Paths.LegacyRecord()
		}
		if paths := dofilePaths(string(data)); len(paths) == 2 && rec.Backup == "" {
			rec.Backup, rec.Root = paths[0], filepath.Dir(paths[1])
		}
		l.gotoRoot = rec.Root
		data, _ = os.ReadFile(rec.Backup)
		if rec.Backup != "" && !bytes.HasPrefix(data, []byte(rexZellijMarker)) {
			l.originals = append(l.originals, rec.Backup)
		}
	}
	if bytes.HasPrefix(data, []byte(rexZellijMarker)) {
		if paths := dofilePaths(string(data)); len(paths) == 1 {
			l.zellijRoot = filepath.Dir(paths[0])
		}
		// rex-zellij copied the whole config directory before installing.
		dirs, _ := filepath.Glob(filepath.Join(e.Paths.ConfigHome, "rex-zellij-backup-*", "init.lua"))
		slices.Reverse(dirs)
		l.originals = append(l.originals, dirs...)
	}
	return l
}

// migrations describes what an install replaces, for the record.
func (l legacy) migrations() []Migration {
	var out []Migration
	if l.gotoRoot != "" {
		out = append(out, Migration{From: "rex-goto", Root: l.gotoRoot, Record: l.gotoRecord})
	}
	if l.zellijRoot != "" {
		out = append(out, Migration{From: "rex-zellij", Root: l.zellijRoot})
	}
	return out
}

// legacyState lists the state directories the old tools left, which the
// user may delete once they are happy with rexkit.
func (e *Env) legacyState() []string {
	var dirs []string
	for _, name := range []string{"rex-goto", "rex-zellij"} {
		dir := filepath.Join(e.Paths.StateHome, name)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
