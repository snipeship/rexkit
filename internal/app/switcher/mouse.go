package switcher

import tea "charm.land/bubbletea/v2"

// wheelStep is how many rows a wheel notch moves.
const wheelStep = 3

// click handles a left click: a chip switches category, the scope chip
// leaves the drill-down, a row is selected and a double-click goes there.
func (m *model) click(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft || m.busy {
		return nil
	}
	s := m.layout()
	x, y := mouse.X-s.inner.X, mouse.Y-s.inner.Y
	if m.help {
		if !m.panelRect(s).Contains(x, y) {
			m.help = false // a click outside the key panel closes it
		}
		return nil
	}
	if s.hitsHeader(x, y) {
		hx := x - s.areas.Header.X
		if s.hits.Scope.Contains(hx, 0) {
			m.leaveScope()
			return nil
		}
		for i, r := range s.hits.Chips {
			if r.Contains(hx, 0) {
				m.setCategory(s.chips[i])
				return nil
			}
		}
		return nil
	}
	if !s.areas.List.Contains(x, y) {
		return nil
	}
	_, double, ok := m.list.Click(y-s.areas.List.Y, m.now())
	if !ok {
		return nil
	}
	m.moved = true
	if !double {
		return nil
	}
	if _, e, ok := m.selected(); ok {
		return m.goTo(e, byClick)
	}
	return nil
}

// wheel scrolls whatever is under the pointer.
func (m *model) wheel(mouse tea.Mouse) {
	delta := wheelStep
	if mouse.Button == tea.MouseWheelUp {
		delta = -wheelStep
	} else if mouse.Button != tea.MouseWheelDown {
		return
	}
	s := m.layout()
	x, y := mouse.X-s.inner.X, mouse.Y-s.inner.Y
	switch {
	case s.areas.PreviewVisible() && s.areas.Preview.Contains(x, y):
		m.pv.view.Scroll(delta, s.areas.Preview.H)
	case s.areas.List.Contains(x, y):
		m.list.Wheel(delta)
		m.moved = true
	}
}

// hitsHeader reports whether (x, y), in content coordinates, is on the
// header's text row or on the edge rows of its shapes: a click on a chip's
// rounded top or bottom is a click on the chip.
func (s screen) hitsHeader(x, y int) bool {
	line := s.areas.HeaderLine()
	if !s.areas.Banded() {
		return s.areas.Header.Contains(x, y) && y == line
	}
	return s.areas.Header.Contains(x, y) && y >= line-1 && y <= line+1
}
