package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/app/files"
	"rexkit/internal/app/hints"
	"rexkit/internal/app/hold"
	"rexkit/internal/app/keys"
	"rexkit/internal/app/rename"
	"rexkit/internal/app/scrollback"
	"rexkit/internal/app/switcher"
	"rexkit/internal/app/toast"
	"rexkit/internal/backdrop"
	"rexkit/internal/popup"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
	"rexkit/internal/ui/snap"
)

// placement is how the Lua layer places a tool in its whole-window float
// (lua/tools.lua card_flags, and app.notify for toasts).
type placement int

const (
	placeFloat  placement = iota // a card sized by the window's width
	placeDialog                  // a fixed-size dialog, centred
	placeToast                   // one row in the top-right corner
	placeWhole                   // the whole window or pane, no card
	placePane                    // exactly over one pane, the others as they are (snap.Scene.Window)
)

// snapTool is a tool `rexkit snap` can draw.
type snapTool struct {
	name   string
	states []string
	scene  func(dark bool, state string) (snap.Scene, error)
	place  placement
	sizes  []string // the window sizes of --all; nil is just snapSize
}

// snapSize is the user's usual window: 241×70 cells.
const snapSize = "241x70"

func snapTools() []snapTool {
	return []snapTool{
		{"goto", switcher.SnapStates, switcher.Snap, placeFloat, []string{snapSize, "110x34", "70x24"}},
		{"files", files.SnapStates, files.Snap, placeFloat, nil},
		{"keys", keys.SnapStates, keys.Snap, placeFloat, nil},
		{"rename", rename.SnapStates, rename.Snap, placeDialog, nil},
		{"toast", toast.SnapStates, toast.Snap, placeToast, nil},
		{"hold", hold.SnapStates, hold.Snap, placeWhole, nil},
		{"scrollback-loader", scrollback.SnapStates, scrollback.Snap, placeWhole, nil},
		{"hints", hints.SnapStates, hints.Snap, placePane, nil},
	}
}

// card is where Lua would draw the tool in a w×h window.
func (t snapTool) card(w int, s snap.Scene) popup.Card {
	switch t.place {
	case placeDialog:
		return popup.Card{Cols: s.Cols, Rows: s.Rows}
	case placeToast:
		return popup.Card{Cols: s.Cols, Rows: s.Rows, TopRight: true}
	case placeWhole:
		return popup.Card{}
	}
	var fw, fh float64
	switch {
	case w >= 120:
		fw, fh = 0.78, 0.74
	case w >= 80:
		fw, fh = 0.94, 0.88
	default:
		return popup.Card{}
	}
	return popup.Card{Area: [4]float64{(1 - fw) / 2, (1 - fh) / 2, fw, fh}}
}

// dims reports whether Lua dims the window around the tool's card (the
// backdrop setting): every card but a toast's. Tools without a card do not.
func (t snapTool) dims() bool { return t.place == placeFloat || t.place == placeDialog }

// What --backdrop can draw around a --window card that dims.
const (
	backdropDemo  = "demo"  // backdrop.Demo's made-up panes, dimmed, as with Lua's "dim"
	backdropClear = "clear" // the made-up panes as they are, no shadow: what "clear" shows over real panes
	backdropNone  = "none"  // nothing: the margin is see-through
)

// demoBackdrop draws backdrop.Demo's panes in theme behind a card at a
// strength: 1 fully dimmed (snapshots skip the fade), 0 the panes as they
// are, for a card that does not dim (a toast). Such a card is no layer
// over the panes, so they are drawn whole, under its Clear cells too. It
// keeps one demo a window size.
func demoBackdrop(theme rextheme.Theme, strength float64) popup.Backdrop {
	demos := map[[2]int]*backdrop.Backdrop{}
	return func(w, h int, card ui.Silhouette) string {
		d := demos[[2]int{w, h}]
		if d == nil {
			d = backdrop.Demo(theme, w, h)
			demos[[2]int{w, h}] = d
		}
		if strength == 0 {
			card = ui.Silhouette{}
		}
		return d.Frame(w, h, card, strength)
	}
}

// look is what shots in one theme are drawn with.
type look struct {
	renderer *snap.Renderer
	backdrop popup.Backdrop // nil for --backdrop none
	panes    popup.Backdrop // the demo panes undimmed, around cards that do not dim
}

// shot is one picture to take.
type shot struct {
	tool   snapTool
	state  string
	dark   bool
	w, h   int // the window
	window bool
}

// picture is a shot taken: the screen, its size, and whether a backdrop
// was drawn around the card.
type picture struct {
	content  string
	cursor   *snap.Bar
	w, h     int
	backdrop bool
}

