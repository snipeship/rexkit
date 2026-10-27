package attend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
)

// world is an inventory of two sessions: "me" (a shell, the user's pane)
// and agents in every state, with their records' times.
func world() rex.Inventory {
	term := func(app, st, kind, msg string, ago time.Duration) *rex.Terminal {
		return &rex.Terminal{Process: proc(app, "/bin/"+app, ""),
			Status: rex.List[rex.StatusRecord]{{App: app, State: st, Kind: kind, Msg: msg, UpdatedAt: t0.Add(-ago)}}}
	}
	block := func(id string, t *rex.Terminal) rex.Block {
		return rex.Block{ID: id, Creator: rex.TerminalCreator, Terminal: t}
	}
	return rex.Inventory{Sessions: rex.List[rex.SessionView]{
		{Order: 1, View: rex.View{SessionID: "s2", Label: "infrastructure-and-deployment-pipelines", ActiveWindowID: "w3",
			Windows: rex.List[rex.Window]{{ID: "w3", Label: "deploy", FocusedBlockID: "err", Layers: rex.List[rex.Layer]{{Kind: rex.LayerTiled,
				Blocks: rex.List[rex.Block]{block("err", term("gemini", rex.StateError, "", "state lock", 41*time.Minute))}}}}}}},
		{Order: 0, View: rex.View{SessionID: "s1", Label: "studio", ActiveWindowID: "w1", Windows: rex.List[rex.Window]{
			{ID: "w1", Label: "web", FocusedBlockID: "me", Layers: rex.List[rex.Layer]{
				{Kind: rex.LayerTiled, Blocks: rex.List[rex.Block]{
					block("me", &rex.Terminal{Process: proc("zsh", "/bin/zsh", "")}),
					block("perm", term("codex", rex.StateBlocked, "permission", "Allow Bash(pnpm test)?", 12*time.Minute)),
					block("done", term("claude", rex.StateDone, "", "Navbar fixed", 2*time.Minute)),
					block("work", term("claude", rex.StateWorking, "", "", time.Second)),
				}},
				{Kind: rex.LayerFloating, Blocks: rex.List[rex.Block]{{ID: "toast", Label: "rexkit:toast", Terminal: &rex.Terminal{}}}},
			}},
			{ID: "w2", Label: "Window 0a1b2c3d", Layers: rex.List[rex.Layer]{{Kind: rex.LayerTiled, Blocks: rex.List[rex.Block]{
				block("q", &rex.Terminal{Process: &rex.Process{Foreground: &rex.ProcessInfo{Name: "claude", Path: "/bin/claude", Cwd: "/Users/me/code/api"}},
					Status: rex.List[rex.StatusRecord]{{App: "claude", State: rex.StateBlocked, Kind: "question", Msg: "Redis?", UpdatedAt: t0.Add(-4 * time.Minute)}}}),
				block("guess", &rex.Terminal{Process: proc("claude", "/bin/claude", ""), Title: "⠙ porting"}),
			}}}},
		}}},
	}}
}

func blocks(panes []Pane) []string {
	var out []string
	for _, p := range panes {
		out = append(out, p.Block)
	}
	return out
}

func TestPanes(t *testing.T) {
	panes := Panes(world(), Memory{}, t0)
	if got, want := blocks(panes), []string{"perm", "done", "work", "q", "err"}; !slices.Equal(got, want) {
		t.Fatalf("panes = %q, want %q (session order, no shell, no guess, no tool)", got, want)
	}
	if panes[3].Where != "studio › api" || panes[4].Where != "infrastructure-and-deployment-pipelines › deploy" {
		t.Errorf("where = %q, %q; want the folder for an unnamed tab", panes[3].Where, panes[4].Where)
	}
	if got, want := blocks(Waiting(panes, t0)), []string{"perm", "q", "err", "done"}; !slices.Equal(got, want) {
		t.Errorf("waiting = %q, want %q (blocked oldest first, failed, done)", got, want)
	}
}

