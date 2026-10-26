package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"rexkit/internal/state"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// SnapStates are the states Snap can show.
var SnapStates = []string{
	"default", "search", "preview", "path", "help",
	"toggles", "image", "nomatch", "empty", "error", "refused", "busy",
}

// snapNow is the clock of snapshots, so ages never change.
var snapNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// snapTree is the sample project Files browses in snapshots, by path and
// age; a path ending in / is a folder.
var snapTree = []struct {
	path    string
	ago     time.Duration
	content string
}{
	{"cmd/", 2 * time.Hour, ""},
	{"cmd/rexkit/", 2 * time.Hour, ""},
	{"cmd/rexkit/main.go", 2 * time.Hour, "package main\n"},
	{"docs/", 26 * time.Hour, ""},
	{"docs/DESIGN.md", 26 * time.Hour, "# rexkit — design spec\n"},
	{"internal/", 3 * time.Hour, ""},
	{"internal/ui/", 3 * time.Hour, ""},
	{"internal/ui/theme.go", 3 * time.Hour, "package ui\n"},
	{"lua/", 5 * 24 * time.Hour, ""},
	{"lua/init.lua", 5 * 24 * time.Hour, "return {}\n"},
	{".github/", 40 * 24 * time.Hour, ""},
	{".gitignore", 40 * 24 * time.Hour, "/bin/\n"},
	{"go.mod", 9 * 24 * time.Hour, "module rexkit\n\ngo 1.27.0\n"},
	{"go.sum", 9 * 24 * time.Hour, ""},
	{"main.go", 12 * time.Minute, snapCode},
	{"Makefile", 30 * 24 * time.Hour, "build:\n\tgo build ./cmd/rexkit\n"},
	{"README.md", 4 * time.Hour, "# rexkit\n\nKeyboard-driven workspace tools for Rex.\n"},
	{"screenshot.png", 3 * 24 * time.Hour, snapPicture},
}

const snapCode = `// Command snapdemo shows what Files previews: a Go file, highlighted.
package main

import (
	"fmt"
	"os"
	"time"
)

// greeting is what main prints, once per name.
const greeting = "Hello, %s! It is %s.\n"

func main() {
	names := os.Args[1:]
	if len(names) == 0 {
		names = []string{"world"}
	}
	now := time.Now().Format(time.Kitchen)
	for i, name := range names {
		if i > 2 {
			fmt.Fprintln(os.Stderr, "…and", len(names)-i, "more")
			break
		}
		fmt.Printf(greeting, name, now)
	}
}
`

// snapPicture marks the tree's image: writeSnapTree draws one.
const snapPicture = "\x00png"

// snapStatus is the git status of the sample project.
var snapStatus = strings.Join([]string{
	"# branch.oid 1234567890abcdef", "# branch.head main", "# branch.ab +2 -0",
	"1 .M N... 100644 100644 100644 abc abc README.md",
	"1 .M N... 100644 100644 100644 abc abc main.go",
	"1 A. N... 000000 100644 100644 000 abc cmd/rexkit/main.go",
	"? docs/", "",
}, "\x00")

