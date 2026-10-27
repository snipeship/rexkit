package hints

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

// How the pane recedes under its hints: text loses colour and moves towards
// its own background, so the matches (at full contrast) are what is left
// to read. Backgrounds stay, so the pane keeps its shape.
const (
	dimText       = 0.62
	dimDesaturate = 0.5
	// dimFill is how far a coloured background (a diff line, a selection,
	// a status bar) moves towards the pane's own: it keeps its shape but
	// stops pulling the eye away from the hints.
	dimFill = 0.5
	// matchWash is how far a match's background moves towards the accent:
	// enough to see the extent of the thing that will be picked.
	matchWashDark  = 0.20
	matchWashLight = 0.14
	// chipTyped is how far a typed letter of a chip fades into the chip.
	chipTyped = 0.55
)

// look is the overlay's colours, from the Rex theme.
type look struct {
	chipBg, chipFg, chipTyped rextheme.Color
	wash                      float64
	accent, flash             rextheme.Color
}

func (m *model) look() look {
	u := m.rex.UI
	lk := look{chipBg: u.AccentSolid, chipFg: u.AccentOnSolid, accent: u.Accent, wash: matchWashDark,
		flash: rextheme.Of(m.theme.C.WellFlashBg)}
	if !m.rex.Dark {
		lk.wash = matchWashLight
	}
	lk.chipTyped = lk.chipFg.Mix(lk.chipBg, chipTyped)
	return lk
}

// cellOut is one cell as the overlay draws it.
type cellOut struct {
	text                      string
	fg, bg                    rextheme.Color
	bold, italic, under, strk bool
}

// visible reports whether a match is shown: while picking, those whose
// label starts with what was typed; once picked, the picked text (every
// place it shows), flashing.
func (m *model) visible(mt Match) bool {
	if m.picked != nil {
		return mt.Text == m.picked.Match.Text
	}
	return strings.HasPrefix(mt.Label, m.typed)
}

