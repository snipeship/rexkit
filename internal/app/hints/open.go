package hints

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// runner runs a helper program (pbcopy, open, git, docker) with stdin and
// returns its standard output. Arguments are argv elements, never shell
// text. Tests substitute fakes.
type runner func(ctx context.Context, dir, stdin, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, dir, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(stdin)
	cmd.WaitDelay = 200 * time.Millisecond
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && stderr.Len() > 0 {
			msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
			return out, fmt.Errorf("%s: %s", filepath.Base(name), msg)
		}
		return out, err
	}
	return out, nil
}

// starter starts a program and leaves it running on its own (a hint's
// command). Tests substitute fakes.
type starter func(dir string, argv ...string) error

// startCommand starts argv in dir, detached from the tool, which closes
// right after: in a process group of its own, its output dropped.
func startCommand(dir string, argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// lookPath finds a program on $PATH, then where Homebrew and other
// installers put programs a GUI app's $PATH can lack.
func lookPath(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// worker carries out picks against Rex. Picks that open something put a
// terminal in a split beside the pane the tool covers, in its folder.
type worker struct {
	api     rex.API
	session string
	origin  string   // the pane the tool covers
	size    rex.Size // its size, which decides the split's direction
	dir     string   // its working directory
	home    string
	editor  []string // the editor's argv; the file (and its line) go after it
	command []string // a hint key's command (ActCommand); the text goes after it
	run     runner
	start   starter
	look    func(name string) (string, bool)
}

// outcome is what a pick did, for the toast that says so: none when what
// it did shows by itself (a split).
type outcome struct {
	kind ui.ToastKind
	text string
}

func ok(format string, args ...any) outcome {
	return outcome{ui.ToastOK, fmt.Sprintf(format, args...)}
}

// do carries out p.
func (w worker) do(ctx context.Context, p Pick) outcome {
	text := p.Match.Text
	switch p.Action {
	case ActPaste:
		if err := w.api.Write(ctx, w.session, w.origin, w.pasted(ctx, text)); err != nil {
			return failed("Couldn't paste", err)
		}
		return ok("Pasted %s", shown(text))
	case ActOpen:
		return w.open(ctx, p)
	case ActCommand:
		return w.runHint(text)
	}
	return w.copy(ctx, text, "")
}

// runHint starts the hint key's command with text as its last argument,
// as Alacritty's hint commands run: in the background, in the pane's
// folder. A program named without a path is looked up as lookPath does.
func (w worker) runHint(text string) outcome {
	if len(w.command) == 0 {
		return failed("Couldn't run", errors.New("the hint has no command"))
	}
	argv := append(slices.Clone(w.command), text)
	name := filepath.Base(argv[0])
	if !strings.Contains(argv[0], "/") {
		found, ok := w.look(argv[0])
		if !ok {
			return failed("Couldn't run "+shown(name), errors.New("it is not installed"))
		}
		argv[0] = found
	}
	if err := w.start(w.dir, argv...); err != nil {
		return failed("Couldn't run "+shown(name), err)
	}
	return ok("Ran %s with %s", shown(name), shown(text))
}

// pasted is text as a terminal pastes it: between the bracketed paste
// marks when the program in the pane turned them on (an editor, an agent,
// a shell's line editor), so that it reads the text as text, not as keys
// (Neovim would run the "6cc" of a SHA as a command). Otherwise, or when
// Rex can't say, the text is typed as it is.
func (w worker) pasted(ctx context.Context, text string) string {
	var res struct {
		Modes []struct {
			Name string `json:"name"`
			Set  bool   `json:"set"`
		} `json:"modes"`
	}
	if err := w.api.BlockCall(ctx, w.session, w.origin, "modes", nil, &res); err != nil {
		return text
	}
	for _, m := range res.Modes {
		if m.Name == "bracketed_paste" && m.Set {
			return "\x1b[200~" + text + "\x1b[201~"
		}
	}
	return text
}

// copy puts text on the clipboard; why says why a pick that was to open
// it copied it instead.
func (w worker) copy(ctx context.Context, text, why string) outcome {
	if _, err := w.run(ctx, "", text, "pbcopy"); err != nil {
		return failed("Couldn't copy", err)
	}
	if why != "" {
		return outcome{ui.ToastInfo, why + " · copied " + shown(text)}
	}
	return ok("Copied %s", shown(text))
}

// open opens what p picked by its kind; what nothing opens is copied.
func (w worker) open(ctx context.Context, p Pick) outcome {
	text := p.Match.Text
	switch p.Match.Kind {
	case KindURL:
		if _, err := w.run(ctx, "", "", "open", text); err != nil {
			return failed("Couldn't open", err)
		}
		return ok("Opened %s", shown(text))
	case KindPath:
		return w.edit(ctx, text)
	case KindSHA:
		return w.hex(ctx, text)
	case KindPod:
		kubectl, found := w.look("kubectl")
		if !found {
			return w.copy(ctx, text, "kubectl is not installed")
		}
		argv := []string{kubectl, "describe", "pod", text}
		if p.Namespace != "" {
			argv = append(argv, "--namespace="+p.Namespace) // one argument: never read as a flag
		}
		// describe prints and ends: a pager keeps it on screen until q.
		return w.split(ctx, text, paged("", argv...)...)
	case KindUUID:
		return w.copy(ctx, text, "Nothing opens an id")
	case KindText:
		return w.copy(ctx, text, "Nothing opens it")
	}
	return w.copy(ctx, text, "Nothing opens an address") // KindIP
}

// paged is argv with its output (and errors) in less, which keeps it on
// screen until q, as a split closes when its command ends. flags are less's
// own ("+F" follows the output as it grows).
func paged(flags string, argv ...string) []string {
	script := `"$@" 2>&1 | LESS=R less`
	if flags != "" {
		script += " " + flags
	}
	return append([]string{"/bin/sh", "-c", script, "sh"}, argv...)
}

// hex opens a hex id: a commit of the pane's repository in git show, else
// a container in docker logs -f, else it is copied.
func (w worker) hex(ctx context.Context, id string) outcome {
	if git, found := w.look("git"); found {
		if _, err := w.run(ctx, w.dir, "", git, "cat-file", "-e", id+"^{commit}"); err == nil {
			// LESS set (without F) keeps git's pager open on a short commit
			// too: the split stays until q.
			return w.split(ctx, id, "/usr/bin/env", "LESS=R", git, "show", id)
		}
	}
	if docker, found := w.look("docker"); found {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := w.run(cctx, "", "", docker, "container", "inspect", "--format", "{{.Id}}", id)
		cancel()
		if err == nil {
			// A stopped container's logs end at once: the pager keeps them.
			return w.split(ctx, id, paged("+F", docker, "logs", "-f", id)...)
		}
	}
	return w.copy(ctx, id, "Not a commit or container here")
}

// edit opens a path in the editor at its line and column.
func (w worker) edit(ctx context.Context, text string) outcome {
	name, line, col := splitPosition(text)
	path, found := w.resolve(ctx, name)
	if !found {
		return w.copy(ctx, text, "No such file here")
	}
	argv := append(append([]string(nil), w.editor...), positionArgs(w.editor, path, line, col)...)
	return w.split(ctx, filepath.Base(path), argv...)
}

// split runs argv in a new terminal beside the pane, in its folder, and
// focuses it: right of a wide pane, below a tall one, as Rex's own split
// in the longer direction does.
func (w worker) split(ctx context.Context, label string, argv ...string) outcome {
	dir := rex.Horizontal
	cw, ch := max(w.size.CellWidthPx, 1), max(w.size.CellHeightPx, 2)
	if w.size.Columns*cw < w.size.Rows*ch {
		dir = rex.Vertical
	}
	_, err := w.api.NewSplit(ctx, w.session, rex.NewSplitRequest{
		AnchorID: w.origin, Direction: dir, Side: "after", Ratio: 0.5,
		Layout: rex.Command(sanitize.Label(label), w.dir, argv...), Focus: true,
	})
	if err != nil {
		return failed("Couldn't open a split", err)
	}
	return outcome{}
}

func failed(what string, err error) outcome {
	return outcome{ui.ToastErr, what + ": " + rex.UserMessage(err)}
}

// shown is a picked text as a toast shows it: one line, its braces made
// harmless (a toast draws {key:…} as keycaps).
func shown(text string) string { return ui.ToastEscape(sanitize.Label(text)) }

// splitPosition splits "path:line:col" (both optional) into its parts; 0
// for a part that is not there.
func splitPosition(text string) (path string, line, col int) {
	path = text
	var nums []int
	for range 2 {
		i := strings.LastIndexByte(path, ':')
		if i < 0 {
			break
		}
		n, err := strconv.Atoi(path[i+1:])
		if err != nil || n <= 0 {
			break
		}
		nums = append([]int{n}, nums...)
		path = path[:i]
	}
	if len(nums) > 0 {
		line = nums[0]
	}
	if len(nums) > 1 {
		col = nums[1]
	}
	return path, line, col
}

// positionArgs are the arguments that open path at line and column in the
// editor whose argv is editor: the forms Vim, Emacs, nano, Helix, Kakoune
// and micro take. Another editor gets the file alone.
func positionArgs(editor []string, path string, line, col int) []string {
	if line <= 0 || len(editor) == 0 {
		return []string{path}
	}
	at := strconv.Itoa(line)
	switch strings.TrimSuffix(filepath.Base(editor[0]), ".exe") {
	case "nvim", "vim", "mvim", "gvim":
		if col > 0 {
			return []string{fmt.Sprintf("+call cursor(%d,%d)", line, col), path}
		}
		return []string{"+" + at, path}
	case "vi", "view", "ex":
		return []string{"+" + at, path}
	case "emacs", "emacsclient", "kak":
		if col > 0 {
			at += ":" + strconv.Itoa(col)
		}
		return []string{"+" + at, path}
	case "nano":
		if col > 0 {
			at += "," + strconv.Itoa(col)
		}
		return []string{"+" + at, path}
	case "hx", "helix", "micro":
		if col > 0 {
			at += ":" + strconv.Itoa(col)
		}
		return []string{path + ":" + at}
	}
	return []string{path}
}

// searchEntries and searchFor bound the look for a file named by a path
// relative to a folder other than the pane's (go test names a package's files).
const (
	searchEntries = 20000
	searchFor     = time.Second
)

// resolve finds the file a path on screen names: absolute or from ~, else
// relative to the pane's folder, then to its repository's top (git prints
// paths from there), then the one file under the pane's folder whose path
// ends with it (a test failure names a file in its package's folder).
func (w worker) resolve(ctx context.Context, name string) (string, bool) {
	switch {
	case name == "~" || strings.HasPrefix(name, "~/"):
		name = filepath.Join(w.home, strings.TrimPrefix(name, "~"))
	case !filepath.IsAbs(name) && w.dir == "":
		return "", false
	}
	if filepath.IsAbs(name) {
		return name, exists(name)
	}
	if p := filepath.Join(w.dir, name); exists(p) {
		return p, true
	}
	if git, found := w.look("git"); found {
		if out, err := w.run(ctx, w.dir, "", git, "rev-parse", "--show-toplevel"); err == nil {
			if p := filepath.Join(strings.TrimSpace(string(out)), name); exists(p) {
				return p, true
			}
		}
	}
	return findUnder(ctx, w.dir, name)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findUnder looks under root for the one file whose path ends with name,
// skipping hidden folders and node_modules, within searchEntries entries
// and searchFor.
func findUnder(ctx context.Context, root, name string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, searchFor)
	defer cancel()
	suffix := string(filepath.Separator) + filepath.Clean(name)
	var found []string
	seen := 0
	stop := errors.New("stop")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen++; seen > searchEntries || ctx.Err() != nil || len(found) > 1 {
			return stop
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, suffix) {
			found = append(found, path)
		}
		return nil
	})
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}
