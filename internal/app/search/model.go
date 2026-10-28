package search

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rexkit/internal/find"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// origin is the pane the tool was opened from.
type origin struct{ session, window, block string }

// config is how the tool was started.
type config struct {
	api      rex.API
	origin   origin
	query    string // in the field from the start (Go to hands its query over)
	self     string // the tool's own block: never searched
	client   string // the app client that opened the tool (Remote Control)
	stateDir string // where Lua waits for answers; "" when it did not open the tool
	blur     bool   // close when another pane takes focus
	floating bool   // the tool runs in a float (so blur can close it)
	demo     bool
	home     string
	now      func() time.Time
	// spool makes a private folder for the capture handed to Scrollback.
	spool func() (string, error)
	// copy puts text on the clipboard.
	copy func(ctx context.Context, text string) error
	// close closes the tool's float.
	close func(ctx context.Context) error
	// slow lists blocks whose capture takes a while (snapshots of the
	// scanning state): their commands outlive the snapshot driver.
	slow map[string]bool
}

// captureOptions is how every pane is read: the VT export Scrollback reads
// (soft-wrapped lines joined, trailing blanks trimmed), so the line a match
// is on is the line Scrollback opens at.
var captureOptions = rex.CaptureOptions{Format: rex.FormatVT, Trim: true, Unwrap: true}

// Reading Rex: at most maxReads captures (one `rex capture` process each)
// run at once, each for at most captureTimeout (a full scrollback is up to
// 64 MiB).
const (
	maxReads       = 4
	captureTimeout = 30 * time.Second
	listTimeout    = 10 * time.Second
)

// rowKind is what a list row shows.
type rowKind int

const (
	rowPane rowKind = iota // a pane's header: glyph, name, place, count
	rowMore                // "… 134 earlier matches": unfolds the pane's hits
	rowHit                 // one matching line
	rowGap                 // the air between two panes: never selected
)

// row is one row of the list: a pane, its fold, or one of its hits.
type row struct {
	kind  rowKind
	group int
	hit   int // rowHit: index into the group's hits; rowMore: how many are folded
}

// group is a pane's hits, oldest first.
type group struct {
	pane     int   // index into model.panes
	hits     []int // matching lines, from 0
	expanded bool  // every hit is listed, not just the newest
}

// perPane is how many of a pane's newest hits are listed before the rest
// fold into an "earlier matches" row; a fold never hides just one.
const perPane = 5

// model is the search tool.
type model struct {
	cfg           config
	ctx           context.Context // ends with the tool: captures stop
	scheme        ui.Scheme
	theme         ui.Theme
	width, height int
	focused       bool
	started       time.Time

	input                         textinput.Model
	regex, matchCase, sessionOnly bool

	// Reading Rex.
	panes    []*pane
	listed   bool  // the inventory arrived
	listErr  error // why it did not
	readGen  int   // counts reads of everything (⌃R)
	readCtx  context.Context
	stopRead context.CancelFunc
	queue    []int // panes waiting to be read, in order
	reading  int   // captures running
	failed   int   // panes that could not be read

	// Searching. results are the current query's hits per pane, filled
	// in as panes are read; hits, groups and rows are shown's, which trail
	// the query while a slow search runs.
	query    find.Query
	matcher  *find.Matcher
	err      error // the query does not compile
	gen      int64
	latest   *atomic.Int64 // gen, for searches running off the UI goroutine
	results  [][]int
	ready    bool // results hold every pane read so far
	shown    find.Query
	shownM   *find.Matcher
	shownErr error
	hits     [][]int
	groups   []group
	rows     []row
	total    int // hits over every group

	list        ui.List
	preview     ui.Preview
	previewPref ui.PreviewPref
	help        bool
	helpAt      int
	toasts      ui.Toaster
	spinner     ui.Spinner
	busy        bool // a jump runs; keys wait
	// moved: the person picked the selected row (a move key, a click, an
	// unfold), so new results keep it. Until then the first match is
	// selected, whichever pane was read first: the origin's session's,
	// once it is in.
	moved bool

	// A ⏎ pressed before there was anything to act on waits for results.
	pending       bool
	pendingCommit time.Time // its commit marker (popup.TakeCommitAt); zero when typed
	pendingRev    int
}

