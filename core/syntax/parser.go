package syntax

import (
	"fmt"
	"strconv"
)

// Parse đọc source của file thành một Program, hay lỗi đầu tiên ở chỗ của nó. Luật của tập con (CLAUDE.md): chỉ hàm
// mũi tên; không while, do, try, var, class, function; mảng không nhận dấu phẩy cuối (object thì nhận). Thứ lát này
// chưa có (vòng lặp, giải cấu trúc, spread, ?., new…) là lỗi nói rằng nó tới sau.
func Parse(file, source string) (*Program, error) {
	l := newLexer(file, source)
	var tokens []Token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
		if t.Kind == EOF {
			break
		}
	}
	p := &parser{file: file, tokens: tokens}
	program, err := p.program()
	if err != nil {
		return nil, err
	}
	return program, nil
}

type parser struct {
	file   string
	tokens []Token
	at     int
	loops  int // số vòng lặp quanh chỗ đang đọc, trong hàm đang đọc: break, continue chỉ viết được khi > 0
}

// failure là lỗi parser ném lên bằng panic, bắt ở program: mỗi hàm đọc không phải trả lỗi qua từng tầng.
type failure struct{ err *Error }

func (p *parser) fail(at Pos, format string, arguments ...any) {
	panic(failure{&Error{File: p.file, Pos: at, Message: fmt.Sprintf(format, arguments...)}})
}

func (p *parser) peek() Token        { return p.tokens[p.at] }
func (p *parser) peekAt(n int) Token { return p.tokens[min(p.at+n, len(p.tokens)-1)] }
func (p *parser) take() Token        { t := p.tokens[p.at]; p.at = min(p.at+1, len(p.tokens)-1); return t }
func (p *parser) is(text string) bool {
	t := p.peek()
	return (t.Kind == Punct || t.Kind == Name) && t.Text == text
}

func (p *parser) accept(text string) bool {
	if p.is(text) {
		p.take()
		return true
	}
	return false
}

func (p *parser) expect(text string) Token {
	if !p.is(text) {
		p.fail(p.peek().Pos, "expected %s, found %s", text, describe(p.peek()))
	}
	return p.take()
}

// describe là một token cho lời báo.
func describe(t Token) string {
	switch t.Kind {
	case EOF:
		return "the end of the file"
	case String:
		return "a string"
	case Number:
		return t.Text
	case TemplateFull, TemplateHead:
		return "a template"
	}
	return strconv.Quote(t.Text)
}

func (p *parser) program() (program *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			err = f.err
		}
	}()
	program = &Program{File: p.file}
	for p.peek().Kind != EOF {
		if p.is("import") {
			program.Imports = append(program.Imports, p.importDeclaration())
			continue
		}
		if p.is("export") {
			t := p.take()
			if !p.is("const") {
				p.fail(t.Pos, "export is written before a const: export const x = …")
			}
			for _, s := range p.declarations() {
				s.(*Declaration).Export = true
				program.Body = append(program.Body, s)
			}
			continue
		}
		program.Body = append(program.Body, p.statements()...)
	}
	return program, nil
}

// importDeclaration là import { a, b as c } from "…"; hay import * as x from "…";
func (p *parser) importDeclaration() *Import {
	at := p.take().Pos
	imp := &Import{Pos: at}
	switch {
	case p.accept("*"):
		p.expect("as")
		imp.Namespace = p.name("a name for the module")
	case p.accept("{"):
		for !p.is("}") {
			t := p.peek()
			name := p.name("a name the module gives")
			local := name
			if p.accept("as") {
				local = p.name("a local name")
			}
			p.checkBindable(t.Pos, local)
			imp.Names = append(imp.Names, ImportName{Pos: t.Pos, Name: name, Local: local})
			if !p.accept(",") {
				break
			}
		}
		p.expect("}")
	default:
		p.fail(p.peek().Pos, `import takes { names } or * as name: import { website } from "application"`)
	}
	p.expect("from")
	t := p.take()
	if t.Kind != String {
		p.fail(t.Pos, `import takes the module as a string: from "application"`)
	}
	imp.From = t.Value
	p.end()
	return imp
}

// name đọc một tên (không phải dấu); what nói thứ đang chờ, cho lời báo.
func (p *parser) name(what string) string {
	t := p.peek()
	if t.Kind != Name {
		p.fail(t.Pos, "expected %s, found %s", what, describe(t))
	}
	p.take()
	return t.Text
}

