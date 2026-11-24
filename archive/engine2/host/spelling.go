package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/compiler"
)

// The doors of a folder are read without running its files (docs/KITWORK_ROUTER.md mục 3): router.work and
// config.work import the router on one line, `import * as router from "kitwork/router"`, and name it only
// at the head of a statement of the file's top level, router.<word>(…), its rules then its door. The router
// as a value (const r = router, helper(router), router["get"], const cached = router.cache("5m")), a door in
// an if, a loop, a callback or a function's body, another spelling of the import (kitwork.router among them),
// and in router.work a top-level const or let get, post, put or patch are boot errors, each at its line
// (05/11/2026; until then they ran, so helper(router) let a .shared library declare the folder's doors, the
// very thing a library may not import the router to do).
//
// The file is read once, each node once, with the scopes around it as the compiler sees them (scope.go): a
// router a block, a function or a loop declares hides the import inside it, and only there (05/11/2026: a
// walk by reflection read the subtrees x[…] += 1 shares twice, so nested ones took seconds, and passed over
// a whole function that declared a router anywhere in it, an inner block's included).

// maxProblems is how many problems of one file a boot error lists; the rest are counted. A file of 60,000
// of them made an error of 10 MB, each one counting its line from the start of the source.
const maxProblems = 20

// routerWords are the problems of the router's spelling in the file name, of source: "" when none. Source a
// parser refuses has none here: the compiler says what is wrong with it.
//
// design.work is read the same way for its library (docs/KITWORK_DESIGN.md mục 1): import * as design from
// "kitwork/design" on one line, design.<word>(…) at the head of a statement of the top level, one call a line. So is
// module.work (docs/module.md, 17/11/2026): import * as module from "kitwork/module", module.<word>(…) one call a line,
// and no import of a file: the app's root says where the app is and what it keeps, the app's code is in its files.
func routerWords(name, source string) string {
	parser := compiler.NewParser(compiler.NewLexerSource(source, name))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return ""
	}
	module := "router"
	switch name {
	case "design.work":
		module = "design"
	case "module.work":
		module = "module"
	}
	check := &routerSpelling{name: name, source: source, module: module}
	imported := false
	for _, statement := range program.Statements {
		if imported, ok := statement.(*compiler.ImportStatement); ok && module == "module" {
			check.problem(imported.Token, "module.work imports only kitwork/module, not "+strconv.Quote(imported.Source)+": it says where the app's root is and what the whole app keeps; the app's code is in its other files")
			continue
		}
		if declaration, ok := statement.(*compiler.VarStatement); ok {
			if _, importing := moduleImport(declaration, module); importing {
				switch {
				case !check.namespaceImport(declaration, module):
					check.problem(declaration.Token, check.importSpelling())
				case imported:
					check.problem(declaration.Token, `"kitwork/`+module+`" is imported once a file`)
				}
				imported = true
				continue
			}
			if name == "router.work" {
				// every name the declaration binds, a destructured one too: const { get } = o declares get
				keyword := "const"
				if declaration.Token.Kind == compiler.Let {
					keyword = "let"
				}
				for _, variable := range declaration.Names {
					switch variable.Value {
					case "get", "post", "put", "patch":
						check.problem(variable.Token, keyword+" "+variable.Value+" at the top level of router.work looks like a door and opens none: a door is declared router."+variable.Value+"(…)")
					}
				}
			}
		}
		if group, ok := statement.(*compiler.GroupStatement); ok {
			for _, inner := range group.Statements {
				if declaration, ok := inner.(*compiler.VarStatement); ok {
					if _, importing := moduleImport(declaration, module); importing {
						check.problem(declaration.Token, check.importSpelling())
						continue
					}
				}
				check.statement(inner, false)
			}
			continue
		}
		if expression, ok := statement.(*compiler.ExpressionStatement); ok {
			if links, chain := moduleChain(expression.Expression, module); chain {
				if module != "router" && len(links) > 1 {
					// design's words and module's give nothing back: one call a line
					second := links[len(links)-2]
					check.problem(second.Token, module+"."+links[len(links)-1].Method.Value+"(…)."+second.Method.Value+"(…): one call a line, "+module+"."+second.Method.Value+"(…) on a line of its own")
				}
				if module == "router" {
					check.chainOrder(links)
				}
				for _, link := range links {
					for _, argument := range link.Arguments {
						check.expression(argument, true)
					}
				}
				continue
			}
		}
		check.statement(statement, false)
	}
	return check.report()
}

