//go:build integration

package integration

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/backdrop"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
	"rexkit/tests/integration/harness"
)

// resizeScript gives blocks a size. A server without an app sizes every
// terminal 80×24 and keeps sizes only its owner asks for, so the script
// claims each block first (the size stays once it disconnects).
const resizeScript = `
local a = rex.args
for _, b in ipairs(a.blocks) do
  rex.call("session.own_block", {session_id = a.session, block_ids = {b.id}})
  rex.call("block.method", {session_id = a.session, block_id = b.id, method = "resize", args = {columns = b.columns, rows = b.rows}})
end
return {}
`

// blanks is how Rex's vt format writes a run of blank cells: erase n
// characters (in the current background) and move over them, or just move.
var blanks = regexp.MustCompile(`\x1b\[(\d*)X\x1b\[\d*C|\x1b\[(\d*)C`)

// expandBlanks turns those runs into spaces, which a cell buffer can read.
func expandBlanks(vt string) string {
	return blanks.ReplaceAllStringFunc(vt, func(seq string) string {
		m := blanks.FindStringSubmatch(seq)
		n, _ := strconv.Atoi(m[1] + m[2])
		return strings.Repeat(" ", max(n, 1))
	})
}

type sized struct {
	ID      string `json:"id"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}

func resize(t *testing.T, s *harness.Server, session string, blocks ...sized) {
	t.Helper()
	path := t.TempDir() + "/resize.lua"
	if err := os.WriteFile(path, []byte(resizeScript), 0o600); err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"session":%q,"blocks":[`, session)
	for i, b := range blocks {
		if i > 0 {
			args += ","
		}
		args += fmt.Sprintf(`{"id":%q,"columns":%d,"rows":%d}`, b.ID, b.Columns, b.Rows)
	}
	s.CLI(t, "do", path, "-s", session, "--json", "--args", args+"]}")
}

