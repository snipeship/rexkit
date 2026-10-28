package keys

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// keymapCache is the cache entry holding the last keymap, for the first frame.
const keymapCache = "keymap"

// cacheName keys the keymap cache by server, like the switcher's snapshot
// cache, so one server's bindings never paint the first frame on another.
func cacheName(server string) string {
	if server == "" {
		return keymapCache
	}
	h := fnv.New32a()
	h.Write([]byte(server))
	return fmt.Sprintf("%s-%08x", keymapCache, h.Sum32())
}

// Main runs `rexkit keys` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("keys", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	demo := fs.Bool("demo", false, "show a sample keymap instead of the live one")
	mode := fs.String("mode", "", "open filtered to one `MODE`: pane, tab, …, or global")
	firstJSON := fs.String("first-json", "", "the global keys a row lists first, as a JSON array, e.g. `[\"ctrl+shift+a\"]`: the key settings'")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	opts := options{mode: strings.ToLower(*mode), demo: *demo, dismissOnBlur: po.DismissOnBlur, stateDir: po.State, canRun: po.State != ""}
	if *firstJSON != "" {
		if err := json.Unmarshal([]byte(*firstJSON), &opts.first); err != nil {
			return errors.New(`keys: --first-json must be a JSON array of strings, like ["ctrl+shift+a"]`)
		}
	}
	if opts.demo {
		opts.stateDir, opts.canRun = "", false // a demo answers no one
		_, err := run(newModel(opts, Build(demoKeymap()), nil, nil))
		return err
	}
	// The guide finds its layer itself (claim), so the cached guide paints
	// without waiting on Rex.
	return popup.Run("keys", po, popup.RunOptions{Title: "Keys", LazySelf: true}, func(env *popup.Env) error {
		cache := cacheName(os.Getenv("REX_SERVER"))
		m, err := run(newModel(opts, cachedGuide(env.Dirs, cache), loadKeymap(env.API, env.Dirs, cache), claim(env)))
		return errors.Join(err, answer(po, env.Popup.Options().Session, m.chosen))
	})
}

// run runs m and returns it as it ended.
func run(m *model) (*model, error) {
	final, err := tea.NewProgram(m).Run()
	if fm, ok := final.(*model); ok {
		m = fm
	}
	return m, err
}

// cachedGuide is the guide from the last keymap seen, so the first frame is
// never empty; an empty guide when there is none.
func cachedGuide(dirs state.Dirs, cache string) Guide {
	var km rex.Keymap
	if dirs.Cache == "" {
		return Guide{}
	}
	if _, err := dirs.ReadCache(cache, &km); err != nil {
		return Guide{}
	}
	return Build(km)
}

// loadKeymap reads the live keymap and refreshes the cache.
func loadKeymap(api rex.API, dirs state.Dirs, cache string) func() tea.Msg {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		km, err := api.Keymap(ctx)
		if err == nil && dirs.Cache != "" {
			_ = dirs.WriteCache(cache, trim(km)) // only a head start for next time
		}
		return keymapMsg{km: km, err: err}
	}
}

// trim drops what the guide never reads (blocked keys, block actions,
// diagnostics), which can make up most of a keymap.
func trim(km rex.Keymap) rex.Keymap {
	used := map[string]bool{}
	for _, b := range km.Bindings {
		used[b.Action] = true
	}
	out := rex.Keymap{Bindings: km.Bindings, Config: km.Config}
	for _, a := range km.Actions {
		if used[a.Name] {
			out.Actions = append(out.Actions, a)
		}
	}
	for _, m := range km.Modes {
		out.Modes = append(out.Modes, rex.Mode{Name: m.Name, Exclusive: m.Exclusive})
	}
	return out
}

// claim finds the guide's own layer, records it as the owner of the state
// directory, and reports whether the guide floats.
func claim(env *popup.Env) func() tea.Msg {
	return func() tea.Msg {
		self, err := env.ResolveSelf(env.Ctx)
		if err != nil {
			return selfMsg{}
		}
		return selfMsg{floating: self.Floating}
	}
}

// answer tells the Lua layer how the guide ended, before the float closes:
// the chosen action, or "close" (also when nothing asked, which Lua ignores).
func answer(po popup.Options, session string, e *Entry) error {
	if po.State == "" {
		return nil
	}
	r := popup.Result{Action: popup.ActionClose}
	if e != nil {
		r = popup.Result{
			Action:    popup.ActionInvoke,
			SessionID: session,
			WindowID:  po.OriginWindow,
			BlockID:   po.OriginBlock,
			Invoke:    e.Action,
			Args:      e.Args,
		}
	}
	_, err := popup.WriteResult(po.State, r)
	return err
}