// ruleWords are the router's words a chain goes on after (docs/router.md mục 3, 4): a rule stands before its door or
// its .methods(…), and nothing follows those, nor any other word of the router.
var ruleWords = map[string]bool{"guard": true, "limit": true, "cache": true, "persist": true}

// chainOrder is the problem of a word written after one nothing follows: router.get(fn).guard(x), whose rule stands
// before its door, router.guard(x).get(fn). links are the chain's calls, the last written first.
func (check *routerSpelling) chainOrder(links []*compiler.MethodCallExpression) {
	for index := len(links) - 1; index > 0; index-- {
		before, after := links[index].Method.Value, links[index-1].Method.Value
		if ruleWords[before] {
			continue
		}
		if ruleWords[after] {
			check.problem(links[index-1].Token, "router."+before+"(…)."+after+"(…): a rule stands before its door, router."+after+"(…)."+before+"(…)")
		} else {
			check.problem(links[index-1].Token, "router."+before+"(…)."+after+"(…): nothing follows router."+before+"(…); router."+after+"(…) on a line of its own")
		}
		return
	}
}

// importSpelling is the problem of any spelling of the import but the one.
func (check *routerSpelling) importSpelling() string {
	switch check.module {
	case "design":
		return `design is imported on one line, import * as design from "kitwork/design": the look is read from design.<word>(…)`
	case "module":
		return `module is imported on one line, import * as module from "kitwork/module": the app's root is read from module.<word>(…)`
	}
	return `the router is imported on one line, import * as router from "kitwork/router": the doors are read from router.<word>(…)`
}

// what is what the file's words declare: the doors, or the look.
func (check *routerSpelling) what() string {
	switch check.module {
	case "design":
		return "the look is"
	case "module":
		return "the app's root is"
	}
	return "the doors are"
}

// routerSpelling collects the problems of one file; module is the name it imports, router, design or module.
type routerSpelling struct {
	name, source, module string
	problems             []string
	// more counts the problems past maxProblems.
	more int
	// hidden is how many scopes around the node being read declare a router of their own: router there
	// is that variable, not the import.
	hidden int
}

// moduleImport reports a declaration the import of "kitwork/<name>" lowers to (const router =
// kitwork.router), or a reading of it as kitwork.router.<x>, and which.
func moduleImport(declaration *compiler.VarStatement, module string) (string, bool) {
	member, ok := declaration.Value.(*compiler.MemberExpression)
	if !ok {
		return "", false
	}
	if inner, nested := member.Object.(*compiler.MemberExpression); nested {
		member = inner
	}
	object, ok := member.Object.(*compiler.Identifier)
	if !ok || object.Value != "kitwork" || member.Property == nil || member.Property.Value != module {
		return "", false
	}
	return member.Property.Value, true
}

// namespaceImport reports the one spelling of the import: import * as router from "kitwork/router". The
// words are read from the import on as the lexer reads them, so that the spaces between them are the
// writer's (05/11/2026: import*as router from"kitwork/router", which JavaScript reads as the same import,
// was split on spaces and refused).
func (check *routerSpelling) namespaceImport(declaration *compiler.VarStatement, module string) bool {
	if declaration.DestructMode != compiler.DestructNone || len(declaration.Names) != 1 || declaration.Names[0].Value != check.module || module != check.module {
		return false
	}
	if _, plain := declaration.Value.(*compiler.MemberExpression).Object.(*compiler.Identifier); !plain {
		return false
	}
	at := int(declaration.Token.Position)
	if at < 0 || at > len(check.source) {
		return false
	}
	lexer := compiler.NewLexerSource(check.source[at:], check.name)
	for _, want := range []struct {
		kind compiler.Kind
		word string
	}{{compiler.Import, ""}, {compiler.Star, ""}, {compiler.Ident, "as"}, {compiler.Ident, check.module}} {
		token := lexer.NextToken()
		if token.Kind != want.kind || want.word != "" && token.Value.Text() != want.word {
			return false
		}
	}
	return true
}

