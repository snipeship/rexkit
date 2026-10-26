package ui

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestSchemeUpdate(t *testing.T) {
	t.Setenv(ThemeEnv, "")
	tests := []struct {
		name    string
		msgs    []tea.Msg
		dark    bool
		settled bool
		changes int
	}{
		{"unanswered", nil, true, false, 0},
		{"light reply", []tea.Msg{uv.LightColorSchemeEvent{}}, false, true, 1},
		{"dark reply", []tea.Msg{uv.DarkColorSchemeEvent{}}, true, true, 1},
		{"timeout", []tea.Msg{schemeTimeoutMsg{}}, true, true, 1},
		{"reply then timeout", []tea.Msg{uv.LightColorSchemeEvent{}, schemeTimeoutMsg{}}, false, true, 1},
		{"late reply wins", []tea.Msg{schemeTimeoutMsg{}, uv.LightColorSchemeEvent{}}, false, true, 2},
		{"live switch (mode 2031)", []tea.Msg{uv.DarkColorSchemeEvent{}, uv.LightColorSchemeEvent{}, uv.LightColorSchemeEvent{}}, false, true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScheme()
			changes := 0
			for _, m := range tt.msgs {
				if s.Update(m) {
					changes++
				}
			}
			if s.Dark() != tt.dark || s.Settled() != tt.settled || changes != tt.changes {
				t.Errorf("dark=%v settled=%v changes=%d, want %v %v %d", s.Dark(), s.Settled(), changes, tt.dark, tt.settled, tt.changes)
			}
		})
	}
}

func TestSchemeEnvOverride(t *testing.T) {
	for env, dark := range map[string]bool{"light": false, "dark": true, " Light ": false} {
		t.Setenv(ThemeEnv, env)
		s := NewScheme()
		if s.Dark() != dark || !s.Settled() || s.Init() != nil {
			t.Errorf("%q: dark=%v settled=%v", env, s.Dark(), s.Settled())
		}
		if s.Update(uv.DarkColorSchemeEvent{}) || s.Update(uv.LightColorSchemeEvent{}) || s.Dark() != dark {
			t.Errorf("%q: terminal reports must not override the environment", env)
		}
	}
}

func TestFixedScheme(t *testing.T) {
	t.Setenv(ThemeEnv, "dark")
	s := FixedScheme(false)
	if s.Dark() || !s.Settled() || s.Init() != nil || s.Theme().Dark {
		t.Errorf("FixedScheme(false): dark=%v settled=%v", s.Dark(), s.Settled())
	}
	if s.Update(uv.DarkColorSchemeEvent{}) || s.Dark() {
		t.Error("a terminal report overrode a fixed scheme")
	}
}

// schemeModel is a minimal program that waits for the colour scheme.
type schemeModel struct{ scheme Scheme }

func (m schemeModel) Init() tea.Cmd { return m.scheme.Init() }

func (m schemeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scheme.Update(msg) && m.scheme.Settled() {
		return m, tea.Quit
	}
	return m, nil
}

func (m schemeModel) View() tea.View { return tea.NewView("") }

// TestSchemeThroughBubbleTea runs a real Bubble Tea program: it must send the
// query and turn the terminal's reply into a light theme.
func TestSchemeThroughBubbleTea(t *testing.T) {
	t.Setenv(ThemeEnv, "")
	tests := []struct {
		name  string
		reply string
		dark  bool
	}{
		{"light", "\x1b[?997;2n", false},
		{"dark", "\x1b[?997;1n", true},
		{"no reply", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			in, inW := io.Pipe()
			var out bytes.Buffer
			p := tea.NewProgram(schemeModel{NewScheme()},
				tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(&out), tea.WithWindowSize(80, 24))
			go func() {
				if tt.reply != "" {
					time.Sleep(20 * time.Millisecond)
					inW.Write([]byte(tt.reply))
				}
			}()
			final, err := p.Run()
			inW.Close()
			if err != nil {
				t.Fatal(err)
			}
			got := final.(schemeModel).scheme
			if got.Dark() != tt.dark || !got.Settled() {
				t.Errorf("dark=%v settled=%v, want dark=%v", got.Dark(), got.Settled(), tt.dark)
			}
			if !strings.Contains(out.String(), ansi.RequestLightDarkReport) {
				t.Errorf("the program never asked for the colour scheme: %q", out.String())
			}
		})
	}
}

func TestThemeTokens(t *testing.T) {
	hex := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	// Fg is Rex's content token; SelBg is accentDim over the card.
	dark, light := NewTheme(true), NewTheme(false)
	if hex(dark.C.Fg) != "e6edf3" || hex(light.C.Fg) != "1f2328" {
		t.Errorf("fg tokens: %s / %s", hex(dark.C.Fg), hex(light.C.Fg))
	}
	if hex(dark.C.SelBg) != "212f43" || hex(light.C.SelBg) != "dce9f8" {
		t.Errorf("selBg tokens: %s / %s", hex(dark.C.SelBg), hex(light.C.SelBg))
	}
	if dark.CodeStyle() != "github-dark" || light.CodeStyle() != "github" {
		t.Error("code styles")
	}
}
