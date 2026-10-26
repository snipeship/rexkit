package rex

import (
	"errors"
	"os/exec"
	"testing"
)

func TestLocateOrder(t *testing.T) {
	const bundle = "/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex"
	tests := []struct {
		name    string
		env     map[string]string
		path    string // what PATH lookup finds
		present []string
		want    string
	}{
		{"block env wins", map[string]string{"REX_BIN_DIR": "/rex/bin", "REX_BIN": "/other/rex"}, "/usr/bin/rex",
			[]string{"/rex/bin/rex", "/other/rex", "/usr/bin/rex"}, "/rex/bin/rex"},
		{"REX_BIN next", map[string]string{"REX_BIN_DIR": "/missing", "REX_BIN": "/other/rex"}, "/usr/bin/rex",
			[]string{"/other/rex", "/usr/bin/rex"}, "/other/rex"},
		{"PATH next", nil, "/usr/bin/rex", []string{"/usr/bin/rex", bundle}, "/usr/bin/rex"},
		{"server bundle in Application Support", map[string]string{"HOME": "/Users/me"}, "",
			[]string{"/Users/me/Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex", bundle},
			"/Users/me/Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex"},
		{"app bundle last", nil, "", []string{bundle}, bundle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			lookPath := func(string) (string, error) {
				if tt.path == "" {
					return "", exec.ErrNotFound
				}
				return tt.path, nil
			}
			ok := func(p string) bool {
				for _, q := range tt.present {
					if p == q {
						return true
					}
				}
				return false
			}
			got, err := locate(getenv, lookPath, ok)
			if err != nil || got != tt.want {
				t.Errorf("locate = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	_, err := locate(func(string) string { return "" }, func(string) (string, error) { return "", exec.ErrNotFound },
		func(string) bool { return false })
	if !errors.Is(err, ErrNoBinary) {
		t.Errorf("nothing present: err = %v", err)
	}
}
