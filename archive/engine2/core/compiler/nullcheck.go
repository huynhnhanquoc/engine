package compiler

import "fmt"

// The compile-time check of null (19/11/2026, decided 27/10/2026; docs/language.md mục 3.7, root
// docs/KITWORK_NULL_CHECK.md, slice 1): a value a library call may give as null, used where JavaScript throws on null
// (a member read, a method call, a destructuring, for…of, a spread into an array or a call, a member write) on a path
// that has not checked it, is a compile error naming the value, the call and how to check it. The calls are a closed
// set (nullCalls): kitwork/file's read, text, bytes, json, info; kitwork/storage's get() of a handle; kitwork/database's
// first() of a statement; the request's file(name) of a router door's context; and safe(…)'s error, null when the
// function ran through.
//
// Slice 1 follows a value within one file: from the call to a name a const or let holds, through the function it is
// in and the functions written inside it; what passes through another function's return or another file's export is
// not followed. A check is what JavaScript reads as one: if (x), if (x != null), if (!x) return …, x && x.y,
// x ? x.y : d, x ?? d, x?.y. A .ui expression is not checked: its markup reads a member of null as nothing.

// nullKind is what the check knows a name or an expression to be.
type nullKind uint8

const (
	nullOther    nullKind = iota
	nullLibrary           // a library's namespace: const file = kitwork.file
	nullMember            // a library's member: const { memory } = kitwork.storage, const m = kitwork.storage.memory
	nullStorage           // a storage handle: memory("k"), memory("k").segment("a")
	nullDatabase          // a database handle or a statement on it: connection(), db.where(…)
	nullSafe              // what safe(…) gives, whose error may be null
	nullContext           // the context a router door hands its function
	nullMaybe             // what a call of the closed set gave: it may be null
)

// nullInfo is what an expression is to the check: its kind, the library and member of a library's, and for nullMaybe
// the call that gave it and when that call gives null.
type nullInfo struct {
	kind            nullKind
	library, member string
	origin, reason  string
}

// nullSymbol is a name the check follows.
type nullSymbol struct {
	info     nullInfo
	constant bool
}

// nullKey is what a check proves not null: a name, or the error of a name safe(…) gave (field "error").
type nullKey struct {
	symbol *nullSymbol
	field  string
}

type nullScope struct {
	names  map[string]*nullSymbol
	parent *nullScope
}

type nullChecker struct {
	compiler *Compiler
	scope    *nullScope
	checked  map[nullKey]bool
	err      error
}

// fileReads are kitwork/file's calls that give null when the file is not there.
var fileReads = map[string]bool{"read": true, "text": true, "bytes": true, "json": true, "info": true}

// storageMakers are kitwork/storage's functions that make a handle.
var storageMakers = map[string]bool{"memory": true, "disk": true, "datastore": true}

// checkNulls is the check over a file's program: the first use of a value that may be null, unchecked, as a compile
// error.
func (c *Compiler) checkNulls(program *Program) error {
	checker := &nullChecker{compiler: c, scope: &nullScope{names: map[string]*nullSymbol{}}, checked: map[nullKey]bool{}}
	checker.statements(program.Statements)
	return checker.err
}

// ---- scopes ----

func (n *nullChecker) push() { n.scope = &nullScope{names: map[string]*nullSymbol{}, parent: n.scope} }
func (n *nullChecker) pop()  { n.scope = n.scope.parent }

func (n *nullChecker) define(name string, info nullInfo, constant bool) *nullSymbol {
	symbol := &nullSymbol{info: info, constant: constant}
	n.scope.names[name] = symbol
	return symbol
}

func (n *nullChecker) lookup(name string) *nullSymbol {
	for scope := n.scope; scope != nil; scope = scope.parent {
		if symbol, ok := scope.names[name]; ok {
			return symbol
		}
	}
	return nil
}

// builtin reports name read as the builtin or the global of that name: no name of the file hides it.
func (n *nullChecker) builtin(name string) bool { return n.lookup(name) == nil }

