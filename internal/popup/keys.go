package popup

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// HoldKeys puts the tool's terminal into raw mode straight away, before the
// tool talks to Rex or starts its UI. Lua opens the float and may write ⏎ or
// ⎋ into it at once (and the user may type): in the terminal's default
// cooked mode those keys would be echoed, ⏎ would turn into a line feed and
// ⌃C would kill the tool. The returned func restores the terminal; it is a
// no-op when stdin is not a terminal.
func HoldKeys() (restore func()) {
	fd := os.Stdin.Fd()
	if !term.IsTerminal(fd) {
		return func() {}
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}
	return func() { _ = term.Restore(fd, old) }
}
