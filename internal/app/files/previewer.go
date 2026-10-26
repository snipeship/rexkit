package files

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"rexkit/internal/ui"
)

type previewMsg struct {
	rev uint64
	key string
	doc previewDoc
	err error
}

// previewCache keeps the most recent previews (pictures included) so moving
// back over rows is instant.
type previewCache struct {
	size int
	keys []string // oldest first
	docs map[string]previewDoc
}

func newPreviewCache(size int) previewCache {
	return previewCache{size: size, docs: map[string]previewDoc{}}
}

func (c *previewCache) get(key string) (previewDoc, bool) {
	d, ok := c.docs[key]
	return d, ok
}

func (c *previewCache) put(key string, d previewDoc) {
	if _, ok := c.docs[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.docs[key] = d
	if len(c.keys) > c.size {
		delete(c.docs, c.keys[0])
		c.keys = slices.Delete(c.keys, 0, 1)
	}
}

func (c *previewCache) clear() {
	c.keys = nil
	clear(c.docs)
}

// previewTarget is what the preview shows: the selected row, or the current
// folder when nothing is selected (that is what ⏎ would open).
func (m *model) previewTarget() (entry, bool) {
	if r, ok := m.selected(); ok {
		return r.e, true
	}
	if m.path.active {
		return entry{}, false
	}
	e, err := statEntry(m.dir, "")
	return e, err == nil
}

// syncPreview loads the preview for the current target unless it is already
// showing (or cached). Loads are cancelled when the selection moves on, and
// revision-guarded, so a slow file never replaces a newer preview.
func (m *model) syncPreview() tea.Cmd {
	if !m.layout().areas.PreviewVisible() {
		return nil
	}
	e, ok := m.previewTarget()
	if !ok {
		m.previewKey, m.previewDoc = "", previewDoc{}
		m.preview.Set(m.preview.Begin(), nil, nil)
		m.preview.Rule = false
		return nil
	}
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%t%t", e.path, e.size, e.modTime.UnixNano(), m.hidden, m.theme.Dark)
	if key == m.previewKey {
		return nil
	}
	m.previewKey = key
	if m.previewCancel != nil {
		m.previewCancel()
	}
	rev := m.preview.Begin()
	if doc, ok := m.cache.get(key); ok {
		m.showDoc(rev, doc)
		return m.placePicture()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.previewCancel = cancel
	env := previewEnv{theme: m.theme, hidden: m.hidden}
	return func() tea.Msg {
		doc, err := loadPreview(ctx, e, env)
		return previewMsg{rev: rev, key: key, doc: doc, err: err}
	}
}

func (m *model) previewLoaded(msg previewMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, context.Canceled):
		return nil
	case msg.err != nil:
		msg.doc.lines = []string{m.theme.Muted.Render("Can't preview: " + fsErrorText(msg.err))}
	default:
		m.cache.put(msg.key, msg.doc)
	}
	if !m.showDoc(msg.rev, msg.doc) {
		return nil
	}
	return m.placePicture()
}

func (m *model) showDoc(rev uint64, doc previewDoc) bool {
	lines := doc.lines
	if doc.picture != nil {
		lines = nil // drawn once placed; see pictureLines
	}
	facts := append(slices.Clip(doc.meta), ui.RelTimeAt(doc.modTime, m.cfg.now()))
	if !m.preview.Set(rev, previewTitle(m.theme, doc.name, facts), lines) {
		return false
	}
	m.previewDoc, m.previewRev = doc, rev
	m.preview.Rule = doc.folder
	return true
}

// previewTitle is the preview's title row: the name in bold, then its
// facts two cells off, "main.go  492 B · Go · 12m".
func previewTitle(t ui.Theme, name string, facts []string) ui.Line {
	facts = slices.DeleteFunc(facts, func(f string) bool { return f == "" })
	line := ui.Styled(name, t.Text.Bold(true))
	if len(facts) > 0 {
		line = append(line, ui.Span{Text: "  " + strings.Join(facts, " "+ui.Dot+" "), Style: t.Muted})
	}
	return line
}

// pictureBox is the part of the preview an image may cover: where the
// preview draws its content (inside the well).
func (m *model) pictureBox() ui.Rect {
	a := m.layout().areas
	return m.preview.ContentRect(a.Preview.W, a.Preview.H).Offset(a.Preview.X, a.Preview.Y)
}

// placePicture uploads and places the previewed image to fit the preview.
func (m *model) placePicture() tea.Cmd {
	pic := m.previewDoc.picture
	if pic == nil || !m.layout().areas.PreviewVisible() {
		return nil
	}
	box := m.pictureBox()
	cols, rows := m.gfx.fit(pic, box.W, box.H)
	raw := m.gfx.show(pic, cols, rows)
	m.refreshPicture()
	if raw == "" {
		return nil
	}
	return tea.Raw(raw)
}

func (m *model) graphicsEvent(ev uv.KittyGraphicsEvent) tea.Cmd {
	ok := string(ev.Payload) == "OK"
	if ev.Options.ID == graphicsQueryID {
		m.gfx.answered, m.gfx.supported = true, ok
		return m.placePicture()
	}
	if m.gfx.placed(ev.Options.ID, ok) {
		m.refreshPicture()
	}
	return nil
}

// refreshPicture redraws the preview's image cells after a placement
// changed (or failed).
func (m *model) refreshPicture() {
	pic := m.previewDoc.picture
	if pic == nil {
		return
	}
	m.preview.Refresh(m.previewRev, m.preview.Title, m.pictureLines(pic))
}

// pictureLines are the preview lines for an image: placeholder cells,
// centred, once the terminal has the placement; a stand-in until then, and
// in terminals without kitty graphics.
func (m *model) pictureLines(pic *picture) []string {
	if !m.gfx.answered || !m.gfx.supported || !pic.ready {
		return m.pictureStandIn(pic)
	}
	pad := strings.Repeat(" ", max(0, (m.pictureBox().W-pic.cols)/2))
	lines := placeholders(pic)
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return lines
}

// pictureStandIn stands in for an image the terminal does not draw: its
// format and size in the tertiary colour and the key that opens it,
// centred in the well.
func (m *model) pictureStandIn(pic *picture) []string {
	t := m.theme.InWell()
	box := m.pictureBox()
	lines := []ui.Line{
		{{Text: "▣ " + pic.format + " " + pic.dimensions(), Style: t.Subtle}},
		{},
		t.Hints([]ui.Hint{{Key: "ctrl+x", Label: "open"}}, box.W),
	}
	out := make([]string, max(0, (box.H-len(lines))/2), box.H)
	for _, l := range lines {
		out = append(out, strings.Repeat(" ", max(0, (box.W-l.Width())/2))+l.Render())
	}
	return out
}
