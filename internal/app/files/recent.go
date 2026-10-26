package files

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"rexkit/internal/state"
)

// Sizes of the empty-query sections.
const (
	maxRecent   = 5
	maxProjects = 5
	zoxideWait  = 400 * time.Millisecond
)

// recentHere returns the paths opened from Files below root, best first,
// that still exist. rel is relative to root.
func recentHere(dirs state.Dirs, root string, now time.Time) []entry {
	h, err := dirs.History()
	if err != nil {
		return nil
	}
	var out []entry
	for _, f := range h.OpensUnder(root, now, maxRecent) {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil {
			continue
		}
		if e, err := statEntry(f.Path, filepath.ToSlash(rel)); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// projects returns zoxide's best folders (none when zoxide is missing or
// fails), excluding the current one. rel is the home-relative display path.
func projects(ctx context.Context, run runner, home, current string) []entry {
	ctx, cancel := context.WithTimeout(ctx, zoxideWait)
	defer cancel()
	out, err := run(ctx, "", "", "zoxide", "query", "--list", "--score")
	if err != nil {
		return nil
	}
	return parseZoxide(string(out), home, current)
}

// parseZoxide reads `zoxide query --list --score` output: "  12.5 /path".
func parseZoxide(out, home, current string) []entry {
	var found []entry
	for line := range strings.Lines(out) {
		_, p, ok := strings.Cut(strings.TrimSpace(line), " ")
		p = strings.TrimSpace(p)
		if !ok || !filepath.IsAbs(p) || p == current || p == home {
			continue
		}
		e, err := statEntry(p, displayPath(p, home))
		if err != nil || !e.isDir() {
			continue
		}
		if found = append(found, e); len(found) == maxProjects {
			break
		}
	}
	return found
}
