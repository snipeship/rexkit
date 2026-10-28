package hints

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The user's patterns (hints_patterns, a hint key's patterns) are found
// besides the built-in ones, win where they overlap one, hint their first
// group, and open as the built-in kind that matches all of what they found.
func TestFindUserPatterns(t *testing.T) {
	ps, err := Compile([]string{`[A-Z]+-\d+`, `sha=(\w+)`, `https://jira\.example\.com/\S+`}, false)
	if err != nil {
		t.Fatal(err)
	}
	screen := cells(plainScreen(
		"fix ABC-123 in ~/notes/today.md, see https://jira.example.com/browse/ABC-9",
		"build sha=deadbeef1 at https://example.com/x",
	), 90, 2)
	got := kinds(Find(screen, ps))
	want := map[string]Kind{
		"ABC-123": KindText, "~/notes/today.md": KindPath, "deadbeef1": KindSHA, // a group of a SHA opens as one
		"https://jira.example.com/browse/ABC-9": KindURL, "https://example.com/x": KindURL,
	}
	for text, kind := range want {
		if got[text] != kind {
			t.Errorf("%q: kind %v (found %v), want %v", text, got[text], got, kind)
		}
	}
	if _, ok := got["ABC-9"]; ok {
		t.Error("ABC-9 inside the user's URL pattern is a match of its own")
	}

	only, err := Compile([]string{`[A-Z]+-\d+`}, true)
	if err != nil {
		t.Fatal(err)
	}
	ms := Find(screen, only)
	var texts []string
	for _, m := range ms {
		texts = append(texts, m.Text)
	}
	if !slices.Equal(texts, []string{"ABC-123", "ABC-9"}) {
		t.Errorf("only the user's patterns: %v, want ABC-123 ABC-9", texts)
	}

	if _, err := Compile([]string{`(unclosed`}, false); err == nil || !strings.Contains(err.Error(), "(unclosed") {
		t.Errorf("a broken pattern: %v, want an error naming it", err)
	}
}

func TestAlphabet(t *testing.T) {
	for _, bad := range []string{"a", "aa", "aSd", "a d", "a\td"} {
		if CheckAlphabet(bad) == nil {
			t.Errorf("CheckAlphabet(%q) = nil, want an error", bad)
		}
	}
	for _, good := range []string{"jfkdls;ahgurieowpq", "asdfqwerzxcvjklmiuopghtybn", "12"} {
		if err := CheckAlphabet(good); err != nil {
			t.Errorf("CheckAlphabet(%q) = %v", good, err)
		}
	}
	ms := []Match{{Row: 0, Text: "far"}, {Row: 9, Text: "near"}}
	Assign(ms, 9, 0, "jf")
	if ms[1].Label != "j" || ms[0].Label != "f" {
		t.Errorf("labels %q %q, want j for the nearest, f", ms[1].Label, ms[0].Label)
	}
}

// A key of the hints setting opens Hints for one thing: every label does
// it, ⇧ and ⇥ change nothing, and the key bar says what it does.
func TestFixedAction(t *testing.T) {
	src := Source{Screen: plainScreen("open https://example.com/a and ~/notes/today.md")}
	for _, a := range []Action{ActCopy, ActPaste, ActOpen, ActCommand} {
		m := toolWith(t, options{fixed: a, label: "yank it"}, src, 60, 6, true)
		if a == ActCommand && !strings.Contains(ansi.Strip(m.View().Content), "yank it") {
			t.Errorf("the key bar does not say %q:\n%s", "yank it", ansi.Strip(m.View().Content))
		}
		l := label(t, m, "https://example.com/a")
		press(t, m, "tab")
		if m.paste {
			t.Errorf("%s: ⇥ switched to paste", a)
		}
		press(t, m, strings.ToUpper(l))
		if p := m.Picked(); p == nil || p.Action != a || p.Match.Text != "https://example.com/a" {
			t.Errorf("%s: ⇧ and the label picked %+v, want %s of the URL", a, p, a)
		}
	}
	m := toolWith(t, options{alphabet: "jf"}, src, 60, 6, true)
	if l := label(t, m, "~/notes/today.md"); !strings.ContainsAny(l[:1], "jf") {
		t.Errorf("label %q is not of the alphabet jf", l)
	}
}

func TestHintOptions(t *testing.T) {
	bad := []struct {
		action, command, alphabet, patterns string
		only                                bool
	}{
		{action: "yank"},
		{action: "command"},
		{action: "copy", command: `["gh"]`},
		{action: "command", command: `[]`},
		{alphabet: "aa"},
		{patterns: `["(x"]`},
		{patterns: `"x"`},
		{only: true},
	}
	for _, b := range bad {
		if _, _, err := hintOptions(b.action, b.command, "", b.alphabet, b.patterns, b.only); err == nil {
			t.Errorf("hintOptions(%+v) = nil error", b)
		}
	}
	opts, command, err := hintOptions("command", `["gh", "browse"]`, "browse", "jf", `["#\\d+"]`, true)
	if err != nil || opts.fixed != ActCommand || !slices.Equal(command, []string{"gh", "browse"}) || len(opts.patterns) != 1 {
		t.Errorf("hintOptions: %+v %v %v", opts, command, err)
	}
}

// A hint key's command runs on its own in the pane's folder, the text its
// last argument, the program looked up as lookPath does.
func TestRunHint(t *testing.T) {
	wd := newWorld(t, "gh")
	var ran []string
	var in string
	wd.w.start = func(dir string, argv ...string) error { in, ran = dir, argv; return nil }
	wd.w.command = []string{"gh", "issue", "view"}
	o := wd.w.do(t.Context(), pick(ActCommand, KindText, "#42"))
	if !slices.Equal(ran, []string{"/usr/local/bin/gh", "issue", "view", "#42"}) || in != wd.dir {
		t.Errorf("ran %v in %s, want gh issue view #42 in %s", ran, in, wd.dir)
	}
	if !strings.Contains(o.text, "Ran gh") {
		t.Errorf("toast %q", o.text)
	}
	wd.w.command = []string{"missing"}
	if o := wd.w.do(t.Context(), pick(ActCommand, KindText, "x")); !strings.Contains(o.text, "not installed") {
		t.Errorf("a missing program: %q", o.text)
	}
	wd.w.command = []string{"/bin/broken"}
	wd.w.start = func(string, ...string) error { return errors.New("exec format error") }
	if o := wd.w.do(t.Context(), pick(ActCommand, KindText, "x")); !strings.Contains(o.text, "exec format error") {
		t.Errorf("a program that won't start: %q", o.text)
	}
	if o := wd.w.do(t.Context(), pick(ActOpen, KindText, "ABC-1")); !strings.Contains(o.text, "copied") {
		t.Errorf("opening what nothing opens: %q, want it copied", o.text)
	}
}