type inventoryMsg struct {
	gen int
	inv rex.Inventory
	err error
}

type captureMsg struct {
	gen  int
	pane int
	text text
	err  error
}

func newModel(ctx context.Context, cfg config) *model {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	m := &model{cfg: cfg, ctx: ctx, scheme: ui.NewScheme(), latest: new(atomic.Int64), started: cfg.now()}
	m.theme = m.scheme.Theme()
	m.input = ui.NewInput()
	m.input.Placeholder = ""
	// A query from the start is searched once the panes are listed; a ⏎
	// held until then is for it.
	m.input.SetValue(cfg.query)
	m.input.CursorEnd()
	m.query = m.currentQuery()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.scheme.Init(), textinput.Blink, m.read())
}

// read lists every pane and starts reading them: at open, and on ⌃R.
func (m *model) read() tea.Cmd {
	if m.stopRead != nil {
		m.stopRead() // the captures of an earlier read
	}
	ctx, stop := context.WithCancel(m.ctx)
	m.readCtx, m.stopRead = ctx, stop
	m.readGen++
	m.panes, m.queue, m.reading, m.failed, m.listed, m.listErr = nil, nil, 0, 0, false, nil
	m.moved = false
	m.groups, m.rows, m.results, m.hits, m.total = nil, nil, nil, nil, 0
	m.gen++ // a search still running is for panes no longer listed
	m.latest.Store(m.gen)
	m.list.SetItems(0, nil)
	gen, api := m.readGen, m.cfg.api
	list := func() tea.Msg {
		lctx, cancel := context.WithTimeout(ctx, listTimeout)
		defer cancel()
		inv, err := api.Snapshot(lctx)
		return inventoryMsg{gen, inv, err}
	}
	return tea.Batch(m.spinner.Start(m.cfg.now()), list)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
		m.syncPreview()
	}
	if m.toasts.Update(msg) {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
	case inventoryMsg:
		return m, m.inventory(msg)
	case captureMsg:
		return m, m.captured(msg)
	case searchedMsg:
		return m, m.searched(msg)
	case doneMsg:
		return m, m.done(msg)
	case copiedMsg:
		return m, m.copied(msg)
	case stillPendingMsg:
		return m, m.stillPending(msg)
	case ui.SpinnerTickMsg:
		return m, m.spinner.Tick()
	case tea.FocusMsg:
		m.focused = true
	case tea.BlurMsg:
		if m.focused && m.cfg.floating && m.cfg.blur && !m.busy {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		return m, m.key(ui.EarlyEnterSince(m.started, m.cfg.now(), msg))
	case tea.PasteMsg:
		return m, m.edit(msg)
	case tea.MouseClickMsg:
		return m, m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		m.wheel(msg.Mouse())
	}
	return m, nil
}

// inventory lists the panes and starts reading them, maxReads at a time.
func (m *model) inventory(msg inventoryMsg) tea.Cmd {
	if msg.gen != m.readGen {
		return nil // an earlier read's
	}
	m.listed = true
	if msg.err != nil {
		m.spinner.Stop()
		m.listErr = msg.err
		return nil
	}
	m.panes = panesOf(msg.inv, m.cfg.origin, m.cfg.self)
	m.queue = make([]int, len(m.panes))
	for i := range m.panes {
		m.queue[i] = i
	}
	cmd := m.next()
	if !m.scanning() {
		m.spinner.Stop()
	}
	return tea.Batch(cmd, m.research(true))
}

// next starts reading queued panes while fewer than maxReads run.
func (m *model) next() tea.Cmd {
	var cmds []tea.Cmd
	for m.reading < maxReads && len(m.queue) > 0 {
		i := m.queue[0]
		m.queue = m.queue[1:]
		m.reading++
		cmds = append(cmds, m.capture(i))
	}
	return tea.Batch(cmds...)
}

