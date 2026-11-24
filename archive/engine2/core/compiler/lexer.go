package compiler

import (
	"fmt"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/kitwork/engine2/core/value"
)

// Khai báo các giá trị toán tử dùng chung để giảm cấp phát RAM khi Stress Test
var (
	valPlus     = value.String("+")
	valMinus    = value.String("-")
	valStar     = value.String("*")
	valSlash    = value.String("/")
	valPercent  = value.String("%")
	valEqual    = value.String("==")
	valAssign   = value.String("=")
	valKeywords = map[Kind]value.Value{}
)

var lexerPool = sync.Pool{
	New: func() any {
		return &Lexer{} // Khởi tạo cấu trúc rỗng
	},
}

func init() {
	// Khởi tạo sẵn Value cho các từ khóa quan trọng
	keywords := []Kind{Const, Let, Var, If, Else, Return, Import, Export, Switch}
	for _, k := range keywords {
		valKeywords[k] = value.String(k.String())
	}
}

func KeywordValue(kind Kind, ident string) value.Value {
	if val, ok := valKeywords[kind]; ok {
		return val
	} else {
		return value.String(ident)
	}
}

type Lexer struct {
	input  []byte
	source string
	base   int32
	ch     byte // Ký tự hiện tại
	pos    int  // Vị trí của ký tự ch
	next   int  // Vị trí đọc tiếp theo
}

func NewLexer(input string) *Lexer {
	return NewLexerSource(input, "")
}

func NewLexerSource(input, source string) *Lexer {
	return newLexerAt(input, source, 0)
}

func newLexerAt(input, source string, base int32) *Lexer {
	// Chuyển string thành []byte một lần duy nhất.
	// Việc này an toàn vì Lexer chỉ đọc chứ không ghi đè vào mảng này.
	l := &Lexer{input: []byte(input), source: source, base: base}
	l.readChar()
	return l
}

func (l *Lexer) b2s(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return *(*string)(unsafe.Pointer(&b))
}

func (l *Lexer) readChar() {
	if l.next >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.next]
	}
	l.pos = l.next
	l.next++
}

func (l *Lexer) peekChar() byte {
	if l.next >= len(l.input) {
		return 0
	}
	return l.input[l.next]
}

