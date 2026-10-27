package rex

import (
	"context"
	"strings"
	"testing"
)

func TestLastLines(t *testing.T) {
	tests := []struct {
		name, content string
		n             int
		want          string
	}{
		// The cursor sits on an empty last row: the content ends with "\r\n".
		{"scrollback", "1\r\n2\r\n3\r\n4\r\n", 3, "3\n4\n"},
		// A full-screen program's last row has no line break after it.
		{"alt screen", "top\r\n\r\nbottom", 3, "top\n\nbottom"},
		{"shorter than n", "a\r\nb", 5, "a\nb"},
		{"zero", "a\r\nb", 0, ""},
		{"one", "a\r\nb", 1, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LastLines(tt.content, tt.n); got != tt.want {
				t.Errorf("LastLines(%q, %d) = %q, want %q", tt.content, tt.n, got, tt.want)
			}
		})
	}
}

func TestScreens(t *testing.T) {
	reply := `{"window_id":"w1","panes":[{"block_id":"b1","rect":[0,0,0.5,1],"size":{"columns":4,"rows":2},` +
		`"screen":"old\r\n\u001b[31mred\u001b[0m\r\n","cursor":{"row":1,"column":0}},{"block_id":"b2","rect":[0.5,0,0.5,1],"error":"gone"}]}`
	c, rec := newTestClient(t, stdout(reply))
	got, err := c.Screens(context.Background(), "s1", "w1")
	if err != nil {
		t.Fatal(err)
	}
	if got.WindowID != "w1" || len(got.Panes) != 2 {
		t.Fatalf("screens = %+v", got)
	}
	if p := got.Panes[0]; p.Screen != "\x1b[31mred\x1b[0m\n" || p.Size.Rows != 2 || p.Rect != (Rect{0, 0, 0.5, 1}) ||
		p.Cursor == nil || *p.Cursor != (Cursor{Row: 1}) {
		t.Errorf("pane 0 = %+v", p)
	}
	if p := got.Panes[1]; p.Error != "gone" || p.Screen != "" || p.Cursor != nil {
		t.Errorf("pane 1 = %+v", p)
	}
	argv := rec.argv[0]
	if !containsRun(argv, []string{"do", "-", "--json"}) || !containsRun(argv, []string{"-s", "s1"}) {
		t.Errorf("argv %q: want the script attached to the session", argv)
	}
	if !strings.Contains(string(rec.stdin[0]), "format") {
		t.Error("stdin is not the screens script")
	}

	c, _ = newTestClient(t, stdout(`{"error":"window w9 not found"}`))
	if _, err := c.Screens(context.Background(), "s1", "w9"); err == nil {
		t.Error("a script error is not an error")
	}
}
