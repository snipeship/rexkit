package keys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"rexkit/internal/ui/uitest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
)

// mark creates Lua's commit marker: the next ⏎ comes from its handshake.
func mark(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, popup.CommitFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readResult returns the result in dir, or "" when there is none.
func readResult(t *testing.T, dir string) popup.Result {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, popup.ResultFile))
	if os.IsNotExist(err) {
		return popup.Result{}
	}
	if err != nil {
		t.Fatal(err)
	}
	var r popup.Result
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// Every key the Lua layer forwards (⏎, ⎋, ⌃C) must get an answer: "stay"
// from the model when the guide stays open, the rest from Main.
func TestHandshakeStay(t *testing.T) {
	tests := []struct {
		name string
		keys []string // the last one is the forwarded key
		stay bool
		quit bool
	}{
		{"esc clears the query", []string{"x", "esc"}, true, false},
		{"esc closes the help", []string{"?", "esc"}, true, false},
		{"enter closes the help and runs", []string{"?", "enter"}, false, true},
		{"enter with nothing to run", []string{"z", "z", "q", "q", "enter"}, true, false},
		{"enter runs", []string{"enter"}, false, true},
		{"esc closes", []string{"esc"}, false, true},
		{"ctrl+c closes, even over the help", []string{"?", "ctrl+c"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REXKIT_THEME", "dark")
			dir := t.TempDir()
			m := newModel(options{stateDir: dir, canRun: true}, Build(demoKeymap()), nil, nil)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			press(t, m, tt.keys[:len(tt.keys)-1]...)
			last := tt.keys[len(tt.keys)-1]
			if last == "enter" {
				mark(t, dir)
			}
			_, cmd := m.Update(uitest.Key(t, last))
			var msg tea.Msg
			if cmd != nil {
				msg = cmd()
			}
			if _, quit := msg.(tea.QuitMsg); quit != tt.quit {
				t.Errorf("quit = %v, want %v", quit, tt.quit)
			}
			if got := readResult(t, dir).Action == "stay"; got != tt.stay {
				t.Errorf("stay answered = %v, want %v", got, tt.stay)
			}
			if _, err := os.Stat(filepath.Join(dir, popup.CommitFile)); err == nil {
				t.Error("the commit marker outlived its ⏎")
			}
		})
	}
}

