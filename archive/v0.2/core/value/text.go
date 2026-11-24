package value

import (
	"strings"
	"unicode/utf8"
)

// A program's string is a sequence of UTF-16 code units, as JavaScript's is: "Phường".length is 6 and
// "😀".length is 2. The payload is a Go string in UTF-8, with one extension: a lone surrogate (half of
// an emoji that slice cut) is kept as its three-byte form (WTF-8), so "😀".slice(0, 1) holds unit
// 0xD83D and joins back into "😀" when its other half follows. ASCII text takes the byte path; the
// unit path never allocates to count or compare, only to build a new string.

// ---- reading units ----

// unitReader walks a string one UTF-16 unit at a time without allocating.
type unitReader struct {
	text string
	at   int
	low  uint16 // the second unit of a pair, waiting
}

func (r *unitReader) next() (uint16, bool) {
	if r.low != 0 {
		unit := r.low
		r.low = 0
		return unit, true
	}
	if r.at >= len(r.text) {
		return 0, false
	}
	b := r.text[r.at]
	if b < utf8.RuneSelf {
		r.at++
		return uint16(b), true
	}
	if unit, ok := loneSurrogateAt(r.text, r.at); ok {
		r.at += 3
		return unit, true
	}
	c, size := utf8.DecodeRuneInString(r.text[r.at:])
	r.at += size
	if c < 0x10000 {
		return uint16(c), true // RuneError for a bad byte, as JavaScript shows U+FFFD
	}
	c -= 0x10000
	r.low = uint16(0xDC00 + c&0x3FF)
	return uint16(0xD800 + c>>10), true
}

// loneSurrogateAt reads the WTF-8 form of a surrogate (ED A0..BF xx) at i.
func loneSurrogateAt(s string, i int) (uint16, bool) {
	if i+3 > len(s) || s[i] != 0xED || s[i+1] < 0xA0 || s[i+1] > 0xBF || s[i+2]&0xC0 != 0x80 {
		return 0, false
	}
	return 0xD000 | uint16(s[i+1]&0x3F)<<6 | uint16(s[i+2]&0x3F), true
}

// isASCII reads eight bytes a step, as unicode/utf8 does: s.length runs it over the whole string on
// every read, so a loop to s.length pays it once per turn.
func isASCII(s string) bool {
	for len(s) >= 8 {
		first := uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
		second := uint32(s[4]) | uint32(s[5])<<8 | uint32(s[6])<<16 | uint32(s[7])<<24
		if (first|second)&0x80808080 != 0 {
			return false
		}
		s = s[8:]
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// units is the string as UTF-16 code units; nil for an ASCII string, which needs none.
func units(s string) []uint16 {
	if isASCII(s) {
		return nil
	}
	out := make([]uint16, 0, len(s))
	r := unitReader{text: s}
	for {
		unit, ok := r.next()
		if !ok {
			return out
		}
		out = append(out, unit)
	}
}

// unitLength is the string's length as a program sees it, counted without allocating.
func unitLength(s string) int {
	if isASCII(s) {
		return len(s)
	}
	n := 0
	r := unitReader{text: s}
	for {
		if _, ok := r.next(); !ok {
			return n
		}
		n++
	}
}

// ---- writing units ----

// appendUnit writes one unit: ASCII and BMP as UTF-8, a surrogate in its three-byte form.
func appendUnit(b []byte, unit uint16) []byte {
	switch {
	case unit < utf8.RuneSelf:
		return append(b, byte(unit))
	case unit >= 0xD800 && unit <= 0xDFFF:
		return append(b, 0xED, 0x80|byte(unit>>6)&0x3F, 0x80|byte(unit)&0x3F)
	}
	return utf8.AppendRune(b, rune(unit))
}

// encodeUnits writes units back to a string, pairing a high and a low surrogate into one character.
func encodeUnits(u []uint16) string {
	b := make([]byte, 0, len(u)*3)
	for i := 0; i < len(u); i++ {
		if u[i] >= 0xD800 && u[i] < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			b = utf8.AppendRune(b, 0x10000+(rune(u[i])-0xD800)<<10+(rune(u[i+1])-0xDC00))
			i++
			continue
		}
		b = appendUnit(b, u[i])
	}
	return string(b)
}

// StringFromUnits is a String of the given UTF-16 units, as String.fromCharCode makes one: a high and
// a low surrogate side by side become one character, a lone one keeps its three-byte form.
func StringFromUnits(u []uint16) Value { return String(encodeUnits(u)) }

var asciiStrings = func() (table [utf8.RuneSelf]string) {
	for i := range table {
		table[i] = string(rune(i))
	}
	return
}()

// unitString is one unit as a string: a table lookup for ASCII, so s[i] does not allocate.
func unitString(unit uint16) string {
	if unit < utf8.RuneSelf {
		return asciiStrings[unit]
	}
	return string(appendUnit(nil, unit))
}

// concatText is a + b for strings: when a ends with a high surrogate and b begins with the low one,
// the two halves become the character they were, as JavaScript's units do by being units.
func concatText(a, b string) string {
	if len(a) >= 3 && len(b) >= 3 && a[len(a)-3] == 0xED && b[0] == 0xED {
		high, okHigh := loneSurrogateAt(a, len(a)-3)
		low, okLow := loneSurrogateAt(b, 0)
		if okHigh && okLow && high < 0xDC00 && low >= 0xDC00 {
			return a[:len(a)-3] + encodeUnits([]uint16{high, low}) + b[3:]
		}
	}
	return a + b
}

// mergeSurrogates joins every high surrogate followed by its low one, in a string built from pieces
// (join, concat, repeat), into the character they make; a string without the three-byte form is
// returned as it is, with no copy.
func mergeSurrogates(s string) string {
	if strings.IndexByte(s, 0xED) < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if high, ok := loneSurrogateAt(s, i); ok && high < 0xDC00 {
			if low, ok := loneSurrogateAt(s, i+3); ok && low >= 0xDC00 {
				b = append(b, encodeUnits([]uint16{high, low})...)
				i += 6
				continue
			}
		}
		b = append(b, s[i])
		i++
	}
	return string(b)
}

