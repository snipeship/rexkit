package search

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/find"
	"rexkit/internal/rex"
	"rexkit/internal/ui/sanitize"
)

// pane is one terminal pane and what was read of it.
type pane struct {
	session, window, block string // Rex IDs
	sessionLabel, tabLabel string // sanitized, as shown
	name                   string // its label, else its program (sanitized)
	agent                  bool   // an agent runs in it (it reports a program status)
	cwd                    string
	origin                 bool // the pane the tool was opened from

	read bool  // its capture arrived (or failed)
	err  error // why it could not be read
	text
}

// text is a pane's capture: the same VT export and the same sanitizing as
// Scrollback's, so line N here is line N there.
type text struct {
	lines []string // text and SGR colours only (sanitize.TerminalLines)
	plain []string // the lines without colours: what is searched and listed
	fold  []string // plain with ASCII case folded (find.Fold)
}

// A line without colours, or without capitals, is shared by lines, plain
// and fold rather than copied: a capture can be 64 MiB.

// newText sanitizes a capture, dropping trailing blank lines as Scrollback
// does. It runs off the UI goroutine.
func newText(raw string) text {
	lines := sanitize.TerminalLines(raw, 0)
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	t := text{lines: lines, plain: make([]string, len(lines)), fold: make([]string, len(lines))}
	for i, l := range lines {
		t.plain[i] = l
		if strings.IndexByte(l, ansi.ESC) >= 0 {
			t.plain[i] = ansi.Strip(l)
		}
		t.fold[i] = find.Fold(t.plain[i]) // the same string without capitals
	}
	return t
}

// page is the capture as Scrollback reads a page (--page): one line each.
func (t text) page() string {
	var b strings.Builder
	for _, l := range t.lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// toolLabelPrefix marks rexkit's own blocks (tools, toasts): they are never
// searched.
const toolLabelPrefix = "rexkit:"

// panesOf lists the terminal panes of every tab: the origin's session
// first, then the others in Rex's order, each tab's tiled panes before its
// floating ones, as in Go to. rexkit's own floats are left out.
func panesOf(inv rex.Inventory, o origin, self string) []*pane {
	sessions := slices.Clone(inv.Sessions)
	slices.SortStableFunc(sessions, func(a, b rex.SessionView) int {
		if ao, bo := a.SessionID == o.session, b.SessionID == o.session; ao != bo {
			if ao {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Order, b.Order)
	})
	var out []*pane
	for _, s := range sessions {
		label := sanitize.Label(s.Label)
		if label == "" {
			label = "Session"
		}
		for n, w := range s.Windows {
			var tab []*pane
			for _, l := range tiledFirst(w.Layers) {
				for _, b := range l.Blocks {
					if !b.IsTerminal() || b.ID == self || strings.HasPrefix(b.Label, toolLabelPrefix) {
						continue
					}
					p := &pane{session: s.SessionID, window: w.ID, block: b.ID, sessionLabel: label,
						origin: b.ID == o.block}
					var program string
					if t := b.Terminal; t != nil {
						if t.Process != nil {
							if c := t.Process.Current(); c != nil {
								program, p.cwd = sanitize.Label(c.Name), c.Cwd
							}
						}
						p.agent = len(t.Status) > 0
					}
					p.name = paneName(b.Label, program)
					tab = append(tab, p)
				}
			}
			name := tabName(w, tab, n+1)
			for _, p := range tab {
				p.tabLabel = name
			}
			out = append(out, tab...)
		}
	}
	return out
}

// paneName is a label the user chose, else the program, as in Go to.
func paneName(label, program string) string {
	label = sanitize.Label(label)
	switch strings.ToLower(label) {
	case "", "shell", "terminal":
	default:
		return label
	}
	if program != "" {
		return program
	}
	return "Terminal"
}

// defaultWindowLabel is how Rex names a tab nobody named: "Window 726ac4f9".
var defaultWindowLabel = regexp.MustCompile(`^Window [0-9a-f]{8}$`)

// tabName is the tab's label, or for an unnamed tab the folder or program
// of its focused pane, or its number, as in Go to.
func tabName(w rex.Window, panes []*pane, number int) string {
	if label := sanitize.Label(w.Label); label != "" && !defaultWindowLabel.MatchString(label) {
		return label
	}
	if len(panes) == 0 {
		return "Tab " + strconv.Itoa(number)
	}
	p := panes[0]
	if i := slices.IndexFunc(panes, func(p *pane) bool { return p.block == w.FocusedBlockID }); i >= 0 {
		p = panes[i]
	}
	if p.cwd != "" && p.cwd != "/" {
		return sanitize.Label(filepath.Base(p.cwd))
	}
	return p.name
}

// tiledFirst lists the tiled layer before floating ones, each in view order.
func tiledFirst(layers []rex.Layer) []rex.Layer {
	out := slices.Clone(layers)
	slices.SortStableFunc(out, func(a, b rex.Layer) int {
		switch {
		case a.Floating() == b.Floating():
			return 0
		case a.Floating():
			return 1
		}
		return -1
	})
	return out
}
