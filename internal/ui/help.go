package ui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/sanitize"
)

// Binding is one line of a key sheet.
type Binding struct {
	Keys []string // alternatives in config notation: {"up", "k"}
	Desc string
}

// KeyGroup is a titled block of bindings.
type KeyGroup struct {
	Title    string
	Bindings []Binding
}

// Key panel tunables.
const (
	// KeyPanelTwoCols is the width from which the key panel sets its groups
	// in two columns.
	KeyPanelTwoCols = 72
	// keyPanelGutter is the air between the panel's two columns.
	keyPanelGutter = 4
	// keyPanelListRows is the least of the list a spanning panel leaves
	// above it: the selection and a row of context.
	keyPanelListRows = 2
	// keyPanelMinCut is the shortest panel that is drawn cut short: its
	// edges, the title, a group's label, a binding and the ▼ count.
	keyPanelMinCut = 6
	// keyPanelMinDesc is the least of a description a narrow two-column
	// panel keeps beside its keys.
	keyPanelMinDesc = 8
	// raiseGutter is how close to the body's left edge a raised box leaves
	// only the gutter beside it.
	raiseGutter = 2
)

// PanelPlace is where KeyPanel puts the panel in its body.
type PanelPlace int

// Key panel places.
const (
	// PanelCorner is as wide as its keys, in the body's bottom-right
	// corner: over the preview, beside the list.
	PanelCorner PanelPlace = iota
	// PanelFill runs across the body, columns 1 to w-2: over a preview
	// well, which it covers whole.
	PanelFill
	// PanelSpan runs across the body over the list's last rows (no
	// preview beside it), and leaves the list's selection above it.
	PanelSpan
)

// KeyPanel renders the "?" key panel for a tool's body of w×h cells, where
// it goes in that body, and how many rows of keys it can scroll by (0 when
// it shows them all). It sits on the body's bottom-right corner (its bottom
// edge on the body's last row, its right end at column w-2, the card's
// column W−3 where the well and the tray's hints end), so it covers the
// preview and never the list's name, context or status columns; place
// says how wide it is. It is an Outline: "Keys" and an esc keycap, then
// each group's label and its rows, a description on the left and its
// keycaps at the column's right edge, in two columns when the panel can be
// KeyPanelTwoCols wide. A panel too short for its keys sets them in two
// narrower columns (when it runs across the body), then drops its blank
// rows, and last shows them from row scroll on, with ▲ ▼ counts on its
// last row (ScrollKeyPanel). Draw it with Raise. A body too small for it
// gets nil.
func (t Theme) KeyPanel(groups []KeyGroup, w, h int, place PanelPlace, scroll int) ([]string, Rect, int) {
	descW, capW := 0, 0
	for _, g := range groups {
		descW = max(descW, ansi.StringWidth(sectionTitle(g.Title)))
		for _, b := range g.Bindings {
			descW = max(descW, ansi.StringWidth(Capitalize(b.Desc)))
			capW = max(capW, t.panelCaps(b.Keys).Width())
		}
	}
	fill := place != PanelCorner
	maxW, maxH := w-3, h
	if fill {
		maxW = w - 2
	}
	if place == PanelSpan {
		maxH = h - keyPanelListRows // the list's selection stays above it
	}
	const chrome = 4 // the ends and a cell of air inside each
	room := func(cols int) int { return (maxW - chrome - (cols-1)*keyPanelGutter) / cols }
	natural := descW + 2 + capW
	cols, colW, gap := 1, min(natural, room(1)), 1
	if maxW >= KeyPanelTwoCols && 2*natural+keyPanelGutter+chrome <= maxW {
		cols, colW = 2, natural
	}
	if colW < 8 || h < 5 {
		return nil, Rect{}, 0
	}
	clipped := func() bool { return 1+gap+len(t.panelRows(groups, cols, colW, gap))+2 > maxH }
	if clipped() && fill && cols == 1 && room(2) >= capW+1+keyPanelMinDesc {
		cols, colW = 2, room(2) // narrower columns, cut descriptions
	}
	if clipped() {
		gap = 0
	}
	if fill {
		colW = room(cols)
	}
	innerW := cols*colW + (cols-1)*keyPanelGutter
	if fill {
		innerW = maxW - chrome
	}
	esc := t.Keycap(CapLabel("esc"))
	head := []Line{Line{{"Keys", t.Text.Bold(true)}}.Fit(innerW - esc.Width()).Append(esc...)}
	for range gap {
		head = append(head, nil)
	}
	body := t.panelRows(groups, cols, colW, gap)
	pw, ph := innerW+chrome, min(maxH, len(head)+len(body)+2)
	more := 0
	if len(head)+len(body)+2 > ph {
		if ph < keyPanelMinCut {
			return nil, Rect{}, 0 // it would show a group's label and no keys
		}
		shown := ph - 3 - len(head) // the last row holds the counts
		more = len(body) - shown
		scroll = max(0, min(scroll, more))
		counts := Line{{GlyphUp + " " + strconv.Itoa(scroll), t.Subtle}, Space(2), {GlyphDown + " " + strconv.Itoa(more-scroll), t.Subtle}}
		switch {
		case scroll == 0:
			counts = counts[2:]
		case scroll == more:
			counts = counts[:1]
		}
		body = append(body[scroll:scroll+shown:scroll+shown], Line{Space(innerW - counts.Width())}.Append(counts...))
	}
	out := []string{t.OutlineEdge(pw, true)}
	for _, l := range append(head, body...) {
		out = append(out, t.OutlineRow(l.Fit(innerW).RenderBg(Clear)))
	}
	out = append(out, t.OutlineEdge(pw, false))
	return out, Rect{w - 1 - pw, h - ph, pw, ph}, more
}

