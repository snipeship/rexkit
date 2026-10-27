package hints

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// recorder is the part of Rex a pick uses: the splits it opens and what it
// types into the pane.
type recorder struct {
	rex.API
	splits []rex.NewSplitRequest
	writes []string
}

func (r *recorder) NewSplit(_ context.Context, _ string, req rex.NewSplitRequest) (rex.Created, error) {
	r.splits = append(r.splits, req)
	return rex.Created{BlockIDs: []string{"block:new"}}, nil
}

func (r *recorder) Write(_ context.Context, _, block, data string) error {
	r.writes = append(r.writes, block+" "+data)
	return nil
}

// fakeRun records the programs a pick runs; fail names the ones that fail
// (by their base name and first argument, "git cat-file").
type fakeRun struct {
	ran  []string
	fail map[string]bool
	out  map[string]string
}

func (f *fakeRun) run(_ context.Context, dir, stdin, name string, args ...string) ([]byte, error) {
	line := filepath.Base(name)
	if len(args) > 0 {
		line += " " + args[0]
	}
	f.ran = append(f.ran, strings.TrimSpace(line+" "+stdin))
	if f.fail[line] {
		return nil, errors.New(line + " failed")
	}
	return []byte(f.out[line]), nil
}

type world struct {
	w   worker
	api *recorder
	run *fakeRun
	dir string
}

func newWorld(t *testing.T, installed ...string) *world {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := &world{api: &recorder{}, run: &fakeRun{fail: map[string]bool{}, out: map[string]string{}}, dir: dir}
	wd.w = worker{api: wd.api, session: "session:1", origin: "block:pane", size: rex.Size{Columns: 120, Rows: 30},
		dir: dir, home: "/Users/sam", editor: []string{"/opt/homebrew/bin/nvim"}, run: wd.run.run,
		look: func(name string) (string, bool) {
			for _, n := range installed {
				if n == name {
					return "/usr/local/bin/" + name, true
				}
			}
			return "", false
		}}
	return wd
}

