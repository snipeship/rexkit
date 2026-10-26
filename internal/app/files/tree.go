package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Bounds of a recursive scan.
const (
	maxEntries   = 50_000
	maxDepth     = 24
	walkDeadline = 4 * time.Second
	gitDeadline  = 5 * time.Second
)

// skipDirs are listed as destinations but never descended into outside a
// git repository: they hold generated or vendored content.
var skipDirs = map[string]bool{
	"node_modules": true, ".venv": true, "venv": true, "vendor": true, ".cache": true,
	"target": true, "dist": true, "build": true, "__pycache__": true,
}

// readTree lists everything below root. Inside a git work tree it asks git
// (so .gitignore is honoured); elsewhere, and inside an ignored folder
// (node_modules/x, build/), it walks the folder with bounds.
func readTree(ctx context.Context, run runner, root string, hidden bool) (listing, error) {
	if l, ok := gitTree(ctx, run, root, hidden); ok {
		return l, nil
	}
	return walkTree(ctx, root, hidden)
}

// gitTree lists root's work tree with git: tracked and untracked but not
// ignored files, the folders they imply, root's own entries, and the
// contents of nested repositories and submodules, which git reports as one
// entry each and which are listed by their own git. Everything shares
// maxEntries and gitDeadline. ok is false outside a work tree and inside an
// ignored folder, where git lists nothing.
func gitTree(ctx context.Context, run runner, root string, hidden bool) (listing, bool) {
	gctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	files, ok := gitFiles(gctx, run, root)
	if !ok || gitIgnored(gctx, run, root) {
		return listing{}, false
	}
	l := listing{root: root, recursive: true, viaGit: true}
	set := newTreeSet(hidden)
	next := set.add("", files)
	next = append(next, set.addTopLevel(root)...)
	for len(next) > 0 && gctx.Err() == nil {
		if room := maxEntries - len(l.entries); len(next) > room {
			next, l.limited = next[:room], true
		}
		found := statTree(gctx, root, next)
		l.entries = append(l.entries, found...)
		next = nil
		for _, e := range found {
			if !set.leaves[e.rel] || !e.isDir() || e.link || len(l.entries)+len(next) >= maxEntries || gctx.Err() != nil {
				continue
			}
			// A folder git listed as a leaf is a work tree of its own.
			if nested, ok := gitFiles(gctx, run, e.path); ok {
				next = append(next, set.add(e.rel+"/", nested)...)
			}
		}
	}
	if errors.Is(gctx.Err(), context.DeadlineExceeded) {
		l.limited = true
	}
	sortTree(l.entries)
	return l, true
}

// gitFiles returns what `git ls-files` shows under root, relative to it:
// tracked files and untracked ones that are not ignored. An untracked
// nested repository comes back as one path ending in "/". ok is false when
// root is not in a work tree.
func gitFiles(ctx context.Context, run runner, root string) ([]string, bool) {
	out, err := git(ctx, run, root, "ls-files", "-co", "--exclude-standard", "-z")
	if err != nil {
		return nil, false
	}
	var rels []string
	for rel := range bytes.SplitSeq(out, []byte{0}) {
		if len(rel) > 0 {
			rels = append(rels, string(rel))
		}
	}
	return rels, true
}

// gitIgnored reports whether root itself is ignored (it is, or is inside,
// an ignored folder).
func gitIgnored(ctx context.Context, run runner, root string) bool {
	_, err := git(ctx, run, root, "check-ignore", "-q", ".")
	return err == nil
}

// treeSet collects the paths of a git listing once each: files, the
// folders they imply, and which paths git listed itself (leaves).
type treeSet struct {
	hidden bool
	seen   map[string]bool
	leaves map[string]bool
}

func newTreeSet(hidden bool) *treeSet {
	return &treeSet{hidden: hidden, seen: map[string]bool{}, leaves: map[string]bool{}}
}

// add records files (relative to prefix) and the folders above them, and
// returns the paths that are new, relative to the tree's root. Hidden paths
// are dropped unless the set shows them.
func (s *treeSet) add(prefix string, files []string) []string {
	var out []string
	for _, f := range files {
		rel := prefix + strings.TrimSuffix(f, "/")
		if !s.hidden && isHiddenPath(rel) {
			continue
		}
		var above []string
		for dir := path.Dir(rel); dir != "." && !s.seen[dir]; dir = path.Dir(dir) {
			s.seen[dir] = true
			above = append(above, dir)
		}
		out = append(out, above...)
		s.leaves[rel] = true
		if !s.seen[rel] {
			s.seen[rel] = true
			out = append(out, rel)
		}
	}
	return out
}