// panelRows lays the key panel's groups out in cols columns colW cells
// wide, with gap blank rows between groups. Descriptions are cut to leave
// their keycaps room.
func (t Theme) panelRows(groups []KeyGroup, cols, colW, gap int) []Line {
	blocks := make([][]Line, len(groups))
	for i, g := range groups {
		blocks[i] = append(blocks[i], Line{{sectionTitle(g.Title), t.Muted.Bold(true)}})
		for _, b := range g.Bindings {
			caps := t.panelCaps(b.Keys)
			desc := Line{{Capitalize(b.Desc), t.Text}}.Truncate(max(0, colW-caps.Width()-1))
			blocks[i] = append(blocks[i], desc.Fit(colW-caps.Width()).Append(caps...))
		}
	}
	columns := balance(blocks, cols, gap)
	rows := 0
	for _, c := range columns {
		rows = max(rows, len(c))
	}
	out := make([]Line, rows)
	for y := range out {
		for c, col := range columns {
			if c > 0 {
				out[y] = append(out[y], Space(keyPanelGutter))
			}
			var cell Line
			if y < len(col) {
				cell = col[y]
			}
			out[y] = append(out[y], cell.Fit(colW)...)
		}
	}
	return out
}

// An outline is how the key panel, a sheet and a preview's well stand
// apart from the float: a rounded hairline in Stroke around cells left
// Clear, so Rex's own float surface shows inside them as it does around
// them. No fill and no shadow: in Rex's blended and glass interface styles
// the float's surface is translucent, and every cell given a background of
// its own is drawn solid.

// OutlineEdge is an outline's top (or bottom) row, w cells wide.
func (t Theme) OutlineEdge(w int, top bool) string {
	if w < 2 {
		return Line{Space(w)}.RenderBg(Clear)
	}
	l, r := "╰", "╯"
	if top {
		l, r = "╭", "╮"
	}
	return Line{{l + strings.Repeat("─", w-2) + r, t.stroke()}}.RenderBg(Clear)
}

// OutlineRow puts content (already rendered, on Clear) between an
// outline's sides with a cell of air inside each: it is two cells wider
// than content on each side.
func (t Theme) OutlineRow(content string) string {
	side := Line{{"│", t.stroke()}, Space(1)}.RenderBg(Clear)
	return side + content + Line{Space(1), {"│", t.stroke()}}.RenderBg(Clear)
}

func (t Theme) stroke() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.C.Stroke) }

// ScrollKeyPanel is where key moves a key panel that can scroll by more
// rows (KeyPanel's third result) from scroll: ↑ ↓ by a row, pgup pgdn to
// its ends. ok is false for any other key, or when the panel shows every
// key; such a key closes the panel as usual.
func ScrollKeyPanel(scroll, more int, key string) (int, bool) {
	if more <= 0 {
		return scroll, false
	}
	switch key {
	case "up":
		return max(0, scroll-1), true
	case "down":
		return min(more, scroll+1), true
	case "pgup":
		return 0, true
	case "pgdown":
		return more, true
	}
	return scroll, false
}

// panelCaps draws a binding's alternative keys as keycaps a space apart.
func (t Theme) panelCaps(keys []string) Line {
	var out Line
	for i, k := range keys {
		if i > 0 {
			out = append(out, Space(1))
		}
		out = append(out, t.Keycap(CapLabel(k))...)
	}
	return out
}

// balance splits groups into n columns of similar height, keeping groups
// whole and in order, with gap blank lines between groups.
func balance(blocks [][]Line, n, gap int) [][]Line {
	total := 0
	for _, b := range blocks {
		total += len(b) + gap
	}
	target := (total + n - 1) / n
	cols := make([][]Line, n)
	c := 0
	for _, b := range blocks {
		if c < n-1 && len(cols[c]) > 0 && len(cols[c])+len(b) > target {
			c++
		}
		for i := 0; i < gap && len(cols[c]) > 0; i++ {
			cols[c] = append(cols[c], Line{})
		}
		cols[c] = append(cols[c], b...)
	}
	return cols[:max(1, c+1)]
}

