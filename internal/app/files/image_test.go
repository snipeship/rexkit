package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

func writeImage(t *testing.T, path string, w, h int) {
	t.Helper()
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	src.SetNRGBA(20, 20, color.NRGBA{255, 40, 120, 255})
	var b bytes.Buffer
	var err error
	if strings.HasSuffix(path, ".jpg") {
		err = jpeg.Encode(&b, src, nil)
	} else {
		err = png.Encode(&b, src)
	}
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, b.String())
}

func openPicture(ctx context.Context, t *testing.T, path string) (*picture, error) {
	t.Helper()
	f, err := openRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	return loadPicture(ctx, f, info.Size())
}

func TestPictureThumbnailAndTransmission(t *testing.T) {
	for _, name := range []string{"wide.png", "wide.jpg"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			writeImage(t, path, 1200, 600)
			p, err := openPicture(context.Background(), t, path)
			if err != nil {
				t.Fatal(err)
			}
			if p.width != 1200 || p.height != 600 || p.dimensions() != "1200×600" {
				t.Fatalf("lost the original size: %d×%d", p.width, p.height)
			}
			raw, err := base64.StdEncoding.DecodeString(p.data)
			if err != nil {
				t.Fatal(err)
			}
			thumb, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if b := thumb.Bounds(); b.Dx() != thumbnailMax || b.Dy() != thumbnailMax/2 {
				t.Fatalf("thumbnail %v is not bounded and aspect-preserving", b)
			}
			p.id = 123
			var assembled strings.Builder
			for chunk := range strings.SplitSeq(transmit(p), "\x1b\\") {
				if chunk == "" {
					continue
				}
				head, payload, ok := strings.Cut(chunk, ";")
				if !ok || len(payload) > kittyChunk || len(payload)%4 != 0 || !strings.Contains(head, "q=2") {
					t.Fatalf("bad chunk header %q (%d bytes)", head, len(payload))
				}
				assembled.WriteString(payload)
			}
			if assembled.String() != p.data {
				t.Fatal("chunks do not reassemble the image")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := openPicture(ctx, t, path); err == nil {
				t.Fatal("a cancelled decode carried on")
			}
		})
	}
}

func TestPictureLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	writeImage(t, path, 64, 64)
	data, _ := os.ReadFile(path)
	// Claim 1000000×64 pixels in the header (and fix its checksum): the size
	// check must reject it before decoding anything.
	binary.BigEndian.PutUint32(data[16:20], 1_000_000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	mustWrite(t, path, string(data))
	if _, err := openPicture(context.Background(), t, path); err == nil || !strings.Contains(err.Error(), "megapixel") {
		t.Errorf("oversized image: %v", err)
	}
	mustWrite(t, path, "not a PNG")
	if _, err := openPicture(context.Background(), t, path); err == nil {
		t.Error("malformed image accepted")
	}
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, maxImageBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := openPicture(context.Background(), t, big); err == nil || !strings.Contains(err.Error(), "32 MB") {
		t.Errorf("oversized file: %v", err)
	}
}

