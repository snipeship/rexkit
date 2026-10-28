package state

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// workspaceFile is the Lua layer's own state (lua/core.lua): only Lua
// writes it, atomically; the tools only read it.
const workspaceFile = "workspace.json"

// HiddenFloat is a floating pane that ⌃P w hid (lua/floats.lua): its
// program runs on, in no tab, until ⌃P w in its tab shows it again.
type HiddenFloat struct {
	SessionID, WindowID, BlockID string
}

// HiddenFloats lists the hidden floating panes the Lua layer remembers, by
// session and tab, back to front within a tab. An entry it cannot read is
// left out: the file is Lua's, and a list that is short by a pane only
// leaves that pane out of Go to.
func (d Dirs) HiddenFloats() []HiddenFloat {
	data, err := os.ReadFile(filepath.Join(d.State, workspaceFile))
	if err != nil {
		return nil
	}
	var sessions map[string]struct {
		Hidden json.RawMessage `json:"hidden"`
	}
	if json.Unmarshal(data, &sessions) != nil {
		return nil
	}
	var out []HiddenFloat
	for _, sid := range slices.Sorted(maps.Keys(sessions)) {
		var windows map[string]struct {
			Layers []struct {
				Tree json.RawMessage `json:"tree"`
			} `json:"layers"`
		}
		if json.Unmarshal(sessions[sid].Hidden, &windows) != nil {
			continue // none: Lua writes an empty table as []
		}
		for _, wid := range slices.Sorted(maps.Keys(windows)) {
			for _, layer := range windows[wid].Layers {
				for _, block := range treeBlocks(layer.Tree) {
					out = append(out, HiddenFloat{SessionID: sid, WindowID: wid, BlockID: block})
				}
			}
		}
	}
	return out
}

// treeBlocks lists the blocks of a hidden layer's layout, in layout order: a
// tree is a block ID, or a split {before, after} of two trees.
func treeBlocks(raw json.RawMessage) []string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		if id == "" {
			return nil
		}
		return []string{id}
	}
	var split struct {
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	}
	if json.Unmarshal(raw, &split) != nil || split.Before == nil || split.After == nil {
		return nil
	}
	return append(treeBlocks(split.Before), treeBlocks(split.After)...)
}