func TestMemoryKeepsSinceAndSeen(t *testing.T) {
	var mem Memory
	mem.Attention.Enter("done", rex.StateDone, "", t0.Add(-time.Hour)) // the watcher saw it finish an hour ago
	mem.History.Visit(state.Visit{Kind: "agent", SessionID: "s1", BlockID: "done", At: t0.Add(-30 * time.Minute)})
	for _, p := range Panes(world(), mem, t0) {
		if p.Block == "done" && (!p.Since.Equal(t0.Add(-time.Hour)) || p.Unseen(t0)) {
			t.Errorf("done = since %v, unseen %v; want the watcher's time and seen by the visit", p.Since, p.Unseen(t0))
		}
	}
	if Unseen(rex.StateDone, t0.Add(-DoneFor), time.Time{}, t0) {
		t.Error("a result nobody looked at for a day still needs you")
	}
	if !NeedsYou(rex.StateBlocked, false) || NeedsYou(rex.StateDone, false) || NeedsYou(rex.StateWorking, true) {
		t.Error("NeedsYou: blocked always, done only unseen, working never")
	}
}

func TestNextWalksUnseenFirstThenRound(t *testing.T) {
	panes := Panes(world(), Memory{}, t0)
	var order []string
	at := "me"
	for range 6 {
		p, ok := Next(panes, at, t0)
		if !ok {
			t.Fatal("Next found nothing")
		}
		order = append(order, p.Block)
		for i := range panes {
			if panes[i].Block == p.Block {
				panes[i].Seen = t0 // landing there is a look
			}
		}
		at = p.Block
	}
	// done leaves once seen; the blocked and failed stay and go round
	if got, want := strings.Join(order, " "), "perm q err done perm q"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

func TestNextJumpAndBack(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	inv := world()
	me := state.Visit{Kind: "pane", SessionID: "s1", WindowID: "w1", BlockID: "me", At: t0}
	keys := Keys{Next: "alt+a", Back: "alt+shift+a"}
	j, err := NextJump(d, inv, me, keys, t0)
	if err != nil || j.BlockID != "perm" || j.WindowID != "w1" || j.Toast == nil {
		t.Fatalf("first ⌥A = %+v, %v; want perm with a toast", j, err)
	}
	if !strings.HasPrefix(j.Toast.Message, "{b:3 more} need you · next: claude asks") || j.Toast.Kind != "info" {
		t.Errorf("toast = %+v", j.Toast)
	}
	at := state.Visit{Kind: "pane", SessionID: j.SessionID, WindowID: j.WindowID, BlockID: j.BlockID, At: t0}
	j, _ = NextJump(d, inv, at, keys, t0.Add(time.Second))
	if j.BlockID != "q" {
		t.Fatalf("second ⌥A went to %q, want q", j.BlockID)
	}
	a := d.Attention()
	if a.Back == nil || a.Back.BlockID != "me" || a.SeenAt("perm").IsZero() {
		t.Fatalf("attention = %+v; want the walk to remember me and perm seen", a)
	}
	back, err := BackJump(d, inv, state.Visit{SessionID: "s1", WindowID: "w2", BlockID: "q"}, t0.Add(2*time.Second))
	if err != nil || back.BlockID != "me" {
		t.Fatalf("⌥⇧A = %+v, %v; want back to me", back, err)
	}
	again, _ := BackJump(d, inv, state.Visit{SessionID: "s1", WindowID: "w1", BlockID: "me"}, t0.Add(3*time.Second))
	if again.BlockID != "q" {
		t.Errorf("⌥⇧A again = %+v, want back to q", again)
	}
}

func TestNextJumpWithNothingWaiting(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	j, err := NextJump(d, rex.Inventory{}, state.Visit{SessionID: "s1", BlockID: "me"}, Keys{}, t0)
	if err != nil || j.SessionID != "" || j.Toast == nil || j.Toast.Message != "No agent needs you" {
		t.Errorf("⌥A with nothing waiting = %+v, %v", j, err)
	}
	b, _ := BackJump(d, rex.Inventory{}, state.Visit{SessionID: "s1", BlockID: "me"}, t0)
	if b.SessionID != "" || b.Toast == nil {
		t.Errorf("⌥⇧A with no walk = %+v", b)
	}
}

func TestBackFallsBackToTheTab(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	inv := world()
	_ = d.UpdateAttention(t0, func(a *state.Attention) {
		a.Back = &state.Visit{SessionID: "s1", WindowID: "w2", BlockID: "closed"}
	})
	j, _ := BackJump(d, inv, state.Visit{SessionID: "s2", BlockID: "err"}, t0)
	if j.SessionID != "s1" || j.WindowID != "w2" || j.BlockID != "" {
		t.Errorf("back to a closed pane = %+v, want its tab", j)
	}
}

// ⌥A on the only agent that needs the user: none other does, and the toast
// says so rather than that none does.
func TestNextJumpFromTheOnlyOneWaiting(t *testing.T) {
	d := state.Dirs{State: t.TempDir()}
	inv := world()
	inv.Sessions = inv.Sessions[:1] // only the failed agent, in s2
	j, err := NextJump(d, inv, state.Visit{SessionID: "s2", WindowID: "w3", BlockID: "err"}, Keys{}, t0)
	if err != nil || j.SessionID != "" || j.Toast == nil || j.Toast.Message != "No other agent needs you" {
		t.Errorf("⌥A on the only agent waiting = %+v, %v", j, err)
	}
}

// A click on a toast goes to its agent: seen, focused, and the app shows
// its session and tab when it shows another one and its client is known.
func TestVisit(t *testing.T) {
	tests := []struct {
		name, client, from, fromTab string
		remote                      bool
		want                        string // the client.perform recorded, if any
		seen                        bool   // the user got there
	}{
		{"same tab", "app-1", "S", "W", true, "", true},
		{"same session, tab unknown", "app-1", "S", "", true, "", true},
		{"other tab", "app-1", "S", "other", true, `{"session_id":"S","window_id":"W"}`, true},
		{"other tab, no client", "", "S", "other", true, "", false},
		{"other session, no client", "", "elsewhere", "", true, "", false},
		{"other session", "app-1", "elsewhere", "", true, `{"session_id":"S","window_id":"W"}`, true},
		{"other session, Remote Control off", "app-1", "elsewhere", "", false, `{"session_id":"S","window_id":"W"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := rexfake.New()
			f.RemoteControl = tt.remote
			sid, _, _ := f.AddSession("agents")
			wid, blocks := f.AddWindow(sid, "api", "shell", "agent")
			if tt.from == "S" {
				tt.from = sid
			}
			if tt.fromTab == "W" {
				tt.fromTab = wid
			}
			d := state.Dirs{State: t.TempDir()}
			to := state.Visit{SessionID: sid, WindowID: wid, BlockID: blocks[1]}
			err := Visit(context.Background(), f, d, to, tt.client, state.Visit{SessionID: tt.from, WindowID: tt.fromTab})
			if tt.remote && err != nil || !tt.remote && !errors.Is(err, rex.ErrRefused) {
				t.Errorf("Visit = %v", err)
			}
			if seen := !d.Attention().SeenAt(blocks[1]).IsZero(); seen != tt.seen {
				t.Errorf("the agent marked seen: %v, want %v (only where the user got there)", seen, tt.seen)
			}
			var focused, performed []string
			for _, c := range f.Calls() {
				switch c.Method {
				case "session.focus_block":
					focused = append(focused, c.Args[1])
				case "client.perform":
					performed = append(performed, strings.Join(c.Args, " "))
				}
			}
			if want := []string{blocks[1]}; tt.seen && !slices.Equal(focused, want) || !tt.seen && focused != nil {
				t.Errorf("focused %q; the agent only where the user gets there", focused)
			}
			want := []string(nil)
			if tt.want != "" {
				want = []string{"app-1 session.select " + strings.ReplaceAll(strings.ReplaceAll(tt.want, `"S"`, `"`+sid+`"`), `"W"`, `"`+wid+`"`)}
			}
			if !slices.Equal(performed, want) {
				t.Errorf("performed %q, want %q", performed, want)
			}
		})
	}
}
