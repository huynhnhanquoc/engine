package compiler

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// Bảng độ ưu tiên
const (
	_ int = iota
	LOWEST
	ASSIGN      // =
	OR          // ||
	AND         // &&
	EQUALS      // ==
	LESSGREATER // > hoặc <
	SUM         // +
	PRODUCT     // *
	PREFIX      // -X hoặc !X
	CALL        // f(x)
	INDEX       // a[0]
	MEMBER      // a.b
	ARROW       // =>
)

var precedences = map[Kind]int{
	Equal:        EQUALS,
	NotEqual:     EQUALS,
	Less:         LESSGREATER,
	Greater:      LESSGREATER,
	LessEqual:    LESSGREATER,
	GreaterEqual: LESSGREATER,
	Plus:         SUM,
	Minus:        SUM,
	Star:         PRODUCT,
	Slash:        PRODUCT,
	Percent:      PRODUCT,
	Question:     ASSIGN,
	PlusAssign:   ASSIGN,
	MinusAssign:  ASSIGN,
	StarAssign:   ASSIGN,
	SlashAssign:  ASSIGN,
	PlusPlus:     CALL,
	MinusMinus:   CALL,
	LeftParen:    CALL,
	LeftBracket:  INDEX,
	Dot:          MEMBER,
	Assign:       ASSIGN,
	LogicalAnd:   AND,
	LogicalOr:    OR,
	FatArrow:     ARROW,
	// ?? đứng ngang ||, như JavaScript; trộn hai thứ không ngoặc là lỗi (parseInfixExpression).
	NullCoalescing: OR,
	OptionalChain:  MEMBER,
}

type (
	prefixParseFn func(*Parser) Expression
	infixParseFn  func(*Parser, Expression) Expression
)

type Parser struct {
	l      *Lexer
	errors []string

	curToken  Token
	peekToken Token

	// Module metadata thu thập khi parse (cho bundler native ở package script).
	exports    []string      // tên export qua `export const/function` / `export { }`
	aliases    []ExportAlias // `export { a as b }` whose names differ
	hasDefault bool          // có `export default …` (đã hạ về const DefaultExportName)
	defaultAt  Token         // chữ default của export default đầu tiên: lần thứ hai báo dòng của cả hai
	breakDepth int           // switch/loop nesting; function bodies reset this boundary
	loopDepth  int           // loop nesting, what continue needs; function bodies reset it too
	// functionDepth is how many function bodies the parser is in, what return needs; statementDepth how many
	// statements, so 1 is a statement of the file's top level, where import and export stand (18/11/2026).
	functionDepth  int
	statementDepth int
	// exportLocals are the names export { … } lists, each held to a name the file's top level declares.
	exportLocals []Token

	// text là toàn bộ nguồn mà Token.Position đếm vào — để báo lỗi dạng file:dòng:cột. Parser con của
	// một ${…} trong template nhận text của file chứa nó.
	text []byte

	// grouped holds the expressions the source wrapped in parentheses: (a ?? b) || c is allowed where
	// a ?? b || c is not, and (a?.b).c ends the optional chain at the parenthesis.
	grouped map[Expression]bool
	// openChain holds the links (member, index, call) of an optional chain not closed yet: the chain
	// becomes one ChainExpression when the next token no longer continues it.
	openChain map[Expression]bool
	// leftStart is where the expression an infix function is handed begins in the source (its first
	// token, a parenthesis included): parseExpression sets it just before the call.
	leftStart int32
	// nesting is how deep the parser recurses now (nesting.go); nestingReported: the error is said.
	nesting         int
	nestingReported bool
	// internal is the fault of the parser's own code ParseProgram recovered, nil when none (guard.go).
	internal *InternalError
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{l: l, errors: []string{}}
	if l.base == 0 {
		p.text = l.input
	}
	p.nextToken()
	p.nextToken()
	return p
}

// prefixParseFns and infixParseFns are the parse functions of each token kind, the same for every
// parser: built once, so a parser allocates no table of its own. Each ${…} of a template is parsed by a
// parser of its own, which built both tables, two maps of bound methods (survey, 03/11/2026: 64
// allocations and 5-8 µs a parser, 19% of a compile's allocations at 1,000 lines).
var (
	prefixParseFns = map[Kind]prefixParseFn{}
	infixParseFns  = map[Kind]infixParseFn{}
)

func init() {
	// Đăng ký Prefix
	registerPrefix(Ident, (*Parser).parseIdentifier)
	registerPrefix(Number, (*Parser).parseLiteral)
	registerPrefix(String, (*Parser).parseLiteral)
	registerPrefix(Boolean, (*Parser).parseLiteral)
	registerPrefix(Null, (*Parser).parseLiteral)
	registerPrefix(Template, (*Parser).parseTemplateLiteral)
	registerPrefix(LogicalNot, (*Parser).parsePrefixExpression)
	registerPrefix(Minus, (*Parser).parsePrefixExpression)
	// +x makes a number of x as Number(x) does (16/10/2026, the owner's E2; it did not parse)
	registerPrefix(Plus, (*Parser).parsePrefixExpression)
	registerPrefix(LeftParen, (*Parser).parseGroupedExpression)
	registerPrefix(If, (*Parser).parseIfAsValue)
	// registerPrefix(For, (*Parser).parseForStatement)
	// registerPrefix(Defer, (*Parser).parseDeferStatement)
	// registerPrefix(Go, (*Parser).parseSpawnStatement)
	registerPrefix(LeftBracket, (*Parser).parseArrayLiteral)
	registerPrefix(LeftBrace, (*Parser).parseObjectLiteral)
	registerPrefix(Function, (*Parser).parseFunctionExpression)
	registerPrefix(New, (*Parser).parseNewExpression)
	registerPrefix(PlusPlus, (*Parser).parsePrefixUpdate)
	registerPrefix(MinusMinus, (*Parser).parsePrefixUpdate)
	registerPrefix(Void, (*Parser).parsePrefixExpression)
	registerPrefix(Reserved, (*Parser).parseReservedKeyword)

	// Đăng ký Infix
	registerInfix(Plus, (*Parser).parseInfixExpression)
	registerInfix(Minus, (*Parser).parseInfixExpression)
	registerInfix(Star, (*Parser).parseInfixExpression)
	registerInfix(Slash, (*Parser).parseInfixExpression)
	registerInfix(Percent, (*Parser).parseInfixExpression)
	registerInfix(Equal, (*Parser).parseInfixExpression)
	registerInfix(NotEqual, (*Parser).parseInfixExpression)
	registerInfix(Less, (*Parser).parseInfixExpression)
	registerInfix(Greater, (*Parser).parseInfixExpression)
	registerInfix(LessEqual, (*Parser).parseInfixExpression)
	registerInfix(GreaterEqual, (*Parser).parseInfixExpression)
	registerInfix(Assign, (*Parser).parseInfixExpression)
	registerInfix(LeftParen, (*Parser).parseCallExpression)
	registerInfix(Dot, (*Parser).parseDotExpression)
	registerInfix(LeftBracket, (*Parser).parseIndexExpression)
	registerInfix(LogicalAnd, (*Parser).parseInfixExpression)
	registerInfix(LogicalOr, (*Parser).parseInfixExpression)
	registerInfix(NullCoalescing, (*Parser).parseInfixExpression)
	registerInfix(OptionalChain, (*Parser).parseOptionalChain)
	registerInfix(FatArrow, (*Parser).parseArrowFunction)
	registerInfix(Question, (*Parser).parseTernaryExpression)
	registerInfix(PlusAssign, (*Parser).parseCompoundAssignment)
	registerInfix(MinusAssign, (*Parser).parseCompoundAssignment)
	registerInfix(StarAssign, (*Parser).parseCompoundAssignment)
	registerInfix(SlashAssign, (*Parser).parseCompoundAssignment)
	registerInfix(PlusPlus, (*Parser).parsePostfixUpdate)
	registerInfix(MinusMinus, (*Parser).parsePostfixUpdate)
}

/* =============================================================================
   1. STATEMENTS (Câu lệnh)
   ============================================================================= */

// ParseProgram reads the whole source. A panic of the parser's own code is its error, the last of Errors, and
// what it read till then the program (13/11/2026): every caller drops a program the parser has errors for.
func (p *Parser) ParseProgram() (program *Program) {
	program = &Program{Statements: []Statement{}}
	defer func() {
		if fault := recover(); fault != nil {
			earlier := ""
			if len(p.errors) > 0 {
				earlier = p.errors[0]
			}
			p.internal = internalError(fault, earlier)
			p.errors = append(p.errors, p.internal.Error())
		}
	}()
	for p.curToken.Kind != EOF {
		// 1. Bỏ qua dấu chấm phẩy thừa
		if p.curToken.Kind == Semicolon {
			p.nextToken()
			continue
		}

		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.endStatement(stmt)

		// 2. CHỈ GỌI nextToken Ở ĐÂY để chuẩn bị cho dòng tiếp theo
		p.nextToken()
	}
	p.checkExports(program)
	program.Exports = p.exports
	program.ExportAliases = p.aliases
	program.HasDefault = p.hasDefault
	p.checkNesting(program)
	return program
}

func (p *Parser) parseStatement() Statement {
	if !p.enterNesting() {
		return nil
	}
	defer p.leaveNesting()
	p.statementDepth++
	defer func() { p.statementDepth-- }()
	if p.curTokenIs(Ident) {
		switch p.curToken.Value.Text() {
		case "break":
			return p.parseBreakStatement()
		case "continue":
			return p.parseContinueStatement()
		case "case", "default":
			p.addError(fmt.Sprintf("'%s' chỉ hợp lệ bên trong switch", p.curToken.Value.Text()))
			return nil
		}
	}
	switch p.curToken.Kind {
	case LeftBrace:
		// a { that starts a statement is a block, as in JavaScript: { const z = 1; … }, case 1: { … }
		return p.parseBlockStatement()
	case Let, Const, Var:
		return p.parseVarStatement()
	case Import:
		return p.parseImportStatement()
	case Export:
		return p.parseExportStatement()
	case Return:
		return p.parseReturnStatement()
	case Function:
		return p.parseFunctionStatement()
	case For:
		return p.parseForStatement()
	case Switch:
		return p.parseSwitchStatement()
	case If:
		// if is a statement, not a value (18/11/2026): it was read as an expression anywhere, so const r = if (c) 1 else 2
		// made bytecode the verifier refused and () => if (c) {1} returned undefined
		stmt := &ExpressionStatement{Token: p.curToken}
		stmt.Expression = p.parseIfExpression()
		return stmt
	default:
		return p.parseExpressionStatement()
	}

}

// Trong parser.go
func (p *Parser) parseVarStatement() Statement {
	stmt := &VarStatement{Token: p.curToken}

	if p.peekTokenIs(LeftBrace) || p.peekTokenIs(LeftBracket) {
		// Destructuring: const { a, b: c, d = 1, e: { f } } = …, const [a, b = 2] = …
		p.nextToken() // cur: { or [
		pattern := p.parseBindingPattern()
		if pattern == nil {
			return nil
		}
		stmt.Pattern = pattern
		stmt.Names = pattern.Names()
		stmt.DestructMode = DestructObject
		if pattern.Array {
			stmt.DestructMode = DestructArray
		}
	} else {
		// Standard: const a = ...
		if !p.expectBindingName() {
			return nil
		}
		stmt.Names = []*Identifier{{Token: p.curToken, Value: p.curToken.Value.Text()}}
		stmt.DestructMode = DestructNone
	}

	if !p.peekTokenIs(Assign) {
		switch {
		case stmt.Pattern != nil:
			p.addErrorAt(p.peekToken, "mẫu tách cần một giá trị để tách: "+stmt.Token.String()+" "+stmt.Pattern.String()+" = …")
			return nil
		case stmt.Token.Kind == Let, stmt.Token.Kind == Var:
			// let y; is let y = undefined, as in JavaScript (14/10/2026); var y; too
			name := stmt.Names[0]
			undefinedToken := tokenWithValue(name.Token, Null, value.Undefined())
			stmt.Value = &Literal{Token: undefinedToken, Value: undefinedToken.Value}
			return stmt
		default:
			p.addErrorAt(p.peekToken, "const cần giá trị ngay khi khai báo, như JavaScript: const "+stmt.Names[0].Value+" = …; chưa có giá trị thì let "+stmt.Names[0].Value+";")
			return nil
		}
	}
	p.nextToken()
	// Lúc này curToken đang là '='

	p.nextToken() // Nhảy qua dấu '=' để đứng tại vị trí của giá trị (VD: số 10)

	stmt.Value = p.parseExpression(LOWEST)

	return stmt
}

/* =============================================================================
   TÁCH OBJECT VÀ MẢNG (14/10/2026)
   const { value: user, error: userError } = safe(() => …): đổi tên, khoá là từ khoá hay chuỗi, giá trị
   mặc định (dùng khi đọc ra undefined, như JavaScript, từ 15/11/2026; trước đó cả null), mẫu lồng nhau,
   phần còn lại (...tên, 15/11/2026), như JavaScript. Chưa có, là lỗi biên dịch nói cách viết thay: chỗ trống
   ([, b]), khoá tính toán ({ [k]: v }).
   ============================================================================= */

// parseBindingPattern reads { … } or [ … ] of a declaration; cur is the opening bracket, and is the
// closing one on return. nil after an error.
func (p *Parser) parseBindingPattern() *BindingPattern {
	pattern := &BindingPattern{Token: p.curToken, Array: p.curTokenIs(LeftBracket)}
	end, example := RightBrace, "const { a, b: c, d = 1 } = o"
	if pattern.Array {
		end, example = RightBracket, "const [a, b = 2] = arr"
	}
	for !p.peekTokenIs(end) {
		p.nextToken() // cur: the element
		element, ok := p.parseBindingElement(pattern.Array)
		if !ok {
			return nil
		}
		pattern.Elements = append(pattern.Elements, element)
		if p.peekTokenIs(Comma) {
			p.nextToken()
		} else if !p.peekTokenIs(end) {
			p.addErrorAt(p.peekToken, fmt.Sprintf("các phần của mẫu tách ngăn bằng dấu phẩy: %s", example))
			return nil
		}
	}
	p.nextToken() // cur: } or ]
	return pattern
}

// parseBindingElement reads one element of a pattern, cur at its first token.
func (p *Parser) parseBindingElement(array bool) (BindingElement, bool) {
	element := BindingElement{KeyToken: p.curToken}
	switch {
	case p.curTokenIs(Spread):
		// ...rest (15/11/2026, round 5 of the 13/11 review): the keys or items the others did not read; a name, or in an
		// array a nested pattern, as JavaScript has it, and the last element, with no default
		element.Rest = true
		p.nextToken() // cur: the target
		if array {
			if !p.parseBindingTarget(&element) {
				return element, false
			}
		} else if !p.bindingName(p.curToken, "phần còn lại của mẫu tách object là một tên: const { a, ...rest } = o") {
			return element, false
		} else {
			element.Name = &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
		}
		if p.peekTokenIs(Assign) {
			p.addErrorAt(p.peekToken, "phần còn lại (...tên) không có giá trị mặc định")
			return element, false
		}
		if p.peekTokenIs(Comma) {
			p.addErrorAt(p.peekToken, "phần còn lại (...tên) là phần cuối của mẫu tách, như JavaScript")
			return element, false
		}
		return element, true
	case array && p.curTokenIs(Comma):
		p.addErrorAt(p.curToken, "Kitwork chưa có chỗ trống khi tách mảng ([, b]); viết const b = arr[1]")
		return element, false
	case array:
		if !p.parseBindingTarget(&element) {
			return element, false
		}
	case p.curTokenIs(LeftBracket):
		p.addErrorAt(p.curToken, "Kitwork chưa có khoá tính toán khi tách object ({ [k]: v }); viết const v = o[k]")
		return element, false
	default:
		key, ok := p.patternKey(p.curToken)
		if !ok {
			return element, false
		}
		element.Key = key
		if p.peekTokenIs(Colon) {
			p.nextToken() // cur: ':'
			p.nextToken() // cur: the target
			if !p.parseBindingTarget(&element) {
				return element, false
			}
		} else {
			// { a } is { a: a }: the key is the name, so it must be one.
			if p.curTokenIs(String) {
				p.addErrorAt(p.curToken, fmt.Sprintf("khoá chuỗi cần một tên đích: { %q: tên }", key))
				return element, false
			}
			if !p.bindingName(p.curToken, "mẫu tách object nhận tên biến: const { a, b: c } = o") {
				return element, false
			}
			element.Name = &Identifier{Token: p.curToken, Value: key}
		}
	}
	if p.peekTokenIs(Assign) {
		p.nextToken() // cur: '='
		p.nextToken() // cur: the default
		element.Default = p.parseExpression(LOWEST)
		if element.Default == nil {
			return element, false
		}
		if element.Name != nil {
			nameFunction(element.Default, element.Name.Value)
		}
	}
	return element, true
}

