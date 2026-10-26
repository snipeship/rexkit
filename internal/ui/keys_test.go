package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestKeyLabel(t *testing.T) {
	tests := map[string]string{
		"ctrl+shift+enter": "⌃⇧⏎",
		"enter":            "⏎",
		"esc":              "⎋",
		"escape":           "⎋",
		"tab":              "⇥",
		"shift+tab":        "⇧⇥",
		"backspace":        "⌫",
		"ctrl+r":           "⌃R",
		"alt+.":            "⌥.",
		"ctrl+/":           "⌃/",
		"cmd+k":            "⌘K",
		"ctrl+shift+space": "⌃⇧Space",
		"up":               "↑",
		"pgdown":           "PgDn",
		"f1":               "F1",
		"?":                "?",
		"q":                "q",
		"ctrl+p e":         "⌃P e",
		"ctrl+b shift+c":   "⌃B ⇧C",
		"shift+p":          "⇧P",
		"shift+r":          "⇧R",
		"shift+/":          "?",
		"shift+5":          "%",
		"shift+'":          "\"",
		"shift+=":          "+",
		"shift+\\":         "|",
		"ctrl+o shift+/":   "⌃O ?",
		"alt+shift+=":      "⌥⇧=",
		"ctrl+shift+/":     "⌃⇧/",
		"ctrl++":           "⌃+",
		"+":                "+",
		"↑↓":               "↑↓",
	}
	for in, want := range tests {
		if got := KeyLabel(in); got != want {
			t.Errorf("KeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapLabel(t *testing.T) {
	tests := map[string]string{
		"enter":     "enter",
		"esc":       "esc",
		"shift+tab": "shift tab",
		"ctrl+/":    "ctrl /",
		"alt+enter": "alt enter",
		"backspace": "bksp",
		"pgdown":    "pgdn",
		"up":        "↑",
		"?":         "?",
		"shift+/":   "?",
		"ctrl+r":    "ctrl r",
		"ctrl+p e":  "ctrl p  e",
		"f5":        "F5",
	}
	for in, want := range tests {
		if got := CapLabel(in); got != want {
			t.Errorf("CapLabel(%q) = %q, want %q", in, got, want)
		}
	}
	th := NewTheme(true)
	if got := th.Keycap(CapLabel("ctrl+p e")).String(); got != "▐ctrl p▌ ▐e▌" {
		t.Errorf("a chord's steps get a cap each: %q", got)
	}
	if got := th.Keycaps([]string{"up", "k"}).String(); got != "▐↑▌  ▐k▌" {
		t.Errorf("alternatives: %q", got)
	}
}

func TestHints(t *testing.T) {
	th := NewTheme(true)
	hints := []Hint{{Key: "enter", Label: "open", Primary: true}, {Key: "tab", Label: "filter"}, {Key: "ctrl+/", Label: "preview"}, {Key: "?", Label: "keys"}}
	tests := []struct {
		w    int
		want string
	}{
		{80, "▐enter▌ open   ▐tab▌ filter   ▐ctrl /▌ preview   ▐?▌ keys"},
		{30, "▐enter▌ open   ▐?▌ keys"},
		{12, "▐enter▌ open"},
		{5, ""},
	}
	for _, tt := range tests {
		if got := th.Hints(hints, tt.w).String(); got != tt.want {
			t.Errorf("Hints(%d) = %q, want %q", tt.w, got, tt.want)
		}
	}
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		at   time.Time
		want string
	}{
		{ago: 2 * time.Second, want: "now"},
		{ago: -time.Minute, want: "now"},
		{ago: 12 * time.Second, want: "12s"},
		{ago: 3 * time.Minute, want: "3m"},
		{ago: 5 * time.Hour, want: "5h"},
		{ago: 2 * 24 * time.Hour, want: "2d"},
		{at: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), want: "Oct 3"},
		{at: time.Date(2024, 1, 3, 9, 0, 0, 0, time.UTC), want: "2024"},
	}
	for _, tt := range tests {
		at := tt.at
		if at.IsZero() {
			at = now.Add(-tt.ago)
		}
		if got := RelTimeAt(at, now); got != tt.want {
			t.Errorf("RelTimeAt(%v) = %q, want %q", at, got, tt.want)
		}
	}
	if RelTimeAt(time.Time{}, now) != "" {
		t.Error("zero time should render empty")
	}
}

func TestPlural(t *testing.T) {
	if Plural(1, "item") != "1 item" || Plural(0, "item") != "0 items" {
		t.Error("Plural")
	}
	if got := Superscript("12"); got != "¹²" {
		t.Errorf("Superscript = %q", got)
	}
}

func TestCapitalize(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		"window limit reached":           "Window limit reached",
		"écran":                          "Écran",
		"⌃E edits files · choose a file": "⌃E edits files · choose a file",
		"\xe2bad":                        "\xe2bad",
	} {
		if got := Capitalize(in); got != want {
			t.Errorf("Capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEarlyEnterSince(t *testing.T) {
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ctrlJ := tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	if got := EarlyEnterSince(start, start.Add(EarlyKeys-time.Millisecond), ctrlJ); got.Code != tea.KeyEnter {
		t.Errorf("an early ⌃J is %v, want ⏎", got)
	}
	if got := EarlyEnterSince(start, start.Add(EarlyKeys), ctrlJ); got != ctrlJ {
		t.Errorf("a later ⌃J is %v, want ⌃J itself", got)
	}
	other := tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	if got := EarlyEnterSince(start, start, other); got != other {
		t.Errorf("an early ⌃K became %v", got)
	}
}
