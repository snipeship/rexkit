package install

import (
	"errors"
	"os"
	"syscall"
)

// lock keeps two installer runs from interleaving their writes.
func (e *Env) lock() (unlock func(), err error) {
	if err := os.MkdirAll(e.Paths.Dirs.Config, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(e.Paths.lock(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another rexkit install or uninstall is running")
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