func (n *nullChecker) copyChecked() map[nullKey]bool {
	copied := make(map[nullKey]bool, len(n.checked))
	for key := range n.checked {
		copied[key] = true
	}
	return copied
}

func intersectChecked(a, b map[nullKey]bool) map[nullKey]bool {
	both := map[nullKey]bool{}
	for key := range a {
		if b[key] {
			both[key] = true
		}
	}
	return both
}

// ---- what an expression is ----

// classify is what e is: a library's, a handle, a value that may be null (unless the path checked it), or other.
func (n *nullChecker) classify(e Expression) nullInfo {
	switch x := e.(type) {
	case *Identifier:
		symbol := n.lookup(x.Value)
		if symbol == nil {
			return nullInfo{}
		}
		if symbol.info.kind == nullMaybe && n.checked[nullKey{symbol: symbol}] {
			return nullInfo{}
		}
		return symbol.info
	case *MemberExpression:
		if x.Optional {
			return nullInfo{}
		}
		if name, ok := x.Object.(*Identifier); ok && name.Value == "kitwork" && n.builtin("kitwork") {
			return nullInfo{kind: nullLibrary, library: x.Property.Value}
		}
		object := n.classify(x.Object)
		switch object.kind {
		case nullLibrary:
			return nullInfo{kind: nullMember, library: object.library, member: x.Property.Value}
		case nullSafe:
			if x.Property.Value == "error" {
				if name, ok := x.Object.(*Identifier); ok {
					if symbol := n.lookup(name.Value); symbol != nil && n.checked[nullKey{symbol: symbol, field: "error"}] {
						return nullInfo{}
					}
				}
				return nullInfo{kind: nullMaybe, origin: "safe(…).error", reason: "là null khi hàm chạy xong"}
			}
		}
		return nullInfo{}
	case *CallExpression:
		if x.Optional {
			return nullInfo{}
		}
		if name, ok := x.Function.(*Identifier); ok && name.Value == "safe" && n.builtin("safe") {
			return nullInfo{kind: nullSafe}
		}
		callee := n.classify(x.Function)
		if callee.kind == nullMember {
			return n.libraryCall(callee.library, callee.member, callee.member, x.Arguments)
		}
		return nullInfo{}
	case *MethodCallExpression:
		if x.Optional || x.OptionalCall || x.Method == nil {
			return nullInfo{}
		}
		method := x.Method.Value
		object := n.classify(x.Object)
		switch object.kind {
		case nullLibrary:
			return n.libraryCall(object.library, method, object.library+"."+method, x.Arguments)
		case nullStorage:
			switch {
			case method == "segment" || method == "expire":
				return object
			case method == "get" && len(x.Arguments) == 0:
				return nullInfo{kind: nullMaybe, origin: "….get()", reason: "của kitwork/storage trả null khi chưa có hay đã hết hạn"}
			}
		case nullDatabase:
			if method == "first" {
				return nullInfo{kind: nullMaybe, origin: "….first()", reason: "của kitwork/database trả null khi không có dòng nào"}
			}
			return object // a statement on it: where, select, ascending…
		}
		if member, ok := x.Object.(*MemberExpression); ok && method == "file" && !member.Optional && member.Property.Value == "request" &&
			n.classify(member.Object).kind == nullContext {
			return nullInfo{kind: nullMaybe, origin: "context.request.file(…)", reason: "trả null khi form không có file ấy"}
		}
		return nullInfo{}
	case *InfixExpression:
		if x.Operator == "??" && n.classify(x.Right).kind != nullMaybe {
			return nullInfo{} // a ?? b is b when a is null
		}
		return nullInfo{}
	}
	return nullInfo{}
}

// libraryCall is what a call of library's member gives: written is how the message quotes it.
func (n *nullChecker) libraryCall(library, member, written string, arguments []Expression) nullInfo {
	switch {
	case library == "file" && fileReads[member]:
		return nullInfo{kind: nullMaybe, origin: written + "(…)", reason: "trả null khi không có file"}
	case library == "storage" && storageMakers[member]:
		return nullInfo{kind: nullStorage}
	case library == "database" && member == "connection":
		return nullInfo{kind: nullDatabase}
	}
	return nullInfo{}
}

