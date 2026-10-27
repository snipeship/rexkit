package search

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

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
	"rexkit/internal/ui/uitest"
)

// testNow is the fixed clock of every test, so renders are reproducible.
var testNow = time.Date(2026, 10, 26, 12, 40, 0, 0, time.UTC)

// harness drives a model like Bubble Tea does, synchronously: commands run
// to completion and their messages are fed back.
type harness struct {
	t      *testing.T
	m      *model
	fake   *rexfake.Fake
	origin origin
	tools  string // where spool makes the pages' folders

	mu     sync.Mutex
	closed int
	copied []string
	quit   bool
}

type harnessOption func(*harness, *config)

// withStateDir enables the handshake: Lua opened the tool.
func withStateDir(dir string) harnessOption {
	return func(_ *harness, c *config) { c.stateDir = dir }
}

func withClient(id string) harnessOption {
	return func(_ *harness, c *config) { c.client = id }
}

// withAPI wraps the fake Rex (to fail or hold some calls).
func withAPI(wrap func(*rexfake.Fake) rex.API) harnessOption {
	return func(h *harness, c *config) { c.api = wrap(h.fake) }
}

// withWorld searches another fake Rex.
func withWorld(fake *rexfake.Fake, o origin) harnessOption {
	return func(h *harness, c *config) {
		h.fake, h.origin = fake, o
		c.api, c.origin = fake, o
	}
}

func withFloat(blur bool) harnessOption {
	return func(_ *harness, c *config) { c.floating, c.blur = true, blur }
}

// newHarness starts the tool on the demo world with a w×h terminal, and
// lets it read every pane.
func newHarness(t *testing.T, w, h int, dark bool, opts ...harnessOption) *harness {
	t.Helper()
	hs := newIdle(t, w, h, dark, opts...)
	hs.run(hs.m.Init())
	return hs
}

// newIdle is newHarness before Init: nothing has been read yet.
func newIdle(t *testing.T, w, h int, dark bool, opts ...harnessOption) *harness {
	t.Helper()
	if dark {
		t.Setenv("REXKIT_THEME", "dark")
	} else {
		t.Setenv("REXKIT_THEME", "light")
	}
	fake, o := demoWorld(testNow)
	hs := &harness{t: t, fake: fake, origin: o, tools: t.TempDir()}
	cfg := config{
		api:    fake,
		origin: o,
		home:   demoHome,
		now:    func() time.Time { return testNow },
		spool:  func() (string, error) { return os.MkdirTemp(hs.tools, "") },
		copy: func(_ context.Context, text string) error {
			hs.mu.Lock()
			defer hs.mu.Unlock()
			hs.copied = append(hs.copied, text)
			return nil
		},
		close: func(context.Context) error {
			hs.mu.Lock()
			defer hs.mu.Unlock()
			hs.closed++
			return nil
		},
	}
	for _, opt := range opts {
		opt(hs, &cfg)
	}
	hs.m = newModel(context.Background(), cfg)
	_, _ = hs.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return hs
}

// send delivers a message and runs the commands it returns.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

// cmdTimeout skips commands that wait on a clock of their own (toast and
// spinner ticks, the grace of a held ⏎): tests deliver those themselves.
// While panes are read or searched, commands get workTimeout instead: a
// capture or a search can take longer than cmdTimeout (under -race), and
// the only clock then is the spinner's, which is short.
const (
	cmdTimeout  = 200 * time.Millisecond
	workTimeout = 10 * time.Second
)

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	timeout := cmdTimeout
	if h.m.scanning() || !h.m.ready {
		timeout = workTimeout
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(timeout):
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

func (h *harness) key(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(uitest.Key(h.t, k))
	}
}

func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, msg := range uitest.Type(s) {
		h.send(msg)
	}
}

// commit presses ⏎ the way Lua's handshake writes it: with the commit
// marker in the state dir.
func (h *harness) commit() {
	h.t.Helper()
	mark(h.t, h.m.cfg.stateDir)
	h.key("enter")
}

// mark creates Lua's commit marker for the next ⏎.
func mark(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, popup.CommitFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// result reads the answer the tool wrote for Lua.
func (h *harness) result() popup.Result {
	h.t.Helper()
	var r popup.Result
	if err := state.ReadJSON(filepath.Join(h.m.cfg.stateDir, popup.ResultFile), &r); err != nil {
		h.t.Fatalf("no result: %v", err)
	}
	return r
}

func (h *harness) screen() string { return ansi.Strip(h.m.View().Content) }

// selectedRow describes the selected row: "pane zsh", "more 60", "hit 1233".
func (h *harness) selectedRow() string {
	r, ok := h.m.selected()
	if !ok {
		return ""
	}
	g := h.m.groups[r.group]
	switch r.kind {
	case rowPane:
		return "pane " + h.m.panes[g.pane].name
	case rowMore:
		return fmt.Sprintf("more %d", r.hit)
	case rowGap:
		return "gap"
	}
	return fmt.Sprintf("hit %d", g.hits[r.hit]+1)
}

// selectHit selects the hit on line n (from 1) of the named pane.
func (h *harness) selectHit(name string, n int) {
	h.t.Helper()
	for i, r := range h.m.rows {
		g := h.m.groups[r.group]
		if r.kind == rowHit && h.m.panes[g.pane].name == name && g.hits[r.hit]+1 == n {
			h.m.list.Select(i)
			h.m.syncPreview()
			return
		}
	}
	h.t.Fatalf("no hit on line %d of %s", n, name)
}

// selectKind selects the first row of a kind in the named pane's group.
func (h *harness) selectKind(name string, kind rowKind) {
	h.t.Helper()
	for i, r := range h.m.rows {
		if r.kind == kind && h.m.panes[h.m.groups[r.group].pane].name == name {
			h.m.list.Select(i)
			h.m.syncPreview()
			return
		}
	}
	h.t.Fatalf("no such row in %s", name)
}

// groupNames lists the panes with matches, in order, as "name place".
func (h *harness) groupNames() []string {
	var out []string
	for _, g := range h.m.groups {
		p := h.m.panes[g.pane]
		out = append(out, p.name+" "+place(p))
	}
	return out
}

// blockOf finds the block of the pane named name in the tab tab.
func (h *harness) blockOf(name, tab string) *pane {
	h.t.Helper()
	for _, p := range h.m.panes {
		if p.name == name && p.tabLabel == tab {
			return p
		}
	}
	h.t.Fatalf("no pane %s in %s", name, tab)
	return nil
}

func teaSize(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }
