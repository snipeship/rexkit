package nvim

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// probe runs once the profile has loaded the capture. It records what the
// viewer shows, then presses each key sequence from $PROBE_KEYS and records
// the cursor, the mode and what reached a fake clipboard (never the real
// one), and prints the record as JSON.
const probe = `
if vim.env.PROBE_COLUMNS then vim.o.columns = tonumber(vim.env.PROBE_COLUMNS) end
if vim.env.PROBE_LUA then assert(loadstring(vim.env.PROBE_LUA))() end
vim.g.clipboard = {
  name = "probe",
  copy = { ["+"] = function(lines) _G.copied = lines end, ["*"] = function() end },
  paste = { ["+"] = function() return {} end, ["*"] = function() return {} end },
}
local report, steps = { keys = {} }, {}
local function step(f) steps[#steps + 1] = f end
local function run()
  local f = table.remove(steps, 1)
  if f then f(); vim.schedule(run) end
end
vim.api.nvim_create_autocmd("User", { pattern = "RexkitScrollbackLoaded", callback = function()
  step(function()
    report.cursor, report.top = vim.fn.line("."), vim.fn.line("w0")
    report.col, report.search = vim.fn.col("."), vim.fn.getreg("/")
    report.hlsearch, report.forward = vim.v.hlsearch, vim.v.searchforward
    report.rows = vim.fn.prevnonblank(vim.fn.line("$"))
    report.head = vim.api.nvim_buf_get_lines(0, 0, 2, false)
    report.text, report.tail = vim.fn.getline("."), vim.fn.getline(report.rows)
    report.status = vim.api.nvim_eval_statusline(vim.o.statusline, {}).str
    report.buftype = vim.bo.buftype
    for _, name in ipairs(vim.json.decode(vim.env.PROBE_HL or "[]")) do
      report.hl = report.hl or {}
      report.hl[name] = vim.api.nvim_get_hl(0, { name = name })
    end
  end)
  for _, keys in ipairs(vim.json.decode(vim.env.PROBE_KEYS)) do
    step(function()
      _G.copied = nil
      vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes(keys, true, false, true), "mx", false)
    end)
    step(function()
      report.keys[#report.keys + 1] = { cursor = vim.fn.line("."), col = vim.fn.col("."), mode = vim.fn.mode(), copied = _G.copied or vim.NIL }
    end)
  end
  step(function() io.stdout:write(vim.json.encode(report)); vim.cmd("qa!") end)
  vim.schedule(run)
end })
`

type keyResult struct {
	Cursor int      `json:"cursor"`
	Col    int      `json:"col"`
	Mode   string   `json:"mode"`
	Copied []string `json:"copied"`
}

type report struct {
	Cursor  int         `json:"cursor"`
	Top     int         `json:"top"`
	Col     int         `json:"col"`
	Search  string      `json:"search"` // the / register
	HLS     int         `json:"hlsearch"`
	Forward int         `json:"forward"` // v:searchforward
	Rows    int         `json:"rows"`
	Head    []string    `json:"head"` // the first two buffer rows
	Text    string      `json:"text"` // the cursor's row
	Tail    string      `json:"tail"` // the last row
	Status  string      `json:"status"`
	Buftype string      `json:"buftype"`
	Keys    []keyResult `json:"keys"`
	// Highlights are the groups viewer.highlights asked for.
	Highlights map[string]highlight `json:"hl"`
}

// highlight is a group as nvim_get_hl reports it: colours as 0xrrggbb.
type highlight struct {
	Fg   *int   `json:"fg"`
	Bg   *int   `json:"bg"`
	Bold bool   `json:"bold"`
	Link string `json:"link"`
}

// view runs the profile in a headless Neovim (an 80×24 screen: 23 rows of
// capture above the status line) and presses keys in order.
func view(t *testing.T, content, start string, keys ...string) report {
	t.Helper()
	return viewWith(t, viewer{start: start}, content, keys...)
}

