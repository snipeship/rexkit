package ui

import (
	"sort"
	"strings"
)

// Breakpoints shared by every tool.
const (
	WideCols    = 120 // list | preview side by side
	MediumCols  = 80  // stacked or on-demand split
	StackedRows = 26  // medium floats this tall stack the preview under the list
	// MaxListCols caps the list in side-by-side mode so meta hugs names.
	MaxListCols = 80
	// StackedMinList is the least a short list stacked over the preview
	// keeps, so the preview does not jump as each letter is typed.
	StackedMinList = 5
)

// PreviewMode is how the preview is placed.
type PreviewMode int

// Preview placements, by available size.
const (
	PreviewNone    PreviewMode = iota // list only
	PreviewSide                       // ≥120 cols: list (44% by default) | preview
	PreviewStacked                    // 80–119 cols, ≥26 rows: list ~60% (less when short) over preview
	PreviewSplit                      // 80–119 cols, short: right half, on demand (⌃/)
	PreviewSheet                      // <80 cols: bottom sheet ~40%, on demand (⌃/)
)

// PreviewPref is the user's ⌃/ choice.
type PreviewPref int

// Preview preferences.
const (
	PreviewAuto PreviewPref = iota // shown where there is room for it by default
	PreviewOn
	PreviewOff
)

// Toggle returns the preference after ⌃/, given whether the preview is
// currently visible.
func (p PreviewPref) Toggle(visible bool) PreviewPref {
	if visible {
		return PreviewOff
	}
	return PreviewOn
}

// LayoutOptions tune Arrange.
type LayoutOptions struct {
	Preview   PreviewPref
	NoPreview bool // the tool has no preview at all
	SubRow    bool // reserve a row under the header (breadcrumb, path field)
	// ListShare is the list's percentage of the width beside a preview
	// (0 means DefaultListShare); list-heavy tools ask for more.
	ListShare int
	// Card: the tool is drawn on a card (Frame.Card), whose bottom lip
	// closes the tray. Without one a banded tray gets a row of air along
	// the window's bottom (TrayAirRows) once the window has TrayAirMinRows.
	Card bool
	// ListRows is how many rows the list has to show (List.Rows), 0 when
	// unknown: a short list stacked over the preview leaves it the rest.
	ListRows int
}

// The chrome's budget: a content area at least
// BandMinRows tall and CompactCols wide gets the banded chrome (HeaderBand,
// a tray with a lip); a smaller one the compact chrome, a row each.
const (
	BandMinRows    = 16
	CompactCols    = 44
	TrayAirMinRows = 20
	// PreviewGap is the air between the list and the preview beside it:
	// no divider, the well stands apart by itself.
	PreviewGap = 3
)

// Banded reports whether a w×h content area gets the banded chrome.
func Banded(w, h int) bool { return h >= BandMinRows && w >= CompactCols }

// DefaultListShare is the list's share of the width in PreviewSide mode.
const DefaultListShare = 44

// Areas is the arrangement of a tool's screen, relative to its content area.
type Areas struct {
	Mode    PreviewMode
	Header  Rect
	Sub     Rect
	Body    Rect // everything between the header (and Sub) and the footer
	List    Rect
	Preview Rect
	Footer  Rect // the tray
}

// InBody returns r (in content coordinates) relative to the body, for
// composing the body on its own (Frame.Render takes it apart from the
// header and the tray).
func (a Areas) InBody(r Rect) Rect { return r.Offset(-a.Body.X, -a.Body.Y) }

// HeaderLine is the row of the header's own line (inside a band, under
// its top edge).
func (a Areas) HeaderLine() int {
	if a.Header.H >= HeaderRows {
		return a.Header.Y + 1
	}
	return a.Header.Y
}

// Banded reports whether the header is a band and the footer a tray with
// a lip.
func (a Areas) Banded() bool { return a.Header.H >= HeaderRows }

// PreviewVisible reports whether the arrangement shows a preview.
func (a Areas) PreviewVisible() bool { return a.Mode != PreviewNone }

