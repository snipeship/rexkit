package rename

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/ui"
)

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	if !m.scheme.Settled() || m.w <= 0 || m.h <= 0 {
		return v // hold the first frame until the palette is known
	}
	v.Content, v.Cursor = m.render(time.Now())
	return v
}

// render draws the dialog and returns where the caret
// goes. At its full 9 rows the card holds the title, the field with its edge
// rows and the helper under it, and the tray. Shorter dialogs use the
// compact chrome (when ui.Banded is false): a one-row field and a one-row
// tray, with the title moving into the tray under 5 rows.
func (m *model) render(now time.Time) (string, *tea.Cursor) {
	t := m.theme
	f := ui.DialogFrame(m.w, m.h)
	in := f.Inner(m.w, m.h)
	if in.W <= 0 || in.H <= 0 {
		return f.Render(t, m.w, m.h, nil, nil, nil), nil
	}
	title := ui.Line{ui.Space(1), {Text: "Rename " + m.opts.target.Kind, Style: t.Text.Bold(true)}}
	field, cx, cy := t.Field(m.field(), in.W, !f.Banded)

	var header, body, tray []string
	fieldY := 0 // the field's first row, within the content area
	switch {
	case f.Banded:
		header, body = []string{title.Render()}, field
		tray = t.Tray(m.tray(now, true, nil), in.W, ui.TrayRows)
		fieldY = 1
	case in.H >= 3:
		fieldY = min(2, in.H-2)
		header = []string{title.Render()}
		body = append(make([]string, fieldY-1), field...)
		tray = t.Tray(m.tray(now, false, nil), in.W, 1)
	case in.H == 2:
		body = field
		tray = t.Tray(m.tray(now, false, title[1:]), in.W, 1)
	default:
		body = field
	}
	out := f.Render(t, m.w, m.h, header, body, tray)
	if cx < 0 || cy < 0 {
		return out, nil
	}
	c := tea.NewCursor(in.X+cx, in.Y+fieldY+cy)
	c.Shape, c.Color = tea.CursorBar, t.C.Accent
	return out, c
}

// field is the field's state; an error toast turns it red, with the
// reason as its helper.
func (m *model) field() ui.Field {
	if to := m.toasts.Current(); to.Kind == ui.ToastErr && to.Text != "" {
		return m.Field(to.Text)
	}
	return m.Field("")
}

// tray holds the keys in the Mac order, the primary rightmost: "ctrl u
// clear" on the left, where the meta goes, then cancel and save. Saving
// puts a spinner in the left; a toast (demo mode's outcome) takes it too,
// and so does an error when the field has no helper row to carry it (the
// compact chrome). lead, when given, replaces the clear key: the title, for
// a dialog too short to show it above the field.
func (m *model) tray(now time.Time, banded bool, lead ui.Line) ui.Tray {
	t := m.theme
	th := t
	if banded {
		th = t.InTray() // the clear key sits on the tray's colour
	}
	tr := ui.Tray{
		Meta:  th.Hints([]ui.Hint{{Key: "ctrl+u", Label: "clear"}}, 1<<30),
		Hints: []ui.Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "save", Primary: true}},
	}
	if lead != nil {
		tr.Meta = lead
	}
	if to := m.toasts.Current(); to.Kind != ui.ToastErr || !banded {
		tr.Toast = to
	}
	if m.spin.Visible(now) {
		tr.Busy = t.Spinning("Saving…", now)
	}
	return tr
}
