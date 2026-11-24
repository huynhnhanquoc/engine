package compiler

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/runtime"
)

// Block scope (14/10/2026): const and let live in the block that declares them, as in JavaScript.
//
// The VM keeps the variables of a running function in one map, by name. LOAD reads that map, then the
// maps of the functions around it (the closure chain), then the program's; STORE writes the first map
// of the chain that has the name, else the function's own. Before this, two variables of one name met
// in one map: a const in an if overwrote the function's variable of that name, a const in a nested
// function wrote through to its parent's (even to a file's top-level variable), and every closure made
// in a loop saw the loop's last value.
//
// The compiler now resolves every name before it emits code, and gives each variable the name it lives
// under, its slot name (runtime.SlotName reads the variable back):
//
//   - a function's own declarations and its parameters keep their names; a declaration of the function's
//     own scope whose name a function around it may hold becomes name@N;
//   - a declaration in a block (if, else, a loop's head or body, a switch) becomes name#N;
//   - a name nothing declares stays as written and keeps the VM's lookup: var x = … (an assignment, the
//     language has no var keyword), a global of the host.
//
// Since bytecode 7 (04/11/2026) a function's variables and the top level's block variables live in
// slots by index (slots.go): their slot names name the slots for a stack trace and a tool, and key the
// variables of an iteration's object. The top level's own variables keep their names in vm.Vars.
//
// A source never spells @ or #, so a slot is never another variable. Declaring a name twice in one
// block is a compile error, as in JavaScript.
//
// A closure made in a loop sees the variables of its own iteration, as in JavaScript. A variable of an
// iteration that some closure reads or writes lives in an object the loop makes for each iteration (its
// environment, slot #N): the loop and the function read and write it there, and the closure is made
// inside a small function whose parameters are those objects, so it keeps the ones of the iteration it
// was made in. A counting loop whose counter is such a variable copies it into the next iteration's
// object before its update, as JavaScript does. Variables no closure reaches stay in the function's map.

type bindingKind uint8

const (
	bindingConst bindingKind = iota
	bindingLet
	bindingParameter
	bindingItem    // for (const x of …)
	bindingCounter // for (let i = 0; …)
)

// binding is one declared variable.
type binding struct {
	name  string
	kind  bindingKind
	token Token
	scope *lexicalScope
	// slot is the name LOAD and STORE use for it.
	slot string
	// loop is the innermost loop of its function that holds its declaration; nil when none.
	loop *loopScope
	// closures lists the functions directly inside its own that read or write it (a deeper one reaches
	// it through them).
	closures []*FunctionLiteral
	// written: assigned after its declaration.
	written bool
	// environment: it lives in its loop's environment object (a closure reaches it).
	environment bool
	// constant: declared with const (a function declaration aside), so never assigned (constantWritten).
	constant bool
	// hoisted: function f() {…}, whose value is made as its block begins (functionsFirst), so a read before its line
	// reads it, as in JavaScript; any other const or let read there is in its temporal dead zone (beforeDeclared)
	hoisted bool
	// level and index are the frame and the slot the variable lives in (slots.go), given as the code of
	// its function starts; level is nil for a variable kept by name (the top level's own) or in an
	// iteration's object.
	level *slotLevel
	index int
	// parameter is the place, from 1, of the parameter that declares it: the parameter itself, or a name of its
	// destructuring; 0 for any other variable. A default reads only the parameters before its own (defaultFor).
	parameter int
	// initializing: its declaration's value is being resolved, where a read of it in its own function is in its
	// temporal dead zone (let x = x); clause is the place, from 1, of the case of a switch that declares it, 0 out of
	// one: a later case reads it before it is made unless the cases fall through (reference).
	initializing bool
	clause       int
}

type lexicalScope struct {
	parent   *lexicalScope
	function *functionScope
	// block is false for the scope of a function's parameters and body, and for the program's top level.
	block bool
	// loop is the innermost loop of its function around it; nil when none.
	loop  *loopScope
	names map[string]*binding
	order []*binding
	// owner is the node whose code begins the scope, where the compiler marks its holes: the Program, a function's
	// body, a block, a switch, a loop.
	owner any
}

type functionScope struct {
	parent  *functionScope
	literal *FunctionLiteral // nil for the program
	scope   *lexicalScope
	// written lists the names this function assigns without any declaration around them (var x = …).
	written map[string]bool
	// held lists the names this function's map may hold under their own spelling.
	held map[string]bool
	// bindings lists the variables the function declares, its parameters first, then those of its body
	// and its blocks in source order (listBindings); loops lists its loops whose iterations make an
	// object (assignEnvironments). Their slots follow that order (slots.go).
	bindings []*binding
	loops    []*loopScope
	// index is the function's place in resolution.functions.
	index int
}

type loopScope struct {
	parent   *loopScope
	function *functionScope
	// environment is the slot of the object each iteration makes; "" when no closure needs one.
	environment string
}