// ---- uses ----

// use is e read where JavaScript throws when it is null: the compile error when e may be null, unchecked.
func (n *nullChecker) use(e Expression) {
	if n.err != nil {
		return
	}
	info := n.classify(e)
	if info.kind != nullMaybe {
		return
	}
	shown := info.origin
	switch x := e.(type) {
	case *Identifier:
		shown = x.Value
	case *MemberExpression:
		if name, ok := x.Object.(*Identifier); ok {
			shown = name.Value + "." + x.Property.Value
		}
	}
	location := locationText(n.compiler.getSourceLocation(getNodeSource(e), startPosition(e)))
	how := "if (" + shown + " == null) return …"
	if shown == info.origin {
		how = "giữ nó trong một const rồi kiểm, if (x == null) return …"
	}
	n.err = fmt.Errorf("%s: %s có thể là null: %s %s; kiểm trước khi đọc (%s), hay viết %s?.…", location, shown,
		info.origin, info.reason, how, shown)
}

// ---- checks ----

// keyOf is the key a check of e proves: a name, or the error of a name safe(…) gave.
func (n *nullChecker) keyOf(e Expression) (nullKey, bool) {
	switch x := e.(type) {
	case *Identifier:
		if symbol := n.lookup(x.Value); symbol != nil {
			return nullKey{symbol: symbol}, true
		}
	case *MemberExpression:
		if name, ok := x.Object.(*Identifier); ok && !x.Optional && x.Property.Value == "error" {
			if symbol := n.lookup(name.Value); symbol != nil && symbol.info.kind == nullSafe {
				return nullKey{symbol: symbol, field: "error"}, true
			}
		}
	}
	return nullKey{}, false
}

// isNullLiteral reports null, or undefined, written.
func isNullLiteral(e Expression) bool {
	literal, ok := e.(*Literal)
	return ok && literal.Value.IsNullish()
}

// narrow is what cond proves not null where it is truthy (when) or falsy.
func (n *nullChecker) narrow(cond Expression, when bool) []nullKey {
	switch x := cond.(type) {
	case *Identifier, *MemberExpression:
		if key, ok := n.keyOf(x); ok && when {
			return []nullKey{key}
		}
	case *PrefixExpression:
		if x.Operator == "!" {
			return n.narrow(x.Right, !when)
		}
	case *InfixExpression:
		switch x.Operator {
		case "&&":
			if when {
				return append(n.narrow(x.Left, true), n.narrow(x.Right, true)...)
			}
		case "||":
			if !when {
				return append(n.narrow(x.Left, false), n.narrow(x.Right, false)...)
			}
		case "!=", "!==", "==", "===":
			other := x.Left
			if isNullLiteral(x.Left) {
				other = x.Right
			} else if !isNullLiteral(x.Right) {
				return nil
			}
			strict := x.Operator == "!==" || x.Operator == "==="
			if strict && !isNullLiteralNull(x.Left, x.Right) {
				return nil // === undefined does not rule out null
			}
			notEqual := x.Operator == "!=" || x.Operator == "!=="
			if key, ok := n.keyOf(other); ok && when == notEqual {
				return []nullKey{key}
			}
		}
	}
	return nil
}

// isNullLiteralNull reports that the null literal of a comparison is null, not undefined.
func isNullLiteralNull(left, right Expression) bool {
	for _, side := range []Expression{left, right} {
		if literal, ok := side.(*Literal); ok && literal.Value.IsNull() {
			return true
		}
	}
	return false
}

// with walks f with keys checked, then puts the checks back as they were.
func (n *nullChecker) with(keys []nullKey, f func()) {
	saved := n.copyChecked()
	for _, key := range keys {
		n.checked[key] = true
	}
	f()
	n.checked = saved
}

