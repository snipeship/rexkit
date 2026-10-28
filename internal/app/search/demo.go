package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
)

// The demo world: the sessions of Go to's demo (rexkit, studio,
// infrastructure-and-deployment-pipelines, 研究ノート, scratch), each pane
// with a scrollback that reads like a real developer's day — a go test run
// that panicked, an API server that recovered panics with request ids, a
// crash-looping worker, a production log, an agent's conversation, an rg.

const demoHome = "/Users/demo"

type demoPane struct {
	label   string // the pane's name in Rex (its label, else the program)
	program string
	cwd     string
	state   string // program status; "" for none
	task    string
	output  string
}

type demoTab struct {
	label string
	panes []demoPane
}

type demoSession struct {
	label string
	tabs  []demoTab
}

// demoOrigin is the pane the tool was opened from: rexkit › spark › zsh.
const demoOrigin = "rexkit/spark/zsh"

// demoWorld builds the fake Rex and returns it with the origin pane.
func demoWorld(now time.Time) (*rexfake.Fake, origin) {
	f := rexfake.New()
	ctx := context.Background()
	var o origin
	for _, s := range demoSessions() {
		sid, firstWin, firstBlock := f.AddSession(s.label)
		for ti, t := range s.tabs {
			var win string
			var blocks []string
			if ti == 0 {
				win, blocks = firstWin, []string{firstBlock}
				for range t.panes[1:] {
					c, _ := f.NewSplit(ctx, sid, rex.NewSplitRequest{
						AnchorID: blocks[len(blocks)-1], Direction: rex.Horizontal, Side: "after", Layout: rex.Shell("", ""),
					})
					blocks = append(blocks, c.BlockIDs[0])
				}
			} else {
				labels := make([]string, len(t.panes))
				win, blocks = f.AddWindow(sid, t.label, labels...)
			}
			_ = f.SetWindowLabel(ctx, sid, win, t.label)
			for pi, p := range t.panes {
				b := blocks[pi]
				name := p.label
				if name == "" {
					name = p.program
				}
				_ = f.SetBlockLabel(ctx, sid, b, name)
				term := rex.Terminal{Process: &rex.Process{Child: &rex.ProcessInfo{Name: p.program, Cwd: p.cwd}}}
				if p.state != "" {
					term.Status = rex.List[rex.StatusRecord]{{App: p.program, State: p.state, Title: p.task, UpdatedAt: now}}
				}
				f.SetTerminal(b, term)
				f.SetCapture(b, p.output)
				if s.label+"/"+t.label+"/"+name == demoOrigin {
					o = origin{session: sid, window: win, block: b}
				}
			}
		}
	}
	return f, o
}

func demoSessions() []demoSession {
	return []demoSession{
		{"rexkit", []demoTab{
			{"spark", []demoPane{
				{program: "claude", cwd: demoHome + "/Coding/rexkit", state: rex.StateWorking, task: "Fix the switcher panic", output: agentLog()},
				{program: "zsh", cwd: demoHome + "/Coding/rexkit", output: goTestLog()},
			}},
			{"docs", []demoPane{{program: "nvim", cwd: demoHome + "/Coding/rexkit/docs", output: editorScreen}}},
		}},
		{"studio", []demoTab{
			{"web", []demoPane{
				{program: "codex", cwd: demoHome + "/code/studio/web", state: rex.StateBlocked, task: "Navigation polish", output: codexLog},
				{program: "pnpm", cwd: demoHome + "/code/studio/web", output: viteLog()},
			}},
			{"api", []demoPane{
				{program: "zsh", cwd: demoHome + "/code/studio/api", output: apiLog()},
				{label: "migrations", program: "psql", cwd: demoHome + "/code/studio/api", output: psqlLog},
			}},
		}},
		{"infrastructure-and-deployment-pipelines", []demoTab{
			{"deploy", []demoPane{
				{program: "kubectl", cwd: demoHome + "/infra", output: kubectlLog()},
				{program: "gemini", cwd: demoHome + "/infra/terraform", state: rex.StateError, task: "Migrate terraform state", output: terraformLog},
			}},
			{"logs", []demoPane{{label: "tail -f production.log", program: "tail", cwd: demoHome + "/infra", output: productionLog()}}},
		}},
		{"研究ノート", []demoTab{
			{"論文", []demoPane{{program: "aider", cwd: demoHome + "/notes/論文", state: rex.StateIdle, task: "要約を書く", output: notesLog}}},
		}},
		{"scratch", []demoTab{
			{"probe", []demoPane{
				{program: "fish", cwd: "/tmp", output: rgLog()},
			}},
		}},
	}
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
	grey  = esc + "90m"
)