// resolution is what the resolver hands the code generator.
type resolution struct {
	slots map[*Identifier]*binding
	// loops maps a for statement to its loop.
	loops map[Statement]*loopScope
	// environments lists, for a closure made in a loop, the environment slots it is handed as it is made.
	environments map[*FunctionLiteral][]string
	// program is the top level's scope, and functionOf each function literal's.
	program    *functionScope
	functionOf map[*FunctionLiteral]*functionScope
	// temps gives a write of a variable of an iteration's object from a closure a slot of its own to
	// pass the value through (a slot of the closure's map, never one of the loop's function).
	temps map[*Identifier]string
	// all bindings and functions, in source order, for tests and scans.
	bindings  []*binding
	functions []*functionScope
	// declaresSafe: the program declares or assigns a name safe somewhere (checkSafeCall); declaresFail, a name
	// fail (checkFailCall).
	declaresSafe bool
	declaresFail bool
	// builtinReads gives each read of a builtin's name that nothing declares the function it is read in,
	// so the compiler can tell whether a function on its way out assigns the name (builtin.go). nil when
	// the program reads no builtin.
	builtinReads map[*Identifier]*functionScope
	// freeReads are the names the program reads that it does not declare and that are no builtin, each once,
	// in the order first read: what an expression of a .ui file reads of its page (CompileExpression).
	freeReads []string
	// undeclared are the names a program reads that nothing declares and no builtin is, and the names it assigns that
	// nothing declares, each where it is first met: a file's compile error (undeclaredName, 15/11/2026); what an
	// expression of a .ui file reads of its page is no file's. typeof x of such a name is not one, as JavaScript gives
	// it "undefined".
	undeclared []undeclaredName
	// once gives an update or a compound assignment of a member or an item whose object or key does
	// more than read (a[i++]++, get().n += 1) the slots they are kept in, so each runs once
	// (evaluateOnce, 15/10/2026).
	once map[*AssignmentExpression]onceSlots
	// hoisted are the function declarations, made as their block begins; holes lists, by the node that begins a
	// scope (lexicalScope.owner), its let and const variables a function made before their line may use, which the
	// compiler marks not yet made there (UNSET_SLOT, UNSET_NAME, 18/11/2026).
	hoisted map[*FunctionLiteral]bool
	holes   map[any][]*binding
}

// undeclaredName is a name a program reads or assigns that nothing declares (resolution.undeclared).
type undeclaredName struct {
	name  *Identifier
	write bool
}

// onceSlots are the slots of an assignment's object and key; "" for one that is compiled where it is.
type onceSlots struct {
	object, key string
}

type resolver struct {
	compiler *Compiler
	function *functionScope
	scope    *lexicalScope
	loop     *loopScope
	result   *resolution
	next     int
	err      error
	// foreignWrites lists the names written from a function other than the one declaring them.
	foreignWrites []*Identifier
	// onces lists, in source order, the assignments that get onceSlots once every other slot is given.
	onces []*AssignmentExpression
	// views are the functions of router.view(…) and component.define(…), where a var may stand (var.go)
	views map[*FunctionLiteral]bool
	// typeofOperand is the name typeof is asked of, which JavaScript reads undeclared as "undefined"
	typeofOperand *Identifier
	// parameterIndex is the place, from 1, of the parameter whose default and destructuring are resolved, 0 out of
	// a function's parameters; defaultFor is it while a default value is resolved (functionLiteral).
	parameterIndex int
	defaultFor     int
	// clauses gives each switch being resolved the place, from 1, of its case being resolved.
	clauses map[*lexicalScope]int
}

// resolveProgram resolves every name of a program and gives each variable its slot.
func (c *Compiler) resolveProgram(program *Program) (*resolution, error) {
	r := &resolver{
		compiler: c,
		result: &resolution{
			slots:        map[*Identifier]*binding{},
			loops:        map[Statement]*loopScope{},
			environments: map[*FunctionLiteral][]string{},
			functionOf:   map[*FunctionLiteral]*functionScope{},
			temps:        map[*Identifier]string{},
			once:         map[*AssignmentExpression]onceSlots{},
			hoisted:      map[*FunctionLiteral]bool{},
			holes:        map[any][]*binding{},
		},
		clauses: map[*lexicalScope]int{},
	}
	r.views = viewFunctions(program)
	r.enterFunction(nil)
	r.scope.owner = program
	r.result.program = r.function
	r.hoist(program.Statements)
	for _, statement := range program.Statements {
		r.statement(statement)
	}
	r.leaveFunction()
	if r.err != nil {
		return nil, r.err
	}
	r.assignSlots()
	r.assignEnvironments()
	if r.assignHoles(); r.err != nil {
		return nil, r.err
	}
	r.listBindings()
	r.assignOnce()
	r.result.declaresSafe = declaresName(r.result, "safe")
	r.result.declaresFail = declaresName(r.result, "fail")
	return r.result, nil
}

// declaresName reports whether a program declares name anywhere (a const, a let, a parameter, a
// function, an import, a loop variable) or assigns it without a declaration (var name = …).
func declaresName(result *resolution, name string) bool {
	for _, declared := range result.bindings {
		if declared.name == name {
			return true
		}
	}
	for _, function := range result.functions {
		if function.written[name] {
			return true
		}
	}
	return false
}

func (r *resolver) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// where is a token's place as file:line:col (line:col when the source has no name).
func (r *resolver) where(token Token) string {
	location := r.compiler.getSourceLocation(token.Source, token.Position)
	if location.Line == 0 {
		return fmt.Sprintf("pos %d", token.Position)
	}
	if location.File == "" {
		return fmt.Sprintf("%d:%d", location.Line, location.Column)
	}
	return fmt.Sprintf("%s:%d:%d", location.File, location.Line, location.Column)
}

func (r *resolver) enterFunction(literal *FunctionLiteral) {
	function := &functionScope{parent: r.function, literal: literal, written: map[string]bool{}, index: len(r.result.functions)}
	function.scope = &lexicalScope{parent: r.scope, function: function, names: map[string]*binding{}}
	if literal != nil {
		function.scope.owner = literal.Body
	}
	r.result.functions = append(r.result.functions, function)
	if literal != nil {
		r.result.functionOf[literal] = function
	}
	r.function = function
	r.scope = function.scope
}

