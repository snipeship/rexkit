//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/tests/integration/harness"
)

// tool is how to open a floating tool and how to back out of it.
type tool struct {
	action string
	args   any
	ready  string // text the tool shows once it is up
	label  string // its float's block label
	title  string // its float's header (the terminal title it sets)
	// dismiss backs out: through the picker mode's ⎋ when it has one, else
	// keys typed into the tool.
	picker bool
	keys   []string
}

var tools = []tool{
	{action: "rexkit_goto", ready: "Search sessions", label: "rexkit:goto", title: "Go to", picker: true},
	{action: "rexkit_files", ready: "Search this folder", label: "rexkit:files", title: "Files", picker: true},
	{action: "rexkit_keys", ready: "Search keys", label: "rexkit:keys", title: "Keys", picker: true},
	{action: "rexkit_rename_pane", ready: "cancel", label: "rexkit:rename", title: "Rename pane", picker: true},
	{action: "rexkit_rename_tab", ready: "cancel", label: "rexkit:rename", title: "Rename tab", picker: true},
	{action: "rexkit_rename_session", ready: "cancel", label: "rexkit:rename", title: "Rename session", picker: true},
	{action: "rexkit_scrollback", ready: "Scrollback", label: "rexkit:scrollback", title: "Scrollback", keys: []string{"q"}},
	{action: "rexkit_search", ready: "Search every pane", label: "rexkit:search", title: "Search", picker: true},
}

