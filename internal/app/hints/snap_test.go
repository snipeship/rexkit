package hints

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/snap"
)

// TestSnapPicks: the states that pick something pick it at every window
// size, with or without the panes around it, and pasted shows the text at
// the prompt (where the pane is wide enough for it).
func TestSnapPicks(t *testing.T) {
	for _, tt := range []struct {
		state, text string
		action      Action
	}{
		{"copied", "a3f9c21", ActCopy},
		{"pasted", "1c2d3e4f5a6b", ActPaste},
	} {
		for _, size := range [][2]int{{241, 70}, {160, 48}, {120, 34}, {100, 30}, {80, 24}} {
			t.Run(fmt.Sprintf("%s-%dx%d", tt.state, size[0], size[1]), func(t *testing.T) {
				s, err := Snap(true, tt.state)
				if err != nil {
					t.Fatal(err)
				}
				_, at := s.Window(size[0], size[1])
				d := snap.NewDriver(s.Model)
				d.Send(tea.WindowSizeMsg{Width: at.Dx(), Height: at.Dy()})
				s.Script(d)
				m := s.Model.(*model)
				if p := m.Picked(); p == nil || p.Match.Text != tt.text || p.Action != tt.action {
					t.Fatalf("picked %+v, want %s to %s", p, tt.text, tt.action)
				}
				if tt.action != ActPaste {
					return
				}
				// (a pane narrower than the prompt cuts it, as it cuts the
				// demo's other long lines)
				rows := strings.Split(ansi.Strip(d.View()), "\n")
				want := "feat/run-queue $ docker start " + tt.text
				if last := rows[len(rows)-1]; at.Dx() > len("~/code/studio/api ")+len(want) && !strings.Contains(last, want) {
					t.Errorf("the prompt is %q, without the paste", last)
				}
			})
		}
	}
}