// reserved là các từ không đặt làm tên.
var reserved = map[string]bool{
	"const": true, "let": true, "var": true, "if": true, "else": true, "return": true, "import": true, "export": true,
	"function": true, "class": true, "new": true, "this": true, "typeof": true, "void": true, "delete": true, "in": true,
	"instanceof": true, "for": true, "while": true, "do": true, "switch": true, "case": true, "default": true,
	"break": true, "continue": true, "try": true, "catch": true, "finally": true, "throw": true, "true": true,
	"false": true, "null": true, "undefined": true, "yield": true, "await": true, "with": true, "debugger": true,
	"super": true, "enum": true, "extends": true, "NaN": true, "Infinity": true,
}

func (p *parser) checkBindable(at Pos, name string) {
	if reserved[name] {
		p.fail(at, "%s is a word of the language, not a name", name)
	}
}

// end đọc dấu kết thúc câu: ; hay, như JavaScript tự chèn, một xuống dòng, một } hay hết file.
func (p *parser) end() {
	if p.accept(";") {
		return
	}
	t := p.peek()
	if t.Newline || t.Kind == EOF || p.is("}") {
		return
	}
	p.fail(t.Pos, "expected ; or a new line, found %s", describe(t))
}

// statements đọc một câu; một khai báo nhiều tên (const a = 1, b = 2) là nhiều câu.
func (p *parser) statements() []Statement {
	t := p.peek()
	if t.Kind == Name {
		switch t.Text {
		case "const", "let":
			return p.declarations()
		case "import":
			p.fail(t.Pos, "import is written at the top of the file")
		}
	}
	if s := p.statement(); s != nil {
		return []Statement{s}
	}
	return nil
}

func (p *parser) statement() Statement {
	t := p.peek()
	if p.accept(";") {
		return nil
	}
	if t.Kind == Name {
		switch t.Text {
		case "return":
			p.take()
			r := &Return{Pos: t.Pos}
			next := p.peek()
			if !next.Newline && next.Kind != EOF && !p.is(";") && !p.is("}") {
				r.Value = p.expression()
			}
			p.end()
			return r
		case "if":
			p.take()
			p.expect("(")
			s := &If{Pos: t.Pos, Condition: p.expression()}
			p.expect(")")
			s.Then = p.single()
			if p.accept("else") {
				s.Else = p.single()
			}
			return s
		case "const", "let":
			p.fail(t.Pos, "a %s written alone after if or else needs a block: { %s … }", t.Text, t.Text)
		case "var":
			p.fail(t.Pos, "Kitwork has no var: write const, or let when the name changes")
		case "function":
			p.fail(t.Pos, "Kitwork has arrow functions only: write const f = (…) => …")
		case "class":
			p.fail(t.Pos, "Kitwork has no class: an object and arrow functions do its work")
		case "while", "do":
			p.fail(t.Pos, "Kitwork has no %s: loops are for (let i = 0; i < n; i++) and for (const x of list)", t.Text)
		case "for":
			return p.forStatement()
		case "break", "continue":
			p.take()
			if p.loops == 0 {
				p.fail(t.Pos, "%s is written inside a loop", t.Text)
			}
			p.end()
			if t.Text == "break" {
				return &Break{Pos: t.Pos}
			}
			return &Continue{Pos: t.Pos}
		case "try", "catch", "finally":
			p.fail(t.Pos, "Kitwork has no try: wrap one call in safe(() => …)")
		case "throw":
			p.fail(t.Pos, "throw comes in a later slice of the engine3 runtime")
		case "switch":
			p.fail(t.Pos, "switch comes in a later slice of the engine3 runtime")
		case "export":
			p.fail(t.Pos, "export is written at the top of the file: export const x = …")
		case "else":
			p.fail(t.Pos, "else without if")
		}
	}
	if p.is("{") {
		return p.block()
	}
	s := &ExpressionStatement{Pos: t.Pos, Value: p.expression()}
	p.end()
	return s
}

// single là câu sau if hay else: một block hay một câu.
func (p *parser) single() Statement {
	if s := p.statement(); s != nil {
		return s
	}
	return &Block{Pos: p.peek().Pos}
}

func (p *parser) block() *Block {
	b := &Block{Pos: p.expect("{").Pos}
	for !p.is("}") {
		if p.peek().Kind == EOF {
			p.fail(b.Pos, "a block { is never closed")
		}
		b.Body = append(b.Body, p.statements()...)
	}
	p.take()
	return b
}

