package attend

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"rexkit/internal/state"
)

// attendDir holds the watcher's lock, its pidfile and its log, under the
// state dir.
const attendDir = "attend"

// Waits when another watcher holds the lock of the same server run.
const (
	// lockWait is how long a new watcher lets an older one go on its own
	// (it sees the config reload too).
	lockWait = 5 * time.Second
	// termWait is how long it then waits after asking it to end.
	termWait = 3 * time.Second
)

// Info is what a running watcher says about itself, in
// <state>/attend/<server run>.json.
type Info struct {
	PID     int       `json:"pid"`
	Token   string    `json:"token"`
	Binary  string    `json:"binary,omitempty"`
	Started time.Time `json:"started"`
}

// Running lists the watchers that hold their lock, whatever server run
// they watch (for rexkit doctor).
func Running(d state.Dirs) []Info {
	dir := filepath.Join(d.State, attendDir)
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []Info
	for _, path := range matches {
		var info Info
		if state.ReadJSON(path, &info) != nil || info.PID <= 0 {
			continue
		}
		if held(strings.TrimSuffix(path, ".json") + ".lock") {
			out = append(out, info)
		}
	}
	return out
}

// held reports whether some process holds the lock file.
func held(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// claim takes the lock of a server run for the watcher with token, and
// writes its pidfile; release gives both up. A watcher of an older config
// load that still holds the lock is given lockWait to end on its own, then
// asked to (SIGTERM). Files of server runs nobody watches any more are
// removed.
func claim(ctx context.Context, d state.Dirs, instance, token string, logger *log.Logger) (release func(), err error) {
	dir := filepath.Join(d.State, attendDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sweep(dir, instance)
	name := safeName(instance)
	lockPath, infoPath := filepath.Join(dir, name+".lock"), filepath.Join(dir, name+".json")
	lock := &lockFile{path: lockPath}
	if !lock.try() {
		var other Info
		_ = state.ReadJSON(infoPath, &other)
		if other.Token == token {
			lock.close()
			return nil, errSuperseded
		}
		got := wait(ctx, lock.try, lockWait)
		if !got {
			got = askToEnd(ctx, lock.try, infoPath, lockPath, token, logger)
		}
		if !got {
			lock.close()
			return nil, errors.New("another watcher holds " + lockPath)
		}
	}
	exe, _ := os.Executable()
	info := Info{PID: os.Getpid(), Token: token, Binary: exe, Started: time.Now()}
	if err := state.WriteJSON(infoPath, info); err != nil {
		lock.close()
		return nil, err
	}
	return func() {
		var now Info
		if state.ReadJSON(infoPath, &now) == nil && now.PID == info.PID {
			os.Remove(infoPath)
		}
		lock.close()
	}, nil
}

// lockFile is a lock file a watcher takes.
type lockFile struct {
	path string
	f    *os.File
}

// try takes the lock without waiting. The file it locks must still be the
// one at path: another server's watcher may have swept it away between the
// open and the lock (sweep), and then the next try opens the new one.
func (l *lockFile) try() bool {
	if l.f == nil {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return false
		}
		l.f = f
	}
	if syscall.Flock(int(l.f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false
	}
	locked, lerr := l.f.Stat()
	now, nerr := os.Stat(l.path)
	if lerr == nil && nerr == nil && os.SameFile(locked, now) {
		return true
	}
	l.close()
	return false
}

func (l *lockFile) close() {
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

// askToEnd asks the watcher that still holds the lock to end (SIGTERM), then
// waits termWait for it to go. Its pidfile is read again first: another
// watcher may hold the lock by now, or the pidfile be one a watcher that
// is gone left behind, whose PID may be some other program's by now. Only
// the holder of the lock, of another config load, is asked.
func askToEnd(ctx context.Context, try func() bool, infoPath, lockPath, token string, logger *log.Logger) bool {
	var other Info
	if state.ReadJSON(infoPath, &other) != nil || other.PID <= 0 || other.PID == os.Getpid() ||
		other.Token == token || !held(lockPath) {
		return wait(ctx, try, termWait)
	}
	logger.Printf("asking the older watcher (pid %d) to end", other.PID)
	_ = syscall.Kill(other.PID, syscall.SIGTERM)
	return wait(ctx, try, termWait)
}

// wait retries try every 100ms for up to d.
func wait(ctx context.Context, try func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
		if try() {
			return true
		}
	}
	return false
}

// sweep removes the files of server runs other than instance that no
// watcher holds: the server restarted since.
func sweep(dir, instance string) {
	locks, _ := filepath.Glob(filepath.Join(dir, "*.lock"))
	for _, path := range locks {
		if filepath.Base(path) == safeName(instance)+".lock" || held(path) {
			continue
		}
		os.Remove(strings.TrimSuffix(path, ".lock") + ".json")
		os.Remove(path)
	}
}

// safeName makes a server run's ID a file name.
func safeName(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == os.PathSeparator || r == 0 {
			return '_'
		}
		return r
	}, id)
}