// A ⏎ typed while the picker mode is not active has no commit marker: Lua
// would never run the chosen action, so the guide stays open and says how.
func TestTypedEnterDoesNotRun(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	dir := t.TempDir()
	m := newModel(options{stateDir: dir, canRun: true}, Build(demoKeymap()), nil, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	_, cmd := m.Update(uitest.Key(t, "enter"))
	if quits(cmd) || m.chosen != nil {
		t.Fatal("a ⏎ without the handshake closed the guide")
	}
	if !strings.Contains(m.toasts.Current().Text, "Reopen the guide") {
		t.Errorf("toast %q", m.toasts.Current().Text)
	}
}

func TestAnswer(t *testing.T) {
	dir := t.TempDir()
	po := popup.Options{State: dir, OriginWindow: "window:w", OriginBlock: "block:b"}
	if err := answer(po, "session:s", nil); err != nil {
		t.Fatal(err)
	}
	if r := readResult(t, dir); r.Action != "close" || r.Seq == 0 {
		t.Errorf("closing answer %+v", r)
	}
	e := &Entry{Action: "client.mode.enter", Args: json.RawMessage(`{"name":"tab"}`)}
	if err := answer(po, "session:s", e); err != nil {
		t.Fatal(err)
	}
	r := readResult(t, dir)
	if r.Action != "invoke" || r.Invoke != e.Action || string(compactJSON(r.Args)) != `{"name":"tab"}` ||
		r.SessionID != "session:s" || r.WindowID != "window:w" || r.BlockID != "block:b" {
		t.Errorf("invoke answer %+v", r)
	}
	if err := answer(popup.Options{}, "session:s", e); err != nil {
		t.Errorf("no state dir: %v", err)
	}
}

func TestPlumbingIsHidden(t *testing.T) {
	km := rex.Keymap{
		Actions: []rex.Action{
			{Name: "rexkit_commit", Kind: "lua", Title: "Confirm in tool", Category: internalCategory},
			{Name: "rexkit_keys", Kind: "lua", Title: "Key guide", Category: "rexkit"},
		},
		Bindings: []rex.Binding{
			{Keys: []string{"enter"}, Action: "rexkit_commit", Args: json.RawMessage(`{"global":true}`)},
			{Keys: []string{"f1"}, Action: "rexkit_keys"},
		},
	}
	g := Build(km)
	if g.total() != 1 || g.Groups[0].Entries[0].Action != "rexkit_keys" {
		t.Errorf("guide %+v", g.Groups)
	}
}

// TestEnterWaitsForTheLiveKeymap types a search and ⏎ before the live keymap
// has arrived (no cached guide): the action runs once the keymap is there.
func TestEnterWaitsForTheLiveKeymap(t *testing.T) {
	t.Setenv("REXKIT_THEME", "dark")
	load := func() tea.Msg { return keymapMsg{km: demoKeymap()} }
	dir := t.TempDir()
	m := newModel(options{stateDir: dir, canRun: true}, Guide{}, load, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, r := range "zoom" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	mark(t, dir) // the held ⏎ must remember that it came from the handshake
	m.Update(uitest.Key(t, "enter"))
	if m.chosen != nil || !m.pending {
		t.Fatalf("chosen=%v pending=%v: ⏎ must wait for the keymap", m.chosen, m.pending)
	}
	m.Update(load())
	if m.chosen == nil || m.chosen.Action != "pane.zoom" {
		t.Fatalf("chosen = %+v, want pane.zoom", m.chosen)
	}
}

// settle runs cmd and what it batches, as the program would, but gives up on
// a command that does not return at once (a toast's timer).
func settle(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(100 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, settle(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// markAt creates Lua's commit marker as written at at.
func markAt(t *testing.T, dir string, at time.Time) {
	t.Helper()
	mark(t, dir)
	if err := os.Chtimes(filepath.Join(dir, popup.CommitFile), at, at); err != nil {
		t.Fatal(err)
	}
}

// waiting is the guide with a search typed and no keymap yet.
func waiting(t *testing.T, canRun bool) (*model, string, func() tea.Msg) {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	load := func() tea.Msg { return keymapMsg{km: demoKeymap()} }
	dir := t.TempDir()
	m := newModel(options{stateDir: dir, canRun: canRun}, Guide{}, load, nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	press(t, m, "z", "o", "o", "m")
	return m, dir, load
}

// TestHeldEnterAnswersStay: a ⏎ from the handshake held for the keymap
// answers "stay" once its grace is over (popup.GraceLeft from Lua's
// marker, not from when the guide read the key), and the keymap arriving
// later does not run the action: the selection stays for a fresh ⏎.
func TestHeldEnterAnswersStay(t *testing.T) {
	m, dir, load := waiting(t, true)
	markAt(t, dir, time.Now().Add(-popup.HandshakeGrace)) // the guide was slow to start
	_, cmd := m.Update(uitest.Key(t, "enter"))
	if r := readResult(t, dir); r.Action != "" {
		t.Fatalf("answered %q before the grace was over", r.Action)
	}
	msgs := settle(cmd) // GraceLeft is 0: the tick fires at once
	if len(msgs) != 1 {
		t.Fatalf("held ⏎ scheduled %v, want one stillPendingMsg", msgs)
	}
	_, cmd = m.Update(msgs[0])
	settle(cmd)
	if r := readResult(t, dir); r.Action != popup.ActionStay {
		t.Fatalf("answer %q after the grace, want stay", r.Action)
	}
	if got := m.toasts.Current().Text; got != "Still reading the keymap" {
		t.Errorf("toast %q", got)
	}
	m.Update(load())
	if m.chosen != nil || m.pending {
		t.Fatalf("chosen=%v pending=%v: a lapsed ⏎ must not run", m.chosen, m.pending)
	}
	if got := selectedTitle(m); got != "Zoom pane" {
		t.Errorf("selected %q, want the entry the ⏎ was on", got)
	}
	if to := m.toasts.Current(); to.Text != "Keymap ready" || len(to.Ways) != 1 || to.Ways[0].Key != "enter" {
		t.Errorf("toast %+v", to)
	}
	mark(t, dir) // a fresh ⏎ goes through the handshake
	if _, cmd := m.Update(uitest.Key(t, "enter")); !quits(cmd) || m.chosen == nil || m.chosen.Action != "pane.zoom" {
		t.Errorf("a fresh ⏎ chose %+v", m.chosen)
	}
}

// TestHeldEnterGraceIsLeft: a fresh marker leaves the held ⏎ its grace;
// a stillPendingMsg for an earlier ⏎ answers nothing.
func TestHeldEnterGraceIsLeft(t *testing.T) {
	m, dir, _ := waiting(t, true)
	mark(t, dir)
	_, cmd := m.Update(uitest.Key(t, "enter"))
	if cmd == nil || settle(cmd) != nil {
		t.Fatal("a fresh handshake ⏎ must wait out its grace")
	}
	if _, cmd := m.Update(stillPendingMsg{rev: m.pendingRev - 1}); cmd != nil {
		t.Error("a stale stillPendingMsg answered")
	}
	if r := readResult(t, dir); r.Action != "" || m.pendingCommit.IsZero() {
		t.Errorf("answered %q within the grace", r.Action)
	}
}

// TestHeldTypedEnter: a ⏎ typed without the handshake (no marker) waits for
// the keymap as before, then explains that only a handshake ⏎ runs.
func TestHeldTypedEnter(t *testing.T) {
	m, dir, load := waiting(t, true)
	if _, cmd := m.Update(uitest.Key(t, "enter")); cmd != nil {
		t.Error("a typed held ⏎ scheduled a grace")
	}
	m.Update(load())
	if m.chosen != nil || m.pending {
		t.Fatalf("chosen=%v pending=%v", m.chosen, m.pending)
	}
	if got := m.toasts.Current().Text; !strings.Contains(got, "Reopen the guide") {
		t.Errorf("toast %q", got)
	}
	if r := readResult(t, dir); r.Action != "" {
		t.Errorf("answered %q for a typed ⏎", r.Action)
	}
}
