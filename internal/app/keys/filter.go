package keys

import (
	"strings"

	"rexkit/internal/match"
)

// shown is one group as the sheet shows it: the entries that pass the query
// and the mode filter, with their highlight positions.
type shown struct {
	group   int // index into Guide.Groups
	entries []hit
}

// hit is an entry that matched.
type hit struct {
	entry int // index into the group's entries
	score int
	keys  []int // matched rune positions in Entry.KeysLabel
	title []int // matched rune positions in Entry.Title
}

// filter selects what the sheet shows. mode is a chip index: 0 is every
// group, i is Guide.Groups[i-1].
func filter(g Guide, query string, mode int) []shown {
	pat := match.Compile(query)
	var out []shown
	for gi, grp := range g.Groups {
		if mode > 0 && gi != mode-1 {
			continue
		}
		s := shown{group: gi}
		for ei, e := range grp.Entries {
			h := hit{entry: ei}
			if !pat.Empty() {
				res, ok := pat.Match(searchFields(grp, e)...)
				if !ok {
					continue
				}
				h.score, h.keys, h.title = res.Score, res.Positions[0], res.Positions[1]
			}
			s.entries = append(s.entries, h)
		}
		if len(s.entries) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// searchFields are what a query matches: the keys and title as shown, and,
// hidden, the mode, the action name, the keys in config notation (so "ctrl"
// finds ⌃ keys) and the arguments ("left", "top").
func searchFields(g Group, e Entry) []match.Field {
	configs := make([]string, len(e.Keys))
	for i, k := range e.Keys {
		configs[i] = k.Config()
	}
	return []match.Field{
		{Text: e.KeysLabel()},
		{Text: e.Title},
		{Text: g.Name(), Hidden: true},
		{Text: e.Action, Hidden: true},
		{Text: strings.Join(configs, " "), Hidden: true},
		{Text: argWords(e.Args), Hidden: true},
	}
}

// count is the number of entries in a filtered view.
func count(view []shown) int {
	n := 0
	for _, s := range view {
		n += len(s.entries)
	}
	return n
}

// total is the number of entries in the guide.
func (g Guide) total() int {
	n := 0
	for _, grp := range g.Groups {
		n += len(grp.Entries)
	}
	return n
}
