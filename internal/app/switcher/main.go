package switcher

import (
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// Main runs `rexkit goto` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("goto", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	demo := fs.Bool("demo", false, "show sample sessions instead of Rex's")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if *demo {
		blur := false // a demo usually runs in an ordinary pane
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "dismiss-on-blur" {
				blur = po.DismissOnBlur
			}
		})
		return runDemo(blur)
	}
	return run(po)
}

func run(po popup.Options) error {
	return popup.Run("goto", po, popup.RunOptions{}, func(pe *popup.Env) error {
		if pe.DirsErr != nil {
			return pe.DirsErr
		}
		dirs, self := pe.Dirs, pe.Self
		var cached *rex.Inventory
		var inv rex.Inventory
		cache := cacheName(os.Getenv("REX_SERVER"))
		if _, err := dirs.ReadCache(cache, &inv); err == nil {
			if _, ok := inv.Session(self.SessionID); ok {
				cached = &inv // else it is another server's, or too old to help
			}
		}
		history, _ := dirs.History() // a missing or broken history only loses RECENT

		opts := options{
			origin:        place{sessionID: self.SessionID, windowID: po.OriginWindow, blockID: po.OriginBlock},
			self:          self.BlockID,
			client:        po.Client,
			stateDir:      po.State,
			dismissOnBlur: po.DismissOnBlur,
			home:          homeDir(),
		}
		if opts.origin.windowID == "" {
			opts.origin.windowID = self.WindowID // a float opens over its origin's window
		}
		e := env{
			api:       pe.API,
			close:     pe.Popup.Close,
			record:    dirs.UpdateHistory,
			save:      func(inv rex.Inventory) { _ = dirs.WriteCache(cache, inv) },
			attention: dirs.Attention,
			see: func(block string, at time.Time) error {
				return dirs.UpdateAttention(at, func(a *state.Attention) { a.See(block, at) })
			},
			now:   time.Now,
			after: tick,
		}
		_, err := tea.NewProgram(popup.Stage(newModel(pe.Ctx, e, opts, cached, history), po.Card), tea.WithContext(pe.Ctx)).Run()
		return err
	})
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// cacheName keys the snapshot cache by server, so one server's sessions never
// paint the first frame on another.
func cacheName(server string) string {
	if server == "" {
		return state.SnapshotCache
	}
	h := fnv.New32a()
	h.Write([]byte(server))
	return fmt.Sprintf("%s-%08x", state.SnapshotCache, h.Sum32())
}