// lines joins output lines the way a terminal capture ends them, with
// tabs expanded to the terminal's tab stops (a capture has no tabs).
func lines(ls ...string) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		for {
			at := strings.IndexByte(l, '\t')
			if at < 0 {
				break
			}
			col := ansi.StringWidth(l[:at])
			l = l[:at] + strings.Repeat(" ", 8-col%8) + l[at+1:]
		}
		out[i] = l
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

// rng is a tiny deterministic generator, so the demo never changes.
type rng uint64

func (r *rng) next(n int) int {
	*r = *r*6364136223846793005 + 1442695040888963407
	return int((uint64(*r) >> 33) % uint64(n))
}

func hexID(r *rng) string {
	const digits = "0123456789abcdef"
	var b [8]byte
	for i := range b {
		b[i] = digits[r.next(16)]
	}
	return string(b[:])
}

// The request the stack traces are about: it panicked in the API, the
// worker crashed on its job, and the production log saw both.
const hotRequest = "8f2c41d7"

func zshPrompt(dir, branch, cmd string) string {
	return green + "➜" + reset + "  " + cyan + dir + reset + " " + blue + "git:(" + red + branch + blue + ")" + reset + " " + cmd
}

var goPackages = []string{
	"internal/match", "internal/panes", "internal/popup", "internal/rex", "internal/rex/rexfake",
	"internal/rextheme", "internal/state", "internal/ui", "internal/ui/sanitize", "internal/ui/snap",
	"internal/app/files", "internal/app/hold", "internal/app/keys", "internal/app/rename",
	"internal/app/scrollback", "internal/app/toast",
}

var goTests = []string{
	"TestArrange", "TestColumnsFit", "TestHeaderBand", "TestHeaderDropsChips", "TestKeyPanel", "TestListView",
	"TestListSections", "TestPreviewScroll", "TestTrayHints", "TestToastExpires", "TestEmptyState",
	"TestHighlight", "TestSanitizeNames", "TestTerminalLines", "TestLegible", "TestStepUp", "TestRaise",
	"TestCaptureTail", "TestSnapshot", "TestWatchEvents", "TestLocate", "TestKeymap",
}

