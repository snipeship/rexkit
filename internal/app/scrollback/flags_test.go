package scrollback

import (
	"errors"
	"flag"
	"testing"

	"rexkit/internal/find"
	"rexkit/internal/popup"
)

func TestParseArgs(t *testing.T) {
	blur := popup.Options{DismissOnBlur: true} // accepted for every tool; Neovim ignores it
	tests := []struct {
		name  string
		args  []string
		want  config
		fails bool
	}{
		{"defaults", nil, config{popup: blur, start: startBottom}, false},
		{"float flags", []string{"--session", "s1", "--origin-block", "b1", "--zoomed", "--state", "/st", "--start", "half-up"},
			config{popup: popup.Options{Session: "s1", OriginBlock: "b1", Zoomed: true, State: "/st", DismissOnBlur: true}, start: startHalfUp}, false},
		{"plain and nvim", []string{"--plain", "--nvim", "/x/nvim", "--start=top"}, config{popup: blur, start: startTop, plain: true, nvim: "/x/nvim"}, false},
		{"demo", []string{"--demo", "--start", "page-up"}, config{popup: blur, start: startPageUp, demo: true}, false},
		{"page, line and search", []string{"--page", "/st/page.txt", "--line", "12", "--search", `5\d\d`, "--regex", "--match-case"},
			config{popup: blur, start: startBottom, page: "/st/page.txt", line: 12, search: find.Query{Text: `5\d\d`, Regex: true, MatchCase: true}}, false},
		{"negative line", []string{"--line", "-1"}, config{}, true},
		{"unknown start", []string{"--start", "middle"}, config{}, true},
		{"stray argument", []string{"file.txt"}, config{}, true},
		{"unknown flag", []string{"--nope"}, config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if (err != nil) != tt.fails {
				t.Fatalf("err = %v", err)
			}
			if !tt.fails && got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	if _, err := parseArgs([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}
}
