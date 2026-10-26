package files

import (
	"slices"
	"strings"
	"unicode/utf8"

	"rexkit/internal/match"
)

// source is where a row comes from.
type source uint8

const (
	fromListing source = iota
	fromRecent         // RECENT HERE: opened from Files below this folder
	fromProject        // PROJECTS: zoxide's best folders
)

// row is one item of the list.
type row struct {
	e    entry
	from source
}

// syncRankLimit is the largest corpus ranked on the UI goroutine; larger ones
// (recursive listings) are ranked in the background.
const syncRankLimit = 5000

// rankRows returns the rows matching p, best first. A row matches through
// its base name, its folder part, or (as a hidden field) its whole relative
// path, so "files/model" still finds internal/app/files/model.go.
func rankRows(p *match.Pattern, corpus []row) []row {
	var buf []match.Field
	ranked := p.Rank(len(corpus), func(i int) []match.Field {
		buf = searchFields(corpus[i].e.label, buf)
		return buf
	})
	out := make([]row, len(ranked))
	for i, r := range ranked {
		out[i] = corpus[r.Index]
	}
	return out
}

// searchFields are the match fields of a display label: base name, then the
// folder part and the full label when there is one.
func searchFields(label string, buf []match.Field) []match.Field {
	dir, base := splitLabel(label)
	buf = append(buf[:0], match.Field{Text: base})
	if dir != "" {
		buf = append(buf, match.Field{Text: dir, Weight: 70}, match.Field{Text: label, Hidden: true})
	}
	return buf
}

// highlights returns the rune positions of label that p matched.
func highlights(p *match.Pattern, label string) []int {
	if p == nil || p.Empty() {
		return nil
	}
	res, ok := p.Match(searchFields(label, nil)...)
	if !ok {
		return nil
	}
	dir, _ := splitLabel(label)
	offset := utf8.RuneCountInString(dir)
	var out []int
	for f, pos := range res.Positions {
		for _, i := range pos {
			if f == 0 {
				i += offset
			}
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// splitLabel splits "a/b/c" into "a/b/" and "c".
func splitLabel(label string) (dir, base string) {
	i := strings.LastIndexByte(label, '/')
	if i < 0 || i == len(label)-1 {
		return "", label
	}
	return label[:i+1], label[i+1:]
}

// withoutDuplicates returns extra rows whose paths are not in rows.
func withoutDuplicates(rows, extra []row) []row {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.e.path] = true
	}
	var out []row
	for _, r := range extra {
		if !seen[r.e.path] {
			out = append(out, r)
		}
	}
	return out
}