// goTestLog is rexkit's zsh: a long verbose test run, then the switcher's
// panic with its stack trace, a fix, and a clean run.
func goTestLog() string {
	r := rng(7)
	var out []string
	out = append(out, zshPrompt("rexkit", "main", "git pull --rebase"),
		"Updating a622017..4eaf457", "Fast-forward",
		" internal/app/switcher/refresh.go | 31 "+green+"++++++++++++++"+red+"-----"+reset,
		" 1 file changed, 19 insertions(+), 12 deletions(-)",
		zshPrompt("rexkit", "main", "go test -v ./..."))
	for _, pkg := range goPackages {
		n := 18 + r.next(40)
		for i := 0; i < n; i++ {
			name := goTests[r.next(len(goTests))]
			sub := fmt.Sprintf("%s/case_%02d", name, i)
			out = append(out, "=== RUN   "+sub)
			out = append(out, fmt.Sprintf("--- "+green+"PASS"+reset+": %s (0.%02ds)", sub, r.next(9)))
		}
		out = append(out, green+"PASS"+reset, fmt.Sprintf("ok  \trexkit/%s\t%d.%03ds", pkg, r.next(2), r.next(999)))
	}
	out = append(out,
		"=== RUN   TestRefreshKeepsSelection",
		"--- "+red+"FAIL"+reset+": TestRefreshKeepsSelection (0.00s)",
		red+"panic: runtime error: invalid memory address or nil pointer dereference [recovered]"+reset,
		"        panic: runtime error: invalid memory address or nil pointer dereference",
		"[signal SIGSEGV: segmentation violation code=0x2 addr=0x18 pc=0x1029c4f50]",
		"",
		"goroutine 34 [running]:",
		"testing.tRunner.func1.2({0x102b1e1a0, 0x102e4a6c0})",
		"        /opt/homebrew/Cellar/go/1.27.0/libexec/src/testing/testing.go:1734 +0x1ac",
		"testing.tRunner.func1()",
		"        /opt/homebrew/Cellar/go/1.27.0/libexec/src/testing/testing.go:1737 +0x334",
		"panic({0x102b1e1a0?, 0x102e4a6c0?})",
		"        /opt/homebrew/Cellar/go/1.27.0/libexec/src/runtime/panic.go:792 +0x124",
		"rexkit/internal/app/switcher.(*model).refresh(0x14000a2c000, {0x0, 0x0})",
		"        "+demoHome+"/Coding/rexkit/internal/app/switcher/refresh.go:88 +0x3c",
		"rexkit/internal/app/switcher.(*model).Update(0x14000a2c000, {0x102b2e0c0, 0x14000190f60})",
		"        "+demoHome+"/Coding/rexkit/internal/app/switcher/model.go:214 +0x5f8",
		"rexkit/internal/app/switcher.TestRefreshKeepsSelection(0x14000183380)",
		"        "+demoHome+"/Coding/rexkit/internal/app/switcher/refresh_test.go:41 +0x1d0",
		"testing.tRunner(0x14000183380, 0x102e3c5d8)",
		"        /opt/homebrew/Cellar/go/1.27.0/libexec/src/testing/testing.go:1792 +0xe4",
		"created by testing.(*T).Run in goroutine 1",
		"        /opt/homebrew/Cellar/go/1.27.0/libexec/src/testing/testing.go:1851 +0x374",
		red+"FAIL"+reset+"\trexkit/internal/app/switcher\t0.412s",
		red+"FAIL"+reset,
		zshPrompt("rexkit", "main", "git stash list"),
		zshPrompt("rexkit", "main", "go test ./internal/app/switcher -run TestRefresh -count=1"),
		"ok  \trexkit/internal/app/switcher\t0.388s",
		zshPrompt("rexkit", "main", "git log --oneline -4"),
		yell+"9c1e2aa"+reset+" Fix panic in refresh before the first snapshot",
		yell+"4eaf457"+reset+" Open every tool over the whole window",
		yell+"cabad71"+reset+" Paint an opaque surface under every tool",
		yell+"d0cd502"+reset+" Mark handshake commits, hold an early ⏎",
		zshPrompt("rexkit", "main", ""),
	)
	return lines(out...)
}

