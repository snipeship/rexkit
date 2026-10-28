package hints

import (
	"regexp"
	"slices"
	"strings"

	"rexkit/internal/panes"
)

// Kind is what a match is, which decides what opening it does.
type Kind int

// Kinds. Where matches overlap, a URL wins, then the longest: a path holds
// the UUID or pod-like name of a folder, and a pod name holds a hex run.
const (
	KindURL  Kind = iota // opened in the browser
	KindUUID             // a request or run id: copied
	KindPod              // a Kubernetes pod name: kubectl describe in a split
	KindIP               // an address, with its port: copied
	KindPath             // opened in the editor at its line, in a split
	KindSHA              // a commit (git show in a split) or a container id (docker logs -f)
)

func (k Kind) String() string {
	return [...]string{"url", "uuid", "pod", "ip", "path", "sha"}[k]
}

// pattern finds one kind of thing in a line of text.
type pattern struct {
	kind Kind
	re   *regexp.Regexp
	keep func(string) bool // nil keeps every match
}

var patterns = []pattern{
	{KindURL, regexp.MustCompile(`\bhttps?://[^\s<>"'` + "`" + `]+`), nil},
	{KindUUID, regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), nil},
	// A pod's name ends in the suffixes Kubernetes makes up (a ReplicaSet's
	// hash, then five more), from letters without vowels and digits without
	// 0, 1 and 3: "studio-api-7d9f8b6c5d-x2k4p", "kube-proxy-x2kqz". Words
	// joined by hyphens (fix-pipeline-crash) are not pods.
	{KindPod, regexp.MustCompile(`\b[a-z0-9]+(?:-[a-z0-9]+)*?(?:-` + podChars + `{6,10})?-` + podChars + `{5}\b`), nil},
	{KindIP, regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`), nil},
	{KindPath, regexp.MustCompile(`(?:~|\.{1,2})?/?(?:` + pathChars + `+/)*` + pathChars + `+(?::\d+){0,2}`), isPath},
	{KindSHA, regexp.MustCompile(`\b[0-9a-f]{7,40}\b`), isHex},
}

// podChars are the characters of the suffixes Kubernetes adds to a pod's
// name; pathChars those of a path's parts, in any script (café/notes.md).
const (
	podChars  = `[bcdfghjklmnpqrstvwxz2456789]`
	pathChars = `[\p{L}\p{M}\p{N}_.@+-]`
)

// pathExt is a file name's extension, where a path's last part has one.
var pathExt = regexp.MustCompile(`\.[a-z][a-z0-9]{0,7}$`)

// fileExts are the extensions a name needs to be read as a file when
// nothing else says it is one (no leading / ~ ./ ../). Code on screen is
// full of lowercase selectors that look like names with an extension
// (p.jobs, ctx.done, Queue.drain), and they are not files.
var fileExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`go mod sum rs py pyi js mjs cjs ts tsx jsx lua vim md mdx txt rst
		json jsonl yaml yml toml ini cfg conf env lock sql proto graphql sh bash zsh fish nu ps1
		c h cc cpp hpp m mm swift kt kts java scala gradle rb php ex exs erl hs ml zig nim dart
		css scss sass less html htm xml svg vue svelte astro tf tfvars hcl nix log csv tsv
		png jpg jpeg gif webp ico pdf wasm patch diff tmpl tpl dockerfile`) {
		fileExts[e] = true
	}
}

// isPath keeps what reads as a file: a path from / ~ or ., or a name with
// a file's extension (queue_test.go:88, internal/runs/pool.go). A bare word,
// a branch (feat/run-queue), a MIME type (application/json), a version
// (v5.7.1), a host (studio.dev) or a Go selector (p.jobs) is not one.
func isPath(s string) bool {
	file := strings.SplitN(s, ":", 2)[0]
	if file == "" || strings.HasSuffix(file, "/") && len(file) == 1 {
		return false
	}
	switch {
	case strings.HasPrefix(file, "/") && strings.Count(file, "/") >= 2,
		strings.HasPrefix(file, "~/"), strings.HasPrefix(file, "./"), strings.HasPrefix(file, "../"):
		return true
	}
	base := file[strings.LastIndex(file, "/")+1:]
	if !pathExt.MatchString(base) || strings.HasPrefix(base, ".") || strings.Count(base, ".") > 2 {
		return false
	}
	return fileExts[base[strings.LastIndex(base, ".")+1:]]
}

// isHex keeps hex runs with a digit and a letter: "a3f9c21", not "deadbeef"
// on its own or "1234567".
func isHex(s string) bool {
	return strings.ContainsAny(s, "0123456789") && strings.ContainsAny(s, "abcdef")
}

// Match is one thing found on the screen.
type Match struct {
	Row, Col, Width int // where it is, in cells
	Text            string
	Kind            Kind
	Label           string // the keys that pick it; equal texts share one
	// Chip is the column the label is drawn from: in the blank cells just
	// before the match when there are enough, else over its first cells
	// (place).
	Chip int
}

// rowText is a screen row as text, with the column each byte starts in.
func rowText(row []panes.Cell) (string, []int) {
	var b strings.Builder
	var cols []int
	for x, c := range row {
		if c.Width == 0 {
			continue
		}
		s := c.Content
		if s == "" {
			s = " "
		}
		for range len(s) {
			cols = append(cols, x)
		}
		b.WriteString(s)
	}
	cols = append(cols, len(row))
	return b.String(), cols
}

// Find returns every match on the screen, top to bottom and left to right.
// Where matches overlap, a URL wins, then the longest, so a path keeps the
// UUID or the hyphenated name inside it. Texts in skip are not matches: the
// pane's own working directory, which every prompt repeats; they still keep
// what is inside them from being one.
func Find(cells [][]panes.Cell, skip ...string) []Match {
	type found struct {
		from, to int
		kind     Kind
		skip     bool
	}
	var out []Match
	for y, row := range cells {
		text, cols := rowText(row)
		var all []found
		for _, p := range patterns {
			for _, loc := range p.re.FindAllStringIndex(text, -1) {
				from := loc[0]
				s := trim(text[from:loc[1]])
				to := from + len(s)
				if len(s) < 4 || p.keep != nil && !p.keep(s) || p.kind == KindSHA && !alone(text, from, to) {
					continue
				}
				all = append(all, found{from, to, p.kind, slices.Contains(skip, s)})
			}
		}
		slices.SortStableFunc(all, func(a, b found) int {
			if (a.kind == KindURL) != (b.kind == KindURL) {
				if a.kind == KindURL {
					return -1
				}
				return 1
			}
			return (b.to - b.from) - (a.to - a.from)
		})
		taken := make([]bool, len(text))
		var kept []Match
		for _, f := range all {
			if slices.Contains(taken[f.from:f.to], true) {
				continue
			}
			for i := f.from; i < f.to; i++ {
				taken[i] = true
			}
			if !f.skip {
				kept = append(kept, Match{Row: y, Col: cols[f.from], Width: cols[f.to] - cols[f.from], Text: text[f.from:f.to], Kind: f.kind})
			}
		}
		slices.SortFunc(kept, func(a, b Match) int { return a.Col - b.Col })
		out = append(out, kept...)
	}
	return out
}

// trim takes the punctuation a sentence puts after a match off its end: a
// full stop, a comma, quotes, and a closing bracket that opens nowhere in
// the match (a URL can end in "(bar)").
func trim(s string) string {
	for s != "" {
		last := s[len(s)-1:]
		if i := strings.Index(")]}>", last); i >= 0 {
			if strings.Count(s, "([{<"[i:i+1]) >= strings.Count(s, last) {
				return s
			}
		} else if !strings.Contains(".,;:!?'\"", last) {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// alone reports whether the hex run at text[from:to] stands on its own: not
// a piece of a hyphenated name, a cut UUID or a file name (a ReplicaSet's
// studio-api-7d9f8b6c5d, 4c1f0a9e-2b7d-…). Spaces, brackets and a range's
// ".." may sit next to it.
func alone(text string, from, to int) bool {
	joins := func(c byte) bool { return c == '-' || c == '_' || c == '/' }
	word := func(i int) bool { return i >= 0 && i < len(text) && isWordByte(text[i]) }
	if from > 0 && (joins(text[from-1]) || text[from-1] == '.' && word(from-2)) {
		return false
	}
	if to < len(text) && (joins(text[to]) || text[to] == '.' && word(to+1)) {
		return false
	}
	return true
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// rowString is a screen row as plain text, one rune a cell.
func rowString(row []panes.Cell) string {
	text, _ := rowText(row)
	return text
}

// namespace is the Kubernetes namespace of what is on row: the -n or
// --namespace of the nearest kubectl command at or above it, or, when that
// command lists every namespace (-A), the row's first column ("" for none).
func namespace(cells [][]panes.Cell, row int) string {
	for y := min(row, len(cells)-1); y >= 0; y-- {
		f := strings.Fields(rowString(cells[y]))
		at := slices.Index(f, "kubectl")
		if at < 0 {
			continue
		}
		for i := at + 1; i < len(f); i++ {
			switch arg := f[i]; {
			case (arg == "-n" || arg == "--namespace") && i+1 < len(f):
				return f[i+1]
			case strings.HasPrefix(arg, "--namespace="):
				return strings.TrimPrefix(arg, "--namespace=")
			case strings.HasPrefix(arg, "-n") && len(arg) > 2 && !strings.HasPrefix(arg, "--"):
				return arg[2:]
			case arg == "-A" || arg == "--all-namespaces" || arg == "--all-namespaces=true":
				if row == y || row >= len(cells) {
					return ""
				}
				if cols := strings.Fields(rowString(cells[row])); len(cols) > 1 {
					return cols[0]
				}
				return ""
			}
		}
		return ""
	}
	return ""
}
