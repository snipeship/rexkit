//go:build integration

package scrollback_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// fixture prints 60 coloured lines (ANSI, 256 and true colour), plus a title
// and a hyperlink that must not survive sanitizing, then waits.
const fixture = `i=1
while [ $i -le 60 ]; do
  printf '\033[3%dmline %02d\033[0m \033[38;2;255;100;0mrgb\033[0m \033]8;;file:///secret\033\\link\033]8;;\033\\\n' $((i % 6 + 1)) $i
  i=$((i + 1))
done
printf '\033]0;fixture title\007'
exec cat`

func buildViewer(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim is not installed")
	}
	bin := filepath.Join(t.TempDir(), "viewer")
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/viewer").CombinedOutput(); err != nil {
		t.Fatalf("build viewer: %v\n%s", err, out)
	}
	return bin
}

// TestScrollbackInAFloat opens the viewer in a float over a zoomed pane, the
// way the Lua layer does, checks what it shows, closes it with q, and checks
// that focus and zoom are back on the pane and the temp files are gone.
func TestScrollbackInAFloat(t *testing.T) {
	viewer := buildViewer(t)
	tmp := filepath.Join(t.TempDir(), "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	s := harness.Start(t, harness.Options{Env: []string{"TMPDIR=" + tmp}})
	ctx := context.Background()
	origin := s.NewSession(t, "origin", "/bin/sh", "-c", fixture)
	s.WaitScreen(t, origin.ID, origin.BlockID, "line 60", 5*time.Second)
	if _, err := s.Rex.NewSplit(ctx, origin.ID, rex.NewSplitRequest{AnchorID: origin.BlockID, Direction: rex.Horizontal,
		Side: "after", Layout: rex.Command("other", "", "/bin/cat")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Rex.ZoomBlock(ctx, origin.ID, origin.BlockID, true); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		start      string
		first, end string // first and last content rows on screen
		search     string // the search hint: upwards unless at the top
	}{
		{"bottom", "", "line 60 rgb link", "?  search up"},
		{"top", "line 01 rgb link", "", "/  search"},
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			layer, err := s.Rex.NewLayer(ctx, origin.ID, rex.NewLayerRequest{Bounds: rex.Rect{0, 0, 1, 1}, Focus: true,
				Layout: rex.Command("Scrollback", "", viewer, "--session", origin.ID, "--origin-block", origin.BlockID,
					"--zoomed", "--start", tt.start)})
			if err != nil {
				t.Fatal(err)
			}
			tool := layer.BlockIDs[0]
			screen := s.WaitScreen(t, origin.ID, tool, "Scrollback", 10*time.Second)
			spools, _ := filepath.Glob(filepath.Join(tmp, "rexkit-scrollback-*"))
			if len(spools) != 1 {
				t.Fatalf("spools while open: %q", spools)
			}
			if fi, err := os.Stat(spools[0]); err != nil || fi.Mode().Perm() != 0o700 {
				t.Errorf("spool %v: %v", fi.Mode(), err)
			}
			rows := strings.Split(strings.TrimRight(screen, "\n"), "\n")
			status := rows[len(rows)-1]
			for _, want := range []string{"Scrollback", tt.search, "q  close", "/60"} {
				if !strings.Contains(status, want) {
					t.Errorf("status line %q lacks %q", status, want)
				}
			}
			if tt.first != "" && rows[0] != tt.first {
				t.Errorf("top row = %q, want %q", rows[0], tt.first)
			}
			if tt.end != "" && rows[len(rows)-2] != tt.end {
				t.Errorf("row above the status line = %q, want %q", rows[len(rows)-2], tt.end)
			}

			vt, err := s.Rex.Capture(ctx, origin.ID, tool, rex.CaptureOptions{Format: rex.FormatVT})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"\x1b[38;5;1m", "\x1b[38;5;6m", "\x1b[38;2;255;100;0m"} {
				if !strings.Contains(vt, want) {
					t.Errorf("the viewer lost the colour %q", want)
				}
			}
			if strings.Contains(vt, "file:///secret") {
				t.Error("a hyperlink survived sanitizing")
			}

			if err := s.Rex.Write(ctx, origin.ID, tool, "q"); err != nil {
				t.Fatal(err)
			}
			harness.WaitFor(t, 5*time.Second, "the float to close", func() bool {
				_, _, _, open := s.View(t, origin.ID).FindBlock(tool)
				return !open
			})
			if focused, zoomed := s.Focus(t, origin.ID); focused != origin.BlockID || zoomed != origin.BlockID {
				t.Errorf("after q: focus %s zoom %s, want both on the pane %s", focused, zoomed, origin.BlockID)
			}
			// Neovim keeps its own nvim.$USER dir there; ours must be gone.
			harness.WaitFor(t, 2*time.Second, "the spool to be removed", func() bool {
				spools, _ := filepath.Glob(filepath.Join(tmp, "rexkit-scrollback-*"))
				return len(spools) == 0
			})
		})
	}
}

