package popup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"rexkit/internal/rex"
)

// Self is where the running tool lives.
type Self struct {
	SessionID string
	WindowID  string
	LayerID   string
	BlockID   string
	Floating  bool // false when the tool runs in an ordinary pane (demo mode)
}

// Popup is a tool's handle on its own floating layer.
type Popup struct {
	api   rex.API
	opts  Options
	block string

	mu     sync.Mutex
	closed bool // a Close succeeded
}

// New returns the popup of the tool running in $REX_BLOCK. opts.Session
// defaults to $REX_SESSION.
func New(api rex.API, opts Options) *Popup {
	if opts.Session == "" {
		opts.Session = os.Getenv("REX_SESSION")
	}
	return &Popup{api: api, opts: opts, block: os.Getenv("REX_BLOCK")}
}

// Options returns the options the popup was created with.
func (p *Popup) Options() Options { return p.opts }

// ErrOutsideRex means the tool is not running inside a Rex block.
var ErrOutsideRex = errors.New("popup: not running inside Rex ($REX_BLOCK is not set)")

// Self finds the tool's own block, window and layer.
func (p *Popup) Self(ctx context.Context) (Self, error) {
	if p.block == "" || p.opts.Session == "" {
		return Self{}, ErrOutsideRex
	}
	view, err := p.api.View(ctx, p.opts.Session)
	if err != nil {
		return Self{}, err
	}
	return find(view, p.block)
}

func find(view rex.View, block string) (Self, error) {
	w, l, _, ok := view.FindBlock(block)
	if !ok {
		return Self{}, fmt.Errorf("popup: block %s is not in session %s: %w", block, view.SessionID, rex.ErrNotFound)
	}
	return Self{SessionID: view.SessionID, WindowID: w.ID, LayerID: l.ID, BlockID: block, Floating: l.Floating()}, nil
}

// Close closes the tool's floating layer. If the tool still has focus and the
// origin was zoomed, the origin is re-zoomed first (zooming also focuses it),
// so nothing has to run after the layer — and this process with it — is gone.
// Otherwise close_layer itself returns focus to exactly where it was. A tool
// that lost focus closes without taking focus back.
//
// Close is safe to call more than once: once a call succeeds, later calls do
// nothing; a call that failed (a transient View or close_layer error, a
// context that ran out) is retried by the next one, which re-reads the
// session first.
func (p *Popup) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if err := p.close(ctx); err != nil {
		return err
	}
	p.closed = true
	return nil
}

// CloseTimeout bounds a tool's final Close (and Guard's).
const CloseTimeout = 3 * time.Second

// beforeSelfClose runs just before the tool closes its own layer. Closing the
// layer hangs up the tool's terminal; ignoring SIGHUP lets the process finish
// the call and its deferred cleanup instead of dying mid-way.
var beforeSelfClose = func() { signal.Ignore(syscall.SIGHUP) }

func (p *Popup) close(ctx context.Context) error {
	if p.block == "" || p.opts.Session == "" {
		return nil // not in Rex: nothing to close
	}
	view, err := p.api.View(ctx, p.opts.Session)
	if err != nil {
		return err
	}
	self, err := find(view, p.block)
	if err != nil || !self.Floating {
		return nil // already gone, or running in an ordinary pane
	}
	if p.opts.Zoomed && p.opts.OriginBlock != "" && hasFocus(view, self) {
		err := p.api.ZoomBlock(ctx, self.SessionID, p.opts.OriginBlock, true)
		if err != nil && !errors.Is(err, rex.ErrNotFound) {
			return err
		}
	}
	beforeSelfClose()
	return p.api.CloseLayer(ctx, self.SessionID, self.LayerID)
}

func hasFocus(view rex.View, self Self) bool {
	w, ok := view.Window(self.WindowID)
	return ok && view.ActiveWindowID == self.WindowID && w.FocusedBlockID == self.BlockID
}

// Guard closes the layer if the calling goroutine panics, then re-panics.
// Use it as the first deferred call of a tool's Main:
//
//	defer p.Guard()
func (p *Popup) Guard() {
	r := recover()
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), CloseTimeout)
	defer cancel()
	_ = p.Close(ctx)
	panic(r)
}
