package hints

import (
	"testing"

	"rexkit/internal/rex"
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
		{"paste-100x46", demoSource(demoShell), 100, 46, true, []string{"alt+y"}},
		{"miss-100x46", demoSource(demoShell), 100, 46, true, []string{"y", "y"}},
		{"copied-100x30", demoSource(demoShell), 100, 30, true, []string{"u"}},
		{"agent-100x36", demoSource(demoAgent), 100, 36, true, nil},
		{"narrow-44x12", demoSource(demoShell), 44, 12, true, nil},
		// A third of a laptop's window: the bar says the least, and still
		// says what a miss was.
		{"narrow-36x16", demoSource(demoShell), 36, 16, true, nil},
		{"narrow-miss-36x16", demoSource(demoShell), 36, 16, true, []string{"y"}},
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
