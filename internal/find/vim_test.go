package find

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// vimFinds runs each pattern over each line in a headless Neovim and
// returns, per pattern and line, the byte range of the first match (-1, -1
// for none).
func vimFinds(t *testing.T, patterns, lines []string) [][][2]int {
	t.Helper()
	bin, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim is not installed")
	}
	const script = `
local input = vim.json.decode(vim.env.FIND_INPUT)
local out = {}
for i, pat in ipairs(input.patterns) do
  out[i] = {}
  for j, line in ipairs(input.lines) do
    local m = vim.fn.matchstrpos(line, pat)
    out[i][j] = { m[2], m[3] }
  end
end
io.stdout:write(vim.json.encode(out))
`
	dir := t.TempDir()
	path := filepath.Join(dir, "find.lua")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"patterns": patterns, "lines": lines})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--clean", "--headless", "-l", path)
	cmd.Env = append(os.Environ(), "FIND_INPUT="+string(input), "XDG_STATE_HOME="+dir, "XDG_DATA_HOME="+dir, "NVIM_LOG_FILE="+filepath.Join(dir, "log"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("nvim: %v\n%s", err, out)
	}
	var res [][][2]int
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("nvim said %q: %v", out, err)
	}
	return res
}

// TestVimFindsWhatGoFinds: the pattern Scrollback searches with finds the
// first match Search showed, on every line.
func TestVimFindsWhatGoFinds(t *testing.T) {
	lines := []string{
		`12:31:08 INF request method=POST path=/api/projects/42/run status=503 dur=12ms request_id=8f2c41d7`,
		`12:31:09 ERR panic recovered request_id=c7d03e19 status=500`,
		`panic: runtime error: invalid memory address or nil pointer dereference [recovered]`,
		`        /opt/homebrew/Cellar/go/1.27.0/libexec/src/runtime/panic.go:792 +0x124`,
		`{"level":"error","ts":"12:31:08","msg":"job failed","job":4411}`,
		`PANIC in Panic: a.c abc a*c [x] $HOME ~/x \d back\slash 100%`,
		`論文の主な貢献は三つあります: キャッシュ ÄRGER ärger`,
		`GET /api/me 200 · POST /api/run 202 · get lower`,
		"tab\there  and  spaces",
		"tab\t1 and [", "only 1", "abcdefghijk", "😀x 7x",
		``,
	}
	queries := []Query{
		{Text: "panic"}, {Text: "Panic"}, {Text: "panic", MatchCase: true},
		{Text: "a.c"}, {Text: "a*c"}, {Text: "[x]"}, {Text: "$HOME"}, {Text: "~/x"}, {Text: `\d`},
		{Text: `back\slash`}, {Text: "100%"}, {Text: "キャッシュ"}, {Text: "ärger"}, {Text: "/api/"},
		{Text: `status=5\d\d`, Regex: true},
		{Text: `request_id=[0-9a-f]{8}`, Regex: true},
		{Text: `^(GET|POST) /api`, Regex: true},
		{Text: `(?i)get`, Regex: true},
		{Text: `"msg":"[^"]+"`, Regex: true},
		{Text: `\bpanic\b`, Regex: true},
		{Text: `[\d.]+:\d+`, Regex: true},
		{Text: `\s{2,}`, Regex: true},
		{Text: `\S+\.go`, Regex: true},
		{Text: `job.*?\d`, Regex: true},
		{Text: `x?y?z?ab+c`, Regex: true},
		{Text: `0x[[:xdigit:]]+`, Regex: true},
		{Text: `\$HOME|~`, Regex: true},
		{Text: `a\.c|\[x\]`, Regex: true},
		{Text: `\x41BC|ärger`, Regex: true},
		{Text: `(?s)re.overed`, Regex: true},
		{Text: `\t`, Regex: true},
		// a control before a digit, a character past the BMP
		{Text: `[\t1]`, Regex: true},
		{Text: `\t1`, Regex: true},
		{Text: `[😀0-9]x`, Regex: true},
		{Text: `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)`, Regex: true}, // more groups than Vim numbers
	}
	var patterns []string
	var compiled []*Matcher
	for _, q := range queries {
		p, ok := q.Vim()
		if !ok {
			t.Fatalf("%+v has no Vim pattern", q)
		}
		m, err := q.Compile()
		if err != nil {
			t.Fatal(err)
		}
		patterns, compiled = append(patterns, p), append(compiled, m)
	}
	vim := vimFinds(t, patterns, lines)
	for i, q := range queries {
		for j, line := range lines {
			want := [2]int{-1, -1}
			if spans := compiled[i].Spans(line); len(spans) > 0 {
				want = spans[0]
			}
			if got := vim[i][j]; got != want {
				t.Errorf("%+v (Vim %q) on %q: Vim finds %v, Go %v", q, patterns[i], line, got, want)
			}
		}
	}
}
