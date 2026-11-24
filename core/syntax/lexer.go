// Package syntax đọc mã nguồn của một chương trình Kitwork (một tập con của JavaScript) thành cây cú pháp: lexer.go
// tách chữ, ast.go là các nút, parser.go dựng cây và từ chối những gì ngôn ngữ không có. Lát đầu của runtime engine3
// (23/11/2026): đủ cho application.work (import, const, let, gọi nối chữ, object, mảng, hàm mũi tên, template, return,
// if) và các hàm của nó; vòng lặp và phần còn lại tới ở lát sau, mỗi thứ là một lỗi nói rõ khi chưa có.
package syntax

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pos là chỗ trong mã nguồn: dòng và cột, đếm từ 1 (cột đếm ký tự).
type Pos struct{ Line, Column int }

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Column) }

// Kind là loại của một token.
type Kind int

const (
	EOF            Kind = iota
	Name                // tên hay từ khoá: website, const, return
	Number              // 8080, 1.5, 0xff
	String              // "a", 'b'
	TemplateFull        // `a` không có ${}
	TemplateHead        // `a${
	TemplateMiddle      // }b${
	TemplateTail        // }c`
	Punct               // { } ( ) . === => …
)

// Token là một chữ của mã nguồn.
type Token struct {
	Kind    Kind
	Text    string  // tên, dấu, hay chữ gốc của số
	Value   string  // chuỗi đã giải mã (String và các phần template)
	Number  float64 // Number
	Pos     Pos
	Newline bool // có xuống dòng giữa token trước và token này (ASI: return xuống dòng là return không giá trị)
}