// capture reads one pane (one `rex capture` process) and sanitizes it, off
// the UI goroutine. Closing the tool or ⌃R stops it.
func (m *model) capture(i int) tea.Cmd {
	p, api, gen, slow, ctx := m.panes[i], m.cfg.api, m.readGen, m.cfg.slow[m.panes[i].block], m.readCtx
	return func() tea.Msg {
		if slow {
			time.Sleep(2 * time.Second)
		}
		cctx, cancel := context.WithTimeout(ctx, captureTimeout)
		defer cancel()
		raw, err := api.Capture(cctx, p.session, p.block, captureOptions)
		if err != nil {
			return captureMsg{gen: gen, pane: i, err: err}
		}
		return captureMsg{gen: gen, pane: i, text: newText(raw)}
	}
}

// captured installs a pane's capture, searches it, and starts the next read.
func (m *model) captured(msg captureMsg) tea.Cmd {
	if msg.gen != m.readGen || msg.pane >= len(m.panes) {
		return nil
	}
	m.reading--
	p := m.panes[msg.pane]
	p.read = true
	if msg.err != nil {
		p.err = msg.err
		m.failed++
	} else {
		p.text = msg.text
	}
	next := m.next()
	if !m.scanning() {
		m.spinner.Stop()
	}
	if msg.err != nil || m.matcher == nil {
		// The empty page counts what was read; a held ⏎ may now have
		// nothing to act on.
		m.fitList()
		return tea.Batch(next, m.runPending())
	}
	return tea.Batch(next, m.searchPanes([]int{msg.pane}, false))
}

// scanning reports whether panes are still being read.
func (m *model) scanning() bool { return !m.listed || m.reading > 0 || len(m.queue) > 0 }

// readCount is how many panes were read so far, well or not.
func (m *model) readCount() int {
	n := 0
	for _, p := range m.panes {
		if p.read {
			n++
		}
	}
	return n
}

// currentQuery is what the field and the chips ask for.
func (m *model) currentQuery() find.Query {
	return find.Query{Text: m.input.Value(), Regex: m.regex, MatchCase: m.matchCase}
}

// research runs the query again when it changed (or anyway, with force),
// while the list still shows the last results. A ⏎ held for the query it
// replaces is dropped.
func (m *model) research(force bool) tea.Cmd {
	q := m.currentQuery()
	if q == m.query && !force {
		return nil
	}
	if q != m.query && m.pending {
		m.pending = false
		if !m.pendingCommit.IsZero() {
			m.stay("enter")
		}
	}
	if q != m.query {
		m.moved = false // a new query: its first match leads
	}
	m.query = q
	m.gen++
	m.latest.Store(m.gen)
	m.matcher, m.err = q.Compile()
	m.results = make([][]int, len(m.panes))
	m.ready = false
	if m.matcher == nil {
		m.ready, m.shown, m.shownM, m.shownErr, m.hits = true, q, nil, m.err, m.results
		m.rebuild()
		return nil
	}
	var read []int
	for i, p := range m.panes {
		if p.read && p.err == nil {
			read = append(read, i)
		}
	}
	return m.searchPanes(read, true)
}

// syncLines is how many lines a literal search reads on the UI goroutine
// (about 10 ms); past that it runs off it, as a regular expression always
// does.
const syncLines = 200_000

// searchPanes searches the given panes for the current query: at once for
// a literal one over few lines, else off the UI goroutine. all says they
// are every pane read so far.
func (m *model) searchPanes(idx []int, all bool) tea.Cmd {
	texts := make([]text, len(idx))
	lines := 0
	for k, i := range idx {
		texts[k] = m.panes[i].text
		lines += len(texts[k].plain)
	}
	gen, latest, matcher := m.gen, m.latest, m.matcher
	stale := func() bool { return latest.Load() != gen }
	run := func() tea.Msg {
		hits, ok := searchTexts(matcher, texts, stale)
		if !ok {
			return nil
		}
		return searchedMsg{gen: gen, panes: idx, hits: hits, all: all}
	}
	if !matcher.Slow() && lines <= syncLines {
		return m.searched(run().(searchedMsg))
	}
	return run
}

