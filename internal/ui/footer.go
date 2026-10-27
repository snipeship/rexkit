package ui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

// ToastKind is what a toast reports.
type ToastKind int

// Toast kinds.
const (
	ToastInfo ToastKind = iota
	ToastOK
	ToastWarn
	ToastErr
)

// Toast lifetimes. Warnings and errors stay until the next key press.
const (
	ToastOKTTL   = 2500 * time.Millisecond
	ToastInfoTTL = 4 * time.Second
)

// Toast is a short message: in the tray, where it replaces the meta, or on
// its own (rexkit toast, ToastStrip).
type Toast struct {
	Kind ToastKind
	Text string
	// Ways are keys that act on what the toast says, drawn as hints after
	// it ("▐ctrl r▌ refresh").
	Ways []Hint
	// Status, when set, makes it an agent's toast: the tile carries the
	// agent's state shape on that state's fill (● on the solid warn fill for
	// blocked, ✗ on the solid error fill for failed, ✓ on the quiet OK
	// container for done), the same shapes Go to's rows use.
	Status Status
	id     int
}

// toastGlyph is a toast kind's glyph and colour.
func (t Theme) toastGlyph(k ToastKind) Span {
	switch k {
	case ToastOK:
		return Span{GlyphOK, t.OK}
	case ToastWarn:
		return Span{GlyphWarn, t.Warn}
	case ToastErr:
		return Span{GlyphFailed, t.Err}
	}
	return Span{GlyphInfo, t.Info}
}

// Toast renders a toast as a line: the kind's glyph in its colour, the text
// in the primary colour (bold for an error), then its ways out:
// "✗ That pane closed   ▐ctrl r▌ refresh".
func (t Theme) Toast(to Toast) Line { return t.toastCut(to, 1<<30) }

// toastCut is Toast with its text cut to w cells. The ways out are never cut:
// they are what the toast asks the person to do.
func (t Theme) toastCut(to Toast, w int) Line {
	if to.Text == "" {
		return nil
	}
	text := t.Text
	if to.Kind == ToastErr {
		text = t.Text.Bold(true)
	}
	line := Line{t.toastGlyph(to.Kind), Space(1)}.Append(Line{{to.Text, text}}.Truncate(w)...)
	if len(to.Ways) > 0 {
		line = append(line, Space(hintGap))
		line = append(line, t.Hints(to.Ways, 1<<30)...)
	}
	return line
}

// toastExpiredMsg retires toast id.
type toastExpiredMsg struct{ id int }

// Toaster holds the current toast and its lifetime: ok and info toasts expire
// on a timer, warnings and errors on the next key press.
type Toaster struct {
	current Toast
	seq     int
}

// Show replaces the current toast and returns the command that expires it.
// ways are keys that act on it (Toast.Ways).
func (ts *Toaster) Show(kind ToastKind, text string, ways ...Hint) tea.Cmd {
	ts.seq++
	ts.current = Toast{Kind: kind, Text: text, Ways: ways, id: ts.seq}
	ttl := ToastInfoTTL
	switch kind {
	case ToastOK:
		ttl = ToastOKTTL
	case ToastWarn, ToastErr:
		return nil
	}
	id := ts.seq
	return tea.Tick(ttl, func(time.Time) tea.Msg { return toastExpiredMsg{id} })
}

// Update retires toasts. It reports whether msg was the toaster's own (an
// expiry timer), which the model need not look at; every other message,
// key presses included, is the model's to handle. A key press also clears a
// warning or error — it dismisses the toast and still does its job, so
// typing after an error loses no keystroke and ⏎ or ⎋ still get answered.
func (ts *Toaster) Update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case toastExpiredMsg:
		if msg.id == ts.current.id {
			ts.current = Toast{}
		}
		return true
	case tea.KeyPressMsg:
		if k := ts.current.Kind; ts.current.Text != "" && (k == ToastErr || k == ToastWarn) {
			ts.current = Toast{}
		}
	}
	return false
}

// Current returns the toast to show (zero when none).
func (ts *Toaster) Current() Toast { return ts.current }

// Tray is the bottom of every tool: the meta on the left
// and key hints on the right. A toast, or else a busy state, replaces the
// meta; the hints stay.
type Tray struct {
	Meta  Line   // "26 places", "2 of 26" (MetaCount), a conflict note in Warn
	Hints []Hint // the primary first, "? keys" last; dialogs put the primary last (Mac order)
	Toast Toast  // replaces the meta while it is shown
	Busy  Line   // replaces the meta while a verb runs: Spinning("Opening…", now)
}

