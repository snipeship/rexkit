package switcher

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/app/rename"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Go to manages what it lists: ⌃E renames the selected session, tab or pane,
// ⌃X closes it (after asking), ⌃A makes a new session. (⌃N, the obvious key,
// enters Resize mode wherever rexkit's modes are on.) Each opens an ask, a
// sheet raised over the list, whose ⏎ does it and ⎋ backs out.
//
// A rename, and a close of a place the switcher is not in, go through the
// server, and Go to stays open on the refreshed list. A close of the place
// it is in (its pane, tab or session) and a new session need the app: Go
// to closes and hands them to the Lua layer as an invoke result, which only
// a handshake ⏎ delivers (Lua reads the result and runs it).

// Lua actions the switcher hands work to (lua/tools.lua).
const (
	luaClosePlace = "rexkit_close_place"
	luaNewSession = "rexkit_new_session"
)

// askKind is what an ask does.
type askKind int

const (
	askRename askKind = iota + 1
	askClose
	askNew
)

// ask is the sheet open over the list.
type ask struct {
	kind    askKind
	target  entry         // what is renamed or closed (a close may widen it: closePlan)
	picked  entry         // what was selected when a close was asked, planned again on ⏎
	via     *entry        // what a close widened from (it is the only pane or tab there)
	editor  rename.Editor // the name field of a rename or a new session
	problem string        // why the last save failed, shown in the field
	busy    bool          // the server is on it: keys wait, ⎋ still backs out
	spin    ui.Spinner    // "Saving…" or "Closing…" in the tray, once it takes a while
}

// askDoneMsg ends a rename or a close done through the server.
type askDoneMsg struct {
	ask    *ask // the ask that started it; it may have been backed out of since
	kind   askKind
	target rename.Target
	err    error
	name   string // what it acted on, for the toast
}

// target is the rename.Target of a session, tab or pane entry.
func target(e entry) rename.Target {
	switch e.kind {
	case ui.KindSession:
		return rename.Target{Kind: "session", ID: e.sessionID}
	case ui.KindTab:
		return rename.Target{Kind: "tab", ID: e.windowID}
	}
	return rename.Target{Kind: "pane", ID: e.blockID}
}

// openRename asks for a new name for the selected entry.
func (m *model) openRename() tea.Cmd {
	_, e, ok := m.selected()
	if !ok {
		return nil
	}
	return m.openAsk(&ask{kind: askRename, target: e, editor: rename.NewEditor(target(e), e.label)})
}

// openClose asks to confirm closing the selected entry, or what closing it
// would empty. The only session is never closed: nothing would be left.
func (m *model) openClose() tea.Cmd {
	_, e, ok := m.selected()
	if !ok {
		return nil
	}
	t, via := m.closePlan(e)
	if t.kind == ui.KindSession && m.sessions() <= 1 {
		return m.toast.Show(ui.ToastWarn, "That is the only session")
	}
	return m.openAsk(&ask{kind: askClose, target: t, picked: e, via: via})
}

// openNew asks for the new session's name.
func (m *model) openNew() tea.Cmd {
	return m.openAsk(&ask{kind: askNew, editor: rename.NewEditor(rename.Target{Kind: "session"}, "")})
}

// openAsk raises a's sheet, unless the window is too small to show it. A ⏎
// still held for the catalog is dropped: it was not meant for the sheet.
func (m *model) openAsk(a *ask) tea.Cmd {
	m.ask = a
	if !m.askFits() {
		m.ask = nil
		return m.toast.Show(ui.ToastWarn, "Make Go to bigger for that")
	}
	m.help = false
	if m.pending {
		m.pending = false
		m.pendingRev++
		if !m.pendingCommit.IsZero() {
			m.stay("enter")
			m.pendingCommit = time.Time{}
		}
	}
	m.layoutChanged() // the list scrolls the selection above the sheet
	return nil
}

// askFits reports whether the open ask's sheet fits the window.
func (m *model) askFits() bool {
	panel, _, _, _ := m.askPanel(m.layout().areas)
	return panel != nil
}

func (m *model) closeAsk() {
	m.ask = nil
	m.layoutChanged()
}

// sessions counts the sessions listed.
func (m *model) sessions() int {
	n := 0
	if m.cat != nil {
		for _, e := range m.cat.entries {
			if e.kind == ui.KindSession {
				n++
			}
		}
	}
	return n
}