// TestBackdrop opens a tool over three tiled panes in a 120×40 window (the
// sizes the user's app would give them) and reads the tool's float back: each
// pane's text sits where backdrop.Place says, in dimmed colours, the margin
// is drawn (not see-through) and the card is on top.
func TestBackdrop(t *testing.T) {
	s := startWith(t, strings.Replace(settings, "return {", `return { backdrop = "dim",`, 1), rextheme.EnvTheme+"="+rextheme.DefaultDark)
	// One pane with a long scrollback, one plain, one on the alternate
	// screen (a full-screen program): the backdrop shows each one's screen.
	marked := func(label, before string) rex.Node {
		script := before + `printf '\033[31mMARK-%s\033[0m tail\n' "$1"; exec cat`
		n := rex.Command(label, "", "/bin/sh", "-c", script, "sh", strings.ToUpper(label))
		n.Block.Options.Exit = nil
		return n
	}
	sess, blocks := s.CreateSession(t, "backdrop", hsplit(1.0/3, marked("one", "seq 1 500; clear; "),
		hsplit(0.5, marked("two", `printf '\033]0;title-two\007'; `), marked("three", `seq 1 80; printf '\033[?1049h\033[2J\033[H'; `))))
	const W, H = 120, 40
	view := s.View(t, sess.ID)
	w, _ := view.Window(sess.WindowID)
	tiled := w.Layers[0]
	var sizes []sized
	rects := map[string]rex.Rect{}
	for _, b := range blocks {
		r, ok := tiled.Rect(b)
		if !ok {
			t.Fatalf("no rect for %s", b)
		}
		rects[b] = r
		at := backdrop.Place(r, 0, 0, W, H)
		cols := int(math.Round((r[0]+r[2])*W)) - at.X - 1
		sizes = append(sizes, sized{b, cols, H - at.Y})
	}
	resize(t, s, sess.ID, sizes...)
	for i, b := range blocks {
		waitScreen(t, s, sess.ID, b, "MARK-"+strings.ToUpper([]string{"one", "two", "three"}[i]))
	}

	start := time.Now()
	if _, err := backdrop.Load(context.Background(), s.Rex, rextheme.Builtin(true), sess.ID, blocks[0]); err != nil {
		t.Fatal(err)
	}
	t.Logf("loading the three panes took %s", time.Since(start).Round(time.Millisecond))
	screens, err := s.Rex.Screens(context.Background(), sess.ID, blocks[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range screens.Panes {
		if p.Program == "login" {
			t.Errorf("pane %s: the terminal's own child named its title bar", p.BlockID)
		}
		t.Logf("pane %s: program %q, title %q", p.BlockID, p.Program, p.Title)
	}

	origin := blocks[2]
	focus(t, s, sess.ID, origin)
	o := open(t, s, sess.ID, origin, "rexkit_rename_pane", nil, "Rename pane")
	resize(t, s, sess.ID, sized{o.Block, W, H})

	theme := rextheme.Builtin(true)
	red := theme.Terminal.Indexed(1)
	var buf uv.ScreenBuffer
	read := func() string {
		vt, err := s.Rex.Capture(context.Background(), sess.ID, o.Block, rex.CaptureOptions{Format: rex.FormatVT})
		if err != nil {
			t.Fatal(err)
		}
		buf = uv.NewScreenBuffer(W, H)
		buf.Method = ansi.GraphemeWidth
		if !strings.HasSuffix(vt, "\r\n") { // the newline rex capture adds
			vt = strings.TrimSuffix(vt, "\n")
		}
		uv.NewStyledString(expandBlanks(rex.LastLines(vt, H))).Draw(buf, buf.Bounds())
		return vt
	}
	text := func(x, y, n int) string {
		var b strings.Builder
		for i := range n {
			if c := buf.CellAt(x+i, y); c != nil {
				b.WriteString(c.Content)
			}
		}
		return b.String()
	}
	// Wait for the backdrop to be loaded at 120×40 and faded in: every
	// marker in place, and the frame no longer changing.
	var last string
	defer func() {
		if t.Failed() {
			t.Logf("the float shows:\n%s", ansi.Strip(buf.Render()))
		}
	}()
	harness.WaitFor(t, 5*time.Second, "the backdrop", func() bool {
		vt := read()
		for i, b := range blocks {
			r := backdrop.Place(rects[b], sizes[i].Columns, sizes[i].Rows, W, H)
			if !strings.HasPrefix(text(r.X, r.Y, 10), "MARK-") {
				return false
			}
		}
		done := vt == last
		last = vt
		return done
	})
	for i, b := range blocks {
		r := backdrop.Place(rects[b], sizes[i].Columns, sizes[i].Rows, W, H)
		want := "MARK-" + strings.ToUpper([]string{"one", "two", "three"}[i]) + " tail"
		if got := text(r.X, r.Y, len(want)); got != want {
			t.Errorf("pane %d at %+v reads %q, want %q", i, r, got, want)
		}
		fg := rextheme.Of(buf.CellAt(r.X, r.Y).Style.Fg)
		if fg == red || fg.Luma() >= red.Luma() {
			t.Errorf("pane %d: marker in %s, want it dimmed from %s", i, fg, red)
		}
		t.Logf("pane %d: rect %v, %dx%d at %+v, red %s dimmed to %s", i, rects[b], sizes[i].Columns, sizes[i].Rows, r, red, fg)
		// Only pane two has a title (and a plain command shows no program).
		bar := strings.TrimSpace(text(r.X, r.Y-backdrop.HeaderRows, 20))
		if want := map[int]string{1: "›_ title-two"}[i]; bar != want {
			t.Errorf("pane %d: title bar reads %q, want %q", i, bar, want)
		}
	}
	if c := buf.CellAt(0, 0); c == nil || c.Style.Bg == nil {
		t.Error("the inset is see-through: the backdrop must draw every cell")
	}
	if screen := ansi.Strip(buf.Render()); !strings.Contains(screen, "Rename pane") {
		t.Errorf("no card on top:\n%s", screen)
	}
}
