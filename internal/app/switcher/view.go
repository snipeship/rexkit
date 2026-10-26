package switcher

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/ui"
)

// title is the tool's name, at the start of the header.
const title = "Go to"

// listShare gives the list more than the default width beside the preview:
// rows carry names, context, status and age.
const listShare = 55

// screen is where everything is drawn, for the current size and state.
type screen struct {
	frame  ui.Frame
	inner  ui.Rect // the frame's content area
	areas  ui.Areas
	header ui.Header
	chips  []category // the category of each header chip
	hits   ui.HeaderHits
	lines  []string // the rendered header
}

func (m *model) layout() screen {
	f := ui.FloatFrame(m.width, m.height, m.carded)
	s := screen{frame: f, inner: f.Inner(m.width, m.height)}
	s.areas = ui.Arrange(s.inner.W, s.inner.H, ui.LayoutOptions{Preview: m.pv.pref, ListShare: listShare, Card: f.Card, ListRows: m.list.Rows()})
	s.header, s.chips = m.header(s.areas.Header.W)
	switch {
	case s.areas.Banded():
		s.lines, s.hits = m.theme.HeaderBand(s.header, s.areas.Header.W)
	case s.areas.Header.H > 0:
		var line string
		line, s.hits = m.theme.Header(s.header, s.areas.Header.W)
		s.lines = []string{line}
	default:
		s.hits.Cursor = -1
	}
	return s
}

func (m *model) View() tea.View {
	v := tea.View{AltScreen: true, MouseMode: tea.MouseModeCellMotion, ReportFocus: true}
	if !m.scheme.Settled() || m.width <= 0 || m.height <= 0 {
		return v // hold the first frame until the colour scheme is known (≤120ms)
	}
	s := m.layout()
	v.Content = m.render(s)
	if m.ask != nil {
		if _, at, cx, cy := m.askPanel(s.areas); cx >= 0 && !m.busy {
			c := tea.NewCursor(s.inner.X+s.areas.Body.X+at.X+cx, s.inner.Y+s.areas.Body.Y+at.Y+cy)
			c.Shape, c.Color = tea.CursorBar, m.theme.C.Accent
			v.Cursor = c
		}
	} else if s.hits.Cursor >= 0 && !m.help && !m.busy {
		c := tea.NewCursor(s.inner.X+s.areas.Header.X+s.hits.Cursor, s.inner.Y+s.areas.HeaderLine())
		c.Shape, c.Color = tea.CursorBar, m.theme.C.Accent
		v.Cursor = c
	}
	return v
}

func (m *model) render(s screen) string {
	t := m.theme
	a := s.areas
	body := m.statePage(a.Body.W, a.Body.H)
	if body == nil {
		parts := []ui.Part{{Rect: a.InBody(a.List), Lines: m.list.View(t, a.List.W, m.drawRow(a.List.W), nil)}}
		if a.PreviewVisible() {
			parts = append(parts, ui.Part{Rect: a.InBody(a.Preview), Lines: m.previewView(a)})
		}
		body = ui.Compose(a.Body.W, a.Body.H, parts...)
	}
	if m.help {
		if panel, at, _ := m.keyPanel(a); panel != nil {
			body = t.Raise(body, panel, at)
		}
	}
	tr := m.tray()
	if m.ask != nil {
		if panel, at, _, _ := m.askPanel(a); panel != nil {
			body = t.Raise(body, panel, at)
		}
		tr = m.askTray()
	}
	tray := t.Tray(tr, a.Footer.W, a.Footer.H)
	return s.frame.Render(t, m.width, m.height, s.lines, body, tray)
}

// keyPanel draws the key panel, says where it goes in the body and how far
// it scrolls. Without a preview beside the list it spans the body, over the
// list's last rows.
func (m *model) keyPanel(a ui.Areas) ([]string, ui.Rect, int) {
	return m.theme.KeyPanel(keySheet, a.Body.W, a.Body.H, m.panelPlace(a), m.helpAt)
}

// panelPlace is where the key panel and an ask's sheet go: in the corner
// over the preview beside the list, else across the body.
func (m *model) panelPlace(a ui.Areas) ui.PanelPlace {
	if a.Mode != ui.PreviewSide {
		return ui.PanelSpan
	}
	return ui.PanelCorner
}

