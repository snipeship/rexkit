package snap

import (
	"image"
	"image/color"
	"math"
)

// Ghostty draws box drawing, block elements and braille itself instead of
// taking them from the font, so that lines meet seamlessly from cell to
// cell. drawSprite does the same; it reports false for any other rune.

// Arm weights of a box-drawing character.
const (
	armNone = iota
	armLight
	armHeavy
	armDouble
)

// boxArms are the arms of U+2500–257F as up, right, down, left weights
// (digits of armNone…armDouble). Dashed lines share the arms of their solid
// line; "a" marks the rounded corners and "x" the diagonals.
var boxArms = [128]string{
	"0101", "0202", "1010", "2020", "0101", "0202", "1010", "2020", // ─━│┃┄┅┆┇
	"0101", "0202", "1010", "2020", "0110", "0210", "0120", "0220", // ┈┉┊┋┌┍┎┏
	"0011", "0012", "0021", "0022", "1100", "1200", "2100", "2200", // ┐┑┒┓└┕┖┗
	"1001", "1002", "2001", "2002", "1110", "1210", "2110", "1120", // ┘┙┚┛├┝┞┟
	"2120", "2210", "1220", "2220", "1011", "1012", "2011", "1021", // ┠┡┢┣┤┥┦┧
	"2021", "2012", "1022", "2022", "0111", "0112", "0211", "0212", // ┨┩┪┫┬┭┮┯
	"0121", "0122", "0221", "0222", "1101", "1102", "1201", "1202", // ┰┱┲┳┴┵┶┷
	"2101", "2102", "2201", "2202", "1111", "1112", "1211", "1212", // ┸┹┺┻┼┽┾┿
	"2111", "1121", "2121", "2112", "2211", "1122", "1221", "2212", // ╀╁╂╃╄╅╆╇
	"1222", "2122", "2221", "2222", "0101", "0202", "1010", "2020", // ╈╉╊╋╌╍╎╏
	"0303", "3030", "0310", "0130", "0330", "0013", "0031", "0033", // ═║╒╓╔╕╖╗
	"1300", "3100", "3300", "1003", "3001", "3003", "1310", "3130", // ╘╙╚╛╜╝╞╟
	"3330", "1013", "3031", "3033", "0313", "0131", "0333", "1303", // ╠╡╢╣╤╥╦╧
	"3101", "3303", "1313", "3131", "3333", "a", "a", "a", // ╨╩╪╫╬╭╮╯
	"a", "x", "x", "x", "0001", "1000", "0100", "0010", // ╰╱╲╳╴╵╶╷
	"0002", "2000", "0200", "0020", "0201", "1020", "0102", "2010", // ╸╹╺╻╼╽╾╿
}

// boxDashes is the number of dashes of the dashed lines.
var boxDashes = map[rune]int{
	'┄': 3, '┅': 3, '┆': 3, '┇': 3,
	'┈': 4, '┉': 4, '┊': 4, '┋': 4,
	'╌': 2, '╍': 2, '╎': 2, '╏': 2,
}

// pen paints coverage in one colour onto an image, inside one cell box.
type pen struct {
	img  *image.RGBA
	box  image.Rectangle // the cell(s), in pixels
	fg   color.RGBA
	thin int // light line thickness
}

// fill paints rectangle r (relative to the box) at full coverage.
func (p pen) fill(r image.Rectangle) { p.blend(r, 1) }

// blend paints r (relative to the box) with coverage a.
func (p pen) blend(r image.Rectangle, a float64) {
	r = r.Add(p.box.Min).Intersect(p.box)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p.dot(x, y, a)
		}
	}
}

