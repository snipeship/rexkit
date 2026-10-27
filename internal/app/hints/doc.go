// Package hints labels what is worth picking on a Rex pane (URLs, paths
// with :line:col, git SHAs, UUIDs, IPs, container and pod ids) so it can be
// copied, opened or pasted with a key or two. It redraws the pane in place,
// dimmed, with the things lifted back to full contrast and a short label in
// an accent chip on each. A label copies its thing; ⇧ and the label opens
// it, ⌥ and the label types it into the pane.
//
// It runs in a whole-window float, as every rexkit tool, and draws the pane
// at the pane's own place in the window (backdrop.Place, from rex.API.Screens);
// the rest of the float is see-through, so the other panes stay as they are.
// A pane with nothing to pick never gets the overlay: the tool closes and
// says so in a toast.
package hints
