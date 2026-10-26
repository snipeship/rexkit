package rex

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestDecodeEventsSkipsNoise(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"ready"}`,
		`listening: 16 handlers, every session; Ctrl-C to stop`,
		`{"type":"session_view_changed","session_id":"session:1"}`,
		`{"no_type":true}`,
		`{"type":"terminal.pwd_changed","session_id":"session:1","block_id":"block:2"}`,
	}, "\n")
	events := make(chan Event, 10)
	decodeEvents(context.Background(), strings.NewReader(input), events)
	close(events)
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	want := []Event{
		{Type: EventReady},
		{Type: "session_view_changed", SessionID: "session:1"},
		{Type: "terminal.pwd_changed", SessionID: "session:1", BlockID: "block:2"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("events = %+v\nwant     %+v", got, want)
	}
}

// The watcher subscribes to what Go to shows, and not to title changes:
// a program that animates its title would re-collect the catalog every
// second.
func TestWatchScriptEvents(t *testing.T) {
	script := string(watchScript)
	for _, want := range []string{
		"session_created", "session_destroyed", "session_label_changed", "session_view_changed",
		"window_created", "window_closed", "window_label_changed", "active_window_changed",
		"block_created", "block_closed",
		"terminal.program_status_changed", "terminal.program_status_removed",
		"terminal.pwd_changed", "terminal.process_changed", "terminal.child_exited",
	} {
		if !strings.Contains(script, `"`+want+`"`) {
			t.Errorf("watch.lua does not subscribe to %s", want)
		}
	}
	if strings.Contains(script, `"terminal.title_changed"`) {
		t.Error("watch.lua subscribes to terminal.title_changed")
	}
}

func TestDecodeEventsStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan Event) // unbuffered and never read
	decodeEvents(ctx, strings.NewReader(`{"type":"a"}`+"\n"+`{"type":"b"}`), events)
}