// viewer is how view runs the profile.
type viewer struct {
	start   string
	dropped int    // $REXKIT_SCROLLBACK's dropped
	newer   int    // its newer
	line    int    // its line
	search  string // its search
	columns int    // the screen width, if not 80
	lua     string // run before the profile loads
	// colors is $REXKIT_SCROLLBACK's colour table; highlights are the
	// groups to report.
	colors     map[string]string
	highlights []string
}

func viewWith(t *testing.T, v viewer, content string, keys ...string) report {
	t.Helper()
	bin, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim is not installed")
	}
	dir := t.TempDir()
	write := func(name, data string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	profile, page, probeFile := write("scrollback.lua", string(Scrollback)), write("page.txt", content), write("probe.lua", probe)
	colors := v.colors
	if colors == nil {
		colors = map[string]string{}
	}
	settings, _ := json.Marshal(map[string]any{"file": page, "start": v.start, "dropped": v.dropped, "newer": v.newer, "line": v.line, "search": v.search,
		"dark": true, "colors": colors})
	groups, _ := json.Marshal(append([]string{}, v.highlights...))
	pressed, _ := json.Marshal(append([]string{}, keys...))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--headless", "-u", profile, "-i", "NONE", "-n", "--noplugin", "--cmd", "luafile "+probeFile)
	// Keep Neovim off the user's own config, data, state (its log) and cache:
	// later duplicates in Env win.
	xdg := t.TempDir()
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+filepath.Join(xdg, "config"),
		"XDG_DATA_HOME="+filepath.Join(xdg, "data"),
		"XDG_STATE_HOME="+filepath.Join(xdg, "state"),
		"XDG_CACHE_HOME="+filepath.Join(xdg, "cache"),
		"XDG_CONFIG_DIRS="+filepath.Join(xdg, "config-dirs"),
		"XDG_DATA_DIRS="+filepath.Join(xdg, "data-dirs"),
		"NVIM_LOG_FILE="+filepath.Join(xdg, "nvim.log"),
		"REXKIT_SCROLLBACK="+string(settings), "PROBE_KEYS="+string(pressed), "PROBE_HL="+string(groups))
	if v.lua != "" {
		cmd.Env = append(cmd.Env, "PROBE_LUA="+v.lua)
	}
	if v.columns > 0 {
		cmd.Env = append(cmd.Env, "PROBE_COLUMNS="+strconv.Itoa(v.columns))
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("nvim: %v\n%s", err, out)
	}
	var r report
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("probe output %q: %v", out, err)
	}
	if len(r.Keys) != len(keys) {
		t.Fatalf("probe saw %d key steps, want %d", len(r.Keys), len(keys))
	}
	return r
}

// numbered is n coloured lines "line 1" … "line n".
func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "\x1b[3%dmline %d\x1b[m\n", 1+i%7, i)
	}
	return b.String()
}

func TestStartPositions(t *testing.T) {
	// 100 lines in a 23-row window.
	tests := []struct {
		start       string
		cursor, top int
	}{
		{"bottom", 100, 78}, // the last screenful, like the pane
		{"top", 1, 1},
		{"page-up", 79, 57}, // ⌃B: 23 rows up, less 2 of overlap
		{"half-up", 89, 67}, // ⌃U: half a page (11 rows) up
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			r := view(t, numbered(100), tt.start)
			if r.Buftype != "terminal" {
				t.Errorf("buftype = %q, want a terminal buffer (it renders the colours)", r.Buftype)
			}
			if r.Rows != 100 || r.Cursor != tt.cursor || r.Top != tt.top {
				t.Errorf("rows %d cursor %d top %d, want 100, %d, %d", r.Rows, r.Cursor, r.Top, tt.cursor, tt.top)
			}
		})
	}
}

func TestStatusLine(t *testing.T) {
	// From the bottom the hint is the upward search; from the top, /.
	// Each key is a cap (" ? " on the keycap colour), then its label.
	for start, search := range map[string]string{"bottom": " ?  search up", "half-up": " ?  search up", "top": " /  search"} {
		r := view(t, numbered(100), start)
		for _, want := range []string{" Scrollback   ", search, " v  select", " y  copy", " q  close", "/100 "} {
			if !strings.Contains(r.Status, want) {
				t.Errorf("%s: status line %q lacks %q", start, r.Status, want)
			}
		}
	}
}