// unitForm is s with every character past U+FFFF written as its two halves, each in the three-byte form
// of a lone surrogate, so that a byte search of it is a search unit by unit: a lone half of a search meets
// the half of a pair, which no byte of the four-byte form can. mergeSurrogates writes such a text back.
// Every other byte is kept. s itself when it has no such character.
func unitForm(s string) string {
	first := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xF0 {
			first = i
			break
		}
	}
	if first < 0 {
		return s
	}
	b := make([]byte, 0, len(s)+len(s)/2)
	b = append(b, s[:first]...)
	for i := first; i < len(s); {
		if s[i] < 0xF0 {
			b = append(b, s[i])
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size != 4 {
			b = append(b, s[i])
			i++
			continue
		}
		r -= 0x10000
		b = appendUnit(b, uint16(0xD800+r>>10))
		b = appendUnit(b, uint16(0xDC00+r&0x3FF))
		i += 4
	}
	return string(b)
}

// ---- unit positions ----

// byteOffset is where unit i begins in s, and whether i falls on the second unit of a pair (the
// middle of a four-byte character), which no byte offset can express.
func byteOffset(s string, i int) (offset int, midPair bool) {
	if i <= 0 {
		return 0, false
	}
	r := unitReader{text: s}
	for n := 0; n < i; n++ {
		if _, ok := r.next(); !ok {
			return len(s), false
		}
	}
	return r.at, r.low != 0
}

// hasLoneSurrogate reports the three-byte form of a surrogate anywhere in s: text that only a
// unit-by-unit comparison can match inside a pair. 0xED also leads U+D000–U+D7FF (most Hangul
// syllables), which are not surrogates: the search goes on past them, and ends when no 0xED is left
// (it once stayed on the last one forever, so "abc".indexOf("한") never returned).
func hasLoneSurrogate(s string) bool {
	for i := strings.IndexByte(s, 0xED); i >= 0; {
		if _, ok := loneSurrogateAt(s, i); ok {
			return true
		}
		next := strings.IndexByte(s[i+1:], 0xED)
		if next < 0 {
			return false
		}
		i += next + 1
	}
	return false
}

// unitSlice is s[start:end] by units, both already clamped to 0..length. When both ends fall on
// character boundaries the result shares the string's bytes; only a cut through a pair encodes.
func unitSlice(s string, start, end int) string {
	if start >= end {
		return ""
	}
	if isASCII(s) {
		return s[start:end]
	}
	from, cutStart := byteOffset(s, start)
	to, cutEnd := byteOffset(s, end)
	if !cutStart && !cutEnd {
		return s[from:to]
	}
	return encodeUnits(units(s)[start:end])
}

