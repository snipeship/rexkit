package ui

// Col describes one column of a row layout.
type Col struct {
	// Width fixes the column width. Zero sizes it from its content (the
	// natural width passed to Fit), bounded by Min and Max.
	Width    int
	Min, Max int
	// Flex marks the main column (the name). It gets the space other columns
	// leave, down to Min; at most one column should flex.
	Flex bool
	// Shrink lets the column give up width (down to Min) before the flex
	// column does; use it for context such as "session › tab".
	Shrink bool
	// Drop orders which columns disappear when even shrinking is not enough:
	// 1 goes first, then 2, …; 0 never drops.
	Drop int
	// Gap is the number of blank cells before the column (ignored for the
	// first visible column).
	Gap int
	// Right aligns content to the right edge (sizes, ages).
	Right bool
	// KeepEnd truncates from the start, keeping the end (paths).
	KeepEnd bool
}

// Columns resolves a row layout once per frame so that every row lines up.
type Columns []Col

// Grid is a resolved layout: a width per column, 0 for dropped columns.
type Grid struct {
	cols   Columns
	widths []int
	width  int
}

// Fit resolves widths for rows of total width w. natural holds each column's
// content width (the widest cell among the rows being laid out; see Measure).
// Space left over goes to the flex column, so the columns after it (status,
// size, age) sit against the row's right edge, where they stay as the rows
// change: pass them a natural width that does not depend on which rows are
// shown, and typing or drilling never moves them.
func (cs Columns) Fit(w int, natural []int) Grid {
	g := Grid{cols: cs, widths: make([]int, len(cs)), width: max(0, w)}
	nat := func(i int) int {
		if i < len(natural) {
			return natural[i]
		}
		return 0
	}
	floor := make([]int, len(cs))
	for i, c := range cs {
		switch {
		case c.Width > 0:
			g.widths[i], floor[i] = c.Width, c.Width
		default:
			v := nat(i)
			if !c.Flex && !c.Shrink {
				v = max(v, c.Min) // for flexible columns Min is a floor, not a size
			}
			if c.Max > 0 {
				v = min(v, c.Max)
			}
			g.widths[i] = v
			floor[i] = v
			if c.Flex || c.Shrink {
				floor[i] = min(v, c.Min)
			}
		}
	}
	over := g.total() - g.width
	// Shrink context first, then the flex column, each down to its floor.
	for _, flex := range []bool{false, true} {
		for i, c := range cs {
			if over <= 0 {
				break
			}
			if (flex && c.Flex) || (!flex && c.Shrink && !c.Flex) {
				give := min(over, g.widths[i]-floor[i])
				g.widths[i] -= give
				over -= give
			}
		}
	}
	// Then drop columns in order, giving the room back to the flex column.
	last := 0
	for _, c := range cs {
		last = max(last, c.Drop)
	}
	for rank := 1; over > 0 && rank <= last; rank++ {
		for i, c := range cs {
			if c.Drop == rank {
				g.widths[i] = 0
			}
		}
		over = g.total() - g.width
	}
	if over < 0 {
		g.grow(-over)
	}
	// Finally squeeze the flex column, then anything else, to fit at all.
	g.squeeze(g.total() - g.width)
	return g
}

// grow gives the room left over to the flex column.
func (g *Grid) grow(room int) {
	for i, c := range g.cols {
		if c.Flex && g.widths[i] > 0 {
			g.widths[i] += room
			return
		}
	}
}

func (g *Grid) squeeze(over int) {
	for _, flexFirst := range []bool{true, false} {
		for i := len(g.cols) - 1; i >= 0 && over > 0; i-- {
			if g.cols[i].Flex == flexFirst && g.widths[i] > 0 {
				give := min(over, g.widths[i])
				g.widths[i] -= give
				over -= give
			}
		}
	}
}

func (g Grid) total() int {
	sum, first := 0, true
	for i, w := range g.widths {
		if w == 0 {
			continue
		}
		if !first {
			sum += g.cols[i].Gap
		}
		sum += w
		first = false
	}
	return sum
}

// Width returns column i's resolved width (0 if dropped).
func (g Grid) Width(i int) int { return g.widths[i] }

// Visible reports whether column i survived the layout.
func (g Grid) Visible(i int) bool { return g.widths[i] > 0 }

// Row lays out cells (one per column; missing cells are blank) into a line
// exactly as wide as the grid.
func (g Grid) Row(cells ...Line) Line {
	var out Line
	first := true
	for i, c := range g.cols {
		w := g.widths[i]
		if w == 0 {
			continue
		}
		if !first && c.Gap > 0 {
			out = append(out, Space(c.Gap))
		}
		first = false
		var cell Line
		if i < len(cells) {
			cell = cells[i]
		}
		switch {
		case c.Right:
			cell = cell.FitRight(w)
		case c.KeepEnd:
			cell = cell.FitStart(w)
		default:
			cell = cell.Fit(w)
		}
		out = append(out, cell...)
	}
	return out.Fit(g.width)
}

// Measure returns the natural width of each column over a set of rows.
func Measure(rows [][]Line, columns int) []int {
	out := make([]int, columns)
	for _, row := range rows {
		for i, cell := range row {
			if i < columns {
				out[i] = max(out[i], cell.Width())
			}
		}
	}
	return out
}
