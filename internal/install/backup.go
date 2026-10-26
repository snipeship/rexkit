package install

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// backup keeps a copy of data in the backups directory, once per distinct
// content: an identical earlier copy is reused. created reports whether a
// new file was written (and so must be removed on rollback).
func (e *Env) backup(data []byte) (path string, created bool, err error) {
	dir := e.Paths.Backups()
	existing, _ := filepath.Glob(filepath.Join(dir, "init.*.lua"))
	for _, p := range existing {
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
			return p, false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	stamp := e.Now().Format("20060102-150405")
	for n := 1; ; n++ {
		name := "init." + stamp + ".lua"
		if n > 1 {
			name = fmt.Sprintf("init.%s-%d.lua", stamp, n)
		}
		path = filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		_, werr := f.Write(data)
		if err := errors.Join(werr, f.Sync(), f.Close()); err != nil {
			os.Remove(path)
			return "", false, err
		}
		return path, true, nil
	}
}

// listBackups returns the backups install made, newest first. Their names
// carry the time they were made: init.20261026-120000.lua, then -2, -3, …
// for more in the same second.
func (e *Env) listBackups() []string {
	files, _ := filepath.Glob(filepath.Join(e.Paths.Backups(), "init.*.lua"))
	const stampLen = len("20060102-150405")
	key := func(path string) (string, int) {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "init."), ".lua")
		if len(name) < stampLen {
			return name, 1
		}
		seq, err := strconv.Atoi(strings.TrimPrefix(name[stampLen:], "-"))
		if err != nil {
			seq = 1
		}
		return name[:stampLen], seq
	}
	slices.SortFunc(files, func(a, b string) int {
		sa, na := key(a)
		sb, nb := key(b)
		return cmp.Or(strings.Compare(sb, sa), nb-na)
	})
	return files
}
