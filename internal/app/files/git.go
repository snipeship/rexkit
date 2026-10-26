package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitTimeout bounds each git call made for the header chip and badges.
const gitTimeout = 2 * time.Second

// repoStatus is what git says about the folder being browsed.
type repoStatus struct {
	branch        string
	ahead, behind int
	dirty         int             // changed or untracked paths
	prefix        string          // the folder's path inside the repo, "" or "sub/dir/"
	badges        map[string]byte // repo-relative path → badge; folders aggregate
	byID          map[fileID]byte // badges of the folder's own non-ASCII entries, by identity
}

// badge returns the git badge for a path relative to the browsed folder.
func (s repoStatus) badge(rel string) byte {
	if s.badges == nil || strings.HasPrefix(rel, "../") {
		return 0
	}
	return s.badges[s.prefix+rel]
}

// badgeOf returns the git badge of an entry of the browsed folder. Git
// spells names precomposed (core.precomposeunicode on macOS) while a folder
// listing returns the bytes a name was created with, often decomposed; an
// entry whose name git spells differently is found by its identity.
func (s repoStatus) badgeOf(e entry) byte {
	if b := s.badge(e.rel); b != 0 || s.byID == nil || e.id == (fileID{}) {
		return b
	}
	return s.byID[e.id]
}

// git runs git for browsing: the folder may be a download, an extracted
// archive or a fresh clone, and its own .git/config must not run anything.
// core.fsmonitor names a program that status and ls-files would start; the
// command line overrides every config file, and reaches submodules through
// the environment. (Empty rather than "false": before git 2.36 the value
// is a program path, and "false" would name one.)
func git(ctx context.Context, run runner, dir string, args ...string) ([]byte, error) {
	return run(ctx, dir, "", "git", append([]string{"-c", "core.fsmonitor="}, args...)...)
}

// gitStatus reads the status of the repository containing dir. ok is false
// outside a work tree, when git is missing, or on timeout.
func gitStatus(ctx context.Context, run runner, dir string) (repoStatus, bool) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := git(ctx, run, dir, "rev-parse", "--show-prefix")
	if err != nil {
		return repoStatus{}, false
	}
	prefix := strings.TrimSpace(string(out))
	filters, ok := repoFilterOverrides(ctx, run, dir)
	if !ok {
		return repoStatus{}, false
	}
	// Submodules' work trees are not scanned: that would be a git status in
	// each of them, under their own config.
	args := append(filters, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal", "--ignore-submodules=dirty")
	out, err = git(ctx, run, dir, args...)
	if err != nil {
		return repoStatus{}, false
	}
	s := parseStatus(out)
	s.prefix = prefix
	s.byID = badgeAliases(dir, prefix, s.badges)
	return s, true
}

// repoFilterOverrides returns -c options that switch off the content
// filters the repository configures itself (.git/config and what it
// includes): git status runs a filter's clean command whenever it re-reads
// a file, so a cloned or downloaded repository could run anything. Filters
// from the user's own global or system config, like git-lfs, are trusted
// and kept. ok is false when that can't be done safely.
func repoFilterOverrides(ctx context.Context, run runner, dir string) ([]string, bool) {
	out, err := git(ctx, run, dir, "config", "--show-scope", "-z", "--get-regexp", `^filter\..*\.(clean|process)$`)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, true // no filters at all
	}
	if err != nil {
		return nil, false
	}
	var args []string
	seen := map[string]bool{}
	// Records are "scope\0key\nvalue\0".
	records := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(records); i += 2 {
		scope := records[i]
		key, _, _ := strings.Cut(records[i+1], "\n")
		if scope == "global" || scope == "system" {
			continue
		}
		name := strings.TrimPrefix(key[:strings.LastIndexByte(key, '.')], "filter.")
		if strings.ContainsAny(name, "=\n") {
			return nil, false // a name -c can't express: no status rather than a risk
		}
		if !seen[name] {
			seen[name] = true
			args = append(args, "-c", "filter."+name+".clean=", "-c", "filter."+name+".process=", "-c", "filter."+name+".required=false")
		}
	}
	return args, true
}

// badgeAliases maps the identity of each non-ASCII entry of dir that has a
// badge to that badge (see badgeOf). Only the folder's own entries are
// looked up: deeper paths in a listing come from git, spelled as git
// spells them.
func badgeAliases(dir, prefix string, badges map[string]byte) map[fileID]byte {
	var out map[fileID]byte
	for p, b := range badges {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok || rest == "" || strings.Contains(rest, "/") || isASCII(rest) {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, rest))
		if err != nil {
			continue
		}
		if id, ok := idOf(info); ok {
			if out == nil {
				out = map[fileID]byte{}
			}
			out[id] = b
		}
	}
	return out
}

// badgeRank orders badges for aggregation onto folders: the most urgent wins.
var badgeRank = map[byte]int{'U': 6, 'D': 5, 'M': 4, 'A': 3, 'R': 2, '?': 1}

// parseStatus parses `git status --porcelain=v2 --branch -z`. Paths are
// relative to the repository root.
func parseStatus(out []byte) repoStatus {
	s := repoStatus{badges: map[string]byte{}}
	var oid string
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		rec := string(records[i])
		switch {
		case strings.HasPrefix(rec, "# branch.oid "):
			oid = strings.TrimPrefix(rec, "# branch.oid ")
		case strings.HasPrefix(rec, "# branch.head "):
			s.branch = strings.TrimPrefix(rec, "# branch.head ")
		case strings.HasPrefix(rec, "# branch.ab "):
			for f := range strings.FieldsSeq(strings.TrimPrefix(rec, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					s.ahead = n
				} else {
					s.behind = n
				}
			}
		case strings.HasPrefix(rec, "1 "), strings.HasPrefix(rec, "2 "), strings.HasPrefix(rec, "u "):
			fields := 9 // 1 XY sub mH mI mW hH hI path
			switch rec[0] {
			case '2':
				fields = 10 // … Xscore path, then the original path as its own record
				i++
			case 'u':
				fields = 11
			}
			parts := strings.SplitN(rec, " ", fields)
			if len(parts) == fields {
				s.mark(parts[fields-1], changeBadge(rec[0], parts[1]))
			}
		case strings.HasPrefix(rec, "? "):
			s.mark(strings.TrimSuffix(rec[2:], "/"), '?')
		}
	}
	if s.branch == "(detached)" && len(oid) >= 7 {
		s.branch = oid[:7]
	}
	return s
}

// changeBadge picks the badge for a changed entry from its XY code,
// preferring the worktree state over the index state.
func changeBadge(kind byte, xy string) byte {
	if kind == 'u' {
		return 'U'
	}
	code := xy[1]
	if code == '.' {
		code = xy[0]
	}
	switch code {
	case 'A', 'C':
		return 'A'
	case 'D':
		return 'D'
	case 'R':
		return 'R'
	}
	return 'M'
}

// mark records a badge for a path and every folder above it.
func (s *repoStatus) mark(p string, b byte) {
	s.dirty++
	for {
		if badgeRank[b] > badgeRank[s.badges[p]] {
			s.badges[p] = b
		}
		if p = path.Dir(p); p == "." || p == "/" {
			return
		}
	}
}
