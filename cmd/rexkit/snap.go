package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"image"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/app/files"
	"rexkit/internal/app/hints"
	"rexkit/internal/app/keys"
	"rexkit/internal/app/rename"
	"rexkit/internal/app/scrollback"
	"rexkit/internal/app/search"
	"rexkit/internal/app/switcher"
	"rexkit/internal/app/toast"
	"rexkit/internal/panes"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// placement is how the Lua layer places a tool's float in the window
// (lua/tools.lua place, and app.notify for toasts).
type placement int

const (
	placeFloat  placement = iota // the float size: a share of the window, by its width
	placeDialog                  // a fixed size, centred over the pane it opens over
	placeToast                   // fitted to its message, in the window's top-right corner
	placePane                    // exactly over the pane it opens over
)

// snapTool is a tool `rexkit snap` can draw.
type snapTool struct {
	name   string
	states []string
	scene  func(dark bool, state string) (snap.Scene, error)
	place  placement
	sizes  []string // the window sizes of --all; nil is just snapSize
	title  string   // the float's title, unless the scene has its own
}

// snapSize is the user's usual window: 241×70 cells.
const snapSize = "241x70"

func snapTools() []snapTool {
	return []snapTool{
		{"goto", switcher.SnapStates, switcher.Snap, placeFloat, []string{snapSize, "110x34", "70x24"}, "Go to"},
		{"files", files.SnapStates, files.Snap, placeFloat, nil, "Files"},
		{"search", search.SnapStates, search.Snap, placeFloat, nil, "Search"},
		{"keys", keys.SnapStates, keys.Snap, placeFloat, nil, "Keys"},
		{"rename", rename.SnapStates, rename.Snap, placeDialog, nil, ""},
		{"toast", toast.SnapStates, toast.Snap, placeToast, nil, ""},
		{"scrollback-loader", scrollback.SnapStates, scrollback.Snap, placePane, nil, "Scrollback"},
		{"hints", hints.SnapStates, hints.Snap, placePane, nil, ""},
	}
}

// floatShare is the float size's share of a window w columns wide, as Lua
// sizes it: most of a window 120 columns wide or more, all of a narrower
// one.
func floatShare(w int) (fw, fh float64) {
	if w >= 120 {
		return 0.78, 0.74
	}
	return 1, 1
}

// placeIn is where Lua opens the tool's float in a window whose layout
// area is l and whose one pane would be w columns wide, over the pane
// at over.
func (t snapTool) placeIn(l panes.Layout, w int, over rex.Rect, sc snap.Scene) image.Rectangle {
	switch t.place {
	case placeDialog:
		size := panes.Size(sc.Cols, sc.Rows, snap.CellW, snap.CellH)
		size = image.Pt(min(size.X, l.W), min(size.Y, l.H))
		pane := l.Pane(over)
		at := pane.Min.Add(pane.Size().Sub(size).Div(2))
		at.X = min(max(at.X, 0), l.W-size.X)
		at.Y = min(max(at.Y, 0), l.H-size.Y)
		return image.Rectangle{Min: at, Max: at.Add(size)}
	case placeToast:
		cols := sc.Cols
		if sc.FitCols != nil {
			cols = sc.FitCols(w)
		}
		size := panes.Size(cols, max(sc.Rows, 1), snap.CellW, snap.CellH)
		return image.Rect(l.W-size.X, 0, l.W, size.Y)
	case placePane:
		return l.Pane(over)
	}
	fw, fh := floatShare(w)
	return l.Float(rex.Rect{(1 - fw) / 2, (1 - fh) / 2, fw, fh})
}

// shot is one picture to take.
type shot struct {
	tool   snapTool
	state  string
	dark   bool
	w, h   int // the window, in the cells of its one pane
	window bool
}

// picture is a shot taken: the tool's screen at its size and, for
// --window, the window it is drawn in.
type picture struct {
	content string
	cursor  *snap.Bar
	w, h    int // the window, in the cells of its one pane
	cols    int // the tool's float, in cells
	rows    int
	window  *snap.Window
}

func (s shot) theme() string {
	if s.dark {
		return "dark"
	}
	return "light"
}

