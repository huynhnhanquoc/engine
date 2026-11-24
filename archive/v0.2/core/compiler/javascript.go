package compiler

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/builtins"
)

// The browser's expressions as JavaScript (round 4 of the 13/11 review, the owner's choice of 15/11/2026: "Server
// sinh JS thật"). What a .ui page runs in the browser, a value that follows a component's state, what an event does,
// is written by this compiler as JavaScript, so the browser's own engine runs it with JavaScript's meaning, the one
// the VM gives the same source on the server (docs/KITWORK_LANG.md mục 3), with .ui's two rules over it: every link
// of a chain an expression reads is ?. (safeChains), and the page writes nothing for null and undefined (the
// kernel's, as it writes the DOM). The kernel had a language of its own, a parser and a tree walker in kernel.js,
// whose meaning was another: null was "" in a string and 0 in a sum, a call of what is no function was undefined,
// a name nobody gave was 0, and a template, typeof, a spread or an arrow with a body was refused.
//
// The function the page's program wraps the text in is called with this, the scope the kernel reads the element's
// names from (its component's, the page's, $event and the aliases): a name the source does not declare is read from
// it, this.title, this.toggle(); what the source declares (an arrow's parameter, a const in its body) is a variable
// of the text, written as it is. this is no name a source can declare, and an arrow keeps it, so the two never meet.
// A builtin is the browser's own (Math, JSON, Date…), but safe and error, the server's.
//
// What could reach code through a key is read through the kernel (kitwork.internal): a key written in brackets, and
// a member named constructor, __proto__, prototype, the window's names among them, read, called and written by
// kitwork.internal.read, call and write, which give undefined for those names, as the VM, whose values have none of
// them, does. kitwork is no name a source may use, so nothing hides the kernel.

// blockedMembers are the members kitwork.internal reads as undefined: the way to Function (constructor), to a
// prototype, to the window from the DOM. The kernel's blockedKey holds the same names.
var blockedMembers = map[string]bool{
	"constructor": true, "__proto__": true, "prototype": true, "__defineGetter__": true, "__defineSetter__": true,
	"__lookupGetter__": true, "__lookupSetter__": true, "ownerDocument": true, "defaultView": true,
	"contentWindow": true, "window": true, "parent": true, "top": true, "self": true, "globalThis": true,
}

// serverBuiltins are the builtins that run only on the server: an expression of the browser that reads one is an
// error.
var serverBuiltins = map[string]bool{"safe": true, "error": true}

// constructors are the builtins a source calls without new, which the parser drops (new Date(x) is Date(x)), and
// which JavaScript makes an object of only with new: the VM makes a date of Date(x), so the browser is given new.
var constructors = map[string]bool{"Date": true}

// ExpressionJavaScript is source, an expression of a .ui file that reads (what {{ }}, if, show, the list of a for
// and a binding read), as a JavaScript expression of the browser: the value CompileExpression gives on the server.
func ExpressionJavaScript(source string) (string, error) {
	return browserJavaScript(source, false)
}

// HandlerJavaScript is source, what an event of a .ui file runs (CompileHandler), as a JavaScript expression of the
// browser. It may write a name (this.count = …), a member, ++ and --; what it reads follows .ui's ?. as an
// expression's does.
func HandlerJavaScript(source string) (string, error) {
	return browserJavaScript(source, true)
}

func browserJavaScript(source string, handler bool) (_ string, err error) {
	defer contain(&err)
	name := "<expression>"
	if handler {
		name = "<handler>"
	}
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, name)
	if err != nil {
		return "", err
	}
	statement, ok := oneValue(program)
	if !ok {
		return "", fmt.Errorf("%q là một biểu thức, không phải câu lệnh", source)
	}
	if handler {
		statement.Value, err = handlerChains(statement.Value)
	} else {
		statement.Value, err = safeChains(statement.Value)
	}
	if err != nil {
		return "", err
	}
	c := newCompilerWithSources(map[string]string{name: text})
	if err := c.Compile(program); err != nil {
		return "", err
	}
	w := &javascriptWriter{scopes: c.scopes}
	w.expression(statement.Value)
	if w.err != nil {
		return "", w.err
	}
	return w.out.String(), nil
}

