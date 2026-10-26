package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

func TestNavigationRestoresQueryAndSelection(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	project := filepath.Join(root, "project space ' $(not-a-command)")

	e.typeText(t, "proj")
	if e.m.selectedPath() != project {
		t.Fatalf("query selected %q", e.selectedLabel())
	}
	e.press(t, "right")
	if e.m.dir != project || e.m.query.Value() != "" {
		t.Fatalf("→ did not enter the folder: dir %q query %q", e.m.dir, e.m.query.Value())
	}
	e.press(t, "left")
	if e.m.dir != root || e.m.query.Value() != "proj" || e.m.selectedPath() != project {
		t.Fatalf("← did not restore: dir %q query %q selected %q", e.m.dir, e.m.query.Value(), e.selectedLabel())
	}

	// ⌫ on an empty query goes up, selecting the folder it came from.
	e.press(t, "backspace", "backspace", "backspace", "backspace")
	if e.m.dir != root {
		t.Fatal("a held ⌫ that emptied the query walked up")
	}
	e.m.emptiedAt = time.Time{}
	e.press(t, "backspace")
	if e.m.dir != home || e.m.selectedPath() != root {
		t.Fatalf("⌫ on empty query: dir %q selected %q", e.m.dir, e.selectedLabel())
	}
}

func TestBreadcrumbClickGoesToAncestor(t *testing.T) {
	home, root := fixtureTree(t)
	for _, banded := range []bool{true, false} {
		h := 30
		if !banded {
			h = 12 // the compact header
		}
		e := newEnv(t, home, filepath.Join(root, "cmd", "rexkit"), 120, h)
		hits, x, y := e.headerHits()
		project := hits.Crumbs[1] // ~ › project › cmd › rexkit
		e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + project.X, Y: y})
		if e.m.dir != root || e.m.selectedPath() != filepath.Join(root, "cmd") {
			t.Fatalf("banded %v: crumb click: dir %q selected %q", banded, e.m.dir, e.selectedLabel())
		}
		hits, x, y = e.headerHits()
		here := hits.Crumbs[len(hits.Crumbs)-1]
		click := tea.MouseClickMsg{Button: tea.MouseLeft, X: x + here.X, Y: y}
		e.send(t, click)
		if _, ok := e.m.selected(); ok || e.m.path.active || e.m.target().dir != root {
			t.Fatalf("banded %v: clicking the current folder: selected %q, path field %v", banded, e.selectedLabel(), e.m.path.active)
		}
		e.send(t, click)
		if !e.m.path.active {
			t.Fatalf("banded %v: clicking the selected current folder did not open the path field", banded)
		}
	}
}

// TestChipClicksToggle: a click on a toggle's words, or on the edge rows of
// its shape once it is on, toggles it; the band's row of air does not.
func TestChipClicksToggle(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 160, 40)
	hits, x, y := e.headerHits()
	all := hits.Chips[0]
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + all.X + 1, Y: y})
	if !e.m.recursive {
		t.Fatal("clicking all files did not turn it on")
	}
	hits, x, y = e.headerHits()
	all = hits.Chips[0]
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + all.X + 1, Y: y + 2}) // the air row
	if !e.m.recursive {
		t.Fatal("a click on the band's air toggled all files")
	}
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + all.X + 1, Y: y - 1}) // the shape's top edge
	if e.m.recursive {
		t.Fatal("clicking the on chip's edge did not turn it off")
	}
	hidden := hits.Chips[1]
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + hidden.X + hidden.W - 1, Y: y})
	if !e.m.hidden {
		t.Fatal("clicking hidden did not show hidden files")
	}
}

// headerHits is the header's hit rects, and the screen cell where their
// coordinates start (the header's text row).
func (e *testEnv) headerHits() (ui.HeaderHits, int, int) {
	s := e.m.layout()
	_, hits, _ := e.m.headerView(s.areas.Header.W, s.areas.Banded())
	return hits, s.inner.X + s.areas.Header.X, s.inner.Y + s.areas.HeaderLine()
}

