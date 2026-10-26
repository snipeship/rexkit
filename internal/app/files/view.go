package files

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// screen is the arrangement of the tool: the float's frame, its content
// area, and the areas inside it (relative to the content area).
type screen struct {
	frame ui.Frame
	inner ui.Rect
	areas ui.Areas
}

// layout arranges the tool for the current size: on a card when the stage
// draws one, banded when there is room for it (ui.FloatFrame).
func (m *model) layout() screen {
	frame := ui.FloatFrame(m.width, m.height, m.carded)
	inner := frame.Inner(m.width, m.height)
	areas := ui.Arrange(inner.W, inner.H, ui.LayoutOptions{Preview: m.previewPref, Card: frame.Card, ListRows: m.list.Rows()})
	return screen{frame: frame, inner: inner, areas: areas}
}

// relayout applies a new size or arrangement: the list keeps its selection
// visible, and the preview loads or re-fits.
func (m *model) relayout() tea.Cmd {
	m.fitList()
	return tea.Batch(m.syncPreview(), m.requestCounts(), m.placePicture())
}

// fitList sizes the list to where it is drawn, so the selection stays in
// view there.
func (m *model) fitList() {
	a := m.layout().areas
	m.list.SetHeight(max(1, listRect(a, m.keyPanelAt(a)).H))
}

// listRect is where the list is drawn: its area, less what the key panel
// at covers (empty while it is closed). A panel over the end of the list
// leaves the rows above it; one about as tall as the list leaves the
// columns to its left, so the names (and the list's ▲ ▼ counts) stay
// readable either way. A panel that leaves too little of either simply
// covers the list.
func listRect(a ui.Areas, at ui.Rect) ui.Rect {
	r := a.List
	if at.Empty() || at.X >= r.X+r.W || at.Y >= r.Y+r.H {
		return r
	}
	switch above, left := at.Y-r.Y, at.X-r.X-1; {
	case above >= minListRows:
		r.H = above
	case left >= minListCols:
		r.W = left
	}
	return r
}

// The least of the list worth keeping beside the key panel: rows above it,
// or columns to its left.
const (
	minListRows = 3
	minListCols = 20
)

// setHelp opens or closes the key panel.
func (m *model) setHelp(on bool) {
	m.help, m.helpAt = on, 0
	m.fitList()
}

// wellSliver is the most of the preview's well the key panel leaves beside
// it: closer to the well's left edge than this, it covers the well whole.
const wellSliver = 4

// keyPanel draws the key panel, says where it goes in the body and how far
// it scrolls (nil when the body is too small for it). Beside the list it
// keeps to the preview's side, so the list's columns stay readable;
// without a preview beside the list it spans the body, over the list's
// last rows.
func (m *model) keyPanel(a ui.Areas) ([]string, ui.Rect, int) {
	if a.Mode == ui.PreviewSide {
		// KeyPanel keeps two cells of air left of the panel: from x on, it
		// starts no further left than the preview.
		x := a.Preview.X - a.Body.X - 2
		well := x + 3 // the well's first column
		if panel, at, more := m.theme.KeyPanel(helpKeys, a.Body.W-x, a.Body.H, ui.PanelCorner, m.helpAt); panel != nil {
			if at.X+x-well > wellSliver {
				return panel, at.Offset(x, 0), more
			}
			// Not a sliver of the well beside it: it covers the well.
			panel, at, more = m.theme.KeyPanel(helpKeys, a.Body.W-well+1, a.Body.H, ui.PanelFill, m.helpAt)
			return panel, at.Offset(well-1, 0), more
		}
	}
	return m.theme.KeyPanel(helpKeys, a.Body.W, a.Body.H, ui.PanelSpan, m.helpAt)
}

// keyPanelAt is where the open key panel goes, in content coordinates:
// empty while it is closed, or when the body is too small for it.
func (m *model) keyPanelAt(a ui.Areas) ui.Rect {
	if !m.help {
		return ui.Rect{}
	}
	_, at, _ := m.keyPanel(a)
	return at.Offset(a.Body.X, a.Body.Y)
}

