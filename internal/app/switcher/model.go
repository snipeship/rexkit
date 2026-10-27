package switcher

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rexkit/internal/attend"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// env is what the model needs from the world outside it. Tests and the demo
// replace parts of it.
type env struct {
	api rex.API
	// close closes the switcher's float (popup.Close), restoring focus.
	close func(context.Context) error
	// record updates the visit history; nil keeps visits in memory only.
	record func(func(*state.History)) error
	// save stores a fresh inventory for the next instant first frame.
	save func(rex.Inventory)
	// attention reads the attention state (internal/attend) afresh; nil
	// keeps it in memory only.
	attention func() state.Attention
	// see marks a pane seen in the attention state (⌃⌫); nil keeps it in
	// memory only.
	see func(block string, at time.Time) error
	now func() time.Time
	// after delivers msg once d has passed.
	after func(d time.Duration, msg tea.Msg) tea.Cmd
	demo  bool
}

func tick(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

// options describe where the switcher runs.
type options struct {
	origin        place  // where the user was when the switcher opened
	self          string // the switcher's own block
	client        string // the app client that opened it
	stateDir      string // Lua's state dir: results and the commit marker (the handshake)
	dismissOnBlur bool
	home          string
	// nextKey is the key that goes to the next agent that needs the user
	// (agent_key, ⌥A or ⌃O a, whichever Lua found bound), which here
	// selects the next row of Needs you: its header and the key panel name
	// it. Empty: they name none.
	nextKey string
}

// crumb is a step of the drill-down trail, enough to go back to it.
type crumb struct {
	filter   filter
	selected string // key of the entry that was selected
}

type model struct {
	env
	opts options
	ctx  context.Context

	scheme        ui.Scheme
	theme         ui.Theme
	width, height int
	carded        bool // drawn in a card (popup.CardMsg)

	cat       *catalog
	inv       *rex.Inventory // what cat was built from
	updatedAt time.Time      // when cat was collected
	stale     bool           // the last refresh failed; cat may be old
	loadErr   string         // why there is nothing to show
	history   state.History
	attn      state.Attention // what the catalog knows of agents' news (attention)
	listing   listing
	filter    filter
	trail     []crumb
	list      ui.List
	input     textinput.Model

	refresh refresher
	pv      previewState

	toast   ui.Toaster
	loading ui.Spinner
	going   ui.Spinner // a jump or a move in flight: "Going…" in the tray
	clock   clock
	help    bool
	helpAt  int  // the key panel's scroll position, when it is cut short
	ask     *ask // a rename, close or new session being asked about (manage.go)
	busy    bool // closing or jumping: input is ignored

	live    bool // a fresh inventory arrived (or failed to); until then ⏎ waits
	pending bool // ⏎ was pressed before the catalog was live
	// pendingCommit is when Lua wrote the commit marker of the held ⏎ (zero:
	// no handshake waits for it, or it was answered already); pendingRev
	// tells a late stillPendingMsg from a current one.
	pendingCommit time.Time
	pendingRev    int
	// moved is set while the selection is one the user picked (arrows, wheel,
	// click) rather than one the listing chose; a new query, category or scope
	// clears it. A held ⏎ keeps a picked row but re-ranks a chosen one.
	moved bool

	started   time.Time // when the switcher started, for earlyEnter
	emptiedAt time.Time // when ⌫ last emptied the query: a held ⌫ must not back out
}

func newModel(ctx context.Context, e env, o options, inv *rex.Inventory, h state.History) *model {
	m := &model{
		env: e, opts: o, ctx: ctx, history: h,
		scheme: ui.NewScheme(), input: ui.NewInput(),
		pv: newPreviewState(), started: e.now(),
	}
	m.theme = m.scheme.Theme()
	if inv != nil {
		m.cat, m.inv, m.updatedAt = buildCatalog(*inv, m.catalogOptions()), inv, inv.CollectedAt
		m.refresh.saved = inv // the cache it came from
	}
	m.rebuild(selectDefault)
	return m
}

// catalogOptions read the attention state afresh: the watcher and ⌥A
// change it while Go to is open.
func (m *model) catalogOptions() catalogOptions {
	if m.attention != nil {
		m.attn = m.attention()
	}
	return catalogOptions{origin: m.opts.origin, self: m.opts.self, home: m.opts.home,
		memory: attend.Memory{Attention: m.attn, History: m.history}, now: m.now()}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), m.collect(), m.startWatch(), m.clock.start(m), m.previewTail(), m.selectionChanged())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
	}
	m.toast.Update(msg)
	before := m.selectedKey()
	cmd := m.update(msg)
	if m.selectedKey() != before {
		cmd = tea.Batch(cmd, m.selectionChanged())
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case popup.CardMsg:
		m.carded = true
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutChanged()
		return m.previewResized()
	case tea.KeyPressMsg:
		return m.key(m.earlyEnter(msg))
	case tea.PasteMsg:
		if m.ask != nil {
			if m.ask.kind == askClose || m.ask.busy {
				return nil
			}
			cmd := m.ask.editor.Paste(msg)
			m.layoutChanged()
			return cmd
		}
		return m.edit(msg)
	case tea.MouseClickMsg:
		if m.ask != nil {
			return nil // the ask holds the list still
		}
		return m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		if m.ask == nil {
			m.wheel(msg.Mouse())
		}
	case tea.BlurMsg:
		if m.opts.dismissOnBlur {
			return m.dismiss()
		}
	case clockMsg:
		return m.clock.tick(m, msg)
	case inventoryMsg:
		return m.inventoryArrived(msg)
	case watchMsg:
		return m.watchEvent(msg)
	case refreshDueMsg:
		m.refresh.due = false
		return m.collect()
	case pollMsg:
		return tea.Batch(m.collect(), m.after(pollInterval, pollMsg{}))
	case previewDueMsg:
		return m.previewDue(msg)
	case previewMsg:
		m.previewArrived(msg)
	case tailMsg:
		return m.tailTick()
	case doneMsg:
		return m.done(msg)
	case askDoneMsg:
		return m.askDone(msg)
	case stillPendingMsg:
		return m.stillPending(msg)
	}
	return nil
}

