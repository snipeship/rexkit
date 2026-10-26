package match

import "math"

// maxDP bounds the dynamic-programming window. Longer windows fall back to a
// greedy alignment, which is linear and still good on long paths.
const maxDP = 512

const unreachable = math.MinInt32 / 2

// term is one whitespace-separated piece of a query.
type term struct {
	runes     []rune // folded unless caseSensitive
	sensitive bool   // smart case: the term contains an upper-case letter
}

// scratch holds buffers reused across alignments so matching does not
// allocate per row.
type scratch struct {
	text  []rune
	bonus []int32
	score []int32 // len(term) × window, row-major
	chunk []int32 // bonus of the run each cell's match belongs to
	pos   []int
}

func (t term) eq(r, p rune) bool {
	if t.sensitive {
		return r == p
	}
	return fold(r) == p
}

// window finds the smallest span of text that can contain the term: from the
// first occurrence of its first rune to the last occurrence of its last rune.
// ok is false when the term is not a subsequence of text.
func (t term) window(text []rune) (from, to int, ok bool) {
	pat := t.runes
	from, i := -1, 0
	for j, r := range text {
		if t.eq(r, pat[i]) {
			if i == 0 {
				from = j
			}
			if i++; i == len(pat) {
				break
			}
		}
	}
	if i < len(pat) {
		return 0, 0, false
	}
	last := pat[len(pat)-1]
	for to = len(text) - 1; to >= from; to-- {
		if t.eq(text[to], last) {
			break
		}
	}
	return from, to + 1, true
}

// align returns the best score of t in text and, if pos is non-nil, fills it
// with the matched rune indexes (len(pos) == len(t.runes)). With contiguous
// set, only unbroken occurrences of the term count.
func (s *scratch) align(t term, text []rune, pos []int, contiguous bool) (int, bool) {
	from, to, ok := t.window(text)
	if !ok {
		return 0, false
	}
	s.bonus = s.bonus[:0]
	prev := classWhite
	if from > 0 {
		prev = classOf(text[from-1])
	}
	for j, r := range text[from:to] {
		cur := classOf(r)
		b := bonusAt(prev, cur)
		if from+j == 0 && b > 0 {
			b = bonusStart
		}
		s.bonus = append(s.bonus, int32(b))
		prev = cur
	}
	var score int
	switch {
	case contiguous:
		if score, ok = s.substring(t, text, from, to, pos); !ok {
			return 0, false
		}
	case to-from > maxDP:
		score = s.greedy(t, text, from, to, pos)
	default:
		score = s.optimal(t, text[from:to], from, pos)
	}
	if len(t.runes) == len(text) {
		score += bonusExact
	}
	return score, true
}

// substring scores the best unbroken occurrence of t in text[from:to].
func (s *scratch) substring(t term, text []rune, from, to int, pos []int) (int, bool) {
	m := len(t.runes)
	pos = s.positions(pos, m)
	best, at := unreachable, -1
	for j := from; j+m <= to; j++ {
		k := 0
		for k < m && t.eq(text[j+k], t.runes[k]) {
			k++
		}
		if k < m {
			continue
		}
		for i := range pos {
			pos[i] = j + i
		}
		if sc := s.scoreAt(pos, from); sc > best {
			best, at = sc, j
		}
	}
	for i := range pos {
		pos[i] = at + i
	}
	return best, at >= 0
}

