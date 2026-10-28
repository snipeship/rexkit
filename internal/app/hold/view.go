package hold

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// Card geometry.
const (
	padX    = 2 // air inside the card's side columns: text starts 3 cells in
	marginX = 1 // the least pane left around the card, each side: its edge (ui.Edged)
	marginY = 1
)

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if !m.scheme.Settled() || m.w <= 0 || m.h <= 0 {
		return v
	}
	// The pane keeps Rex's own background; the card is raised on it.
	v.Content = ui.Bleed(m.render(m.now()))
	return v
}

// render centres the card in the pane: exactly h lines of w cells. The card
// has a row of air above and below its text; when the pane cannot hold it
// with a cell to spare around it, the text sits flat on the pane, dropping
// what does not fit (fit).
func (m *model) render(now time.Time) string {
	if box := m.onCard(m.sections(now, m.w-2*marginX-2-2*padX)); box != nil {
		return place(box, m.w, m.h)
	}
	rows := fit(m.sections(now, m.w), m.h)
	width := 0
	for i, r := range rows {
		rows[i] = trimEnd(r).Truncate(m.w)
		width = max(width, rows[i].Width())
	}
	box := make([]string, len(rows))
	for i, r := range rows {
		box[i] = r.Fit(width).Render()
	}
	return place(box, m.w, m.h)
}

// onCard draws sections on the card (the Card profile, on the pane),
// or returns nil when the whole card does not fit in the pane.
func (m *model) onCard(sections [][]ui.Line) []string {
	rows := fit(sections, m.h-2*marginY-2)
	if len(rows) == 0 || len(rows[0]) > 0 || !complete(rows, sections) {
		return nil // no air around the text, or something was dropped
	}
	width := 0
	for _, r := range rows {
		width = max(width, trimEnd(r).Width())
	}
	cardW := width + 2*padX + 2
	if cardW > m.w-2*marginX {
		return nil
	}
	body := make([]string, len(rows))
	for i, r := range rows {
		body[i] = ui.Line{ui.Space(padX)}.Append(r...).Render()
	}
	f := ui.Frame{Card: true}
	return m.theme.Edged(strings.Split(f.Render(m.theme, cardW, len(rows)+2, nil, body, nil), "\n"), ui.Clear)
}

// complete reports whether rows hold every line of sections.
func complete(rows []ui.Line, sections [][]ui.Line) bool {
	lines, shown := 0, 0
	for _, s := range sections {
		lines += len(s)
	}
	for _, r := range rows {
		if len(r) > 0 {
			shown++
		}
	}
	return shown == lines
}

