package popup

import (
	"context"
	"errors"
	"time"

	"rexkit/internal/rex"
	"rexkit/internal/state"
)

// Claim records that the tool running in self owns its state directory, so
// GC removes the directory as soon as the tool's block is gone.
func (p *Popup) Claim(self Self) error {
	if p.opts.State == "" {
		return nil
	}
	return state.Claim(p.opts.State, state.Owner{SessionID: self.SessionID, BlockID: self.BlockID})
}

// GC removes stale tool directories: claimed ones whose owner block no longer
// exists, and unclaimed ones older than state.ToolDirMaxAge. A directory whose
// owner Rex cannot vouch for either way (Rex unreachable) is kept. Sessions
// are viewed once each.
func GC(ctx context.Context, api rex.API, dirs state.Dirs) ([]string, error) {
	type session struct {
		view    rex.View
		gone    bool // the session no longer exists
		unknown bool // Rex could not tell: keep the directory
	}
	sessions := map[string]session{}
	alive := func(o state.Owner) bool {
		s, seen := sessions[o.SessionID]
		if !seen {
			view, err := api.View(ctx, o.SessionID)
			s = session{view: view, gone: errors.Is(err, rex.ErrNotFound), unknown: err != nil && !errors.Is(err, rex.ErrNotFound)}
			sessions[o.SessionID] = s
		}
		if s.unknown {
			return true
		}
		_, _, _, ok := s.view.FindBlock(o.BlockID)
		return !s.gone && ok
	}
	return dirs.GCTools(time.Now(), state.ToolDirMaxAge, alive)
}
