package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestToasterLifetimes(t *testing.T) {
	tests := []struct {
		kind     ToastKind
		timer    bool // expires on a timer
		keyClear bool // a key press clears it
	}{
		{ToastOK, true, false},
		{ToastInfo, true, false},
		{ToastWarn, false, true},
		{ToastErr, false, true},
	}
	for _, tt := range tests {
		var ts Toaster
		cmd := ts.Show(tt.kind, "hello")
		if (cmd != nil) != tt.timer {
			t.Errorf("kind %d: timer=%v, want %v", tt.kind, cmd != nil, tt.timer)
		}
		if ts.Update(tea.KeyPressMsg{Code: 'a'}) {
			t.Errorf("kind %d: the key that dismisses a toast is the model's to handle too", tt.kind)
		}
		if cleared := ts.Current().Text == ""; cleared != tt.keyClear {
			t.Errorf("kind %d: cleared by key=%v, want %v", tt.kind, cleared, tt.keyClear)
		}
	}
}

func TestToasterIgnoresStaleExpiry(t *testing.T) {
	var ts Toaster
	ts.Show(ToastOK, "first")
	stale := toastExpiredMsg{ts.current.id}
	ts.Show(ToastOK, "second")
	if !ts.Update(stale) || ts.Current().Text != "second" {
		t.Fatal("an old timer retired a newer toast (or was handed to the model)")
	}
	if !ts.Update(toastExpiredMsg{ts.current.id}) || ts.Current().Text != "" {
		t.Fatal("the current toast did not expire")
	}
}

func TestTrayToastReplacesMeta(t *testing.T) {
	th := NewTheme(true)
	hints := []Hint{{Key: "enter", Label: "open", Primary: true}, {Key: "tab", Label: "filter"}, {Key: "ctrl+/", Label: "preview"}, {Key: "?", Label: "keys"}}
	tray := Tray{Meta: th.MetaCount(26, 26, "place"), Hints: hints, Toast: Toast{Kind: ToastErr, Text: "That pane closed"}}
	for _, w := range []int{0, 1, 10, 30, 60, 120} {
		for _, h := range []int{1, TrayRows, TrayAirRows} {
			lines := th.Tray(tray, w, h)
			if w == 0 {
				if lines != nil {
					t.Fatalf("width 0: %d lines", len(lines))
				}
				continue
			}
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for _, l := range lines {
				if cw := ansi.StringWidth(l); cw != w {
					t.Fatalf("%dx%d: a line is %d cells", w, h, cw)
				}
			}
			row := ansi.Strip(lines[min(1, h-1)])
			if strings.Contains(row, "26 places") {
				t.Errorf("%dx%d: the meta shows under a toast: %q", w, h, row)
			}
			if w >= 40 && !strings.Contains(row, "✗ That pane closed") {
				t.Errorf("%dx%d: error toast hidden: %q", w, h, row)
			}
			if w >= 40 && !strings.Contains(row, "enter") {
				t.Errorf("%dx%d: the primary hint is gone: %q", w, h, row)
			}
		}
	}
}

