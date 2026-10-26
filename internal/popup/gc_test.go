package popup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"rexkit/internal/rex/rexfake"
	"rexkit/internal/state"
)

func TestClaimAndGC(t *testing.T) {
	f := rexfake.New()
	s, _, live := f.AddSession("work")
	dirs := state.Dirs{State: t.TempDir()}
	claim := func(o state.Owner) string {
		dir, err := dirs.NewToolDir()
		if err != nil {
			t.Fatal(err)
		}
		p := New(f, Options{Session: o.SessionID, State: dir})
		if err := p.Claim(Self{SessionID: o.SessionID, BlockID: o.BlockID}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	keep := claim(state.Owner{SessionID: s, BlockID: live})
	closed := claim(state.Owner{SessionID: s, BlockID: "block:closed"})
	goneSession := claim(state.Owner{SessionID: "session:gone", BlockID: "block:x"})

	removed, err := GC(ctx, f, dirs)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(removed)
	want := []string{closed, goneSession}
	slices.Sort(want)
	if !slices.Equal(removed, want) {
		t.Errorf("removed %v, want %v", removed, want)
	}
	if _, err := os.Stat(filepath.Join(keep, "owner.json")); err != nil {
		t.Errorf("live tool dir lost: %v", err)
	}
	if err := New(f, Options{}).Claim(Self{}); err != nil {
		t.Errorf("Claim without a state dir: %v", err)
	}
}
