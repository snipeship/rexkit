package rename

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"rexkit/internal/popup"
)

func readResult(t *testing.T, dir string) popup.Result {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, popup.ResultFile))
	if os.IsNotExist(err) {
		return popup.Result{}
	}
	if err != nil {
		t.Fatal(err)
	}
	var r popup.Result
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// runBatch executes cmd and the commands it batches, skipping timers.
func runBatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				go c() // a toast's timer would block
			}
		}
	}
}

// A ⏎ Lua forwarded must be answered: a failed rename keeps the dialog
// open and says "stay".
func TestHandshakeStayOnFailure(t *testing.T) {
	dir := t.TempDir()
	m := dialog(t, "api", func(string) tea.Cmd { return nil }, 56, 9)
	m.opts.stateDir = dir
	typeKeys(t, m, "x", "enter")
	_, cmd := m.Update(savedMsg{problem: "That pane is gone"})
	if quits(cmd) || m.saved {
		t.Fatal("a failed rename should keep the dialog")
	}
	runBatch(cmd)
	deadline := time.Now().Add(2 * time.Second)
	for readResult(t, dir).Action != "stay" {
		if time.Now().After(deadline) {
			t.Fatal("no stay answer")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAnswer(t *testing.T) {
	dir := t.TempDir()
	po := popup.Options{State: dir, OriginWindow: "window:w", OriginBlock: "block:b"}
	if err := answer(po, "session:s", false); err != nil {
		t.Fatal(err)
	}
	if r := readResult(t, dir); r.Action != "close" || r.Seq == 0 {
		t.Errorf("closing answer %+v", r)
	}
	if err := answer(po, "session:s", true); err != nil {
		t.Fatal(err)
	}
	if r := readResult(t, dir); r.Action != "rename" || r.SessionID != "session:s" || r.WindowID != "window:w" || r.BlockID != "block:b" {
		t.Errorf("rename answer %+v", r)
	}
	if err := answer(popup.Options{}, "session:s", true); err != nil {
		t.Errorf("no state dir: %v", err)
	}
}