// Snap returns Files browsing a sample project in a named state, for
// `rexkit snap`. The project lives in a temporary folder that the scene's
// Cleanup removes.
func Snap(dark bool, name string) (snap.Scene, error) {
	if !slices.Contains(SnapStates, name) {
		return snap.Scene{}, fmt.Errorf("files has no state %q (try %v)", name, SnapStates)
	}
	home, err := os.MkdirTemp("", "rexkit-snap-")
	if err != nil {
		return snap.Scene{}, err
	}
	root := filepath.Join(home, "Coding", "rexkit")
	website := filepath.Join(home, "Coding", "website")
	private := filepath.Join(home, "Coding", "private")
	cleanup := func() {
		_ = os.Chmod(private, 0o755)
		_ = os.RemoveAll(home)
	}
	if err := writeSnapTree(root, website); err != nil {
		cleanup()
		return snap.Scene{}, err
	}
	if err := os.Mkdir(private, 0o000); err != nil {
		cleanup()
		return snap.Scene{}, err
	}
	none := func(context.Context, string, string, string, ...string) ([]byte, error) {
		return nil, errors.New("not in snapshots")
	}
	now := func() time.Time { return snapNow }
	dirs := state.Dirs{State: filepath.Join(home, "state"), Cache: filepath.Join(home, "cache"), Config: filepath.Join(home, "config")}
	cfg := config{
		dir: root, home: home, demo: true, dirs: dirs, run: none, now: now, imageBase: 1000,
		launcher: launcher{dirs: dirs, home: home, run: none, now: now, editor: []string{"vi"}},
	}
	m := newModel(cfg)
	m.scheme = ui.FixedScheme(dark)
	m.theme = m.scheme.Theme()
	loaded := func(d *snap.Driver) {
		project, err := statEntry(website, "~/Coding/website")
		if err == nil {
			d.Send(projectsMsg{[]entry{project}})
		}
		d.Send(gitMsg{dir: root, status: parseStatus([]byte(snapStatus)), ok: true})
	}
	s := snap.Scene{Model: m, Script: loaded, Cleanup: cleanup}
	switch name {
	case "search":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Key("ctrl+f")
			d.Type("main")
		}
	case "preview":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			if i := m.indexOf(filepath.Join(root, "main.go")); i >= 0 {
				m.here = false
				m.list.Select(i)
				d.Run(m.selectionChanged())
			}
		}
	case "path":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Key("ctrl+l")
			d.Type("do")
		}
	case "help":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Key("?")
		}
	case "toggles":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Key("ctrl+f", "alt+.")
		}
	case "image":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			if i := m.indexOf(filepath.Join(root, "screenshot.png")); i >= 0 {
				m.here = false
				m.list.Select(i)
				d.Run(m.selectionChanged())
			}
		}
	case "nomatch":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Type("zqxj")
		}
	case "empty", "error":
		dir := website
		if name == "error" {
			dir = private
		}
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Run(m.navigate(dir, ""))
		}
	case "refused":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			d.Key("ctrl+l")
			d.Type("zz")
		}
	case "busy":
		s.Script = func(d *snap.Driver) {
			loaded(d)
			// A verb that has been opening a terminal for a while.
			m.busy, m.busySince = true, snapNow.Add(-time.Second)
		}
	}
	return s, nil
}

// writeSnapTree creates the sample project at root, and an empty project
// at other (a zoxide favourite), with fixed times.
func writeSnapTree(root, other string) error {
	if err := os.MkdirAll(other, 0o755); err != nil {
		return err
	}
	for _, f := range snapTree {
		p := filepath.Join(root, f.path)
		var err error
		switch {
		case strings.HasSuffix(f.path, "/"):
			err = os.MkdirAll(p, 0o755)
		case f.content == snapPicture:
			err = writeSnapPicture(p)
		default:
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				err = os.WriteFile(p, []byte(f.content), 0o644)
			}
		}
		if err != nil {
			return err
		}
	}
	// Times last, folders after what is in them, so writing does not touch them.
	for i := len(snapTree) - 1; i >= 0; i-- {
		f := snapTree[i]
		at := snapNow.Add(-f.ago)
		if err := os.Chtimes(filepath.Join(root, f.path), at, at); err != nil {
			return err
		}
	}
	at := snapNow.Add(-26 * time.Hour)
	for _, p := range []string{root, other} {
		if err := os.Chtimes(p, at, at); err != nil {
			return err
		}
	}
	return nil
}

// writeSnapPicture draws the sample screenshot: a 640×400 gradient.
func writeSnapPicture(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := range 400 {
		for x := range 640 {
			img.Set(x, y, color.RGBA{uint8(40 + x/5), uint8(90 + y/4), 200, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}
