package scrollback

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"rexkit/internal/ui"
)

// awkward is a path that would break any shell string built from it.
const awkward = "/tmp/a dir/it's \"$(rm -rf ~)\"; `x`.txt"

func TestNvimCommand(t *testing.T) {
	for _, dark := range []bool{true, false} {
		c := nvimCommand("/opt/homebrew/bin/nvim", "/tmp/p rof.lua", awkward, startHalfUp, page{dropped: 7}, opening{}, ui.NewTheme(dark))
		want := []string{"/opt/homebrew/bin/nvim", "-u", "/tmp/p rof.lua", "-i", "NONE", "-n", "--noplugin"}
		if !slices.Equal(c.argv, want) {
			t.Errorf("argv = %q\nwant %q", c.argv, want)
		}
		if len(c.env) != 1 || !strings.HasPrefix(c.env[0], "REXKIT_SCROLLBACK=") {
			t.Fatalf("env = %q", c.env)
		}
		var s nvimSettings
		if err := json.Unmarshal([]byte(strings.TrimPrefix(c.env[0], "REXKIT_SCROLLBACK=")), &s); err != nil {
			t.Fatal(err)
		}
		if s.File != awkward || s.Start != startHalfUp || s.Dropped != 7 || s.Dark != dark || s.Line != 0 || s.Search != "" {
			t.Errorf("settings = %+v", s)
		}
		// The profile's colour table: the capture in the pane's text
		// colour, the status line on the tray's.
		p := ui.NewTheme(dark).C
		colors := map[string]color.Color{
			"fg": p.Fg, "muted": p.FgMuted, "subtle": p.FgSubtle, "term_fg": p.WellFg,
			"tray": p.Tray, "keycap": p.TrayKeyCap, "accent": p.Accent, "accent_solid": p.AccentSolid,
			"on_accent": p.AccentOnSolid, "ok": p.OK, "err": p.Err,
			"sel": p.WellSelBg, "sel_fg": p.WellSelFg, "find": p.WellFindBg, "flash": p.WellFlashBg,
		}
		if len(s.Colors) != len(colors) {
			t.Errorf("colors = %v", s.Colors)
		}
		for name, col := range colors {
			if s.Colors[name] != hex(col) {
				t.Errorf("colors[%q] = %q, want %s", name, s.Colors[name], hex(col))
			}
		}
	}
}

func TestHex(t *testing.T) {
	dark, light := ui.NewTheme(true), ui.NewTheme(false)
	if hex(dark.C.Accent) != "#3485fa" || hex(light.C.Accent) != "#0667d8" { // Rex's accent tokens
		t.Errorf("accent = %s / %s", hex(dark.C.Accent), hex(light.C.Accent))
	}
}

func TestLessCommand(t *testing.T) {
	tests := []struct {
		start string
		tail  []string
	}{
		{startBottom, []string{"+G", "--", awkward}},
		{startPageUp, []string{"+Gb", "--", awkward}}, // the end, then a page back
		{startHalfUp, []string{"+Gu", "--", awkward}}, // the end, then half a page back
		{startTop, []string{"--", awkward}},
	}
	for _, tt := range tests {
		c := lessCommand("/usr/bin/less", awkward, tt.start, 0)
		want := append([]string{"/usr/bin/less", "-R", "-K", "-~", "-Ps" + lessPrompt(tt.start)}, tt.tail...)
		if !slices.Equal(c.argv, want) {
			t.Errorf("%s: argv = %q\nwant %q", tt.start, c.argv, want)
		}
		if !slices.Equal(c.env, []string{"LESSSECURE=1", "LESSHISTFILE=-"}) {
			t.Errorf("%s: env = %q", tt.start, c.env)
		}
	}
}

func TestLessCommandAtALine(t *testing.T) {
	c := lessCommand("/usr/bin/less", awkward, startBottom, 1233)
	want := []string{"/usr/bin/less", "-R", "-K", "-~", "-Ps" + lessPrompt(startBottom), "-j.3", "+1233g", "--", awkward}
	if !slices.Equal(c.argv, want) {
		t.Errorf("argv = %q\nwant %q", c.argv, want)
	}
}

