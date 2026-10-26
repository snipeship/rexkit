package rex

import "context"

// API is what rexkit tools need from Rex. *Client implements it over the
// CLI; rexfake implements it in memory for tests.
type API interface {
	Call(ctx context.Context, method string, args, out any) error

	Sessions(ctx context.Context) ([]SessionInfo, error)
	View(ctx context.Context, session string) (View, error)
	Snapshot(ctx context.Context) (Inventory, error)
	Watch(ctx context.Context) (<-chan Event, error)
	Keymap(ctx context.Context) (Keymap, error)

	FocusBlock(ctx context.Context, session, block string) error
	FocusWindow(ctx context.Context, session, window string) error
	ZoomBlock(ctx context.Context, session, block string, zoom bool) error
	NewWindow(ctx context.Context, session string, req NewWindowRequest) (Created, error)
	NewSplit(ctx context.Context, session string, req NewSplitRequest) (Created, error)
	NewLayer(ctx context.Context, session string, req NewLayerRequest) (Created, error)
	CloseLayer(ctx context.Context, session, layer string) error
	MoveBlock(ctx context.Context, session string, req MoveBlockRequest) error
	SetBlockLabel(ctx context.Context, session, block, label string) error
	SetWindowLabel(ctx context.Context, session, window, label string) error
	SetSessionLabel(ctx context.Context, session, label string) error
	CloseBlock(ctx context.Context, session, block string) error
	CloseWindow(ctx context.Context, session, window string) error
	DestroySession(ctx context.Context, session string) error

	BlockCall(ctx context.Context, session, block, method string, args, out any) error
	Process(ctx context.Context, session, block string) (Process, error)
	ProgramStatus(ctx context.Context, session, block string) ([]StatusRecord, error)
	Size(ctx context.Context, session, block string) (Size, error)
	Write(ctx context.Context, session, block, data string) error
	Capture(ctx context.Context, session, block string, opts CaptureOptions) (string, error)
	Screens(ctx context.Context, session, window string) (Screens, error)

	ClientPerform(ctx context.Context, client, action string, args any) error
}

var _ API = (*Client)(nil)
