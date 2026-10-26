package switcher

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/ui/uitest"
)

// checkSize asserts that a frame is exactly h lines of w cells.
func checkSize(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, ansi.Strip(l))
		}
	}
}

// The goldens draw wide and medium windows in a card, as the stage does
// from 80 columns on, and narrow ones flat across the whole window.
func TestGolden(t *testing.T) {
	sizes := []struct {
		name   string
		w, h   int
		carded bool
	}{
		{"wide", 160, 40, true},
		{"medium", 100, 30, true},
		{"narrow", 70, 20, false},
	}
	states := []struct {
		name  string
		dark  []bool
		setup func(*harness)
		opts  []harnessOption
	}{
		{"switcher", []bool{true, false}, func(*harness) {}, nil},
		{"search", []bool{true}, func(h *harness) { h.typeText("nav"); h.fire() }, nil},
		{"drill", []bool{false}, func(h *harness) { h.selectEntry("studio"); h.key("right"); h.fire() }, nil},
		{"help", []bool{true}, func(h *harness) { h.key("?") }, nil},
		{"preview", []bool{true}, func(h *harness) { h.key("ctrl+/"); h.fire() }, nil},
		{"empty", []bool{true}, func(h *harness) { h.typeText("zqxj") }, nil},
		{"rename", []bool{true, false}, func(h *harness) { h.selectEntry("migrations"); h.key("ctrl+e") }, nil},
		{"close", []bool{true}, func(h *harness) { h.selectEntry("studio"); h.key("ctrl+x") }, nil},
		{"new", []bool{false}, func(h *harness) { h.key("ctrl+a"); h.typeText("demo") }, nil},
		{"error", []bool{false}, func(*harness) {}, []harnessOption{withSnapshotError()}},
		{"failed", []bool{true}, func(h *harness) {
			h.selectEntry("nvim")
			h.fake.Fail("session.focus_block", fmt.Errorf("block gone: %w", rex.ErrNotFound))
			h.key("enter")
		}, nil},
	}
	for _, st := range states {
		for _, sz := range sizes {
			for _, dark := range st.dark {
				if st.name == "preview" && sz.name != "narrow" {
					continue // ⌃/ matters where the preview is not shown by default
				}
				scheme := "light"
				if dark {
					scheme = "dark"
				}
				name := st.name + "-" + sz.name + "-" + scheme
				t.Run(name, func(t *testing.T) {
					opts := st.opts
					if sz.carded {
						opts = append(slices.Clip(opts), withCard())
					}
					h := newHarness(t, sz.w, sz.h, dark, opts...)
					st.setup(h)
					out := h.m.View().Content
					checkSize(t, out, sz.w, sz.h)
					uitest.Golden(t, name, out)
				})
			}
		}
	}
}
