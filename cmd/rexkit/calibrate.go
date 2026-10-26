package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/backdrop"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/internal/ui"
)

// calibrate draws the window's own panes over the whole window for a while,
// undimmed by default. Run in an unfocused whole-window float, a screenshot
// with and one without it show whether the backdrop's insets
// (internal/backdrop/geometry.go) put every pane's text where Rex draws it:
// where they do, nothing moves.
func calibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	session := fs.String("session", os.Getenv("REX_SESSION"), "Rex session `ID` the float runs in")
	show := fs.Duration("for", 20*time.Second, "how long to show the copy")
	strength := fs.Float64("strength", 0, "how far to dim the copy, 0 to 1")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	block := os.Getenv("REX_BLOCK")
	if *session == "" || block == "" {
		return errors.New("calibrate: run it in a Rex float (it reads $REX_BLOCK)")
	}
	api, err := rex.New()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := backdrop.Load(ctx, api, rextheme.Load(), *session, block)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(&calibration{b: b, strength: *strength, show: *show}).Run()
	return err
}

type calibration struct {
	b        *backdrop.Backdrop
	strength float64
	show     time.Duration
	w, h     int
}

type calibrationDoneMsg struct{}

func (c *calibration) Init() tea.Cmd {
	return tea.Tick(c.show, func(time.Time) tea.Msg { return calibrationDoneMsg{} })
}

func (c *calibration) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.w, c.h = msg.Width, msg.Height
	case calibrationDoneMsg, tea.KeyPressMsg, tea.MouseClickMsg:
		return c, tea.Quit
	}
	return c, nil
}

func (c *calibration) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if c.w > 0 && c.h > 0 {
		v.Content = c.b.Frame(c.w, c.h, ui.Silhouette{}, c.strength)
	}
	return v
}
