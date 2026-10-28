package ui

import "image/color"

// Sheet is a small task raised over a tool's body, such as Go to's rename,
// close and new-session asks. It is drawn like the key panel, an Outline
// that Raise puts over the body: a title row with an esc keycap,
// then a text field with its helper, or a message. The task's keys go in
// the tray.
type Sheet struct {
	Title Line
	Field *Field // a text field, or nil
	Lines []Line // a message, under the title (or under the field)
}

// SheetWidth is how wide a sheet in the body's corner is, at most.
const SheetWidth = 60

// sheetMinInner is the narrowest a sheet's content gets.
const sheetMinInner = 20

// Sheet renders s for a tool's body of w×h cells, as KeyPanel places its
// panel: PanelCorner puts it in the body's bottom-right corner, at most
// SheetWidth wide; PanelFill and PanelSpan run it across the body, and
// PanelSpan leaves the list's selection above it. It returns the lines,
// where they go in the body, and the field's cursor within them (-1, -1
// when there is none). A body too small for it gets nil. Draw it with
// Raise. A sheet short of rows drops its air, then the field's edges and
// helper (not a helper that says what went wrong), then message lines.
func (t Theme) Sheet(s Sheet, w, h int, place PanelPlace) ([]string, Rect, int, int) {
	maxW, maxH := w-3, h
	if place != PanelCorner {
		maxW = w - 2
	}
	if place == PanelSpan {
		maxH = h - keyPanelListRows
	}
	pw := maxW
	if place == PanelCorner {
		pw = min(pw, SheetWidth)
	}
	const chrome = 4 // the ends and a cell of air inside each
	innerW := pw - chrome
	if innerW < sheetMinInner || maxH < 3 {
		return nil, Rect{}, -1, -1
	}

	esc := t.Keycap(CapLabel("esc"))
	head := Line{Space(0)}.Append(s.Title...).Fit(innerW - esc.Width()).Append(esc...)
	fill := t.sheetField()
	var field []string
	cx, cy := -1, -1
	layout := func(gap, compact bool, lines int) ([]string, int) {
		rows := []string{t.sheetRow(head.RenderBg(Clear), innerW)}
		if gap {
			rows = append(rows, t.sheetRow(Line{Space(innerW)}.RenderBg(Clear), innerW))
		}
		at := -1
		if s.Field != nil {
			field, cx, cy = t.sheetFieldRows(*s.Field, innerW, fill, compact)
			at = len(rows)
			for _, f := range field {
				rows = append(rows, t.sheetRow(f, innerW))
			}
		}
		for _, l := range s.Lines[:lines] {
			rows = append(rows, t.sheetRow(l.Fit(innerW).RenderBg(Clear), innerW))
		}
		return rows, at
	}
	var rows []string
	fieldAt := -1
	fits := false
	for _, try := range []struct{ gap, compact bool }{{true, false}, {false, false}, {false, true}} {
		rows, fieldAt = layout(try.gap, try.compact, len(s.Lines))
		if len(rows)+2 <= maxH {
			fits = true
			break
		}
	}
	for n := len(s.Lines) - 1; !fits && n >= 0; n-- {
		rows, fieldAt = layout(false, true, n)
		fits = len(rows)+2 <= maxH
	}
	if !fits {
		return nil, Rect{}, -1, -1
	}

	out := append([]string{t.OutlineEdge(pw, true)}, rows...)
	out = append(out, t.OutlineEdge(pw, false))
	ph := len(out)
	if cx >= 0 && fieldAt >= 0 {
		cx, cy = 2+cx, 1+fieldAt+cy // the panel's end and air, its top edge
	} else {
		cx, cy = -1, -1
	}
	return out, Rect{w - 1 - pw, h - ph, pw, ph}, cx, cy
}

// sheetRow puts content (innerW cells, already on Clear) between the
// sheet's sides.
func (t Theme) sheetRow(content string, _ int) string { return t.OutlineRow(content) }

// sheetFieldRows draws a field across innerW cells inside the sheet:
// its edges, its text and its helper, or (compact) its text alone as a
// Tag, with its helper only when that says what went wrong. The helper's
// row is kept when there is none, so the sheet does not change height as
// the helper comes and goes. It returns the rows and the cursor's column
// and row in them.
func (t Theme) sheetFieldRows(f Field, innerW int, fill color.Color, compact bool) ([]string, int, int) {
	if f.Err {
		fill = t.C.ErrField
	}
	pad := 2 // the end and a cell of air
	if compact {
		pad = 1
	}
	text, cursor := t.Query(f.Query, max(0, innerW-2*pad), true)
	row := Control(text, fill)
	if compact {
		row = Pill(text, fill)
	}
	textRow := row.Fit(innerW).RenderBg(Clear)
	cx, cy := -1, -1
	if cursor >= 0 {
		cx = pad + cursor
	}
	helper := Line{Space(pad), {f.Helper, t.Subtle}}
	if f.Err {
		helper = Line{Space(pad), {GlyphFailed + " " + f.Helper, t.Err}}
	}
	helperRow := helper.Fit(innerW).RenderBg(Clear)
	if compact {
		if cx >= 0 {
			cy = 0
		}
		if f.Err {
			return []string{textRow, helperRow}, cx, cy
		}
		return []string{textRow}, cx, cy
	}
	if cx >= 0 {
		cy = 1
	}
	shape := ControlShape(0, innerW, fill)
	return []string{EdgeRow(innerW, true, Clear, shape), textRow, EdgeRow(innerW, false, Clear, shape), helperRow}, cx, cy
}

// sheetField is the fill of a field in a sheet: Field, as on the float.
func (t Theme) sheetField() color.Color { return t.C.Field }