// render draws the tool: the pane under its hints and the key bar, at the
// pane's place; the rest of the window is left see-through.
func (m *model) render() string {
	pane := m.renderPane()
	if m.area == (ui.Rect{W: m.w, H: m.h}) {
		return pane
	}
	buf := uv.NewScreenBuffer(m.w, m.h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(pane).Draw(buf, uv.Rect(m.area.X, m.area.Y, m.area.W, m.area.H))
	return buf.Render()
}

// renderPane draws the pane under its hints, then the key bar.
func (m *model) renderPane() string {
	lk := m.look()
	h, w := len(m.cells), 0
	if h > 0 {
		w = len(m.cells[0])
	}
	picked := m.picked != nil
	grid := make([][]cellOut, h)
	for y := range h {
		grid[y] = make([]cellOut, w)
		for x := range w {
			c := m.cells[y][x]
			o := cellOut{text: c.Content, fg: c.Fg, bg: c.Bg, bold: c.Bold, italic: c.Italic, under: c.Underline, strk: c.Strike}
			if c.Width == 0 {
				o.text = ""
			}
			if !picked {
				o.fg = c.Fg.Desaturate(dimDesaturate).Mix(c.Bg, dimText)
				if bg := m.rex.Terminal.Background; c.Bg != bg {
					o.bg = c.Bg.Mix(bg, dimFill)
					o.fg = o.fg.Mix(bg, dimFill)
				}
			}
			grid[y][x] = o
		}
	}
	for _, mt := range m.matches {
		if !m.visible(mt) || mt.Row >= h {
			continue
		}
		row := m.cells[mt.Row]
		for x := mt.Col; x < min(mt.Col+mt.Width, w); x++ {
			o := &grid[mt.Row][x]
			o.fg = row[x].Fg
			if picked {
				o.bg = lk.flash
				continue
			}
			o.bg = row[x].Bg.Mix(lk.accent, lk.wash)
		}
		if picked {
			continue
		}
		// A chip with a blank cell before it is rounded there, as Rex's
		// tags are drawn ("▐ya3f9c21").
		if x := mt.Chip - 1; x >= 0 && ui.Shapes() != ui.ShapesFlat && isBlank(row[x]) {
			grid[mt.Row][x] = cellOut{text: "▐", fg: lk.chipBg, bg: row[x].Bg}
		}
		for i, r := range mt.Label {
			x := mt.Chip + i
			if x >= w {
				break
			}
			o := &grid[mt.Row][x]
			*o = cellOut{text: string(r), fg: lk.chipFg, bg: lk.chipBg, bold: true}
			if i < len(m.typed) {
				o.fg, o.bold = lk.chipTyped, false
			}
		}
	}
	if picked {
		return encode(grid)
	}
	line, at, ok := m.bar()
	if !ok {
		return encode(grid)
	}
	// The bar floats with a cell of air each side: the pane's text there
	// goes, its background stays.
	for y := at.Y; y < at.Y+barRows; y++ {
		for x := max(at.X-1, 0); x < min(at.X+at.W+1, w); x++ {
			grid[y][x].text, grid[y][x].under, grid[y][x].strk = " ", false, false
		}
	}
	return m.withBar(encode(grid), line, at)
}

// barRows is the key bar's height: its text row and its two edge rows.
const barRows = 3

// The key bar keeps two cells from the pane's sides (one in a narrow pane)
// and a row from its top and bottom (barPlace picks where), as the toast
// floats in the window's top-right.
const (
	barMarginCols = 2
	barMarginRows = 1
)

// paneSize is the pane's size in cells, cut at the window's edge: a pane
// can report a size its place in the window has not caught up with.
func (m *model) paneSize() (w, h int) {
	if len(m.cells) == 0 {
		return 0, 0
	}
	return max(min(len(m.cells[0]), m.w-m.area.X), 0), max(min(len(m.cells), m.h-m.area.Y), 0)
}

// bar is the key bar and where it floats. Of the tiers that fit the pane
// (barTier), it takes the one whose place covers the least text, the
// fuller one when they tie: a blank band for the whole bar when there is
// one, else a shorter bar rather than one cut through the pane's output.
func (m *model) bar() (ui.Line, ui.Rect, bool) {
	var line ui.Line
	var at ui.Rect
	least := -1
	for _, tier := range []barTier{barFull, barShort, barMinimal} {
		l := ui.Control(m.barText(tier), m.theme.C.Toast)
		r, n, ok := m.barPlace(l.Width())
		if ok && (least < 0 || n < least) {
			line, at, least = l, r, n
		}
		if least == 0 {
			break
		}
	}
	return line, at, least >= 0
}

// barPlace finds where a key bar w cells wide floats, and how many cells
// of text it covers there. It looks at bands by the pane's right edge, the
// highest one first (over the oldest output, where the toast sits in the
// window), then by its left edge, and takes the first that hides no hint
// and covers the least text; the top right, as costly as can be, when
// every band hides a hint. The margins shrink to a cell in a narrow pane.
// It depends on the screen alone, so the bar stays put while you type.
func (m *model) barPlace(w int) (ui.Rect, int, bool) {
	pw, ph := m.paneSize()
	margin := barMarginCols
	if w > pw-2*margin {
		margin = 1
	}
	xr, xl := pw-margin-w, margin
	yb, yt := ph-barMarginRows-barRows, barMarginRows
	if w <= 0 || xr < 1 || yb < 0 {
		return ui.Rect{}, 0, false
	}
	best, least := ui.Rect{X: xr, Y: yt, W: w, H: barRows}, math.MaxInt
	for _, x := range []int{xr, xl} {
		for y := yt; y <= yb; y++ {
			r := ui.Rect{X: x, Y: y, W: w, H: barRows}
			if m.hidesHint(r) {
				continue
			}
			if n := m.covers(r); n < least {
				best, least = r, n
			}
			if least == 0 {
				return best, 0, true
			}
		}
	}
	return best, least, true
}

// covers counts the cells of r, with a cell of air each side, that hold
// text or a background of their own.
func (m *model) covers(r ui.Rect) int {
	pw, _ := m.paneSize()
	n := 0
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := max(r.X-1, 0); x < min(r.X+r.W+1, pw); x++ {
			if c := m.cells[y][x]; !isBlank(c) && c.Width != 0 || c.Bg != m.rex.Terminal.Background {
				n++
			}
		}
	}
	return n
}

// hidesHint reports whether a bar at r (and its air) covers a hint.
func (m *model) hidesHint(r ui.Rect) bool {
	for _, mt := range m.matches {
		from, to := min(mt.Chip, mt.Col)-1, mt.Col+mt.Width
		if mt.Row >= r.Y && mt.Row < r.Y+r.H && from < r.X+r.W+1 && to > r.X-1 {
			return true
		}
	}
	return false
}

// encode writes cells as SGR-styled rows.
func encode(grid [][]cellOut) string {
	var b strings.Builder
	for y, row := range grid {
		if y > 0 {
			b.WriteByte('\n')
		}
		var prev cellOut
		for x, o := range row {
			if o.text == "" && x > 0 && ansi.StringWidth(row[x-1].text) == 2 {
				continue
			}
			text := o.text
			if text == "" {
				text = " "
			}
			key := o
			key.text = ""
			if x == 0 || key != prev {
				b.WriteString(sgr(o))
				prev = key
			}
			b.WriteString(text)
		}
		b.WriteString("\x1b[m")
	}
	return b.String()
}

func sgr(o cellOut) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	for _, a := range []struct {
		on   bool
		code string
	}{{o.bold, ";1"}, {o.italic, ";3"}, {o.under, ";4"}, {o.strk, ";9"}} {
		if a.on {
			b.WriteString(a.code)
		}
	}
	fmt.Fprintf(&b, ";38;2;%d;%d;%d;48;2;%d;%d;%dm", o.fg.R, o.fg.G, o.fg.B, o.bg.R, o.bg.G, o.bg.B)
	return b.String()
}

