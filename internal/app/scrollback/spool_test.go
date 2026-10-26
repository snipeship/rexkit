package scrollback

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpoolIsPrivateAndRemoved(t *testing.T) {
	parent := t.TempDir()
	sp, err := newSpool(parent)
	if err != nil {
		t.Fatal(err)
	}
	path, err := sp.write("scrollback.txt", []byte("secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != sp.dir || filepath.Dir(sp.dir) != parent {
		t.Errorf("file %s, spool %s, parent %s", path, sp.dir, parent)
	}
	for p, want := range map[string]os.FileMode{sp.dir: 0o700, path: 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", p, got, want)
		}
	}
	if err := sp.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sp.dir); !os.IsNotExist(err) {
		t.Errorf("spool still exists: %v", err)
	}
}
