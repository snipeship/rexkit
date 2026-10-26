package scrollback

import (
	"context"
	"fmt"
	"strings"
)

// demoSource is a generated, colourful terminal session in Rex's VT export
// style (SGR colours, CRLF line ends): prompts, a git log, a test run, a
// build log, 256 and true colours, wide characters and a long line.
func demoSource(context.Context) (string, error) {
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\r\n")
	}
	prompt := func(cmd string) {
		line("\x1b[38;5;4m~/code/rexkit\x1b[m \x1b[38;5;5m⎇ main\x1b[m \x1b[38;5;2m❯\x1b[m %s", cmd)
	}

	prompt("git log --oneline --graph -n 6")
	commits := []struct{ graph, hash, ref, msg string }{
		{"*", "3f9c2a1", " (\x1b[1;38;5;6mHEAD -> \x1b[1;38;5;2mmain\x1b[33m, \x1b[1;38;5;1morigin/main\x1b[33m)", "Show scrollback in a float over the pane"},
		{"*", "b81e07d", "", "Keep colours in captures"},
		{"*", "0c4d9e2", "", "Sanitize captures to SGR only"},
		{"|\\", "", "", ""},
		{"| *", "9a7f3b0", " (\x1b[1;38;5;2mfeature/viewer\x1b[33m)", "Start positions: top, bottom, page-up"},
		{"|/", "", "", ""},
	}
	for _, c := range commits {
		if c.hash == "" {
			line("\x1b[38;5;1m%s\x1b[m", c.graph)
			continue
		}
		line("\x1b[38;5;1m%s\x1b[m \x1b[33m%s%s\x1b[m %s", c.graph, c.hash, c.ref, c.msg)
	}

	prompt("go test ./...")
	tests := []struct {
		ok   bool
		pkg  string
		took string
	}{
		{true, "rexkit/internal/match", "0.214s"},
		{true, "rexkit/internal/ui", "0.412s"},
		{false, "rexkit/internal/app/scrollback", "0.088s"},
		{true, "rexkit/internal/ui/sanitize", "1.031s"},
	}
	for _, t := range tests {
		if t.ok {
			line("\x1b[32mok\x1b[m  \t%-34s %s", t.pkg, t.took)
			continue
		}
		line("\x1b[1;31m--- FAIL\x1b[m: TestStartPosition (0.00s)")
		line("    page_test.go:42: start = \x1b[31m\"top\"\x1b[m, want \x1b[32m\"bottom\"\x1b[m")
		line("\x1b[1;31mFAIL\x1b[m\t%-34s %s", t.pkg, t.took)
	}

	prompt("rexkit doctor")
	line("\x1b[32m✓\x1b[m binary matches the source tree")
	line("\x1b[32m✓\x1b[m config chain loads (\x1b[2m~/.config/rex/init.lua\x1b[m)")
	line("\x1b[33m⚠\x1b[m Remote Control is off \x1b[2m· cross-session double-click needs it\x1b[m")
	line("\x1b[31m✗\x1b[m zoxide not found \x1b[2m· brew install zoxide\x1b[m")

	prompt("tail -n 240 build.log")
	levels := []string{"\x1b[38;5;6mINFO\x1b[m ", "\x1b[38;5;6mINFO\x1b[m ", "\x1b[38;5;6mINFO\x1b[m ", "\x1b[33mWARN\x1b[m ", "\x1b[38;5;6mINFO\x1b[m ", "\x1b[1;31mERROR\x1b[m"}
	steps := []string{"compile", "link", "embed assets", "sign", "test", "package"}
	for i := range 240 {
		level := levels[i%len(levels)]
		line("\x1b[2m12:%02d:%02d.%03d\x1b[m %s \x1b[38;5;5m%-12s\x1b[m step %3d · %s \x1b[2m(%d ms)\x1b[m",
			i/60, i%60, (i*37)%1000, level, steps[i%len(steps)], i+1, strings.Repeat("▪", 1+i%9), 3+(i*13)%200)
	}

	prompt("colours")
	var c256 strings.Builder
	for i := range 36 {
		fmt.Fprintf(&c256, "\x1b[48;5;%dm  ", 16+i*6)
	}
	line("256  %s\x1b[m", c256.String())
	var rgb strings.Builder
	for i := range 72 {
		r, g := 255-i*3, 80+i*2
		fmt.Fprintf(&rgb, "\x1b[48;2;%d;%d;200m ", r, g)
	}
	line("rgb  %s\x1b[m", rgb.String())
	line("\x1b[1mbold\x1b[m \x1b[2mdim\x1b[m \x1b[3mitalic\x1b[m \x1b[4munderline\x1b[m \x1b[7mreverse\x1b[m \x1b[9mstrike\x1b[m")
	line("wide  日本語のテキスト 🦖 emoji · tabs\tline\tup")
	line("long  %s", strings.Repeat("a long line that wraps at the viewer's width · ", 6))
	prompt("")
	return b.String(), nil
}