func TestRecursiveAndHiddenToggles(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	labels := func() []string {
		var out []string
		for _, r := range e.m.rows {
			out = append(out, r.e.label)
		}
		return out
	}
	if slices.Contains(labels(), "cmd/rexkit/main.go") {
		t.Fatal("shallow listing shows nested files")
	}
	e.press(t, "ctrl+f")
	got := labels()
	for _, want := range []string{"cmd/rexkit/main.go", "docs/guide.md", "node_modules"} {
		if !slices.Contains(got, want) {
			t.Errorf("recursive listing lacks %s", want)
		}
	}
	for _, label := range got {
		if strings.HasPrefix(label, "node_modules/") || strings.HasPrefix(label, ".") {
			t.Errorf("recursive listing entered %s", label)
		}
	}
	h, _ := e.m.header()
	if !h.Chips[0].On {
		t.Error("all files chip is off while recursive")
	}
	e.typeText(t, "guide")
	if e.selectedLabel() != "docs/guide.md" {
		t.Fatalf("recursive search selected %q", e.selectedLabel())
	}
	if pos := highlights(e.m.pattern, "docs/guide.md"); !slices.Equal(pos, []int{5, 6, 7, 8, 9}) {
		t.Errorf("highlights %v, want the base name", pos)
	}
	e.press(t, "alt+.")
	if !e.m.hidden || !slices.ContainsFunc(e.m.data.entries, func(en entry) bool { return en.rel == ".env" }) {
		t.Error("⌥. did not show hidden files")
	}
}

func TestVerbsCallRex(t *testing.T) {
	home, root := fixtureTree(t)
	readme := filepath.Join(root, "README.md")
	literal := filepath.Join(root, "quotes ' \" $(never-run)\nfolder")
	mustMkdir(t, literal)
	tests := []struct {
		name      string
		key       string
		selection string
		check     func(t *testing.T, e *testEnv)
	}{
		{"enter opens a tab in a file's folder", "enter", readme, func(t *testing.T, e *testEnv) {
			req := onlyWindow(t, e)
			opts := req.Layout.Block.Options
			if req.Label != "project" || opts.Cwd != root || opts.Shell != "/bin/zsh" || opts.Command != nil {
				t.Errorf("new_window %+v options %+v", req, opts)
			}
		}},
		{"enter on a folder opens it", "enter", filepath.Join(root, "docs"), func(t *testing.T, e *testEnv) {
			if req := onlyWindow(t, e); req.Layout.Block.Options.Cwd != filepath.Join(root, "docs") || req.Label != "docs" {
				t.Errorf("new_window %+v", req)
			}
		}},
		{"alt+enter splits right of the origin", "alt+enter", readme, func(t *testing.T, e *testEnv) {
			req := onlySplit(t, e)
			if req.AnchorID != e.origin || req.Direction != rex.Horizontal || req.Side != "after" || !req.Focus {
				t.Errorf("new_split %+v", req)
			}
		}},
		{"shift+enter splits below", "shift+enter", readme, func(t *testing.T, e *testEnv) {
			if req := onlySplit(t, e); req.Direction != rex.Vertical || req.Layout.Block.Options.Cwd != root {
				t.Errorf("new_split %+v", req)
			}
		}},
		{"a hostile folder name reaches Rex literally", "enter", literal, func(t *testing.T, e *testEnv) {
			req := onlyWindow(t, e)
			if req.Layout.Block.Options.Cwd != literal || strings.ContainsAny(req.Label, "\n\x1b") {
				t.Errorf("new_window %+v", req)
			}
		}},
		{"enter on a broken link opens its folder", "enter", filepath.Join(root, "broken-link"), func(t *testing.T, e *testEnv) {
			if req := onlyWindow(t, e); req.Layout.Block.Options.Cwd != root {
				t.Errorf("new_window %+v", req)
			}
		}},
		{"ctrl+e edits the file", "ctrl+e", readme, func(t *testing.T, e *testEnv) {
			req := onlyWindow(t, e)
			opts := req.Layout.Block.Options
			if req.Label != "README.md" || opts.Shell != "none" || !slices.Equal(opts.Command, []string{"/usr/bin/vi", readme}) {
				t.Errorf("edit new_window %+v options %+v", req, opts)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, home, root, 120, 30)
			e.selectPath(t, tt.selection)
			e.press(t, tt.key)
			tt.check(t, e)
			if !e.m.done {
				t.Error("the tool did not end after opening")
			}
			var r popup.Result
			if err := state.ReadJSON(filepath.Join(e.state, popup.ResultFile), &r); err != nil || r.BlockID == "" || r.WindowID == "" {
				t.Errorf("result %+v, %v", r, err)
			}
			h, _ := e.dirs.History()
			if len(h.Opens) != 1 || h.Opens[0].Path != tt.selection {
				t.Errorf("opens %+v", h.Opens)
			}
			if prev, ok := h.Previous(state.Visit{BlockID: r.BlockID}); !ok || prev.BlockID != e.origin {
				t.Errorf("previous location %+v, want the origin pane", prev)
			}
		})
	}
}

