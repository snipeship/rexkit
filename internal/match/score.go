package match

import "unicode"

// Scoring follows fzf's v2 algorithm: every matched rune earns scoreMatch,
// gaps cost gapStart+gapExtension per skipped rune, and runes that start a
// word (after a space, a delimiter or a camelCase hump) earn a bonus. The best
// alignment is found with dynamic programming, so "nav" in "fix navbar" picks
// the word "nav", not three scattered letters.
const (
	scoreMatch        = 16
	gapStart          = -3
	gapExtension      = -1
	bonusBoundary     = scoreMatch / 2
	bonusWhite        = bonusBoundary + 2
	bonusDelimiter    = bonusBoundary + 1
	bonusNonWord      = scoreMatch / 2
	bonusCamel        = bonusBoundary + gapExtension
	bonusConsecutive  = -(gapStart + gapExtension)
	firstCharMultiple = 2

	// bonusExact rewards a term that equals the whole field ("api" vs "api").
	bonusExact = 2 * scoreMatch
	// bonusStart rewards matching from the very first rune of a field.
	bonusStart = bonusWhite + 1
)

type charClass uint8

const (
	classWhite charClass = iota
	classDelimiter
	classNonWord
	classLower
	classUpper
	classLetter
	classNumber
)

func classOf(r rune) charClass {
	switch {
	case r >= 'a' && r <= 'z':
		return classLower
	case r >= 'A' && r <= 'Z':
		return classUpper
	case r >= '0' && r <= '9':
		return classNumber
	case r == ' ' || r == '\t' || r == '\n':
		return classWhite
	case r == '/' || r == '\\' || r == ',' || r == ':' || r == ';' || r == '|' || r == '-' || r == '_' || r == '.':
		return classDelimiter
	case r < 0x80:
		return classNonWord
	case unicode.IsLower(r):
		return classLower
	case unicode.IsUpper(r):
		return classUpper
	case unicode.IsLetter(r):
		return classLetter
	case unicode.IsNumber(r):
		return classNumber
	case unicode.IsSpace(r):
		return classWhite
	default:
		return classNonWord
	}
}

// bonusAt is the bonus for matching a rune of class cur that follows prev.
func bonusAt(prev, cur charClass) int {
	if cur > classNonWord {
		switch prev {
		case classWhite:
			return bonusWhite
		case classDelimiter:
			return bonusDelimiter
		case classNonWord:
			return bonusBoundary
		}
	}
	switch {
	case prev == classLower && cur == classUpper,
		prev != classNumber && cur == classNumber:
		return bonusCamel
	case cur == classNonWord || cur == classDelimiter:
		return bonusNonWord
	case cur == classWhite:
		return bonusWhite
	}
	return 0
}

// fold lowercases r for case-insensitive comparison.
func fold(r rune) rune {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}
	return unicode.ToLower(r)
}