func (l *Lexer) NextToken() Token {
	var tok Token

	// white space and comments, in a loop: a file of many comments recurses no deeper than one
	for {
		l.skipWhitespace()
		if l.ch != '/' {
			break
		}
		if next := l.peekChar(); next == '/' {
			l.skipComment()
		} else if next == '*' {
			if refused, ok := l.skipBlockComment(); !ok {
				return refused
			}
		} else {
			break
		}
	}

	tok.Source = l.source
	tok.Position = l.base + int32(l.pos)

	// Tra bảng phân loại ký tự (O(1))
	cType := Table[l.ch]

	switch cType {
	case Alpha:
		ident := l.readIdent()

		// 1. Kiểm tra nhanh xem có phải Keyword không
		kind := LookupIdentifier(ident)
		tok.Kind = kind
		tok.Length = int16(len(ident))

		// 2. Phân luồng xử lý cực nhanh
		if kind == Ident {
			// Chỉ intern tên biến (x, y, myVar...)
			// Nếu Stress Test chạy đa luồng, bỏ qua intern() sẽ NHANH hơn do không bị nghẽn Lock
			// ident = l.intern(ident)
			tok.Value = value.String(ident)
		} else {
			// Đối với Keywords, ta dùng switch-case để gán giá trị tĩnh
			// Giúp tránh việc gọi String(ident) tạo object mới trên Heap
			switch kind {
			case Boolean:
				tok.Value = value.Boolean(ident[0] == 't')
			case Null:
				// null and undefined are one token, two values, as in JavaScript
				if ident[0] == 'u' {
					tok.Value = value.Undefined()
				} else {
					tok.Value = value.Null()
				}
			default:
				// Các từ khóa như const, let, if...
				// Nếu Parser không cần text của 'if', bạn thậm chí có thể để Value rỗng
				tok.Value = KeywordValue(kind, ident)
			}
		}
		return tok
	case Digit:
		tok.Kind = Number
		tok.Value = l.readNumber()
		tok.Length = int16(l.base + int32(l.pos) - tok.Position)
		return tok

	case Quote:
		// a literal's Length is its source, quotes included (lineBreakBeforePeek reads past it); the
		// cooked text of an escape is shorter
		if l.ch == '`' {
			tok.Kind = Template
			tok.Value = l.readTemplate()
		} else {
			tok.Kind = String
			tok.Value = l.readString(l.ch)
		}
		tok.Length = int16(l.base + int32(l.pos) - tok.Position)
		return tok

	case Operator:
		switch l.ch {
		case '=':
			if l.peekChar() == '>' {
				l.readChar()
				tok.Kind = FatArrow
				tok.Value = value.String("=>")
			} else if l.peekChar() == '=' {
				l.readChar()
				// === is strict equality, == loose (03/11/2026: they were one), as in JavaScript; the token
				// keeps its spelling, which the compiler reads.
				if l.peekChar() == '=' {
					l.readChar()
					tok.Kind = Equal
					tok.Value = value.String("===")
				} else {
					tok.Kind = Equal
					tok.Value = valEqual
				}
			} else {
				tok.Kind = Assign
				tok.Value = valAssign
			}
		case '!':
			if l.peekChar() == '=' {
				l.readChar()
				// Hỗ trợ '!==' tương tự '==='.
				if l.peekChar() == '=' {
					l.readChar()
					tok.Kind = NotEqual
					tok.Value = value.String("!==")
				} else {
					tok.Kind = NotEqual
					tok.Value = value.String("!=")
				}
			} else {
				tok.Kind = LogicalNot
				tok.Value = value.String("!")
			}
		case '+':
			if l.peekChar() == '+' {
				l.readChar()
				tok.Kind = PlusPlus
				tok.Value = value.String("++")
			} else if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = PlusAssign
				tok.Value = value.String("+=")
			} else {
				tok.Kind = Plus
				tok.Value = valPlus
			}
		case '-':
			if l.peekChar() == '-' {
				l.readChar()
				tok.Kind = MinusMinus
				tok.Value = value.String("--")
			} else if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = MinusAssign
				tok.Value = value.String("-=")
			} else {
				tok.Kind = Minus
				tok.Value = valMinus
			}
		case '*':
			if l.peekChar() == '*' {
				if l.next+1 < len(l.input) && l.input[l.next+1] == '=' {
					l.refuseOperator(&tok, "**=", "viết a = Math.pow(a, b)")
				} else {
					l.refuseOperator(&tok, "**", "viết Math.pow(a, b)")
				}
			} else if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = StarAssign
				tok.Value = value.String("*=")
			} else {
				tok.Kind = Star
				tok.Value = valStar
			}
		case '/':
			if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = SlashAssign
				tok.Value = value.String("/=")
			} else {
				tok.Kind = Slash
				tok.Value = valSlash
			}
		case '%':
			if l.peekChar() == '=' {
				l.refuseOperator(&tok, "%=", "viết a = a % b")
			} else {
				tok.Kind = Percent
				tok.Value = valPercent
			}
		case '>':
			if l.peekChar() == '>' {
				l.refuseShift(&tok, ">>")
			} else if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = GreaterEqual
				tok.Value = value.String(">=")
			} else {
				tok.Kind = Greater
				tok.Value = value.String(">")
			}
		case '<':
			if l.peekChar() == '<' {
				l.refuseShift(&tok, "<<")
			} else if l.peekChar() == '=' {
				l.readChar()
				tok.Kind = LessEqual
				tok.Value = value.String("<=")
			} else {
				tok.Kind = Less
				tok.Value = value.String("<")
			}
		case '(':
			tok.Kind = LeftParen
			tok.Value = value.String("(")
		case ')':
			tok.Kind = RightParen
			tok.Value = value.String(")")
		case '{':
			tok.Kind = LeftBrace
			tok.Value = value.String("{")
		case '}':
			tok.Kind = RightBrace
			tok.Value = value.String("}")
		case '[':
			tok.Kind = LeftBracket
			tok.Value = value.String("[")
		case ']':
			tok.Kind = RightBracket
			tok.Value = value.String("]")
		case ',':
			tok.Kind = Comma
			tok.Value = value.String(",")
		case ';':
			tok.Kind = Semicolon
			tok.Value = value.String(";")
		case ':':
			tok.Kind = Colon
			tok.Value = value.String(":")
		case '.':
			if Table[l.peekChar()] == Digit {
				// .5 is a number, as in JavaScript (a ?.5 : 1 included)
				tok.Kind = Number
				tok.Value = l.readNumber()
				tok.Length = int16(l.base + int32(l.pos) - tok.Position)
				return tok
			}
			if l.peekChar() == '.' {
				// Check for third dot
				if l.next+1 < len(l.input) && l.input[l.next+1] == '.' {
					l.readChar()
					l.readChar()
					tok.Kind = Spread
					tok.Value = value.String("...")
				} else {
					tok.Kind = Dot
					tok.Value = value.String(".")
				}
			} else {
				tok.Kind = Dot
				tok.Value = value.String(".")
			}
		case '&':
			if l.peekChar() == '&' && l.next+1 < len(l.input) && l.input[l.next+1] == '=' {
				l.refuseOperator(&tok, "&&=", "viết a = a && b, hay if (a) a = b")
			} else if l.peekChar() == '&' {
				l.readChar()
				tok.Kind = LogicalAnd
				tok.Value = value.String("&&")
			} else {
				tok.Kind = Illegal
				tok.Value = literalFailure(illegalProblem('&', false))
			}
		case '|':
			if l.peekChar() == '|' && l.next+1 < len(l.input) && l.input[l.next+1] == '=' {
				l.refuseOperator(&tok, "||=", "viết a = a || b, hay if (!a) a = b")
			} else if l.peekChar() == '|' {
				l.readChar()
				tok.Kind = LogicalOr
				tok.Value = value.String("||")
			} else {
				tok.Kind = Illegal
				tok.Value = literalFailure(illegalProblem('|', false))
			}
		case '?':
			if l.peekChar() == '?' && l.next+1 < len(l.input) && l.input[l.next+1] == '=' {
				l.refuseOperator(&tok, "??=", "viết a = a ?? b, hay if (a == null) a = b")
			} else if l.peekChar() == '?' {
				l.readChar()
				tok.Kind = NullCoalescing
				tok.Value = value.String("??")
			} else if l.peekChar() == '.' && !(l.next+1 < len(l.input) && Table[l.input[l.next+1]] == Digit) {
				// ?. là chuỗi tuỳ chọn, trừ khi một chữ số theo sau: a ?.5 : 1 là ba ngôi, như JavaScript.
				l.readChar()
				tok.Kind = OptionalChain
				tok.Value = value.String("?.")
			} else {
				// Toán tử ternary: cond ? a : b
				tok.Kind = Question
				tok.Value = value.String("?")
			}
		}

	case Space:
		if l.pos >= len(l.input) {
			tok.Kind = EOF
			return tok
		}
		// what skipWhitespace left: a NUL, a CR alone, another control byte
		tok.Kind = Illegal
		tok.Value = literalFailure(controlProblem(l.ch))

	default:
		tok.Kind = Illegal
		r, size := utf8.DecodeRune(l.input[l.pos:])
		tok.Value = literalFailure(illegalProblem(r, l.pos == 0))
		for i := 1; i < size; i++ {
			l.readChar()
		}
	}

	l.readChar()
	tok.Length = int16(l.base + int32(l.pos) - tok.Position)
	return tok
}

