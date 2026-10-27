package find

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"
)

// Vim translates the query into a Vim pattern that finds the same text:
// very nomagic (\V), with \C or \c for its case. A regular expression is
// translated from its syntax tree; ok is false for one Vim cannot say the
// same way (\B, case folding switched on inside a case-sensitive query), and
// the caller then searches for the text it matched instead.
func (q Query) Vim() (pattern string, ok bool) {
	if q.Text == "" {
		return "", false
	}
	prefix := `\V\c`
	if q.Sensitive() {
		prefix = `\V\C`
	}
	if !q.Regex {
		return prefix + strings.ReplaceAll(q.Text, `\`, `\\`), true
	}
	if !q.Sensitive() && caseOff.MatchString(q.Text) {
		return "", false // (?-i): case matters in part of a query that ignores it
	}
	re, err := syntax.Parse(q.Text, syntax.Perl)
	if err != nil {
		return "", false
	}
	v := vimWriter{sensitive: q.Sensitive()}
	v.write(re)
	if v.failed {
		return "", false
	}
	return prefix + v.b.String(), true
}

// caseOff finds flags that turn case folding off: (?-i), (?s-i:.
var caseOff = regexp.MustCompile(`\(\?[a-zA-Z]*-[a-zA-Z]*i`)

type vimWriter struct {
	b         strings.Builder
	sensitive bool
	failed    bool
}

func (v *vimWriter) write(re *syntax.Regexp) {
	// One case for the whole pattern: (?i) inside a query that matches
	// case has no \c to say it.
	if re.Flags&syntax.FoldCase != 0 && v.sensitive && (re.Op == syntax.OpLiteral || re.Op == syntax.OpCharClass) {
		v.failed = true
		return
	}
	switch re.Op {
	case syntax.OpEmptyMatch:
	case syntax.OpLiteral:
		for _, r := range re.Rune {
			v.rune(r)
		}
	case syntax.OpCharClass:
		v.class(re.Rune)
	case syntax.OpAnyCharNotNL:
		v.b.WriteString(`\.`)
	case syntax.OpAnyChar:
		v.b.WriteString(`\_.`)
	case syntax.OpBeginLine, syntax.OpBeginText:
		v.b.WriteString(`\_^`) // a line is the whole text searched
	case syntax.OpEndLine, syntax.OpEndText:
		v.b.WriteString(`\_$`)
	case syntax.OpWordBoundary:
		v.b.WriteString(`\%(\<\|\>\)`)
	case syntax.OpCapture:
		// Go has no backreferences, so groups need no numbers: Vim allows
		// only nine that have them.
		v.group(re.Sub[0])
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		v.atom(re.Sub[0])
		v.repeat(re)
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpAlternate {
				v.group(sub)
			} else {
				v.write(sub)
			}
		}
	case syntax.OpAlternate:
		for i, sub := range re.Sub {
			if i > 0 {
				v.b.WriteString(`\|`)
			}
			v.write(sub)
		}
	default: // \B, a class that matches nothing
		v.failed = true
	}
}

// atom writes re so that a repetition after it applies to all of it.
func (v *vimWriter) atom(re *syntax.Regexp) {
	switch {
	case re.Op == syntax.OpLiteral && len(re.Rune) == 1,
		re.Op == syntax.OpCharClass, re.Op == syntax.OpAnyChar, re.Op == syntax.OpAnyCharNotNL,
		re.Op == syntax.OpCapture:
		v.write(re)
	default:
		v.group(re)
	}
}

// group writes re in a group that captures nothing.
func (v *vimWriter) group(re *syntax.Regexp) {
	v.b.WriteString(`\%(`)
	v.write(re)
	v.b.WriteString(`\)`)
}

func (v *vimWriter) repeat(re *syntax.Regexp) {
	lazy := re.Flags&syntax.NonGreedy != 0
	lo, hi := re.Min, re.Max
	switch re.Op {
	case syntax.OpStar:
		lo, hi = 0, -1
	case syntax.OpPlus:
		lo, hi = 1, -1
	case syntax.OpQuest:
		lo, hi = 0, 1
	}
	switch {
	case !lazy && lo == 0 && hi == -1:
		v.b.WriteString(`\*`)
		return
	case !lazy && lo == 1 && hi == -1:
		v.b.WriteString(`\+`)
		return
	case !lazy && lo == 0 && hi == 1:
		v.b.WriteString(`\=`)
		return
	}
	v.b.WriteString(`\{`)
	if lazy {
		v.b.WriteByte('-')
	}
	v.b.WriteString(strconv.Itoa(lo))
	switch {
	case hi < 0:
		v.b.WriteByte(',')
	case hi != lo:
		v.b.WriteString("," + strconv.Itoa(hi))
	}
	v.b.WriteByte('}')
}

// rune writes a literal character: in \V only the backslash is special.
func (v *vimWriter) rune(r rune) {
	switch {
	case r == '\\':
		v.b.WriteString(`\\`)
	case r < 0x20 || r == 0x7f:
		fmt.Fprintf(&v.b, `\%%x%02x`, r) // two digits: a digit after it is not part of it
	default:
		v.b.WriteRune(r)
	}
}

// class writes a character class given as ranges (lo, hi pairs). One that
// runs to the last code point is written as the negation of the rest.
func (v *vimWriter) class(ranges []rune) {
	negate := len(ranges) > 0 && ranges[len(ranges)-1] == unicode.MaxRune
	if negate {
		ranges = complement(ranges)
		if len(ranges) == 0 {
			v.b.WriteString(`\_.`) // every character
			return
		}
	}
	if len(ranges) == 0 {
		v.failed = true // matches nothing
		return
	}
	v.b.WriteString(`\[`)
	if negate {
		v.b.WriteByte('^')
	}
	for i := 0; i+1 < len(ranges); i += 2 {
		v.classRune(ranges[i])
		if ranges[i+1] != ranges[i] {
			v.b.WriteByte('-')
			v.classRune(ranges[i+1])
		}
	}
	v.b.WriteByte(']')
}

// classRune writes a character inside [ ]: ] \ ^ - escaped, controls and
// characters past the BMP by their code, in all its digits (a digit after
// it is not part of it).
func (v *vimWriter) classRune(r rune) {
	switch {
	case strings.ContainsRune(`]\^-`, r):
		v.b.WriteByte('\\')
		v.b.WriteRune(r)
	case r < 0x20 || r == 0x7f:
		fmt.Fprintf(&v.b, `\x%02x`, r)
	case r > 0xffff:
		fmt.Fprintf(&v.b, `\U%08x`, r)
	default:
		v.b.WriteRune(r)
	}
}

// complement returns the ranges of the characters not in ranges.
func complement(ranges []rune) []rune {
	var out []rune
	next := rune(0)
	for i := 0; i+1 < len(ranges); i += 2 {
		if ranges[i] > next {
			out = append(out, next, ranges[i]-1)
		}
		next = ranges[i+1] + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, next, unicode.MaxRune)
	}
	return out
}