// TestToolsOpenAsFloatsAndRestoreFocus opens every tool from the second of
// three panes and backs out again: the float is labelled, titled (Rex's
// header shows the terminal title the tool sets), focused, enters
// the picker mode when it has a handshake, and on the way out focus returns
// to exactly the pane it came from, zoomed again if it was zoomed.
func TestToolsOpenAsFloatsAndRestoreFocus(t *testing.T) {
	s := start(t)
	for _, zoomed := range []bool{false, true} {
		for _, tl := range tools {
			name := tl.action
			if zoomed {
				name += "/zoomed"
			}
			t.Run(name, func(t *testing.T) {
				sess, blocks := s.CreateSession(t, "tools", hsplit(0.5, cat("A", ""), vsplit(0.5, cat("B", ""), cat("C", ""))))
				origin := blocks[1]
				focus(t, s, sess.ID, origin)
				wantZoom := ""
				if zoomed {
					zoom(t, s, sess.ID, origin)
					wantZoom = origin
				}
				o := open(t, s, sess.ID, origin, tl.action, tl.args, tl.ready)

				fl := floats(t, s, sess.ID)
				if len(fl) != 1 || fl[0].Block.ID != o.Block || fl[0].Block.Label != tl.label {
					t.Fatalf("floats = %+v, want one labelled %s", fl, tl.label)
				}
				if got := title(t, s, sess.ID, o.Block); got != tl.title {
					t.Errorf("title %q, want %q", got, tl.title)
				}
				if f, _ := s.Focus(t, sess.ID); f != o.Block {
					t.Errorf("focus %s, want the tool %s", f, o.Block)
				}
				_, picker := queued(o.Outcome, "client.mode.enter")
				if picker != tl.picker {
					t.Errorf("entered the picker mode: %v, want %v (queued %s)", picker, tl.picker, names(o.Outcome))
				}

				if tl.picker {
					out := act(t, s, sess.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
					if got := names(out); got != "client.mode.exit_all" {
						t.Errorf("dismiss queued %q", got)
					}
				} else {
					var in []harness.Input
					for _, k := range tl.keys {
						in = append(in, harness.Press(t, o.Block, k))
					}
					s.Drive(t, sess.ID, in...)
				}
				waitNoFloats(t, s, sess.ID)
				assertFocus(t, s, sess.ID, origin, wantZoom)
			})
		}
	}
}

// TestToolFollowsItsFloat: a tool lays itself out again when its float is
// resized (the user drags Rex's native float): at each size its screen has
// that many rows at most, its header on top, and its key bar on the row
// above the last, which is air (Rex stretches the last row down into the
// float's padding).
func TestToolFollowsItsFloat(t *testing.T) {
	s := start(t)
	sess, blocks := s.CreateSession(t, "resize", hsplit(0.5, cat("A", ""), cat("B", "")))
	sizes := [][2]int{{60, 16}, {100, 30}, {50, 12}}
	args, err := json.Marshal(map[string]any{"session": sess.ID, "block": blocks[0], "action": "rexkit_goto",
		"ready": "Search sessions", "bar": "keys", "sizes": sizes})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(s.Bin, "--autostart=false", "-S", s.Addr, "do", "-", "--json", "--args", string(args))
	cmd.Env, cmd.Stdin = s.Env, strings.NewReader(resizeTool)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var shots rex.List[struct {
		Columns int    `json:"columns"`
		Rows    int    `json:"rows"`
		Screen  string `json:"screen"`
		Error   string `json:"error"`
	}]
	if err := json.Unmarshal(out, &shots); err != nil || len(shots) != len(sizes) {
		t.Fatalf("%v: %s", err, out)
	}
	for i, sh := range shots {
		w, h := sizes[i][0], sizes[i][1]
		lines := strings.Split(strings.TrimRight(sh.Screen, "\n"), "\n")
		bar := -1
		for y, l := range lines {
			if strings.Contains(l, "keys") {
				bar = y
			}
		}
		top := strings.Join(lines[:min(2, len(lines))], "\n") // a band's field is on its second row
		if sh.Error != "" || sh.Columns != w || sh.Rows != h || len(lines) > h || !strings.Contains(top, "Search sessions") || bar != h-2 {
			t.Errorf("%dx%d (%s): %dx%d, key bar on row %d:\n%s", w, h, sh.Error, sh.Columns, sh.Rows, bar, sh.Screen)
		}
	}
	waitNoFloats(t, s, sess.ID)
}

// resizeTool opens a tool with rex.args.action from rex.args.block, as a key
// would, waits for it to show rex.args.ready, and for each of
// rex.args.sizes resizes its terminal (the client that opened it owns it,
// as the app owns a float it shows) and returns the screen it draws once
// its key bar (a row with rex.args.bar) is on the row above the last.
const resizeTool = `
local T, a = "com.superlogical.terminal", rex.args
rex.call("session.attach", {session_id = a.session})
local res = assert(rex.call("client.action", {session_id = a.session, block_id = a.block, action = a.action, origin = "key"}))
local tool = res.result.block_id
local function screen()
  local f = rex.call(T .. ".format", {session_id = a.session, block_id = tool, args = {format = "text"}})
  return f and f.content or ""
end
-- row is the nth line of text, nil if it has fewer.
local function row(text, n)
  local at = 1
  for _ = 2, n do
    local nl = text:find("\n", at, true)
    if not nl then return nil end
    at = nl + 1
  end
  return text:sub(at, (text:find("\n", at, true) or #text + 1) - 1)
end
local function wait(fn)
  for _ = 1, 250 do
    if fn() then return true end
    rex.sleep(0.02)
  end
  return false
end
local out = {}
if not wait(function() return screen():find(a.ready, 1, true) end) then return {{error = "never ready"}} end
for i, size in ipairs(a.sizes) do
  rex.call(T .. ".resize", {session_id = a.session, block_id = tool, args = {columns = size[1], rows = size[2]}})
  local ok = wait(function()
    local line = row(screen(), size[2] - 1)
    return line and line:find(a.bar, 1, true)
  end)
  local got = rex.call(T .. ".size", {session_id = a.session, block_id = tool, args = {}}) or {}
  out[i] = {columns = got.columns, rows = got.rows, screen = screen()}
  if not ok then out[i].error = "the key bar never moved" end
end
rex.call("client.action", {session_id = a.session, block_id = tool, action = "rexkit_dismiss", args = {key = "ctrl+c"}, origin = "key"})
return out
`

// TestSettingsFloat: the settings action opens the editor on settings.lua in
// a float of its own, centred as Files is, over a pane of a split.
func TestSettingsFloat(t *testing.T) {
	s := startWith(t, fmt.Sprintf("return { shell = %q, editor = { %q, %q, %q, %q }, binary = %q }",
		"/bin/sh", "/bin/sh", "-c", `echo "editing $1"; exec /bin/cat`, "sh", rexkitBin))
	sess, blocks := s.CreateSession(t, "settings", hsplit(0.5, cat("A", ""), cat("B", "")))
	focus(t, s, sess.ID, blocks[1])
	act(t, s, sess.ID, blocks[1], "rexkit_settings", nil)
	var fl []float
	harness.WaitFor(t, 5*time.Second, "the settings float", func() bool {
		fl = floats(t, s, sess.ID)
		return len(fl) == 1
	})
	if fl[0].Block.Label != "rexkit:settings" || fmt.Sprint(fl[0].Layer.Bounds) != "[0.11 0.13 0.78 0.74]" {
		t.Errorf("float %s at %v, want rexkit:settings at the centred float bounds", fl[0].Block.Label, fl[0].Layer.Bounds)
	}
	waitScreen(t, s, sess.ID, fl[0].Block.ID, "rexkit/settings.lua") // its path, which may wrap
	if screen := s.Capture(t, sess.ID, fl[0].Block.ID); !strings.HasPrefix(strings.ReplaceAll(screen, "\n", ""), "editing "+s.Dir) {
		t.Errorf("the editor got:\n%s", screen)
	}
	call(t, s, "session.close_layer", map[string]string{"session_id": sess.ID, "layer_id": fl[0].Layer.ID}, nil)
	waitNoFloats(t, s, sess.ID)
}
