package switcher

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Go to finds places by their names; Search (internal/app/search) finds
// the text inside every pane. ⌃F goes from one to the other: Go to closes
// and Search opens over the same pane, with Go to's query typed and
// searched at once, as if it had been opened there.

// luaSearch is the Lua action that opens Search (lua/tools.lua).
const luaSearch = "rexkit_search"

// searchText hands the query over to Search (⌃F). Lua opens Search once
// the block named "after", Go to's own, is gone. A ⌃F that came with Lua's
// handshake (the picker mode binds it) goes in the result, as Go to's other
// hand-offs do. One typed without it (commit_via = "global", where no mode
// binds ⌃F) starts the action itself, before it closes: closing the float
// soon ends the switcher, so the action must not be its child. It runs for
// Go to's client, which gets Search's picker mode (if the app allows Remote
// Control), and names that client, as `rex do` runs with one of its own.
func (m *model) searchText(commit time.Time) tea.Cmd {
	q := m.input.Value()
	if m.demo {
		if q == "" {
			return m.toast.Show(ui.ToastInfo, "Demo: this would open Search")
		}
		return m.toast.Show(ui.ToastInfo, "Demo: this would search the text of every pane for “"+q+"”")
	}
	args := map[string]string{"query": q, "after": m.opts.self}
	if !commit.IsZero() {
		return m.handOff("search", luaSearch, args, false)
	}
	args["client"] = m.opts.client
	return m.act("hand off", "search", func(ctx context.Context) error {
		if m.start == nil {
			return rex.ErrUnavailable
		}
		if err := m.start(luaSearch, args); err != nil {
			return err
		}
		return m.close(ctx)
	})
}

// starter is env.start for api: rex.Client.Start in session, for client.
func starter(api rex.API, session, client string) func(string, any) error {
	c, ok := api.(interface {
		Start(session, client, name string, args any) error
	})
	if !ok {
		return nil
	}
	return func(name string, args any) error { return c.Start(session, client, name, args) }
}
