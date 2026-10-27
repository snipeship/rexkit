package scrollback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/ui"
)

// float is a session whose pane opened the tool in a floating layer; the
// tool's $REX_SESSION and $REX_BLOCK point at its block.
type float struct {
	f           *rexfake.Fake
	session     string
	pane, tool  string
	toolLayerID string
}

func newFloat(t *testing.T) float {
	t.Helper()
	ctx := context.Background()
	f := rexfake.New()
	s, _, pane := f.AddSession("work")
	layer, err := f.NewLayer(ctx, s, rex.NewLayerRequest{Focus: true, Layout: rex.Command("Scrollback", "", "rexkit", "scrollback")})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REX_SESSION", s)
	t.Setenv("REX_BLOCK", layer.BlockIDs[0])
	f.ResetCalls()
	return float{f: f, session: s, pane: pane, tool: layer.BlockIDs[0], toolLayerID: layer.LayerID}
}

func (fl float) closes() int {
	n := 0
	for _, c := range fl.f.Calls() {
		if c.Method == "session.close_layer" && slices.Contains(c.Args, fl.toolLayerID) {
			n++
		}
	}
	return n
}

// TestRunClosesTheFloat: the viewer runs in the tool's float, which closes
// afterwards (popup.Run), and the state directory is claimed for the float.
func TestRunClosesTheFloat(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	fl := newFloat(t)
	dir := t.TempDir()
	sys := newFakeSystem(t, newScreen(t), "nvim")
	c := config{demo: true, start: startBottom, popup: popup.Options{OriginBlock: fl.pane, State: dir}}
	if err := run(context.Background(), c, sys.system, popup.RunOptions{LazySelf: true, API: fl.f}); err != nil {
		t.Fatal(err)
	}
	if len(sys.ran) != 1 || fl.closes() != 1 {
		t.Errorf("viewer runs %d, close_layer calls: %v", len(sys.ran), fl.f.Methods())
	}
	if _, err := os.Stat(filepath.Join(dir, "owner.json")); err != nil {
		t.Errorf("state dir not claimed: %v", err)
	}
}

// TestRunByHand: started in an ordinary pane, the viewer runs and closes no
// layer.
func TestRunByHand(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	fl := newFloat(t)
	t.Setenv("REX_BLOCK", fl.pane)
	sys := newFakeSystem(t, newScreen(t), "nvim")
	if err := run(context.Background(), config{demo: true}, sys.system, popup.RunOptions{LazySelf: true, API: fl.f}); err != nil {
		t.Fatal(err)
	}
	if len(sys.ran) != 1 || slices.Contains(fl.f.Methods(), "session.close_layer") {
		t.Errorf("viewer runs %d, calls %v", len(sys.ran), fl.f.Methods())
	}
}

func TestSourceFor(t *testing.T) {
	api := rexfake.New()
	s, _, b := api.AddSession("work")
	api.SetCapture(b, "from the origin\r\n")
	t.Setenv("REX_BLOCK", b)
	tests := []struct {
		name    string
		c       config
		session string
		want    string
		fails   bool
	}{
		{"origin block", config{popup: popup.Options{OriginBlock: b}}, s, "from the origin\r\n", false},
		{"defaults to its own pane", config{}, s, "from the origin\r\n", false},
		{"no session", config{}, "", "", true},
		{"demo needs no pane", config{demo: true}, "", "~/code/rexkit", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := sourceFor(tt.c, api, tt.session, "")
			if (err != nil) != tt.fails {
				t.Fatalf("err = %v", err)
			}
			if tt.fails {
				return
			}
			got, err := src(context.Background())
			if err != nil || !strings.Contains(got, tt.want) {
				t.Errorf("source gave %.40q, %v", got, err)
			}
		})
	}
}

// fakeSystem records the viewer command and checks the spool while the
// viewer "runs".
type fakeSystem struct {
	system
	ran      []command
	sawPage  string
	sawFiles []string
}

