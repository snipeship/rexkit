package switcher

import (
	"cmp"
	"slices"
	"strconv"
	"time"
	"unicode"

	"rexkit/internal/attend"
	"rexkit/internal/match"
	"rexkit/internal/state"
	"rexkit/internal/ui"
)

// category limits which kinds of places are listed.
type category int

const (
	catAll category = iota
	catSessions
	catTabs
	catPanes // agents are panes too
	catAgents
	numCategories
)

var categoryNames = [numCategories]string{"All", "Sessions", "Tabs", "Panes", "Agents"}

func (c category) admits(e entry) bool {
	switch c {
	case catSessions:
		return e.kind == ui.KindSession
	case catTabs:
		return e.kind == ui.KindTab
	case catPanes:
		return e.pane()
	case catAgents:
		return e.kind == ui.KindAgent
	}
	return true
}

// Section titles.
const (
	secNeedsYou = "Needs you"
	secWorking  = "Working"
	secRecent   = "Recent"
	secSessions = "Sessions"
	secIdle     = "Idle"
)

// maxRecent bounds the RECENT section.
const maxRecent = 5

// row is one line of a listing.
type row struct {
	entry   int           // index into catalog.entries
	depth   int           // indentation, for tree listings
	tree    bool          // inside a tree: show contents (counts, cwd) rather than the path
	match   *match.Result // why it matched, when searching
	visited time.Time     // when it was last visited, in RECENT
	ask     bool          // in NEEDS YOU: the agent's message gets a row of its own under it
}

// listing is what the list shows for the current query, category and scope.
type listing struct {
	rows     []row
	sections []ui.Section
	total    int // places the category and scope admit
	search   bool
}

// filter is what the user asked to see.
type filter struct {
	query string
	cat   category
	scope string // key of the session or tab drilled into; "" for everything
}

// lister builds listings from a catalog and the visit history.
type lister struct {
	cat    *catalog
	recent []state.Visit // newest first, without the origin
	origin place
}

func (l lister) list(f filter) listing {
	var out listing
	candidates := l.candidates(f)
	out.total = len(candidates)
	switch p := match.Compile(f.query); {
	case !p.Empty():
		out.search = true
		out.rows = l.rank(p, candidates)
	case f.scope != "":
		scope, _ := l.cat.find(f.scope)
		out.rows = l.tree(candidates, scope.depth+1)
	case f.cat == catAll:
		out.rows, out.sections = l.switcher()
	case f.cat == catAgents:
		out.rows, out.sections = l.agents(candidates)
	default:
		out.rows = l.flat(candidates, f.cat == catSessions)
	}
	return out
}

// candidates are the entries the category and scope admit, in tree order.
func (l lister) candidates(f filter) []int {
	var out []int
	for i, e := range l.cat.entries {
		if f.cat.admits(e) && (f.scope == "" || l.cat.within(i, f.scope)) {
			out = append(out, i)
		}
	}
	return out
}

func (l lister) flat(entries []int, tree bool) []row {
	rows := make([]row, len(entries))
	for k, i := range entries {
		rows[k] = row{entry: i, tree: tree}
	}
	return rows
}

// tree lists entries indented below a scope at depth base.
func (l lister) tree(entries []int, base int) []row {
	rows := make([]row, len(entries))
	for k, i := range entries {
		rows[k] = row{entry: i, depth: max(0, l.cat.entries[i].depth-base), tree: true}
	}
	return rows
}

