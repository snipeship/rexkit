package popup

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/backdrop"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

// Backdrop settings (--backdrop): what shows around a card.
const (
	BackdropDim   = "dim"   // the window's own panes, dimmed, with a shadow under the card
	BackdropClear = "clear" // the default background, which Rex draws see-through
)

// dimTimeout bounds loading the window behind the card.
const dimTimeout = 3 * time.Second

// animated is a backdrop that loads and animates itself: Stage hands it
// Init and offers it every message first.
type animated interface {
	Init() tea.Cmd
	// Update reports whether msg was the backdrop's own.
	Update(msg tea.Msg) (tea.Cmd, bool)
}

// dimmer is the "dim" backdrop. Init starts loading the window's panes in
// the background, so the card's first frame is never held up; until they
// arrive the margin stays see-through. Then the dimming fades in over
// backdrop.FadeDuration. A resize loads the panes again (they reflow), and
// a failed load just leaves the margin see-through.
type dimmer struct {
	load    func(context.Context) (*backdrop.Backdrop, error)
	b       *backdrop.Backdrop
	frame   int  // fade frame shown, 1…backdrop.FadeFrames once loaded
	w, h    int  // the window size last seen
	loadW   int  // the window width the panes were loaded at
	loadH   int  // and its height
	loading bool // a load is running
}

type dimLoadedMsg struct {
	d    *dimmer
	b    *backdrop.Backdrop
	w, h int
	err  error
}

type dimFrameMsg struct{ d *dimmer }

// dimInRex returns the dim backdrop of the tool running in $REX_BLOCK of
// $REX_SESSION, or nil outside Rex.
func dimInRex() *dimmer {
	session, block := os.Getenv("REX_SESSION"), os.Getenv("REX_BLOCK")
	if session == "" || block == "" {
		return nil
	}
	return &dimmer{load: func(ctx context.Context) (*backdrop.Backdrop, error) {
		client, err := rex.New()
		if err != nil {
			return nil, err
		}
		return backdrop.Load(ctx, client, rextheme.Load(), session, block)
	}}
}

func (d *dimmer) Init() tea.Cmd { return d.fetch() }

func (d *dimmer) fetch() tea.Cmd {
	d.loading = true
	w, h := d.w, d.h
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dimTimeout)
		defer cancel()
		b, err := d.load(ctx)
		return dimLoadedMsg{d: d, b: b, w: w, h: h, err: err}
	}
}

func (d *dimmer) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.w, d.h = msg.Width, msg.Height
		return d.reload(), false // the tool needs the size too
	case dimLoadedMsg:
		if msg.d != d {
			return nil, false
		}
		d.loading = false
		if msg.err != nil || msg.b == nil {
			return nil, true
		}
		d.b, d.loadW, d.loadH = msg.b, msg.w, msg.h
		if msg.w == 0 { // loaded before the first size: read at the size it opened with
			d.loadW, d.loadH = d.w, d.h
		}
		if d.frame > 0 {
			return d.reload(), true // already faded in
		}
		d.frame = 1
		return tea.Batch(d.tick(), d.reload()), true
	case dimFrameMsg:
		if msg.d != d {
			return nil, false
		}
		if d.frame++; d.frame < backdrop.FadeFrames {
			return d.tick(), true
		}
		return nil, true
	}
	return nil, false
}

// reload loads the panes again when the window changed size since they
// were read (they reflow), unless a load is running.
func (d *dimmer) reload() tea.Cmd {
	if d.b == nil || d.loading || d.w == 0 || (d.w == d.loadW && d.h == d.loadH) {
		return nil
	}
	return d.fetch()
}

func (d *dimmer) tick() tea.Cmd {
	return tea.Tick(backdrop.FadeDuration/backdrop.FadeFrames, func(time.Time) tea.Msg { return dimFrameMsg{d} })
}

// draw is the stage's Backdrop: nothing (see-through) until the panes are
// loaded, then the panes at the fade's current strength.
func (d *dimmer) draw(w, h int, card ui.Silhouette) string {
	if d.b == nil {
		return ""
	}
	return d.b.Frame(w, h, card, backdrop.Fade(d.frame))
}

// backdropFlag is the --backdrop flag: "dim" sets Card.Dim.
type backdropFlag struct{ c *Card }

func (f backdropFlag) String() string {
	if f.c != nil && f.c.Dim {
		return BackdropDim
	}
	return BackdropClear
}

func (f backdropFlag) Set(s string) error {
	switch s {
	case BackdropDim, BackdropClear:
		f.c.Dim = s == BackdropDim
		return nil
	}
	return fmt.Errorf("want %s or %s", BackdropDim, BackdropClear)
}
