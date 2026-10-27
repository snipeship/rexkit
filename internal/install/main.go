package install

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"rexkit/internal/popup"
)

// Main is `rexkit install [--root DIR] [--dry-run]`.
func Main(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	root := fs.String("root", "", "rexkit's `DIR`, a clone or a packaged tree (default: this binary's own files, or the clone around the working directory or this binary)")
	dryRun := fs.Bool("dry-run", false, "show what would change, and check the new config, without changing anything")
	if err := parse(fs, args); err != nil {
		return err
	}
	return run(func(ctx context.Context, e *Env) error {
		return e.Install(ctx, Options{Root: *root, DryRun: *dryRun})
	})
}

// UninstallMain is `rexkit uninstall [--to FILE] [--dry-run]`.
func UninstallMain(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	to := fs.String("to", "", "restore `FILE` instead of the config backed up at the first install")
	dryRun := fs.Bool("dry-run", false, "show what would change, and check the old config, without changing anything")
	if err := parse(fs, args); err != nil {
		return err
	}
	return run(func(ctx context.Context, e *Env) error {
		return e.Uninstall(ctx, UninstallOptions{To: *to, DryRun: *dryRun})
	})
}

// DoctorMain is `rexkit doctor [--root DIR]`. It exits 1 when a check fails.
func DoctorMain(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	root := fs.String("root", "", "rexkit's `DIR`, a clone or a packaged tree (default: the installed one)")
	if err := parse(fs, args); err != nil {
		return err
	}
	return run(func(ctx context.Context, e *Env) error { return e.Doctor(ctx, *root) })
}

// parse parses the flags and refuses arguments: flag parsing stops at the
// first one, so `install x --dry-run` would otherwise drop --dry-run and run
// for real.
func parse(fs *flag.FlagSet, args []string) error {
	if err := popup.ParseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return nil
	}
	hint := "flags go before arguments; see --help"
	if fs.Name() == "uninstall" && strings.ContainsAny(fs.Arg(0), "/.") {
		hint = "to restore a file, use --to " + fs.Arg(0)
	}
	return fmt.Errorf("%s: unexpected argument %q (%s)", fs.Name(), fs.Arg(0), hint)
}

func run(fn func(context.Context, *Env) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A closed stdout (`rexkit install | head`) must not kill the installer
	// between writing the config and reloading or rolling back: writes then
	// fail with EPIPE, which the printer ignores.
	signal.Ignore(syscall.SIGPIPE)
	e, err := DefaultEnv(os.Stdout)
	if err != nil {
		return err
	}
	return fn(ctx, e)
}