// switcher is the empty-query view: NEEDS YOU (oldest first), WORKING,
// RECENT (previous location first), then every session as a tree. An
// agent only guessed to work is not listed as working: it is in its
// session.
func (l lister) switcher() ([]row, []ui.Section) {
	var needs, working []row
	for i, e := range l.cat.entries {
		switch {
		case e.needsYou():
			needs = append(needs, row{entry: i, ask: true})
		case e.kind == ui.KindAgent && e.status == ui.StatusWorking && !e.guess:
			working = append(working, row{entry: i})
		}
	}
	l.longestWaitingFirst(needs)
	all := make([]int, len(l.cat.entries))
	for i := range all {
		all[i] = i
	}
	var b sectionBuilder
	b.add(secNeedsYou, strconv.Itoa(len(needs)), needs)
	b.hint(secNeedsYou, waitingHint)
	b.add(secWorking, strconv.Itoa(len(working)), working)
	b.add(secRecent, "", l.recentRows())
	b.add(secSessions, "", l.tree(all, 0))
	return b.rows, b.sections
}

// recentRows resolves the visit history to entries that still exist: the
// pane if it is still there, else its tab, else its session.
func (l lister) recentRows() []row {
	var rows []row
	seen := map[int]bool{}
	for _, v := range l.recent {
		i, ok := l.resolve(v)
		if !ok || seen[i] || l.origin.at(l.cat.entries[i]) {
			continue
		}
		seen[i] = true
		rows = append(rows, row{entry: i, visited: v.At})
		if len(rows) == maxRecent {
			break
		}
	}
	return rows
}

func (l lister) resolve(v state.Visit) (int, bool) {
	for _, key := range []string{v.BlockID, v.WindowID, v.SessionID} {
		if i, ok := l.cat.index[key]; ok && key != "" {
			return i, true
		}
	}
	return 0, false
}

// agents groups agents by what they need: NEEDS YOU, WORKING, IDLE. An
// agent only guessed to work counts as idle (its row says "working?").
func (l lister) agents(entries []int) ([]row, []ui.Section) {
	var needs, working, idle []row
	for _, i := range entries {
		e := l.cat.entries[i]
		switch {
		case e.needsYou():
			needs = append(needs, row{entry: i, ask: true})
		case e.status == ui.StatusWorking && !e.guess:
			working = append(working, row{entry: i})
		default:
			idle = append(idle, row{entry: i})
		}
	}
	l.longestWaitingFirst(needs)
	var b sectionBuilder
	b.add(secNeedsYou, strconv.Itoa(len(needs)), needs)
	b.hint(secNeedsYou, waitingHint)
	b.add(secWorking, strconv.Itoa(len(working)), working)
	b.add(secIdle, strconv.Itoa(len(idle)), idle)
	return b.rows, b.sections
}

// rank scores entries against the query, adds boosts for places that matter
// now (needing attention, recently visited), and sorts best first.
func (l lister) rank(p *match.Pattern, entries []int) []row {
	type scored struct {
		row   row
		score int
	}
	var hits []scored
	best := 0
	for _, i := range entries {
		res, ok := p.Match(fieldsOf(l.cat.entries[i])...)
		if !ok {
			continue
		}
		best = max(best, res.Score)
		hits = append(hits, scored{row{entry: i, match: &res}, res.Score + l.boost(i)})
	}
	// Letters scattered through a long name score far below a real match;
	// such rows are noise next to it. So are rows that only matched by
	// hopping into the middle of words, once anything matched cleanly.
	clean := slices.ContainsFunc(hits, func(h scored) bool { return !scattered(h.row.match, fieldsOf(l.cat.entries[h.row.entry])) })
	hits = slices.DeleteFunc(hits, func(h scored) bool {
		return h.row.match.Score*100 < best*minRelativeScore ||
			clean && scattered(h.row.match, fieldsOf(l.cat.entries[h.row.entry]))
	})
	slices.SortStableFunc(hits, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return cmp.Compare(len(l.cat.entries[a.row.entry].name), len(l.cat.entries[b.row.entry].name))
	})
	rows := make([]row, len(hits))
	for k, h := range hits {
		rows[k] = h.row
	}
	return rows
}

// minRelativeScore is the percentage of the best match score a row needs to
// be listed at all.
const minRelativeScore = 40

