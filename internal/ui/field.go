package ui

// Field is a dialog's text field (Rename): a Control
// shape across the dialog on the Field fill, and a helper row under it.
type Field struct {
	Query Query
	// Err marks a refused value: the field turns ErrField and the helper
	// is the reason, after ✗, in the error colour.
	Err bool
	// Helper is the row under the field, in the tertiary colour: "Was api",
	// "enter resets it to Block 3bccb71a"; with Err, the reason.
	Helper string
}

// FieldRows is the height of a field with its edge rows and helper.
const FieldRows = 4

// Field renders f across w cells (with a cell of margin each side): its top
// edge, its text row, its bottom edge and the helper row, aligned with the
// field's text. compact (a dialog under DialogRows) drops the edge rows and
// the helper: one row with Tag ends. It returns the lines, and the column
// and row of the cursor (-1, -1 when the field is not focused).
func (t Theme) Field(f Field, w int, compact bool) (lines []string, cursorX, cursorY int) {
	fill := t.C.Field
	if f.Err {
		fill = t.C.ErrField
	}
	fw := max(0, w-2)
	pad := 2 // the end and a cell of air
	if compact {
		pad = 1
	}
	text, cursor := t.Query(f.Query, max(0, fw-2*pad), true)
	row := Control(text, fill)
	if compact {
		row = Pill(text, fill)
	}
	textRow := Line{Space(1)}.Append(row...).Fit(w).Render()
	cursorX, cursorY = -1, -1
	if cursor >= 0 && fw > 2*pad {
		cursorX = 1 + pad + cursor
	}
	if compact {
		if cursorX >= 0 {
			cursorY = 0
		}
		return []string{textRow}, cursorX, cursorY
	}
	if cursorX >= 0 {
		cursorY = 1
	}
	helper := Line{{f.Helper, t.Subtle}}
	if f.Err {
		helper = Line{{GlyphFailed + " " + f.Helper, t.Err}}
	}
	shape := ControlShape(1, fw, fill)
	return []string{
		EdgeRow(w, true, Clear, shape),
		textRow,
		EdgeRow(w, false, Clear, shape),
		Line{Space(1 + pad)}.Append(helper...).Fit(w).Render(),
	}, cursorX, cursorY
}
