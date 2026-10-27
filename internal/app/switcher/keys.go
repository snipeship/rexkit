package switcher

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/ui"
)

// previewScroll is how far ⌃U/⌃D and the wheel scroll the preview.
const previewScroll = 3

// heldBackspace is how long after ⌫ empties the query a further ⌫ still
// counts as the same held key: it is swallowed rather than backing out of
// the drill-down (and wiping the query that restores). Files does the same.
const heldBackspace = 400 * time.Millisecond

// earlyEnter reads a ⌃J from the first moments as the ⏎ it was
// (ui.EarlyKeys); later ⌃J moves down like ↓, with ⌃K up.
func (m *model) earlyEnter(msg tea.KeyPressMsg) tea.KeyPressMsg {
	return ui.EarlyEnterSince(m.started, m.now(), msg)
}

// key handles a key press. Every ⏎ consumes Lua's commit marker first,
// whatever it then does: only a ⏎ that took one is the handshake's, whose
// result Lua reads.
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	var commit time.Time
	if msg.String() == "enter" {
		commit, _ = popup.TakeCommitAt(m.opts.stateDir)
	}
	return m.press(msg, commit)
}

// press handles a key. commit is when Lua wrote the commit marker for a ⏎
// that came from its handshake; zero for any other key.
func (m *model) press(msg tea.KeyPressMsg, commit time.Time) tea.Cmd {
	if m.busy {
		return nil
	}
	k := msg.String()
	if m.ask != nil {
		if k != "ctrl+c" {
			return m.askKey(msg, commit)
		}
		m.ask = nil // ⌃C closes Go to, as over the key panel
	}
	if m.help {
		_, _, more := m.keyPanel(m.layout().areas)
		if at, ok := ui.ScrollKeyPanel(m.helpAt, more, k); ok {
			m.helpAt = at // a key panel cut short scrolls
			return nil
		}
		m.help = false
		if k == "?" || k == "esc" {
			m.stay(k) // they only close the key panel
			return nil
		}
		// any other key closes it and still does its job
	}
	if k != "backspace" {
		m.emptiedAt = time.Time{} // any other key ends a held ⌫
	}
	empty := m.input.Value() == ""
	atEnd := m.input.Position() == len([]rune(m.input.Value()))
	switch k {
	case "esc", "ctrl+c":
		return m.dismiss()
	case "enter":
		if !m.live {
			return m.holdEnter(commit) // typed before the catalog loaded: go once it has
		}
		var cmd tea.Cmd
		if _, e, ok := m.selected(); ok {
			how := byKey
			if !commit.IsZero() {
				how = byHandshake
			}
			cmd = m.goTo(e, how)
		}
		if !m.busy {
			m.stay(k) // nothing to go to, or only a toast
		}
		return cmd
	case "alt+enter":
		var cmd tea.Cmd
		if _, e, ok := m.selected(); ok {
			cmd = m.bringHere(e)
		}
		if !m.busy {
			m.stay(k) // nothing to bring, or only a toast
		}
		return cmd
	case "up", "ctrl+k":
		m.list.Up()
		m.moved = true
	case "down", "ctrl+j":
		m.list.Down()
		m.moved = true
	case "pgup":
		m.list.PageUp()
		m.moved = true
	case "pgdown":
		m.list.PageDown()
		m.moved = true
	case "alt+a":
		if !m.nextWaiting() {
			return m.noneWaiting()
		}
		m.moved = true
	case "ctrl+backspace", "ctrl+h": // a terminal that cannot tell ⌃⌫ from ⌃H sends ⌃H
		if !empty {
			return m.edit(msg) // in a search it deletes a word, as in any field
		}
		return m.markSeen()
	case "tab":
		m.setCategory(m.filter.cat + 1)
	case "shift+tab":
		m.setCategory(m.filter.cat - 1)
	case "right":
		if !atEnd {
			return m.edit(msg)
		}
		m.drill()
	case "left":
		if !empty {
			return m.edit(msg)
		}
		m.back()
	case "backspace":
		if m.now().Sub(m.emptiedAt) < heldBackspace {
			m.emptiedAt = m.now() // still held: swallow repeats until it is let go
			return nil
		}
		if !empty || !m.back() {
			return m.edit(msg) // edit notes when this ⌫ empties the query
		}
		m.emptiedAt = m.now() // backed out: a held ⌫ must not eat the restored query
	case "ctrl+r":
		return m.reload()
	case "ctrl+e":
		return m.openRename()
	case "ctrl+x":
		return m.openClose()
	case "ctrl+a":
		return m.openNew()
	case "ctrl+/", "ctrl+_", "ctrl+7":
		return m.togglePreview()
	case "ctrl+u":
		m.pv.view.Scroll(-m.previewPage(), m.layout().areas.Preview.H)
	case "ctrl+d":
		m.pv.view.Scroll(m.previewPage(), m.layout().areas.Preview.H)
	case "?":
		m.help, m.helpAt = true, 0
		m.layoutChanged() // the list scrolls the selection above the panel
	default:
		return m.edit(msg)
	}
	return nil
}