// agentLog is the agent that found and fixed the panic.
func agentLog() string {
	return lines(
		grey+"╭"+strings.Repeat("─", 46)+"╮"+reset,
		grey+"│"+reset+" ✻ "+bold+"agent"+reset+" in the rexkit repository"+strings.Repeat(" ", 13)+grey+"│"+reset,
		grey+"│"+reset+"   cwd: ~/Coding/rexkit"+strings.Repeat(" ", 23)+grey+"│"+reset,
		grey+"╰"+strings.Repeat("─", 46)+"╯"+reset,
		"",
		"> the switcher tests panic since the refresh change, can you look?",
		"",
		cyan+"●"+reset+" I'll reproduce it first.",
		"",
		cyan+"●"+reset+" "+bold+"Bash"+reset+"(go test ./internal/app/switcher -run TestRefresh)",
		"  ⎿  --- FAIL: TestRefreshKeepsSelection (0.00s)",
		"     panic: runtime error: invalid memory address or nil pointer dereference [recovered]",
		"     "+dim+"… +21 lines (ctrl+r to expand)"+reset,
		"",
		cyan+"●"+reset+" The panic comes from refresh() reading m.inv before the first snapshot",
		"  has arrived: a refreshMsg can win the race against the first inventoryMsg.",
		"",
		cyan+"●"+reset+" "+bold+"Read"+reset+"(internal/app/switcher/refresh.go)",
		"  ⎿  Read 247 lines",
		"",
		cyan+"●"+reset+" "+bold+"Update"+reset+"(internal/app/switcher/refresh.go)",
		"  ⎿  Updated internal/app/switcher/refresh.go with "+green+"4 additions"+reset+" and "+red+"1 removal"+reset,
		"       86      func (m *model) refresh(msg refreshMsg) tea.Cmd {",
		"       87 "+green+"+        if m.inv == nil {"+reset,
		"       88 "+green+"+                return nil // no snapshot yet: nothing to refresh"+reset,
		"       89 "+green+"+        }"+reset,
		"",
		cyan+"●"+reset+" "+bold+"Bash"+reset+"(go test ./internal/app/switcher)",
		"  ⎿  ok   rexkit/internal/app/switcher  0.388s",
		"",
		cyan+"●"+reset+" Fixed: refresh() returns early until the first inventory arrives, so it",
		"  can no longer panic on a nil map. Added TestRefreshBeforeSnapshot.",
		"",
		"> ",
	)
}