// handlerChains makes what an event reads follow ?. as an expression's chains do, and leaves what it writes as
// written: JavaScript writes no member through ?., and writing one of null is its TypeError.
func handlerChains(expression Expression) (Expression, error) {
	switch e := expression.(type) {
	case *AssignmentExpression:
		if right, shared := e.sharedTarget(); shared {
			// x op= y and x++: the value reads the target, whose object is read as the target's
			if err := targetChains(e.Name); err != nil {
				return nil, err
			}
			safe, err := handlerChains(right)
			if err != nil {
				return nil, err
			}
			e.Value.(*InfixExpression).Right = safe
			return e, nil
		}
		if err := targetChains(e.Name); err != nil {
			return nil, err
		}
		value, err := handlerChains(e.Value)
		if err != nil {
			return nil, err
		}
		e.Value = value
		return e, nil
	case *InfixExpression:
		left, err := handlerChains(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := handlerChains(e.Right)
		e.Left, e.Right = left, right
		return e, err
	case *TernaryExpression:
		parts := []*Expression{&e.Condition, &e.Consequence, &e.Alternative}
		for _, part := range parts {
			safe, err := handlerChains(*part)
			if err != nil {
				return nil, err
			}
			*part = safe
		}
		return e, nil
	case *PrefixExpression:
		right, err := handlerChains(e.Right)
		e.Right = right
		return e, err
	case *CallExpression, *MethodCallExpression, *MemberExpression, *IndexExpression, *ChainExpression:
		if hasWrite(e) {
			return nil, fmt.Errorf("một sự kiện gán bên trong lời gọi hay chuỗi truy cập: tách thành hai sự kiện, hay gán trước rồi gọi")
		}
		return safeChains(e)
	}
	if hasWrite(expression) {
		return nil, fmt.Errorf("một sự kiện gán bên trong một giá trị (mảng, object, template): gán ở ngoài cùng của sự kiện")
	}
	return safeChains(expression)
}

// targetChains makes the object a written member is read from follow ?., the member itself staying the target:
// o.a.b = 1 reads o?.a and writes its b.
func targetChains(target Expression) error {
	switch t := target.(type) {
	case *MemberExpression:
		object, err := safeChains(t.Object)
		t.Object = object
		return err
	case *IndexExpression:
		left, err := safeChains(t.Left)
		if err != nil {
			return err
		}
		index, err := safeChains(t.Index)
		t.Left, t.Index = left, index
		return err
	}
	return nil
}

// hasWrite reports an assignment anywhere in expression but inside an arrow.
func hasWrite(expression Expression) bool {
	return len(writtenNames(expression)) > 0 || writesMember(expression)
}

// writesMember reports an assignment to a member anywhere in expression but inside an arrow.
func writesMember(expression Expression) bool {
	found := false
	var walk func(Expression)
	walk = func(expression Expression) {
		switch e := expression.(type) {
		case *AssignmentExpression:
			found = true
		case *ChainExpression:
			walk(e.Expression)
		case *MemberExpression:
			walk(e.Object)
		case *IndexExpression:
			walk(e.Left)
			walk(e.Index)
		case *MethodCallExpression:
			walk(e.Object)
			for _, argument := range e.Arguments {
				walk(argument)
			}
		case *CallExpression:
			walk(e.Function)
			for _, argument := range e.Arguments {
				walk(argument)
			}
		case *PrefixExpression:
			walk(e.Right)
		case *InfixExpression:
			walk(e.Left)
			walk(e.Right)
		case *TernaryExpression:
			walk(e.Condition)
			walk(e.Consequence)
			walk(e.Alternative)
		case *ArrayLiteral:
			for _, element := range e.Elements {
				walk(element)
			}
		case *ObjectLiteral:
			for _, entry := range e.Entries {
				walk(entry.Value)
			}
		case *SpreadExpression:
			walk(e.Value)
		case *TemplateLiteral:
			for _, part := range e.Parts {
				walk(part)
			}
		}
	}
	walk(expression)
	return found
}

// javascriptWriter writes an expression and the statements of the arrows in it as JavaScript.
type javascriptWriter struct {
	out    strings.Builder
	scopes *resolution
	err    error
	// renamed are, for each function being written, the inner one last, the names its parameters are written with
	// where the source's is no JavaScript name: a destructured parameter, ({ id, ...rest }) => …, which the parser
	// names by its pattern ("{ id, ...rest }") and reads in its body (parseGroupedExpression's prelude). The name
	// written holds a Greek letter, which no name of Kitwork holds (its names are ASCII), so it meets none.
	renamed []map[string]string
	fresh   int
}

func (w *javascriptWriter) write(parts ...string) {
	for _, part := range parts {
		w.out.WriteString(part)
	}
}

func (w *javascriptWriter) fail(format string, arguments ...any) {
	if w.err == nil {
		w.err = fmt.Errorf(format, arguments...)
	}
}

// local reports a name the source declares: a variable of the text, written as it is.
func (w *javascriptWriter) local(name *Identifier) bool {
	_, ok := w.scopes.slots[name]
	return ok
}

// name writes a name the source reads or writes: its variable, a builtin of the browser, or the scope's.
func (w *javascriptWriter) name(name *Identifier) {
	switch {
	case w.local(name):
		w.write(w.localName(name.Value))
	case name.Value == kitworkName:
		w.fail("kitwork không đọc được trong một biểu thức của .ui chạy ở trình duyệt: trang gọi kernel qua component, không qua kitwork")
	case serverBuiltins[name.Value]:
		w.fail("%s(…) chỉ chạy ở server: một biểu thức của .ui chạy ở trình duyệt không gọi nó", name.Value)
	default:
		if _, builtin := w.scopes.builtinReads[name]; builtin {
			w.write(name.Value)
			return
		}
		w.write("this.", name.Value)
	}
}

func (w *javascriptWriter) list(expressions []Expression) {
	for i, each := range expressions {
		if i > 0 {
			w.write(", ")
		}
		w.expression(each)
	}
}

func (w *javascriptWriter) expression(expression Expression) {
	if w.err != nil {
		return
	}
	switch e := expression.(type) {
	case nil:
		w.write("void 0")
	case *Identifier:
		w.name(e)
	case *Literal:
		w.literal(e)
	case *ChainExpression:
		w.expression(e.Expression)
	case *PrefixExpression:
		switch e.Operator {
		case "!", "-", "+":
			w.write("(", e.Operator)
		case "typeof", "void", "delete":
			w.write("(", e.Operator, " ")
		default:
			w.fail("trình duyệt chưa có toán tử %s", e.Operator)
			return
		}
		w.expression(e.Right)
		w.write(")")
	case *InfixExpression:
		switch e.Operator {
		case "+", "-", "*", "/", "%", "==", "!=", "===", "!==", "<", ">", "<=", ">=", "&&", "||", "??":
		default:
			w.fail("trình duyệt chưa có toán tử %s", e.Operator)
			return
		}
		w.write("(")
		w.expression(e.Left)
		w.write(" ", e.Operator, " ")
		w.expression(e.Right)
		w.write(")")
	case *TernaryExpression:
		w.write("(")
		w.expression(e.Condition)
		w.write(" ? ")
		w.expression(e.Consequence)
		w.write(" : ")
		w.expression(e.Alternative)
		w.write(")")
	case *AssignmentExpression:
		w.assignment(e)
	case *MemberExpression:
		if e.Property == nil {
			w.fail("một thành viên không có tên")
			return
		}
		if blockedMembers[e.Property.Value] {
			w.write("kitwork.internal.read(")
			w.expression(e.Object)
			w.write(", ", quoted(e.Property.Value), ", ", strconv.FormatBool(e.Optional), ")")
			return
		}
		w.expression(e.Object)
		if e.Optional {
			w.write("?.")
		} else {
			w.write(".")
		}
		w.write(e.Property.Value)
	case *IndexExpression:
		w.write("kitwork.internal.read(")
		w.expression(e.Left)
		w.write(", ")
		w.expression(e.Index)
		w.write(", ", strconv.FormatBool(e.Optional), ")")
	case *MethodCallExpression:
		if e.Method == nil {
			w.fail("một lời gọi không có tên")
			return
		}
		if blockedMembers[e.Method.Value] {
			w.write("kitwork.internal.call(")
			w.expression(e.Object)
			w.write(", ", quoted(e.Method.Value), ", [")
			w.list(e.Arguments)
			w.write("], ", strconv.FormatBool(e.Optional), ", ", strconv.FormatBool(e.OptionalCall), ")")
			return
		}
		w.expression(e.Object)
		if e.Optional {
			w.write("?.")
		} else {
			w.write(".")
		}
		w.write(e.Method.Value)
		if e.OptionalCall {
			w.write("?.")
		}
		w.write("(")
		w.list(e.Arguments)
		w.write(")")
	case *CallExpression:
		w.call(e)
	case *ArrayLiteral:
		w.write("[")
		w.list(e.Elements)
		w.write("]")
	case *SpreadExpression:
		w.write("...")
		w.expression(e.Value)
	case *ObjectLiteral:
		w.object(e)
	case *TemplateLiteral:
		// each part a substitution: a piece of text is a string, which a template writes as it is
		w.write("`")
		for _, part := range e.Parts {
			w.write("${")
			w.expression(part)
			w.write("}")
		}
		w.write("`")
	case *FunctionLiteral:
		w.function(e)
	default:
		w.fail("trình duyệt chưa chạy được %T trong một biểu thức của .ui", expression)
	}
}

// literal writes a value written in the source.
func (w *javascriptWriter) literal(e *Literal) {
	v := e.Value
	switch {
	case v.IsUndefined():
		w.write("void 0")
	case v.IsNull():
		w.write("null")
	case v.IsBoolean():
		w.write(strconv.FormatBool(v.ToBoolean()))
	case v.IsNumber():
		w.write("(", strconv.FormatFloat(v.Number(), 'g', -1, 64), ")")
	case v.IsString():
		w.write(quoted(v.Text()))
	default:
		w.fail("trình duyệt chưa viết được giá trị %s", v.Kind())
	}
}

// quoted is text as a JavaScript string: JSON's, whose escapes JavaScript reads alike.
func quoted(text string) string {
	b, _ := json.Marshal(text)
	return string(b)
}

// call writes a call: of a name (this.toggle(…) for the scope's, so this is the scope as the kernel calls a
// component's method), of a constructor of the browser with new, of a member read by its key through the kernel.
func (w *javascriptWriter) call(e *CallExpression) {
	if index, ok := e.Function.(*IndexExpression); ok {
		w.write("kitwork.internal.call(")
		w.expression(index.Left)
		w.write(", ")
		w.expression(index.Index)
		w.write(", [")
		w.list(e.Arguments)
		w.write("], ", strconv.FormatBool(index.Optional), ", ", strconv.FormatBool(e.Optional), ")")
		return
	}
	if name, ok := e.Function.(*Identifier); ok && constructors[name.Value] {
		if _, builtin := w.scopes.builtinReads[name]; builtin {
			w.write("new ", name.Value, "(")
			w.list(e.Arguments)
			w.write(")")
			return
		}
	}
	if _, ok := e.Function.(*FunctionLiteral); ok {
		w.write("(")
		w.expression(e.Function)
		w.write(")")
	} else {
		w.expression(e.Function)
	}
	if e.Optional {
		w.write("?.")
	}
	w.write("(")
	w.list(e.Arguments)
	w.write(")")
}

// assignment writes what an event writes: a name (the scope's, this.count, or a variable of the text), a member,
// and x++, ++x, x op= y as JavaScript writes them.
func (w *javascriptWriter) assignment(e *AssignmentExpression) {
	right, shared := e.sharedTarget()
	if e.Update != NotAnUpdate {
		operator := "++"
		if infix, ok := e.Value.(*InfixExpression); ok && infix.Operator == "-" {
			operator = "--"
		}
		w.write("(")
		if e.Update == PrefixUpdate {
			w.write(operator)
		}
		w.target(e.Name)
		if e.Update == PostfixUpdate {
			w.write(operator)
		}
		w.write(")")
		return
	}
	if shared {
		infix := e.Value.(*InfixExpression)
		w.write("(")
		w.target(e.Name)
		w.write(" ", infix.Operator, "= ")
		w.expression(right)
		w.write(")")
		return
	}
	w.write("(")
	w.target(e.Name)
	w.write(" = ")
	w.expression(e.Value)
	w.write(")")
}

// target writes what an assignment writes; a key in brackets and a blocked member are written by the kernel, which
// has no ++ of them.
func (w *javascriptWriter) target(target Expression) {
	switch t := target.(type) {
	case *Identifier:
		if _, builtin := builtins.Slot(t.Value); builtin && !w.local(t) {
			w.fail("%s là builtin, không gán được", t.Value)
			return
		}
		w.name(t)
	case *MemberExpression:
		if t.Property == nil || blockedMembers[t.Property.Value] {
			w.fail("không gán được thành viên %s trong một sự kiện của .ui", t.Property)
			return
		}
		// in brackets: JavaScript writes no member at the end of a chain with ?. in it, and a parenthesized chain
		// ends there (o.b.c += 1 is (this.o?.b).c += 1)
		w.write("(")
		w.expression(t.Object)
		w.write(").", t.Property.Value)
	default:
		w.fail("một sự kiện của .ui gán vào một tên hay một thành viên tên viết sẵn (o.a = …), không gán %s", target.String())
	}
}

// object writes an object literal: each key as text (__proto__ in brackets, so it is a key and never the
// prototype, as the VM makes it), a computed key in brackets, a spread.
func (w *javascriptWriter) object(e *ObjectLiteral) {
	w.write("({")
	for i, entry := range e.Entries {
		if i > 0 {
			w.write(", ")
		}
		if entry.IsSpread {
			w.write("...")
			w.expression(entry.Value)
			continue
		}
		switch key := entry.Key.(type) {
		case *Identifier:
			w.key(key.Value)
		case *Literal:
			if key.Value.IsString() {
				w.key(key.Value.Text())
			} else {
				w.write("[")
				w.literal(key)
				w.write("]")
			}
		default:
			computed := computedKey(entry.Key)
			if computed == nil {
				w.fail("một khoá của object trình duyệt chưa viết được: %s", entry.Key.String())
				return
			}
			w.write("[")
			w.expression(computed)
			w.write("]")
		}
		w.write(": ")
		w.expression(entry.Value)
	}
	w.write("})")
}

func (w *javascriptWriter) key(name string) {
	if name == "__proto__" {
		w.write("[", quoted(name), "]")
		return
	}
	w.write(quoted(name))
}

// function writes an arrow: its parameters, and its body as a block, the functions it declares first, as they are
// made when the block begins (functionsFirst).
func (w *javascriptWriter) function(e *FunctionLiteral) {
	renamed := map[string]string{}
	for _, parameter := range e.Parameters {
		if !identifierPattern.MatchString(parameter.Value) {
			renamed[parameter.Value] = "φ" + strconv.Itoa(w.fresh)
			w.fresh++
		}
	}
	w.renamed = append(w.renamed, renamed)
	defer func() { w.renamed = w.renamed[:len(w.renamed)-1] }()
	w.write("((")
	for i, parameter := range e.Parameters {
		if i > 0 {
			w.write(", ")
		}
		if e.Rest && i == len(e.Parameters)-1 {
			w.write("...")
		}
		w.write(w.localName(parameter.Value))
	}
	w.write(") => ")
	w.block(e.Body)
	w.write(")")
}

// localName is the name a variable of the source is written with: its own, or the one its function's parameter was
// renamed to (renamed).
func (w *javascriptWriter) localName(name string) string {
	for i := len(w.renamed) - 1; i >= 0; i-- {
		if renamed, ok := w.renamed[i][name]; ok {
			return renamed
		}
	}
	return name
}

// identifierPattern is a name JavaScript writes as it is: ASCII letters, digits, _ and $, not starting with a digit.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func (w *javascriptWriter) block(block *BlockStatement) {
	w.write("{ ")
	if block != nil {
		for _, statement := range functionsFirst(block.Statements) {
			w.statement(statement)
		}
	}
	w.write("}")
}