// Error là một lỗi của mã nguồn ở một chỗ: "application.work:3:7: …".
type Error struct {
	File    string
	Pos     Pos
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s:%s: %s", e.File, e.Pos, e.Message) }

// punctuators là các dấu, dài trước ngắn sau để khớp dài nhất.
var punctuators = []string{
	"===", "!==", "**=", "...", "&&=", "||=", "??=", ">>>",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=", "*=", "/=", "%=", "**", "<<", ">>",
	"{", "}", "(", ")", "[", "]", ";", ",", ".", ":", "?", "=", "+", "-", "*", "/", "%", "<", ">", "!", "&", "|", "^", "~",
}

// lexer tách source thành token khi parser hỏi.
type lexer struct {
	file      string
	source    string
	at        int // byte đang đọc
	line, col int
	templates []int // mỗi template đang mở một phần tử: số dấu { chưa đóng trong ${…} của nó
}

func newLexer(file, source string) *lexer {
	return &lexer{file: file, source: source, line: 1, col: 1}
}

func (l *lexer) fail(p Pos, format string, arguments ...any) *Error {
	return &Error{File: l.file, Pos: p, Message: fmt.Sprintf(format, arguments...)}
}

func (l *lexer) pos() Pos { return Pos{l.line, l.col} }

// peekRune là ký tự ở at+offset byte (0 khi hết).
func (l *lexer) peekByte(offset int) byte {
	if l.at+offset < len(l.source) {
		return l.source[l.at+offset]
	}
	return 0
}

// advance đọc qua một ký tự, giữ dòng và cột.
func (l *lexer) advance() rune {
	r, size := utf8.DecodeRuneInString(l.source[l.at:])
	l.at += size
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// skip đọc qua khoảng trắng và chú thích; trả có xuống dòng không.
func (l *lexer) skip() (bool, *Error) {
	newline := false
	for l.at < len(l.source) {
		c := l.source[l.at]
		switch {
		case c == '\n':
			newline = true
			l.advance()
		case c == ' ' || c == '\t' || c == '\r':
			l.advance()
		case c == '/' && l.peekByte(1) == '/':
			for l.at < len(l.source) && l.source[l.at] != '\n' {
				l.advance()
			}
		case c == '/' && l.peekByte(1) == '*':
			start := l.pos()
			l.advance()
			l.advance()
			for {
				if l.at >= len(l.source) {
					return newline, l.fail(start, "a comment /* is never closed")
				}
				if l.source[l.at] == '*' && l.peekByte(1) == '/' {
					l.advance()
					l.advance()
					break
				}
				if l.advance() == '\n' {
					newline = true
				}
			}
		default:
			r, _ := utf8.DecodeRuneInString(l.source[l.at:])
			if r == 0xFEFF || r == 0x00A0 || unicode.Is(unicode.Zs, r) {
				l.advance()
				continue
			}
			if r == 0x2028 || r == 0x2029 {
				newline = true
				l.advance()
				continue
			}
			return newline, nil
		}
	}
	return newline, nil
}

// next là token kế tiếp.
func (l *lexer) next() (Token, *Error) {
	newline, err := l.skip()
	if err != nil {
		return Token{}, err
	}
	start := l.pos()
	if l.at >= len(l.source) {
		return Token{Kind: EOF, Pos: start, Newline: newline}, nil
	}
	c := l.source[l.at]
	r, _ := utf8.DecodeRuneInString(l.source[l.at:])
	switch {
	case isNameStart(r):
		begin := l.at
		for l.at < len(l.source) {
			r, _ := utf8.DecodeRuneInString(l.source[l.at:])
			if !isNamePart(r) {
				break
			}
			l.advance()
		}
		return Token{Kind: Name, Text: l.source[begin:l.at], Pos: start, Newline: newline}, nil
	case c >= '0' && c <= '9', c == '.' && l.peekByte(1) >= '0' && l.peekByte(1) <= '9':
		t, err := l.number(start)
		t.Newline = newline
		return t, err
	case c == '"' || c == '\'':
		t, err := l.quoted(start, c)
		t.Newline = newline
		return t, err
	case c == '`':
		l.advance()
		t, err := l.template(start, TemplateFull, TemplateHead)
		t.Newline = newline
		return t, err
	case c == '}' && len(l.templates) > 0 && l.templates[len(l.templates)-1] == 0:
		l.templates = l.templates[:len(l.templates)-1]
		l.advance()
		t, err := l.template(start, TemplateTail, TemplateMiddle)
		t.Newline = newline
		return t, err
	}
	for _, p := range punctuators {
		if strings.HasPrefix(l.source[l.at:], p) {
			for range p {
				l.advance()
			}
			if len(l.templates) > 0 {
				switch p {
				case "{":
					l.templates[len(l.templates)-1]++
				case "}":
					l.templates[len(l.templates)-1]--
				}
			}
			return Token{Kind: Punct, Text: p, Pos: start, Newline: newline}, nil
		}
	}
	return Token{}, l.fail(start, "unexpected character %q", r)
}

func isNameStart(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) }
func isNamePart(r rune) bool {
	return isNameStart(r) || unicode.IsDigit(r) || r == 0x200C || r == 0x200D || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r)
}

