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

// findRoot locates the rexkit checkout: the explicit directory, else the
// first checkout above the working directory, else the one the running
// binary was built into (<root>/bin/rexkit).
func findRoot(explicit, cwd, exe string) (string, error) {
	if explicit != "" {
		root, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if err := checkCheckout(root); err != nil {
			return "", fmt.Errorf("%s is not a rexkit checkout: %w", root, err)
		}
		return root, nil
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
	return "", errors.New("no rexkit checkout here: run this inside the rexkit repository, or pass --root DIR")
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
