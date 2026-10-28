package ui

import (
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rextheme"
)

// Nothing stands in for the float's own surface with a colour: in Rex's
// blended and glass interface styles the float is translucent and a cell
// with a background of its own is drawn solid, so a fill in a surface
// colour shows as a block. Small fills that mean something (keycaps, the
// selection, a field, a chip) are fine, so a surface colour that is also
// one of theirs is not checked.
func TestNoSurfaceFills(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := NewTheme(dark)
		var surfaces []rextheme.Color
		for _, s := range []color.Color{th.C.Surface, th.C.Raised, th.C.Popover, th.C.Well, th.C.Tray} {
			meant := false
			for _, f := range []color.Color{th.C.KeyCap, th.C.TrayKeyCap, th.C.Field, th.C.SelBg} {
				meant = meant || near(rextheme.Of(s), rextheme.Of(f))
			}
			if !meant {
				surfaces = append(surfaces, rextheme.Of(s))
			}
		}
		views := map[string]string{"gallery": Gallery(140, 44, dark)}
		base := make([]string, 20)
		for i := range base {
			base[i] = Paint(strings.Repeat("x", 90), Clear)
		}
		groups := []KeyGroup{{"Go", []Binding{{[]string{"enter"}, "go"}, {[]string{"esc"}, "close"}}}}
		panel, at, _ := th.KeyPanel(groups, 90, 20, PanelCorner, 0)
		views["key panel"] = strings.Join(th.Raise(base, panel, at), "\n")
		var p Preview
		p.Set(p.Begin(), Plain("title"), lines(30))
		views["preview"] = strings.Join(p.View(th, 60, 20), "\n")
		views["tray"] = strings.Join(th.Tray(Tray{Hints: []Hint{{Key: "enter", Label: "go"}}}, 60, TrayRows), "\n")
		for name, view := range views {
			lines := strings.Split(Bleed(view), "\n")
			buf := uv.NewScreenBuffer(ansi.StringWidth(lines[0]), len(lines))
			uv.NewStyledString(strings.Join(lines, "\n")).Draw(buf, buf.Bounds())
			bad := 0
			for y := range len(lines) {
				for x := range buf.Width() {
					c := buf.CellAt(x, y)
					if c == nil || c.Style.Bg == nil {
						continue
					}
					bg := rextheme.Of(c.Style.Bg)
					for _, s := range surfaces {
						if near(bg, s) {
							bad++
							break
						}
					}
				}
			}
			if bad > 0 {
				t.Errorf("dark=%v %s: %d cells painted in a surface colour", dark, name, bad)
			}
		}
	}
}

// near reports whether two colours are within a shade of each other.
func near(a, b rextheme.Color) bool {
	d := func(x, y uint8) int { return max(int(x), int(y)) - min(int(x), int(y)) }
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
}
