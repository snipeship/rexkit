package ui

import (
	"testing"
	"time"
)

func TestStatusTextAndTag(t *testing.T) {
	th := NewTheme(true)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if got := th.StatusText(StatusFailed, now).String(); got != GlyphFailed+" failed" {
		t.Errorf("StatusText = %q", got)
	}
	if line := th.StatusText(StatusBlocked, now); cellsWithBg(t, line.Render(), th.C.WarnBg) != 0 {
		t.Error("list statuses are never filled")
	}
	if th.StatusText(StatusNone, now) != nil {
		t.Error("no status, no text")
	}
	tag := th.Tag(StatusBlocked, "claude needs you", now)
	if got := tag.String(); got != "▐● claude needs you▌" {
		t.Errorf("Tag = %q", got)
	}
	if n := cellsWithBg(t, tag.Render(), th.C.WarnBg); n != tag.Width()-2 {
		t.Errorf("tag fill covers %d of %d cells", n, tag.Width()-2)
	}
	if got := th.Tag(StatusIdle, "", now).String(); got != "○ idle" {
		t.Errorf("an idle tag is plain text: %q", got)
	}
}
