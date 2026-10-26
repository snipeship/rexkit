package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui/uitest"
)

// golden compares got with testdata/name.golden; go test -update rewrites it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	uitest.Golden(t, name, got)
}

func TestGalleryGolden(t *testing.T) {
	tests := []struct {
		w, h int
		dark bool
	}{
		{140, 44, true},
		{140, 44, false},
		{100, 28, true},
		{70, 20, true},
		{44, 12, true},
	}
	for _, tt := range tests {
		scheme := "dark"
		if !tt.dark {
			scheme = "light"
		}
		name := "gallery-" + size(tt.w, tt.h) + "-" + scheme
		t.Run(name, func(t *testing.T) {
			got := Gallery(tt.w, tt.h, tt.dark)
			checkSize(t, name, got, tt.w, tt.h)
			golden(t, name, got)
		})
	}
}

func TestComponentGolden(t *testing.T) {
	th := NewTheme(true)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	goTo := Header{
		Scope: "studio › web",
		Query: Query{Value: "nav", Cursor: 3, Focused: true},
		Chips: []Chip{{Kind: ChipTab, Label: "All", On: true}, {Kind: ChipTab, Label: "Agents", Badge: "5"}},
	}
	files := filesHeader()
	files.Scope = ""
	files.Info = Line{{GlyphBranch + " main", th.Muted}, {" ●3", th.Warn}}

	var list List
	list.SetItems(12, []Section{{Title: "Needs you", Start: 0}, {Title: "Recent", Start: 2}})
	list.SetHeight(6)
	list.Select(3)
	rows := list.View(th, 48, func(i, w int, selected bool) Row {
		name := []string{"Navigation polish", "Fix navbar", "zsh", "漢字 notes", "api"}[i%5]
		cells := []Line{{th.Glyph(KindAgent)}, Highlight(name, []int{0, 1}, th.Text, th.Match), Styled("studio › web", th.Muted), {th.StatusGlyph(StatusBlocked, now)}, Styled(StatusBlocked.Word(), th.Warn.Bold(true)), Styled("12m", th.Subtle)}
		return Row{Line: rowColumns.Fit(w, []int{1, 17, 12, 1, 7, 3}).Row(cells...), Current: i == 0}
	}, Styled("No matches", th.Muted))

	groups := []KeyGroup{
		{"Navigate", []Binding{{[]string{"up", "down"}, "move"}, {[]string{"enter"}, "open"}}},
		{"View", []Binding{{[]string{"ctrl+/"}, "preview"}, {[]string{"?"}, "keys"}}},
	}
	band := func(h Header, w int) string {
		lines, _ := th.HeaderBand(h, w)
		return strings.Join(lines, "\n")
	}
	tests := map[string]string{
		"header-goto-100":      must(th.Header(goTo, 100)),
		"header-goto-44":       must(th.Header(goTo, 44)),
		"header-files-120":     must(th.Header(files, 120)),
		"header-files-50":      must(th.Header(files, 50)),
		"headerband-goto-100":  band(goTo, 100),
		"headerband-files-120": band(files, 120),
		"headerband-files-50":  band(files, 50),
		"list-48x6":            strings.Join(rows, "\n"),
		"tray-80x3": strings.Join(th.Tray(Tray{
			Meta:  th.MetaCount(3, 63, "place"),
			Hints: []Hint{{Key: "enter", Label: "go", Outcome: "to studio", Primary: true}, {Key: "tab", Label: "filter"}, {Key: "?", Label: "keys"}},
			Toast: Toast{Kind: ToastErr, Text: "That pane closed", Ways: []Hint{{Key: "ctrl+r", Label: "refresh"}}},
		}, 80, TrayAirRows), "\n"),
		"keypanel-60x10": strings.Join(func() []string {
			body := make([]string, 10)
			for i := range body {
				body[i] = Paint(strings.Repeat(".", 60), th.C.Surface)
			}
			panel, at, _ := th.KeyPanel(groups, 60, 10, PanelCorner, 0)
			return th.Raise(body, panel, at)
		}(), "\n"),
		// CJK rows under the panel's top-left corner and its shadow:
		// wide characters are shaded whole, or blanked where the panel's
		// edge cuts them, never split into loose halves.
		"keypanel-cjk-60x10": strings.Join(func() []string {
			body := make([]string, 10)
			for i := range body {
				row := Line{{strings.Repeat("要約を書く", 6), th.Text}}
				if i%2 == 1 {
					row = Line{{" " + strings.Repeat("研究ノート", 6), th.Text}}
				}
				body[i] = Paint(row.Fit(60).Render(), th.C.Surface)
			}
			panel, at, _ := th.KeyPanel(groups, 60, 10, PanelCorner, 0)
			return th.Raise(body, panel, at)
		}(), "\n"),
		"dialog-40x9": func() string {
			f := DialogFrame(40, 9)
			field, _, _ := th.Field(Field{Query: Query{Value: "api-server", Cursor: 10, Focused: true}, Helper: "Was api"}, 38, false)
			tray := th.Tray(Tray{Hints: []Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "save", Primary: true}}}, 38, TrayRows)
			return f.Render(th, 40, 9, []string{" Rename pane"}, field, tray)
		}(),
		"field-err-40": strings.Join(must3(th.Field(Field{Query: Query{Value: "a/b"}, Err: true, Helper: "Names cannot contain /"}, 40, false)), "\n"),
		"preview-40x5": strings.Join(func() []string {
			p := Preview{}
			p.Set(p.Begin(), th.Meta("go.mod", "1.1 KB", "3h"), lines(9))
			return p.View(th, 40, 5)
		}(), "\n"),
		"preview-60x20": strings.Join(func() []string {
			p := Preview{Grid: []MetaRow{{"Directory", Styled("~/code/studio/web", th.Text)}, {"Program", Styled("codex", th.Text)}}}
			p.Set(p.Begin(), th.Meta("Fix navbar", "studio › web"), lines(30))
			p.Right = th.Tag(StatusBlocked, "", now)
			return p.View(th, 60, 20)
		}(), "\n"),
		"empty-50x8": strings.Join(th.EmptyState(Line{{"No matches", th.Text.Bold(true)}}, Line{{"Try fewer letters.", th.Muted}},
			[]Hint{{Key: "backspace", Label: "edit"}, {Key: "esc", Label: "close"}}, 50, 8), "\n"),
		"toast-60x4": strings.Join(th.ToastStrip(Toast{Kind: ToastOK, Text: "Opened api"}, 60, 4, ToastRows), "\n"),
	}
	for name, got := range tests {
		t.Run(name, func(t *testing.T) { golden(t, name, got) })
	}
}

