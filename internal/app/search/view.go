package search

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/find"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// listMax lets the list grow past Go to's and Files' 80 columns: its rows
// are lines of output. It grows only while the preview keeps previewWant
// columns (a 120-column log line and its line numbers); below that the
// list is the usual width.
const (
	listMax     = 104
	previewWant = 126
)

// listShare is the list's share of a w-column body for ui.Arrange.
func listShare(w int) int {
	want := min(listMax, w-ui.PreviewGap-previewWant)
	if w <= 0 || want <= ui.MaxListCols {
		return 0
	}
	return (want*100 + w - 1) / w
}

// screen is the tool's arrangement: the frame, its content area, the areas
// inside it, and where the header's chips are.
type screen struct {
	frame ui.Frame
	inner ui.Rect
	areas ui.Areas
	hits  ui.HeaderHits
}

func (m *model) layout() screen {
	frame := ui.FloatFrame(m.width, m.height)
	inner := frame.Inner(m.width, m.height)
	areas := ui.Arrange(inner.W, inner.H, ui.LayoutOptions{Preview: m.previewPref, ListShare: listShare(inner.W),
		ListMax: listMax, ListRows: m.list.Rows()})
	s := screen{frame: frame, inner: inner, areas: areas, hits: ui.HeaderHits{Cursor: -1}}
	if !areas.Header.Empty() {
		if areas.Banded() {
			_, s.hits = m.theme.HeaderBand(m.header(), areas.Header.W)
		} else {
			_, s.hits = m.theme.Header(m.header(), areas.Header.W)
		}
	}
	return s
}

func (m *model) relayout() {
	m.fitList()
	m.syncPreview()
}

func (m *model) fitList() {
	a := m.layout().areas
	m.list.SetHeight(max(1, listRect(a, m.keyPanelAt(a)).H))
}

// listRect is where the list is drawn, less what the key panel covers
// (as in Files).
func listRect(a ui.Areas, at ui.Rect) ui.Rect {
	r := a.List
	if at.Empty() || at.X >= r.X+r.W || at.Y >= r.Y+r.H {
		return r
	}
	switch above, left := at.Y-r.Y, at.X-r.X-1; {
	case above >= 3:
		r.H = above
	case left >= 20:
		r.W = left
	}
	return r
}

func (m *model) setHelp(on bool) {
	m.help, m.helpAt = on, 0
	m.fitList()
}

// keyPanel places the key panel as Files does: over the preview beside
// the list, across the body without one.
func (m *model) keyPanel(a ui.Areas) ([]string, ui.Rect, int) {
	if a.Mode == ui.PreviewSide {
		x := a.Preview.X - a.Body.X - 2
		well := x + 3
		if panel, at, more := m.theme.KeyPanel(helpKeys, a.Body.W-x, a.Body.H, ui.PanelCorner, m.helpAt); panel != nil {
			if at.X+x-well > 4 {
				return panel, at.Offset(x, 0), more
			}
			panel, at, more = m.theme.KeyPanel(helpKeys, a.Body.W-well+1, a.Body.H, ui.PanelFill, m.helpAt)
			return panel, at.Offset(well-1, 0), more
		}
	}
	return m.theme.KeyPanel(helpKeys, a.Body.W, a.Body.H, ui.PanelSpan, m.helpAt)
}

