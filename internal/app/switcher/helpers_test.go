package switcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/attend"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
	"rexkit/internal/ui/uitest"
)

// testNow is the fixed clock of every test, so renders are reproducible.
var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// harness drives a model like Bubble Tea does, synchronously: commands run
// to completion and their messages are fed back. Timers (env.after) are held
// until fire is called, so nothing happens behind a test's back.
type harness struct {
	t      *testing.T
	m      *model
	fake   *rexfake.Fake
	origin place

	carded bool // drawn in a card, as popup.Stage does above 80 columns

	mu       sync.Mutex
	timers   []tea.Msg
	delays   []delay // every timer ever set, with its duration
	closed   int
	quit     bool
	recorded state.History
}

type harnessOption func(*harness, *options)

// delay is a timer the model set.
type delay struct {
	d   time.Duration
	msg tea.Msg
}

// withStateDir enables the handshake by giving the switcher a result dir.
func withStateDir(dir string) harnessOption {
	return func(_ *harness, o *options) { o.stateDir = dir }
}

// withCard puts the switcher in a card, as the stage does: it gets a
// popup.CardMsg before its first size.
func withCard() harnessOption {
	return func(h *harness, _ *options) { h.carded = true }
}

// withSnapshotError makes every snapshot fail, so Rex cannot be read.
func withSnapshotError() harnessOption {
	return func(h *harness, _ *options) {
		h.fake.Fail("snapshot", &rex.Error{Op: "snapshot", Message: "the Rex server is not running"})
	}
}

// withWorld runs the switcher on another demo world (attentionWorld). It
// must come before options that use the fake.
func withWorld(world func(time.Time) (*rexfake.Fake, place, state.History)) harnessOption {
	return func(h *harness, o *options) {
		h.fake, h.origin, h.recorded = world(testNow)
		o.origin = h.origin
	}
}

func withClient(id string) harnessOption {
	return func(_ *harness, o *options) { o.client = id }
}

// withNextKey names another next-agent key ("" for none).
func withNextKey(key string) harnessOption {
	return func(_ *harness, o *options) { o.nextKey = key }
}

// newHarness starts the switcher on the demo world (seen at testNow), with the
// given terminal size and colour scheme.
func newHarness(t *testing.T, w, h int, dark bool, opts ...harnessOption) *harness {
	t.Helper()
	if dark {
		t.Setenv("REXKIT_THEME", "dark")
	} else {
		t.Setenv("REXKIT_THEME", "light")
	}
	fake, origin, history := demoWorld(testNow)
	hs := &harness{t: t, fake: fake, origin: origin, recorded: history}
	e := env{
		api:    fake,
		close:  func(context.Context) error { hs.mu.Lock(); hs.closed++; hs.mu.Unlock(); return nil },
		record: hs.record,
		now:    func() time.Time { return testNow },
		after: func(d time.Duration, msg tea.Msg) tea.Cmd {
			hs.mu.Lock()
			hs.timers = append(hs.timers, msg)
			hs.delays = append(hs.delays, delay{d, msg})
			hs.mu.Unlock()
			return nil
		},
	}
	o := options{origin: origin, home: demoHome, dismissOnBlur: true, nextKey: attend.DefaultNextKey}
	for _, opt := range opts {
		opt(hs, &o)
	}
	e.api, history = hs.fake, hs.recorded
	hs.m = newModel(context.Background(), e, o, nil, history)
	hs.run(hs.m.Init())
	if hs.carded {
		hs.send(popup.CardMsg{})
	}
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	hs.fire()
	return hs
}

func (h *harness) record(fn func(*state.History)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(&h.recorded)
	return nil
}

// send delivers a message and runs the commands it returns.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

// cmdTimeout skips commands that wait on a clock of their own (ui's toast
// and spinner ticks): they would only deliver timers the test controls.
const cmdTimeout = 200 * time.Millisecond

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(cmdTimeout):
		return
	}
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			h.run(c)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		if foreign(msg) {
			return
		}
		h.send(msg)
	}
}

// fire delivers the held timers once, except the endless ones (clock, live
// tail, polling), which tests deliver explicitly.
func (h *harness) fire() {
	h.t.Helper()
	h.mu.Lock()
	timers := h.timers
	h.timers = nil
	h.mu.Unlock()
	for _, msg := range timers {
		switch msg.(type) {
		case clockMsg, tailMsg, pollMsg:
			continue
		}
		h.send(msg)
	}
}

// foreign reports messages the model does not handle itself: terminal
// requests and the timers of ui components.
func foreign(msg tea.Msg) bool {
	name := fmt.Sprintf("%T", msg)
	for _, pkg := range []string{"tea.", "ui.", "uv.", "cursor."} {
		if strings.HasPrefix(name, pkg) {
			return true
		}
	}
	return false
}

// key presses a key given in Bubble Tea notation ("enter", "ctrl+r", "a").
func (h *harness) key(k string) {
	h.t.Helper()
	h.send(uitest.Key(h.t, k))
}

// commit presses ⏎ the way Lua's handshake writes it: with the commit
// marker in the state dir.
func (h *harness) commit() {
	h.t.Helper()
	h.mark()
	h.key("enter")
}

// mark creates Lua's commit marker for the next ⏎.
func (h *harness) mark() {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.m.opts.stateDir, popup.CommitFile), nil, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// marked reports whether the commit marker is still there.
func (h *harness) marked() bool {
	_, err := os.Stat(filepath.Join(h.m.opts.stateDir, popup.CommitFile))
	return err == nil
}

// typeText types text into the query.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// screen renders the model as plain text.
func (h *harness) screen() string {
	return ansi.Strip(h.m.View().Content)
}

// selected returns the selected entry's name.
func (h *harness) selected() string {
	_, e, ok := h.m.selected()
	if !ok {
		return ""
	}
	return e.name
}

// find returns the entry with the given name.
func (h *harness) find(name string) entry {
	h.t.Helper()
	for _, e := range h.m.cat.entries {
		if e.name == name {
			return e
		}
	}
	h.t.Fatalf("no entry named %q", name)
	return entry{}
}

// selectEntry moves the selection to the first row showing name.
func (h *harness) selectEntry(name string) {
	h.t.Helper()
	for i, r := range h.m.listing.rows {
		if h.m.cat.entries[r.entry].name == name {
			before := h.m.selectedKey()
			h.m.list.Select(i)
			if h.m.selectedKey() != before {
				h.run(h.m.selectionChanged())
			}
			return
		}
	}
	h.t.Fatalf("no row named %q in %v", name, h.rowNames())
}

func (h *harness) rowNames() []string {
	var names []string
	for _, r := range h.m.listing.rows {
		names = append(names, h.m.cat.entries[r.entry].name)
	}
	return names
}

// inventory snapshots the fake, for feeding refreshes by hand.
func (h *harness) inventory() rex.Inventory {
	inv, err := h.fake.Snapshot(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return inv
}

// click left-clicks the first place on screen showing text.
func (h *harness) click(text string) {
	h.t.Helper()
	for y, line := range strings.Split(h.screen(), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			h.send(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
			return
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", text, h.screen())
}
