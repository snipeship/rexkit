package files

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadFolder(t *testing.T) {
	_, root := fixtureTree(t)
	l, err := readFolder(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]entry{}
	var order []string
	for _, e := range l.entries {
		byName[e.rel] = e
		order = append(order, e.rel)
	}
	tests := []struct {
		name string
		kind kind
		link bool
	}{
		{"docs", kindDir, false},
		{"README.md", kindFile, false},
		{"readme-link.md", kindFile, true},
		{"broken-link", kindBroken, true},
		{"trap.png", kindSpecial, false},
	}
	for _, tt := range tests {
		e, ok := byName[tt.name]
		if !ok || e.kind != tt.kind || e.link != tt.link {
			t.Errorf("%s: %+v (listed %v)", tt.name, e, ok)
		}
	}
	if _, ok := byName[".env"]; ok {
		t.Error("hidden file listed")
	}
	if !slices.IsSortedFunc(order[:5], func(a, b string) int { return compareNames(a, b) }) || !byName[order[0]].isDir() {
		t.Errorf("folders do not come first, by name: %v", order)
	}
	if e := byName["evil\x1b[31mname.txt"]; e.label != "evil␛[31mname.txt" {
		t.Errorf("label %q is not sanitized", e.label)
	}
	if _, err := readFolder(context.Background(), filepath.Join(root, "missing"), false); err == nil {
		t.Error("missing folder listed")
	}
}

func TestWalkTreeOutsideGit(t *testing.T) {
	_, root := fixtureTree(t)
	l, err := walkTree(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, e := range l.entries {
		rels = append(rels, e.rel)
	}
	for _, want := range []string{"cmd", "cmd/rexkit", "cmd/rexkit/main.go", "node_modules", "empty folder"} {
		if !slices.Contains(rels, want) {
			t.Errorf("walk lacks %s", want)
		}
	}
	for _, r := range rels {
		if strings.HasPrefix(r, "node_modules/") || strings.HasPrefix(r, ".") {
			t.Errorf("walk entered %s", r)
		}
	}
	if i, j := slices.Index(rels, "cmd"), slices.Index(rels, "cmd/rexkit/main.go"); i > j {
		t.Error("a folder sorts after its contents")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := walkTree(ctx, root, true); err == nil {
		t.Error("walk ignored cancellation")
	}
}

func TestReadTreeInsideGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, root := fixtureTree(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, ".gitignore"), "docs/\n")
	l, err := readTree(context.Background(), runCommand, root, false)
	if err != nil || !l.viaGit {
		t.Fatalf("readTree: %v, via git %v", err, l.viaGit)
	}
	var rels []string
	for _, e := range l.entries {
		rels = append(rels, e.rel)
	}
	if slices.Contains(rels, "docs/guide.md") {
		t.Error("ignored file listed")
	}
	for _, want := range []string{"cmd/rexkit/main.go", "cmd", "empty folder", "docs"} {
		if !slices.Contains(rels, want) {
			t.Errorf("git tree lacks %s: %v", want, rels)
		}
	}

	mustWrite(t, filepath.Join(root, "README.md"), "changed\n")
	git("add", "cmd")
	s, ok := gitStatus(context.Background(), runCommand, filepath.Join(root, "cmd"))
	if !ok || s.branch != "main" || s.prefix != "cmd/" {
		t.Fatalf("status %+v ok %v", s, ok)
	}
	if b := s.badge("rexkit/main.go"); b != 'A' {
		t.Errorf("badge for staged file %q", b)
	}
	if b := s.badge("rexkit"); b != 'A' {
		t.Errorf("folder badge %q", b)
	}
}

