package sanitize

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	tabStop = 8
	// maxCUF bounds a single cursor-forward so a hostile "CSI 999999999 C"
	// cannot allocate gigabytes.
	maxCUF = 4096
	// maxCarried bounds the SGR sequences re-applied at the start of a line.
	maxCarried = 8
	reset      = "\x1b[m"
)

// TerminalLines renders captured terminal output (Rex's VT export or raw pane
// output) as display lines, keeping colours and nothing else:
//
//   - SGR (CSI … m with digits, ';' and ':' only) is kept; colours that are
//     active at a line break are re-applied on the next line, and every line
//     that sets attributes ends with a reset;
//   - CUF (CSI n C), which Rex uses for untouched blank cells, becomes spaces
//     with default attributes;
//   - tabs expand to the next multiple of 8 columns;
//   - every other sequence or control (OSC incl. hyperlinks and clipboard, DCS,
//     APC incl. kitty graphics, PM, SOS, cursor movement, mode changes, C0/C1)
//     is dropped, as are bidi controls, zero-width characters outside grapheme
//     clusters and kitty image placeholders.
//
// When width > 0 no line is wider than width cells. A line is cut silently
// when only blank cells overflow, and with "…" when content does.
func TerminalLines(s string, width int) []string {
	var w vtWriter
	w.width = width
	w.feed(s)
	if w.col == 0 && strings.HasSuffix(s, "\n") {
		return w.lines // a trailing line break does not start a new line
	}
	return append(w.lines, w.finish())
}

// TerminalLine is TerminalLines for a single line; line breaks are dropped.
func TerminalLine(s string, width int) string {
	var w vtWriter
	w.width = width
	w.singleLine = true
	w.feed(s)
	return w.finish()
}

type vtWriter struct {
	width      int
	singleLine bool

	lines   []string
	line    strings.Builder
	col     int      // cells written on the current line
	content int      // col just after the last non-blank glyph
	sgr     []string // SGR sequences in effect since the last reset
	styled  bool     // the current line contains SGR
}

func (w *vtWriter) feed(s string) {
	var state byte
	for len(s) > 0 {
		seq, width, n, next := ansi.DecodeSequence(s, state, nil)
		if n == 0 {
			break
		}
		s, state = s[n:], next
		switch {
		case width > 0:
			w.text(seq, width)
		case seq == "\t":
			w.blank(tabStop - w.col%tabStop)
		case seq == "\n":
			if !w.singleLine {
				w.lines = append(w.lines, w.finish())
			}
		case strings.HasPrefix(seq, "\x1b[") && len(seq) > 2:
			w.csi(seq[2:len(seq)-1], seq[len(seq)-1], seq)
		}
	}
}

// text writes one grapheme cluster unless it carries something hidden.
func (w *vtWriter) text(cluster string, width int) {
	if !utf8.ValidString(cluster) {
		cluster, width = string(utf8.RuneError), 1
	}
	for _, r := range cluster {
		if r == kittyPlaceholder || (isHidden(r) && !isJoiner(r)) {
			return
		}
	}
	if w.overflowing(width) {
		return
	}
	w.line.WriteString(cluster)
	w.col += width
	if strings.TrimSpace(cluster) != "" {
		w.content = w.col
	}
}

func (w *vtWriter) csi(params string, final byte, seq string) {
	switch final {
	case 'm':
		if strings.Trim(params, "0123456789;:") != "" {
			return
		}
		w.line.WriteString(seq)
		w.styled = true
		if params == "" || params == "0" {
			w.sgr = w.sgr[:0]
		} else if len(w.sgr) < maxCarried {
			w.sgr = append(w.sgr, seq)
		} else {
			w.sgr = append(w.sgr[1:], seq)
		}
	case 'C':
		if strings.Trim(params, "0123456789") != "" {
			return
		}
		n := 1
		if params != "" {
			// Overlong numbers fail to parse and are bounded below either way.
			v, err := strconv.Atoi(params)
			if err != nil || v > maxCUF {
				v = maxCUF
			}
			n = max(1, v)
		}
		w.blank(n)
	}
}

// blank writes n blank cells with default attributes, the way a terminal
// shows cells the cursor skipped over.
func (w *vtWriter) blank(n int) {
	if w.width > 0 {
		n = min(n, w.width+1-w.col)
	}
	if n <= 0 {
		return
	}
	active := len(w.sgr) > 0
	if active {
		w.line.WriteString(reset)
	}
	w.line.WriteString(strings.Repeat(" ", n))
	w.col += n
	if active {
		w.line.WriteString(strings.Join(w.sgr, ""))
	}
}

// overflowing reports that the line already holds more than it can show, so
// further glyphs only need to be counted, not written.
func (w *vtWriter) overflowing(width int) bool {
	if w.width <= 0 || w.col <= w.width {
		return false
	}
	w.col += width
	w.content = w.col
	return true
}

// finish closes the current line and starts the next with the carried SGR.
func (w *vtWriter) finish() string {
	line := w.line.String()
	if w.width > 0 && w.col > w.width {
		tail := ""
		if w.content > w.width {
			tail = "…"
		}
		line = ansi.Truncate(line, w.width, tail)
	}
	if w.styled {
		line += reset
	}
	w.line.Reset()
	w.col, w.content, w.styled = 0, 0, false
	if len(w.sgr) > 0 {
		w.line.WriteString(strings.Join(w.sgr, ""))
		w.styled = true
	}
	return line
}
