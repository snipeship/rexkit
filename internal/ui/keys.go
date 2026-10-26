package ui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var modifierGlyphs = map[string]string{
	"ctrl":    "⌃",
	"control": "⌃",
	"alt":     "⌥",
	"opt":     "⌥",
	"option":  "⌥",
	"shift":   "⇧",
	"cmd":     "⌘",
	"command": "⌘",
	"super":   "⌘",
	"meta":    "⌘",
}

var keyGlyphs = map[string]string{
	"enter":     "⏎",
	"return":    "⏎",
	"esc":       "⎋",
	"escape":    "⎋",
	"tab":       "⇥",
	"backspace": "⌫",
	"delete":    "⌦",
	"del":       "⌦",
	"up":        "↑",
	"down":      "↓",
	"left":      "←",
	"right":     "→",
	"space":     "Space",
	"pgup":      "PgUp",
	"pageup":    "PgUp",
	"pgdown":    "PgDn",
	"pgdn":      "PgDn",
	"pagedown":  "PgDn",
	"home":      "Home",
	"end":       "End",
	"insert":    "Ins",
}

// shifted maps a US-layout key to the character shift+key types. Rex has no
// names for these characters ("?" is not a key; configs bind "shift+/"), but
// users know the binding by what it types.
var shifted = map[string]string{
	"1": "!", "2": "@", "3": "#", "4": "$", "5": "%", "6": "^", "7": "&", "8": "*", "9": "(", "0": ")",
	"-": "_", "=": "+", "[": "{", "]": "}", "\\": "|", ";": ":", "'": "\"", ",": "<", ".": ">", "/": "?", "`": "~",
}

// KeyLabel turns a config key string into the glyph notation used everywhere
// in the UI: "ctrl+shift+enter" → "⌃⇧⏎", "alt+." → "⌥.", "ctrl+p e" → "⌃P e",
// "shift+/" → "?", "ctrl+o shift+/" → "⌃O ?". A letter is upper-case only
// with a modifier ("ctrl+r" → "⌃R", "shift+p" → "⇧P"); a bare key, also inside
// a chord, stays as typed, so the same key reads the same everywhere. Shift
// alone on punctuation or a digit shows the character it types.
func KeyLabel(key string) string {
	chord := strings.Fields(key)
	labels := make([]string, len(chord))
	for i, k := range chord {
		labels[i] = keyLabel(k)
	}
	return strings.Join(labels, " ")
}

func keyLabel(k string) string {
	parts := strings.Split(k, "+")
	if strings.HasSuffix(k, "++") || k == "+" { // the "+" key itself
		parts = append(parts[:len(parts)-2], "+")
	}
	mods, key := parts[:len(parts)-1], parts[len(parts)-1]
	if len(mods) == 1 && strings.EqualFold(mods[0], "shift") {
		if c, ok := shifted[key]; ok {
			return c
		}
	}
	var b strings.Builder
	for _, mod := range mods {
		if g, ok := modifierGlyphs[strings.ToLower(mod)]; ok {
			b.WriteString(g)
		} else {
			b.WriteString(mod)
		}
	}
	switch g, ok := keyGlyphs[strings.ToLower(key)]; {
	case ok:
		b.WriteString(g)
	case len(key) > 1 && (key[0] == 'f' || key[0] == 'F') && isDigits(key[1:]):
		b.WriteString(strings.ToUpper(key))
	case utf8.RuneCountInString(key) == 1 && len(mods) > 0:
		b.WriteString(strings.ToUpper(key))
	default:
		b.WriteString(key)
	}
	return b.String()
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// capWords spell keys on keycaps: the font (Berkeley Mono, and most coding
// fonts) has no ⌃ ⏎ ⇥ ⌫ ⌥ ⇧, and the fallback font draws them at about 60 %
// size, which reads poorly on a cap. Arrows and printable keys keep their
// glyphs.
var capWords = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "alt": "alt", "opt": "alt", "option": "alt",
	"shift": "shift", "cmd": "cmd", "command": "cmd", "super": "cmd", "meta": "cmd",
	"enter": "enter", "return": "enter", "esc": "esc", "escape": "esc", "tab": "tab",
	"backspace": "bksp", "delete": "del", "del": "del", "space": "space",
	"pgup": "pgup", "pageup": "pgup", "pgdown": "pgdn", "pgdn": "pgdn", "pagedown": "pgdn",
	"home": "home", "end": "end", "insert": "ins",
}

