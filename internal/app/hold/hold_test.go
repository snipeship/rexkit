package hold

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/ui"
	"rexkit/internal/ui/uitest"
)

var now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func sampleCard() Card {
	return Card{
		Message: []string{"api is parked in Scratch", "It keeps running there."},
		Hints:   []Hint{{"ctrl+p e", "bring it back here", "close this pane"}, {"ctrl+p w", "visit Scratch", ""}},
		Since:   now.Add(-3 * time.Minute),
	}
}

func screenOf(t *testing.T, c Card, status parked, w, h int) string {
	t.Helper()
	t.Setenv("REXKIT_THEME", "dark")
	m := newModel(c, nil, false)
	m.now = func() time.Time { return now }
	m.status = status
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.render(now)
}

func TestGolden(t *testing.T) {
	working := parked{known: true, state: ui.StatusWorking, app: "claude"}
	tests := []struct {
		name   string
		card   Card
		status parked
		w, h   int
	}{
		{"card-100x30", sampleCard(), working, 100, 30},
		{"blocked-60x14", sampleCard(), parked{known: true, state: ui.StatusBlocked, app: "claude"}, 60, 14},
		{"running-44x12", sampleCard(), parked{known: true, running: "nvim"}, 44, 12},
		{"closed-50x10", sampleCard(), parked{known: true, gone: true}, 50, 10},
		{"short-60x6", sampleCard(), working, 60, 6},
		{"tiny-18x3", sampleCard(), working, 18, 3},
		{"plain-60x9", Card{Message: []string{"Restoring api…"}}, parked{}, 60, 9},
		{"narrow-40x12", sampleCard(), working, 40, 12},
		{"narrow-30x8", sampleCard(), working, 30, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, tt.name, screenOf(t, tt.card, tt.status, tt.w, tt.h))
		})
	}
}

func TestEveryLineFits(t *testing.T) {
	for w := 1; w <= 90; w += 4 {
		for h := 1; h <= 20; h += 2 {
			out := screenOf(t, sampleCard(), parked{known: true, state: ui.StatusIdle}, w, h)
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("%dx%d line %d is %d wide: %q", w, h, i, got, ansi.Strip(l))
				}
			}
			if h >= 1 && w >= 30 && !strings.Contains(out, "api is parked") {
				t.Fatalf("%dx%d lost the headline:\n%s", w, h, ansi.Strip(out))
			}
		}
	}
}

func TestSwallowsEveryKey(t *testing.T) {
	m := newModel(sampleCard(), nil, false)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'c', Mod: tea.ModCtrl}, {Code: 'z', Mod: tea.ModCtrl}, {Code: '\\', Mod: tea.ModCtrl},
		{Code: 'd', Mod: tea.ModCtrl}, {Code: 'q', Text: "q"}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape},
	} {
		if _, cmd := m.Update(k); cmd != nil {
			t.Errorf("%s did something", k)
		}
	}
	if _, cmd := m.Update(tea.PasteMsg{Content: "rm -rf"}); cmd != nil {
		t.Error("paste did something")
	}
	demo := newModel(sampleCard(), nil, true)
	if _, cmd := demo.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Error("q should end a demo")
	}
}

func TestPolling(t *testing.T) {
	looks := 0
	results := []statusMsg{
		{p: parked{known: true, state: ui.StatusWorking}},
		{err: errors.New("rex server unavailable")},
		{p: parked{known: true, gone: true}},
	}
	w := func(context.Context) statusMsg { looks++; return results[looks-1] }
	m := newModel(sampleCard(), w, false)
	if m.Init() == nil {
		t.Fatal("no first poll")
	}
	steps := []struct {
		want     parked
		pollMore bool
	}{
		{results[0].p, true},  // working: poll again, spin
		{results[0].p, true},  // Rex could not tell: keep what we knew
		{results[2].p, false}, // gone: stop looking
	}
	for i, s := range steps {
		_, cmd := m.Update(pollMsg{})
		_, next := m.Update(cmd())
		if m.status != s.want {
			t.Errorf("look %d: status %+v, want %+v", i, m.status, s.want)
		}
		if (next != nil) != s.pollMore {
			t.Errorf("look %d: more polling = %v, want %v", i, next != nil, s.pollMore)
		}
		if i == 0 && !m.spinning {
			t.Error("a working pane should spin")
		}
	}
	if _, cmd := m.Update(spinMsg{}); cmd != nil || m.spinning {
		t.Error("the spinner should stop once the pane is gone")
	}
}

