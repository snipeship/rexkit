// Package scrollback implements `rexkit scrollback`: it captures a pane's
// screen and scrollback with colours, sanitizes it to plain text plus SGR,
// and shows it in Neovim (nvim/scrollback.lua) in a float laid exactly over
// the pane, so it feels like scrolling the pane itself. Without Neovim it
// falls back to `less -R`.
//
// The tool owns a private temp dir for the capture and always ends by
// closing its own float (popup.Close), which returns focus to the pane.
package scrollback
