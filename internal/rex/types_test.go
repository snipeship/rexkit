package rex

import (
	"encoding/json"
	"testing"
)

func TestListAcceptsLuaEmptyTables(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{`[]`, 0, false},
		{`null`, 0, false},
		{`{}`, 0, false},
		{` { } `, 0, false},
		{`["a","b"]`, 2, false},
		{`{"a":1}`, 0, true},
		{`"x"`, 0, true},
	}
	for _, tt := range tests {
		var l List[string]
		err := json.Unmarshal([]byte(tt.in), &l)
		if (err != nil) != tt.wantErr || len(l) != tt.want {
			t.Errorf("%s: len %d err %v, want len %d err %v", tt.in, len(l), err, tt.want, tt.wantErr)
		}
	}
}

func TestViewLookups(t *testing.T) {
	var v View
	if err := json.Unmarshal([]byte(viewJSON), &v); err != nil {
		t.Fatal(err)
	}
	w, l, b, ok := v.FindBlock("block:2")
	if !ok || w.ID != "window:1" || l.ID != "layer:1" || b.Label != "logs" || l.Floating() {
		t.Fatalf("FindBlock = %v %v %v %v", w.ID, l.ID, b, ok)
	}
	if r, ok := l.Rect("block:2"); !ok || r != (Rect{0.5, 0, 0.5, 1}) {
		t.Errorf("Rect = %v %v", r, ok)
	}
	if _, ok := l.Rect("block:9"); ok {
		t.Error("Rect found a missing block")
	}
	_, float, _, _ := v.FindBlock("block:3")
	if !float.Floating() {
		t.Error("layer:2 should be floating")
	}
	if _, _, _, ok := v.FindBlock("block:9"); ok {
		t.Error("found a missing block")
	}
}

func TestProcessCurrent(t *testing.T) {
	fg, child := &ProcessInfo{Name: "nvim"}, &ProcessInfo{Name: "zsh"}
	tests := []struct {
		p    Process
		want *ProcessInfo
	}{
		{Process{Foreground: fg, Child: child}, fg},
		{Process{Child: child}, child},
		{Process{}, nil},
	}
	for _, tt := range tests {
		if got := tt.p.Current(); got != tt.want {
			t.Errorf("Current() = %v, want %v", got, tt.want)
		}
	}
}

const viewJSON = `{
  "revision": 3, "session_id": "session:1", "label": "work", "active_window_id": "window:1",
  "windows": [{
    "window_id": "window:1", "label": "editor", "focused_block_id": "block:3",
    "layers": [
      {"layer_id": "layer:1", "kind": "tiled", "bounds": [0,0,1,1],
       "layout": {"rect": [0,0,1,1], "split": {"split_id": "split:1", "direction": "horizontal", "ratio": 0.5,
         "before": {"rect": [0,0,0.5,1], "block_id": "block:1"},
         "after": {"rect": [0.5,0,0.5,1], "block_id": "block:2"}}},
       "blocks": [
         {"block_id": "block:1", "label": "shell", "creator_name": "com.superlogical.terminal", "flavor": "com.superlogical.terminal.shell"},
         {"block_id": "block:2", "label": "logs", "creator_name": "com.superlogical.terminal", "flavor": "com.superlogical.terminal.shell"}]},
      {"layer_id": "layer:2", "kind": "floating", "bounds": [0.1,0.1,0.8,0.8],
       "layout": {"rect": [0.1,0.1,0.8,0.8], "block_id": "block:3"},
       "blocks": [{"block_id": "block:3", "label": "Go to", "creator_name": "com.superlogical.terminal", "flavor": "com.superlogical.terminal.shell"}]}
    ]
  }]
}`
