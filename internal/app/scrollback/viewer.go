package scrollback

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"rexkit/internal/ui"
)

// Where Neovim and less live when $PATH (a block's environment can be
// minimal) does not have them.
var (
	nvimFallbacks = []string{"/opt/homebrew/bin/nvim", "/usr/local/bin/nvim"}
	lessFallbacks = []string{"/usr/bin/less", "/opt/homebrew/bin/less"}
)

// finder locates a program: name on $PATH, else the first fallback that is
// an executable file.
type finder func(name string, fallbacks ...string) (string, bool)

func findProgram(name string, fallbacks ...string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}
	for _, path := range fallbacks {
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return path, true
		}
	}
	return "", false
}

// program is the viewer chosen for this run.
type program struct {
	path string
	less bool // the less -R fallback instead of Neovim
}

func pickProgram(override string, find finder) (program, error) {
	if override != "" {
		return program{path: override}, nil
	}
	if path, ok := find("nvim", nvimFallbacks...); ok {
		return program{path: path}, nil
	}
	if path, ok := find("less", lessFallbacks...); ok {
		return program{path: path, less: true}, nil
	}
	return program{}, fmt.Errorf("neither nvim nor less is installed")
}

// command is a viewer to run: argv plus additions to the environment. It is
// never a shell string; paths stay single arguments whatever they contain.
type command struct {
	argv []string
	env  []string
}

// nvimSettings is what nvim/scrollback.lua reads from $REXKIT_SCROLLBACK.
type nvimSettings struct {
	File    string            `json:"file"`
	Start   string            `json:"start"`
	Dropped int               `json:"dropped"`          // older lines the page already left out (its first line notes them)
	Newer   int               `json:"newer,omitempty"`  // newer lines it left out to keep Line (its last line notes them)
	Line    int               `json:"line,omitempty"`   // the page line to open at, instead of start
	Search  string            `json:"search,omitempty"` // a Vim pattern to search for, so n and N go on
	Dark    bool              `json:"dark"`
	Colors  map[string]string `json:"colors"`
}

// nvimCommand runs Neovim with only the scrollback profile: no user config,
// no plugins, no shada or swap files. The page's own counts of lines left
// out are what the profile adds to the ones it trims; at, when set, opens
// at a line with a search instead of at start.
func nvimCommand(bin, profile, file, start string, pg page, at opening, t ui.Theme) command {
	// Strings, numbers, a bool and a string map always marshal.
	settings, _ := json.Marshal(nvimSettings{File: file, Start: start, Dropped: pg.dropped, Newer: pg.newer, Line: at.line,
		Search: at.pattern, Dark: t.Dark, Colors: palette(t)})
	return command{
		argv: []string{bin, "-u", profile, "-i", "NONE", "-n", "--noplugin"},
		env:  []string{"REXKIT_SCROLLBACK=" + string(settings)},
	}
}

// lessPrompt mirrors the Neovim status line: title, hints, line/total. Like
// the profile, it offers the upward search (? in less too, escaped as \? in a
// prompt) unless the viewer opens at the top.
func lessPrompt(start string) string {
	search := `\? search up`
	if start == startTop {
		search = "/ search"
	}
	return " Scrollback   " + search + "   q close   ?lb%lb?L/%L.."
}

// lessCommand is the fallback viewer: colours via -R, secure mode (no shell
// escapes or editing), no history file. It opens where Neovim would: at the
// end (G), then a page (b) or half a page (u) up; or at line, a third of
// the way down the screen.
func lessCommand(bin, file, start string, line int) command {
	argv := []string{bin, "-R", "-K", "-~", "-Ps" + lessPrompt(start)}
	switch {
	case line > 0:
		argv = append(argv, "-j.3", "+"+strconv.Itoa(line)+"g")
	case start == startTop:
	case start == startPageUp:
		argv = append(argv, "+Gb")
	case start == startHalfUp:
		argv = append(argv, "+Gu")
	default: // bottom, as in the profile
		argv = append(argv, "+G")
	}
	return command{
		argv: append(argv, "--", file),
		env:  []string{"LESSSECURE=1", "LESSHISTFILE=-"},
	}
}

// palette maps the theme's tokens to the names the profile uses (its
// highlight table is in nvim/scrollback.lua): the capture in the pane's own
// text colour on Rex's surface, the status line on the tray's.
func palette(t ui.Theme) map[string]string {
	p := t.C
	return map[string]string{
		"fg": hex(p.Fg), "muted": hex(p.FgMuted), "subtle": hex(p.FgSubtle),
		"term_fg": hex(p.WellFg), "tray": hex(p.Tray), "keycap": hex(p.TrayKeyCap),
		"accent": hex(p.Accent), "accent_solid": hex(p.AccentSolid), "on_accent": hex(p.AccentOnSolid),
		"ok": hex(p.OK), "err": hex(p.Err),
		"sel": hex(p.WellSelBg), "sel_fg": hex(p.WellSelFg), "find": hex(p.WellFindBg), "flash": hex(p.WellFlashBg),
	}
}

func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// startError means a viewer could not be started at all (as opposed to one
// that ran and failed, which has already shown its own errors).
type startError struct{ err error }

func (e startError) Error() string { return "starting the viewer: " + e.err.Error() }
func (e startError) Unwrap() error { return e.err }

// runCommand runs a viewer on the terminal and waits for it. Cancelling ctx
// (the float was closed under us) asks the viewer to quit.
func runCommand(ctx context.Context, c command, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	cmd.Env = append(os.Environ(), c.env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return startError{err}
	}
	return cmd.Wait()
}