// TestTinySizes renders everything at every small size: nothing may panic
// and every output must be exactly the requested size.
func TestTinySizes(t *testing.T) {
	th := NewTheme(true)
	for w := 0; w <= 48; w++ {
		for h := 0; h <= 14; h++ {
			checkSize(t, "gallery "+size(w, h), Gallery(w, h, true), w, h)
			for _, carded := range []bool{false, true} {
				f := FloatFrame(w, h, carded)
				in := f.Inner(w, h)
				tray := th.Tray(Tray{Meta: Plain("1 of 2"), Hints: []Hint{{Key: "?", Label: "keys"}}}, in.W, min(in.H, TrayRows))
				checkSize(t, "frame "+size(w, h), f.Render(th, w, h, []string{"Go to"}, []string{"x"}, tray), w, h)
			}
			if w > 0 && h > 0 {
				checkSize(t, "toast "+size(w, h), strings.Join(th.ToastStrip(Toast{Kind: ToastErr, Text: "nope"}, w, h, ToastRows), "\n"), w, h)
				checkSize(t, "empty "+size(w, h), strings.Join(th.EmptyState(Plain("No matches"), Plain("detail"), []Hint{{Key: "esc", Label: "close"}}, w, h), "\n"), w, h)
				if panel, at, _ := th.KeyPanel([]KeyGroup{{"Go", []Binding{{[]string{"enter"}, "go"}}}}, w, h, PanelCorner, 0); panel != nil {
					base := make([]string, h)
					for i := range base {
						base[i] = strings.Repeat(" ", w)
					}
					checkSize(t, "keypanel "+size(w, h), strings.Join(th.Raise(base, panel, at), "\n"), w, h)
				}
			}

			var l List
			l.SetItems(5, []Section{{Title: "A", Start: 0}})
			l.SetHeight(h)
			l.Bottom()
			out := l.View(th, w, func(i, rw int, _ bool) Row { return Row{Line: Plain("漢字 row")} }, nil)
			if len(out) != h {
				t.Fatalf("list %s: %d rows", size(w, h), len(out))
			}
			for _, line := range out {
				if ansi.StringWidth(line) != w {
					t.Fatalf("list %s: row is %d cells", size(w, h), ansi.StringWidth(line))
				}
			}

			var p Preview
			p.Set(p.Begin(), Plain("title"), lines(20))
			p.Grid = []MetaRow{{"Task", Plain("t")}}
			if out := p.View(th, w, h); w > 0 && h > 0 {
				checkSize(t, "preview "+size(w, h), strings.Join(out, "\n"), w, h)
			}
		}
	}
}

// must3 keeps a field's lines.
func must3(lines []string, _, _ int) []string { return lines }