// moduleChain reports router.<word>(…).<word>(…)…, a call of the module at the head of a statement, with
// its calls from the module out.
func moduleChain(expression compiler.Expression, module string) ([]*compiler.MethodCallExpression, bool) {
	var links []*compiler.MethodCallExpression
	for {
		call, ok := expression.(*compiler.MethodCallExpression)
		if !ok || call.Optional || call.OptionalCall || call.Computed {
			// router["get"](…) is no chain of the module: the module is read as a value (18/11/2026: the parser makes
			// it a method call, Computed)
			return nil, false
		}
		links = append(links, call)
		if head, ok := call.Object.(*compiler.Identifier); ok {
			return links, head.Value == module
		}
		expression = call.Object
	}
}

// statement reads one statement for the router read anywhere but at the head of a statement of the top
// level: nested is true inside a statement's arguments, a block or a function, and a declaration's value
// keeps it, as the value of a top-level const is not in a block.
func (check *routerSpelling) statement(statement compiler.Statement, nested bool) {
	switch node := statement.(type) {
	case nil:
	case *compiler.VarStatement:
		if node.Pattern != nil && !node.Pattern.Array && kitworkWord(node.Value) {
			for _, element := range node.Pattern.Elements {
				if element.Key == check.module { // const { router } = kitwork reads it as kitwork.router does
					check.problem(element.KeyToken, check.importSpelling())
				}
			}
		}
		check.expression(node.Value, nested)
		if node.Pattern != nil {
			check.pattern(node.Pattern, nested)
		}
	case *compiler.ExpressionStatement:
		check.expression(node.Expression, true)
	case *compiler.GroupStatement:
		for _, inner := range node.Statements {
			check.statement(inner, true)
		}
	case *compiler.BlockStatement:
		check.block(node)
	case *compiler.ReturnStatement:
		check.expression(node.ReturnValue, true)
	case *compiler.SwitchStatement:
		check.expression(node.Discriminant, true)
		// the cases of a switch are one block, as the compiler reads them
		var declared []compiler.Statement
		for _, clause := range node.Cases {
			declared = append(declared, clause.Statements...)
		}
		hides := declares(declared, check.module)
		check.enter(hides)
		for _, clause := range node.Cases {
			check.expression(clause.Test, true)
			for _, inner := range clause.Statements {
				check.statement(inner, true)
			}
		}
		check.leave(hides)
	case *compiler.ForStatement:
		check.forOf(node)
	case *compiler.ForRangeStatement:
		check.forRange(node)
	case *compiler.DeferStatement:
		check.expression(node.Fn, true)
	case *compiler.SpawnStatement:
		check.expression(node.Fn, true)
	case *compiler.BreakStatement, *compiler.ContinueStatement, *compiler.ImportStatement:
	default:
		if expression, ok := statement.(compiler.Expression); ok {
			check.expression(expression, true)
		}
	}
}

