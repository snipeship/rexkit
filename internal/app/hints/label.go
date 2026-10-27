package hints

import (
	"slices"

	"rexkit/internal/backdrop"
)

// Alphabet is what labels are made of, easiest first: the home row, then
// the rows above and below. The nearest things get single home-row letters;
// the least easy letters become prefixes.
const Alphabet = "asdfjklghqweruiopzxcvbnmty"

// Labels returns n labels, prefix-free (no label starts another), as short
// as they can be: single letters while they last, then pairs. Pairs start
// with the least easy letters (the end of the alphabet), so the singles the
// nearest things get stay on the home row. Past every pair, labels grow a
// letter at a time, breadth first.
func Labels(n int, alphabet string) []string {
	letters := []rune(alphabet)
	l := len(letters)
	if n <= 0 || l < 2 {
		return nil
	}
	if n <= l {
		out := make([]string, n)
		for i := range n {
			out[i] = string(letters[i])
		}
		return out
	}
	if n <= l*l {
		k := (n - l + l - 2) / (l - 1) // prefixes: (l-k) singles + k·l pairs ≥ n
		var out []string
		for i := range l - k {
			out = append(out, string(letters[i]))
		}
		for p := l - 1; p >= l-k; p-- {
			for _, r := range letters {
				out = append(out, string(letters[p])+string(r))
			}
		}
		return out[:n]
	}
	labels, next := []string{""}, 0
	for len(labels)-next < n || next == 0 {
		head := labels[next]
		next++
		for _, r := range letters {
			labels = append(labels, head+string(r))
		}
	}
	return labels[next : next+n]
}

// Assign labels the matches by distance from (row, col), the cursor: the
// nearest thing gets the first label, and a text seen twice keeps one label.
func Assign(ms []Match, row, col int) {
	type text struct {
		s    string
		dist int
	}
	best := map[string]int{}
	for _, m := range ms {
		d := abs(m.Row-row)*1000 + abs(m.Col-col)
		if b, ok := best[m.Text]; !ok || d < b {
			best[m.Text] = d
		}
	}
	texts := make([]text, 0, len(best))
	for s, d := range best {
		texts = append(texts, text{s, d})
	}
	slices.SortFunc(texts, func(a, b text) int {
		if a.dist != b.dist {
			return a.dist - b.dist
		}
		if a.s < b.s {
			return -1
		}
		return 1
	})
	labels := Labels(len(texts), Alphabet)
	of := map[string]string{}
	for i, t := range texts {
		of[t.s] = labels[i]
	}
	for i := range ms {
		ms[i].Label = of[ms[i].Text]
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// place decides where each label is drawn: in the cells just before its
// match, so the whole match stays readable. Those are blank cells where
// there are enough; else the chip also takes the end of the dimmed text
// before them (a two-letter label after one space). It covers the first
// cells of its match only where nothing before it can be taken: at column
// 0, right after another match or chip, or where it would split a wide
// glyph. A chip always ends at or inside its own match, never in another, so
// no two labels run together, and the chips of a column of matches line up.
func place(ms []Match, cells [][]backdrop.Cell) {
	// Find lists a row's matches left to right: each one's chip starts
	// after the end of the one before it.
	end, row := 0, -1
	for i := range ms {
		m := &ms[i]
		if m.Row != row {
			end, row = 0, m.Row
		}
		chip := max(m.Col-len(m.Label), end)
		for chip < m.Col && cells[m.Row][chip].Width == 0 { // the second half of a wide glyph
			chip++
		}
		m.Chip = chip
		end = max(m.Col+m.Width, chip+len(m.Label))
	}
}

// isBlank reports a cell with nothing on it.
func isBlank(c backdrop.Cell) bool { return c.Width == 1 && (c.Content == " " || c.Content == "") }
