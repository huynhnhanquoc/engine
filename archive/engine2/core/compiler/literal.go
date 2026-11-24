package compiler

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// The literals of the source, read as ECMAScript reads them (03/11/2026): a string's and a template's
// escapes (12.9.4, 12.9.6) and a number's spellings (12.9.3), in strict mode, since a .work file is a
// module and a module is strict. A spelling JavaScript refuses is a compile error: the lexer gives its
// token a failure for a value, which the parser reports at the token (Parser.nextToken). Until then an
// escape Go's strconv did not know stayed as written (a backslash before a q was two characters, a
// code point escape in braces six), 0x1f and 1_000 did not lex, and 010 was ten.

// literalFailure is the value of a literal JavaScript refuses; its text is the compile error's.
func literalFailure(text string) value.Value {
	return value.Fail(value.InvalidInputCode, text)
}

// ---- strings and templates ----

// readString reads a string literal from its opening quote to its closing one and gives its value. A
// line break before the closing quote is an unterminated string, as in JavaScript (a backslash before
// it continues the line, and a template holds line breaks).
func (l *Lexer) readString(quote byte) value.Value {
	l.readChar() // the opening quote
	start := l.pos
	for l.ch != quote {
		if l.pos >= len(l.input) || l.ch == '\n' || l.ch == '\r' {
			return literalFailure(fmt.Sprintf("chuỗi chưa đóng: thiếu %c trước khi hết dòng; chuỗi nhiều dòng thì viết template `…`", quote))
		}
		if l.ch == '\\' {
			l.readChar()
			if l.pos >= len(l.input) {
				continue // the check above reports it
			}
			if l.ch == '\r' && l.peekChar() == '\n' {
				l.readChar() // CR LF after a backslash is one line break the string goes on past
			}
		}
		l.readChar()
	}
	raw := l.b2s(l.input[start:l.pos])
	l.readChar() // the closing quote
	cooked, _ := cookText(raw, false)
	return cooked
}

// readTemplate reads a template from its opening backtick to its closing one and gives its source text
// between them as written, escapes and ${…} included: the parser splits it (scanTemplate) and cooks
// each piece of text on its own, so an escaped dollar stays text and a string inside ${…} keeps its own
// escapes for its own lexer.
func (l *Lexer) readTemplate() value.Value {
	start := l.pos + 1
	_, end, closed := scanTemplate(l.input[start:])
	l.next = start + end
	l.readChar() // the closing backtick, or the end of the input
	if !closed {
		return literalFailure("template chưa đóng: thiếu ` ở cuối")
	}
	raw := string(l.input[start : start+end])
	l.readChar() // past the closing backtick
	return value.String(raw)
}

// templatePiece is a run of a template's text: written text, or the expression of a ${…} without the
// ${ and the }, by byte offset in the text.
type templatePiece struct {
	start, end int
	expression bool
}

// scanTemplate splits the text of a template, from just after its opening backtick, into its pieces up
// to its closing backtick: end is where that backtick is, len(text) when none closes it (closed false).
// A backslash escapes the character after it, so an escaped backtick or dollar is text; inside ${…}
// braces nest, and a string, a template or a comment there is passed over whole, so a } or a backtick
// in it ends nothing. Text that is empty is no piece.
func scanTemplate(text []byte) (pieces []templatePiece, end int, closed bool) {
	start := 0
	addText := func(to int) {
		if to > start {
			pieces = append(pieces, templatePiece{start: start, end: to})
		}
	}
	for i := 0; i < len(text); {
		switch text[i] {
		case '\\':
			i += 2
		case '`':
			addText(i)
			return pieces, i, true
		case '$':
			if i+1 < len(text) && text[i+1] == '{' {
				addText(i)
				closing, ok := skipTemplateExpression(text, i+2)
				if !ok {
					return pieces, len(text), false
				}
				pieces = append(pieces, templatePiece{start: i + 2, end: closing, expression: true})
				i = closing + 1
				start = i
				continue
			}
			i++
		default:
			i++
		}
	}
	addText(len(text))
	return pieces, len(text), false
}

