package files

import (
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/match"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// rankedMsg delivers a background ranking of a large corpus.
type rankedMsg struct {
	rev  int
	rows []row
	keep string
}

// rebuild recomputes the rows from the listing and the query, then selects
// keep (a path) if it is still listed, else the first row.
func (m *model) rebuild(keep string) tea.Cmd {
	if m.path.active {
		return m.complete(keep)
	}
	m.rankRev++
	m.pattern = match.Compile(m.query.Value())
	corpus := rowsOf(m.data.entries, fromListing)
	if m.pattern.Empty() {
		var head []row
		if m.data.err == nil { // an unreadable folder shows only why (emptyPage)
			head = slices.Concat(rowsOf(m.recent, fromRecent), rowsOf(m.startProjects(), fromProject))
		}
		var sections []ui.Section
		if len(head) > 0 {
			sections = []ui.Section{
				{Title: "Recent here", Start: 0},
				{Title: "Projects", Start: len(m.recent)},
				{Title: "In " + sanitize.Name(filepath.Base(m.dir)), Start: len(head)},
			}
		}
		m.rankedRev = m.rankRev
		return m.setRows(slices.Concat(head, corpus), sections, keep)
	}
	corpus = append(corpus, withoutDuplicates(corpus, rowsOf(m.recent, fromRecent))...)
	m.searched = len(corpus)
	if len(corpus) <= syncRankLimit {
		m.rankedRev = m.rankRev
		return m.setRows(rankRows(m.pattern, corpus), nil, keep)
	}
	rev, query := m.rankRev, m.query.Value()
	return tea.Batch(m.spinner.Start(m.cfg.now()), func() tea.Msg {
		return rankedMsg{rev: rev, rows: rankRows(match.Compile(query), corpus), keep: keep}
	})
}

func (m *model) ranked(msg rankedMsg) tea.Cmd {
	if msg.rev != m.rankRev {
		return nil
	}
	m.spinner.Stop()
	m.rankedRev = msg.rev
	return tea.Batch(m.setRows(msg.rows, nil, msg.keep), m.flush())
}

// settled reports whether the rows show the current folder and query. A
// verb asked for before then (a name typed and ⏎ pressed straight away)
// waits, so that it acts on what was typed.
func (m *model) settled() bool {
	return m.loadedRev == m.scanRev && m.rankedRev == m.rankRev
}

// flush runs the verb that waited for the rows, once they are settled.
func (m *model) flush() tea.Cmd {
	if m.pending == nil || !m.settled() {
		return nil
	}
	v := *m.pending
	m.pending = nil
	return m.act(v)
}

// startProjects are zoxide's projects, offered only in the folder Files
// opened in (they are about where to start, not about this folder).
func (m *model) startProjects() []entry {
	if len(m.back) > 0 || m.dir != m.cfg.dir || m.recursive {
		return nil
	}
	return m.projects
}

func rowsOf(entries []entry, from source) []row {
	out := make([]row, len(entries))
	for i, e := range entries {
		out[i] = row{e: e, from: from}
	}
	return out
}

// setRows installs rows and selects keep. When keep is not listed the
// selection rests on the folder itself (nothing selected) while the query
// is empty, and on the best match while searching.
func (m *model) setRows(rows []row, sections []ui.Section, keep string) tea.Cmd {
	m.rows = rows
	m.nameWidth = 0
	for _, r := range rows {
		m.nameWidth = max(m.nameWidth, nameWidth(r))
	}
	m.list.SetItems(len(rows), sections)
	m.fitList() // a stacked list is as tall as its rows
	i := m.indexOf(keep)
	m.here = i < 0 && m.canRestHere()
	m.list.Select(max(0, i))
	return m.selectionChanged()
}

// canRestHere reports whether "nothing selected" is a place the selection
// can be: in the folder's own listing, not while searching or in the path
// field (there the best match or completion leads).
func (m *model) canRestHere() bool {
	return !m.path.active && m.query.Value() == ""
}

// rest leaves every row unselected, so verbs act on the folder itself.
func (m *model) rest() {
	m.here = true
	m.list.Select(0) // scrolled to the top, where the breadcrumb is
}

func (m *model) indexOf(path string) int {
	if path == "" {
		return -1
	}
	return slices.IndexFunc(m.rows, func(r row) bool { return r.e.path == path })
}

// selectionChanged follows the selection with the preview and folder counts.
func (m *model) selectionChanged() tea.Cmd {
	return tea.Batch(m.syncPreview(), m.requestCounts())
}

func (m *model) selected() (row, bool) {
	i := m.list.Cursor()
	if m.here || i < 0 || i >= len(m.rows) {
		return row{}, false
	}
	return m.rows[i], true
}

func (m *model) selectedPath() string {
	r, ok := m.selected()
	if !ok {
		return ""
	}
	return r.e.path
}

// navigate goes into dir, remembering where the user was so ← comes back
// to the same query and selection.
func (m *model) navigate(dir, selectAfter string) tea.Cmd {
	m.back = append(m.back, place{dir: m.dir, query: m.query.Value(), selected: m.selectedPath(), recursive: m.recursive})
	return m.goTo(dir, "", selectAfter, m.recursive)
}

// goBack returns to the previous place, or goes up to the parent folder
// (selecting the folder just left) when there is none.
func (m *model) goBack() tea.Cmd {
	if n := len(m.back); n > 0 {
		p := m.back[n-1]
		m.back = m.back[:n-1]
		return m.goTo(p.dir, p.query, p.selected, p.recursive)
	}
	parent := filepath.Dir(m.dir)
	if parent == m.dir {
		return nil
	}
	return m.goTo(parent, "", m.dir, m.recursive)
}

func (m *model) goTo(dir, query, selectAfter string, recursive bool) tea.Cmd {
	moved := dir != m.dir
	m.dir, m.recursive = dir, recursive
	m.query.SetValue(query)
	m.query.CursorEnd()
	m.restore = selectAfter
	if selectAfter == "" {
		m.restore = dir // nothing listed has this path: rest on the folder itself
	}
	if !moved {
		return m.load()
	}
	return tea.Batch(m.load(), m.loadGit())
}

func (m *model) toggleRecursive() tea.Cmd {
	m.recursive = !m.recursive
	// With a query, the search now covers other files: its best match leads.
	m.bestFirst = m.query.Value() != ""
	return m.load()
}

func (m *model) toggleHidden() tea.Cmd {
	m.hidden = !m.hidden
	clear(m.counts)
	clear(m.counting)
	if m.path.active {
		return m.complete(m.selectedPath())
	}
	return m.load()
}

// refresh forgets cached listings and previews and reloads everything.
func (m *model) refresh() tea.Cmd {
	clear(m.listings)
	clear(m.counts)
	clear(m.counting)
	m.cache.clear()
	m.previewKey = ""
	return tea.Batch(m.load(), m.loadGit())
}
