package scrollback

import (
	"os"
	"path/filepath"
)

// spool is the viewer's private temp dir (mode 0700): the capture and the
// Neovim profile live there while the viewer runs, and nowhere after.
type spool struct{ dir string }

// newSpool creates a spool under parent (os.TempDir() when empty).
func newSpool(parent string) (*spool, error) {
	dir, err := os.MkdirTemp(parent, "rexkit-scrollback-")
	if err != nil {
		return nil, err
	}
	return &spool{dir: dir}, nil
}

// write stores data as name (mode 0600) and returns its path.
func (s *spool) write(name string, data []byte) (string, error) {
	path := filepath.Join(s.dir, name)
	return path, os.WriteFile(path, data, 0o600)
}

// remove deletes the spool and everything in it.
func (s *spool) remove() error { return os.RemoveAll(s.dir) }