func TestDescribe(t *testing.T) {
	shell := &rex.ProcessInfo{PID: 10, Name: "zsh"}
	tests := []struct {
		name    string
		proc    rex.Process
		records []rex.StatusRecord
		want    string
	}{
		{"shell at its prompt", rex.Process{Child: shell, Foreground: shell}, nil, "○ idle"},
		{"a program in front", rex.Process{Child: shell, Foreground: &rex.ProcessInfo{PID: 11, Name: "nvim"}}, nil, "› running nvim"},
		{"an agent working", rex.Process{Child: shell}, []rex.StatusRecord{{State: rex.StateWorking, App: "claude"}}, "▐⠋ claude working▌"},
		{"the most urgent record wins", rex.Process{}, []rex.StatusRecord{
			{State: rex.StateWorking, App: "a"}, {State: rex.StateBlocked, EffectiveApp: "codex", App: "x"}, {State: rex.StateDone}}, "▐● codex needs you▌"},
		{"failed", rex.Process{}, []rex.StatusRecord{{State: rex.StateError}}, "▐✗ failed▌"},
		{"names are sanitized", rex.Process{Child: shell, Foreground: &rex.ProcessInfo{PID: 2, Name: "evil\x1b]0;x\a"}}, nil, "› running evil"},
	}
	th := ui.NewTheme(true)
	frame0 := time.UnixMilli(0)
	for _, tt := range tests {
		got := strings.TrimRight(ansi.Strip(describe(tt.proc, tt.records).line(th, frame0).Render()), " ")
		if got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestWatchAgainstFake(t *testing.T) {
	fake := rexfake.New()
	sess, _, block := fake.AddSession("work")
	fake.SetTerminal(block, rex.Terminal{
		Process: &rex.Process{Child: &rex.ProcessInfo{PID: 1, Name: "zsh"}, Foreground: &rex.ProcessInfo{PID: 2, Name: "claude"}},
		Status:  rex.List[rex.StatusRecord]{{State: rex.StateBlocked, App: "claude"}},
	})
	got := watch(fake, sess, block)(context.Background())
	if got.err != nil || got.p.state != ui.StatusBlocked || got.p.running != "claude" {
		t.Errorf("watch = %+v", got)
	}
	if got := watch(fake, sess, "block:gone")(context.Background()); !got.p.gone {
		t.Errorf("a missing block should read as gone: %+v", got)
	}
}

func TestFlags(t *testing.T) {
	if got := parseHint(" ⌃P e | bring it back | close it "); got != (Hint{"⌃P e", "bring it back", "close it"}) {
		t.Errorf("parseHint = %+v", got)
	}
	var h hintList
	if err := h.Set("no label"); err == nil {
		t.Error("a hint without | should be rejected")
	}
	if got := lines(`api is parked\nIt keeps running.` + "\n\n third "); !slices.Equal(got, []string{"api is parked", "It keeps running.", "third"}) {
		t.Errorf("lines = %q", got)
	}
	if err := Main([]string{"--since", "yesterday"}); err == nil || !strings.Contains(err.Error(), "--since") ||
		!strings.Contains(err.Error(), "Unix seconds") || strings.Contains(err.Error(), "2006") {
		t.Errorf("bad --since: %v", err)
	}

	// The form the Lua layer passes.
	var lua hintList
	if err := (hintsJSON{&lua}).Set(`[{"key":"ctrl+p e","label":"bring it back here","gone":"close this pane"},{"key":"","label":"dropped"},{"key":"ctrl+p w","label":"visit Scratch"}]`); err != nil {
		t.Fatal(err)
	}
	if want := (hintList{{"ctrl+p e", "bring it back here", "close this pane"}, {"ctrl+p w", "visit Scratch", ""}}); !slices.Equal(lua, want) {
		t.Errorf("--hints = %+v", lua)
	}
	if err := (hintsJSON{&lua}).Set("ctrl+p e|x"); err == nil {
		t.Error("--hints should want JSON")
	}
	if got, err := parseSince("1792986541"); err != nil || !got.Equal(time.Unix(1792986541, 0)) {
		t.Errorf("--since unix = %v, %v", got, err)
	}
	if got, err := parseSince("2026-10-26T10:00:00Z"); err != nil || !got.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("--since RFC3339 = %v, %v", got, err)
	}
}

func TestParkedAgo(t *testing.T) {
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{time.Second, "parked just now"},
		{3 * time.Minute, "parked 3m ago"},
		{5 * time.Hour, "parked 5h ago"},
		{30 * 24 * time.Hour, "parked since Sep 26"},
	}
	for _, tt := range tests {
		if got := parkedAgo(now.Add(-tt.ago), now); got != tt.want {
			t.Errorf("%v: %q, want %q", tt.ago, got, tt.want)
		}
	}
}