func (r *resolver) leaveFunction() {
	r.scope = r.function.scope.parent
	r.function = r.function.parent
}

func (r *resolver) enterBlock() {
	r.scope = &lexicalScope{parent: r.scope, function: r.function, block: true, loop: r.loop, names: map[string]*binding{}}
}

func (r *resolver) leaveBlock() {
	r.scope = r.scope.parent
}

// hoist declares the variables a list of statements declares, before any of them is read: a function
// may call one declared after it, as in JavaScript.
func (r *resolver) hoist(statements []Statement) {
	for _, statement := range statements {
		switch node := statement.(type) {
		case *VarStatement:
			kind := bindingConst
			if node.Token.Kind == Let || node.Token.Kind == Var {
				kind = bindingLet // a var is the let its context fills (var.go)
			}
			for _, name := range node.Names {
				declared := r.declare(name, kind)
				if declared != nil && node.Function {
					declared.constant, declared.hoisted = false, true
					if literal, ok := node.Value.(*FunctionLiteral); ok {
						r.result.hoisted[literal] = true
					}
				}
				if declared != nil {
					declared.parameter = r.parameterIndex
				}
			}
		case *GroupStatement:
			r.hoist(node.Statements)
		}
	}
}

// declare adds a variable to the current scope. A name declared twice in one scope is the compile
// error JavaScript gives.
func (r *resolver) declare(name *Identifier, kind bindingKind) *binding {
	if name == nil {
		return nil
	}
	if name.Value == kitworkName {
		r.fail(r.kitworkShadowed(name))
		return nil
	}
	if strictReserved[name.Value] {
		r.fail(fmt.Errorf("%s: %s là tên mà JavaScript giữ riêng trong một module (strict), không khai được: đặt tên khác", r.where(name.Token), name.Value))
		return nil
	}
	if first, ok := r.scope.names[name.Value]; ok {
		r.fail(r.redeclared(name, kind, first))
		return nil
	}
	declared := &binding{name: name.Value, kind: kind, token: name.Token, scope: r.scope, loop: r.loop, constant: kind == bindingConst}
	r.scope.names[name.Value] = declared
	r.scope.order = append(r.scope.order, declared)
	r.result.bindings = append(r.result.bindings, declared)
	r.result.slots[name] = declared
	return declared
}

func (r *resolver) redeclared(name *Identifier, kind bindingKind, first *binding) error {
	if first.kind == bindingParameter && kind == bindingParameter {
		return fmt.Errorf("%s: hàm có hai tham số cùng tên %s (tham số kia ở %s): đặt tên khác cho một tham số",
			r.where(name.Token), name.Value, r.where(first.token))
	}
	if first.kind == bindingParameter {
		return fmt.Errorf("%s: %s đã là tham số của hàm (%s): đặt tên khác cho biến, hoặc gán %s = … nếu muốn đổi giá trị của tham số",
			r.where(name.Token), name.Value, r.where(first.token), name.Value)
	}
	return fmt.Errorf("%s: %s đã được khai báo trong cùng khối ở %s; một tên chỉ khai báo một lần trong một khối, như JavaScript: đặt tên khác (const { error: saveError } = …), hoặc khai báo một lần bằng let rồi gán %s = …",
		r.where(name.Token), name.Value, r.where(first.token), name.Value)
}

// strictReserved are the names a JavaScript module (strict) never binds (18/11/2026, docs/language.md mục 3.2): the
// lexer reads them as names, and const this = 1, (yield) => …, function package() {} compiled.
var strictReserved = map[string]bool{
	"this": true, "super": true, "yield": true, "await": true, "enum": true, "static": true, "implements": true,
	"interface": true, "package": true, "private": true, "protected": true, "public": true, "eval": true,
	"arguments": true,
}

// kitworkName is Kitwork's own name: every module is a member of it (import * as http from
// "kitwork/http" is const http = kitwork.http), and the compiler reads it as BUILTIN 0 wherever it is
// written, so a variable of that name would never be read.
const kitworkName = "kitwork"

// kitworkShadowed is the error of declaring kitwork (a const, a let, a parameter, a function, a loop
// variable, a name of a pattern) or assigning it (03/11/2026): until then const kitwork = 5 compiled and
// was silently ignored, every later kitwork still the app.
func (r *resolver) kitworkShadowed(name *Identifier) error {
	return fmt.Errorf("%s: kitwork là tên của chính Kitwork (mỗi thư viện là kitwork.<tên>), không khai báo hay gán lại được: đặt tên khác",
		r.where(name.Token))
}

// constantWritten is the error of a write to a const: JavaScript throws it when the write runs
// (TypeError: Assignment to constant variable), Kitwork refuses the program when it compiles
// (14/10/2026; the write happened and the program went on).
func (r *resolver) constantWritten(name *Identifier, declared *binding) error {
	what := "hằng (const)"
	switch declared.kind {
	case bindingItem:
		what = "biến của for (const … of …)"
	case bindingCounter:
		what = "biến đếm khai báo bằng const"
	}
	return fmt.Errorf("%s: %s là %s, khai báo ở %s, không gán lại được, như JavaScript: khai báo bằng let nếu cần đổi giá trị",
		r.where(name.Token), name.Value, what, r.where(declared.token))
}

func (r *resolver) lookup(name string) *binding {
	for scope := r.scope; scope != nil; scope = scope.parent {
		if found, ok := scope.names[name]; ok {
			return found
		}
	}
	return nil
}

// closureUnder is the function literal directly inside function that holds the code being read.
func (r *resolver) closureUnder(function *functionScope) *FunctionLiteral {
	for inner := r.function; inner != nil; inner = inner.parent {
		if inner.parent == function {
			return inner.literal
		}
	}
	return nil
}

