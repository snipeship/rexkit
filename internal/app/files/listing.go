package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// listing is the content of a folder, or of a whole tree in recursive mode.
type listing struct {
	root       string
	entries    []entry
	recursive  bool
	limited    bool  // the recursive scan stopped at maxEntries, maxDepth or its deadline
	unreadable int   // entries that could not be described
	viaGit     bool  // the tree came from git ls-files
	err        error // why the folder could not be listed (shown in the list)
}

// readFolder lists one folder. Hidden entries are skipped unless hidden.
func readFolder(ctx context.Context, root string, hidden bool) (listing, error) {
	dirents, err := readDir(root)
	if err != nil {
		return listing{root: root}, err
	}
	l := listing{root: root, entries: make([]entry, 0, len(dirents))}
	for _, d := range dirents {
		if err := ctx.Err(); err != nil {
			return l, err
		}
		if !hidden && strings.HasPrefix(d.Name(), ".") {
			continue
		}
		e, err := statEntry(filepath.Join(root, d.Name()), d.Name())
		if err != nil {
			l.unreadable++
			continue
		}
		l.entries = append(l.entries, e)
	}
	sortFolder(l.entries)
	return l, nil
}

// errNotFolder is returned when the path to list is not a folder.
var errNotFolder = errors.New("not a folder")

// checkFolder reports whether path is a folder that can be listed.
func checkFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotFolder
	}
	return nil
}

// folderOf returns the folder a path stands for: itself for a folder, its
// parent for anything else.
func folderOf(e entry) string {
	if e.isDir() {
		return e.path
	}
	return filepath.Dir(e.path)
}

// fsErrorText turns a filesystem error into a short phrase without the path
// (the UI shows the path itself, sanitized).
func fsErrorText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "it no longer exists"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, errNotFolder):
		return "not a folder"
	case errors.Is(err, syscall.ENOTDIR):
		return "no longer a folder" // it was one when it was listed
	}
	return err.Error()
}
