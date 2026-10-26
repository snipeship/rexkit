package files

import (
	"cmp"
	"context"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rexkit/internal/match"
	"rexkit/internal/ui"
)

// pathMode is the path field (⌃L, or a click on the current breadcrumb):
// type a path, see the folder's entries live, ⇥ completes, ⏎ goes there,
// ⎋ returns to the list as it was.
type pathMode struct {
	active     bool
	input      textinput.Model
	savedQuery string
	savedSel   string
	dir        string // the folder whose entries are offered
	data       listing
	rev        int
	listed     bool  // dir's entries have arrived
	typed      entry // the typed path, when it exists
	exists     bool
}

type pathListingMsg struct {
	rev  int
	data listing
}

func newPathInput() textinput.Model {
	in := ui.NewInput()
	in.CharLimit = 4096
	in.Blur()
	return in
}

// openPath enters the path field, pre-filled with the current folder.
func (m *model) openPath() tea.Cmd {
	p := &m.path
	p.active = true
	p.savedQuery, p.savedSel = m.query.Value(), m.selectedPath()
	p.input.SetValue(m.typedForm(m.dir) + "/")
	p.input.CursorEnd()
	p.input.Focus()
	m.query.Blur()
	return tea.Batch(m.relayout(), m.complete(""))
}

// closePath leaves the path field and restores the list as it was.
func (m *model) closePath() tea.Cmd {
	m.leavePath()
	return m.rebuild(m.path.savedSel)
}

func (m *model) leavePath() {
	m.path.active = false
	m.path.input.Blur()
	m.query.SetValue(m.path.savedQuery)
	m.query.CursorEnd()
	m.query.Focus()
}

// typedForm is how a folder is written in the path field: ~/… under home.
func (m *model) typedForm(dir string) string {
	home := m.cfg.home
	switch {
	case home != "" && dir == home:
		return "~"
	case home != "" && strings.HasPrefix(dir, home+"/"):
		return "~" + dir[len(home):]
	case dir == "/":
		return ""
	}
	return dir
}

// typedPath splits the field into the folder to list and the name prefix
// being typed: "~/Cod" → (home, "Cod"); "~/Coding/" → (~/Coding, "").
func (m *model) typedPath() (dir, prefix string) {
	v := m.path.input.Value()
	if v == "" || v == "~" || strings.HasSuffix(v, "/") {
		return expandPath(cmp.Or(v, "."), m.dir, m.cfg.home), ""
	}
	return expandPath(filepath.Dir(v), m.dir, m.cfg.home), filepath.Base(v)
}

// complete shows the entries of the typed folder that match the typed
// name, loading the folder first when it changed.
func (m *model) complete(keep string) tea.Cmd {
	dir, prefix := m.typedPath()
	typed, err := statEntry(expandPath(cmp.Or(m.path.input.Value(), "."), m.dir, m.cfg.home), "")
	m.path.typed, m.path.exists = typed, err == nil
	m.pattern = match.Compile(prefix)
	if dir != m.path.dir {
		m.path.dir = dir
		m.path.data, m.path.listed = listing{root: dir}, false
		m.path.rev++
		rev := m.path.rev
		load := func() tea.Msg {
			l, _ := readFolder(context.Background(), dir, true)
			return pathListingMsg{rev: rev, data: l}
		}
		return tea.Batch(m.setRows(nil, nil, ""), load)
	}
	return m.setRows(completions(m.path.data.entries, prefix, m.hidden), nil, keep)
}

func (m *model) pathListed(msg pathListingMsg) tea.Cmd {
	if msg.rev != m.path.rev || !m.path.active {
		return nil
	}
	m.path.data, m.path.listed = msg.data, true
	return m.complete("")
}

// completions are the entries whose names start with prefix (ignoring
// case), then those that contain it. Dotfiles are offered when hidden files
// are shown or the prefix starts with a dot.
func completions(entries []entry, prefix string, hidden bool) []row {
	lower := strings.ToLower(prefix)
	dots := hidden || strings.HasPrefix(prefix, ".")
	var starts, contains []row
	for _, e := range entries {
		name := strings.ToLower(e.name())
		switch {
		case !dots && strings.HasPrefix(name, "."):
		case strings.HasPrefix(name, lower):
			starts = append(starts, row{e: e})
		case strings.Contains(name, lower):
			contains = append(contains, row{e: e})
		}
	}
	return append(starts, contains...)
}

// tabComplete extends the field to the completions' common prefix, or to
// the selected completion.
func (m *model) tabComplete() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	v := m.path.input.Value()
	head := v[:strings.LastIndexByte(v, '/')+1]
	if v == "~" {
		head = "~/"
	}
	_, prefix := m.typedPath()
	var names []string
	for _, r := range m.rows {
		if strings.HasPrefix(strings.ToLower(r.e.name()), strings.ToLower(prefix)) {
			names = append(names, r.e.name())
		}
	}
	if common := commonPrefix(names); len(names) > 1 && len(common) > len(prefix) {
		v = head + common
	} else if r, ok := m.selected(); ok {
		v = head + r.e.name()
		if r.e.isDir() {
			v += "/"
		}
	}
	m.path.input.SetValue(v)
	m.path.input.CursorEnd()
	return m.complete("")
}

func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	p := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, p) {
			p = p[:len(p)-1]
		}
	}
	for !utf8.ValidString(p) {
		p = p[:len(p)-1]
	}
	return p
}

// pathGoal is where ⏎ in the path field goes: the typed path when it
// exists, else the selected completion.
func (m *model) pathGoal() (entry, bool) {
	if m.path.exists {
		return m.path.typed, true
	}
	r, ok := m.selected()
	return r.e, ok
}

// pathRefused reports a typed path that leads nowhere: nothing by that name,
// and nothing that starts with it, in a folder whose entries have arrived.
// The field turns ErrField and the tray says so.
func (m *model) pathRefused() bool {
	if p := &m.path; !p.active || !p.listed || p.input.Value() == "" {
		return false
	}
	_, ok := m.pathGoal()
	return !ok
}

// goPath goes to pathGoal: a folder is entered, a file's folder is entered
// with the file selected.
func (m *model) goPath() tea.Cmd {
	e, ok := m.pathGoal()
	if !ok {
		v := expandPath(cmp.Or(m.path.input.Value(), "."), m.dir, m.cfg.home)
		return m.toast.Show(ui.ToastErr, "Nothing at "+displayPath(v, m.cfg.home))
	}
	m.leavePath()
	if e.isDir() {
		return m.navigate(e.path, "")
	}
	return m.navigate(filepath.Dir(e.path), e.path)
}