// number đọc một số: thập phân (1, 1.5, .5, 1e3), 0x, 0o, 0b; dấu _ giữa hai chữ số như JavaScript. BigInt (1n) chưa có.
func (l *lexer) number(start Pos) (Token, *Error) {
	begin := l.at
	base := 10
	if l.source[l.at] == '0' {
		switch l.peekByte(1) | 0x20 {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		}
		if base != 10 {
			l.advance()
			l.advance()
		}
	}
	digits := func(accept func(byte) bool) {
		for l.at < len(l.source) && (accept(l.source[l.at]) || l.source[l.at] == '_') {
			l.advance()
		}
	}
	decimal := func(c byte) bool { return c >= '0' && c <= '9' }
	if base != 10 {
		digits(func(c byte) bool {
			switch base {
			case 16:
				return decimal(c) || (c|0x20 >= 'a' && c|0x20 <= 'f')
			case 8:
				return c >= '0' && c <= '7'
			}
			return c == '0' || c == '1'
		})
	} else {
		digits(decimal)
		if l.at < len(l.source) && l.source[l.at] == '.' {
			l.advance()
			digits(decimal)
		}
		if l.at < len(l.source) && l.source[l.at]|0x20 == 'e' {
			l.advance()
			if c := l.peekByte(0); c == '+' || c == '-' {
				l.advance()
			}
			digits(decimal)
		}
	}
	text := l.source[begin:l.at]
	if l.at < len(l.source) {
		if r, _ := utf8.DecodeRuneInString(l.source[l.at:]); isNamePart(r) {
			if r == 'n' {
				return Token{}, l.fail(start, "BigInt (%sn) is not in Kitwork: numbers are float64", text)
			}
			return Token{}, l.fail(start, "a number %s is followed by %q", text, r)
		}
	}
	clean := strings.ReplaceAll(text, "_", "")
	if strings.Contains(text, "__") || strings.HasSuffix(text, "_") || strings.Contains(text, "_.") || strings.Contains(text, "._") {
		return Token{}, l.fail(start, "a number %s has a separator _ that is not between two digits", text)
	}
	var n float64
	if base != 10 {
		u, err := strconv.ParseUint(clean[2:], base, 64)
		if err != nil {
			if len(clean) == 2 {
				return Token{}, l.fail(start, "a number %s has no digits", text)
			}
			f, ok := new(big).parse(clean[2:], base)
			if !ok {
				return Token{}, l.fail(start, "%s is not a number", text)
			}
			n = f
		} else {
			n = float64(u)
		}
	} else {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil && !strings.Contains(err.Error(), "value out of range") {
			return Token{}, l.fail(start, "%s is not a number", text)
		}
		n = f
	}
	return Token{Kind: Number, Text: text, Number: n, Pos: start}, nil
}

// big đọc một số nguyên quá 64 bit ở cơ số base thành float64 (làm tròn như JavaScript đọc một chữ số dài).
type big struct{}

func (big) parse(digits string, base int) (float64, bool) {
	n := 0.0
	for _, c := range strings.ToLower(digits) {
		d := strings.IndexRune("0123456789abcdef", c)
		if d < 0 || d >= base {
			return 0, false
		}
		n = n*float64(base) + float64(d)
	}
	return n, !math.IsInf(n, 0)
}

// quoted đọc một chuỗi trong dấu nháy quote.
func (l *lexer) quoted(start Pos, quote byte) (Token, *Error) {
	l.advance()
	var b strings.Builder
	for {
		if l.at >= len(l.source) {
			return Token{}, l.fail(start, "a string is never closed: end it with %c", quote)
		}
		c := l.source[l.at]
		switch {
		case c == quote:
			l.advance()
			return Token{Kind: String, Value: mergeSurrogates(b.String()), Text: string(quote), Pos: start}, nil
		case c == '\n' || c == '\r':
			return Token{}, l.fail(start, "a string is never closed on its line: end it with %c, or write `…` for many lines", quote)
		case c == '\\':
			if err := l.escape(&b, false); err != nil {
				return Token{}, err
			}
		default:
			b.WriteRune(l.advance())
		}
	}
}

// template đọc phần chữ của một template tới ` (kết quả là end) hay ${ (kết quả là open, và một template mở thêm).
func (l *lexer) template(start Pos, end, open Kind) (Token, *Error) {
	var b strings.Builder
	for {
		if l.at >= len(l.source) {
			return Token{}, l.fail(start, "a template is never closed: end it with `")
		}
		c := l.source[l.at]
		switch {
		case c == '`':
			l.advance()
			return Token{Kind: end, Value: mergeSurrogates(b.String()), Pos: start}, nil
		case c == '$' && l.peekByte(1) == '{':
			l.advance()
			l.advance()
			l.templates = append(l.templates, 0)
			return Token{Kind: open, Value: mergeSurrogates(b.String()), Pos: start}, nil
		case c == '\\':
			if err := l.escape(&b, true); err != nil {
				return Token{}, err
			}
		case c == '\r':
			l.advance()
			if l.peekByte(0) == '\n' {
				l.advance()
			}
			b.WriteByte('\n') // JavaScript đọc \r\n và \r trong template là \n
		default:
			b.WriteRune(l.advance())
		}
	}
}

