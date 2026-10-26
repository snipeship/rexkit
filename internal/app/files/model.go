package files

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"rexkit/internal/match"
	"rexkit/internal/popup"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// config is how Files was started and what it may touch.
type config struct {
	dir           string // the folder to start in
	home          string
	dismissOnBlur bool
	demo          bool
	notice        string // shown as a warning on the first frame
	dirs          state.Dirs
	run           runner
	now           func() time.Time
	launcher      launcher
	imageBase     int // first kitty image id (random in production)
}

// place is a stop on the back stack: where the user was and what they had
// typed and selected there.
type place struct {
	dir       string
	query     string
	selected  string
	recursive bool
}

// model is the Files tool. It is driven entirely by messages; every slow
// operation (listing, git, previews, counts, verbs) runs in a command and
// comes back revision-guarded.
type model struct {
	cfg           config
	scheme        ui.Scheme
	theme         ui.Theme
	width, height int
	carded        bool // drawn in a card of the float (popup.CardMsg)

	// What is being browsed.
	dir               string
	hidden, recursive bool
	query             textinput.Model
	pattern           *match.Pattern
	data              listing
	recent            []entry
	projects          []entry
	listings          map[string]listing // recently seen listings, for an instant first frame
	back              []place
	restore           string // path to select when the listing arrives
	bestFirst         bool   // select the best match when it arrives (a search over new files)
	scanRev           int
	scanCancel        context.CancelFunc
	rankRev           int
	rankedRev         int       // rankRev of the rows shown
	loadedRev         int       // scanRev of the listing shown
	pending           *verb     // a verb waiting for the rows to settle
	pendingRev        int       // counts held verbs, so a late stillPendingMsg is ignored
	answerFailure     bool      // a verb run by a handshake ⏎ (popup.TakeCommit) answers if it fails
	commitAt          time.Time // when Lua wrote that ⏎'s commit marker (popup.GraceLeft)
	searched          int       // rows the query ran over

	// The list. With here set no row is selected: Files rests on the folder
	// itself (the breadcrumb), and ⏎ opens it. That is where every folder
	// starts, until the user moves or types.
	rows      []row
	list      ui.List
	here      bool
	nameWidth int

	// Decorations, filled in lazily.
	git      repoStatus
	inRepo   bool
	counts   map[string]count
	counting map[string]bool

	// The preview.
	preview       ui.Preview
	previewPref   ui.PreviewPref
	previewKey    string
	previewDoc    previewDoc
	previewRev    uint64 // revision of the shown preview, for image redraws
	previewCancel context.CancelFunc
	cache         previewCache
	gfx           graphics

	path      pathMode
	help      bool // the key panel is open
	helpAt    int  // the key panel's scroll position, when it is cut short
	toast     ui.Toaster
	spinner   ui.Spinner // loading a listing or ranking a search
	emptiedAt time.Time  // when ⌫ last emptied the query (held ⌫ must not walk up)
	started   time.Time  // when Files started, for ui.EarlyEnterSince
	busy      bool       // a terminal verb is running
	busySince time.Time  // when it began, for the tray's "Opening…"
	busyRev   int        // which busyTickMsg chain is the running verb's
	done      bool
}

func newModel(cfg config) *model {
	m := &model{
		cfg:      cfg,
		scheme:   ui.NewScheme(),
		dir:      cfg.dir,
		query:    ui.NewInput(),
		listings: map[string]listing{},
		counts:   map[string]count{},
		counting: map[string]bool{},
		cache:    newPreviewCache(32),
		gfx:      newGraphics(cfg.imageBase),
		width:    100,
		height:   30,
	}
	m.theme = m.scheme.Theme()
	m.started = cfg.now()
	m.pattern = match.Compile("")
	m.path.input = newPathInput()
	return m
}

// Init starts loading everything at once; the first frame waits only for the
// colour scheme (at most ui.DetectTimeout).
func (m *model) Init() tea.Cmd {
	var notice tea.Cmd
	if m.cfg.notice != "" {
		notice = m.toast.Show(ui.ToastWarn, m.cfg.notice)
	}
	return tea.Batch(
		notice,
		m.scheme.Init(),
		m.load(),
		m.loadGit(),
		m.loadProjects(),
		tea.Raw(graphicsQuery),
		tea.Tick(graphicsWait, func(time.Time) tea.Msg { return graphicsTimeoutMsg{} }),
	)
}

// graphicsWait is how long to wait for the kitty graphics query's answer.
const graphicsWait = 300 * time.Millisecond