func (wd *world) file(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(wd.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func pick(a Action, kind Kind, text string) Pick {
	return Pick{Action: a, Match: Match{Text: text, Kind: kind}}
}

// argv is the command of the one split a pick opened.
func (wd *world) argv(t *testing.T) []string {
	t.Helper()
	if len(wd.api.splits) != 1 {
		t.Fatalf("%d splits, want 1", len(wd.api.splits))
	}
	s := wd.api.splits[0]
	if s.AnchorID != "block:pane" || !s.Focus || s.Layout.Block.Options.Cwd != wd.dir {
		t.Errorf("split %+v, want one beside the pane, focused, in %s", s, wd.dir)
	}
	return s.Layout.Block.Options.Command
}

func TestCopy(t *testing.T) {
	wd := newWorld(t)
	out := wd.w.do(context.Background(), pick(ActCopy, KindSHA, "a3f9c21"))
	if out != (outcome{ui.ToastOK, "Copied a3f9c21"}) || strings.Join(wd.run.ran, "|") != "pbcopy a3f9c21" {
		t.Fatalf("outcome %+v, ran %q", out, wd.run.ran)
	}
	wd.run.fail["pbcopy"] = true
	if out := wd.w.do(context.Background(), pick(ActCopy, KindSHA, "a3f9c21")); out.kind != ui.ToastErr || !strings.HasPrefix(out.text, "Couldn't copy") {
		t.Errorf("a failed copy: %+v", out)
	}
	// A toast draws {key:…} as keycaps: picked text cannot.
	if out := wd.w.do(context.Background(), pick(ActCopy, KindPath, "{key:x}")); out.text == "Copied {key:x}" {
		t.Errorf("toast text %q is not escaped", out.text)
	}
}

func TestPaste(t *testing.T) {
	wd := newWorld(t)
	out := wd.w.do(context.Background(), pick(ActPaste, KindPath, "internal/runs/pool.go:128"))
	if out.text != "" || strings.Join(wd.api.writes, "|") != "block:pane internal/runs/pool.go:128" || len(wd.run.ran) != 0 {
		t.Fatalf("outcome %+v, writes %q, ran %q", out, wd.api.writes, wd.run.ran)
	}
}

func TestOpenURL(t *testing.T) {
	wd := newWorld(t)
	out := wd.w.do(context.Background(), pick(ActOpen, KindURL, "https://example.com/a"))
	if out.kind != ui.ToastOK || strings.Join(wd.run.ran, "|") != "open https://example.com/a" {
		t.Fatalf("outcome %+v, ran %q", out, wd.run.ran)
	}
}

func TestOpenPath(t *testing.T) {
	tests := []struct {
		name, file, pane, text string // file and pane: under the repository's top
		installed              []string
		want                   string // the editor's arguments; "" copies instead
	}{
		{"relative to the pane", "internal/runs/pool.go", "", "internal/runs/pool.go:128:9", nil, "+call cursor(128,9) internal/runs/pool.go"},
		{"from the repository's top", "api/pool.go", "web", "api/pool.go:3", []string{"git"}, "+3 api/pool.go"},
		{"by the end of its path", "internal/runs/queue_test.go", "", "queue_test.go:88", nil, "+88 internal/runs/queue_test.go"},
		{"a folder", "internal/runs/pool.go", "", "./internal/runs/", nil, "./internal/runs/"},
		{"no such file", "other.go", "", "missing.go:1", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := newWorld(t, tt.installed...)
			wd.file(t, tt.file)
			top := wd.dir
			wd.dir = filepath.Join(top, tt.pane)
			wd.w.dir = wd.dir
			if err := os.MkdirAll(wd.dir, 0o755); err != nil {
				t.Fatal(err)
			}
			wd.run.out["git rev-parse"] = top + "\n"
			out := wd.w.do(context.Background(), pick(ActOpen, KindPath, tt.text))
			if tt.want == "" {
				if len(wd.api.splits) != 0 || out.kind != ui.ToastInfo || !strings.Contains(out.text, "No such file") {
					t.Fatalf("outcome %+v, splits %d: want a copy", out, len(wd.api.splits))
				}
				return
			}
			args := strings.Fields(tt.want)
			args[len(args)-1] = filepath.Join(top, args[len(args)-1])
			want := "/opt/homebrew/bin/nvim " + strings.Join(args, " ")
			if got := strings.Join(wd.argv(t), " "); got != want || out.text != "" {
				t.Errorf("argv %q, want %q (outcome %+v)", got, want, out)
			}
		})
	}
}

func TestOpenHex(t *testing.T) {
	t.Run("a commit", func(t *testing.T) {
		wd := newWorld(t, "git", "docker")
		wd.w.do(context.Background(), pick(ActOpen, KindSHA, "a3f9c21"))
		if got := strings.Join(wd.argv(t), " "); got != "/usr/bin/env LESS=R /usr/local/bin/git show a3f9c21" {
			t.Errorf("argv %q", got)
		}
	})
	t.Run("a container", func(t *testing.T) {
		wd := newWorld(t, "git", "docker")
		wd.run.fail["git cat-file"] = true
		wd.w.do(context.Background(), pick(ActOpen, KindSHA, "f3a9b2c4d1e8"))
		if got := strings.Join(wd.argv(t), " "); got != `/bin/sh -c "$@" 2>&1 | LESS=R less +F sh /usr/local/bin/docker logs -f f3a9b2c4d1e8` {
			t.Errorf("argv %q", got)
		}
	})
	t.Run("neither", func(t *testing.T) {
		wd := newWorld(t, "git", "docker")
		wd.run.fail["git cat-file"], wd.run.fail["docker container"] = true, true
		out := wd.w.do(context.Background(), pick(ActOpen, KindSHA, "f3a9b2c4d1e8"))
		if len(wd.api.splits) != 0 || out.kind != ui.ToastInfo || !strings.HasSuffix(out.text, "copied f3a9b2c4d1e8") {
			t.Errorf("outcome %+v, splits %d: want a copy", out, len(wd.api.splits))
		}
	})
}

func TestOpenPod(t *testing.T) {
	wd := newWorld(t, "kubectl")
	p := pick(ActOpen, KindPod, "studio-api-7d9f8b6c5d-x2k4p")
	p.Namespace = "staging"
	wd.w.do(context.Background(), p)
	want := `/bin/sh -c "$@" 2>&1 | LESS=R less sh /usr/local/bin/kubectl describe pod studio-api-7d9f8b6c5d-x2k4p --namespace=staging`
	if got := strings.Join(wd.argv(t), " "); got != want {
		t.Errorf("argv %q, want %q", got, want)
	}

	wd = newWorld(t)
	out := wd.w.do(context.Background(), p)
	if len(wd.api.splits) != 0 || !strings.HasPrefix(out.text, "kubectl is not installed") {
		t.Errorf("without kubectl: %+v", out)
	}
}

// TestOpenCopiesWhatNothingOpens: a UUID or an address has nothing to open
// it, so ⇧ copies it, and the toast says why.
func TestOpenCopiesWhatNothingOpens(t *testing.T) {
	for k, want := range map[Kind]string{KindUUID: "Nothing opens an id · copied 10.0.0.1:80",
		KindIP: "Nothing opens an address · copied 10.0.0.1:80"} {
		wd := newWorld(t, "git")
		if out := wd.w.do(context.Background(), pick(ActOpen, k, "10.0.0.1:80")); out != (outcome{ui.ToastInfo, want}) || len(wd.api.splits) != 0 {
			t.Errorf("%v: %+v", k, out)
		}
	}
}

// TestSplitDirection: right of a wide pane, below a tall one.
func TestSplitDirection(t *testing.T) {
	for _, tt := range []struct {
		size rex.Size
		want string
	}{
		{rex.Size{Columns: 120, Rows: 30}, rex.Horizontal},
		{rex.Size{Columns: 60, Rows: 50}, rex.Vertical},
		{rex.Size{Columns: 100, Rows: 40, CellWidthPx: 14, CellHeightPx: 29}, rex.Horizontal},
		{rex.Size{Columns: 80, Rows: 40, CellWidthPx: 14, CellHeightPx: 29}, rex.Vertical},
	} {
		wd := newWorld(t)
		wd.w.size = tt.size
		wd.w.split(context.Background(), "x", "/bin/true")
		if got := wd.api.splits[0].Direction; got != tt.want {
			t.Errorf("%+v: %s, want %s", tt.size, got, tt.want)
		}
	}
}
