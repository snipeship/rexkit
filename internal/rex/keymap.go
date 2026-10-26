package rex

import (
	"context"
	"encoding/json"
	"errors"
)

// Keymap is the server's resolved keymap (rex keymap --json): the config
// file's bindings, modes and actions.
type Keymap struct {
	Bindings    List[Binding]    `json:"bindings"`
	Actions     List[Action]     `json:"actions"`
	Modes       List[Mode]       `json:"modes"`
	Diagnostics List[Diagnostic] `json:"diagnostics"`
	Config      KeymapConfig     `json:"config"`
}

// Binding maps keys (one element per key of a sequence) to an action.
type Binding struct {
	Keys   []string        `json:"keys"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
	Mode   string          `json:"mode,omitempty"`
	Source string          `json:"source,omitempty"`
}

// Action is an action keys can be bound to.
type Action struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"` // client, lua or block
	Title       string `json:"title,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Keywords    string `json:"keywords,omitempty"`
	Source      string `json:"source,omitempty"`
}

// Mode is a named key table.
type Mode struct {
	Name      string     `json:"name"`
	Exclusive bool       `json:"exclusive"`
	Blocked   [][]string `json:"blocked,omitempty"`
}

// Diagnostic is a problem the server found in the keymap.
type Diagnostic struct {
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message"`
	Key      string `json:"key,omitempty"`
	Action   string `json:"action,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Source   string `json:"source,omitempty"`
}

// KeymapConfig identifies the config file the keymap came from.
type KeymapConfig struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Hash   string `json:"hash,omitempty"`
}

// Keymap returns the server's keymap. The CLI exits 1 when the keymap has
// error diagnostics; those are returned in Diagnostics, not as an error.
func (c *Client) Keymap(ctx context.Context) (Keymap, error) {
	out, err := c.exec(ctx, "keymap", nil, "keymap", "--json")
	if err != nil && len(out) == 0 {
		return Keymap{}, err
	}
	var km Keymap
	if derr := decode("keymap", out, &km); derr != nil {
		return Keymap{}, errors.Join(err, derr)
	}
	return km, nil
}