// Raise draws box (a KeyPanel or a Sheet) over base at at: it covers what
// is under it whole. To its right, on its rows, is plain float (Clear), so
// nothing under it (a list's overflow marker, a selection's wash) peeks
// out; so is the gutter left of a box that spans the body (a row's ▎). A
// wide character the box's edge cuts in half becomes a blank. base lines
// must be equally wide.
func (t Theme) Raise(base, box []string, at Rect) []string {
	if len(base) == 0 || len(box) == 0 || at.Empty() {
		return base
	}
	w, h := ansi.StringWidth(base[0]), len(base)
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(strings.Join(base, "\n")).Draw(buf, buf.Bounds())
	float := func() *uv.Cell {
		blank := uv.EmptyCell
		blank.Style.Bg = Clear
		return &blank
	}
	for y := at.Y; y < min(h, at.Y+at.H); y++ {
		for x := at.X + at.W; x < w; x++ {
			buf.SetCell(x, y, float())
		}
		for x := 0; at.X <= raiseGutter && x < at.X; x++ {
			buf.SetCell(x, y, float())
		}
		for _, x := range []int{at.X, at.X + at.W} {
			if c := buf.CellAt(x, y); c != nil && c.Width == 0 {
				if lead := buf.CellAt(x-1, y); lead != nil {
					blank := lead.Clone()
					blank.Empty()
					buf.SetCell(x-1, y, blank) // blanks both halves
				}
			}
		}
	}
	uv.NewStyledString(strings.Join(box, "\n")).Draw(buf, uv.Rect(at.X, at.Y, at.W, at.H))
	return strings.Split(strings.ReplaceAll(buf.Render(), "\r\n", "\n"), "\n")
}

// EmptyStateMinRows is the shortest body that gets the centred EmptyState;
// a shorter one gets its one-line form.
const EmptyStateMinRows = 6

// EmptyState fills a w×h body with a centred page: title a
// third of the way down, then, a row of air apart, detail and the ways out
// as hints, so an empty card reads as a deliberate page and not as a list
// that failed. Build the title with bold primary text, or with Quoted for
// what was typed ("No matches for “zqxj”"); detail is usually secondary
// text. Under EmptyStateMinRows it is one line: the title, then the ways
// after " · " ("No matches for “zqxj” · ▐bksp▌ edit").
func (t Theme) EmptyState(title, detail Line, ways []Hint, w, h int) []string {
	out := make([]string, max(0, h))
	blank := strings.Repeat(" ", max(0, w))
	for y := range out {
		out[y] = blank
	}
	if w <= 0 || h <= 0 {
		return out
	}
	center := func(l Line) string {
		l = l.Truncate(w)
		return Line{Space((w - l.Width()) / 2)}.Append(l...).Fit(w).Render()
	}
	if h < EmptyStateMinRows {
		line := title
		if hints := t.Hints(ways, max(0, w-title.Width()-5)); len(hints) > 0 { // no " · " before nothing
			line = append(append(line[:len(line):len(line)], Span{" " + Dot + " ", t.Subtle}), hints...)
		}
		out[0] = Line{Space(1)}.Append(line...).Fit(w).Render()
		return out
	}
	y := max(0, h/3-1)
	out[y] = center(title)
	if len(detail) > 0 && y+2 < h {
		y += 2
		out[y] = center(detail)
	}
	if len(ways) > 0 && y+2 < h {
		out[y+2] = center(t.Hints(ways, w-2))
	}
	return out
}

// Quoted is an empty state's title about what was typed: lead in bold, then
// typed in curly quotes, in accent ("No matches for “zqxj”"). typed is
// sanitized (sanitize.Name): a pasted query can hold control characters.
func (t Theme) Quoted(lead, typed string) Line {
	bold := t.Text.Bold(true)
	return Line{{lead + "“", bold}, {sanitize.Name(typed), t.Accent.Bold(true)}, {"”", bold}}
}

// ErrorState is EmptyState for a list that could not load:
// "✗ " and title in bold error, the reason (rex.UserMessage, never an error
// chain) in the secondary colour, and the ways out.
func (t Theme) ErrorState(title, reason string, ways []Hint, w, h int) []string {
	head := Line{{GlyphFailed + " " + title, t.Err.Bold(true)}}
	var why Line
	if reason != "" {
		why = Line{{reason, t.Muted}}
	}
	return t.EmptyState(head, why, ways, w, h)
}

// LoadingState is a body with nothing to show yet: the
// spinner and label ("Reading Rex…") centred a third of the way down. Show
// it once the load has taken SpinnerDelay (Spinner.Visible).
func (t Theme) LoadingState(label string, now time.Time, w, h int) []string {
	return t.EmptyState(t.Spinning(label, now), nil, nil, w, h)
}
