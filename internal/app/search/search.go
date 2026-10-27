package search

import (
	"runtime"
	"sync"
	"sync/atomic"

	"rexkit/internal/find"
)

// checkEvery is how many lines a search reads between looks at whether a
// newer one has made it stale.
const checkEvery = 4096

// scan returns the lines of t that m finds, oldest first; ok is false when
// stale said to stop.
func scan(m *find.Matcher, t text, stale func() bool) (hits []int, ok bool) {
	for i, l := range t.plain {
		if i%checkEvery == 0 && stale() {
			return nil, false
		}
		if m.In(l, t.fold[i]) {
			hits = append(hits, i)
		}
	}
	return hits, true
}

// searchTexts runs m over texts, on every core at once, and returns each
// one's hits; ok is false when stale said to stop.
func searchTexts(m *find.Matcher, texts []text, stale func() bool) (hits [][]int, ok bool) {
	hits = make([][]int, len(texts))
	var wg sync.WaitGroup
	var stopped atomic.Bool
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, t := range texts {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			var ok bool
			if hits[i], ok = scan(m, t, stale); !ok {
				stopped.Store(true)
			}
		}()
	}
	wg.Wait()
	if stopped.Load() {
		return nil, false
	}
	return hits, true
}

// searchedMsg delivers the hits of a search: for every pane read when it
// started (all), or for panes read since.
type searchedMsg struct {
	gen   int64
	panes []int   // indexes into model.panes
	hits  [][]int // per pane
	all   bool
}
