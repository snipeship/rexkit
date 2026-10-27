package install

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Marker starts every init.lua that rexkit wrote.
const Marker = "-- Managed by rexkit"

// Markers of the installers rexkit replaces.
const (
	rexGotoMarker   = "-- Managed by rex-goto"
	rexZellijMarker = "-- Managed by the local rex-zellij repository."
)

// Loader is the init.lua rexkit installs: a marker, a note for humans, and
// one dofile of the Lua layer of rexkit's tree (a clone or a packaged one). settings is the settings file as
// shown to the user. Only the marker and the dofile identify the file as
// rexkit's (managedRoot), so the note may change between versions.
func Loader(root, settings string) []byte {
	return []byte(Marker + ". `rexkit install` rewrites this file and `rexkit uninstall`\n" +
		"-- restores the one it replaced. Your settings belong in " + luaComment(settings) + ".\n" +
		"return dofile(" + longBracket(filepath.Join(root, "lua", "init.lua")) + ")\n")
}

// configKind is what currently sits in Rex's init.lua.
type configKind int

const (
	configAbsent    configKind = iota
	configManaged              // a loader rexkit wrote: the marker, comments and one dofile of <root>/lua/init.lua
	configEdited               // rexkit's marker, but code was added by hand
	configRexGoto              // the old rex-goto wrapper
	configRexZellij            // the old rex-zellij bootstrap
	configUser                 // anything else: the user's own config
)

// current is Rex's init.lua as found.
type current struct {
	kind configKind
	data []byte      // the content (a symlink's target's content)
	root string      // the rexkit tree (a clone or a packaged one) a managed loader loads
	link string      // the raw target, when init.lua is a symlink
	mode fs.FileMode // the permissions of the file (or of a symlink's target)
}

func (c current) exists() bool { return c.kind != configAbsent }

// readCurrent reads and classifies Rex's init.lua.
func (e *Env) readCurrent() (current, error) {
	path := e.Paths.RexConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return current{kind: configAbsent}, nil
	}
	if err != nil {
		return current{}, err
	}
	c := current{kind: configUser, data: data}
	if info, err := os.Stat(path); err == nil {
		c.mode = info.Mode().Perm()
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if c.link, err = os.Readlink(path); err != nil {
			return current{}, err
		}
	}
	switch {
	case bytes.HasPrefix(data, []byte(Marker)):
		c.kind = configEdited
		if root, ok := managedRoot(string(data)); ok {
			c.kind, c.root = configManaged, root
		}
	case bytes.HasPrefix(data, []byte(rexGotoMarker)):
		c.kind = configRexGoto
	case bytes.HasPrefix(data, []byte(rexZellijMarker)):
		c.kind = configRexZellij
	}
	return c, nil
}

// managedRoot recognizes a loader rexkit wrote by its structure, not its
// bytes: the marker, then nothing but comments and a single
// `return dofile(<string>)` of an absolute <root>/lua/init.lua. Any other
// code makes the file hand-edited. The comments' wording and the settings
// path they name may differ from what this version writes.
func managedRoot(src string) (string, bool) {
	if !strings.HasPrefix(src, Marker) {
		return "", false
	}
	p := &luaParser{src: src}
	if !p.word("return") || !p.word("dofile") || !p.symbol("(") {
		return "", false
	}
	v, err := p.value(nil)
	path, isString := v.(string)
	if err != nil || !isString || !p.symbol(")") {
		return "", false
	}
	p.symbol(";")
	if p.space(); p.pos != len(p.src) {
		return "", false
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || filepath.Base(path) != "init.lua" || filepath.Base(filepath.Dir(path)) != "lua" {
		return "", false
	}
	return filepath.Dir(filepath.Dir(path)), true
}

func (e *Env) loader(root string) []byte {
	return Loader(root, e.Paths.Tilde(e.Paths.Dirs.Settings()))
}

// dofilePaths returns the literal string arguments of every dofile(...) in a
// Lua source, in order.
func dofilePaths(src string) []string {
	var paths []string
	for rest := src; ; {
		i := strings.Index(rest, "dofile(")
		if i < 0 {
			return paths
		}
		p := &luaParser{src: rest, pos: i + len("dofile(")}
		if v, err := p.value(nil); err == nil {
			if s, ok := v.(string); ok {
				paths = append(paths, s)
			}
		}
		rest = rest[max(p.pos, i+1):]
	}
}