func (r *resolver) reference(name *Identifier, write bool) {
	if name == nil {
		return
	}
	if name.Value == kitworkName {
		if write {
			r.fail(r.kitworkShadowed(name))
		}
		return // BUILTIN 0, not a variable
	}
	found := r.lookup(name.Value)
	if found == nil {
		if write || name != r.typeofOperand {
			if _, builtin := builtins.Slot(name.Value); write || !builtin {
				r.undeclaredAt(name, write)
			}
		}
		if write {
			r.function.written[name.Value] = true
		} else if _, builtin := builtins.Slot(name.Value); builtin {
			if r.result.builtinReads == nil {
				r.result.builtinReads = map[*Identifier]*functionScope{}
			}
			r.result.builtinReads[name] = r.function
		} else if !slices.Contains(r.result.freeReads, name.Value) {
			r.result.freeReads = append(r.result.freeReads, name.Value)
		}
		return
	}
	if previous, ok := r.result.slots[name]; ok && previous != found {
		r.fail(fmt.Errorf("%s: compiler: tên %s trỏ tới hai biến", r.where(name.Token), name.Value))
		return
	}
	r.result.slots[name] = found
	if found.initializing && found.scope.function == r.function {
		r.fail(r.readInItsDeclaration(name))
		return
	}
	if found.clause > 0 && !found.hoisted && r.clauses[found.scope] > found.clause {
		r.fail(r.readInALaterCase(name, found))
		return
	}
	if r.defaultFor > 0 && found.scope.function == r.function && found.parameter > 0 &&
		(found.parameter > r.defaultFor || found.parameter == r.defaultFor && found.kind == bindingParameter) {
		r.fail(r.parameterNotYet(name))
		return
	}
	if found.scope.function == r.function && !found.hoisted && (found.kind == bindingConst || found.kind == bindingLet) &&
		name.Token.Source == found.token.Source && name.Token.Position < found.token.Position {
		// read or written before its line, in the function that declares it: JavaScript throws there
		r.fail(r.beforeDeclared(name, found))
		return
	}
	if write && found.scope.function != r.function {
		r.foreignWrites = append(r.foreignWrites, name)
	}
	if found.scope.function != r.function {
		if closure := r.closureUnder(found.scope.function); closure != nil {
			known := false
			for _, seen := range found.closures {
				if seen == closure {
					known = true
					break
				}
			}
			if !known {
				found.closures = append(found.closures, closure)
			}
		}
	}
	if write {
		if found.constant {
			r.fail(r.constantWritten(name, found))
			return
		}
		found.written = true
	}
}

// beforeDeclared is the compile error of a const or a let used before its line in the function that declares it
// (15/11/2026, round 3 of the 13/11 review): JavaScript throws ReferenceError there, its temporal dead zone, and
// Kitwork read undefined, or in a loop the value of the turn before. A function made before the line reads it when it
// is called, as JavaScript allows.
func (r *resolver) beforeDeclared(name *Identifier, declared *binding) error {
	return fmt.Errorf("%s: %s được dùng trước dòng khai nó (%s): JavaScript ném ReferenceError ở đây; dời dòng khai lên trước, hoặc dùng nó trong một hàm được gọi sau dòng ấy",
		r.where(name.Token), name.Value, r.where(declared.token))
}

// letPlace is the compile error of a let where a file's code has none (18/11/2026, docs/language.md mục 2.1; it was
// decided 27/10/2026): at a file's top level, a name that changes lives through every request, of every user, so it is
// const there, as var is refused there; at the top level of a router.view(…) or component.define(…) function, the
// screen's state is var and the rest const. nil anywhere else, and for a source compiled alone (a test's snippet),
// whose top level runs once.
func (r *resolver) letPlace(node *VarStatement) error {
	if !r.compiler.declaredOnly || r.scope != r.function.scope || len(node.Names) == 0 {
		return nil
	}
	name := node.Names[0].Value
	switch literal := r.function.literal; {
	case literal == nil || literal.DebugName == moduleFunctionName:
		return fmt.Errorf("%s: let %s ở cấp cao nhất của file: tên đổi được ở cấp file sống qua mọi request, của mọi người dùng; ở đây viết const (docs/language.md mục 2)",
			r.where(node.Token), name)
	case r.views[literal]:
		return fmt.Errorf("%s: let %s ở cấp cao nhất của hàm router.view(…) hay component.define(…): trạng thái của màn hình viết var, giá trị không đổi viết const (docs/language.md mục 2)",
			r.where(node.Token), name)
	}
	return nil
}

// errorAlone is the compile error of error(…) standing alone in a function, not returned (18/11/2026, docs/language.md
// mục 5.1; it was decided 27/10/2026): it stops the call as a return of it does, which the line does not show, so the
// function is written return error(…). A file's top level has no return: error(…) stands alone there, and stops the
// boot. nil for any other statement, and for an error a program declares itself.
func (r *resolver) errorAlone(node *ExpressionStatement) error {
	call, ok := node.Expression.(*CallExpression)
	if !ok {
		return nil
	}
	name, ok := call.Function.(*Identifier)
	if !ok || name.Value != "error" || r.lookup("error") != nil {
		return nil
	}
	if literal := r.function.literal; literal == nil || literal.DebugName == moduleFunctionName {
		return nil
	}
	return fmt.Errorf("%s: error(…) đứng riêng trong một hàm: viết return error(…); lỗi đi lên như nhau, return cho thấy hàm dừng ở đây (docs/language.md mục 5.1)",
		r.where(name.Token))
}

