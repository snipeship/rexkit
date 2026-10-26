// Package match is rexkit's fuzzy matcher. A query is split into terms that
// must all match (AND), each in whichever field of a row fits it best. Results
// carry a score for ranking, the matched rune positions of every field for
// highlighting, and which hidden field (if any) a row matched through, so a
// tool can say why a row is there ("in ~/code/api").
package match

import (
	"slices"
	"strings"
	"unicode"
)

// Default field weights, in percent of a term's raw score.
const (
	WeightVisible = 100
	WeightHidden  = 60
)

// Field is one searchable piece of a row.
type Field struct {
	Text string
	// Weight scales scores in this field, in percent. Zero means
	// WeightVisible, or WeightHidden for hidden fields.
	Weight int
	// Hidden marks text the row does not show (cwd, program, task). Hidden
	// fields need a stronger match (word-like rather than scattered letters)
	// and are reported through Result.Via.
	Hidden bool
}

func (f Field) weight() int {
	switch {
	case f.Weight > 0:
		return f.Weight
	case f.Hidden:
		return WeightHidden
	default:
		return WeightVisible
	}
}

// Result describes how a row matched.
type Result struct {
	Score int
	// Positions[i] lists the matched rune indexes of field i in ascending
	// order; it is nil for fields no term matched in.
	Positions [][]int
	// Via is the index of a hidden field that some term matched only
	// through, or -1 when every term matched a visible field.
	Via int
}

// Pattern is a compiled query. It keeps scratch buffers, so a Pattern must not
// be used from several goroutines at once; Compile one per goroutine instead.
type Pattern struct {
	terms []term
	s     scratch
	pos   []int
}

// Compile parses a query. Terms are separated by whitespace and matched
// case-insensitively unless they contain an upper-case letter (smart case).
func Compile(query string) *Pattern {
	p := &Pattern{}
	for _, word := range strings.Fields(query) {
		t := term{runes: []rune(word)}
		t.sensitive = slices.ContainsFunc(t.runes, unicode.IsUpper)
		if !t.sensitive {
			for i, r := range t.runes {
				t.runes[i] = fold(r)
			}
		}
		p.terms = append(p.terms, t)
	}
	return p
}

// Empty reports whether the query has no terms; everything matches it.
func (p *Pattern) Empty() bool { return len(p.terms) == 0 }

// Score reports whether a row matches and how well, without computing
// positions. It does not allocate; use it to filter and rank many rows.
func (p *Pattern) Score(fields ...Field) (int, bool) {
	total := 0
	for _, t := range p.terms {
		best, _, ok := p.best(t, fields, nil)
		if !ok {
			return 0, false
		}
		total += best
	}
	return total, true
}

// Match is Score plus highlight positions and the hidden field a row matched
// through. Call it for the rows being drawn.
func (p *Pattern) Match(fields ...Field) (Result, bool) {
	res := Result{Positions: make([][]int, len(fields)), Via: -1}
	for _, t := range p.terms {
		if cap(p.pos) < len(t.runes) {
			p.pos = make([]int, len(t.runes))
		}
		pos := p.pos[:len(t.runes)]
		best, field, ok := p.best(t, fields, pos)
		if !ok {
			return Result{}, false
		}
		res.Score += best
		res.Positions[field] = merge(res.Positions[field], pos)
		if fields[field].Hidden && res.Via < 0 {
			res.Via = field
		}
	}
	return res, true
}

// best finds the field where t scores highest, preferring any visible field
// over hidden ones so highlights land on what the user can see. When pos is
// non-nil it receives the winning positions.
func (p *Pattern) best(t term, fields []Field, pos []int) (score, field int, ok bool) {
	field = -1
	for _, hidden := range []bool{false, true} {
		for i, f := range fields {
			if f.Hidden != hidden || f.Text == "" {
				continue
			}
			s, matched := p.scoreField(t, f, nil)
			if matched && (field < 0 || s > score) {
				score, field = s, i
			}
		}
		if field >= 0 {
			break
		}
	}
	if field < 0 {
		return 0, -1, false
	}
	if pos != nil {
		p.scoreField(t, fields[field], pos)
	}
	return score, field, true
}

func (p *Pattern) scoreField(t term, f Field, pos []int) (int, bool) {
	if !t.mayMatch(f.Text) {
		return 0, false
	}
	p.s.text = p.s.text[:0]
	for _, r := range f.Text {
		p.s.text = append(p.s.text, r)
	}
	// Hidden fields match whole fragments only: a row must never appear
	// because of scattered letters in text the user cannot see.
	raw, ok := p.s.align(t, p.s.text, pos, f.Hidden)
	if !ok {
		return 0, false
	}
	return raw * f.weight() / 100, true
}

// mayMatch is a cheap subsequence test on the string, so rows that cannot
// match are rejected before any decoding or alignment.
func (t term) mayMatch(s string) bool {
	i := 0
	for _, r := range s {
		if t.eq(r, t.runes[i]) {
			if i++; i == len(t.runes) {
				return true
			}
		}
	}
	return false
}

// merge returns the sorted union of a and b.
func merge(a, b []int) []int {
	out := append(a, b...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Ranked is one row of a Rank result.
type Ranked struct {
	Index int
	Score int
	size  int // visible text length, the tie-breaker
}

// Rank scores n rows and returns the matching ones: best score first, then
// shorter visible text, then original order. fields(i) returns row i's fields
// and may reuse one backing slice between calls. An empty pattern returns
// every row in order.
func (p *Pattern) Rank(n int, fields func(i int) []Field) []Ranked {
	out := make([]Ranked, 0, n)
	for i := range n {
		fs := fields(i)
		s, ok := p.Score(fs...)
		if !ok {
			continue
		}
		size := 0
		for _, f := range fs {
			if !f.Hidden {
				size += len(f.Text)
			}
		}
		out = append(out, Ranked{Index: i, Score: s, size: size})
	}
	if !p.Empty() {
		slices.SortStableFunc(out, func(a, b Ranked) int {
			if a.Score != b.Score {
				return b.Score - a.Score
			}
			return a.size - b.size
		})
	}
	return out
}