// ---- statements ----

// statements walks a list; it reports whether the list always leaves (return, break, continue).
func (n *nullChecker) statements(list []Statement) bool {
	for _, statement := range list {
		if n.err != nil {
			return false
		}
		if n.statement(statement) {
			return true
		}
	}
	return false
}

func (n *nullChecker) block(b *BlockStatement) bool {
	if b == nil {
		return false
	}
	n.push()
	defer n.pop()
	return n.statements(b.Statements)
}

func (n *nullChecker) statement(s Statement) bool {
	switch x := s.(type) {
	case *VarStatement:
		n.declare(x)
	case *ExpressionStatement:
		if branch, ok := x.Expression.(*IfExpression); ok {
			return n.ifStatement(branch)
		}
		n.expression(x.Expression)
	case *BlockStatement:
		return n.block(x)
	case *GroupStatement:
		return n.statements(x.Statements)
	case *ReturnStatement:
		if x.ReturnValue != nil {
			n.expression(x.ReturnValue)
		}
		return true
	case *BreakStatement, *ContinueStatement:
		return true
	case *SwitchStatement:
		n.expression(x.Discriminant)
		entry := n.copyChecked()
		after := n.copyChecked()
		for _, each := range x.Cases {
			n.checked = n.copyOf(entry)
			if each.Test != nil {
				n.expression(each.Test)
			}
			n.push()
			n.statements(each.Statements)
			n.pop()
			after = intersectChecked(after, n.checked)
		}
		n.checked = after
	case *ForStatement:
		n.use(x.Iterable)
		n.expression(x.Iterable)
		n.loop(x.Body, func() {
			if x.Item != nil {
				n.define(x.Item.Value, nullInfo{}, x.Const)
			}
		})
	case *ForRangeStatement:
		n.push()
		if x.Init != nil {
			n.statement(x.Init)
		}
		n.loop(x.Body, func() {
			if x.Cond != nil {
				n.expression(x.Cond)
			}
			if x.Update != nil {
				n.expression(x.Update)
			}
		})
		n.pop()
	case *DeferStatement:
		n.expression(x.Fn)
	case *SpawnStatement:
		n.expression(x.Fn)
	}
	return false
}

func (n *nullChecker) copyOf(checked map[nullKey]bool) map[nullKey]bool {
	copied := make(map[nullKey]bool, len(checked))
	for key := range checked {
		copied[key] = true
	}
	return copied
}

// loop walks a loop's body, which runs any number of times: a let the body may make null is not checked at its start,
// and what holds after the loop is what holds both before and after the body.
func (n *nullChecker) loop(body *BlockStatement, head func()) {
	for symbol, info := range n.assignedMaybe(body) {
		// a later turn reads what an earlier one assigned: the let may be null from the body's start
		symbol.info = info
		delete(n.checked, nullKey{symbol: symbol})
	}
	entry := n.copyChecked()
	n.push()
	head()
	n.block(body)
	n.pop()
	n.checked = intersectChecked(entry, n.checked)
}

// assignedMaybe are the lets a block assigns a value that may be null, in it or in a block inside it, with what that
// value is.
func (n *nullChecker) assignedMaybe(body *BlockStatement) map[*nullSymbol]nullInfo {
	found := map[*nullSymbol]nullInfo{}
	if body == nil {
		return found
	}
	var visit func(node any)
	visit = func(node any) {
		switch x := node.(type) {
		case *BlockStatement:
			if x == nil { // an if with no else
				return
			}
			for _, s := range x.Statements {
				visit(s)
			}
		case *ExpressionStatement:
			visit(x.Expression)
		case *IfExpression:
			visit(x.Consequence)
			visit(x.Alternative)
		case *AssignmentExpression:
			if name, ok := x.Name.(*Identifier); ok {
				if symbol := n.lookup(name.Value); symbol != nil && !symbol.constant {
					if info := n.classify(x.Value); info.kind == nullMaybe {
						found[symbol] = info
					}
				}
			}
		}
	}
	visit(body)
	return found
}

