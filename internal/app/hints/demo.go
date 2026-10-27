package hints

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The demo pane: a developer's shell after a morning of work on a Go API —
// a search, a failing test with a panic, the branch's log, a health check
// that answers 503 and the containers behind it. Every kind of thing hints
// picks shows up in it, as it would on a real screen.

// span is text in one SGR style ("" for the default).
type span struct{ sgr, text string }

// vt encodes spans as Rex's vt format does: each run restates its style.
func vt(spans ...span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString("\x1b[0")
		if s.sgr != "" {
			b.WriteString(";" + s.sgr)
		}
		b.WriteString("m" + s.text)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func plain(s string) string { return vt(span{"", s}) }

func prompt(cmd string) string {
	return vt(span{"36", "~/code/studio/api"}, span{"", " "}, span{"35", "feat/run-queue"}, span{"", " "},
		span{"1;32", "$"}, span{"", " " + cmd})
}

// demoShell is the pane's screen at cols×rows: its last rows lines.
func demoShell(cols, rows int) string {
	const (
		dim  = "90"
		red  = "31"
		grn  = "32"
		yel  = "33"
		mag  = "35"
		bold = "1"
	)
	rg := func(file, line, text string) string {
		return vt(span{mag, file}, span{"", ":"}, span{grn, line}, span{"", ":" + text})
	}
	commit := func(hash, refs, msg string) string {
		s := []span{{yel, hash}, {"", " "}}
		if refs != "" {
			s = append(s, span{yel, "("}, span{"1;36", refs}, span{yel, ")"}, span{"", " "})
		}
		return vt(append(s, span{"", msg})...)
	}
	lines := []string{
		prompt("git pull --rebase"),
		plain("remote: Enumerating objects: 14, done."),
		plain("From github.com:snipeship/studio"),
		plain("   9a8d3e6..c2b77f0  main       -> origin/main"),
		plain("Successfully rebased and updated refs/heads/feat/run-queue."),
		prompt("make migrate"),
		plain("goose -dir db/migrations postgres up"),
		vt(span{dim, "2026/10/26 11:41:07"}, span{grn, " OK"}, span{"", "   20261025_run_worker_id.sql (6.81ms)"}),
		vt(span{dim, "2026/10/26 11:41:07"}, span{"", " goose: successfully migrated database to version: 20261025"}),
		prompt("gh pr create --fill --draft"),
		plain("Creating draft pull request for feat/run-queue into main in snipeship/studio"),
		"",
		plain("https://github.com/snipeship/studio/pull/482"),
		prompt("kubectl -n staging get pods -l app=studio-api"),
		plain("NAME                          READY   STATUS             RESTARTS      AGE"),
		vt(span{"", "studio-api-7d9f8b6c5d-x2k4p   0/1     "}, span{red, "CrashLoopBackOff"}, span{"", "   6 (41s ago)   12m"}),
		plain("studio-api-7d9f8b6c5d-q8m4z   1/1     Running            0             12m"),
		plain("studio-worker-5c8d7f9b4-h7t2n 1/1     Running            0             3h"),
		prompt("kubectl -n staging logs studio-api-7d9f8b6c5d-x2k4p --tail 3"),
		vt(span{dim, "2026-10-26T11:48:02Z"}, span{"", " INFO  listening on 10.42.3.17:8080"}),
		vt(span{dim, "2026-10-26T11:48:09Z"}, span{"", " INFO  connected to postgres at 10.42.0.9:5432"}),
		vt(span{dim, "2026-10-26T11:48:11Z"}, span{red, " ERROR"}, span{"", " drain: runs: pool closed (run 0b9e4c2f-6a1d-4f3e-9c55-2d7e81a0f4b6)"}),
		prompt("git status -sb"),
		vt(span{grn, "## feat/run-queue"}, span{"", "..."}, span{red, "origin/feat/run-queue"}, span{"", " [ahead 2]"}),
		vt(span{"", " "}, span{red, "M"}, span{"", " internal/runs/pool.go"}),
		vt(span{"", " "}, span{red, "M"}, span{"", " internal/runs/queue.go"}),
		vt(span{red, "??"}, span{"", " internal/runs/drain_test.go"}),
		prompt("rg -n ErrPoolClosed"),
		rg("internal/runs/pool.go", "27", `var ErrPoolClosed = errors.New("runs: pool closed")`),
		rg("internal/runs/pool.go", "118", "		return ErrPoolClosed"),
		rg("internal/runs/queue.go", "142", "	if errors.Is(err, ErrPoolClosed) {"),
		prompt("go test ./internal/runs/..."),
		vt(span{red, "--- FAIL: TestQueueDrainsOnShutdown (0.21s)"}),
		plain("    queue_test.go:88: drained 3 of 4 runs before the pool closed"),
		plain("    queue_test.go:91: run 0b9e4c2f-6a1d-4f3e-9c55-2d7e81a0f4b6 was left in the queue"),
		vt(span{red, "--- FAIL: TestRetryLostWorker (0.05s)"}),
		plain("    retry_test.go:54: got status \"queued\", want \"running\""),
		vt(span{bold, "panic: send on closed channel"}, span{"", " [recovered]"}),
		"",
		plain("goroutine 41 [running]:"),
		plain("github.com/snipeship/studio/api/internal/runs.(*Pool).Submit(0xc0001a2c40, {0x10c3e80, 0xc00012e1b0})"),
		plain("	/Users/sam/code/studio/api/internal/runs/pool.go:131 +0x1c4"),
		plain("github.com/snipeship/studio/api/internal/runs.(*Queue).drain(0xc0000b6a00)"),
		plain("	/Users/sam/code/studio/api/internal/runs/queue.go:142 +0x9e"),
		vt(span{red, "FAIL"}, span{"", "	github.com/snipeship/studio/api/internal/runs	0.412s"}),
		vt(span{grn, "ok  "}, span{"", "	github.com/snipeship/studio/api/internal/store	0.118s"}),
		prompt("git log --oneline -6"),
		commit("a3f9c21", "HEAD -> feat/run-queue", "Drain the queue before closing the pool"),
		commit("7d0e5b8", "", "Retry a run that lost its worker"),
		commit("e41c9aa", "", "Add the run endpoint to the projects API"),
		commit("c2b77f0", "origin/main, main", "Bump pgx to v5.7.1"),
		commit("9a8d3e6", "", "Log the worker id with every run"),
		commit("5f1e0c3", "", "Move run state into its own table"),
		prompt("curl -sI https://api.studio.dev/v1/health"),
		vt(span{bold, "HTTP/2 503"}),
		vt(span{dim, "content-type:"}, span{"", " application/json"}),
		vt(span{dim, "x-request-id:"}, span{"", " 4c1f0a9e-2b7d-4e8a-a6f3-91d5c07e2b14"}),
		vt(span{dim, "link:"}, span{"", ` <https://status.studio.dev/incidents/2291>; rel="incident"`}),
		prompt("docker ps"),
		plain("CONTAINER ID   IMAGE                STATUS                     PORTS                      NAMES"),
		plain("f3a9b2c4d1e8   postgres:16-alpine   Up 2 hours (healthy)       127.0.0.1:5432->5432/tcp   studio-db"),
		plain("8e7d6c5b4a39   redis:7.4            Up 2 hours                 127.0.0.1:6379->6379/tcp   studio-cache"),
		plain("1c2d3e4f5a6b   studio-api:dev       Exited (2) 4 minutes ago                              studio-api"),
		prompt("") + "\x1b[7m \x1b[0m",
	}
	for i, l := range lines {
		lines[i] = strings.ReplaceAll(l, "\t", "    ")
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for len(lines) < rows {
		lines = append([]string{""}, lines...)
	}
	return strings.Join(lines, "\n")
}

// demoAgent is a coding agent's pane in the same repo at cols×rows: it has
// found the failing test's cause, fixed it, run the tests and pushed. What
// you would want from it is the files it touched (to open at the line), the
// commit and the PR.
func demoAgent(cols, rows int) string {
	const (
		dim  = "90"
		grn  = "32"
		bold = "1"
		del  = "30;48;2;255;180;180"
		add  = "30;48;2;170;230;170"
	)
	tool := func(name, arg string) string {
		return vt(span{grn, "⏺ "}, span{bold, name}, span{"", "(" + arg + ")"})
	}
	out := func(text string) string { return vt(span{dim, "  ⎿  "}, span{"", text}) }
	diff := func(n, sign, sgr, code string) string {
		return vt(span{dim, "      " + n + " "}, span{sgr, sign + " " + code})
	}
	box := func(sgr string, inner string) []string {
		w := max(cols-2, 4)
		pad := w - 3 - ansi.StringWidth(ansi.Strip(inner))
		return []string{
			vt(span{sgr, "╭" + strings.Repeat("─", w-2) + "╮"}),
			vt(span{sgr, "│"}, span{"", " "}) + inner + vt(span{"", strings.Repeat(" ", max(pad, 0))}, span{sgr, "│"}),
			vt(span{sgr, "╰" + strings.Repeat("─", w-2) + "╯"}),
		}
	}
	lines := []string{
		vt(span{dim, "> "}, span{"", "go test is failing on the drain test, can you fix it and push?"}),
		"",
		plain("⏺ The pool closes while drain() still holds runs, so Submit sends on a"),
		plain("  closed channel. I'll make Submit refuse work once the pool is closed."),
		"",
		tool("Read", "internal/runs/queue.go"),
		out("Read 186 lines"),
		"",
		tool("Update", "internal/runs/pool.go"),
		out("Updated internal/runs/pool.go with 5 additions and 1 removal"),
		diff("127", " ", "", "func (p *Pool) Submit(ctx context.Context, r Run) error {"),
		diff("128", "-", del, "    p.jobs <- r"),
		diff("128", "+", add, "    select {"),
		diff("129", "+", add, "    case <-p.closed:"),
		diff("130", "+", add, "        return ErrPoolClosed"),
		diff("131", "+", add, "    case p.jobs <- r:"),
		diff("132", "+", add, "    }"),
		"",
		tool("Bash", "go test ./internal/runs/..."),
		out("ok    github.com/snipeship/studio/api/internal/runs    0.388s"),
		"",
		tool("Bash", `git commit -am "Refuse runs once the pool is closed" && git push`),
		out("[feat/run-queue 4b7e2d1] Refuse runs once the pool is closed"),
		vt(span{"", "     "}, span{dim, "To github.com:snipeship/studio.git  a3f9c21..4b7e2d1"}),
		"",
		plain("⏺ Fixed in internal/runs/pool.go:128, and drain at internal/runs/queue.go:142"),
		plain("  now finishes before Close. The tests pass; 4b7e2d1 is pushed to"),
		plain("  https://github.com/snipeship/studio/pull/482."),
		"",
	}
	tail := box(dim, vt(span{"", "> "})+"\x1b[7m \x1b[0m")
	tail = append(tail, vt(span{dim, "  ? for shortcuts"}))
	for len(lines)+len(tail) < rows {
		lines = append(lines, "")
	}
	lines = append(lines, tail...)
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	return strings.Join(lines, "\n")
}

// The demo's user and the folder its shell is in.
const (
	demoHome = "/Users/sam"
	demoDir  = demoHome + "/code/studio/api"
)

// demoSource is a demo pane drawn at whatever size the tool has, in the
// demo's folder, its cursor on the last row.
func demoSource(draw func(cols, rows int) string) Source {
	return Source{Screen: draw, Dir: demoDir, Home: demoHome}
}

// demoQuiet is a shell with nothing on it worth picking.
func demoQuiet(cols, rows int) string {
	lines := []string{prompt("make lint"), plain("0 issues."), prompt("") + "\x1b[7m \x1b[0m"}
	for len(lines) < rows {
		lines = append([]string{""}, lines...)
	}
	return strings.Join(lines[len(lines)-rows:], "\n")
}