func TestStatusLineKeepsAGapBeforeTheCounter(t *testing.T) {
	// At these widths a hint used to fit exactly and touch "100/100".
	for _, cols := range []int{33, 36, 40, 46, 50, 56} {
		r := viewWith(t, viewer{start: "bottom", columns: cols}, numbered(100))
		status := strings.TrimRight(r.Status, " ")
		if !strings.HasSuffix(status, "   100/100") {
			t.Errorf("%d columns: status line %q has no gap before the counter", cols, r.Status)
		}
		if !strings.HasPrefix(r.Status, " Scrollback") {
			t.Errorf("%d columns: status line %q lost its title", cols, r.Status)
		}
	}
}

// TestHighlights: the profile draws with the colour table Go passes: the
// capture in the pane's text colour on no background of its own (Rex's
// surface shows), the status line on the tray's, matches and selections in
// fills of their own.
func TestHighlights(t *testing.T) {
	names := []string{"fg", "muted", "subtle", "term_fg", "tray", "keycap", "accent", "accent_solid",
		"on_accent", "ok", "err", "sel", "sel_fg", "find", "flash"}
	colors := map[string]string{}
	for i, n := range names {
		colors[n] = fmt.Sprintf("#0000%02x", i+1) // a distinct colour per token
	}
	type want struct {
		fg, bg string
		bold   bool
	}
	wants := map[string]want{
		"Normal":       {"term_fg", "", false},
		"NormalNC":     {"term_fg", "", false},
		"StatusLine":   {"fg", "tray", false},
		"StatusLineNC": {"muted", "tray", false},
		"RexkitTitle":  {"fg", "tray", true},
		"RexkitKey":    {"fg", "keycap", false},
		"RexkitLabel":  {"muted", "tray", false},
		"RexkitMeta":   {"subtle", "tray", false},
		"RexkitMatch":  {"accent", "tray", true},
		"RexkitOK":     {"ok", "tray", true},
		"RexkitErr":    {"err", "tray", true},
		"Visual":       {"sel_fg", "sel", false},
		"Search":       {"fg", "find", false},
		"CurSearch":    {"on_accent", "accent_solid", true},
		"YankFlash":    {"fg", "flash", false},
	}
	var groups []string
	for g := range wants {
		groups = append(groups, g)
	}
	r := viewWith(t, viewer{start: "bottom", colors: colors, highlights: append(groups, "IncSearch")}, numbered(10))
	token := func(c *int) string {
		if c == nil {
			return ""
		}
		return names[*c-1]
	}
	for g, w := range wants {
		h := r.Highlights[g]
		if got := (want{token(h.Fg), token(h.Bg), h.Bold}); got != w {
			t.Errorf("%s = %+v, want %+v", g, got, w)
		}
	}
	if l := r.Highlights["IncSearch"].Link; l != "CurSearch" {
		t.Errorf("IncSearch links to %q", l)
	}
}

// longLines is n coloured lines: "000001 " and 190 characters of fill, so
// 197 cells (3 rows at 80 columns) of "z", or 387 (5 rows) of "日".
func longLines(n int, fill string) string {
	var b strings.Builder
	tail := strings.Repeat(fill, 190/len([]rune(fill)))
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "\x1b[3%dm%06d %s\x1b[m\n", 1+i%7, i, tail)
	}
	return b.String()
}

var omittedNote = regexp.MustCompile(`^… (\d+) lines of older output not shown$`)

