package search

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
)

// Main runs `rexkit search` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	demo := fs.Bool("demo", false, "search sample sessions instead of Rex's")
	query := fs.String("query", "", "start with `TEXT` in the field, searched at once (Go to's ⌃F)")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if *demo {
		return runDemo(*query)
	}
	return run(po, *query)
}

func run(po popup.Options, query string) error {
	return popup.Run("search", po, popup.RunOptions{Title: "Search"}, func(pe *popup.Env) error {
		if pe.DirsErr != nil {
			return pe.DirsErr
		}
		self := pe.Self
		cfg := config{
			api:      pe.API,
			origin:   origin{session: self.SessionID, window: po.OriginWindow, block: po.OriginBlock},
			query:    query,
			self:     self.BlockID,
			client:   po.Client,
			stateDir: po.State,
			blur:     po.DismissOnBlur,
			floating: self.Floating,
			home:     homeDir(),
			spool:    pe.Dirs.NewToolDir,
			copy:     pbcopy,
			close:    pe.Popup.Close,
		}
		if cfg.origin.window == "" {
			cfg.origin.window = self.WindowID // a float opens over its origin's window
		}
		_, err := tea.NewProgram(newModel(pe.Ctx, cfg), tea.WithContext(pe.Ctx)).Run()
		return err
	})
}

// runDemo searches the demo world in an ordinary pane: nothing is read
// from Rex and ⏎ only says what it would do.
func runDemo(query string) error {
	fake, o := demoWorld(time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config{api: fake, origin: o, query: query, demo: true, home: demoHome}
	_, err := tea.NewProgram(newModel(ctx, cfg), tea.WithContext(ctx)).Run()
	return err
}

// pbcopy puts text on the clipboard.
func pbcopy(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
