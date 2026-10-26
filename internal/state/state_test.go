package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestDirsFrom(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Dirs
	}{
		{"defaults", map[string]string{"HOME": "/h"},
			Dirs{State: "/h/.local/state/rexkit", Cache: "/h/.cache/rexkit", Config: "/h/.config/rexkit"}},
		{"xdg", map[string]string{"HOME": "/h", "XDG_STATE_HOME": "/s", "XDG_CACHE_HOME": "/c", "XDG_CONFIG_HOME": "/cfg"},
			Dirs{State: "/s/rexkit", Cache: "/c/rexkit", Config: "/cfg/rexkit"}},
		{"relative xdg is ignored", map[string]string{"HOME": "/h", "XDG_STATE_HOME": "rel"},
			Dirs{State: "/h/.local/state/rexkit", Cache: "/h/.cache/rexkit", Config: "/h/.config/rexkit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DirsFrom(func(k string) string { return tt.env[k] })
			if err != nil || got != tt.want {
				t.Errorf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
	if _, err := DirsFrom(func(string) string { return "" }); err == nil {
		t.Error("no HOME should fail")
	}
}

func testDirs(t *testing.T) Dirs {
	root := t.TempDir()
	return Dirs{State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
}

func TestWriteJSONIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "dir", "x.json")
	if err := WriteJSON(path, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(path, map[string]int{"a": 2}); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := ReadJSON(path, &got); err != nil || got["a"] != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v", info.Mode().Perm())
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", dir.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
	if err := WriteJSON(path, func() {}); err == nil {
		t.Error("unencodable value should fail")
	}
}

func TestCache(t *testing.T) {
	d := testDirs(t)
	var v []string
	if _, err := d.ReadCache(SnapshotCache, &v); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing cache: %v", err)
	}
	if err := d.WriteCache(SnapshotCache, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	at, err := d.ReadCache(SnapshotCache, &v)
	if err != nil || len(v) != 1 || time.Since(at) > time.Minute {
		t.Fatalf("got %v at %v, %v", v, at, err)
	}
}

func TestToolDirsAndGC(t *testing.T) {
	d := testDirs(t)
	now := time.Now()
	mk := func(owner *Owner, age time.Duration) string {
		dir, err := d.NewToolDir()
		if err != nil {
			t.Fatal(err)
		}
		if owner != nil {
			if err := Claim(dir, *owner); err != nil {
				t.Fatal(err)
			}
		}
		old := now.Add(-age)
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	fresh := mk(nil, time.Minute)
	old := mk(nil, 25*time.Hour)
	live := mk(&Owner{SessionID: "s", BlockID: "live"}, time.Minute)
	gone := mk(&Owner{SessionID: "s", BlockID: "gone"}, time.Minute)
	oldLive := mk(&Owner{SessionID: "s", BlockID: "live"}, 25*time.Hour) // a float left open for a day
	oldGone := mk(&Owner{SessionID: "s", BlockID: "gone"}, 25*time.Hour)
	if info, _ := os.Stat(fresh); info.Mode().Perm() != 0o700 {
		t.Errorf("tool dir mode = %v", info.Mode().Perm())
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(d.ToolsDir(), "link")); err != nil {
		t.Fatal(err)
	}

	removed, err := d.GCTools(now, ToolDirMaxAge, func(o Owner) bool { return o.BlockID == "live" })
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(removed)
	want := []string{gone, old, oldGone}
	slices.Sort(want)
	if !slices.Equal(removed, want) {
		t.Errorf("removed %v, want %v", removed, want)
	}
	for _, dir := range []string{fresh, live, oldLive, filepath.Join(d.ToolsDir(), "link")} {
		if _, err := os.Lstat(dir); err != nil {
			t.Errorf("%s should survive: %v", dir, err)
		}
	}
	// Without a way to ask about owners, age decides.
	if removed, err := d.GCTools(now, ToolDirMaxAge, nil); err != nil || !slices.Equal(removed, []string{oldLive}) {
		t.Errorf("GC without alive removed %v, %v; want %v", removed, err, oldLive)
	}
	if removed, err := (Dirs{State: t.TempDir()}).GCTools(now, ToolDirMaxAge, nil); err != nil || removed != nil {
		t.Errorf("no tools dir: %v %v", removed, err)
	}
}

func TestHistoryVisits(t *testing.T) {
	var h History
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	h.Visit(Visit{Kind: "pane", SessionID: "s1", WindowID: "w1", BlockID: "b1", At: t0})
	h.Visit(Visit{Kind: "pane", SessionID: "s1", WindowID: "w2", BlockID: "b2", At: t0.Add(time.Minute)})
	h.Visit(Visit{Kind: "pane", SessionID: "s1", WindowID: "w1", BlockID: "b1", At: t0.Add(2 * time.Minute)})
	if len(h.Visits) != 2 || h.Visits[0].BlockID != "b1" {
		t.Fatalf("visits = %+v", h.Visits)
	}
	tests := []struct {
		name    string
		current Visit
		want    string
	}{
		{"bounce back from the newest", Visit{SessionID: "s1", BlockID: "b1"}, "b2"},
		{"elsewhere: the newest", Visit{SessionID: "s9", BlockID: "b9"}, "b1"},
		{"session-only current matches by session", Visit{SessionID: "s1"}, ""},
	}
	for _, tt := range tests {
		got, ok := h.Previous(tt.current)
		if got.BlockID != tt.want || ok != (tt.want != "") {
			t.Errorf("%s: Previous = %q %v, want %q", tt.name, got.BlockID, ok, tt.want)
		}
	}
	for i := range MaxVisits + 10 {
		h.Visit(Visit{SessionID: "s", BlockID: string(rune('A' + i)), At: t0})
	}
	if len(h.Visits) != MaxVisits {
		t.Errorf("history not bounded: %d", len(h.Visits))
	}
}

func TestHistoryOpens(t *testing.T) {
	var h History
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	h.Open("/src/api/old.go", now.Add(-30*24*time.Hour))
	for range 3 {
		h.Open("/src/api/old.go", now.Add(-30*24*time.Hour))
	}
	h.Open("/src/api/new.go", now.Add(-time.Minute))
	h.Open("/src/web/x.go", now)
	h.Open("/src/api/", now) // the root itself is not "under" it
	got := h.OpensUnder("/src/api", now, 5)
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	// new.go: 1×4 beats old.go: 4×0.25.
	if want := []string{"/src/api/new.go", "/src/api/old.go"}; !slices.Equal(paths, want) {
		t.Errorf("OpensUnder = %v, want %v", paths, want)
	}
	if got[1].Count != 4 {
		t.Errorf("count = %d", got[1].Count)
	}
	if n := len(h.OpensUnder("/src", now, 1)); n != 1 {
		t.Errorf("limit not applied: %d", n)
	}
	for i := range MaxOpens + 5 {
		h.Open(filepath.Join("/many", string(rune('a'+i%26)), string(rune('A'+i))), now)
	}
	if len(h.Opens) != MaxOpens {
		t.Errorf("opens not bounded: %d", len(h.Opens))
	}
}

func TestUpdateHistoryIsSerialized(t *testing.T) {
	d := testDirs(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := d.UpdateHistory(func(h *History) {
				h.Open(filepath.Join("/p", string(rune('a'+i))), time.Now())
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	h, err := d.History()
	if err != nil || len(h.Opens) != 20 {
		t.Fatalf("history has %d opens (%v), want 20: lost updates", len(h.Opens), err)
	}
}

func TestCorruptHistoryIsReplaced(t *testing.T) {
	d := testDirs(t)
	if err := WriteFile(filepath.Join(d.State, historyFile), []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.History(); err == nil {
		t.Error("History should report corruption")
	}
	if err := d.UpdateHistory(func(h *History) { h.Visit(Visit{SessionID: "s"}) }); err != nil {
		t.Fatal(err)
	}
	if h, err := d.History(); err != nil || len(h.Visits) != 1 {
		t.Errorf("history = %+v, %v", h, err)
	}
}
