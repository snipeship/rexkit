package state

import (
	"os"
	"path/filepath"
	"time"
)

// SnapshotCache is the cache entry holding the last Rex inventory, used to
// paint the first frame before fresh data arrives.
const SnapshotCache = "snapshot"

// WriteCache stores v under name in the cache directory.
func (d Dirs) WriteCache(name string, v any) error {
	return WriteJSON(d.cachePath(name), v)
}

// ReadCache loads the entry name into v and reports when it was written.
// A missing entry returns an error satisfying errors.Is(err, fs.ErrNotExist).
func (d Dirs) ReadCache(name string, v any) (time.Time, error) {
	path := d.cachePath(name)
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), ReadJSON(path, v)
}

func (d Dirs) cachePath(name string) string {
	return filepath.Join(d.Cache, name+".json")
}