func (w *javascriptWriter) statement(statement Statement) {
	if w.err != nil {
		return
	}
	switch s := statement.(type) {
	case *ReturnStatement:
		w.write("return ")
		w.expression(s.ReturnValue)
		w.write("; ")
	case *ExpressionStatement:
		if branch, ok := s.Expression.(*IfExpression); ok {
			w.ifStatement(branch)
			return
		}
		w.expression(s.Expression)
		w.write("; ")
	case *VarStatement:
		keyword := "let"
		if s.Token.Kind == Const {
			keyword = "const"
		}
		if s.Pattern != nil {
			w.write(keyword, " ")
			w.pattern(s.Pattern)
		} else if len(s.Names) == 1 {
			w.write(keyword, " ", s.Names[0].Value)
		} else {
			w.fail("trình duyệt chưa viết được dòng khai %s", s.String())
			return
		}
		if s.Value != nil {
			w.write(" = ")
			w.expression(s.Value)
		}
		w.write("; ")
	case *BlockStatement:
		w.block(s)
	case *ForStatement:
		keyword := "let"
		if s.Const {
			keyword = "const"
		}
		// a destructured item, for (const { a } of list), is named by its pattern (patternParameter): renamed as a
		// destructured parameter is
		renamed := map[string]string{}
		if !identifierPattern.MatchString(s.Item.Value) {
			renamed[s.Item.Value] = "φ" + strconv.Itoa(w.fresh)
			w.fresh++
		}
		w.write("for (", keyword, " ")
		w.renamed = append(w.renamed, renamed)
		w.write(w.localName(s.Item.Value), " of ")
		w.renamed = w.renamed[:len(w.renamed)-1]
		w.expression(s.Iterable)
		w.write(") ")
		w.renamed = append(w.renamed, renamed)
		w.block(s.Body)
		w.renamed = w.renamed[:len(w.renamed)-1]
		w.write(" ")
	case *ForRangeStatement:
		init, ok := s.Init.(*VarStatement)
		if !ok || len(init.Names) != 1 {
			w.fail("trình duyệt chưa viết được vòng for này")
			return
		}
		w.write("for (let ", init.Names[0].Value, " = ")
		w.expression(init.Value)
		w.write("; ")
		w.expression(s.Cond)
		w.write("; ")
		w.expression(s.Update)
		w.write(") ")
		w.block(s.Body)
		w.write(" ")
	case *BreakStatement:
		w.write("break; ")
	case *ContinueStatement:
		w.write("continue; ")
	default:
		w.fail("trình duyệt chưa chạy được câu lệnh %T trong một biểu thức của .ui", statement)
	}
}

func (w *javascriptWriter) ifStatement(branch *IfExpression) {
	w.write("if (")
	w.expression(branch.Condition)
	w.write(") ")
	w.block(branch.Consequence)
	if branch.Alternative != nil {
		w.write(" else ")
		w.block(branch.Alternative)
	}
	w.write(" ")
}

// pattern writes a destructuring of a declaration: { a, b: c = d } or [a, b].
func (w *javascriptWriter) pattern(pattern *BindingPattern) {
	open, close := "{ ", " }"
	if pattern.Array {
		open, close = "[", "]"
	}
	w.write(open)
	for i, element := range pattern.Elements {
		if i > 0 {
			w.write(", ")
		}
		if element.Rest {
			w.write("...")
		} else if !pattern.Array && element.Key != "" {
			w.write(quoted(element.Key), ": ")
		}
		if element.Pattern != nil {
			w.pattern(element.Pattern)
		} else if element.Name != nil {
			w.write(element.Name.Value)
		}
		if element.Default != nil {
			w.write(" = ")
			w.expression(element.Default)
		}
	}
	w.write(close)
}