// keyPanelAt is where the key panel is, in content coordinates; empty when
// it is closed.
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
	if !m.scheme.Settled() || m.width <= 0 || m.height <= 0 {
		return v
	}
	s := m.layout()
	a, t := s.areas, m.theme
	var header, tray []string
	hits := ui.HeaderHits{Cursor: -1}
	if !a.Header.Empty() {
		if a.Banded() {
			header, hits = t.HeaderBand(m.header(), a.Header.W)
		} else {
			var line string
			line, hits = t.Header(m.header(), a.Header.W)
			header = []string{line}
		}
	}
	var panel []string
	var at ui.Rect
	if m.help {
		panel, at, _ = m.keyPanel(a)
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

// header is "Search", the field, and the chips: match case, regex, and
// this session only.
func (m *model) header() ui.Header {
	runes := []rune(m.input.Value())
	cur := min(m.input.Position(), len(runes))
	before := sanitize.Name(string(runes[:cur]))
	return ui.Header{
		Query: ui.Query{
			Value:       before + sanitize.Name(string(runes[cur:])),
			Cursor:      len([]rune(before)),
			Focused:     !m.help,
			Placeholder: "Search the screen and scrollback of every pane",
			RealCursor:  true,
		},
		Chips: []ui.Chip{
			{Kind: ui.ChipToggle, Label: "match case", Key: "alt+c", On: m.matchCase},
			{Kind: ui.ChipToggle, Label: "regex", Key: "alt+r", On: m.regex},
			{Kind: ui.ChipToggle, Label: "this session", Key: "tab", On: m.sessionOnly},
		},
	}
}

// body is the list and the preview, or a page when there is no list.
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

// searchedPanes is how many panes (and sessions) the query runs over: not
// those that could not be read.
func (m *model) searchedPanes() (panes, sessions int) {
	seen := map[string]bool{}
	for _, p := range m.panes {
		if !m.inScope(p) || p.err != nil {
			continue
		}
		panes++
		seen[p.session] = true
	}
	return panes, len(seen)
}

// emptyPage explains a body without rows: Rex still being read (or not
// readable), nothing typed yet, a regex that does not compile, or no
// matches.
func (m *model) emptyPage(w, h int) []string {
	t, now := m.theme, m.cfg.now()
	q := m.shown.Text
	panes, sessions := m.searchedPanes()
	where := plural(panes, "pane") + " across " + plural(sessions, "session")
	reading := t.Spinning("Reading "+strconv.Itoa(m.readCount())+" of "+plural(len(m.panes), "pane")+"…", now)
	switch {
	case m.listErr != nil:
		return t.ErrorState("Can't read Rex", rex.UserMessage(m.listErr), []ui.Hint{{Key: "ctrl+r", Label: "try again"}}, w, h)
	case !m.listed:
		if !m.spinner.Visible(now) {
			return ui.Compose(w, h)
		}
		return t.LoadingState("Reading Rex…", now, w, h)
	case q == "":
		detail := ui.Line{{Text: "Every line of the screen and scrollback of " + where + ".", Style: t.Muted}}
		if m.scanning() {
			detail = reading
		}
		scope := ui.Hint{Key: "tab", Label: "this session only"}
		if m.sessionOnly {
			scope.Label = "all sessions"
		}
		syntax := ui.Hint{Key: "alt+r", Label: "regex"}
		if m.regex {
			syntax.Label = "plain text"
		}
		ways := []ui.Hint{scope, syntax}
		return t.EmptyState(ui.Styled("Search every pane", t.Text.Bold(true)), detail, ways, w, h)
	case m.shownErr != nil:
		return t.ErrorState("Not a regular expression", find.Reason(m.shownErr), []ui.Hint{{Key: "alt+r", Label: "search the text"}}, w, h)
	}
	detail := ui.Styled("Searched "+groupDigits(m.searchedLines())+" lines in "+where+".", t.Muted)
	if m.scanning() {
		detail = reading
	}
	var ways []ui.Hint
	if m.sessionOnly {
		ways = append(ways, ui.Hint{Key: "tab", Label: "all sessions"})
	}
	// ⌥C ignores case only when match case is what makes the query match
	// it; a capital does too (smart case), with match case on or off.
	if m.shown.MatchCase && !(find.Query{Text: m.shown.Text, Regex: m.shown.Regex}).Sensitive() {
		ways = append(ways, ui.Hint{Key: "alt+c", Label: "ignore case"})
	}
	ways = append(ways, ui.Hint{Key: "ctrl+r", Label: "read the panes again"})
	return t.EmptyState(t.Quoted("No matches for ", q), detail, ways, w, h)
}

func (m *model) searchedLines() int {
	n := 0
	for _, p := range m.panes {
		if m.inScope(p) {
			n += len(p.lines)
		}
	}
	return n
}

// groupDigits writes n with its thousands apart: 48,210.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// plural is a count and its noun, with thousands grouped: "1 match",
// "1,204 matches".
func plural(n int, noun string) string {
	s := groupDigits(n) + " " + noun
	switch {
	case n == 1:
	case strings.HasSuffix(noun, "ch"):
		s += "es"
	default:
		s += "s"
	}
	return s
}

// tray: the count (or the reading in progress) and what ⏎ does. A pane
// that could not be read, or a toast, keeps its place: the hints give way.
func (m *model) tray() ui.Tray {
	t, now := m.theme, m.cfg.now()
	tr := ui.Tray{Meta: m.meta(), Toast: m.toasts.Current()}
	tr.Urgent = m.unreadable() > 0 || tr.Toast.Text != ""
	switch {
	case m.listed && m.scanning() && m.spinner.Visible(now):
		label := "Reading " + strconv.Itoa(m.readCount()) + " of " + plural(len(m.panes), "pane")
		if m.shownM != nil {
			label += " " + ui.Dot + " " + plural(m.total, "match") + " so far"
		}
		tr.Busy = t.Spinning(label, now)
	case m.busy:
		tr.Busy = t.Spinning("Going…", now)
	}
	scope := ui.Hint{Key: "tab", Label: "this session"}
	if m.sessionOnly {
		scope.Label = "all sessions"
	}
	r, ok := m.selected()
	switch {
	case !ok || len(m.rows) == 0:
		tr.Hints = []ui.Hint{scope, {Key: "esc", Label: "close"}, {Key: "?", Label: "keys"}}
	case r.kind == rowHit:
		line := m.groups[r.group].hits[r.hit] + 1
		tr.Hints = []ui.Hint{
			{Key: "enter", Label: "jump", Outcome: "to line " + strconv.Itoa(line), Primary: true},
			{Key: "ctrl+y", Label: "copy line"}, scope, {Key: "?", Label: "keys"},
		}
	case r.kind == rowMore:
		tr.Hints = []ui.Hint{{Key: "enter", Label: "show", Outcome: strconv.Itoa(r.hit) + " earlier", Primary: true}, scope, {Key: "?", Label: "keys"}}
	default:
		p := m.panes[m.groups[r.group].pane]
		tr.Hints = []ui.Hint{{Key: "enter", Label: "go to", Outcome: p.name, Primary: true}, scope, {Key: "?", Label: "keys"}}
	}
	if m.questionTyped() {
		tr.Hints[len(tr.Hints)-1].Key = "f1" // ? goes into the query
	}
	return tr
}

// unreadable is how many panes in scope could not be read.
func (m *model) unreadable() int {
	n := 0
	for _, p := range m.panes {
		if p.err != nil && m.inScope(p) {
			n++
		}
	}
	return n
}

// meta is "214 matches in 6 panes", numbers secondary and words tertiary,
// and how many panes could not be read.
func (m *model) meta() ui.Line {
	t := m.theme
	var line ui.Line
	if m.shownM != nil && len(m.groups) > 0 {
		line = append(line, count(t, m.total, "match")...)
		line = append(line, ui.Span{Text: " in ", Style: t.Subtle})
		line = append(line, count(t, len(m.groups), "pane")...)
	}
	if failed := m.unreadable(); failed > 0 {
		if len(line) > 0 {
			line = append(line, ui.Span{Text: " " + ui.Dot + " ", Style: t.Subtle})
		}
		line = append(line, ui.Span{Text: plural(failed, "pane") + " could not be read", Style: t.Warn})
	}
	return line
}

// count is "92 matches": the number secondary, the noun tertiary.
func count(t ui.Theme, n int, noun string) ui.Line {
	num, word, _ := strings.Cut(plural(n, noun), " ")
	return ui.Line{{Text: num, Style: t.Muted}, {Text: " " + word, Style: t.Subtle}}
}

// paneColumns is a pane row: glyph, name, place, count.
var paneColumns = ui.Columns{
	{Width: 1},
	{Min: 6, Max: 26, Gap: 1},
	{Flex: true, Min: 8, Gap: 2},
	{Max: 6, Gap: 2, Right: true, Drop: 1},
}

// hitIndent sets a pane's hits under its name.
const hitIndent = 2

func (m *model) listView(w int) []string {
	rw := m.list.RowWidth(w)
	// The name and place columns are sized for every pane searched, not
	// only those with matches, so they hold still while panes stream in and
	// as the query changes.
	nameW, placeW, countW, numW := 0, 0, 0, 1
	for _, p := range m.panes {
		if m.inScope(p) {
			nameW = max(nameW, ansi.StringWidth(p.name))
			placeW = max(placeW, ansi.StringWidth(place(p)))
		}
	}
	for _, g := range m.groups {
		countW = max(countW, len(strconv.Itoa(len(g.hits))))
		numW = max(numW, len(strconv.Itoa(g.hits[len(g.hits)-1]+1)))
	}
	grid := paneColumns.Fit(rw, []int{1, nameW, placeW, countW})
	draw := func(i, width int, selected bool) ui.Row {
		r := m.rows[i]
		switch r.kind {
		case rowPane:
			return m.paneRow(grid, r)
		case rowMore:
			return ui.Row{Line: m.moreRow(r, numW)}
		case rowGap:
			return ui.Row{}
		}
		return ui.Row{Line: m.hitRow(r, numW, width)}
	}
	return m.list.View(m.theme, w, draw, nil)
}

// place is where a pane is: "studio › api".
func place(p *pane) string {
	return p.sessionLabel + " " + ui.GlyphCrumb + " " + p.tabLabel
}

// placeCell is a pane's place in w cells: the session label gives way
// before the tab ("infrastr… › deploy"), as in Go to.
func placeCell(t ui.Theme, p *pane, w int) ui.Line {
	tab := " " + ui.GlyphCrumb + " " + p.tabLabel
	sess := ui.Line{{Text: p.sessionLabel, Style: t.Muted}}
	if room := w - ansi.StringWidth(tab); sess.Width() > room && room >= 4 {
		sess = sess.Truncate(room)
	}
	return sess.Append(ui.Span{Text: tab, Style: t.Muted})
}

// glyph is a pane's kind: an agent's mark, or a pane's.
func glyph(t ui.Theme, p *pane) ui.Span {
	if p.agent {
		return t.Glyph(ui.KindAgent)
	}
	return t.Glyph(ui.KindPane)
}

func (m *model) paneRow(g ui.Grid, r row) ui.Row {
	t := m.theme
	grp := m.groups[r.group]
	p := m.panes[grp.pane]
	name := ui.Styled(p.name, t.Text.Bold(true))
	n := ui.Styled(strconv.Itoa(len(grp.hits)), t.Muted)
	return ui.Row{Line: g.Row(ui.Line{glyph(t, p)}, name, placeCell(t, p, g.Width(2)), n), Current: p.origin}
}

// moreRow is the fold over a pane's older hits: "…  134 earlier matches".
func (m *model) moreRow(r row, numW int) ui.Line {
	t := m.theme
	return ui.Line{ui.Space(hitIndent), {Text: strings.Repeat(" ", numW-1) + ui.Ellipsis, Style: t.Subtle}, ui.Space(2),
		{Text: plural(r.hit, "earlier match"), Style: t.Subtle}}
}

// hitRow is a matching line: its number, then the line with every match
// highlighted, its indentation dropped and its start cut ("…") when the
// first match would be out of sight.
func (m *model) hitRow(r row, numW, w int) ui.Line {
	t := m.theme
	g := m.groups[r.group]
	n := g.hits[r.hit]
	text := m.panes[g.pane].plain[n]
	num := ui.Styled(lpad(strconv.Itoa(n+1), numW), t.Subtle)
	room := w - hitIndent - numW - 2
	// The matches are found on the whole line (a regex may need its
	// indentation), then moved with the text.
	trimmed := strings.TrimLeft(text, " ")
	trimmed, spans := squeeze(trimmed, shift(m.shownM.Spans(text), len(text)-len(trimmed)))
	var lead ui.Line
	// Keep the first match in sight, with some context before it.
	if len(spans) > 0 {
		first := ansi.StringWidth(trimmed[:spans[0][0]])
		end := first + ansi.StringWidth(trimmed[spans[0][0]:spans[0][1]])
		if end > room-1 {
			// Cut as little as keeps the match whole, with a little after
			// it, and never less than hitContext before it.
			skip := byteAtCell(trimmed, min(first-hitContext, end+hitAfter-room+1))
			skip = wordStart(trimmed, skip, spans[0][0])
			trimmed = trimmed[skip:]
			spans = shift(spans, skip)
			lead = ui.Line{{Text: ui.Ellipsis, Style: t.Subtle}}
		}
	}
	body := ui.Highlight(trimmed, runeRanges(trimmed, spans), t.Muted, t.Match)
	line := ui.Line{ui.Space(hitIndent)}.Append(num...).Append(ui.Space(2)).Append(lead...).Append(body...)
	return line.Truncate(w)
}

// squeeze closes runs of three or more spaces to two (alignment padding,
// expanded tabs), moving the spans with the text.
func squeeze(s string, spans [][2]int) (string, [][2]int) {
	if !strings.Contains(s, "   ") {
		return s, spans
	}
	var b strings.Builder
	moved := make([][2]int, len(spans))
	copy(moved, spans)
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			run++
			if run > 2 {
				for j := range moved {
					if moved[j][0] > b.Len() {
						moved[j][0]--
					}
					if moved[j][1] > b.Len() {
						moved[j][1]--
					}
				}
				continue
			}
		} else {
			run = 0
		}
		b.WriteByte(s[i])
	}
	return b.String(), moved
}