// dot blends fg into one pixel with coverage a.
func (p pen) dot(x, y int, a float64) {
	if a <= 0 {
		return
	}
	i := p.img.PixOffset(x, y)
	px := p.img.Pix[i : i+4 : i+4]
	if a >= 1 {
		px[0], px[1], px[2], px[3] = p.fg.R, p.fg.G, p.fg.B, 255
		return
	}
	mix := func(b, f uint8) uint8 { return uint8(math.Round(float64(b)*(1-a) + float64(f)*a)) }
	px[0], px[1], px[2], px[3] = mix(px[0], p.fg.R), mix(px[1], p.fg.G), mix(px[2], p.fg.B), 255
}

// shape paints the pixels of the box by coverage(x, y) of a point in box
// pixels, supersampled 4×4.
func (p pen) shape(coverage func(x, y float64) bool) {
	w, h := p.box.Dx(), p.box.Dy()
	for y := range h {
		for x := range w {
			n := 0
			for sy := range 4 {
				for sx := range 4 {
					if coverage(float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4) {
						n++
					}
				}
			}
			p.dot(p.box.Min.X+x, p.box.Min.Y+y, float64(n)/16)
		}
	}
}

// drawSprite draws r if it is a box-drawing, block or braille character, or
// a symbol none of the fonts has.
func drawSprite(p pen, r rune) bool {
	switch {
	case r == '⏺': // Claude Code's bullet; Rex falls back to a font we lack
		p.disc()
	case r >= 0x2500 && r <= 0x257f:
		p.boxDrawing(r)
	case r >= 0x2580 && r <= 0x259f:
		p.block(r)
	case r >= 0x2800 && r <= 0x28ff:
		p.braille(r)
	case r >= 0x1fb00 && r <= 0x1fb3b:
		p.sextant(r)
	case r >= wedgeFirst && r <= wedgeLast:
		p.wedge(r)
	default:
		return false
	}
	return true
}

// disc draws a filled circle a little over half the cell wide, centred on
// the text's middle.
func (p pen) disc() {
	w, h := float64(p.box.Dx()), float64(p.box.Dy())
	cx, cy, r := w/2, h*0.55, w*0.32
	p.shape(func(x, y float64) bool { return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r })
}

func (p pen) boxDrawing(r rune) {
	spec := boxArms[r-0x2500]
	switch spec {
	case "a":
		p.arc(r)
		return
	case "x":
		p.diagonal(r)
		return
	}
	var arms [4]int
	for i := range arms {
		arms[i] = int(spec[i] - '0')
	}
	if n := boxDashes[r]; n > 0 {
		p.dashes(arms, n)
		return
	}
	for dir := range arms {
		if arms[dir] != armNone {
			p.arm(arms, dir)
		}
	}
}

// strokes are the bands an arm of weight wt covers across a line of n
// pixels: one band for light and heavy, two for double.
func (p pen) strokes(n, wt int) [][2]int {
	t := p.thin
	switch wt {
	case armHeavy:
		t *= 2
	case armDouble:
		s := (n - 3*t) / 2
		return [][2]int{{s, s + t}, {s + 2*t, s + 3*t}}
	}
	s := (n - t) / 2
	return [][2]int{{s, s + t}}
}

// Directions of an arm, the index into its weights.
const (
	dirUp = iota
	dirRight
	dirDown
	dirLeft
)

