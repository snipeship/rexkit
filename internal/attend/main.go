package attend

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// toastStates maps the words of the agent_toast_states setting to OSC 7501
// states.
var toastStates = map[string]string{"blocked": rex.StateBlocked, "failed": rex.StateError, "done": rex.StateDone}

// Main runs `rexkit attend`: the watcher the Lua layer starts for each
// config load, or, with next or back, the work of ⌥A and ⌥⇧A.
func Main(args []string) error {
	if len(args) > 0 && (args[0] == "next" || args[0] == "back") {
		return jumpMain(args[0], args[1:])
	}
	fs := flag.NewFlagSet("attend", flag.ContinueOnError)
	token := fs.String("token", "", "the `TOKEN` of the config load that starts the watcher (lua/attend.lua)")
	nextKey := fs.String("next-key", "alt+a", "the `KEY` toasts name for going to the agent")
	toasts := fs.String("toasts", "blocked,failed,done", "the `STATES` that toast, comma-separated (blocked, failed, done); empty for none")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if *token == "" {
		return errors.New("attend: --token is required: rexkit's Lua layer starts the watcher")
	}
	var states []string
	for _, word := range strings.Split(*toasts, ",") {
		if word = strings.TrimSpace(word); word == "" {
			continue
		}
		st, ok := toastStates[word]
		if !ok {
			return fmt.Errorf("attend: --toasts: %q is not blocked, failed or done", word)
		}
		states = append(states, st)
	}
	dirs, err := state.Default()
	if err != nil {
		return err
	}
	client, err := rex.New()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stop()
	return Watch(ctx, client, dirs, WatchOptions{Token: *token, NextKey: *nextKey, Toasts: states})
}

// jumpTimeout bounds ⌥A's work: a snapshot and the attention state.
const jumpTimeout = 8 * time.Second

// jumpMain prints where ⌥A (next) or ⌥⇧A (back) goes from the pane the
// user is on, as JSON (Jump), for the Lua layer to carry out.
func jumpMain(which string, args []string) error {
	fs := flag.NewFlagSet("attend "+which, flag.ContinueOnError)
	var here state.Visit
	fs.StringVar(&here.SessionID, "session", "", "the `ID` of the session the user is in")
	fs.StringVar(&here.WindowID, "window", "", "the `ID` of the tab the user is in")
	fs.StringVar(&here.BlockID, "block", "", "the `ID` of the pane the user is on")
	var keys Keys
	fs.StringVar(&keys.Next, "next-key", "alt+a", "the `KEY` toasts name for the next agent")
	fs.StringVar(&keys.Back, "back-key", "alt+shift+a", "the `KEY` toasts name for going back")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	dirs, err := state.Default()
	if err != nil {
		return err
	}
	client, err := rex.New()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), jumpTimeout)
	defer cancel()
	inv, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	here.Kind, here.At = "pane", now
	var j Jump
	if which == "next" {
		j, err = NextJump(dirs, inv, here, keys, now)
	} else {
		j, err = BackJump(dirs, inv, here, now)
	}
	if err != nil { // the jump stands even when its memory could not be written
		fmt.Fprintln(os.Stderr, "rexkit attend:", err)
	}
	return json.NewEncoder(os.Stdout).Encode(j)
}
