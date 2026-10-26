package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestField(t *testing.T) {
	th := NewTheme(true)
	q := Query{Value: "api", Cursor: 3, Focused: true, RealCursor: true}
	lines, x, y := th.Field(Field{Query: q, Helper: "Was api"}, 40, false)
	if len(lines) != FieldRows {
		t.Fatalf("%d rows", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 40 {
			t.Fatalf("a row is %d cells", ansi.StringWidth(l))
		}
	}
	row := ansi.Strip(lines[y])
	if !strings.HasPrefix(row, " ▐ api") || []rune(row)[x-1] != 'i' {
		t.Errorf("text row %q, cursor at %d", row, x)
	}
	if got := strings.TrimSpace(ansi.Strip(lines[3])); got != "Was api" {
		t.Errorf("helper %q", got)
	}
	if cellsWithBg(t, lines[1], th.C.Field) != 36 {
		t.Errorf("field fill: %d cells", cellsWithBg(t, lines[1], th.C.Field))
	}

	lines, _, _ = th.Field(Field{Query: q, Err: true, Helper: "Names cannot contain /"}, 40, false)
	if cellsWithBg(t, lines[1], th.C.ErrField) != 36 || !strings.Contains(ansi.Strip(lines[3]), GlyphFailed+" Names") {
		t.Errorf("error field: %q", ansi.Strip(strings.Join(lines, "\n")))
	}

	lines, x, y = th.Field(Field{Query: q, Helper: "Was api"}, 30, true)
	if len(lines) != 1 || y != 0 || x != 1+1+3 {
		t.Errorf("compact: %d lines, cursor %d,%d: %q", len(lines), x, y, ansi.Strip(lines[0]))
	}
}
