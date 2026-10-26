package install

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"rexkit/internal/ui"
)

// Mark is the verdict of one line of a checklist.
type Mark int

const (
	MarkOK   Mark = iota // ✓ fine
	MarkWarn             // ⚠ works, but needs attention
	MarkFail             // ✗ broken: doctor exits non-zero
	MarkOff              // ○ an optional feature is off
	MarkPlan             // · a dry run's planned step
)

func (m Mark) glyph() string {
	return [...]string{ui.GlyphOK, ui.GlyphWarn, ui.GlyphFailed, ui.GlyphOff, ui.Dot}[m]
}

// Line is one checklist entry: a verdict, a short name, what was found and,
// when something is wrong, how to fix it.
type Line struct {
	Mark   Mark
	Name   string
	Detail string
	Fix    string
}

// nameWidth aligns details; it fits the longest check name.
const nameWidth = 17

// printer writes checklists in the design system's colours, or plain text
// when output is not a terminal.
type printer struct {
	env   *Env
	marks [5]lipgloss.Style
	title lipgloss.Style
	muted lipgloss.Style
	fix   lipgloss.Style
}

func (e *Env) printer() printer {
	p := printer{env: e}
	if !e.Color {
		return p
	}
	t := ui.NewTheme(e.Dark)
	p.marks = [5]lipgloss.Style{t.OK, t.Warn, t.Err, t.Subtle, t.Subtle}
	p.title = lipgloss.NewStyle().Bold(true)
	p.muted = t.Muted
	p.fix = t.Accent
	return p
}

// heading prints a bold title with muted context after it.
func (p printer) heading(title string, context ...string) {
	s := p.title.Render(title)
	if len(context) > 0 {
		s += p.muted.Render(" · " + strings.Join(context, " · "))
	}
	fmt.Fprintf(p.env.Out, "%s\n\n", s)
}

func (p printer) line(l Line) {
	name := l.Name + strings.Repeat(" ", max(1, nameWidth-len([]rune(l.Name))))
	fmt.Fprintf(p.env.Out, "  %s %s%s\n", p.marks[l.Mark].Render(l.Mark.glyph()), name, l.Detail)
	if l.Fix != "" {
		fmt.Fprintf(p.env.Out, "    %s%s\n", strings.Repeat(" ", nameWidth), p.fix.Render("→ "+l.Fix))
	}
}

func (p printer) note(text string) {
	fmt.Fprintf(p.env.Out, "\n%s\n", p.muted.Render(text))
}

// summary prints the closing count of a checklist.
func (p printer) summary(lines []Line) {
	var n [5]int
	for _, l := range lines {
		n[l.Mark]++
	}
	var parts []string
	if n[MarkFail] > 0 {
		parts = append(parts, p.marks[MarkFail].Render(ui.Plural(n[MarkFail], "problem")))
	}
	if n[MarkWarn] > 0 {
		parts = append(parts, p.marks[MarkWarn].Render(ui.Plural(n[MarkWarn], "warning")))
	}
	if n[MarkOff] > 0 {
		parts = append(parts, ui.Plural(n[MarkOff], "optional feature")+" off")
	}
	if len(parts) == 0 {
		parts = append(parts, p.marks[MarkOK].Render("All good."))
	}
	fmt.Fprintf(p.env.Out, "\n  %s\n", strings.Join(parts, p.muted.Render(" · ")))
}

// ago renders how long ago t was, as prose: "just now", "3m ago", "on Oct 3".
func ago(t, now time.Time) string {
	rel := ui.RelTimeAt(t, now)
	switch strings.TrimLeft(rel, "0123456789") {
	case "now":
		return "just now"
	case "s", "m", "h", "d":
		return rel + " ago"
	}
	return "on " + rel
}