// patternKey is the property an object pattern element reads: a name, a reserved word ({ default: d },
// as after a dot) or a string ({ "content-type": t }).
func (p *Parser) patternKey(tok Token) (string, bool) {
	switch {
	case tok.Kind == Ident:
		return tok.Value.Text(), true
	case tok.Kind == String:
		return tok.Value.Text(), true
	case tok.Kind == Number:
		p.addErrorAt(tok, fmt.Sprintf("khoá số khi tách object chưa có; viết khoá chuỗi { \"%s\": tên }", tok.Value.ToString().Text()))
		return "", false
	}
	if word, ok := p.keywordSpelling(tok); ok {
		if !p.peekTokenIs(Colon) {
			// { default } would bind a variable named by a keyword.
			p.reservedNameError(tok, word)
			return "", false
		}
		return word, true
	}
	p.addErrorAt(tok, "mẫu tách object nhận tên, ngăn bằng dấu phẩy: const { a, b: c } = o")
	return "", false
}

// parseBindingTarget reads what a pattern element binds, cur at it: a variable, or a nested pattern.
func (p *Parser) parseBindingTarget(element *BindingElement) bool {
	if p.curTokenIs(LeftBrace) || p.curTokenIs(LeftBracket) {
		element.Pattern = p.parseBindingPattern()
		return element.Pattern != nil
	}
	if p.curTokenIs(Spread) {
		p.addErrorAt(p.curToken, "... đứng đầu phần còn lại của mẫu tách: const { a, ...rest } = o, const [first, ...others] = arr")
		return false
	}
	if !p.bindingName(p.curToken, "sau dấu : là tên biến hoặc một mẫu { … } hay [ … ]: const { a: tên } = o") {
		return false
	}
	element.Name = &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
	return true
}

// assignmentPattern turns the left side of ({ … } = o) or [ … ] = arr, parsed as a literal, into the
// pattern it spells. The targets are variables (or nested patterns): a member target is not there yet.
func (p *Parser) assignmentPattern(left Expression) (*BindingPattern, bool) {
	switch node := left.(type) {
	case *ObjectLiteral:
		pattern := &BindingPattern{Token: node.Token}
		for index, entry := range node.Entries {
			if entry.IsSpread {
				// ({ a, ...rest } = o): a name, the last entry (15/11/2026)
				at := Token{Position: getNodePosition(entry.Value), Source: getNodeSource(entry.Value)}
				name, ok := entry.Value.(*Identifier)
				if !ok {
					p.addErrorAt(at, "phần còn lại của mẫu tách object là một tên: ({ a, ...rest } = o)")
					return nil, false
				}
				if index != len(node.Entries)-1 {
					p.addErrorAt(at, "phần còn lại (...tên) là phần cuối của mẫu tách, như JavaScript")
					return nil, false
				}
				pattern.Elements = append(pattern.Elements, BindingElement{Name: name, Rest: true})
				continue
			}
			element := BindingElement{}
			target := entry.Value
			if entry.Value == entry.Key {
				// { a } or { a = 1 }: the key is the name.
				target = entry.Key
				if assign, ok := entry.Key.(*AssignmentExpression); ok {
					if id, ok := assign.Name.(*Identifier); ok {
						element.Key, element.KeyToken = id.Value, id.Token
					}
				}
			}
			if element.Key == "" {
				key, keyToken, ok := p.literalPatternKey(entry.Key)
				if !ok {
					return nil, false
				}
				element.Key, element.KeyToken = key, keyToken
			}
			if !p.assignmentTarget(target, &element) {
				return nil, false
			}
			pattern.Elements = append(pattern.Elements, element)
		}
		return pattern, true
	case *ArrayLiteral:
		pattern := &BindingPattern{Token: node.Token, Array: true}
		for index, item := range node.Elements {
			element := BindingElement{}
			if spread, ok := item.(*SpreadExpression); ok {
				// [a, ...rest] = arr: a name or a nested pattern, the last element (15/11/2026)
				if index != len(node.Elements)-1 {
					p.addErrorAt(spread.Token, "phần còn lại (...tên) là phần cuối của mẫu tách, như JavaScript")
					return nil, false
				}
				element.Rest = true
				item = spread.Value
				if _, defaulted := item.(*AssignmentExpression); defaulted {
					p.addErrorAt(spread.Token, "phần còn lại (...tên) không có giá trị mặc định")
					return nil, false
				}
			}
			if !p.assignmentTarget(item, &element) {
				return nil, false
			}
			pattern.Elements = append(pattern.Elements, element)
		}
		return pattern, true
	}
	return nil, true
}

// literalPatternKey reads the key of an object literal entry used as a pattern.
func (p *Parser) literalPatternKey(key Expression) (string, Token, bool) {
	at := Token{Position: getNodePosition(key), Source: getNodeSource(key)}
	switch node := key.(type) {
	case *Identifier:
		return node.Value, node.Token, true
	case *Literal:
		if node.Value.IsString() {
			return node.Value.Text(), node.Token, true
		}
		if node.Value.IsNumber() {
			p.addErrorAt(at, fmt.Sprintf("khoá số khi tách object chưa có; viết khoá chuỗi { \"%s\": tên }", node.Value.ToString().Text()))
			return "", at, false
		}
	case *ArrayLiteral:
		p.addErrorAt(at, "Kitwork chưa có khoá tính toán khi tách object ({ [k]: v } = o); viết v = o[k]")
		return "", at, false
	}
	p.addErrorAt(at, "mẫu tách object nhận tên, ngăn bằng dấu phẩy: ({ a, b: c } = o)")
	return "", at, false
}

// assignmentTarget reads what one entry of an assignment pattern binds: a variable, a nested pattern,
// either with a default (x = 1, { b } = {}).
func (p *Parser) assignmentTarget(target Expression, element *BindingElement) bool {
	if target == nil {
		return false // its own error is already reported
	}
	if assign, ok := target.(*AssignmentExpression); ok && assign.Token.Kind == Assign {
		element.Default = assign.Value
		target = assign.Name
	}
	switch node := target.(type) {
	case *Identifier:
		element.Name = node
		if element.Default != nil {
			nameFunction(element.Default, node.Value)
		}
		return true
	case *BindingPattern:
		element.Pattern = node
		return true
	case *ObjectLiteral, *ArrayLiteral:
		nested, ok := p.assignmentPattern(node)
		if !ok {
			return false
		}
		element.Pattern = nested
		return true
	}
	at := Token{Position: startPosition(target), Source: getNodeSource(target)}
	switch target.(type) {
	case *MemberExpression, *IndexExpression:
		p.addErrorAt(at, fmt.Sprintf("Kitwork chưa có gán vào %s khi tách; tách vào một tên rồi gán: ({ a } = o); %s = a", target.String(), target.String()))
	default:
		// ({ a: 1 } = o): 1 takes no value; the place after : names the variable
		p.addErrorAt(at, fmt.Sprintf("không gán được vào %s khi tách: sau dấu hai chấm là tên nhận giá trị, ({ a: tên } = o)", target.String()))
	}
	return false
}

/* =============================================================================
   MODULES — native import / export

   Strategy: lower to existing AST nodes, reuse all existing machinery. The global kitwork (BUILTIN 0) is
   the app; each library is a member of it, which the app gives when the program runs.
     import * as http from "kitwork/http"     →  const http = kitwork.http
     import { get, post } from "kitwork/http" →  const { get, post } = kitwork.http
     import { get as g } from "kitwork/http"  →  const g = kitwork.http.get
     export const x = ...                      →  const x = ...   (export stripped)
     export default expr                       →  expr;           (evaluated)
     export { a, b }                           →  (no-op)
     import { x } from "./x.work" / "app/core/x.work"  →  ImportStatement, resolved by the bundler
   Anything the native path can't express records a parser error.
   ============================================================================= */

// kitworkModulePath reads a specifier of Kitwork's: "kitwork/<name>" gives the library's word. ok is
// false for any other specifier (a file); module is "" for one of Kitwork's that names no library
// ("kitwork", "kitwork/a/b"), a compile error. Which libraries exist is the app's to say when the program
// runs, not the parser's.
func kitworkModulePath(spec string) (module string, ok bool) {
	if spec != "kitwork" && !strings.HasPrefix(spec, "kitwork/") {
		return "", false
	}
	if module = strings.TrimPrefix(spec, "kitwork/"); module == spec || !isPlainWord(module) {
		return "", true
	}
	return module, true
}

// kitworkModuleMessage is the error of an import of Kitwork's that names no library.
func kitworkModuleMessage(spec string) string {
	return fmt.Sprintf(`import %q: một thư viện của Kitwork là một chữ sau "kitwork/": import from "kitwork/<tên>"`, spec)
}

// isRelativeSpecifier báo specifier là module tương đối (file trong tenant)
// → sẽ phát ImportStatement cho bundler native giải quyết.
func isRelativeSpecifier(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/")
}

// isAppRootSpecifier reports an import from the app's root, `app/…` (22/10/2026, KITWORK_UI.md mục 4):
// the same in every folder of the app, resolved by the bundler from the folder of the app the file belongs
// to (app/ for one app, apps/<identity>/ for many), never past it.
func isAppRootSpecifier(s string) bool {
	// app\… is the same import written with Windows' separator: refused by name, not as a place unknown
	return s == "app" || strings.HasPrefix(s, "app/") || strings.HasPrefix(s, `app\`)
}

// appRootSpecifierProblem is what is wrong with an app/… specifier as written, "" when nothing is. The
// path after app/ goes down from the app's folder, written with "/", one name a step: it never leaves the
// app (no "..", no absolute path) and never reaches the app's data (.database/). The bundler checks the
// same on the disk, past links.
func appRootSpecifierProblem(spec string) string {
	if strings.Contains(spec, `\`) {
		return fmt.Sprintf(`import %q: đường dẫn viết bằng "/", không dùng "\": "app/core/x.work"`, spec)
	}
	rest, _ := strings.CutPrefix(spec, "app/")
	if spec == "app" || rest == "" {
		return fmt.Sprintf(`import %q: sau app/ là đường dẫn của một file từ gốc app: "app/core/x.work"`, spec)
	}
	segments := strings.Split(rest, "/")
	if segments[0] == "" || strings.Contains(segments[0], ":") {
		return fmt.Sprintf(`import %q: sau app/ là đường dẫn từ gốc app, không phải đường dẫn tuyệt đối: "app/core/x.work"`, spec)
	}
	for _, segment := range segments {
		switch {
		case segment != "" && strings.Trim(segment, ".") == "":
			// ".", "..", and a name of dots alone, which Windows trims to nothing
			return fmt.Sprintf(`import %q ra ngoài lối của app/: đường dẫn đi thẳng từ gốc app xuống, không có "." hay "..": "app/core/x.work"`, spec)
		case segment == "" || strings.Contains(segment, ":"):
			return fmt.Sprintf(`import %q: mỗi đoạn giữa hai "/" là tên một thư mục hay file, không rỗng, không có ":"`, spec)
		}
	}
	// Windows opens ".database." and ".database " as .database
	if strings.EqualFold(strings.TrimRight(segments[0], ". "), ".database") {
		return fmt.Sprintf(`import %q: .database/ là dữ liệu của app, không import được; code dùng chung đặt trong một thư mục của app (core/)`, spec)
	}
	return ""
}

// unsupportedSpecifierMessage is the error of an import from a place the native path does not know.
func unsupportedSpecifierMessage(spec string) string {
	return fmt.Sprintf(`import %q: chỉ import được thư viện của Kitwork ("kitwork/<tên>"), file từ gốc app ("app/core/x.work") hay file tương đối ("./x.work")`, spec)
}

// moduleImport is the ImportStatement of a module the bundler resolves, or nil with the error recorded
// when an app/… path is written wrong. tok is the specifier's string.
func (p *Parser) moduleImport(statement *ImportStatement, tok Token) Statement {
	if isAppRootSpecifier(statement.Source) {
		if problem := appRootSpecifierProblem(statement.Source); problem != "" {
			p.addErrorAt(tok, problem)
			return nil
		}
	}
	statement.SourceToken = tok
	return statement
}

// hasAlias báo có ít nhất một binding đổi tên (`imported as local`).
func hasAlias(specs []ImportSpec) bool {
	for _, s := range specs {
		if s.Local != s.Imported {
			return true
		}
	}
	return false
}

// memberConst dựng `const <local> = <obj>.<prop>`.
func memberConst(local string, obj Expression, prop string, origin Token) Statement {
	return &VarStatement{
		Token: constToken(origin),
		Names: []*Identifier{{
			Token: tokenWithValue(origin, Ident, value.String(local)),
			Value: local,
		}},
		DestructMode: DestructNone,
		Value: &MemberExpression{
			Token:  tokenWithValue(origin, Dot, value.Undefined()),
			Object: obj,
			Property: &Identifier{
				Token: tokenWithValue(origin, Ident, value.String(prop)),
				Value: prop,
			},
		},
	}
}

// constToken builds a synthetic `const` token for lowered declarations.
func constToken(origins ...Token) Token {
	var origin Token
	if len(origins) > 0 {
		origin = origins[0]
	}
	return tokenWithValue(origin, Const, value.String("const"))
}

// kitworkModule builds the expression `kitwork.<module>`: a member of the global kitwork, the app.
func kitworkModule(module string, origin Token) Expression {
	return &MemberExpression{
		Token: tokenWithValue(origin, Dot, value.Undefined()),
		Object: &Identifier{
			Token: tokenWithValue(origin, Ident, value.String("kitwork")),
			Value: "kitwork",
		},
		Property: &Identifier{
			Token: tokenWithValue(origin, Ident, value.String(module)),
			Value: module,
		},
	}
}

func tokenWithValue(origin Token, kind Kind, item value.Value) Token {
	return Token{
		Kind:     kind,
		Value:    item,
		Source:   origin.Source,
		Position: origin.Position,
	}
}

// parseFromSpecifier consumes a contextual `from "specifier"` and returns the
// specifier string. `from` is contextual (a normal identifier), not a keyword.
func (p *Parser) parseFromSpecifier() (string, bool) {
	if !p.peekTokenIs(Ident) || p.peekToken.Value.Text() != "from" {
		p.addError("import: cần from ở đây; Kitwork chưa có import x, { y } trong một dòng: viết hai dòng import")
		return "", false
	}
	p.nextToken() // cur: from
	if !p.expectPeek(String) {
		return "", false
	}
	return p.curToken.Value.Text(), true
}

