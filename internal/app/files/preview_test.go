package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

func previewOf(t *testing.T, path string) previewDoc {
	t.Helper()
	e, err := statEntry(path, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadPreview(context.Background(), e, previewEnv{theme: ui.NewTheme(true)})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func plain(lines []string) string { return ansi.Strip(strings.Join(lines, "\n")) }

func TestPreviewKinds(t *testing.T) {
	_, root := fixtureTree(t)
	mustWrite(t, filepath.Join(root, "unicode.txt"), strings.Repeat("界", maxPreviewBytes/3+10))
	mustWrite(t, filepath.Join(root, "long.txt"), strings.Repeat("line\n", maxPreviewLines+50))
	tests := []struct {
		name     string
		contains []string
		meta     string
	}{
		{"cmd/rexkit/main.go", []string{"1  package main", "3  func main() {}"}, "Go"},
		{"binary.bin", []string{"Binary file", "ctrl x▌ opens it"}, "binary"},
		{"empty.txt", []string{"Empty file"}, "empty"},
		{"trap.png", []string{"Named pipe. Not opened"}, "fifo"},
		{"broken-link", []string{"Broken link", "→ missing-target"}, "broken link"},
		{"docs", []string{"· guide.md"}, "1 item"},
		{"empty folder", []string{"Empty folder"}, "empty"},
		{"escapes.txt", []string{"hello ␛]52;c;Y2xpcA==␇ ␛[31mred␛[0m"}, "36 B"},
		{"unicode.txt", []string{"界界界", "preview shows the first"}, ""},
		{"long.txt", []string{"2000  line", "preview shows the first"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := previewOf(t, filepath.Join(root, tt.name))
			text := plain(doc.lines)
			for _, want := range tt.contains {
				if !strings.Contains(text, want) {
					t.Errorf("preview lacks %q:\n%s", want, text)
				}
			}
			if tt.meta != "" && !strings.Contains(strings.Join(doc.meta, " · "), tt.meta) {
				t.Errorf("meta %q lacks %q", doc.meta, tt.meta)
			}
			raw := strings.Join(doc.lines, "\n")
			for _, bad := range []string{"\x1b]", "\x07", "\x1b[2J"} {
				if strings.Contains(raw, bad) {
					t.Errorf("preview contains raw %q", bad)
				}
			}
			if len(doc.lines) > maxPreviewLines+1 {
				t.Errorf("preview has %d lines", len(doc.lines))
			}
		})
	}
}

// TestSpecialFilesNeverBlock covers the old hang: a FIFO named like an
// image, and a regular file swapped for a FIFO between listing and opening.
func TestSpecialFilesNeverBlock(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "photo.jpg")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := entry{path: fifo, kind: kindFile, size: 10} // listed as a file before the swap
	done := make(chan error, 1)
	go func() {
		_, err := loadPreview(context.Background(), stale, previewEnv{theme: ui.NewTheme(true)})
		done <- err
	}()
	select {
	case err := <-done:
		if err != errNotRegular {
			t.Errorf("err = %v, want errNotRegular", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("previewing a FIFO blocked")
	}
	if doc := previewOf(t, fifo); !strings.Contains(plain(doc.lines), "Named pipe") {
		t.Errorf("FIFO not explained: %q", doc.lines)
	}
}

func TestPreviewOfUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	path := filepath.Join(t.TempDir(), "locked.txt")
	mustWrite(t, path, "x")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	e, _ := statEntry(path, "locked.txt")
	if _, err := loadPreview(context.Background(), e, previewEnv{theme: ui.NewTheme(true)}); fsErrorText(err) != "permission denied" {
		t.Errorf("err = %v", err)
	}
}

func TestHighlightKeepsStylesPerLine(t *testing.T) {
	lines, lang, _ := highlight(context.Background(), "/* a\n   b */\nx := 1", "x.go", ui.NewTheme(true))
	if lang != "Go" || len(lines) != 3 {
		t.Fatalf("lang %q lines %q", lang, lines)
	}
	for i, l := range lines[:2] {
		if !strings.HasPrefix(l, "\x1b[") || !strings.HasSuffix(l, "\x1b[m") {
			t.Errorf("comment line %d does not carry its own style: %q", i, l)
		}
	}
	if lines, lang, _ := highlight(context.Background(), "plain", "notes.unknownext", ui.NewTheme(true)); lang != "" || lines[0] != "plain" {
		t.Errorf("unknown type: %q %q", lang, lines)
	}
}

func TestTrimPartialRune(t *testing.T) {
	full := []byte("ab界")
	for cut := 1; cut <= 2; cut++ {
		if got := string(trimPartialRune(full[:len(full)-cut])); got != "ab" {
			t.Errorf("cut %d: %q", cut, got)
		}
	}
	if got := string(trimPartialRune(full)); got != "ab界" {
		t.Errorf("whole: %q", got)
	}
}

// TestHighlightIsBounded: Chroma's backtracking lexers take minutes on
// crafted lines; such files come back plain within the budget.
func TestHighlightIsBounded(t *testing.T) {
	theme := ui.NewTheme(true)
	for name, text := range map[string]string{
		"one long line":    strings.Repeat("a", 16<<10),
		"many medium ones": strings.Repeat(strings.Repeat("a", 1000)+"\n", 64),
	} {
		start := time.Now()
		lines, lang, err := highlight(context.Background(), text, "x.desktop", theme)
		if took := time.Since(start); took > time.Second {
			t.Errorf("%s: took %v", name, took)
		}
		if err != nil || lang == "" || len(lines) != strings.Count(text, "\n")+1 {
			t.Errorf("%s: %d lines, %q, %v", name, len(lines), lang, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := highlight(ctx, strings.Repeat("x := 1\n", 500), "x.go", theme); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled highlight: %v", err)
	}
}

// The tray offers the editor for text and the default app for what is not
// text: a binary file as its preview found, an image by its name before.
func TestTrayOpensWhatIsNotText(t *testing.T) {
	home, root := fixtureTree(t)
	mustWrite(t, filepath.Join(root, "shot.png"), "not really a png")
	e := newEnv(t, home, root, 160, 40)
	tests := []struct {
		name string
		want string
	}{
		{"README.md", "edit"},
		{"binary.bin", "open"},
		{"shot.png", "open"},
	}
	for _, tt := range tests {
		e.selectPath(t, filepath.Join(root, tt.name))
		var labels []string
		for _, h := range e.m.tray().Hints {
			if h.Key == "ctrl+e" || h.Key == "ctrl+x" {
				labels = append(labels, h.Label)
			}
		}
		if len(labels) != 1 || labels[0] != tt.want {
			t.Errorf("%s: tray offers %v, want [%s]", tt.name, labels, tt.want)
		}
	}
	if !e.m.opensInApp(filepath.Join(root, "later.jpg")) {
		t.Error("an image not previewed yet should open in its app")
	}
}