// panelTop is the first row of the list the key panel (or an ask's sheet)
// covers or shades (its shadow reaches the row above it), when it covers
// the list at all (without a preview beside it).
func (m *model) panelTop(s screen) (int, bool) {
	a := s.areas
	_, at, _ := m.keyPanel(a)
	if m.ask != nil {
		_, at, _, _ = m.askPanel(a)
	}
	list := a.InBody(a.List)
	if at.Empty() || at.X >= list.X+list.W || at.Y >= list.Y+list.H {
		return 0, false
	}
	return max(1, at.Y-list.Y-1), true
}

// panelRect is where the key panel is, in content coordinates.
func (m *model) panelRect(s screen) ui.Rect {
	a := s.areas
	_, at, _ := m.keyPanel(a)
	return at.Offset(a.Body.X, a.Body.Y)
}

// compactHeader is the header width below which only the active category
// chip and Agents are shown: ⇥ still cycles through all.
const compactHeader = 90

func (m *model) header(w int) (ui.Header, []category) {
	h := ui.Header{
		Title: title,
		Query: ui.Query{
			Value: m.input.Value(), Cursor: m.input.Position(), Focused: !m.busy && m.ask == nil,
			Placeholder: m.placeholder(), RealCursor: true,
		},
	}
	// The chips are there from the first frame, while Rex is read or can't
	// be: the header keeps its shape when the places arrive.
	if m.cat != nil {
		h.Scope = m.scopeLabel()
	}
	waiting := m.needsYou()
	var cats []category
	for c := range numCategories {
		on := c == m.filter.cat
		if w < compactHeader && !on && c != catAgents {
			continue
		}
		chip := ui.Chip{Kind: ui.ChipTab, Label: categoryNames[c], On: on}
		if c == catAgents {
			// The badge's cell is kept while none wait (or Rex is still
			// being read): the field does not shrink when the count comes.
			chip.Badged = true
			if waiting > 0 {
				chip.Badge = strconv.Itoa(waiting)
			}
		}
		h.Chips = append(h.Chips, chip)
		cats = append(cats, c)
	}
	return h, cats
}

func (m *model) placeholder() string {
	if m.filter.scope != "" {
		if e, ok := m.cat.find(m.filter.scope); ok {
			return "Search in " + e.name
		}
	}
	if m.filter.cat != catAll {
		return "Search " + strings.ToLower(categoryNames[m.filter.cat])
	}
	return "Search sessions, tabs, panes, agents"
}

// tray is the bottom of the card: the count (or what the data is doing) on
// the left, the keys for what is selected on the right.
func (m *model) tray() ui.Tray {
	t := m.theme
	now := m.now()
	tr := ui.Tray{Meta: m.meta(), Hints: m.hints(), Toast: m.toast.Current()}
	switch {
	case m.going.Visible(now):
		tr.Busy = t.Spinning("Going"+ui.Ellipsis, now)
	case m.cat != nil && m.loading.Visible(now):
		tr.Busy = t.Spinning("Updating", now)
	}
	return tr
}

// meta counts the places listed ("26 places", "2 of 26"), and says how old
// they are when the last refresh failed, or that Rex can't be reached.
func (m *model) meta() ui.Line {
	t := m.theme
	if m.cat == nil {
		if m.loadErr != "" {
			return ui.Styled("offline", t.Subtle)
		}
		return nil
	}
	total := m.listing.total
	shown := total
	if !m.switcherView() {
		shown = len(m.listing.rows)
	}
	line := t.MetaCount(shown, total, "place")
	if m.stale && !m.updatedAt.IsZero() {
		line = append(line, ui.Space(3), ui.Span{Text: ui.GlyphWarn + " ", Style: t.Warn},
			ui.Span{Text: "from " + ui.RelTimeAt(m.updatedAt, m.now()) + " ago", Style: t.Muted})
	}
	return line
}