// take drives the tool into its state at the size of its float in the
// window, and with --window lays the window out around it: the made-up
// panes (or the scene's), the float where Lua opens it, and the toast the
// tool leaves, if any.
func (s shot) take(theme rextheme.Theme) (picture, error) {
	sc, err := s.tool.scene(s.dark, s.state)
	if err != nil {
		return picture{}, err
	}
	if sc.Cleanup != nil {
		defer sc.Cleanup()
	}
	p := picture{w: s.w, h: s.h}
	if sc.W > 0 && sc.H > 0 {
		p.w, p.h = sc.W, sc.H
	}
	layout := panes.LayoutFor(p.w, p.h, snap.CellW, snap.CellH)
	demo := sc.Panes
	if demo == nil {
		demo = panes.DemoPanes()
	}
	over := demo[min(sc.Over, len(demo)-1)].Rect
	at := s.tool.placeIn(layout, p.w, over, sc)
	p.cols, p.rows, _ = panes.Content(at, snap.CellW, snap.CellH)

	d := snap.NewDriver(sc.Model)
	d.Send(tea.WindowSizeMsg{Width: p.cols, Height: p.rows})
	if sc.Script != nil {
		sc.Script(d)
	}
	p.content, p.cursor = d.View(), d.Cursor()
	if !s.window {
		return p, nil
	}
	win := &snap.Window{Layout: layout, Panes: panes.DemoScreens(demo, layout, snap.CellW, snap.CellH).Panes}
	title := cmp.Or(sc.Title, s.tool.title)
	if !d.Quit() { // a tool that quit (Hints with nothing to pick) has closed its float
		win.Floats = append(win.Floats, snap.Float{At: at, Title: title, Content: p.content, Cursor: p.cursor})
	}
	if sc.Toast.Text != "" {
		th := ui.FromRex(theme)
		cols := th.ToastWidth(sc.Toast, p.w)
		size := panes.Size(cols, 1, snap.CellW, snap.CellH)
		win.Floats = append(win.Floats, snap.Float{At: image.Rect(layout.W-size.X, 0, layout.W, size.Y),
			Title: title, Content: ui.Bleed(th.ToastRow(sc.Toast, cols))})
	}
	p.window = win
	return p, nil
}

