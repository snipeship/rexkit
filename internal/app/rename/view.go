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

// render draws the dialog and returns where the caret goes. Its title
// ("Rename pane") is the float's header. At its full ui.DialogRows the
// dialog holds the field with its edge rows and the helper under it, and
// the tray. Shorter dialogs use the compact chrome: a one-row field and a
// one-row tray (with a row of air under it from four rows), and under two
// rows only the field.
func (m *model) render(now time.Time) (string, *tea.Cursor) {
	t := m.theme
	f := ui.DialogFrame(m.w, m.h)
	in := f.Inner(m.w, m.h)
	if in.W <= 0 || in.H <= 0 {
		return f.Render(t, m.w, m.h, nil, nil, nil), nil
	}
	field, cx, cy := t.Field(m.field(), in.W, !f.Banded)
	var tray []string
	switch {
	case f.Banded:
		tray = t.Tray(m.tray(now, true), in.W, ui.TrayRows)
	case in.H >= 4:
		tray = t.Tray(m.tray(now, false), in.W, 2) // with a row of air under it
	case in.H >= 2:
		tray = t.Tray(m.tray(now, false), in.W, 1)
	}
	out := f.Render(t, m.w, m.h, nil, field, tray)
	if cx < 0 || cy < 0 {
		return out, nil
	}
	c := tea.NewCursor(in.X+cx, in.Y+cy)
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
// compact chrome).
func (m *model) tray(now time.Time, banded bool) ui.Tray {
	t := m.theme
	th := t
	if banded {
		th = t.InTray() // the clear key sits on the tray's colour
	}
	tr := ui.Tray{
		Meta:  th.Hints([]ui.Hint{{Key: "ctrl+u", Label: "clear"}}, 1<<30),
		Hints: []ui.Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "save", Primary: true}},
	}
	if to := m.toasts.Current(); to.Kind != ui.ToastErr || !banded {
		tr.Toast = to
	}
	if m.spin.Visible(now) {
		tr.Busy = t.Spinning("Saving…", now)
	}
	return tr
}