// Tray heights: TrayRows for the banded tray (its lip and the hints row),
// one more for a tray along the window's bottom (a row of air in the tray
// colour, so the hints do not sit on the window's last row), one for the
// compact chrome.
const (
	TrayRows    = 2
	TrayAirRows = 3
)

// minToast is the narrowest a toast in the tray is cut to before it is
// dropped.
const minToast = 6

// Tray renders the tray as h lines of w cells: h = 1 is the compact tray (one
// row on the card), 2 the banded one (a lip in the tray colour over the
// card, then the row on the tray colour), 3 the banded one with a row of air
// under it. When the row is short the meta gives way first, then the hints
// (Hints); an error or warning toast, or the busy state, keeps its place
// before the hints. A
// meta is shown whole or not at all (paths in it are the tool's to
// middle-truncate); a toast is cut with "…".
func (t Theme) Tray(tr Tray, w, h int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	th, bg := t, t.C.Surface
	if h >= TrayRows {
		th, bg = t.InTray(), t.C.Tray
	}
	left := tr.Meta
	switch {
	case tr.Toast.Text != "":
		left = th.Toast(tr.Toast)
	case len(tr.Busy) > 0:
		left = tr.Busy
	}
	room := w - 2 // a cell of margin each side
	// An error, a warning or a verb in flight keeps its place: the hints
	// give way to it, not it to them.
	urgent := tr.Toast.Text != "" && (tr.Toast.Kind == ToastErr || tr.Toast.Kind == ToastWarn) ||
		tr.Toast.Text == "" && len(tr.Busy) > 0
	hintRoom := room
	if urgent {
		hintRoom = room - min(left.Width(), room*2/3) - hintGap
	}
	hints := th.Hints(tr.Hints, max(0, hintRoom))
	leftRoom := room - hints.Width()
	if len(hints) > 0 {
		leftRoom -= hintGap
	}
	// A toast with ways out gives up its text before them: "⚠ faraway is
	// an…   ▐ctrl shift space▌ to jump there", not "▐ctrl shift spa…".
	if text := (Line{{Text: tr.Toast.Text}}).Width(); len(tr.Toast.Ways) > 0 && left.Width() > leftRoom &&
		text-(left.Width()-leftRoom) >= minToast {
		left = th.toastCut(tr.Toast, text-(left.Width()-leftRoom))
	}
	// A meta that does not fit is dropped whole ("8 plac…" says nothing);
	// a toast is cut, down to minToast.
	if left.Width() > leftRoom && (tr.Toast.Text == "" || leftRoom < min(minToast, left.Width())) {
		left = nil
	}
	left = left.Truncate(max(0, leftRoom))
	row := Line{Space(1)}.Append(left...)
	row = row.Fit(w - 1 - hints.Width()).Append(hints...).Fit(w)
	if h == 1 {
		return []string{row.Render()}
	}
	out := []string{t.trayLip(w), Paint(row.RenderBg(bg), bg)}
	for len(out) < h {
		out = append(out, lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", w)))
	}
	return out
}

// trayLip is the tray's top edge across w cells: its lip in the tray colour
// over the card.
func (t Theme) trayLip(w int) string {
	return straightEdge(w, true, t.C.Tray, t.C.Surface)
}

// MetaCount is the tray's count: "26 places" when nothing is filtered, "2 of
// 26" when it is, with the numbers in the secondary colour and the words in
// the tertiary one. noun is singular; "s" is added for other counts.
func (t Theme) MetaCount(shown, total int, noun string) Line {
	if shown == total {
		words := strings.TrimPrefix(Plural(total, noun), strconv.Itoa(total))
		return Line{{strconv.Itoa(total), t.Muted}, {words, t.Subtle}}
	}
	return Line{{strconv.Itoa(shown), t.Muted}, {" of ", t.Subtle}, {strconv.Itoa(total), t.Muted}}
}

// Spinning is a busy or loading state: the spinner in the OK colour, then
// label in the secondary colour ("⠋ Opening…", "⠋ Updating").
func (t Theme) Spinning(label string, now time.Time) Line {
	return Line{{SpinnerFrame(now), t.OK}, Space(1), {label, t.Muted}}
}