func (p *Parser) parseImportStatement() Statement {
	// curToken == import
	importTok := p.curToken
	if !p.atTopLevel("import") {
		return nil
	}

	// Side-effect:  import "./mod"
	if p.peekTokenIs(String) {
		p.nextToken() // cur: string
		spec := p.curToken.Value.Text()
		if isRelativeSpecifier(spec) || isAppRootSpecifier(spec) {
			return p.moduleImport(&ImportStatement{Token: importTok, Source: spec, SideEffect: true}, p.curToken)
		}
		p.addErrorAt(p.curToken, unsupportedSpecifierMessage(spec))
		return nil
	}

	// Named:   import { a, b as c } from "..."
	if p.peekTokenIs(LeftBrace) {
		p.nextToken() // cur: {
		specs := []ImportSpec{}
		for !p.peekTokenIs(RightBrace) {
			p.nextToken() // cur: tên import
			aliased := isContextualToken(p.peekToken, "as")
			// Tên được import là tên thuộc tính, như JS: `import { function as f }` là
			// `const f = ….function`. Không có `as` thì chính nó thành tên biến, nên từ khoá bị chặn.
			if !aliased {
				if word, reserved := p.reservedWord(p.curToken); reserved {
					p.reservedImportError(p.curToken, word, fmt.Sprintf("import { %s as tên_khác } from …", word))
					return nil
				}
			}
			if !p.curTokenIs(Ident) && !(aliased && p.isKeyword(p.curToken)) {
				p.addError("import { … } liệt kê tên, ngăn bằng dấu phẩy: import { a, b as c } from …")
				return nil
			}
			imported := p.wordOf(p.curToken)
			local := imported
			// alias tùy chọn: `as local` (`as` là identifier theo ngữ cảnh)
			if aliased {
				p.nextToken() // cur: as
				if word, reserved := p.reservedWord(p.peekToken); reserved {
					p.reservedImportError(p.peekToken, word, fmt.Sprintf("import { %s as tên_khác } from …", imported))
					return nil
				}
				if !p.expectPeek(Ident) {
					return nil
				}
				local = p.curToken.Value.Text() // cur: local
			}
			specs = append(specs, ImportSpec{Imported: imported, Local: local})
			if p.peekTokenIs(Comma) {
				p.nextToken()
			} else if !p.peekTokenIs(RightBrace) {
				p.addError("import { … } liệt kê tên, ngăn bằng dấu phẩy: import { a, b as c } from …")
				return nil
			}
		}
		if !p.expectPeek(RightBrace) {
			return nil
		}
		spec, ok := p.parseFromSpecifier()
		if !ok {
			return nil
		}
		if module, ok := kitworkModulePath(spec); ok {
			if module == "" {
				p.addErrorAt(p.curToken, kitworkModuleMessage(spec))
				return nil
			}
			if !hasAlias(specs) {
				// → const { a, b } = kitwork.<module>
				names := make([]*Identifier, len(specs))
				for i, s := range specs {
					names[i] = &Identifier{
						Token: tokenWithValue(importTok, Ident, value.String(s.Local)),
						Value: s.Local,
					}
				}
				return &VarStatement{
					Token:        constToken(importTok),
					Names:        names,
					DestructMode: DestructObject,
					Value:        kitworkModule(module, importTok),
				}
			}
			// có alias → nhóm `const local = kitwork.<module>.imported`
			stmts := make([]Statement, len(specs))
			for i, s := range specs {
				stmts[i] = memberConst(s.Local, kitworkModule(module, importTok), s.Imported, importTok)
			}
			return &GroupStatement{Statements: stmts}
		}
		if isRelativeSpecifier(spec) || isAppRootSpecifier(spec) {
			return p.moduleImport(&ImportStatement{Token: importTok, Names: specs, Source: spec}, p.curToken)
		}
		p.addErrorAt(p.curToken, unsupportedSpecifierMessage(spec))
		return nil
	}

	// Namespace:  import * as http from "kitwork/http"  →  const http = kitwork.http
	// The name is the object a default import of the same specifier gives (19/10/2026).
	if p.peekTokenIs(Star) {
		p.nextToken() // cur: *
		if !isContextualToken(p.peekToken, "as") {
			p.addErrorAt(p.peekToken, "import * cần một tên: import * as x from \"kitwork/x\"")
			return nil
		}
		p.nextToken() // cur: as
		if word, reserved := p.reservedWord(p.peekToken); reserved {
			p.reservedImportError(p.peekToken, word, "import * as tên_khác from …")
			return nil
		}
		if !p.expectPeek(Ident) {
			return nil
		}
		name := &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
		spec, ok := p.parseFromSpecifier()
		if !ok {
			return nil
		}
		module, ok := kitworkModulePath(spec)
		if !ok {
			p.addErrorAt(importTok, fmt.Sprintf("import * as %s from %q: import * chỉ dùng với thư viện của Kitwork (\"kitwork/<tên>\"); file của site thì gọi tên từng thứ cần: import { a, b } from %q", name.Value, spec, spec))
			return nil
		}
		if module == "" {
			p.addErrorAt(p.curToken, kitworkModuleMessage(spec))
			return nil
		}
		return &VarStatement{
			Token:        constToken(importTok),
			Names:        []*Identifier{name},
			DestructMode: DestructNone,
			Value:        kitworkModule(module, importTok),
		}
	}

	// Default:  import name from "..."
	if word, reserved := p.reservedWord(p.peekToken); reserved {
		p.reservedImportError(p.peekToken, word, "import tên_khác from …")
		return nil
	}
	if p.peekTokenIs(Ident) {
		p.nextToken() // cur: local name
		name := &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
		spec, ok := p.parseFromSpecifier()
		if !ok {
			return nil
		}
		if module, ok := kitworkModulePath(spec); ok {
			if module == "" {
				p.addErrorAt(p.curToken, kitworkModuleMessage(spec))
				return nil
			}
			// → const name = kitwork.<module>, as import * as name gives
			return &VarStatement{
				Token:        constToken(importTok),
				Names:        []*Identifier{name},
				DestructMode: DestructNone,
				Value:        kitworkModule(module, importTok),
			}
		}
		if isRelativeSpecifier(spec) || isAppRootSpecifier(spec) {
			return p.moduleImport(&ImportStatement{Token: importTok, Default: name, Source: spec}, p.curToken)
		}
		p.addErrorAt(p.curToken, unsupportedSpecifierMessage(spec))
		return nil
	}

	p.addError("import viết một trong các dạng: import * as x from \"kitwork/x\", import { a } from \"./a.work\", import x from \"./a.work\", import \"./a.work\"")
	return nil
}

func (p *Parser) parseExportStatement() Statement {
	// curToken == export
	if !p.atTopLevel("export") {
		return nil
	}

	// export default <expr>  →  const __kw_default = <expr>
	// (giữ side-effect; bundler đưa __kw_default vào object export dưới khóa "default")
	if p.peekTokenIs(Ident) && p.peekToken.Value.Text() == "default" {
		p.nextToken() // cur: default
		defaultTok := p.curToken
		if p.hasDefault {
			// said here, by the author's words: left to the resolver it is a second const of the
			// internal name with no place (19/10/2026)
			p.addErrorAt(defaultTok, fmt.Sprintf("một file chỉ có một export default: export default thứ hai ở dòng %d, lần đầu ở dòng %d", p.lineOf(defaultTok), p.lineOf(p.defaultAt)))
			return nil
		}
		p.nextToken() // cur: start of expression
		p.hasDefault = true
		p.defaultAt = defaultTok
		return &VarStatement{
			Token:        constToken(defaultTok),
			Names:        []*Identifier{{Token: tokenWithValue(defaultTok, Ident, value.String(DefaultExportName)), Value: DefaultExportName}},
			DestructMode: DestructNone,
			Value:        p.parseExpression(LOWEST),
		}
	}

	// export const / export let  → ghi nhận các tên được khai báo
	if p.peekTokenIs(Const) || p.peekTokenIs(Let) {
		p.nextToken() // cur: const/let
		stmt := p.parseVarStatement()
		if vs, ok := stmt.(*VarStatement); ok {
			for _, n := range vs.Names {
				p.exports = append(p.exports, n.Value)
			}
		}
		return stmt
	}

	// export function f(){}  → ghi nhận tên hàm
	if p.peekTokenIs(Function) {
		p.nextToken() // cur: function
		if p.peekTokenIs(Ident) {
			p.exports = append(p.exports, p.peekToken.Value.Text())
		}
		return p.parseFunctionStatement()
	}

	// export { a, b as c }  — re-export local đã khai báo: ghi nhận tên, không sinh lệnh. `b as c` exports
	// b under the name c (22/10/2026; until then `as` and c were both recorded as names of their own).
	if p.peekTokenIs(LeftBrace) {
		// the list is names a comma holds apart, as in JavaScript (18/11/2026: a token that was no name was skipped, so
		// export { x y } exported both and export { x 5 + } compiled)
		p.nextToken() // cur: {
		p.nextToken()
		for !p.curTokenIs(RightBrace) {
			if !p.curTokenIs(Ident) {
				p.addError("export { … } liệt kê tên khai ở cấp cao nhất của file, ngăn bằng dấu phẩy: export { a, b as c }")
				return nil
			}
			localToken := p.curToken
			local := p.curToken.Value.Text()
			exported := local
			if isContextualToken(p.peekToken, "as") {
				p.nextToken() // cur: as
				if !p.expectPeek(Ident) {
					return nil
				}
				exported = p.curToken.Value.Text()
				if exported != local {
					p.aliases = append(p.aliases, ExportAlias{Local: local, Exported: exported, Token: p.curToken})
				}
			}
			p.exports = append(p.exports, exported)
			p.exportLocals = append(p.exportLocals, localToken)
			p.nextToken()
			if p.curTokenIs(Comma) {
				p.nextToken()
				continue
			}
			if !p.curTokenIs(RightBrace) {
				p.addError("export { … } liệt kê tên khai ở cấp cao nhất của file, ngăn bằng dấu phẩy: export { a, b as c }")
				return nil
			}
		}
		if isContextualToken(p.peekToken, "from") {
			// export { x } from "./o.work" was export { x } and then from and the string as statements of their own
			p.addErrorAt(p.peekToken, "Kitwork chưa có export … from: import tên ấy rồi export nó, import { x } from \"./o.work\"; export { x }")
			return nil
		}
		return nil
	}

	p.addError("export viết một trong các dạng: export const x = …, export function f() {…}, export default …, export { a, b as c }")
	return nil
}

// parseReturnStatement reads return and its value. A return with no value ends at ;, at the } of its block, at the
// end of the source, and at a line break, as JavaScript inserts the semicolon there: return⏎x returns undefined and
// leaves x a statement of its own (18/11/2026, docs/language.md mục 8.1; it returned x). The current token stays on
// the return or its ;, so the } that follows still closes the block: it was consumed, and the rest of the file
// silently became that block.
func (p *Parser) parseReturnStatement() Statement {
	stmt := &ReturnStatement{Token: p.curToken}
	if p.functionDepth == 0 {
		// a module's top level runs to its end, as in JavaScript (18/11/2026: a return there ended the file's run, and
		// in an imported file left its exports undefined)
		p.addError("return chỉ đứng trong một hàm: cấp cao nhất của file chạy tới cuối, như module của JavaScript")
		return nil
	}
	if p.peekTokenIs(Semicolon) {
		p.nextToken()
		return stmt
	}
	if p.peekTokenIs(RightBrace) || p.peekTokenIs(EOF) || p.lineBreakBeforePeek() {
		return stmt
	}
	p.nextToken()
	stmt.ReturnValue = p.parseExpression(LOWEST)
	return stmt
}

func (p *Parser) parseBreakStatement() Statement {
	stmt := &BreakStatement{Token: p.curToken}
	if p.breakDepth == 0 {
		p.addError("'break' chỉ hợp lệ bên trong switch hoặc vòng for")
	}
	if p.peekTokenIs(Semicolon) {
		p.nextToken()
		return stmt
	}
	// a line break ends it, as JavaScript inserts the semicolon there (18/11/2026: break⏎x was the labeled-break error)
	if p.peekTokenIs(RightBrace) || p.peekTokenIs(EOF) || p.lineBreakBeforePeek() ||
		isContextualToken(p.peekToken, "case") ||
		isContextualToken(p.peekToken, "default") {
		return stmt
	}
	p.addError("Kitwork không hỗ trợ labeled break; hãy dùng 'break;'")
	return stmt
}

// parseContinueStatement reads continue; (14/10/2026; it was the compile error "Kitwork chưa có
// continue"): the nearest loop goes on with its next iteration, the counting for after its update, as in
// JavaScript. Outside a loop (a switch alone, a function made in a loop) it is JavaScript's SyntaxError.
func (p *Parser) parseContinueStatement() Statement {
	stmt := &ContinueStatement{Token: p.curToken}
	if p.loopDepth == 0 {
		p.addError("'continue' chỉ hợp lệ bên trong vòng for (một switch không có vòng for bao ngoài thì không), như JavaScript")
	}
	if p.peekTokenIs(Semicolon) {
		p.nextToken()
		return stmt
	}
	// a line break ends it, as JavaScript inserts the semicolon there: a label would stay on its line
	if p.peekTokenIs(RightBrace) || p.peekTokenIs(EOF) || p.lineBreakBeforePeek() ||
		isContextualToken(p.peekToken, "case") ||
		isContextualToken(p.peekToken, "default") {
		return stmt
	}
	p.addError("Kitwork không hỗ trợ continue có nhãn; hãy dùng 'continue;'")
	return stmt
}

// lineBreakBeforePeek reports a line break (a comment's included) between the current token and the
// next one.
func (p *Parser) lineBreakBeforePeek() bool {
	start := int(p.curToken.Position-p.l.base) + int(p.curToken.Length)
	end := int(p.peekToken.Position - p.l.base)
	if start < 0 || end > len(p.l.input) || start > end {
		return false
	}
	return strings.IndexByte(string(p.l.input[start:end]), '\n') >= 0
}

// endStatement holds JavaScript's rule of where a statement ends (18/11/2026, docs/language.md mục 3.4): a ; is
// inserted only before a line break, a } or the end of the source, so a statement that does not end with a block of
// its own needs one of those, or a ;, before the next one. const r = 1 2 read r = 1 and then 2, as two statements; it
// is a compile error, as JavaScript's SyntaxError.
func (p *Parser) endStatement(stmt Statement) {
	if stmt == nil || endsWithItsBlock(stmt) || p.curTokenIs(Semicolon) {
		return
	}
	if p.peekTokenIs(Semicolon) || p.peekTokenIs(RightBrace) || p.peekTokenIs(EOF) || p.lineBreakBeforePeek() {
		return
	}
	if message, notYet := notYetWords[p.peekToken.Value.Text()]; notYet && p.peekTokenIs(Ident) {
		// a in o, x instanceof Array: the operator Kitwork has not, said as such, not as a second statement
		p.addErrorAt(p.peekToken, message)
		return
	}
	p.addErrorAt(p.peekToken, fmt.Sprintf("cần ; hay xuống dòng trước %q: hai câu lệnh trên một dòng ngăn nhau bằng ;", p.peekToken.String()))
}

// endsWithItsBlock: a statement that ends with a block of its own needs nothing after it, as in JavaScript
// (if (c) {} f(), function f() {} f()); an if's bodies are held where they end (parseIfExpression).
func endsWithItsBlock(stmt Statement) bool {
	switch s := stmt.(type) {
	case *BlockStatement, *ForStatement, *ForRangeStatement, *SwitchStatement:
		return true
	case *VarStatement:
		return s.Function
	case *ExpressionStatement:
		_, isIf := s.Expression.(*IfExpression)
		return isIf
	}
	return false
}

