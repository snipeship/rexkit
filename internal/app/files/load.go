package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/ui"
)

// Messages from background loads.
type (
	listingMsg struct {
		rev    int
		data   listing
		recent []entry
		err    error
	}
	gitMsg struct {
		dir    string
		status repoStatus
		ok     bool
	}
	projectsMsg struct{ entries []entry }
	countsMsg   struct {
		hidden bool
		counts map[string]count
	}
)

// count is a folder's number of (visible) entries.
type count struct {
	n      int
	capped bool // stopped counting at maxCount
	failed bool
}

// maxCount bounds how many names are read to count a folder.
const maxCount = 10_000

// maxListings bounds the listing cache.
const maxListings = 32

func listingKey(dir string, hidden, recursive bool) string {
	return dir + "\x00" + strconv.FormatBool(hidden) + strconv.FormatBool(recursive)
}

// load (re)reads the current folder. A listing seen before is shown at once
// and revalidated in the background.
func (m *model) load() tea.Cmd {
	if m.scanCancel != nil {
		m.scanCancel()
	}
	m.scanRev++
	rev, dir, hidden, recursive := m.scanRev, m.dir, m.hidden, m.recursive
	if cached, ok := m.listings[listingKey(dir, hidden, recursive)]; ok {
		m.data = cached
	} else {
		m.data = listing{root: dir, recursive: recursive}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.scanCancel = cancel
	run, dirs, now := m.cfg.run, m.cfg.dirs, m.cfg.now()
	load := func() tea.Msg {
		var l listing
		var err error
		if recursive {
			l, err = readTree(ctx, run, dir, hidden)
		} else {
			l, err = readFolder(ctx, dir, hidden)
		}
		return listingMsg{rev: rev, data: l, recent: recentHere(dirs, dir, now), err: err}
	}
	m.restore = m.restoreOrSelected()
	return tea.Batch(m.spinner.Start(now), m.rebuild(m.restore), load)
}

func (m *model) listingLoaded(msg listingMsg) tea.Cmd {
	if msg.rev != m.scanRev {
		return nil
	}
	m.spinner.Stop()
	m.loadedRev = msg.rev
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		return tea.Batch(m.listingFailed(msg.err), m.flush())
	}
	if len(m.listings) >= maxListings {
		clear(m.listings)
	}
	m.listings[listingKey(msg.data.root, m.hidden, msg.data.recursive)] = msg.data
	m.data, m.recent = msg.data, msg.recent
	keep := m.restoreOrSelected()
	m.restore = ""
	m.bestFirst = false
	cmd := m.rebuild(keep)
	return tea.Batch(cmd, m.flush())
}

// listingFailed explains why a folder can't be shown. A folder that
// disappeared is replaced by its nearest surviving parent.
func (m *model) listingFailed(err error) tea.Cmd {
	shown := displayPath(m.dir, m.cfg.home)
	if errors.Is(err, fs.ErrNotExist) {
		parent := m.dir
		for parent != filepath.Dir(parent) && checkFolder(parent) != nil {
			parent = filepath.Dir(parent)
		}
		if parent != m.dir {
			m.dir = parent
			return tea.Batch(m.toast.Show(ui.ToastWarn, shown+" no longer exists"), m.load(), m.loadGit())
		}
	}
	m.data = listing{root: m.dir, err: err}
	return m.rebuild("") // the body explains (emptyPage)
}

func (m *model) restoreOrSelected() string {
	if m.restore != "" {
		return m.restore
	}
	if m.bestFirst {
		return ""
	}
	return m.selectedPath()
}

// loadGit reads the repository status of the current folder.
func (m *model) loadGit() tea.Cmd {
	m.git, m.inRepo = repoStatus{}, false
	run, dir := m.cfg.run, m.dir
	return func() tea.Msg {
		s, ok := gitStatus(context.Background(), run, dir)
		return gitMsg{dir: dir, status: s, ok: ok}
	}
}

// loadProjects asks zoxide for the user's projects (shown at the start only).
func (m *model) loadProjects() tea.Cmd {
	run, home, dir := m.cfg.run, m.cfg.home, m.dir
	return func() tea.Msg { return projectsMsg{projects(context.Background(), run, home, dir)} }
}

// requestCounts counts the entries of the folders on screen that have not
// been counted yet.
func (m *model) requestCounts() tea.Cmd {
	var want []string
	for y := range m.list.Height() {
		i, ok := m.list.ItemAt(y)
		if !ok || i >= len(m.rows) {
			continue
		}
		if e := m.rows[i].e; e.isDir() && !m.counting[e.path] {
			if _, done := m.counts[e.path]; !done {
				want = append(want, e.path)
				m.counting[e.path] = true
			}
		}
	}
	if len(want) == 0 {
		return nil
	}
	hidden := m.hidden
	return func() tea.Msg { return countsMsg{hidden: hidden, counts: countFolders(want, hidden)} }
}

func (m *model) countsLoaded(msg countsMsg) {
	if msg.hidden != m.hidden {
		return // counted under the other hidden setting
	}
	for p, c := range msg.counts {
		m.counts[p] = c
		delete(m.counting, p)
	}
}

// countFolders counts each folder's entries by name, reading at most
// maxCount names per folder.
func countFolders(paths []string, hidden bool) map[string]count {
	out := make(map[string]count, len(paths))
	for _, p := range paths {
		out[p] = countFolder(p, hidden)
	}
	return out
}

func countFolder(path string, hidden bool) count {
	f, err := openDir(path)
	if err != nil {
		return count{failed: true}
	}
	defer f.Close()
	names, err := f.Readdirnames(maxCount + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return count{failed: true}
	}
	c := count{capped: len(names) > maxCount}
	for _, n := range names {
		if hidden || !strings.HasPrefix(n, ".") {
			c.n++
		}
	}
	return c
}
