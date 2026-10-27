package switcher

import (
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
)

// attentionWorld is the demo world of agent attention: a developer's late
// morning with agents in every state that needs them, each saying why (OSC
// 7501 kind and msg). Two are blocked (a permission prompt and a question),
// one failed, one finished unseen, and one finished and already looked at;
// one more reports nothing, so its state is guessed from its title.
func attentionWorld(now time.Time) (*rexfake.Fake, place, state.History) {
	return buildDemo(now, attentionSessions, []demoVisit{
		{"rexkit/spark/zsh", 0},
		{"studio/api/zsh", 4 * time.Minute},
		{"payments/billing/codex", 25 * time.Minute},
		{"研究ノート/論文/aider", 52 * time.Minute},
		{"infrastructure-and-deployment-pipelines/deploy/kubectl", 3 * time.Hour},
	})
}

var attentionSessions = []demoSession{
	{"rexkit", []demoTab{
		{"spark", []demoPane{
			{program: "claude", cwd: demoHome + "/Coding/rexkit", state: rex.StateWorking, task: "Revamp the switcher UI",
				msg: "Running go test ./internal/app/switcher", ago: 3 * time.Second, output: demoClaude},
			{program: "zsh", cwd: demoHome + "/Coding/rexkit", output: demoShell},
		}},
		{"docs", []demoPane{{program: "nvim", cwd: demoHome + "/Coding/rexkit/docs", output: demoEditor}}},
	}},
	{"studio", []demoTab{
		{"web", []demoPane{
			{program: "codex", cwd: demoHome + "/code/studio/web", state: rex.StateBlocked, kind: "permission",
				task: "Navigation polish", msg: "Allow Bash(pnpm test --filter nav)?", ago: 12 * time.Minute, output: demoCodexAsk},
			{program: "claude", cwd: demoHome + "/code/studio/web", state: rex.StateDone, task: "Fix navbar overflow on small screens",
				msg: "Navbar wraps cleanly at 375px · 3 files changed, 48 tests pass", ago: 2 * time.Minute, output: demoClaudeDone},
		}},
		{"api", []demoPane{
			{program: "zsh", cwd: demoHome + "/code/studio/api", output: demoServer},
			{program: "claude", cwd: demoHome + "/code/studio/api", state: rex.StateBlocked, kind: "question",
				task: "Rate-limit the public API", msg: "Use Redis or the in-memory limiter for /api/projects?", ago: 4 * time.Minute, output: demoClaudeQuestion},
			{label: "migrations", program: "psql", cwd: demoHome + "/code/studio/api", output: demoSQL},
		}},
	}},
	{"infrastructure-and-deployment-pipelines", []demoTab{
		{"deploy", []demoPane{
			{program: "kubectl", cwd: demoHome + "/infra", output: demoPods},
			{program: "gemini", cwd: demoHome + "/infra/terraform", state: rex.StateError, task: "Migrate terraform state to the new bucket",
				msg: "Error acquiring the state lock (held by ci@runner-17)", ago: 41 * time.Minute, output: demoFailure},
		}},
		{"logs", []demoPane{{label: "tail -f production.log (eu-west-1, all services)", program: "tail", cwd: demoHome + "/infra", output: demoLogs}}},
	}},
	{"payments", []demoTab{
		{"billing", []demoPane{
			{program: "codex", cwd: demoHome + "/code/payments", state: rex.StateDone, task: "Bump stripe-go to v82",
				msg: "Bumped stripe-go to v82 · go test ./... passes", ago: 80 * time.Minute, output: demoCodexBump},
		}},
		{"webhooks", []demoPane{
			{program: "claude", cwd: demoHome + "/code/payments/webhook", title: "⠙ Port the webhook handlers", output: demoClaude},
		}},
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

var (
	demoCodexAsk = mag + "codex" + reset + " " + dim + "· gpt-5-codex · ~/code/studio/web" + reset + "\r\n\r\n" +
		"› Polish the navigation: the active route should be obvious\r\n\r\n" +
		green + "✓" + reset + " Read src/nav/Navbar.tsx, src/router.ts\r\n" +
		green + "✓" + reset + " Edited src/nav/Navbar.tsx " + green + "+42" + reset + " " + red + "-18" + reset + "\r\n" +
		green + "✓" + reset + " Edited src/nav/useActiveRoute.ts " + green + "+12" + reset + " " + red + "-3" + reset + "\r\n\r\n" +
		bold + "Allow command?" + reset + "\r\n" +
		"  " + dim + "$" + reset + " pnpm test --filter nav\r\n\r\n" +
		cyan + "› 1. Yes, run it" + reset + "\r\n" +
		"  2. Yes, and don't ask again for " + bold + "pnpm test" + reset + "\r\n" +
		"  3. No, tell codex what to do instead\r\n"
	demoClaudeQuestion = cyan + "●" + reset + " There are two ways to rate-limit " + bold + "/api/projects" + reset + ":\r\n\r\n" +
		"  " + bold + "Redis" + reset + " keeps one budget across the 3 API replicas, but adds a\r\n" +
		"  dependency to docker-compose.yml and the deploy chart.\r\n" +
		"  " + bold + "In-memory" + reset + " (golang.org/x/time/rate) needs nothing new, but each\r\n" +
		"  replica counts on its own.\r\n\r\n" +
		bold + "Use Redis or the in-memory limiter for /api/projects?" + reset + "\r\n" +
		cyan + "❯ 1. Redis (shared across replicas)" + reset + "\r\n" +
		"  2. In-memory (per replica)\r\n" +
		"  3. Type something else\r\n"
	demoClaudeDone = cyan + "●" + reset + " The navbar no longer overflows below 400px.\r\n\r\n" +
		green + "✓" + reset + " Edit " + bold + "src/nav/Navbar.tsx" + reset + "  " + green + "+18" + reset + " " + red + "-6" + reset + "\r\n" +
		green + "✓" + reset + " Edit " + bold + "src/nav/Navbar.module.css" + reset + "  " + green + "+9" + reset + " " + red + "-2" + reset + "\r\n" +
		green + "✓" + reset + " Edit " + bold + "src/nav/Navbar.test.tsx" + reset + "  " + green + "+31" + reset + "\r\n" +
		green + "✓" + reset + " Bash " + bold + "pnpm test --filter nav" + reset + dim + "  48 passed (3.1s)" + reset + "\r\n\r\n" +
		"  Items now wrap onto a second row at 375px instead of scrolling\r\n" +
		"  sideways; the menu button takes over below 320px.\r\n\r\n" +
		dim + "> " + reset + "\r\n"
	demoCodexBump = mag + "codex" + reset + " " + dim + "· gpt-5-codex · ~/code/payments" + reset + "\r\n\r\n" +
		green + "✓" + reset + " go get github.com/stripe/stripe-go/v82@v82.1.0\r\n" +
		green + "✓" + reset + " Rewrote 14 imports from stripe-go/v79\r\n" +
		green + "✓" + reset + " go test ./...\r\n" +
		dim + "ok  \tpayments/billing\t0.412s\r\nok  \tpayments/webhook\t0.208s" + reset + "\r\n\r\n" +
		bold + "Bumped stripe-go to v82." + reset + "\r\n"
)
