package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// History bounds.
const (
	MaxVisits = 50
	MaxOpens  = 500
)

// Visit is a destination the user jumped to.
type Visit struct {
	Kind      string    `json:"kind"` // session, tab, pane or agent
	SessionID string    `json:"session_id"`
	WindowID  string    `json:"window_id,omitempty"`
	BlockID   string    `json:"block_id,omitempty"`
	At        time.Time `json:"at"`
}

// Same reports whether two visits point at the same place, comparing the
// most specific ID both have.
func (v Visit) Same(o Visit) bool {
	switch {
	case v.BlockID != "" && o.BlockID != "":
		return v.BlockID == o.BlockID
	case v.WindowID != "" && o.WindowID != "":
		return v.WindowID == o.WindowID
	}
	return v.SessionID == o.SessionID
}

// FileOpen counts how often a path was opened from the file launcher.
type FileOpen struct {
	Path  string    `json:"path"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

// Score ranks an open by frequency weighted by recency.
func (f FileOpen) Score(now time.Time) float64 {
	age := now.Sub(f.Last)
	weight := 0.25
	switch {
	case age <= time.Hour:
		weight = 4
	case age <= 24*time.Hour:
		weight = 2
	case age <= 7*24*time.Hour:
		weight = 1
	}
	return float64(f.Count) * weight
}

// History is the visit and file-open history, newest first.
type History struct {
	Visits []Visit    `json:"visits"`
	Opens  []FileOpen `json:"opens"`
}

// Visit records a jump, replacing an older visit to the same place.
func (h *History) Visit(v Visit) {
	h.Visits = slices.DeleteFunc(h.Visits, v.Same)
	h.Visits = append([]Visit{v}, h.Visits...)
	if len(h.Visits) > MaxVisits {
		h.Visits = h.Visits[:MaxVisits]
	}
}

// Previous is the most recent visit that is not the current location.
func (h History) Previous(current Visit) (Visit, bool) {
	for _, v := range h.Visits {
		if !v.Same(current) {
			return v, true
		}
	}
	return Visit{}, false
}

// Open records that path was opened at time at.
func (h *History) Open(path string, at time.Time) {
	path = filepath.Clean(path)
	i := slices.IndexFunc(h.Opens, func(f FileOpen) bool { return f.Path == path })
	if i < 0 {
		h.Opens = append(h.Opens, FileOpen{Path: path})
		i = len(h.Opens) - 1
	}
	h.Opens[i].Count++
	h.Opens[i].Last = at
	sortByScore(h.Opens, at)
	if len(h.Opens) > MaxOpens {
		h.Opens = h.Opens[:MaxOpens]
	}
}

// OpensUnder returns up to n paths opened below root, best first.
func (h History) OpensUnder(root string, now time.Time, n int) []FileOpen {
	prefix := strings.TrimSuffix(filepath.Clean(root), "/") + "/"
	var out []FileOpen
	for _, f := range h.Opens {
		if strings.HasPrefix(f.Path, prefix) {
			out = append(out, f)
		}
	}
	sortByScore(out, now)
	return out[:min(n, len(out))]
}

func sortByScore(opens []FileOpen, now time.Time) {
	slices.SortStableFunc(opens, func(a, b FileOpen) int {
		if sa, sb := a.Score(now), b.Score(now); sa != sb {
			if sa > sb {
				return -1
			}
			return 1
		}
		return b.Last.Compare(a.Last)
	})
}

const historyFile = "visits.json"

// History loads the history; a missing file is an empty history.
func (d Dirs) History() (History, error) {
	var h History
	err := ReadJSON(filepath.Join(d.State, historyFile), &h)
	if errors.Is(err, fs.ErrNotExist) {
		return History{}, nil
	}
	return h, err
}

// UpdateHistory applies fn to the stored history under an exclusive lock,
// so concurrent tools never lose each other's updates.
func (d Dirs) UpdateHistory(fn func(*History)) error {
	if err := os.MkdirAll(d.State, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(d.State, historyFile+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	h, err := d.History()
	if err != nil {
		h = History{} // a corrupt history is not worth failing a jump over
	}
	fn(&h)
	return WriteJSON(filepath.Join(d.State, historyFile), h)
}