// readInItsDeclaration is the compile error of a let or a const read in its own declaration, let x = x (18/11/2026):
// JavaScript throws ReferenceError there, and Kitwork read undefined.
func (r *resolver) readInItsDeclaration(name *Identifier) error {
	return fmt.Errorf("%s: %s được đọc trong chính dòng khai nó: nó chưa có cho tới khi dòng ấy chạy xong; JavaScript ném ReferenceError ở đây",
		r.where(name.Token), name.Value)
}

// readInALaterCase is the compile error of a let or a const of a case of a switch used in a later case, or by a function
// made there (18/11/2026): the later case runs it before it is made unless the cases fall through, where JavaScript
// throws ReferenceError, and Kitwork read undefined.
func (r *resolver) readInALaterCase(name *Identifier, declared *binding) error {
	return fmt.Errorf("%s: %s khai trong một case trước (%s): một case sau dùng nó khi case khai nó không chạy thì JavaScript ném ReferenceError; khai nó trước switch, hay bọc mỗi case trong { } với tên của riêng nó",
		r.where(name.Token), name.Value, r.where(declared.token))
}

// assignHoles lists the let and const variables a function made before their line may use (18/11/2026, decided
// 27/10/2026): one read or written by a function declared in their scope, which is made as the scope begins, or by
// a function written before their line. The compiler marks each not yet made as its scope begins (UNSET_SLOT,
// UNSET_NAME), so the function meets NOT_INITIALIZED, JavaScript's ReferenceError, where it read undefined. Every
// other variable costs nothing. One kept in a loop's iteration object, which a closure of the iteration reaches, is
// a compile error: the object has no mark.
func (r *resolver) assignHoles() {
	for _, declared := range r.result.bindings {
		if declared.hoisted || declared.parameter > 0 || declared.kind != bindingConst && declared.kind != bindingLet {
			continue
		}
		early := false
		for _, closure := range declared.closures {
			if r.result.hoisted[closure] || closure.Token.Source == declared.token.Source && closure.Token.Position < declared.token.Position {
				early = true
				break
			}
		}
		if !early {
			continue
		}
		if declared.environment {
			r.fail(fmt.Errorf("%s: %s của một lượt vòng lặp được một hàm tạo trước dòng khai nó dùng: Kitwork chưa báo được lúc chạy khi hàm ấy chạy trước dòng khai; dời dòng khai lên trước hàm ấy",
				r.where(declared.token), declared.name))
			return
		}
		r.result.holes[declared.scope.owner] = append(r.result.holes[declared.scope.owner], declared)
	}
}

// prototypeKey is the compile error of constructor or __proto__ read or written by name, o.constructor, o["__proto__"],
// const { constructor } = o (18/11/2026, docs/language.md mục 8.2): JavaScript reads them off the prototype chain, which
// Kitwork has not, so o.constructor was undefined where JavaScript gives Object, and o.__proto__ a plain key. nil for
// any other key.
func (r *resolver) prototypeKey(key string, at Token) error {
	switch key {
	case "constructor":
		return fmt.Errorf("%s: Kitwork không có prototype nên không có constructor: hỏi loại của một giá trị thì typeof x, Array.isArray(x)", r.where(at))
	case "__proto__":
		return fmt.Errorf("%s: Kitwork không có prototype nên __proto__ không đọc, không đặt prototype: object mới từ một object khác thì { ...o, … }", r.where(at))
	}
	return nil
}

// protoEntry is the compile error of __proto__: v in an object literal, which sets the object's prototype in
// JavaScript (a shorthand { __proto__ } and a computed [k] key are plain keys there, and here); nil for any other entry.
func (r *resolver) protoEntry(entry ObjectEntry) error {
	if entry.IsSpread || entry.Key == entry.Value {
		return nil
	}
	switch key := entry.Key.(type) {
	case *Identifier:
		if key.Value == "__proto__" {
			return r.prototypeKey(key.Value, key.Token)
		}
	case *Literal:
		if key.Value.IsString() && key.Value.Text() == "__proto__" {
			return r.prototypeKey("__proto__", key.Token)
		}
	}
	return nil
}

// patternKeys is prototypeKey of each key an object pattern reads, nested ones too.
func (r *resolver) patternKeys(pattern *BindingPattern) error {
	for _, element := range pattern.Elements {
		if !pattern.Array && !element.Rest {
			if problem := r.prototypeKey(element.Key, element.KeyToken); problem != nil {
				return problem
			}
		}
		if element.Pattern != nil {
			if problem := r.patternKeys(element.Pattern); problem != nil {
				return problem
			}
		}
	}
	return nil
}

// dateWithoutNew is the compile error of Date(…) called without new, the builtin's (18/11/2026, docs/language.md mục
// 8.2): JavaScript's Date() leaves its arguments and gives the text of the time now, which code that writes it almost
// never means; Kitwork made the time of its arguments, as new Date(…) does. nil for any other call, and for a Date a
// program declares itself.
func (r *resolver) dateWithoutNew(call *CallExpression) error {
	name, ok := call.Function.(*Identifier)
	if !ok || call.New || name.Value != "Date" || r.lookup("Date") != nil {
		return nil
	}
	return fmt.Errorf("%s: Date(…) không new: JavaScript bỏ các đối số và trả chuỗi của giờ hiện tại; tạo thời gian thì new Date(…), số mili giây của giờ hiện tại thì Date.now()",
		r.where(name.Token))
}

