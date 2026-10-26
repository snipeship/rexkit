package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

// testNow is the fixed clock of model tests and goldens.
var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// fixtureTree builds a folder with the edge cases Files must handle, all
// with fixed modification times, and returns (home, root) where root is
// home/project.
func fixtureTree(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "project")
	dirs := []string{"cmd/rexkit", "docs", "empty folder", "node_modules/leftpad", ".github", "project space ' $(not-a-command)"}
	for _, d := range dirs {
		mustMkdir(t, filepath.Join(root, d))
	}
	files := map[string]string{
		"cmd/rexkit/main.go":        "package main\n\nfunc main() {}\n",
		"go.mod":                    "module fixture\n\ngo 1.27\n",
		"README.md":                 "# Fixture\n",
		"node_modules/leftpad/i.js": "x\n",
		".env":                      "secret\n",
		"escapes.txt":               "hello \x1b]52;c;Y2xpcA==\x07 \x1b[31mred\x1b[0m\n",
		"evil\x1b[31mname.txt":      "",
		"bidi\u202etxt.exe":         "",
		"docs/guide.md":             "guide\n",
		"empty.txt":                 "",
		"binary.bin":                "\x00\x01\x02",
		"an-extremely-long-name-that-keeps-going-and-going.md": "x",
	}
	for name, content := range files {
		mustWrite(t, filepath.Join(root, name), content)
	}
	mustSymlink(t, "README.md", filepath.Join(root, "readme-link.md"))
	mustSymlink(t, "missing-target", filepath.Join(root, "broken-link"))
	if err := syscall.Mkfifo(filepath.Join(root, "trap.png"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := testNow.Add(-3 * time.Hour)
	_ = filepath.WalkDir(home, func(p string, _ os.DirEntry, _ error) error {
		_ = os.Chtimes(p, stamp, stamp)
		return nil
	})
	return home, root
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, p string) {
	t.Helper()
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

// noTools is a runner for which every helper program fails, as if git,
// zoxide, pbcopy and open were missing.
func noTools(context.Context, string, string, string, ...string) ([]byte, error) {
	return nil, errors.New("not available in tests")
}

// recorder wraps the fake Rex and keeps the requests that create terminals.
type recorder struct {
	*rexfake.Fake
	windows []rex.NewWindowRequest
	splits  []rex.NewSplitRequest
}

func (r *recorder) NewWindow(ctx context.Context, s string, req rex.NewWindowRequest) (rex.Created, error) {
	r.windows = append(r.windows, req)
	return r.Fake.NewWindow(ctx, s, req)
}

func (r *recorder) NewSplit(ctx context.Context, s string, req rex.NewSplitRequest) (rex.Created, error) {
	r.splits = append(r.splits, req)
	return r.Fake.NewSplit(ctx, s, req)
}

// testEnv is a model wired to fakes.
type testEnv struct {
	m      *model
	rex    *recorder
	dirs   state.Dirs
	origin string
	state  string
}

type envOption func(*config)

func demoMode(c *config) { c.launcher.api = nil; c.demo = true }

// newEnv builds a model browsing dir, already loaded and sized w×h.
func newEnv(t *testing.T, home, dir string, w, h int, opts ...envOption) *testEnv {
	t.Helper()
	t.Setenv(ui.ThemeEnv, "dark")
	fake := &recorder{Fake: rexfake.New()}
	session, window, origin := fake.AddSession("work")
	dirs := state.Dirs{State: t.TempDir(), Cache: t.TempDir(), Config: t.TempDir()}
	stateDir := t.TempDir()
	now := func() time.Time { return testNow }
	cfg := config{
		dir: dir, home: home, dirs: dirs, run: noTools, now: now, imageBase: 1000,
		launcher: launcher{
			api: fake, session: session, origin: origin, originWindow: window, stateDir: stateDir,
			dirs: dirs, shell: "/bin/zsh", editor: []string{"/usr/bin/vi"}, home: home, run: noTools, now: now,
		},
	}
	for _, o := range opts {
		o(&cfg)
	}
	env := &testEnv{m: newModel(cfg), rex: fake, dirs: dirs, origin: origin, state: stateDir}
	env.send(t, tea.WindowSizeMsg{Width: w, Height: h})
	env.do(t, env.m.load())
	return env
}

// send delivers a message and runs the commands it causes.
func (e *testEnv) send(t *testing.T, msg tea.Msg) {
	t.Helper()
	_, cmd := e.m.Update(msg)
	e.do(t, cmd)
}

// press sends keys: "enter", "alt+enter", "ctrl+f", or text to type.
func (e *testEnv) press(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		e.send(t, uitest.Key(t, k))
	}
}

func (e *testEnv) typeText(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		e.send(t, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// do runs cmd and everything it leads to. Commands run concurrently in
// waves; results are delivered in order. Timers that fire later than cmdWait
// (toast expiry) are dropped, as are spinner ticks and raw terminal output.
func (e *testEnv) do(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	wave := []tea.Cmd{cmd}
	for round := 0; len(wave) > 0; round++ {
		if round > 200 {
			t.Fatal("commands never settle")
		}
		var next []tea.Cmd
		for _, msg := range runAll(wave) {
			switch msg := msg.(type) {
			case nil, ui.SpinnerTickMsg, busyTickMsg, tea.RawMsg, tea.QuitMsg:
			case tea.BatchMsg:
				next = append(next, msg...)
			default:
				_, c := e.m.Update(msg)
				next = append(next, c)
			}
		}
		wave = next
	}
}

const cmdWait = 500 * time.Millisecond

// runAll runs commands concurrently and returns their messages in order;
// a command still running after cmdWait yields nil.
func runAll(cmds []tea.Cmd) []tea.Msg {
	results := make([]chan tea.Msg, len(cmds))
	for i, c := range cmds {
		results[i] = make(chan tea.Msg, 1)
		if c == nil {
			results[i] <- nil
			continue
		}
		go func() { results[i] <- c() }()
	}
	deadline := time.After(cmdWait)
	out := make([]tea.Msg, len(cmds))
	expired := false
	for i, ch := range results {
		if !expired {
			select {
			case out[i] = <-ch:
				continue
			case <-deadline:
				expired = true
			}
		}
		select {
		case out[i] = <-ch:
		default:
		}
	}
	return out
}

// screen renders the model as plain text lines.
func (e *testEnv) screen() []string {
	return strings.Split(ansi.Strip(e.m.View().Content), "\n")
}

// selection is the washed (selected) rows of the screen, as plain text.
func (e *testEnv) selection() []string {
	return washed(e.m.View().Content, e.m.theme.C.SelBg)
}

func (e *testEnv) selectedLabel() string {
	r, ok := e.m.selected()
	if !ok {
		return ""
	}
	return r.e.label
}

// selectPath moves the selection to the row for path.
func (e *testEnv) selectPath(t *testing.T, path string) {
	t.Helper()
	i := e.m.indexOf(path)
	if i < 0 {
		t.Fatalf("%s is not listed", path)
	}
	e.m.here = false
	e.m.list.Select(i)
	e.do(t, e.m.selectionChanged())
}
