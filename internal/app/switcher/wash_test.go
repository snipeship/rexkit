package switcher

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// washed returns, as plain text, the lines of a rendered view with cells on
// bg. The selection is a background wash, not a glyph, so this is how a
// test finds the selected row: washed(view, m.theme.C.SelBg).
func washed(view string, bg color.Color) []string {
	r, g, b, _ := bg.RGBA()
	sgr := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
	var out []string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, sgr) {
			out = append(out, ansi.Strip(l))
		}
	}
	return out
}
