package snap

import (
	"cmp"
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/panes"
	"rexkit/internal/ui"
)

// Scene is a tool in a named state, ready to be shot: its model and the
// script that brings it into the state after its first WindowSizeMsg.
type Scene struct {
	Model tea.Model
	// Script drives the model into the state: keys, text, messages.
	Script func(*Driver)
	// W and H, when set, are the window size the state is about (a narrow
	// layout), whatever size was asked for.
	W, H int
	// Title is the float's title, which Rex shows in its header.
	Title string
	// Cols and Rows, when set, are the size of the tool's float when Lua
	// sizes it to its content (dialogs and toasts); zero lets the window
	// decide. FitCols, when set, is Cols in a window w columns wide.
	Cols, Rows int
	FitCols    func(w int) int
	// Cleanup, when set, removes what the scene made (sample files) once
	// it has been shot.
	Cleanup func()
	// Panes, when set, are the window the tool opens in (else a made-up
	// one), and Over the pane it covers exactly (hints): alone the tool is
	// shot at that pane's size; with --window it is drawn over it.
	Panes []panes.DemoPane
	Over  int
	// Toast, when set, is shown in the window's top-right corner with the
	// tool, titled as the tool: the toast it leaves once it closes.
	Toast ui.Toast
}

// CmdWait is how long the Driver lets a command run by default. Commands
// that take longer wait on a clock of their own (blinks, spinner ticks, a
// toast's expiry): their messages would only animate the frame, so they are
// dropped.
const CmdWait = 150 * time.Millisecond

// Driver runs a model the way Bubble Tea does, but synchronously and
// without a terminal: each command runs (concurrently with its batch) and
// its message is fed back, until nothing is left to run.
type Driver struct {
	Model tea.Model
	// Drop reports messages not to deliver; nil drops terminal requests
	// and the timers of ui components (see Foreign).
	Drop func(tea.Msg) bool
	// Wait is how long a command may run; zero is CmdWait.
	Wait time.Duration
	quit bool
}

// NewDriver starts m: it runs Init and its commands.
func NewDriver(m tea.Model) *Driver {
	d := &Driver{Model: m}
	d.Run(m.Init())
	return d
}

// Send delivers messages one by one, running the commands each returns.
func (d *Driver) Send(msgs ...tea.Msg) {
	for _, msg := range msgs {
		if d.quit {
			return
		}
		var cmd tea.Cmd
		d.Model, cmd = d.Model.Update(msg)
		d.Run(cmd)
	}
}

// Key presses keys given in Bubble Tea notation: "enter", "ctrl+r",
// "shift+tab", "?", "a".
func (d *Driver) Key(keys ...string) {
	for _, k := range keys {
		d.Send(Key(k))
	}
}

// Type types text, one key press per character.
func (d *Driver) Type(text string) {
	for _, r := range text {
		d.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Quit reports whether the model quit: the tool's float has closed.
func (d *Driver) Quit() bool { return d.quit }

// View returns the model's view content.
func (d *Driver) View() string { return d.Model.View().Content }

// Cursor returns where the model puts the terminal's cursor, or nil.
func (d *Driver) Cursor() *Bar {
	c := d.Model.View().Cursor
	if c == nil {
		return nil
	}
	return &Bar{X: c.X, Y: c.Y, Color: c.Color}
}

// Run runs cmd and everything that follows from it, wave by wave: the
// commands of a wave run at once and their messages are delivered in order.
func (d *Driver) Run(cmd tea.Cmd) {
	wave := []tea.Cmd{cmd}
	for round := 0; len(wave) > 0 && round < 1000; round++ {
		var next []tea.Cmd
		for _, msg := range runAll(wave, cmp.Or(d.Wait, CmdWait)) {
			if d.quit {
				return
			}
			if cmds, ok := batch(msg); ok {
				next = append(next, cmds...)
				continue
			}
			switch msg.(type) {
			case nil:
				continue
			case tea.QuitMsg:
				d.quit = true
				continue
			}
			drop := d.Drop
			if drop == nil {
				drop = Foreign
			}
			if drop(msg) {
				continue
			}
			var c tea.Cmd
			d.Model, c = d.Model.Update(msg)
			next = append(next, c)
		}
		wave = next
	}
}

// batch returns the commands of a tea.Batch or tea.Sequence message.
func batch(msg tea.Msg) ([]tea.Cmd, bool) {
	if b, ok := msg.(tea.BatchMsg); ok {
		return b, true
	}
	// tea.Sequence's message type is unexported; it is a []tea.Cmd too.
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	cmds := make([]tea.Cmd, v.Len())
	for i := range cmds {
		cmds[i] = v.Index(i).Interface().(tea.Cmd)
	}
	return cmds, true
}

// runAll runs commands concurrently and returns their messages in order; a
// command still running after wait yields nil.
func runAll(cmds []tea.Cmd, wait time.Duration) []tea.Msg {
	results := make([]chan tea.Msg, len(cmds))
	for i, c := range cmds {
		results[i] = make(chan tea.Msg, 1)
		if c == nil {
			results[i] <- nil
			continue
		}
		go func() { results[i] <- c() }()
	}
	deadline := time.After(wait)
	out := make([]tea.Msg, len(cmds))
	expired := false
	for i, ch := range results {
		if !expired {
			select {
			case out[i] = <-ch:
				continue
			case <-deadline:
				expired = true
			}
		}
		select {
		case out[i] = <-ch:
		default:
		}
	}
	return out
}

// Foreign reports messages a tool does not need in a snapshot: requests to
// the terminal, and the timers of ui and bubbles components (spinner
// frames, toast expiry, cursor blinks).
func Foreign(msg tea.Msg) bool {
	name := fmt.Sprintf("%T", msg)
	for _, pkg := range []string{"tea.", "ui.", "uv.", "cursor.", "textinput."} {
		if strings.HasPrefix(name, pkg) {
			return true
		}
	}
	return false
}

// keyCodes are the named keys of Key.
var keyCodes = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
	"delete": tea.KeyDelete, "space": tea.KeySpace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
}

// Key builds the key press for a key in Bubble Tea notation.
func Key(spec string) tea.KeyPressMsg {
	var k tea.Key
	name := spec
	if len(spec) > 1 {
		parts := strings.Split(spec, "+")
		if strings.HasSuffix(spec, "++") { // the + key itself
			parts = append(parts[:len(parts)-2], "+")
		}
		name = parts[len(parts)-1]
		for _, mod := range parts[:len(parts)-1] {
			switch mod {
			case "ctrl":
				k.Mod |= tea.ModCtrl
			case "alt":
				k.Mod |= tea.ModAlt
			case "shift":
				k.Mod |= tea.ModShift
			}
		}
	}
	if code, ok := keyCodes[name]; ok {
		k.Code = code
		if code == tea.KeySpace && k.Mod == 0 {
			k.Text = " "
		}
		return tea.KeyPressMsg(k)
	}
	k.Code = []rune(name)[0]
	if k.Mod == 0 {
		k.Text = name
	}
	return tea.KeyPressMsg(k)
}
