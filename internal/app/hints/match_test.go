package hints

import (
	"strings"
	"testing"

	"rexkit/internal/backdrop"
	"rexkit/internal/rextheme"
)

// cells parses a demo screen at cols×rows.
func cells(draw func(cols, rows int) string, cols, rows int) [][]backdrop.Cell {
	return backdrop.Cells(draw(cols, rows), cols, rows, rextheme.Builtin(true).Terminal)
}

// plainScreen is a screen of plain lines.
func plainScreen(lines ...string) func(cols, rows int) string {
	return func(cols, rows int) string {
		ls := append([]string(nil), lines...)
		for len(ls) < rows {
			ls = append(ls, "")
		}
		return strings.Join(ls[:rows], "\n")
	}
}

func kinds(ms []Match) map[string]Kind {
	got := map[string]Kind{}
	for _, m := range ms {
		got[m.Text] = m.Kind
	}
	return got
}

func TestFind(t *testing.T) {
	got := kinds(Find(cells(demoShell, 119, 66), demoSource(nil).skip()...))
	want := map[string]Kind{
		"https://github.com/snipeship/studio/pull/482":          KindURL,
		"https://status.studio.dev/incidents/2291":              KindURL,
		"studio-api-7d9f8b6c5d-x2k4p":                           KindPod,
		"10.42.3.17:8080":                                       KindIP,
		"127.0.0.1:5432":                                        KindIP,
		"0b9e4c2f-6a1d-4f3e-9c55-2d7e81a0f4b6":                  KindUUID,
		"internal/runs/pool.go:27":                              KindPath,
		"queue_test.go:88":                                      KindPath,
		"20261025_run_worker_id.sql":                            KindPath,
		"/Users/sam/code/studio/api/internal/runs/queue.go:142": KindPath,
		"a3f9c21":      KindSHA,
		"f3a9b2c4d1e8": KindSHA,
	}
	for text, kind := range want {
		if k, ok := got[text]; !ok || k != kind {
			t.Errorf("%q: got kind %v (found %t), want %v", text, k, ok, kind)
		}
	}
	// The pane's own folder (every prompt shows it), branches, MIME types,
	// versions, hosts and durations are not hints.
	for _, not := range []string{"~/code/studio/api", "application/json", "feat/run-queue", "v5.7.1", "studio.dev", ".drain", "0.412s"} {
		if _, ok := got[not]; ok {
			t.Errorf("%q should not be a match", not)
		}
	}
}

func TestFindAgent(t *testing.T) {
	got := kinds(Find(cells(demoAgent, 119, 36)))
	for text, kind := range map[string]Kind{
		"internal/runs/pool.go:128":                    KindPath,
		"internal/runs/queue.go:142":                   KindPath,
		"4b7e2d1":                                      KindSHA,
		"https://github.com/snipeship/studio/pull/482": KindURL,
	} {
		if k, ok := got[text]; !ok || k != kind {
			t.Errorf("%q: got kind %v (found %t), want %v", text, k, ok, kind)
		}
	}
	// Go selectors in the diff are not files.
	for _, not := range []string{"p.jobs", "p.closed", "context.Context"} {
		if _, ok := got[not]; ok {
			t.Errorf("%q should not be a match", not)
		}
	}
}

func TestFindTrimsAndPlaces(t *testing.T) {
	ms := Find(cells(plainScreen(
		"see (https://example.com/a?b=1), then ~/notes/today.md:12:3.",
		"wide 漢字 a3f9c21",
	), 70, 2))
	got := map[string]Match{}
	for _, m := range ms {
		got[m.Text] = m
	}
	if m, ok := got["https://example.com/a?b=1"]; !ok || m.Col != 5 || m.Width != 25 {
		t.Errorf("URL in brackets: %+v (found %t)", m, ok)
	}
	if _, ok := got["~/notes/today.md:12:3"]; !ok {
		t.Errorf("path with line and column, its full stop trimmed: %v", kinds(ms))
	}
	// A wide glyph takes two cells: the SHA after it starts at cell 10.
	if m, ok := got["a3f9c21"]; !ok || m.Row != 1 || m.Col != 10 {
		t.Errorf("SHA after wide glyphs: %+v (found %t)", m, ok)
	}
}

// TestFindOverlaps: where matches overlap, a URL wins, then the longest; a
// pod is a name with Kubernetes' suffixes, not any hyphenated words, and a
// hex run inside a name is no SHA.
func TestFindOverlaps(t *testing.T) {
	ms := Find(cells(plainScreen(
		"--- FAIL fix-pipeline-crash.go:12 and ./scripts/build-standard-image.sh",
		"$ git checkout fix-pipeline-crash",
		"/tmp/runs/123e4567-e89b-42d3-a456-426614174000/log.txt in rexkit-terminal-tools/main.go",
		"kube-proxy-x2kqz  studio-api-7d9f8b6c5d-x2k4p  studio-api-7d9f8b6c5d  4c1f0a9e-2b7d-4e8a-a6f3-91d",
		"see https://en.wikipedia.org/wiki/Foo_(bar), café/notes.md:3 and 9a8d3e6..c2b77f0.",
		"read /var/log/10.0.0.1.log or 10.0.0.1:80",
	), 120, 6))
	got := kinds(ms)
	want := map[string]Kind{
		"fix-pipeline-crash.go:12":                               KindPath,
		"./scripts/build-standard-image.sh":                      KindPath,
		"/tmp/runs/123e4567-e89b-42d3-a456-426614174000/log.txt": KindPath,
		"rexkit-terminal-tools/main.go":                          KindPath,
		"kube-proxy-x2kqz":                                       KindPod,
		"studio-api-7d9f8b6c5d-x2k4p":                            KindPod,
		"https://en.wikipedia.org/wiki/Foo_(bar)":                KindURL,
		"café/notes.md:3":                                        KindPath,
		"9a8d3e6":                                                KindSHA,
		"c2b77f0":                                                KindSHA,
		"/var/log/10.0.0.1.log":                                  KindPath,
		"10.0.0.1:80":                                            KindIP,
	}
	for text, kind := range want {
		if k, ok := got[text]; !ok || k != kind {
			t.Errorf("%q: got kind %v (found %t), want %v", text, k, ok, kind)
		}
	}
	if len(got) != len(want) {
		t.Errorf("found %v, want only %v", got, want)
	}
}

func TestNamespace(t *testing.T) {
	c := cells(plainScreen(
		"$ kubectl -n staging get pods",
		"web-7d9f8b6c5d-x2k4p   1/1",
		"$ kubectl get pods --namespace=prod",
		"api-7d9f8b6c5d-q8m4z   1/1",
		"$ kubectl get pods",
		"db-7d9f8b6c5d-h7t2n    1/1",
		"$ kubectl get pods -A",
		"NAMESPACE   NAME                   READY",
		"kube-system kube-proxy-x2kqz       1/1",
		"$ echo done",
	), 40, 10)
	for row, want := range map[int]string{1: "staging", 3: "prod", 5: "", 0: "staging", 8: "kube-system", 6: ""} {
		if got := namespace(c, row); got != want {
			t.Errorf("row %d: namespace %q, want %q", row, got, want)
		}
	}
}

func TestSplitPosition(t *testing.T) {
	tests := []struct {
		in        string
		path      string
		line, col int
	}{
		{"pool.go", "pool.go", 0, 0},
		{"pool.go:128", "pool.go", 128, 0},
		{"pool.go:128:9", "pool.go", 128, 9},
		{"/a/b.go:1:2", "/a/b.go", 1, 2},
	}
	for _, tt := range tests {
		path, line, col := splitPosition(tt.in)
		if path != tt.path || line != tt.line || col != tt.col {
			t.Errorf("splitPosition(%q) = %q %d %d, want %q %d %d", tt.in, path, line, col, tt.path, tt.line, tt.col)
		}
	}
}

func TestPositionArgs(t *testing.T) {
	tests := []struct {
		editor    string
		line, col int
		want      string
	}{
		{"/opt/homebrew/bin/nvim", 12, 0, "+12 f.go"},
		{"/opt/homebrew/bin/nvim", 12, 5, "+call cursor(12,5) f.go"},
		{"/usr/bin/vi", 12, 5, "+12 f.go"},
		{"emacs", 12, 5, "+12:5 f.go"},
		{"nano", 12, 5, "+12,5 f.go"},
		{"hx", 12, 5, "f.go:12:5"},
		{"code", 12, 5, "f.go"},
		{"nvim", 0, 0, "f.go"},
	}
	for _, tt := range tests {
		if got := strings.Join(positionArgs([]string{tt.editor}, "f.go", tt.line, tt.col), " "); got != tt.want {
			t.Errorf("%s at %d:%d: %q, want %q", tt.editor, tt.line, tt.col, got, tt.want)
		}
	}
}
