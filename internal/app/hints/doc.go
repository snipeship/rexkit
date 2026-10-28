// Package hints labels what is worth picking on a Rex pane (URLs, paths
// with :line:col, git SHAs, UUIDs, IPs, container and pod ids) so it can be
// copied, opened or pasted with a key or two. It redraws the pane in place,
// dimmed, with the things lifted back to full contrast and a short label in
// an accent chip on each. A label copies its thing, or after ⇥ types it
// into the pane; ⇧ and the label opens it. A key of the hints setting
// opens it for one thing (copy, paste, open or the user's command), and the
// user's patterns add to what it labels or replace it, as Alacritty's hints
// and WezTerm's quick select do.
//
// It runs in a float exactly over the pane (Rex draws a float at a pane's
// rect with the pane's cells in the same places), so the other panes stay
// as they are and the pane's own header is the float's.
// A pane with nothing to pick never gets the overlay: the tool closes and
// says so in a toast.
package hints