// View draws the tool.
func (m *model) View() tea.View {
	v := tea.View{AltScreen: true, MouseMode: tea.MouseModeCellMotion, ReportFocus: true}
	if m.done || !m.scheme.Settled() || m.width <= 0 || m.height <= 0 {
		return v // hold the first frame until the palette is known
	}
	s := m.layout()
	a, t := s.areas, m.theme
	var header, tray []string
	hits := ui.HeaderHits{Cursor: -1}
	if !a.Header.Empty() {
		header, hits, _ = m.headerView(a.Header.W, a.Banded())
	}
	var panel []string
	var at ui.Rect // in the body
	if m.help {
		panel, at, _ = m.keyPanel(a) // once a frame: it is the costliest part
	}
	body := m.body(a, at.Offset(a.Body.X, a.Body.Y))
	if m.help {
		if panel != nil {
			body = t.Raise(body, panel, at)
		}
	} else if hits.Cursor >= 0 {
		v.Cursor = tea.NewCursor(s.inner.X+a.Header.X+hits.Cursor, s.inner.Y+a.HeaderLine())
		v.Cursor.Shape, v.Cursor.Color = tea.CursorBar, t.C.Accent
	}
	if !a.Footer.Empty() {
		tray = t.Tray(m.tray(), a.Footer.W, a.Footer.H)
	}
	v.Content = s.frame.Render(t, m.width, m.height, header, body, tray)
	return v
}

// headerView draws the header w cells wide, as a band or a compact row,
// with its hit rects (in the coordinates of its text row) and an action
// for each chip. The git badge gives up its counts before it goes whole.
func (m *model) headerView(w int, banded bool) ([]string, ui.HeaderHits, []func() tea.Cmd) {
	h, actions := m.header()
	t := m.theme
	if m.pathRefused() {
		t.C.Field = t.C.ErrField // the typed path leads nowhere
	}
	draw := func(h ui.Header) ([]string, ui.HeaderHits) {
		if banded {
			return t.HeaderBand(h, w)
		}
		line, hits := t.Header(h, w)
		return []string{line}, hits
	}
	lines, hits := draw(h)
	text := lines[0]
	if banded {
		text = lines[1]
	}
	if branch := m.gitInfo(false); len(h.Info) > len(branch) && !strings.Contains(text, ui.GlyphBranch) {
		h.Info = branch
		lines, hits = draw(h)
	}
	return lines, hits, actions
}

// header describes the header, with an action for each chip: the
// breadcrumb and git badge (or "Path" while a path is typed), the field,
// and the toggles.
func (m *model) header() (ui.Header, []func() tea.Cmd) {
	hidden := ui.Chip{Kind: ui.ChipToggle, Label: "hidden", Key: "alt+.", On: m.hidden}
	if m.path.active {
		return ui.Header{
			Title: "Path",
			Query: m.fieldQuery(m.path.input.Value(), m.path.input.Position(), "~/path/to/folder"),
			Chips: []ui.Chip{hidden},
		}, []func() tea.Cmd{m.toggleHidden}
	}
	var labels []string
	for _, c := range crumbs(m.dir, m.cfg.home) {
		labels = append(labels, c.label)
	}
	h := ui.Header{
		Crumbs: labels,
		Info:   m.gitInfo(true),
		Query:  m.fieldQuery(m.query.Value(), m.query.Position(), "Search this folder"),
		Chips: []ui.Chip{
			{Kind: ui.ChipToggle, Label: "all files", Key: "ctrl+f", On: m.recursive},
			hidden,
		},
	}
	if m.recursive {
		h.Query.Placeholder = "Search all files below"
	}
	return h, []func() tea.Cmd{m.toggleRecursive, m.toggleHidden}
}

// fieldQuery shows a field's text sanitized (pasted text could hold
// controls), with the cursor where it is in the sanitized text.
func (m *model) fieldQuery(value string, cursor int, placeholder string) ui.Query {
	runes := []rune(value)
	before := sanitize.Name(string(runes[:min(cursor, len(runes))]))
	return ui.Query{
		Value:       before + sanitize.Name(string(runes[min(cursor, len(runes)):])),
		Cursor:      len([]rune(before)),
		Focused:     true,
		Placeholder: placeholder,
		RealCursor:  true,
	}
}

