package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A guard fails closed (docs/KITWORK_ROUTER.md, "Hỏng thì đóng", 27/10/2026): it lets a request on only by return
// context.next(), and refuses with return context.error(…) or any other answer. What it returns otherwise is refused at
// run time with 403 (host, site.go); what can be seen where it is written is the boot's error (16/11/2026, the owner):
// a branch of the guard reaching its end without return (the request would be refused for a word forgotten), a value
// written straight in a return (true, false, null, a string, a number, an object, an array, a function, a comparison,
// also behind ?:, ||, &&, ??, ! or - and + with a string), return with nothing, context.next() anywhere but straight
// in a return of the guard (or a branch of ?: there), a parameter named next (Express's, which Kitwork's guard has
// not), a page drawn (context.view, context.bind). context.next() in a door's function, a view's or a work's is an
// error too: it is a guard's word. A guard is read where router.guard(…) is written in a router.work or a config.work:
// the function written there, held by a const of the file's top level, or imported from a file of the app (import {
// admin } from "app/auth.work"); one made by a call is left to the run.

// GuardProblem is a guard, or a door's function, written as a guard cannot run: the file it is written in, its line
// and column there, what to write.
type GuardProblem struct {
	File         string
	Line, Column int
	Message      string
}

// guardReader reads the guards of one router file.
type guardReader struct {
	appRoot  string
	problems []GuardProblem
}

// GuardProblems are the problems of the guards and doors of file, a router.work or a config.work of the app at appRoot
// whose words are source, in the order written; nil when there is none, or when the file does not read (its own error
// is the compiler's).
func GuardProblems(appRoot, file, source string) (problems []GuardProblem) {
	defer func() {
		if recover() != nil {
			problems = nil
		}
	}()
	program, ok := parseRouterFile(file, source)
	if !ok {
		return nil
	}
	reader := &guardReader{appRoot: appRoot}
	modules, functions, lines := topLevel(program)
	imports := importsOf(program)
	for _, line := range lines {
		if !isRouterChain(line, modules) {
			continue
		}
		for _, call := range chainCalls(line) {
			for _, argument := range call.Arguments {
				if call.Method.Value == "guard" {
					if fn, at, words := reader.guardFunction(argument, functions, imports, file, source); fn != nil {
						reader.guard(fn, at, words)
					}
					continue
				}
				if !doorWords[call.Method.Value] && call.Method.Value != "view" {
					continue // a word that takes no door's function says so itself (host, spelling.go)
				}
				if fn := functionOf(argument, functions); fn != nil && len(fn.Parameters) > 0 && fn.Parameters[0] != nil {
					reader.door(fn, fn.Parameters[0].Value, file, source)
				}
			}
		}
	}
	return reader.problems
}

// parseRouterFile is the program of a router file's words, false when they do not read.
func parseRouterFile(file, source string) (*Program, bool) {
	parser := NewParser(NewLexerSource(source, filepath.Base(file)))
	program := parser.ParseProgram()
	return program, program != nil && len(parser.Errors()) == 0
}

// importsOf are the names a program imports from files of the app, by their local name: where from and under what
// name there.
func importsOf(program *Program) map[string]ImportSpec {
	found := map[string]ImportSpec{}
	for _, statement := range program.Statements {
		if imported, ok := statement.(*ImportStatement); ok {
			for _, name := range imported.Names {
				found[name.Local] = ImportSpec{Imported: name.Imported, Local: imported.Source}
			}
		}
	}
	return found
}

// isRouterChain says a line of the top level is a chain of calls that begins with the router (router.guard(g).view(…)).
func isRouterChain(line Expression, modules map[string]string) bool {
	_, ok := line.(*MethodCallExpression)
	return ok && modules[rootName(line)] == "router"
}

// chainCalls are the method calls of a chain, from its first to its last.
func chainCalls(expression Expression) []*MethodCallExpression {
	var calls []*MethodCallExpression
	for {
		call, ok := expression.(*MethodCallExpression)
		if !ok || call.Method == nil {
			break
		}
		calls = append([]*MethodCallExpression{call}, calls...)
		expression = call.Object
	}
	return calls
}

// guardFunction is the function a guard's argument is, with the file and words it is written in: one written there,
// one a const of the top level holds, one imported from a file of the app; nil for one the boot cannot see.
func (g *guardReader) guardFunction(argument Expression, functions map[string]*FunctionLiteral, imports map[string]ImportSpec,
	file, source string) (*FunctionLiteral, string, string) {
	if fn := functionOf(argument, functions); fn != nil {
		return fn, file, source
	}
	name, ok := argument.(*Identifier)
	if !ok {
		return nil, "", ""
	}
	imported, ok := imports[name.Value]
	if !ok {
		return nil, "", ""
	}
	path, ok := g.importedFile(file, imported.Local)
	if !ok {
		return nil, "", ""
	}
	words, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ""
	}
	program, ok := parseRouterFile(path, string(words))
	if !ok {
		return nil, "", ""
	}
	_, theirs, _ := topLevel(program)
	exported := false
	for _, export := range program.Exports {
		exported = exported || export == imported.Imported
	}
	if fn := theirs[imported.Imported]; fn != nil && exported {
		return fn, path, string(words)
	}
	return nil, "", ""
}

