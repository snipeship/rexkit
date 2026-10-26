package rename

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// maxLabel is the longest label Rex accepts, in characters.
const maxLabel = 128

// Target is what gets renamed.
type Target struct {
	Kind string // "pane", "tab" or "session"
	ID   string // block, window or session ID
}

// DefaultLabel is the label Rex gives a pane or tab whose label is cleared,
// made from its ID ("Block 3bccb71a"); "" for sessions, where Rex picks a
// new name.
func (t Target) DefaultLabel() string {
	prefix := map[string]string{"pane": "Block ", "tab": "Window "}[t.Kind]
	if prefix == "" || len(t.ID) < 8 {
		return ""
	}
	return prefix + t.ID[len(t.ID)-8:]
}

// Automatic reports whether label is the default Rex makes from the ID,
// which the field shows as empty.
func (t Target) Automatic(label string) bool {
	d := t.DefaultLabel()
	return d != "" && label == d
}

// Apply sets the target's label in session (the session's own ID for a
// session); an empty label restores the automatic name.
func (t Target) Apply(ctx context.Context, api rex.API, session, label string) error {
	switch t.Kind {
	case "pane":
		return api.SetBlockLabel(ctx, session, t.ID, label)
	case "tab":
		return api.SetWindowLabel(ctx, session, t.ID, label)
	}
	return api.SetSessionLabel(ctx, t.ID, label)
}

// Problem puts a failed rename in words for the helper under the field, ""
// for success.
func (t Target) Problem(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, rex.ErrNotFound):
		return "That " + t.Kind + " is gone"
	}
	return "Could not rename: " + rex.UserMessage(err)
}

// Outcome says what saving label does, for demo mode.
func (t Target) Outcome(label string) string {
	if label == "" {
		if d := t.DefaultLabel(); d != "" {
			return "Would reset the " + t.Kind + " to “" + d + "”"
		}
		return "Would let Rex pick a new name for the " + t.Kind
	}
	return "Would rename the " + t.Kind + " to “" + label + "”"
}

// Editor is the name field of a rename: the current label prefilled and
// selected (typing replaces it), ⌃U to clear it, and a helper that says what
// saving will do. The Rename dialog and Go to's rename sheet both use it;
// they handle ⏎, ⎋ and ⌃C themselves.
type Editor struct {
	target   Target
	input    textinput.Model
	selected bool // the whole value is selected: typing replaces it
	original string
	edited   bool
}

// NewEditor returns an editor for t holding current.
func NewEditor(t Target, current string) Editor {
	e := Editor{target: t, input: ui.NewInput()}
	e.input.CharLimit = maxLabel
	e.prefill(current)
	return e
}

// Target is what the editor renames.
func (e *Editor) Target() Target { return e.target }

// prefill puts the current label in the field, fully selected.
func (e *Editor) prefill(label string) {
	label = sanitize.Label(label)
	if e.target.Automatic(label) {
		label = ""
	}
	e.original = label
	e.input.SetValue(label)
	e.input.CursorEnd()
	e.selected = label != ""
}

// Label is the name to save: the field's value, trimmed.
func (e *Editor) Label() string { return strings.TrimSpace(e.input.Value()) }

// Unchanged reports whether saving would change nothing.
func (e *Editor) Unchanged() bool { return e.Label() == e.original }

// Key edits the field with a key other than ⏎, ⎋ and ⌃C.
func (e *Editor) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+u":
		e.input.SetValue("")
		e.selected, e.edited = false, true
		return nil
	}
	if e.selected {
		switch msg.String() {
		case "backspace", "delete", "ctrl+w":
			e.input.SetValue("")
			e.selected, e.edited = false, true
			return nil
		case "left", "home", "ctrl+a":
			e.selected = false
			e.input.CursorStart()
			return nil
		case "right", "end", "ctrl+e":
			e.selected = false
			return nil
		}
	}
	return e.edit(msg, msg.Text != "")
}

// Paste puts pasted text in the field; it replaces a selected value.
func (e *Editor) Paste(msg tea.PasteMsg) tea.Cmd { return e.edit(msg, true) }

// edit passes a message to the field; text replaces a selected value.
func (e *Editor) edit(msg tea.Msg, replaces bool) tea.Cmd {
	if e.selected && replaces {
		e.input.SetValue("")
	}
	e.selected = false
	before := e.input.Value()
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	if e.input.Value() != before {
		e.edited = true
	}
	return cmd
}

// Field is the field's state, a refused rename's problem (when not "")
// turning it red with the problem as its helper. A selected value sits on
// the selection fill, so it is clear that typing replaces it.
func (e *Editor) Field(problem string) ui.Field {
	q := ui.QueryOf(e.input, "Automatic name")
	q.RealCursor, q.Selected = true, e.selected
	if problem != "" {
		return ui.Field{Query: q, Err: true, Helper: problem}
	}
	return ui.Field{Query: q, Helper: e.Helper()}
}

// Helper is the quiet line under the field: what saving will do.
func (e *Editor) Helper() string {
	value, d := e.input.Value(), e.target.DefaultLabel()
	switch {
	case value == "" && e.original == "" && d != "":
		return "Rex calls it " + d
	case value == "" && e.original != "" && d != "":
		return "enter resets it to " + d
	case value == "" && e.original != "":
		return "enter lets Rex pick a new name"
	case value != e.original && e.original != "":
		return "Was " + e.original
	}
	return ""
}