// ifStatement walks an if: its branches with what its condition proves, and after it what holds on every way out.
func (n *nullChecker) ifStatement(x *IfExpression) bool {
	n.expression(x.Condition)
	entry := n.copyChecked()
	for _, key := range n.narrow(x.Condition, true) {
		n.checked[key] = true
	}
	thenLeaves := n.block(x.Consequence)
	thenEnd := n.checked
	n.checked = n.copyOf(entry)
	for _, key := range n.narrow(x.Condition, false) {
		n.checked[key] = true
	}
	elseLeaves := false
	if x.Alternative != nil {
		elseLeaves = n.block(x.Alternative)
	}
	elseEnd := n.checked
	switch {
	case thenLeaves && elseLeaves:
		n.checked = entry
		return true
	case thenLeaves:
		n.checked = elseEnd
	case elseLeaves:
		n.checked = thenEnd
	default:
		n.checked = intersectChecked(thenEnd, elseEnd)
	}
	return false
}

// declare walks a const or a let and defines what it declares.
func (n *nullChecker) declare(x *VarStatement) {
	constant := x.Token.Kind == Const // a let and a var change
	if x.Value != nil {
		if function, ok := x.Value.(*FunctionLiteral); ok && x.Function {
			// a function declaration is hoisted: it may run before any check of the file, so it inherits none
			for _, name := range x.Names {
				n.define(name.Value, nullInfo{}, false)
			}
			n.function(function, nil, false)
			return
		}
		if x.Pattern != nil {
			n.use(x.Value)
		}
		n.expression(x.Value)
	}
	info := nullInfo{}
	if x.Value != nil {
		info = n.classify(x.Value)
	}
	switch {
	case x.Pattern == nil && len(x.Names) == 1 && x.DestructMode == DestructNone:
		n.define(x.Names[0].Value, info, constant)
	case x.Pattern == nil && info.kind == nullLibrary:
		// import { memory } from "kitwork/storage", lowered to const { memory } = kitwork.storage
		for _, name := range x.Names {
			n.define(name.Value, nullInfo{kind: nullMember, library: info.library, member: name.Value}, constant)
		}
	case x.Pattern != nil && info.kind == nullSafe:
		// const { value, error } = safe(…): error may be null
		for _, element := range x.Pattern.Elements {
			if element.Name != nil && element.Key == "error" && element.Default == nil {
				n.define(element.Name.Value, nullInfo{kind: nullMaybe, origin: "safe(…).error", reason: "là null khi hàm chạy xong"}, constant)
			}
		}
		for _, name := range x.Names {
			if n.scope.names[name.Value] == nil {
				n.define(name.Value, nullInfo{}, constant)
			}
		}
	default:
		for _, name := range x.Names {
			n.define(name.Value, nullInfo{}, constant)
		}
	}
	if x.Pattern != nil {
		n.patternDefaults(x.Pattern)
	}
}

func (n *nullChecker) patternDefaults(pattern *BindingPattern) {
	for _, element := range pattern.Elements {
		if element.Default != nil {
			n.expression(element.Default)
		}
		if element.Pattern != nil {
			n.patternDefaults(element.Pattern)
		}
	}
}

// ---- expressions ----