// importedFile is the file an import of the file at from names, app/… from the app's root, ./… beside it; false for
// one that does not resolve (the compiler says so).
func (g *guardReader) importedFile(from, specifier string) (string, bool) {
	switch {
	case strings.HasPrefix(specifier, "app/"):
		if g.appRoot == "" {
			return "", false
		}
		return statModule(filepath.Join(g.appRoot, filepath.FromSlash(strings.TrimPrefix(specifier, "app/"))))
	case strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../"):
		return statModule(filepath.Join(filepath.Dir(from), filepath.FromSlash(specifier)))
	}
	return "", false
}

// problem adds what is wrong at token of the file whose words are source.
func (g *guardReader) problem(file, source string, token Token, message string) {
	at := min(max(int(token.Position), 0), len(source))
	line := 1 + strings.Count(source[:at], "\n")
	column := utf8.RuneCountInString(source[strings.LastIndexByte(source[:at], '\n')+1:at]) + 1
	g.problems = append(g.problems, GuardProblem{File: file, Line: line, Column: column, Message: message})
}

// guard reads one guard function.
func (g *guardReader) guard(fn *FunctionLiteral, file, source string) {
	context := ""
	for i, parameter := range fn.Parameters {
		if parameter == nil {
			continue
		}
		if i == 0 {
			context = parameter.Value
		}
		if parameter.Value == "next" {
			g.problem(file, source, parameter.Token, "a guard has no next parameter: it lets the request on with return context.next() and refuses with return context.error(…) or another answer")
		}
	}
	if end := openEnd(fn.Body.Statements); end != nil || len(fn.Body.Statements) == 0 {
		at := fn.Token
		if end != nil {
			at = statementToken(end)
		}
		g.problem(file, source, at, "the guard reaches its end here without a return, which refuses the request with 403 and says nothing: a guard lets the request on with return context.next() and refuses with return context.error(…) or another answer")
	}
	allowed := map[*MethodCallExpression]bool{}
	for _, returned := range returnsOf(fn.Body) {
		if returned.ReturnValue == nil {
			g.problem(file, source, returned.Token, "the guard returns nothing: a guard lets the request on with return context.next() and refuses with return context.error(…) or another answer")
			continue
		}
		g.returned(returned.ReturnValue, context, allowed, file, source)
	}
	g.walkContext(fn.Body, context, func(call *MethodCallExpression) {
		switch call.Method.Value {
		case "next":
			if !allowed[call] {
				g.problem(file, source, tokenOf(call), "context.next() stands straight after the guard's return, or in a branch of ?: there: the guard lets the request on by returning it")
			}
		case "view", "bind":
			g.problem(file, source, tokenOf(call), "the guard draws a page, context."+call.Method.Value+"(…): a guard lets the request on with return context.next() or answers it, never with a page")
		}
	})
}

// returned reads what a guard returns: the context.next() it may hold straight, or in a branch of ?:, and the values
// written there that are no answer.
func (g *guardReader) returned(expression Expression, context string, allowed map[*MethodCallExpression]bool, file, source string) {
	switch e := expression.(type) {
	case *TernaryExpression:
		g.returned(e.Consequence, context, allowed, file, source)
		g.returned(e.Alternative, context, allowed, file, source)
		return
	case *MethodCallExpression:
		if e.Method != nil && e.Method.Value == "next" && isName(e.Object, context) {
			allowed[e] = true
		}
		return
	}
	if kind := writtenValue(expression); kind != "" {
		g.problem(file, source, tokenOf(expression), "the guard returns "+kind+": a guard lets the request on with return context.next() and refuses with return context.error(…); a check that gives true or false is no guard, write check(context) ? context.next() : context.error(\"…\")")
	}
}

// writtenValue is what a value written straight in a return is ("true", "a string"…), "" for one that may be an
// answer (a call, a name, a member): those are the run's to read.
func writtenValue(expression Expression) string {
	switch e := expression.(type) {
	case *Literal:
		switch {
		case e.Value.IsBoolean():
			return e.Value.ToString().Text()
		case e.Value.IsNull():
			return "null"
		case e.Value.IsString():
			return "a string"
		case e.Value.IsNumber():
			return "a number"
		}
		return "a value"
	case *Identifier:
		if e.Value == "undefined" {
			return "undefined"
		}
	case *ObjectLiteral:
		return "an object"
	case *ArrayLiteral:
		return "an array"
	case *FunctionLiteral:
		return "a function"
	case *TemplateLiteral:
		return "a string"
	case *PrefixExpression:
		switch e.Operator {
		case "!":
			return "true or false"
		case "-", "+", "~", "typeof":
			return "a value"
		}
	case *TernaryExpression:
		if kind := writtenValue(e.Consequence); kind != "" {
			return kind
		}
		return writtenValue(e.Alternative)
	case *InfixExpression:
		switch e.Operator {
		case "||", "&&", "??":
			if kind := writtenValue(e.Left); kind != "" {
				return kind
			}
			return writtenValue(e.Right)
		case "===", "!==", "==", "!=", "<", ">", "<=", ">=", "in", "instanceof":
			return "true or false"
		case "+":
			if writtenValue(e.Left) == "a string" || writtenValue(e.Right) == "a string" {
				return "a string"
			}
			return ""
		case "-", "*", "/", "%", "**":
			return "a number"
		}
	}
	return ""
}

