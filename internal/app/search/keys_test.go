package search

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// click left-clicks the first place on screen showing text.
func (h *harness) click(text string) {
	h.t.Helper()
	for y, line := range strings.Split(h.screen(), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			h.send(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
			return
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", text, h.screen())
}

// clickRow left-clicks list row i, which must be in view.
func (h *harness) clickRow(i int) {
	h.t.Helper()
	s := h.m.layout()
	list := listRect(s.areas, h.m.keyPanelAt(s.areas))
	for y := range list.H {
		if item, ok := h.m.list.ItemAt(y); ok && item == i {
			h.send(tea.MouseClickMsg{X: s.inner.X + list.X + 4, Y: s.inner.Y + list.Y + y, Button: tea.MouseLeft})
			return
		}
	}
	h.t.Fatalf("row %d is not in view", i)
}

// rowOf is the index of the first row of a kind in the named pane's group.
func (h *harness) rowOf(name string, kind rowKind) int {
	h.t.Helper()
	for i, r := range h.m.rows {
		if r.kind == kind && h.m.panes[h.m.groups[r.group].pane].name == name {
			return i
		}
	}
	h.t.Fatalf("no such row in %s", name)
	return -1
}

// The chips are the way to switch when ⌥ keys do not arrive.
func TestClickingTheChips(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("panic")
	h.click("match case")
	h.click("regex")
	h.click("this session")
	if !h.m.matchCase || !h.m.regex || !h.m.sessionOnly {
		t.Errorf("match case %v, regex %v, this session %v", h.m.matchCase, h.m.regex, h.m.sessionOnly)
	}
	if h.m.shown != (h.m.currentQuery()) || !h.m.shown.Regex || !h.m.shown.MatchCase {
		t.Errorf("the search did not follow the chips: %+v", h.m.shown)
	}
	h.click("match case")
	if h.m.matchCase {
		t.Error("a second click did not turn match case off")
	}
}

func TestClickingRows(t *testing.T) {
	h := newHarness(t, 160, 40, true, withStateDir(t.TempDir()))
	h.typeText("panic")
	fold := h.rowOf("zsh", rowMore) // studio › api: 7 earlier
	h.clickRow(fold)
	if got := h.selectedRow(); got != "more 7" {
		t.Fatalf("a click selected %q", got)
	}
	shown := len(h.m.rows)
	h.clickRow(fold) // the second click of a double-click unfolds
	if len(h.m.rows) != shown+6 || h.quit {
		t.Errorf("rows %d → %d, quit %v", shown, len(h.m.rows), h.quit)
	}
	pane := h.rowOf("claude", rowPane)
	h.clickRow(pane)
	h.clickRow(pane) // a double-click goes to the pane
	if !h.quit {
		t.Fatal("a double-click did not go to the pane")
	}
	if r := h.result(); r.BlockID != h.blockOf("claude", "spark").block || r.Invoke != "" {
		t.Errorf("result %+v", r)
	}
}

func TestClickOutsideTheKeysClosesThem(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("f1")
	s := h.m.layout()
	h.send(tea.MouseClickMsg{X: s.inner.X + 1, Y: s.inner.Y + s.inner.H - 2, Button: tea.MouseLeft})
	if h.m.help {
		t.Error("the key panel stayed open")
	}
}

// ? opens the keys only on an empty query; after that it is typed, and the
// tray offers F1.
func TestQuestionMarkInAQuery(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.typeText("runs?limit")
	if h.m.help || h.m.input.Value() != "runs?limit" {
		t.Fatalf("help %v, query %q", h.m.help, h.m.input.Value())
	}
	if s := h.screen(); !strings.Contains(s, "F1▌ keys") {
		t.Errorf("no F1 in the tray:\n%s", s)
	}
}
