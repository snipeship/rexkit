package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"rexkit/internal/state"
)

// undo takes back the writes of one run, newest first. Each write is
// recorded the moment it is made, together with what the path held just
// before, so a file the run never wrote is never touched, and a file that
// changed again after the run wrote it (a park in workspace.json, a saved
// settings edit) is left as it is and reported.
type undo struct {
	tilde   func(string) string // shows a path to the user
	writes  []undoWrite
	created []string // backups the run created: always removed
}

type undoWrite struct {
	path          string
	before, after fileState
}

func (e *Env) newUndo() *undo { return &undo{tilde: e.Paths.Tilde} }

// fileState is a path as it is on disk: absent, a symlink, or a regular
// file with its content and permissions.
type fileState struct {
	exists bool
	link   string      // the raw target, when the path is a symlink
	data   []byte      // a regular file's content
	mode   fs.FileMode // a regular file's permission bits
}

// statFile reads path without following a symlink.
func statFile(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return fileState{exists: true, link: target}, err
	}
	data, err := os.ReadFile(path)
	return fileState{exists: true, data: data, mode: info.Mode().Perm()}, err
}

// same compares what two states hold; permissions alone are no change.
func (s fileState) same(o fileState) bool {
	return s.exists == o.exists && s.link == o.link && bytes.Equal(s.data, o.data)
}

// put makes path hold s.
func (s fileState) put(path string) error {
	switch {
	case !s.exists:
		return removeIfExists(path)
	case s.link != "":
		return replaceWithSymlink(path, s.link)
	default:
		return writeFileMode(path, s.data, s.mode)
	}
}

// change records what path holds, makes it hold next, and remembers both.
func (u *undo) change(path string, next fileState) error {
	before, err := statFile(path)
	if err != nil {
		return err
	}
	if err := next.put(path); err != nil {
		return err
	}
	u.writes = append(u.writes, undoWrite{path: path, before: before, after: next})
	return nil
}

// write replaces path with data. mode 0 keeps the private default (0600).
func (u *undo) write(path string, data []byte, mode fs.FileMode) error {
	return u.change(path, fileState{exists: true, data: data, mode: mode})
}

// writeJSON replaces path with v as indented JSON.
func (u *undo) writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return u.write(path, append(data, '\n'), 0)
}

// remove adds a new file that undo deletes.
func (u *undo) remove(path string) { u.created = append(u.created, path) }

func (u *undo) run() error {
	var errs []error
	for i := len(u.writes) - 1; i >= 0; i-- {
		w := u.writes[i]
		now, err := statFile(w.path)
		switch {
		case err != nil:
			errs = append(errs, err)
		case !now.same(w.after):
			errs = append(errs, fmt.Errorf("%s changed after rexkit wrote it, so it was left as it is", u.tilde(w.path)))
		default:
			errs = append(errs, w.before.put(w.path))
		}
	}
	for _, path := range u.created {
		errs = append(errs, removeIfExists(path))
	}
	return errors.Join(errs...)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// writeFileMode writes path atomically, then gives it mode (0 keeps 0600).
func writeFileMode(path string, data []byte, mode fs.FileMode) error {
	if err := state.WriteFile(path, data); err != nil {
		return err
	}
	if mode != 0 && mode != 0o600 {
		return os.Chmod(path, mode)
	}
	return nil
}

// replaceWithSymlink atomically makes path a symlink to target: the link is
// made under a temporary name beside path and renamed over it.
func replaceWithSymlink(path, target string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for {
		f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.link")
		if err != nil {
			return err
		}
		tmp := f.Name()
		f.Close()
		os.Remove(tmp)
		if err := os.Symlink(target, tmp); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	}
}
