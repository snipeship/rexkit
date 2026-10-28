package switcher

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// lineWith returns the plain text of the first screen line showing text.
func lineWith(h *harness, text string) string {
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}

// sgrBg is the SGR parameter that sets c as the background.
func sgrBg(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestTrayCountsPlaces(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	if l := lineWith(h, "keys"); !strings.Contains(l, "26 places") {
		t.Errorf("tray %q, want the count on its left", l)
	}
	if l := lineWith(h, "Agents"); strings.Contains(l, "26") {
		t.Errorf("the header still counts: %q", l)
	}
	h.typeText("nav")
	if l := lineWith(h, "keys"); !strings.Contains(l, "2 of 26") {
		t.Errorf("tray %q, want 2 of 26 while searching", l)
	}
}

func TestPrimaryHintSaysWhereEnterGoes(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.selectEntry("migrations")
	if l := lineWith(h, "keys"); !strings.Contains(l, "enter▌ go to studio") {
		t.Errorf("tray %q, want ⏎'s outcome for another session", l)
	}
	h.selectEntry("nvim")
	if l := lineWith(h, "keys"); !strings.Contains(l, "enter▌ go ") || strings.Contains(l, "go to") {
		t.Errorf("tray %q, want a plain go within this session", l)
	}
	h.typeText("zqxj")
	if l := lineWith(h, "keys"); !strings.Contains(l, "bksp▌ edit") {
		t.Errorf("tray %q, want ⌫ edit when nothing matches", l)
	}
}

// Statuses in the list are coloured words, never fills: consecutive "needs
// you" rows must not merge into a block, nor punch holes in the selection.
func TestListStatusIsText(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	view := h.m.View().Content
	c := h.m.theme.C
	for _, bg := range []color.Color{c.WarnBg, c.ErrBg, c.OKBg} {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, sgrBg(bg)) && !strings.Contains(ansi.Strip(l), "▐") {
				t.Errorf("a list row is filled with a status colour: %s", ansi.Strip(l))
			}
		}
	}
	if l := lineWith(h, "Migrate terraform state"); !strings.Contains(l, "✗ failed") {
		t.Errorf("row %q, want the status as glyph and word", l)
	}
}

// ▍ marks the selection; ▎ marks only the pane the switcher was opened from,
// never the session or tab rows that would also land there.
func TestSelectionAndOriginMarks(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	var sel []string
	for _, l := range washed(h.m.View().Content, h.m.theme.C.SelBg) {
		if strings.Contains(l, ui.GlyphSelection) {
			sel = append(sel, l)
		}
	}
	if len(sel) != 1 || !strings.Contains(sel[0], ui.GlyphSelection+"› zsh") {
		t.Errorf("selected rows %q, want one washed row with %s", sel, ui.GlyphSelection)
	}
	var current []string
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, ui.GlyphCurrent) {
			current = append(current, strings.TrimSpace(l))
		}
	}
	if len(current) != 1 || !strings.Contains(current[0], "zsh") || !strings.Contains(current[0], "~/Coding/rexkit") {
		t.Errorf("rows marked current: %q, want only the origin pane", current)
	}
}

func TestEmptyStateIsCentredWithoutPreview(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {100, 30}, {70, 20}} {
		h := newHarness(t, size[0], size[1], true)
		h.typeText("zqxj")
		l := lineWith(h, "No place matches")
		if l == "" {
			t.Fatalf("%v: no empty state:\n%s", size, h.screen())
		}
		left := len(l) - len(strings.TrimLeft(l, " "))
		right := ansi.StringWidth(l) - ansi.StringWidth(strings.TrimRight(l, " "))
		if d := left - right; d < -2 || d > 2 {
			t.Errorf("%v: %q is not centred (%d left, %d right)", size, l, left, right)
		}
		// (the tray is on the well's colour in dark themes)
		rows := strings.Split(h.m.View().Content, "\n")
		if strings.Contains(strings.Join(rows[:len(rows)-ui.TrayRows], "\n"), sgrBg(h.m.theme.C.Well)) {
			t.Errorf("%v: an empty preview well is still drawn:\n%s", size, h.screen())
		}
		if !strings.Contains(h.screen(), "edit the search") {
			t.Errorf("%v: no way out:\n%s", size, h.screen())
		}
	}
}

