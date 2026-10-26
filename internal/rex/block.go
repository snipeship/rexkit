package rex

import (
	"context"
	_ "embed"
	"strings"
)

// BlockCall calls a block method by its short name, such as "process",
// "program_status", "size", "title", "write" or "format". args may be nil.
func (c *Client) BlockCall(ctx context.Context, session, block, method string, args, out any) error {
	if args == nil {
		args = object{}
	}
	req := object{"session_id": session, "block_id": block, "method": method, "args": args}
	return c.Call(ctx, "block.method", req, out)
}

// Process reports the processes running in a terminal block.
func (c *Client) Process(ctx context.Context, session, block string) (Process, error) {
	var p Process
	err := c.BlockCall(ctx, session, block, "process", nil, &p)
	return p, err
}

// ProgramStatus lists a terminal's OSC 7501 status records.
func (c *Client) ProgramStatus(ctx context.Context, session, block string) ([]StatusRecord, error) {
	var res struct {
		Records List[StatusRecord] `json:"records"`
	}
	err := c.BlockCall(ctx, session, block, "program_status", nil, &res)
	return res.Records, err
}

// Size reports a terminal's size in cells.
func (c *Client) Size(ctx context.Context, session, block string) (Size, error) {
	var s Size
	err := c.BlockCall(ctx, session, block, "size", nil, &s)
	return s, err
}

// Write types data into a terminal as if the user had typed it. It goes into
// the pty after everything typed before it.
func (c *Client) Write(ctx context.Context, session, block, data string) error {
	return c.BlockCall(ctx, session, block, "write", object{"data": data}, nil)
}

// Capture formats.
const (
	FormatText = "text"
	FormatVT   = "vt"
	FormatHTML = "html"
)

// CaptureOptions selects what Capture returns.
type CaptureOptions struct {
	Format string // FormatText (default), FormatVT or FormatHTML
	Trim   bool   // remove trailing spaces from each line
	Unwrap bool   // join soft-wrapped lines
	// TailBytes, when positive, returns only about the last TailBytes bytes,
	// starting at a line boundary.
	TailBytes int
}

//go:embed lua/tail.lua
var tailScript []byte

// Capture returns a terminal's screen and scrollback.
func (c *Client) Capture(ctx context.Context, session, block string, opts CaptureOptions) (string, error) {
	if opts.Format == "" {
		opts.Format = FormatText
	}
	if opts.TailBytes > 0 {
		return c.tail(ctx, session, block, opts)
	}
	args := []string{"capture", "-s", session, "-b", block, "--format", opts.Format}
	if opts.Trim {
		args = append(args, "--trim")
	}
	if opts.Unwrap {
		args = append(args, "--unwrap")
	}
	out, err := c.exec(ctx, "capture", nil, args...)
	return string(out), err
}

func (c *Client) tail(ctx context.Context, session, block string, opts CaptureOptions) (string, error) {
	args := object{
		"session": session, "block": block, "format": opts.Format,
		"trim": opts.Trim, "unwrap": opts.Unwrap, "max": opts.TailBytes,
	}
	var res struct {
		Content string `json:"content"`
		Partial bool   `json:"partial"`
		Error   string `json:"error"`
	}
	// The script reads the block through block.method, which only works on a
	// session the script is attached to: -s attaches it to the target's
	// session, whichever session (if any) the caller runs in.
	if err := c.script(ctx, "capture", tailScript, args, &res, "-s", session); err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", parseError("capture", 0, res.Error)
	}
	if res.Partial {
		// The read began mid-line, maybe inside a UTF-8 rune or an escape
		// sequence. Rex restates styles on every line, so dropping it is safe.
		_, rest, _ := strings.Cut(res.Content, "\n")
		return rest, nil
	}
	return res.Content, nil
}
