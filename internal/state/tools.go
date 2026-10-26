package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ToolDirMaxAge is how long an unclaimed tool directory may live. A claimed
// one lives exactly as long as its owner block, however long that is.
const ToolDirMaxAge = 24 * time.Hour

// Owner identifies the block a tool instance runs in.
type Owner struct {
	SessionID string `json:"session_id"`
	BlockID   string `json:"block_id"`
}

const ownerFile = "owner.json"

// ToolsDir holds one private directory per running tool instance.
func (d Dirs) ToolsDir() string { return filepath.Join(d.State, "tools") }

// NewToolDir creates a fresh private (0700) directory for one tool instance.
// The Lua layer makes the real ones (with mktemp); this is for tests.
func (d Dirs) NewToolDir() (string, error) {
	root := d.ToolsDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "")
}

// Claim records which block owns a tool directory, so GC can remove it as
// soon as that block is gone.
func Claim(dir string, o Owner) error {
	return WriteJSON(filepath.Join(dir, ownerFile), o)
}

// GCTools removes stale tool directories: a claimed one when its owner is no
// longer alive (whatever its age: a float left open for days keeps its
// state), an unclaimed one when it is older than maxAge. alive may be nil;
// then every directory is judged by age. It returns the directories it
// removed.
func (d Dirs) GCTools(now time.Time, maxAge time.Duration, alive func(Owner) bool) ([]string, error) {
	entries, err := os.ReadDir(d.ToolsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	var errs []error
	for _, e := range entries {
		if !e.IsDir() { // never follow symlinks out of the tools dir
			continue
		}
		dir := filepath.Join(d.ToolsDir(), e.Name())
		if !stale(dir, e, now, maxAge, alive) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, dir)
	}
	return removed, errors.Join(errs...)
}

func stale(dir string, e fs.DirEntry, now time.Time, maxAge time.Duration, alive func(Owner) bool) bool {
	var o Owner
	if alive != nil && ReadJSON(filepath.Join(dir, ownerFile), &o) == nil && o.BlockID != "" {
		return !alive(o) // claimed: the owner decides
	}
	info, err := e.Info()
	return err == nil && now.Sub(info.ModTime()) > maxAge
}
