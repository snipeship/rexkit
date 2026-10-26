package toast

import (
	"errors"
	"flag"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
)

// Main runs `rexkit toast` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("toast", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	message := fs.String("message", "", "the `TEXT` to show; {key:ctrl+p e} in it is drawn as keycaps")
	kindName := fs.String("kind", "info", "`KIND` of news: ok, info, warn or err")
	ttl := fs.Duration("ttl", 0, "how long to stay (default 2.5s ok, 4s info, 6s warn, 8s err)")
	demo := fs.Bool("demo", false, "show a sample toast")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	kind, err := parseKind(*kindName)
	if err != nil {
		return err
	}
	if *demo && *message == "" {
		*message = "Parked api in Scratch · {key:ctrl+p e} brings it back"
	}
	if *message == "" {
		return errors.New("toast: --message is required")
	}
	m := newModel(kind, *message, *ttl)
	if *demo {
		_, err := tea.NewProgram(m).Run()
		return err
	}
	// The toast never has focus: it holds no keys, and closing its layer
	// cannot move focus. It needs nothing of its own layer either.
	return popup.Run("toast", po, popup.RunOptions{NoKeys: true, LazySelf: true}, func(*popup.Env) error {
		// Lua opens the toast as a strip along the top of the window; it
		// draws in the strip's top Rows (the strip can round up a row).
		_, err := tea.NewProgram(popup.Stage(m, popup.Card{Rows: Rows, TopRight: true})).Run()
		return err
	})
}