// closePlan is what closing e closes: e, or the tab it is the only pane of,
// or the session that tab is the only tab of, since Rex keeps a session
// whose last pane closed, empty. via is what it widened from (nil when it
// did not): the only pane or tab there.
func (m *model) closePlan(e entry) (entry, *entry) {
	var via *entry
	if e.pane() {
		if tab, ok := m.cat.find(e.windowID); ok && tab.panes <= 1 {
			pane := e
			via, e = &pane, tab
		}
	}
	if e.kind == ui.KindTab {
		if sess, ok := m.cat.find(e.sessionID); ok && sess.tabs <= 1 {
			tab := e
			via, e = &tab, sess
		}
	}
	return e, via
}

// samePlan reports whether a close plan still closes the same thing, as
// much of it as the sheet said.
func samePlan(a, b entry) bool {
	return a.kind == b.kind && a.sessionID == b.sessionID && a.windowID == b.windowID && a.blockID == b.blockID &&
		a.tabs == b.tabs && a.panes == b.panes && a.working+a.guessed+a.blocked == b.working+b.guessed+b.blocked
}

// holdsOrigin reports whether closing e closes the place the switcher was
// opened from: its pane, or the tab or session around it.
func (m *model) holdsOrigin(e entry) bool {
	o := m.opts.origin
	switch e.kind {
	case ui.KindSession:
		return e.sessionID == o.sessionID
	case ui.KindTab:
		return e.windowID == o.windowID
	}
	return e.blockID == o.blockID
}

// askKey handles a key other than ⌃C (which closes Go to) while an ask is
// open. ⏎ came with Lua's commit marker when commit is set.
func (m *model) askKey(msg tea.KeyPressMsg, commit time.Time) tea.Cmd {
	a := m.ask
	switch msg.String() {
	case "esc":
		m.stay("esc") // it only backs out of the ask
		m.closeAsk()
		return nil
	case "enter":
		return m.confirm(commit)
	case "ctrl+f":
		m.stay("ctrl+f") // the sheet keeps Go to: in a field it moves the cursor
	}
	if a.busy || a.kind == askClose {
		return nil
	}
	a.problem = ""
	cmd := a.editor.Key(msg)
	m.layoutChanged() // the helper may have changed the sheet's height
	return cmd
}

// confirm does what the ask asks.
func (m *model) confirm(commit time.Time) tea.Cmd {
	a := m.ask
	if a.busy {
		m.stay("enter")
		return nil
	}
	switch a.kind {
	case askRename:
		return m.saveName()
	case askClose:
		return m.closeTarget(commit)
	}
	return m.newSession(commit)
}

// saveName renames the target through the server; Go to stays open.
func (m *model) saveName() tea.Cmd {
	a := m.ask
	m.stay("enter")
	t, label := a.editor.Target(), a.editor.Label()
	switch {
	case a.editor.Unchanged():
		m.closeAsk()
		return nil
	case m.demo:
		m.closeAsk()
		return m.toast.Show(ui.ToastInfo, "Demo: "+t.Outcome(label))
	}
	api, session := m.api, a.target.sessionID
	return m.askWork(a.target.name, func(ctx context.Context) error {
		return t.Apply(ctx, api, session, label)
	})
}

// closeTarget closes the target. A place Go to is not in closes through the
// server while Go to stays open; the place it is in is closed by the Lua
// layer once Go to has closed.
func (m *model) closeTarget(commit time.Time) tea.Cmd {
	a := m.ask
	// The list may have changed under the sheet: plan again, and ask again
	// if it would now close something else, or more.
	picked, ok := m.cat.find(a.picked.key())
	if !ok {
		m.stay("enter")
		m.closeAsk()
		return tea.Batch(m.toast.Show(ui.ToastInfo, "The "+target(a.picked).Kind+" was already closed"), m.collect())
	}
	if e, via := m.closePlan(picked); !samePlan(e, a.target) {
		m.stay("enter")
		a.target, a.picked, a.via = e, picked, via
		m.layoutChanged()
		return m.toast.Show(ui.ToastWarn, "That changed: check it again")
	}
	e := a.target
	what := target(e).Kind + " " + e.name
	if m.demo {
		m.stay("enter")
		m.closeAsk()
		return m.toast.Show(ui.ToastInfo, "Demo: this would close the "+what)
	}
	api := m.api
	shut := func(ctx context.Context) error {
		switch e.kind {
		case ui.KindSession:
			return api.DestroySession(ctx, e.sessionID)
		case ui.KindTab:
			return api.CloseWindow(ctx, e.sessionID, e.windowID)
		}
		return api.CloseBlock(ctx, e.sessionID, e.blockID)
	}
	if !m.holdsOrigin(e) {
		m.stay("enter")
		return m.askWork(what, shut)
	}
	if commit.IsZero() {
		// Nobody reads a result. A session can't be closed from here (the
		// app would be left on it); a tab or pane can: the float goes with
		// its tab, and a pane is closed first, then the float.
		if e.kind == ui.KindSession {
			return m.toast.Show(ui.ToastWarn, "Can't close the session you are in from here",
				ui.Hint{Key: "ctrl+shift+space", Label: "close it"})
		}
		return m.act("close it", target(e).Kind, func(ctx context.Context) error {
			if err := shut(ctx); err != nil {
				return err
			}
			return m.close(ctx)
		})
	}
	args := map[string]string{"kind": target(e).Kind, "session_id": e.sessionID, "window_id": e.windowID, "block_id": e.blockID}
	return m.handOff(target(e).Kind, luaClosePlace, args, false)
}

