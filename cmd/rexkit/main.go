// Command rexkit is the Rex workspace toolkit: floating tools (goto, files,
// hints, keys, rename, scrollback, search), helpers the Lua layer runs (hold,
// toast, await, attend), and the installer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"rexkit/internal/app/files"
	"rexkit/internal/app/hints"
	"rexkit/internal/app/hold"
	"rexkit/internal/app/keys"
	"rexkit/internal/app/rename"
	"rexkit/internal/app/scrollback"
	"rexkit/internal/app/search"
	"rexkit/internal/app/switcher"
	"rexkit/internal/app/toast"
	"rexkit/internal/attend"
	"rexkit/internal/popup"
	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

type command struct {
	name    string
	summary string
	group   string
	run     func(args []string) error
}

const (
	groupTools    = "Tools"
	groupSetup    = "Setup"
	groupOther    = "Other"
	groupInternal = "Internal (used by the Lua layer)"
)

func commands() []command {
	return []command{
		{"goto", "switch to a session, tab, pane or agent", groupTools, switcher.Main},
		{"files", "open a file or folder in a new tab, split or editor", groupTools, files.Main},
		{"hints", "label what is on a pane (URLs, paths, SHAs …) to copy, open or paste it", groupTools, hints.Main},
		{"keys", "show what every key does", groupTools, keys.Main},
		{"rename", "rename a pane, tab or session (--target pane|tab|session)", groupTools, rename.Main},
		{"scrollback", "view a pane's scrollback in Neovim", groupTools, scrollback.Main},
		{"search", "search the screen and scrollback of every pane", groupTools, search.Main},
		{"hold", "placeholder screen for a pane parked in Scratch", groupTools, hold.Main},
		{"toast", "show a short notification", groupTools, toast.Main},
		{"install", "install rexkit into ~/.config/rex", groupSetup, runInstall},
		{"uninstall", "restore the Rex config from before rexkit", groupSetup, runUninstall},
		{"doctor", "check the installation and explain problems", groupSetup, runDoctor},
		{"demo", "run a tool with sample data: demo <tool> [flags]", groupOther, demo},
		{"snap", "draw a tool in a scripted state to a PNG, for design review", groupOther, snapCmd},
		{"version", "print the version", groupOther, version},
		{"await", "wait for a tool's result (--state DIR --timeout 1500ms)", groupInternal, await},
		{"collect", "print a snapshot of everything Rex runs, as JSON", groupInternal, collect},
		{"attend", "watch agents and toast when one needs you; attend next|back for the agent keys (⌃⇧A, ⌃⌥⇧A)", groupInternal, attend.Main},
	}
}

func main() {
	ui.SetShapes(ui.DetectShapes(os.Getenv))
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		usage(stderr)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	cmd, ok := lookup(args[0])
	if !ok {
		fmt.Fprintf(stderr, "rexkit: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	if cmd.group == groupTools || cmd.name == "demo" {
		ui.UseUserThemes()
	}
	err := cmd.run(args[1:])
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	}
	// A flag mistake was already reported, with the usage, by the flag package.
	var reported interface{ Reported() bool }
	if !errors.As(err, &reported) || !reported.Reported() {
		fmt.Fprintln(stderr, "rexkit:", err)
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "-help" || arg == "--help"
}

func usage(w io.Writer) {
	fmt.Fprint(w, "rexkit — keyboard-driven workspace tools for Rex\n\nUsage: rexkit <command> [flags]\n")
	width := 0
	for _, c := range commands() {
		width = max(width, len(c.name))
	}
	group := ""
	for _, c := range commands() {
		if c.group != group {
			group = c.group
			fmt.Fprintf(w, "\n%s:\n", group)
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.name, c.summary)
	}
	fmt.Fprint(w, "\nRun \"rexkit <command> -h\" for a command's flags.\n")
}

// demo runs a tool with sample data instead of the live Rex state.
func demo(args []string) error {
	if len(args) == 0 {
		return errors.New("demo: name a tool, for example: rexkit demo goto")
	}
	cmd, ok := lookup(args[0])
	if !ok || cmd.group != groupTools {
		return fmt.Errorf("demo: %q is not a tool", args[0])
	}
	return cmd.run(append([]string{"--demo"}, args[1:]...))
}

func version(args []string) error {
	fmt.Println("rexkit", buildVersion())
	return nil
}

// releaseVersion is set by a release build: -ldflags "-X main.releaseVersion=0.1.0".
var releaseVersion string

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	switch {
	case !ok && releaseVersion != "":
		return releaseVersion
	case !ok:
		return "unknown"
	case releaseVersion != "":
		return releaseVersion + " " + info.GoVersion
	}
	v := info.Main.Version // a pseudo-version with the commit (and +dirty) when built from git
	if v == "" || v == "(devel)" {
		v = "devel"
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				v += " " + s.Value[:min(12, len(s.Value))]
			}
		}
	}
	return v + " " + info.GoVersion
}

func await(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return popup.Await(ctx, args, os.Stdout)
}

func collect(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	server := fs.String("server", "", "Rex server `ADDRESS` (default $REX_SERVER or the local server)")
	timing := fs.Bool("timing", false, "report how long collecting took on stderr")
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	client, err := rex.New(rex.WithServer(*server))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	inv, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	if *timing {
		fmt.Fprintf(os.Stderr, "collected %d sessions in %s\n", len(inv.Sessions), time.Since(start).Round(time.Millisecond))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}