// trimEnd drops a line's trailing blanks (the status word's padding), so
// cutting a line never ends it in "…" for blanks alone.
func trimEnd(l ui.Line) ui.Line {
	out := l[:len(l):len(l)]
	for len(out) > 0 {
		last := out[len(out)-1]
		text := strings.TrimRight(last.Text, " ")
		if text != "" {
			out = append(out[:len(out)-1:len(out)-1], ui.Span{Text: text, Style: last.Style})
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// sections are the card's blocks, most important first: the message, the
// hints, the time and status line (metaSection). Once the parked pane has
// closed, the message says so and the hints say what is left to do (Hint.Gone).
// The message and the labels come from
// outside (the parked pane's name can be its process name), so they are
// sanitized and their escapes drawn apart.
func (m *model) sections(now time.Time, w int) [][]ui.Line {
	t := m.theme
	escaped := func(s string, base lipgloss.Style) ui.Line {
		text, escapes := sanitize.LabelSpans(s)
		return t.Escaped(text, escapes, base)
	}
	var out [][]ui.Line
	var msg []ui.Line
	for i, text := range m.card.Message {
		if i == 0 {
			msg = append(msg, ui.Line{{Text: "◇", Style: t.Accent}, ui.Space(2)}.Append(escaped(text, t.Text.Bold(true))...))
			continue
		}
		msg = append(msg, ui.Line{ui.Space(3)}.Append(escaped(text, t.Muted)...))
	}
	shown := m.card.Hints
	if m.status.gone {
		// "It keeps running there" is no longer true, and only the keys
		// that still lead somewhere stay, saying what they do now.
		msg = append(msg[:min(1, len(msg))], ui.Line{ui.Space(3), {Text: "It has exited since.", Style: t.Muted}})
		shown = nil
		for _, h := range m.card.Hints {
			if h.Gone != "" {
				shown = append(shown, Hint{Key: h.Key, Label: h.Gone})
			}
		}
	}
	out = append(out, msg)

	keyW := 0
	for _, h := range shown {
		keyW = max(keyW, ansi.StringWidth(ui.KeyLabel(sanitize.Label(h.Key))))
	}
	var hints []ui.Line
	for _, h := range shown {
		// Glyph keys without caps: caps stacked row on row would merge into
		// one block.
		key := ui.Styled(ui.KeyLabel(sanitize.Label(h.Key)), t.Text).Fit(keyW)
		hints = append(hints, ui.Line{ui.Space(3)}.Append(key...).Append(ui.Space(3)).Append(escaped(h.Label, t.Muted)...))
	}
	out = append(out, hints)
	return append(out, m.metaSection(now, w))
}

// metaSection is the card's last block: when the pane was parked and its
// status, which loses the time when it is wider than w; empty without
// either.
func (m *model) metaSection(now time.Time, w int) []ui.Line {
	t := m.theme
	meta := ui.Line{ui.Space(3)}
	status := m.status.line(t, now)
	if !m.card.Since.IsZero() {
		meta = append(meta, ui.Span{Text: parkedAgo(m.card.Since, now), Style: t.Subtle})
		if len(status) > 0 {
			meta = append(meta, ui.Space(3))
		}
	}
	if meta = append(meta, status...); trimEnd(meta).Width() > w && len(status) > 0 {
		meta = append(ui.Line{ui.Space(3)}, status...)
	}
	if len(meta) <= 1 {
		return nil
	}
	return []ui.Line{meta}
}

// fit lays sections out in at most h rows: blank rows around and between
// them while there is room, then sections are dropped from the end, then
// lines from the end of the first.
func fit(sections [][]ui.Line, h int) []ui.Line {
	var nonEmpty [][]ui.Line
	for _, s := range sections {
		if len(s) > 0 {
			nonEmpty = append(nonEmpty, s)
		}
	}
	for len(nonEmpty) > 0 {
		lines := 0
		for _, s := range nonEmpty {
			lines += len(s)
		}
		gaps := len(nonEmpty) - 1
		switch {
		case lines+gaps+2 <= h:
			return join(nonEmpty, true, true)
		case lines+gaps <= h:
			return join(nonEmpty, true, false)
		case lines <= h:
			return join(nonEmpty, false, false)
		case len(nonEmpty) > 1:
			nonEmpty = nonEmpty[:len(nonEmpty)-1]
		default:
			return nonEmpty[0][:max(0, h)]
		}
	}
	return nil
}

func join(sections [][]ui.Line, gaps, edges bool) []ui.Line {
	var out []ui.Line
	if edges {
		out = append(out, nil)
	}
	for i, s := range sections {
		if i > 0 && gaps {
			out = append(out, nil)
		}
		out = append(out, s...)
	}
	if edges {
		out = append(out, nil)
	}
	return out
}

// place centres box (lines of equal width) in a w×h screen.
func place(box []string, w, h int) string {
	boxW := 0
	if len(box) > 0 {
		boxW = ansi.StringWidth(box[0])
	}
	left := strings.Repeat(" ", max(0, (w-boxW)/2))
	top := max(0, (h-len(box))/2)
	blank := strings.Repeat(" ", w)
	out := make([]string, h)
	for y := range out {
		out[y] = blank
		if i := y - top; i >= 0 && i < len(box) {
			out[y] = ui.Fit(left+box[i], w)
		}
	}
	return strings.Join(out, "\n")
}

// parkedAgo says when the pane was parked: "parked just now", "parked 3m
// ago", "parked since Oct 3".
func parkedAgo(since, now time.Time) string {
	ago := ui.RelTimeAt(since, now)
	switch {
	case ago == "now":
		return "parked just now"
	case now.Sub(since) < 7*24*time.Hour:
		return "parked " + ago + " ago"
	}
	return "parked since " + ago
}