func TestErrorPageWhenRexCannotBeRead(t *testing.T) {
	h := newHarness(t, 160, 40, false, withSnapshotError())
	s := h.screen()
	if !strings.Contains(s, "✗ Can't reach Rex") || !strings.Contains(s, "the Rex server is not running") || !strings.Contains(s, "retry") {
		t.Errorf("no error page:\n%s", s)
	}
	if strings.Contains(s, "snapshot:") {
		t.Errorf("the error chain reached the screen:\n%s", s)
	}
}

func TestFailedJumpToastsWithAWayOut(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.selectEntry("nvim")
	h.fake.Fail("session.focus_block", fmt.Errorf("session.focus_block: %w", rex.ErrNotFound))
	h.key("enter")
	if l := lineWith(h, "keys"); !strings.Contains(l, "✗ That pane closed") || !strings.Contains(l, "ctrl r▌ refresh") {
		t.Errorf("tray %q, want the toast with its way out", l)
	}
}

// Clicks land on what is drawn there, in the band's geometry: a chip on its
// text row or its rounded edges, the scope's ×, a row of the list.
func TestClicksInTheCard(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {100, 30}, {70, 20}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			h := newHarness(t, size[0], size[1], true)
			s := h.m.layout()
			chip := -1
			for i, c := range s.chips {
				if c == catAgents {
					chip = i
				}
			}
			r := s.hits.Chips[chip]
			x := s.inner.X + s.areas.Header.X + r.X + 1
			y := s.inner.Y + s.areas.HeaderLine()
			h.send(tea.MouseClickMsg{X: x, Y: y - 1, Button: tea.MouseLeft}) // the chip's top edge
			if h.m.filter.cat != catAgents {
				t.Fatalf("a click on the Agents chip left category %v", h.m.filter.cat)
			}
			h.send(tea.MouseClickMsg{X: x, Y: y + 2, Button: tea.MouseLeft}) // the band's air row
			if h.m.filter.cat != catAgents {
				t.Errorf("a click under the band changed the category to %v", h.m.filter.cat)
			}
			h.click("Navigation polish")
			if h.selected() != "Navigation polish" {
				t.Errorf("a click on a row selected %q", h.selected())
			}
		})
	}
}

func TestClickOnScopeLeavesTheDrill(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.selectEntry("studio")
	h.key("right")
	h.click(ui.GlyphRemove)
	if h.m.filter.scope != "" {
		t.Errorf("still scoped to %q after clicking ×", h.m.filter.scope)
	}
}

func TestClickOutsideTheKeyPanelClosesIt(t *testing.T) {
	h := newHarness(t, 160, 40, true)
	h.key("?")
	s := h.m.layout()
	at := h.m.panelRect(s)
	h.send(tea.MouseClickMsg{X: s.inner.X + at.X + 2, Y: s.inner.Y + at.Y + 2, Button: tea.MouseLeft})
	if !h.m.help {
		t.Fatal("a click inside the panel closed it")
	}
	h.send(tea.MouseClickMsg{X: s.inner.X + 2, Y: s.inner.Y + at.Y + 2, Button: tea.MouseLeft})
	if h.m.help {
		t.Error("a click outside the panel left it open")
	}
}

// Stand-ins for control characters are drawn apart from the text, also in
// the ~-abbreviated folder and a tab named after it.
func TestEscapesKeepTheirPlace(t *testing.T) {
	cwd, esc := abbreviate("/home/u/a␛b", escapes{{9, 12}}, "/home/u")
	if cwd != "~/a␛b" || len(esc) != 1 || esc[0] != [2]int{3, 6} {
		t.Errorf("abbreviate = %q %v, want ~/a␛b with the escape at 3–6", cwd, esc)
	}
	panes := []entry{{blockID: "b", cwd: "~/x/a␛b", cwdEsc: escapes{{5, 8}}}}
	name, nameEsc := tabName("", panes, "b", 1)
	if name != "a␛b" || len(nameEsc) != 1 || nameEsc[0] != [2]int{1, 4} {
		t.Errorf("tabName = %q %v, want a␛b with the escape at 1–4", name, nameEsc)
	}
	th := ui.NewTheme(true)
	line := th.Escaped(name, nameEsc, th.Text)
	if len(line) != 3 || line[1].Text != "␛" || line[1].Style.GetForeground() != th.EscapeStyle().GetForeground() {
		t.Errorf("escaped line %+v", line)
	}
}