func TestParseStatus(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid 1234567890abcdef",
		"# branch.head (detached)",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 abc abc src/app/main.go",
		"1 A. N... 000000 100644 100644 000 abc src/new.go",
		"2 R. N... 100644 100644 100644 abc abc R100 docs/new name.md",
		"docs/old.md",
		"u UU N... 100644 100644 100644 100644 a b c conflict.txt",
		"? notes/",
		"? todo.txt",
		"! ignored.log",
		"",
	}, "\x00")
	s := parseStatus([]byte(out))
	if s.branch != "1234567" || s.ahead != 2 || s.behind != 1 || s.dirty != 6 {
		t.Fatalf("status %+v", s)
	}
	tests := map[string]byte{
		"src/app/main.go":  'M',
		"src/app":          'M',
		"src":              'M', // modified outranks added
		"src/new.go":       'A',
		"docs/new name.md": 'R',
		"docs/old.md":      0,
		"conflict.txt":     'U',
		"notes":            '?',
		"todo.txt":         '?',
		"ignored.log":      0,
	}
	for p, want := range tests {
		if got := s.badges[p]; got != want {
			t.Errorf("badge %s = %q, want %q", p, got, want)
		}
	}
}

func TestTreeSet(t *testing.T) {
	s := newTreeSet(false)
	got := s.add("", []string{"a/b/c.go", "a/d.go", ".github/ci.yml", "x/.env", "top.md", "nested/"})
	want := []string{"a/b", "a", "a/b/c.go", "a/d.go", "top.md", "nested"}
	if !slices.Equal(got, want) {
		t.Errorf("add = %v, want %v", got, want)
	}
	if !s.leaves["nested"] || s.leaves["a"] || !s.leaves["top.md"] {
		t.Errorf("leaves %v", s.leaves)
	}
	if got := s.add("nested/", []string{"y.txt", "deep/z.txt"}); !slices.Equal(got, []string{"nested/y.txt", "nested/deep", "nested/deep/z.txt"}) {
		t.Errorf("nested add = %v", got)
	}
	if got := newTreeSet(true).add("", []string{".github/ci.yml"}); !slices.Equal(got, []string{".github", ".github/ci.yml"}) {
		t.Errorf("hidden add = %v", got)
	}
}

