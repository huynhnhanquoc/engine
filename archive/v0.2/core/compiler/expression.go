package compiler

import (
	"errors"
	"fmt"
	"slices"
)

// ExpressionValue is the top-level variable an expression's program leaves its value in (CompileExpression): a name
// no source can write, so it never takes a name of the page. 15/11/2026: it was value, the name the text is written
// with (wrapper), and {{ value }} read the program's own variable: the page's value reached it only because the
// evaluator gives a page's names in the same slots, and reads left value out, so a part of a page kept for the next
// request (frozen.go) showed the value of the request before, another visitor's.
const ExpressionValue = "value#"

// wrapper is the name the text of an expression is written with, const value = (…), which oneValue renames to
// ExpressionValue before anything resolves a name.
const wrapper = "value"

// CompileExpression compiles source, an expression a .ui file writes between {{ }} or in an attribute (if,
// show, the list of a for), as .ui reads one (docs/KITWORK_LANG.md mục 3): with .work's semantics, and two
// rules over them for markup.
//   - Every link of a chain reads as ?. does: a.b, a[k] and a.f() of null or undefined are undefined, never
//     a failure, and a page writes nothing for undefined and null.
//   - reads are the names it reads that it does not declare (an arrow's parameter inside it declares one) and
//     that are no builtin, each once, in the order first read: the page refuses one it is not given.
//
// An expression reads and never writes: an assignment, ++ or -- in it is an error, since what a page shows
// does not change what it shows. Source is one expression: what would close the parenthesis and go on to a
// second statement is an error. The program leaves the value in its variable ExpressionValue.
func CompileExpression(source string) (*Bytecode, []string, error) {
	bytecode, reads, _, err := CompileExpressionBuiltins(source)
	return bytecode, reads, err
}

// CompileExpressionBuiltins is CompileExpression that also gives the builtins source reads, each once, sorted (Math,
// Date, JSON…): what reads tells nothing of, since a builtin is no name of the page (10/11/2026, for a part of a page
// that stays the same: ui, frozen.go).
func CompileExpressionBuiltins(source string) (_ *Bytecode, _, _ []string, err error) {
	defer contain(&err)
	const name = "<expression>"
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, name)
	if err != nil {
		return nil, nil, nil, err
	}
	statement, ok := oneValue(program)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%q là một biểu thức, không phải câu lệnh", source)
	}
	safe, err := safeChains(statement.Value)
	if err != nil {
		return nil, nil, nil, err
	}
	statement.Value = safe
	compiler := newCompilerWithSources(map[string]string{name: text})
	if err := compiler.Compile(program); err != nil {
		return nil, nil, nil, err
	}
	bytecode, err := compiler.ByteCodeResult()
	if err != nil {
		return nil, nil, nil, err
	}
	var builtins []string
	for identifier := range compiler.scopes.builtinReads {
		if !slices.Contains(builtins, identifier.Value) {
			builtins = append(builtins, identifier.Value)
		}
	}
	slices.Sort(builtins)
	return bytecode, compiler.scopes.freeReads, builtins, nil
}

// CompileHandler reads source, what an event of a .ui file runs (click="toggle()", click="format = 'rich'",
// click="minutes--"): one expression, which may write, unlike CompileExpression's, since an event is how a page
// changes what it shows. The browser's kernel runs it; the server only reads it, for the names it reads and
// writes that it does not declare (an arrow's parameter inside it declares one) and that are no builtin, each
// once, in the order first met, so it can tell whose they are (a component's, an alias, the page's).
func CompileHandler(source string) (_ []string, err error) {
	defer contain(&err)
	const name = "<handler>"
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, name)
	if err != nil {
		return nil, err
	}
	statement, ok := oneValue(program)
	if !ok {
		return nil, fmt.Errorf("%q là một biểu thức, không phải câu lệnh", source)
	}
	compiler := newCompilerWithSources(map[string]string{name: text})
	if err := compiler.Compile(program); err != nil {
		return nil, err
	}
	names := append([]string(nil), compiler.scopes.freeReads...)
	for _, written := range writtenNames(statement.Value) {
		if !slices.Contains(names, written) {
			names = append(names, written)
		}
	}
	return names, nil
}

// writtenNames are the names expression assigns to, outside the arrows inside it (whose names are their own).
func writtenNames(expression Expression) []string {
	var names []string
	var walk func(Expression)
	walk = func(expression Expression) {
		switch e := expression.(type) {
		case *AssignmentExpression:
			if name, ok := e.Name.(*Identifier); ok {
				names = append(names, name.Value)
			} else {
				walk(e.Name)
			}
			walk(e.Value)
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
		}
	}
	walk(expression)
	return names
}

// ExpressionStrings are the strings source, an expression of a .ui file, can be when each of its ends is a string
// written in it ({{ dark ? 'sun' : 'moon' }} is sun or moon), in the order written, each once; false when an end is
// anything else (a name, a number, a call), whose value only the page knows. A class that follows a component's
// state in the browser is written out once for each (KITWORK_UI mục 11).
//
// A fault of the compiler's own code here is false, as an end it cannot tell, and refuses nothing (guard.go);
// FuzzExpressionStrings of the package's tests looks for one.
func ExpressionStrings(source string) (written []string, known bool) {
	defer func() {
		if recover() != nil {
			written, known = nil, false
		}
	}()
	return expressionStrings(source)
}

