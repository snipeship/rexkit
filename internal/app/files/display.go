package files

import (
	"os"
	"path/filepath"
	"strings"

	"rexkit/internal/ui/sanitize"
)

// displayPath shows a path relative to home as ~/…, sanitized.
func displayPath(p, home string) string {
	switch {
	case home != "" && p == home:
		return "~"
	case home != "" && strings.HasPrefix(p, home+string(filepath.Separator)):
		p = "~" + p[len(home):]
	}
	return sanitize.Name(p)
}

// crumb is one breadcrumb segment and the folder it stands for.
type crumb struct {
	label string
	path  string
}

// crumbs splits a folder into breadcrumbs: "~ › Coding › rexkit", or
// "/ › usr › local" outside home.
func crumbs(dir, home string) []crumb {
	var out []crumb
	rest := dir
	switch {
	case home != "" && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))):
		out = append(out, crumb{"~", home})
		rest = strings.TrimPrefix(dir[len(home):], string(filepath.Separator))
	default:
		out = append(out, crumb{"/", string(filepath.Separator)})
		rest = strings.TrimPrefix(dir, string(filepath.Separator))
	}
	at := out[0].path
	for part := range strings.SplitSeq(rest, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		at = filepath.Join(at, part)
		out = append(out, crumb{sanitize.Name(part), at})
	}
	return out
}

// expandPath resolves what the user typed in the path field: ~ is home and a
// relative path is relative to base. The result is clean and absolute.
func expandPath(typed, base, home string) string {
	switch {
	case typed == "~":
		return home
	case strings.HasPrefix(typed, "~/"):
		typed = filepath.Join(home, typed[2:])
	case !filepath.IsAbs(typed):
		typed = filepath.Join(base, typed)
	}
	return filepath.Clean(typed)
}

// homeDir is the user's home, or "" when unknown.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(home)
}
