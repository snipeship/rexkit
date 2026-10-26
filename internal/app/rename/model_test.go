package rename

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
)

func TestEditing(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		keys     []string
		value    string
		selected bool
	}{
		{"prefilled and selected", "api-server", nil, "api-server", true},
		{"typing replaces the selection", "api-server", []string{"w", "e", "b"}, "web", false},
		{"backspace clears the selection", "api-server", []string{"backspace"}, "", false},
		{"right keeps the text, cursor at the end", "api", []string{"right", "s"}, "apis", false},
		{"left goes to the start", "api", []string{"left", "x"}, "xapi", false},
		{"ctrl+u clears", "api", []string{"ctrl+u"}, "", false},
		{"automatic labels start empty", "Block 3bccb71a", nil, "", false},
		{"labels are sanitized", "a\x1b[31mb\u202e", nil, "ab<U+202E>", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := dialog(t, tt.current, nil, 56, 9)
			typeKeys(t, m, tt.keys...)
			if got := m.input.Value(); got != tt.value || m.selected != tt.selected {
				t.Errorf("value %q selected %v, want %q %v", got, m.selected, tt.value, tt.selected)
			}
		})
	}
}

func TestPaste(t *testing.T) {
	m := dialog(t, "old", nil, 56, 9)
	m.Update(tea.PasteMsg{Content: "new name"})
	if got := m.input.Value(); got != "new name" {
		t.Errorf("paste over a selection = %q", got)
	}
}

func TestSave(t *testing.T) {
	tests := []struct {
		name    string
		current string
		keys    []string
		applied []string // labels apply was called with
		saved   bool
	}{
		{"unchanged closes without a call", "api", []string{"enter"}, nil, true},
		{"a new label", "api", []string{"w", "e", "b", "enter"}, []string{"web"}, false},
		{"spaces are trimmed", "api", []string{" ", "w", " ", "enter"}, []string{"w"}, false},
		{"empty resets to automatic", "api", []string{"ctrl+u", "enter"}, []string{""}, false},
		{"double enter saves once", "api", []string{"x", "enter", "enter"}, []string{"x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var applied []string
			apply := func(label string) tea.Cmd {
				applied = append(applied, label)
				return func() tea.Msg { return savedMsg{} }
			}
			m := dialog(t, tt.current, apply, 56, 9)
			cmd := typeKeys(t, m, tt.keys...)
			if strings.Join(applied, "|") != strings.Join(tt.applied, "|") || len(applied) != len(tt.applied) {
				t.Errorf("applied %q, want %q", applied, tt.applied)
			}
			if m.saved != tt.saved {
				t.Errorf("saved = %v, want %v", m.saved, tt.saved)
			}
			if tt.saved && !quits(cmd) {
				t.Error("an unchanged label should just close")
			}
		})
	}
}

func TestSaveResult(t *testing.T) {
	m := dialog(t, "api", func(string) tea.Cmd { return nil }, 56, 9)
	typeKeys(t, m, "x", "enter")
	if _, cmd := m.Update(savedMsg{}); !quits(cmd) || !m.saved {
		t.Error("a saved label should close the dialog")
	}
	m = dialog(t, "api", func(string) tea.Cmd { return nil }, 56, 9)
	typeKeys(t, m, "x", "enter")
	m.Update(savedMsg{problem: "That pane is gone"})
	if m.saved || m.saving || m.toasts.Current().Text != "That pane is gone" {
		t.Errorf("a failure must stay open and say why: saved=%v saving=%v toast=%q", m.saved, m.saving, m.toasts.Current().Text)
	}
	if !strings.Contains(plain(screen(m)), "✗ That pane is gone") {
		t.Errorf("the error is not shown:\n%s", plain(screen(m)))
	}
}