func onlyWindow(t *testing.T, e *testEnv) rex.NewWindowRequest {
	t.Helper()
	if len(e.rex.windows) != 1 || len(e.rex.splits) != 0 {
		t.Fatalf("windows %d splits %d, want one window", len(e.rex.windows), len(e.rex.splits))
	}
	return e.rex.windows[0]
}

func onlySplit(t *testing.T, e *testEnv) rex.NewSplitRequest {
	t.Helper()
	if len(e.rex.splits) != 1 || len(e.rex.windows) != 0 {
		t.Fatalf("windows %d splits %d, want one split", len(e.rex.windows), len(e.rex.splits))
	}
	return e.rex.splits[0]
}

func TestVerbFailuresKeepTheToolOpen(t *testing.T) {
	home, root := fixtureTree(t)
	tests := []struct {
		name  string
		setup func(e *testEnv)
		key   string
		want  string
	}{
		{"split after the origin closed", func(e *testEnv) { e.m.cfg.launcher.origin = "block:gone" }, "alt+enter", "has closed"},
		{"edit a folder", func(e *testEnv) { e.selectPath(t, filepath.Join(root, "docs")) }, "ctrl+e", "Choose a file"},
		{"server error", func(e *testEnv) {
			e.rex.Fail("session.new_window", &rex.Error{Op: "session.new_window", Message: "boom"})
		}, "enter", "Boom"},
		{"target vanished", func(e *testEnv) {
			e.selectPath(t, filepath.Join(root, "empty folder"))
			_ = os.Remove(filepath.Join(root, "empty folder"))
		}, "enter", "no longer exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, home, root, 120, 30)
			tt.setup(e)
			e.press(t, tt.key)
			if e.m.done || e.m.busy {
				t.Fatal("the tool ended or stayed busy after a failure")
			}
			if got := e.m.toast.Current().Text; !strings.Contains(got, tt.want) {
				t.Fatalf("toast %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDemoVerbsOnlyDescribe(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30, demoMode)
	e.press(t, "enter")
	if e.m.done || len(e.rex.Calls()) != 0 {
		t.Fatalf("demo created something: done %v calls %v", e.m.done, e.rex.Methods())
	}
	if got := e.m.toast.Current().Text; got != "Demo · would new tab in ~/project" {
		t.Fatalf("toast %q", got)
	}
}

func TestCopyAndOpenUseHelpers(t *testing.T) {
	home, root := fixtureTree(t)
	readme := filepath.Join(root, "README.md")
	var got [][]string
	fake := func(_ context.Context, _, stdin, name string, args ...string) ([]byte, error) {
		got = append(got, append([]string{name, stdin}, args...))
		return nil, nil
	}
	e := newEnv(t, home, root, 120, 30, func(c *config) { c.launcher.run = fake })
	e.selectPath(t, readme)
	e.press(t, "ctrl+y", "ctrl+x")
	want := [][]string{{"pbcopy", readme}, {"open", "", readme}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("helpers %q, want %q", got, want)
	}
	if e.m.done {
		t.Fatal("copy/open ended the tool")
	}
}

func TestFIFOPreviewDoesNotBlock(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 140, 30)
	done := make(chan struct{})
	go func() {
		e.selectPath(t, filepath.Join(root, "trap.png"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("selecting a FIFO blocked")
	}
	if text := strings.Join(e.screen(), "\n"); !strings.Contains(text, "Named pipe") {
		t.Fatalf("FIFO not explained:\n%s", text)
	}
}

func TestHostileNamesAreShownSafely(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 140, 40)
	view := e.m.View().Content
	for _, bad := range []string{"\x1b[31mname", "\u202e", "\x07"} {
		if strings.Contains(view, bad) {
			t.Errorf("view contains raw %q", bad)
		}
	}
	text := strings.Join(e.screen(), "\n")
	for _, shown := range []string{"evil␛[31mname.txt", "bidi<U+202E>txt.exe", "project space ' $(not-a-command)/", "broken-link ↗"} {
		if !strings.Contains(text, shown) {
			t.Errorf("view lacks %q", shown)
		}
	}
	e.selectPath(t, filepath.Join(root, "escapes.txt"))
	if text := strings.Join(e.screen(), "\n"); !strings.Contains(text, "␛]52;c;Y2xpcA==␇") {
		t.Errorf("file controls not shown as pictures:\n%s", text)
	}
	if strings.Contains(e.m.View().Content, "\x1b]52") {
		t.Error("OSC 52 from a file reached the screen")
	}
}

func TestResizeKeepsSelectionVisible(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "many")
	for i := range 80 {
		mustWrite(t, filepath.Join(root, "file-"+string(rune('a'+i/26))+string(rune('a'+i%26))+".txt"), "x")
	}
	e := newEnv(t, home, root, 140, 44)
	e.press(t, "up") // from the folder itself, ↑ wraps to the last row
	for _, size := range [][2]int{{140, 44}, {70, 24}, {60, 20}, {44, 12}, {200, 60}} {
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !slices.ContainsFunc(e.selection(), func(l string) bool { return strings.Contains(l, "file-db.txt") }) {
			t.Fatalf("%dx%d: selected row is off screen:\n%s", size[0], size[1], strings.Join(e.screen(), "\n"))
		}
	}
}

func TestPathFieldCompletesAndEscapeOnlyLeavesIt(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.typeText(t, "read")
	e.press(t, "ctrl+l")
	if !e.m.path.active || e.m.path.input.Value() != "~/project/" {
		t.Fatalf("path field %q", e.m.path.input.Value())
	}
	e.typeText(t, "cm")
	e.press(t, "tab")
	if got := e.m.path.input.Value(); got != "~/project/cmd/" {
		t.Fatalf("⇥ completed to %q", got)
	}
	if e.selectedLabel() != "rexkit" {
		t.Fatalf("live listing shows %q", e.selectedLabel())
	}
	e.press(t, "esc")
	if e.m.path.active || e.m.done || e.m.query.Value() != "read" || e.m.dir != root {
		t.Fatalf("⎋ in the path field: active %v done %v query %q", e.m.path.active, e.m.done, e.m.query.Value())
	}
	e.press(t, "ctrl+l")
	e.typeText(t, "docs/guide.md")
	e.press(t, "enter")
	if e.m.dir != filepath.Join(root, "docs") || e.selectedLabel() != "guide.md" {
		t.Fatalf("⏎ on a file path: dir %q selected %q", e.m.dir, e.selectedLabel())
	}
	e.press(t, "esc")
	if !e.m.done {
		t.Fatal("⎋ outside the path field did not close")
	}
}

func TestRecentHereComesFirst(t *testing.T) {
	home, root := fixtureTree(t)
	guide := filepath.Join(root, "docs", "guide.md")
	e := newEnv(t, home, root, 120, 30, func(c *config) {
		_ = c.dirs.UpdateHistory(func(h *state.History) { h.Open(guide, testNow) })
	})
	if e.m.rows[0].e.label != "docs/guide.md" || e.m.rows[0].from != fromRecent {
		t.Fatalf("first row %q", e.m.rows[0].e.label)
	}
	if e.press(t, "down"); e.selectedLabel() != "docs/guide.md" {
		t.Fatalf("↓ from the folder selected %q", e.selectedLabel())
	}
	if text := strings.Join(e.screen(), "\n"); !strings.Contains(text, "Recent here") || !strings.Contains(text, "In project") {
		t.Fatalf("sections missing:\n%s", text)
	}
}

func TestFooterSaysWhatEnterDoes(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 70, 20)
	e.selectPath(t, filepath.Join(root, "README.md"))
	if footer := footerLine(e); !strings.Contains(footer, "enter▌ new tab in ~/project") {
		t.Fatalf("footer %q", footer)
	}
	e.m.cfg.home = ""
	if footer := footerLine(e); !strings.Contains(footer, "enter▌ new tab in /") || !strings.Contains(footer, "…") || !strings.Contains(footer, "/project ") {
		t.Fatalf("long path not cut in the middle: %q", footer)
	}
}

func TestStaleLoadsAreIgnored(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 140, 30)
	before := e.m.selectedPath()
	e.send(t, listingMsg{rev: e.m.scanRev - 1, data: listing{root: root, entries: []entry{{path: "/stale", label: "stale"}}}})
	if e.m.selectedPath() != before {
		t.Fatal("a stale listing replaced the current one")
	}
	e.send(t, previewMsg{rev: e.m.previewRev - 1, doc: previewDoc{name: "stale", lines: []string{"stale"}}})
	if strings.Contains(strings.Join(e.m.preview.Lines, ""), "stale") {
		t.Fatal("a stale preview replaced the current one")
	}
}

func TestBlurClosesUnlessDemo(t *testing.T) {
	home, root := fixtureTree(t)
	for _, tt := range []struct {
		opts []envOption
		want bool
	}{
		{[]envOption{func(c *config) { c.dismissOnBlur = true }}, true},
		{[]envOption{func(c *config) { c.dismissOnBlur = false }}, false},
		{[]envOption{func(c *config) { c.dismissOnBlur = true }, demoMode}, false},
	} {
		e := newEnv(t, home, root, 100, 30, tt.opts...)
		e.send(t, tea.BlurMsg{})
		if e.m.done != tt.want {
			t.Errorf("blur closed = %v, want %v", e.m.done, tt.want)
		}
	}
}

func TestEditorArgv(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	tests := []struct {
		json string
		vars map[string]string
		want []string
		err  bool
	}{
		{json: `["/opt/nvim","-p"]`, want: []string{"/opt/nvim", "-p"}},
		{vars: map[string]string{"VISUAL": "/usr/bin/vi -R", "EDITOR": "nano"}, want: []string{"/usr/bin/vi", "-R"}},
		{vars: map[string]string{"EDITOR": "/bin/ed"}, want: []string{"/bin/ed"}},
		{json: `[]`, err: true},
		{json: `"nvim"`, err: true},
	}
	for _, tt := range tests {
		got, err := editorArgv(tt.json, env(tt.vars))
		if (err != nil) != tt.err || (!tt.err && !slices.Equal(got, tt.want)) {
			t.Errorf("editorArgv(%q, %v) = %q, %v", tt.json, tt.vars, got, err)
		}
	}
	if got, _ := editorArgv("", env(nil)); filepath.Base(got[0]) != "vi" {
		t.Errorf("default editor %q", got)
	}
	raw, _ := json.Marshal([]string{"/usr/bin/vi"})
	if o, err := parseFlags([]string{"--editor-json", string(raw), "--directory", "/nonexistent/x"}, env(nil)); err != nil || o.notice == "" || o.dir != homeDir() {
		t.Errorf("parseFlags: %+v, %v", o, err)
	}
}

// footerLine is the tray's row with runs of spaces squeezed: keycaps pad
// their labels.
func footerLine(e *testEnv) string {
	w := e.m.layout().areas.Footer.W
	return squeeze(ansi.Strip(e.m.theme.Tray(e.m.tray(), w, 1)[0]))
}

func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") + " " }

func TestClearingTheSearchKeepsTheFind(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 100, 30)
	e.typeText(t, "go.m")
	if e.selectedLabel() != "go.mod" {
		t.Fatalf("search selected %q", e.selectedLabel())
	}
	e.press(t, "backspace", "backspace", "backspace", "backspace")
	if e.m.query.Value() != "" || e.selectedLabel() != "go.mod" {
		t.Fatalf("after clearing: query %q, selected %q", e.m.query.Value(), e.selectedLabel())
	}
	if !slices.ContainsFunc(e.selection(), func(l string) bool { return strings.Contains(l, "· go.mod") }) {
		t.Errorf("the find is not visible:\n%s", strings.Join(e.screen(), "\n"))
	}
}

