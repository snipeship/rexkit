package state

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func TestAttentionEnterAndSee(t *testing.T) {
	var a Attention
	if !a.Enter("b", "done", "", at) {
		t.Fatal("first Enter is not news")
	}
	if a.Enter("b", "done", "", at.Add(time.Minute)) {
		t.Error("the same state again is news")
	}
	if got := a.Since("b", "done", "", at.Add(time.Hour)); !got.Equal(at) {
		t.Errorf("Since = %v, want when it entered", got)
	}
	if got := a.Since("b", "blocked", "question", at.Add(time.Hour)); !got.Equal(at.Add(time.Hour)) {
		t.Errorf("Since of another state = %v, want the fallback", got)
	}
	a.See("b", at.Add(time.Minute))
	a.See("b", at) // never back in time
	if got := a.SeenAt("b"); !got.Equal(at.Add(time.Minute)) {
		t.Errorf("SeenAt = %v", got)
	}
	if !a.Enter("b", "blocked", "question", at.Add(2*time.Minute)) || !a.SeenAt("b").Equal(at.Add(time.Minute)) {
		t.Error("a new state is news and keeps when it was last seen")
	}
}

func TestAttentionSurvivesCorruption(t *testing.T) {
	d := Dirs{State: t.TempDir()}
	if err := os.WriteFile(filepath.Join(d.State, attentionFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a := d.Attention(); len(a.Panes) != 0 {
		t.Errorf("corrupt file read as %+v", a)
	}
	if err := d.UpdateAttention(at, func(a *Attention) { a.See("b", at) }); err != nil {
		t.Fatal(err)
	}
	if got := d.Attention().SeenAt("b"); !got.Equal(at) {
		t.Errorf("after an update over a corrupt file: SeenAt = %v", got)
	}
}

func TestAttentionConcurrentWriters(t *testing.T) {
	d := Dirs{State: t.TempDir()}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.UpdateAttention(at, func(a *Attention) { a.See("b"+strconv.Itoa(i), at) })
		}()
	}
	wg.Wait()
	if n := len(d.Attention().Panes); n != 20 {
		t.Errorf("%d panes after 20 concurrent writers, want 20", n)
	}
}

func TestAttentionIsBounded(t *testing.T) {
	d := Dirs{State: t.TempDir()}
	_ = d.UpdateAttention(at, func(a *Attention) {
		a.See("old", at.Add(-MarkMaxAge-time.Hour))
		for i := range MaxMarks + 10 {
			a.See("b"+strconv.Itoa(i), at.Add(time.Duration(i)*time.Second))
		}
	})
	a := d.Attention()
	if len(a.Panes) != MaxMarks {
		t.Errorf("%d marks, want %d", len(a.Panes), MaxMarks)
	}
	if _, ok := a.Panes["old"]; ok {
		t.Error("kept a mark untouched for longer than MarkMaxAge")
	}
	if _, ok := a.Panes["b0"]; ok {
		t.Error("kept the least recent mark over the bound")
	}
}

func TestAttentionForget(t *testing.T) {
	a := Attention{Back: &Visit{BlockID: "gone"}, Landed: &Visit{BlockID: "here"}}
	a.See("gone", at)
	a.See("here", at)
	a.Forget(func(b string) bool { return b == "here" })
	if _, ok := a.Panes["gone"]; ok || a.Back != nil || a.Landed == nil {
		t.Errorf("after Forget: %+v", a)
	}
}
