package rename

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
)

// Main runs `rexkit rename` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	var po popup.Options
	po.RegisterFlags(fs)
	kind := fs.String("target", "pane", "what to rename: `pane`, tab or session")
	id := fs.String("id", "", "block, window or session `ID` (default: the origin pane, its tab, or this session)")
	current := fs.String("current", "", "the current `LABEL`, shown selected (default: read it from Rex)")
	demo := fs.Bool("demo", false, "show the dialog without renaming anything")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	opts := options{target: Target{Kind: *kind, ID: *id}, demo: *demo, dismissOnBlur: po.DismissOnBlur, stateDir: po.State}
	if opts.demo {
		opts.stateDir = "" // a demo answers no one
		if !given["current"] {
			*current = "api-server"
		}
		_, err := run(newModel(opts, *current, nil, nil, nil), popup.Card{})
		return err
	}
	return popup.Run("rename", po, popup.RunOptions{LazySelf: true}, func(env *popup.Env) error {
		session := env.Popup.Options().Session
		var err error
		if opts.target, err = resolve(opts.target, session, po); err != nil {
			return err
		}
		var fetch tea.Cmd
		if !given["current"] {
			fetch = currentLabel(env.API, session, opts.target)
		}
		m, err := run(newModel(opts, *current, applier(env.API, session, opts.target), fetch, claim(env)), po.Card)
		return errors.Join(err, answer(po, session, m.saved))
	})
}

// run runs m in its card of the float (popup.Stage); the zero card is the
// whole float.
func run(m *model, card popup.Card) (*model, error) {
	final, err := tea.NewProgram(popup.Stage(m, card)).Run()
	if fm, ok := popup.Unstage(final).(*model); ok {
		m = fm
	}
	return m, err
}

// resolve fills in the target's ID from the launch context.
func resolve(t Target, session string, po popup.Options) (Target, error) {
	if t.ID != "" {
		return t, nil
	}
	switch t.Kind {
	case "pane":
		t.ID = po.OriginBlock
	case "tab":
		t.ID = po.OriginWindow
	case "session":
		t.ID = session
	default:
		return t, fmt.Errorf("--target must be pane, tab or session, not %q", t.Kind)
	}
	if t.ID == "" {
		return t, fmt.Errorf("no %s to rename (pass --id)", t.Kind)
	}
	return t, nil
}

// applier returns the command that sets the target's label.
func applier(api rex.API, session string, t Target) func(string) tea.Cmd {
	return func(label string) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			switch t.Kind {
			case "pane":
				err = api.SetBlockLabel(ctx, session, t.ID, label)
			case "tab":
				err = api.SetWindowLabel(ctx, session, t.ID, label)
			default:
				err = api.SetSessionLabel(ctx, t.ID, label)
			}
			return savedMsg{problem: problem(t.Kind, err)}
		}
	}
}

// currentLabel reads the target's label from the session view.
func currentLabel(api rex.API, session string, t Target) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		id := session
		if t.Kind == "session" {
			id = t.ID
		}
		view, err := api.View(ctx, id)
		if err != nil {
			return nil
		}
		switch t.Kind {
		case "pane":
			if _, _, b, ok := view.FindBlock(t.ID); ok {
				return currentMsg{b.Label}
			}
		case "tab":
			if w, ok := view.Window(t.ID); ok {
				return currentMsg{w.Label}
			}
		default:
			return currentMsg{view.Label}
		}
		return nil
	}
}

// claim finds the dialog's own layer, records it as the owner of the state
// directory, and reports whether the dialog floats. It runs after the first
// frame (popup.RunOptions.LazySelf), so the dialog never waits on Rex to show.
func claim(env *popup.Env) tea.Cmd {
	return func() tea.Msg {
		self, err := env.ResolveSelf(env.Ctx)
		if err != nil {
			return selfMsg{}
		}
		return selfMsg{floating: self.Floating}
	}
}

// problem puts a failed rename in words for the helper under the field, ""
// for success.
func problem(kind string, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, rex.ErrNotFound):
		return "That " + kind + " is gone"
	}
	return "Could not rename: " + rex.UserMessage(err)
}

// answer tells the Lua layer how the dialog ended, before the float closes:
// "rename" (back to the origin pane) or "close".
func answer(po popup.Options, session string, saved bool) error {
	if po.State == "" {
		return nil
	}
	r := popup.Result{Action: popup.ActionClose}
	if saved {
		r = popup.Result{Action: popup.ActionRename, SessionID: session, WindowID: po.OriginWindow, BlockID: po.OriginBlock}
	}
	_, err := popup.WriteResult(po.State, r)
	return err
}
