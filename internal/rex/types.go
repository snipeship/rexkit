package rex

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

// List is a JSON array that also accepts an object. Lua encodes an empty
// table as {}, so anything that passed through a Lua script may hold {} where
// the server would have sent [].
type List[T any] []T

// UnmarshalJSON accepts an array, null, or an empty object.
func (l *List[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		if len(obj) != 0 {
			return errors.New("rex: expected an array, got an object")
		}
		*l = nil
		return nil
	}
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	*l = items
	return nil
}

// Rect is a rectangle as [x, y, w, h] fractions of a window.
type Rect [4]float64

// SessionInfo is one entry of session.list.
type SessionInfo struct {
	ID    string `json:"session_id"`
	Label string `json:"label"`
}

// View is a session's structure, as session.view returns it.
type View struct {
	SessionID      string       `json:"session_id"`
	Label          string       `json:"label"`
	Revision       int64        `json:"revision"`
	ActiveWindowID string       `json:"active_window_id,omitempty"`
	Windows        List[Window] `json:"windows"`
}

// Window is a tab of a session.
type Window struct {
	ID             string      `json:"window_id"`
	Label          string      `json:"label"`
	FocusedBlockID string      `json:"focused_block_id,omitempty"`
	ZoomedBlockID  string      `json:"zoomed_block_id,omitempty"`
	Layers         List[Layer] `json:"layers"`
}

// Layer kinds reported by session.view.
const (
	LayerTiled    = "tiled"
	LayerFloating = "floating"
)

// Layer is the tiled layout of a window or a floating layer above it.
type Layer struct {
	ID     string      `json:"layer_id"`
	Kind   string      `json:"kind"`
	Bounds Rect        `json:"bounds"`
	Layout *LayoutNode `json:"layout,omitempty"`
	Blocks List[Block] `json:"blocks"`
}

// Floating reports whether the layer floats above the window's layout. Rex
// asks clients to treat unknown kinds as floating.
func (l Layer) Floating() bool { return l.Kind != LayerTiled }

// LayoutNode is a node of a layer's split tree: either a block or a split.
type LayoutNode struct {
	Rect    Rect   `json:"rect"`
	BlockID string `json:"block_id,omitempty"`
	Split   *Split `json:"split,omitempty"`
}

// Split divides a node in two.
type Split struct {
	ID        string     `json:"split_id"`
	Direction string     `json:"direction"`
	Ratio     float64    `json:"ratio"`
	Before    LayoutNode `json:"before"`
	After     LayoutNode `json:"after"`
}

// TerminalCreator is the creator name of terminal blocks.
const TerminalCreator = "com.superlogical.terminal"

// Block is a block placed in a layer. Terminal is filled in only by Snapshot.
type Block struct {
	ID       string    `json:"block_id"`
	Label    string    `json:"label"`
	Creator  string    `json:"creator_name"`
	Flavor   string    `json:"flavor"`
	HostID   string    `json:"host_id,omitempty"`
	Owner    string    `json:"owner,omitempty"`
	Terminal *Terminal `json:"terminal,omitempty"`
}

// IsTerminal reports whether the block is a terminal.
func (b Block) IsTerminal() bool { return b.Creator == TerminalCreator }

// Terminal is what Snapshot learns about a terminal block.
type Terminal struct {
	Process *Process           `json:"process,omitempty"`
	Status  List[StatusRecord] `json:"status"`
	Title   string             `json:"title,omitempty"`
}

// Process is the result of the terminal process method.
type Process struct {
	Foreground *ProcessInfo `json:"foreground,omitempty"`
	Child      *ProcessInfo `json:"child,omitempty"`
	LastExit   *Exit        `json:"last_exit,omitempty"`
	Home       string       `json:"home,omitempty"`
}

// Current is the foreground process, or the terminal's own child when the
// foreground is unknown.
func (p Process) Current() *ProcessInfo {
	if p.Foreground != nil {
		return p.Foreground
	}
	return p.Child
}

// Exit is how a terminal's own program exited.
type Exit struct {
	Code  int    `json:"exit_code"`
	Error string `json:"error,omitempty"`
}

// ProcessInfo describes one process in a terminal.
type ProcessInfo struct {
	PID         int    `json:"pid"`
	Name        string `json:"name"`
	Path        string `json:"path,omitempty"`
	InvokedPath string `json:"invoked_path,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

// Program states reported through OSC 7501.
const (
	StateIdle    = "idle"
	StateWorking = "working"
	StateDone    = "done"
	StateBlocked = "blocked"
	StateError   = "error"
)

// StatusRecord is one program status record (OSC 7501).
type StatusRecord struct {
	ID           string    `json:"id,omitempty"`
	App          string    `json:"app,omitempty"`
	EffectiveApp string    `json:"effective_app,omitempty"`
	State        string    `json:"state"`
	Kind         string    `json:"kind,omitempty"`
	Title        string    `json:"title,omitempty"`
	Msg          string    `json:"msg,omitempty"`
	Progress     *int      `json:"progress,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Size is the result of the terminal size method.
type Size struct {
	Columns      int    `json:"columns"`
	Rows         int    `json:"rows"`
	CellWidthPx  int    `json:"cell_width_px,omitempty"`
	CellHeightPx int    `json:"cell_height_px,omitempty"`
	Owner        string `json:"owner,omitempty"`
}

// FindBlock locates a block in the view.
func (v View) FindBlock(id string) (Window, Layer, Block, bool) {
	for _, w := range v.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if b.ID == id {
					return w, l, b, true
				}
			}
		}
	}
	return Window{}, Layer{}, Block{}, false
}

// Window returns the window with the given ID.
func (v View) Window(id string) (Window, bool) {
	for _, w := range v.Windows {
		if w.ID == id {
			return w, true
		}
	}
	return Window{}, false
}

// Rect returns the rectangle of a block in the layer's split tree.
func (l Layer) Rect(blockID string) (Rect, bool) {
	if l.Layout == nil {
		return Rect{}, false
	}
	return l.Layout.find(blockID)
}

func (n LayoutNode) find(blockID string) (Rect, bool) {
	if n.Split == nil {
		return n.Rect, n.BlockID == blockID
	}
	if r, ok := n.Split.Before.find(blockID); ok {
		return r, true
	}
	return n.Split.After.find(blockID)
}