func (n *nullChecker) expression(e Expression) {
	if n.err != nil || e == nil {
		return
	}
	switch x := e.(type) {
	case *MemberExpression:
		if !x.Optional {
			n.use(x.Object)
		}
		n.expression(x.Object)
	case *IndexExpression:
		if !x.Optional {
			n.use(x.Left)
		}
		n.expression(x.Left)
		n.expression(x.Index)
	case *ChainExpression:
		n.expression(x.Expression)
	case *MethodCallExpression:
		if !x.Optional {
			n.use(x.Object)
		}
		n.expression(x.Object)
		door := false
		if name, ok := x.Object.(*Identifier); ok {
			if info := n.classify(name); info.kind == nullLibrary && info.library == "router" {
				door = true // router.get((context) => …): the first parameter is the request's context
			}
		}
		n.arguments(x.Arguments, door)
	case *CallExpression:
		n.expression(x.Function)
		n.arguments(x.Arguments, false)
	case *InfixExpression:
		n.expression(x.Left)
		switch x.Operator {
		case "&&":
			n.with(n.narrow(x.Left, true), func() { n.expression(x.Right) })
		case "||":
			n.with(n.narrow(x.Left, false), func() { n.expression(x.Right) })
		default:
			n.expression(x.Right)
		}
	case *PrefixExpression:
		n.expression(x.Right)
	case *TernaryExpression:
		n.expression(x.Condition)
		n.with(n.narrow(x.Condition, true), func() { n.expression(x.Consequence) })
		n.with(n.narrow(x.Condition, false), func() { n.expression(x.Alternative) })
	case *AssignmentExpression:
		n.assign(x)
	case *ArrayLiteral:
		for _, element := range x.Elements {
			if spread, ok := element.(*SpreadExpression); ok {
				n.use(spread.Value)
			}
			n.expression(element)
		}
	case *ObjectLiteral:
		for _, entry := range x.Entries {
			if !entry.IsSpread {
				n.expression(entry.Key)
			}
			n.expression(entry.Value)
		}
	case *SpreadExpression:
		n.expression(x.Value)
	case *TemplateLiteral:
		for _, part := range x.Parts {
			n.expression(part)
		}
	case *FunctionLiteral:
		n.function(x, nil, true)
	case *IfExpression:
		n.ifStatement(x)
	}
}

// arguments walks a call's arguments: a spread of one that may be null is a use; a router door's function gets the
// request's context as its first parameter.
func (n *nullChecker) arguments(arguments []Expression, door bool) {
	for _, argument := range arguments {
		if spread, ok := argument.(*SpreadExpression); ok {
			n.use(spread.Value)
		}
		if function, ok := argument.(*FunctionLiteral); ok && door {
			n.function(function, &nullInfo{kind: nullContext}, true)
			continue
		}
		n.expression(argument)
	}
}

// assign walks an assignment: to a member, a use of its object; to a let, what it may now be.
func (n *nullChecker) assign(x *AssignmentExpression) {
	switch target := x.Name.(type) {
	case *MemberExpression:
		n.use(target.Object)
		n.expression(target.Object)
	case *IndexExpression:
		n.use(target.Left)
		n.expression(target.Left)
		n.expression(target.Index)
	}
	if right, shared := x.sharedTarget(); shared {
		// x op= y, x++: the value's left side is the target, the node just walked; walked again, a nest of
		// x[x[…] += 1] += 1 cost twice as much at each level (as the resolver found, 05/11/2026)
		n.expression(right)
	} else {
		n.expression(x.Value)
	}
	name, ok := x.Name.(*Identifier)
	if !ok {
		return
	}
	symbol := n.lookup(name.Value)
	if symbol == nil || symbol.constant {
		return
	}
	info := n.classify(x.Value)
	if info.kind == nullMaybe {
		symbol.info = info
		delete(n.checked, nullKey{symbol: symbol})
		return
	}
	n.checked[nullKey{symbol: symbol}] = true
}

// function walks a function written here: a const the path checked before it stays checked inside (a const does not
// change), a let does not (the function may run after the let changed); first, when set, is its first parameter's.
func (n *nullChecker) function(f *FunctionLiteral, first *nullInfo, inherit bool) {
	saved := n.checked
	inside := map[nullKey]bool{}
	if inherit {
		for key := range saved {
			if key.symbol.constant {
				inside[key] = true
			}
		}
	}
	n.checked = inside
	n.push()
	for i, parameter := range f.Parameters {
		info := nullInfo{}
		if i == 0 && first != nil {
			info = *first
		}
		n.define(parameter.Value, info, false)
	}
	if f.Body != nil {
		n.statements(f.Body.Statements)
	}
	n.pop()
	n.checked = saved
}