// optimal is fzf v2's Smith-Waterman style alignment over text (a window
// starting at offset in the field).
func (s *scratch) optimal(t term, text []rune, offset int, pos []int) int {
	m, n := len(t.runes), len(text)
	s.score = grow(s.score, m*n)
	s.chunk = grow(s.chunk, m*n)
	H, C := s.score, s.chunk

	for j, r := range text {
		H[j] = unreachable
		if t.eq(r, t.runes[0]) {
			H[j] = scoreMatch + s.bonus[j]*firstCharMultiple
			C[j] = s.bonus[j]
		}
	}
	for i := 1; i < m; i++ {
		row, up := H[i*n:(i+1)*n], H[(i-1)*n:i*n]
		crow, cup := C[i*n:(i+1)*n], C[(i-1)*n:i*n]
		gap := int32(unreachable) // best up[k] + gap penalty, k ≤ j-2
		for j := range n {
			row[j] = unreachable
			if j >= 2 {
				gap = max(gap+gapExtension, up[j-2]+gapStart)
			}
			if j < i || !t.eq(text[j], t.runes[i]) {
				continue
			}
			b := s.bonus[j]
			viaGap := gap + scoreMatch + b
			viaRun := int32(unreachable)
			run := cup[max(j-1, 0)]
			if j >= 1 && up[j-1] > unreachable {
				if b >= bonusBoundary && b > run {
					run = b
				}
				viaRun = up[j-1] + scoreMatch + max(b, run, bonusConsecutive)
			}
			if viaRun >= viaGap && viaRun > unreachable {
				row[j], crow[j] = viaRun, run
			} else if gap > unreachable {
				row[j], crow[j] = viaGap, b
			}
		}
	}

	last := H[(m-1)*n : m*n]
	best, end := int32(unreachable), -1
	for j, v := range last {
		if v > best {
			best, end = v, j
		}
	}
	if pos != nil {
		s.backtrack(t, n, end, offset, pos)
	}
	return int(best)
}

// backtrack recovers the matched positions by replaying the recurrence.
func (s *scratch) backtrack(t term, n, j, offset int, pos []int) {
	H, C := s.score, s.chunk
	for i := len(t.runes) - 1; i > 0; i-- {
		pos[i] = offset + j
		cur, b := H[i*n+j], s.bonus[j]
		if j >= 1 && H[(i-1)*n+j-1] > unreachable {
			run := C[(i-1)*n+j-1]
			if b >= bonusBoundary && b > run {
				run = b
			}
			if H[(i-1)*n+j-1]+scoreMatch+max(b, run, bonusConsecutive) == cur {
				j--
				continue
			}
		}
		for k := j - 2; k >= i-1; k-- {
			if up := H[(i-1)*n+k]; up > unreachable &&
				up+gapStart+gapExtension*int32(j-k-2)+scoreMatch+b == cur {
				j = k
				break
			}
		}
	}
	pos[0] = offset + j
}

// greedy aligns long texts in linear time (fzf v1): the first full match
// scanning forward, tightened by scanning backward from where it ended.
func (s *scratch) greedy(t term, text []rune, from, to int, pos []int) int {
	m := len(t.runes)
	pos = s.positions(pos, m)
	end, i := from, 0
	for j := from; j < to && i < m; j++ {
		if t.eq(text[j], t.runes[i]) {
			i, end = i+1, j
		}
	}
	i = m - 1
	for j := end; j >= from && i >= 0; j-- {
		if t.eq(text[j], t.runes[i]) {
			pos[i] = j
			i--
		}
	}
	return s.scoreAt(pos, from)
}

// scoreAt scores a given alignment with the same rules as optimal; s.bonus
// must describe the window starting at from.
func (s *scratch) scoreAt(pos []int, from int) int {
	score := 0
	var run int32
	for k, at := range pos {
		b := s.bonus[at-from]
		switch {
		case k == 0:
			score += scoreMatch + int(b)*firstCharMultiple
			run = b
		case at == pos[k-1]+1:
			if b >= bonusBoundary && b > run {
				run = b
			}
			score += scoreMatch + int(max(b, run, bonusConsecutive))
		default:
			score += scoreMatch + int(b) + gapStart + gapExtension*(at-pos[k-1]-2)
			run = b
		}
	}
	return score
}

// positions returns pos, or a scratch slice of length m when pos is nil.
func (s *scratch) positions(pos []int, m int) []int {
	if pos != nil {
		return pos
	}
	if cap(s.pos) < m {
		s.pos = make([]int, m)
	}
	return s.pos[:m]
}

func grow(buf []int32, n int) []int32 {
	if cap(buf) < n {
		return make([]int32, n)
	}
	return buf[:n]
}
