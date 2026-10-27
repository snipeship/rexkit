package install

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// buildTimeout bounds `go build`, which may download modules on a cold cache.
const buildTimeout = 5 * time.Minute

// Packaged is the rexkit tree of a binary built by a package manager, which
// sets it at build time:
//
//	go build -ldflags "-X rexkit/internal/install.Packaged=$(brew --prefix)/opt/rexkit/libexec"
//
// The tree holds lua/ and bin/rexkit, this binary; it is never built. A path
// that stays the same across upgrades (Homebrew's opt/) keeps the loader
// working after one.
var Packaged string

// findRoot locates the rexkit tree to install: the explicit directory (a
// checkout or a packaged tree), else the packaged tree of a packaged binary,
// else the first checkout above the working directory, else the one the
// running binary was built into (<root>/bin/rexkit).
func findRoot(explicit, cwd, exe, packaged string) (string, error) {
	if explicit != "" {
		root, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if err := checkCheckout(root); err != nil {
			if checkPackaged(root) == nil {
				return stable(root, packaged), nil
			}
			return "", fmt.Errorf("%s is neither a rexkit clone nor a packaged rexkit: %w", root, err)
		}
		return root, nil
	}
	if packaged != "" {
		if err := checkPackaged(packaged); err != nil {
			return "", fmt.Errorf("rexkit's files in %s are incomplete (%w): reinstall rexkit", packaged, err)
		}
		return packaged, nil
	}
	for _, start := range []string{cwd, filepath.Dir(exe)} {
		for dir := start; dir != "" && dir != "."; dir = filepath.Dir(dir) {
			if checkCheckout(dir) == nil {
				return dir, nil
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	return "", errors.New("rexkit's files were not found: run this inside a rexkit clone, or pass --root DIR")
}

// stable turns a packaged tree named by its versioned path (Homebrew's
// Cellar/rexkit/<version>/libexec) into the packaged path that leads there
// (opt/rexkit/libexec), which survives the upgrade that removes the version.
func stable(root, packaged string) string {
	if packaged == "" {
		return root
	}
	a, errA := filepath.EvalSymlinks(root)
	b, errB := filepath.EvalSymlinks(packaged)
	if errA == nil && errB == nil && a == b {
		return packaged
	}
	return root
}

// checkCheckout verifies that dir holds the rexkit module.
func checkCheckout(dir string) error {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return errors.New("no go.mod")
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if module, ok := strings.CutPrefix(strings.TrimSpace(scan.Text()), "module "); ok {
			if strings.TrimSpace(module) != "rexkit" {
				return fmt.Errorf("go.mod is module %s", module)
			}
			if _, err := os.Stat(filepath.Join(dir, "cmd", "rexkit")); err != nil {
				return errors.New("no cmd/rexkit")
			}
			return nil
		}
	}
	return errors.New("go.mod names no module")
}

// checkPackaged verifies that dir is a packaged rexkit tree: the Lua layer
// and the binary, no sources.
func checkPackaged(dir string) error {
	for _, f := range []string{filepath.Join("lua", "init.lua"), filepath.Join("bin", "rexkit")} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return errors.New("no " + f)
		}
	}
	return nil
}

// isCheckout reports whether root is a checkout, which install builds, rather
// than a packaged tree.
func isCheckout(root string) bool { return checkCheckout(root) == nil }

// probe runs a packaged tree's bin/rexkit once, as build does a new one, and
// returns the version it printed.
func (e *Env) probe(ctx context.Context, root string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	out, err := e.command(ctx, "running bin/rexkit", filepath.Join(root, "bin", "rexkit"), "version")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "rexkit "), nil
}

// build compiles <root>/bin/rexkit and runs it once, which also pays macOS's
// first-launch validation now instead of on the first key press. The new
// binary replaces the old one only after it ran, by an atomic rename (never
// rewriting a binary that may be running). It returns the version it printed.
func (e *Env) build(ctx context.Context, root string) (string, error) {
	if e.Go == "" {
		return "", errors.New("the go tool was not found: install Go 1.27 or newer, or add it to PATH")
	}
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(bin, ".rexkit-*")
	if err != nil {
		return "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := e.command(ctx, "go build", e.Go, "-C", root, "build", "-trimpath", "-o", tmp.Name(), "./cmd/rexkit"); err != nil {
		return "", err
	}
	out, err := e.command(ctx, "running the new bin/rexkit", tmp.Name(), "version")
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(bin, "rexkit")); err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))
	return strings.TrimPrefix(version, "rexkit "), nil
}