// unitIndex is the unit index of the first needle in s at or after from, -1 when none. Bytes find
// the same matches as units, except a lone-surrogate needle inside a pair, which goes unit by unit.
func unitIndex(s, needle string, from int) int {
	if from > unitLength(s) {
		return -1
	}
	if isASCII(s) && isASCII(needle) {
		i := strings.Index(s[from:], needle)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if hasLoneSurrogate(needle) {
		return unitIndexSlow(s, needle, from)
	}
	offset, midPair := byteOffset(s, from)
	if midPair {
		return unitIndexSlow(s, needle, from)
	}
	i := strings.Index(s[offset:], needle)
	if i < 0 {
		return -1
	}
	return from + unitLength(s[offset:offset+i])
}

func unitIndexSlow(s, needle string, from int) int {
	haystack, pattern := unitsOrASCII(s), unitsOrASCII(needle)
	for at := from; at+len(pattern) <= len(haystack); at++ {
		if equalUnits(haystack[at:at+len(pattern)], pattern) {
			return at
		}
	}
	return -1
}

// unitLastIndex is the unit index of the last needle in s starting at or before from.
func unitLastIndex(s, needle string, from int) int {
	if isASCII(s) && isASCII(needle) {
		for at := min(from, len(s)-len(needle)); at >= 0; at-- {
			if s[at:at+len(needle)] == needle {
				return at
			}
		}
		return -1
	}
	if hasLoneSurrogate(needle) {
		return unitLastIndexSlow(s, needle, from)
	}
	limit, midPair := byteOffset(s, from)
	if midPair {
		return unitLastIndexSlow(s, needle, from)
	}
	i := strings.LastIndex(s[:min(len(s), limit+len(needle))], needle)
	if i < 0 {
		return -1
	}
	return unitLength(s[:i])
}

func unitLastIndexSlow(s, needle string, from int) int {
	haystack, pattern := unitsOrASCII(s), unitsOrASCII(needle)
	for at := min(from, len(haystack)-len(pattern)); at >= 0; at-- {
		if equalUnits(haystack[at:at+len(pattern)], pattern) {
			return at
		}
	}
	return -1
}

func unitsOrASCII(s string) []uint16 {
	if u := units(s); u != nil {
		return u
	}
	return asciiUnits(s)
}

func asciiUnits(s string) []uint16 {
	out := make([]uint16, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = uint16(s[i])
	}
	return out
}

func equalUnits(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unitAt is the unit at index i and whether it exists. When the bytes up to i are ASCII they are the
// units up to i, so s[i] reads only those, not the whole string. A string never has more units than
// bytes (a unit takes one byte or more, a pair four), so an index at or past the bytes is past the
// units too.
func unitAt(s string, i int) (uint16, bool) {
	if i < 0 || i >= len(s) {
		return 0, false
	}
	if isASCII(s[:i+1]) {
		return uint16(s[i]), true
	}
	r := unitReader{text: s}
	for n := 0; ; n++ {
		unit, ok := r.next()
		if !ok {
			return 0, false
		}
		if n == i {
			return unit, true
		}
	}
}

// compareText orders two strings by unit, as JavaScript's < does: -1, 0 or 1. Byte order is unit
// order for ASCII and for the basic plane; an emoji (a surrogate pair) sorts below U+E000–U+FFFF.
func compareText(a, b string) int {
	order, _ := compareTextRead(a, b)
	return order
}

// compareTextRead is compareText with what it read in units of energy (CompareTexts): the ASCII each text was
// checked for and the bytes compared after it, a unit for EqualBytesPerUnit, or compareStepUnits a unit of each
// of two texts other than ASCII, read to the first that differs. A text is checked for ASCII as isASCII checks it
// (asciiPrefix), the second only when the first is not.
func compareTextRead(a, b string) (int, uint64) {
	checked := asciiPrefix(a)
	if checked < len(a) {
		ascii := asciiPrefix(b)
		checked += ascii
		if ascii < len(b) {
			x, y := unitReader{text: a}, unitReader{text: b}
			order, steps := 0, uint64(0)
			for order == 0 {
				u, okU := x.next()
				w, okW := y.next()
				steps++
				switch {
				case !okU && !okW:
					return 0, uint64(checked)/EqualBytesPerUnit + steps*compareStepUnits
				case !okU, okW && u < w:
					order = -1
				case !okW, u > w:
					order = 1
				}
			}
			return order, uint64(checked)/EqualBytesPerUnit + steps*compareStepUnits
		}
	}
	return strings.Compare(a, b), uint64(checked+min(len(a), len(b))) / EqualBytesPerUnit
}

// relativeIndex is JavaScript's relative index for slice and at: negative counts from the end, the
// result clamped to 0..length.
func relativeIndex(n float64, length int) int {
	i := toIntegerOrInfinity(n)
	if i < 0 {
		i += float64(length)
		if i < 0 {
			return 0
		}
	}
	if i > float64(length) {
		return length
	}
	return int(i)
}

// clampIndex is substring's: negative is 0, past the end is the end.
func clampIndex(n float64, length int) int {
	i := toIntegerOrInfinity(n)
	if i < 0 {
		return 0
	}
	if i > float64(length) {
		return length
	}
	return int(i)
}

// ---- white space ----

// trimJavaScriptSpace drops what JavaScript's trim() and StringToNumber drop around a text: its
// WhiteSpace and LineTerminator characters. Go's unicode.IsSpace differs on two: U+0085 and U+FEFF.
func trimJavaScriptSpace(text string) string {
	return trimJavaScriptSpaceRight(trimJavaScriptSpaceLeft(text))
}

func trimJavaScriptSpaceLeft(text string) string {
	for text != "" {
		c, size := utf8.DecodeRuneInString(text)
		if !javaScriptSpace(c) {
			break
		}
		text = text[size:]
	}
	return text
}

func trimJavaScriptSpaceRight(text string) string {
	for text != "" {
		c, size := utf8.DecodeLastRuneInString(text)
		if !javaScriptSpace(c) {
			break
		}
		text = text[:len(text)-size]
	}
	return text
}

func javaScriptSpace(c rune) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}