// holdEnter keeps a ⏎ until the catalog is live (goLive runs it). A held
// handshake ⏎ answers "stay" popup.HandshakeGrace after Lua wrote it, before
// Lua gives up waiting; the ⏎ then runs as one typed without the handshake.
func (m *model) holdEnter(commit time.Time) tea.Cmd {
	m.pending = true
	if commit.IsZero() {
		return nil
	}
	m.pendingCommit = commit
	m.pendingRev++
	return m.after(popup.GraceLeft(commit), stillPendingMsg{m.pendingRev})
}

// stillPendingMsg fires when the grace of a held handshake ⏎ is over.
type stillPendingMsg struct{ rev int }

func (m *model) stillPending(msg stillPendingMsg) tea.Cmd {
	if msg.rev != m.pendingRev || !m.pending || m.pendingCommit.IsZero() {
		return nil // it ran, or was answered already
	}
	m.pendingCommit = time.Time{}
	m.stay("enter")
	return m.toast.Show(ui.ToastInfo, "Still updating", ui.Hint{Key: "enter", Label: "goes when it's ready"})
}

func (m *model) previewPage() int { return max(previewScroll, m.layout().areas.Preview.H/2) }

// edit passes a message to the query field and refilters if the query
// changed.
func (m *model) edit(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "backspace" {
		if before != "" && m.input.Value() == "" {
			m.emptiedAt = m.now()
		}
	} else {
		m.emptiedAt = time.Time{}
	}
	if m.input.Value() != before {
		policy := selectTop
		if m.input.Value() == "" {
			policy = selectDefault
		}
		m.rebuild(policy)
	}
	return cmd
}

// keySheet is the key panel (?).
var keySheet = []ui.KeyGroup{
	{Title: "Move", Bindings: []ui.Binding{
		{Keys: []string{"up", "down"}, Desc: "select"},
		{Keys: []string{"pgup", "pgdown"}, Desc: "page"},
		{Keys: []string{"right"}, Desc: "open session or tab"},
		{Keys: []string{"left", "backspace"}, Desc: "back out"},
		{Keys: []string{"tab", "shift+tab"}, Desc: "filter by kind"},
		{Keys: []string{"alt+a"}, Desc: "next agent that needs you"},
	}},
	{Title: "Go", Bindings: []ui.Binding{
		{Keys: []string{"enter"}, Desc: "go there"},
		{Keys: []string{"alt+enter"}, Desc: "bring pane here"},
		{Keys: []string{"double-click"}, Desc: "go there"},
		{Keys: []string{"esc"}, Desc: "close"},
	}},
	{Title: "Manage", Bindings: []ui.Binding{
		{Keys: []string{"ctrl+backspace"}, Desc: "mark agent seen (no search)"},
		{Keys: []string{"ctrl+e"}, Desc: "rename"},
		{Keys: []string{"ctrl+x"}, Desc: "close"},
		{Keys: []string{"ctrl+a"}, Desc: "new session"},
	}},
	{Title: "View", Bindings: []ui.Binding{
		{Keys: []string{"ctrl+/"}, Desc: "preview"},
		{Keys: []string{"ctrl+u", "ctrl+d"}, Desc: "scroll preview"},
		{Keys: []string{"ctrl+r"}, Desc: "refresh"},
		{Keys: []string{"?"}, Desc: "keys"},
	}},
}

