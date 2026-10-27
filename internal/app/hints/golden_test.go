package hints

import (
	"fmt"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

func TestGolden(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		w, h int
		dark bool
		keys []string
	}{
		{"default-100x30", demoSource(demoShell), 100, 30, true, nil},
		{"light-100x30", demoSource(demoShell), 100, 30, false, nil},
		// 30 things: y starts five labels (ya … yj), and yy none.
		{"typed-100x46", demoSource(demoShell), 100, 46, true, []string{"y"}},
		{"open-100x46", demoSource(demoShell), 100, 46, true, []string{"shift+y"}},
		// ⇥ switched to paste, before and after a first letter.
		{"paste-100x30", demoSource(demoShell), 100, 30, true, []string{"tab"}},
		{"paste-light-100x30", demoSource(demoShell), 100, 30, false, []string{"tab"}},
		{"paste-typed-100x46", demoSource(demoShell), 100, 46, true, []string{"tab", "y"}},
		{"miss-100x46", demoSource(demoShell), 100, 46, true, []string{"y", "y"}},
		// After a miss the bar still says a label pastes, also where it says
		// the least (labels y… in a narrow pane of SHAs).
		{"paste-miss-100x46", demoSource(demoShell), 100, 46, true, []string{"tab", "y", "y"}},
		{"narrow-paste-miss-34x44", Source{Screen: shas(40)}, 34, 44, true, []string{"tab", "y", "y"}},
		{"copied-100x30", demoSource(demoShell), 100, 30, true, []string{"u"}},
		{"agent-100x36", demoSource(demoAgent), 100, 36, true, nil},
		{"narrow-44x12", demoSource(demoShell), 44, 12, true, nil},
		// A third of a laptop's window: the bar says the least, and still
		// says what a miss was.
		{"narrow-36x16", demoSource(demoShell), 36, 16, true, nil},
		{"narrow-miss-36x16", demoSource(demoShell), 36, 16, true, []string{"y"}},
		{"narrow-paste-36x16", demoSource(demoShell), 36, 16, true, []string{"tab"}},
		{"tiny-20x4", demoSource(demoShell), 20, 4, true, nil},
		// In a whole-window float, at the pane's place: the right half.
		{"placed-90x16", Source{Screen: demoShell, Size: rex.Size{Columns: 44, Rows: 14}, Rect: rex.Rect{0.5, 0, 0.5, 1}, Dir: demoDir, Home: demoHome}, 90, 16, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tool(t, tt.src, tt.w, tt.h, tt.dark)
			press(t, m, tt.keys...)
			uitest.Golden(t, tt.name, m.View().Content)
		})
	}
}

// shas is a pane of n SHAs, one a line: with forty, labels go two keys
// deep.
func shas(n int) func(cols, rows int) string {
	var ls []string
	for i := range n {
		ls = append(ls, fmt.Sprintf("  %07x", 0xa1b2c3d+i*0x10101f))
	}
	return plainScreen(ls...)
}

// TestBarStaysPutUnderShift: ⇧ on a letter makes the label open and hides
// ⇥, but the bar keeps its width, so it stays where it was.
func TestBarStaysPutUnderShift(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  Source
		w, h int
	}{
		{"demo-100x46", demoSource(demoShell), 100, 46},
		{"demo-119x66", demoSource(demoShell), 119, 66},
		{"shas-36x44", Source{Screen: shas(40)}, 36, 44},
		{"shas-34x44", Source{Screen: shas(40)}, 34, 44},
	} {
		for _, keys := range [][2][]string{
			{{"y"}, {"shift+y"}},
			{{"tab", "y"}, {"tab", "shift+y"}},
			{{"y", "y"}, {"shift+y", "y"}},
			{{"tab", "y", "y"}, {"tab", "shift+y", "y"}},
		} {
			at := func(keys []string) ui.Rect {
				m := tool(t, tt.src, tt.w, tt.h, true)
				press(t, m, keys...)
				_, r, ok := m.bar()
				if !ok {
					t.Fatalf("%s %v: no bar", tt.name, keys)
				}
				return r
			}
			if plain, shift := at(keys[0]), at(keys[1]); plain != shift {
				t.Errorf("%s: the bar is at %+v after %v, at %+v after %v", tt.name, plain, keys[0], shift, keys[1])
			}
		}
	}
}
