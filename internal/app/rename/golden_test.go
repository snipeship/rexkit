package rename

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/uitest"
)

func TestGolden(t *testing.T) {
	tests := []struct {
		name    string
		current string
		keys    []string
		problem string // the save fails with this
		w, h    int
	}{
		{"selected-56x9", "api-server", nil, "", 56, 9},
		{"edited-56x9", "api-server", []string{"right", "-", "2"}, "", 56, 9},
		{"cleared-56x9", "api-server", []string{"ctrl+u"}, "", 56, 9},
		{"automatic-56x9", "Block 3bccb71a", nil, "", 56, 9},
		{"demo-save-56x9", "api-server", []string{"w", "e", "b", "enter"}, "", 56, 9},
		{"error-56x9", "api-server", []string{"right", "-", "2", "enter"}, "That pane is gone", 56, 9},
		{"compact-56x7", "api-server", []string{"right", "-", "2"}, "", 56, 7},
		{"compact-error-56x7", "api-server", []string{"right", "-", "2", "enter"}, "That pane is gone", 56, 7},
		{"short-56x4", "api-server", nil, "", 56, 4},
		{"tiny-24x3", "a-very-long-pane-label-that-does-not-fit", nil, "", 24, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var apply func(string) tea.Cmd
			if tt.problem != "" {
				apply = func(string) tea.Cmd { return nil }
			}
			m := dialog(t, tt.current, apply, tt.w, tt.h)
			typeKeys(t, m, tt.keys...)
			if tt.problem != "" {
				m.Update(savedMsg{problem: tt.problem})
			}
			uitest.Golden(t, tt.name, screen(m))
		})
	}
}