// checkTrimmed checks that the first row notes the left-out lines and the
// second is the start of the oldest line kept, the one right after them.
func checkTrimmed(t *testing.T, r report, total int) {
	t.Helper()
	if len(r.Head) != 2 {
		t.Fatalf("head = %q", r.Head)
	}
	m := omittedNote.FindStringSubmatch(r.Head[0])
	if m == nil {
		t.Fatalf("row 1 = %q, want the note on left-out lines", r.Head[0])
	}
	n, _ := strconv.Atoi(m[1])
	if n <= 0 || n >= total {
		t.Fatalf("note says %d lines left out of %d", n, total)
	}
	if want := fmt.Sprintf("%06d ", n+1); !strings.HasPrefix(r.Head[1], want) {
		t.Errorf("row 2 = %q, want the oldest kept line %q…", r.Head[1][:min(20, len(r.Head[1]))], want)
	}
	if r.Rows > 100000 {
		t.Errorf("%d rows: over the terminal's limit, so some were dropped silently", r.Rows)
	}
}

func TestWrappedLinesStayWithinTheRowLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("feeds 8 MB into a terminal buffer")
	}
	// 40k lines × 3 rows = 120k rows: past the 100k a terminal buffer keeps.
	r := view(t, longLines(40000, "z"), "top", "Y")
	checkTrimmed(t, r, 40000)
	// Copies still rejoin the wrapped rows of the lines kept.
	if got := r.Keys[0].Copied; len(got) < 1 || !strings.HasPrefix(got[0], "… ") || strings.Contains(got[0], "\x1b") {
		t.Errorf("Y on row 1 copied %q, want the plain note", got)
	}
}

func TestWideCharactersStayWithinTheRowLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("feeds 11 MB into a terminal buffer")
	}
	// Double-width characters, which may wrap a cell early.
	checkTrimmed(t, view(t, longLines(40000, "日"), "top"), 40000)
}

func TestTrimmingAddsToGoNote(t *testing.T) {
	if testing.Short() {
		t.Skip("feeds 8 MB into a terminal buffer")
	}
	// Go already left out 500 lines (its note is line 1); the profile replaces
	// that note with one that counts both.
	content := "\x1b[2;3m… 500 lines of older output not shown\x1b[m\n" + longLines(40000, "z")
	r := viewWith(t, viewer{start: "top", dropped: 500}, content)
	m := omittedNote.FindStringSubmatch(r.Head[0])
	if m == nil {
		t.Fatalf("row 1 = %q, want the note", r.Head[0])
	}
	n, _ := strconv.Atoi(m[1])
	if want := fmt.Sprintf("%06d ", n-500+1); !strings.HasPrefix(r.Head[1], want) {
		t.Errorf("note says %d left out; row 2 = %.20q, want %q…", n, r.Head[1], want)
	}
	if strings.Contains(r.Head[1], "older output") {
		t.Errorf("two notes: %q", r.Head)
	}
}

func TestShortPagesAreNotTrimmed(t *testing.T) {
	// Go's note stays as it is when everything else fits.
	content := "\x1b[2;3m… 9 lines of older output not shown\x1b[m\n" + numbered(3)
	r := viewWith(t, viewer{start: "top", dropped: 9}, content)
	if r.Head[0] != "… 9 lines of older output not shown" || r.Head[1] != "line 1" || r.Rows != 4 {
		t.Errorf("head %q, rows %d", r.Head, r.Rows)
	}
}