// hints are the tray's key hints for what is selected now: the primary one
// says where ⏎ goes when it leaves this session. Without places (Rex is
// being read, can't be, or has nothing else open) there is nothing to
// filter or preview: the ways out are left.
func (m *model) hints() []ui.Hint {
	keys := ui.Hint{Key: "?", Label: "keys"}
	switch {
	case m.cat == nil && m.loadErr != "":
		return []ui.Hint{{Key: "ctrl+r", Label: "retry", Primary: true}, {Key: "esc", Label: "close"}, keys}
	case m.cat == nil || len(m.cat.entries) == 0:
		return []ui.Hint{{Key: "esc", Label: "close"}, keys}
	}
	_, e, ok := m.selected()
	var hints []ui.Hint
	switch {
	case ok:
		primary := ui.Hint{Key: "enter", Label: "go", Primary: true}
		if e.sessionID != m.opts.origin.sessionID {
			primary.Outcome = "to " + e.sessionName()
		}
		hints = append(hints, primary)
	case m.input.Value() != "":
		hints = append(hints, ui.Hint{Key: "backspace", Label: "edit", Primary: true}) // nothing matches: the way out is the query
	}
	if m.stale && m.cat != nil { // without a catalog the error page offers the retry
		hints = append(hints, ui.Hint{Key: "ctrl+r", Label: "refresh"})
	}
	if len(m.trail) > 0 {
		if ok && !e.pane() {
			hints = append(hints, ui.Hint{Key: "right", Label: "open"})
		}
		hints = append(hints, ui.Hint{Key: "left", Label: "back"})
	}
	hints = append(hints, ui.Hint{Key: "tab", Label: "filter"})
	if m.list.Len() > 0 {
		hints = append(hints, ui.Hint{Key: "ctrl+/", Label: "preview"})
	}
	return append(hints, keys)
}

// statePage is the body when there is no list to show: Rex is still being
// read or could not be, nothing is open, or nothing matches. It is nil when
// there are rows.
func (m *model) statePage(w, h int) []string {
	if m.list.Len() > 0 {
		return nil
	}
	t := m.theme
	bold := t.Text.Bold(true)
	escClose := ui.Hint{Key: "esc", Label: "close"}
	switch {
	case m.cat == nil && m.loadErr != "":
		return t.ErrorState("Can't reach Rex", m.loadErr, []ui.Hint{{Key: "ctrl+r", Label: "retry"}, escClose}, w, h)
	case m.cat == nil && m.loading.Visible(m.now()):
		return t.LoadingState("Reading Rex"+ui.Ellipsis, m.now(), w, h)
	case m.cat == nil:
		return t.EmptyState(nil, nil, nil, w, h) // the spinner waits a moment, so a fast load never flashes it
	case len(m.cat.entries) == 0:
		return t.EmptyState(ui.Styled("Nothing else is open", bold),
			ui.Styled("Sessions, tabs and panes show up here once there are others.", t.Muted),
			[]ui.Hint{escClose}, w, h)
	}
	var ways []ui.Hint
	q := m.input.Value()
	if q != "" {
		ways = append(ways, ui.Hint{Key: "backspace", Label: "edit the search"})
	}
	if m.filter.cat != catAll {
		ways = append(ways, ui.Hint{Key: "tab", Label: "search all kinds"})
	}
	ways = append(ways, escClose)
	if q == "" {
		none := "Nothing here"
		if m.filter.cat != catAll {
			none = "No " + strings.ToLower(categoryNames[m.filter.cat])
		}
		return t.EmptyState(ui.Styled(none, bold), nil, ways, w, h)
	}
	detail := "Names, folders, programs and tasks are all searched."
	if m.filter.scope != "" {
		detail = "Only " + m.scopeLabel() + " is searched."
	}
	return t.EmptyState(t.Quoted("No matches for ", q), ui.Styled(detail, t.Muted), ways, w, h)
}

// previewEntry is the entry whose pane the preview shows.
func (m *model) previewEntry() (entry, bool) {
	if m.cat == nil {
		return entry{}, false
	}
	return m.cat.find(m.pv.shown)
}

// previewView draws the preview: the title row (glyph, name, where it is;
// the status tag and age on the right), the context row, the metadata grid,
// and the pane's live tail in the well.
func (m *model) previewView(a ui.Areas) []string {
	w, h := a.Preview.W, a.Preview.H
	e, ok := m.previewEntry()
	if ok {
		if sel, found := m.cat.find(m.selectedKey()); found && sel.blockID == e.blockID {
			e = sel // a session or tab shows the pane it would focus: describe the session or tab
		}
	}
	v := &m.pv.view
	v.Title, v.Right = m.previewTitle(e, ok), m.previewRight(e, ok)
	v.Grid = m.previewGrid(e, ok)
	v.Context = m.previewContext(e, ok, len(v.Grid) > 0 && h >= ui.MetaGridMinRows)
	return v.View(m.theme, w, h)
}

