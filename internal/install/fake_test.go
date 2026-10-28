package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"rexkit/internal/rex"
)

// world is a temp home with a rexkit checkout and a fake rex, go and
// defaults behind Env.Run.
type world struct {
	t    *testing.T
	env  *Env
	out  *bytes.Buffer
	home string
	root string

	mu        sync.Mutex
	calls     [][]string
	reloads   int
	ctx       context.Context // of the rex call being answered
	reloadCtx []error         // ctx.Err() as each reload started
	onReload  func()          // runs inside each reload, as if Rex took a while

	checkFails  bool   // rex config check reports an error
	strictFails bool   // a module fails: an error only in a strict check, a missing module at runtime
	luaDiag     string // rex do rexkit_diagnostics output; empty = derived from the live config
	diagMissing bool   // the server has no rexkit_diagnostics action
	reloadFails int    // this many reloads report an error
	down        bool   // the server cannot be reached (the CLI still runs)
	shown       int    // what rex do rexkit_show_hidden shows
	showFails   bool   // rex do rexkit_show_hidden fails
	keymap      string // rex keymap --json output; empty = derived from the live config
	methods     []string
	apps        string // rex api call client.list output; empty = Rex Beta 0.1.0 (1541)
	remote      string // defaults read output; empty = the key does not exist
	tools       map[string]string
}

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