func TestEveryLineFits(t *testing.T) {
	for w := 1; w <= 70; w += 3 {
		for h := 1; h <= 10; h++ {
			m := dialog(t, "api-server", nil, w, h)
			lines := strings.Split(screen(m), "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("%dx%d line %d is %d wide: %q", w, h, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

// TestCursorSitsInTheField: the bar caret follows the label on the field's
// text row, in the full dialog and the compact one.
func TestCursorSitsInTheField(t *testing.T) {
	for _, tt := range []struct{ h, row int }{{9, 3}, {7, 3}, {4, 1}, {3, 1}} {
		m := dialog(t, "api", nil, 56, tt.h)
		_, c := m.render(time.Now())
		lines := strings.Split(plain(screen(m)), "\n")
		if c == nil || c.Y != tt.row || !strings.HasSuffix(string([]rune(lines[c.Y])[:c.X]), "api") {
			t.Errorf("56x%d: cursor %+v, want after the label on row %d:\n%s", tt.h, c, tt.row, strings.Join(lines, "\n"))
		}
	}
}

// TestClearingSaysWhatComesBack: an empty field names the label Rex will
// give the pane or tab; for a session, Rex picks a new one.
func TestClearingSaysWhatComesBack(t *testing.T) {
	m := dialog(t, "shell", nil, 56, 9)
	typeKeys(t, m, "ctrl+u")
	if out := plain(screen(m)); !strings.Contains(out, "Automatic name") || !strings.Contains(out, "enter resets it to Block 3bccb71a") {
		t.Errorf("pane helper:\n%s", out)
	}
	if got := m.outcome(""); got != "Would reset the pane to “Block 3bccb71a”" {
		t.Errorf("outcome %q", got)
	}
	m = dialog(t, "", nil, 56, 9)
	if out := plain(screen(m)); !strings.Contains(out, "Rex calls it Block 3bccb71a") {
		t.Errorf("an automatic label should say what it is:\n%s", out)
	}

	s := newModel(options{target: Target{Kind: "session", ID: "session:01a1763b-f56c-72ce-b5e2-fb233bccb71a"}}, "work", nil, nil, nil)
	s.Update(tea.WindowSizeMsg{Width: 56, Height: 9})
	typeKeys(t, s, "ctrl+u")
	if out := plain(screen(s)); !strings.Contains(out, "enter lets Rex pick a new name") {
		t.Errorf("session helper:\n%s", out)
	}
}

// TestRefusedValue: a failed save turns the field ErrField and says why
// under it, not in the tray; the next key clears both.
func TestRefusedValue(t *testing.T) {
	m := dialog(t, "api", func(string) tea.Cmd { return nil }, 56, 9)
	typeKeys(t, m, "x", "enter")
	m.Update(savedMsg{problem: "That pane is gone"})
	lines := strings.Split(screen(m), "\n")
	if got := plain(lines[5]); strings.TrimSpace(got) != "✗ That pane is gone" {
		t.Errorf("helper row %q", got)
	}
	if !hasBg(lines[3], m.theme.C.ErrField) || strings.Contains(plain(lines[7]), "gone") {
		t.Error("the field should turn ErrField and the tray keep its keys")
	}
	typeKeys(t, m, "y")
	if lines := strings.Split(screen(m), "\n"); hasBg(lines[3], m.theme.C.ErrField) {
		t.Error("typing should clear the error")
	}
}

// TestMacOrder: the tray keeps clear on the left and puts save, the
// primary, rightmost.
func TestMacOrder(t *testing.T) {
	m := dialog(t, "api", nil, 56, 9)
	row := plain(strings.Split(screen(m), "\n")[7])
	clear, cancel, save := strings.Index(row, "clear"), strings.Index(row, "cancel"), strings.Index(row, "save")
	if clear < 0 || !(clear < cancel && cancel < save) || !strings.HasSuffix(row, "save ") && !strings.HasSuffix(row, "save") {
		t.Errorf("tray %q", row)
	}
}

// TestSaving: a slow save shows "Saving…" in the tray's left and ignores
// keys other than ⎋.
func TestSaving(t *testing.T) {
	m := dialog(t, "api", func(string) tea.Cmd { return nil }, 56, 9)
	typeKeys(t, m, "x", "enter", "y")
	if m.input.Value() != "x" {
		t.Errorf("typing while saving changed the field: %q", m.input.Value())
	}
	out, _ := m.render(time.Now().Add(time.Second))
	if row := plain(strings.Split(out, "\n")[7]); !strings.Contains(row, "Saving…") || strings.Contains(row, "clear") {
		t.Errorf("tray while saving %q", row)
	}
}

// TestSelectionFillEndsAtTheLabel: a selected label is on the selection
// fill, and the field around it is not.
func TestSelectionFillEndsAtTheLabel(t *testing.T) {
	m := dialog(t, "api-server", nil, 56, 9)
	row := strings.Split(screen(m), "\n")[3]
	sel := sgrBg(m.theme.C.SelField)
	var on []string
	for _, r := range runs(row) {
		if r.bg == sel {
			on = append(on, r.text)
		}
	}
	if got := strings.Join(on, ""); got != "api-server" {
		t.Errorf("on the selection fill: %q, want just the label", got)
	}
}

type textRun struct{ text, bg string }

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// runs splits a rendered line into its text runs and their background
// (the SGR parameters after 48;, "" for the default).
func runs(line string) []textRun {
	var out []textRun
	bg := ""
	last := 0
	for _, loc := range sgr.FindAllStringSubmatchIndex(line, -1) {
		if loc[0] > last {
			out = append(out, textRun{line[last:loc[0]], bg})
		}
		params := line[loc[2]:loc[3]]
		if params == "" || params == "0" || strings.HasPrefix(params, "0;") {
			bg = ""
		}
		if i := strings.Index(params, "48;"); i >= 0 {
			if f := strings.SplitN(params[i+3:], ";", 5); len(f) >= 4 {
				bg = strings.Join(f[:4], ";")
			}
		}
		last = loc[1]
	}
	if last < len(line) {
		out = append(out, textRun{line[last:], bg})
	}
	return out
}

func sgrBg(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func hasBg(line string, c color.Color) bool {
	return strings.Contains(line, "48;"+sgrBg(c))
}