// searched takes a search's hits, and shows them once the current query's
// hits are in for every pane read.
func (m *model) searched(msg searchedMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil // a query since replaced
	}
	for k, i := range msg.panes {
		if i < len(m.results) {
			m.results[i] = msg.hits[k]
			if m.results[i] == nil {
				m.results[i] = []int{} // searched, no hits (settled)
			}
		}
	}
	if msg.all {
		m.ready = true
	}
	if !m.ready {
		return nil
	}
	m.shown, m.shownM, m.shownErr, m.hits = m.query, m.matcher, m.err, m.results
	m.rebuild()
	return m.runPending()
}

// inScope reports whether p is searched: every pane, or the origin's
// session's with "this session".
func (m *model) inScope(p *pane) bool {
	return !m.sessionOnly || p.session == m.cfg.origin.session
}

// rebuild lays the results out as groups and rows, keeping the line (or
// pane) the person selected when it is still there, else selecting the
// first match, and each unfolded pane unfolded.
func (m *model) rebuild() {
	keepBlock, keepLine := "", -1
	if r, ok := m.selected(); ok && m.moved {
		g := m.groups[r.group]
		keepBlock = m.panes[g.pane].block
		if r.kind == rowHit {
			keepLine = g.hits[r.hit]
		}
	}
	expanded := map[string]bool{}
	for _, g := range m.groups {
		if g.expanded && g.pane < len(m.panes) {
			expanded[m.panes[g.pane].block] = true
		}
	}
	m.groups, m.total = m.groups[:0], 0
	for i, hits := range m.hits {
		if len(hits) == 0 || !m.inScope(m.panes[i]) {
			continue
		}
		m.groups = append(m.groups, group{pane: i, hits: hits, expanded: expanded[m.panes[i].block]})
		m.total += len(hits)
	}
	m.buildRows()
	sel := -1
	for i, r := range m.rows {
		g := m.groups[r.group]
		if m.panes[g.pane].block != keepBlock {
			continue
		}
		if (r.kind == rowHit && g.hits[r.hit] == keepLine) || (keepLine < 0 && r.kind == rowPane) {
			sel = i
			break
		}
	}
	if sel < 0 {
		sel = slices.IndexFunc(m.rows, func(r row) bool { return r.kind == rowHit })
	}
	m.list.Select(max(0, sel))
	m.fitList()
	m.syncPreview()
}

// buildRows lays the groups out as rows: each pane, then (when it has more
// than perPane+1 hits and is folded) its fold and newest hits, with a row
// of air before every pane after the first.
func (m *model) buildRows() {
	m.rows = m.rows[:0]
	for gi, g := range m.groups {
		if gi > 0 {
			m.rows = append(m.rows, row{kind: rowGap, group: gi})
		}
		m.rows = append(m.rows, row{kind: rowPane, group: gi})
		from := 0
		if !g.expanded && len(g.hits) > perPane+1 {
			from = len(g.hits) - perPane
			m.rows = append(m.rows, row{kind: rowMore, group: gi, hit: from})
		}
		for hi := from; hi < len(g.hits); hi++ {
			m.rows = append(m.rows, row{kind: rowHit, group: gi, hit: hi})
		}
	}
	m.list.SetItems(len(m.rows), nil)
}

// settled reports whether the first match can no longer change: every
// pane listed before the selected row's (the origin's session's come
// first) is read and searched, so no match can still arrive above it.
func (m *model) settled() bool {
	r, ok := m.selected()
	if !ok {
		return false
	}
	for i, p := range m.panes[:m.groups[r.group].pane] {
		if m.inScope(p) && (!p.read || p.err == nil && (i >= len(m.results) || m.results[i] == nil)) {
			return false
		}
	}
	return true
}

func (m *model) selected() (row, bool) {
	i := m.list.Cursor()
	if i < 0 || i >= len(m.rows) {
		return row{}, false
	}
	return m.rows[i], true
}