// nextWaiting selects the next row of Needs you, round its end: ⌥A, which
// everywhere else goes to the next agent that needs you, here walks their
// previews (⏎ then goes). It reports whether there was one.
func (m *model) nextWaiting() bool {
	for k, s := range m.listing.sections {
		if s.Title != secNeedsYou {
			continue
		}
		end := len(m.listing.rows)
		if k+1 < len(m.listing.sections) {
			end = m.listing.sections[k+1].Start
		}
		next := s.Start
		if c := m.list.Cursor(); c >= s.Start && c+1 < end {
			next = c + 1
		}
		m.list.Select(next)
		return true
	}
	return false
}

// stayInNeedsYou selects the row of Needs you that took the place of the
// one at row, which ⌃⌫ took out (or the last, if it was the last): the
// next one to read, not the result itself further down the list. With
// Needs you empty, it selects the top of the list.
func (m *model) stayInNeedsYou(row int) {
	for k, s := range m.listing.sections {
		if s.Title != secNeedsYou {
			continue
		}
		end := len(m.listing.rows)
		if k+1 < len(m.listing.sections) {
			end = m.listing.sections[k+1].Start
		}
		if end > s.Start {
			m.list.Select(min(max(row, s.Start), end-1))
			return
		}
	}
	m.list.Select(0)
}

// noneWaiting says why ⌥A found no row of Needs you: no agent needs the
// user, or the search or the filter hides them.
func (m *model) noneWaiting() tea.Cmd {
	if m.cat == nil {
		return nil // not loaded yet
	}
	if slices.ContainsFunc(m.cat.entries, entry.needsYou) {
		switch {
		case m.filter.query != "":
			return m.toast.Show(ui.ToastInfo, "Clear the search to see Needs you")
		case m.filter.scope != "":
			return m.toast.Show(ui.ToastInfo, "Go back to see Needs you", ui.Hint{Key: "left", Label: "back"})
		}
		return m.toast.Show(ui.ToastInfo, "Needs you is under All and Agents", ui.Hint{Key: "tab", Label: "filter"})
	}
	return m.toast.Show(ui.ToastInfo, "No agent needs you")
}

// markSeen dismisses the selected agent of Needs you (⌃⌫): what it says
// counts as seen. A result leaves Needs you; a blocked or failed agent
// stays, since it still needs the user, but ⌥A visits it after the news.
func (m *model) markSeen() tea.Cmd {
	_, e, ok := m.selected()
	if !ok || !e.needsYou() {
		return m.toast.Show(ui.ToastInfo, "Only an agent that needs you can be marked seen")
	}
	now := m.now()
	m.attn.See(e.blockID, now)
	if m.see != nil {
		if err := m.see(e.blockID, now); err != nil {
			return m.toast.Show(ui.ToastErr, "Could not mark it seen: "+err.Error())
		}
	}
	if m.inv != nil {
		prev := m.selection()
		m.cat = buildCatalog(*m.inv, m.catalogOptions())
		if e.status == ui.StatusDone && prev.section == secNeedsYou {
			// The result leaves Needs you: the selection stays there, and
			// never passes by the result's row further down (the list
			// would scroll away).
			m.rebuildFrom(selectKeep, "", selection{})
			m.stayInNeedsYou(prev.index)
		} else {
			m.rebuildFrom(selectKeep, "", prev)
		}
	}
	if e.status == ui.StatusDone {
		return nil // it is gone from the list: that says it
	}
	return m.toast.Show(ui.ToastInfo, "Marked seen · it stays in Needs you until the agent goes on")
}