// selectPolicy says where the selection goes when the listing is rebuilt.
type selectPolicy int

const (
	selectKeep    selectPolicy = iota // the same entry, in the same section if possible
	selectTop                         // the first row
	selectDefault                     // the previous location in the switcher view, else the top
)

// rebuild recomputes the listing for the current filter.
func (m *model) rebuild(policy selectPolicy) {
	m.rebuildAt(policy, "")
}

// rebuildAt rebuilds and, when key is set, selects that entry.
func (m *model) rebuildAt(policy selectPolicy, key string) {
	m.rebuildFrom(policy, key, m.selection())
}

// selection is where the cursor is, remembered across a catalog change.
type selection struct {
	key, section string
	index        int
}

func (m *model) selection() selection {
	return selection{m.selectedKey(), m.selectedSection(), m.list.Cursor()}
}

// rebuildFrom rebuilds; selectKeep restores prev, which must be read before
// m.cat changed (rows index the catalog they were built from).
func (m *model) rebuildFrom(policy selectPolicy, key string, prev selection) {
	keepKey, keepSection, keepIndex := prev.key, prev.section, prev.index
	if policy != selectKeep {
		m.moved = false // the listing picks the row again
	}
	if m.cat == nil {
		m.listing = listing{}
		m.list.SetItems(0, nil)
		return
	}
	m.dropMissingScopes()
	m.filter.query = m.input.Value()
	m.listing = m.lister().list(m.filter)
	m.list.SetItems(len(m.listing.rows), m.listing.sections)
	m.list.SetTall(func(i int) bool { return m.detail(m.listing.rows[i]) != "" })
	m.list.SetHeight(m.layout().areas.List.H) // a stacked list is as tall as its rows
	switch {
	case key != "":
		m.list.Select(m.rowOf(key, ""))
	case policy == selectKeep && keepKey != "":
		if i := m.rowOf(keepKey, keepSection); i >= 0 {
			m.list.Select(i)
		} else {
			m.list.Select(keepIndex)
		}
	case policy == selectDefault && m.switcherView():
		m.list.Select(max(0, m.firstRowIn(secRecent)))
	default:
		m.list.Select(0)
	}
}

func (m *model) lister() lister {
	cur := m.opts.origin.visit(time.Time{})
	var recent []state.Visit
	for _, v := range m.history.Visits {
		if !v.Same(cur) {
			recent = append(recent, v)
		}
	}
	return lister{cat: m.cat, recent: recent, origin: m.opts.origin, nextKey: m.opts.nextKey}
}

// switcherView reports the sectioned empty-query view.
func (m *model) switcherView() bool {
	return m.filter.query == "" && m.filter.cat == catAll && m.filter.scope == ""
}

// dropMissingScopes leaves scopes whose session or tab closed.
func (m *model) dropMissingScopes() {
	for m.filter.scope != "" {
		if _, ok := m.cat.find(m.filter.scope); ok {
			return
		}
		if len(m.trail) == 0 {
			m.filter.scope = ""
			return
		}
		m.popCrumb()
	}
}