// capStep separates the steps of a chord in a CapLabel: each step gets a
// cap of its own (Keycap).
const capStep = "  "

// CapLabel is the label of a keycap: a config key string with modifiers and
// named keys spelled as words, "ctrl+/" → "ctrl /", "enter" → "enter",
// "shift+tab" → "shift tab", "backspace" → "bksp". Arrows, printable keys and
// shifted punctuation are kept as KeyLabel writes them ("up" → "↑",
// "shift+/" → "?"). The steps of a chord ("ctrl+p e") are two spaces apart,
// which Keycap draws as separate caps. A ready label ("double-click") is
// kept.
func CapLabel(key string) string {
	chord := strings.Fields(key)
	steps := make([]string, len(chord))
	for i, k := range chord {
		parts := strings.Split(k, "+")
		if strings.HasSuffix(k, "++") || k == "+" { // the "+" key itself
			parts = append(parts[:len(parts)-2], "+")
		}
		mods, last := parts[:len(parts)-1], parts[len(parts)-1]
		if len(mods) == 1 && strings.EqualFold(mods[0], "shift") {
			if c, ok := shifted[last]; ok {
				steps[i] = c
				continue
			}
		}
		words := make([]string, 0, len(parts))
		for _, m := range mods {
			if w, ok := capWords[strings.ToLower(m)]; ok {
				m = w
			}
			words = append(words, m)
		}
		if w, ok := capWords[strings.ToLower(last)]; ok {
			words = append(words, w)
		} else {
			words = append(words, keyLabel(last))
		}
		steps[i] = strings.Join(words, " ")
	}
	return strings.Join(steps, capStep)
}

// Keycap draws a key label (CapLabel) as Tag shapes on the theme's KeyCap
// fill, "▐ctrl /▌": the half-block ends round the key and give it air. A
// chord's steps get a cap each, one space apart.
func (t Theme) Keycap(label string) Line {
	var out Line
	for i, step := range strings.Split(label, capStep) {
		if i > 0 {
			out = append(out, Space(1))
		}
		out = append(out, Pill(Styled(step, t.Key), t.C.KeyCap)...)
	}
	return out
}

// altGap is the air between the caps of alternative keys.
const altGap = 2

// Keycaps draws alternative keys (config notation) as separate keycaps,
// two spaces apart: "▐↑▌  ▐ctrl k▌".
func (t Theme) Keycaps(keys []string) Line {
	var out Line
	for i, k := range keys {
		if i > 0 {
			out = append(out, Space(altGap))
		}
		out = append(out, t.Keycap(CapLabel(k))...)
	}
	return out
}

// Hint is one key hint: a keycap and what the key does, "▐enter▌ go".
type Hint struct {
	Key   string // config notation ("ctrl+e") or a ready label ("double-click")
	Label string // lower case: "go", "new tab"
	// Outcome says what the key will act on, after the label: "to
	// studio", "in". It is shown whole: when the tray is short it goes
	// after every other hint has, leaving the verb.
	Outcome string
	// Path ends the outcome with a path, "~/Coding/rexkit": when the tray
	// is short it is middle-truncated first, before any hint drops, and
	// goes with the outcome.
	Path string
	// Primary is the hint of the key that does the obvious thing (enter):
	// its label is in the primary text colour, and it is never dropped.
	Primary bool
}

// hintGap is the air between two hints.
const hintGap = 3