// Arrange lays out a w×h content area: the header (a band, or a row), an
// optional sub row, the body, and the tray. Beside the list the preview
// stands PreviewGap cells off; under it, a row of air off. At tiny sizes
// chrome disappears before the list does.
func Arrange(w, h int, o LayoutOptions) Areas {
	w, h = max(0, w), max(0, h)
	var a Areas
	y, bottom := 0, h
	switch {
	case Banded(w, h):
		tray := TrayRows
		if !o.Card && h >= TrayAirMinRows {
			tray = TrayAirRows
		}
		a.Header = Rect{0, 0, w, HeaderRows}
		a.Footer = Rect{0, h - tray, w, tray}
		y, bottom = HeaderRows, h-tray
	default:
		if h >= 2 {
			a.Header = Rect{0, 0, w, 1}
			y = 1
		}
		if h >= 3 {
			a.Footer = Rect{0, h - 1, w, 1}
			bottom = h - 1
		}
	}
	if o.SubRow && bottom-y >= 4 {
		a.Sub = Rect{0, y, w, 1}
		y++
	}
	body := Rect{0, y, w, bottom - y}
	a.Body, a.List = body, body
	if o.NoPreview || body.Empty() {
		return a
	}
	mode, byDefault := previewMode(w, h)
	switch {
	case o.Preview == PreviewOff,
		o.Preview == PreviewAuto && !byDefault:
		return a
	}
	switch mode {
	case PreviewSide, PreviewSplit:
		listW := w / 2
		if mode == PreviewSide {
			share := o.ListShare
			if share <= 0 {
				share = DefaultListShare
			}
			listW = min(w*share/100, MaxListCols)
		}
		a.List.W = listW
		a.Preview = Rect{listW + PreviewGap, y, max(0, w-listW-PreviewGap), body.H}
	case PreviewStacked, PreviewSheet:
		if body.H < 6 {
			return a // too short to show both
		}
		listH := body.H * 60 / 100
		if mode == PreviewSheet {
			listH = body.H - max(3, body.H*40/100)
		} else if o.ListRows > 0 {
			listH = min(listH, max(o.ListRows, StackedMinList))
		}
		a.List.H = listH
		a.Preview = Rect{0, y + listH + 1, w, body.H - listH - 1} // a row of air between them
	}
	a.Mode = mode
	return a
}

// previewMode returns where a preview fits at this size and whether it is
// shown without being asked for.
func previewMode(w, h int) (PreviewMode, bool) {
	switch {
	case w >= WideCols:
		return PreviewSide, true
	case w >= MediumCols && h >= StackedRows:
		return PreviewStacked, true
	case w >= MediumCols:
		return PreviewSplit, false
	default:
		return PreviewSheet, false
	}
}

// Part is a block of rendered lines placed at a rectangle.
type Part struct {
	Rect  Rect
	Lines []string
}

// Compose paints parts onto a w×h screen and returns its lines, each exactly
// w cells. Parts must not overlap (Theme.Raise lays one over the rest);
// uncovered cells are blank.
func Compose(w, h int, parts ...Part) []string {
	out := make([]string, max(0, h))
	for y := range out {
		var row []Part
		for _, p := range parts {
			if y >= p.Rect.Y && y < p.Rect.Y+p.Rect.H && !p.Rect.Empty() {
				row = append(row, p)
			}
		}
		sort.SliceStable(row, func(i, j int) bool { return row[i].Rect.X < row[j].Rect.X })
		var b strings.Builder
		x := 0
		for _, p := range row {
			if p.Rect.X < x || p.Rect.X >= w {
				continue
			}
			b.WriteString(strings.Repeat(" ", p.Rect.X-x))
			line := ""
			if i := y - p.Rect.Y; i < len(p.Lines) {
				line = p.Lines[i]
			}
			cw := min(p.Rect.W, w-p.Rect.X)
			b.WriteString(Fit(line, cw))
			x = p.Rect.X + cw
		}
		if x < w {
			b.WriteString(strings.Repeat(" ", w-x))
		}
		out[y] = b.String()
	}
	return out
}
