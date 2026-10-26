package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
	"rexkit/internal/ui/sanitize"
)

// verb is something Files does with the selected entry. Destinations are
// verbs on keys, not a mode.
type verb int

const (
	verbTab   verb = iota // ⏎ new tab here
	verbRight             // ⌥⏎ split right of the pane Files was opened from
	verbBelow             // ⇧⏎ split below it
	verbEdit              // ⌃E edit the file in a new tab
	verbCopy              // ⌃Y copy the path
	verbOpen              // ⌃X open with the default app
)

// opens reports verbs that create a terminal and end the tool.
func (v verb) opens() bool { return v <= verbEdit }

// phrase is what the verb does, for the footer and toasts: "new tab in".
func (v verb) phrase() string {
	return [...]string{"new tab in", "split right in", "split below in", "edit", "copy", "open"}[v]
}

// target is what a verb acts on: the selected entry, or the current folder
// when nothing is selected.
type target struct {
	path string
	dir  string // where a terminal starts: the folder itself, or a file's folder
	file bool   // path is a regular file (it can be edited)
}

// launcher performs verbs. With a nil api (demo mode) terminal verbs only
// describe what they would do.
type launcher struct {
	api          rex.API
	session      string
	origin       string // the pane Files was opened from
	originWindow string
	stateDir     string // where the result for the Lua layer goes ("" for none)
	dirs         state.Dirs
	shell        string   // the shell for new terminals ("" for Rex's default)
	editor       []string // editor argv; the path is appended as its own element
	home         string
	run          runner
	now          func() time.Time
}

// Results of verbs, delivered to the model.
type (
	openedMsg struct{ created rex.Created }
	demoMsg   struct {
		v verb
		t target
	}
	doneMsg struct{ text string } // a verb that keeps the tool open succeeded
	failMsg struct{ err error }
)

// errOriginClosed means a split was asked for but its anchor is gone.
var errOriginClosed = errors.New("the pane Files was opened from has closed")

// do performs v on t. It runs off the UI goroutine.
func (l launcher) do(ctx context.Context, v verb, t target) any {
	switch v {
	case verbCopy:
		if _, err := l.run(ctx, "", t.path, "pbcopy"); err != nil {
			return failMsg{fmt.Errorf("couldn't copy: %w", err)}
		}
		return doneMsg{"Copied " + displayPath(t.path, l.home)}
	case verbOpen:
		if _, err := l.run(ctx, "", "", "open", t.path); err != nil {
			return failMsg{err}
		}
		return doneMsg{"Opened " + sanitize.Name(filepath.Base(t.path))}
	}
	if err := l.check(v, t); err != nil {
		return failMsg{err}
	}
	if l.api == nil {
		return demoMsg{v, t}
	}
	created, window, err := l.create(ctx, v, t)
	if err != nil {
		return failMsg{err}
	}
	if l.stateDir != "" {
		r := popup.Result{Action: popup.ActionFilesOpen, SessionID: l.session, WindowID: window, BlockID: first(created.BlockIDs)}
		if _, err := popup.WriteResult(l.stateDir, r); err != nil {
			return failMsg{err}
		}
	}
	l.remember(window, first(created.BlockIDs), t.path)
	created.WindowID = window
	return openedMsg{created}
}

// check revalidates the target just before acting: the listing may be
// seconds old.
func (l launcher) check(v verb, t target) error {
	// A shell needs its folder; only the editor needs the file itself (so ⏎
	// on a broken link or a FIFO still opens the folder it is in).
	path := t.dir
	if v == verbEdit {
		path = t.path
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %s", displayPath(path, l.home), fsErrorText(err))
	}
	switch {
	case v == verbEdit && !info.Mode().IsRegular():
		return errors.New("⌃E edits files · choose a file")
	case v != verbEdit && !info.IsDir():
		return fmt.Errorf("%s is not a folder", displayPath(path, l.home))
	}
	return nil
}

// create makes the terminal and returns it with the window it is in.
func (l launcher) create(ctx context.Context, v verb, t target) (rex.Created, string, error) {
	label := sanitize.Label(filepath.Base(t.dir))
	layout := rex.Shell(label, t.dir)
	layout.Block.Options.Shell = l.shell
	if v == verbEdit {
		label = sanitize.Label(filepath.Base(t.path))
		layout = rex.Command(label, t.dir, append(append([]string(nil), l.editor...), t.path)...)
	}
	focus := true
	switch v {
	case verbRight, verbBelow:
		if l.origin == "" {
			return rex.Created{}, "", errOriginClosed
		}
		view, err := l.api.View(ctx, l.session)
		if err != nil {
			return rex.Created{}, "", err
		}
		w, _, _, ok := view.FindBlock(l.origin)
		if !ok {
			return rex.Created{}, "", errOriginClosed
		}
		dir := rex.Horizontal
		if v == verbBelow {
			dir = rex.Vertical
		}
		created, err := l.api.NewSplit(ctx, l.session, rex.NewSplitRequest{
			AnchorID: l.origin, Direction: dir, Side: "after", Ratio: 0.5, Layout: layout, Focus: true,
		})
		if errors.Is(err, rex.ErrNotFound) {
			err = errOriginClosed
		}
		return created, w.ID, err
	default:
		created, err := l.api.NewWindow(ctx, l.session, rex.NewWindowRequest{Label: label, Layout: layout, Focus: &focus})
		return created, created.WindowID, err
	}
}

// remember records the jump for Go to's RECENT and previous location, and
// the opened path for RECENT HERE. History is a convenience: failures are
// ignored.
func (l launcher) remember(window, block, path string) {
	now := l.now()
	_ = l.dirs.UpdateHistory(func(h *state.History) {
		if l.origin != "" {
			h.Visit(state.Visit{Kind: "pane", SessionID: l.session, WindowID: l.originWindow, BlockID: l.origin, At: now.Add(-time.Millisecond)})
		}
		h.Visit(state.Visit{Kind: "pane", SessionID: l.session, WindowID: window, BlockID: block, At: now})
		h.Open(path, now)
	})
}

func first(ids rex.List[string]) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}
