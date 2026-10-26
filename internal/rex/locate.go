package rex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrNoBinary means the rex CLI could not be found.
var ErrNoBinary = errors.New("rex: the rex command was not found (is Rex installed?)")

// Locate finds the rex CLI: $REX_BIN_DIR/rex (set inside every Rex block),
// $REX_BIN, PATH, then the app bundles.
func Locate() (string, error) {
	return locate(os.Getenv, exec.LookPath, isExecutable)
}

func locate(getenv func(string) string, lookPath func(string) (string, error), ok func(string) bool) (string, error) {
	var candidates []string
	if dir := getenv("REX_BIN_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "rex"))
	}
	candidates = append(candidates, getenv("REX_BIN"))
	if path, err := lookPath("rex"); err == nil {
		candidates = append(candidates, path)
	}
	if home := getenv("HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home,
			"Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex"))
	}
	candidates = append(candidates,
		"/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex",
		"/Applications/Rex.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex",
	)
	for _, c := range candidates {
		if c != "" && ok(c) {
			return c, nil
		}
	}
	return "", ErrNoBinary
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
