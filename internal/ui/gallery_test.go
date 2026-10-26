package ui

import (
	"fmt"
	"strings"
	"time"

	"rexkit/internal/match"
	"rexkit/internal/ui/sanitize"
)

// galleryNow fixes the clock so the gallery renders identically every time.
var galleryNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type demoRow struct {
	kind    Kind
	name    string
	context string
	status  Status
	ago     time.Duration
	current bool
}

var demoSections = []Section{
	{Title: "Needs you", Note: "1", Start: 0},
	{Title: "Working", Note: "2", Start: 1},
	{Title: "Recent", Start: 3},
	{Title: "Sessions", Start: 5},
}

var demoRows = []demoRow{
	{KindAgent, "Navigation polish", "studio › web", StatusBlocked, 12 * time.Minute, false},
	{KindAgent, "Fix navbar overflow", "studio › web", StatusWorking, 2 * time.Minute, false},
	{KindAgent, "claude-code", "rexkit › spark", StatusWorking, 3 * time.Second, false},
	{KindPane, "zsh", "audit › exp", StatusIdle, 3 * time.Hour, true},
	{KindTab, "漢字 notes 📝", "studio", StatusNone, 26 * time.Hour, false},
	{KindSession, "rexkit", "2 tabs · 4 panes", StatusFailed, 0, false},
	{KindSession, "studio", "2 tabs · 2 panes", StatusNone, 0, false},
	{KindSession, "audit-platform-exp", "4 tabs · 6 panes", StatusExited, 9 * 24 * time.Hour, false},
}

// demoColumns is the row layout of Go to's list: glyph, name, context,
// status (glyph and word), age.
var demoColumns = Columns{
	{Width: 1},
	{Flex: true, Min: 18, Gap: 1},
	{Shrink: true, Min: 10, Max: 28, Gap: 2, Drop: 1},
	{Max: 10, Gap: 2, Drop: 3},
	{Max: 6, Gap: 2, Right: true, Drop: 2},
}

// Gallery renders every v2 component with sample data at the given size,
// the way the tools compose them: a "Go to" card (banded header, sections,
// the selection, a preview with its grid and well, the tray), a "Files"
// card with the key panel raised over it, a rename dialog, a toast strip,
// an empty state, and a strip of chips, keycaps, statuses and toasts. It
// is the fixture of the golden tests in golden_test.go.
func Gallery(width, height int, dark bool) string {
	t := NewTheme(dark)
	if width <= 0 || height <= 0 {
		return ""
	}
	pickerH := height
	if height >= 30 {
		pickerH = height * 55 / 100
	}
	rest := height - pickerH
	parts := []Part{{Rect{0, 0, width, pickerH}, strings.Split(galleryPicker(t, width, pickerH), "\n")}}
	if rest >= 12 {
		lowerH := rest - stripRows
		half := width / 2
		right := width - half
		dialogH := min(DialogRows, lowerH)
		parts = append(parts,
			Part{Rect{0, pickerH, half - 1, lowerH}, strings.Split(galleryFiles(t, half-1, lowerH), "\n")},
			Part{Rect{half, pickerH, right, dialogH}, strings.Split(galleryDialog(t, min(56, right), dialogH), "\n")},
			Part{Rect{half, pickerH + dialogH, right, 4}, seeThrough(t, t.ToastStrip(Toast{Kind: ToastErr, Text: "Could not open the editor: nvim exited 1"}, right, 4, ToastRows))},
			Part{Rect{half, pickerH + dialogH + 4, right, lowerH - dialogH - 4}, t.EmptyState(
				Line{{"No matches for “", t.Text.Bold(true)}, {"zqxj", t.Accent.Bold(true)}, {"”", t.Text.Bold(true)}},
				Line{{"Names, folders, programs and tasks are all searched.", t.Muted}},
				[]Hint{{Key: "backspace", Label: "edit the search"}, {Key: "esc", Label: "close"}},
				right, lowerH-dialogH-4)},
			Part{Rect{0, pickerH + lowerH, width, stripRows}, galleryStrip(t, width)},
		)
	}
	return strings.Join(Compose(width, height, parts...), "\n")
}

// seeThrough shows a strip's Clear cells as the terminal's background, as
// the stage does over a pane.
func seeThrough(t Theme, lines []string) []string {
	r, g, b, _ := t.C.Well.RGBA()
	well := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.ReplaceAll(l, "48;2;1;2;3", well)
	}
	return out
}

