package scrollback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/find"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
	"rexkit/nvim"
)

// captureTimeout bounds reading a pane: Rex keeps up to 64 MiB of
// scrollback, which takes well under a second to export.
const captureTimeout = 30 * time.Second

// prepared is everything the viewer needs, ready on disk.
type prepared struct {
	page    page
	file    string // the spooled page
	profile string // the spooled Neovim profile (empty for less)
	program program
	at      opening
}

// target is a line of the capture to open at, and what to search for.
type target struct {
	line   int
	search find.Query
}

// opening is where the viewer opens instead of its start: a line of the
// page (0 for none) and a Vim pattern to search for ("" for none).
type opening struct {
	line    int
	pattern string
}

// command returns the viewer command for this run.
func (p prepared) command(start string, t ui.Theme) command {
	if p.program.less {
		return lessCommand(p.program.path, p.file, start, p.at.line)
	}
	return nvimCommand(p.program.path, p.profile, p.file, start, p.page, p.at, t)
}

// open finds where in pg to open for tg: its line in the page, and the
// search as a Vim pattern. A regular expression Vim cannot say the same way
// searches for the text it matched on that line instead.
func open(pg page, tg target) opening {
	o := opening{line: pg.at(tg.line)}
	if tg.search.Text == "" {
		return o
	}
	if pat, ok := tg.search.Vim(); ok {
		o.pattern = pat
		return o
	}
	if o.line == 0 {
		return o
	}
	m, err := tg.search.Compile()
	if err != nil {
		return o
	}
	line := pg.plainLine(o.line)
	if spans := m.Spans(line); len(spans) > 0 {
		o.pattern, _ = find.Query{Text: line[spans[0][0]:spans[0][1]], MatchCase: true}.Vim()
	}
	return o
}

// job produces the prepared viewer; the loader runs it while the terminal
// reports its colour scheme.
type job func(ctx context.Context) (prepared, error)

// prepare captures from src, sanitizes, spools the page (and the profile)
// and picks the viewer program.
func prepare(src source, sp *spool, nvimOverride string, lookup finder, tg target) job {
	return func(ctx context.Context) (prepared, error) {
		ctx, cancel := context.WithTimeout(ctx, captureTimeout)
		defer cancel()
		raw, err := src(ctx)
		if err != nil {
			return prepared{}, fmt.Errorf("reading the pane: %w", err)
		}
		p := prepared{page: newPage(raw, maxLines, tg.line)}
		p.at = open(p.page, tg)
		if p.file, err = sp.write("scrollback.txt", []byte(p.page.text)); err != nil {
			return prepared{}, err
		}
		if p.program, err = pickProgram(nvimOverride, lookup); err != nil {
			return prepared{}, err
		}
		if !p.program.less {
			if p.profile, err = sp.write("scrollback.lua", nvim.Scrollback); err != nil {
				return prepared{}, err
			}
		}
		return p, nil
	}
}

// failed is a job that only reports err, for showing late errors.
func failed(err error) job {
	return func(context.Context) (prepared, error) { return prepared{}, err }
}

// errCancelled means the user closed the tool before the viewer opened.
var errCancelled = errors.New("scrollback: cancelled")

// load runs j behind a Bubble Tea screen that stays blank unless loading is
// slow (then a spinner) or fails (then an error card until a key). It also
// learns whether the terminal is dark or light. It returns errCancelled when
// the user closed the tool while loading, or j's error after showing it.
func load(ctx context.Context, j job, in io.Reader, out io.Writer, opts ...tea.ProgramOption) (prepared, ui.Theme, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // stops j when the user leaves early
	m := newLoader(ctx, j)
	opts = append([]tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out), tea.WithoutSignalHandler()}, opts...)
	final, err := tea.NewProgram(m, opts...).Run()
	if err != nil {
		return prepared{}, ui.Theme{}, err
	}
	m = final.(*loader)
	theme := m.scheme.Theme()
	switch {
	case m.err != nil:
		return prepared{}, theme, m.err
	case m.result == nil:
		return prepared{}, theme, errCancelled
	}
	return *m.result, theme, nil
}

// loaded carries the job's outcome into the model.
type loaded struct {
	result prepared
	err    error
}

type loader struct {
	start  tea.Cmd // the job and the spinner
	scheme ui.Scheme
	spin   ui.Spinner
	w, h   int

	result *prepared
	err    error
}