// parameterNotYet is the compile error of a default that reads its own parameter or one after it (18/11/2026):
// JavaScript throws ReferenceError there, the parameter's temporal dead zone, and Kitwork read undefined.
func (r *resolver) parameterNotYet(name *Identifier) error {
	return fmt.Errorf("%s: mặc định của tham số đọc %s, một tham số chưa có lúc ấy (chính nó, hay đứng sau nó): JavaScript ném ReferenceError ở đây; mặc định chỉ đọc tham số đứng trước",
		r.where(name.Token), name.Value)
}

// undeclaredAt notes name, read or assigned with nothing declaring it, once a name (resolution.undeclared).
func (r *resolver) undeclaredAt(name *Identifier, write bool) {
	for _, met := range r.result.undeclared {
		if met.name.Value == name.Value {
			return
		}
	}
	r.result.undeclared = append(r.result.undeclared, undeclaredName{name: name, write: write})
}

// undeclaredError is a file's compile error for a name nothing declares (15/11/2026, the owner's round 3 of the
// 13/11 review: a name read undeclared was undefined and one assigned undeclared made a variable, where JavaScript
// throws ReferenceError, so a misspelt name ran on as nothing).
func (r *resolver) undeclaredError(met undeclaredName) error {
	if met.write {
		return fmt.Errorf("%s: %s = … gán vào một tên chưa khai: khai nó ở chỗ nó bắt đầu, let %s = …; JavaScript (strict) ném ReferenceError ở đây",
			r.where(met.name.Token), met.name.Value, met.name.Value)
	}
	return fmt.Errorf("%s: %s chưa được khai: khai nó bằng const hay let, hay import nó; JavaScript ném ReferenceError ở đây",
		r.where(met.name.Token), met.name.Value)
}

func (r *resolver) statement(statement Statement) {
	if r.err != nil || statement == nil {
		return
	}
	switch node := statement.(type) {
	case *VarStatement:
		if node.Token.Kind == Var {
			if r.varStatement(node); r.err != nil {
				return
			}
		} else if node.Token.Kind == Let && r.parameterIndex == 0 {
			if problem := r.letPlace(node); problem != nil {
				r.fail(problem)
				return
			}
		}
		// its value is resolved before it is made: a read of it there, in its own function, is the compile error of a
		// name read in its own declaration (let x = x); a pattern's own defaults read its earlier names (reference)
		var initializing []*binding
		if !node.Function {
			for _, name := range node.Names {
				if declared := r.scope.names[name.Value]; declared != nil {
					declared.initializing = true
					initializing = append(initializing, declared)
				}
			}
		}
		r.expression(node.Value)
		for _, declared := range initializing {
			declared.initializing = false
		}
		if node.Pattern != nil {
			if problem := r.patternKeys(node.Pattern); problem != nil {
				r.fail(problem)
				return
			}
		}
		if node.Pattern != nil {
			outer := r.defaultFor
			r.defaultFor = r.parameterIndex // a parameter's destructuring: its defaults read only the parameters before
			r.patternDefaults(node.Pattern)
			r.defaultFor = outer
		}
	case *ExpressionStatement:
		if problem := r.errorAlone(node); problem != nil {
			r.fail(problem)
			return
		}
		r.expression(node.Expression)
	case *GroupStatement:
		for _, inner := range node.Statements {
			r.statement(inner)
		}
	case *BlockStatement:
		r.block(node)
	case *ReturnStatement:
		r.expression(node.ReturnValue)
	case *BreakStatement, *ContinueStatement, *ImportStatement:
	case *SwitchStatement:
		r.switchStatement(node)
	case *ForStatement:
		r.forOf(node)
	case *ForRangeStatement:
		r.forRange(node)
	case *DeferStatement:
		r.expression(node.Fn)
	case *SpawnStatement:
		r.expression(node.Fn)
	default:
		if expression, ok := statement.(Expression); ok {
			r.expression(expression)
		}
	}
}

func (r *resolver) patternDefaults(pattern *BindingPattern) {
	for _, element := range pattern.Elements {
		r.expression(element.Default)
		if element.Pattern != nil {
			r.patternDefaults(element.Pattern)
		}
	}
}

func (r *resolver) block(block *BlockStatement) {
	if block == nil {
		return
	}
	r.enterBlock()
	r.scope.owner = block
	r.hoist(block.Statements)
	for _, statement := range block.Statements {
		r.statement(statement)
	}
	r.leaveBlock()
}

func (r *resolver) switchStatement(node *SwitchStatement) {
	r.expression(node.Discriminant)
	// the cases of a switch are one block, as in JavaScript
	r.enterBlock()
	r.scope.owner = node
	for index, clause := range node.Cases {
		before := len(r.scope.order)
		r.hoist(clause.Statements)
		for _, declared := range r.scope.order[before:] {
			declared.clause = index + 1
		}
	}
	for index, clause := range node.Cases {
		r.clauses[r.scope] = index + 1
		r.expression(clause.Test)
		for _, statement := range clause.Statements {
			r.statement(statement)
		}
	}
	delete(r.clauses, r.scope)
	r.leaveBlock()
}

func (r *resolver) enterLoop(node Statement) *loopScope {
	loop := &loopScope{parent: r.loop, function: r.function}
	r.result.loops[node] = loop
	r.loop = loop
	return loop
}

func (r *resolver) forOf(node *ForStatement) {
	r.expression(node.Iterable)
	loop := r.enterLoop(node)
	r.enterBlock()
	r.scope.owner = node
	if item := r.declare(node.Item, bindingItem); item != nil {
		item.constant = node.Const
	}
	r.block(node.Body)
	r.leaveBlock()
	r.loop = loop.parent
}