// declarations là const a = 1; hay let a, b = 2; const phải có giá trị.
func (p *parser) declarations() []Statement {
	t := p.take()
	var list []Statement
	for {
		at := p.peek().Pos
		if p.is("[") {
			p.fail(at, "array destructuring comes in a later slice of the engine3 runtime: write const x = a[0]")
		}
		d := &Declaration{Pos: at, Const: t.Text == "const"}
		if p.is("{") {
			d.Pattern = p.pattern()
			if !p.accept("=") {
				p.fail(at, "a pattern takes a value: %s { … } = …", t.Text)
			}
			d.Init = p.assignment()
		} else {
			d.Name = p.name("a name")
			p.checkBindable(at, d.Name)
			if p.accept("=") {
				d.Init = p.assignment()
			} else if d.Const {
				p.fail(at, "const %s needs a value: const %s = …", d.Name, d.Name)
			}
		}
		list = append(list, d)
		if !p.accept(",") {
			break
		}
	}
	p.end()
	return list
}

// pattern là { a, b: c, d = 1, e: { f } } bên trái một khai báo hay của for…of.
func (p *parser) pattern() *ObjectPattern {
	pattern := &ObjectPattern{Pos: p.expect("{").Pos}
	for !p.is("}") {
		t := p.peek()
		switch {
		case p.is("..."):
			p.fail(t.Pos, "a rest ...x in a pattern comes in a later slice of the engine3 runtime")
		case p.is("["):
			p.fail(t.Pos, "a computed key [k] in a pattern comes in a later slice of the engine3 runtime")
		case t.Kind != Name && t.Kind != String:
			p.fail(t.Pos, "expected a key, found %s", describe(t))
		}
		p.take()
		property := PatternProperty{Pos: t.Pos, Key: t.Text}
		if t.Kind == String {
			property.Key = t.Value
		}
		switch {
		case p.accept(":"):
			switch {
			case p.is("{"):
				property.Pattern = p.pattern()
			case p.is("["):
				p.fail(p.peek().Pos, "array destructuring comes in a later slice of the engine3 runtime")
			default:
				n := p.peek()
				property.Name = p.name("a name")
				p.checkBindable(n.Pos, property.Name)
				property.Pos = n.Pos
			}
		case t.Kind == String:
			p.fail(t.Pos, "a key written as text needs a name: %q: name", property.Key)
		default:
			p.checkBindable(t.Pos, property.Key)
			property.Name = property.Key
		}
		if p.accept("=") {
			property.Default = p.assignment()
		}
		pattern.Properties = append(pattern.Properties, property)
		if !p.accept(",") {
			break
		}
	}
	p.expect("}")
	return pattern
}

// forStatement là for (let i = 0; i < n; i++) … hay for (const x of list) …: hai dạng vòng lặp của Kitwork.
func (p *parser) forStatement() Statement {
	at := p.take().Pos
	p.expect("(")
	kind := p.peek()
	if kind.Kind != Name || (kind.Text != "const" && kind.Text != "let") {
		p.fail(kind.Pos, "loops are for (let i = 0; i < n; i++) and for (const x of list)")
	}
	p.take()
	binding := p.peek()
	var name string
	var pattern *ObjectPattern
	if p.is("{") {
		pattern = p.pattern()
	} else {
		name = p.name("a name")
		p.checkBindable(binding.Pos, name)
	}
	if p.accept("of") {
		if kind.Text != "const" {
			p.fail(kind.Pos, "write for (const %s of …): the name is new at each turn", name)
		}
		s := &ForOf{Pos: at, Name: name, Pattern: pattern, Iterable: p.expression()}
		p.expect(")")
		s.Body = p.loopBody()
		return s
	}
	if kind.Text != "let" || pattern != nil {
		p.fail(kind.Pos, "a counting for changes its counter: for (let i = 0; i < n; i++)")
	}
	p.expect("=")
	s := &For{Pos: at, Init: &Declaration{Pos: binding.Pos, Name: name, Init: p.assignment()}}
	p.expect(";")
	if p.is(";") {
		p.fail(p.peek().Pos, "a for takes a condition, i < n: Kitwork has no endless loop")
	}
	s.Test = p.expression()
	p.expect(";")
	if p.is(")") {
		p.fail(p.peek().Pos, "a for takes an update of its counter: i++")
	}
	s.Update = p.expression()
	if !updatesCounter(s.Update, name) {
		p.fail(s.Update.At(), "the update of a counting for changes its counter: %s++, %s--, %s += n or %s -= n", name, name, name, name)
	}
	p.expect(")")
	s.Body = p.loopBody()
	return s
}

