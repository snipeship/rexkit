package rex

import (
	"context"
	_ "embed"
	"strings"
)

// Screens is what a window's tiled panes show: everything a floating tool
// needs to redraw the window behind its card.
type Screens struct {
	WindowID string           `json:"window_id"`
	Zoomed   string           `json:"zoomed,omitempty"` // the zoomed block; then it is the only pane
	Panes    List[PaneScreen] `json:"panes"`
}

// PaneScreen is one visible terminal pane of a window's tiled layer.
type PaneScreen struct {
	BlockID string `json:"block_id"`
	Rect    Rect   `json:"rect"` // fractions of the window; the whole window when zoomed
	Size    Size   `json:"size"`
	// Screen is the pane's visible screen as escape sequences (format vt):
	// exactly its last Size.Rows lines, joined by "\n" (no "\r").
	Screen string `json:"screen"`
	Error  string `json:"error,omitempty"` // why the pane could not be read
	// Cursor is where the cursor is on Screen (0-based); nil while the
	// program hides it (DEC mode 25), as a TUI that draws its own does.
	Cursor *Cursor `json:"cursor,omitempty"`
	// Title is the pane's terminal title and Program what runs in it (the
	// command it was started as: "nu", "claude"), which Rex's pane header
	// shows. Either may be empty.
	Title   string `json:"title,omitempty"`
	Program string `json:"program,omitempty"`
}

// Cursor is a cell of a terminal's screen.
type Cursor struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

//go:embed lua/screens.lua
var screensScript []byte

// Screens reads the visible screens of a window's tiled panes in one round
// trip (an embedded Lua script). window is a window ID, a block ID (the
// window holding that block: a float finds the window it covers by its own
// block) or empty (the session's active window). Floating layers are left
// out; a zoomed window has only its zoomed pane. A pane that cannot be read
// comes back with Error set.
func (c *Client) Screens(ctx context.Context, session, window string) (Screens, error) {
	var out struct {
		Screens
		Error string `json:"error"`
	}
	args := object{"session": session, "window": window}
	if err := c.script(ctx, "screens", screensScript, args, &out, "-s", session); err != nil {
		return Screens{}, err
	}
	if out.Error != "" {
		return Screens{}, parseError("screens", 0, out.Error)
	}
	for i := range out.Panes {
		p := &out.Panes[i]
		p.Screen = LastLines(p.Screen, p.Size.Rows)
	}
	return out.Screens, nil
}

// LastLines returns the last n lines of a terminal's formatted content (its
// screen, when the content is screen and scrollback), with "\r" dropped. Rex
// ends every row but the last with "\r\n", so the content of a terminal
// whose cursor sits on an empty last row ends with "\r\n" and its last line
// is empty.
func LastLines(content string, n int) string {
	if n <= 0 {
		return ""
	}
	end := len(content)
	start := end
	for lines := 0; start > 0; start-- {
		if content[start-1] == '\n' {
			if lines++; lines == n {
				break
			}
		}
	}
	return strings.ReplaceAll(content[start:end], "\r", "")
}