// --- Helpers tối ưu với Table Lookup ---

// skipWhitespace skips what JavaScript reads as white space and line breaks that Kitwork takes: space, tab, vertical
// tab, form feed, LF and CRLF (18/11/2026). Every byte up to 0x20 was white space, a NUL ended the source without a
// word, and a CR alone was white space but no line break, so a // comment ran past it; each is now the Illegal token
// NextToken makes, a compile error that names it.
func (l *Lexer) skipWhitespace() {
	for {
		switch l.ch {
		case ' ', '\t', '\n', '\v', '\f':
		case '\r':
			if l.peekChar() != '\n' {
				return
			}
		default:
			return
		}
		l.readChar()
	}
}

// skipComment skips a // comment to its line's end: LF, CR, or U+2028 or U+2029, the line terminators of JavaScript
// (18/11/2026: it ran past a CR alone and a U+2028 to the next LF, and a NUL ended the source inside it).
func (l *Lexer) skipComment() {
	for l.pos < len(l.input) && l.ch != '\n' && l.ch != '\r' && !l.atLineSeparator() {
		l.readChar()
	}
	l.skipWhitespace()
}

// skipBlockComment skips a /* … */ comment as JavaScript reads one (19/11/2026, the owner; it was read as / and *, an
// error of the parser): it ends at the first */ after its /*, so /*/ does not close it and comments do not nest. A
// comment that holds a line break is one between the tokens around it, as in JavaScript: lineBreakBeforePeek reads the
// source between them. It holds any text but the line breaks Kitwork does not take outside it, a CR alone, U+2028,
// U+2029, so a line is counted alike everywhere (a comment ends at its */ all the same). ok is false with the Illegal
// token of the first such break, or of a /* the source ends inside, at the /*.
func (l *Lexer) skipBlockComment() (refused Token, ok bool) {
	start := l.pos
	problem, problemAt, problemSize := "", 0, 0
	l.readChar() // the /
	l.readChar() // the *, which no */ shares
	for l.pos < len(l.input) {
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar()
			l.readChar()
			if problem != "" {
				return l.illegal(problemAt, problemSize, problem), false
			}
			return Token{}, true
		}
		if problem == "" {
			if l.ch == '\r' && l.peekChar() != '\n' {
				problem, problemAt, problemSize = controlProblem('\r'), l.pos, 1
			} else if l.atLineSeparator() {
				r, size := utf8.DecodeRune(l.input[l.pos:])
				problem, problemAt, problemSize = illegalProblem(r, false), l.pos, size
			}
		}
		l.readChar()
	}
	if problem != "" {
		return l.illegal(problemAt, problemSize, problem), false
	}
	return l.illegal(start, 2, "chú thích /* chưa đóng: file hết trước */"), false
}