// apiLog is the studio API server's log: requests with ids, and three
// panics the middleware recovered, one of them on the hot request.
func apiLog() string {
	r := rng(42)
	paths := []struct{ method, path string }{
		{"GET", "/api/projects"}, {"GET", "/api/projects/42"}, {"GET", "/api/projects/42/runs"},
		{"POST", "/api/projects/42/run"}, {"GET", "/api/me"}, {"PATCH", "/api/projects/17"}, {"GET", "/api/health"},
	}
	out := []string{
		zshPrompt("api", "feat/run-queue", "go run ./cmd/api --dev"),
		grey + "12:00:02" + reset + " " + green + "INF" + reset + " listening addr=:8080 env=dev",
	}
	h, m, s := 12, 0, 2
	tick := func() string {
		s += 1 + r.next(3)
		if s >= 60 {
			s, m = s-60, m+1
		}
		if m >= 60 {
			m, h = m-60, h+1
		}
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	request := func(at, method, path string, status int, id string) string {
		col := green
		if status >= 500 {
			col = red
		} else if status >= 400 {
			col = yell
		}
		return grey + at + reset + " " + green + "INF" + reset + fmt.Sprintf(" request method=%s path=%s status=%s%d%s dur=%dms request_id=%s",
			method, path, col, status, reset, 2+r.next(40), id)
	}
	panicAt := map[int]string{260: "c7d03e19", 610: hotRequest, 890: "0b5e9a62"}
	for i := 0; i < 960; i++ {
		at := tick()
		if id, ok := panicAt[i]; ok {
			out = append(out,
				grey+at+reset+" "+red+"ERR"+reset+" panic recovered request_id="+id+" method=POST path=/api/projects/42/run",
				"  panic: assignment to entry in nil map",
				"",
				"  goroutine 1187 [running]:",
				"  runtime/debug.Stack()",
				"          /opt/homebrew/Cellar/go/1.27.0/libexec/src/runtime/debug/stack.go:26 +0x64",
				"  studio/api/middleware.Recover.func1.1()",
				"          "+demoHome+"/code/studio/api/middleware/recover.go:24 +0x5c",
				"  panic({0x1031c2a40?, 0x1032f8e10?})",
				"          /opt/homebrew/Cellar/go/1.27.0/libexec/src/runtime/panic.go:792 +0x124",
				"  studio/api/runner.(*Queue).Enqueue(0x14000216f00, {0x14000a1e0c0, 0x24})",
				"          "+demoHome+"/code/studio/api/runner/queue.go:57 +0x1b8",
				"  studio/api/handlers.(*Projects).Run(0x140001a2120, {0x1033a6e88, 0x14000b2a0e0})",
				"          "+demoHome+"/code/studio/api/handlers/projects.go:131 +0x2f0",
				request(at, "POST", "/api/projects/42/run", 500, id),
			)
			continue
		}
		p := paths[r.next(len(paths))]
		status := 200
		switch {
		case p.method == "POST":
			status = 202
		case r.next(25) == 0:
			status = 404
		}
		out = append(out, request(at, p.method, p.path, status, hexID(&r)))
	}
	return lines(out...)
}

// kubectlLog is the crash-looping worker: its previous container's log
// (the panic on the hot request's job) and the pod's events.
func kubectlLog() string {
	return lines(
		"❯ kubectl get pods -n studio",
		bold+"NAME                      READY   STATUS             RESTARTS      AGE"+reset,
		"api-7d49b6c5f-x2k9p       1/1     "+green+"Running"+reset+"            0             3d",
		"web-9f7326d4b-pq8lm       1/1     "+green+"Running"+reset+"            0             3d",
		"worker-5c8d9f7b6-zz4nt    0/1     "+red+"CrashLoopBackOff"+reset+"   14 (2m ago)   52m",
		"❯ kubectl logs -n studio worker-5c8d9f7b6-zz4nt --previous",
		`{"level":"info","ts":"12:31:07","msg":"worker started","queue":"runs","concurrency":4}`,
		`{"level":"info","ts":"12:31:08","msg":"job claimed","job":4411,"request_id":"`+hotRequest+`","attempt":3}`,
		`{"level":"error","ts":"12:31:08","msg":"job failed","job":4411,"err":"panic: assignment to entry in nil map"}`,
		"panic: assignment to entry in nil map",
		"",
		"goroutine 52 [running]:",
		"studio/worker/jobs.(*RunJob).collect(0x140002c4a80, {0x1400031e1e0, 0x6})",
		"        /src/worker/jobs/run.go:94 +0x1a4",
		"studio/worker/jobs.(*RunJob).Do(0x140002c4a80, {0x1012a8d40, 0x1400012e0a0})",
		"        /src/worker/jobs/run.go:61 +0x2c8",
		"studio/worker/queue.(*Pool).work(0x14000110120, 0x2)",
		"        /src/worker/queue/pool.go:118 +0x1f0",
		"created by studio/worker/queue.(*Pool).Start in goroutine 1",
		"        /src/worker/queue/pool.go:72 +0x84",
		"❯ kubectl describe pod -n studio worker-5c8d9f7b6-zz4nt | tail -6",
		"Events:",
		"  Type     Reason   Age                  From     Message",
		"  ----     ------   ----                 ----     -------",
		"  Normal   Pulled   12m (x14 over 52m)   kubelet  Container image \"studio/worker:1.18.2\" already present",
		"  Warning  BackOff  2m (x231 over 51m)   kubelet  Back-off restarting failed container worker",
		"❯ ",
	)
}

// productionLog is the tail of production.log across services: requests,
// jobs, and the worker panicking on every retry.
func productionLog() string {
	r := rng(99)
	services := []string{"api", "web", "worker", "billing"}
	colours := map[string]string{"api": cyan, "web": blue, "worker": yell, "billing": mag}
	var out []string
	sec := 0
	stamp := func() string {
		sec += r.next(2)
		return fmt.Sprintf("2026-10-26T%02d:%02d:%02dZ", 11+sec/3600, sec/60%60, sec%60)
	}
	for i := 0; i < 2400; i++ {
		at := grey + stamp() + reset
		svc := services[r.next(len(services))]
		tag := colours[svc] + fmt.Sprintf("%-7s", svc) + reset
		switch {
		case i == 2210:
			out = append(out, at+" "+colours["api"]+"api    "+reset+" request_id="+hotRequest+" status=500 latency=3ms path=/api/projects/42/run")
		case i == 2211:
			out = append(out, at+" "+colours["worker"]+"worker "+reset+" job=4411 request_id="+hotRequest+" claimed attempt=3")
		case i%37 == 11:
			out = append(out, at+" "+colours["worker"]+"worker "+reset+" "+red+"panic: assignment to entry in nil map"+reset+fmt.Sprintf(" job=%d attempt=%d", 4300+i/37, 1+i%3))
		case svc == "worker":
			out = append(out, at+" "+tag+fmt.Sprintf(" job=%d done in %dms", 4300+r.next(120), 40+r.next(900)))
		default:
			status := 200
			if r.next(30) == 0 {
				status = 404
			}
			out = append(out, at+" "+tag+fmt.Sprintf(" request_id=%s status=%d latency=%dms", hexID(&r), status, 2+r.next(80)))
		}
	}
	return lines(out...)
}

// rgLog is a scratch fish searching the code for panics.
func rgLog() string {
	return lines(
		"/tmp> rg -n 'panic\\(' ~/Coding/rexkit --type go",
		mag+"internal/ui/columns.go"+reset,
		green+"212"+reset+":\t\tpanic(\"ui: two flex columns\")",
		mag+"internal/rex/rexfake/fake.go"+reset,
		green+"81"+reset+":\t\tpanic(\"rexfake: no session \" + sessionID)",
		mag+"internal/app/switcher/catalog.go"+reset,
		green+"143"+reset+":\tdefault: panic(fmt.Sprintf(\"switcher: unknown kind %d\", k))",
		"/tmp> ",
	)
}

// viteLog is the web app's dev server.
func viteLog() string {
	r := rng(5)
	out := []string{
		"> studio-web@0.18.2 dev /Users/demo/code/studio/web", "> vite", "",
		"  " + green + bold + "VITE v6.2.1" + reset + "  ready in " + bold + "412" + reset + " ms", "",
		"  " + green + "➜" + reset + "  " + bold + "Local" + reset + ":   " + cyan + "http://localhost:5173/" + reset,
	}
	files := []string{"src/nav/Navbar.tsx", "src/nav/NavLink.tsx", "src/routes/Projects.tsx", "src/app.css"}
	for i := 0; i < 60; i++ {
		out = append(out, grey+fmt.Sprintf("12:%02d:%02d", 10+i/6, (i*7)%60)+reset+" "+cyan+bold+"[vite]"+reset+" "+green+"hmr update"+reset+" "+dim+"/"+files[r.next(len(files))]+reset)
	}
	return lines(out...)
}

const editorScreen = blue + "  1 " + reset + bold + "# rexkit — design spec" + reset + "\r\n" +
	blue + "  2 " + reset + "\r\n" +
	blue + "  3 " + reset + "rexkit is one Go binary plus one thin Lua layer that together make Rex\r\n" +
	blue + "  4 " + reset + "feel like a first-class, keyboard-driven workspace.\r\n"

var codexLog = lines(
	mag+"codex"+reset+" "+dim+"· gpt-5 · ~/code/studio/web"+reset, "",
	"› Refine the navigation so the active route is obvious", "",
	green+"✓"+reset+" Read src/router.ts",
	green+"✓"+reset+" Rewrote src/nav/Navbar.tsx "+green+"+88"+reset+" "+red+"-51"+reset, "",
	bold+"Changes are ready."+reset,
	yell+"●"+reset+" Waiting for your review "+dim+"— approve with ⏎, or type feedback"+reset,
)

var psqlLog = lines(
	"studio=# "+bold+"select id, state from runs where request_id = '"+hotRequest+"';"+reset,
	"  id  |  state", "------+---------", " 4411 | failed", "(1 row)", "",
	"studio=# ",
)

var terraformLog = lines(
	mag+"gemini"+reset+" ❯ terraform state mv -state-out=gs://infra-state-v2", "",
	red+"Error:"+reset+" "+bold+"Error acquiring the state lock"+reset, "",
	"  Lock Info:", "    ID:        7d1c0e5a-1b2f-4f0c-9d5e-3a1e0f6b8c21", "    Who:       ci@runner-17", "",
	red+"✗"+reset+" Task failed after 3 attempts",
)

var notesLog = lines(
	"aider> 要約を書く", "",
	"論文の主な貢献は三つあります:",
	"  1. 低遅延の推論パイプライン",
	"  2. "+bold+"キャッシュ"+reset+"を使った再計算の削減",
	"  3. 評価用のベンチマーク公開", "",
	dim+"aider> "+reset,
)
