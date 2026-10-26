package backdrop

import (
	"math"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Where Rex draws pane content inside a window, in cells, calibrated against
// screenshots of the user's app (Rex Beta 0.1.0 (1408), merino-dark,
// comfortable density) with \`rexkit calibrate\`: a 199×57 window split into a
// 97×55 pane on the left and two 97×26 panes stacked on the right. Their
// content starts at (0, 2), (101, 2) and (101, 31):
//
//   - every pane has a title bar of HeaderRows above its content, and the
//     window has no other inset (a pane at the left or top edge starts at
//     column 0 or right under its title bar);
//   - a pane that does not start at the window's left or top edge starts
//     GapCols or GapRows (half of the gap between two panes, which is the
//     same in pixels both ways) after its rect's edge.
//
// Positions are rounded to the nearest cell.
const (
	HeaderRows = 2
	GapCols    = 1.2
	GapRows    = 0.6
)

// Place returns where a pane's cols×rows content sits in a w×h window, given
// the pane's rect as fractions of the window. The result may stick out of
// the window; callers clip.
func Place(r rex.Rect, cols, rows, w, h int) ui.Rect {
	return ui.Rect{X: lead(r[0], w, GapCols), Y: lead(r[1], h, GapRows) + HeaderRows, W: cols, H: rows}
}

// paneSize is about the size Rex gives a pane with rect r in a w×h window
// (Demo sizes its made-up panes with it; real panes report their size): up
// to half a gap before the rect's far edge, unless that is the window's.
func paneSize(r rex.Rect, w, h int) (cols, rows int) {
	at := Place(r, 0, 0, w, h)
	cols = trail(r[0]+r[2], w, GapCols) - at.X
	rows = trail(r[1]+r[3], h, GapRows) - at.Y
	return max(cols, 1), max(rows, 1)
}

// lead is the first cell after a rect edge at fraction f of n cells.
func lead(f float64, n int, gap float64) int {
	if f <= 0 {
		return 0
	}
	return int(math.Round(f*float64(n) + gap))
}

// trail is the cell after the last one before a rect edge at fraction f.
func trail(f float64, n int, gap float64) int {
	if f >= 1 {
		return n
	}
	return int(math.Round(f*float64(n) - gap))
}
