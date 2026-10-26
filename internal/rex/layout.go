package rex

// Node is a layout to create: exactly one of Block or Split is set.
type Node struct {
	Block *BlockSpec `json:"block,omitempty"`
	Split *SplitSpec `json:"split,omitempty"`
}

// BlockSpec starts one new block.
type BlockSpec struct {
	Flavor  string           `json:"flavor"`
	Label   string           `json:"label,omitempty"`
	Options *TerminalOptions `json:"options,omitempty"`
}

// SplitSpec divides space in two. Direction is "horizontal" (side by side)
// or "vertical" (stacked).
type SplitSpec struct {
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio,omitempty"`
	Before    Node    `json:"before"`
	After     Node    `json:"after"`
}

// Split directions.
const (
	Horizontal = "horizontal"
	Vertical   = "vertical"
)

// TerminalOptions are the options of a terminal block. Rex has no env option
// (unknown options are silently ignored): wrap the command in /usr/bin/env.
type TerminalOptions struct {
	Shell        string       `json:"shell,omitempty"`
	Command      []string     `json:"command,omitempty"`
	Cwd          string       `json:"cwd,omitempty"`
	Login        *bool        `json:"login,omitempty"`
	PathPrepend  []string     `json:"path_prepend,omitempty"`
	InitialInput string       `json:"initial_input,omitempty"`
	Exit         *ExitOptions `json:"exit,omitempty"`
}

// ExitOptions controls what happens when a terminal's program exits.
type ExitOptions struct {
	OnCompletion         bool `json:"on_completion"`
	QuickExitThresholdMs *int `json:"quick_exit_threshold_ms,omitempty"`
}

// ShellFlavor is the flavor of an ordinary terminal block.
const ShellFlavor = "com.superlogical.terminal.shell"

// Shell is a terminal running the user's shell in cwd.
func Shell(label, cwd string) Node {
	opts := &TerminalOptions{Cwd: cwd}
	return Node{Block: &BlockSpec{Flavor: ShellFlavor, Label: label, Options: opts}}
}

// Command is a terminal that execs argv directly (no shell, no startup
// files) and closes when the program exits, however quickly.
func Command(label, cwd string, argv ...string) Node {
	zero := 0
	opts := &TerminalOptions{
		Shell:   "none",
		Command: argv,
		Cwd:     cwd,
		Exit:    &ExitOptions{OnCompletion: true, QuickExitThresholdMs: &zero},
	}
	return Node{Block: &BlockSpec{Flavor: ShellFlavor, Label: label, Options: opts}}
}

// NewWindowRequest creates a window (tab).
type NewWindowRequest struct {
	Label  string `json:"window_label,omitempty"`
	Layout Node   `json:"layout"`
	Focus  *bool  `json:"focus,omitempty"`
}

// NewSplitRequest creates blocks beside an anchor block or split.
type NewSplitRequest struct {
	AnchorID  string  `json:"anchor_id,omitempty"`
	WindowID  string  `json:"window_id,omitempty"`
	Direction string  `json:"direction"`
	Side      string  `json:"side"` // "before" or "after"
	Ratio     float64 `json:"ratio,omitempty"`
	Layout    Node    `json:"layout"`
	Focus     bool    `json:"focus,omitempty"`
}

// NewLayerRequest creates a floating layer.
type NewLayerRequest struct {
	WindowID string `json:"window_id,omitempty"`
	Bounds   Rect   `json:"bounds"`
	Layout   Node   `json:"layout"`
	Focus    bool   `json:"focus,omitempty"`
}

// MoveBlockRequest places a block beside an anchor block.
type MoveBlockRequest struct {
	BlockID   string  `json:"block_id"`
	AnchorID  string  `json:"anchor_block_id"`
	Direction string  `json:"direction,omitempty"`
	Side      string  `json:"side,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
}

// Created reports what a new_window, new_split or new_layer call made.
type Created struct {
	WindowID string       `json:"window_id,omitempty"`
	LayerID  string       `json:"layer_id,omitempty"`
	SplitIDs List[string] `json:"split_ids,omitempty"`
	BlockIDs List[string] `json:"block_ids"`
	Revision int64        `json:"revision"`
}
