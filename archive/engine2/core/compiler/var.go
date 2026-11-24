package compiler

import (
	"fmt"

	"github.com/kitwork/engine2/core/value"
)

// var (docs/language.md mục 2, decided 27/10/2026): the state of a screen. It stands only at the top of the function
// of router.view(…) or component.define(…), whose first parameter is the run's context:
//
//	router.view((context) => { var title = ""; const add = () => { … title = ""; }; return context.bind({ title, add }); })
//
// The compiler writes it as a let the run's context fills (library/router, bridge.go):
//
//	let title = context["var#"]("title", "", () => title);
//
// When the page is drawn the context gives the value written, and keeps the function that reads the variable; when
// the browser calls a work of the page it gives what the browser held, and after the work the host reads each var
// back through its function, the value the work left. A source never spells #, so "var#" is the compiler's alone.
// A var anywhere else is a compile error: at the top of a file a name that changes lives through every request; in
// any other function or block it is a let.

// varCall is the method of the context a var is filled by.
const varCall = "var#"

// viewFunctions are the functions a program gives router.view(…) and component.define(…) at its top level: written
// there, or a const of the top level holding one. router and component are the names the program imports
// "kitwork/router" and "kitwork/component" as (import * as router from "kitwork/router" is const router =
// kitwork.router); a rule written before the view, router.guard(g).view(fn), begins with router too, and the view
// is the line's last call (a rule stands before its door, docs/router.md).
func viewFunctions(program *Program) map[*FunctionLiteral]bool {
	modules, functions, lines := topLevel(program)
	views := map[*FunctionLiteral]bool{}
	mark := func(argument Expression) {
		if fn := functionOf(argument, functions); fn != nil {
			views[fn] = true
		}
	}
	for _, line := range lines {
		call, ok := line.(*MethodCallExpression)
		if !ok || call.Method == nil {
			continue
		}
		switch module := modules[rootName(call.Object)]; {
		case module == "router" && call.Method.Value == "view" && len(call.Arguments) == 1:
			mark(call.Arguments[0])
		case module == "component" && call.Method.Value == "define" && len(call.Arguments) == 2:
			mark(call.Arguments[1])
		}
	}
	return views
}

// topLevel reads the top level of program: the module each name imports (router for const router =
// kitwork.router), the function each const of the top level holds, and the lines, its expression statements.
func topLevel(program *Program) (modules map[string]string, functions map[string]*FunctionLiteral, lines []Expression) {
	modules = map[string]string{}
	functions = map[string]*FunctionLiteral{}
	var read func(statements []Statement)
	read = func(statements []Statement) {
		for _, statement := range statements {
			switch node := statement.(type) {
			case *VarStatement:
				if node.Token.Kind != Const || node.Pattern != nil || len(node.Names) != 1 {
					continue
				}
				switch v := node.Value.(type) {
				case *MemberExpression:
					if object, ok := v.Object.(*Identifier); ok && object.Value == kitworkName && v.Property != nil {
						modules[node.Names[0].Value] = v.Property.Value
					}
				case *FunctionLiteral:
					functions[node.Names[0].Value] = v
				}
			case *ExpressionStatement:
				lines = append(lines, node.Expression)
			case *GroupStatement:
				read(node.Statements)
			}
		}
	}
	read(program.Statements)
	return modules, functions, lines
}

// functionOf is the function argument is: one written there, or the one a const of the top level holds; nil else.
func functionOf(argument Expression, functions map[string]*FunctionLiteral) *FunctionLiteral {
	switch fn := argument.(type) {
	case *FunctionLiteral:
		return fn
	case *Identifier:
		return functions[fn.Value]
	}
	return nil
}

// rootName is the name a chain of members and calls begins with (router in router.guard(g).view), "" when it begins
// with anything else.
func rootName(expression Expression) string {
	for {
		switch node := expression.(type) {
		case *Identifier:
			return node.Value
		case *MethodCallExpression:
			expression = node.Object
		case *MemberExpression:
			expression = node.Object
		case *CallExpression:
			expression = node.Function
		default:
			return ""
		}
	}
}

// varStatement writes a var at the top of a view's or a component's function as the let its context fills, and fails
// a var anywhere else.
func (r *resolver) varStatement(node *VarStatement) {
	if node.Pattern != nil || len(node.Names) != 1 {
		r.fail(fmt.Errorf("%s: var khai một tên mỗi dòng, mỗi tên là một trạng thái của màn hình: var query = \"\"; var page = 1;",
			r.where(node.Token)))
		return
	}
	name := node.Names[0]
	function := r.function
	switch {
	case function.literal == nil:
		r.fail(fmt.Errorf("%s: var %s ở cấp cao nhất của file: tên đổi được ở cấp file sống qua mọi request, của mọi người dùng; ở đây viết const (docs/language.md mục 2)",
			r.where(node.Token), name.Value))
		return
	case !r.views[function.literal] || r.scope != function.scope:
		r.fail(fmt.Errorf("%s: var %s chỉ đứng ở cấp cao nhất của hàm router.view(…) hay component.define(…), là trạng thái của màn hình; ở đây viết let, hay const nếu không đổi (docs/language.md mục 2)",
			r.where(node.Token), name.Value))
		return
	case len(function.literal.Parameters) == 0:
		r.fail(fmt.Errorf("%s: var %s là trạng thái của màn hình, giữ trong context của lần chạy: hàm cần tham số context, (context) => { var %s = …; … }",
			r.where(node.Token), name.Value, name.Value))
		return
	}
	context := function.literal.Parameters[0]
	initial := node.Value
	if initial == nil {
		initial = &Literal{Token: tokenWithValue(name.Token, Null, value.Undefined()), Value: value.Undefined()}
	}
	read := &Identifier{Token: name.Token, Value: name.Value}
	node.Value = &MethodCallExpression{
		Token:  tokenWithValue(name.Token, Dot, value.Undefined()),
		Object: &Identifier{Token: tokenWithValue(name.Token, Ident, value.String(context.Value)), Value: context.Value},
		Method: &Identifier{Token: tokenWithValue(name.Token, Ident, value.String(varCall)), Value: varCall},
		Arguments: []Expression{
			&Literal{Token: tokenWithValue(name.Token, String, value.String(name.Value)), Value: value.String(name.Value)},
			initial,
			&FunctionLiteral{Token: tokenWithValue(name.Token, FatArrow, value.Undefined()),
				Body: &BlockStatement{Statements: []Statement{&ReturnStatement{Token: name.Token, ReturnValue: read}}}},
		},
	}
	node.Token = tokenWithValue(node.Token, Let, value.String("let"))
}
