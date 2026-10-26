package ui

import (
	"image/color"
	"slices"
	"strings"
)

// The narrowest the search field gets before other header parts are given
// up for it: in the band (with its ends and air) and in the compact header.
const (
	minField = 20
	minQuery = 14
	// roomyField is how much more than that the field keeps before it
	// shrinks for the title, the chips' keys, the git badge or the outer
	// crumbs: on a short header those go first.
	roomyField = 8
)

// Header is a tool's header:
//
//	Title  crumb › crumb › here  ⎇ main  ( in studio › web × )  ( Search… )  (All) Sessions   Agents²
type Header struct {
	Title  string   // the tool's name; dropped first when the header is short or narrow
	Crumbs []string // breadcrumb, outermost first; the last is where you are
	Scope  string   // a removable scope ("studio › web"), "" for none
	Info   Line     // extra context after the breadcrumb (git: ⎇ main ↑1 ●3)
	Query  Query
	Chips  []Chip
}

// HeaderHits locates the header's clickable parts, in the coordinates of
// its text row (row 0). Parts that were dropped for space have empty rects.
type HeaderHits struct {
	Crumbs []Rect
	Scope  Rect
	Query  Rect
	Chips  []Rect
	Cursor int // query cursor column, -1 when the field is not focused
	// Shapes are the Control shapes on the text row (the field, the scope,
	// filled chips), whose edge rows the band draws above and below it.
	Shapes []Shape
}

// HeaderRows is the height of the header band: the controls' top edges,
// the text row, their bottom edges, and a row of air.
const HeaderRows = 4

// HeaderBand renders the header as HeaderRows lines of w cells: the field,
// the scope and the active chips are Control shapes two rows and a third
// tall. The text row is row 1; hits are in its coordinates.
func (t Theme) HeaderBand(h Header, w int) ([]string, HeaderHits) {
	line, hits := t.header(h, w, true)
	return []string{
		EdgeRow(w, true, t.C.Surface, hits.Shapes...),
		line,
		EdgeRow(w, false, t.C.Surface, hits.Shapes...),
		strings.Repeat(" ", max(0, w)),
	}, hits
}

// Header renders the compact header into exactly w cells:
// one row with no shapes, the field a "❯" and the query on the card, the
// active chip in bold accent.
func (t Theme) Header(h Header, w int) (string, HeaderHits) { return t.header(h, w, false) }

// headerPlan is how much of the header is shown; header degrades it step by
// step until the field has room.
type headerPlan struct {
	title, info, chipKeys bool
	crumbsFrom            int    // first crumb shown; earlier ones collapse to "…"
	crumbWidth            int    // >0: cap the breadcrumb width
	scopeWidth            int    // >0: cap the scope's label
	dropped               []bool // chips given up for space
}