// newSession has the Lua layer make a session and switch the app to it,
// which only a handshake ⏎ can ask for.
func (m *model) newSession(commit time.Time) tea.Cmd {
	label := m.ask.editor.Label()
	switch {
	case m.demo:
		m.stay("enter")
		m.closeAsk()
		if label == "" {
			return m.toast.Show(ui.ToastInfo, "Demo: this would make a new session")
		}
		return m.toast.Show(ui.ToastInfo, "Demo: this would make the session “"+label+"”")
	case commit.IsZero():
		// ⌃⇧Space is a ⏎ that comes with the handshake
		return m.toast.Show(ui.ToastWarn, "Can't switch to a new session from here",
			ui.Hint{Key: "ctrl+shift+space", Label: "create"})
	}
	return m.handOff("session", luaNewSession, map[string]string{"label": label}, true)
}

// handOff closes Go to with a result that has the Lua layer run action with
// args. back brings focus back to the origin first; a close of the origin
// leaves it out, since it is going.
func (m *model) handOff(subject, action string, args map[string]string, back bool) tea.Cmd {
	return m.act("hand off", subject, func(ctx context.Context) error {
		data, err := json.Marshal(args)
		if err != nil {
			return err
		}
		r := popup.Result{Action: popup.ActionInvoke, Invoke: action, Args: data}
		if o := m.opts.origin; back {
			r.SessionID, r.WindowID, r.BlockID = o.sessionID, o.windowID, o.blockID
		}
		if _, err := popup.WriteResult(m.opts.stateDir, r); err != nil {
			return err
		}
		return m.close(ctx)
	})
}

// askWork runs fn in the background while the ask waits, saying
// "Saving…" or "Closing…" in the tray once it takes a while.
func (m *model) askWork(name string, fn func(context.Context) error) tea.Cmd {
	a := m.ask
	a.busy = true
	a.spin.Start(m.now())
	msg := askDoneMsg{ask: a, kind: a.kind, target: target(a.target), name: name}
	if a.kind == askRename {
		msg.target = a.editor.Target()
	}
	return tea.Batch(m.clock.wake(m), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		msg.err = fn(ctx)
		return msg
	})
}

// askDone ends a rename or close: the list refreshes and a toast says what
// happened; a failed rename keeps its ask open with the reason in its field.
// An ask backed out of meanwhile only gets the toast.
func (m *model) askDone(msg askDoneMsg) tea.Cmd {
	a := m.ask
	if a != msg.ask {
		a = nil // another ask, or none
	}
	if a != nil {
		a.busy = false
		a.spin.Stop()
	}
	var text string
	kind := ui.ToastOK
	switch {
	case msg.err != nil && msg.kind == askRename:
		if a != nil {
			a.problem = msg.target.Problem(msg.err)
			m.layoutChanged()
			return m.collect()
		}
		text, kind = "Can't rename the "+msg.target.Kind+" "+msg.name+": "+rex.UserMessage(msg.err), ui.ToastErr
		if errors.Is(msg.err, rex.ErrNotFound) {
			text = "The " + msg.target.Kind + " " + msg.name + " is gone"
		}
	case msg.err != nil:
		text, kind = "Can't close the "+msg.name+": "+rex.UserMessage(msg.err), ui.ToastErr
		if errors.Is(msg.err, rex.ErrNotFound) {
			text, kind = "The "+msg.name+" was already closed", ui.ToastInfo
		}
	case msg.kind == askRename:
		text = "Renamed"
	default:
		text = "Closed the " + msg.name
	}
	if a != nil {
		m.closeAsk()
	}
	return tea.Batch(m.toast.Show(kind, text), m.collect())
}