func (m *model) previewTitle(e entry, ok bool) ui.Line {
	if !ok {
		return nil
	}
	t := m.theme
	line := ui.Line{t.Glyph(e.kind), ui.Space(1)}
	line = append(line, t.Escaped(e.name, e.nameEsc, t.Text.Bold(true))...)
	where := e.path()
	if !e.pane() {
		where = e.counts()
	}
	if where != "" {
		line = append(line, ui.Space(2), ui.Span{Text: where, Style: t.Muted})
	}
	return line
}

// previewRight is the status tag (blocked, failed, working) and the age,
// with the spinner while a capture is late.
func (m *model) previewRight(e entry, ok bool) ui.Line {
	t := m.theme
	now := m.now()
	var line ui.Line
	if ok {
		s, word := e.status, ""
		if !e.pane() {
			s, word = e.summary()
		}
		if s == ui.StatusBlocked || s == ui.StatusFailed || s == ui.StatusWorking {
			line = t.Tag(s, word, now)
		}
		if age := ui.RelTimeAt(e.since, now); e.pane() && e.status != ui.StatusNone && age != "" {
			line = append(line, ui.Span{Text: "  " + age, Style: t.Subtle})
		}
	}
	if m.pv.spin.Visible(now) {
		line = append(line, ui.Span{Text: "  " + ui.SpinnerFrame(now), Style: t.OK})
	}
	return line
}

// previewContext is the row under the title: the pane's folder, and its
// task when the grid does not show it.
func (m *model) previewContext(e entry, ok, grid bool) ui.Line {
	if !ok {
		return nil
	}
	if p, found := m.cat.find(e.blockID); found && !e.pane() {
		e = p // a session or tab: describe the pane it would focus
	}
	t := m.theme
	var line ui.Line
	add := func(l ui.Line) {
		if len(l) == 0 || l[0].Text == "" {
			return
		}
		if len(line) > 0 {
			line = append(line, ui.Span{Text: " " + ui.Dot + " ", Style: t.Subtle})
		}
		line = append(line, l...)
	}
	add(t.Escaped(e.cwd, e.cwdEsc, t.Subtle))
	if !grid {
		if e.task != "" && e.task != e.name {
			add(t.Escaped(e.task, e.taskEsc, t.Subtle))
		}
		add(ui.Styled(e.exit, t.Subtle))
	}
	return line
}

// previewGrid is the metadata grid: an agent's folder, program, task and
// exit; a session's or tab's contents and the pane it would focus. Panes
// have none: their context row says it all.
func (m *model) previewGrid(e entry, ok bool) []ui.MetaRow {
	if !ok {
		return nil
	}
	t := m.theme
	var rows []ui.MetaRow
	add := func(label string, value ui.Line) {
		if len(value) > 0 && value[0].Text != "" {
			rows = append(rows, ui.MetaRow{Label: label, Value: value})
		}
	}
	switch e.kind {
	case ui.KindAgent:
		add("Directory", t.Escaped(e.cwd, e.cwdEsc, t.Text))
		add("Program", t.Escaped(e.program, e.programEsc, t.Text))
		if e.task != e.name { // else the title says it
			add("Task", t.Escaped(e.task, e.taskEsc, t.Text))
		}
		add("Exit", ui.Styled(strings.TrimPrefix(e.exit, "exit "), t.Err))
	case ui.KindSession, ui.KindTab:
		if e.kind == ui.KindSession {
			add("Tabs", ui.Styled(strconv.Itoa(e.tabs), t.Text))
		}
		add("Panes", ui.Styled(strconv.Itoa(e.panes), t.Text))
		if p, found := m.cat.find(e.blockID); found {
			focus := t.Escaped(p.name, p.nameEsc, t.Text)
			if e.kind == ui.KindSession {
				focus = append(ui.Line{{Text: p.tab + " " + ui.GlyphCrumb + " ", Style: t.Muted}}, focus...)
			}
			add("Focus", focus)
		}
	}
	return rows
}