func TestSearchingAllFilesLeadsWithTheBestMatch(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 100, 30)
	e.typeText(t, "main")
	if got := e.selectedLabel(); got == "cmd/rexkit/main.go" || got == "" {
		t.Fatalf("this folder alone selected %q", got)
	}
	e.press(t, "ctrl+f")
	if got := e.selectedLabel(); got != "cmd/rexkit/main.go" {
		t.Fatalf("after ⌃F the selection is %q, want the best match", got)
	}
}

// TestEnterStartsOnTheFolderItself: Files opens resting on its folder (no
// row selected), so ⏎ straight away opens a tab there; ↓ selects the first
// row and ⏎ then opens that.
func TestEnterStartsOnTheFolderItself(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	if r, ok := e.m.selected(); ok {
		t.Fatalf("a row is selected at the start: %q", r.e.label)
	}
	if footer := footerLine(e); !strings.Contains(footer, "enter▌ new tab in ~/project ") {
		t.Fatalf("footer %q", footer)
	}
	if len(e.selection()) > 0 {
		t.Fatalf("a row looks selected:\n%s", strings.Join(e.screen(), "\n"))
	}
	e.press(t, "enter")
	if req := onlyWindow(t, e); req.Layout.Block.Options.Cwd != root || req.Label != "project" {
		t.Fatalf("⏎ at the start opened %+v", req.Layout.Block.Options)
	}

	e = newEnv(t, home, root, 120, 30)
	e.press(t, "down")
	if got := e.selectedLabel(); got != "cmd" {
		t.Fatalf("↓ selected %q, want the first row", got)
	}
	e.press(t, "up")
	if _, ok := e.m.selected(); ok {
		t.Fatal("↑ from the first row did not go back to the folder")
	}
	e.press(t, "down", "enter")
	if req := onlyWindow(t, e); req.Layout.Block.Options.Cwd != filepath.Join(root, "cmd") {
		t.Fatalf("↓ ⏎ opened %+v", req.Layout.Block.Options)
	}
}

