// Package nvim embeds rexkit's Neovim profiles, so the binary never depends
// on where the source tree lives.
package nvim

import _ "embed"

// Scrollback is the profile of the scrollback viewer (scrollback.lua).
//
//go:embed scrollback.lua
var Scrollback []byte
