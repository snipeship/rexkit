package search

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/popup"
	"rexkit/internal/ui/uitest"
)

func TestGolden(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		dark   bool
		card   bool
		script func(*harness)
	}{
		{"start-120x30", 120, 30, true, false, nil},
		{"default-160x40", 160, 40, true, false, func(h *harness) { h.typeText("panic"); h.key("alt+down") }},
		{"light-160x40", 160, 40, false, false, func(h *harness) { h.typeText("panic"); h.key("alt+down") }},
		{"card-160x40", 160, 40, true, true, func(h *harness) { h.typeText("panic") }},
		{"pane-160x40", 160, 40, true, false, func(h *harness) {
			h.typeText("panic")
			h.selectKind("tail -f production.log", rowPane)
		}},
		{"unfolded-160x40", 160, 40, true, false, func(h *harness) {
			h.typeText("panic")
			h.selectKind("zsh", rowMore)
			h.key("right")
		}},
		{"regex-160x40", 160, 40, true, false, func(h *harness) { h.key("alt+r"); h.typeText(`status=5\d\d`) }},
		{"badregex-120x30", 120, 30, true, false, func(h *harness) { h.key("alt+r"); h.typeText(`status=(5\d\d`) }},
		{"nomatch-120x30", 120, 30, true, false, func(h *harness) { h.key("tab"); h.typeText("Segfault") }},
		{"help-160x40", 160, 40, true, false, func(h *harness) { h.typeText("panic"); h.key("f1") }},
		{"narrow-76x30", 76, 30, true, false, func(h *harness) { h.typeText("panic"); h.key("alt+down") }},
		{"tiny-40x10", 40, 10, true, false, func(h *harness) { h.typeText("panic") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newIdle(t, tt.w, tt.h, tt.dark)
			if tt.card {
				h.send(popup.CardMsg{})
				h.send(teaSize(tt.w, tt.h))
			}
			h.run(h.m.Init())
			if tt.script != nil {
				tt.script(h)
			}
			out := h.m.View().Content
			lines := strings.Split(out, "\n")
			if len(lines) != tt.h {
				t.Errorf("%d lines, want %d", len(lines), tt.h)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != tt.w {
					t.Errorf("line %d is %d wide, want %d", i+1, w, tt.w)
				}
			}
			uitest.Golden(t, tt.name, out)
		})
	}
}