// door reads a door's function, a view's or a work's: context.next() in it is a guard's word.
func (g *guardReader) door(fn *FunctionLiteral, context, file, source string) {
	g.walkContext(fn.Body, context, func(call *MethodCallExpression) {
		if call.Method.Value == "next" {
			g.problem(file, source, tokenOf(call), "context.next() is a guard's, return context.next() in the function of router.guard(…): a door's function answers")
		}
	})
}

// walkContext calls seen with every method call on the name context (context.next(), context.title(…).view(…)) under
// node, a function inside that declares the name again passed over.
func (g *guardReader) walkContext(node Node, context string, seen func(*MethodCallExpression)) {
	if context == "" {
		return
	}
	stack := []Node{node}
	children := make([]Node, 0, 16)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch n := node.(type) {
		case *FunctionLiteral:
			if declaresParameter(n, context) {
				continue
			}
		case *MethodCallExpression:
			if n.Method != nil && rootName(n) == context {
				seen(n)
			}
		}
		children = nodeChildren(node, children[:0])
		stack = append(stack, children...)
	}
}

// returnsOf are the returns of a function's body, not those of a function inside it.
func returnsOf(body *BlockStatement) []*ReturnStatement {
	var found []*ReturnStatement
	stack := []Node{body}
	children := make([]Node, 0, 16)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch n := node.(type) {
		case *FunctionLiteral:
			continue
		case *ReturnStatement:
			found = append(found, n)
			continue
		}
		children = nodeChildren(node, children[:0])
		stack = append(stack, children...)
	}
	return found
}

// openEnd is the statement after which a run of statements can reach its end without a return, nil when every way
// through it returns: the last statement, unless it returns, or is an if whose branches both return, a block or a
// switch (with a default) that returns on every way. A loop is never a sure return.
func openEnd(statements []Statement) Statement {
	for _, statement := range statements {
		if returns(statement) {
			return nil
		}
	}
	if len(statements) == 0 {
		return nil
	}
	return statements[len(statements)-1]
}

// returns says every way through statement returns.
func returns(statement Statement) bool {
	switch s := statement.(type) {
	case *ReturnStatement:
		return true
	case *BlockStatement:
		return len(s.Statements) > 0 && openEnd(s.Statements) == nil
	case *ExpressionStatement:
		if branch, ok := s.Expression.(*IfExpression); ok {
			return branch.Consequence != nil && branch.Alternative != nil &&
				returns(branch.Consequence) && returns(branch.Alternative)
		}
	case *SwitchStatement:
		hasDefault := false
		for i, each := range s.Cases {
			if each.Test == nil {
				hasDefault = true
			}
			last := i == len(s.Cases)-1
			if len(each.Statements) == 0 && !last {
				continue // falls through to the next case
			}
			if len(each.Statements) == 0 || openEnd(each.Statements) != nil {
				return false
			}
		}
		return hasDefault
	}
	return false
}

// statementToken is where a statement is written.
func statementToken(statement Statement) Token {
	switch s := statement.(type) {
	case *ReturnStatement:
		return s.Token
	case *BlockStatement:
		return s.Token
	case *ExpressionStatement:
		if branch, ok := s.Expression.(*IfExpression); ok {
			return branch.Token
		}
		if s.Expression != nil {
			return tokenOf(s.Expression)
		}
		return s.Token
	case *SwitchStatement:
		return s.Token
	case *VarStatement:
		return s.Token
	}
	return Token{}
}

// tokenOf is where an expression begins.
func tokenOf(expression Expression) Token {
	switch e := expression.(type) {
	case *Identifier:
		return e.Token
	case *Literal:
		return e.Token
	case *MethodCallExpression:
		return tokenOf(e.Object)
	case *MemberExpression:
		return tokenOf(e.Object)
	case *CallExpression:
		return tokenOf(e.Function)
	case *InfixExpression:
		return tokenOf(e.Left)
	case *TernaryExpression:
		return tokenOf(e.Condition)
	case *PrefixExpression:
		return e.Token
	case *ObjectLiteral:
		return e.Token
	case *ArrayLiteral:
		return e.Token
	case *FunctionLiteral:
		return e.Token
	case *TemplateLiteral:
		return e.Token
	}
	return Token{}
}

// isName says expression is the name name.
func isName(expression Expression, name string) bool {
	identifier, ok := expression.(*Identifier)
	return ok && name != "" && identifier.Value == name
}
