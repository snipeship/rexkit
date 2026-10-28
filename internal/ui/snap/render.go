// Package snap renders terminal screens to PNG images, so a tool's views can
// be looked at (and judged) as pictures: `rexkit snap` draws a tool in a
// scripted state the way Rex would show it.
//
// A screen is what a Bubble Tea view's Content is: lines of text with SGR
// styling. It is parsed into cells with ultraviolet, exactly as Bubble Tea's
// renderer would, then drawn at Rex's Retina cell size (CellW×CellH) with
// Berkeley Mono and the fallback fonts a terminal uses. Box drawing, block
// elements and braille are drawn as shapes, as Ghostty does, so lines join.
package snap

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"slices"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Options configure a Renderer.
type Options struct {
	// Palette gives cells without a colour their colours, and the indexed
	// colours their values. The zero Palette is DefaultPalette(true).
	Palette Palette
	// Half draws at half size (7×14.5 pixels a cell), for quick previews.
	Half bool
	// Fonts are the font files; the zero value is DefaultFontPaths().
	Fonts FontPaths
}

// Renderer draws screens. It caches glyphs between screens; it is not safe
// for concurrent use.
type Renderer struct {
	opts    Options
	fonts   *fontSet
	glyphs  map[glyphKey]*glyph
	missing map[rune]bool
	fell    map[rune]string // runes drawn from a fallback font, and its name
	// sansFace is the face of a Window's headers, loaded on first use.
	sansFace *typeface
}

// NewRenderer loads the fonts. It fails only when the terminal font (the
// regular style) cannot be read.
func NewRenderer(opts Options) (*Renderer, error) {
	if opts.Palette == (Palette{}) {
		opts.Palette = DefaultPalette(true)
	}
	if opts.Fonts.Primary[styleRegular] == "" {
		opts.Fonts = DefaultFontPaths()
	}
	fs, err := newFontSet(opts.Fonts)
	if err != nil {
		return nil, err
	}
	return &Renderer{opts: opts, fonts: fs, glyphs: map[glyphKey]*glyph{}, missing: map[rune]bool{}, fell: map[rune]string{}}, nil
}