// illegal is the Illegal token of size bytes at at, saying problem.
func (l *Lexer) illegal(at, size int, problem string) Token {
	return Token{Kind: Illegal, Source: l.source, Position: l.base + int32(at), Length: int16(size), Value: literalFailure(problem)}
}

// refuseOperator makes tok the Illegal token of written, an operator of JavaScript Kitwork does not have (docs/language.md
// mục 3.3), saying how to write it instead (19/11/2026: ** and %= were "không đọc được" of their second character). It
// reads written but its last character, which NextToken reads after its switch.
func (l *Lexer) refuseOperator(tok *Token, written, instead string) {
	for range len(written) - 1 {
		l.readChar()
	}
	tok.Kind = Illegal
	tok.Value = literalFailure("Kitwork không có " + written + ": " + instead)
}

// refuseShift is refuseOperator of a shift, <<, >>, >>> and their assignments: a bit operator, as & and | are.
func (l *Lexer) refuseShift(tok *Token, shift string) {
	written := shift
	if shift == ">>" && l.next+1 < len(l.input) && l.input[l.next+1] == '>' {
		written = ">>>"
	}
	if end := l.pos + len(written); end < len(l.input) && l.input[end] == '=' {
		written += "="
	}
	l.refuseOperator(tok, written, "toán tử bit, dịch bit viết bằng phép nhân, phép chia và Math.floor")
}

// atLineSeparator reports U+2028 or U+2029 at the current byte.
func (l *Lexer) atLineSeparator() bool {
	return l.ch == 0xE2 && l.next+1 < len(l.input) && l.input[l.next] == 0x80 &&
		(l.input[l.next+1] == 0xA8 || l.input[l.next+1] == 0xA9)
}

// controlProblem says why a control byte outside a string is an error.
func controlProblem(ch byte) string {
	switch ch {
	case 0:
		return "byte NUL (0x00) ngoài chuỗi: file .work là chữ; NUL thường là file hỏng hay lưu sai mã"
	case '\r':
		return "CR đứng riêng làm dòng mới (kiểu Mac cũ): lưu file với dòng mới LF hay CRLF"
	}
	return fmt.Sprintf("ký tự điều khiển 0x%02X ngoài chuỗi: khoảng trắng là dấu cách, tab hay dòng mới", ch)
}

// illegalProblem says why a character outside a string is no token (18/11/2026: each said "ILLEGAL").
func illegalProblem(r rune, atStart bool) string {
	switch {
	case r == 0xFEFF && atStart:
		return "file bắt đầu bằng BOM (U+FEFF): lưu file là UTF-8 không có BOM"
	case r == 0x2028 || r == 0x2029:
		return fmt.Sprintf("ký tự dòng mới Unicode U+%04X ngoài chuỗi: xuống dòng bằng LF", r)
	case r == utf8.RuneError:
		return "byte không phải UTF-8 ngoài chuỗi: lưu file là UTF-8"
	case r >= 0x80 && unicode.IsSpace(r) || r == 0xFEFF:
		return fmt.Sprintf("khoảng trắng Unicode U+%04X ngoài chuỗi: dùng dấu cách thường", r)
	case r >= 0x80:
		return fmt.Sprintf("ký tự %q ngoài chuỗi và chú thích: tên viết bằng chữ cái ASCII, chữ số, _ và $", r)
	case r == '&' || r == '|' || r == '^' || r == '~':
		return fmt.Sprintf("Kitwork không có toán tử bit (%c): điều kiện viết bằng && và ||", r)
	case r == '#':
		return "# không có trong cú pháp của Kitwork: không có tên riêng #x, dòng #! đầu file"
	case r == '@':
		return "@ không có trong cú pháp của Kitwork: không có decorator"
	}
	return fmt.Sprintf("ký tự %q không có trong cú pháp của Kitwork", r)
}

func (l *Lexer) readIdent() string {
	start := l.pos
	for {
		cType := Table[l.ch]
		if cType != Alpha && cType != Digit {
			break
		}
		l.readChar()
	}

	// Thay vì: return string(l.input[start:l.pos]) -> Tốn thời gian copy
	// Hãy dùng:
	slice := l.input[start:l.pos]
	return l.b2s(slice) // Biến slice thành string ngay lập tức với 0 allocs
}
