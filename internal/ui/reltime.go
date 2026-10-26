package ui

import (
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

// RelTimeAt renders how long ago t was, in at most 6 cells: now, 12s, 3m, 5h,
// 2d, Oct 3 (this year), 2024 (earlier). The zero time renders as "".
func RelTimeAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 5*time.Second:
		return "now"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	default:
		return strconv.Itoa(t.Year())
	}
}

// Capitalize upper-cases the first letter of s (a whole rune, so "écran"
// becomes "Écran" and "⌃E edits" stays intact).
func Capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// Plural renders a count with its noun: "1 item", "3 items".
func Plural(n int, noun string) string {
	s := strconv.Itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}