// gitInfo is the git badge of a work tree, "⎇ main ↑1 ↓2 ●3" (or just
// "⎇ main" without counts); nothing elsewhere.
func (m *model) gitInfo(counts bool) ui.Line {
	if !m.inRepo || m.git.branch == "" {
		return nil
	}
	t := m.theme
	branch, escapes := sanitize.NameSpans(m.git.branch)
	line := ui.Line{{Text: ui.GlyphBranch + " ", Style: t.Subtle}}
	line = append(line, t.Escaped(branch, escapes, t.Muted)...)
	if !counts {
		return line
	}
	if m.git.ahead > 0 {
		line = append(line, ui.Span{Text: " ↑" + strconv.Itoa(m.git.ahead), Style: t.Accent})
	}
	if m.git.behind > 0 {
		line = append(line, ui.Span{Text: " ↓" + strconv.Itoa(m.git.behind), Style: t.Accent})
	}
	if m.git.dirty > 0 {
		line = append(line, ui.Span{Text: " ●" + strconv.Itoa(m.git.dirty), Style: t.Warn})
	}
	return line
}

// body draws the area between the header and the tray: the list (beside
// the key panel at, if open) and the preview, or a page that explains an
// empty list.
func (m *model) body(a ui.Areas, panel ui.Rect) []string {
	if len(m.rows) == 0 {
		return m.emptyPage(a.Body.W, a.Body.H)
	}
	list := listRect(a, panel)
	parts := []ui.Part{{Rect: a.InBody(list), Lines: m.listView(list.W)}}
	if a.PreviewVisible() {
		parts = append(parts, ui.Part{Rect: a.InBody(a.Preview), Lines: m.preview.View(m.theme, a.Preview.W, a.Preview.H)})
	}
	return ui.Compose(a.Body.W, a.Body.H, parts...)
}

// emptyPage explains an empty list across the whole body,
// with the keys that lead somewhere: a folder that can't be read, one
// still loading, no completions, no matches, an empty folder.
func (m *model) emptyPage(w, h int) []string {
	t := m.theme
	switch {
	case m.data.err != nil && !m.path.active:
		ways := []ui.Hint{{Key: "left", Label: "back"}, {Key: "ctrl+r", Label: "retry"}}
		return t.ErrorState("Can't read this folder", fsErrorText(m.data.err), ways, w, h)
	case m.path.active:
		if !m.path.listed {
			return ui.Compose(w, h) // a folder lists quickly: no spinner
		}
		dir, prefix := m.typedPath()
		ways := []ui.Hint{{Key: "backspace", Label: "edit the path"}, {Key: "esc", Label: "back to the list"}}
		where := displayPath(dir, m.cfg.home)
		if prefix == "" {
			title := ui.Styled("Nothing in ", t.Text.Bold(true)).Append(ui.Span{Text: where, Style: t.Accent.Bold(true)})
			return t.EmptyState(title, nil, ways, w, h)
		}
		detail := ui.Styled("Names in "+where+" are offered as you type.", t.Muted)
		return t.EmptyState(t.Quoted("Nothing starts with ", prefix), detail, ways, w, h)
	case m.spinner.Active() && len(m.data.entries) == 0:
		if !m.spinner.Visible(m.cfg.now()) {
			return ui.Compose(w, h)
		}
		return t.LoadingState("Reading this folder…", m.cfg.now(), w, h)
	case !m.pattern.Empty():
		detail, ways := "Only this folder is searched.", []ui.Hint{{Key: "ctrl+f", Label: "search all files below"}}
		if m.recursive {
			detail, ways = "All files below this folder are searched.", []ui.Hint{{Key: "backspace", Label: "edit the search"}}
		}
		if !m.hidden {
			ways = append(ways, ui.Hint{Key: "alt+.", Label: "show hidden"})
		}
		return t.EmptyState(t.Quoted("No matches for ", m.query.Value()), ui.Styled(detail, t.Muted), ways, w, h)
	}
	ways := []ui.Hint{{Key: "left", Label: "up"}, {Key: "ctrl+f", Label: "all files below"}}
	if !m.hidden {
		ways = append(ways, ui.Hint{Key: "alt+.", Label: "show hidden"})
	}
	return t.EmptyState(ui.Styled("Nothing here", t.Text.Bold(true)), ui.Styled("This folder is empty.", t.Muted), ways, w, h)
}

