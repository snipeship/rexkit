package scrollback

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"rexkit/internal/popup"
)

// Start positions: where the viewer opens.
const (
	startBottom = "bottom"  // the last screenful, as the pane showed it
	startTop    = "top"     // the oldest line
	startPageUp = "page-up" // one page above the bottom
	startHalfUp = "half-up" // half a page above the bottom
)

var starts = []string{startBottom, startTop, startPageUp, startHalfUp}

// config is the parsed command line.
type config struct {
	popup popup.Options
	start string
	plain bool   // capture text only, without colours
	demo  bool   // show a generated sample instead of a pane
	nvim  string // Neovim binary; empty to search for one
}

func parseArgs(args []string) (config, error) {
	var c config
	fs := flag.NewFlagSet("scrollback", flag.ContinueOnError)
	c.popup.RegisterFlags(fs)
	fs.StringVar(&c.start, "start", startBottom, "where the viewer opens: "+strings.Join(starts, ", "))
	fs.BoolVar(&c.plain, "plain", false, "capture text only, without colours")
	fs.BoolVar(&c.demo, "demo", false, "show a generated colourful sample instead of a pane")
	fs.StringVar(&c.nvim, "nvim", "", "Neovim `binary` (default: nvim on $PATH or from Homebrew)")
	if err := popup.ParseFlags(fs, args); err != nil {
		return c, err
	}
	if fs.NArg() > 0 {
		return c, fmt.Errorf("scrollback: unexpected argument %q", fs.Arg(0))
	}
	if !slices.Contains(starts, c.start) {
		return c, fmt.Errorf("scrollback: --start must be one of %s, not %q", strings.Join(starts, ", "), c.start)
	}
	return c, nil
}