func newLoader(ctx context.Context, j job) *loader {
	m := &loader{scheme: ui.NewScheme()}
	tick := m.spin.Start(time.Now())
	work := func() tea.Msg {
		p, err := j(ctx)
		return loaded{p, err}
	}
	m.start = tea.Batch(work, tick)
	return m
}

func (m *loader) Init() tea.Cmd { return tea.Batch(m.scheme.Init(), m.start) }

func (m *loader) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.scheme.Update(msg)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case ui.SpinnerTickMsg:
		return m, m.spin.Tick() // one tick chain, however often Start ran
	case loaded:
		m.spin.Stop()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.result = &msg.result
		}
	case tea.KeyPressMsg:
		switch {
		case m.err != nil: // any key dismisses the error card
			return m, quit
		case isClose(msg): // closed before the viewer opened
			m.result = nil
			return m, quit
		}
	}
	if m.result != nil && m.scheme.Settled() {
		return m, quit
	}
	return m, nil
}

func isClose(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "q", "esc", "ctrl+c":
		return true
	}
	return false
}

// quit turns colour-scheme reports off again before leaving, so they never
// reach the viewer (or the shell, in demo mode) as stray input.
var quit = tea.Sequence(tea.Raw(ansi.ResetModeLightDark), tea.Quit)

// The error card's size: at most cardMaxW wide, with cardPadX cells of air
// inside its side columns and a row above and below its text.
const (
	cardMaxW = 64
	cardPadX = 2
	hang     = 3 // the glyph and its gap
)

// View leaves the float's own background, as Neovim does, so the switch to
// the viewer does not flash.
func (m *loader) View() tea.View {
	v := tea.NewView(ui.Bleed(m.content()))
	v.AltScreen = true
	return v
}

func (m *loader) content() string {
	t := m.scheme.Theme()
	now := time.Now()
	var box string
	switch {
	case m.err != nil:
		box = errorCard(t, m.err, m.w)
	case m.spin.Visible(now):
		box = t.Spinning("Reading scrollback…", now).Render()
	default:
		return ""
	}
	if m.w <= 0 || m.h <= 0 {
		return box
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

// errorCard explains a failure on a card (the Card profile, as narrow as
// its text allows and at most cardMaxW wide): what failed, the detail in
// words for people (rex.UserMessage; wrapped, at most four lines) and how to
// leave. The glyph hangs and the rest lines up after it.
func errorCard(t ui.Theme, err error, w int) string {
	maxW := cardMaxW
	if w > 0 {
		maxW = max(16, min(maxW, w-4))
	}
	textW := maxW - 2 - 2*cardPadX - hang
	rows := []ui.Line{ui.Line{{Text: ui.GlyphFailed, Style: t.Err}, ui.Space(hang - 1), {Text: "Can't show the scrollback", Style: t.Err.Bold(true)}}.Truncate(textW + hang)}
	detail := ansi.Wordwrap(detailOf(err), textW, "")
	for i, l := range strings.Split(detail, "\n") {
		if i == 4 {
			break
		}
		rows = append(rows, ui.Line{ui.Space(hang), {Text: ansi.Truncate(l, textW, ui.Ellipsis), Style: t.Muted}})
	}
	rows = append(rows, nil, ui.Line{ui.Space(hang)}.Append(t.Hints([]ui.Hint{{Key: "esc", Label: "close"}}, textW)...))
	width := 0
	for _, r := range rows {
		width = max(width, r.Width())
	}
	body := []string{""}
	for _, r := range rows {
		body = append(body, ui.Line{ui.Space(cardPadX)}.Append(r...).Render())
	}
	f := ui.Frame{Card: true}
	card := f.Render(t, width+2+2*cardPadX, len(body)+3, nil, append(body, ""), nil)
	return strings.Join(t.Edged(strings.Split(card, "\n"), ui.Clear), "\n")
}

// detailOf says what went wrong as a sentence for people:
// a pane that is gone is not "reading the pane: that target is gone".
func detailOf(err error) string {
	switch {
	case errors.Is(err, rex.ErrNotFound):
		return "That pane is gone."
	case errors.Is(err, rex.ErrUnavailable):
		return "Rex is not reachable."
	}
	msg := sanitize.Label(rex.UserMessage(err))
	if msg == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(msg)
	return string(unicode.ToUpper(r)) + msg[n:]
}
