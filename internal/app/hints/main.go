package hints

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Timeouts: reading the pane, a pick's work, the toast that reports it.
const (
	loadTimeout   = 5 * time.Second
	workTimeout   = 10 * time.Second
	notifyTimeout = 5 * time.Second
)

// notifyAction is the Lua action that shows a toast in the window
// (lua/tools.lua).
const notifyAction = "rexkit_notify"

// Messages for picks that end without one.
const (
	nothingText = "Nothing to pick in this pane"
	notPaneText = "Hints works on a terminal pane"
)

// Main runs `rexkit hints` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("hints", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	dir := fs.String("directory", "", "the pane's working `DIR`: the folder a path on it is relative to")
	editorJSON := fs.String("editor-json", "", "editor command as a JSON array, e.g. `[\"nvim\"]` (default vi)")
	demo := fs.Bool("demo", false, "label a sample shell screen; a pick only says what it would do")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("hints: unexpected argument %q", fs.Arg(0))
	}
	editor, err := editorArgv(*editorJSON)
	if err != nil {
		return err
	}
	if *demo {
		return runDemo()
	}
	if os.Getenv("REX_BLOCK") == "" {
		return fmt.Errorf("hints: %w", popup.OutsideRexError("hints"))
	}
	home, _ := os.UserHomeDir()
	return popup.Run("hints", po, popup.RunOptions{Title: "Hints", LazySelf: true}, func(env *popup.Env) error {
		session := env.Popup.Options().Session
		if po.OriginBlock == "" {
			return errors.New("--origin-block is required")
		}
		w := &worker{api: env.API, session: session, origin: po.OriginBlock, dir: *dir, home: home,
			editor: editor, run: runCommand, look: lookPath}
		say := func(o outcome) { notify(env.API, session, o) }
		load := func() tea.Msg {
			ctx, cancel := context.WithTimeout(env.Ctx, loadTimeout)
			defer cancel()
			src, err := read(ctx, env.API, session, po.OriginBlock)
			if err == nil {
				// Over a zoomed pane the float opened without focus, which
				// would have ended the zoom: the pane is read, take it now.
				_ = env.Popup.TakeFocus(ctx)
			}
			src.Dir, src.Home = *dir, home
			w.size = src.Size
			return loadedMsg{src: src, err: err}
		}
		work := func(p Pick) {
			ctx, cancel := context.WithTimeout(env.Ctx, workTimeout)
			defer cancel()
			say(w.do(ctx, p))
		}
		m := newModel(options{dismissOnBlur: po.DismissOnBlur, stateDir: po.State}, ui.NewScheme(), load, work)
		m.self = claim(env)
		final, err := tea.NewProgram(m).Run()
		if fm, ok := final.(*model); ok {
			m = fm
		}
		switch {
		case err != nil:
			return err
		case errors.Is(m.err, errNotPane):
			say(outcome{ui.ToastInfo, notPaneText})
		case m.err != nil:
			say(failed("Couldn't read the pane", m.err))
		case m.ready() && len(m.matches) == 0:
			say(outcome{ui.ToastInfo, nothingText})
		}
		return nil
	})
}

// errNotPane: the tool was opened over something that is not a terminal
// pane of the window's tiled layer.
var errNotPane = errors.New("not a terminal pane")

// read reads the pane the tool covers: its screen, size and cursor
// (rex.API.Screens, one round trip).
func read(ctx context.Context, api rex.API, session, block string) (Source, error) {
	screens, err := api.Screens(ctx, session, block)
	if err != nil {
		return Source{}, err
	}
	for _, p := range screens.Panes {
		if p.BlockID != block {
			continue
		}
		if p.Error != "" {
			return Source{}, errors.New(p.Error)
		}
		screen := p.Screen
		return Source{Screen: func(int, int) string { return screen }, Size: p.Size, Cursor: p.Cursor}, nil
	}
	return Source{}, errNotPane
}

// claim finds the tool's own layer and records it as the owner of the
// state directory, after the first frame (popup.RunOptions.LazySelf).
func claim(env *popup.Env) tea.Cmd {
	return func() tea.Msg {
		self, err := env.ResolveSelf(env.Ctx)
		return selfMsg{floating: err == nil && self.Floating}
	}
}

// notify shows o as a toast in the session's window (Lua's app.notify,
// which places it as every rexkit toast). Best effort: the pick is done.
func notify(api rex.API, session string, o outcome) {
	actor, ok := api.(interface {
		Action(ctx context.Context, session, name string, args, out any) error
	})
	if o.text == "" || !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	_ = actor.Action(ctx, session, notifyAction, map[string]string{"title": "Hints", "message": o.text, "kind": kindName(o.kind)}, nil)
}

func kindName(k ui.ToastKind) string {
	switch k {
	case ui.ToastOK:
		return "ok"
	case ui.ToastWarn:
		return "warn"
	case ui.ToastErr:
		return "err"
	}
	return "info"
}

// editorArgv is the editor command from --editor-json (vi without one). Rex
// starts it without a shell, so its program is looked up on $PATH now.
func editorArgv(jsonArg string) ([]string, error) {
	argv := []string{"vi"}
	if jsonArg != "" {
		if err := json.Unmarshal([]byte(jsonArg), &argv); err != nil || len(argv) == 0 || argv[0] == "" {
			return nil, errors.New(`hints: --editor-json must be a JSON array of strings, like ["nvim"]`)
		}
	}
	if !filepath.IsAbs(argv[0]) {
		if p, err := exec.LookPath(argv[0]); err == nil {
			argv[0] = p
		}
	}
	return argv, nil
}

// runDemo labels the demo shell in this terminal; a pick prints what it
// would have done.
func runDemo() error {
	src := demoSource(demoShell)
	m := newModel(options{demo: true}, ui.NewScheme(), func() tea.Msg { return loadedMsg{src: src} }, nil)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(*model); ok && fm.picked != nil {
		fmt.Printf("would %s %s\n", fm.picked.Action, fm.picked.Match.Text)
	}
	return nil
}