const testDefaults = `-- rexkit's settings: name, default and what it does.
return {
  { "shell", "/bin/zsh", "shell for new panes" },
  { "editor", { "nvim" }, "editor command" },
  { "resize_step", 0.1, "how far one resize key moves a border" },
  { "alt_shortcuts", true, "global ⌥ shortcuts" },
  { "compact_labels", true, "short action titles" },
  { "commit_via", "mode", "how ⏎ reaches an open tool", choices = { "mode", "global" } },
  { "hints", {}, "keys that open Hints for one thing" },
  { "hints_patterns", {}, "regular expressions Hints labels", empty = true },
  { "hints_alphabet", "", "the letters of Hints' labels" },
}
`

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	paths, err := PathsFrom(func(k string) string { return map[string]string{"HOME": home}[k] })
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, home: home, out: &bytes.Buffer{},
		methods: []string{"session.close_layer", "session.new_layer", "session.view"},
		tools:   map[string]string{"nvim": "/opt/homebrew/bin/nvim", "git": "/usr/bin/git"}}
	w.root = w.checkout("code/rexkit")
	w.env = &Env{
		Paths:    paths,
		Rex:      "rex",
		Go:       "go",
		Defaults: "defaults",
		Run:      w.run,
		LookPath: func(name string) (string, error) {
			if p, ok := w.tools[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Now: func() time.Time { return testNow },
		Out: w.out,
	}
	return w
}

// checkout creates a minimal rexkit checkout under the home directory.
func (w *world) checkout(rel string) string {
	root := filepath.Join(w.home, rel)
	w.write(filepath.Join(root, "go.mod"), "module rexkit\n\ngo 1.27.0\n")
	w.write(filepath.Join(root, "cmd", "rexkit", "main.go"), "package main\n")
	w.write(filepath.Join(root, "lua", "init.lua"), "return {}\n")
	w.write(filepath.Join(root, "lua", "defaults.lua"), testDefaults)
	return root
}

// packaged creates a packaged rexkit tree (Homebrew's libexec) under the
// home directory: the Lua layer and the binary, no sources.
func (w *world) packaged(rel string) string {
	root := filepath.Join(w.home, rel)
	w.write(filepath.Join(root, "bin", "rexkit"), "#!fake rexkit\n")
	w.write(filepath.Join(root, "lua", "init.lua"), "return {}\n")
	w.write(filepath.Join(root, "lua", "defaults.lua"), testDefaults)
	return root
}

func (w *world) write(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(path string) string {
	w.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func (w *world) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (w *world) install(opts Options) error {
	if opts.Root == "" {
		opts.Root = w.root
	}
	return w.env.Install(context.Background(), opts)
}

func (w *world) uninstall(opts UninstallOptions) error {
	return w.env.Uninstall(context.Background(), opts)
}

func (w *world) backups() []string {
	files, _ := filepath.Glob(filepath.Join(w.env.Paths.Backups(), "*"))
	return files
}

func (w *world) record() Record {
	w.t.Helper()
	rec, ok, err := w.env.readRecord()
	if err != nil || !ok {
		w.t.Fatalf("no install record: %v", err)
	}
	return rec
}

// run is the fake process runner.
func (w *world) run(ctx context.Context, argv []string, stdin []byte) (rex.Output, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, argv)
	switch {
	case argv[0] == "go":
		for i, a := range argv {
			if a == "-o" {
				return rex.Output{}, os.WriteFile(argv[i+1], []byte("#!fake rexkit\n"), 0o755)
			}
		}
	case strings.Contains(filepath.Base(argv[0]), "rexkit") && argv[1] == "version":
		return rex.Output{Stdout: []byte("rexkit test go1.27.0\n")}, nil
	case argv[0] == "defaults":
		if w.remote == "" {
			return rex.Output{ExitCode: 1, Stderr: []byte("does not exist")}, nil
		}
		return rex.Output{Stdout: []byte(w.remote + "\n")}, nil
	case argv[0] == "rex":
		w.ctx = ctx
		return w.rex(rexArgs(argv[1:]))
	}
	w.t.Errorf("unexpected command %q", argv)
	return rex.Output{ExitCode: 127}, nil
}

// rexArgs drops the CLI's global flags.
func rexArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--timeout" || args[i] == "-S":
			i++
		case strings.HasPrefix(args[i], "--autostart") || strings.HasPrefix(args[i], "--color"):
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func (w *world) rex(args []string) (rex.Output, error) {
	cmd := strings.Join(args[:min(2, len(args))], " ")
	if w.down && cmd != "config check" && cmd != "version" {
		if cmd == "config reload" {
			w.reloads++
		}
		return rex.Output{ExitCode: 4, Stderr: []byte("Error: connecting to Rex server: no such file")}, nil
	}
	switch cmd {
	case "config check":
		path := args[len(args)-1]
		data, _ := os.ReadFile(path)
		return w.load(path, w.checkFails || w.strictFails && strings.HasPrefix(string(data), strictPrefix)), nil
	case "do rexkit_diagnostics":
		return w.diagnostics(), nil
	case "do rexkit_show_hidden":
		if w.showFails {
			return rex.Output{ExitCode: 1, Stderr: []byte("Error: rexkit: no such tab")}, nil
		}
		return rex.Output{Stdout: []byte(`{"shown":` + strconv.Itoa(w.shown) + `}`)}, nil
	case "config reload":
		w.reloads++
		w.reloadCtx = append(w.reloadCtx, w.ctx.Err())
		if w.onReload != nil {
			w.onReload()
		}
		fail := w.reloadFails > 0
		w.reloadFails--
		return w.load(w.env.Paths.RexConfig, fail), nil
	case "keymap --json":
		km := w.keymap
		if km == "" {
			data, _ := os.ReadFile(w.env.Paths.RexConfig)
			km = keymapJSON(w.env.Paths.RexConfig, string(data))
		}
		return rex.Output{Stdout: []byte(km)}, nil
	case "api list":
		type method struct {
			Name string `json:"name"`
		}
		var list struct {
			Methods []method `json:"methods"`
		}
		for _, m := range w.methods {
			list.Methods = append(list.Methods, method{m})
		}
		data, _ := json.Marshal(list)
		return rex.Output{Stdout: data}, nil
	case "version":
		return rex.Output{Stdout: []byte("rex 0.0.1-test\napi: revision 7\n")}, nil
	case "api call":
		if args[2] == "client.list" {
			apps := w.apps
			if apps == "" {
				apps = `{"clients":[{"client_id":"client:cli","info":{"kind":"cli","name":"rex","version":"0.0.1"},"connection_state":"connected"},
					{"client_id":"client:app","info":{"kind":"app","name":"Rex Beta","version":"0.1.0 (1541)"},"connection_state":"connected"}]}`
			}
			return rex.Output{Stdout: []byte(apps)}, nil
		}
	}
	w.t.Errorf("unexpected rex %q", args)
	return rex.Output{ExitCode: 2}, nil
}

// load imitates Rex loading a config: a loader whose dofile target is
// missing fails, as does any config when fail is set.
func (w *world) load(path string, fail bool) rex.Output {
	data, err := os.ReadFile(path)
	r := ConfigReport{Path: path, Exists: err == nil, Bindings: 3, Modes: 1, Generation: 7}
	for _, target := range dofilePaths(string(data)) {
		if _, err := os.Stat(target); err != nil {
			fail = true
		}
	}
	if fail && r.Exists {
		r.Diagnostics = append(r.Diagnostics, rex.Diagnostic{Severity: "error", Source: "init.lua:1", Message: "boom"})
	}
	out, _ := json.Marshal(r)
	if len(r.Diagnostics) > 0 {
		return rex.Output{Stdout: out, Stderr: []byte("Error: the configuration has errors\n"), ExitCode: 1}
	}
	return rex.Output{Stdout: out}
}

// diagnostics imitates the Lua layer's rexkit_diagnostics action, which only
// a server running rexkit's loader has.
func (w *world) diagnostics() rex.Output {
	if w.luaDiag != "" {
		return rex.Output{Stdout: []byte(w.luaDiag)}
	}
	data, _ := os.ReadFile(w.env.Paths.RexConfig)
	if _, ok := managedRoot(string(data)); !ok || w.diagMissing {
		return rex.Output{ExitCode: 1, Stderr: []byte(`Error: no action named "rexkit_diagnostics". Run "rex actions" to list them.` + "\n")}
	}
	modules := slices.Clone(luaModules)
	warnings := []string{}
	if w.strictFails {
		modules = slices.DeleteFunc(modules, func(m string) bool { return m == "bindings" })
		warnings = append(warnings, "bindings.lua failed to load: bindings.lua:1: boom")
	}
	out, _ := json.Marshal(map[string]any{"modules": modules, "warnings": warnings})
	return rex.Output{Stdout: out}
}

func keymapJSON(path, content string) string {
	data, _ := json.Marshal(map[string]any{
		"bindings":    []rex.Binding{{Keys: []string{"ctrl+shift+space"}, Action: "rexkit_goto", Source: "tools.lua:1"}},
		"actions":     []rex.Action{{Name: "rexkit_goto", Kind: "lua"}},
		"modes":       []rex.Mode{{Name: "pane"}},
		"diagnostics": []rex.Diagnostic{},
		"config":      map[string]any{"path": path, "exists": content != "", "hash": sha256Hex(content)},
	})
	return string(data)
}

func (w *world) count(prefix string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, c := range w.calls {
		if strings.HasPrefix(strings.Join(rexArgs(c), " "), prefix) {
			n++
		}
	}
	return n
}