func TestLessPromptSearchHint(t *testing.T) {
	// ? is special in a less prompt, so the literal one is escaped.
	for start, want := range map[string]string{startBottom: `\? search up`, startPageUp: `\? search up`, startTop: "/ search"} {
		if got := lessPrompt(start); !strings.Contains(got, "   "+want+"   q close") {
			t.Errorf("%s: prompt %q lacks %q", start, got, want)
		}
	}
}

func TestPreparedCommandPassesDropped(t *testing.T) {
	p := prepared{page: newPage("1\n2\n3\n4\n5\n", 3, 0), file: "/f", profile: "/p.lua", program: program{path: "/bin/nvim"}}
	c := p.command(startBottom, ui.NewTheme(true))
	var s nvimSettings
	if err := json.Unmarshal([]byte(strings.TrimPrefix(c.env[0], "REXKIT_SCROLLBACK=")), &s); err != nil {
		t.Fatal(err)
	}
	if s.Dropped != 3 || p.page.dropped != 3 {
		t.Errorf("dropped = %d (page %d), want 3", s.Dropped, p.page.dropped)
	}
}

func TestPreparedCommandOpensAtTheLine(t *testing.T) {
	p := prepared{page: newPage("a\nb\n", 0, 0), file: "/f", profile: "/p.lua", program: program{path: "/bin/nvim"},
		at: opening{line: 2, pattern: `\V\cb`}}
	var s nvimSettings
	if err := json.Unmarshal([]byte(strings.TrimPrefix(p.command(startBottom, ui.NewTheme(true)).env[0], "REXKIT_SCROLLBACK=")), &s); err != nil {
		t.Fatal(err)
	}
	if s.Line != 2 || s.Search != `\V\cb` {
		t.Errorf("line %d search %q", s.Line, s.Search)
	}
	p.program.less = true
	if c := p.command(startBottom, ui.NewTheme(true)); !slices.Contains(c.argv, "+2g") {
		t.Errorf("less argv %q does not open at line 2", c.argv)
	}
}

func TestPickProgram(t *testing.T) {
	has := func(names ...string) finder {
		return func(name string, _ ...string) (string, bool) {
			if slices.Contains(names, name) {
				return "/bin/" + name, true
			}
			return "", false
		}
	}
	tests := []struct {
		name     string
		override string
		find     finder
		want     program
		fails    bool
	}{
		{"nvim", "", has("nvim", "less"), program{path: "/bin/nvim"}, false},
		{"override wins", "/opt/nvim", has("nvim"), program{path: "/opt/nvim"}, false},
		{"less fallback", "", has("less"), program{path: "/bin/less", less: true}, false},
		{"nothing", "", has(), program{}, true},
	}
	for _, tt := range tests {
		got, err := pickProgram(tt.override, tt.find)
		if got != tt.want || (err != nil) != tt.fails {
			t.Errorf("%s: %+v, %v", tt.name, got, err)
		}
	}
}

func TestFindProgramFallsBackToKnownPaths(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // an empty PATH, as blocks may have
	dir := t.TempDir()
	exe, plain := filepath.Join(dir, "nvim"), filepath.Join(dir, "notes")
	if err := os.WriteFile(exe, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		fallbacks []string
		want      string
		ok        bool
	}{
		{[]string{filepath.Join(dir, "missing"), exe}, exe, true},
		{[]string{plain, dir}, "", false}, // not executable; a directory
		{nil, "", false},
	}
	for _, tt := range tests {
		got, ok := findProgram("rexkit-test-no-such-program", tt.fallbacks...)
		if got != tt.want || ok != tt.ok {
			t.Errorf("findProgram(%q) = %q, %v", tt.fallbacks, got, ok)
		}
	}
}