// snapCmd is `rexkit snap`: draw a tool in a scripted state to a PNG.
func snapCmd(args []string) error {
	// Snapshots are drawn as Rex draws them, with sprites, unless
	// REXKIT_SHAPES asks for another mode.
	if os.Getenv(ui.EnvShapes) == "" {
		ui.SetShapes(ui.ShapesSprite)
	}
	fs := flag.NewFlagSet("snap", flag.ContinueOnError)
	var names []string
	for _, t := range snapTools() {
		names = append(names, t.name)
	}
	tool := fs.String("tool", "goto", "the `TOOL` to draw: "+strings.Join(names, ", "))
	state := fs.String("state", "", "the tool's `STATE` (default its first; see -list)")
	size := fs.String("size", snapSize, "window size in cells, `COLSxROWS`")
	themeName := fs.String("theme", "dark", "`dark` or light (with --all also both)")
	rexTheme := fs.String("rex-theme", "merino", "draw in Rex's default theme (`merino`) or in the user's own (mine)")
	out := fs.String("out", "", "the PNG `FILE` to write")
	window := fs.Bool("window", false, "draw the tool in its float in a made-up window, as Rex shows it")
	scale := fs.Int("scale", 2, "2 for Rex's Retina size (14×29 px a cell), 1 for half")
	all := fs.Bool("all", false, "draw every tool in every state into --out-dir, with an index.txt (with --window, only in their windows)")
	outDir := fs.String("out-dir", "", "the `DIR` for --all")
	list := fs.Bool("list", false, "list the tools and their states")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if *list {
		for _, t := range snapTools() {
			fmt.Printf("%-18s %s\n", t.name, strings.Join(t.states, " "))
		}
		return nil
	}
	if *scale != 1 && *scale != 2 {
		return errors.New("snap: --scale must be 1 or 2")
	}
	themes, err := snapThemes(*themeName, *all)
	if err != nil {
		return err
	}
	themeOf := snap.Theme
	switch *rexTheme {
	case "merino":
	case "mine":
		ui.UseUserThemes()
		themeOf = ui.RexTheme
	default:
		return fmt.Errorf("snap: --rex-theme must be merino or mine, not %q", *rexTheme)
	}
	var shots []shot
	if *all {
		if *outDir == "" {
			return errors.New("snap: --all needs --out-dir")
		}
		shots, err = allShots(themes, *window)
	} else {
		if *out == "" {
			return errors.New("snap: --out is required")
		}
		shots, err = oneShot(*tool, *state, *size, themes[0], *window)
	}
	if err != nil {
		return err
	}
	renderers := map[bool]*snap.Renderer{}
	var index []string
	for _, s := range shots {
		theme := themeOf(s.dark)
		r := renderers[s.dark]
		if r == nil {
			// A tool's default background is its float's surface.
			pal := snap.PaletteOf(theme.Terminal)
			pal.Bg = snap.SurfaceOf(theme)
			if r, err = snap.NewRenderer(snap.Options{Palette: pal, Half: *scale == 1}); err != nil {
				return err
			}
			renderers[s.dark] = r
		}
		pic, err := s.take(theme)
		if err != nil {
			return err
		}
		path := *out
		if *all {
			name := fmt.Sprintf("%s-%s-%s-%dx%d", s.tool.name, s.state, s.theme(), pic.w, pic.h)
			if s.window {
				name += "-window"
			}
			path = filepath.Join(*outDir, name+".png")
			index = append(index, fmt.Sprintf("%s\ttool=%s state=%s theme=%s size=%dx%d float=%dx%d window=%t",
				filepath.Base(path), s.tool.name, s.state, s.theme(), pic.w, pic.h, pic.cols, pic.rows, s.window))
			if err := os.MkdirAll(*outDir, 0o755); err != nil {
				return err
			}
		}
		var img image.Image
		if pic.window != nil {
			img = r.Window(theme, *pic.window)
		} else {
			img = r.RenderBar(pic.content, pic.cols, pic.rows, pic.cursor)
		}
		if err := snap.WriteFile(path, img); err != nil {
			return err
		}
		if !*all {
			fmt.Println(path)
		}
	}
	if *all {
		if err := os.WriteFile(filepath.Join(*outDir, "index.txt"), []byte(strings.Join(index, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Printf("%d images in %s\n", len(index), *outDir)
	}
	for _, r := range renderers {
		reportGlyphs(r)
	}
	return nil
}

func snapThemes(name string, all bool) ([]bool, error) {
	switch name {
	case "dark":
		return []bool{true}, nil
	case "light":
		return []bool{false}, nil
	case "both":
		if all {
			return []bool{true, false}, nil
		}
	}
	return nil, fmt.Errorf("snap: --theme must be dark or light (or both with --all), not %q", name)
}

func oneShot(name, state, size string, dark, window bool) ([]shot, error) {
	i := slices.IndexFunc(snapTools(), func(t snapTool) bool { return t.name == name })
	if i < 0 {
		return nil, fmt.Errorf("snap: no tool %q (see rexkit snap -list)", name)
	}
	t := snapTools()[i]
	if state == "" {
		state = t.states[0]
	}
	w, h, err := parseSize(size)
	if err != nil {
		return nil, err
	}
	return []shot{{tool: t, state: state, dark: dark, w: w, h: h, window: window}}, nil
}

// allShots is every tool in every state and theme, alone and in its float
// (only in its float with windowOnly), at each of the tool's sizes. A state
// that forces its own size is drawn once.
func allShots(themes []bool, windowOnly bool) ([]shot, error) {
	var shots []shot
	for _, t := range snapTools() {
		sizes := t.sizes
		if sizes == nil {
			sizes = []string{snapSize}
		}
		for _, state := range t.states {
			for _, dark := range themes {
				for i, size := range sizes {
					w, h, err := parseSize(size)
					if err != nil {
						return nil, err
					}
					if i > 0 && forcesSize(t, state) {
						break
					}
					if !windowOnly {
						shots = append(shots, shot{tool: t, state: state, dark: dark, w: w, h: h})
					}
					shots = append(shots, shot{tool: t, state: state, dark: dark, w: w, h: h, window: true})
				}
			}
		}
	}
	return shots, nil
}

// forcesSize reports whether a state draws at a size of its own.
func forcesSize(t snapTool, state string) bool {
	sc, err := t.scene(true, state)
	if err != nil {
		return false
	}
	if sc.Cleanup != nil {
		sc.Cleanup()
	}
	return sc.W > 0
}

func parseSize(s string) (int, int, error) {
	c, r, ok := strings.Cut(s, "x")
	w, err1 := strconv.Atoi(c)
	h, err2 := strconv.Atoi(r)
	if !ok || err1 != nil || err2 != nil || w <= 0 || h <= 0 || w > 1000 || h > 500 {
		return 0, 0, fmt.Errorf("snap: --size must be COLSxROWS, not %q", s)
	}
	return w, h, nil
}

// reportGlyphs says which characters came from a fallback font, and which
// none could draw.
func reportGlyphs(r *snap.Renderer) {
	if missing := r.Missing(); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "snap: no font has %q\n", string(missing))
	}
	byFont := map[string][]rune{}
	for ch, font := range r.Fallbacks() {
		byFont[font] = append(byFont[font], ch)
	}
	for _, font := range slices.Sorted(maps.Keys(byFont)) {
		runes := byFont[font]
		slices.Sort(runes)
		fmt.Fprintf(os.Stderr, "snap: from %s: %s\n", font, string(runes))
	}
}