func (p *Parser) parseSwitchStatement() Statement {
	stmt := &SwitchStatement{Token: p.curToken, Cases: []SwitchCase{}}
	if !p.expectPeek(LeftParen) {
		return nil
	}
	p.nextToken()
	stmt.Discriminant = p.parseExpression(LOWEST)
	if stmt.Discriminant == nil || !p.expectPeek(RightParen) || !p.expectPeek(LeftBrace) {
		return nil
	}

	p.breakDepth++
	defer func() { p.breakDepth-- }()
	seenDefault := false
	p.nextToken()
	for !p.curTokenIs(RightBrace) && !p.curTokenIs(EOF) {
		if p.curTokenIs(Semicolon) {
			p.nextToken()
			continue
		}

		clause := SwitchCase{Token: p.curToken, Statements: []Statement{}}
		switch {
		case isContextualToken(p.curToken, "case"):
			p.nextToken()
			clause.Test = p.parseExpression(LOWEST)
			if clause.Test == nil || !p.expectPeek(Colon) {
				return nil
			}
		case isContextualToken(p.curToken, "default"):
			if seenDefault {
				p.addError("switch chỉ được có một default")
				return nil
			}
			seenDefault = true
			if !p.expectPeek(Colon) {
				return nil
			}
		default:
			p.addError("switch chỉ chấp nhận 'case' hoặc 'default' ở cấp thân trực tiếp")
			return nil
		}

		p.nextToken()
		for !p.curTokenIs(RightBrace) && !p.curTokenIs(EOF) &&
			!isContextualToken(p.curToken, "case") &&
			!isContextualToken(p.curToken, "default") {
			if p.curTokenIs(Semicolon) {
				p.nextToken()
				continue
			}
			caseStatement := p.parseStatement()
			if caseStatement != nil {
				clause.Statements = append(clause.Statements, caseStatement)
			}
			p.endStatement(caseStatement)
			p.nextToken()
		}
		stmt.Cases = append(stmt.Cases, clause)
	}
	if p.curTokenIs(EOF) {
		p.addError("switch thiếu dấu '}' kết thúc")
		return nil
	}
	return stmt
}

func (p *Parser) parseExpressionStatement() Statement {
	stmt := &ExpressionStatement{Token: p.curToken}
	stmt.Expression = p.parseExpression(LOWEST)
	// if p.peekTokenIs(Semicolon) {
	// 	p.nextToken()
	// }
	return stmt
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	block := &BlockStatement{Token: p.curToken, Statements: []Statement{}}
	p.nextToken() // Bỏ dấu {

	for !p.curTokenIs(RightBrace) && !p.curTokenIs(EOF) {
		// Bỏ qua dấu chấm phẩy thừa
		if p.curTokenIs(Semicolon) {
			p.nextToken()
			continue
		}

		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.endStatement(stmt)
		p.nextToken()
	}
	// a block the source ends inside is JavaScript's SyntaxError (18/11/2026, docs/language.md mục 8.3): it was
	// accepted, the rest of the file read as the block's
	if p.curTokenIs(EOF) {
		p.addErrorAt(block.Token, "khối mở bằng { ở đây chưa đóng: thiếu } trước khi hết file")
	}
	return block
}

func (p *Parser) parseBreakableBlock() *BlockStatement {
	p.breakDepth++
	defer func() { p.breakDepth-- }()
	return p.parseBlockStatement()
}

// parseLoopBody reads the body of a for, where break and continue are allowed.
func (p *Parser) parseLoopBody() *BlockStatement {
	p.loopDepth++
	defer func() { p.loopDepth-- }()
	return p.parseBreakableBlock()
}

func (p *Parser) parseFunctionBlock() *BlockStatement {
	outerBreakDepth, outerLoopDepth := p.breakDepth, p.loopDepth
	p.breakDepth, p.loopDepth = 0, 0
	p.functionDepth++
	defer func() { p.breakDepth, p.loopDepth = outerBreakDepth, outerLoopDepth; p.functionDepth-- }()
	return p.parseBlockStatement()
}

// checkExports holds each name export { … } lists to one the file's top level declares, as JavaScript does
// (18/11/2026: an undeclared one was exported as undefined).
func (p *Parser) checkExports(program *Program) {
	if len(p.exportLocals) == 0 {
		return
	}
	declared := map[string]bool{}
	var collect func(statements []Statement)
	collect = func(statements []Statement) {
		for _, statement := range statements {
			switch s := statement.(type) {
			case *VarStatement:
				for _, name := range s.Names {
					declared[name.Value] = true
				}
			case *GroupStatement:
				collect(s.Statements)
			case *ImportStatement:
				for _, spec := range s.Names {
					declared[spec.Local] = true
				}
				if s.Default != nil {
					declared[s.Default.Value] = true
				}
			}
		}
	}
	collect(program.Statements)
	for _, tok := range p.exportLocals {
		if name := tok.Value.Text(); !declared[name] {
			p.addErrorAt(tok, fmt.Sprintf("export { %s }: %s không được khai ở cấp cao nhất của file", name, name))
			return
		}
	}
}

// arrowParameterProblem says why what stands before => is no parameter list (18/11/2026: it said "arrow function
// parameters must be identifiers", for async () => … too).
func arrowParameterProblem(left Expression) string {
	if call, ok := left.(*CallExpression); ok {
		if name, ok := call.Function.(*Identifier); ok && name.Value == "async" {
			return "Kitwork không có async, await: việc chạy song song và chờ đi qua kitwork/routine"
		}
	}
	return "tham số của hàm mũi tên là tên hay mẫu tách, ngăn bằng dấu phẩy: (a, { b }, ...rest) => …"
}

// atTopLevel holds import and export to a statement of the file's top level, as in JavaScript (18/11/2026: in a
// function a library's import ran at each call, and a file's was an error of the bundler with no place).
func (p *Parser) atTopLevel(word string) bool {
	if p.statementDepth > 1 || p.functionDepth > 0 {
		p.addError(word + " chỉ đứng ở cấp cao nhất của file, không trong hàm hay khối")
		return false
	}
	return true
}

func isContextualToken(token Token, word string) bool {
	return token.Kind == Ident && token.Value.Text() == word
}

/* =============================================================================
   2. EXPRESSIONS (Biểu thức)
   ============================================================================= */

func (p *Parser) parseExpression(precedence int) Expression {
	if !p.enterNesting() {
		return nil
	}
	defer p.leaveNesting()
	// Một từ khoá đứng trơ trước `,` `)` `=>` `]` `}` `;` hay hết nguồn — (new) => …, f(function), [new],
	// { ...new }, ${function} — là đang được dùng làm tên. true/false/null/undefined là giá trị nên đi tiếp
	// xuống parseLiteral.
	if p.curToken.Kind != Boolean && p.curToken.Kind != Null &&
		(p.peekTokenIs(Comma) || p.peekTokenIs(RightParen) || p.peekTokenIs(FatArrow) ||
			p.peekTokenIs(RightBracket) || p.peekTokenIs(RightBrace) || p.peekTokenIs(Semicolon) || p.peekTokenIs(EOF)) {
		if word, ok := p.keywordSpelling(p.curToken); ok {
			p.reservedNameError(p.curToken, word)
			return nil
		}
	}
	prefix := prefixParseFns[p.curToken.Kind]
	if prefix == nil {
		if p.curTokenIs(Spread) {
			p.addErrorAt(p.curToken, "... chỉ trải trong một mảng ([...a]), đối số của lời gọi (f(...args)) hay một object ({ ...o }); phần còn lại của tham số và mẫu tách viết (a, ...rest) => …, const [a, ...rest] = list")
			return nil
		}
		p.addError(fmt.Sprintf("không đọc được %q ở đầu một biểu thức", p.curToken.String()))
		return nil
	}
	// An error said while the expression is read leaves it nothing (13/11/2026): a part that failed is nil, and an
	// expression made around it (!nil, nil++, a.b of nil) was a node with a nil inside, which the next function
	// out read and dereferenced, ++++! the compiler's panic. A program with errors is dropped and its first error is
	// what is said, so reading on after one made nothing but the risk.
	failed := len(p.errors)
	start := p.curToken.Position
	leftExp := prefix(p)
	if len(p.errors) > failed {
		return nil
	}

	for !p.peekTokenIs(Semicolon) && precedence < p.peekPrecedence() {
		if (p.peekTokenIs(PlusPlus) || p.peekTokenIs(MinusMinus)) && p.lineBreakBeforePeek() {
			// x\n++y is x; ++y: a line break before ++ or -- ends the expression, as JavaScript inserts the
			// semicolon there (15/10/2026; it was x++; y, an update of the wrong variable)
			break
		}
		if p.peekTokenIs(FatArrow) && !p.arrowMayStand(precedence) {
			return nil
		}
		infix := infixParseFns[p.peekToken.Kind]
		if infix == nil {
			p.refuseTag()
			return p.closeChain(leftExp)
		}
		p.nextToken()
		p.leftStart = start
		leftExp = infix(p, leftExp)
		if len(p.errors) > failed {
			return nil
		}
		if !p.continuesChain() {
			leftExp = p.closeChain(leftExp)
		}
	}
	p.refuseTag()
	return p.closeChain(leftExp)
}

// arrowMayStand holds where JavaScript reads a =>: its parameters on its line, and the function a whole expression
// (18/11/2026, docs/language.md mục 3.5): an argument, a value, an initializer, the right of = or an arm of ?:. As the
// operand of an operator it is a SyntaxError (1 + a => 2 read "1function"), and so is a line break before =>.
func (p *Parser) arrowMayStand(precedence int) bool {
	if p.lineBreakBeforePeek() {
		p.addErrorAt(p.peekToken, "=> đứng cùng dòng với tham số của nó, như JavaScript: (a) => …")
		return false
	}
	if precedence > LOWEST {
		p.addErrorAt(p.peekToken, "hàm mũi tên làm toán hạng của một toán tử thì đặt trong ngoặc: 1 + ((a) => 2), !((a) => a)")
		return false
	}
	return true
}

// parseIfAsValue: if is a statement; where a value stands it is an error (18/11/2026).
func (p *Parser) parseIfAsValue() Expression {
	p.addError("if là một câu lệnh, không có giá trị: ở chỗ của một giá trị viết điều_kiện ? a : b")
	return nil
}

// refuseTag: a template right after an expression, on its line or the next, is a tagged template in JavaScript, which
// calls the expression with the template's parts. Kitwork has none (18/11/2026, docs/language.md mục 8.1): f`x` read
// f and then the template as a statement of its own, so the tag was silently never called. It is a compile error.
func (p *Parser) refuseTag() {
	if p.peekTokenIs(Template) {
		p.addErrorAt(p.peekToken, "Kitwork chưa có chuỗi mẫu có thẻ (tag`…`): gọi hàm với chuỗi mẫu làm đối số, tag(`…`); chuỗi mẫu đứng riêng ở dòng sau một biểu thức thì đặt ; trước nó")
	}
}

// continuesChain reports whether the next token adds a link to the expression just read: . ?. [ (
func (p *Parser) continuesChain() bool {
	return p.peekTokenIs(Dot) || p.peekTokenIs(OptionalChain) || p.peekTokenIs(LeftBracket) || p.peekTokenIs(LeftParen)
}

// link records that node, a member, index or call on object, belongs to an optional chain still open:
// object's, or the one node starts (optional).
func (p *Parser) link(node, object Expression, optional bool) {
	if node == nil || (!optional && !p.openChain[object]) {
		return
	}
	if p.openChain == nil {
		p.openChain = map[Expression]bool{}
	}
	delete(p.openChain, object)
	p.openChain[node] = true
}

// closeChain wraps an optional chain that nothing continues into one ChainExpression, the unit a ?.
// that finds null skips to the end of.
func (p *Parser) closeChain(e Expression) Expression {
	if e == nil || !p.openChain[e] {
		return e
	}
	delete(p.openChain, e)
	return &ChainExpression{Expression: e}
}

func (p *Parser) parseIdentifier() Expression {
	if operatorWords[p.curToken.Value.Text()] {
		if p.curToken.Value.Text() == "delete" {
			return p.parseDeleteExpression()
		}
		return p.parseTypeofExpression()
	}
	// break/case/default/continue chỉ có nghĩa ở đầu câu lệnh (parseStatement, switch). Ở chỗ biểu thức
	// chúng là đọc hoặc gán một biến không khai báo được (default = 5, [case], ${break}), nên cũng là lỗi
	// từ khoá.
	if message, notYet := notYetWords[p.curToken.Value.Text()]; notYet {
		p.addErrorAt(p.curToken, message)
		return nil
	}
	if word := p.curToken.Value.Text(); statementWords[word] {
		p.reservedNameError(p.curToken, word)
		return nil
	}
	return &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
}

// parseTypeofExpression reads typeof x (14/10/2026; it was the compile error "Kitwork chưa có typeof"):
// JavaScript's name of x's kind, a string, bound as tightly as ! and - are (typeof a.b is typeof (a.b),
// typeof x === "string" compares the string). Standing where a name would (typeof = 1, (typeof) => …)
// it is the keyword error.
func (p *Parser) parseTypeofExpression() Expression {
	if p.peekTokenIs(Comma) || p.peekTokenIs(RightParen) || p.peekTokenIs(FatArrow) || p.peekTokenIs(Assign) ||
		p.peekTokenIs(RightBracket) || p.peekTokenIs(RightBrace) || p.peekTokenIs(Semicolon) || p.peekTokenIs(EOF) ||
		p.peekTokenIs(Colon) {
		p.reservedNameError(p.curToken, p.curToken.Value.Text())
		return nil
	}
	return p.parsePrefixExpression()
}

// parseDeleteExpression reads delete o.k and delete o[k] (18/11/2026, docs/language.md mục 3.3; decided 27/10/2026,
// it was the compile error "Kitwork chưa có delete"): strict JavaScript's delete of a key, true once the key is gone
// (runtime DELETE). What is no key, a name or anything else strict JavaScript refuses, is a compile error, and so is
// delete through ?. (delete o?.k), which Kitwork has not.
func (p *Parser) parseDeleteExpression() Expression {
	deleteToken := p.curToken
	operand := p.parseTypeofExpression()
	prefix, ok := operand.(*PrefixExpression)
	if !ok {
		return operand
	}
	switch target := prefix.Right.(type) {
	case *MemberExpression:
		if !target.Optional {
			return prefix
		}
	case *IndexExpression:
		if !target.Optional {
			return prefix
		}
	case *Identifier:
		p.addErrorAt(deleteToken, fmt.Sprintf("delete %s: strict JavaScript không xoá một tên, chỉ xoá một khoá: delete o.k, delete o[k]", target.Value))
		return nil
	}
	if _, chain := prefix.Right.(*ChainExpression); !chain && !p.openChain[prefix.Right] {
		p.addErrorAt(deleteToken, "delete xoá một khoá của một object: delete o.k, delete o[k]")
		return nil
	}
	p.addErrorAt(deleteToken, "Kitwork chưa có delete qua ?.: kiểm trước, if (o != null) delete o.k")
	return nil
}

func (p *Parser) parseLiteral() Expression {
	return &Literal{Token: p.curToken, Value: p.curToken.Value}
}

// parseTemplateLiteral splits a template's source text (the lexer keeps it as written) into its pieces:
// each piece of text cooked on its own (cookText), each ${…} parsed by a parser of its own over the
// source as written, at its own place in the file. Until 03/11/2026 the whole text was unescaped first
// and split after, so an escaped dollar before { opened an expression and a string inside ${…} lost its
// escapes to the template's.
func (p *Parser) parseTemplateLiteral() Expression {
	templateToken := p.curToken
	text := templateToken.Value.Text()
	tl := &TemplateLiteral{Token: templateToken, Parts: []Expression{}}
	pieces, _, _ := scanTemplate([]byte(text))
	for _, piece := range pieces {
		// the text starts one byte after the backtick, and keeps the source's bytes, so a piece's place
		// in the file is an addition
		at := templateToken.Position + 1 + int32(piece.start)
		if !piece.expression {
			cooked, escape := cookText(text[piece.start:piece.end], true)
			if cooked.IsInvalid() {
				p.addErrorAt(Token{Source: templateToken.Source, Position: at + int32(escape)}, cooked.Message())
				return nil
			}
			tl.Parts = append(tl.Parts, &Literal{
				Token: Token{Kind: String, Source: templateToken.Source, Position: at},
				Value: cooked,
			})
			continue
		}
		subLexer := newLexerAt(text[piece.start:piece.end], templateToken.Source, at)
		subParser := NewParser(subLexer)
		subParser.text = p.text
		subParser.nesting = p.nesting
		expr := subParser.parseExpression(LOWEST)
		if errors := subParser.Errors(); len(errors) > 0 {
			p.addError("invalid template expression: " + errors[0])
			return nil
		}
		if expr == nil {
			p.addError("template interpolation requires an expression")
			return nil
		}
		if !subParser.peekTokenIs(EOF) {
			subParser.addErrorAt(subParser.peekToken, fmt.Sprintf("${…} là một biểu thức; gặp thêm %q sau nó", subParser.peekToken.String()))
			p.addError("invalid template expression: " + subParser.Errors()[0])
			return nil
		}
		tl.Parts = append(tl.Parts, expr)
	}
	return tl
}

