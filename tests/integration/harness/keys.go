//go:build integration

package harness

import (
	"fmt"
	"strings"
	"unicode"
)

// KeyEvent is a client.key event, as the app would send it.
type KeyEvent struct {
	Key                string `json:"key"`
	Mods               string `json:"mods"`
	ConsumedMods       string `json:"consumed_mods"`
	Text               string `json:"text"`
	UnshiftedCodepoint int    `json:"unshifted_codepoint"`
	Composing          bool   `json:"composing"`
	Action             string `json:"action"`
}

type namedKey struct {
	code      string
	text      string
	codepoint int
}

var namedKeys = map[string]namedKey{
	"enter":     {"enter", "\r", 13},
	"escape":    {"escape", "\x1b", 27},
	"esc":       {"escape", "\x1b", 27},
	"tab":       {"tab", "\t", 9},
	"space":     {"space", " ", 32},
	"backspace": {"backspace", "\x7f", 127},
	"delete":    {"delete", "", 0},
	"up":        {"arrow_up", "", 0},
	"down":      {"arrow_down", "", 0},
	"left":      {"arrow_left", "", 0},
	"right":     {"arrow_right", "", 0},
	"home":      {"home", "", 0},
	"end":       {"end", "", 0},
	"pageup":    {"page_up", "", 0},
	"pagedown":  {"page_down", "", 0},
}

var punctuation = map[rune]string{
	'`': "backquote", '\\': "backslash", '[': "bracket_left", ']': "bracket_right", ',': "comma",
	'=': "equal", '-': "minus", '.': "period", '\'': "quote", ';': "semicolon", '/': "slash",
}

// modOrder is the order Rex writes modifiers in.
var modOrder = []string{"shift", "ctrl", "alt", "super"}

var modAliases = map[string]string{
	"shift": "shift", "ctrl": "ctrl", "control": "ctrl", "alt": "alt", "opt": "alt", "option": "alt",
	"cmd": "super", "command": "super", "super": "super", "meta": "super",
}

// Key parses a binding-style key such as "enter", "a", "ctrl+c",
// "shift+tab" or "ctrl+shift+space" into the event the app would send.
func Key(spec string) (KeyEvent, error) {
	parts := strings.Split(strings.ToLower(spec), "+")
	name := parts[len(parts)-1]
	if name == "" && len(parts) > 1 { // "ctrl++"
		name = "+"
	}
	held := map[string]bool{}
	for _, m := range parts[:len(parts)-1] {
		mod, ok := modAliases[m]
		if !ok {
			return KeyEvent{}, fmt.Errorf("harness: unknown modifier %q in %q", m, spec)
		}
		held[mod] = true
	}
	ev := KeyEvent{Action: "press"}
	runes := []rune(name)
	switch nk, named := namedKeys[name]; {
	case named:
		ev.Key, ev.Text, ev.UnshiftedCodepoint = nk.code, nk.text, nk.codepoint
	case len(name) >= 2 && name[0] == 'f' && strings.Trim(name[1:], "0123456789") == "":
		ev.Key = name
	case len(runes) == 1 && runes[0] >= 'a' && runes[0] <= 'z':
		ev.Key, ev.Text, ev.UnshiftedCodepoint = "key_"+name, name, int(runes[0])
		if held["shift"] {
			ev.Text, ev.ConsumedMods = strings.ToUpper(name), "shift"
		}
	case len(runes) == 1 && unicode.IsDigit(runes[0]):
		ev.Key, ev.Text, ev.UnshiftedCodepoint = "digit_"+name, name, int(runes[0])
	case len(runes) == 1 && punctuation[runes[0]] != "":
		ev.Key, ev.Text, ev.UnshiftedCodepoint = punctuation[runes[0]], name, int(runes[0])
	default:
		return KeyEvent{}, fmt.Errorf("harness: unknown key %q in %q", name, spec)
	}
	if held["ctrl"] && ev.UnshiftedCodepoint >= 'a' && ev.UnshiftedCodepoint <= 'z' {
		ev.Text = string(rune(ev.UnshiftedCodepoint - 'a' + 1)) // ctrl+c types ETX
	} else if held["ctrl"] || held["alt"] || held["super"] {
		ev.Text = ""
	}
	var mods []string
	for _, m := range modOrder {
		if held[m] {
			mods = append(mods, m)
		}
	}
	ev.Mods = strings.Join(mods, "+")
	return ev, nil
}

// Text turns typed text into one key event per character.
func Text(s string) ([]KeyEvent, error) {
	var events []KeyEvent
	for _, r := range s {
		spec := string(r)
		switch {
		case r == ' ':
			spec = "space"
		case r == '\n' || r == '\r':
			spec = "enter"
		case unicode.IsUpper(r):
			spec = "shift+" + string(unicode.ToLower(r))
		}
		ev, err := Key(spec)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}