// TestKeysOnTheFolderItself: with nothing selected → has nothing to enter
// and ⌃E nothing to edit; entering a folder rests on it again.
func TestKeysOnTheFolderItself(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.press(t, "right")
	if e.m.dir != root {
		t.Fatalf("→ with nothing selected entered %q", e.m.dir)
	}
	e.press(t, "ctrl+e")
	if len(e.rex.windows) != 0 || !strings.Contains(e.m.toast.Current().Text, "Choose a file") {
		t.Fatalf("⌃E on the folder: windows %d toast %q", len(e.rex.windows), e.m.toast.Current().Text)
	}
	e.selectPath(t, filepath.Join(root, "docs"))
	e.press(t, "right")
	if _, ok := e.m.selected(); ok || e.m.dir != filepath.Join(root, "docs") {
		t.Fatalf("entering docs selected %q", e.selectedLabel())
	}
	e.press(t, "left")
	if e.m.selectedPath() != filepath.Join(root, "docs") {
		t.Fatalf("← selected %q, want docs", e.selectedLabel())
	}
}

// TestStartSelectionDoesNotDependOnLoadOrder delivers zoxide's projects
// before and after the listing: either way Files rests on its folder, never
// on a project.
func TestStartSelectionDoesNotDependOnLoadOrder(t *testing.T) {
	home, root := fixtureTree(t)
	other := filepath.Join(home, "other")
	mustMkdir(t, other)
	project, err := statEntry(other, "~/other")
	if err != nil {
		t.Fatal(err)
	}
	for _, projectsFirst := range []bool{true, false} {
		e := newEnv(t, home, root, 120, 30)
		load := e.m.load() // the start: the listing still in flight
		if projectsFirst {
			e.send(t, projectsMsg{[]entry{project}})
			e.do(t, load)
		} else {
			e.do(t, load)
			e.send(t, projectsMsg{[]entry{project}})
		}
		if e.m.rows[0].from != fromProject {
			t.Fatalf("projects first %v: the project is not listed", projectsFirst)
		}
		if _, ok := e.m.selected(); ok || e.m.target().dir != root {
			t.Errorf("projects first %v: selected %q, ⏎ opens %q", projectsFirst, e.selectedLabel(), e.m.target().dir)
		}
	}
}

