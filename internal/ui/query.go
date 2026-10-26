package ui

import "charm.land/bubbles/v2/textinput"

// queryPromptWidth is the width of the "❯ " prompt.
const queryPromptWidth = 2

// Query is the state of a search field. Editing is done by a bubbles
// textinput (see NewInput); the design system draws it, so the placeholder
// never sits under the cursor and the field fits the header's rhythm.
type Query struct {
	Value       string
	Cursor      int // rune offset of the cursor in Value
	Focused     bool
	Placeholder string
	// RealCursor draws no caret glyph: the caller places the terminal's own
	// (bar) cursor at the column the header reports, via tea.View.Cursor.
	RealCursor bool
	// Selected draws the value on the selection fill: a preselected value
	// that typing replaces (Rename).
	Selected bool
}

// NewInput returns a textinput configured as an editing engine for Query: no
// prompt, no placeholder and no virtual cursor of its own, focused.
func NewInput() textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""
	in.CharLimit = 256
	in.SetVirtualCursor(false)
	in.Focus()
	return in
}

// QueryOf reads a textinput's state.
func QueryOf(in textinput.Model, placeholder string) Query {
	return Query{Value: in.Value(), Cursor: in.Position(), Focused: in.Focused(), Placeholder: placeholder}
}

// Query renders a search field's text into exactly w cells. framed is a
// field inside a Control shape (the header band, a dialog), which says it is
// a field by itself: no prompt glyph. Unframed (the compact header) it
// starts with "❯ ". It also returns the cursor column within the field (-1
// when not focused). Typed text is primary, the placeholder tertiary.
func (t Theme) Query(q Query, w int, framed bool) (Line, int) {
	if w <= 0 {
		return nil, -1
	}
	prompt := Span{GlyphPrompt + " ", t.Subtle}
	pw := queryPromptWidth
	if framed {
		prompt, pw = Span{}, 0
	}
	caret := Line{{GlyphCaret, t.Accent}}
	if !q.Focused || q.RealCursor {
		caret = nil
	}
	budget := max(0, w-pw-caret.Width())
	line := Line{prompt}
	cursorX := -1

	if q.Value == "" {
		if q.Focused {
			cursorX = pw
			if q.RealCursor {
				caret = Line{Space(1)} // the terminal cursor gets its own cell
			}
		}
		line = append(line, caret...)
		line = append(line, Styled(q.Placeholder, t.Subtle).Truncate(max(0, w-pw-caret.Width()))...)
		return line.Fit(w), clampCursor(cursorX, w)
	}

	runes := []rune(q.Value)
	at := max(0, min(q.Cursor, len(runes)))
	text := t.Text
	if q.Selected {
		text = text.Background(t.C.SelField)
	}
	left, right := Styled(string(runes[:at]), text), Styled(string(runes[at:]), text)
	lw, rw := left.Width(), right.Width()
	if lw+rw > budget {
		// Keep the cursor in view: the text after it gets at most a third of
		// the field, the text before it the rest; clipped ends show "…".
		rb := min(rw, budget/3)
		lb := budget - rb
		if lw < lb {
			lb, rb = lw, budget-lw
		}
		left, right = left.TruncateStart(lb), right.Truncate(rb)
	}
	if q.Focused {
		cursorX = pw + left.Width()
	}
	line = append(line, left...)
	line = append(line, caret...)
	line = append(line, right...)
	return line.Fit(w), clampCursor(cursorX, w)
}

func clampCursor(x, w int) int {
	if x < 0 {
		return -1
	}
	return min(x, w-1)
}