func galleryPicker(t Theme, w, h int) string {
	f := FloatFrame(w, h, true)
	in := f.Inner(w, h)
	a := Arrange(in.W, in.H, LayoutOptions{Card: f.Card, ListShare: 55})
	hdr := Header{
		Title: "Go to",
		Query: Query{Focused: true, Placeholder: "Search sessions, tabs, panes, agents", RealCursor: true},
		Chips: []Chip{
			{Kind: ChipTab, Label: "All", On: true},
			{Kind: ChipTab, Label: "Sessions"},
			{Kind: ChipTab, Label: "Tabs"},
			{Kind: ChipTab, Label: "Agents", Badge: "1"},
		},
	}
	var header []string
	if a.Banded() {
		header, _ = t.HeaderBand(hdr, a.Header.W)
	} else {
		line, _ := t.Header(hdr, a.Header.W)
		header = []string{line}
	}
	cells := func(r demoRow, selected bool) []Line {
		name := t.Text
		if r.status == StatusExited {
			name = t.Subtle
		}
		if selected || r.kind == KindSession {
			name = name.Bold(true)
		}
		status := t.StatusText(r.status, galleryNow)
		if r.kind == KindSession && r.status != StatusNone {
			status = t.StatusSummary(r.status, "1 "+r.status.Word(), galleryNow, true)
		}
		return []Line{
			{t.Glyph(r.kind)},
			Styled(r.name, name),
			Styled(r.context, t.Muted),
			status,
			Styled(demoAge(r.ago), t.Subtle),
		}
	}
	all := make([][]Line, len(demoRows))
	for i, r := range demoRows {
		all[i] = cells(r, false)
	}
	var list List
	list.SetItems(len(demoRows), demoSections)
	list.SetHeight(a.List.H)
	list.Select(1)
	grid := demoColumns.Fit(list.RowWidth(a.List.W), Measure(all, len(demoColumns)))
	rows := list.View(t, a.List.W, func(i, _ int, selected bool) Row {
		return Row{Line: grid.Row(cells(demoRows[i], selected)...), Current: demoRows[i].current}
	}, nil)
	parts := []Part{{a.InBody(a.List), rows}}
	if a.PreviewVisible() {
		var p Preview
		p.Set(p.Begin(), Line{t.Glyph(KindAgent), Space(1), {"Fix navbar overflow", t.Text.Bold(true)}, Space(2), {"studio › web", t.Muted}},
			sanitize.TerminalLines(demoPane, a.Preview.W))
		p.Right = t.Tag(StatusWorking, "", galleryNow).Append(Span{"  2m", t.Subtle})
		p.Context = Line{{"~/code/studio/web", t.Subtle}}
		p.Grid = []MetaRow{
			{"Directory", Styled("~/code/studio/web", t.Text)},
			{"Program", Styled("codex · gpt-5", t.Text)},
			{"Task", Styled("Refine the navigation so the active route is obvious", t.Text)},
		}
		parts = append(parts, Part{a.InBody(a.Preview), p.View(t, a.Preview.W, a.Preview.H)})
	}
	body := Compose(a.Body.W, a.Body.H, parts...)
	tray := t.Tray(Tray{
		Meta: t.MetaCount(len(demoRows), len(demoRows), "place"),
		Hints: []Hint{
			{Key: "enter", Label: "go", Outcome: "to studio", Primary: true},
			{Key: "alt+enter", Label: "bring here"},
			{Key: "tab", Label: "filter"},
			{Key: "ctrl+/", Label: "preview"},
			{Key: "?", Label: "keys"},
		},
	}, a.Footer.W, a.Footer.H)
	return f.Render(t, w, h, header, body, tray)
}

// demoAge renders an age; zero means the row has none.
func demoAge(ago time.Duration) string {
	if ago == 0 {
		return ""
	}
	return RelTimeAt(galleryNow.Add(-ago), galleryNow)
}