func TestDisplayHelpers(t *testing.T) {
	home := "/Users/me"
	for _, tt := range []struct{ in, want string }{
		{"/Users/me", "~"},
		{"/Users/me/code", "~/code"},
		{"/Users/meow", "/Users/meow"},
		{"/tmp/a\x1bb", "/tmp/a␛b"},
	} {
		if got := displayPath(tt.in, home); got != tt.want {
			t.Errorf("displayPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	for _, tt := range []struct {
		dir  string
		want []string
	}{
		{"/Users/me/Coding/rexkit", []string{"~", "Coding", "rexkit"}},
		{"/Users/me", []string{"~"}},
		{"/usr/local", []string{"/", "usr", "local"}},
		{"/", []string{"/"}},
	} {
		var got []string
		for _, c := range crumbs(tt.dir, home) {
			got = append(got, c.label)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("crumbs(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
	if c := crumbs("/Users/me/Coding/rexkit", home); c[1].path != "/Users/me/Coding" {
		t.Errorf("crumb path %q", c[1].path)
	}
	for _, tt := range []struct{ typed, want string }{
		{"~", home},
		{"~/x/../y", "/Users/me/y"},
		{"sub", "/base/sub"},
		{"/abs/", "/abs"},
	} {
		if got := expandPath(tt.typed, "/base", home); got != tt.want {
			t.Errorf("expandPath(%q) = %q, want %q", tt.typed, got, tt.want)
		}
	}
}

func TestSizes(t *testing.T) {
	for _, tt := range []struct {
		n    int64
		want string
	}{{0, "0 B"}, {1023, "1023 B"}, {1126, "1.1 KB"}, {500 << 20, "500 MB"}, {3 << 40, "3.0 TB"}} {
		if got := humanSize(tt.n); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	for _, tt := range []struct {
		n      int
		capped bool
		want   string
	}{{0, false, "empty"}, {1, false, "1 item"}, {12, false, "12 items"}, {1234, false, "1.2k items"}, {10_000, true, "10k+ items"}} {
		if got := itemCount(tt.n, tt.capped); got != tt.want {
			t.Errorf("itemCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestCompletions(t *testing.T) {
	entries := []entry{{path: "/r/Coding"}, {path: "/r/code"}, {path: "/r/.config"}, {path: "/r/decode"}}
	names := func(rows []row) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.e.name())
		}
		return out
	}
	if got := names(completions(entries, "cod", false)); !slices.Equal(got, []string{"Coding", "code", "decode"}) {
		t.Errorf("completions = %v", got)
	}
	if got := names(completions(entries, ".c", false)); !slices.Equal(got, []string{".config"}) {
		t.Errorf("dot completions = %v", got)
	}
	if got := commonPrefix([]string{"Coding", "Code"}); got != "Cod" {
		t.Errorf("commonPrefix = %q", got)
	}
	if got := commonPrefix([]string{"日本", "日記"}); got != "日" {
		t.Errorf("commonPrefix cut a rune: %q", got)
	}
}

func TestParseZoxide(t *testing.T) {
	home, root := fixtureTree(t)
	out := "  40.0 " + root + "\n  12.5 " + filepath.Join(root, "docs") + "\n   3.0 /nonexistent\n   2.0 " + filepath.Join(root, "README.md") + "\n"
	got := parseZoxide(out, home, root)
	if len(got) != 1 || got[0].path != filepath.Join(root, "docs") || got[0].label != "~/project/docs" {
		t.Fatalf("projects %+v", got)
	}
}

// gitRepo makes dir a git repository and returns a helper that runs git in
// it (tests that need git skip without it).
func gitRepo(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	return run
}

func relsOf(l listing) []string {
	var out []string
	for _, e := range l.entries {
		out = append(out, e.rel)
	}
	return out
}

// TestReadTreeInIgnoredFolderAndNestedRepos: ⌃F inside an ignored folder
// walks it (git lists nothing there), and a nested repository is listed
// once, with its own files.
func TestReadTreeInIgnoredFolderAndNestedRepos(t *testing.T) {
	outer := t.TempDir()
	git := gitRepo(t, outer)
	mustWrite(t, filepath.Join(outer, ".gitignore"), "build/\n")
	mustWrite(t, filepath.Join(outer, "a.txt"), "a")
	mustWrite(t, filepath.Join(outer, "build", "deep", "f.txt"), "f")
	nested := filepath.Join(outer, "nested")
	mustMkdir(t, nested)
	gitRepo(t, nested)
	mustWrite(t, filepath.Join(nested, "y.txt"), "y")
	mustWrite(t, filepath.Join(nested, "sub", "z.txt"), "z")
	git("add", "a.txt")

	l, err := readTree(context.Background(), runCommand, outer, false)
	if err != nil || !l.viaGit {
		t.Fatalf("readTree: %v, via git %v", err, l.viaGit)
	}
	rels := relsOf(l)
	for _, want := range []string{"a.txt", "build", "nested", "nested/y.txt", "nested/sub", "nested/sub/z.txt"} {
		if !slices.Contains(rels, want) {
			t.Errorf("lacks %s: %v", want, rels)
		}
	}
	for _, bad := range []string{"nested/", "build/deep/f.txt"} {
		if slices.Contains(rels, bad) {
			t.Errorf("lists %q: %v", bad, rels)
		}
	}
	if n := len(rels); n != len(slices.Compact(slices.Sorted(slices.Values(rels)))) {
		t.Errorf("duplicates: %v", rels)
	}

	l, err = readTree(context.Background(), runCommand, filepath.Join(outer, "build"), false)
	if err != nil || l.viaGit {
		t.Fatalf("inside an ignored folder: %v, via git %v", err, l.viaGit)
	}
	if rels := relsOf(l); !slices.Contains(rels, "deep/f.txt") {
		t.Errorf("ignored folder's files not listed: %v", rels)
	}
}

// TestFolderSwappedForFIFO: every place that opens a folder fails at once
// when it has become a FIFO, instead of blocking in open(2).
func TestFolderSwappedForFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "was-a-folder")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	e := entry{path: fifo, kind: kindDir} // as it was listed
	checks := map[string]func() error{
		"folderPreview": func() error { _, err := folderPreview(context.Background(), e, previewDoc{}, previewEnv{}); return err },
		"countFolder": func() error {
			if c := countFolder(fifo, false); !c.failed {
				return nil
			}
			return errors.New("failed")
		},
		"readFolder": func() error { _, err := readFolder(context.Background(), fifo, false); return err },
		"walkTree":   func() error { _, err := walkTree(context.Background(), fifo, false); return err },
	}
	for name, check := range checks {
		done := make(chan error, 1)
		go func() { done <- check() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s accepted a FIFO", name)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s blocked on a FIFO", name)
		}
	}
	_, err := readDir(fifo)
	if got := fsErrorText(err); got != "no longer a folder" {
		t.Errorf("fsErrorText = %q", got)
	}
}

// TestDecomposedNames: macOS keeps a name's bytes as written (often NFD)
// while git reports it precomposed (NFC). The entry must still get its
// badge, and appear once in all-files mode.
func TestDecomposedNames(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("normalization-insensitive file names are a macOS matter")
	}
	root := t.TempDir()
	gitRepo(t, root)
	const nfdFile, nfdDir = "café.txt", "diré"
	mustWrite(t, filepath.Join(root, nfdFile), "x")
	mustWrite(t, filepath.Join(root, nfdDir, "x.txt"), "x")
	mustWrite(t, filepath.Join(root, "plain.txt"), "x")

	s, ok := gitStatus(context.Background(), runCommand, root)
	if !ok {
		t.Fatal("no git status")
	}
	l, err := readFolder(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range l.entries {
		if b := s.badgeOf(e); b != '?' {
			t.Errorf("%q: badge %q, want ?", e.rel, b)
		}
	}
	tree, err := readTree(context.Background(), runCommand, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tree.entries); n != 4 { // café.txt, diré, diré/x.txt, plain.txt: each once
		t.Errorf("%d entries: %q", n, relsOf(tree))
	}
}

// TestGitRunsNothingFromTheRepository: a repository's own config can name
// programs (core.fsmonitor, filter drivers) that git status and ls-files
// would run. Browsing it must not run them.
func TestGitRunsNothingFromTheRepository(t *testing.T) {
	root := t.TempDir()
	git := gitRepo(t, root)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "evil.sh")
	mustWrite(t, script, "#!/bin/sh\necho ran >> '"+marker+"'\ncat\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "a.txt"), "hello\n")
	git("add", "a.txt")
	git("commit", "-qm", "x")
	git("config", "core.fsmonitor", script)
	git("config", "filter.x.clean", script)
	git("config", "filter.x.required", "true")
	mustWrite(t, filepath.Join(root, ".gitattributes"), "* filter=x\n")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "a.txt"), later, later); err != nil { // stat-dirty: status re-reads it
		t.Fatal(err)
	}
	if _, ok := gitStatus(context.Background(), runCommand, root); !ok {
		t.Fatal("no git status")
	}
	if _, err := readTree(context.Background(), runCommand, root, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err == nil {
		t.Fatalf("the repository's program ran: %q", data)
	}
}

// TestProjectsFromZoxide goes through the runner seam, so it does not
// depend on zoxide being installed.
func TestProjectsFromZoxide(t *testing.T) {
	home := t.TempDir()
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	mustMkdir(t, a)
	mustMkdir(t, b)
	mustWrite(t, filepath.Join(home, "file"), "")
	out := "  20.0 " + a + "\n  12.5 " + b + "\n   9.0 " + home + "\n   3.0 " + filepath.Join(home, "file") + "\n   1.0 /gone\n"
	run := func(_ context.Context, _, _, name string, args ...string) ([]byte, error) {
		if name != "zoxide" || !slices.Equal(args, []string{"query", "--list", "--score"}) {
			return nil, errors.New("unexpected " + name)
		}
		return []byte(out), nil
	}
	got := projects(context.Background(), run, home, b)
	if len(got) != 1 || got[0].path != a || got[0].rel != "~/a" {
		t.Fatalf("projects = %+v", got)
	}
	if got := projects(context.Background(), noTools, home, b); got != nil {
		t.Errorf("without zoxide: %+v", got)
	}
}
