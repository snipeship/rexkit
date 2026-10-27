package scrollback

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"rexkit/internal/find"
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
	// page is a capture to show instead of reading the pane (Search hands
	// over the one it searched); line, when positive, is the line of it to
	// open at, and search what to search for there, so n and N go on.
	page   string
	line   int
	search find.Query
}

func parseArgs(args []string) (config, error) {
	var c config
	fs := flag.NewFlagSet("scrollback", flag.ContinueOnError)
	c.popup.RegisterFlags(fs)
	fs.StringVar(&c.start, "start", startBottom, "where the viewer opens: "+strings.Join(starts, ", "))
	fs.BoolVar(&c.plain, "plain", false, "capture text only, without colours")
	fs.BoolVar(&c.demo, "demo", false, "show a generated colourful sample instead of a pane")
	fs.StringVar(&c.nvim, "nvim", "", "Neovim `binary` (default: nvim on $PATH or from Homebrew)")
	fs.StringVar(&c.page, "page", "", "show this capture `FILE` (text and SGR colours) instead of the pane's; a page Search handed over is removed once read")
	fs.IntVar(&c.line, "line", 0, "open at line `N` (from 1) instead of --start")
	fs.StringVar(&c.search.Text, "search", "", "search for `TEXT`, so n and N find the next and previous match")
	fs.BoolVar(&c.search.Regex, "regex", false, "--search is a regular expression (Go's syntax)")
	fs.BoolVar(&c.search.MatchCase, "match-case", false, "--search matches case (else it does once it has a capital)")
	if err := popup.ParseFlags(fs, args); err != nil {
		return c, err
	}
	if fs.NArg() > 0 {
		return c, fmt.Errorf("scrollback: unexpected argument %q", fs.Arg(0))
	}
	if c.line < 0 {
		return c, fmt.Errorf("scrollback: --line must be 1 or more, not %d", c.line)
	}
	if !slices.Contains(starts, c.start) {
		return c, fmt.Errorf("scrollback: --start must be one of %s, not %q", strings.Join(starts, ", "), c.start)
	}
	return c, nil
}
