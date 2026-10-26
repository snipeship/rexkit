package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// Preview bounds.
const (
	maxPreviewBytes   = 64 << 10
	maxPreviewLines   = 2000
	maxFolderPreview  = 200
	folderPreviewScan = 5000
)

// previewDoc is a loaded preview: title metadata, content lines (styled,
// sanitized) and, for images, the picture to place.
type previewDoc struct {
	path    string   // the entry previewed
	name    string   // shown bold at the start of the title
	meta    []string // shown after the name: size, language (the age is added when shown)
	modTime time.Time
	lines   []string
	picture *picture
	folder  bool // a folder's listing: a rule under its title
	// opaque: not text (an image or a binary file), for its default app
	// (⌃X) rather than the editor.
	opaque bool
}

// previewEnv is what loading a preview depends on besides the entry.
type previewEnv struct {
	theme  ui.Theme
	hidden bool
}

// errNotRegular means a path stopped being a regular file between listing
// and opening it.
var errNotRegular = errors.New("not a regular file")

// loadPreview describes an entry for the preview pane. It only ever opens
// regular files, and opens them non-blocking, so FIFOs and devices can never
// hang it.
func loadPreview(ctx context.Context, e entry, env previewEnv) (previewDoc, error) {
	t := env.theme
	doc := previewDoc{path: e.path, name: sanitize.Name(e.name()), modTime: e.modTime}
	switch e.kind {
	case kindBroken:
		target, _ := os.Readlink(e.path)
		doc.meta = []string{"broken link"}
		doc.lines = []string{
			t.Err.Render("Broken link"),
			ui.Line{{Text: "→ " + sanitize.Name(target), Style: t.Muted}, {Text: "  (missing)", Style: t.Subtle}}.Render(),
		}
		return doc, nil
	case kindSpecial:
		doc.meta = []string{specialWord(e.mode)}
		doc.lines = []string{t.Muted.Render(specialExplanation(e))}
		return doc, nil
	case kindDir:
		doc.name += "/"
		doc.folder = true
		return folderPreview(ctx, e, doc, env)
	}
	f, err := openRegular(e.path)
	if err != nil {
		return doc, err
	}
	defer f.Close()
	if isImage(e.path) {
		doc.opaque = true
		pic, err := loadPicture(ctx, f, e.size)
		if ctx.Err() != nil {
			return doc, ctx.Err() // not wanted any more: nothing to show or cache
		}
		if err != nil {
			doc.meta = []string{humanSize(e.size)}
			doc.lines = []string{t.Muted.Render("Can't show this image: " + sanitize.Label(err.Error()))}
			return doc, nil
		}
		doc.picture = pic
		doc.meta = []string{pic.format + " " + pic.dimensions(), humanSize(e.size)}
		return doc, nil
	}
	return textPreview(ctx, f, e, doc, env)
}

// openRegular opens a file for reading only if it is (still) a regular file.
// O_NONBLOCK makes opening a FIFO that replaced it return at once.
func openRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errNotRegular
	}
	return f, nil
}

func specialExplanation(e entry) string {
	switch specialWord(e.mode) {
	case "fifo":
		return "Named pipe. Not opened: reading it could wait forever."
	case "socket":
		return "Unix socket. There is nothing to preview."
	case "device":
		return "Device file. Not opened: reading it could have side effects."
	}
	return "Special file. Not opened."
}