func (p *Parser) parseInfixExpression(left Expression) Expression {
	// Xử lý toán tử gán riêng biệt
	if p.curToken.Kind == Assign {
		if !p.assignable(left, p.curToken) {
			return nil
		}
		ae := &AssignmentExpression{Token: p.curToken, Name: left}
		// ({ a, b: c } = o), [a, b] = arr: the literal on the left is a pattern.
		pattern, ok := p.assignmentPattern(left)
		if !ok {
			return nil
		}
		if pattern != nil {
			ae.Name = pattern
		}
		p.nextToken()
		ae.Value = p.parseExpression(ASSIGN - 1)
		return ae
	}

	exp := &InfixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Value.Text(),
		Left:     left,
	}

	prec := p.curPrecedence()
	p.nextToken()
	exp.Right = p.parseExpression(prec)
	if p.mixesNullish(exp) {
		p.addErrorAt(exp.Token, "không trộn ?? với && hay || khi không có ngoặc (JavaScript cũng báo lỗi): viết (a ?? b) || c hoặc a ?? (b || c)")
		return nil
	}
	return exp
}

// mixesNullish reports a ?? written next to && or || with no parenthesis between them: JavaScript
// refuses a ?? b || c, since nothing says which one goes first.
func (p *Parser) mixesNullish(exp *InfixExpression) bool {
	var other []string
	switch exp.Operator {
	case "??":
		other = []string{"&&", "||"}
	case "&&", "||":
		other = []string{"??"}
	default:
		return false
	}
	for _, side := range []Expression{exp.Left, exp.Right} {
		infix, ok := side.(*InfixExpression)
		if !ok || p.grouped[side] {
			continue
		}
		for _, operator := range other {
			if infix.Operator == operator {
				return true
			}
		}
	}
	return false
}

// assignable refuses an optional chain on the left of =, +=, ++: a?.b = 1 is a syntax error in
// JavaScript too.
func (p *Parser) assignable(left Expression, operator Token) bool {
	if chain, ok := left.(*ChainExpression); ok {
		p.addErrorAt(operator, fmt.Sprintf("không gán được vào %s: ?. chỉ để đọc; kiểm rồi gán, như if (o) o.x = …", chain.String()))
		return false
	}
	return true
}

func (p *Parser) parseDotExpression(left Expression) Expression {
	tok := p.curToken // Dấu '.'
	start := p.leftStart
	p.nextToken() // Sang tên phương thức/thuộc tính

	// Tạo Identifier từ Token hiện tại. Từ khoá sau dấu chấm là tên thuộc tính như JS (o.function,
	// o.default, o.undefined): lấy đúng chữ trong nguồn, vì token true/false/null/undefined không giữ chữ.
	property := p.curToken.Value.Text()
	if word, ok := p.keywordSpelling(p.curToken); ok {
		property = word
	} else if !p.curTokenIs(Ident) {
		// o.), o.'a', o.+: JavaScript's SyntaxError (03/11/2026); it read the token's text as the name, so
		// o.) was o[")"], undefined
		found := p.curToken.String()
		if start := int(p.curToken.Position - p.l.base); p.curToken.Length > 0 && start >= 0 && start+int(p.curToken.Length) <= len(p.l.input) {
			found = string(p.l.input[start : start+int(p.curToken.Length)]) // 'a' with its quotes, as written
		}
		p.addErrorAt(p.curToken, fmt.Sprintf("sau . là một tên: o.x (gặp %q); khoá là chuỗi hay số thì o[\"a\"], o[0]", found))
		return nil
	}
	name := &Identifier{
		Token: p.curToken,
		Value: property,
	}

	// Kiểm tra xem có phải là gọi Method không: object.method(...)
	if p.peekTokenIs(LeftParen) {
		p.nextToken() // Chuyển curToken sang dấu '('
		call := &MethodCallExpression{
			Token:     tok,
			Object:    left,
			Method:    name,
			Arguments: p.parseExpressionList(RightParen),
		}
		if property == "safe" && call.Arguments != nil && len(call.Arguments) == 0 {
			p.addErrorAt(name.Token, removedSafeMessage(p.sourceBetween(start, tok.Position)))
		}
		p.link(call, left, false)
		return call
	}

	// Nếu không có dấu '(', đây là truy cập thuộc tính bình thường
	member := &MemberExpression{
		Token:    tok,
		Object:   left,
		Property: name,
	}
	p.link(member, left, false)
	return member
}

// parseOptionalChain reads what follows ?.: a name (o?.x, o?.x(…)), [ (o?.[k]) or ( (f?.(…), o.x?.(…)).
// Each link is read only when what is on its left is not null or undefined; otherwise the rest of the
// chain is skipped and it is undefined (ChainExpression).
func (p *Parser) parseOptionalChain(left Expression) Expression {
	tok := p.curToken // ?.
	start := p.leftStart
	var node Expression
	switch {
	case p.peekTokenIs(LeftParen):
		p.nextToken() // cur: (
		paren := p.curToken
		args := p.parseExpressionList(RightParen)
		if member, ok := left.(*MemberExpression); ok && !p.grouped[left] {
			// o.x?.(…) calls x as o.x(…) does, when o.x is not null.
			delete(p.openChain, left)
			node = &MethodCallExpression{Token: member.Token, Object: member.Object, Method: member.Property,
				Arguments: args, Optional: member.Optional, OptionalCall: true}
			// o.safe?.() is the removed .safe() as much as o.safe() is
			if member.Property.Value == "safe" && args != nil && len(args) == 0 {
				p.addErrorAt(member.Property.Token, removedSafeMessage(p.sourceBetween(start, member.Token.Position)))
			}
		} else {
			node = &CallExpression{Token: paren, Function: left, Arguments: args, Optional: true}
		}
	case p.peekTokenIs(LeftBracket):
		p.nextToken() // cur: [
		index := &IndexExpression{Token: p.curToken, Left: left, Optional: true}
		p.nextToken()
		index.Index = p.parseExpression(LOWEST)
		if !p.expectPeek(RightBracket) {
			return nil
		}
		node = index
	default:
		p.nextToken() // cur: the name
		property, ok := p.keywordSpelling(p.curToken)
		if !ok {
			if !p.curTokenIs(Ident) {
				p.addErrorAt(tok, "sau ?. là một tên, [ hay (: o?.x, o?.[k], f?.()")
				return nil
			}
			property = p.curToken.Value.Text()
		}
		name := &Identifier{Token: p.curToken, Value: property}
		if p.peekTokenIs(LeftParen) {
			p.nextToken() // cur: (
			call := &MethodCallExpression{Token: tok, Object: left, Method: name,
				Arguments: p.parseExpressionList(RightParen), Optional: true}
			// o?.safe() is the removed .safe() as much as o.safe() is
			if property == "safe" && call.Arguments != nil && len(call.Arguments) == 0 {
				p.addErrorAt(name.Token, removedSafeMessage(p.sourceBetween(start, tok.Position)))
			}
			node = call
		} else {
			node = &MemberExpression{Token: tok, Object: left, Property: name, Optional: true}
		}
	}
	p.link(node, left, true)
	return node
}

func (p *Parser) parsePrefixExpression() Expression {
	exp := &PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Value.Text(),
	}
	p.nextToken()
	exp.Right = p.parseExpression(PREFIX)
	return exp
}

// parseNewExpression reads new as JavaScript does for what Kitwork makes with it: Date, Array and Object, whose new
// is their call (new Date(x) is Date(x), new Array(3) three undefined, new Object() {}), and new X with no parentheses
// calls X with none (18/11/2026, docs/language.md mục 3.3: new was a prefix that meant nothing, so new X was X itself,
// typeof new Date "function", and new on any function was a plain call). Kitwork has no constructor of a program (no
// class, no this) and no wrapper object, so new on anything else is a compile error: new String(5) was the string "5",
// where JavaScript makes an object.
func (p *Parser) parseNewExpression() Expression {
	p.nextToken() // bỏ qua từ khóa 'new'
	if !p.curTokenIs(Ident) {
		p.addError("new chỉ dùng với Date, Array, Object: new Date(…), new Array(n), new Object()")
		return nil
	}
	name := p.curToken.Value.Text()
	switch name {
	case "Date", "Array", "Object":
	case "String", "Number", "Boolean":
		p.addError(fmt.Sprintf("Kitwork không có object bọc: new %s(x) viết %s(x), cho một giá trị thường", name, name))
		return nil
	default:
		p.addError("Kitwork không có hàm dựng của chương trình (không class, không this): gọi thẳng hàm, bỏ new; new chỉ dùng với Date, Array, Object")
		return nil
	}
	callee := &Identifier{Token: p.curToken, Value: name}
	if p.peekTokenIs(LeftParen) {
		p.nextToken()
		call := p.parseCallExpression(callee)
		if made, ok := call.(*CallExpression); ok {
			made.New = true
		}
		return call
	}
	if p.peekTokenIs(Dot) || p.peekTokenIs(LeftBracket) || p.peekTokenIs(OptionalChain) {
		// new Date.now is new (Date.now) in JavaScript, not (new Date).now
		p.addErrorAt(p.peekToken, fmt.Sprintf("new %s không ngoặc rồi một thành viên: viết (new %s()).x hay new %s().x", name, name, name))
		return nil
	}
	close := p.curToken.Position + int32(p.curToken.Length)
	return &CallExpression{Token: p.curToken, Function: callee, Arguments: []Expression{}, Close: close, New: true}
}

// parseTernaryExpression xử lý `cond ? consequence : alternative`.
func (p *Parser) parseTernaryExpression(cond Expression) Expression {
	exp := &TernaryExpression{Token: p.curToken, Condition: cond}

	p.nextToken()
	exp.Consequence = p.parseExpression(ASSIGN - 1)

	if !p.expectPeek(Colon) {
		return nil
	}

	p.nextToken()
	// ASSIGN-1 cho phép ternary lồng nhau kết hợp phải: a ? b : c ? d : e
	exp.Alternative = p.parseExpression(ASSIGN - 1)
	return exp
}

// parseCompoundAssignment desugar `x += y` thành `x = x + y` (tương tự -=, *=, /=)
// nên không cần opcode mới — tái dùng đường biên dịch Assignment + Infix sẵn có.
func (p *Parser) parseCompoundAssignment(left Expression) Expression {
	tok := p.curToken
	baseOp := string(tok.Value.Text()[0]) // "+=" -> "+"
	if !p.assignable(left, tok) {
		return nil
	}

	p.nextToken()
	right := p.parseExpression(ASSIGN - 1)

	return &AssignmentExpression{
		Token: tok,
		Name:  left,
		Value: &InfixExpression{Token: tok, Left: left, Operator: baseOp, Right: right},
	}
}

// updateExpression dựng `target = target ± 1` dùng chung cho ++ và --, đánh dấu trước hay sau toán hạng:
// compiler cho x++ giá trị cũ, ++x giá trị mới khi giá trị được dùng (14/10/2026).
func updateExpression(tok Token, target Expression, kind UpdateKind) Expression {
	op := "+"
	if tok.Kind == MinusMinus {
		op = "-"
	}
	one := &Literal{Token: Token{Kind: Number}, Value: value.Number(1)}
	return &AssignmentExpression{
		Token:  tok,
		Name:   target,
		Value:  &InfixExpression{Token: tok, Left: target, Operator: op, Right: one, Step: true},
		Update: kind,
	}
}

// parsePostfixUpdate xử lý `i++` / `i--`: giá trị là i CŨ, như JavaScript (14/10/2026; trước đó là giá trị
// mới, để tiết kiệm byte khi bundle lớn nhất sát trần 65 535 byte của bytecode 2). Làm câu lệnh hay bước
// nhảy của for thì giá trị bị bỏ, và mã dịch ra y như trước.
func (p *Parser) parsePostfixUpdate(left Expression) Expression {
	if left == nil {
		return nil // what is before ++ failed and said why
	}
	if !p.assignable(left, p.curToken) || !p.updatable(left, p.curToken) {
		return nil
	}
	return updateExpression(p.curToken, left, PostfixUpdate)
}

// updatable refuses ++ or -- of an update (++x++, (x++)++, --x--) in the words the source wrote: it was
// told of the lowering, "không gán được vào (x = (x + 1)): vế trái của = …", an = the author never wrote
// (15/10/2026).
func (p *Parser) updatable(target Expression, operator Token) bool {
	update, ok := target.(*AssignmentExpression)
	if !ok || update.Update == NotAnUpdate {
		return true
	}
	inner := "++"
	if update.Token.Kind == MinusMinus {
		inner = "--"
	}
	written := inner + update.Name.String()
	if update.Update == PostfixUpdate {
		written = update.Name.String() + inner
	}
	verb := "tăng"
	if operator.Kind == MinusMinus {
		verb = "giảm"
	}
	p.addErrorAt(operator, fmt.Sprintf("không %s được %s: ++ và -- chỉ dùng trên một tên, o.x hay o[k], không trên một ++ hay -- khác", verb, written))
	return false
}

// parsePrefixUpdate xử lý `++i` / `--i`: giá trị là i MỚI.
func (p *Parser) parsePrefixUpdate() Expression {
	tok := p.curToken
	p.nextToken()
	target := p.parseExpression(PREFIX)
	if target == nil {
		// what follows ++ failed and said why: --(--x) is the error of the inner --x, and ----x read on as an
		// update of nothing, which the next -- out read as a name (13/11/2026: a nil dereference in updatable
		// at --------x, the compiler's panic, the host's end at boot)
		if len(p.errors) == 0 {
			p.addErrorAt(tok, fmt.Sprintf("sau %s là một tên, o.x hay o[k]", tok.String()))
		}
		return nil
	}
	if !p.assignable(target, tok) || !p.updatable(target, tok) {
		return nil
	}
	return updateExpression(tok, target, PrefixUpdate)
}

// parseReservedKeyword báo lỗi biên dịch thân thiện cho các từ khóa bị loại bỏ
// có chủ đích khỏi ngôn ngữ — kèm hướng dẫn cách viết thay thế theo triết lý Kitwork.
func (p *Parser) parseReservedKeyword() Expression {
	word := p.curToken.Value.Text()
	switch word {
	case "while", "do":
		p.addError(fmt.Sprintf("Kitwork không hỗ trợ vòng điều kiện tuỳ ý '%s' (có thể lặp vô tận). Hãy dùng vòng ĐẾM 'for (let i = 0; i < n; i++)', duyệt 'for (const x of arr)', hoặc .map()/.filter()/.find() trên mảng.", word))
	case "try", "catch", "finally", "throw":
		p.addError(fmt.Sprintf("Kitwork không hỗ trợ '%s' (loại bỏ có chủ đích cho đơn giản): báo lỗi bằng return error(\"…\"), lỗi tự đi lên; thu lỗi thành kết quả bằng safe(() => …), đọc result.error (docs/language.md mục 5).", word))
	case "class":
		p.addError("Kitwork không hỗ trợ 'class'. Hãy dùng object literal và arrow function.")
	default:
		p.addError(fmt.Sprintf("Từ khóa '%s' không được hỗ trợ trong Kitwork.", word))
	}
	return nil
}

