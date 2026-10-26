package files

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/sanitize"
)

// kind is what an entry is, after following a symlink.
type kind uint8

const (
	kindFile    kind = iota
	kindDir          // a folder, or a link to one
	kindSpecial      // FIFO, socket or device: never opened
	kindBroken       // a symlink whose target is missing
)

// entry is one row-worthy filesystem object. Paths are data: they are kept
// exactly as the filesystem returned them and sanitized only for display.
type entry struct {
	path    string // absolute
	rel     string // relative to the folder being listed, or a display path
	label   string // rel, sanitized: what is shown and searched
	width   int    // display width of label
	kind    kind
	link    bool        // a symlink; kind describes its target
	mode    fs.FileMode // of the target (of the link itself when broken)
	size    int64
	modTime time.Time
	id      fileID // of the path itself (the link, for a symlink)
}

// fileID identifies a file whatever its name is spelled like.
type fileID struct{ dev, ino uint64 }

func idOf(info fs.FileInfo) (fileID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func (e entry) name() string { return filepath.Base(e.path) }
func (e entry) isDir() bool  { return e.kind == kindDir }

// isRegular reports a regular file (possibly through a link): the only kind
// of file rexkit ever opens.
func (e entry) isRegular() bool { return e.kind == kindFile }

// statEntry describes path without opening it. A symlink is followed for its
// target's kind; a dangling one becomes kindBroken rather than an error.
func statEntry(path, rel string) (entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return entry{}, err
	}
	label := sanitize.Name(rel)
	e := entry{path: path, rel: rel, label: label, width: labelWidth(label)}
	e.id, _ = idOf(info)
	if info.Mode()&fs.ModeSymlink != 0 {
		e.link = true
		target, err := os.Stat(path)
		if err != nil {
			e.kind, e.mode, e.modTime = kindBroken, info.Mode(), info.ModTime()
			return e, nil
		}
		info = target
	}
	e.mode, e.size, e.modTime = info.Mode(), info.Size(), info.ModTime()
	switch {
	case info.IsDir():
		e.kind = kindDir
	case info.Mode().IsRegular():
		e.kind = kindFile
	default:
		e.kind = kindSpecial
	}
	return e, nil
}

// specialWord names a file that is neither regular nor a folder.
func specialWord(m fs.FileMode) string {
	switch {
	case m&fs.ModeNamedPipe != 0:
		return "fifo"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeDevice != 0:
		return "device"
	}
	return "special"
}

// sortFolder orders a folder listing: folders first, then by name, ignoring
// case.
func sortFolder(entries []entry) {
	slices.SortStableFunc(entries, func(a, b entry) int {
		if a.isDir() != b.isDir() {
			if a.isDir() {
				return -1
			}
			return 1
		}
		return compareNames(a.rel, b.rel)
	})
}

// sortTree orders a recursive listing by path, a folder before its contents.
func sortTree(entries []entry) {
	slices.SortFunc(entries, func(a, b entry) int {
		as, bs := strings.Split(a.rel, "/"), strings.Split(b.rel, "/")
		for i := range min(len(as), len(bs)) {
			if c := compareNames(as[i], bs[i]); c != 0 {
				return c
			}
		}
		return len(as) - len(bs)
	})
}

func compareNames(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

// isHiddenPath reports whether any component of a slash-separated relative
// path is a dotfile.
func isHiddenPath(rel string) bool {
	for part := range strings.SplitSeq(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// humanSize renders a byte count in at most 8 cells: "512 B", "1.1 KB".
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	v, suffix := float64(n)/unit, "KB"
	for _, s := range []string{"MB", "GB", "TB"} {
		if v < unit {
			break
		}
		v, suffix = v/unit, s
	}
	if v >= 100 {
		return strconv.FormatFloat(v, 'f', 0, 64) + " " + suffix
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + suffix
}

// itemCount renders a folder's size: "empty", "1 item", "12 items", "1.2k items".
func itemCount(n int, capped bool) string {
	switch {
	case n == 0:
		return "empty"
	case capped || n >= 10_000:
		return strconv.Itoa(n/1000) + "k+ items"
	case n >= 1000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k items"
	case n == 1:
		return "1 item"
	}
	return strconv.Itoa(n) + " items"
}

// labelWidth is the display width of a label, cheaply for ASCII.
func labelWidth(s string) int {
	if isASCII(s) {
		return len(s)
	}
	return ansi.StringWidth(s)
}