// tray is the bottom of the tool: the count (or what is going on) on the
// left; on the right ⏎ with what it will open, ⌃E when a file is selected,
// and "? keys", which leads to every other key.
func (m *model) tray() ui.Tray {
	t, now := m.theme, m.cfg.now()
	tr := ui.Tray{Meta: m.meta(), Toast: m.toast.Current()}
	switch {
	case m.opening(now):
		tr.Busy = t.Spinning("Opening…", now)
	case m.spinner.Visible(now) && len(m.rows) > 0:
		tr.Busy = t.Spinning("Updating", now)
	}
	if m.path.active {
		// A path that leads nowhere is said once, on the page and by the
		// field's colour; the tray offers the ways out.
		if e, ok := m.pathGoal(); ok {
			tr.Hints = append(tr.Hints, ui.Hint{Key: "enter", Label: "open", Path: displayPath(e.path, m.cfg.home), Primary: true})
		} else if m.pathRefused() {
			tr.Hints = append(tr.Hints, ui.Hint{Key: "backspace", Label: "edit", Primary: true})
		}
		if len(m.rows) > 0 {
			tr.Hints = append(tr.Hints, ui.Hint{Key: "tab", Label: "complete"})
		}
		tr.Hints = append(tr.Hints, ui.Hint{Key: "esc", Label: "back"}, ui.Hint{Key: "?", Label: "keys"})
		return tr
	}
	if m.data.err != nil {
		// Nothing to act on in a folder that can't be read: the ways out.
		tr.Hints = []ui.Hint{{Key: "ctrl+r", Label: "retry", Primary: true}, {Key: "esc", Label: "close"}, {Key: "?", Label: "keys"}}
		return tr
	}
	tg := m.target()
	tr.Hints = []ui.Hint{{Key: "enter", Label: "new tab", Outcome: "in", Path: displayPath(tg.dir, m.cfg.home), Primary: true}}
	switch {
	case tg.file && m.opensInApp(tg.path):
		tr.Hints = append(tr.Hints, ui.Hint{Key: "ctrl+x", Label: "open"})
	case tg.file:
		tr.Hints = append(tr.Hints, ui.Hint{Key: "ctrl+e", Label: "edit"})
	}
	tr.Hints = append(tr.Hints, ui.Hint{Key: "?", Label: "keys"})
	return tr
}

// opensInApp reports whether the file at path is one for its default app
// rather than the editor: an image or a binary file, as its preview found,
// or by its name while the preview has not looked.
func (m *model) opensInApp(path string) bool {
	if d := m.previewDoc; d.path == path {
		return d.opaque
	}
	return isImage(path)
}

// meta is the tray's count: the folder's items ("0 items" in an empty
// one), "matches of searched" while a query is typed, the completions in
// the path field, or "unreadable"; and a warning when a recursive scan
// stopped short.
func (m *model) meta() ui.Line {
	t := m.theme
	var meta ui.Line
	switch {
	case m.path.active:
		if len(m.rows) > 0 {
			meta = t.MetaCount(len(m.rows), len(m.rows), "item")
		}
		return meta
	case m.data.err != nil:
		meta = ui.Styled("unreadable", t.Subtle)
	case !m.pattern.Empty():
		meta = t.MetaCount(len(m.rows), m.searched, "item")
	case len(m.data.entries) > 0 || !m.spinner.Active():
		meta = t.MetaCount(len(m.data.entries), len(m.data.entries), "item")
	}
	if m.data.limited {
		if len(meta) > 0 {
			meta = append(meta, ui.Space(3))
		}
		meta = append(meta, ui.Span{Text: ui.GlyphWarn + " limited", Style: t.Warn})
	}
	return meta
}