// TestUnreadableFolder: the reason is shown in the list, projects are not
// offered as if they were its content, and ⏎ still targets the folder.
func TestUnreadableFolder(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "noperm")
	mustMkdir(t, root)
	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, err := os.ReadDir(root); err == nil {
		t.Skip("running with permission to read anything")
	}
	e := newEnv(t, home, root, 120, 30)
	other, _ := statEntry(t.TempDir(), "~/website")
	e.send(t, projectsMsg{[]entry{other}})
	if len(e.m.rows) != 0 || e.m.target().dir != root {
		t.Fatalf("rows %d, ⏎ opens %q", len(e.m.rows), e.m.target().dir)
	}
	text := strings.Join(e.screen(), "\n")
	if !strings.Contains(text, "✗ Can't read this folder") || !strings.Contains(text, "permission denied") {
		t.Fatalf("no reason in the body:\n%s", text)
	}
}

// TestDoubleClickInThePathField accepts the completion like ⏎: the field
// closes and the folder is shown; one ⎋ then closes Files.
func TestDoubleClickInThePathField(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.press(t, "ctrl+l")
	i := e.m.indexOf(filepath.Join(root, "docs"))
	if i < 0 {
		t.Fatal("docs is not offered")
	}
	s := e.m.layout()
	y := -1
	for row := range e.m.list.Height() {
		if item, ok := e.m.list.ItemAt(row); ok && item == i {
			y = row
		}
	}
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: s.inner.X + s.areas.List.X + 4, Y: s.inner.Y + s.areas.List.Y + y}
	e.send(t, click)
	e.send(t, click)
	if e.m.path.active || e.m.dir != filepath.Join(root, "docs") {
		t.Fatalf("double-click: path field %v dir %q", e.m.path.active, e.m.dir)
	}
	if len(e.m.rows) != 1 || e.m.rows[0].e.label != "guide.md" {
		t.Fatalf("rows are not docs': %+v", e.m.rows)
	}
	e.press(t, "esc")
	if !e.m.done {
		t.Fatal("⎋ did not close Files")
	}
}