func (s shot) theme() string {
	if s.dark {
		return "dark"
	}
	return "light"
}

// take drives the tool into its state and returns its screen. In its float
// the card is drawn over b where Lua would dim the window; elsewhere, or
// with b nil, the margin is see-through.
func (s shot) take(b, panes popup.Backdrop) (picture, error) {
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
	m := sc.Model
	if sc.Window != nil {
		return s.takeOverPane(sc, p)
	}
	if s.window {
		card := s.tool.card(p.w, sc)
		switch {
		case card.IsZero():
			b = nil
		case !s.tool.dims():
			b = panes
		}
		m = popup.Stage(m, card, popup.WithBackdrop(b))
		p.backdrop = b != nil
	} else if sc.Cols > 0 && sc.Rows > 0 {
		p.w, p.h = sc.Cols, sc.Rows
	}
	d := snap.NewDriver(m)
	d.Send(tea.WindowSizeMsg{Width: p.w, Height: p.h})
	if sc.Script != nil {
		sc.Script(d)
	}
	p.content, p.cursor = d.View(), d.Cursor()
	return p, nil
}

// takeOverPane shoots a tool drawn exactly over a pane (placePane): alone
// at the pane's size, or with --window at its place in the window, the
// other panes around it as they are (the float's margin is see-through).
func (s shot) takeOverPane(sc snap.Scene, p picture) (picture, error) {
	panes, at := sc.Window(p.w, p.h)
	m := sc.Model
	if s.window {
		card := popup.Card{
			Area: [4]float64{float64(at.Min.X) / float64(p.w), float64(at.Min.Y) / float64(p.h),
				float64(at.Dx()) / float64(p.w), float64(at.Dy()) / float64(p.h)},
			Cols: at.Dx(), Rows: at.Dy(),
		}
		m = popup.Stage(m, card, popup.WithBackdrop(func(int, int, ui.Silhouette) string { return panes }))
		p.backdrop = true
	} else {
		p.w, p.h = at.Dx(), at.Dy()
	}
	d := snap.NewDriver(m)
	d.Send(tea.WindowSizeMsg{Width: p.w, Height: p.h})
	if sc.Script != nil {
		sc.Script(d)
	}
	p.content, p.cursor = d.View(), d.Cursor()
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
	window := fs.Bool("window", false, "draw the tool in its card of a whole-window float, as Rex shows it")
	scale := fs.Int("scale", 2, "2 for Rex's Retina size (14×29 px a cell), 1 for half")
	backdropName := fs.String("backdrop", backdropDemo, "what shows around a --window card: `demo` (made-up panes, dimmed, as Lua's dim), clear (them undimmed, as Lua's clear) or none")
	all := fs.Bool("all", false, "draw every tool in every state into --out-dir, with an index.txt (with --window, only in their floats)")
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
	if *backdropName != backdropDemo && *backdropName != backdropClear && *backdropName != backdropNone {
		return fmt.Errorf("snap: --backdrop must be demo, clear or none, not %q", *backdropName)
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
	looks := map[bool]*look{}
	var index []string
	for _, s := range shots {
		lk := looks[s.dark]
		if lk == nil {
			theme := themeOf(s.dark)
			r, err := snap.NewRenderer(snap.Options{Palette: snap.PaletteOf(theme.Terminal), Half: *scale == 1})
			if err != nil {
				return err
			}
			lk = &look{renderer: r}
			switch *backdropName {
			case backdropDemo:
				lk.backdrop = demoBackdrop(theme, 1)
				lk.panes = demoBackdrop(theme, 0)
			case backdropClear:
				lk.backdrop = demoBackdrop(theme, 0)
				lk.panes = lk.backdrop
			}
			looks[s.dark] = lk
		}
		pic, err := s.take(lk.backdrop, lk.panes)
		if err != nil {
			return err
		}
		w, h := pic.w, pic.h
		path := *out
		if *all {
			name := fmt.Sprintf("%s-%s-%s-%dx%d", s.tool.name, s.state, s.theme(), w, h)
			if s.window {
				name += "-window"
			}
			if pic.backdrop {
				name += "-" + *backdropName
			}
			path = filepath.Join(*outDir, name+".png")
			index = append(index, fmt.Sprintf("%s\ttool=%s state=%s theme=%s size=%dx%d window=%t backdrop=%t",
				filepath.Base(path), s.tool.name, s.state, s.theme(), w, h, s.window, pic.backdrop))
			if err := os.MkdirAll(*outDir, 0o755); err != nil {
				return err
			}
		}
		if err := snap.WriteFile(path, lk.renderer.RenderBar(pic.content, w, h, pic.cursor)); err != nil {
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
	for _, lk := range looks {
		reportGlyphs(lk.renderer)
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
