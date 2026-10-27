package switcher

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
)

// runDemo runs the switcher on a made-up Rex, so it can be looked at (and
// reviewed) anywhere: sessions with agents in every state, long and CJK names,
// and colourful pane output.
func runDemo(dismissOnBlur bool, nextKey string) error {
	now := time.Now()
	fake, origin, history := demoWorld(now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := env{api: fake, close: func(context.Context) error { return nil }, now: time.Now, after: tick, demo: true}
	o := options{origin: origin, dismissOnBlur: dismissOnBlur, home: demoHome, nextKey: nextKey}
	_, err := tea.NewProgram(newModel(ctx, e, o, nil, history), tea.WithContext(ctx)).Run()
	return err
}

const demoHome = "/Users/demo"

// demoPane describes one pane of the demo world.
type demoPane struct {
	label   string // "" leaves Rex's automatic label
	program string
	cwd     string
	state   string // program status; "" for none
	task    string
	ago     time.Duration // since the status changed
	exit    *rex.Exit
	output  string
	kind    string // why a blocked agent waits (OSC 7501 kind)
	msg     string // what the agent says (OSC 7501 msg)
	title   string // the terminal's title
}

type demoTab struct {
	label string
	panes []demoPane
}

type demoSession struct {
	label string
	tabs  []demoTab
}

// demoWorld builds the fake server, the origin (the "you are here" pane) and
// a visit history whose previous location is studio › api.
func demoWorld(now time.Time) (*rexfake.Fake, place, state.History) {
	return buildDemo(now, demoSessions, []demoVisit{
		{"rexkit/spark/zsh", 0},
		{"studio/api/zsh", 4 * time.Minute},
		{"研究ノート/論文/aider", 52 * time.Minute},
		{"infrastructure-and-deployment-pipelines/deploy/kubectl", 3 * time.Hour},
	})
}

// demoVisit is a visit in a demo history: "session/tab/program", how long ago.
type demoVisit struct {
	key string
	ago time.Duration
}

// buildDemo builds a fake server from sessions; the origin is the first
// session's first tab's second pane.
func buildDemo(now time.Time, sessions []demoSession, visits []demoVisit) (*rexfake.Fake, place, state.History) {
	f := rexfake.New()
	var origin place
	ids := map[string]place{}
	for si, s := range sessions {
		sid, firstWin, firstBlock := f.AddSession(s.label)
		for ti, t := range s.tabs {
			var win string
			var blocks []string
			if ti == 0 {
				win, blocks = firstWin, []string{firstBlock}
				for _, p := range t.panes[1:] {
					blocks = append(blocks, splitDemo(f, sid, blocks[len(blocks)-1], p.label))
				}
			} else {
				win, blocks = f.AddWindow(sid, t.label, labels(t.panes)...)
			}
			_ = f.SetWindowLabel(context.Background(), sid, win, t.label)
			for pi, p := range t.panes {
				b := blocks[pi]
				if p.label != "" {
					_ = f.SetBlockLabel(context.Background(), sid, b, p.label)
				}
				f.SetTerminal(b, p.terminal(now))
				f.SetCapture(b, p.output)
				ids[s.label+"/"+t.label+"/"+p.program] = place{sid, win, b}
				if si == 0 && ti == 0 && pi == 1 {
					origin = place{sid, win, b}
				}
			}
		}
	}
	_ = f.FocusBlock(context.Background(), origin.sessionID, origin.blockID)
	visit := func(key string, ago time.Duration) state.Visit {
		p := ids[key]
		return state.Visit{Kind: "pane", SessionID: p.sessionID, WindowID: p.windowID, BlockID: p.blockID, At: now.Add(-ago)}
	}
	var history state.History
	for _, v := range visits {
		history.Visits = append(history.Visits, visit(v.key, v.ago))
	}
	return f, origin, history
}

// splitDemo adds a pane beside anchor in the first window of a session.
func splitDemo(f *rexfake.Fake, session, anchor, label string) string {
	c, _ := f.NewSplit(context.Background(), session, rex.NewSplitRequest{
		AnchorID: anchor, Direction: rex.Horizontal, Side: "after", Layout: rex.Shell(label, ""),
	})
	return c.BlockIDs[0]
}

func labels(panes []demoPane) []string {
	out := make([]string, len(panes))
	for i, p := range panes {
		out[i] = p.label
	}
	return out
}

func (p demoPane) terminal(now time.Time) rex.Terminal {
	t := rex.Terminal{Process: &rex.Process{
		Child:    &rex.ProcessInfo{Name: p.program, Path: "/usr/local/bin/" + p.program, Cwd: p.cwd},
		LastExit: p.exit,
	}, Title: p.title}
	if p.state != "" {
		t.Status = rex.List[rex.StatusRecord]{{App: p.program, State: p.state, Kind: p.kind, Title: p.task, Msg: p.msg, UpdatedAt: now.Add(-p.ago)}}
	}
	return t
}

var demoSessions = []demoSession{
	{"rexkit", []demoTab{
		{"spark", []demoPane{
			{program: "claude", cwd: demoHome + "/Coding/rexkit", state: rex.StateWorking, task: "Revamp the switcher UI", ago: 3 * time.Second, output: demoClaude},
			{program: "zsh", cwd: demoHome + "/Coding/rexkit", output: demoShell},
		}},
		{"docs", []demoPane{{program: "nvim", cwd: demoHome + "/Coding/rexkit/docs", output: demoEditor}}},
	}},
	{"studio", []demoTab{
		{"web", []demoPane{
			{program: "codex", cwd: demoHome + "/code/studio/web", state: rex.StateBlocked, task: "Navigation polish", ago: 12 * time.Minute, output: demoReview},
			{program: "claude", cwd: demoHome + "/code/studio/web", state: rex.StateWorking, task: "Fix navbar overflow on small screens", ago: 2 * time.Minute, output: demoClaude},
		}},
		{"api", []demoPane{
			{program: "zsh", cwd: demoHome + "/code/studio/api", output: demoServer},
			{label: "migrations", program: "psql", cwd: demoHome + "/code/studio/api", output: demoSQL},
		}},
	}},
	{"infrastructure-and-deployment-pipelines", []demoTab{
		{"deploy", []demoPane{
			{program: "kubectl", cwd: demoHome + "/infra", output: demoPods},
			{program: "gemini", cwd: demoHome + "/infra/terraform", state: rex.StateError, task: "Migrate terraform state to the new bucket", ago: 41 * time.Minute, output: demoFailure},
		}},
		{"logs", []demoPane{{label: "tail -f production.log (eu-west-1, all services)", program: "tail", cwd: demoHome + "/infra", output: demoLogs}}},
	}},
	{"研究ノート", []demoTab{
		{"論文", []demoPane{
			{program: "aider", cwd: demoHome + "/notes/論文", state: rex.StateIdle, task: "要約を書く", ago: 52 * time.Minute, output: demoNotes},
		}},
	}},
	{"scratch", []demoTab{
		{"probe", []demoPane{
			{label: "probe", program: "probe", cwd: "/tmp", exit: &rex.Exit{Code: 1}, output: demoExited},
			{program: "fish", cwd: "/tmp", output: demoShell},
		}},
	}},
}

const (
	esc   = "\x1b["
	reset = esc + "0m"
	bold  = esc + "1m"
	dim   = esc + "2m"
	red   = esc + "31m"
	green = esc + "32m"
	yell  = esc + "33m"
	blue  = esc + "34m"
	mag   = esc + "35m"
	cyan  = esc + "36m"
)

var (
	demoClaude = cyan + "●" + reset + " I'll restructure the row renderer so the selection bar spans every column.\r\n\r\n" +
		green + "✓" + reset + " Read " + bold + "internal/ui/list_view.go" + reset + " (120 lines)\r\n" +
		green + "✓" + reset + " Read " + bold + "internal/app/switcher/row.go" + reset + "\r\n" +
		green + "✓" + reset + " Edit " + bold + "row.go" + reset + "  " + green + "+38" + reset + " " + red + "-12" + reset + "\r\n\r\n" +
		"  The status column now keeps its glyph at every width; the word drops\r\n" +
		"  after the age when the list gets narrow.\r\n\r\n" +
		yell + "✻" + reset + " Running " + bold + "go test ./internal/app/switcher" + reset + dim + " (12s)" + reset + "\r\n"
	demoShell = green + "➜" + reset + "  " + cyan + "rexkit" + reset + " " + blue + "git:(" + red + "main" + blue + ")" + reset + " git status --short\r\n" +
		" " + red + "M" + reset + " internal/app/switcher/row.go\r\n" +
		" " + red + "M" + reset + " internal/app/switcher/view.go\r\n" +
		red + "??" + reset + " internal/app/switcher/demo.go\r\n" +
		green + "➜" + reset + "  " + cyan + "rexkit" + reset + " " + blue + "git:(" + red + "main" + blue + ")" + reset + " " + yell + "✗" + reset + " \r\n"
	demoEditor = blue + "  1 " + reset + bold + "# rexkit — design spec" + reset + "\r\n" +
		blue + "  2 " + reset + "\r\n" +
		blue + "  3 " + reset + "rexkit is one Go binary plus one thin Lua layer that together make Rex\r\n" +
		blue + "  4 " + reset + "feel like a first-class, keyboard-driven workspace.\r\n" +
		dim + "~" + reset + "\r\n" + dim + "~" + reset + "\r\n" +
		esc + "7m" + " NORMAL " + reset + " DESIGN.md                                   1,1  Top\r\n"
	demoReview = mag + "codex" + reset + " " + dim + "· gpt-5 · ~/code/studio/web" + reset + "\r\n\r\n" +
		"› Refine the navigation so the active route is obvious\r\n\r\n" +
		green + "✓" + reset + " Read src/router.ts\r\n" +
		green + "✓" + reset + " Updated src/middleware.ts\r\n" +
		green + "✓" + reset + " Rewrote src/nav/Navbar.tsx " + green + "+88" + reset + " " + red + "-51" + reset + "\r\n\r\n" +
		bold + "Changes are ready." + reset + "\r\n" +
		yell + "●" + reset + " Waiting for your review " + dim + "— approve with ⏎, or type feedback" + reset + "\r\n"
	demoServer = dim + "[12:04:31]" + reset + " " + green + "GET" + reset + "  /api/projects        200  " + dim + "12ms" + reset + "\r\n" +
		dim + "[12:04:32]" + reset + " " + green + "GET" + reset + "  /api/projects/42     200  " + dim + "8ms" + reset + "\r\n" +
		dim + "[12:04:35]" + reset + " " + yell + "POST" + reset + " /api/projects/42/run 202  " + dim + "31ms" + reset + "\r\n" +
		dim + "[12:04:39]" + reset + " " + red + "GET" + reset + "  /api/health          503  " + dim + "2ms" + reset + "\r\n" +
		green + "✓" + reset + " Ready on " + cyan + "http://localhost:3000" + reset + "\r\n"
	demoSQL = "studio=# " + bold + "select count(*) from migrations;" + reset + "\r\n" +
		" count \r\n-------\r\n    37\r\n(1 row)\r\n\r\nstudio=# \r\n"
	demoPods = bold + "NAME                      READY   STATUS             RESTARTS   AGE" + reset + "\r\n" +
		"api-7d49b6c5f-x2k9p       1/1     " + green + "Running" + reset + "            0          3d\r\n" +
		"web-9f7326d4b-pq8lm       1/1     " + green + "Running" + reset + "            0          3d\r\n" +
		"worker-5c8d9f7b6-zz4nt    0/1     " + red + "CrashLoopBackOff" + reset + "   14         52m\r\n"
	demoFailure = mag + "gemini" + reset + " ❯ terraform state mv -state-out=gs://infra-state-v2\r\n\r\n" +
		red + "Error:" + reset + " " + bold + "Error acquiring the state lock" + reset + "\r\n\r\n" +
		"  Lock Info:\r\n    ID:        7d1c0e5a-1b2f-4f0c-9d5e-3a1e0f6b8c21\r\n    Operation: OperationTypeApply\r\n    Who:       ci@runner-17\r\n\r\n" +
		red + "✗" + reset + " Task failed after 3 attempts\r\n"
	demoLogs = dim + "2026-10-26T12:04:39Z" + reset + " " + cyan + "api" + reset + "    request_id=8f2c status=200 latency=12ms\r\n" +
		dim + "2026-10-26T12:04:40Z" + reset + " " + yell + "worker" + reset + " retrying job 4411 (attempt 3)\r\n" +
		dim + "2026-10-26T12:04:41Z" + reset + " " + red + "worker" + reset + " " + bold + "panic: nil map assignment" + reset + "\r\n"
	demoNotes = "aider> 要約を書く\r\n\r\n" +
		"論文の主な貢献は三つあります:\r\n" +
		"  1. 低遅延の推論パイプライン\r\n" +
		"  2. " + bold + "キャッシュ" + reset + "を使った再計算の削減\r\n" +
		"  3. 評価用のベンチマーク公開\r\n\r\n" +
		dim + "aider> " + reset + "\r\n"
	demoExited = "probe: connecting to 127.0.0.1:64180…\r\n" + red + "probe: connection refused" + reset + "\r\n\r\n" +
		dim + "[process exited with code 1]" + reset + "\r\n"
)