// skipTemplateExpression finds the } that closes a ${ whose expression starts at i.
func skipTemplateExpression(text []byte, i int) (int, bool) {
	depth := 0
	for i < len(text) {
		switch text[i] {
		case '{':
			depth++
			i++
		case '}':
			if depth == 0 {
				return i, true
			}
			depth--
			i++
		case '\'', '"':
			quote := text[i]
			for i++; i < len(text) && text[i] != quote; i++ {
				if text[i] == '\\' {
					i++
				}
			}
			i++
		case '`':
			_, end, closed := scanTemplate(text[i+1:])
			if !closed {
				return len(text), false
			}
			i += end + 2
		case '/':
			if i+1 < len(text) && text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				continue
			}
			i++
		default:
			i++
		}
	}
	return len(text), false
}

// cookText is the value of a string literal's or a template's text: each escape read as ECMAScript
// reads it in strict mode, everything else as written. In a template, CR LF and a lone CR are a line
// feed, as JavaScript makes them. For an escape JavaScript refuses it is a failure, and at is where
// that escape starts in raw (-1 otherwise).
//
//   - b f n r t v are their control characters; 0 is U+0000 when no digit follows;
//   - x takes exactly two hex digits; u takes exactly four, or one or more in braces up to 10FFFF;
//     a high and a low surrogate side by side are one character, a lone one keeps its unit
//     (value.StringFromUnits writes both);
//   - a backslash before a line break is nothing: the line goes on;
//   - 1 to 9, and 0 before a digit, are octal escapes, a SyntaxError in strict code;
//   - any other character escaped is itself.
func cookText(raw string, template bool) (cooked value.Value, at int) {
	if strings.IndexByte(raw, '\\') < 0 && (!template || strings.IndexByte(raw, '\r') < 0) {
		// a copy, so that a constant does not keep the whole source alive
		return value.String(strings.Clone(raw)), -1
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		c := raw[i]
		if c == '\r' && template {
			out = append(out, '\n')
			i++
			if i < len(raw) && raw[i] == '\n' {
				i++
			}
			continue
		}
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		escape := i
		i++
		if i >= len(raw) {
			return literalFailure("dấu \\ ở cuối chuỗi không thoát ký tự nào"), escape
		}
		c = raw[i]
		switch c {
		case 'b':
			out = append(out, '\b')
			i++
		case 'f':
			out = append(out, '\f')
			i++
		case 'n':
			out = append(out, '\n')
			i++
		case 'r':
			out = append(out, '\r')
			i++
		case 't':
			out = append(out, '\t')
			i++
		case 'v':
			out = append(out, '\v')
			i++
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if c != '0' || (i+1 < len(raw) && isDecimalDigit(raw[i+1])) {
				end := i + 1
				for end < len(raw) && isDecimalDigit(raw[end]) {
					end++
				}
				return literalFailure(fmt.Sprintf("escape bát phân %s: JavaScript (strict) không nhận; ký tự mã 0 là \\0, ký tự khác viết \\x41", raw[escape:end])), escape
			}
			out = append(out, 0)
			i++
		case 'x':
			if i+2 >= len(raw) || !isHexDigit(raw[i+1]) || !isHexDigit(raw[i+2]) {
				return literalFailure(fmt.Sprintf("escape %s: \\x cần đúng hai chữ số hex, như \\x41", clipEscape(raw, escape, 4))), escape
			}
			out = utf8.AppendRune(out, rune(hexValue(raw[i+1])<<4|hexValue(raw[i+2])))
			i += 3
		case 'u':
			codePoint, size, ok := unicodeEscape(raw[i+1:])
			if !ok {
				return literalFailure(fmt.Sprintf("escape %s: sau \\%s là đúng bốn chữ số hex, hoặc một hay nhiều chữ số hex trong { } tới 10FFFF", clipEscape(raw, escape, 12), "u")), escape
			}
			out = appendCodePoint(out, codePoint)
			i += 1 + size
		case '\n':
			i++
		case '\r':
			i++
			if i < len(raw) && raw[i] == '\n' {
				i++
			}
		default:
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == 0x2028 || r == 0x2029 {
				// LINE SEPARATOR and PARAGRAPH SEPARATOR are line terminators: the line goes on
				i += size
				continue
			}
			out = append(out, raw[i:i+size]...)
			i += size
		}
	}
	return value.String(string(out)), -1
}