// TestScrollbackErrorCard shows the error card for a pane that is gone and
// still closes its float after a key.
func TestScrollbackErrorCard(t *testing.T) {
	viewer := buildViewer(t)
	s := harness.Start(t, harness.Options{})
	ctx := context.Background()
	origin := s.NewSession(t, "origin")
	layer, err := s.Rex.NewLayer(ctx, origin.ID, rex.NewLayerRequest{Bounds: rex.Rect{0.1, 0.1, 0.8, 0.8}, Focus: true,
		Layout: rex.Command("Scrollback", "", viewer, "--session", origin.ID, "--origin-block", "block:gone")})
	if err != nil {
		t.Fatal(err)
	}
	tool := layer.BlockIDs[0]
	s.WaitScreen(t, origin.ID, tool, "Can't show the scrollback", 10*time.Second)
	if err := s.Rex.Write(ctx, origin.ID, tool, "x"); err != nil {
		t.Fatal(err)
	}
	harness.WaitFor(t, 5*time.Second, "the float to close", func() bool {
		_, _, _, open := s.View(t, origin.ID).FindBlock(tool)
		return !open
	})
	if focused, _ := s.Focus(t, origin.ID); focused != origin.BlockID {
		t.Errorf("focus %s, want the pane %s", focused, origin.BlockID)
	}
}

// TestScrollbackClosedFromOutside closes the float under the running viewer
// (the tool gets SIGHUP) and checks that its spool, inside the tool's state
// dir, is still removed.
func TestScrollbackClosedFromOutside(t *testing.T) {
	viewer := buildViewer(t)
	state := t.TempDir()
	s := harness.Start(t, harness.Options{})
	ctx := context.Background()
	origin := s.NewSession(t, "origin", "/bin/sh", "-c", fixture)
	s.WaitScreen(t, origin.ID, origin.BlockID, "line 60", 5*time.Second)
	layer, err := s.Rex.NewLayer(ctx, origin.ID, rex.NewLayerRequest{Bounds: rex.Rect{0, 0, 1, 1}, Focus: true,
		Layout: rex.Command("Scrollback", "", viewer, "--session", origin.ID, "--origin-block", origin.BlockID, "--state", state)})
	if err != nil {
		t.Fatal(err)
	}
	s.WaitScreen(t, origin.ID, layer.BlockIDs[0], "Scrollback", 10*time.Second)
	spools, _ := filepath.Glob(filepath.Join(state, "rexkit-scrollback-*"))
	if len(spools) != 1 {
		t.Fatalf("spools in the state dir while open: %q", spools)
	}
	if err := s.Rex.CloseLayer(ctx, origin.ID, layer.LayerID); err != nil {
		t.Fatal(err)
	}
	harness.WaitFor(t, 5*time.Second, "the spool to be removed", func() bool {
		_, err := os.Stat(spools[0])
		return os.IsNotExist(err)
	})
}