// arm draws the arm dir of a character with arms: from the cell's edge to
// the middle, far enough to join the arms across it.
func (p pen) arm(arms [4]int, dir int) {
	w, h := p.box.Dx(), p.box.Dy()
	horizontal := dir == dirRight || dir == dirLeft
	forward := dir == dirRight || dir == dirDown // runs to the far edge
	main, across := h, w                         // a vertical arm runs along h
	low, high, opp := dirLeft, dirRight, dirUp   // the arms beside it, and opposite
	if horizontal {
		main, across = w, h
		low, high = dirUp, dirDown
		opp = dirLeft
	}
	if dir == dirUp {
		opp = dirDown
	} else if dir == dirLeft {
		opp = dirRight
	}
	// Bands of the arms beside this one, along this arm.
	side := func(d int) [][2]int {
		if arms[d] == armNone {
			return nil
		}
		return p.strokes(main, arms[d])
	}
	mid := main / 2
	var rects [][4]int // [from, to) along the arm, [from, to) across it
	bands := p.strokes(across, arms[dir])
	if arms[dir] != armDouble {
		from, to := 0, 0
		beside := append(side(low), side(high)...)
		if len(beside) == 0 {
			own := p.strokes(main, arms[dir])[0]
			from, to = own[0], own[1]
		} else {
			from, to = main, 0
			for _, b := range beside {
				from, to = min(from, b[0]), max(to, b[1])
			}
		}
		if forward {
			rects = append(rects, [4]int{from, main, bands[0][0], bands[0][1]})
		} else {
			rects = append(rects, [4]int{0, to, bands[0][0], bands[0][1]})
		}
	} else {
		// Each band of a double arm meets the nearest band of the arm on
		// its side, runs on to the opposite arm, or turns into the far band
		// of the arm on the other side.
		for i, b := range bands {
			near, far := low, high
			if i == 1 {
				near, far = high, low
			}
			stop := mid
			switch {
			case arms[near] != armNone:
				s := side(near)
				if forward {
					stop = s[len(s)-1][0]
				} else {
					stop = s[0][1]
				}
			case arms[opp] != armNone:
			case arms[far] != armNone:
				s := side(far)
				if forward {
					stop = s[0][0]
				} else {
					stop = s[len(s)-1][1]
				}
			}
			if forward {
				rects = append(rects, [4]int{stop, main, b[0], b[1]})
			} else {
				rects = append(rects, [4]int{0, stop, b[0], b[1]})
			}
		}
	}
	for _, r := range rects {
		if horizontal {
			p.fill(image.Rect(r[0], r[2], r[1], r[3]))
		} else {
			p.fill(image.Rect(r[2], r[0], r[3], r[1]))
		}
	}
}

// dashes draws a straight dashed line of n dashes.
func (p pen) dashes(arms [4]int, n int) {
	w, h := p.box.Dx(), p.box.Dy()
	horizontal := arms[dirRight] != armNone
	main, across, wt := h, w, arms[dirUp]
	if horizontal {
		main, across, wt = w, h, arms[dirRight]
	}
	band := p.strokes(across, wt)[0]
	gap := max(1, int(math.Round(float64(main)/float64(n)/3)))
	for i := range n {
		from := i*main/n + gap/2
		to := (i+1)*main/n - (gap - gap/2)
		if horizontal {
			p.fill(image.Rect(from, band[0], to, band[1]))
		} else {
			p.fill(image.Rect(band[0], from, band[1], to))
		}
	}
}

// arc draws a rounded corner ╭╮╯╰: a quarter circle between the middles of
// its two arms, then straight on to the edges.
func (p pen) arc(r rune) {
	w, h := float64(p.box.Dx()), float64(p.box.Dy())
	t := float64(p.thin)
	cx := float64((p.box.Dx()-p.thin)/2) + t/2 // middle of a vertical line
	cy := float64((p.box.Dy()-p.thin)/2) + t/2 // middle of a horizontal line
	sx, sy := 1.0, 1.0                         // where the arms go: right, down
	switch r {
	case '╮':
		sx = -1
	case '╯':
		sx, sy = -1, -1
	case '╰':
		sy = -1
	}
	rad := min(cx, w-cx, cy, h-cy)
	ox, oy := cx+sx*rad, cy+sy*rad // the circle's centre
	p.shape(func(x, y float64) bool {
		var d float64
		dx, dy := x-ox, y-oy
		switch {
		case dx*sx <= 0 && dy*sy <= 0: // the quarter facing the corner
			d = math.Abs(math.Hypot(dx, dy) - rad)
		case dx*sx > 0 && dy*sy <= 0: // beside the horizontal arm
			d = math.Abs(y - cy)
		case dx*sx <= 0 && dy*sy > 0: // beside the vertical arm
			d = math.Abs(x - cx)
		default: // beyond both arms' ends
			return false
		}
		return d <= t/2
	})
}

