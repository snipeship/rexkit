package files

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/state"
)

// Main runs `rexkit files` with the arguments after the subcommand.
func Main(args []string) error {
	opts, err := parseFlags(args, os.Getenv)
	if err != nil {
		return err
	}
	if opts.demo {
		dirs, err := state.Default()
		if err != nil {
			return err
		}
		return runProgram(newModel(opts.config(dirs)), popup.Card{})
	}
	return popup.Run("files", opts.popup, popup.RunOptions{}, func(env *popup.Env) error {
		if env.DirsErr != nil {
			return env.DirsErr
		}
		cfg := opts.config(env.Dirs)
		cfg.launcher.api = env.API
		cfg.launcher.session = env.Self.SessionID
		return runProgram(newModel(cfg), opts.popup.Card)
	})
}

// runProgram runs the model in its card of the float (popup.Stage) and
// removes its images from the terminal.
func runProgram(m *model, card popup.Card) error {
	_, err := tea.NewProgram(popup.Stage(m, card)).Run()
	if clear := m.gfx.clear(); clear != "" {
		_, _ = os.Stdout.WriteString(clear)
	}
	return err
}

// options are the parsed command line.
type options struct {
	popup  popup.Options
	demo   bool
	dir    string
	editor []string
	shell  string
	notice string // why the start folder was replaced
}

func parseFlags(args []string, getenv func(string) string) (options, error) {
	var o options
	fs := flag.NewFlagSet("files", flag.ContinueOnError)
	o.popup.RegisterFlags(fs)
	fs.BoolVar(&o.demo, "demo", false, "run without Rex: verbs describe what they would do")
	fs.StringVar(&o.dir, "directory", "", "folder to start in (default: the current folder)")
	editor := fs.String("editor-json", "", "editor command as a JSON array, e.g. `[\"nvim\"]` (default $VISUAL, $EDITOR, vi)")
	fs.StringVar(&o.shell, "shell", "", "shell `PATH` for new terminals (default: Rex's)")
	if err := popup.ParseFlags(fs, args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("files: unexpected argument %q", fs.Arg(0))
	}
	var err error
	if o.editor, err = editorArgv(*editor, getenv); err != nil {
		return o, err
	}
	o.dir, o.notice = startFolder(o.dir)
	return o, nil
}

// editorArgv is the editor command: --editor-json, else $VISUAL or $EDITOR
// split on spaces, else vi. The program is resolved on $PATH now, because
// Rex starts it directly, without a shell.
func editorArgv(jsonArg string, getenv func(string) string) ([]string, error) {
	var argv []string
	switch {
	case jsonArg != "":
		if err := json.Unmarshal([]byte(jsonArg), &argv); err != nil || len(argv) == 0 || argv[0] == "" {
			return nil, errors.New(`files: --editor-json must be a JSON array of strings, like ["nvim"]`)
		}
	case getenv("VISUAL") != "":
		argv = strings.Fields(getenv("VISUAL"))
	case getenv("EDITOR") != "":
		argv = strings.Fields(getenv("EDITOR"))
	default:
		argv = []string{"vi"}
	}
	if !filepath.IsAbs(argv[0]) {
		if p, err := exec.LookPath(argv[0]); err == nil {
			argv[0] = p
		}
	}
	return argv, nil
}

// startFolder resolves the folder to start in, falling back to home (with a
// notice) when it can't be listed.
func startFolder(dir string) (string, string) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		if err = checkFolder(abs); err == nil {
			return filepath.Clean(abs), ""
		}
	}
	home := homeDir()
	return home, fmt.Sprintf("Can't open %s: %s · showing ~", displayPath(dir, home), fsErrorText(err))
}

func (o options) config(dirs state.Dirs) config {
	home := homeDir()
	return config{
		dir:           o.dir,
		home:          home,
		dismissOnBlur: o.popup.DismissOnBlur,
		demo:          o.demo,
		notice:        o.notice,
		dirs:          dirs,
		run:           runCommand,
		now:           time.Now,
		imageBase:     randomInt(),
		launcher: launcher{
			origin:       o.popup.OriginBlock,
			originWindow: o.popup.OriginWindow,
			stateDir:     o.popup.State,
			dirs:         dirs,
			shell:        o.shell,
			editor:       o.editor,
			home:         home,
			run:          runCommand,
			now:          time.Now,
		},
	}
}

func randomInt() int {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return int(binary.BigEndian.Uint32(b[:]) >> 1)
}