func TestPathFieldIsNotCalledGoTo(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.press(t, "ctrl+l")
	if h, _ := e.m.header(); h.Title != "Path" {
		t.Errorf("path field title %q", h.Title)
	}
	if footer := footerLine(e); !strings.Contains(footer, "tab▌ complete") {
		t.Errorf("footer %q", footer)
	}
}

// TestNarrowFooterKeepsKeySheet: the path in ⏎'s outcome is cut first,
// then other hints give way, but "? keys" (how every other key is found)
// stays.
func TestNarrowFooterKeepsKeySheet(t *testing.T) {
	home, root := fixtureTree(t)
	deep := filepath.Join(root, "project space ' $(not-a-command)")
	for _, w := range []int{70, 56, 44} {
		e := newEnv(t, home, deep, w, 20)
		e.m.cfg.home = "" // a long path
		footer := footerLine(e)
		if !strings.Contains(footer, "?▌ keys") || !strings.Contains(footer, "enter▌ new tab") {
			t.Errorf("width %d: footer %q", w, footer)
		}
		if w >= 56 && (!strings.Contains(footer, "…") || !strings.Contains(footer, "' $(not-a-command) ")) {
			t.Errorf("width %d: the path's end is not kept: %q", w, footer)
		}
	}
}

func TestErrorTextCapitalisesRunes(t *testing.T) {
	for in, want := range map[string]string{
		"⌃E edits files · choose a file": "⌃E edits files · choose a file",
		"é…":                             "É…",
		"boom":                           "Boom",
	} {
		if got := errorText(errors.New(in)); got != want {
			t.Errorf("errorText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCtrlJMovesAfterStart: ⌃J is ⏎ only in the first moments (ui.EarlyKeys);
// later ⌃J and ⌃K move like ↓ and ↑, as in Go to.
func TestCtrlJMovesAfterStart(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.m.started = e.m.started.Add(-time.Second)
	e.press(t, "ctrl+j")
	if got := e.selectedLabel(); got != "cmd" {
		t.Fatalf("⌃J selected %q, want the first row", got)
	}
	e.press(t, "ctrl+k")
	if _, ok := e.m.selected(); ok {
		t.Fatal("⌃K from the first row did not go back to the folder")
	}
}

// TestSelectionIsMarked: the selected row is washed and carries ▍ in its
// gutter; resting on the folder itself, no row is.
func TestSelectionIsMarked(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	if sel := e.selection(); len(sel) != 0 {
		t.Fatalf("rows washed at the start: %q", sel)
	}
	e.selectPath(t, filepath.Join(root, "README.md"))
	sel := e.selection()
	if len(sel) != 1 || !strings.HasPrefix(sel[0], "▍") || !strings.Contains(sel[0], "README.md") {
		t.Fatalf("selected row %q, want README.md marked with ▍", sel)
	}
}

// TestBusyTicksApart: "Opening…" ticks on a chain of its own. The loading
// spinner's ticks never carry it on (each would start one more chain), and
// an earlier verb's chain ends when the next verb starts.
func TestBusyTicksApart(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	if e.m.spinner.Active() {
		t.Fatal("still loading")
	}
	tick := func(msg tea.Msg) bool {
		_, cmd := e.m.Update(msg)
		return cmd != nil
	}
	if e.m.setBusy(true) == nil {
		t.Fatal("a verb started no tick")
	}
	first := busyTickMsg{e.m.busyRev}
	if tick(ui.SpinnerTickMsg(e.m.cfg.now())) {
		t.Error("a loading tick carried on while only a verb runs")
	}
	if !tick(first) {
		t.Error("the verb's chain ended while it runs")
	}
	e.m.setBusy(false)
	e.m.setBusy(true)
	if tick(first) {
		t.Error("an earlier verb's chain carried on")
	}
	if !tick(busyTickMsg{e.m.busyRev}) {
		t.Error("the running verb's chain ended")
	}
	e.m.setBusy(false)
	if tick(busyTickMsg{e.m.busyRev}) {
		t.Error("the chain outlived its verb")
	}
}

// TestKeyPanel: ? opens the panel; a click on it keeps it, a click outside
// closes it; any key but ? and ⎋ closes it and does its job.
func TestKeyPanel(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 140, 36)
	e.press(t, "?")
	s := e.m.layout()
	at := e.m.keyPanelAt(s.areas)
	if !e.m.help || at.Empty() {
		t.Fatal("? did not open the key panel")
	}
	if text := strings.Join(e.screen(), "\n"); !strings.Contains(text, "Keys") || !strings.Contains(text, "Split right of your pane") {
		t.Fatalf("no key panel:\n%s", text)
	}
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: s.inner.X + at.X + 3, Y: s.inner.Y + at.Y + 3})
	if !e.m.help {
		t.Fatal("a click on the panel closed it")
	}
	e.send(t, tea.MouseClickMsg{Button: tea.MouseLeft, X: s.inner.X + 2, Y: s.inner.Y + s.areas.List.Y})
	if e.m.help {
		t.Fatal("a click outside the panel left it open")
	}
	if at := e.m.keyPanelAt(s.areas); !at.Empty() || listRect(s.areas, at) != s.areas.List {
		t.Errorf("a closed panel is still placed at %+v", at)
	}
	e.press(t, "?", "down")
	if e.m.help || e.selectedLabel() != "cmd" {
		t.Fatalf("↓ with the panel open: panel %v, selected %q", e.m.help, e.selectedLabel())
	}
}

// TestKeyPanelScrolls: a panel too short for its keys scrolls with ↑ ↓
// instead of closing; any other key still closes it.
func TestKeyPanelScrolls(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 66, 22)
	e.press(t, "?")
	if _, _, more := e.m.keyPanel(e.m.layout().areas); more == 0 {
		t.Skip("the panel shows every key at this size")
	}
	if text := strings.Join(e.screen(), "\n"); strings.Contains(text, "Close") {
		t.Fatalf("the panel's last key shows before it scrolls:\n%s", text)
	}
	e.press(t, "pgdown")
	if text := strings.Join(e.screen(), "\n"); !e.m.help || !strings.Contains(text, "Close") {
		t.Fatalf("pgdn did not scroll the panel to its end (open: %v):\n%s", e.m.help, text)
	}
	e.press(t, "up")
	if !e.m.help || e.m.helpAt == 0 {
		t.Fatalf("↑ closed the panel or jumped: open %v, at %d", e.m.help, e.m.helpAt)
	}
	e.press(t, "right")
	if e.m.help {
		t.Fatal("→ left the panel open")
	}
}

