package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoders for the preview
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi/kitty"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Image preview limits. Decoding is bounded by file size, pixel count and
// decoded size before any pixel is decoded; the thumbnail sent to the
// terminal is small.
const (
	maxImageBytes   = 32 << 20
	maxImagePixels  = 24_000_000
	maxDecodedBytes = 96 << 20
	thumbnailMax    = 960
	kittyChunk      = 4096
)

// decodeSlot lets one image decode at a time. Decoding can't be cancelled
// once it started, so moving quickly over a folder of large images would
// otherwise hold a decoded image in memory for every row passed.
var decodeSlot = make(chan struct{}, 1)

// picture is a decoded, downscaled image ready for the kitty graphics
// protocol. The terminal-side state (id, upload, placement) is managed by
// graphics.
type picture struct {
	data          string // base64 PNG thumbnail: data, never a path the terminal reads
	width, height int    // of the original, in pixels
	format        string

	id         int  // kitty image id, 0 until uploaded
	cols, rows int  // current placement size in cells
	ready      bool // the terminal confirmed the placement
}

func (p *picture) dimensions() string { return fmt.Sprintf("%d×%d", p.width, p.height) }

// loadPicture decodes an already opened regular file into a thumbnail. It
// waits for decodeSlot, and gives up (with ctx's error) when the preview is
// no longer wanted before its turn.
func loadPicture(ctx context.Context, f *os.File, size int64) (*picture, error) {
	if size > maxImageBytes {
		return nil, errors.New("larger than the 32 MB preview limit")
	}
	select {
	case decodeSlot <- struct{}{}:
		defer func() { <-decodeSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, errors.New("larger than the 32 MB preview limit")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a readable image")
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 || pixels > maxImagePixels {
		return nil, fmt.Errorf("%d×%d is over the 24 megapixel preview limit", cfg.Width, cfg.Height)
	}
	if float64(pixels)*bytesPerPixel(cfg.ColorModel) > maxDecodedBytes {
		return nil, fmt.Errorf("%d×%d at this colour depth is too large to preview", cfg.Width, cfg.Height)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a readable image")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scale := math.Min(1, thumbnailMax/float64(max(cfg.Width, cfg.Height)))
	w, h := max(1, int(float64(cfg.Width)*scale)), max(1, int(float64(cfg.Height)*scale))
	thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), src, src.Bounds(), draw.Src, nil)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumb); err != nil {
		return nil, err
	}
	return &picture{
		data:   base64.StdEncoding.EncodeToString(encoded.Bytes()),
		width:  cfg.Width,
		height: cfg.Height,
		format: strings.ToUpper(format),
	}, nil
}

// bytesPerPixel is how much memory a decoded pixel takes in the colour
// model a decoder reports: 16-bit images take twice what 8-bit ones do.
func bytesPerPixel(m color.Model) float64 {
	switch m {
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	case color.Gray16Model:
		return 2
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.YCbCrModel:
		return 3 // 4:4:4 at most
	case color.NYCbCrAModel:
		return 4
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 4 // RGBA, NRGBA, CMYK and anything unknown
}

// Kitty graphics protocol. Images are sent with the direct medium (t=d) in
// base64 chunks and shown through virtual placements (U=1): the image is
// drawn wherever placeholder cells appear in the text, so it scrolls,
// clips and redraws with the rest of the screen — no cursor races.
const (
	graphicsQueryID = 31
	// graphicsQuery asks whether direct-medium images work (a=q answers
	// "OK") and for the cell size in pixels (CSI 16 t).
	graphicsQuery = "\x1b_Gi=31,a=q,t=d,f=24,s=1,v=1;AAAA\x1b\\\x1b[16t"
	// maxUploaded is how many images stay uploaded for instant re-display.
	maxUploaded = 8
)

// graphics tracks what the terminal supports and which images it holds.
type graphics struct {
	answered, supported bool
	cellW, cellH        int
	nextID              int
	uploaded            []*picture // oldest first
}

// newGraphics starts with image ids from base, which should be random so
// ids are not predictable by content shown in other panes. Ids stay below
// 2^24 because the placeholder cells carry them as a 24-bit colour.
func newGraphics(base int) graphics {
	return graphics{cellW: 8, cellH: 16, nextID: 1 + base%(1<<23)}
}

// fit returns the placement size, in cells, of a picture shown in a box of
// w×h cells: aspect kept, never larger than the original pixels.
func (g *graphics) fit(p *picture, w, h int) (cols, rows int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	scale := math.Min(1, math.Min(float64(w*g.cellW)/float64(p.width), float64(h*g.cellH)/float64(p.height)))
	cols = int(math.Ceil(float64(p.width) * scale / float64(g.cellW)))
	rows = int(math.Ceil(float64(p.height) * scale / float64(g.cellH)))
	return max(1, min(cols, w)), max(1, min(rows, h))
}

// show returns the escape sequences that make p displayable at cols×rows:
// an upload the first time (evicting the oldest upload beyond maxUploaded),
// and a placement whenever the size changes.
func (g *graphics) show(p *picture, cols, rows int) string {
	if !g.supported || cols <= 0 || rows <= 0 {
		return ""
	}
	var b strings.Builder
	if p.id == 0 {
		p.id = g.nextID
		g.nextID = g.nextID%(1<<24-1) + 1
		b.WriteString(transmit(p))
		g.uploaded = append(g.uploaded, p)
		if len(g.uploaded) > maxUploaded {
			old := g.uploaded[0]
			g.uploaded = g.uploaded[1:]
			b.WriteString(deleteImage(old.id))
			old.id, old.cols, old.rows, old.ready = 0, 0, 0, false
		}
	}
	if p.cols != cols || p.rows != rows {
		p.cols, p.rows, p.ready = cols, rows, false
		fmt.Fprintf(&b, "\x1b_Ga=p,i=%d,p=1,U=1,c=%d,r=%d,q=0\x1b\\", p.id, cols, rows)
	}
	return b.String()
}

// placed records the terminal's answer to a placement; it reports whether
// the answer was for one of our images.
func (g *graphics) placed(id int, ok bool) bool {
	for _, p := range g.uploaded {
		if p.id == id {
			p.ready = ok
			return true
		}
	}
	return false
}

// clear deletes every uploaded image.
func (g *graphics) clear() string {
	var b strings.Builder
	for _, p := range g.uploaded {
		b.WriteString(deleteImage(p.id))
		p.id, p.cols, p.rows, p.ready = 0, 0, 0, false
	}
	g.uploaded = nil
	return b.String()
}

// transmit sends the PNG thumbnail in base64 chunks, quietly (q=2).
func transmit(p *picture) string {
	var b strings.Builder
	for start := 0; start < len(p.data); start += kittyChunk {
		end := min(start+kittyChunk, len(p.data))
		more := 0
		if end < len(p.data) {
			more = 1
		}
		if start == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;", p.id, more)
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d,q=2;", more)
		}
		b.WriteString(p.data[start:end])
		b.WriteString("\x1b\\")
	}
	return b.String()
}

func deleteImage(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// placeholders draws a placed picture as cols×rows placeholder cells: the
// foreground colour carries the image id, diacritics the row and column.
func placeholders(p *picture) []string {
	lines := make([]string, p.rows)
	colour := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (p.id>>16)&255, (p.id>>8)&255, p.id&255)
	for y := range lines {
		var b strings.Builder
		b.WriteString(colour)
		for x := range p.cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(y))
			b.WriteRune(kitty.Diacritic(x))
		}
		b.WriteString("\x1b[m")
		lines[y] = b.String()
	}
	return lines
}
