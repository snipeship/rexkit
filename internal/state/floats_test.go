package state

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// HiddenFloats reads what lua/floats.lua writes: a tab's hidden layers back
// to front, each a block or a split of two, and an empty table as [].
func TestHiddenFloats(t *testing.T) {
	d := Dirs{State: t.TempDir()}
	if got := d.HiddenFloats(); got != nil {
		t.Errorf("no file: %v", got)
	}
	data := `{
  "s1": {"hidden": {"w1": {"focus": "b2", "layers": [
    {"bounds": [0.25, 0.25, 0.5, 0.5], "tree": "b1"},
    {"bounds": [0.27, 0.29, 0.5, 0.5], "tree": {"direction": "vertical", "ratio": 0.5, "before": "b2",
      "after": {"direction": "horizontal", "ratio": 0.3, "before": "b3", "after": "b4"}}}]}},
    "homes": [], "back": [], "last_tab": "w9"},
  "s2": {"hidden": [], "homes": []},
  "s3": {"hidden": {"w3": {"layers": [{"tree": 7}]}}}
}`
	if err := os.WriteFile(filepath.Join(d.State, workspaceFile), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []HiddenFloat{{"s1", "w1", "b1"}, {"s1", "w1", "b2"}, {"s1", "w1", "b3"}, {"s1", "w1", "b4"}}
	if got := d.HiddenFloats(); !slices.Equal(got, want) {
		t.Errorf("HiddenFloats = %v, want %v", got, want)
	}
	if err := os.WriteFile(filepath.Join(d.State, workspaceFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := d.HiddenFloats(); got != nil {
		t.Errorf("a broken file: %v", got)
	}
}
