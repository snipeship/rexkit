// Package search is `rexkit search`: one query over the screen and
// scrollback of every pane in every session. Panes are read four at a
// time, origin session first, and searched as they come in; the matches
// are grouped by pane with a preview of the lines around them. ⏎ on a
// match goes to its pane and opens Scrollback on the capture that was
// searched, at that line, with the query as its search (Lua's
// rexkit_search_jump); internal/find does the matching.
package search