// escape đọc một dãy thoát bắt đầu bằng \ và viết ký tự của nó. Đơn vị UTF-16 đứng một mình (nửa cặp) viết dạng WTF-8,
// như mọi chuỗi của Kitwork; mergeSurrogates gộp hai nửa liền nhau thành một ký tự.
func (l *lexer) escape(b *strings.Builder, template bool) *Error {
	at := l.pos()
	l.advance() // \
	if l.at >= len(l.source) {
		return l.fail(at, "a string ends inside an escape")
	}
	c := l.advance()
	switch c {
	case 'n':
		b.WriteByte('\n')
	case 't':
		b.WriteByte('\t')
	case 'r':
		b.WriteByte('\r')
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'v':
		b.WriteByte('\v')
	case '0':
		if d := l.peekByte(0); d >= '0' && d <= '9' {
			return l.fail(at, "an octal escape is not in a module: write the character, or a u-escape")
		}
		b.WriteByte(0)
	case '\n':
		// dòng nối: không viết gì
	case '\r':
		if l.peekByte(0) == '\n' {
			l.advance()
		}
	case 'x':
		n, ok := l.hex(2)
		if !ok {
			return l.fail(at, "an x-escape takes two hex digits")
		}
		b.WriteRune(rune(n))
	case 'u':
		var n int
		var ok bool
		if l.peekByte(0) == '{' {
			l.advance()
			n, ok = l.hexUntilBrace()
			if !ok || n > 0x10FFFF {
				return l.fail(at, "a u{…} escape takes a code point up to 10FFFF")
			}
		} else if n, ok = l.hex(4); !ok {
			return l.fail(at, "a u-escape takes four hex digits, or {…}")
		}
		writeUnit(b, n)
	default:
		if c >= '1' && c <= '9' {
			return l.fail(at, "an octal escape is not in a module: write the character, or a u-escape")
		}
		b.WriteRune(c) // \" \' \\ \` \$ và mọi ký tự khác là chính nó
	}
	return nil
}

func (l *lexer) hex(count int) (int, bool) {
	n := 0
	for range count {
		d := strings.IndexByte("0123456789abcdef", l.peekByte(0)|0x20)
		if d < 0 || l.at >= len(l.source) {
			return 0, false
		}
		n = n*16 + d
		l.advance()
	}
	return n, true
}

func (l *lexer) hexUntilBrace() (int, bool) {
	n, digits := 0, 0
	for l.at < len(l.source) && l.source[l.at] != '}' {
		d := strings.IndexByte("0123456789abcdef", l.source[l.at]|0x20)
		if d < 0 || digits > 8 {
			return 0, false
		}
		n = n*16 + d
		digits++
		l.advance()
	}
	if l.at >= len(l.source) || digits == 0 {
		return 0, false
	}
	l.advance() // }
	return n, true
}

// writeUnit viết một code point; một nửa cặp (D800–DFFF) viết ba byte WTF-8.
func writeUnit(b *strings.Builder, n int) {
	if n >= 0xD800 && n <= 0xDFFF {
		b.WriteByte(byte(0xE0 | n>>12))
		b.WriteByte(byte(0x80 | (n>>6)&0x3F))
		b.WriteByte(byte(0x80 | n&0x3F))
		return
	}
	b.WriteRune(rune(n))
}

// mergeSurrogates gộp mỗi nửa cao WTF-8 liền trước một nửa thấp thành ký tự của cặp, như core/javascript.
func mergeSurrogates(s string) string {
	if !strings.Contains(s, "\xed") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if hi, ok := unitAt(s, i); ok && hi >= 0xD800 && hi <= 0xDBFF {
			if lo, ok := unitAt(s, i+3); ok && lo >= 0xDC00 && lo <= 0xDFFF {
				b.WriteRune(rune(0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)))
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// unitAt là nửa cặp WTF-8 ở byte i của s.
func unitAt(s string, i int) (int, bool) {
	if i+3 > len(s) || s[i] != 0xED || s[i+1] < 0xA0 || s[i+1] > 0xBF || s[i+2]&0xC0 != 0x80 {
		return 0, false
	}
	return int(s[i]&0x0F)<<12 | int(s[i+1]&0x3F)<<6 | int(s[i+2]&0x3F), true
}