// demoPane is pane output as Rex exports it: SGR colours, CUF for blank
// cells, and controls the sanitizer must drop (title, hyperlink, bidi).
const demoPane = "\x1b]0;codex\x07\x1b[38;2;121;192;255m›\x1b[0m Refine the navigation\r\n" +
	"\r\n" +
	"\x1b[32m✓\x1b[0m Read \x1b]8;;file:///router.ts\x1b\\router.ts\x1b]8;;\x1b\\\r\n" +
	"\x1b[32m✓\x1b[0m Updated middleware\x1b[4C\x1b[2m(3 files)\x1b[0m\r\n" +
	"\x1b[1mChanges are ready.\x1b[0m\r\n" +
	"\x1b[33m●\x1b[0m Waiting for your review\u202e\r\n" +
	"\r\n" +
	"  src/router.ts      \x1b[32m+42\x1b[0m \x1b[31m-7\x1b[0m\r\n" +
	"  src/middleware.ts  \x1b[32m+12\x1b[0m \x1b[31m-3\x1b[0m\r\n" +
	"  src/nav/Navbar.tsx \x1b[32m+88\x1b[0m \x1b[31m-51\x1b[0m\r\n" +
	"\r\n" +
	"\x1b[2m  approve with ⏎, or type feedback\x1b[0m\r\n"

var demoFiles = []struct {
	name, git, size string
	ago             time.Duration
	folder          bool
}{
	{"cmd/", "", "1 item", 24 * time.Hour, true},
	{"internal/", "M", "9 items", 12 * time.Minute, true},
	{"go.mod", "M", "1.1 KB", 3 * time.Hour, false},
	{"an-extremely-long-filename-that-keeps-going.md", "", "2 B", 40 * 24 * time.Hour, false},
	{"README.md", "?", "7.8 KB", 26 * time.Minute, false},
}

func galleryFiles(t Theme, w, h int) string {
	f := FloatFrame(w, h, true)
	in := f.Inner(w, h)
	a := Arrange(in.W, in.H, LayoutOptions{Card: f.Card, NoPreview: true})
	query := Query{Value: "md", Cursor: 2, Focused: true, RealCursor: true}
	pattern := match.Compile(query.Value)
	var hits []int
	var found [][]int
	for i, fl := range demoFiles {
		if res, ok := pattern.Match(match.Field{Text: fl.name}); ok {
			hits, found = append(hits, i), append(found, res.Positions[0])
		}
	}
	hdr := Header{
		Crumbs: []string{"~", "Coding", "snipeship", "rexkit"},
		Info:   Line{{GlyphBranch + " ", t.Subtle}, {"main", t.Muted}, {" ↑1", t.Accent}, {" ●3", t.Warn}},
		Query:  query,
		Chips:  []Chip{{Kind: ChipToggle, Label: "all files", Key: "ctrl+f"}, {Kind: ChipToggle, Label: "hidden", Key: "alt+.", On: true}},
	}
	var header []string
	if a.Banded() {
		header, _ = t.HeaderBand(hdr, a.Header.W)
	} else {
		line, _ := t.Header(hdr, a.Header.W)
		header = []string{line}
	}
	cols := Columns{{Width: 1}, {Flex: true, Min: 8, Gap: 1}, {Width: 1, Gap: 2}, {Max: 8, Gap: 2, Right: true, Drop: 2}, {Max: 6, Gap: 2, Right: true, Drop: 1}}
	cells := func(k int, selected bool) []Line {
		fl := demoFiles[hits[k]]
		glyph, base := t.Glyph(KindFile), t.Text
		if fl.folder {
			glyph, base = t.Glyph(KindFolder), t.Accent
		}
		if selected {
			base = base.Bold(true)
		}
		git := map[string]Span{"M": {"M", t.Warn}, "?": {"?", t.Info}}[fl.git]
		return []Line{
			{glyph},
			Highlight(sanitize.Name(fl.name), found[k], base, t.Match),
			{git},
			Styled(fl.size, t.Subtle),
			Styled(RelTimeAt(galleryNow.Add(-fl.ago), galleryNow), t.Subtle),
		}
	}
	all := make([][]Line, len(hits))
	for k := range all {
		all[k] = cells(k, false)
	}
	var list List
	list.SetItems(len(hits), []Section{{Title: "Folders", Start: 0}, {Title: "Files", Start: 1}})
	list.SetHeight(a.List.H)
	list.Select(1)
	grid := cols.Fit(list.RowWidth(a.List.W), Measure(all, len(cols)))
	rows := list.View(t, a.List.W, func(k, _ int, selected bool) Row { return Row{Line: grid.Row(cells(k, selected)...)} }, nil)
	body := Compose(a.Body.W, a.Body.H, Part{a.InBody(a.List), rows})
	if panel, at, _ := t.KeyPanel(demoKeys, a.Body.W, a.Body.H, PanelCorner, 0); panel != nil {
		body = t.Raise(body, panel, at)
	}
	tray := t.Tray(Tray{
		Meta:  t.MetaCount(len(hits), len(demoFiles), "item"),
		Hints: []Hint{{Key: "enter", Label: "new tab", Outcome: "in", Path: "~/Coding/snipeship/rexkit", Primary: true}, {Key: "ctrl+e", Label: "edit"}, {Key: "?", Label: "keys"}},
	}, a.Footer.W, a.Footer.H)
	return f.Render(t, w, h, header, body, tray)
}

