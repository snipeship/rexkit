package toast

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/attend"
	"rexkit/internal/popup"
	"rexkit/internal/state"
	"rexkit/internal/ui/sanitize"
)

// stdout is where --measure writes.
var stdout io.Writer = os.Stdout

// Main runs `rexkit toast` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("toast", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	title := fs.String("title", "rexkit", "the `TITLE` in the float's header: who the news is from")
	message := fs.String("message", "", "the `TEXT` to show; {key:ctrl+p e} in it is drawn as keycaps")
	kindName := fs.String("kind", "info", "`KIND` of news: ok, info, warn or err")
	statusName := fs.String("status", "", "for an agent's toast, the agent's `STATE`: blocked, error or done (its shape replaces the kind's)")
	ttl := fs.Duration("ttl", 0, "how long to stay (default 2.5s ok, 4s info, 6s warn, 8s err)")
	var to state.Visit
	fs.StringVar(&to.SessionID, "go-session", "", "a click on the toast goes to this `SESSION`'s pane --go-block")
	fs.StringVar(&to.WindowID, "go-window", "", "the `WINDOW` of --go-block")
	fs.StringVar(&to.BlockID, "go-block", "", "the agent's pane (`BLOCK`) a click goes to")
	measure := fs.Int("measure", 0, "print how wide the toast is in a window `COLUMNS` wide, and exit (Lua sizes its float by it)")
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
	status, err := parseStatus(*statusName)
	if err != nil {
		return err
	}
	m := newModel(kind, *message, *ttl)
	m.status = status
	if *measure > 0 {
		_, err := fmt.Fprintln(stdout, m.theme.ToastWidth(m.toast(), *measure))
		return err
	}
	if *demo {
		_, err := tea.NewProgram(m).Run()
		return err
	}
	// The toast never has focus: it holds no keys, and closing its layer
	// cannot move focus. It needs nothing of its own layer either.
	return popup.Run("toast", po, popup.RunOptions{Title: sanitize.Label(*title), NoKeys: true, LazySelf: true}, func(pe *popup.Env) error {
		if to.SessionID != "" && to.BlockID != "" && pe.DirsErr == nil {
			// the session and tab it shows in
			from := state.Visit{SessionID: cmp.Or(po.Session, os.Getenv("REX_SESSION")), WindowID: po.OriginWindow}
			m.jump = func(ctx context.Context) error {
				return attend.Visit(ctx, pe.API, pe.Dirs, to, po.Client, from)
			}
		}
		_, err := tea.NewProgram(m).Run()
		return err
	})
}