// expression reads one expression as statement reads a statement. A member's object and a method call's
// object and arguments keep nested, as a chain's do; every other part is nested.
func (check *routerSpelling) expression(expression compiler.Expression, nested bool) {
	switch node := expression.(type) {
	case nil:
	case *compiler.Identifier:
		if node.Value == check.module && check.hidden == 0 {
			check.problem(node.Token, check.module+" is read only as "+check.module+".<word>(…) at the head of a statement: const r = "+check.module+", helper("+check.module+") and "+check.module+`["get"] are not read without running the file`)
		}
	case *compiler.Literal, *compiler.ParameterList:
	case *compiler.MemberExpression:
		if kitworkWord(node.Object) && node.Property != nil && node.Property.Value == check.module {
			check.problem(node.Object.(*compiler.Identifier).Token, check.importSpelling())
			return
		}
		check.expression(node.Object, nested)
	case *compiler.IndexExpression:
		if literal, ok := node.Index.(*compiler.Literal); ok && kitworkWord(node.Left) && literal.Value.IsString() && literal.Value.Text() == check.module {
			check.problem(node.Left.(*compiler.Identifier).Token, check.importSpelling())
			return
		}
		check.expression(node.Left, true)
		check.expression(node.Index, true)
	case *compiler.MethodCallExpression:
		if links, chain := moduleChain(node, check.module); chain && check.hidden == 0 {
			first := links[len(links)-1]
			if nested {
				check.problem(first.Token, check.module+"."+first.Method.Value+"(…) is written at the top level of the file, not in an if, a loop, a callback or a function's body: "+check.what()+" read without running the file")
			} else {
				check.problem(first.Token, check.module+"."+first.Method.Value+"(…) is a statement of its own, its value kept by nothing: "+check.what()+" read without running the file")
			}
			return
		}
		if kitworkWord(node.Object) && node.Method != nil && node.Method.Value == check.module {
			check.problem(node.Object.(*compiler.Identifier).Token, check.importSpelling())
			return
		}
		check.expression(node.Object, nested)
		for _, argument := range node.Arguments {
			check.expression(argument, nested)
		}
	case *compiler.FunctionLiteral:
		check.function(node)
	case *compiler.PrefixExpression:
		check.expression(node.Right, true)
	case *compiler.InfixExpression:
		check.expression(node.Left, true)
		check.expression(node.Right, true)
	case *compiler.IfExpression:
		check.expression(node.Condition, true)
		check.block(node.Consequence)
		check.block(node.Alternative)
	case *compiler.TernaryExpression:
		check.expression(node.Condition, true)
		check.expression(node.Consequence, true)
		check.expression(node.Alternative, true)
	case *compiler.CallExpression:
		check.expression(node.Function, true)
		for _, argument := range node.Arguments {
			check.expression(argument, true)
		}
	case *compiler.ChainExpression:
		check.expression(node.Expression, true)
	case *compiler.AssignmentExpression:
		// x += 1 and x++ are x = x + 1 with one x in both places: read once, through the value
		if infix, ok := node.Value.(*compiler.InfixExpression); ok && infix.Left == node.Name {
			check.expression(node.Value, true)
			return
		}
		if pattern, ok := node.Name.(*compiler.BindingPattern); ok {
			check.pattern(pattern, true)
		} else {
			check.expression(node.Name, true)
		}
		check.expression(node.Value, true)
	case *compiler.BindingPattern:
		check.pattern(node, true)
	case *compiler.ArrayLiteral:
		for _, element := range node.Elements {
			check.expression(element, true)
		}
	case *compiler.ObjectLiteral:
		for _, entry := range node.Entries {
			// a name before ':' is the key's spelling, not a read ({ router: 1 }); { router } reads it
			// through its value, which is the same node
			if _, name := entry.Key.(*compiler.Identifier); !entry.IsSpread && !name && entry.Key != entry.Value {
				check.expression(entry.Key, true)
			}
			check.expression(entry.Value, true)
		}
	case *compiler.SpreadExpression:
		check.expression(node.Value, true)
	case *compiler.TemplateLiteral:
		for _, part := range node.Parts {
			check.expression(part, true)
		}
	case *compiler.DeferStatement:
		check.expression(node.Fn, true)
	case *compiler.SpawnStatement:
		check.expression(node.Fn, true)
	case *compiler.ForStatement:
		check.forOf(node)
	case *compiler.ForRangeStatement:
		check.forRange(node)
	}
}

// pattern reads a destructuring's defaults and nested patterns; the names it binds are not reads.
func (check *routerSpelling) pattern(pattern *compiler.BindingPattern, nested bool) {
	for _, element := range pattern.Elements {
		if element.Pattern != nil {
			check.pattern(element.Pattern, nested)
		}
		check.expression(element.Default, nested)
	}
}

// block reads a block's statements in its scope: a router it declares hides the import in all of it.
func (check *routerSpelling) block(block *compiler.BlockStatement) {
	if block == nil {
		return
	}
	hides := declares(block.Statements, check.module)
	check.enter(hides)
	for _, statement := range block.Statements {
		check.statement(statement, true)
	}
	check.leave(hides)
}

// function reads a function's body in its scope, which holds its parameters and what its body declares.
func (check *routerSpelling) function(literal *compiler.FunctionLiteral) {
	var statements []compiler.Statement
	if literal.Body != nil {
		statements = literal.Body.Statements
	}
	hides := declares(statements, check.module)
	for _, parameter := range literal.Parameters {
		hides = hides || parameter.Value == check.module
	}
	check.enter(hides)
	for _, statement := range statements {
		check.statement(statement, true)
	}
	check.leave(hides)
}

