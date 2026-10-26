package snap

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Cell size of Rex on a Retina screen with Berkeley Mono, in pixels.
const (
	CellW = 14
	CellH = 29
)

// Font styles, an index into FontPaths.Primary.
const (
	styleRegular = iota
	styleBold
	styleItalic
	styleBoldItalic
)

// FontPaths are the font files a Renderer draws with: the terminal font in
// its four styles, then the fonts that supply what it lacks, in order (a
// .ttc collection contributes its first font).
type FontPaths struct {
	Primary  [4]string // regular, bold, italic, bold italic
	Fallback []string
}

// DefaultFontPaths are Berkeley Mono (Rex's font here) and the macOS fonts a
// terminal falls back to for symbols and CJK.
func DefaultFontPaths() FontPaths {
	home, _ := os.UserHomeDir()
	user := filepath.Join(home, "Library", "Fonts")
	return FontPaths{
		Primary: [4]string{
			filepath.Join(user, "BerkeleyMono-Regular.ttf"),
			filepath.Join(user, "BerkeleyMono-Bold.ttf"),
			filepath.Join(user, "BerkeleyMono-Italic.ttf"),
			filepath.Join(user, "BerkeleyMono-BoldItalic.ttf"),
		},
		Fallback: []string{
			"/System/Library/Fonts/Apple Symbols.ttf",
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/SFNSMono.ttf",
			filepath.Join(user, "HackNerdFont-Regular.ttf"),
			"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		},
	}
}

// typeface is one parsed font with the faces drawn from it, by pixel size.
type typeface struct {
	name  string
	font  *sfnt.Font
	buf   sfnt.Buffer
	faces map[float64]font.Face
}

func parseFont(path string) (*typeface, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f, err := c.Font(0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &typeface{name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), font: f, faces: map[float64]font.Face{}}, nil
}

func (t *typeface) has(r rune) bool {
	i, err := t.font.GlyphIndex(&t.buf, r)
	return err == nil && i != 0
}

// face returns the face at size pixels (a Face is not safe for concurrent
// use; a Renderer draws on one goroutine).
func (t *typeface) face(size float64) font.Face {
	if f, ok := t.faces[size]; ok {
		return f
	}
	f, err := opentype.NewFace(t.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	t.faces[size] = f
	return f
}

// advance is r's width at size pixels.
func (t *typeface) advance(r rune, size float64) float64 {
	i, err := t.font.GlyphIndex(&t.buf, r)
	if err != nil {
		return 0
	}
	a, err := t.font.GlyphAdvance(&t.buf, i, fixed.Int26_6(math.Round(size*64)), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(a) / 64
}

// fontSet is the terminal font and its fallbacks, sized to the cell.
type fontSet struct {
	primary  [4]*typeface
	paths    []string
	fallback []*typeface // parsed on first use; nil entries failed
	parsed   int         // how many of paths were tried
	size     float64     // pixel size: the primary font's advance is CellW
	baseline int         // from the cell's top
}

func newFontSet(p FontPaths) (*fontSet, error) {
	fs := &fontSet{paths: p.Fallback}
	for i, path := range p.Primary {
		t, err := parseFont(path)
		if err != nil {
			if i == styleRegular {
				return nil, fmt.Errorf("snap: the terminal font: %w", err)
			}
			t = fs.primary[styleRegular] // draw a missing style in regular
		}
		fs.primary[i] = t
	}
	reg := fs.primary[styleRegular]
	unit := reg.advance('0', 100) / 100
	if unit <= 0 {
		return nil, fmt.Errorf("snap: %s has no advance for 0", reg.name)
	}
	fs.size = CellW / unit
	m, err := reg.font.Metrics(&reg.buf, fixed.Int26_6(math.Round(fs.size*64)), font.HintingNone)
	if err != nil {
		return nil, err
	}
	ascent, descent := float64(m.Ascent)/64, float64(m.Descent)/64
	fs.baseline = int(math.Round((CellH-ascent-descent)/2 + ascent))
	return fs, nil
}

// find returns the font that draws r in style: the primary font, else the
// first fallback that has it, else nil.
func (fs *fontSet) find(r rune, style int) *typeface {
	if t := fs.primary[style]; t.has(r) {
		return t
	}
	if t := fs.primary[styleRegular]; t.has(r) {
		return t
	}
	for i := 0; ; i++ {
		if i == len(fs.fallback) {
			if fs.parsed == len(fs.paths) {
				return nil
			}
			t, _ := parseFont(fs.paths[fs.parsed])
			fs.parsed++
			fs.fallback = append(fs.fallback, t)
		}
		if t := fs.fallback[i]; t != nil && t.has(r) {
			return t
		}
	}
}

// glyph is a rasterized glyph: its coverage and where it goes relative to
// the left end of the baseline in its cell.
type glyph struct {
	mask *image.Alpha
	at   image.Point
}

type glyphKey struct {
	t    *typeface
	r    rune
	span int // cells the glyph is centred in
	room int // cells a fallback symbol may spread over
}

// capHeight is t's cap height at size pixels.
func (t *typeface) capHeight(size float64) float64 {
	m, err := t.font.Metrics(&t.buf, fixed.Int26_6(math.Round(size*64)), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(m.CapHeight) / 64
}

// matched reports whether r from t is a symbol drawn to the terminal font's
// measure: one a fallback font supplies (⌥ ⎇ ⏎ ⎋ ⚠), which a terminal scales
// to the primary font's cap height and centres on its capitals, so it
// neither shrinks to a speck nor sits off the baseline. Braille stays as it
// is, so spinner frames do not jump.
func (fs *fontSet) matched(t *typeface, r rune, span int) bool {
	return span == 1 && !slices.Contains(fs.primary[:], t) &&
		(unicode.Is(unicode.So, r) || unicode.Is(unicode.Sm, r)) && !unicode.Is(unicode.Braille, r)
}

// raster draws r from t centred in span cells, shrunk to fit if it is
// wider (a symbol from a proportional fallback font). A fallback symbol
// (matched) is set to the primary font's cap height and centred on it, and
// like a terminal draws it, it may spill right into half of a blank after
// it (room counts its own cells and that blank) before it shrinks, keeping
// a gap before the next word.
func (fs *fontSet) raster(t *typeface, r rune, span, room int) *glyph {
	size := fs.size
	matched := fs.matched(t, r, span)
	capH := fs.primary[styleRegular].capHeight(fs.size)
	if own := t.capHeight(size); matched && own > 0 && capH > own {
		size *= capH / own
	}
	box := float64(span * CellW)
	fit := box
	if matched {
		fit += float64((room-span)*CellW) / 2
	}
	adv := t.advance(r, size)
	if adv > fit*1.05 {
		size *= fit / adv
		adv = fit
	}
	face := t.face(size)
	if face == nil {
		return nil
	}
	x := (box - adv) / 2
	if matched {
		x = max(0, x)
	}
	if adv == 0 {
		x = box // a combining mark: it reaches back over the character before
	}
	dot := fixed.Point26_6{X: fixed.Int26_6(math.Round(x * 64))}
	dr, mask, mp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() {
		return &glyph{}
	}
	// The face reuses its mask: keep a copy.
	own := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	draw.Draw(own, own.Bounds(), mask, mp, draw.Src)
	at := dr.Min
	if matched && capH > 0 {
		// Centre the ink on the capitals' middle, above the baseline.
		at.Y = int(math.Round(-capH/2 - float64(dr.Dy())/2))
	}
	return &glyph{mask: own, at: at}
}