func (r *resolver) forRange(node *ForRangeStatement) {
	loop := r.enterLoop(node)
	r.enterBlock()
	r.scope.owner = node
	if init, ok := node.Init.(*VarStatement); ok && len(init.Names) == 1 {
		if counter := r.declare(init.Names[0], bindingCounter); counter != nil {
			counter.constant = init.Token.Kind == Const
		}
		r.expression(init.Value)
	} else {
		r.statement(node.Init)
	}
	r.expression(node.Cond)
	r.expression(node.Update)
	r.block(node.Body)
	r.leaveBlock()
	r.loop = loop.parent
}

func (r *resolver) expression(expression Expression) {
	if r.err != nil || expression == nil {
		return
	}
	switch node := expression.(type) {
	case *Identifier:
		r.reference(node, false)
	case *Literal, *ParameterList:
	case *PrefixExpression:
		if name, ok := node.Right.(*Identifier); ok && node.Operator == "typeof" {
			r.typeofOperand = name
			defer func() { r.typeofOperand = nil }()
		}
		r.expression(node.Right)
	case *InfixExpression:
		r.expression(node.Left)
		r.expression(node.Right)
	case *IfExpression:
		r.expression(node.Condition)
		r.block(node.Consequence)
		r.block(node.Alternative)
	case *TernaryExpression:
		r.expression(node.Condition)
		r.expression(node.Consequence)
		r.expression(node.Alternative)
	case *CallExpression:
		if problem := r.dateWithoutNew(node); problem != nil {
			r.fail(problem)
			return
		}
		r.expression(node.Function)
		for _, argument := range node.Arguments {
			r.expression(argument)
		}
	case *MemberExpression:
		if problem := r.prototypeKey(node.Property.Value, node.Property.Token); problem != nil {
			r.fail(problem)
			return
		}
		r.expression(node.Object)
	case *IndexExpression:
		if key, ok := node.Index.(*Literal); ok && key.Value.IsString() {
			if problem := r.prototypeKey(key.Value.Text(), key.Token); problem != nil {
				r.fail(problem)
				return
			}
		}
		r.expression(node.Left)
		r.expression(node.Index)
	case *ChainExpression:
		r.expression(node.Expression)
	case *AssignmentExpression:
		r.target(node.Name)
		if right, shared := node.sharedTarget(); shared {
			// x op= y and x++ read their target too, the same node the line above resolved: a name is
			// read as well as written, and o.x or o[k] is not resolved again. Resolved twice, a nest of
			// them cost twice as much at each level, and a function in the target was declared twice
			// (05/11/2026).
			if name, ok := node.Name.(*Identifier); ok {
				r.reference(name, false)
			}
			r.expression(right)
		} else if node.Default {
			outer := r.defaultFor
			r.defaultFor = r.parameterIndex // a parameter's default reads only the parameters before it
			r.expression(node.Value)
			r.defaultFor = outer
		} else {
			r.expression(node.Value)
		}
		if object, key := onceParts(node); object || key {
			r.onces = append(r.onces, node)
		}
	case *BindingPattern:
		r.target(node)
	case *ArrayLiteral:
		for _, element := range node.Elements {
			r.expression(element)
		}
	case *ObjectLiteral:
		for _, entry := range node.Entries {
			if problem := r.protoEntry(entry); problem != nil {
				r.fail(problem)
				return
			}
			if assignment, ok := entry.Key.(*AssignmentExpression); ok && !entry.IsSpread && entry.Key == entry.Value {
				// { a = 1 } is a default of a pattern (const { a = 1 } = o); in an object it is the
				// SyntaxError of JavaScript, not an assignment to a and a key "1" (14/10/2026)
				at, name := assignment.Token, assignment.Name.String()
				if id, ok := assignment.Name.(*Identifier); ok {
					at = id.Token
				}
				r.fail(fmt.Errorf("%s: { %s = … } chỉ là giá trị mặc định khi tách (const { %s = … } = o); trong một object viết { %s: … }",
					r.where(at), name, name, name))
				return
			}
			if !entry.IsSpread {
				// a name before ':' is the key's spelling; { a } reads a through its value
				if _, isName := entry.Key.(*Identifier); !isName {
					r.expression(entry.Key)
				}
			}
			r.expression(entry.Value)
		}
	case *SpreadExpression:
		r.expression(node.Value)
	case *FunctionLiteral:
		r.functionLiteral(node)
	case *MethodCallExpression:
		r.expression(node.Object)
		for _, argument := range node.Arguments {
			r.expression(argument)
		}
	case *TemplateLiteral:
		for _, part := range node.Parts {
			r.expression(part)
		}
	case *DeferStatement:
		r.expression(node.Fn)
	case *SpawnStatement:
		r.expression(node.Fn)
	case *ForStatement:
		r.forOf(node)
	case *ForRangeStatement:
		r.forRange(node)
	}
}

// target reads the left side of an assignment.
func (r *resolver) target(target Expression) {
	switch node := target.(type) {
	case *Identifier:
		r.reference(node, true)
	case *BindingPattern:
		if problem := r.patternKeys(node); problem != nil {
			r.fail(problem)
			return
		}
		for _, element := range node.Elements {
			if element.Pattern != nil {
				r.target(element.Pattern)
			} else {
				r.reference(element.Name, true)
			}
			r.expression(element.Default)
		}
	default:
		r.expression(target)
	}
}