// forOf reads a for…of: its item is declared for the body, not for what it walks.
func (check *routerSpelling) forOf(node *compiler.ForStatement) {
	check.expression(node.Iterable, true)
	hides := node.Item != nil && node.Item.Value == check.module
	check.enter(hides)
	check.block(node.Body)
	check.leave(hides)
}

// forRange reads a counting for: its counter is declared for the whole loop.
func (check *routerSpelling) forRange(node *compiler.ForRangeStatement) {
	hides := node.Counter == check.module
	check.enter(hides)
	if init, ok := node.Init.(*compiler.VarStatement); ok {
		check.expression(init.Value, true)
	} else {
		check.statement(node.Init, true)
	}
	check.expression(node.Cond, true)
	check.expression(node.Update, true)
	check.block(node.Body)
	check.leave(hides)
}

func (check *routerSpelling) enter(hides bool) {
	if hides {
		check.hidden++
	}
}

func (check *routerSpelling) leave(hides bool) {
	if hides {
		check.hidden--
	}
}

// declares reports a variable of the module's name the statements of one scope declare themselves (a
// block's, a function's body, a switch's cases): it is the scope's from its start, as the compiler hoists it.
func declares(statements []compiler.Statement, module string) bool {
	for _, statement := range statements {
		switch node := statement.(type) {
		case *compiler.VarStatement:
			for _, variable := range node.Names {
				if variable.Value == module {
					return true
				}
			}
		case *compiler.GroupStatement:
			if declares(node.Statements, module) {
				return true
			}
		}
	}
	return false
}

// kitworkWord reports the global kitwork itself, which no program may declare (scope.go).
func kitworkWord(expression compiler.Expression) bool {
	identifier, ok := expression.(*compiler.Identifier)
	return ok && identifier.Value == "kitwork"
}

// problem records why at token's place, file:line:column; past maxProblems it only counts.
func (check *routerSpelling) problem(token compiler.Token, why string) {
	if len(check.problems) == maxProblems {
		check.more++
		return
	}
	line, column := sourcePlace(check.source, token.Position)
	check.problems = append(check.problems, fmt.Sprintf("%s:%d:%d: %s", check.name, line, column, why))
}

// sourcePlace is the line and the column, in characters, of the byte at position in source, each from 1.
func sourcePlace(source string, position int32) (line, column int) {
	at := min(max(int(position), 0), len(source))
	line = 1 + strings.Count(source[:at], "\n")
	column = utf8.RuneCountInString(source[strings.LastIndexByte(source[:at], '\n')+1:at]) + 1
	return line, column
}

// report is the problems as one boot error, "" when none.
func (check *routerSpelling) report() string {
	text := strings.Join(check.problems, "; ")
	if check.more > 0 {
		text += "; and " + strconv.Itoa(check.more) + " more"
	}
	return text
}

// guardReport is the problems of a router file's guards as one boot error: each at its file of the app, line and
// column, at most five, then how many more.
func guardReport(appRoot string, problems []compiler.GuardProblem) string {
	var lines []string
	for i, problem := range problems {
		if i == 5 {
			lines = append(lines, "and "+strconv.Itoa(len(problems)-5)+" more")
			break
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", writtenIn(appRoot, problem.File), problem.Line, problem.Column, problem.Message))
	}
	return strings.Join(lines, "; ")
}

// viewWithoutPage is where the router.work file answers a door with context.view(…), router.work:line:column, the
// first written (compiler.ViewAnswers); "" when it does not, or cannot be read: a folder with doors and no content.ui
// is held to it at boot (walk).
func viewWithoutPage(file string) string {
	source, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	name := filepath.Base(file)
	parser := compiler.NewParser(compiler.NewLexerSource(string(source), name))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return ""
	}
	answers := compiler.ViewAnswers(program)
	if len(answers) == 0 {
		return ""
	}
	line, column := sourcePlace(string(source), answers[0].Position)
	return fmt.Sprintf("%s:%d:%d", name, line, column)
}