// TestKeyPanelCoversTheWell: a panel that would leave a sliver of the
// preview's well beside it covers the well whole.
func TestKeyPanelCoversTheWell(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 186, 60) // the panel would start 3 columns into the well
	e.press(t, "?")
	a := e.m.layout().areas
	if a.Mode != ui.PreviewSide {
		t.Skipf("no preview beside the list (mode %v)", a.Mode)
	}
	_, at, _ := e.m.keyPanel(a)
	well := a.Preview.X - a.Body.X + 1
	if gap := at.X - well; gap > 0 && gap <= wellSliver {
		t.Errorf("the panel at column %d leaves %d columns of the well (from %d) beside it", at.X, gap, well)
	}
}

// TestKeyPanelKeepsTheSelectionInView: where the panel lies over the end
// of the list, the list scrolls so the selected row stays above it.
func TestKeyPanelKeepsTheSelectionInView(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 100, 40) // stacked: the panel covers the list's end
	e.selectPath(t, filepath.Join(root, "README.md"))
	e.press(t, "?")
	a := e.m.layout().areas
	at := e.m.keyPanelAt(a)
	if r := listRect(a, at); r.H >= a.List.H || r.Y+r.H > at.Y {
		t.Fatalf("list %+v under the panel at %+v", r, at)
	}
	sel := e.selection()
	if len(sel) != 1 || !strings.Contains(sel[0], "README.md") {
		t.Fatalf("selected row hidden by the panel: %q", sel)
	}
}

// TestPathFieldRefusesNowhere: a typed path with nothing by its name turns
// the field into the error fill; the page says so, once, and the tray
// offers the ways out, "? keys" last.
func TestPathFieldRefusesNowhere(t *testing.T) {
	home, root := fixtureTree(t)
	e := newEnv(t, home, root, 120, 30)
	e.press(t, "ctrl+l")
	if e.m.pathRefused() {
		t.Fatal("the folder itself was refused")
	}
	e.typeText(t, "zz")
	if !e.m.pathRefused() {
		t.Fatal("a path to nothing was not refused")
	}
	tr := e.m.tray()
	if tr.Toast.Text != "" {
		t.Errorf("tray toast %q repeats the page", tr.Toast.Text)
	}
	if n := len(tr.Hints); n == 0 || tr.Hints[0].Key != "backspace" || tr.Hints[n-1].Key != "?" {
		t.Errorf("tray hints %+v", tr.Hints)
	}
	r, g, b, _ := e.m.theme.C.ErrField.RGBA()
	if fill := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8); !strings.Contains(e.m.View().Content, fill) {
		t.Error("the field is not on the error fill")
	}
}
