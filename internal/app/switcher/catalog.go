package switcher

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// toolLabelPrefix marks rexkit's own tool blocks; they are never listed.
const toolLabelPrefix = "rexkit:"

// catalog is every place the switcher can go to, in tree order: each session
// followed by its tabs, each tab followed by its panes.
type catalog struct {
	entries []entry
	index   map[string]int // entry key → position
}

// find returns the entry with the given key.
func (c *catalog) find(key string) (entry, bool) {
	i, ok := c.index[key]
	if !ok {
		return entry{}, false
	}
	return c.entries[i], true
}

// within reports whether entry i lies below the session or tab scope.
func (c *catalog) within(i int, scope string) bool {
	e := c.entries[i]
	return e.key() != scope && (e.sessionID == scope || e.kind != ui.KindSession && e.windowID == scope)
}

// catalogOptions say whose catalog it is.
type catalogOptions struct {
	origin place  // where the switcher was opened; its session is listed first
	self   string // the switcher's own block, never listed
	home   string // abbreviated to ~ in paths
}

// buildCatalog turns a Rex inventory into a catalog.
func buildCatalog(inv rex.Inventory, o catalogOptions) *catalog {
	sessions := slices.Clone(inv.Sessions)
	slices.SortStableFunc(sessions, func(a, b rex.SessionView) int {
		if ah, bh := a.SessionID == o.origin.sessionID, b.SessionID == o.origin.sessionID; ah != bh {
			if ah {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Order, b.Order)
	})
	c := &catalog{index: map[string]int{}}
	for _, s := range sessions {
		c.addSession(s.View, o)
	}
	for i, e := range c.entries {
		c.index[e.key()] = i
	}
	return c
}

func (c *catalog) addSession(v rex.View, o catalogOptions) {
	sess := entry{kind: ui.KindSession, sessionID: v.SessionID}
	sess.name, sess.nameEsc = sanitize.LabelSpans(v.Label)
	if sess.name == "" {
		sess.name = "Session"
	}
	var tabs [][]entry // each tab entry followed by its panes
	for n, w := range v.Windows {
		group := tabEntries(sess, w, n+1, o)
		if group == nil {
			continue
		}
		tabs = append(tabs, group)
		if w.ID == v.ActiveWindowID || sess.windowID == "" {
			sess.windowID, sess.blockID = group[0].windowID, group[0].blockID
		}
		sess.tabs++
		sess.panes += group[0].panes
		sess.blocked += group[0].blocked
		sess.failed += group[0].failed
		sess.working += group[0].working
	}
	if len(tabs) == 0 {
		return // nothing in it but rexkit's own tools
	}
	c.entries = append(c.entries, sess)
	for _, group := range tabs {
		c.entries = append(c.entries, group...)
	}
}

// tabEntries returns a window's tab entry followed by its panes, or nil when
// it has no listable block.
func tabEntries(sess entry, w rex.Window, number int, o catalogOptions) []entry {
	tab := entry{kind: ui.KindTab, sessionID: sess.sessionID, windowID: w.ID, session: sess.name, depth: 1}
	var panes []entry
	for _, l := range tiledFirst(w.Layers) {
		for _, b := range l.Blocks {
			if b.ID == o.self || strings.HasPrefix(b.Label, toolLabelPrefix) {
				continue
			}
			panes = append(panes, paneEntry(tab, b, o.home))
		}
	}
	if len(panes) == 0 {
		return nil
	}
	tab.blockID = jumpBlock(w, panes, o.origin)
	tab.name, tab.nameEsc = tabName(w.Label, panes, tab.blockID, number)
	tab.panes = len(panes)
	for i := range panes {
		panes[i].tab = tab.name
		switch {
		case panes[i].kind != ui.KindAgent:
		case panes[i].status == ui.StatusFailed:
			tab.failed++
		case panes[i].status == ui.StatusBlocked:
			tab.blocked++
		case panes[i].status == ui.StatusWorking:
			tab.working++
		}
	}
	return append([]entry{tab}, panes...)
}

// tiledFirst lists the tiled layer before floating ones, each in view order.
func tiledFirst(layers []rex.Layer) []rex.Layer {
	out := slices.Clone(layers)
	slices.SortStableFunc(out, func(a, b rex.Layer) int {
		switch {
		case a.Floating() == b.Floating():
			return 0
		case a.Floating():
			return 1
		}
		return -1
	})
	return out
}

// jumpBlock is the block going to a tab focuses: its focused block, or the
// origin when the switcher's own float has focus there, or the first pane.
func jumpBlock(w rex.Window, panes []entry, origin place) string {
	has := func(id string) bool {
		return id != "" && slices.ContainsFunc(panes, func(p entry) bool { return p.blockID == id })
	}
	switch {
	case has(w.FocusedBlockID):
		return w.FocusedBlockID
	case w.ID == origin.windowID && has(origin.blockID):
		return origin.blockID
	}
	return panes[0].blockID
}

func paneEntry(tab entry, b rex.Block, home string) entry {
	t := readTerminal(b.Terminal)
	e := entry{
		kind: ui.KindPane, sessionID: tab.sessionID, windowID: tab.windowID, blockID: b.ID,
		session: tab.session, depth: 2,
		program: t.program, task: t.task, title: t.title,
		programEsc: t.programEsc, taskEsc: t.taskEsc, titleEsc: t.titleEsc,
		status: t.status, since: t.since, exit: t.exit,
	}
	cwd, cwdEsc := sanitize.LabelSpans(t.cwd)
	e.cwd, e.cwdEsc = abbreviate(cwd, cwdEsc, home)
	if !b.IsTerminal() && e.program == "" {
		e.program, e.programEsc = sanitize.LabelSpans(b.Flavor)
	}
	if t.agent {
		e.kind = ui.KindAgent
	}
	e.name, e.nameEsc = paneName(b.Label, e)
	return e
}

// paneName prefers a label the user chose, then what an agent is doing, then
// the program.
func paneName(raw string, e entry) (string, escapes) {
	label, esc := sanitize.LabelSpans(raw)
	switch {
	case !automaticLabel(label):
		return label, esc
	case e.kind == ui.KindAgent && e.task != "":
		return e.task, e.taskEsc
	case e.program != "":
		return e.program, e.programEsc
	}
	return "Terminal", nil
}

// automaticLabel reports a label Rex made up rather than one the user set.
func automaticLabel(label string) bool {
	switch strings.ToLower(label) {
	case "", "shell", "terminal":
		return true
	}
	return false
}

// defaultWindowLabel is how Rex names a tab nobody named: "Window 726ac4f9".
var defaultWindowLabel = regexp.MustCompile(`^Window [0-9a-f]{8}$`)

// tabName is the tab's label, or for an unnamed tab the folder or program of
// the pane it focuses.
func tabName(raw string, panes []entry, focused string, number int) (string, escapes) {
	label, esc := sanitize.LabelSpans(raw)
	if label != "" && !defaultWindowLabel.MatchString(label) {
		return label, esc
	}
	for _, p := range panes {
		if p.blockID != focused {
			continue
		}
		if p.cwd != "" && p.cwd != "~" && p.cwd != "/" {
			base := filepath.Base(p.cwd)
			if !strings.HasSuffix(p.cwd, base) {
				return base, nil
			}
			cut := len(p.cwd) - len(base)
			return base, p.cwdEsc.after(cut, -cut)
		}
		if p.program != "" {
			return p.program, p.programEsc
		}
	}
	return "Tab " + strconv.Itoa(number), nil
}

// abbreviate shows paths under home as ~/…, keeping their escapes in place.
func abbreviate(path string, esc escapes, home string) (string, escapes) {
	if home == "" || path == "" {
		return path, esc
	}
	if path == home {
		return "~", nil
	}
	if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(home, "/")+"/"); ok {
		cut := len(path) - len(rest)
		return "~/" + rest, esc.after(cut, len("~/")-cut)
	}
	return path, esc
}