func (r *resolver) functionLiteral(literal *FunctionLiteral) {
	outerLoop, outerParameter, outerDefault := r.loop, r.parameterIndex, r.defaultFor
	r.loop, r.parameterIndex, r.defaultFor = nil, 0, 0
	r.enterFunction(literal)
	for index, parameter := range literal.Parameters {
		if declared := r.declare(parameter, bindingParameter); declared != nil {
			declared.parameter = index + 1
		}
	}
	if literal.Body != nil {
		statements := literal.Body.Statements
		// each parameter's default and destructuring, in the parameters' scope as JavaScript has it: every name they
		// declare first, then each resolved with its parameter's place, before the body's declarations are made, so
		// a default never sees the body's names (18/11/2026: the defaults were the body's first statements, and a
		// function in a default read the body's variables)
		at := 0
		for index, count := range literal.Initializers {
			r.parameterIndex = index + 1
			r.hoist(statements[at : at+count])
			at += count
		}
		at = 0
		for index, count := range literal.Initializers {
			r.parameterIndex = index + 1
			for _, statement := range statements[at : at+count] {
				r.statement(statement)
			}
			at += count
		}
		r.parameterIndex = 0
		r.hoist(statements[at:])
		for _, statement := range statements[at:] {
			r.statement(statement)
		}
	}
	r.leaveFunction()
	r.loop, r.parameterIndex, r.defaultFor = outerLoop, outerParameter, outerDefault
}

// assignSlots gives every variable the name it lives under. Functions come outer first, so what a
// function around holds is known when a function inside decides.
func (r *resolver) assignSlots() {
	for _, function := range r.result.functions {
		function.held = map[string]bool{}
		for _, declared := range function.scope.order {
			switch {
			case function.literal == nil, declared.kind == bindingParameter:
				declared.slot = declared.name
			case r.heldAround(function, declared.name):
				declared.slot = r.slot(declared.name, runtime.SlotFunctionMark)
			default:
				declared.slot = declared.name
			}
			if declared.slot == declared.name {
				function.held[declared.name] = true
			}
		}
		for name := range function.written {
			function.held[name] = true
		}
		// A parameter assigned while a function around held its name was copied into name@N as the
		// function started, so that STORE, which looked in the maps around first, kept the write in the
		// function. A parameter is a slot now, written by index (bytecode 7): no copy.
	}
	for _, declared := range r.result.bindings {
		if declared.scope.block {
			declared.slot = r.slot(declared.name, runtime.SlotBlockMark)
		}
	}
}

// listBindings gives every function the list of the variables it declares, in source order, which puts
// its parameters first: one array for the whole program, cut per function, so a program of many small
// functions does not grow a list for each.
func (r *resolver) listBindings() {
	counts := make([]int, len(r.result.functions))
	for _, declared := range r.result.bindings {
		counts[declared.scope.function.index]++
	}
	all := make([]*binding, len(r.result.bindings))
	offset := 0
	for index, function := range r.result.functions {
		function.bindings = all[offset : offset : offset+counts[index]]
		offset += counts[index]
	}
	for _, declared := range r.result.bindings {
		function := declared.scope.function
		function.bindings = append(function.bindings, declared)
	}
}

// heldAround reports whether a function around this one may hold name in its map under that spelling:
// a parameter, a declaration of its own scope that kept its name, or a name it assigns undeclared.
func (r *resolver) heldAround(function *functionScope, name string) bool {
	for around := function.parent; around != nil; around = around.parent {
		if around.held[name] {
			return true
		}
	}
	return false
}

func (r *resolver) slot(name string, mark byte) string {
	r.next++
	return name + string(mark) + strconv.Itoa(r.next)
}

// assignOnce gives each assignment of r.onces its slots, after every other slot, so the slots of a program
// without such an assignment keep their names. A node resolved twice (the target of an update is also
// the left side of its value) gets its slots once.
func (r *resolver) assignOnce() {
	for _, node := range r.onces {
		if _, given := r.result.once[node]; given {
			continue
		}
		object, key := onceParts(node)
		var slots onceSlots
		if object {
			slots.object = r.slot("", runtime.SlotBlockMark)
		}
		if key {
			slots.key = r.slot("", runtime.SlotBlockMark)
		}
		r.result.once[node] = slots
	}
}

// onceParts tells which parts of an update or a compound assignment of a member or an item are kept in a
// slot: the key when it does more than read, and then the object too, which JavaScript evaluates before
// the key; the object alone when only it does. A plain assignment (a[f()] = v) evaluates its target once
// already.
func onceParts(node *AssignmentExpression) (object, key bool) {
	infix, ok := node.Value.(*InfixExpression)
	if !ok || infix.Left != node.Name {
		return false, false
	}
	switch target := node.Name.(type) {
	case *MemberExpression:
		return !readsOnly(target.Object), false
	case *IndexExpression:
		if !readsOnly(target.Index) {
			return true, true
		}
		return !readsOnly(target.Left), false
	}
	return false, false
}

// assignEnvironments puts every variable of an iteration that a closure reaches in its loop's
// environment, and lists for each such closure the environments it is handed as it is made.
func (r *resolver) assignEnvironments() {
	for _, declared := range r.result.bindings {
		if declared.loop == nil || len(declared.closures) == 0 {
			continue
		}
		declared.environment = true
		if declared.loop.environment == "" {
			declared.loop.environment = r.slot("", runtime.SlotBlockMark)
			declared.loop.function.loops = append(declared.loop.function.loops, declared.loop)
		}
		for _, closure := range declared.closures {
			handed := r.result.environments[closure]
			known := false
			for _, slot := range handed {
				if slot == declared.loop.environment {
					known = true
					break
				}
			}
			if !known {
				r.result.environments[closure] = append(handed, declared.loop.environment)
			}
		}
	}
	for _, name := range r.foreignWrites {
		if found := r.result.slots[name]; found != nil && found.environment {
			if _, ok := r.result.temps[name]; !ok {
				r.result.temps[name] = r.slot("", runtime.SlotBlockMark)
			}
		}
	}
}
