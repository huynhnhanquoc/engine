package builtins

import (
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// encodeURIComponent, decodeURIComponent, encodeURI and decodeURI follow ECMAScript's Encode and
// Decode (19.2.6): every character outside the set a function keeps is written as the %XX of each
// byte of its UTF-8, in upper-case hexadecimal, and decoding reads %XX sequences back into the
// characters they encode. The pairs differ only in their sets: the component pair keeps letters,
// digits and -_.!~*'() and decodes everything; encodeURI also keeps ;/?:@&=+$,# and decodeURI leaves
// the %XX of those characters as written, so a whole URI keeps its structure.
//
// JavaScript throws URIError for a lone surrogate to encode and for a malformed %XX sequence to decode
// (a % without two hexadecimal digits, a byte that starts no UTF-8 character, a sequence cut short or
// encoding a surrogate, an overlong form or a point past 0x10FFFF). The closed set of failure codes has
// no word for URIError: the text is input that cannot be read as a URI, so the failure is
// INVALID_INPUT, which is also what a JSON.parse of a bad text gives.

const (
	// uriMarks are the characters other than letters and digits that every function keeps.
	uriMarks = "-_.!~*'()"
	// uriReserved are the characters that give a URI its structure, kept by encodeURI and left encoded
	// by decodeURI, "#" with them.
	uriReserved = ";/?:@&=+$,#"
)

func EncodeURIComponent() value.Value {
	return native("encodeURIComponent", 1, func(arguments ...value.Value) value.Value {
		text, failure := argument(arguments, 0).ToText()
		if failure.IsInvalid() {
			return failure
		}
		return encode(text, "")
	})
}

func EncodeURI() value.Value {
	return native("encodeURI", 1, func(arguments ...value.Value) value.Value {
		text, failure := argument(arguments, 0).ToText()
		if failure.IsInvalid() {
			return failure
		}
		return encode(text, uriReserved)
	})
}

func DecodeURIComponent() value.Value {
	return native("decodeURIComponent", 1, func(arguments ...value.Value) value.Value {
		text, failure := argument(arguments, 0).ToText()
		if failure.IsInvalid() {
			return failure
		}
		return decode(text, "")
	})
}

func DecodeURI() value.Value {
	return native("decodeURI", 1, func(arguments ...value.Value) value.Value {
		text, failure := argument(arguments, 0).ToText()
		if failure.IsInvalid() {
			return failure
		}
		return decode(text, uriReserved)
	})
}

func malformedURI(what string) value.Value {
	return value.Fail(value.InvalidInputCode, "URI malformed: "+what)
}

// unescaped is the set encode keeps: letters, digits, the marks, and extra.
func unescaped(c byte, extra string) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		strings.IndexByte(uriMarks, c) >= 0 || strings.IndexByte(extra, c) >= 0
}

const upperHex = "0123456789ABCDEF"

// encode is ECMAScript's Encode: text is UTF-8 already, so each byte of a character outside the set is
// one %XX; a lone surrogate (in its three-byte form) is no character and fails. A result past the longest
// string is RANGE before anything is written, as JavaScript throws there: each byte can become three, so
// encodeURIComponent of 180 MB of "ạ" built 540 MB, two gigabytes of growth for one call (05/11/2026).
func encode(text string, extra string) value.Value {
	if len(text) > value.MaxStringLength/3 {
		length := len(text)
		for i := 0; i < len(text); i++ {
			if c := text[i]; c >= utf8.RuneSelf || !unescaped(c, extra) {
				length += 2
			}
		}
		if length > value.MaxStringLength {
			return value.TooLong()
		}
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf && unescaped(c, extra) {
			b.WriteByte(c)
			i++
			continue
		}
		if loneSurrogateAt(text, i) {
			return malformedURI("a lone surrogate cannot be encoded")
		}
		size := 1
		if c >= utf8.RuneSelf {
			_, size = utf8.DecodeRuneInString(text[i:])
		}
		for _, octet := range []byte(text[i : i+size]) {
			b.WriteByte('%')
			b.WriteByte(upperHex[octet>>4])
			b.WriteByte(upperHex[octet&0xF])
		}
		i += size
	}
	return value.String(b.String())
}

// decode is ECMAScript's Decode: each %XX is a byte, one of 0xxxxxxx a character on its own (left as
// written when it is in preserve), else the first of a UTF-8 sequence of 2 to 4 bytes, each following
// one a %XX of 10xxxxxx, the whole a valid encoding. Everything that is not a % is copied as it is.
func decode(text string, preserve string) value.Value {
	if strings.IndexByte(text, '%') < 0 {
		return value.String(text)
	}
	b := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] != '%' {
			b = append(b, text[i])
			i++
			continue
		}
		first, ok := hexOctet(text, i)
		if !ok {
			return malformedURI("% is not followed by two hexadecimal digits")
		}
		if first < utf8.RuneSelf {
			if strings.IndexByte(preserve, first) >= 0 {
				b = append(b, text[i:i+3]...)
			} else {
				b = append(b, first)
			}
			i += 3
			continue
		}
		var length int
		switch {
		case first&0xE0 == 0xC0:
			length = 2
		case first&0xF0 == 0xE0:
			length = 3
		case first&0xF8 == 0xF0:
			length = 4
		default:
			return malformedURI("a byte that begins no UTF-8 character")
		}
		sequence := []byte{first}
		at := i + 3
		for n := 1; n < length; n++ {
			octet, ok := hexOctet(text, at)
			if !ok || octet&0xC0 != 0x80 {
				return malformedURI("a UTF-8 sequence cut short")
			}
			sequence = append(sequence, octet)
			at += 3
		}
		// Go's decoder refuses what ECMAScript refuses: an overlong form, a surrogate, a point past
		// 0x10FFFF; it says so with RuneError of size 1, while the bytes of U+FFFD itself decode whole
		if r, size := utf8.DecodeRune(sequence); r == utf8.RuneError && size != length {
			return malformedURI("an invalid UTF-8 sequence")
		}
		b = append(b, sequence...)
		i = at
	}
	return value.String(string(b))
}

// hexOctet reads %XX at i: the byte the two hexadecimal digits write, and whether they are there.
func hexOctet(text string, i int) (byte, bool) {
	if i+2 >= len(text) || text[i] != '%' {
		return 0, false
	}
	high, okHigh := hexDigit(text[i+1])
	low, okLow := hexDigit(text[i+2])
	return high<<4 | low, okHigh && okLow
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