func TestGraphicsFitAndPlacement(t *testing.T) {
	g := newGraphics(41)
	g.cellW, g.cellH = 10, 20
	p := &picture{width: 400, height: 300, data: "AAAA"}
	tests := []struct{ w, h, cols, rows int }{
		{80, 40, 40, 15}, // natural size fits: never upscaled
		{20, 40, 20, 8},  // width-bound
		{80, 5, 14, 5},   // height-bound
		{0, 10, 0, 0},    // no room
	}
	for _, tt := range tests {
		if c, r := g.fit(p, tt.w, tt.h); c != tt.cols || r != tt.rows {
			t.Errorf("fit(%d×%d) = %d×%d, want %d×%d", tt.w, tt.h, c, r, tt.cols, tt.rows)
		}
	}

	if g.show(p, 10, 5) != "" {
		t.Error("placed an image before the terminal said it supports them")
	}
	g.supported = true
	first := g.show(p, 10, 5)
	if p.id != 42 || !strings.Contains(first, "a=t,f=100,t=d,i=42") || !strings.Contains(first, "a=p,i=42,p=1,U=1,c=10,r=5") {
		t.Fatalf("first show %q", first)
	}
	if again := g.show(p, 10, 5); again != "" {
		t.Errorf("unchanged placement re-sent: %q", again)
	}
	if resized := g.show(p, 12, 6); strings.Contains(resized, "a=t") || !strings.Contains(resized, "c=12,r=6") {
		t.Errorf("resize re-uploaded or did not re-place: %q", resized)
	}
	if !g.placed(42, true) || !p.ready {
		t.Fatal("placement answer not recorded")
	}
	for _, line := range placeholders(p) {
		if ansi.StringWidth(line) != p.cols {
			t.Fatalf("placeholder row is %d cells, want %d", ansi.StringWidth(line), p.cols)
		}
	}

	var pics []*picture
	for range maxUploaded {
		q := &picture{width: 10, height: 10, data: "AAAA"}
		g.show(q, 1, 1)
		pics = append(pics, q)
	}
	if p.id != 0 || len(g.uploaded) != maxUploaded {
		t.Errorf("oldest upload not evicted: id %d, %d uploaded", p.id, len(g.uploaded))
	}
	if clear := g.clear(); strings.Count(clear, "a=d,d=I") != maxUploaded || pics[0].id != 0 {
		t.Errorf("clear %q", clear)
	}
}

// pngHeader is a PNG that only declares its size and format (IHDR): enough
// for DecodeConfig, never decodable.
func pngHeader(w, h int, depth, colourType byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8], ihdr[9] = depth, colourType
	chunk := append([]byte("IHDR"), ihdr...)
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// TestPictureDecodedSizeLimit: a small file can declare an image that
// decodes to hundreds of MB (16 bits per channel); it is refused from its
// header, before any pixel is decoded.
func TestPictureDecodedSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep.png")
	mustWrite(t, path, string(pngHeader(4898, 4898, 16, 6))) // RGBA64: 192 MB decoded
	if _, err := openPicture(context.Background(), t, path); err == nil || !strings.Contains(err.Error(), "too large to preview") {
		t.Errorf("16-bit 24 MP image: %v", err)
	}
	mustWrite(t, path, string(pngHeader(4898, 4898, 8, 6))) // RGBA: 96 MB, within the limit
	if _, err := openPicture(context.Background(), t, path); err == nil || strings.Contains(err.Error(), "too large") {
		t.Errorf("8-bit 24 MP image refused by size: %v", err)
	}
}

// TestPictureDecodesOneAtATime: a preview that is no longer wanted while
// another image decodes gives up without decoding, and its failure is not
// cached as the image's preview.
func TestPictureDecodesOneAtATime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	writeImage(t, path, 64, 64)
	decodeSlot <- struct{}{} // another decode is running
	f, err := openRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := loadPicture(ctx, f, 0); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("decoded while another decode ran: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled while waiting: %v", err)
	}
	<-decodeSlot
	if _, err := openPicture(context.Background(), t, path); err != nil {
		t.Errorf("after the slot freed: %v", err)
	}

	e, _ := statEntry(path, "a.png")
	if _, err := loadPreview(ctx, e, previewEnv{theme: ui.NewTheme(true)}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled image preview returned %v, want the cancellation (so it is not cached)", err)
	}
}

// TestPictureStandIn: until the terminal draws the image (and in one
// without kitty graphics), the well says what it is and how to open it.
func TestPictureStandIn(t *testing.T) {
	home, root := fixtureTree(t)
	writeImage(t, filepath.Join(root, "shot.png"), 64, 40)
	e := newEnv(t, home, root, 140, 30)
	e.selectPath(t, filepath.Join(root, "shot.png"))
	text := strings.Join(e.screen(), "\n")
	for _, want := range []string{"▣ PNG 64×40", "ctrl x▌ open"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the image preview:\n%s", want, text)
		}
	}
	e.m.gfx.answered = true
	e.m.refreshPicture()
	if text := strings.Join(e.screen(), "\n"); !strings.Contains(text, "▣ PNG 64×40") {
		t.Errorf("no stand-in without kitty graphics:\n%s", text)
	}
}