// header lays the header out into w cells, banded (shapes) or compact.
func (t Theme) header(h Header, w int, banded bool) (string, HeaderHits) {
	hits := HeaderHits{Crumbs: make([]Rect, len(h.Crumbs)), Chips: make([]Rect, len(h.Chips)), Cursor: -1}
	if w <= 0 {
		return "", hits
	}
	least := minQuery
	if banded {
		least = minField
	}
	// In the narrow layout the title is the first thing to go, before any
	// chip.
	narrow := w < MediumCols
	plan := headerPlan{title: h.Title != "" && !narrow, info: len(h.Info) > 0, chipKeys: true, dropped: make([]bool, len(h.Chips))}
	drop := func(i int) func() { return func() { plan.dropped[i] = true } }
	steps := []func(){
		func() { plan.chipKeys = false },
		func() { plan.info = false },
		func() { plan.crumbsFrom = max(plan.crumbsFrom, len(h.Crumbs)-2) },
		func() { plan.title = false },
	}
	extras := len(steps) // the steps that keep the field roomy
	quiet, kept := chipDropOrder(h.Chips)
	for _, i := range quiet {
		steps = append(steps, drop(i))
	}
	steps = append(steps,
		func() { plan.crumbsFrom = max(plan.crumbsFrom, len(h.Crumbs)-1) },
		func() { plan.crumbWidth = 16 },
		func() { plan.scopeWidth = 8 },
	)
	for _, i := range kept {
		steps = append(steps, drop(i))
	}
	for i, degrade := range steps {
		want := least
		if i < extras {
			want += roomyField
		}
		if w-headerFixed(t.headerLeft(h, plan, banded, nil), t.headerRight(h, plan, banded, nil)) >= want {
			if i < extras {
				continue // the extras are kept; the chips may still have to go
			}
			break
		}
		degrade()
	}

	// Lay out for real, now recording hit rects.
	left := t.headerLeft(h, plan, banded, &hits)
	right := t.headerRight(h, plan, banded, &hits)
	queryX := 1 + left.Width()
	if len(left) > 0 {
		queryX += 2
	}
	queryW := max(0, w-headerFixed(left, right))
	var field Line
	var cursor int
	if banded && queryW >= 5 {
		field, cursor = t.Query(h.Query, queryW-4, true)
		field = Control(field, t.C.Field)
		if cursor >= 0 {
			cursor += 2 // the field's end and its air
		}
		hits.Shapes = append(hits.Shapes, ControlShape(queryX, queryW, t.C.Field))
	} else {
		field, cursor = t.Query(h.Query, queryW, false)
	}
	hits.Query = Rect{queryX, 0, queryW, 1}
	if cursor >= 0 && queryW > 0 {
		hits.Cursor = queryX + cursor
	}
	line := Line{Space(1)}.Append(left...)
	if len(left) > 0 {
		line = append(line, Space(2))
	}
	line = append(line, field...)
	if len(right) > 0 {
		line = append(line, Space(2))
		shiftRects(hits.Chips, line.Width())
	}
	line = append(line, right...)
	if banded {
		for i, c := range h.Chips {
			if r := hits.Chips[i]; !r.Empty() && c.Filled() {
				hits.Shapes = append(hits.Shapes, ControlShape(r.X, r.W, t.chipFill(c)))
			}
		}
		if !hits.Scope.Empty() {
			hits.Shapes = append(hits.Shapes, ControlShape(hits.Scope.X, hits.Scope.W, t.C.Field))
		}
		slices.SortFunc(hits.Shapes, func(a, b Shape) int { return a.X - b.X })
	}
	return line.Fit(w).Render(), hits
}

// chipFill is the fill of a filled chip's shape.
func (t Theme) chipFill(c Chip) color.Color {
	if c.Kind == ChipScope {
		return t.C.Field
	}
	return t.C.SelBg
}

// headerFixed is the width of everything except the query field.
func headerFixed(left, right Line) int {
	n := 2 + left.Width() + right.Width() // 1-cell margins
	if len(left) > 0 {
		n += 2
	}
	if len(right) > 0 {
		n += 2
	}
	return n
}

// headerLeft builds title, breadcrumb, info and scope; with hits it records
// their rects (x from the header's left edge, after the 1-cell margin).
func (t Theme) headerLeft(h Header, p headerPlan, banded bool, hits *HeaderHits) Line {
	var out Line
	gap := func() {
		if len(out) > 0 {
			out = append(out, Space(2))
		}
	}
	if p.title {
		out = append(out, Span{h.Title, t.Text.Bold(true)})
	}
	if len(h.Crumbs) > 0 {
		gap()
		x := 1 + out.Width()
		crumbs := t.crumbs(h.Crumbs, p.crumbsFrom, hits, x)
		if p.crumbWidth > 0 && crumbs.Width() > p.crumbWidth {
			crumbs = crumbs.TruncateStart(p.crumbWidth)
			if hits != nil {
				clear(hits.Crumbs) // positions no longer line up; the last crumb is still visible
			}
		}
		out = append(out, crumbs...)
	}
	if p.info {
		gap()
		out = append(out, h.Info...)
	}
	if h.Scope != "" {
		gap()
		label := h.Scope
		if p.scopeWidth > 0 {
			label = truncateKeepEnd(label, " "+GlyphCrumb+" ", p.scopeWidth)
		}
		chip := t.chip(Chip{Kind: ChipScope, Label: label}, true, banded)
		if hits != nil {
			hits.Scope = Rect{1 + out.Width(), 0, chip.Width(), 1}
		}
		out = append(out, chip...)
	}
	return out
}