// unicodeEscape reads what follows a backslash and u: four hex digits, or hex digits in braces whose
// value is at most 10FFFF; size is how many bytes it read.
func unicodeEscape(s string) (codePoint rune, size int, ok bool) {
	if strings.HasPrefix(s, "{") {
		end := 1
		for end < len(s) && isHexDigit(s[end]) {
			if codePoint = codePoint<<4 | rune(hexValue(s[end])); codePoint > utf8.MaxRune {
				return 0, 0, false
			}
			end++
		}
		if end == 1 || end >= len(s) || s[end] != '}' {
			return 0, 0, false
		}
		return codePoint, end + 1, true
	}
	if len(s) < 4 {
		return 0, 0, false
	}
	for i := 0; i < 4; i++ {
		if !isHexDigit(s[i]) {
			return 0, 0, false
		}
		codePoint = codePoint<<4 | rune(hexValue(s[i]))
	}
	return codePoint, 4, true
}

// appendCodePoint writes a code point an escape gives. A surrogate is a UTF-16 unit of its own, in the
// three-byte form a program's string keeps one in (core/value text.go); a low one right after a lone
// high one makes one character with it, as two units side by side do in JavaScript.
func appendCodePoint(out []byte, codePoint rune) []byte {
	if codePoint < 0xD800 || codePoint > 0xDFFF {
		return utf8.AppendRune(out, codePoint)
	}
	if high, ok := endsWithHighSurrogate(out); ok && codePoint >= 0xDC00 {
		return append(out[:len(out)-3], value.StringFromUnits([]uint16{high, uint16(codePoint)}).Text()...)
	}
	return append(out, value.StringFromUnits([]uint16{uint16(codePoint)}).Text()...)
}

// endsWithHighSurrogate reads a lone high surrogate's three-byte form (ED A0..AF xx) at the end of out.
func endsWithHighSurrogate(out []byte) (uint16, bool) {
	n := len(out)
	if n < 3 || out[n-3] != 0xED || out[n-2] < 0xA0 || out[n-2] > 0xAF || out[n-1]&0xC0 != 0x80 {
		return 0, false
	}
	return 0xD000 | uint16(out[n-2]&0x3F)<<6 | uint16(out[n-1]&0x3F), true
}

// clipEscape is the escape at start as written, at most width bytes, for an error's text.
func clipEscape(raw string, start, width int) string {
	end := start + width
	if end > len(raw) {
		end = len(raw)
	}
	for end > start && !utf8.ValidString(raw[start:end]) {
		end--
	}
	return raw[start:end]
}

func isDecimalDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDecimalDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexValue(c byte) int {
	switch {
	case c >= 'a':
		return int(c-'a') + 10
	case c >= 'A':
		return int(c-'A') + 10
	}
	return int(c - '0')
}

// ---- numbers ----

