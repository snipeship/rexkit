package switcher

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

const (
	// previewDebounce lets the selection settle before capturing.
	previewDebounce = 40 * time.Millisecond
	// previewTTL is how long a capture is fresh enough to show without
	// capturing again.
	previewTTL = 1500 * time.Millisecond
	// tailInterval re-captures the previewed pane while it is visible.
	tailInterval = time.Second
	// tailBytes bounds a capture: enough for a screenful and some history.
	tailBytes = 48 << 10
	// maxPreviewLines bounds what the preview keeps of a capture.
	maxPreviewLines = 400
	previewTimeout  = 3 * time.Second
	// maxCached bounds the capture cache.
	maxCached = 32
)

// capture is a pane's captured output.
type capture struct {
	raw string
	err error
	at  time.Time
}

// previewState is the live tail of the selected pane.
type previewState struct {
	view     ui.Preview
	pref     ui.PreviewPref
	rev      uint64 // revision of the newest request
	want     string // block the preview should show
	shown    string // block whose output is shown
	width    int    // width the shown lines were cut for
	inflight bool
	cancel   context.CancelFunc
	cache    map[string]capture
	spin     ui.Spinner
}

func newPreviewState() previewState {
	return previewState{view: ui.Preview{Follow: true}, cache: map[string]capture{}}
}

type (
	previewDueMsg struct{ rev uint64 }
	previewMsg    struct {
		rev   uint64
		block string
		capture
	}
	tailMsg struct{}
)

// selectionChanged points the preview at the selected entry. A cached capture
// shows at once; a missing or old one is captured after a short debounce.
// With nothing selected (no matches) the preview is cleared: it must not
// describe a pane ⏎ would not go to.
func (m *model) selectionChanged() tea.Cmd {
	_, e, ok := m.selected()
	if !ok {
		m.clearPreview()
		return nil
	}
	// A session or tab previews the pane going there would focus.
	block := e.blockID
	if block == m.pv.want {
		return nil
	}
	m.stopCapture()
	m.pv.want = block
	m.pv.rev = m.pv.view.Begin()
	c, cached := m.pv.cache[block]
	if cached {
		m.showCapture(m.pv.rev, block, c)
		if m.now().Sub(c.at) < previewTTL {
			return nil
		}
	}
	return m.after(previewDebounce, previewDueMsg{m.pv.rev})
}

func (m *model) previewDue(msg previewDueMsg) tea.Cmd {
	if msg.rev != m.pv.rev || !m.previewVisible() {
		return nil
	}
	return m.capture()
}

// capture starts capturing the wanted pane, cancelling an older capture.
func (m *model) capture() tea.Cmd {
	_, e, ok := m.selected()
	if !ok || e.blockID != m.pv.want {
		return nil
	}
	m.stopCapture()
	ctx, cancel := context.WithTimeout(m.ctx, previewTimeout)
	m.pv.cancel, m.pv.inflight = cancel, true
	if m.pv.shown != m.pv.want {
		m.pv.spin.Start(m.now())
	}
	api, rev, now := m.api, m.pv.rev, m.now
	session, block := e.sessionID, m.pv.want
	return tea.Batch(m.clock.wake(m), func() tea.Msg {
		defer cancel()
		raw, err := api.Capture(ctx, session, block, rex.CaptureOptions{Format: rex.FormatVT, Trim: true, TailBytes: tailBytes})
		return previewMsg{rev: rev, block: block, capture: capture{raw: raw, err: err, at: now()}}
	})
}

// clearPreview empties the preview. The capture cache stays, so selecting a
// pane again shows its last output at once.
func (m *model) clearPreview() {
	if m.pv.want == "" && m.pv.shown == "" {
		return
	}
	m.stopCapture()
	m.pv.want, m.pv.shown = "", ""
	m.pv.rev = m.pv.view.Begin() // a capture still on its way is now stale
	m.pv.view.Set(m.pv.rev, nil, nil)
	m.pv.view.Empty = nil
}

// stopCapture cancels the capture in flight, if any.
func (m *model) stopCapture() {
	if m.pv.cancel != nil {
		m.pv.cancel()
		m.pv.cancel = nil
	}
	m.pv.inflight = false
	m.pv.spin.Stop()
}

func (m *model) previewArrived(msg previewMsg) {
	if errors.Is(msg.err, context.Canceled) {
		return
	}
	if msg.err == nil || m.pv.cache[msg.block].raw == "" {
		if len(m.pv.cache) >= maxCached {
			clear(m.pv.cache)
		}
		m.pv.cache[msg.block] = msg.capture
	}
	if msg.rev != m.pv.rev {
		return // a capture for an older selection
	}
	m.pv.inflight = false
	m.pv.spin.Stop()
	if msg.err != nil && m.pv.shown == msg.block {
		return // keep what is shown; the next tail may succeed
	}
	m.showCapture(msg.rev, msg.block, msg.capture)
}

// showCapture installs a capture as revision rev. Fresh output of the pane
// already shown keeps the reader's scroll position.
func (m *model) showCapture(rev uint64, block string, c capture) {
	width := m.layout().areas.Preview.W
	lines := previewLines(c, width)
	if block == m.pv.shown {
		m.pv.view.Refresh(rev, nil, lines)
	} else if m.pv.view.Set(rev, nil, lines) {
		m.pv.shown = block
	}
	m.pv.width = width
	m.pv.view.Empty = previewEmpty(m.theme, c)
}

// previewLines cuts a capture into display lines, without the blank rows
// below the last output.
func previewLines(c capture, width int) []string {
	if c.err != nil || width <= 0 {
		return nil
	}
	lines := sanitize.TerminalLines(c.raw, width)
	for len(lines) > 0 && strings.TrimSpace(sanitize.TerminalLine(lines[len(lines)-1], 0)) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxPreviewLines {
		lines = lines[len(lines)-maxPreviewLines:]
	}
	return lines
}

func previewEmpty(t ui.Theme, c capture) ui.Line {
	switch {
	case errors.Is(c.err, rex.ErrNotFound):
		return ui.Line{{Text: "That pane closed", Style: t.Subtle}}
	case c.err != nil:
		return ui.Line{{Text: ui.GlyphFailed + " " + rex.UserMessage(c.err), Style: t.Err}}
	}
	return ui.Line{{Text: "No output yet", Style: t.Subtle}}
}

// previewResized re-cuts the shown output for a new preview width.
func (m *model) previewResized() tea.Cmd {
	width := m.layout().areas.Preview.W
	if width == m.pv.width || m.pv.shown == "" {
		return nil
	}
	m.pv.width = width
	m.pv.view.Lines = previewLines(m.pv.cache[m.pv.shown], width)
	return nil
}

// previewTail starts the live tail: the previewed pane is re-captured every
// tailInterval while the preview is visible.
func (m *model) previewTail() tea.Cmd { return m.after(tailInterval, tailMsg{}) }

func (m *model) tailTick() tea.Cmd {
	next := m.previewTail()
	if m.pv.inflight || m.pv.want == "" || !m.previewVisible() || m.busy {
		return next
	}
	return tea.Batch(next, m.capture())
}

// togglePreview flips the preview (⌃/) and captures if it just appeared.
func (m *model) togglePreview() tea.Cmd {
	m.pv.pref = m.pv.pref.Toggle(m.previewVisible())
	m.layoutChanged()
	if !m.previewVisible() {
		return nil
	}
	m.previewResized()
	if m.pv.shown != m.pv.want || m.pv.shown == "" {
		return m.capture()
	}
	return nil
}

func (m *model) previewVisible() bool { return m.layout().areas.PreviewVisible() }
