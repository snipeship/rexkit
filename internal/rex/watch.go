package rex

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
)

// Event is a change notification from Watch. Type is a Rex event name such
// as session_view_changed or terminal.program_status_changed; the IDs say
// where it happened when Rex tells. Event payloads are not forwarded: re-read
// what changed (Snapshot, View).
type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	BlockID   string `json:"block_id,omitempty"`
}

// EventReady is sent once the watcher listens to every session.
const EventReady = "ready"

// EventKeymapChanged is sent when the server reloads its config.
const EventKeymapChanged = "keymap_changed"

// eventTick is the watch script's keep-alive (lua/watch.lua); it is not
// passed on.
const eventTick = "tick"

//go:embed lua/watch.lua
var watchScript []byte

// Watch streams change events from every session, including sessions created
// later, through one long-running `rex do --all` script. The channel closes
// when ctx is done or the watcher dies (for example when the server stops);
// callers should then fall back to polling.
func (c *Client) Watch(ctx context.Context) (<-chan Event, error) {
	argv := c.argv(ctx, []string{"do", "-", "--all"})
	cmd := c.command(ctx, argv)
	cmd.Stdin = bytes.NewReader(watchScript)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, &Error{Op: "watch", Message: cleanText(err.Error(), 300), kind: ErrUnavailable}
	}
	events := make(chan Event, 64)
	go func() {
		defer close(events)
		decodeEvents(ctx, stdout, events)
		_ = cmd.Wait()
	}()
	return events, nil
}

// decodeEvents forwards each JSON line of r as an Event, skipping other output.
func decodeEvents(ctx context.Context, r io.Reader, events chan<- Event) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var ev Event
		if json.Unmarshal(scanner.Bytes(), &ev) != nil || ev.Type == "" || ev.Type == eventTick {
			continue
		}
		select {
		case events <- ev:
		case <-ctx.Done():
			return
		}
	}
}