// updatesCounter là e là i++, i--, ++i, --i, i += n hay i -= n với i là name.
func updatesCounter(e Expression, name string) bool {
	switch e := e.(type) {
	case *Update:
		id, ok := e.Target.(*Identifier)
		return ok && id.Name == name
	case *Assign:
		id, ok := e.Target.(*Identifier)
		return ok && id.Name == name && (e.Op == "+=" || e.Op == "-=")
	}
	return false
}

// loopBody là thân một vòng lặp: break, continue viết được trong nó.
func (p *parser) loopBody() Statement {
	p.loops++
	defer func() { p.loops-- }()
	return p.single()
}

// updatable kiểm đích của ++, --: một tên, a.b hay a[b].
func (p *parser) updatable(t Token, target Expression) {
	switch target.(type) {
	case *Identifier, *Member, *Index:
		return
	}
	p.fail(t.Pos, "%s takes a name, a.b or a[b]", t.Text)
}

// --- Biểu thức, từ thấp tới cao ---

// expression là một biểu thức; dấu phẩy chỉ ngăn đối số, khoá, phần tử: không có biểu thức a, b.
func (p *parser) expression() Expression { return p.assignment() }

var assignOperators = map[string]bool{"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true}

func (p *parser) assignment() Expression {
	if arrow := p.arrow(); arrow != nil {
		return arrow
	}
	left := p.conditional()
	t := p.peek()
	if t.Kind == Punct && assignOperators[t.Text] {
		switch left.(type) {
		case *Identifier, *Member, *Index:
		default:
			p.fail(t.Pos, "the left of %s must be a name, a.b or a[b]", t.Text)
		}
		p.take()
		return &Assign{Pos: t.Pos, Op: t.Text, Target: left, Value: p.assignment()}
	}
	if t.Kind == Punct && (t.Text == "&&=" || t.Text == "||=" || t.Text == "??=") {
		p.fail(t.Pos, "%s comes in a later slice of the engine3 runtime", t.Text)
	}
	return left
}

// arrow đọc một hàm mũi tên ở chỗ đang đứng, hay trả nil (không phải hàm mũi tên): x => …, (…) => ….
func (p *parser) arrow() Expression {
	t := p.peek()
	if t.Kind == Name && t.Text == "async" && (p.peekAt(1).Kind == Name || p.peekAt(1).Text == "(") && !p.peekAt(1).Newline {
		p.fail(t.Pos, "Kitwork has no async: an engine call waits by itself")
	}
	if t.Kind == Name && !reserved[t.Text] && p.peekAt(1).Kind == Punct && p.peekAt(1).Text == "=>" {
		p.take()
		p.take()
		return &Arrow{Pos: t.Pos, Parameters: []Parameter{{Pos: t.Pos, Name: t.Text}}, Body: p.arrowBody()}
	}
	if !p.is("(") {
		return nil
	}
	// (…) là tham số khi dấu ) khớp với nó đứng ngay trước =>
	depth := 0
	for i := p.at; i < len(p.tokens); i++ {
		u := p.tokens[i]
		if u.Kind == EOF {
			return nil
		}
		if u.Kind != Punct {
			continue
		}
		switch u.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 {
			if next := p.tokens[min(i+1, len(p.tokens)-1)]; next.Kind != Punct || next.Text != "=>" {
				return nil
			}
			break
		}
	}
	at := p.take().Pos // (
	var parameters []Parameter
	for !p.is(")") {
		u := p.peek()
		if p.is("{") || p.is("[") {
			p.fail(u.Pos, "destructuring parameters come in a later slice of the engine3 runtime: write (context) => context.x")
		}
		if p.is("...") {
			p.fail(u.Pos, "rest parameters come in a later slice of the engine3 runtime")
		}
		name := p.name("a parameter name")
		p.checkBindable(u.Pos, name)
		for _, other := range parameters {
			if other.Name == name {
				p.fail(u.Pos, "the parameter %s is written twice", name)
			}
		}
		parameter := Parameter{Pos: u.Pos, Name: name}
		if p.accept("=") {
			parameter.Default = p.assignment()
		}
		parameters = append(parameters, parameter)
		if !p.accept(",") {
			break
		}
	}
	p.expect(")")
	arrow := p.expect("=>")
	if arrow.Newline {
		p.fail(arrow.Pos, "=> goes on the line of its parameters")
	}
	return &Arrow{Pos: at, Parameters: parameters, Body: p.arrowBody()}
}

// arrowBody là thân hàm mũi tên: một block, hay một biểu thức (object trả về phải trong ngoặc: x => ({ … })).
func (p *parser) arrowBody() Node {
	loops := p.loops // vòng lặp quanh hàm không phải của thân nó: break trong hàm là lỗi
	p.loops = 0
	defer func() { p.loops = loops }()
	if p.is("{") {
		return p.block()
	}
	return p.assignment()
}

func (p *parser) conditional() Expression {
	condition := p.nullish()
	if t := p.peek(); t.Kind == Punct && t.Text == "?" {
		p.take()
		then := p.assignment()
		p.expect(":")
		return &Conditional{Pos: t.Pos, Condition: condition, Then: then, Otherwise: p.assignment()}
	}
	return condition
}

// nullish là a ?? b; JavaScript không cho trộn ?? với && hay || khi không có ngoặc.
func (p *parser) nullish() Expression {
	left := p.or()
	for {
		t := p.peek()
		if t.Kind != Punct || t.Text != "??" {
			return left
		}
		if l, ok := left.(*Logical); ok && l.Op != "??" {
			p.fail(t.Pos, "?? and %s need parentheses to be written together: (a %s b) ?? c", l.Op, l.Op)
		}
		p.take()
		right := p.or()
		if r, ok := right.(*Logical); ok {
			p.fail(t.Pos, "?? and %s need parentheses to be written together: a ?? (b %s c)", r.Op, r.Op)
		}
		left = &Logical{Pos: t.Pos, Op: "??", Left: left, Right: right}
	}
}

func (p *parser) or() Expression {
	left := p.and()
	for p.is("||") {
		t := p.take()
		left = &Logical{Pos: t.Pos, Op: "||", Left: left, Right: p.and()}
	}
	return left
}

func (p *parser) and() Expression {
	left := p.bitwise()
	for p.is("&&") {
		t := p.take()
		left = &Logical{Pos: t.Pos, Op: "&&", Left: left, Right: p.bitwise()}
	}
	return left
}

// bitwise từ chối |, ^, & (lát sau); chừa chỗ cho thứ tự ưu tiên của JavaScript.
func (p *parser) bitwise() Expression {
	left := p.equality()
	if t := p.peek(); t.Kind == Punct && (t.Text == "|" || t.Text == "^" || t.Text == "&") {
		p.fail(t.Pos, "the bitwise %s comes in a later slice of the engine3 runtime", t.Text)
	}
	return left
}

func (p *parser) equality() Expression {
	left := p.relational()
	for {
		t := p.peek()
		if t.Kind != Punct {
			return left
		}
		switch t.Text {
		case "===", "!==":
			p.take()
			left = &Binary{Pos: t.Pos, Op: t.Text, Left: left, Right: p.relational()}
		case "==", "!=":
			p.fail(t.Pos, "write %s=: Kitwork compares with === and !==, the loose %s comes in a later slice if ever", t.Text, t.Text)
		default:
			return left
		}
	}
}

func (p *parser) relational() Expression {
	left := p.shift()
	for {
		t := p.peek()
		switch {
		case t.Kind == Punct && (t.Text == "<" || t.Text == ">" || t.Text == "<=" || t.Text == ">="):
			p.take()
			left = &Binary{Pos: t.Pos, Op: t.Text, Left: left, Right: p.shift()}
		case t.Kind == Name && (t.Text == "in" || t.Text == "instanceof"):
			p.fail(t.Pos, "%s comes in a later slice of the engine3 runtime", t.Text)
		default:
			return left
		}
	}
}

func (p *parser) shift() Expression {
	left := p.additive()
	if t := p.peek(); t.Kind == Punct && (t.Text == "<<" || t.Text == ">>" || t.Text == ">>>") {
		p.fail(t.Pos, "the shift %s comes in a later slice of the engine3 runtime", t.Text)
	}
	return left
}

func (p *parser) additive() Expression {
	left := p.multiplicative()
	for p.is("+") || p.is("-") {
		t := p.take()
		left = &Binary{Pos: t.Pos, Op: t.Text, Left: left, Right: p.multiplicative()}
	}
	return left
}

func (p *parser) multiplicative() Expression {
	left := p.exponent()
	for p.is("*") || p.is("/") || p.is("%") {
		t := p.take()
		left = &Binary{Pos: t.Pos, Op: t.Text, Left: left, Right: p.exponent()}
	}
	return left
}

// exponent là a ** b, kết hợp từ phải; JavaScript không cho -a ** b khi không có ngoặc.
func (p *parser) exponent() Expression {
	left := p.unary()
	if t := p.peek(); t.Kind == Punct && t.Text == "**" {
		if _, ok := left.(*Unary); ok {
			p.fail(t.Pos, "write (-a) ** b or -(a ** b): JavaScript does not read -a ** b")
		}
		p.take()
		return &Binary{Pos: t.Pos, Op: "**", Left: left, Right: p.exponent()}
	}
	return left
}

func (p *parser) unary() Expression {
	t := p.peek()
	switch {
	case t.Kind == Punct && (t.Text == "!" || t.Text == "-" || t.Text == "+"):
		p.take()
		return &Unary{Pos: t.Pos, Op: t.Text, Operand: p.unary()}
	case t.Kind == Name && t.Text == "typeof":
		p.take()
		return &Unary{Pos: t.Pos, Op: "typeof", Operand: p.unary()}
	case t.Kind == Punct && (t.Text == "++" || t.Text == "--"):
		p.take()
		target := p.unary()
		p.updatable(t, target)
		return &Update{Pos: t.Pos, Op: t.Text, Prefix: true, Target: target}
	case t.Kind == Punct && t.Text == "~":
		p.fail(t.Pos, "the bitwise ~ comes in a later slice of the engine3 runtime")
	case t.Kind == Name && (t.Text == "void" || t.Text == "delete" || t.Text == "await"):
		p.fail(t.Pos, "%s comes in a later slice of the engine3 runtime", t.Text)
	}
	e := p.postfix()
	if t := p.peek(); t.Kind == Punct && (t.Text == "++" || t.Text == "--") && !t.Newline {
		p.take()
		p.updatable(t, e)
		return &Update{Pos: t.Pos, Op: t.Text, Target: e}
	}
	return e
}

// postfix là a.b, a[b], a(…), nối nhau.
func (p *parser) postfix() Expression {
	e := p.primary()
	for {
		t := p.peek()
		if t.Kind != Punct {
			return e
		}
		switch t.Text {
		case ".":
			p.take()
			name := p.peek()
			if name.Kind != Name {
				p.fail(name.Pos, "expected a name after ., found %s", describe(name))
			}
			p.take()
			e = &Member{Pos: name.Pos, Object: e, Name: name.Text}
		case "[":
			p.take()
			e = &Index{Pos: t.Pos, Object: e, Index: p.expression()}
			p.expect("]")
		case "(":
			p.take()
			e = &Call{Pos: t.Pos, Callee: e, Arguments: p.arguments()}
		case "?.":
			p.fail(t.Pos, "?. comes in a later slice of the engine3 runtime")
		default:
			return e
		}
		if next := p.peek(); next.Kind == TemplateFull || next.Kind == TemplateHead {
			p.fail(next.Pos, "a tagged template comes in a later slice of the engine3 runtime")
		}
	}
}

// arguments đọc các đối số tới ): dấu phẩy cuối được, như JavaScript.
func (p *parser) arguments() []Expression {
	var list []Expression
	for !p.is(")") {
		if p.is("...") {
			p.fail(p.peek().Pos, "spread in a call, f(...list), comes in a later slice of the engine3 runtime")
		}
		list = append(list, p.assignment())
		if !p.accept(",") {
			break
		}
	}
	p.expect(")")
	return list
}

func (p *parser) primary() Expression {
	t := p.peek()
	switch t.Kind {
	case Number:
		p.take()
		return &Literal{Pos: t.Pos, Kind: NumberLiteral, Num: t.Number}
	case String:
		p.take()
		return &Literal{Pos: t.Pos, Kind: StringLiteral, Text: t.Value}
	case TemplateFull:
		p.take()
		return &Template{Pos: t.Pos, Parts: []string{t.Value}}
	case TemplateHead:
		return p.template()
	case Name:
		p.take()
		switch t.Text {
		case "true", "false":
			return &Literal{Pos: t.Pos, Kind: BooleanLiteral, Truth: t.Text == "true"}
		case "null":
			return &Literal{Pos: t.Pos, Kind: NullLiteral}
		case "undefined":
			return &Literal{Pos: t.Pos, Kind: UndefinedLiteral}
		case "this":
			p.fail(t.Pos, "Kitwork has no this: an arrow function reads the names around it")
		case "new":
			p.fail(t.Pos, "new comes in a later slice of the engine3 runtime")
		case "function":
			p.fail(t.Pos, "Kitwork has arrow functions only: write (…) => …")
		case "class":
			p.fail(t.Pos, "Kitwork has no class: an object and arrow functions do its work")
		}
		if reserved[t.Text] && t.Text != "NaN" && t.Text != "Infinity" {
			p.fail(t.Pos, "%s cannot be written here", t.Text)
		}
		return &Identifier{Pos: t.Pos, Name: t.Text}
	case Punct:
		switch t.Text {
		case "(":
			p.take()
			e := p.expression()
			p.expect(")")
			return e
		case "[":
			return p.array()
		case "{":
			return p.object()
		case "/", "/=":
			p.fail(t.Pos, "regular expressions come in a later slice of the engine3 runtime")
		}
	case EOF:
		p.fail(t.Pos, "the file ends where a value is expected")
	}
	p.fail(t.Pos, "expected a value, found %s", describe(t))
	return nil
}

func (p *parser) template() Expression {
	head := p.take()
	e := &Template{Pos: head.Pos, Parts: []string{head.Value}}
	for {
		e.Values = append(e.Values, p.expression())
		t := p.take()
		switch t.Kind {
		case TemplateMiddle:
			e.Parts = append(e.Parts, t.Value)
		case TemplateTail:
			e.Parts = append(e.Parts, t.Value)
			return e
		default:
			p.fail(t.Pos, "expected } to close ${ in a template, found %s", describe(t))
		}
	}
}

// array là [a, b]: không dấu phẩy cuối (luật của Kitwork), không lỗ ([1, , 2]).
func (p *parser) array() Expression {
	e := &Array{Pos: p.take().Pos}
	for !p.is("]") {
		if p.is(",") {
			p.fail(p.peek().Pos, "an array has no holes: write undefined where nothing is")
		}
		if t := p.peek(); p.accept("...") {
			e.Items = append(e.Items, &Spread{Pos: t.Pos, Value: p.assignment()})
		} else {
			e.Items = append(e.Items, p.assignment())
		}
		if !p.accept(",") {
			break
		}
		if p.is("]") {
			p.fail(p.peek().Pos, "an array takes no comma after its last item")
		}
	}
	p.expect("]")
	return e
}

// object là { a: 1, b, "c": 2, 3: x, ...o }: dấu phẩy cuối được; một ...o là một Property có Value *Spread, không khoá;
// khoá tính ([k]: v), phương thức tới sau.
func (p *parser) object() Expression {
	e := &Object{Pos: p.take().Pos}
	for !p.is("}") {
		t := p.peek()
		if p.accept("...") {
			e.Properties = append(e.Properties, Property{Pos: t.Pos, Value: &Spread{Pos: t.Pos, Value: p.assignment()}})
			if !p.accept(",") {
				break
			}
			continue
		}
		var key string
		switch {
		case t.Kind == Name:
			key = t.Text
		case t.Kind == String:
			key = t.Value
		case t.Kind == Number:
			key = strconv.FormatFloat(t.Number, 'f', -1, 64)
		case p.is("["):
			p.fail(t.Pos, "a computed key [k]: comes in a later slice of the engine3 runtime")
		default:
			p.fail(t.Pos, "expected a key, found %s", describe(t))
		}
		p.take()
		switch {
		case p.accept(":"):
			e.Properties = append(e.Properties, Property{Pos: t.Pos, Key: key, Value: p.assignment()})
		case p.is("("):
			p.fail(p.peek().Pos, "Kitwork has arrow functions only: write %s: (…) => …", key)
		case t.Kind == Name:
			if reserved[key] {
				p.fail(t.Pos, "%s is a word of the language: write %s: value", key, key)
			}
			e.Properties = append(e.Properties, Property{Pos: t.Pos, Key: key, Value: &Identifier{Pos: t.Pos, Name: key}, Shorthand: true})
		default:
			p.fail(p.peek().Pos, "expected : after the key %q", key)
		}
		if !p.accept(",") {
			break
		}
	}
	p.expect("}")
	return e
}