func newFakeSystem(t *testing.T, s *screen, programs ...string) *fakeSystem {
	fs := &fakeSystem{}
	fs.system = system{
		stdin: s.in, stdout: s, stderr: s,
		tempDir: t.TempDir(),
		screen:  []tea.ProgramOption{tea.WithWindowSize(60, 12)},
		find: func(name string, _ ...string) (string, bool) {
			return "/bin/" + name, slices.Contains(programs, name)
		},
	}
	fs.run = func(_ context.Context, c command) error {
		fs.ran = append(fs.ran, c)
		entries, _ := os.ReadDir(filepath.Dir(fs.page(t, c)))
		for _, e := range entries {
			fs.sawFiles = append(fs.sawFiles, e.Name())
		}
		data, _ := os.ReadFile(fs.page(t, c))
		fs.sawPage = string(data)
		return nil
	}
	return fs
}

// page finds the spooled page in a viewer command.
func (fs *fakeSystem) page(t *testing.T, c command) string {
	if strings.HasSuffix(c.argv[0], "less") {
		return c.argv[len(c.argv)-1]
	}
	var s nvimSettings
	if err := json.Unmarshal([]byte(strings.TrimPrefix(c.env[0], "REXKIT_SCROLLBACK=")), &s); err != nil {
		t.Fatal(err)
	}
	return s.File
}

func (fs *fakeSystem) spoolsLeft(t *testing.T) []os.DirEntry {
	entries, err := os.ReadDir(fs.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func colourful(context.Context) (string, error) { return "\x1b[32mok\x1b[m\x1b]0;title\x07\r\n", nil }

func TestShowRunsTheViewerOnASpooledCopy(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "light")
	tests := []struct {
		name     string
		programs []string
		viewer   string
		files    []string
	}{
		{"nvim", []string{"nvim", "less"}, "/bin/nvim", []string{"scrollback.lua", "scrollback.txt"}},
		{"less fallback", []string{"less"}, "/bin/less", []string{"scrollback.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScreen(t)
			sys := newFakeSystem(t, s, tt.programs...)
			if err := show(context.Background(), config{start: startTop}, colourful, sys.system); err != nil {
				t.Fatal(err)
			}
			if len(sys.ran) != 1 || sys.ran[0].argv[0] != tt.viewer {
				t.Fatalf("ran %+v", sys.ran)
			}
			if sys.sawPage != "\x1b[32mok\x1b[m\x1b[m\n" {
				t.Errorf("viewer saw %q, want the sanitized capture", sys.sawPage)
			}
			if !slices.Equal(sys.sawFiles, tt.files) {
				t.Errorf("spool held %v, want %v", sys.sawFiles, tt.files)
			}
			if left := sys.spoolsLeft(t); len(left) != 0 {
				t.Errorf("spool not removed: %v", left)
			}
		})
	}
}

func TestShowPassesTheThemeAndStart(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "light")
	s := newScreen(t)
	sys := newFakeSystem(t, s, "nvim")
	if err := show(context.Background(), config{start: startPageUp}, colourful, sys.system); err != nil {
		t.Fatal(err)
	}
	var settings nvimSettings
	_ = json.Unmarshal([]byte(strings.TrimPrefix(sys.ran[0].env[0], "REXKIT_SCROLLBACK=")), &settings)
	if settings.Dark || settings.Start != startPageUp || settings.Colors["fg"] != hex(ui.NewTheme(false).C.Fg) {
		t.Errorf("settings = %+v", settings)
	}
}

func TestShowReportsFailures(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	unreadable := func(context.Context) (string, error) { return "", rex.ErrNotFound }
	tests := []struct {
		name     string
		src      source
		programs []string
		runErr   error
		err      string // what the error says
		card     string // what the card says; it starts with a capital
	}{
		{"pane gone", unreadable, []string{"nvim"}, nil, "reading the pane", "That pane is gone."},
		{"no viewer", colourful, nil, nil, "either nvim nor less", "either nvim nor less"},
		{"viewer won't start", colourful, []string{"nvim"}, startError{os.ErrPermission}, "starting the viewer", "Starting the viewer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScreen(t)
			sys := newFakeSystem(t, s, tt.programs...)
			if tt.runErr != nil {
				sys.run = func(context.Context, command) error { return tt.runErr }
			}
			s.press(t, tt.card, "x")
			err := show(context.Background(), config{start: startBottom}, tt.src, sys.system)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("err = %v, want it to mention %q", err, tt.err)
			}
			if !strings.Contains(s.String(), tt.card) {
				t.Errorf("the error card never showed %q", tt.card)
			}
			if left := sys.spoolsLeft(t); len(left) != 0 {
				t.Errorf("spool not removed: %v", left)
			}
		})
	}
}