// columns is the row layout: glyph, name, git badge, size or item count, age.
var columns = ui.Columns{
	{Width: 1},
	{Flex: true, Min: 12, Gap: 1},
	{Max: 1, Gap: 2},
	{Max: 10, Gap: 2, Right: true, Drop: 2},
	{Max: 6, Gap: 2, Right: true, Drop: 1},
}

func (m *model) listView(w int) []string {
	badge := 0
	if m.inRepo && !m.path.active {
		badge = 1
	}
	grid := columns.Fit(m.list.RowWidth(w), []int{1, m.nameWidth, badge, 10, 6})
	draw := func(i, _ int, selected bool) ui.Row {
		return ui.Row{Line: m.rowLine(grid, m.rows[i], selected)}
	}
	m.list.HideCursor(m.here) // resting on the folder: no row is what ⏎ acts on
	return m.list.View(m.theme, w, draw, nil)
}

func (m *model) rowLine(g ui.Grid, r row, selected bool) ui.Line {
	t, e := m.theme, r.e
	glyph := t.Glyph(ui.KindFile)
	switch e.kind {
	case kindDir:
		glyph = t.Glyph(ui.KindFolder)
	case kindBroken:
		glyph.Style = t.Err
	}
	var badge ui.Line
	if b := m.git.badgeOf(e); b != 0 && r.from == fromListing && !m.path.active {
		badge = ui.Styled(string(b), badgeStyle(t, b))
	}
	name := m.nameCell(e, g.Width(1), selected)
	if r.from == fromProject {
		name = m.projectCell(e, g.Width(1), selected)
	}
	return g.Row(ui.Line{glyph}, name, badge, m.sizeCell(e), ui.Styled(ui.RelTimeAt(e.modTime, m.cfg.now()), t.Subtle))
}

// nameStyle is how an entry's name is drawn: folders in accent, broken
// links in the error colour, special files secondary; bold when selected.
func (m *model) nameStyle(e entry, selected bool) lipgloss.Style {
	t := m.theme
	base := t.Text
	switch e.kind {
	case kindDir:
		base = t.Accent
	case kindBroken:
		base = t.Err
	case kindSpecial:
		base = t.Muted
	}
	return base.Bold(selected)
}

// nameCell draws a label: a folder part (subtle) before the base name, match
// highlights, escapes (␛, <U+202E>) in the escape style, "/" after folders
// and ↗ after links. When it is too long the folder part gives way first,
// so the name itself stays readable.
func (m *model) nameCell(e entry, w int, selected bool) ui.Line {
	t := m.theme
	base := m.nameStyle(e, selected)
	text, escapes := sanitize.NameSpans(e.rel)
	if text != e.label {
		escapes = nil // a label written for display (a project's path)
	}
	pos := highlights(m.pattern, e.label)
	dir, name := splitLabel(e.label)
	nd := len([]rune(dir))
	var dirPos, namePos []int
	for _, p := range pos {
		if p < nd {
			dirPos = append(dirPos, p)
		} else {
			namePos = append(namePos, p-nd)
		}
	}
	tail := ui.HighlightEscaped(name, namePos, escapesIn(escapes, len(dir), len(e.label)), base, t.Match, t.EscapeStyle())
	tail = append(tail, m.nameSuffix(e, base)...)
	if dir == "" {
		return tail.Truncate(w)
	}
	room := w - tail.Width()
	if room < 2 {
		return tail.TruncateStart(w)
	}
	head := ui.HighlightEscaped(dir, dirPos, escapesIn(escapes, 0, len(dir)), t.Subtle, t.Match, t.EscapeStyle())
	return append(head.TruncateStart(room), tail...)
}

// projectCell draws a project (a zoxide favourite): its name, then where
// it is, middle-truncated so both its start and its name stay readable
// (~/Co…/website).
func (m *model) projectCell(e entry, w int, selected bool) ui.Line {
	base := m.nameStyle(e, selected)
	name := ui.Styled(sanitize.Name(e.name()), base).Append(m.nameSuffix(e, base)...)
	if room := w - name.Width() - projectGap; room >= 8 {
		return name.Append(ui.Space(projectGap), ui.Span{Text: ui.TruncateMiddle(e.label, room), Style: m.theme.Muted})
	}
	return name.Truncate(w)
}

