//go:build integration

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var build struct {
	once sync.Once
	dir  string
	bin  string
	err  error
	out  []byte
}

// Run runs the tests and removes what Rexkit built. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(harness.Run(m)) }
func Run(m *testing.M) int {
	code := m.Run()
	if build.dir != "" {
		os.RemoveAll(build.dir)
	}
	return code
}

// Rexkit builds the working tree's cmd/rexkit once per test binary and
// returns its path, so integration tests never run a stale installed binary.
// Use Run in TestMain to remove it afterwards.
func Rexkit(t testing.TB) string {
	t.Helper()
	build.once.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			build.err = err
			return
		}
		build.dir, build.err = os.MkdirTemp("", "rexkit-it-")
		if build.err != nil {
			return
		}
		build.bin = filepath.Join(build.dir, "rexkit")
		cmd := exec.Command("go", "build", "-o", build.bin, "./cmd/rexkit")
		cmd.Dir = root
		build.out, build.err = cmd.CombinedOutput()
	})
	if build.err != nil {
		t.Fatalf("building rexkit: %v\n%s", build.err, build.out)
	}
	return build.bin
}

func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}
