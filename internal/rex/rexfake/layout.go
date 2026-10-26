package rexfake

import "rexkit/internal/rex"

// row lays blocks out side by side.
func row(blocks []rex.Block) *rex.LayoutNode {
	if len(blocks) == 0 {
		return nil
	}
	n := &rex.LayoutNode{BlockID: blocks[0].ID}
	for _, b := range blocks[1:] {
		n = &rex.LayoutNode{Split: &rex.Split{Direction: rex.Horizontal, Ratio: 0.5,
			Before: *n, After: rex.LayoutNode{BlockID: b.ID}}}
	}
	relayout(n, rex.Rect{0, 0, 1, 1})
	return n
}

// leaves lists the blocks of a creation layout in layout order.
func leaves(n rex.Node) []rex.BlockSpec {
	if n.Block != nil {
		return []rex.BlockSpec{*n.Block}
	}
	if n.Split == nil {
		return nil
	}
	return append(leaves(n.Split.Before), leaves(n.Split.After)...)
}

// insert splits the leaf holding anchor so that blockID sits on side.
func insert(n *rex.LayoutNode, anchor, blockID, direction, side string) bool {
	if n == nil {
		return false
	}
	if n.Split == nil {
		if n.BlockID != anchor {
			return false
		}
		old, added := rex.LayoutNode{BlockID: anchor}, rex.LayoutNode{BlockID: blockID}
		if direction == "" {
			direction = rex.Horizontal
		}
		split := &rex.Split{Direction: direction, Ratio: 0.5, Before: old, After: added}
		if side == "before" {
			split.Before, split.After = added, old
		}
		*n = rex.LayoutNode{Rect: n.Rect, Split: split}
		relayout(n, n.Rect)
		return true
	}
	return insert(&n.Split.Before, anchor, blockID, direction, side) ||
		insert(&n.Split.After, anchor, blockID, direction, side)
}

// remove drops the leaf holding blockID; its sibling takes the split's place.
// It returns the new root (nil when the tree is empty).
func remove(n *rex.LayoutNode, blockID string) *rex.LayoutNode {
	if n == nil || n.Split == nil {
		if n != nil && n.BlockID == blockID {
			return nil
		}
		return n
	}
	before, after := remove(&n.Split.Before, blockID), remove(&n.Split.After, blockID)
	switch {
	case before == nil:
		after.Rect = n.Rect
		relayout(after, n.Rect)
		return after
	case after == nil:
		before.Rect = n.Rect
		relayout(before, n.Rect)
		return before
	}
	n.Split.Before, n.Split.After = *before, *after
	return n
}

func relayout(n *rex.LayoutNode, r rex.Rect) {
	n.Rect = r
	if n.Split == nil {
		return
	}
	a, b := r, r
	if n.Split.Direction == rex.Vertical {
		a[3] = r[3] * n.Split.Ratio
		b[1], b[3] = r[1]+a[3], r[3]-a[3]
	} else {
		a[2] = r[2] * n.Split.Ratio
		b[0], b[2] = r[0]+a[2], r[2]-a[2]
	}
	relayout(&n.Split.Before, a)
	relayout(&n.Split.After, b)
}
