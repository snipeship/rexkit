package install

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"rexkit/internal/state"
)

// Record is ~/.config/rexkit/install.json: what install did, so that
// uninstall can undo it.
type Record struct {
	Root        string      `json:"root"`                // the rexkit tree (a clone or a packaged one) the loader loads
	Backup      string      `json:"backup,omitempty"`    // init.lua as it was before rexkit's most recent takeover: what uninstall restores
	NoConfig    bool        `json:"no_config,omitempty"` // there was no init.lua when rexkit took over: uninstall removes it
	Link        string      `json:"link,omitempty"`      // init.lua was a symlink to this target: uninstall puts the link back
	Mode        uint32      `json:"mode,omitempty"`      // init.lua's permissions before the takeover
	Originals   []string    `json:"originals,omitempty"` // older configs rexkit replaced or found while migrating, newest first
	Migrated    []Migration `json:"migrated,omitempty"`
	InstalledAt time.Time   `json:"installed_at"`
}

// Migration is one thing install took over from a legacy tool.
type Migration struct {
	From     string    `json:"from"`               // rex-goto, rex-zellij, settings, or scratch (older installs)
	Root     string    `json:"root,omitempty"`     // the legacy checkout
	Record   string    `json:"record,omitempty"`   // the legacy installer's record
	Files    []string  `json:"files,omitempty"`    // files read
	Wrote    string    `json:"wrote,omitempty"`    // file written
	Settings []string  `json:"settings,omitempty"` // settings carried over
	Dropped  []string  `json:"dropped,omitempty"`  // settings left behind
	Skipped  string    `json:"skipped,omitempty"`  // why nothing was carried over
	At       time.Time `json:"at"`
}

// readRecord returns the installation record; ok is false when there is none.
func (e *Env) readRecord() (rec Record, ok bool, err error) {
	err = state.ReadJSON(e.Paths.Record(), &rec)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Record{}, false, nil
	case err != nil:
		return Record{}, false, fmt.Errorf("reading %s: %w", e.Paths.Tilde(e.Paths.Record()), err)
	}
	return rec, true, nil
}
