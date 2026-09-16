package filesystem

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NaturalLess compares two names using Unicode-aware natural ordering:
// digit runs are compared numerically (so 2 < 10), ties between numerically
// equal runs (1, 01, 001) fall back to fewer leading zeros first, and every
// other run is compared by Unicode code point, which keeps CJK names stable
// and grouped. Comparison is case-insensitive first, then case-sensitive to
// keep the ordering deterministic.
func NaturalLess(a, b string) bool {
	if a == b {
		return false
	}
	if r := naturalCompare(a, b); r != 0 {
		return r < 0
	}
	return a < b
}

func naturalCompare(a, b string) int {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := a[ia], b[ib]
		da, db := isDigit(ca), isDigit(cb)

		switch {
		case da && db:
			ra, na := digitRun(a, ia)
			rb, nb := digitRun(b, ib)
			if c := compareNumeric(ra, rb); c != 0 {
				return c
			}
			ia, ib = na, nb
		default:
			// Compare one rune at a time for correct Unicode handling.
			ra, sa := utf8.DecodeRuneInString(a[ia:])
			rb, sb := utf8.DecodeRuneInString(b[ib:])
			if c := compareRune(ra, rb); c != 0 {
				return c
			}
			ia += sa
			ib += sb
		}
	}
	switch {
	case ia < len(a):
		return 1
	case ib < len(b):
		return -1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRun(s string, i int) (string, int) {
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	return s[i:j], j
}

// compareNumeric compares two all-digit strings by numeric value, then by
// length (fewer leading zeros first) so "1" < "01" < "001".
func compareNumeric(a, b string) int {
	ta := strings.TrimLeft(a, "0")
	tb := strings.TrimLeft(b, "0")
	if ta == "" {
		ta = "0"
	}
	if tb == "" {
		tb = "0"
	}
	if len(ta) != len(tb) {
		if len(ta) < len(tb) {
			return -1
		}
		return 1
	}
	if c := strings.Compare(ta, tb); c != 0 {
		return c
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareRune(a, b rune) int {
	la, lb := unicode.ToLower(a), unicode.ToLower(b)
	switch {
	case la < lb:
		return -1
	case la > lb:
		return 1
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