// hint draws one hint with its path cut to path cells (-1 for all of it),
// or without its outcome and path when bare.
func (t Theme) hint(h Hint, path int, bare bool) Line {
	label := t.Muted
	if h.Primary {
		label = t.Text
	}
	out := t.Keycap(CapLabel(h.Key)).Append(Span{" " + h.Label, label})
	if bare {
		return out
	}
	o := h.Outcome
	if h.Path != "" {
		p := h.Path
		if path >= 0 {
			p = TruncateMiddle(p, path)
		}
		o = strings.TrimPrefix(o+" "+p, " ")
	}
	if o != "" {
		out = append(out, Span{" " + o, label})
	}
	return out
}

// keepHint reports whether a hint survives every drop: the primary one,
// and "? keys", which is how every other key is found.
func keepHint(h Hint) bool { return h.Primary || h.Key == "?" || h.Key == "shift+/" }

// Hints renders key hints as "▐enter▌ go   ▐tab▌ filter   ▐?▌ keys" within w
// cells: keycaps on the theme's KeyCap fill (InTray for the tray), labels in
// the secondary colour (the primary one in the primary colour), three cells
// apart. When they do not fit, in order: a hint's Path is middle-truncated,
// hints drop from the end (never the primary one or "? keys"), and the
// outcomes go, leaving the verbs. A name in an outcome ("to 研究ノート")
// is never cut. List the most important first.
func (t Theme) Hints(hints []Hint, w int) Line {
	shown := make([]bool, len(hints))
	for i := range shown {
		shown[i] = true
	}
	path, bare := -1, false
	width := func() int {
		n, first := 0, true
		for i, h := range hints {
			if !shown[i] {
				continue
			}
			if !first {
				n += hintGap
			}
			n += t.hint(h, path, bare).Width()
			first = false
		}
		return n
	}
	// fitPath cuts the path just enough for the shown hints to fit.
	fitPath := func() {
		path = -1
		over := width() - w
		if over <= 0 {
			return
		}
		for _, h := range hints {
			if h.Path != "" {
				path = max(minPath, ansi.StringWidth(h.Path)-over)
				return
			}
		}
	}
	fitPath()
	for i := len(hints) - 1; i >= 0 && width() > w; i-- {
		if !keepHint(hints[i]) {
			shown[i] = false
			fitPath()
		}
	}
	if width() > w {
		bare = true
	}
	var out Line
	for i, h := range hints {
		if !shown[i] {
			continue
		}
		hl := t.hint(h, path, bare)
		gap := 0
		if len(out) > 0 {
			gap = hintGap
		}
		if out.Width()+gap+hl.Width() > w {
			break
		}
		if gap > 0 {
			out = append(out, Space(gap))
		}
		out = append(out, hl...)
	}
	return out
}

// minPath is the narrowest a hint's Path is cut to before it is
// dropped: enough for "~/…/docs".
const minPath = 8

// EarlyEnter turns a line feed (⌃J) back into ⏎. A ⏎ that reaches a tool's
// terminal before the tool made it raw is translated to a line feed by the
// terminal; tools read keys through EarlyEnter so that it still commits.
func EarlyEnter(msg tea.KeyPressMsg) tea.KeyPressMsg {
	if msg.Code == 'j' && msg.Mod == tea.ModCtrl {
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return msg
}

// EarlyKeys is how long after a tool starts a ⌃J still reads as ⏎. A CR that
// reached the pty before popup.HoldKeys made the tty raw (typed, or written
// by the Lua handshake) arrives as LF, i.e. ⌃J. Later the tty is raw, so a
// ⌃J is the user's own: the list tools move down with it (⌃K moves up).
const EarlyKeys = 250 * time.Millisecond

// EarlyEnterSince is EarlyEnter for the first EarlyKeys after start, and the
// key unchanged after that.
func EarlyEnterSince(start, now time.Time, msg tea.KeyPressMsg) tea.KeyPressMsg {
	if now.Sub(start) < EarlyKeys {
		return EarlyEnter(msg)
	}
	return msg
}
