package hold

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// Main runs `rexkit hold` with the arguments after the subcommand.
func Main(args []string) error {
	fs := flag.NewFlagSet("hold", flag.ContinueOnError)
	_ = fs.String("title", "", "ignored: the headline names where the pane is parked (still passed by the Lua layer)")
	message := fs.String("message", "This pane is parked in Scratch", "what the card says; a line break (or \\n) starts a quieter second line")
	var hints hintList
	fs.Var(&hints, "hint", "a `KEY|WHAT` line such as \"ctrl+p e|bring it back\" (repeatable)")
	fs.Var(hintsJSON{&hints}, "hints", "hints as a `JSON` array of {\"key\":\"ctrl+p e\",\"label\":\"bring it back here\",\"gone\":\"close this pane\"} (gone: what the key does once the pane has exited)")
	since := fs.String("since", "", "when the pane was parked, as `UNIX_SECONDS` or RFC3339; shows \"parked 3m ago\"")
	session := fs.String("session", "", "session `ID` of --block (default $REX_SESSION)")
	block := fs.String("block", "", "show the live status of the parked pane's `BLOCK`")
	demo := fs.Bool("demo", false, "show a sample card; q quits")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}

	card := Card{Message: lines(*message), Hints: hints}
	if *since != "" {
		t, err := parseSince(*since)
		if err != nil {
			return fmt.Errorf("hold: --since: %w", err)
		}
		card.Since = t
	}
	var w watcher
	switch {
	case *demo:
		card = demoCard(card, given(fs))
		w = func(context.Context) statusMsg {
			return statusMsg{p: parked{known: true, state: ui.StatusWorking, app: "claude"}}
		}
	case *block != "":
		if *session == "" {
			*session = os.Getenv("REX_SESSION")
		}
		w = lazyWatch(*session, *block)
	}
	return run(newModel(card, w, *demo))
}

// run shows the screen until SIGTERM or SIGHUP. ⌃C, ⌃Z and ⌃\ are plain
// keys in raw mode, and the matching signals are ignored too, so nothing a
// user types can end the pane that holds a Scratch slot.
func run(m *model) error {
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	p := tea.NewProgram(m, tea.WithoutSignalHandler())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	go func() {
		if s, ok := <-stop; ok && s == syscall.SIGHUP {
			p.Kill() // the terminal is gone: nothing to restore
		} else if ok {
			p.Quit()
		}
	}()
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

// lazyWatch watches block, creating the Rex client on the first look so
// nothing but drawing happens before the first frame.
func lazyWatch(session, block string) watcher {
	var look watcher
	return func(ctx context.Context) statusMsg {
		if look == nil {
			api, err := rex.New()
			if err != nil {
				return statusMsg{err: err}
			}
			look = watch(api, session, block)
		}
		return look(ctx)
	}
}

// lines splits a message on line breaks, real or written as \n.
func lines(s string) []string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func given(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// demoCard fills in what a Scratch park would pass, unless given.
func demoCard(c Card, set map[string]bool) Card {
	if !set["message"] {
		c.Message = []string{"api is parked in Scratch", "It keeps running there."}
	}
	if !set["hint"] {
		c.Hints = []Hint{{"ctrl+p e", "bring it back here", "close this pane"}, {"ctrl+p w", "visit Scratch", ""}}
	}
	if !set["since"] {
		c.Since = time.Now().Add(-3 * time.Minute)
	}
	return c
}

// hintList collects repeated --hint flags.
type hintList []Hint

func (h *hintList) String() string {
	parts := make([]string, len(*h))
	for i, x := range *h {
		parts[i] = x.Key + "|" + x.Label
	}
	return strings.Join(parts, ", ")
}

func (h *hintList) Set(s string) error {
	hint := parseHint(s)
	if hint.Key == "" || hint.Label == "" {
		return fmt.Errorf("want KEY|WHAT, got %q", s)
	}
	*h = append(*h, hint)
	return nil
}

// hintsJSON reads --hints, the form the Lua layer passes:
// [{"key":"ctrl+p e","label":"bring it back here","gone":"close this pane"}, …].
type hintsJSON struct{ list *hintList }

func (h hintsJSON) String() string {
	if h.list == nil {
		return ""
	}
	return h.list.String()
}

func (h hintsJSON) Set(s string) error {
	var in []struct{ Key, Label, Gone string }
	if err := json.Unmarshal([]byte(s), &in); err != nil {
		return fmt.Errorf("want a JSON array of {key, label}: %w", err)
	}
	for _, x := range in {
		hint := Hint{Key: strings.TrimSpace(x.Key), Label: strings.TrimSpace(x.Label), Gone: strings.TrimSpace(x.Gone)}
		if hint.Key != "" && hint.Label != "" {
			*h.list = append(*h.list, hint)
		}
	}
	return nil
}

// parseSince reads Unix seconds (what Lua's os.time gives) or RFC3339.
func parseSince(s string) (time.Time, error) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(n, 0), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("want Unix seconds or RFC3339, got %q", s)
}