func (p *Parser) parseIfExpression() Expression {
	exp := &IfExpression{Token: p.curToken}
	if !p.expectPeek(LeftParen) {
		return nil
	}
	p.nextToken()
	exp.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(RightParen) {
		return nil
	}

	if p.peekTokenIs(LeftBrace) {
		p.nextToken()
		exp.Consequence = p.parseBlockStatement()
	} else {
		p.nextToken()
		stmt := p.parseStatement()
		exp.Consequence = &BlockStatement{Statements: []Statement{stmt}}
		// a body that is no block ends as any statement does, and its ; may stand before else, as in JavaScript:
		// if (c) x = 1; else y = 2 (18/11/2026; the ; was an error and if (c) x = 1 else y = 2 was read)
		p.endStatement(stmt)
		if p.peekTokenIs(Semicolon) {
			p.nextToken()
		}
	}

	if p.peekTokenIs(Else) {
		p.nextToken()
		if p.peekTokenIs(LeftBrace) {
			p.nextToken()
			exp.Alternative = p.parseBlockStatement()
		} else {
			p.nextToken()
			stmt := p.parseStatement()
			exp.Alternative = &BlockStatement{Statements: []Statement{stmt}}
			p.endStatement(stmt)
		}
	}
	return exp
}

// parseForStatement accepts EXACTLY two shapes, and rejects everything else with a friendly error:
//
//	for (let i = 0; i < n; i++) { … }   // a BOUNDED counting loop  → ForRangeStatement
//	for (const x of arr) { … }          // iterate a collection      → ForStatement (ITER)
//
// It deliberately refuses for(;;) / for(; cond ;) — an arbitrary condition loop is `while` in
// disguise, and Kitwork's guarantee is "no infinite loop by construction". The counter must be
// declared (let/const), the condition must compare THAT counter, and the update must mutate it.
func (p *Parser) parseForStatement() Statement {
	forTok := p.curToken
	if !p.expectPeek(LeftParen) {
		return nil
	}

	// Reject the empty / condition-only forms up front.
	if p.peekTokenIs(Semicolon) || p.peekTokenIs(RightParen) {
		p.addError("Kitwork chỉ hỗ trợ vòng ĐẾM: for (let i = 0; i < n; i++). Dạng for(;;) / for(; điều_kiện ;) không được phép — đó là 'while' trá hình (dùng vòng đếm, hoặc .map()/.filter()).")
		return nil
	}

	// The counter MUST be declared with let/const.
	if !p.peekTokenIs(Let) && !p.peekTokenIs(Const) {
		p.addError("Biến đếm của for phải khai báo bằng 'let'/'const': for (let i = 0; i < n; i++) hoặc for (const x of arr).")
		return nil
	}
	declTok := p.peekToken
	p.nextToken() // cur: let/const
	if p.peekTokenIs(LeftBrace) || p.peekTokenIs(LeftBracket) {
		// for (const { id, name } of list), for (const [key, value] of Object.entries(o)) (15/11/2026, round 5 of the
		// 13/11 review): the item is a variable named by the pattern, and each iteration starts by destructuring it
		p.nextToken() // cur: { or [
		pattern := p.parseBindingPattern()
		if pattern == nil {
			return nil
		}
		if !(p.peekTokenIs(Ident) && p.peekToken.Value.Text() == "of") {
			p.addErrorAt(p.peekToken, "tách trong for chỉ có ở for…of: for (const { a } of list)")
			return nil
		}
		item, destructure := patternParameter(pattern, declTok.Kind)
		statement := p.parseForOf(forTok, item)
		loop, ok := statement.(*ForStatement)
		if !ok || loop == nil || loop.Body == nil {
			return statement
		}
		loop.Const = declTok.Kind == Const
		loop.Body.Statements = append([]Statement{destructure}, loop.Body.Statements...)
		return loop
	}
	if !p.expectBindingName() {
		return nil
	}
	counter := &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}

	// Disambiguate on the token after the counter name: '=' → counting loop, 'of' → iteration.
	switch {
	case p.peekTokenIs(Assign):
		return p.parseCountedFor(forTok, declTok, counter)
	case p.peekTokenIs(Ident) && p.peekToken.Value.Text() == "of":
		statement := p.parseForOf(forTok, counter)
		if loop, ok := statement.(*ForStatement); ok && loop != nil {
			loop.Const = declTok.Kind == Const
		}
		return statement
	case p.peekTokenIs(Ident) && p.peekToken.Value.Text() == "in":
		p.addErrorAt(p.peekToken, "Kitwork chưa có for…in: duyệt khoá thì for (const k of Object.keys(o)), duyệt phần tử thì for (const x of arr)")
		return nil
	default:
		p.addError(fmt.Sprintf("Cú pháp for không hợp lệ sau '%s'. Dùng vòng đếm 'for (let %s = 0; %s < n; %s++)' hoặc duyệt 'for (const %s of arr)'.",
			counter.Value, counter.Value, counter.Value, counter.Value, counter.Value))
		return nil
	}
}

// parseCountedFor parses `for (<decl> i = <init> ; <cond> ; <update>)` and validates that <cond>
// compares the counter and <update> mutates it — so the loop is bounded by construction.
func (p *Parser) parseCountedFor(forTok, declTok Token, counter *Identifier) Statement {
	stmt := &ForRangeStatement{Token: forTok, Counter: counter.Value}

	// Init:  let i = <expr>
	p.nextToken() // cur: '='
	p.nextToken() // cur: init expression
	init := p.parseExpression(LOWEST)
	stmt.Init = &VarStatement{Token: declTok, Names: []*Identifier{counter}, Value: init, DestructMode: DestructNone}
	if !p.expectPeek(Semicolon) {
		return nil
	}

	// Cond:  i < n  (must compare the counter)
	p.nextToken()
	stmt.Cond = p.parseExpression(LOWEST)
	if !condRefsCounter(stmt.Cond, counter.Value) {
		p.addError(fmt.Sprintf("Điều kiện for phải so sánh biến đếm '%s' (ví dụ '%s < n', '%s >= 0'). Điều kiện tuỳ ý là 'while' trá hình và không được phép.", counter.Value, counter.Value, counter.Value))
		return nil
	}
	if !p.expectPeek(Semicolon) {
		return nil
	}

	// Update:  i++ / i-- / i += k / i -= k  (must mutate the counter)
	p.nextToken()
	stmt.Update = p.parseExpression(LOWEST)
	if !updateMutatesCounter(stmt.Update, counter.Value) {
		p.addError(fmt.Sprintf("Bước nhảy for phải cập nhật biến đếm '%s' (%s++, %s--, %s += n).", counter.Value, counter.Value, counter.Value, counter.Value))
		return nil
	}

	if !p.expectPeek(RightParen) {
		return nil
	}
	if !p.expectPeek(LeftBrace) {
		return nil
	}
	stmt.Body = p.parseLoopBody()
	return stmt
}

// parseForOf parses `for (<decl> x of <iterable>)` — bounded iteration over a collection.
func (p *Parser) parseForOf(forTok Token, item *Identifier) Statement {
	stmt := &ForStatement{Token: forTok, Item: item}
	p.nextToken() // cur: 'of'
	p.nextToken() // cur: iterable expression
	stmt.Iterable = p.parseExpression(LOWEST)
	if !p.expectPeek(RightParen) {
		return nil
	}
	if !p.expectPeek(LeftBrace) {
		return nil
	}
	stmt.Body = p.parseLoopBody()
	return stmt
}

// condRefsCounter reports whether a for-condition is a comparison against the loop counter.
func condRefsCounter(e Expression, name string) bool {
	inf, ok := e.(*InfixExpression)
	if !ok {
		return false
	}
	switch inf.Operator {
	case "<", "<=", ">", ">=", "!=":
		return isIdent(inf.Left, name) || isIdent(inf.Right, name)
	}
	return false
}

// updateMutatesCounter reports whether a for-update assigns/updates the loop counter (i++/i+=k/…).
func updateMutatesCounter(e Expression, name string) bool {
	as, ok := e.(*AssignmentExpression)
	return ok && isIdent(as.Name, name)
}

func isIdent(e Expression, name string) bool {
	id, ok := e.(*Identifier)
	return ok && id.Value == name
}

func (p *Parser) parseDeferStatement() Expression {
	stmt := &DeferStatement{Token: p.curToken}
	p.nextToken()
	stmt.Fn = p.parseExpression(LOWEST)
	return stmt
}

func (p *Parser) parseSpawnStatement() Expression {
	stmt := &SpawnStatement{Token: p.curToken}
	p.nextToken()
	stmt.Fn = p.parseExpression(LOWEST)
	return stmt
}

func (p *Parser) parseArrayLiteral() Expression {
	// the token is read before the elements are: in one composite literal Go may read p.curToken after
	// the call that moved it, and the literal's place was its closing bracket
	open := p.curToken
	return &ArrayLiteral{
		Token:    open,
		Elements: p.parseExpressionList(RightBracket),
	}
}

func (p *Parser) parseObjectLiteral() Expression {
	obj := &ObjectLiteral{Token: p.curToken, Entries: []ObjectEntry{}}
	for !p.peekTokenIs(RightBrace) {
		p.nextToken()

		if p.curTokenIs(Spread) {
			p.nextToken()
			val := p.parseExpression(LOWEST)
			if val == nil {
				p.addError("object spread requires an expression")
				return nil
			}
			obj.Entries = append(obj.Entries, ObjectEntry{
				Value:    val,
				IsSpread: true,
			})
		} else {
			var key Expression
			entryToken := p.curToken
			if word, ok := p.keywordSpelling(p.curToken); ok && p.peekTokenIs(Colon) {
				// Từ khoá trước ':' là tên thuộc tính, như JS: { function: … } chính là { "function": … },
				// cùng một nút Literal chuỗi nên ra cùng bytecode.
				keyToken := tokenWithValue(p.curToken, String, value.String(word))
				keyToken.Length = p.curToken.Length
				key = &Literal{Token: keyToken, Value: keyToken.Value}
			} else if p.curTokenIs(Ident) && languageWord(p.curToken.Value.Text()) && p.peekTokenIs(Colon) {
				// break/case/default/continue/typeof trước ':' cũng là tên thuộc tính. parseIdentifier từ chối
				// hay đọc khác chúng, nên dựng
				// thẳng đúng nút Identifier nó vẫn trả về trước đây: bytecode không đổi.
				key = &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
			} else if word, reserved := p.reservedWord(p.curToken); reserved && p.peekTokenIs(LeftParen) {
				// { function() { … } } là phương thức viết tắt của JS; Kitwork chưa có, trước đây nó lặng lẽ
				// thành một khoá rỗng giữ hàm.
				p.addErrorAt(p.curToken, fmt.Sprintf("Kitwork chưa có phương thức viết tắt trong object ({ %s() { … } }); viết { %s: (…) => … }", word, word))
				return nil
			} else if word, reserved := p.reservedWord(p.curToken); reserved &&
				(p.peekTokenIs(Comma) || p.peekTokenIs(RightBrace)) {
				// { function } là viết tắt của { function: function }: đọc một biến không thể có.
				p.addErrorAt(p.curToken, fmt.Sprintf("{ %s } là viết tắt của { %s: %s }, tức đọc biến tên %s; mà %s là từ khoá của ngôn ngữ, không làm tên biến được. Viết rõ { %s: giá_trị } hoặc { \"%s\": giá_trị }",
					word, word, word, word, word, word, word))
				return nil
			} else {
				key = p.parseExpression(LOWEST)
			}
			if key == nil {
				p.addError("object key requires an expression")
				return nil
			}
			if _, isFunction := key.(*FunctionLiteral); isFunction {
				// { function foo() { … } }, { (x) => 1 }: một hàm đứng chỗ khoá, không có tên khoá nào.
				p.addErrorAt(entryToken, "Kitwork chưa có phương thức viết tắt trong object; hàm phải đứng sau một khoá: { tên: (…) => … }")
				return nil
			}
			if problem := objectKeyProblem(key, p.peekTokenIs(Colon)); problem != "" {
				p.addErrorAt(entryToken, problem)
				return nil
			}
			if p.peekTokenIs(Colon) {
				p.nextToken()
				p.nextToken()
				val := p.parseExpression(LOWEST)
				if val == nil {
					p.addError("object value requires an expression")
					return nil
				}
				obj.Entries = append(obj.Entries, ObjectEntry{
					Key:   key,
					Value: val,
				})
			} else {
				// Shorthand: { name } -> { name: name }
				obj.Entries = append(obj.Entries, ObjectEntry{
					Key:   key,
					Value: key,
				})
			}
		}

		if !p.peekTokenIs(RightBrace) && !p.expectPeek(Comma) {
			return nil
		}
	}
	if !p.expectPeek(RightBrace) {
		return nil
	}
	return obj
}

// objectKeyProblem holds an object's key to what JavaScript reads there (18/11/2026, docs/language.md mục 3.3): a
// name, a string, a number, or one expression in brackets, { [k]: v }; a shorthand { x } is a name alone. Any
// expression was read as a key and computed ({ 1 + 1: v }, { o.b: v }, { f(): v }, the shorthand { k() } called k
// twice), and { [a, b]: v } took the text of the array, "a,b": each is JavaScript's SyntaxError.
func objectKeyProblem(key Expression, withValue bool) string {
	if !withValue {
		if _, isName := key.(*Identifier); isName {
			return ""
		}
		// { a = 1 } is a pattern's default read first as an object, JavaScript's cover grammar: ({ a = 1 }) => a. Left
		// an object, the resolver says so (scope.go).
		if assignment, ok := key.(*AssignmentExpression); ok && assignment.Update == NotAnUpdate {
			if _, isName := assignment.Name.(*Identifier); isName {
				return ""
			}
		}
		return "viết tắt { x } trong object chỉ dùng với một tên (là { x: x }); thứ khác viết { khoá: giá_trị }"
	}
	switch k := key.(type) {
	case *Identifier, *Literal:
		return ""
	case *ArrayLiteral:
		if computedKey(k) != nil {
			return ""
		}
		return "khoá tính của object là đúng một biểu thức trong ngoặc vuông: { [k]: v }"
	}
	return "khoá của object là một tên, một chuỗi, một số hay một biểu thức trong ngoặc vuông: { [a + b]: v }"
}

func (p *Parser) parseCallExpression(left Expression) Expression {
	if method := memberCalled(left); method != nil {
		// (o.m)(…) and o["m"](…) call m on o, as o.m(…) does: JavaScript keeps o as the call's receiver, so a method of
		// a value runs on it (18/11/2026: they read the method, then called it apart from o, which only a method bound
		// at its read let run)
		call := &MethodCallExpression{Token: method.Token, Object: method.Object, Method: method.Method, Computed: method.Computed,
			Arguments: p.parseExpressionList(RightParen)}
		p.link(call, left, false)
		return call
	}
	exp := &CallExpression{Token: p.curToken, Function: left}
	exp.Arguments = p.parseExpressionList(RightParen)
	if exp.Arguments != nil {
		exp.Close = p.curToken.Position
	}
	p.link(exp, left, false)
	return exp
}

// memberCalled is the object and the method of a callee that names a member, (o.m) or o["m"] with a key written as a
// string that is no index; nil for any other callee, o[k] with a key from a name among them.
func memberCalled(callee Expression) *MethodCallExpression {
	switch member := callee.(type) {
	case *MemberExpression:
		if !member.Optional {
			return &MethodCallExpression{Token: member.Token, Object: member.Object, Method: member.Property}
		}
	case *IndexExpression:
		key, ok := member.Index.(*Literal)
		if member.Optional || !ok || !key.Value.IsString() {
			return nil
		}
		name := key.Value.Text()
		if _, err := strconv.Atoi(name); err == nil {
			return nil // an index: the item of an array, called as it is
		}
		return &MethodCallExpression{Token: member.Token, Object: member.Left, Computed: true,
			Method: &Identifier{Token: tokenWithValue(key.Token, Ident, value.String(name)), Value: name}}
	}
	return nil
}

