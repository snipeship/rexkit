// Package uitest holds the test helpers every tool's UI tests share: golden
// files (with one -update flag) and key messages built from the config key
// notation. Import it from _test.go files only. A test binary that imports it
// must not define its own -update flag: the flag package panics on the
// duplicate.
package uitest

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Update reports -update: rewrite golden files instead of comparing.
var Update = flag.Bool("update", false, "rewrite testdata/*.golden")

// Golden compares got with testdata/name.golden (ANSI included, so colours
// are covered too) and rewrites it with -update. A failure names the first
// differing line, raw and as plain text.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *Update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if got == string(want) {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("%s differs at line %d:\n got %q\nwant %q\n(plain)\n got %s\nwant %s",
				path, i+1, g, w, ansi.Strip(g), ansi.Strip(w))
		}
	}
}

var named = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
	"delete": tea.KeyDelete, "space": tea.KeySpace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"f1": tea.KeyF1,
}

var mods = map[string]tea.KeyMod{"ctrl": tea.ModCtrl, "alt": tea.ModAlt, "shift": tea.ModShift, "super": tea.ModSuper}

// Key builds the key press for a spec in config notation: "enter",
// "ctrl+r", "alt+enter", "shift+tab", "a", "?", "+". A spec that is neither
// a known key name nor a single character fails the test, instead of
// silently typing its first letter.
func Key(t testing.TB, spec string) tea.KeyPressMsg {
	t.Helper()
	parts := strings.Split(spec, "+")
	if strings.HasSuffix(spec, "++") || spec == "+" { // the "+" key itself
		parts = append(parts[:len(parts)-2], "+")
	}
	var msg tea.KeyPressMsg
	for _, m := range parts[:len(parts)-1] {
		mod, ok := mods[m]
		if !ok {
			t.Fatalf("uitest.Key(%q): unknown modifier %q", spec, m)
		}
		msg.Mod |= mod
	}
	key := parts[len(parts)-1]
	if code, ok := named[key]; ok {
		msg.Code = code
		if code == tea.KeySpace && msg.Mod&^tea.ModShift == 0 {
			msg.Text = " "
		}
		return msg
	}
	r, n := utf8.DecodeRuneInString(key)
	if n == 0 || n != len(key) {
		t.Fatalf("uitest.Key(%q): %q is not a key name or a single character", spec, key)
	}
	msg.Code = r
	switch msg.Mod {
	case 0:
		msg.Text = key
	case tea.ModShift:
		msg.ShiftedCode = unicode.ToUpper(r)
		msg.Text = string(msg.ShiftedCode)
	}
	return msg
}

// Type turns text into one key press per rune.
func Type(text string) []tea.Msg {
	var out []tea.Msg
	for _, r := range text {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}