// boost nudges ranking towards places the user is likely after. It is small
// next to match scores (16 per matched character), so it only breaks near-ties.
func (l lister) boost(i int) int {
	e := l.cat.entries[i]
	b := 0
	switch {
	case e.needsYou():
		b += 12
	case e.status == ui.StatusWorking:
		b += 4
	}
	for rank, v := range l.recent {
		if j, ok := l.resolve(v); ok && j == i {
			b += max(0, 20-4*rank)
			break
		}
	}
	if l.origin.at(e) {
		b -= 8 // you are already there
	}
	return b
}

// Match fields, in the order fieldsOf returns them.
const (
	fieldName = iota
	fieldPath
	fieldStatus
	fieldCwd
	fieldProgram
	fieldTask
	fieldTitle
)

// fieldsOf is what a query is matched against: what the row shows, then what
// it does not show but is worth finding it by.
func fieldsOf(e entry) []match.Field {
	task := e.task
	if task == e.name {
		task = ""
	}
	return []match.Field{
		fieldName:    {Text: e.name},
		fieldPath:    {Text: e.path(), Weight: 70},
		fieldStatus:  {Text: e.status.Word(), Weight: 50},
		fieldCwd:     {Text: e.cwd, Hidden: true},
		fieldProgram: {Text: e.program, Hidden: true},
		fieldTask:    {Text: task, Hidden: true},
		fieldTitle:   {Text: e.title, Hidden: true},
	}
}

// sectionBuilder concatenates sections of rows, skipping empty ones.
type sectionBuilder struct {
	rows     []row
	sections []ui.Section
}

func (b *sectionBuilder) add(title, note string, rows []row) {
	if len(rows) == 0 {
		return
	}
	b.sections = append(b.sections, ui.Section{Title: title, Note: note, Start: len(b.rows)})
	b.rows = append(b.rows, rows...)
}

// waitingHint is the key Needs you's header shows: ⌥A, the global "next
// agent that needs you", which in Go to selects the next of its rows.
var waitingHint = ui.Hint{Key: "alt+a", Label: "next"}

// hint gives the section added last a key hint, when it was added.
func (b *sectionBuilder) hint(title string, h ui.Hint) {
	if n := len(b.sections); n > 0 && b.sections[n-1].Title == title {
		b.sections[n-1].Hint = h
	}
}

// scattered reports a match that starts runs of matched letters in the middle
// of words more than once: "notes" found as n…o…t…e…s across
// "infrastructure-and-deployment-pipelines". One such hop is fine (an
// abbreviation like "kctl"); more is coincidence.
func scattered(res *match.Result, fields []match.Field) bool {
	hops := 0
	for f, pos := range res.Positions {
		if len(pos) == 0 {
			continue
		}
		text := []rune(fields[f].Text)
		for k, p := range pos {
			if k > 0 && pos[k-1] == p-1 {
				continue // inside a run
			}
			if p > 0 && p < len(text) && !wordStart(text[p-1], text[p]) {
				hops++
			}
		}
	}
	return hops > 1
}

// wordStart reports whether cur begins a word after prev: after a separator,
// at a lower-to-upper case change, or where letters and digits meet.
func wordStart(prev, cur rune) bool {
	switch {
	case !unicode.IsLetter(prev) && !unicode.IsDigit(prev):
		return true
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return true
	}
	return unicode.IsDigit(prev) != unicode.IsDigit(cur)
}

// longestWaitingFirst orders NEEDS YOU rows: blocked agents first (they
// cannot go on without you), then failed ones, then results to read; in
// each, whoever has waited longest is first.
func (l lister) longestWaitingFirst(rows []row) {
	slices.SortStableFunc(rows, func(a, b row) int {
		ea, eb := l.cat.entries[a.entry], l.cat.entries[b.entry]
		if c := cmp.Compare(attend.Rank(ea.state), attend.Rank(eb.state)); c != 0 {
			return c
		}
		return ea.since.Compare(eb.since)
	})
}
