package snap

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"rexkit/internal/panes"
	"rexkit/internal/rex"
	"rexkit/internal/rextheme"
)

// Window is a made-up Rex window for a snapshot, drawn as Rex (build 1541)
// draws one (internal/panes geometry): the panes tiled on their surfaces,
// each with its header, and floats above them with Rex's frame, header
// and shadow. It is a mock: no tab bar, no pane buttons.
type Window struct {
	// Layout is the layout area the panes and floats are in.
	Layout panes.Layout
	Panes  []rex.PaneScreen
	Floats []Float
}

// Float is a float of a Window: where it is in the layout area (pixels),
// its title, and its cells.
type Float struct {
	At      image.Rectangle
	Title   string
	Content string
	Cursor  *Bar
}

// The header's title, in pixels on a Retina screen.
const (
	headerTextSize = 26
	headerMid      = 36 // the title's middle, below the surface's top
	headerIconX    = 26 // the prompt icon, from the surface's left
	headerTitleX   = 60 // the title
)

// Shadow of a float: offset down, blurred, at a theme's strength.
const (
	floatShadowY     = 10
	floatShadowBlur  = 28
	floatShadowDark  = 0.55
	floatShadowLight = 0.16
)

// windowColours are what a Window is drawn in, from the Rex theme: the
// window behind its panes (canvas), the panes' and floats' surface, their
// header's text, a float's hairline and its shadow.
type windowColours struct {
	window, surface, title, context, stroke, shadow color.RGBA
	peak                                            float64
}

func coloursOf(t rextheme.Theme) windowColours {
	c := windowColours{
		window:  rgba(t.UI.Canvas),
		surface: rgba(t.UI.Surface),
		title:   rgba(t.UI.Content),
		context: rgba(t.UI.ContentSecondary),
		stroke:  rgba(t.UI.StrokeSubtle),
		peak:    floatShadowDark,
	}
	if !t.Dark {
		c.shadow, c.peak = color.RGBA{R: 40, G: 48, B: 64, A: 255}, floatShadowLight
	}
	c.shadow.A = 255
	return c
}

// SurfaceOf is the colour Rex draws a pane's or float's default
// background in, in a snapshot: theme's surface.
func SurfaceOf(t rextheme.Theme) color.RGBA { return rgba(t.UI.Surface) }

// Size is the window's size in pixels.
func (w Window) Size() image.Point {
	return image.Pt(w.Layout.W+2*panes.Margin, panes.TabBar+w.Layout.H+panes.Margin)
}

// origin is where the layout area starts in the window.
func origin() image.Point { return image.Pt(panes.Margin, panes.TabBar) }

// Window draws w in theme: its panes, then its floats in order. Cells are
// drawn on the surface, as Rex draws a pane's default background.
func (r *Renderer) Window(theme rextheme.Theme, w Window) *image.RGBA {
	half, pal := r.opts.Half, r.opts.Palette
	r.opts.Half = false
	defer func() { r.opts.Half, r.opts.Palette = half, pal }()
	c := coloursOf(theme)
	r.opts.Palette.Bg = c.surface

	img := image.NewRGBA(image.Rectangle{Max: w.Size()})
	draw.Draw(img, img.Bounds(), image.NewUniform(c.window), image.Point{}, draw.Src)
	o := origin()
	for _, p := range w.Panes {
		at := w.Layout.Pane(p.Rect).Add(o)
		roundRect(img, at, panes.Radius, c.surface, 1)
		r.header(img, at, c, p.Program, p.Title)
		cols, rows, cells := panes.Content(at, CellW, CellH)
		cols, rows = min(cols, p.Size.Columns), min(rows, p.Size.Rows)
		r.paste(img, r.Render(p.Screen, cols, rows), r.padding(p.Screen, cols, rows), cells, at)
	}
	for _, f := range w.Floats {
		at := f.At.Add(o)
		shadow(img, at.Add(image.Pt(0, floatShadowY)), panes.Radius, floatShadowBlur, c.shadow, c.peak)
		roundRect(img, at, panes.Radius, c.surface, 1)
		ring(img, at, panes.Radius, c.stroke)
		r.header(img, at, c, f.Title, "")
		cols, rows, cells := panes.Content(at, CellW, CellH)
		r.paste(img, r.RenderBar(f.Content, cols, rows, f.Cursor), r.padding(f.Content, cols, rows), cells, at)
	}
	if half {
		return halfImage(img)
	}
	return img
}