// readNumber reads a numeric literal from the digit or the dot it starts with and gives its value: a
// decimal (1, 1.5, .5, 5., 1e3), or an integer in hex, octal or binary (0x1f, 0o17, 0b101), with a _
// between two digits wherever it helps reading (1_000). It is a failure for what strict JavaScript
// refuses: a 0 before a digit (the legacy octal 010, or 08), a _ that is not between two digits, an
// exponent or a prefix without digits, a letter or a digit right after the number (3in, 0b12, 1n).
func (l *Lexer) readNumber() value.Value {
	var failure value.Value
	fail := func(text string) {
		if !failure.IsInvalid() {
			failure = literalFailure(text)
		}
	}
	number := value.Value{}
	if base := numberBase(l.ch, l.peekChar()); base != 0 {
		prefix := string([]byte{l.ch, l.peekChar()})
		l.readChar()
		l.readChar()
		digits := l.readDigits(func(c byte) bool { return digitValue(c) < base }, fail)
		if digits == "" {
			fail(fmt.Sprintf("%s cần ít nhất một chữ số sau nó, như %s1", prefix, prefix))
		} else {
			integer, _ := new(big.Int).SetString(digits, base)
			// the nearest float64, ties to even, as JavaScript rounds an integer too long to be exact
			converted, _ := new(big.Float).SetInt(integer).Float64()
			number = value.Number(converted)
		}
	} else {
		var text strings.Builder
		if l.ch != '.' {
			legacy := l.ch == '0' && (isDecimalDigit(l.peekChar()) || l.peekChar() == '_')
			text.WriteString(l.readDigits(isDecimalDigit, fail))
			if legacy {
				fail("số bắt đầu bằng 0 rồi một chữ số (010, 08): JavaScript (strict) không nhận; bát phân viết 0o10, thập phân bỏ số 0 ở đầu")
			}
		}
		if l.ch == '.' {
			// one dot only: 5..toFixed(1) is 5. and .toFixed, 1.5.toFixed(1) is 1.5 and .toFixed
			l.readChar()
			text.WriteByte('.')
			text.WriteString(l.readDigits(isDecimalDigit, fail))
		}
		if l.ch == 'e' || l.ch == 'E' {
			l.readChar()
			text.WriteByte('e')
			if l.ch == '+' || l.ch == '-' {
				text.WriteByte(l.ch)
				l.readChar()
			}
			exponent := l.readDigits(isDecimalDigit, fail)
			if exponent == "" {
				fail("số mũ cần chữ số sau e, như 1e3")
			}
			text.WriteString(exponent)
		}
		converted, err := strconv.ParseFloat(text.String(), 64)
		if err != nil && !isRangeError(err) {
			fail("không đọc được số " + strconv.Quote(text.String()))
		}
		// past the largest float64 is Infinity (1e400), below the smallest 0, as JavaScript reads them
		number = value.Number(converted)
	}
	if Table[l.ch] == Alpha || Table[l.ch] == Digit {
		if l.ch == 'n' && Table[l.peekChar()] != Alpha && Table[l.peekChar()] != Digit {
			fail("Kitwork không có BigInt (1n): số là number")
		} else {
			fail("ngay sau một số không được là chữ hay chữ số: cách ra (3 in x), hoặc viết chữ số đúng hệ (0b101, 0o17, 0x1f)")
		}
		for Table[l.ch] == Alpha || Table[l.ch] == Digit {
			l.readChar() // the rest of the word, so the error is one token
		}
	}
	if failure.IsInvalid() {
		return failure
	}
	return number
}

// numberBase is the base a 0x, 0o or 0b prefix names; 0 for none.
func numberBase(first, second byte) int {
	if first != '0' {
		return 0
	}
	switch second {
	case 'x', 'X':
		return 16
	case 'o', 'O':
		return 8
	case 'b', 'B':
		return 2
	}
	return 0
}

// digitValue is the value of a hex digit; 99 for anything else.
func digitValue(c byte) int {
	if !isHexDigit(c) {
		return 99
	}
	return hexValue(c)
}

// readDigits reads digits of one kind and the separators between them, and gives the digits without
// the separators ("" when there is none). A _ stands only between two digits, as JavaScript allows it:
// one at either end, or two side by side, is a failure.
func (l *Lexer) readDigits(isDigit func(byte) bool, fail func(string)) string {
	var digits []byte
	for {
		switch {
		case isDigit(l.ch):
			digits = append(digits, l.ch)
		case l.ch == '_':
			if len(digits) == 0 || !isDigit(l.peekChar()) {
				fail("dấu _ trong một số chỉ đứng giữa hai chữ số, như 1_000")
			}
		default:
			return string(digits)
		}
		l.readChar()
	}
}

func isRangeError(err error) bool {
	numberError, ok := err.(*strconv.NumError)
	return ok && numberError.Err == strconv.ErrRange
}