// diagonal draws ╱ ╲ ╳ corner to corner.
func (p pen) diagonal(r rune) {
	w, h := float64(p.box.Dx()), float64(p.box.Dy())
	t := float64(p.thin)
	l := math.Hypot(w, h)
	p.shape(func(x, y float64) bool {
		up := math.Abs(h*x+w*y-w*h) / l // distance to ╱
		down := math.Abs(h*x-w*y) / l   // distance to ╲
		switch r {
		case '╱':
			return up <= t/2
		case '╲':
			return down <= t/2
		}
		return up <= t/2 || down <= t/2
	})
}

// block draws U+2580–259F: eighths, halves, quadrants and shades.
func (p pen) block(r rune) {
	w, h := p.box.Dx(), p.box.Dy()
	x8 := func(n int) int { return int(math.Round(float64(w*n) / 8)) }
	y8 := func(n int) int { return int(math.Round(float64(h*n) / 8)) }
	hw, hh := w/2, h/2
	quads := map[rune]string{ // upper left, upper right, lower left, lower right
		'▖': "0010", '▗': "0001", '▘': "1000", '▙': "1011", '▚': "1001",
		'▛': "1110", '▜': "1101", '▝': "0100", '▞': "0110", '▟': "0111",
	}
	switch {
	case r == '▀':
		p.fill(image.Rect(0, 0, w, hh))
	case r >= '▁' && r <= '█': // lower eighths, the full block
		p.fill(image.Rect(0, h-y8(int(r-'▀')), w, h))
	case r >= '▉' && r <= '▏': // left eighths, 7/8 down to 1/8
		p.fill(image.Rect(0, 0, x8(int('▐'-r)), h))
	case r == '▐':
		p.fill(image.Rect(hw, 0, w, h))
	case r >= '░' && r <= '▓':
		p.blend(image.Rect(0, 0, w, h), float64(r-'░'+1)/4)
	case r == '▔':
		p.fill(image.Rect(0, 0, w, y8(1)))
	case r == '▕':
		p.fill(image.Rect(w-x8(1), 0, w, h))
	default:
		q := quads[r]
		cells := []image.Rectangle{
			image.Rect(0, 0, hw, hh), image.Rect(hw, 0, w, hh),
			image.Rect(0, hh, hw, h), image.Rect(hw, hh, w, h),
		}
		for i, c := range cells {
			if q[i] == '1' {
				p.fill(c)
			}
		}
	}
}

// braille draws U+2800–28FF as round dots in two columns of four.
func (p pen) braille(r rune) {
	w, h := float64(p.box.Dx()), float64(p.box.Dy())
	bits := int(r - 0x2800)
	// Dot number n (1–8) is bit n-1; dots 1–3 and 7 are the left column.
	at := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	rad := w * 0.13
	p.shape(func(x, y float64) bool {
		for i, d := range at {
			if bits&(1<<i) == 0 {
				continue
			}
			dx := w * (0.3 + 0.4*float64(d[0]))
			dy := h * (0.14 + 0.24*float64(d[1]))
			if math.Hypot(x-dx, y-dy) <= rad {
				return true
			}
		}
		return false
	})
}

// sextant draws U+1FB00–1FB3B: a cell in two columns of three rows, with
// the patterns that are already block elements (▌ ▐) skipped.
func (p pen) sextant(r rune) {
	n := int(r-0x1fb00) + 1
	if n >= 21 {
		n++ // 21 is ▌
	}
	if n >= 42 {
		n++ // 42 is ▐
	}
	w, h := p.box.Dx(), p.box.Dy()
	y3 := func(i int) int { return int(math.Round(float64(h*i) / 3)) }
	for i := range 6 {
		if n&(1<<i) == 0 {
			continue
		}
		xs := [3]int{0, w / 2, w}
		col, row := i%2, i/2
		p.fill(image.Rect(xs[col], y3(row), xs[col+1], y3(row+1)))
	}
}