// chip draws a label as the screen shows it, for the key bar: "▐a▌" on the
// solid accent, typed letters faded.
func (m *model) chip(label string, typed int) ui.Line {
	lk := m.look()
	var l ui.Line
	for i, r := range label {
		st := lipgloss.NewStyle().Foreground(lk.chipFg).Bold(true)
		if i < typed {
			st = lipgloss.NewStyle().Foreground(lk.chipTyped)
		}
		l = append(l, ui.Span{Text: string(r), Style: st})
	}
	return ui.Pill(l, lk.chipBg)
}

// barTheme is the theme on the key bar: it is drawn as the toast is (the
// key panel's fill in dark themes, surfaceSubtle in light ones, where the
// panel's would vanish into the pane), with the toast's keycaps.
func (m *model) barTheme() ui.Theme {
	t := m.theme.OnFill(m.theme.C.Toast)
	t.C.KeyCap = t.C.ToastKeyCap
	return t
}

// barTier is how much the key bar says.
type barTier int

const (
	barFull    barTier = iota // the count and every key
	barShort                  // the count, the label and esc: no ⇧ open, ⌥ paste or ⌫ back
	barMinimal                // the label and esc alone, for a narrow pane
)

// barText is the key bar's text at a tier: how many things there are and
// the keys. A key that starts no label says so where the count was.
func (m *model) barText(tier barTier) ui.Line {
	t := m.barTheme()
	gap := ui.Space(3)
	if tier == barMinimal {
		gap = ui.Space(2)
	}
	var l ui.Line
	count := func(n int, one, many string) ui.Line {
		if m.miss != 0 {
			return ui.Line{{Text: "no label ", Style: t.Subtle}, {Text: m.typed + string(m.miss), Style: t.Warn}}
		}
		if tier == barMinimal {
			return nil
		}
		return ui.Line{{Text: strconv.Itoa(n), Style: t.Muted}, {Text: " " + plural(n, one, many), Style: t.Subtle}}
	}
	if m.typed == "" {
		if c := count(m.left(), "match", "matches"); c != nil {
			l = append(c, gap)
		}
		l = append(l, m.chip("a", 0)...)
		l = append(l, ui.Span{Text: " copy", Style: t.Text}, gap)
		if tier == barFull {
			for _, k := range []struct{ key, verb string }{{"shift", " open"}, {"alt", " paste"}} {
				l = append(l, t.Keycap(k.key)...)
				l = append(l, ui.Space(1))
				l = append(l, m.chip("a", 0)...)
				l = append(l, ui.Span{Text: k.verb, Style: t.Muted}, gap)
			}
		}
	} else {
		// ⇧ held shows as a capital: "Y…" will open what it picks.
		typed, verb := m.typed, "copy"
		switch {
		case m.alt:
			verb = "paste"
		case m.shift:
			typed, verb = strings.ToUpper(typed), "open"
		}
		l = append(l, m.chip(typed+"…", 0)...)
		if c := count(m.left(), "left", "left"); c != nil {
			l = append(append(l, ui.Space(1)), c...)
		}
		if m.miss == 0 {
			if tier != barMinimal {
				l = append(l, ui.Span{Text: " to", Style: t.Subtle})
			}
			l = append(l, ui.Span{Text: " " + verb, Style: t.Text})
		}
		l = append(l, gap)
		if tier == barFull {
			l = append(l, t.Keycap("bksp")...)
			l = append(l, ui.Span{Text: " back", Style: t.Muted}, gap)
		}
	}
	l = append(l, t.Keycap("esc")...)
	if tier == barMinimal {
		return l
	}
	return append(l, ui.Span{Text: " cancel", Style: t.Muted})
}

// withBar draws the key bar over the pane: a Control shape on the toast's
// fill, with its edge rows, at the place barPlace found.
func (m *model) withBar(base string, line ui.Line, at ui.Rect) string {
	pw, ph := m.paneSize()
	fill := m.theme.C.Toast
	buf := uv.NewScreenBuffer(pw, ph)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(base).Draw(buf, buf.Bounds())
	shape := ui.ControlShape(0, at.W, fill)
	for i, row := range []string{
		ui.EdgeRow(at.W, true, ui.Clear, shape),
		line.RenderBg(ui.Clear),
		ui.EdgeRow(at.W, false, ui.Clear, shape),
	} {
		over(&buf, row, at.X, at.Y+i, at.W)
	}
	return buf.Render()
}

// over draws one row of a shape at (x, y): its ui.Clear cells keep the
// pane's cell, or its background under a lip or an end.
func over(buf *uv.ScreenBuffer, row string, x, y, w int) {
	under := buf.CloneArea(uv.Rect(x, y, w, 1))
	uv.NewStyledString(row).Draw(buf, uv.Rect(x, y, w, 1))
	for i := range w {
		c := buf.CellAt(x+i, y)
		if c == nil || !ui.IsClear(c.Style.Bg) {
			continue
		}
		b := under.CellAt(i, 0)
		if b == nil {
			continue
		}
		if c.Content == " " || c.Content == "" {
			buf.SetCell(x+i, y, b.Clone())
			continue
		}
		c.Style.Bg = b.Style.Bg
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