// A toast's way out survives a short tray: its text is cut first.
func TestTrayToastKeepsItsWays(t *testing.T) {
	th := NewTheme(true)
	tray := Tray{
		Hints: []Hint{{Key: "enter", Label: "go", Primary: true}, {Key: "?", Label: "keys"}},
		Toast: Toast{Kind: ToastWarn, Text: "faraway is another session", Ways: []Hint{{Key: "ctrl+shift+space", Label: "to jump there"}}},
	}
	row := ansi.Strip(th.Tray(tray, 79, TrayRows)[1])
	if !strings.Contains(row, "ctrl shift space") || !strings.Contains(row, "to jump there") || !strings.Contains(row, "⚠ faraway") {
		t.Errorf("the way out is cut: %q", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("the text is not cut: %q", row)
	}
}

func TestTrayMetaGivesWayFirst(t *testing.T) {
	th := NewTheme(true)
	tray := Tray{Meta: th.MetaCount(2, 26, "place"), Hints: []Hint{{Key: "enter", Label: "go", Primary: true}, {Key: "?", Label: "keys"}}}
	wide := ansi.Strip(th.Tray(tray, 80, 1)[0])
	if !strings.HasPrefix(wide, " 2 of 26") || !strings.HasSuffix(strings.TrimRight(wide, " "), "keys") {
		t.Fatalf("meta left, hints right: %q", wide)
	}
	hw := th.Hints(tray.Hints, 1<<20).Width()
	narrow := ansi.Strip(th.Tray(tray, hw+2+hintGap+3, 1)[0])
	if strings.Contains(narrow, "of") || !strings.Contains(narrow, "keys") {
		t.Errorf("a short tray drops the meta before a hint: %q", narrow)
	}
}

// A verb in flight keeps its place in a short tray: the hints after the
// primary drop for it, and "? keys" stays.
func TestTrayBusyOutranksHints(t *testing.T) {
	th := NewTheme(true)
	tray := Tray{
		Meta: th.MetaCount(26, 26, "place"),
		Busy: th.Spinning("Going…", time.Time{}),
		Hints: []Hint{{Key: "enter", Label: "go", Outcome: "to studio", Primary: true}, {Key: "tab", Label: "filter"},
			{Key: "ctrl+/", Label: "preview"}, {Key: "?", Label: "keys"}},
	}
	got := ansi.Strip(th.Tray(tray, 66, TrayRows)[1])
	if !strings.Contains(got, "Going…") || !strings.Contains(got, "keys") || !strings.Contains(got, "go") {
		t.Errorf("busy tray at 66: %q", got)
	}
	if strings.Contains(got, "preview") {
		t.Errorf("the hints did not give way to busy: %q", got)
	}
}

// An Urgent meta (a warning) and an Urgent ok toast keep their place as
// a busy state does; "F1 keys" stays as "? keys" does.
func TestTrayUrgentOutranksHints(t *testing.T) {
	th := NewTheme(true)
	hints := []Hint{{Key: "enter", Label: "jump", Outcome: "to line 1233", Primary: true}, {Key: "ctrl+y", Label: "copy line"},
		{Key: "tab", Label: "this session"}, {Key: "f1", Label: "keys"}}
	meta := Line{{Text: "90 matches in 5 panes · 1 pane could not be read", Style: th.Warn}}
	for _, tr := range []Tray{{Meta: meta, Hints: hints}, {Toast: Toast{Kind: ToastOK, Text: "Copied line 1233 of zsh"}, Hints: hints}} {
		if got := ansi.Strip(th.Tray(tr, 80, 1)[0]); strings.Contains(got, "could not") || strings.Contains(got, "of zsh") {
			t.Fatalf("not urgent, the hints win: %q", got)
		}
		tr.Urgent = true
		got := ansi.Strip(th.Tray(tr, 80, 1)[0])
		if !strings.Contains(got, "1 pane could not be read") && !strings.Contains(got, "Copied line 1233 of zsh") {
			t.Errorf("urgent at 80: %q", got)
		}
		if !strings.Contains(got, "jump") || !strings.Contains(got, "keys") || strings.Contains(got, "this session") {
			t.Errorf("hints at 80: %q", got)
		}
	}
}

func TestHintsDropOrder(t *testing.T) {
	th := NewTheme(true)
	hints := []Hint{
		{Key: "enter", Label: "new tab", Outcome: "in", Path: "~/Coding/snipeship/website", Primary: true},
		{Key: "ctrl+e", Label: "edit"},
		{Key: "tab", Label: "filter"},
		{Key: "?", Label: "keys"},
	}
	full := ansi.Strip(th.Hints(hints, 1<<20).Render())
	tests := []struct {
		w          int
		has, lacks []string
	}{
		{len([]rune(full)), []string{"in ~/Coding/snipeship/website", "edit", "filter", "keys"}, nil},
		{len([]rune(full)) - 10, []string{"…", "edit", "filter", "keys"}, []string{"snipeship"}},
		{44, []string{"new tab in ~/", "…/website", "keys"}, []string{"filter"}},
		{27, []string{"new tab", "keys"}, []string{" in ", "edit", "filter"}},
	}
	for _, tt := range tests {
		got := th.Hints(hints, tt.w)
		if got.Width() > tt.w {
			t.Errorf("w=%d: %d cells", tt.w, got.Width())
		}
		s := ansi.Strip(got.Render())
		for _, want := range tt.has {
			if !strings.Contains(s, want) {
				t.Errorf("w=%d: %q lacks %q", tt.w, s, want)
			}
		}
		for _, bad := range tt.lacks {
			if strings.Contains(s, bad) {
				t.Errorf("w=%d: %q still has %q", tt.w, s, bad)
			}
		}
	}
}

// A name in an outcome is never cut: the other hints drop first, then the
// outcome goes whole, leaving the verb.
func TestHintsKeepNames(t *testing.T) {
	th := NewTheme(true)
	hints := []Hint{
		{Key: "enter", Label: "go", Outcome: "to 研究ノート", Primary: true},
		{Key: "tab", Label: "filter"},
		{Key: "ctrl+/", Label: "preview"},
		{Key: "?", Label: "keys"},
	}
	full := th.Hints(hints, 1<<20).Width()
	least := th.Hints([]Hint{{Key: "enter", Label: "go"}, {Key: "?", Label: "keys"}}, 1<<20).Width()
	for w := full - 1; w >= least; w-- {
		s := ansi.Strip(th.Hints(hints, w).Render())
		if strings.Contains(s, "…") || strings.Contains(s, " to ") && !strings.Contains(s, "to 研究ノート") {
			t.Fatalf("w=%d: the name was cut: %q", w, s)
		}
		if strings.Contains(s, "to 研究ノート") && strings.Contains(s, "preview") && w < full {
			t.Errorf("w=%d: preview kept while the outcome must shorten: %q", w, s)
		}
		if !strings.Contains(s, "keys") || !strings.Contains(s, " go") {
			t.Errorf("w=%d: lost the primary or ? keys: %q", w, s)
		}
	}
}

func TestToastStrip(t *testing.T) {
	th := NewTheme(true)
	to := Toast{Kind: ToastWarn, Text: "1 conflict in ~/code/studio, see the session for the details"}
	for _, h := range []int{1, 2, 3, 4, 5} {
		lines := th.ToastStrip(to, 70, h, ToastRows)
		if len(lines) != h {
			t.Fatalf("h=%d: %d lines", h, len(lines))
		}
		text := -1
		for y, l := range lines {
			if ansi.StringWidth(l) != 70 {
				t.Fatalf("h=%d: row %d is %d cells", h, y, ansi.StringWidth(l))
			}
			if strings.Contains(ansi.Strip(l), "conflict") {
				text = y
			}
		}
		want := map[int]int{1: 0, 2: 0, 3: 1, 4: 2, 5: 2}[h]
		if text != want {
			t.Errorf("h=%d: text on row %d, want %d", h, text, want)
		}
		row := ansi.Strip(lines[want])
		if used := len([]rune(strings.TrimLeft(row, " "))); float64(used) > 70*ToastMaxShare+ToastMarginCols {
			t.Errorf("h=%d: toast is %d cells wide", h, used)
		}
		if !strings.HasSuffix(row, strings.Repeat(" ", ToastMarginCols)) {
			t.Errorf("h=%d: no margin on the right: %q", h, row)
		}
	}
	if lines := th.ToastStrip(to, 70, 5, 1); strings.Contains(ansi.Strip(strings.Join(lines, "")), "conflict") {
		t.Error("the first frame shows only the top edge")
	}
}

func TestSpinnerDelay(t *testing.T) {
	th := NewTheme(true)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var s Spinner
	if s.Start(start) == nil {
		t.Fatal("Start should return a tick")
	}
	if s.Visible(start.Add(100*time.Millisecond)) || len(s.View(th, start.Add(100*time.Millisecond))) != 0 {
		t.Error("spinner shown before 120ms")
	}
	if !s.Visible(start.Add(SpinnerDelay)) {
		t.Error("spinner hidden after 120ms")
	}
	s.Start(start.Add(time.Second)) // already running: keeps its start
	if !s.Visible(start.Add(SpinnerDelay)) {
		t.Error("restarting reset the delay")
	}
	s.Stop()
	if s.Visible(start.Add(time.Hour)) || s.Active() {
		t.Error("stopped spinner still visible")
	}
	if SpinnerFrame(start) == SpinnerFrame(start.Add(SpinnerInterval)) {
		t.Error("frames should advance with time")
	}
}

// One spinner runs one tick chain, however often loading starts and stops.
func TestSpinnerRunsOneTickChain(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var s Spinner
	chains := 0
	start := func() {
		if s.Start(now) != nil {
			chains++
		}
	}
	start()
	if chains != 1 {
		t.Fatal("the first Start must schedule a tick")
	}
	if s.Tick() == nil { // the model owns the chain from now on
		t.Fatal("Tick while loading must schedule the next tick")
	}
	start()  // a second load while the first runs
	s.Stop() // the first load ends…
	start()  // …and another begins within the same frame
	if chains != 1 {
		t.Errorf("%d chains for overlapping loads, want 1", chains)
	}
	if s.Tick() == nil {
		t.Fatal("still loading: the chain must go on")
	}
	s.Stop()
	if s.Tick() != nil {
		t.Error("Tick after Stop must end the chain")
	}
	start()
	if chains != 2 {
		t.Error("after the chain ended, Start must schedule a new one")
	}
}

// Ticks started before the model first called Tick merge into one chain.
func TestSpinnerMergesEarlyChains(t *testing.T) {
	var s Spinner
	now := time.Now()
	a, b := s.Start(now), s.Start(now)
	if a == nil || b == nil {
		t.Fatal("before the first Tick, Start always returns a tick")
	}
	if s.Tick() != nil {
		t.Error("the first of two chains must end")
	}
	if s.Tick() == nil {
		t.Error("the last chain must go on")
	}
}

// A key in a toast's message is drawn as keycaps, a cap a step.
func TestToastStripKeycaps(t *testing.T) {
	th := NewTheme(true)
	got := ansi.Strip(strings.Join(th.ToastStrip(Toast{Kind: ToastInfo, Text: "Parked api · {key:ctrl+p e} brings it back"}, 90, 4, ToastRows), "\n"))
	if !strings.Contains(got, "Parked api · ▐ctrl p▌ ▐e▌ brings it back") {
		t.Errorf("toast:\n%s", got)
	}
}
