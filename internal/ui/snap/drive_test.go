package snap

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// counter counts key presses and asks for a follow-up message on each.
type counter struct {
	keys, followUps int
	slow            bool // a command that outlives the driver's wait
}

type followUpMsg struct{}

func (c *counter) Init() tea.Cmd {
	return tea.Sequence(func() tea.Msg { return followUpMsg{} }, tea.Batch(func() tea.Msg { return followUpMsg{} }))
}

func (c *counter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyPressMsg:
		c.keys++
		if c.slow {
			return c, tea.Tick(time.Hour, func(time.Time) tea.Msg { return followUpMsg{} })
		}
		return c, func() tea.Msg { return followUpMsg{} }
	case followUpMsg:
		c.followUps++
	}
	return c, nil
}

func (c *counter) View() tea.View { return tea.NewView("") }

func TestDriverRunsCommandsToTheEnd(t *testing.T) {
	c := &counter{}
	d := NewDriver(c)
	if c.followUps != 2 {
		t.Fatalf("Init's sequence and batch delivered %d messages, want 2", c.followUps)
	}
	d.Key("a", "enter")
	d.Type("xy")
	if c.keys != 4 || c.followUps != 6 {
		t.Errorf("keys %d follow-ups %d, want 4 and 6", c.keys, c.followUps)
	}
	slow := &counter{slow: true}
	d = NewDriver(slow)
	d.Wait = 10 * time.Millisecond
	d.Key("a")
	if slow.followUps != 2 {
		t.Errorf("a timer's message was delivered")
	}
}

func TestKey(t *testing.T) {
	tests := map[string]tea.KeyPressMsg{
		"enter":     {Code: tea.KeyEnter},
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"?":         {Code: '?', Text: "?"},
		"+":         {Code: '+', Text: "+"},
		"ctrl++":    {Code: '+', Mod: tea.ModCtrl},
		"漢":         {Code: '漢', Text: "漢"},
	}
	for spec, want := range tests {
		if got := Key(spec); got != want {
			t.Errorf("Key(%q) = %+v, want %+v", spec, got, want)
		}
	}
}
