package ui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// cellsWithBg counts the cells of a rendered line drawn on background bg.
func cellsWithBg(t *testing.T, line string, bg color.Color) int {
	t.Helper()
	r, g, b, _ := bg.RGBA()
	want := fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
	on, count := false, 0
	var state byte
	for s := line; len(s) > 0; {
		seq, width, n, next := ansi.DecodeSequence(s, state, nil)
		s, state = s[n:], next
		switch {
		case width > 0:
			if on {
				count += width
			}
		case strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m"):
			on = applySGRBg(seq[2:len(seq)-1], want, on)
		}
	}
	return count
}

// applySGRBg tracks whether the background is want ("r;g;b") after an SGR.
func applySGRBg(params, want string, on bool) bool {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		switch ps[i] {
		case "", "0", "49":
			on = false
		case "48":
			if i+4 < len(ps) && ps[i+1] == "2" {
				on = strings.Join(ps[i+2:i+5], ";") == want
				i += 4
			}
		case "38", "58":
			if i+1 < len(ps) && ps[i+1] == "2" {
				i += 4
			} else if i+1 < len(ps) && ps[i+1] == "5" {
				i += 2
			}
		}
	}
	return on
}

// checkSize asserts that out is exactly h lines of w cells.
func checkSize(t *testing.T, name, out string, w, h int) {
	t.Helper()
	if w == 0 || h == 0 {
		if strings.TrimSpace(ansi.Strip(out)) != "" {
			t.Fatalf("%s: expected nothing, got %q", name, out)
		}
		return
	}
	lines := strings.Split(out, "\n")
	if out == "" {
		lines = nil
	}
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want %d", name, len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("%s: line %d is %d cells, want %d: %q", name, i, got, w, ansi.Strip(l))
		}
	}
}

func size(w, h int) string { return strconv.Itoa(w) + "x" + strconv.Itoa(h) }