// sourceBetween is the source from start up to end (a position, end excluded) as one line: a run of
// spaces and line breaks is one space, and none next to . , ) ] or after ( [ (the line breaks of a
// chain written one call a line go away). "" when the parser has no source.
func (p *Parser) sourceBetween(start, end int32) string {
	if p.text == nil || start < 0 || end > int32(len(p.text)) || start >= end {
		return ""
	}
	return oneLine(string(p.text[start:end]))
}

// oneLine is code as one line, for a message that quotes it as the fix to copy: a string is copied as
// it is written, its spaces and escapes included, and a // comment is left out (kept on the line it
// would hide the rest of it), so the copy means what the code means.
func oneLine(text string) string {
	var out strings.Builder
	space := false
	runes := []rune(strings.TrimSpace(text))
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if char == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			space = true
			continue
		}
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			space = true
			continue
		}
		if space {
			written := out.String()
			last := byte(0)
			if len(written) > 0 {
				last = written[len(written)-1]
			}
			if !strings.ContainsRune(".,)]", char) && last != '(' && last != '[' && last != '.' {
				out.WriteByte(' ')
			}
			space = false
		}
		if char == '"' || char == '\'' || char == '`' {
			end := i + 1
			for end < len(runes) && runes[end] != char {
				if runes[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(runes) {
				end = len(runes) - 1
			}
			out.WriteString(string(runes[i : end+1]))
			i = end
			continue
		}
		out.WriteRune(char)
	}
	return out.String()
}

// removedSafeTextLimit bounds the receiver quoted in the error of x.safe(), in characters.
const removedSafeTextLimit = 100

// removedSafeMessage is the error of x.safe(), gone since 14/10/2026: it quotes the receiver in the
// new spelling, so the fix is a copy. A receiver longer than removedSafeTextLimit keeps its head (cut
// back to a space) and its tail (from a dot on) with … between.
func removedSafeMessage(receiver string) string {
	if receiver == "" {
		receiver = "…"
	}
	if runes := []rune(receiver); len(runes) > removedSafeTextLimit {
		head := string(runes[:60])
		if cut := strings.LastIndex(head, " "); cut > 0 {
			head = head[:cut+1]
		}
		tail := string(runes[len(runes)-40:])
		if cut := strings.Index(tail, "."); cut >= 0 && cut < len(tail)-1 {
			tail = tail[cut+1:]
		}
		receiver = head + "…" + tail
	}
	if strings.HasPrefix(receiver, "{") {
		receiver = "(" + receiver + ")"
	}
	return ".safe() đã bỏ (14/10/2026): viết safe(() => " + receiver + "), nhận { value, error }"
}

func (p *Parser) parseIndexExpression(left Expression) Expression {
	exp := &IndexExpression{Token: p.curToken, Left: left}
	p.nextToken()
	exp.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(RightBracket) {
		return nil
	}
	p.link(exp, left, false)
	return exp
}

func (p *Parser) parseGroupedExpression() Expression {
	// Nếu là () => ...
	if p.peekTokenIs(RightParen) {
		open := p.curToken
		p.nextToken() // Sang dấu )
		if !p.peekTokenIs(FatArrow) {
			// () holds no expression: it is only the parameters of () => …, as in JavaScript (18/11/2026: const r = ()
			// made bytecode the verifier refused)
			p.addErrorAt(open, "() rỗng chỉ là tham số của hàm mũi tên () => …; ngoặc tròn khác bọc một biểu thức")
			return nil
		}
		return &ParameterList{Token: p.curToken, Parameters: []*Identifier{}}
	}

	exps := p.parseExpressionList(RightParen)

	// Nếu tiếp sau là =>, biến list này thành ParameterList
	if p.peekTokenIs(FatArrow) {
		lowered, fine := p.functionParameters(exps)
		if !fine {
			return &ParameterList{Token: p.curToken}
		}
		return &ParameterList{Token: p.curToken, Parameters: lowered.names, Prelude: lowered.prelude, Rest: lowered.rest,
			Initializers: lowered.initializers, Defaulted: lowered.defaulted}
	}

	if len(exps) == 0 {
		return nil
	}
	for _, e := range exps {
		if spread, ok := e.(*SpreadExpression); ok {
			p.addErrorAt(spread.Token, "... chỉ trải trong một mảng, đối số của lời gọi hay một object ([...a], f(...args), { ...o }), không đứng trong ngoặc đơn")
			return nil
		}
	}
	if len(exps) > 1 && exps[1] != nil {
		// (a, b) not followed by => is JavaScript's comma operator, which Kitwork does not have: it kept
		// a and dropped b without a word (14/10/2026)
		at := Token{Position: startPosition(exps[1]), Source: getNodeSource(exps[1])}
		p.addErrorAt(at, "Kitwork không có toán tử dấu phẩy: (a, b) chỉ là tham số của hàm mũi tên (a, b) => …; mỗi việc viết thành một câu lệnh")
		return nil
	}
	if exps[0] != nil {
		if p.grouped == nil {
			p.grouped = map[Expression]bool{}
		}
		p.grouped[exps[0]] = true
	}
	return exps[0]
}

// patternParameterError reports a destructuring parameter, ({ id }) => … or ([a, b]) => …, which
// Kitwork does not have yet; false when e is something else.
func (p *Parser) patternParameterError(e Expression) bool {
	at := Token{Position: startPosition(e), Source: getNodeSource(e)}
	kind := ""
	switch node := e.(type) {
	case *SpreadExpression:
		p.addErrorAt(node.Token, "Kitwork chưa có tham số phần còn lại (...args); nhận một mảng: (args) => …, gọi f(...list)")
		return true
	case *ObjectLiteral:
		kind = "object"
	case *ArrayLiteral:
		kind = "mảng"
		if len(node.Elements) > 0 && node.Elements[0] != nil {
			// an array literal's own place is its closing bracket; its first element is nearer the start
			at.Position = startPosition(node.Elements[0])
		}
	case *AssignmentExpression:
		// ({ a } = {}) => …, ([a] = []) => …: the left side was read as a pattern already.
		if pattern, ok := node.Name.(*BindingPattern); ok && node.Token.Kind == Assign {
			kind = "object"
			if pattern.Array {
				kind = "mảng"
			}
		} else if node.Token.Kind == Assign {
			p.addErrorAt(at, fmt.Sprintf("Kitwork chưa có giá trị mặc định cho tham số (%s); viết trong thân: const x = tham_số ?? mặc_định", node.String()))
			return true
		}
	}
	if kind == "" {
		return false
	}
	p.addErrorAt(at, fmt.Sprintf("Kitwork chưa có tách %s trong tham số; nhận cả giá trị rồi tách trong thân: (o) => { const { a, b } = o; … }", kind))
	return true
}

func (p *Parser) parseArrowFunction(left Expression) Expression {
	tok := p.curToken // =>
	p.nextToken()

	var params []*Identifier
	var prelude []Statement
	var initializers []int
	rest, defaulted := false, 0
	if pl, ok := left.(*ParameterList); ok {
		params = pl.Parameters
		prelude = pl.Prelude
		rest = pl.Rest
		initializers, defaulted = pl.Initializers, pl.Defaulted
	} else if id, reserved := p.parameterName(left); reserved {
		return nil
	} else if id != nil {
		params = []*Identifier{id}
	} else {
		p.addError(arrowParameterProblem(left))
		return nil
	}

	// Xử lý block { } hoặc single expression
	if p.curTokenIs(LeftBrace) {
		body := p.parseFunctionBlock()
		if body != nil && len(prelude) > 0 {
			body.Statements = append(append([]Statement{}, prelude...), body.Statements...)
		}
		return &FunctionLiteral{
			Token:        tok,
			Parameters:   params,
			Body:         body,
			Rest:         rest,
			Initializers: initializers,
			Defaulted:    defaulted,
		}
	}

	// Single expression body: a => a * 2 -> { return a * 2 }
	body := &BlockStatement{
		Statements: append(append([]Statement{}, prelude...), &ReturnStatement{
			ReturnValue: p.parseExpression(LOWEST),
		}),
	}

	return &FunctionLiteral{
		Token:        tok,
		Parameters:   params,
		Body:         body,
		Rest:         rest,
		Initializers: initializers,
		Defaulted:    defaulted,
	}
}

/* =============================================================================
   3. HELPERS
   ============================================================================= */

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
	// a literal JavaScript refuses (a bad escape, 010, a string a line break ends) comes from the lexer
	// with a failure for a value: the compile error, at the literal, whatever reads the token next
	if p.peekToken.Value.IsInvalid() {
		p.addErrorAt(p.peekToken, p.peekToken.Value.Message())
	}
}

func (p *Parser) curTokenIs(k Kind) bool  { return p.curToken.Kind == k }
func (p *Parser) peekTokenIs(k Kind) bool { return p.peekToken.Kind == k }

func (p *Parser) expectPeek(k Kind) bool {
	if p.peekTokenIs(k) {
		p.nextToken()
		return true
	}
	p.addErrorAt(p.peekToken, fmt.Sprintf("cần %s ở đây, gặp %q", k, p.peekToken.String()))
	return false
}

func (p *Parser) parseExpressionList(end Kind) []Expression {
	list := []Expression{}
	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}
	p.nextToken()
	list = append(list, p.listItem())
	for p.peekTokenIs(Comma) {
		p.nextToken()
		if p.peekTokenIs(end) {
			p.addError(fmt.Sprintf("trailing comma is not supported before %s", end))
			return nil
		}
		p.nextToken()
		list = append(list, p.listItem())
	}
	if !p.expectPeek(end) {
		return nil
	}
	return list
}

// listItem reads an item of a list, an expression or a spread of one: [...a, x] and f(...args) spread (15/11/2026,
// round 5 of the 13/11 review; runtime spread.go). What reads the list as something else says where a spread may
// not stand: a group in parentheses, a function's parameters (patternParameterError).
func (p *Parser) listItem() Expression {
	if !p.curTokenIs(Spread) {
		return p.parseExpression(LOWEST)
	}
	tok := p.curToken
	p.nextToken()
	spread := p.parseExpression(LOWEST)
	if spread == nil {
		return nil
	}
	return &SpreadExpression{Token: tok, Value: spread}
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Kind]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Kind]; ok {
		return p
	}
	return LOWEST
}

func registerPrefix(k Kind, fn prefixParseFn) { prefixParseFns[k] = fn }
func registerInfix(k Kind, fn infixParseFn)   { infixParseFns[k] = fn }

// addError reports msg at the current token, file:line:column first, as addErrorAt does (14/10/2026;
// it gave a byte offset).
func (p *Parser) addError(msg string) {
	p.addErrorAt(p.curToken, msg)
}
func (p *Parser) Errors() []string {
	return p.errors
}

// addErrorAt báo lỗi tại tok dạng file:dòng:cột — cột đếm ký tự từ 1, như chẩn đoán lúc chạy của compiler.
//
// Only the first error is shown (parseProgramSource): a later one, most often what the first left
// behind, is kept without its place, since finding a place reads the source from its start and a file
// that fails at every level (code nested past nestingLimit) would read it once per level.
func (p *Parser) addErrorAt(tok Token, msg string) {
	if len(p.errors) > 0 {
		p.errors = append(p.errors, msg)
		return
	}
	p.errors = append(p.errors, p.location(tok)+": "+msg)
}

// location trả về chỗ tok bắt đầu dạng file:dòng:cột (chỉ dòng:cột khi nguồn không có tên). Cột đếm
// theo ký tự như trình soạn thảo hiện, không theo byte (14/10/2026): chữ có dấu trước chỗ lỗi không làm
// cột lệch. Thiếu nguồn thì lùi về vị trí byte.
func (p *Parser) location(tok Token) string {
	return textLocation(p.text, tok)
}

// textLocation is where tok starts in text, the source it was read from, as location writes it; the
// bundler writes an import's place with it too.
func textLocation(text []byte, tok Token) string {
	pos := int(tok.Position)
	if text == nil || pos < 0 || pos > len(text) {
		return fmt.Sprintf("pos %d", pos)
	}
	line, lineStart := 1, 0
	for index := 0; index < pos; index++ {
		if text[index] == '\n' {
			line++
			lineStart = index + 1
		}
	}
	where := fmt.Sprintf("%d:%d", line, utf8.RuneCount(text[lineStart:pos])+1)
	if tok.Source != "" {
		where = tok.Source + ":" + where
	}
	return where
}

// lineOf is the line tok starts on, counted from 1; 0 when the parser has no source text.
func (p *Parser) lineOf(tok Token) int {
	pos := int(tok.Position)
	if p.text == nil || pos < 0 || pos > len(p.text) {
		return 0
	}
	return bytes.Count(p.text[:pos], []byte{'\n'}) + 1
}

/* =============================================================================
   4. TỪ KHOÁ: làm tên thuộc tính được, làm tên biến thì không
   Như JS (IdentifierName): sau dấu chấm và trước ':' trong object, mọi từ khoá là tên thuộc tính —
   { function: 1 } là { "function": 1 }, o.function là o["function"], cùng bytecode. Chỗ đặt tên biến
   (const/let, tham số, tên hàm, biến đếm for, tên import, viết tắt { x }) thì từ khoá là lỗi biên dịch.
   ============================================================================= */

// statementWords là các chữ parser cho nghĩa riêng ở đầu câu lệnh (break;, continue;, case …:, default:).
// Lexer để chúng là identifier nên vẫn làm tên thuộc tính được, nhưng một biến mang tên đó không bao giờ
// dùng được, nên chúng cũng không làm tên biến.
var statementWords = map[string]bool{"break": true, "case": true, "default": true, "continue": true}

// operatorWords là các chữ parser đọc thành toán tử đứng trước toán hạng: typeof x (14/10/2026). Như
// statementWords, chúng làm tên thuộc tính được ({ typeof: 1 }, o.typeof), tên biến thì không.
var operatorWords = map[string]bool{"typeof": true, "delete": true}

// languageWord: một chữ lexer để là identifier mà parser cho nghĩa riêng, nên không làm tên biến.
func languageWord(word string) bool {
	return statementWords[word] || operatorWords[word] || notYetWords[word] != ""
}

// notYetWords are JavaScript words Kitwork does not have yet (14/10/2026): each was a name nothing
// declares, so delete o.a left o as it was and the rest of the line was another statement. Each is a
// compile error saying how to write it. They stay property names: o.delete("a"), { in: 1 }, and are no
// variable names. continue and typeof were here until they were made (14/10/2026, the same day).
var notYetWords = map[string]string{
	"instanceof": "Kitwork chưa có instanceof (không có lớp): một mảng thì Array.isArray(x)",
	"in":         "Kitwork chưa có toán tử in: hỏi một khoá thì Object.hasOwn(o, \"khoá\")",
}

// keywordSpelling trả về chữ của một token từ khoá trong nguồn ("function", "true", "undefined",
// "while"…). Token true/false/null/undefined không giữ chữ nên đọc lại từ input. ok là false với mọi
// token không phải từ khoá của lexer (kể cả identifier).
func (p *Parser) keywordSpelling(tok Token) (string, bool) {
	if tok.Kind == Ident || tok.Length <= 0 {
		return "", false
	}
	start := int(tok.Position - p.l.base)
	end := start + int(tok.Length)
	if start < 0 || end > len(p.l.input) {
		return "", false
	}
	word := string(p.l.input[start:end])
	if kind, ok := Keywords[word]; !ok || kind != tok.Kind {
		return "", false
	}
	return word, true
}

func (p *Parser) isKeyword(tok Token) bool {
	_, ok := p.keywordSpelling(tok)
	return ok
}