// selected returns the selected row and its entry.
func (m *model) selected() (row, entry, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.listing.rows) {
		return row{}, entry{}, false
	}
	r := m.listing.rows[i]
	return r, m.cat.entries[r.entry], true
}

func (m *model) selectedKey() string {
	_, e, ok := m.selected()
	if !ok {
		return ""
	}
	return e.key()
}

func (m *model) selectedSection() string {
	return m.sectionOf(m.list.Cursor())
}

func (m *model) sectionOf(i int) string {
	title := ""
	for _, s := range m.listing.sections {
		if s.Start <= i {
			title = s.Title
		}
	}
	return title
}

// rowOf finds the row showing key, preferring one in section; -1 if none.
func (m *model) rowOf(key, section string) int {
	found := -1
	for i, r := range m.listing.rows {
		if m.cat.entries[r.entry].key() != key {
			continue
		}
		if section == "" || m.sectionOf(i) == section {
			return i
		}
		if found < 0 {
			found = i
		}
	}
	return found
}

func (m *model) firstRowIn(section string) int {
	for _, s := range m.listing.sections {
		if s.Title == section {
			return s.Start
		}
	}
	return -1
}

// layoutChanged applies the current size to the list. With the key panel
// open the list scrolls so that the selection stays above the panel.
func (m *model) layoutChanged() {
	if m.ask != nil && !m.askFits() {
		m.ask = nil // the window shrank under it: an ask is never acted on unseen
	}
	s := m.layout()
	m.list.SetHeight(s.areas.List.H)
	if top, ok := m.panelTop(s); ok && (m.help || m.ask != nil) {
		keepAbove(&m.list, top)
	}
}

// keepAbove scrolls l so that its selection is drawn above row top, by
// selecting the item after the last visible one (which scrolls the list a
// row or two) and coming back, until it is.
func keepAbove(l *ui.List, top int) {
	cur := l.Cursor()
	for range l.Len() {
		y := -1
		for row := range l.Height() {
			if i, ok := l.ItemAt(row); ok && i == cur {
				y = row
				break
			}
		}
		if y < top {
			return
		}
		last := -1
		for row := l.Height() - 1; row >= 0 && last < 0; row-- {
			if i, ok := l.ItemAt(row); ok {
				last = i
			}
		}
		if last < 0 || last+1 >= l.Len() {
			return // nothing further down to scroll to
		}
		l.Select(last + 1)
		l.Select(cur)
	}
}

// setCategory switches the category chip, keeping the scope.
func (m *model) setCategory(c category) {
	m.filter.cat = (c + numCategories) % numCategories
	m.rebuild(selectDefault)
}

// drill scopes the list to the selected session or tab.
func (m *model) drill() {
	_, e, ok := m.selected()
	if !ok || e.pane() {
		return
	}
	m.trail = append(m.trail, crumb{filter: m.filter, selected: e.key()})
	m.filter = filter{cat: catAll, scope: e.key()}
	m.input.SetValue("")
	m.rebuild(selectTop)
}

// back returns to where the last drill started, restoring its query and
// selection. It reports whether there was anywhere to go back to.
func (m *model) back() bool {
	if len(m.trail) == 0 {
		return false
	}
	c := m.popCrumb()
	m.rebuildAt(selectKeep, c.selected)
	m.moved = true // the row the drill started from, which the user picked
	return true
}

// popCrumb restores the filter of the last trail step and removes it.
func (m *model) popCrumb() crumb {
	c := m.trail[len(m.trail)-1]
	m.trail = m.trail[:len(m.trail)-1]
	m.filter = c.filter
	m.input.SetValue(c.filter.query)
	m.input.CursorEnd()
	return c
}

// leaveScope goes all the way back out of a drill-down.
func (m *model) leaveScope() {
	for len(m.trail) > 1 {
		m.trail = m.trail[:len(m.trail)-1]
	}
	m.back()
}

// scopeLabel names the scope for the header chip: "studio › web".
func (m *model) scopeLabel() string {
	e, ok := m.cat.find(m.filter.scope)
	if !ok {
		return ""
	}
	if e.kind == ui.KindTab {
		return e.session + " " + ui.GlyphCrumb + " " + e.name
	}
	return e.name
}

// needsYou counts agents waiting for the user, for the Agents chip badge.
func (m *model) needsYou() int {
	n := 0
	if m.cat != nil {
		for _, e := range m.cat.entries {
			if e.needsYou() {
				n++
			}
		}
	}
	return n
}
