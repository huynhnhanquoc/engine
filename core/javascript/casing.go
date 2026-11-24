package javascript

//go:generate go run casing_gen.go

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// toUpperCase and toLowerCase as JavaScript gives them (from engine2, 18/11/2026; checked against node): Unicode's full case
// mappings, a character to as many as it maps to ("ß" is "SS", "ﬁ" "FI", "İ" lower "i̇"), and the final sigma of
// lowercase ("ΑΣ" is "ας"). Go's strings.ToUpper and strings.ToLower map one character to one, so "ß" stayed "ß".
// casing_table.go holds what node gives where Go's one-to-one mapping differs, generated against node itself, so a
// character Unicode added after Go's tables maps as node maps it too. A text of ASCII alone takes Go's fast path; a
// byte of no character (a lone surrogate's) is kept as it is, as JavaScript keeps a lone surrogate.

// upperText is s.toUpperCase().
func upperText(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for at := 0; at < len(s); {
		if c := s[at]; c < utf8.RuneSelf {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			b.WriteByte(c)
			at++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[at:])
		switch mapped, special := lookUp(&upperMarks, upperDifferences, r); {
		case r == utf8.RuneError && size == 1:
			b.WriteByte(s[at])
		case special:
			b.WriteString(mapped)
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
		at += size
	}
	return b.String()
}

// lowerText is s.toLowerCase(): Σ is ς where it ends a word, a cased letter before it and none after it, the
// characters case ignores passed over both ways (Unicode's Final_Sigma), σ anywhere else.
func lowerText(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for at := 0; at < len(s); {
		if c := s[at]; c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
			at++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[at:])
		switch mapped, special := lookUp(&lowerMarks, lowerDifferences, r); {
		case r == utf8.RuneError && size == 1:
			b.WriteByte(s[at])
		case r == 0x03A3 && finalSigma(s, at, at+size): // capital sigma
			b.WriteRune(0x03C2) // final sigma
		case special:
			b.WriteString(mapped)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
		at += size
	}
	return b.String()
}

// upperMarks and lowerMarks mark the code points of the basic plane the tables hold, so a character is looked up in a
// map only where it may be: a lookup of each character made a text of Vietnamese two and a half times as slow to
// convert as Go's one-to-one mapping.
var upperMarks, lowerMarks = planeMarks(upperDifferences), planeMarks(lowerDifferences)

func planeMarks(table map[rune]string) (marks [0x10000 / 64]uint64) {
	for r := range table {
		if r <= 0xFFFF {
			marks[r>>6] |= 1 << (r & 63)
		}
	}
	return marks
}

// lookUp is what table maps r to, when it does.
func lookUp(marks *[0x10000 / 64]uint64, table map[rune]string, r rune) (string, bool) {
	if r <= 0xFFFF && marks[r>>6]&(1<<(r&63)) == 0 {
		return "", false
	}
	mapped, ok := table[r]
	return mapped, ok
}

// finalSigma reports whether the Σ at s[start:end] ends a word: a cased letter before it and none after it, each
// looked for past the characters case ignores.
func finalSigma(s string, start, end int) bool {
	before := false
	for at := start; at > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:at])
		at -= size
		if caseIgnorable(r) {
			continue
		}
		before = cased(r)
		break
	}
	if !before {
		return false
	}
	for at := end; at < len(s); {
		r, size := utf8.DecodeRuneInString(s[at:])
		at += size
		if caseIgnorable(r) {
			continue
		}
		return !cased(r)
	}
	return true
}

// cased is Unicode's Cased: a lowercase letter, an uppercase one, a titlecase one, Other_Lowercase, Other_Uppercase.
func cased(r rune) bool {
	return unicode.IsLower(r) || unicode.IsUpper(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

// caseIgnorable is Unicode's Case_Ignorable: a nonspacing or enclosing mark, a format character, a modifier letter or
// symbol, or a character whose Word_Break is MidLetter, MidNumLet or Single_Quote.
func caseIgnorable(r rune) bool {
	switch r {
	// Word_Break MidLetter, MidNumLet and Single_Quote
	case 0x0027, 0x002E, 0x003A, 0x00B7, 0x0387, 0x055F, 0x05F4, 0x2018, 0x2019, 0x2024, 0x2027, 0xFE13, 0xFE52,
		0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}