// wordOf trả về chữ của một identifier hoặc từ khoá.
func (p *Parser) wordOf(tok Token) string {
	if word, ok := p.keywordSpelling(tok); ok {
		return word
	}
	return tok.Value.Text()
}

// reservedWord báo tok có phải chữ không làm tên biến được không: từ khoá của lexer, hoặc statementWords.
func (p *Parser) reservedWord(tok Token) (string, bool) {
	if tok.Kind == Ident {
		word := tok.Value.Text()
		return word, languageWord(word)
	}
	return p.keywordSpelling(tok)
}

func (p *Parser) reservedNameError(tok Token, word string) {
	p.addErrorAt(tok, fmt.Sprintf("%s là từ khoá của ngôn ngữ, không làm tên biến được; làm khoá object thì viết thẳng { %s: … } hoặc { \"%s\": … }, đọc thì o.%s",
		word, word, word, word))
}

// reservedImportError là lỗi từ khoá cho một tên import: cách viết đúng là đặt tên khác, không phải khoá object.
func (p *Parser) reservedImportError(tok Token, word, rename string) {
	p.addErrorAt(tok, fmt.Sprintf("%s là từ khoá của ngôn ngữ, không làm tên biến được; đặt tên khác: %s", word, rename))
}

// bindingName kiểm tok (đang là curToken) làm tên biến được: từ khoá thì báo lỗi từ khoá, thứ khác không
// phải identifier thì báo otherwise.
func (p *Parser) bindingName(tok Token, otherwise string) bool {
	if word, reserved := p.reservedWord(tok); reserved {
		p.reservedNameError(tok, word)
		return false
	}
	if tok.Kind != Ident {
		p.addErrorAt(tok, otherwise)
		return false
	}
	return true
}

// expectBindingName như expectPeek(Ident) cho một tên biến, nhưng từ khoá được báo bằng lỗi từ khoá.
func (p *Parser) expectBindingName() bool {
	if word, reserved := p.reservedWord(p.peekToken); reserved {
		p.reservedNameError(p.peekToken, word)
		return false
	}
	return p.expectPeek(Ident)
}

// parameterName trả về identifier mà một tham số đã parse đặt tên. Từ khoá thì báo lỗi, reserved = true
// (true/false/null/undefined parse thành Literal; break/case/default thì parseIdentifier đã báo); biểu
// thức khác không phải tên thì trả về nil, false để nơi gọi xử lý như trước.
func (p *Parser) parameterName(e Expression) (*Identifier, bool) {
	switch node := e.(type) {
	case *Identifier:
		return node, false
	case *Literal:
		if word, ok := p.keywordSpelling(node.Token); ok {
			p.reservedNameError(node.Token, word)
			return nil, true
		}
	}
	return nil, false
}

// functionParameters đổi danh sách tham số của `function (…)` thành identifier. Từ khoá thì false;
// biểu thức khác để nil như trước, compiler sẽ từ chối.
// loweredParameters is what a function's parameter list lowers to: its parameters, the statements its body starts
// with and how many of them each parameter owns (FunctionLiteral.Initializers), how many parameters its length
// leaves out (FunctionLiteral.Defaulted), and whether the last is a rest.
type loweredParameters struct {
	names        []*Identifier
	prelude      []Statement
	initializers []int
	defaulted    int
	rest         bool
}

func (p *Parser) functionParameters(exps []Expression) (loweredParameters, bool) {
	lowered := loweredParameters{names: make([]*Identifier, len(exps))}
	firstDefault := -1
	for index, e := range exps {
		if spread, ok := e.(*SpreadExpression); ok {
			id, fine := p.restParameter(spread, index == len(exps)-1)
			if !fine {
				return loweredParameters{}, false
			}
			lowered.names[index], lowered.rest = id, true
			continue
		}
		parameter, statements, defaulted, fine := p.parameterOf(e)
		if !fine {
			return loweredParameters{}, false
		}
		if defaulted && firstDefault < 0 {
			firstDefault = index
		}
		lowered.names[index] = parameter
		lowered.prelude = append(lowered.prelude, statements...)
		lowered.initializers = append(lowered.initializers, len(statements))
	}
	if len(lowered.prelude) == 0 {
		lowered.initializers = nil
	}
	if firstDefault >= 0 {
		lowered.defaulted = len(exps) - firstDefault
		if lowered.rest {
			lowered.defaulted--
		}
	}
	return lowered, true
}

// parameterOf reads one parameter of an arrow or a function: a name; a name with its default, (x = 1, y = x * 2) => …
// (15/11/2026, round 5 of the 13/11 review); a destructured object, with a default or not. It gives the parameter and
// the statements the body starts with, in order: a default is if (x === undefined) { x = d; }, as JavaScript uses
// it for undefined only, read where the parameters before it are bound; a destructuring reads its names off the
// parameter. false, with the error said, for anything else.
func (p *Parser) parameterOf(e Expression) (*Identifier, []Statement, bool, bool) {
	var defaultValue Expression
	if assign, ok := e.(*AssignmentExpression); ok && assign.Token.Kind == Assign && assign.Update == NotAnUpdate {
		defaultValue = assign.Value
		e = assign.Name
	}
	var statements []Statement
	var parameter *Identifier
	switch node := e.(type) {
	case *ObjectLiteral, *ArrayLiteral:
		pattern, ok := p.assignmentPattern(node)
		if !ok || pattern == nil {
			return nil, nil, false, false
		}
		var destructure Statement
		parameter, destructure = patternParameter(pattern, Let)
		statements = append(statements, destructure)
	case *BindingPattern:
		var destructure Statement
		parameter, destructure = patternParameter(node, Let)
		statements = append(statements, destructure)
	default:
		id, reserved := p.parameterName(e)
		if reserved {
			return nil, nil, false, false
		}
		if id == nil {
			if !p.patternParameterError(e) {
				p.addError(arrowParameterProblem(e))
			}
			return nil, nil, false, false
		}
		parameter = id
	}
	if defaultValue != nil {
		nameFunction(defaultValue, parameter.Value)
		statements = append([]Statement{defaultStatement(parameter, defaultValue)}, statements...)
	}
	return parameter, statements, defaultValue != nil, true
}

// patternParameter lowers a destructured parameter, ({ request, response }) => … (19/10/2026), ([x, y]) => …, or the
// item of for (const { a } of list) (15/11/2026), the way JavaScript binds it: one variable named by the pattern as
// written (patternParameterName), a name no source can declare, so it never meets one of the body's, and the
// declaration of kind reading the pattern's names off it, which the body starts with. A host that passes arguments
// by name (Context.arguments) reads the parameter as the whole value, and reads which names were taken
// (DestructuredNames). An array pattern reads its items as an object pattern reads its keys, its rest as a spread.
func patternParameter(pattern *BindingPattern, kind Kind) (*Identifier, Statement) {
	name := patternParameterName(pattern)
	at := tokenWithValue(pattern.Token, Ident, value.String(name))
	mode, keyword := DestructObject, "let"
	if pattern.Array {
		mode = DestructArray
	}
	if kind == Const {
		keyword = "const"
	}
	return &Identifier{Token: at, Value: name}, &VarStatement{
		Token:        tokenWithValue(at, kind, value.String(keyword)),
		Names:        pattern.Names(),
		DestructMode: mode,
		Pattern:      pattern,
		Value:        &Identifier{Token: at, Value: name},
	}
}

// defaultStatement is a parameter's default as its body starts: if (x === undefined) { x = d; }.
func defaultStatement(parameter *Identifier, defaultValue Expression) Statement {
	at := parameter.Token
	read := func() *Identifier { return &Identifier{Token: at, Value: parameter.Value} }
	return &ExpressionStatement{Token: at, Expression: &IfExpression{
		Token: tokenWithValue(at, If, value.String("if")),
		Condition: &InfixExpression{Token: tokenWithValue(at, Equal, value.String("===")), Left: read(), Operator: "===",
			Right: &Literal{Token: tokenWithValue(at, Null, value.Undefined()), Value: value.Undefined()}},
		Consequence: &BlockStatement{Token: at, Statements: []Statement{&ExpressionStatement{Token: at,
			Expression: &AssignmentExpression{Token: tokenWithValue(at, Assign, value.String("=")), Name: read(), Value: defaultValue, Default: true}}}},
	}}
}

// restParameter reads a rest parameter, (a, ...rest) => … (15/11/2026, round 5 of the 13/11 review): a name, the
// last parameter, as JavaScript has it; false, with the error said, for one that is not last or not a name.
func (p *Parser) restParameter(spread *SpreadExpression, last bool) (*Identifier, bool) {
	if !last {
		p.addErrorAt(spread.Token, "phần còn lại (...tên) là tham số cuối, như JavaScript: (a, ...rest) => …")
		return nil, false
	}
	if assign, defaulted := spread.Value.(*AssignmentExpression); defaulted && assign.Token.Kind == Assign {
		p.addErrorAt(spread.Token, "phần còn lại (...tên) không có giá trị mặc định: không có đối số thì nó là mảng rỗng")
		return nil, false
	}
	id, reserved := p.parameterName(spread.Value)
	if reserved {
		return nil, false
	}
	if id == nil {
		p.addErrorAt(spread.Token, "Kitwork chưa có tách trong phần còn lại (...[a, b]); nhận ...rest rồi tách trong thân")
		return nil, false
	}
	return id, true
}

func (p *Parser) parseFunctionStatement() Statement {
	tok := p.curToken

	if !p.expectBindingName() {
		return nil
	}
	name := &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}

	if !p.expectPeek(LeftParen) {
		return nil
	}

	lowered, ok := p.functionParameters(p.parseExpressionList(RightParen))
	if !ok {
		return nil
	}

	if !p.expectPeek(LeftBrace) {
		return nil
	}

	body := p.parseFunctionBlock()
	if body != nil && len(lowered.prelude) > 0 {
		body.Statements = append(append([]Statement{}, lowered.prelude...), body.Statements...)
	}

	funcLit := &FunctionLiteral{
		Token:        tok,
		Parameters:   lowered.names,
		Body:         body,
		Rest:         lowered.rest,
		Initializers: lowered.initializers,
		Defaulted:    lowered.defaulted,
	}

	return &VarStatement{
		Token: Token{
			Kind:  Const,
			Value: value.String("const"),
		},
		Names:        []*Identifier{name},
		Value:        funcLit,
		DestructMode: DestructNone,
		Function:     true,
	}
}

func (p *Parser) parseFunctionExpression() Expression {
	tok := p.curToken // function

	if word, reserved := p.reservedWord(p.peekToken); reserved {
		p.reservedNameError(p.peekToken, word)
		return nil
	}
	var own *Identifier
	if p.peekTokenIs(Ident) {
		p.nextToken()
		own = &Identifier{Token: p.curToken, Value: p.curToken.Value.Text()}
	}

	if !p.expectPeek(LeftParen) {
		return nil
	}

	lowered, ok := p.functionParameters(p.parseExpressionList(RightParen))
	if !ok {
		return nil
	}

	if !p.expectPeek(LeftBrace) {
		return nil
	}

	body := p.parseFunctionBlock()
	if body != nil && len(lowered.prelude) > 0 {
		body.Statements = append(append([]Statement{}, lowered.prelude...), body.Statements...)
	}

	literal := &FunctionLiteral{
		Token:        tok,
		Parameters:   lowered.names,
		Body:         body,
		Rest:         lowered.rest,
		Initializers: lowered.initializers,
		Defaulted:    lowered.defaulted,
	}
	if own == nil {
		return literal
	}
	return namedFunction(own, literal)
}

// namedFunction is a named function expression, function g() { … }: its name is the function itself inside its body,
// and the function's name (f.name), as JavaScript binds it in a scope of its own between the function and the code
// around it; it lowers to (() => { const g = function () { … }; return g })() (18/11/2026: the name was skipped, so
// the body could not call the function by it and f.name was the name of the const that held it).
func namedFunction(own *Identifier, literal *FunctionLiteral) Expression {
	literal.DebugName = own.Value
	at := own.Token
	read := &Identifier{Token: at, Value: own.Value}
	return &CallExpression{Token: tokenWithValue(at, LeftParen, value.String("(")), Function: &FunctionLiteral{
		Token:      literal.Token,
		Parameters: []*Identifier{},
		Body: &BlockStatement{Token: tokenWithValue(at, LeftBrace, value.String("{")), Statements: []Statement{
			&VarStatement{Token: constToken(at), Names: []*Identifier{own}, Value: literal, DestructMode: DestructNone},
			&ReturnStatement{Token: tokenWithValue(at, Return, value.String("return")), ReturnValue: read},
		}},
	}}
}

// patternParameterName spells a pattern as the name of the parameter it lowers to: its keys and
// targets, nested ones in place, without the defaults (their text is not the parameter's), a key that
// is not a plain word in quotes, so DestructuredEntries reads it back whole.
func patternParameterName(bp *BindingPattern) string {
	open, close := "{ ", " }"
	if bp.Array {
		open, close = "[ ", " ]"
	}
	parts := make([]string, 0, len(bp.Elements))
	for _, element := range bp.Elements {
		target := ""
		if element.Pattern != nil {
			target = patternParameterName(element.Pattern)
		} else if element.Name != nil {
			target = element.Name.Value
		}
		if element.Rest {
			parts = append(parts, "..."+target)
			continue
		}
		if bp.Array {
			parts = append(parts, target)
			continue
		}
		key := element.Key
		if !isPlainWord(key) {
			key = strconv.Quote(key)
		}
		if element.Name != nil && element.Name.Value == element.Key {
			parts = append(parts, key)
		} else {
			parts = append(parts, key+": "+target)
		}
	}
	return open + strings.Join(parts, ", ") + close
}

func isPlainWord(s string) bool {
	if s == "" {
		return false
	}
	for i, char := range s {
		if char == '_' || char == '$' || unicode.IsLetter(char) || (i > 0 && unicode.IsDigit(char)) {
			continue
		}
		return false
	}
	return true
}

// DestructuredEntry is one top-level name of a destructured parameter: the key it reads, and whether
// it is taken apart further ({ request: { body } }).
type DestructuredEntry struct {
	Key    string
	Nested bool
	Target string // what the key is taken into: its name, or the nested pattern ("{ body }")
}

// DestructuredEntries reads back the pattern a destructured parameter's name spells (objectParameter):
// its top-level keys in order; nil when the name is a plain one.
func DestructuredEntries(parameter string) []DestructuredEntry {
	if !strings.HasPrefix(parameter, "{ ") || !strings.HasSuffix(parameter, " }") {
		return nil
	}
	inner := parameter[2 : len(parameter)-2]
	var entries []DestructuredEntry
	depth, start := 0, 0
	flush := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		key, rest := part, ""
		if strings.HasPrefix(part, "\"") {
			if quoted, err := strconv.QuotedPrefix(part); err == nil {
				key, rest = quoted, strings.TrimSpace(part[len(quoted):])
				key, _ = strconv.Unquote(key)
			}
		} else if cut := strings.Index(part, ":"); cut >= 0 {
			key, rest = strings.TrimSpace(part[:cut]), strings.TrimSpace(part[cut:])
		}
		target := ""
		if strings.HasPrefix(rest, ":") {
			target = strings.TrimSpace(rest[1:])
		}
		nested := strings.HasPrefix(target, "{") || strings.HasPrefix(target, "[")
		if target == "" {
			target = key
		}
		entries = append(entries, DestructuredEntry{Key: key, Nested: nested, Target: target})
	}
	var quote rune
	for i, char := range inner {
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '"', '\'', '`':
			quote = char
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				flush(inner[start:i])
				start = i + 1
			}
		}
	}
	flush(inner[start:])
	return entries
}

// DestructuredNames is the keys of DestructuredEntries.
func DestructuredNames(parameter string) []string {
	entries := DestructuredEntries(parameter)
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Key
	}
	return names
}
