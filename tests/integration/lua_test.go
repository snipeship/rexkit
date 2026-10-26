//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"rexkit/internal/rex"
)

// TestLuaChecks runs lua/tests/check.lua, the Lua layer's own live checks,
// against an isolated server whose tools are the stand-in from
// lua/tests/standin (see lua/tests/README.md).
func TestLuaChecks(t *testing.T) {
	dir := t.TempDir()
	standin := filepath.Join(dir, "rexkit")
	build := exec.Command("go", "build", "-C", filepath.Join(root, "lua", "tests", "standin"), "-o", standin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in: %v\n%s", err, out)
	}
	log := filepath.Join(dir, "standin.log")
	s := startWith(t, fmt.Sprintf("return { shell = %q, binary = %q }", "/bin/sh", standin), "STANDIN_LOG="+log)

	args, _ := json.Marshal(map[string]any{"isolated": true, "log": log, "rex": s.Bin, "server": s.Addr})
	out := s.CLI(t, "--timeout", "5m", "do", filepath.Join(root, "lua", "tests", "check.lua"), "--json", "--args", string(args))
	var res struct {
		OK     bool              `json:"ok"`
		Passed []string          `json:"passed"`
		Failed map[string]string `json:"failed"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("check.lua: %v\n%s", err, out)
	}
	names := make([]string, 0, len(res.Failed))
	for name := range res.Failed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Errorf("%s: %s", name, res.Failed[name])
	}
	if !res.OK || len(res.Passed) == 0 {
		t.Errorf("check.lua: ok=%v, %d passed", res.OK, len(res.Passed))
	}
	t.Logf("%d Lua checks passed", len(res.Passed))
}

// TestLuaSettingsPrograms: a shell or editor that isn't there is reported in
// the diagnostics (Rex would start such a pane and it would vanish without a
// word); a missing shell falls back to a real one.
func TestLuaSettingsPrograms(t *testing.T) {
	s := startWith(t, `return { shell = "/nonexistent/nu", editor = { "/nonexistent/nvim" }, binary = "~/rexkit-test-bin" }`)
	out := s.CLI(t, "do", "rexkit_diagnostics", "--json")
	var diag struct {
		Warnings []string `json:"warnings"`
		Settings struct {
			Shell  string   `json:"shell"`
			Editor []string `json:"editor"`
			Binary string   `json:"binary"`
		} `json:"settings"`
	}
	if err := json.Unmarshal([]byte(out), &diag); err != nil {
		t.Fatalf("diagnostics: %v\n%s", err, out)
	}
	warned := func(part string) bool {
		for _, w := range diag.Warnings {
			if strings.Contains(w, part) {
				return true
			}
		}
		return false
	}
	if !warned("shell /nonexistent/nu is missing") || !warned("editor /nonexistent/nvim is missing") {
		t.Errorf("warnings %q, want the missing shell and editor", diag.Warnings)
	}
	if sh := diag.Settings.Shell; sh == "/nonexistent/nu" || !strings.HasPrefix(sh, "/") {
		t.Errorf("shell %q, want a fallback that exists", sh)
	}
	if b := diag.Settings.Binary; !strings.HasPrefix(b, "/") || strings.Contains(b, "~") || !strings.HasSuffix(b, "/rexkit-test-bin") {
		t.Errorf("binary %q, want ~/ expanded to the home directory", b)
	}
}

// TestLuaStrictCheck: `rexkit install` and `rexkit doctor` check the config
// with `rexkit_strict = true` ahead of the loader. A module that fails to
// load must then fail `rex config check`, while a normal load stays lenient
// (the other modules keep working).
func TestLuaStrictCheck(t *testing.T) {
	bin, err := rex.Locate()
	if err != nil {
		t.Skip("rex is not installed:", err)
	}
	dir := t.TempDir()
	co := filepath.Join(dir, "checkout")
	if err := os.MkdirAll(co, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-R", filepath.Join(root, "lua"), co).CombinedOutput(); err != nil {
		t.Fatalf("copying lua/: %v\n%s", err, out)
	}
	loader := fmt.Sprintf("return dofile(%q)\n", filepath.Join(co, "lua", "init.lua"))
	env := []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH"),
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "config"), "XDG_STATE_HOME=" + filepath.Join(dir, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(dir, "cache")}
	check := func(name, config string) (string, error) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "--autostart=false", "config", "check", path, "--json")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := check("strict.lua", "rexkit_strict = true\n"+loader); err != nil {
		t.Fatalf("strict check of a healthy layer: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(co, "lua", "bindings.lua"), []byte(`error("boom")`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := check("strict.lua", "rexkit_strict = true\n"+loader)
	if err == nil || !strings.Contains(out, "bindings.lua failed to load") || !strings.Contains(out, "boom") {
		t.Errorf("strict check of a broken module: err=%v, want an error naming bindings.lua\n%s", err, out)
	}
	if out, err := check("plain.lua", loader); err != nil {
		t.Errorf("a normal load must isolate the broken module: %v\n%s", err, out)
	}
}