// wordStart moves a cut at byte i of s forward to the start of the next
// word or field (after a space, a comma or an opening bracket), when one
// begins within wordSlack bytes and before the match at byte limit, so a
// cut line starts on `"msg":` rather than on `s":"12:31`.
func wordStart(s string, i, limit int) int {
	if i <= 0 || i >= len(s) || strings.ContainsRune(" ,;({[", rune(s[i-1])) {
		return i
	}
	for j := i; j < min(limit, i+wordSlack); j++ {
		if strings.ContainsRune(" ,;({[", rune(s[j])) {
			return j + 1
		}
	}
	return i
}

// wordSlack is how far wordStart looks for the start of a word.
const wordSlack = 12

// hitContext is the least of a line kept before a match far along it;
// hitAfter is how much after it a cut line keeps in view.
const (
	hitContext = 16
	hitAfter   = 6
)

func lpad(s string, w int) string {
	if n := w - len(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// shift moves byte ranges left by n, dropping what falls before 0.
func shift(spans [][2]int, n int) [][2]int {
	var out [][2]int
	for _, s := range spans {
		if s[1]-n <= 0 {
			continue
		}
		out = append(out, [2]int{max(0, s[0]-n), s[1] - n})
	}
	return out
}

// byteAtCell is the byte offset of the cell col of s (0 when col ≤ 0).
func byteAtCell(s string, col int) int {
	if col <= 0 {
		return 0
	}
	w := 0
	for i, r := range s {
		if w >= col {
			return i
		}
		w += ansi.StringWidth(string(r))
	}
	return len(s)
}

// runeRanges converts byte ranges in s into the rune positions they cover,
// for ui.Highlight.
func runeRanges(s string, spans [][2]int) []int {
	var out []int
	for _, sp := range spans {
		from := len([]rune(s[:sp[0]]))
		n := len([]rune(s[sp[0]:sp[1]]))
		for i := range n {
			out = append(out, from+i)
		}
	}
	return out
}

// cellRanges converts byte ranges in s into cell ranges, for drawing over
// the coloured line.
func cellRanges(s string, spans [][2]int) [][2]int {
	out := make([][2]int, len(spans))
	for i, sp := range spans {
		a := ansi.StringWidth(s[:sp[0]])
		out[i] = [2]int{a, a + ansi.StringWidth(s[sp[0]:sp[1]])}
	}
	return out
}

// previewRows is how many content rows the preview shows.
func (m *model) previewRows() int {
	a := m.layout().areas
	return m.preview.ContentRect(a.Preview.W, a.Preview.H).H
}

// syncPreview shows the selected pane's capture around the selected line:
// line numbers, the pane's own colours, every match on the find fill and
// the selected one on the accent, as Scrollback shows them after ⏎.
func (m *model) syncPreview() {
	a := m.layout().areas
	r, ok := m.selected()
	if !ok || !a.PreviewVisible() || len(m.rows) == 0 || r.kind == rowGap {
		return
	}
	t := m.theme
	g := m.groups[r.group]
	p := m.panes[g.pane]
	cur := -1
	if r.kind == rowHit {
		cur = g.hits[r.hit]
	}
	title := ui.Line{glyph(t, p), ui.Space(1), {Text: p.name, Style: t.Text.Bold(true)}, ui.Space(2), {Text: place(p), Style: t.Muted}}
	room := m.preview.ContentRect(a.Preview.W, a.Preview.H).W
	m.preview.Set(m.preview.Begin(), title, p.lines)
	m.preview.Decorate = m.decorate(p, cur, room)
	m.preview.Context = ui.Styled(homePath(p.cwd, m.cfg.home), t.Subtle)
	if cur >= 0 {
		m.preview.Right = ui.Line{{Text: "line ", Style: t.Subtle}, {Text: strconv.Itoa(cur + 1), Style: t.Muted},
			{Text: " of ", Style: t.Subtle}, {Text: strconv.Itoa(len(p.lines)), Style: t.Muted}}
		m.preview.Follow = false
		m.preview.Scroll(cur-m.previewRows()/3, a.Preview.H)
	} else {
		m.preview.Right = count(t, len(g.hits), "match")
		m.preview.Follow = true
	}
}

// decorate draws a line of p's capture as the preview shows it: numbered,
// every match on the find fill and the selected line's on the accent. The
// selected line is cut from the start ("…") when its match would fall past
// room cells, so the match ⏎ jumps to is always in sight.
func (m *model) decorate(p *pane, cur, room int) func(int, string) string {
	t := m.theme
	tw := t
	numW := len(strconv.Itoa(len(p.lines)))
	find := lipgloss.NewStyle().Background(t.C.WellFindBg).Foreground(t.C.WellFg)
	now := lipgloss.NewStyle().Background(t.C.AccentSolid).Foreground(t.C.AccentOnSolid).Bold(true)
	matcher := m.shownM
	return func(i int, l string) string {
		numStyle := tw.Subtle
		if i == cur {
			numStyle = tw.Accent.Bold(true)
		}
		gutter := numStyle.Render(lpad(strconv.Itoa(i+1), numW)) + "  "
		if spans := matcher.Spans(p.plain[i]); len(spans) > 0 {
			style := find
			if i == cur {
				style = now
			}
			cells := cellRanges(p.plain[i], spans)
			l = overlay(l, cells, style)
			if i == cur {
				// Less the gutter and the well's ▲/▼ column (air, "▼ n", margin).
				marks := 6 + max(2, len(strconv.Itoa(len(p.lines))))
				l = keepInView(l, cells[0], room-numW-2-marks, tw.Subtle)
			}
		}
		return gutter + l
	}
}

// keepInView cuts a coloured line from the start, behind a "…", when the
// cell range hit would end past w cells, keeping hitContext cells before it.
func keepInView(line string, hit [2]int, w int, dim lipgloss.Style) string {
	if w <= 0 || hit[1] <= w-1 {
		return line
	}
	skip := max(1, min(hit[0]-hitContext, hit[1]+hitAfter-w+1))
	return dim.Render(ui.Ellipsis) + sgrBefore(line, skip) + ansi.Cut(line, skip, ansi.StringWidth(line))
}

// overlay draws style over the cell ranges of a coloured line, keeping its
// colours around them.
func overlay(line string, cells [][2]int, style lipgloss.Style) string {
	var b strings.Builder
	pos := 0
	for _, c := range cells {
		if c[0] < pos {
			continue
		}
		b.WriteString(ansi.Cut(line, pos, c[0]))
		b.WriteString(style.Render(ansi.Strip(ansi.Cut(line, c[0], c[1]))))
		b.WriteString(sgrBefore(line, c[1]))
		pos = c[1]
	}
	b.WriteString(ansi.Cut(line, pos, ansi.StringWidth(line)))
	return b.String()
}

// sgrBefore is every SGR sequence of line before cell col, so text after a
// highlight gets back its colour.
func sgrBefore(line string, col int) string {
	var b strings.Builder
	w := 0
	for i := 0; i < len(line) && w < col; {
		if strings.HasPrefix(line[i:], "\x1b[") {
			j := i + 2
			for j < len(line) && (line[j] >= '0' && line[j] <= '9' || line[j] == ';' || line[j] == ':') {
				j++
			}
			if j < len(line) {
				j++
			}
			b.WriteString(line[i:j])
			i = j
			continue
		}
		cluster, cw := ansi.FirstGraphemeCluster(line[i:], ansi.GraphemeWidth)
		w += cw
		i += len(cluster)
	}
	return b.String()
}

// homePath writes a path under home with "~".
func homePath(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return sanitize.Label(p)
}

// helpKeys are the key panel's groups.
var helpKeys = []ui.KeyGroup{
	{Title: "Jump", Bindings: []ui.Binding{
		{Keys: []string{"enter"}, Desc: "Open the line in Scrollback"},
		{Keys: []string{"alt+enter"}, Desc: "Go to the pane"},
		{Keys: []string{"ctrl+y"}, Desc: "Copy the line"},
		{Keys: []string{"right"}, Desc: "Show earlier matches"},
	}},
	{Title: "Move", Bindings: []ui.Binding{
		{Keys: []string{"up", "down"}, Desc: "Move"},
		{Keys: []string{"alt+up", "alt+down"}, Desc: "Previous, next pane"},
		{Keys: []string{"pgup", "pgdown"}, Desc: "Page"},
	}},
	{Title: "Search", Bindings: []ui.Binding{
		{Keys: []string{"tab"}, Desc: "This session only"},
		{Keys: []string{"alt+c"}, Desc: "Match case"},
		{Keys: []string{"alt+r"}, Desc: "Regular expression"},
		{Keys: []string{"ctrl+r"}, Desc: "Read the panes again"},
	}},
	{Title: "Show", Bindings: []ui.Binding{
		{Keys: []string{"ctrl+/"}, Desc: "Preview"},
		{Keys: []string{"ctrl+u", "ctrl+d"}, Desc: "Scroll the preview"},
		{Keys: []string{"f1", "?"}, Desc: "Keys"},
		{Keys: []string{"esc"}, Desc: "Close"},
	}},
}