// addTopLevel adds root's own entries that git did not list: git knows
// nothing of empty or ignored folders, but they are still places to open a
// terminal in. It returns the names added.
//
// Git spells names precomposed (core.precomposeunicode on macOS) while the
// folder holds whatever bytes a name was created with, often decomposed;
// a name git listed under its other spelling is recognised by identity.
func (s *treeSet) addTopLevel(root string) []string {
	dirents, err := readDir(root)
	if err != nil {
		return nil
	}
	var ids map[fileID]bool
	var out []string
	for _, d := range dirents {
		name := d.Name()
		if s.seen[name] || name == ".git" || (!s.hidden && strings.HasPrefix(name, ".")) {
			continue
		}
		if !isASCII(name) {
			if ids == nil {
				ids = s.topLevelIDs(root)
			}
			if info, err := os.Lstat(filepath.Join(root, name)); err == nil {
				if id, ok := idOf(info); ok && ids[id] {
					continue
				}
			}
		}
		s.seen[name] = true
		out = append(out, name)
	}
	return out
}

// topLevelIDs are the identities of the non-ASCII top-level names listed.
func (s *treeSet) topLevelIDs(root string) map[fileID]bool {
	ids := map[fileID]bool{}
	for rel := range s.seen {
		if strings.Contains(rel, "/") || isASCII(rel) {
			continue
		}
		if info, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			if id, ok := idOf(info); ok {
				ids[id] = true
			}
		}
	}
	return ids
}

// statTree describes slash-separated paths under root, in parallel. Paths
// that vanished (deleted but still tracked) are skipped, and so are those
// not reached before ctx ends.
func statTree(ctx context.Context, root string, rels []string) []entry {
	found := make([]entry, len(rels))
	ok := make([]bool, len(rels))
	var wg sync.WaitGroup
	workers := min(8, runtime.GOMAXPROCS(0))
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(rels) && ctx.Err() == nil; i += workers {
				e, err := statEntry(filepath.Join(root, filepath.FromSlash(rels[i])), rels[i])
				found[i], ok[i] = e, err == nil
			}
		})
	}
	wg.Wait()
	out := make([]entry, 0, len(rels))
	for i, e := range found {
		if ok[i] {
			out = append(out, e)
		}
	}
	return out
}

// errWalkDone stops a walk that reached its bounds.
var errWalkDone = errors.New("walk bounds reached")

// walkTree lists root's tree outside git: skipDirs and .git are not entered,
// symlinks are not followed, and the walk stops at maxEntries, maxDepth or
// walkDeadline (marking the listing limited). Folders are opened with
// openDir, so one swapped for a FIFO can't block it.
func walkTree(ctx context.Context, root string, hidden bool) (listing, error) {
	l := listing{root: root, recursive: true}
	dirents, err := readDir(root)
	if err != nil {
		return l, err
	}
	w := walker{ctx: ctx, root: root, hidden: hidden, deadline: time.Now().Add(walkDeadline), l: &l}
	err = w.walk("", dirents)
	if errors.Is(err, errWalkDone) {
		err = nil
	}
	sortTree(l.entries)
	return l, err
}

type walker struct {
	ctx      context.Context
	root     string
	hidden   bool
	deadline time.Time
	l        *listing
}

// walk adds the entries of the folder rel (its dirents in name order) and
// everything below them, depth first.
func (w *walker) walk(rel string, dirents []os.DirEntry) error {
	for _, d := range dirents {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		name := d.Name()
		if name == ".git" || (!w.hidden && strings.HasPrefix(name, ".")) {
			continue
		}
		if len(w.l.entries) >= maxEntries || time.Now().After(w.deadline) {
			w.l.limited = true
			return errWalkDone
		}
		child := path.Join(rel, name)
		abs := filepath.Join(w.root, filepath.FromSlash(child))
		e, err := statEntry(abs, child)
		if err != nil {
			w.l.unreadable++
			continue
		}
		w.l.entries = append(w.l.entries, e)
		if !d.IsDir() || skipDirs[name] {
			continue // a symlink to a folder is not a folder here: not followed
		}
		if strings.Count(child, "/") >= maxDepth {
			w.l.limited = true
			continue
		}
		sub, err := readDir(abs)
		if err != nil {
			w.l.unreadable++
		}
		if err := w.walk(child, sub); err != nil {
			return err
		}
	}
	return nil
}

// openDir opens path only if it is (still) a folder. O_DIRECTORY makes a
// FIFO or device that replaced it fail at once (ENOTDIR) instead of
// blocking in open(2), as a plain os.Open would.
func openDir(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}

// readDir is os.ReadDir through openDir: a folder's entries sorted by name,
// with those read before an error.
func readDir(path string) ([]os.DirEntry, error) {
	f, err := openDir(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dirents, err := f.ReadDir(-1)
	slices.SortFunc(dirents, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return dirents, err
}