// TestPollingBacksOff: while nothing changes the looks thin out, up to
// maxPollEvery; a change, or Rex answering after a failure, brings them back.
func TestPollingBacksOff(t *testing.T) {
	idle := statusMsg{p: parked{known: true, state: ui.StatusIdle}}
	working := statusMsg{p: parked{known: true, state: ui.StatusWorking, app: "claude"}}
	down := statusMsg{err: errors.New("rex server unavailable")}
	m := newModel(sampleCard(), func(context.Context) statusMsg { return idle }, false)
	var got []time.Duration
	for _, msg := range []statusMsg{idle, idle, idle, idle, idle, idle, working, down, down, working, working} {
		m.Update(msg)
		got = append(got, m.every)
	}
	s := time.Second
	want := []time.Duration{5 * s, 10 * s, 20 * s, 40 * s, 60 * s, 60 * s, 5 * s, 10 * s, 20 * s, 5 * s, 10 * s}
	if !slices.Equal(got, want) {
		t.Errorf("waits %v, want %v", got, want)
	}
}

// TestCardIsSanitized: the message names the parked pane, which can be its
// process name; nothing in the card reaches the terminal as an escape.
func TestCardIsSanitized(t *testing.T) {
	evil := "x\x1b]52;c;aGk=\x07\x1b[2J\x1bP+q544e\x1b\\\x1b_Ga=d\x1b\\\u202ey"
	c := Card{
		Message: lines(evil + " is parked in Scratch\nsecond " + evil),
		Hints:   []Hint{{"ctrl+p e\x1b[2J", "bring " + evil, ""}},
	}
	out := screenOf(t, c, parked{}, 100, 20)
	plainOut := ansi.Strip(out)
	if strings.ContainsRune(plainOut, '\x1b') || strings.ContainsRune(plainOut, '\a') {
		t.Errorf("escapes reach the screen: %q", plainOut)
	}
	for _, seq := range []string{"\x1b]", "\x1bP", "\x1b_", "\x1b[2J"} {
		if strings.Contains(out, seq) {
			t.Errorf("the frame carries %q", seq)
		}
	}
	if !strings.Contains(plainOut, "<U+202E>") || !strings.Contains(plainOut, "is parked in Scratch") {
		t.Errorf("card text lost:\n%s", plainOut)
	}
}

// TestCardOrFlat: the card is drawn only when the pane holds all of it with a
// cell to spare around it; otherwise the text sits flat on the pane.
func TestCardOrFlat(t *testing.T) {
	working := parked{known: true, state: ui.StatusWorking, app: "claude"}
	for _, tt := range []struct {
		w, h int
		card bool
	}{{100, 30, true}, {60, 14, true}, {60, 12, false}, {50, 10, false}, {44, 13, true}, {34, 13, false}} {
		out := ansi.Strip(screenOf(t, sampleCard(), working, tt.w, tt.h))
		if got := strings.Contains(out, "🭊"); got != tt.card {
			t.Errorf("%dx%d: card %v, want %v:\n%s", tt.w, tt.h, got, tt.card, out)
		}
	}
}