type graphicsTimeoutMsg struct{}

// Update handles one message.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	if m.scheme.Update(msg) {
		m.theme = m.scheme.Theme()
		m.cache.clear()
		m.previewKey = ""
		return m, m.syncPreview()
	}
	if m.toast.Update(msg) {
		return m, nil
	}
	switch msg := msg.(type) {
	case popup.CardMsg:
		m.carded = true
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.relayout()
	case tea.KeyPressMsg:
		return m, m.key(ui.EarlyEnterSince(m.started, m.cfg.now(), msg))
	case tea.PasteMsg:
		return m, m.edit(msg)
	case tea.MouseClickMsg:
		return m, m.click(msg)
	case tea.MouseWheelMsg:
		return m, m.wheel(msg)
	case tea.BlurMsg:
		// Opening a terminal moves focus away; that ends Files by itself.
		if m.cfg.dismissOnBlur && !m.cfg.demo && !m.busy {
			return m, m.quit()
		}
	case ui.SpinnerTickMsg:
		return m, m.spinner.Tick() // one tick chain, however often Start ran
	case busyTickMsg:
		if m.busy && msg.rev == m.busyRev {
			return m, m.busyTick()
		}
	case uv.CellSizeEvent:
		if msg.Width > 0 && msg.Height > 0 {
			m.gfx.cellW, m.gfx.cellH = msg.Width, msg.Height
		}
		return m, m.placePicture()
	case uv.KittyGraphicsEvent:
		return m, m.graphicsEvent(msg)
	case graphicsTimeoutMsg:
		if !m.gfx.answered {
			m.gfx.answered = true
			return m, m.placePicture()
		}
	case stillPendingMsg:
		return m, m.stillPending(msg)
	case listingMsg:
		return m, m.listingLoaded(msg)
	case rankedMsg:
		return m, m.ranked(msg)
	case gitMsg:
		if msg.dir == m.dir {
			m.git, m.inRepo = msg.status, msg.ok
		}
	case projectsMsg:
		m.projects = msg.entries
		if m.pattern.Empty() && !m.path.active {
			return m, m.rebuild(m.selectedPath())
		}
	case countsMsg:
		m.countsLoaded(msg)
	case previewMsg:
		return m, m.previewLoaded(msg)
	case pathListingMsg:
		return m, m.pathListed(msg)
	case openedMsg:
		return m, m.quit()
	case demoMsg:
		m.setBusy(false)
		return m, m.toast.Show(ui.ToastInfo, "Demo · would "+msg.v.phrase()+" "+displayPath(msg.t.dir, m.cfg.home))
	case doneMsg:
		return m, m.toast.Show(ui.ToastOK, msg.text)
	case failMsg:
		m.setBusy(false)
		if m.answerFailure {
			m.answerFailure = false
			m.cfg.launcher.answer(popup.ActionStay)
		}
		if errors.Is(msg.err, errOriginClosed) {
			return m, m.toast.Show(ui.ToastErr, errorText(msg.err), ui.Hint{Key: "enter", Label: "opens a new tab"})
		}
		return m, m.toast.Show(ui.ToastErr, errorText(msg.err))
	}
	return m, nil
}

// quit ends the tool; Main then removes its images and closes the float.
func (m *model) quit() tea.Cmd {
	m.done = true
	if m.scanCancel != nil {
		m.scanCancel()
	}
	if m.previewCancel != nil {
		m.previewCancel()
	}
	return tea.Quit
}

// setBusy marks a terminal verb as running (or done): keys are ignored
// meanwhile, and after ui.SpinnerDelay the tray says "Opening…".
func (m *model) setBusy(busy bool) tea.Cmd {
	m.busy = busy
	if !busy {
		return nil
	}
	m.busySince = m.cfg.now()
	m.busyRev++ // an earlier verb's chain, still in flight, ends
	return m.busyTick()
}

// busyTickMsg animates "Opening…". It is not a ui.SpinnerTickMsg: a
// ui.Spinner takes every one of those for its own, so two spinners on that
// message would start chains for each other.
type busyTickMsg struct{ rev int }

func (m *model) busyTick() tea.Cmd {
	rev := m.busyRev
	return tea.Tick(ui.SpinnerInterval, func(time.Time) tea.Msg { return busyTickMsg{rev} })
}

// opening reports whether the tray says "Opening…": a verb has been running
// for ui.SpinnerDelay.
func (m *model) opening(now time.Time) bool {
	return m.busy && now.Sub(m.busySince) >= ui.SpinnerDelay
}