// Points on the edges of a cell for the smooth mosaic wedges, in cell
// units: x across, y down, both from 0 to 1, on the sextant grid (thirds of
// the height).
var (
	upperLeft        = [2]float64{0, 0}
	upperCentre      = [2]float64{0.5, 0}
	upperRight       = [2]float64{1, 0}
	upperMiddleLeft  = [2]float64{0, 1.0 / 3}
	upperMiddleRight = [2]float64{1, 1.0 / 3}
	lowerMiddleLeft  = [2]float64{0, 2.0 / 3}
	lowerMiddleRight = [2]float64{1, 2.0 / 3}
	lowerLeft        = [2]float64{0, 1}
	lowerCentre      = [2]float64{0.5, 1}
	lowerRight       = [2]float64{1, 1}
)

// The smooth mosaic wedges.
const wedgeFirst, wedgeLast rune = 0x1fb3c, 0x1fb67

// wedgeLines are the 22 lines of the smooth mosaic wedges U+1FB3C–1FB51,
// each with the corner whose side of it is filled ("LOWER LEFT BLOCK
// DIAGONAL LOWER MIDDLE LEFT TO LOWER CENTRE"). U+1FB52–1FB67 are the same
// lines in the same order, filled on the other side.
var wedgeLines = [22]struct{ from, to, corner [2]float64 }{
	{lowerMiddleLeft, lowerCentre, lowerLeft},       // 🬼
	{lowerMiddleLeft, lowerRight, lowerLeft},        // 🬽
	{upperMiddleLeft, lowerCentre, lowerLeft},       // 🬾
	{upperMiddleLeft, lowerRight, lowerLeft},        // 🬿
	{upperLeft, lowerCentre, lowerLeft},             // 🭀
	{upperMiddleLeft, upperCentre, lowerRight},      // 🭁
	{upperMiddleLeft, upperRight, lowerRight},       // 🭂
	{lowerMiddleLeft, upperCentre, lowerRight},      // 🭃
	{lowerMiddleLeft, upperRight, lowerRight},       // 🭄
	{lowerLeft, upperCentre, lowerRight},            // 🭅
	{lowerMiddleLeft, upperMiddleRight, lowerRight}, // 🭆
	{lowerCentre, lowerMiddleRight, lowerRight},     // 🭇
	{lowerLeft, lowerMiddleRight, lowerRight},       // 🭈
	{lowerCentre, upperMiddleRight, lowerRight},     // 🭉
	{lowerLeft, upperMiddleRight, lowerRight},       // 🭊
	{lowerCentre, upperRight, lowerRight},           // 🭋
	{upperCentre, upperMiddleRight, lowerLeft},      // 🭌
	{upperLeft, upperMiddleRight, lowerLeft},        // 🭍
	{upperCentre, lowerMiddleRight, lowerLeft},      // 🭎
	{upperLeft, lowerMiddleRight, lowerLeft},        // 🭏
	{upperCentre, lowerRight, lowerLeft},            // 🭐
	{upperMiddleLeft, lowerMiddleRight, lowerLeft},  // 🭑
}

// wedge fills smooth mosaic wedge r (U+1FB3C–1FB67): the part of the cell
// on its corner's side of its line.
func (p pen) wedge(r rune) {
	i := int(r - wedgeFirst)
	l := wedgeLines[i%len(wedgeLines)]
	corner := l.corner
	if i >= len(wedgeLines) { // the other side: the opposite corner's
		corner = [2]float64{1 - corner[0], 1 - corner[1]}
	}
	w, h := float64(p.box.Dx()), float64(p.box.Dy())
	ax, ay, bx, by := l.from[0]*w, l.from[1]*h, l.to[0]*w, l.to[1]*h
	side := func(x, y float64) float64 { return (bx-ax)*(y-ay) - (by-ay)*(x-ax) }
	want := side(corner[0]*w, corner[1]*h) > 0
	p.shape(func(x, y float64) bool { return side(x, y) > 0 == want })
}
