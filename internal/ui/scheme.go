package ui

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// DetectTimeout is how long Detect waits for the terminal to report its
// colour scheme before settling on the dark default.
const DetectTimeout = 120 * time.Millisecond

// ThemeEnv overrides detection: "light" or "dark".
const ThemeEnv = "REXKIT_THEME"

// schemeTimeoutMsg ends the wait for a colour scheme report.
type schemeTimeoutMsg struct{}

// Scheme tracks the terminal's light/dark preference. Rex answers the
// colour-scheme query (CSI ?996n → CSI ?997;1n dark, ;2n light) and, with mode
// 2031 on, reports changes; OSC 10/11 colour queries get no reply in unthemed
// blocks, so they are not used.
//
// Typical use in a Bubble Tea v2 model:
//
//	func (m model) Init() tea.Cmd { return m.scheme.Init() }
//
//	func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
//		if m.scheme.Update(msg) {
//			m.theme = m.scheme.Theme()
//		}
//		...
//	}
//
// Until Settled reports true a model may hold its first frame back, so the
// user never sees the wrong palette flash.
type Scheme struct {
	dark    bool
	settled bool
	forced  bool
}

// NewScheme starts with the REXKIT_THEME override if set, otherwise the dark
// default (unsettled until the terminal answers or DetectTimeout passes).
func NewScheme() Scheme {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ThemeEnv))) {
	case "light":
		return Scheme{dark: false, settled: true, forced: true}
	case "dark":
		return Scheme{dark: true, settled: true, forced: true}
	}
	return Scheme{dark: true}
}

// FixedScheme is a scheme settled on dark or light that never asks the
// terminal, for rendering a tool where there is none (rexkit snap).
func FixedScheme(dark bool) Scheme {
	return Scheme{dark: dark, settled: true, forced: true}
}

// Init asks the terminal for its scheme and subscribes to changes. It returns
// nil when the scheme is forced by the environment.
func (s Scheme) Init() tea.Cmd {
	if s.forced {
		return nil
	}
	return Detect()
}

// Detect is the command behind Scheme.Init: it sends the query, enables
// change notifications (mode 2031) and starts the timeout.
func Detect() tea.Cmd {
	return tea.Batch(
		tea.Raw(ansi.SetModeLightDark+ansi.RequestLightDarkReport),
		tea.Tick(DetectTimeout, func(time.Time) tea.Msg { return schemeTimeoutMsg{} }),
	)
}

// Update consumes scheme reports and the detection timeout. It reports true
// when the caller should rebuild its theme (or show its first frame).
func (s *Scheme) Update(msg tea.Msg) bool {
	if s.forced {
		return false
	}
	switch msg.(type) {
	case uv.DarkColorSchemeEvent:
		return s.set(true)
	case uv.LightColorSchemeEvent:
		return s.set(false)
	case schemeTimeoutMsg:
		if !s.settled {
			s.settled = true
			return true
		}
	}
	return false
}

func (s *Scheme) set(dark bool) bool {
	changed := !s.settled || s.dark != dark
	s.dark, s.settled = dark, true
	return changed
}

// Dark reports the current preference.
func (s Scheme) Dark() bool { return s.dark }

// Settled reports whether the scheme is known (answered, forced or timed out).
func (s Scheme) Settled() bool { return s.settled }

// Theme returns the theme for the current preference.
func (s Scheme) Theme() Theme { return NewTheme(s.dark) }