// Missing returns the runes no font could draw (drawn as a hollow box), in
// order.
func (r *Renderer) Missing() []rune {
	out := make([]rune, 0, len(r.missing))
	for c := range r.missing {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Fallbacks returns the runes drawn from a fallback font, with the font's
// name.
func (r *Renderer) Fallbacks() map[rune]string { return r.fell }

// Bar is a terminal's bar cursor: where it is (in cells) and its colour
// (nil for the palette's cursor colour).
type Bar struct {
	X, Y  int
	Color color.Color
}

// barWidth is the bar cursor's width in pixels at Retina size.
const barWidth = 3

// RenderBar is Render with a bar cursor drawn at bar's cell (nil for none),
// as the terminal draws the cursor a tool places (tea.View.Cursor).
func (r *Renderer) RenderBar(content string, w, h int, bar *Bar) *image.RGBA {
	half := r.opts.Half
	r.opts.Half = false
	img := r.Render(content, w, h)
	r.opts.Half = half
	if bar != nil && bar.X >= 0 && bar.X < w && bar.Y >= 0 && bar.Y < h {
		c := r.resolve(bar.Color, r.opts.Palette.Cursor)
		box := image.Rect(bar.X*CellW, bar.Y*CellH+2, bar.X*CellW+barWidth, (bar.Y+1)*CellH-2)
		draw.Draw(img, box, image.NewUniform(c), image.Point{}, draw.Src)
	}
	if half {
		return halfImage(img)
	}
	return img
}

// Render draws a w×h-cell screen. Lines beyond h and cells beyond w are cut.
func (r *Renderer) Render(content string, w, h int) *image.RGBA {
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(content).Draw(buf, buf.Bounds())

	img := image.NewRGBA(image.Rect(0, 0, w*CellW, h*CellH))
	draw.Draw(img, img.Bounds(), image.NewUniform(r.opts.Palette.Bg), image.Point{}, draw.Src)
	// Backgrounds first, then glyphs, which may overhang into the next cell.
	type cellAt struct {
		c    *uv.Cell
		box  image.Rectangle
		room int // cells the glyph may take: its own, and a blank after it
		fg   color.RGBA
	}
	cells := make([]cellAt, 0, w*h)
	for y := range h {
		for x := 0; x < w; x++ {
			c := buf.CellAt(x, y)
			if c == nil || c.IsZero() {
				continue // the second half of a wide character
			}
			span := max(1, c.Width)
			box := image.Rect(x*CellW, y*CellH, min(x+span, w)*CellW, (y+1)*CellH)
			fg, bg := r.colours(c.Style)
			if bg != r.opts.Palette.Bg {
				draw.Draw(img, box, image.NewUniform(bg), image.Point{}, draw.Src)
			}
			room := span
			if next := buf.CellAt(x+span, y); next != nil && (next.Content == "" || next.Content == " ") {
				room++
			}
			cells = append(cells, cellAt{c, box, room, fg})
		}
	}
	for _, at := range cells {
		r.drawCell(img, at.c, at.box, at.room, at.fg)
	}
	if r.opts.Half {
		return halfImage(img)
	}
	return img
}

// padding is what Rex stretches into a surface's padding, as it (and
// Ghostty) does: the background of each row's end cells into the sides
// (ends), and of the last row's cells into the bottom (bottom, a colour
// per column).
type padding struct {
	ends   [][2]color.RGBA
	bottom []color.RGBA
}

// padding is the padding around content drawn as w×h cells.
func (r *Renderer) padding(content string, w, h int) padding {
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = ansi.GraphemeWidth
	uv.NewStyledString(content).Draw(buf, buf.Bounds())
	bg := func(x, y int) color.RGBA {
		if c := buf.CellAt(x, y); c != nil {
			_, b := r.colours(c.Style)
			return b
		}
		return r.opts.Palette.Bg
	}
	p := padding{ends: make([][2]color.RGBA, max(h, 1)), bottom: make([]color.RGBA, max(w, 1))}
	for y := range p.ends {
		p.ends[y] = [2]color.RGBA{bg(0, y), bg(w-1, y)}
	}
	for x := range p.bottom {
		p.bottom[x] = bg(x, h-1)
	}
	return p
}

// colours resolves a cell's foreground and background, with reverse, faint
// and conceal applied.
func (r *Renderer) colours(s uv.Style) (fg, bg color.RGBA) {
	pal := r.opts.Palette
	fg, bg = r.resolve(s.Fg, pal.Fg), r.resolve(s.Bg, pal.Bg)
	if s.Attrs&uv.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if s.Attrs&uv.AttrFaint != 0 {
		fg = mix(fg, bg, 0.5)
	}
	if s.Attrs&uv.AttrConceal != 0 {
		fg = bg
	}
	return fg, bg
}

// resolve turns a cell colour into RGB: the palette's for indexed colours,
// def for none.
func (r *Renderer) resolve(c color.Color, def color.RGBA) color.RGBA {
	switch c := c.(type) {
	case nil:
		return def
	case ansi.BasicColor:
		return r.opts.Palette.Indexed[c&15]
	case ansi.IndexedColor:
		return r.opts.Palette.Indexed[c]
	}
	cr, cg, cb, _ := c.RGBA()
	return color.RGBA{R: uint8(cr >> 8), G: uint8(cg >> 8), B: uint8(cb >> 8), A: 255}
}

// drawCell draws a cell's character and its lines over its background. The
// character may take room cells.
func (r *Renderer) drawCell(img *image.RGBA, c *uv.Cell, box image.Rectangle, room int, fg color.RGBA) {
	st := c.Style
	text := c.Content
	if strings.IndexByte(text, 0x1b) >= 0 {
		text = ansi.Strip(text) // image protocol sequences riding in the cell
	}
	thin := max(1, int(math.Round(CellW/7.0)))
	p := pen{img: img, box: box, fg: fg, thin: thin}
	runes := []rune(text)
	switch {
	case len(runes) == 0, runes[0] == ' ', runes[0] == 0x10eeee: // blank, or a kitty image placeholder
	case drawSprite(p, runes[0]):
	default:
		for _, ch := range runes {
			if ch != 0x200d && ch != 0xfe0e && ch != 0xfe0f { // joiners and variation selectors
				r.drawGlyph(img, ch, st.Attrs, box, room, fg)
			}
		}
	}
	if st.Underline != uv.UnderlineNone {
		ul := fg
		if st.UnderlineColor != nil {
			ul = r.resolve(st.UnderlineColor, fg)
		}
		y := r.fonts.baseline + max(1, thin)
		pen{img: img, box: box, fg: ul}.fill(image.Rect(0, y, box.Dx(), y+thin))
		if st.Underline == uv.UnderlineDouble {
			pen{img: img, box: box, fg: ul}.fill(image.Rect(0, y+2*thin, box.Dx(), y+3*thin))
		}
	}
	if st.Attrs&uv.AttrStrikethrough != 0 {
		y := CellH/2 + thin/2
		p.fill(image.Rect(0, y, box.Dx(), y+thin))
	}
}

// drawGlyph draws ch from the font in the cell's style, centred in box and
// at most room cells wide.
func (r *Renderer) drawGlyph(img *image.RGBA, ch rune, attrs uint8, box image.Rectangle, room int, fg color.RGBA) {
	style := styleRegular
	if attrs&uv.AttrBold != 0 {
		style = styleBold
	}
	if attrs&uv.AttrItalic != 0 {
		style += styleItalic
	}
	t := r.fonts.find(ch, style)
	if t == nil {
		r.missing[ch] = true
		p := pen{img: img, box: box, fg: fg}
		in := image.Rect(2, 6, box.Dx()-2, CellH-5)
		p.fill(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+1))
		p.fill(image.Rect(in.Min.X, in.Max.Y-1, in.Max.X, in.Max.Y))
		p.fill(image.Rect(in.Min.X, in.Min.Y, in.Min.X+1, in.Max.Y))
		p.fill(image.Rect(in.Max.X-1, in.Min.Y, in.Max.X, in.Max.Y))
		return
	}
	if !slices.Contains(r.fonts.primary[:], t) {
		r.fell[ch] = t.name
	}
	key := glyphKey{t: t, r: ch, span: box.Dx() / CellW, room: room}
	g, ok := r.glyphs[key]
	if !ok {
		g = r.fonts.raster(t, ch, key.span, key.room)
		r.glyphs[key] = g
	}
	if g == nil || g.mask == nil {
		return
	}
	at := box.Min.Add(image.Pt(0, r.fonts.baseline)).Add(g.at)
	draw.DrawMask(img, g.mask.Bounds().Add(at), image.NewUniform(fg), image.Point{}, g.mask, image.Point{}, draw.Over)
}

// halfImage scales img down by two, averaging each 2×2 block.
func halfImage(img *image.RGBA) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()/2, b.Dy()/2))
	for y := range out.Rect.Dy() {
		for x := range out.Rect.Dx() {
			var sum [4]int
			for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				i := img.PixOffset(2*x+d[0], 2*y+d[1])
				for k := range 4 {
					sum[k] += int(img.Pix[i+k])
				}
			}
			o := out.PixOffset(x, y)
			for k := range 4 {
				out.Pix[o+k] = uint8((sum[k] + 2) / 4)
			}
		}
	}
	return out
}

// mix blends a towards b by f.
func mix(a, b color.RGBA, f float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-f) + float64(y)*f)) }
	return color.RGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 255}
}

// Encode writes img as a PNG, favouring speed over size.
func Encode(w io.Writer, img image.Image) error {
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, img)
}

// WriteFile writes img as a PNG file.
func WriteFile(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