// expressionStrings is ExpressionStrings without its recover, which a fuzzer of the package's own tests reads.
func expressionStrings(source string) ([]string, bool) {
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, "<expression>")
	if err != nil {
		return nil, false
	}
	statement, ok := oneValue(program)
	if !ok {
		return nil, false
	}
	var out []string
	var ends func(Expression) bool
	ends = func(expression Expression) bool {
		switch e := expression.(type) {
		case *TernaryExpression:
			return ends(e.Consequence) && ends(e.Alternative)
		case *Literal:
			if !e.Value.IsString() {
				return false
			}
			if text := e.Value.Text(); !slices.Contains(out, text) {
				out = append(out, text)
			}
			return true
		}
		return false
	}
	if !ends(statement.Value) {
		return nil, false
	}
	return out, true
}

// oneValue is the one statement const value = (…) the text of an expression parses to, its variable renamed
// ExpressionValue, so a value the expression reads is a name of the page.
func oneValue(program *Program) (*VarStatement, bool) {
	if len(program.Statements) != 1 {
		return nil, false
	}
	statement, ok := program.Statements[0].(*VarStatement)
	if !ok || statement.Pattern != nil || statement.Value == nil || len(statement.Names) != 1 || statement.Names[0].Value != wrapper {
		return nil, false
	}
	statement.Names[0].Value = ExpressionValue
	return statement, true
}

// errWrite is an expression of a .ui file that writes.
var errWrite = errors.New("biểu thức của .ui chỉ đọc: gán, ++ và -- không viết trong {{ }}, if, show hay for")

// safeChains is expression with every link of every chain reading as ?. (MemberExpression, IndexExpression and
// the object of a MethodCallExpression), each chain wrapped whole in one ChainExpression, so the first link
// that meets null or undefined makes the whole chain undefined. A call of a name (money(x)) stays a call: a
// function the page is not given is an error, not nothing.
func safeChains(expression Expression) (Expression, error) {
	switch e := expression.(type) {
	case nil:
		return nil, nil
	case *AssignmentExpression:
		return nil, errWrite
	case *MemberExpression, *IndexExpression, *MethodCallExpression:
		if err := optionalLinks(e); err != nil {
			return nil, err
		}
		return &ChainExpression{Expression: e}, nil
	case *ChainExpression:
		if err := optionalLinks(e.Expression); err != nil {
			return nil, err
		}
		return e, nil
	case *CallExpression:
		function, err := safeChains(e.Function)
		if err != nil {
			return nil, err
		}
		e.Function = function
		return e, arguments(e.Arguments)
	case *PrefixExpression:
		right, err := safeChains(e.Right)
		e.Right = right
		return e, err
	case *InfixExpression:
		left, err := safeChains(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := safeChains(e.Right)
		e.Left, e.Right = left, right
		return e, err
	case *TernaryExpression:
		parts := []*Expression{&e.Condition, &e.Consequence, &e.Alternative}
		for _, part := range parts {
			safe, err := safeChains(*part)
			if err != nil {
				return nil, err
			}
			*part = safe
		}
		return e, nil
	case *ArrayLiteral:
		return e, arguments(e.Elements)
	case *ObjectLiteral:
		for i := range e.Entries {
			value, err := safeChains(e.Entries[i].Value)
			if err != nil {
				return nil, err
			}
			e.Entries[i].Value = value
		}
		return e, nil
	case *SpreadExpression:
		value, err := safeChains(e.Value)
		e.Value = value
		return e, err
	case *TemplateLiteral:
		return e, arguments(e.Parts)
	case *FunctionLiteral:
		return e, safeBlock(e.Body)
	}
	return expression, nil
}

// optionalLinks makes every link of the chain whose outermost link is expression read as ?. does, and makes
// the expressions inside it (an index, the arguments of a call) safe too.
func optionalLinks(expression Expression) error {
	for {
		switch e := expression.(type) {
		case *MemberExpression:
			e.Optional = true
			expression = e.Object
		case *IndexExpression:
			e.Optional = true
			index, err := safeChains(e.Index)
			if err != nil {
				return err
			}
			e.Index = index
			expression = e.Left
		case *MethodCallExpression:
			e.Optional = true
			if err := arguments(e.Arguments); err != nil {
				return err
			}
			expression = e.Object
		case *CallExpression:
			if err := arguments(e.Arguments); err != nil {
				return err
			}
			expression = e.Function
		case *AssignmentExpression:
			return errWrite
		default:
			// the chain's start: a name, a literal, or an expression of its own, made safe as itself
			_, err := safeChains(expression)
			return err
		}
	}
}

// arguments makes each of list safe in place.
func arguments(list []Expression) error {
	for i, each := range list {
		safe, err := safeChains(each)
		if err != nil {
			return err
		}
		list[i] = safe
	}
	return nil
}

// safeBlock makes the body of an arrow inside an expression safe: its return values, its expressions and the
// values of its constants.
func safeBlock(block *BlockStatement) error {
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		var err error
		switch s := statement.(type) {
		case *ReturnStatement:
			s.ReturnValue, err = safeChains(s.ReturnValue)
		case *ExpressionStatement:
			s.Expression, err = safeChains(s.Expression)
		case *VarStatement:
			s.Value, err = safeChains(s.Value)
		case *BlockStatement:
			err = safeBlock(s)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