// crumbs renders "~ › Coding › rexkit": earlier crumbs secondary, the
// current one bold primary, the separators tertiary; collapsed leading
// crumbs become "…".
func (t Theme) crumbs(crumbs []string, from int, hits *HeaderHits, x int) Line {
	var out Line
	sep := Span{" " + GlyphCrumb + " ", t.Subtle}
	if from > 0 {
		out = append(out, Span{Ellipsis, t.Subtle}, sep)
	}
	for i := from; i < len(crumbs); i++ {
		if i > from {
			out = append(out, sep)
		}
		style := t.Muted
		if i == len(crumbs)-1 {
			style = t.Text.Bold(true)
		}
		span := Span{crumbs[i], style}
		if hits != nil {
			hits.Crumbs[i] = Rect{x + out.Width(), 0, Line{span}.Width(), 1}
		}
		out = append(out, span)
	}
	return out
}

// Air between chips: plain words stand three cells apart (two in the
// compact header); next to a shape, whose ends and air already set it
// apart, one. A shape's end and inner air (controlPad) take the other two,
// so a chip's label stays where it is as the chip fills.
const (
	chipGap        = 3
	compactChipGap = 2
	controlPad     = 2
)

// headerRight builds the chips (and the deprecated count); chip rects are
// relative to the start of the right group until header shifts them. In the
// band, the first and the last chip keep room for a shape's ends while
// they are plain words, so the group keeps its width (and the field its
// size) whichever chip is active. Only two filled chips side by side take
// more: the air between their shapes.
func (t Theme) headerRight(h Header, p headerPlan, banded bool, hits *HeaderHits) Line {
	var out Line
	placed, prevFilled, lastPad := 0, false, false
	for i, c := range h.Chips {
		if p.dropped[i] {
			continue
		}
		filled := banded && c.Filled()
		pad := banded && !filled && (c.Kind == ChipToggle || c.Kind == ChipTab)
		switch {
		case placed == 0 && pad:
			out = append(out, Space(controlPad))
		case placed == 0:
		case filled || prevFilled:
			out = append(out, Space(1))
		case banded:
			out = append(out, Space(chipGap))
		default:
			out = append(out, Space(compactChipGap))
		}
		chip := t.chip(c, p.chipKeys, banded)
		if hits != nil {
			hits.Chips[i] = Rect{out.Width(), 0, chip.Width(), 1}
		}
		out = append(out, chip...)
		placed, prevFilled, lastPad = placed+1, filled, pad
	}
	if lastPad {
		out = append(out, Space(controlPad))
	}
	return slices.Clip(out)
}

func shiftRects(rects []Rect, dx int) {
	for i, r := range rects {
		if !r.Empty() {
			rects[i] = r.Offset(dx, 0)
		}
	}
}

// chipDropOrder lists the chips in the order they are given up for space,
// each from the right: the quiet ones (off toggles, inactive tabs without a
// badge) early, the kept ones (active chips, badges, scopes, information)
// last of all.
func chipDropOrder(chips []Chip) (quiet, kept []int) {
	for i := len(chips) - 1; i >= 0; i-- {
		c := chips[i]
		if (c.Kind == ChipToggle || c.Kind == ChipTab) && !c.On && c.Badge == "" {
			quiet = append(quiet, i)
		} else {
			kept = append(kept, i)
		}
	}
	return quiet, kept
}