// projectGap is the air between a project's name and its path.
const projectGap = 3

// nameSuffix is what follows a name: "/" after folders (in the name's
// colour) and ↗ after links.
func (m *model) nameSuffix(e entry, base lipgloss.Style) ui.Line {
	t := m.theme
	var out ui.Line
	if e.isDir() {
		out = append(out, ui.Span{Text: "/", Style: base})
	}
	if e.link {
		style := t.Subtle
		if e.kind == kindBroken {
			style = t.Err
		}
		out = append(out, ui.Span{Text: " " + ui.GlyphSymlink, Style: style})
	}
	return out
}

// escapesIn returns the escape ranges (byte offsets into a label) that lie
// in label[from:to], relative to from.
func escapesIn(escapes [][2]int, from, to int) [][2]int {
	var out [][2]int
	for _, e := range escapes {
		if e[1] > from && e[0] < to {
			out = append(out, [2]int{max(e[0], from) - from, min(e[1], to) - from})
		}
	}
	return out
}

// nameWidth is the width a row's name cell would take in full.
func nameWidth(r row) int {
	n := r.e.width
	if r.e.isDir() {
		n++ // "/"
	}
	if r.e.link {
		n += 2 // " ↗"
	}
	if r.from == fromProject {
		n += projectGap + ansi.StringWidth(sanitize.Name(r.e.name()))
	}
	return n
}

func (m *model) sizeCell(e entry) ui.Line {
	t := m.theme
	switch e.kind {
	case kindDir:
		c, ok := m.counts[e.path]
		if !ok || c.failed {
			return nil
		}
		return ui.Styled(itemCount(c.n, c.capped), t.Subtle)
	case kindBroken:
		return ui.Styled("broken", t.Err)
	case kindSpecial:
		return ui.Styled(specialWord(e.mode), t.Muted)
	}
	return ui.Styled(humanSize(e.size), t.Muted)
}

func badgeStyle(t ui.Theme, b byte) lipgloss.Style {
	switch b {
	case 'A', 'R':
		return t.OK
	case 'D', 'U':
		return t.Err
	case '?':
		return t.Info
	}
	return t.Warn
}

// helpKeys are the key panel's groups.
var helpKeys = []ui.KeyGroup{
	{Title: "Open", Bindings: []ui.Binding{
		{Keys: []string{"enter"}, Desc: "New tab here"},
		{Keys: []string{"alt+enter"}, Desc: "Split right of your pane"},
		{Keys: []string{"shift+enter"}, Desc: "Split below your pane"},
		{Keys: []string{"ctrl+e"}, Desc: "Edit the file in a new tab"},
		{Keys: []string{"ctrl+y"}, Desc: "Copy the path"},
		{Keys: []string{"ctrl+x"}, Desc: "Open with the default app"},
	}},
	{Title: "Browse", Bindings: []ui.Binding{
		{Keys: []string{"up", "down"}, Desc: "Move"},
		{Keys: []string{"right"}, Desc: "Open the folder"},
		{Keys: []string{"left", "backspace"}, Desc: "Back, or up a folder"},
		{Keys: []string{"ctrl+l"}, Desc: "Type a path"},
		{Keys: []string{"pgup", "pgdown"}, Desc: "Page"},
	}},
	{Title: "Show", Bindings: []ui.Binding{
		{Keys: []string{"ctrl+f"}, Desc: "All files below"},
		{Keys: []string{"alt+."}, Desc: "Hidden files"},
		{Keys: []string{"ctrl+/"}, Desc: "Preview"},
		{Keys: []string{"ctrl+u", "ctrl+d"}, Desc: "Scroll the preview"},
		{Keys: []string{"ctrl+r"}, Desc: "Refresh"},
		{Keys: []string{"esc"}, Desc: "Close"},
	}},
}

// errorText is an error as a toast: one clean line (rex.UserMessage),
// capitalised.
func errorText(err error) string {
	return ui.Capitalize(rex.UserMessage(err))
}
