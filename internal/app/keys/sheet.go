package keys

import (
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

// Sheet geometry.
const (
	colGap    = 4  // between columns: air
	minColW   = 30 // narrowest column worth a second column
	maxColW   = 56 // widest a column gets, so titles stay near their keys
	maxKeyW   = 14 // key cells wider than this push their title right
	maxTitleW = 30 // longest title used to size columns; longer ones may truncate
	keyGap    = 3  // between a column's key cells and their titles
	wideGap   = 2  // after a key wider than its column's key cells
	minRows   = 3  // the shortest body set in bounded columns: a header and two entries
)

// cell is one row of one column: a group header, an entry, or blank.
type cell struct {
	group int  // index into the view; -1 for a blank separator
	entry int  // index into the group's hits; -1 for the group header
	cont  bool // a header repeated over the rest of a group that is split
}

// pos addresses a cell.
type pos struct{ col, row int }

// sheet is the view set in columns, newspaper style, each group whole in
// one column. With room for one column only it is a single column of
// unbounded height that scrolls vertically.
type sheet struct {
	cols    [][]cell
	keyW    []int // per column, the width of its key cells
	visible int   // columns on screen
	colW    int
	height  int   // rows per column; 0 when unbounded
	order   []pos // entries in reading order
}

// layOut sets view in a w×h area: in as few columns as hold it, balanced.
// Groups are kept whole and in order; a group is split only when it is
// taller than the area. When the columns do not fit side by side the sheet
// scrolls by whole columns.
func layOut(t ui.Theme, g Guide, view []shown, w, h int) sheet {
	keyW, titleW, headW := 0, 0, 0
	for _, s := range view {
		grp := g.Groups[s.group]
		headW = max(headW, 1+head(t, grp, false).Width())
		for _, hit := range s.entries {
			e := grp.Entries[hit.entry]
			if !e.Exit { // "⎋ ⏎ ⌃C ⌃P" would push every title right
				keyW = max(keyW, ansi.StringWidth(e.KeysLabel()))
			}
			titleW = max(titleW, ansi.StringWidth(e.Title)+2) // + " ⚠"
		}
	}
	natural := max(minColW, headW, 1+min(keyW, maxKeyW)+keyGap+min(titleW, maxTitleW))
	most := max(1, (w+colGap)/(natural+colGap))

	var sh sheet
	if most == 1 || h < minRows {
		sh.cols = [][]cell{stack(units(view, 0))}
		sh.visible, sh.colW = 1, w
	} else {
		sh.height = h
		sh.cols = balance(units(view, h), h)
		sh.visible = min(most, max(1, len(sh.cols)))
		sh.colW = min(maxColW, (w-colGap*(sh.visible-1))/sh.visible)
	}
	sh.keyW = make([]int, len(sh.cols))
	for c, col := range sh.cols {
		for r, cl := range col {
			if cl.group < 0 || cl.entry < 0 {
				continue
			}
			sh.order = append(sh.order, pos{c, r})
			s := view[cl.group]
			if e := g.Groups[s.group].Entries[s.entries[cl.entry].entry]; !e.Exit {
				sh.keyW[c] = max(sh.keyW[c], min(maxKeyW, ansi.StringWidth(e.KeysLabel())))
			}
		}
	}
	return sh
}

// units cuts the view into the blocks a column takes whole: a group, its
// header first. A group taller than h rows (h > 0) is cut into as few parts
// as fit, of near equal size, each under a header (a repeated one after the
// first), so no part is left with a lone entry.
func units(view []shown, h int) [][]cell {
	var out [][]cell
	for gi, s := range view {
		n := len(s.entries)
		parts := 1
		if h > 0 && n+1 > h {
			parts = (n + h - 2) / (h - 1)
		}
		ei := 0
		for p := range parts {
			size := n / parts
			if p < n%parts {
				size++
			}
			u := []cell{{group: gi, entry: -1, cont: p > 0}}
			for range size {
				u = append(u, cell{group: gi, entry: ei})
				ei++
			}
			out = append(out, u)
		}
	}
	return out
}

// stack sets units one under the other, a blank row between them.
func stack(us [][]cell) []cell {
	var col []cell
	for i, u := range us {
		if i > 0 {
			col = append(col, cell{group: -1, entry: -1})
		}
		col = append(col, u...)
	}
	return col
}

// balance splits units, in order, into the fewest columns at most h rows
// tall, balanced: a linear partition that makes the tallest column as
// short as it can be, so no column stands half empty beside a full one.
func balance(us [][]cell, h int) [][]cell {
	if len(us) == 0 {
		return nil
	}
	// height[i][j] is the height of units i..j-1 stacked in one column.
	height := func(i, j int) int {
		n := j - i - 1 // blank rows between them
		for _, u := range us[i:j] {
			n += len(u)
		}
		return n
	}
	// best[k][j] is the tallest column when units 0..j-1 fill k columns;
	// cut[k][j] where the last of those columns starts.
	const inf = 1 << 30
	n := len(us)
	best := [][]int{make([]int, n+1)}
	cut := [][]int{make([]int, n+1)}
	for j := 1; j <= n; j++ {
		best[0][j] = inf
	}
	k := 0
	for k < n {
		k++
		row, from := make([]int, n+1), make([]int, n+1)
		for j := 1; j <= n; j++ {
			row[j] = inf
			for i := k - 1; i < j; i++ {
				// On a tie the later cut wins: earlier columns fill first.
				if v := max(best[k-1][i], height(i, j)); v <= row[j] {
					row[j], from[j] = v, i
				}
			}
		}
		best, cut = append(best, row), append(cut, from)
		if best[k][n] <= h {
			break
		}
	}
	cols := make([][]cell, k)
	for j, c := n, k; c > 0; c-- {
		i := cut[c][j]
		cols[c-1] = stack(us[i:j])
		j = i
	}
	return cols
}

// rows is the number of rows the sheet's content needs.
func (s sheet) rows() int {
	if s.height > 0 {
		return s.height
	}
	if len(s.cols) == 0 {
		return 0
	}
	return len(s.cols[0])
}

// at returns the cell at p, if there is one.
func (s sheet) at(p pos) (cell, bool) {
	if p.col < 0 || p.col >= len(s.cols) || p.row < 0 || p.row >= len(s.cols[p.col]) {
		return cell{}, false
	}
	return s.cols[p.col][p.row], true
}

// index returns the reading-order index of the entry at p, or -1.
func (s sheet) index(p pos) int {
	for i, q := range s.order {
		if q == p {
			return i
		}
	}
	return -1
}

// beside returns the entry in the column next to sel (dir -1 left, +1 right)
// nearest to sel's row, or sel when there is none.
func (s sheet) beside(sel, dir int) int {
	if sel < 0 || sel >= len(s.order) {
		return sel
	}
	from := s.order[sel]
	best, dist := sel, 1<<30
	for i, p := range s.order {
		if p.col != from.col+dir {
			continue
		}
		if d := abs(p.row - from.row); d < dist {
			best, dist = i, d
		}
	}
	return best
}

// viewport is the part of the sheet on screen: whole columns from col, or,
// for a single unbounded column, rows from row.
type viewport struct{ col, row int }

// follow scrolls vp the least that brings entry sel into an h-row view.
func (s sheet) follow(vp viewport, sel, h int) viewport {
	if sel < 0 || sel >= len(s.order) {
		return viewport{}
	}
	p := s.order[sel]
	if s.height == 0 {
		vp.col = 0
		top := p.row
		if top > 0 { // keep the group header in view when it is just above
			if c, _ := s.at(pos{0, top - 1}); c.entry < 0 && c.group >= 0 {
				top--
			}
		}
		vp.row = clamp(vp.row, p.row-h+1, top)
		vp.row = clamp(vp.row, 0, max(0, s.rows()-h))
		return vp
	}
	vp.row = 0
	vp.col = clamp(vp.col, p.col-s.visible+1, p.col)
	vp.col = clamp(vp.col, 0, max(0, len(s.cols)-s.visible))
	return vp
}

// hidden counts entries before and after the viewport.
func (s sheet) hidden(vp viewport, h int) (before, after int) {
	for _, p := range s.order {
		switch {
		case s.height == 0 && p.row < vp.row, s.height > 0 && p.col < vp.col:
			before++
		case s.height == 0 && p.row >= vp.row+h, s.height > 0 && p.col >= vp.col+s.visible:
			after++
		}
	}
	return before, after
}

// hitTest maps a body cell (x, y) to an entry index, or -1.
func (s sheet) hitTest(vp viewport, x, y int) int {
	stride := s.colW + colGap
	if x < 0 || y < 0 || stride <= 0 || x%stride >= s.colW {
		return -1
	}
	return s.index(pos{vp.col + x/stride, vp.row + y})
}

// head is a group's header: the mode's name, then the key that enters it
// as a keycap and what follows ("Pane  ▐ctrl p▌ then…"), and "(cont.)" over
// the rest of a split group.
func head(t ui.Theme, g Group, cont bool) ui.Line {
	line := ui.Line{{Text: g.Name(), Style: t.Section}}
	if g.Prefix != nil {
		tail := " then…"
		if g.Once {
			tail = " then one key"
		}
		line = append(line, ui.Space(2))
		line = append(line, t.Keycap(ui.CapLabel(g.Prefix.Chord()))...)
		line = append(line, ui.Span{Text: tail, Style: t.Subtle})
	}
	if cont {
		line = append(line, ui.Span{Text: " (cont.)", Style: t.Subtle})
	}
	return line
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