func textPreview(ctx context.Context, f *os.File, e entry, doc previewDoc, env previewEnv) (previewDoc, error) {
	t := env.theme
	data, err := io.ReadAll(io.LimitReader(f, maxPreviewBytes+1))
	if err != nil {
		return doc, err
	}
	truncated := len(data) > maxPreviewBytes
	if truncated {
		data = trimPartialRune(data[:maxPreviewBytes])
	}
	switch {
	case len(data) == 0:
		doc.meta = []string{"empty"}
		doc.lines = []string{t.Muted.Render("Empty file")}
		return doc, nil
	case bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data):
		doc.meta, doc.opaque = []string{humanSize(e.size), "binary"}, true
		doc.lines = []string{ui.Styled("Binary file: ", t.Muted).
			Append(t.Keycap(ui.CapLabel("ctrl+x"))...).
			Append(ui.Span{Text: " opens it with its default app.", Style: t.Muted}).Render()}
		return doc, nil
	}
	if err := ctx.Err(); err != nil {
		return doc, err
	}
	text := sanitize.FileText(string(data))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > maxPreviewLines {
		lines, truncated = lines[:maxPreviewLines], true
	}
	code, language, err := highlight(ctx, strings.Join(lines, "\n"), e.name(), t)
	if err != nil {
		return doc, err
	}
	doc.lines = numberLines(code, t)
	doc.meta = []string{humanSize(e.size), language}
	if truncated {
		doc.lines = append(doc.lines, t.Subtle.Render("… preview shows the first "+humanSize(int64(len(data)))))
	}
	return doc, nil
}

// trimPartialRune drops an incomplete UTF-8 sequence cut off at the end.
func trimPartialRune(b []byte) []byte {
	for n := 0; n < utf8.UTFMax && n < len(b); n++ {
		if utf8.Valid(b[:len(b)-n]) {
			return b[:len(b)-n]
		}
	}
	return b
}

// numberLines prefixes each line with its number in the subtle colour.
func numberLines(lines []string, t ui.Theme) []string {
	width := len(strconv.Itoa(len(lines)))
	out := make([]string, len(lines))
	for i, l := range lines {
		n := strconv.Itoa(i + 1)
		out[i] = t.Subtle.Render(strings.Repeat(" ", width-len(n))+n) + "  " + sanitize.MarkControls(l)
	}
	return out
}

// folderPreview lists a folder's entries by name, without describing each,
// so even very large folders preview quickly.
func folderPreview(ctx context.Context, e entry, doc previewDoc, env previewEnv) (previewDoc, error) {
	t := env.theme
	f, err := openDir(e.path)
	if err != nil {
		return doc, err
	}
	defer f.Close()
	dirents, err := f.ReadDir(folderPreviewScan)
	if err != nil && !errors.Is(err, io.EOF) {
		return doc, err
	}
	more := len(dirents) == folderPreviewScan
	dirents = slices.DeleteFunc(dirents, func(d os.DirEntry) bool {
		return !env.hidden && strings.HasPrefix(d.Name(), ".")
	})
	slices.SortFunc(dirents, func(a, b os.DirEntry) int {
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		return compareNames(a.Name(), b.Name())
	})
	doc.meta = []string{itemCount(len(dirents), more)}
	if len(dirents) == 0 {
		doc.lines = []string{t.Muted.Render("Empty folder")}
		return doc, ctx.Err()
	}
	for i, d := range dirents {
		if i == maxFolderPreview {
			doc.lines = append(doc.lines, t.Subtle.Render("… "+itemCount(len(dirents)-i, more)+" more"))
			break
		}
		doc.lines = append(doc.lines, folderPreviewLine(t, d).Render())
	}
	return doc, ctx.Err()
}

func folderPreviewLine(t ui.Theme, d os.DirEntry) ui.Line {
	name, escapes := sanitize.NameSpans(d.Name())
	line := ui.Line{t.Glyph(ui.KindFile), ui.Space(1)}.Append(t.Escaped(name, escapes, t.Text)...)
	if d.IsDir() {
		line = ui.Line{t.Glyph(ui.KindFolder), ui.Space(1)}.Append(t.Escaped(name, escapes, t.Accent)...)
		line = append(line, ui.Span{Text: "/", Style: t.Accent})
	}
	if d.Type()&os.ModeSymlink != 0 {
		line = append(line, ui.Span{Text: " " + ui.GlyphSymlink, Style: t.Subtle})
	}
	return line
}

// isImage reports file names the image preview can decode.
func isImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}