// paste draws cells at at, inside the rounded surface clip, and fills the
// padding around them as Rex does (pad): a row's sides take the background
// of the cell at that end of the row, and the bottom that of the last row's
// cell above it. A band that runs to a row's ends runs to the surface's
// edges, and the last row's backgrounds run down to its bottom.
func (r *Renderer) paste(img, cells *image.RGBA, pad padding, at image.Point, clip image.Rectangle) {
	b := cells.Bounds()
	last := len(pad.ends) - 1
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		cy := y - at.Y
		if cy < 0 {
			continue
		}
		row := min(cy/CellH, last)
		for x := clip.Min.X; x < clip.Max.X; x++ {
			var c color.RGBA
			switch cx := x - at.X; {
			case cx < 0:
				c = pad.ends[row][0]
			case cx >= b.Dx():
				c = pad.ends[row][1]
			case cy >= b.Dy():
				c = pad.bottom[min(cx/CellW, len(pad.bottom)-1)]
			default:
				c = cells.RGBAAt(cx, cy)
			}
			cover := math.Max(0, math.Min(1, 0.5-roundDist(float64(x)+0.5, float64(y)+0.5, clip, panes.Radius)))
			blendAt(img, x, y, c, cover)
		}
	}
}

// header writes "›_ title context" across the top of a surface at at: the
// title in the content colour, the context (a shell's folder) in the
// secondary one.
func (r *Renderer) header(img *image.RGBA, at image.Rectangle, c windowColours, title, context string) {
	t := r.sans()
	face := t.face(headerTextSize)
	if face == nil {
		return
	}
	base := at.Min.Y + headerMid + int(math.Round(t.capHeight(headerTextSize)/2))
	d := font.Drawer{Dst: img, Src: image.NewUniform(c.title), Face: face}
	d.Dot = fixed.P(at.Min.X+headerIconX, base)
	d.DrawString("›_")
	d.Dot = fixed.P(at.Min.X+headerTitleX, base)
	d.DrawString(title)
	if context != "" {
		d.Src = image.NewUniform(c.context)
		d.DrawString(" " + context)
	}
}

// sansPaths are the faces a header may be set in, the first one found:
// the system's UI font, then common sans faces.
var sansPaths = []string{
	"/System/Library/Fonts/SFNS.ttf",
	"/System/Library/Fonts/Helvetica.ttc",
	"/System/Library/Fonts/Supplemental/Arial.ttf",
}

// sans is the header's face: the first of sansPaths that parses, else the
// terminal font.
func (r *Renderer) sans() *typeface {
	if r.sansFace == nil {
		r.sansFace = r.fonts.primary[styleRegular]
		for _, p := range sansPaths {
			if t, err := parseFont(p); err == nil {
				r.sansFace = t
				break
			}
		}
	}
	return r.sansFace
}

// roundDist is the signed distance from (x, y) to a rounded rectangle:
// negative inside.
func roundDist(x, y float64, r image.Rectangle, rad float64) float64 {
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	hx, hy := float64(r.Dx())/2-rad, float64(r.Dy())/2-rad
	qx, qy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
	out := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	return out + math.Min(math.Max(qx, qy), 0) - rad
}

// blendAt mixes c into img's pixel (x, y) by a.
func blendAt(img *image.RGBA, x, y int, c color.RGBA, a float64) {
	if a <= 0 || !(image.Point{x, y}).In(img.Rect) {
		return
	}
	i := img.PixOffset(x, y)
	under := color.RGBA{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2], A: 255}
	m := mix(under, c, math.Min(a, 1))
	img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = m.R, m.G, m.B, 255
}

// roundRect fills a rounded rectangle, its edge anti-aliased.
func roundRect(img *image.RGBA, r image.Rectangle, rad int, c color.RGBA, a float64) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := roundDist(float64(x)+0.5, float64(y)+0.5, r, float64(rad))
			blendAt(img, x, y, c, a*math.Max(0, math.Min(1, 0.5-d)))
		}
	}
}

// ring draws a rounded rectangle's one-pixel hairline, at half strength.
func ring(img *image.RGBA, r image.Rectangle, rad int, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := roundDist(float64(x)+0.5, float64(y)+0.5, r, float64(rad))
			if d > -1.5 && d < 0.5 {
				blendAt(img, x, y, c, 0.5*math.Max(0, 1-math.Abs(d+0.5)))
			}
		}
	}
}

// shadow darkens around a rounded rectangle towards c, peak at its edge and
// fading out over blur pixels.
func shadow(img *image.RGBA, r image.Rectangle, rad, blur int, c color.RGBA, peak float64) {
	out := r.Inset(-2 * blur)
	for y := out.Min.Y; y < out.Max.Y; y++ {
		for x := out.Min.X; x < out.Max.X; x++ {
			d := roundDist(float64(x)+0.5, float64(y)+0.5, r, float64(rad))
			s := math.Max(d, 0) / float64(blur)
			blendAt(img, x, y, c, peak*math.Exp(-2*s*s))
		}
	}
}