func TestCancel(t *testing.T) {
	for _, k := range []string{"esc", "ctrl+c"} {
		m := dialog(t, "api", func(string) tea.Cmd { t.Fatal("cancel must not rename"); return nil }, 56, 9)
		if cmd := typeKeys(t, m, "x", k); !quits(cmd) || m.saved {
			t.Errorf("%s should close without saving", k)
		}
	}
	m := dialog(t, "api", nil, 56, 9)
	m.Update(selfMsg{floating: true})
	m.Update(tea.FocusMsg{})
	if _, cmd := m.Update(tea.BlurMsg{}); !quits(cmd) {
		t.Error("losing focus should close a floating dialog")
	}
	m.opts.dismissOnBlur = false
	m.Update(tea.FocusMsg{})
	if _, cmd := m.Update(tea.BlurMsg{}); quits(cmd) {
		t.Error("--dismiss-on-blur=false should keep the dialog")
	}
}

func TestCurrentLabelArrivesLate(t *testing.T) {
	m := dialog(t, "", nil, 56, 9)
	m.Update(currentMsg{label: "api"})
	if m.input.Value() != "api" || !m.selected {
		t.Errorf("late label not prefilled: %q", m.input.Value())
	}
	m = dialog(t, "", nil, 56, 9)
	typeKeys(t, m, "w")
	m.Update(currentMsg{label: "api"})
	if m.input.Value() != "w" {
		t.Errorf("a late label overwrote typing: %q", m.input.Value())
	}
}

func TestApplier(t *testing.T) {
	fake := rexfake.New()
	sess, win, block := fake.AddSession("work")
	ctx := context.Background()
	tests := []struct {
		target Target
		label  string
		check  func(rex.View) string
	}{
		{Target{"pane", block}, "api", func(v rex.View) string { _, _, b, _ := v.FindBlock(block); return b.Label }},
		{Target{"tab", win}, "editor", func(v rex.View) string { w, _ := v.Window(win); return w.Label }},
		{Target{"session", sess}, "studio", func(v rex.View) string { return v.Label }},
	}
	for _, tt := range tests {
		msg := applier(fake, sess, tt.target)(tt.label)()
		if msg.(savedMsg).problem != "" {
			t.Fatalf("%s: %v", tt.target.Kind, msg)
		}
		view, _ := fake.View(ctx, sess)
		if got := tt.check(view); got != tt.label {
			t.Errorf("%s label = %q, want %q", tt.target.Kind, got, tt.label)
		}
		if got := currentLabel(fake, sess, tt.target)(); got != (currentMsg{tt.label}) {
			t.Errorf("%s current label = %v", tt.target.Kind, got)
		}
	}
	msg := applier(fake, sess, Target{"pane", "block:gone"})("x")()
	if got := msg.(savedMsg).problem; got != "That pane is gone" {
		t.Errorf("missing pane: %q", got)
	}
	fake.Fail("session.set_label", &rex.Error{Op: "session.set_label", Message: "label too long"})
	msg = applier(fake, sess, Target{"session", sess})("x")()
	if got := msg.(savedMsg).problem; got != "Could not rename: label too long" {
		t.Errorf("failure: %q", got)
	}
}

func TestResolve(t *testing.T) {
	po := popup.Options{OriginBlock: "b1", OriginWindow: "w1"}
	tests := []struct {
		in      Target
		want    string
		wantErr bool
	}{
		{Target{"pane", ""}, "b1", false},
		{Target{"tab", ""}, "w1", false},
		{Target{"session", ""}, "s1", false},
		{Target{"pane", "b9"}, "b9", false},
		{Target{"window", ""}, "", true},
	}
	for _, tt := range tests {
		got, err := resolve(tt.in, "s1", po)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got.ID != tt.want) {
			t.Errorf("resolve(%v) = %v, %v", tt.in, got, err)
		}
	}
	if _, err := resolve(Target{"pane", ""}, "s1", popup.Options{}); err == nil || !strings.Contains(err.Error(), "--id") {
		t.Errorf("no origin: %v", err)
	}
}

func TestProblem(t *testing.T) {
	if got := problem("tab", errors.New("boom")); got != "Could not rename: boom" {
		t.Error(got)
	}
	if problem("tab", nil) != "" {
		t.Error("success is not a problem")
	}
}