// askSheet is the open ask as a sheet: its title, field and what it says.
func (m *model) askSheet() ui.Sheet {
	t := m.theme.InPanel()
	a := m.ask
	bold := t.Text.Bold(true)
	name := func(e entry) ui.Line {
		return append(ui.Line{{Text: " "}}, t.Escaped(e.name, e.nameEsc, t.Muted)...)
	}
	switch a.kind {
	case askRename:
		f := a.editor.Field(a.problem)
		title := append(ui.Line{{Text: "Rename " + target(a.target).Kind, Style: bold}}, name(a.target)...)
		return ui.Sheet{Title: title, Field: &f}
	case askNew:
		f := a.editor.Field("")
		f.Helper = "Leave it empty and Rex picks a name"
		return ui.Sheet{Title: ui.Line{{Text: "New session", Style: bold}}, Field: &f}
	}
	e := a.target
	kind := target(e).Kind
	title := append(ui.Line{{Text: "Close " + kind, Style: bold}}, name(e)...)
	title = append(title, ui.Span{Text: "?", Style: bold})
	// most important first: a sheet short of rows drops the last lines
	var lines []ui.Line
	if v := a.via; v != nil {
		why := t.Escaped(v.name, v.nameEsc, t.Text)
		lines = append(lines, append(why, ui.Span{Text: " is its only " + target(*v).Kind + ", so the " + kind + " closes.", Style: t.Muted}))
	}
	if warn := agentsAt(e); warn != "" {
		lines = append(lines, ui.Line{{Text: ui.GlyphWarn + " " + warn, Style: t.Warn}})
	}
	lines = append(lines, ui.Styled(closeEffect(e), t.Muted))
	if m.holdsOrigin(e) {
		lines = append(lines, ui.Styled("You are in it: Go to closes first.", t.Subtle))
	}
	return ui.Sheet{Title: title, Lines: lines}
}

// closeEffect says what closing e ends, short enough for a sheet's line.
func closeEffect(e entry) string {
	switch e.kind {
	case ui.KindSession:
		return "Ends what runs in its " + ui.Plural(e.tabs, "tab") + " and " + ui.Plural(e.panes, "pane") + "."
	case ui.KindTab:
		if e.panes == 1 {
			return "Ends what runs in its pane."
		}
		return "Ends what runs in its " + ui.Plural(e.panes, "pane") + "."
	}
	if e.program != "" {
		return "Ends what runs in it (" + e.program + ")."
	}
	return "Ends what runs in it."
}

// agentsAt warns of agents at work in e: "2 agents are working in it".
func agentsAt(e entry) string {
	if e.pane() {
		switch e.status {
		case ui.StatusWorking:
			return "Its agent is working"
		case ui.StatusBlocked:
			return "Its agent is waiting for you"
		}
		return ""
	}
	busy := e.working + e.guessed + e.blocked // closing a guess at work warns too
	switch busy {
	case 0:
		return ""
	case 1:
		return "1 agent is at work in it"
	}
	return strconv.Itoa(busy) + " agents are at work in it"
}

// askPanel draws the ask's sheet, says where it goes in the body, and
// where its field's cursor is within it (-1, -1 when none).
func (m *model) askPanel(a ui.Areas) ([]string, ui.Rect, int, int) {
	return m.theme.Sheet(m.askSheet(), a.Body.W, a.Body.H, m.panelPlace(a))
}

// askTray is the tray while an ask is open: its keys, and what is going on.
func (m *model) askTray() ui.Tray {
	a := m.ask
	t := m.theme
	now := m.now()
	tr := ui.Tray{Toast: m.toast.Current()}
	switch a.kind {
	case askRename:
		tr.Meta = t.Hints([]ui.Hint{{Key: "ctrl+u", Label: "clear"}}, 1<<30)
		tr.Hints = []ui.Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "save", Primary: true}}
	case askNew:
		tr.Hints = []ui.Hint{{Key: "esc", Label: "cancel"}, {Key: "enter", Label: "create", Primary: true}}
	case askClose:
		tr.Hints = []ui.Hint{{Key: "esc", Label: "keep"}, {Key: "enter", Label: "close", Primary: true}}
	}
	if a.spin.Visible(now) {
		label := "Saving"
		if a.kind == askClose {
			label = "Closing"
		}
		tr.Busy = t.Spinning(label+ui.Ellipsis, now)
	}
	return tr
}