// galleryDialog is the rename dialog: title, field and helper, Mac-order
// tray.
func galleryDialog(t Theme, w, h int) string {
	f := DialogFrame(w, h)
	in := f.Inner(w, h)
	title := Line{Space(1), {"Rename pane", t.Text.Bold(true)}}.Fit(in.W).Render()
	field, _, _ := t.Field(Field{
		Query:  Query{Value: "api-server", Cursor: 10, Focused: true, RealCursor: true},
		Helper: "Was api",
	}, in.W, !f.Banded)
	trayH := 1
	if f.Banded {
		trayH = TrayRows
	}
	tray := t.Tray(Tray{
		Meta:  t.InTray().Hints([]Hint{{Key: "ctrl+u", Label: "clear"}}, in.W),
		Hints: []Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "save", Primary: true}},
	}, in.W, trayH)
	return f.Render(t, w, h, []string{title}, field, tray)
}

var demoKeys = []KeyGroup{
	{"Move", []Binding{
		{[]string{"up", "down"}, "select"},
		{[]string{"pgup", "pgdown"}, "page"},
		{[]string{"right"}, "open session or tab"},
		{[]string{"left", "backspace"}, "back out"},
	}},
	{"Go", []Binding{
		{[]string{"enter"}, "go there"},
		{[]string{"alt+enter"}, "bring pane here"},
		{[]string{"esc"}, "close"},
	}},
}

// stripRows is the height of galleryStrip.
const stripRows = 6

// galleryStrip shows the small components on the card: chips as the band
// draws them, keycaps, statuses as lists write them and the one tag, and
// toasts as the tray shows them.
func galleryStrip(t Theme, w int) []string {
	chips := Line{Space(1)}
	var shapes []Shape
	for _, c := range []Chip{
		{Kind: ChipToggle, Label: "hidden", Key: "alt+.", On: true},
		{Kind: ChipToggle, Label: "all files", Key: "ctrl+f"},
		{Kind: ChipTab, Label: "Agents", Badge: "5", On: true},
		{Kind: ChipTab, Label: "Panes"},
		{Kind: ChipScope, Label: "studio › web"},
	} {
		line := t.Chip(c)
		if c.Filled() {
			shapes = append(shapes, ControlShape(chips.Width(), line.Width(), t.chipFill(c)))
		}
		chips = append(chips, line...)
		chips = append(chips, Space(2))
	}
	caps := Line{Space(1)}
	for _, k := range []string{"enter", "esc", "shift+tab", "ctrl+/", "alt+enter", "backspace", "ctrl+p e", "?", "up"} {
		caps = append(caps, t.Keycap(CapLabel(k))...)
		caps = append(caps, Space(2))
	}
	caps = append(caps, t.Hints([]Hint{{Key: "ctrl+r", Label: "refresh"}}, 40)...)
	statuses := Line{Space(1)}
	for _, s := range []Status{StatusWorking, StatusBlocked, StatusIdle, StatusFailed, StatusExited} {
		statuses = append(statuses, t.StatusText(s, galleryNow)...)
		statuses = append(statuses, Space(3))
	}
	statuses = append(statuses, t.Tag(StatusBlocked, "", galleryNow)...)
	statuses = append(statuses, Space(2))
	statuses = append(statuses, t.Tag(StatusFailed, "exited 1", galleryNow)...)
	toasts := Line{Space(1)}
	for _, to := range []Toast{
		{Kind: ToastOK, Text: "Opened api"},
		{Kind: ToastWarn, Text: "1 conflict"},
		{Kind: ToastErr, Text: "That pane closed", Ways: []Hint{{Key: "ctrl+r", Label: "refresh"}}},
		{Kind: ToastInfo, Text: "Keymap ready"},
	} {
		toasts = append(toasts, t.Toast(to)...)
		toasts = append(toasts, Space(3))
	}
	return []string{
		EdgeRow(w, true, t.C.Surface, shapes...),
		chips.Fit(w).Render(),
		EdgeRow(w, false, t.C.Surface, shapes...),
		caps.Fit(w).Render(),
		statuses.Fit(w).Render(),
		toasts.Fit(w).Render(),
	}
}