func TestCopiesRejoinWrappedLines(t *testing.T) {
	long := strings.Repeat("x", 100) + "END" // wraps into rows 2 and 3 at 80 columns
	content := "short\n\x1b[31m" + long + "\x1b[m\nlast\n"
	tests := []struct {
		keys string
		want []string
	}{
		{"2GVjy", []string{long}},                   // both rows: one line
		{"3GY", []string{long}},                     // Y on the continuation row: the whole line
		{"1GV3jy", []string{"short", long, "last"}}, // rows 1–4
		{"1Gyy", []string{"short"}},
	}
	for _, tt := range tests {
		t.Run(tt.keys, func(t *testing.T) {
			got := view(t, content, "top", tt.keys).Keys[0].Copied
			// Linewise yanks end with an empty element for the final newline.
			if len(got) > 0 && got[len(got)-1] == "" {
				got = got[:len(got)-1]
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("copied %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCopiesWorkWithoutYankFlash(t *testing.T) {
	long := strings.Repeat("x", 100) + "END"
	content := "short\n" + long + "\nlast\n"
	for name, lua := range map[string]string{
		// Neovim 0.10 has vim.highlight, older ones neither.
		"no vim.hl":   `rawset(vim, "hl", false); rawset(vim, "highlight", false)`,
		"0.10":        `rawset(vim, "hl", false)`,
		"flash fails": `rawset(vim, "hl", { on_yank = function() error("boom") end })`,
	} {
		t.Run(name, func(t *testing.T) {
			r := viewWith(t, viewer{start: "top", lua: lua}, content, "2GVjy")
			got := r.Keys[0].Copied
			if len(got) == 0 || got[0] != long {
				t.Errorf("copied %q, want the rejoined line", got)
			}
		})
	}
}

func TestNeverEntersTerminalMode(t *testing.T) {
	r := view(t, numbered(40), "top", "i", "a", "A", "I")
	for i, k := range r.Keys {
		if k.Mode != "n" || k.Cursor != 1 {
			t.Errorf("key %d: mode %q cursor %d, want normal mode on line 1", i, k.Mode, k.Cursor)
		}
	}
}

func TestOpenAtALineWithASearch(t *testing.T) {
	long := strings.Repeat("x", 100) // two rows at 80 columns
	content := "one\n" + long + "\nthe panic is here, panic\nfour\nno match\n" + numbered(60)
	r := viewWith(t, viewer{start: "bottom", line: 3, search: `\V\cpanic`}, content, "n", "n", "N")
	// Line 3 starts on row 4: line 2 wraps. The cursor is on its first match.
	if r.Cursor != 4 || r.Col != 5 {
		t.Errorf("cursor at row %d col %d, want row 4 col 5", r.Cursor, r.Col)
	}
	if r.Search != `\V\cpanic` || r.HLS != 1 || r.Forward != 1 {
		t.Errorf("search %q hlsearch %d forward %d, want the pattern, highlighted, searching down", r.Search, r.HLS, r.Forward)
	}
	if !strings.Contains(r.Status, " n N  next, previous") || !strings.Contains(r.Status, "match 1/2") {
		t.Errorf("status line %q", r.Status)
	}
	// n goes on to the next match, then wraps to the first; N goes back.
	want := [][2]int{{4, 20}, {4, 5}, {4, 20}}
	for i, k := range r.Keys {
		if [2]int{k.Cursor, k.Col} != want[i] {
			t.Errorf("after key %d at row %d col %d, want %v", i, k.Cursor, k.Col, want[i])
		}
	}
	// The line is in the middle of the window, not at its edge.
	if r.Top > 1 {
		t.Errorf("top %d: the line scrolled out of the middle", r.Top)
	}
}

func TestOpenAtALineFarDown(t *testing.T) {
	r := viewWith(t, viewer{start: "bottom", line: 50, search: `\V\cline 50`}, numbered(100))
	if r.Cursor != 50 || r.Col != 1 {
		t.Errorf("cursor at row %d col %d, want 50, 1", r.Cursor, r.Col)
	}
	// zz: the line sits in the middle of the 23 rows.
	if r.Top != 50-11 {
		t.Errorf("top %d, want %d", r.Top, 50-11)
	}
}

func TestOpenAtALineWithoutAMatchOnIt(t *testing.T) {
	// The pattern is not on the line: the cursor stays on it.
	r := viewWith(t, viewer{start: "bottom", line: 30, search: `\V\cnowhere`}, numbered(100))
	if r.Cursor != 30 || r.Search != `\V\cnowhere` {
		t.Errorf("cursor %d search %q", r.Cursor, r.Search)
	}
}

func TestOpenAtAnOldLineKeepsTheLinesAroundIt(t *testing.T) {
	if testing.Short() {
		t.Skip("feeds 8 MB into a terminal buffer")
	}
	// 40k lines × 3 rows: the trim would leave out line 1 and line 20000;
	// it keeps the lines around them instead, and notes the rest.
	r := viewWith(t, viewer{start: "bottom", line: 1, search: `\V\c000001`}, longLines(40000, "z"))
	if r.Cursor != 1 || r.Col != 1 || r.Search != `\V\c000001` {
		t.Errorf("cursor at row %d col %d, search %q: want line 1", r.Cursor, r.Col, r.Search)
	}
	checkNewerNote(t, r, 40000)
	r = viewWith(t, viewer{start: "bottom", line: 20000, search: `\V\c020000`}, longLines(40000, "z"))
	if !strings.HasPrefix(r.Text, "020000 ") || r.Col != 1 {
		t.Errorf("cursor on %.20q col %d, want line 20000", r.Text, r.Col)
	}
	if !omittedNote.MatchString(r.Head[0]) {
		t.Errorf("row 1 = %q, want the note on older lines", r.Head[0])
	}
	checkNewerNote(t, r, 40000)
	// A line among the newest: the trim keeps the newest as ever.
	r = viewWith(t, viewer{start: "top", line: 39990, search: `\V\c039990`}, longLines(40000, "z"))
	if r.Cursor != r.Rows-3*10-2 || r.Col != 1 {
		t.Errorf("cursor at row %d col %d of %d, want the start of line 39990", r.Cursor, r.Col, r.Rows)
	}
	if strings.Contains(r.Tail, "newer output") {
		t.Errorf("a note on newer lines: %q", r.Tail)
	}
}

// Go kept the lines around the line to open at (its notes are the first and
// last lines): the trim adds to both counts.
func TestTrimmingAddsToGoNotesAroundALine(t *testing.T) {
	if testing.Short() {
		t.Skip("feeds 8 MB into a terminal buffer")
	}
	content := "\x1b[2;3m… 500 lines of older output not shown\x1b[m\n" + longLines(40000, "z") +
		"\x1b[2;3m… 700 lines of newer output not shown\x1b[m\n"
	r := viewWith(t, viewer{start: "bottom", dropped: 500, newer: 700, line: 20001, search: `\V\c020000`}, content)
	if !strings.HasPrefix(r.Text, "020000 ") {
		t.Errorf("cursor on %.20q, want line 20000 of the capture", r.Text)
	}
	m := omittedNote.FindStringSubmatch(r.Head[0])
	if m == nil {
		t.Fatalf("row 1 = %q, want the note", r.Head[0])
	}
	if n, _ := strconv.Atoi(m[1]); !strings.HasPrefix(r.Head[1], fmt.Sprintf("%06d ", n-500+1)) {
		t.Errorf("note says %d left out; row 2 = %.20q", n, r.Head[1])
	}
	if n := checkNewerNote(t, r, 40700); n <= 700 {
		t.Errorf("the note on newer lines says %d, want more than Go's 700", n)
	}
}

func TestShortPagesKeepGoNotes(t *testing.T) {
	content := "\x1b[2;3m… 9 lines of older output not shown\x1b[m\n" + numbered(3) + "\x1b[2;3m… 4 lines of newer output not shown\x1b[m\n"
	r := viewWith(t, viewer{start: "bottom", dropped: 9, newer: 4, line: 3, search: `\V\cline 2`}, content)
	if r.Head[0] != "… 9 lines of older output not shown" || r.Tail != "… 4 lines of newer output not shown" || r.Cursor != 3 {
		t.Errorf("head %q, tail %q, cursor %d", r.Head, r.Tail, r.Cursor)
	}
}

var newerNote = regexp.MustCompile(`^… (\d+) lines of newer output not shown$`)

// checkNewerNote checks that the last row notes the newer lines left out,
// and returns how many it says.
func checkNewerNote(t *testing.T, r report, total int) int {
	t.Helper()
	m := newerNote.FindStringSubmatch(r.Tail)
	if m == nil {
		t.Fatalf("last row = %q, want the note on newer lines", r.Tail)
	}
	n, _ := strconv.Atoi(m[1])
	if n <= 0 || n >= total {
		t.Errorf("note says %d newer lines left out of %d", n, total)
	}
	if r.Rows > 100000 {
		t.Errorf("%d rows: over the terminal's limit", r.Rows)
	}
	return n
}
