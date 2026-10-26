package uitest

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKey(t *testing.T) {
	tests := map[string]tea.KeyPressMsg{
		"enter":     {Code: tea.KeyEnter},
		"alt+enter": {Code: tea.KeyEnter, Mod: tea.ModAlt},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"ctrl+/":    {Code: '/', Mod: tea.ModCtrl},
		"a":         {Code: 'a', Text: "a"},
		"?":         {Code: '?', Text: "?"},
		"+":         {Code: '+', Text: "+"},
		"漢":         {Code: '漢', Text: "漢"},
		"shift+a":   {Code: 'a', ShiftedCode: 'A', Mod: tea.ModShift, Text: "A"},
		"space":     {Code: tea.KeySpace, Text: " "},
	}
	for spec, want := range tests {
		if got := Key(t, spec); got != want {
			t.Errorf("Key(%q) = %+v, want %+v", spec, got, want)
		}
	}
	if got := Key(t, "ctrl+r").String(); got != "ctrl+r" {
		t.Errorf("ctrl+r reads back as %q", got)
	}
}

func TestType(t *testing.T) {
	msgs := Type("hé")
	if len(msgs) != 2 || msgs[1].(tea.KeyPressMsg).Text != "é" {
		t.Errorf("Type = %+v", msgs)
	}
}
