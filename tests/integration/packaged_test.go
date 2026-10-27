//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rexkit/tests/integration/harness"
)

// cellar lays out rexkit the way the Homebrew formula does, under prefix:
// Cellar/rexkit/<version>/libexec/{bin/rexkit,lua}, bin/rexkit linked to it,
// and opt/rexkit pointing at the version, whose libexec the binary is built
// to install. It returns the linked bin/rexkit.
func cellar(t *testing.T, prefix, version string) string {
	t.Helper()
	keg := filepath.Join(prefix, "Cellar", "rexkit", version)
	libexec := filepath.Join(keg, "libexec")
	opt := filepath.Join(prefix, "opt", "rexkit")
	ldflags := "-X rexkit/internal/install.Packaged=" + filepath.Join(opt, "libexec") + " -X main.releaseVersion=" + version
	build := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", filepath.Join(libexec, "bin", "rexkit"), "./cmd/rexkit")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the packaged rexkit: %v\n%s", err, out)
	}
	if out, err := exec.Command("cp", "-R", filepath.Join(root, "lua"), libexec).CombinedOutput(); err != nil {
		t.Fatalf("copying lua/: %v\n%s", err, out)
	}
	for _, dir := range []string{filepath.Dir(opt), filepath.Join(prefix, "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Remove(opt) // an upgrade moves opt/ to the new version
	if err := os.Symlink(keg, opt); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(prefix, "bin", "rexkit")
	_ = os.Remove(bin)
	if err := os.Symlink(filepath.Join(opt, "libexec", "bin", "rexkit"), bin); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestPackagedInstall installs rexkit from a Homebrew-style package into an
// isolated server, upgrades it, and opens Go to from it: the loader names
// opt/'s stable path, the binary is never built, and an upgrade needs only a
// reload.
func TestPackagedInstall(t *testing.T) {
	prefix := t.TempDir()
	bin := cellar(t, prefix, "9.9.1")
	s := harness.Start(t, harness.Options{ // the binary is the package's own
		Config: fmt.Sprintf("return dofile(%q)\n", filepath.Join(root, "lua", "init.lua")),
		Files:  map[string]string{"xdg/config_home/rexkit/settings.lua": `return { shell = "/bin/sh", editor = { "/usr/bin/vi" } }`},
		Unix:   true, // install reloads the config, which Rex takes only from its socket
	})
	env := append(append([]string{}, s.Env...), "REX_CONFIG="+s.ConfigPath(), "HOME="+t.TempDir())

	install := func() string {
		t.Helper()
		cmd := exec.Command(bin, "install")
		cmd.Dir = root // a checkout around the working directory does not win over the package
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rexkit install: %v\n%s", err, out)
		}
		return string(out)
	}
	out := install()
	for _, want := range []string{"9.9.1 ", "(packaged)", "installed", "reloaded", "packaged"} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q:\n%s", want, out)
		}
	}
	loader, err := os.ReadFile(s.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(prefix, "opt", "rexkit", "libexec", "lua", "init.lua"); !strings.Contains(string(loader), want) {
		t.Fatalf("init.lua does not load %s:\n%s", want, loader)
	}

	cellar(t, prefix, "9.9.2") // brew upgrade
	out = install()
	if !strings.Contains(out, "9.9.2 ") || !strings.Contains(out, "up to date") || !strings.Contains(out, "reloaded") {
		t.Errorf("upgrade output:\n%s", out)
	}

	work, blocks := s.CreateSession(t, "work", cat("alpha", ""))
	focus(t, s, work.ID, blocks[0])
	o := open(t, s, work.ID, blocks[0], "rexkit_goto", nil, "Search sessions")
	act(t, s, work.ID, o.Block, "rexkit_dismiss", map[string]string{"key": "escape"})
	waitNoFloats(t, s, work.ID)
}
