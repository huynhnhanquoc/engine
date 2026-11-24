package database

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/schema"
)

// A statement is written on a handle, its lambdas naming the tables by their parameters (docs/database.md mục 3):
//
//	db.list((orders) => orders.status === "moi" && orders.amount >= least)
//	db.select((orders, users) => ({ ...orders, customer: users.name })).where((users) => users.active).list(20)
//
// A lambda is data, not code: the engine reads it, never runs it. As each file of a site boots, every statement
// the file writes on a handle the app declared is translated (Prepare): a column the table has not, an operator
// the translation does not take, a value the database cannot compare is an error where it is written, before the
// site serves; a statement written whole in one place (db.where(…).list(…)) is put together then too (assemble,
// plan.go), so a join without its key or a column not grouped is the boot's error as well. Each lambda is kept by
// its place in its program; the call at a request finds it there, and reads the values the lambda takes from
// outside (least, user.id) off its closure (runtime.FreeValue), never running it. A lambda is written in the call:
// one kept in a variable is not found, and the call says so.

// lambdaKey is a lambda's place: its program, the source as the program names it, its line and column.
type lambdaKey struct {
	program      *runtime.Program
	source       string
	line, column int32
}

// compiled is one lambda of a statement, translated.
type compiled struct {
	entry  *Entry
	word   string   // the statement word it was given to: list, where, select, descending, sum, …
	params []string // the tables it names, in order
	// body is a condition's (where, join, list, …), a key's (ascending, descending, group), a column's (sum,
	// avg, min, max); nil for a condition whose body is a table itself: every row
	body *node
	// shape is a select's object literal; whole the table a select gives itself, (orders) => orders
	shape      []shapeField
	whole      string
	data       []dataEntry     // an insert's, update's, upsert's: the columns written, in the order written
	target     []string        // an upsert's: the columns a row already there is found by
	aggregates bool            // it calls sum, count, avg, min or max
	reads      map[string]bool // the tables whose columns it reads without narrowing the table from null
	nulls      map[string]bool // the tables it compares with null: each a LEFT JOIN
	at         string          // where it is written
}

// dataEntry is one column a write gives a value.
type dataEntry struct {
	field  schema.TableField
	column *declared
	value  *node
	at     string
}

// shapeField is one entry of a select's object.
type shapeField struct {
	name  string
	kind  string // value; spread (...orders); one (customer: users); many (orders: [orders])
	table string // a spread's, a one's, a many's
	value *node  // a value's
	at    string
}

// node is a condition, or a value of one: what SQL the statement is made of.
//
// Conditions: and, or, not, compare (===, !==, <, …), truth (a boolean column alone), in (values.includes(column)),
// text (column.includes(x), startsWith, endsWith), null (a table === null or !== null), case (c ? a : b of
// conditions), constant (true, false), outside (a value from outside, by its truth), matches (full text: the table's
// .searchable() columns, or one of them; search.go).
//
// Values: column, literal, hole (a value from outside: a name and the members read off it), math (+ - * /),
// negate, lower (column.toLowerCase()), aggregate (sum, count, avg, min, max), choose (c ? a : b of values), table
// (a table itself, compared with null or given whole).
type node struct {
	op       string
	operator string // a compare's, a math's, a text's method, an aggregate's function
	left     *node
	right    *node
	third    *node // a case's or a choose's: left the condition, right the consequence, third the alternative
	items    []*node
	table    string    // a column's table, a table's
	column   *declared // a column's
	literal  value.Value
	hole     []string
	fields   []string // a matches': the .searchable() columns it reads
	kind     string   // a value's kind as JavaScript reads it: number, string, boolean, time, json; "" not known (a hole)
	nullable bool     // a value that may be null
	at       string   // where it is written
}

// The statement words, by what their lambda says.
var (
	conditionWords = map[string]bool{"list": true, "first": true, "detail": true, "count": true, "exists": true, "where": true, "join": true, "delete": true, "history": true}
	dataWords      = map[string]bool{"insert": true, "update": true, "upsert": true}
	writeWords     = map[string]bool{"insert": true, "update": true, "upsert": true, "delete": true}
	keyWords       = map[string]bool{"ascending": true, "descending": true, "group": true}
	columnWords    = map[string]bool{"sum": true, "avg": true, "min": true, "max": true}
	actionWords    = map[string]bool{"list": true, "first": true, "detail": true, "count": true, "exists": true, "sum": true, "avg": true, "min": true, "max": true, "insert": true, "update": true, "upsert": true, "delete": true, "history": true}
	aggregateNames = map[string]bool{"sum": true, "count": true, "avg": true, "min": true, "max": true}
	textMethods    = map[string]bool{"includes": true, "startsWith": true, "endsWith": true}
)

func statementWord(word string) bool {
	return conditionWords[word] || keyWords[word] || columnWords[word] || dataWords[word] || word == "select"
}

// Exports records the handles a schema file exports, as the declaring pass ran it: each top-level export whose
// value ends in .tables(…), in the order written, is the next database the file declared.
func (c *Catalog) Exports(file string, text string) {
	parser := compiler.NewParser(compiler.NewLexerSource(text, path.Base(file)))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return
	}
	exported := map[string]bool{}
	for _, name := range program.Exports {
		exported[name] = true
	}
	var mine []*Entry
	for _, entry := range c.entries {
		if strings.HasPrefix(entry.Where, file+":") {
			mine = append(mine, entry)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return placeLine(mine[i].Where) < placeLine(mine[j].Where) })
	next := 0
	for _, statement := range program.Statements {
		declaration, ok := statement.(*compiler.VarStatement)
		if !ok || len(declaration.Names) != 1 || !exported[declaration.Names[0].Value] || !endsInTables(declaration.Value) || next >= len(mine) {
			continue
		}
		if c.handles[file] == nil {
			c.handles[file] = map[string]*Entry{}
		}
		c.handles[file][declaration.Names[0].Value] = mine[next]
		next++
	}
}

func placeLine(where string) int {
	parts := strings.Split(where, ":")
	if len(parts) < 3 {
		return 0
	}
	line := 0
	fmt.Sscanf(parts[len(parts)-2], "%d", &line)
	return line
}

func endsInTables(expression compiler.Expression) bool {
	call, ok := unchain(expression).(*compiler.MethodCallExpression)
	return ok && call.Method != nil && call.Method.Value == "tables"
}

func unchain(expression compiler.Expression) compiler.Expression {
	if chain, ok := expression.(*compiler.ChainExpression); ok {
		return chain.Expression
	}
	return expression
}

// Prepare translates every statement one program writes on a handle the app declared: the program the host
// compiled from entry, and the files it bundled (files). An error says where the statement is written.
func (c *Catalog) Prepare(program *runtime.Program, entry string, files []string) error {
	entryDir := filepath.Dir(entry)
	if absolute, err := filepath.Abs(entryDir); err == nil {
		entryDir = absolute
	}
	root, err := filepath.Abs(c.root)
	if err != nil {
		return err
	}
	for _, file := range files {
		absolute, err := filepath.Abs(file)
		if err != nil {
			continue
		}
		text, err := os.ReadFile(absolute)
		if err != nil {
			continue
		}
		source := relative(entryDir, absolute) // as the bundler and the compiler name it
		written := relative(root, absolute)    // from the app's root
		parser := compiler.NewParser(compiler.NewLexerSource(string(text), source))
		parsed := parser.ParseProgram()
		if len(parser.Errors()) > 0 {
			continue // the compile said it
		}
		handles := c.handlesOf(parsed, written)
		if len(handles) == 0 {
			continue
		}
		reader := &place{text: string(text), file: source}
		names := aggregatesOf(parsed)
		keys := map[*compiler.FunctionLiteral]*compiled{}
		var failure error
		walk(parsed, func(n any) {
			call, ok := n.(*compiler.MethodCallExpression)
			if !ok || failure != nil || call.Method == nil {
				return
			}
			if call.Method.Value == "transaction" {
				// the callback's parameter is the transaction's handle: its statements are the database's
				if entry := handles[rootName(call.Object)]; entry != nil && len(call.Arguments) == 1 {
					if callback, ok := call.Arguments[0].(*compiler.FunctionLiteral); ok && len(callback.Parameters) > 0 {
						handles[callback.Parameters[0].Value] = entry
					}
				}
				return
			}
			if !statementWord(call.Method.Value) {
				return
			}
			entry := handles[rootName(call.Object)]
			if entry == nil {
				return
			}
			for _, argument := range call.Arguments {
				function, ok := argument.(*compiler.FunctionLiteral)
				if !ok {
					continue
				}
				translated, err := translate(function, entry, call.Method.Value, reader, names)
				if err != nil {
					failure = err
					return
				}
				line, column := reader.lineColumn(function.Token.Position)
				c.statements[lambdaKey{program: program, source: source, line: line, column: column}] = translated
				keys[function] = translated
			}
		})
		if failure != nil {
			return failure
		}
		if err := checkChains(parsed, handles, keys, reader); err != nil {
			return err
		}
	}
	return nil
}

// checkChains puts together each statement the file writes whole, db.where(…).list(…), as a request will: what
// only the whole statement shows (a join without its key, a column not grouped) is the boot's error. A
// statement left without its action, its value thrown away, is one too.
func checkChains(parsed *compiler.Program, handles map[string]*Entry, keys map[*compiler.FunctionLiteral]*compiled, reader *place) error {
	inner := map[*compiler.MethodCallExpression]bool{}
	var calls []*compiler.MethodCallExpression
	var unfinished error
	walk(parsed, func(n any) {
		switch e := n.(type) {
		case *compiler.MethodCallExpression:
			if e.Method == nil || !statementWord(e.Method.Value) || handles[rootName(e.Object)] == nil {
				return
			}
			calls = append(calls, e)
			if object, ok := unchain(e.Object).(*compiler.MethodCallExpression); ok {
				inner[object] = true
			}
		case *compiler.ExpressionStatement:
			call, ok := unchain(e.Expression).(*compiler.MethodCallExpression)
			if ok && unfinished == nil && call.Method != nil && statementWord(call.Method.Value) && !actionWords[call.Method.Value] && handles[rootName(call.Object)] != nil {
				unfinished = fmt.Errorf("%s: câu chưa kết thúc: cần list, first, detail, count, exists, sum…; db.%s(…).list()", reader.at(call.Method.Token), call.Method.Value)
			}
		}
	})
	if unfinished != nil {
		return unfinished
	}
	for _, call := range calls {
		if inner[call] || !actionWords[call.Method.Value] {
			continue
		}
		var chain []*compiler.MethodCallExpression
		for link := compiler.Expression(call); ; {
			method, ok := unchain(link).(*compiler.MethodCallExpression)
			if !ok {
				break
			}
			chain = append([]*compiler.MethodCallExpression{method}, chain...)
			link = method.Object
		}
		var steps []step
		whole := true
		for _, link := range chain {
			if link.Method == nil || !statementWord(link.Method.Value) {
				whole = false
				break
			}
			for _, argument := range link.Arguments {
				function, ok := argument.(*compiler.FunctionLiteral)
				if !ok {
					if link.Method.Value != "list" {
						whole = false // a lambda kept in a variable: the run says what it is
					}
					continue
				}
				steps = append(steps, step{compiled: keys[function], descending: link.Method.Value == "descending"})
			}
		}
		if !whole || len(steps) == 0 {
			continue
		}
		if _, err := assemble(steps, call.Method.Value); err != nil {
			return err
		}
	}
	return nil
}

// aggregatesOf are the names a file calls the aggregates of kitwork/database by: import { sum, count } lowers to
// const { sum, count } = kitwork.database, import { sum as total } to const total = kitwork.database.sum, and
// import * as database to const database = kitwork.database (then database.sum(…), keyed "database.sum").
func aggregatesOf(program *compiler.Program) map[string]string {
	names := map[string]string{}
	var visit func(statements []compiler.Statement)
	visit = func(statements []compiler.Statement) {
		for _, statement := range statements {
			switch s := statement.(type) {
			case *compiler.GroupStatement:
				visit(s.Statements)
			case *compiler.VarStatement:
				if isDatabaseModule(s.Value) {
					for _, name := range s.Names {
						if s.DestructMode == compiler.DestructObject && s.Pattern == nil && aggregateNames[name.Value] {
							names[name.Value] = name.Value
						} else if s.DestructMode == compiler.DestructNone {
							for aggregate := range aggregateNames {
								names[name.Value+"."+aggregate] = aggregate
							}
						}
					}
				} else if member, ok := s.Value.(*compiler.MemberExpression); ok && isDatabaseModule(member.Object) && member.Property != nil && aggregateNames[member.Property.Value] && len(s.Names) == 1 {
					names[s.Names[0].Value] = member.Property.Value
				}
			}
		}
	}
	visit(program.Statements)
	return names
}

func isDatabaseModule(expression compiler.Expression) bool {
	member, ok := expression.(*compiler.MemberExpression)
	if !ok || member.Property == nil || member.Property.Value != "database" {
		return false
	}
	object, ok := member.Object.(*compiler.Identifier)
	return ok && object.Value == "kitwork"
}

func relative(base, file string) string {
	rel, err := filepath.Rel(base, file)
	if err != nil {
		return filepath.Base(file)
	}
	return filepath.ToSlash(rel)
}

// handlesOf are the handles a file reaches by its own names: those it imports from a schema file, and those it
// declares itself.
func (c *Catalog) handlesOf(program *compiler.Program, file string) map[string]*Entry {
	handles := map[string]*Entry{}
	for name, entry := range c.handles[file] {
		handles[name] = entry
	}
	for _, statement := range program.Statements {
		imported, ok := statement.(*compiler.ImportStatement)
		if !ok {
			continue
		}
		target := imported.Source
		if strings.HasPrefix(target, "app/") {
			target = strings.TrimPrefix(target, "app/")
		} else {
			target = path.Join(path.Dir(file), target)
		}
		for _, spec := range imported.Names {
			if entry := c.handles[path.Clean(target)][spec.Imported]; entry != nil {
				handles[spec.Local] = entry
			}
		}
	}
	return handles
}

// rootName is the name a chain of calls starts from: db of db.where(…).list(…).
func rootName(expression compiler.Expression) string {
	for {
		switch e := expression.(type) {
		case *compiler.Identifier:
			return e.Value
		case *compiler.MethodCallExpression:
			expression = e.Object
		case *compiler.ChainExpression:
			expression = e.Expression
		default:
			return ""
		}
	}
}

// place reads positions of one file.
type place struct {
	text string
	file string
}

// lineColumn is a position's line and column as the compiler counts them: lines from 1, columns in characters
// from 1.
func (p *place) lineColumn(position int32) (int32, int32) {
	if position < 0 || int(position) > len(p.text) {
		return 0, 0
	}
	before := p.text[:position]
	line := int32(strings.Count(before, "\n") + 1)
	start := strings.LastIndexByte(before, '\n') + 1
	return line, int32(utf8.RuneCountInString(before[start:]) + 1)
}

func (p *place) at(token compiler.Token) string {
	line, column := p.lineColumn(token.Position)
	return fmt.Sprintf("%s:%d:%d", p.file, line, column)
}

// translate reads one lambda of a statement by the word it is given to.
func translate(function *compiler.FunctionLiteral, entry *Entry, word string, reader *place, aggregates map[string]string) (*compiled, error) {
	at := reader.at(function.Token)
	if len(function.Parameters) == 0 {
		return nil, fmt.Errorf("%s: lambda của câu có tham số là bảng: (orders) => …", at)
	}
	out := &compiled{entry: entry, word: word, at: at, reads: map[string]bool{}, nulls: map[string]bool{}}
	t := &translator{entry: entry, word: word, tables: map[string]*Table{}, reader: reader, aggregates: aggregates, narrowed: map[string]bool{}, out: out}
	for _, parameter := range function.Parameters {
		name := parameter.Value
		table := entry.table(name)
		if table == nil {
			return nil, fmt.Errorf("%s: %s không phải bảng của database này; có: %s", at, name, entry.tableNames())
		}
		if t.tables[name] != nil {
			return nil, fmt.Errorf("%s: %s hai lần: mỗi tham số là một bảng; nối một bảng với chính nó đi qua query()", at, name)
		}
		t.tables[name] = table
		out.params = append(out.params, name)
	}
	body, err := lambdaBody(function, at)
	if err != nil {
		return nil, err
	}
	switch {
	case word == "select":
		return out, t.shape(body)
	case dataWords[word]:
		if len(out.params) != 1 {
			return nil, fmt.Errorf("%s: %s ghi một bảng: lambda có một tham số, (orders) => ({ … })", at, word)
		}
		return out, t.data(body)
	case keyWords[word]:
		if isCondition(body) {
			return nil, t.fail(body, "khoá là một cột hay một giá trị gộp, không phải điều kiện: (orders) => orders.day")
		}
		key, err := t.value(body)
		if err != nil {
			return nil, err
		}
		if key.op != "column" && key.op != "aggregate" {
			return nil, t.fail(body, "khoá là một cột hay một giá trị gộp: (orders) => orders.day, (orders) => sum(orders.amount)")
		}
		out.body = key
	case columnWords[word]:
		if isCondition(body) {
			return nil, t.fail(body, "%s cần một cột, điều kiện ở where: db.where((orders) => …).%s((orders) => orders.amount)", word, word)
		}
		column, err := t.value(body)
		if err != nil {
			return nil, err
		}
		if column.op == "table" || !mentions(column) {
			return nil, t.fail(body, "%s cần một cột: db.%s((orders) => orders.amount)", word, word)
		}
		if (word == "sum" || word == "avg") && column.kind != "number" {
			return nil, t.fail(body, "%s cộng số: %s là %s", word, describe(column), column.kind)
		}
		out.body = column
	default:
		if identifier, ok := body.(*compiler.Identifier); ok && t.tables[identifier.Value] != nil {
			return out, nil // every row
		}
		cond, err := t.condition(body)
		if err != nil {
			return nil, err
		}
		out.body = cond
	}
	return out, nil
}

// isCondition reports a body that is a condition, where a value is wanted.
func isCondition(expression compiler.Expression) bool {
	switch e := expression.(type) {
	case *compiler.InfixExpression:
		switch e.Operator {
		case "&&", "||", "===", "!==", "==", "!=", "<", "<=", ">", ">=":
			return true
		}
	case *compiler.PrefixExpression:
		return e.Operator == "!"
	}
	return false
}

// lambdaBody is what a lambda of a statement gives: one expression.
func lambdaBody(function *compiler.FunctionLiteral, at string) (compiler.Expression, error) {
	if function.Body != nil && len(function.Body.Statements) == 1 {
		switch statement := function.Body.Statements[0].(type) {
		case *compiler.ReturnStatement:
			if statement.ReturnValue != nil {
				return statement.ReturnValue, nil
			}
		case *compiler.ExpressionStatement:
			return statement.Expression, nil
		}
	}
	return nil, fmt.Errorf("%s: lambda của câu là một biểu thức: (orders) => orders.day === today", at)
}

func (e *Entry) table(name string) *Table {
	for i := range e.Tables {
		if e.Tables[i].Name == name {
			return &e.Tables[i]
		}
	}
	return nil
}

func (e *Entry) tableNames() string {
	names := make([]string, len(e.Tables))
	for i, table := range e.Tables {
		names[i] = table.Name
	}
	return strings.Join(names, ", ")
}

// translator reads one lambda's body against its tables.
type translator struct {
	entry      *Entry
	word       string
	tables     map[string]*Table
	reader     *place
	aggregates map[string]string
	// narrowed are what the condition has narrowed from null where it is: "orders.note" a .nullable() column,
	// "payments" a table, on the right of x !== null && …, of x === null || …, in a branch of x === null ? … : …
	// (docs/KITWORK_DATABASE.md, "null theo JavaScript")
	narrowed    map[string]bool
	inAggregate bool
	out         *compiled
}

func (t *translator) fail(expression compiler.Expression, format string, arguments ...any) error {
	return fmt.Errorf("%s: "+format, append([]any{t.at(expression)}, arguments...)...)
}

func (t *translator) failAt(token compiler.Token, format string, arguments ...any) error {
	return fmt.Errorf("%s: "+format, append([]any{t.reader.at(token)}, arguments...)...)
}

// narrow runs within with what a condition narrows from null: the right of && after x !== null, of || after
// x === null; a branch of a ternary.
func (t *translator) narrow(cond *node, when string, within func() error) error {
	name := narrowing(cond, when)
	if name == "" || t.narrowed[name] {
		return within()
	}
	t.narrowed[name] = true
	defer delete(t.narrowed, name)
	return within()
}

// narrowing is what a condition narrows from null for what follows it: when "&&" (or the consequence of a
// ternary), a column or a table it holds !== null; when "||" (or the alternative), one it holds === null.
func narrowing(n *node, when string) string {
	want := "!=="
	if when == "||" {
		want = "==="
	}
	switch {
	case n == nil:
	case n.op == "null" && n.operator == want:
		return n.table
	case n.op == "compare" && n.operator == want:
		column, other := n.left, n.right
		if column.op != "column" {
			column, other = other, column
		}
		if column.op == "column" && other.op == "literal" && other.literal.IsNull() {
			return column.table + "." + column.column.name
		}
	}
	return ""
}

// column is table.column of a table of the lambda, with its declaration.
func (t *translator) column(expression compiler.Expression) (*node, bool, error) {
	member, ok := expression.(*compiler.MemberExpression)
	if !ok || member.Property == nil {
		return nil, false, nil
	}
	object, ok := member.Object.(*compiler.Identifier)
	if !ok || t.tables[object.Value] == nil {
		return nil, false, nil
	}
	table := t.tables[object.Value]
	for _, f := range table.Fields {
		if f.Name == member.Property.Value {
			d := declare(f)
			if !t.narrowed[object.Value] {
				t.out.reads[object.Value] = true
			}
			return &node{op: "column", table: object.Value, column: &d, kind: kindOf(&d), nullable: f.Nullable && !t.narrowed[object.Value+"."+f.Name], at: t.reader.at(member.Token)}, true, nil
		}
	}
	if member.Property.Value == "matches" || member.Property.Value == "search" {
		return nil, false, nil
	}
	return nil, false, t.failAt(member.Token, "%s không có cột %s; có: %s", object.Value, member.Property.Value, columnNames(table))
}

// kindOf is a column's kind as JavaScript reads its value.
func kindOf(column *declared) string {
	switch column.kind {
	case "integer", "float", "decimal":
		return "number"
	case "bool":
		return "boolean"
	case "datetime":
		return "time"
	case "json":
		return "json"
	}
	return "string"
}

// nonNull refuses a value that may be null where JavaScript would read it as something else (null < 100 is true,
// null * 2 is 0) or throw (null.includes(x)): it is narrowed from null first.
// doing says what is done with it ("so thứ tự"), example how it reads after the column's name (" < …").
func (t *translator) nonNull(n *node, doing, example string) error {
	if n == nil || !n.nullable || n.op != "column" {
		return nil
	}
	name := n.table + "." + n.column.name
	return fmt.Errorf("%s: %s là .nullable(): %s sau khi thu hẹp khỏi null, %s !== null && %s%s", n.at, name, doing, name, name, example)
}

// condition translates a condition.
func (t *translator) condition(expression compiler.Expression) (*node, error) {
	switch e := expression.(type) {
	case *compiler.InfixExpression:
		switch e.Operator {
		case "&&", "||":
			left, err := t.condition(e.Left)
			if err != nil {
				return nil, err
			}
			var right *node
			if err := t.narrow(left, e.Operator, func() error {
				right, err = t.condition(e.Right)
				return err
			}); err != nil {
				return nil, err
			}
			op := "and"
			if e.Operator == "||" {
				op = "or"
			}
			return &node{op: op, left: left, right: right}, nil
		case "===", "!==", "<", "<=", ">", ">=":
			return t.compare(e)
		case "==", "!=":
			return nil, t.failAt(e.Token, "%s so có đổi kiểu của JavaScript: điều kiện viết %s=", e.Operator, e.Operator)
		}
		return nil, t.failAt(e.Token, "phép %s trong điều kiện chưa có trong engine2", e.Operator)
	case *compiler.PrefixExpression:
		if e.Operator == "!" {
			inner, err := t.condition(e.Right)
			if err != nil {
				return nil, err
			}
			return &node{op: "not", left: inner}, nil
		}
		return nil, t.failAt(e.Token, "phép %s trong điều kiện chưa có trong engine2", e.Operator)
	case *compiler.TernaryExpression:
		cond, err := t.condition(e.Condition)
		if err != nil {
			return nil, err
		}
		var consequence, alternative *node
		if err := t.narrow(cond, "&&", func() error {
			consequence, err = t.condition(e.Consequence)
			return err
		}); err != nil {
			return nil, err
		}
		if err := t.narrow(cond, "||", func() error {
			alternative, err = t.condition(e.Alternative)
			return err
		}); err != nil {
			return nil, err
		}
		return &node{op: "case", left: cond, right: consequence, third: alternative}, nil
	case *compiler.Literal:
		if e.Value.IsBoolean() {
			return &node{op: "constant", literal: e.Value}, nil
		}
	case *compiler.MethodCallExpression:
		return t.call(e)
	case *compiler.MemberExpression:
		column, ok, err := t.column(e)
		if err != nil {
			return nil, err
		}
		if ok {
			if column.column.kind != "bool" {
				return nil, t.failAt(e.Token, "%s.%s là %s, không phải boolean(): so nó với một giá trị", column.table, column.column.name, column.column.spelling)
			}
			return &node{op: "truth", table: column.table, column: column.column, at: column.at}, nil
		}
	}
	if !t.mentionsTable(expression) {
		if path, ok := holePath(expression); ok {
			return &node{op: "outside", hole: path, at: t.at(expression)}, nil
		}
	}
	return nil, fmt.Errorf("%s: điều kiện chưa có trong engine2: %s", t.at(expression), expression.String())
}

// compare translates a comparison: of a column (or a value made of columns) with a value, of two columns, of a
// table with null.
func (t *translator) compare(e *compiler.InfixExpression) (*node, error) {
	left, err := t.value(e.Left)
	if err != nil {
		return nil, err
	}
	right, err := t.value(e.Right)
	if err != nil {
		return nil, err
	}
	at := t.reader.at(e.Token)
	if left.op == "table" || right.op == "table" {
		table, other := left, right
		if table.op != "table" {
			table, other = other, table
		}
		if other.op != "literal" || !other.literal.IsNull() || e.Operator != "===" && e.Operator != "!==" {
			return nil, t.failAt(e.Token, "cả bảng chỉ so với null: %s === null là dòng của %s không có (LEFT JOIN)", table.table, table.table)
		}
		t.out.nulls[table.table] = true
		return &node{op: "null", operator: e.Operator, table: table.table, at: at}, nil
	}
	if !mentions(left) && !mentions(right) {
		return nil, t.failAt(e.Token, "so sánh trong điều kiện có một cột: bảng.cột %s giá_trị", e.Operator)
	}
	ordering := e.Operator != "===" && e.Operator != "!=="
	for _, sides := range [][2]*node{{left, right}, {right, left}} {
		anchor, other := sides[0], sides[1]
		if !mentions(anchor) || other.op != "literal" {
			continue
		}
		if other.literal.IsNull() {
			if ordering {
				return nil, t.failAt(e.Token, "null chỉ so bằng === hay !==, như JavaScript")
			}
			continue
		}
		if anchor.op == "column" && !literalFits(anchor.column, other.literal) || anchor.op != "column" && anchor.kind != "" && literalKind(other.literal) != anchor.kind {
			return nil, t.failAt(e.Token, "%s là %s, %s không phải giá trị của nó", describe(anchor), spelling(anchor), other.literal.ToString().Text())
		}
	}
	if ordering {
		for _, side := range []*node{left, right} {
			if err := t.nonNull(side, "so thứ tự", " "+e.Operator+" …"); err != nil {
				return nil, err
			}
		}
	}
	if mentions(left) && mentions(right) {
		if left.op == "column" && right.op == "column" && left.column.storage != right.column.storage || left.kind != right.kind {
			return nil, t.failAt(e.Token, "%s là %s, %s là %s", describe(left), spelling(left), describe(right), spelling(right))
		}
		if tables := tablesOf(left, tablesOf(right, nil)); len(tables) > 1 && t.word != "join" {
			return nil, t.failAt(e.Token, "điều kiện nối (so cột của %s) viết trong join: db.join((%s) => …)", strings.Join(tables, " và "), strings.Join(tables, ", "))
		}
	}
	if left.kind == "json" || right.kind == "json" {
		return nil, t.failAt(e.Token, "cột json() không dùng trong điều kiện")
	}
	return &node{op: "compare", operator: e.Operator, left: left, right: right, at: at}, nil
}

// call translates a method call in a condition: values.includes(column), column.includes(x),
// column.startsWith(x), column.endsWith(x).
func (t *translator) call(e *compiler.MethodCallExpression) (*node, error) {
	method := ""
	if e.Method != nil {
		method = e.Method.Value
	}
	switch method {
	case "matches":
		return t.matches(e)
	case "search":
		if t.mentionsTable(e.Object) {
			return nil, t.failAt(e.Token, "search của JavaScript trả một vị trí: tìm toàn văn là matches, %s.matches(q)", e.Object.String())
		}
	}
	if !textMethods[method] || len(e.Arguments) != 1 {
		if t.mentionsTable(e) {
			return nil, t.failAt(e.Token, "không dịch được %s: trong câu, trên cột chữ có includes, startsWith, endsWith, toLowerCase", e.String())
		}
		return nil, fmt.Errorf("%s: điều kiện chưa có trong engine2: %s", t.at(e), e.String())
	}
	if method == "includes" && t.mentionsTable(e.Arguments[0]) && !t.mentionsTable(e.Object) {
		return t.membership(e)
	}
	receiver, err := t.value(e.Object)
	if err != nil {
		return nil, err
	}
	if !mentions(receiver) {
		return nil, t.failAt(e.Token, "%s trong điều kiện là danh_sách.includes(bảng.cột), hay bảng.cột.%s(chữ)", method, method)
	}
	if receiver.kind != "string" {
		return nil, t.failAt(e.Token, "%s là %s: %s tìm trong chữ", describe(receiver), spelling(receiver), method)
	}
	if err := t.nonNull(receiver, "gọi "+method, "."+method+"(…)"); err != nil {
		return nil, err
	}
	argument, err := t.value(e.Arguments[0])
	if err != nil {
		return nil, err
	}
	if mentions(argument) {
		return nil, t.failAt(e.Token, "%s(…) nhận một chữ, không phải một cột", method)
	}
	if argument.op == "literal" && !argument.literal.IsString() {
		return nil, t.failAt(e.Token, "%s(…) nhận một chữ", method)
	}
	return &node{op: "text", operator: method, left: receiver, right: argument, at: t.reader.at(e.Token)}, nil
}

// matches translates table.matches(q), over every .searchable() column of the table, and table.column.matches(q),
// over that one (docs/KITWORK_DATABASE.md mục 2): q a string, written or from outside.
func (t *translator) matches(e *compiler.MethodCallExpression) (*node, error) {
	out := &node{op: "matches", at: t.reader.at(e.Token)}
	switch object := e.Object.(type) {
	case *compiler.Identifier:
		table := t.tables[object.Value]
		if table == nil {
			return nil, t.failAt(e.Token, "matches tìm trên bảng hay cột .searchable(): posts.matches(q), posts.title.matches(q)")
		}
		for _, f := range table.Fields {
			if f.Name == "matches" {
				return nil, t.failAt(e.Token, "%s có cột tên matches: %s.matches(…) đọc như cột ấy; tìm trên một cột thì viết tên cột, %s.cột.matches(q)", object.Value, object.Value, object.Value)
			}
			if f.Searchable {
				out.fields = append(out.fields, f.Name)
			}
		}
		if len(out.fields) == 0 {
			return nil, t.failAt(e.Token, "%s không có cột .searchable(): matches tìm trên các cột .searchable() của bảng (docs/KITWORK_SCHEMA.md mục 5)", object.Value)
		}
		out.table = object.Value
	case *compiler.MemberExpression:
		column, ok, err := t.column(object)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, t.failAt(e.Token, "matches tìm trên bảng hay cột .searchable(): posts.matches(q), posts.title.matches(q)")
		}
		if !column.column.field.Searchable {
			return nil, t.failAt(e.Token, "%s.%s không có .searchable(): matches tìm trên cột .searchable()", column.table, column.column.name)
		}
		out.table, out.fields = column.table, []string{column.column.name}
	default:
		return nil, t.failAt(e.Token, "matches tìm trên bảng hay cột .searchable(): posts.matches(q), posts.title.matches(q)")
	}
	if len(e.Arguments) != 1 {
		return nil, t.failAt(e.Token, "matches nhận một chữ: posts.matches(q)")
	}
	argument, err := t.value(e.Arguments[0])
	if err != nil {
		return nil, err
	}
	if mentions(argument) || argument.op == "literal" && !argument.literal.IsString() {
		return nil, t.failAt(e.Token, "matches nhận một chữ, viết thẳng hay từ ngoài lambda: posts.matches(q)")
	}
	out.right = argument
	sort.Strings(out.fields)
	return out, nil
}

// membership translates values.includes(column): the column one of the values (IN).
func (t *translator) membership(e *compiler.MethodCallExpression) (*node, error) {
	column, ok, err := t.column(e.Arguments[0])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, t.failAt(e.Token, "includes trong điều kiện là danh_sách.includes(bảng.cột)")
	}
	if list, ok := e.Object.(*compiler.ArrayLiteral); ok {
		out := &node{op: "in", table: column.table, column: column.column, at: t.reader.at(e.Token)}
		for _, element := range list.Elements {
			item, err := t.value(element)
			if err != nil {
				return nil, err
			}
			if item.op != "literal" || !item.literal.IsNull() && !literalFits(column.column, item.literal) {
				return nil, t.failAt(e.Token, "danh sách viết thẳng chỉ có giá trị của cột %s (%s)", column.column.name, column.column.spelling)
			}
			out.items = append(out.items, item)
		}
		return out, nil
	}
	hole, err := t.value(e.Object)
	if err != nil {
		return nil, err
	}
	if hole.op != "hole" {
		return nil, t.failAt(e.Token, "includes trong điều kiện là danh_sách.includes(bảng.cột), danh sách lấy từ ngoài lambda")
	}
	return &node{op: "in", table: column.table, column: column.column, left: hole, at: t.reader.at(e.Token)}, nil
}

// value translates a value: a column, a literal, a value from outside the lambda, a computation of them.
func (t *translator) value(expression compiler.Expression) (*node, error) {
	at := t.at(expression)
	switch e := expression.(type) {
	case *compiler.Identifier:
		if t.tables[e.Value] != nil {
			return &node{op: "table", table: e.Value, at: at}, nil
		}
		if t.aggregates[e.Value] != "" {
			return nil, t.fail(e, "%s là hàm gộp: %s(orders.amount)", e.Value, e.Value)
		}
	case *compiler.MemberExpression:
		column, ok, err := t.column(e)
		if err != nil {
			return nil, err
		}
		if ok {
			return column, nil
		}
	case *compiler.Literal:
		item := e.Value
		return &node{op: "literal", literal: item, kind: literalKind(item), nullable: item.IsNull(), at: at}, nil
	case *compiler.PrefixExpression:
		if e.Operator == "-" {
			if literal, ok := e.Right.(*compiler.Literal); ok && literal.Value.IsNumber() {
				return &node{op: "literal", literal: value.Number(-literal.Value.Number()), kind: "number", at: at}, nil
			}
			if t.mentionsTable(e.Right) {
				inner, err := t.number(e.Right, "-")
				if err != nil {
					return nil, err
				}
				return &node{op: "negate", left: inner, kind: "number", at: at}, nil
			}
		}
		if e.Operator == "!" && t.mentionsTable(e) {
			return nil, t.failAt(e.Token, "điều kiện đứng chỗ một giá trị: %s", e.String())
		}
	case *compiler.InfixExpression:
		if !t.mentionsTable(e) {
			break
		}
		switch e.Operator {
		case "+", "-", "*", "/":
			left, err := t.number(e.Left, e.Operator)
			if err != nil {
				return nil, err
			}
			right, err := t.number(e.Right, e.Operator)
			if err != nil {
				return nil, err
			}
			return &node{op: "math", operator: e.Operator, left: left, right: right, kind: "number", at: t.reader.at(e.Token)}, nil
		case "%", "**":
			return nil, t.failAt(e.Token, "phép %s trong câu chưa có: + - * / giữa cột và số", e.Operator)
		}
		if isCondition(e) {
			return nil, t.failAt(e.Token, "điều kiện đứng chỗ một giá trị: %s", e.String())
		}
		return nil, t.failAt(e.Token, "phép %s trong câu chưa có: + - * / giữa cột và số", e.Operator)
	case *compiler.TernaryExpression:
		if !t.mentionsTable(e) {
			break
		}
		cond, err := t.condition(e.Condition)
		if err != nil {
			return nil, err
		}
		var consequence, alternative *node
		if err := t.narrow(cond, "&&", func() error {
			consequence, err = t.value(e.Consequence)
			return err
		}); err != nil {
			return nil, err
		}
		if err := t.narrow(cond, "||", func() error {
			alternative, err = t.value(e.Alternative)
			return err
		}); err != nil {
			return nil, err
		}
		kind := consequence.kind
		if kind == "" || consequence.op == "literal" && consequence.literal.IsNull() {
			kind = alternative.kind
		}
		for _, branch := range []*node{consequence, alternative} {
			if branch.op == "table" {
				return nil, t.fail(e, "nhánh của ?: là một giá trị, không phải cả bảng")
			}
			if branch.kind != "" && branch.kind != kind && !(branch.op == "literal" && branch.literal.IsNull()) {
				return nil, t.fail(e, "hai nhánh của ?: khác kiểu: %s và %s", consequence.kind, alternative.kind)
			}
		}
		return &node{op: "choose", left: cond, right: consequence, third: alternative, kind: kind, nullable: consequence.nullable || alternative.nullable, at: at}, nil
	case *compiler.CallExpression:
		if identifier, ok := e.Function.(*compiler.Identifier); ok && t.aggregates[identifier.Value] != "" {
			return t.aggregate(t.aggregates[identifier.Value], e.Arguments, e.Token)
		}
	case *compiler.MethodCallExpression:
		if e.Method == nil {
			break
		}
		if object, ok := e.Object.(*compiler.Identifier); ok && t.aggregates[object.Value+"."+e.Method.Value] != "" {
			return t.aggregate(e.Method.Value, e.Arguments, e.Token)
		}
		if e.Method.Value == "toLowerCase" && t.mentionsTable(e.Object) {
			if len(e.Arguments) != 0 {
				return nil, t.failAt(e.Token, "toLowerCase() không nhận đối số")
			}
			receiver, err := t.value(e.Object)
			if err != nil {
				return nil, err
			}
			if receiver.kind != "string" {
				return nil, t.failAt(e.Token, "%s là %s: toLowerCase đổi chữ", describe(receiver), spelling(receiver))
			}
			if err := t.nonNull(receiver, "gọi toLowerCase", ".toLowerCase()"); err != nil {
				return nil, err
			}
			return &node{op: "lower", left: receiver, kind: "string", at: t.reader.at(e.Token)}, nil
		}
		if textMethods[e.Method.Value] && t.mentionsTable(e) {
			return nil, t.failAt(e.Token, "điều kiện đứng chỗ một giá trị: %s", e.String())
		}
		if e.Method.Value == "search" && t.mentionsTable(e.Object) {
			return nil, t.failAt(e.Token, "search của JavaScript trả một vị trí: tìm toàn văn là matches, một điều kiện: %s.matches(q)", e.Object.String())
		}
		if e.Method.Value == "matches" && t.mentionsTable(e.Object) {
			return nil, t.failAt(e.Token, "matches là một điều kiện, không phải một giá trị: db.list((posts) => posts.matches(q))")
		}
		if t.mentionsTable(e) {
			return nil, t.failAt(e.Token, "không dịch được %s: trong câu, trên cột chữ có includes, startsWith, endsWith, toLowerCase", e.String())
		}
	}
	if t.mentionsTable(expression) {
		return nil, fmt.Errorf("%s: không dịch được %s trong câu", at, expression.String())
	}
	if path, ok := holePath(expression); ok {
		return &node{op: "hole", hole: path, at: at}, nil
	}
	return nil, fmt.Errorf("%s: giá trị từ ngoài lambda là một tên, hay thành phần của nó (user.id): đặt nó vào một const trước câu: %s", at, expression.String())
}

// number translates an operand of + - * /: a number, narrowed from null when it is a column.
func (t *translator) number(expression compiler.Expression, operator string) (*node, error) {
	n, err := t.value(expression)
	if err != nil {
		return nil, err
	}
	if n.op == "table" || n.kind != "" && n.kind != "number" {
		return nil, t.fail(expression, "%s tính với số: %s là %s; nối chữ trong câu chưa có", operator, describe(n), spelling(n))
	}
	if err := t.nonNull(n, "tính "+operator, " "+operator+" …"); err != nil {
		return nil, err
	}
	return n, nil
}

// aggregate translates sum(x), count(), count(x), avg(x), min(x), max(x).
func (t *translator) aggregate(function string, arguments []compiler.Expression, token compiler.Token) (*node, error) {
	if t.inAggregate {
		return nil, t.failAt(token, "%s trong một hàm gộp khác", function)
	}
	if columnWords[t.word] {
		return nil, t.failAt(token, "db.%s đã gộp: db.%s((orders) => orders.amount)", t.word, t.word)
	}
	if t.word == "join" {
		return nil, t.failAt(token, "%s trong join: hàm gộp dùng trong select, where sau group và khoá", function)
	}
	// where it stands in the statement (a where after group) is the whole statement's to say (plan.go)
	t.out.aggregates = true
	out := &node{op: "aggregate", operator: function, kind: "number", at: t.reader.at(token)}
	switch {
	case function == "count" && len(arguments) == 0:
		return out, nil
	case len(arguments) != 1:
		return nil, t.failAt(token, "%s nhận một cột: %s(orders.amount)", function, function)
	}
	t.inAggregate = true
	argument, err := t.value(arguments[0])
	t.inAggregate = false
	if err != nil {
		return nil, err
	}
	if argument.op == "table" || !mentions(argument) {
		return nil, t.failAt(token, "%s nhận một cột: %s(orders.amount)", function, function)
	}
	switch function {
	case "sum", "avg":
		if argument.kind != "number" {
			return nil, t.failAt(token, "%s cộng số: %s là %s", function, describe(argument), spelling(argument))
		}
		out.nullable = function == "avg"
	case "min", "max":
		if argument.kind == "json" || argument.kind == "boolean" {
			return nil, t.failAt(token, "%s so thứ tự: %s là %s", function, describe(argument), spelling(argument))
		}
		out.kind, out.nullable = argument.kind, true
	}
	out.left = argument
	return out, nil
}

// data translates the data of an insert, an update, an upsert (docs/KITWORK_DATABASE.md mục 2): an object of the
// table's columns, each a value. An update reads the row it changes (orders.amount + 1000); an upsert reads a number
// column of the row already there, null in the branch that inserts, which + and - read as 0 as JavaScript does; an
// insert reads no column. A column the engine fills (.created(), .updated()) is never written, nor the key or a
// .readonly() column by an update; an insert names every column it needs.
func (t *translator) data(body compiler.Expression) error {
	param := t.out.params[0]
	table := t.tables[param]
	object, ok := body.(*compiler.ObjectLiteral)
	if !ok {
		if _, isHole := holePath(body); isHole && !t.mentionsTable(body) {
			return t.fail(body, "dữ liệu của %s viết từng cột: (%s) => ({ email, name }); ghi thẳng giá trị của .parse() là lát 4b (docs/KITWORK_SCHEMA.md mục 6)", t.word, param)
		}
		return t.fail(body, "dữ liệu của %s là một object các cột: (%s) => ({ email, name })", t.word, param)
	}
	written := map[string]bool{}
	for _, entry := range object.Entries {
		if entry.IsSpread {
			return t.fail(entry.Value, "spread trong dữ liệu ghi chưa có: viết từng cột, ({ email: form.email, name: form.name }) (docs/KITWORK_DATABASE.md câu 8.25, gán hàng loạt)")
		}
		name := ""
		switch key := entry.Key.(type) {
		case *compiler.Identifier:
			name = key.Value
		case *compiler.Literal:
			if key.Value.IsString() {
				name = key.Value.Text()
			}
		}
		var field *schema.TableField
		for i := range table.Fields {
			if table.Fields[i].Name == name {
				field = &table.Fields[i]
			}
		}
		if field == nil {
			return t.fail(entry.Key, "%s không có cột %s; có: %s", param, name, columnNames(table))
		}
		if written[name] {
			return t.fail(entry.Key, "%s ghi hai lần", name)
		}
		written[name] = true
		switch {
		case field.Created:
			return t.fail(entry.Key, "%s.%s là .created(): engine điền lúc insert, dữ liệu không ghi nó", param, name)
		case field.Updated:
			return t.fail(entry.Key, "%s.%s là .updated(): engine điền lúc insert và mỗi lần update, dữ liệu không ghi nó", param, name)
		case t.word == "update" && field.Key:
			return t.fail(entry.Key, "%s.%s là khoá: update không sửa khoá", param, name)
		case t.word == "update" && field.Readonly:
			return t.fail(entry.Key, "%s.%s là .readonly(): đặt một lần lúc insert, update không ghi nó", param, name)
		}
		d := declare(*field)
		item, err := t.value(entry.Value)
		if err != nil {
			return err
		}
		switch {
		case item.op == "table":
			return t.fail(entry.Value, "%s.%s nhận một giá trị, không phải cả bảng", param, name)
		case mentions(item):
			if t.word == "insert" {
				return t.fail(entry.Value, "insert không đọc cột: dòng chưa có")
			}
			if t.word == "upsert" {
				if column := nonNumberColumn(item); column != nil {
					return fmt.Errorf("%s: upsert chỉ đọc cột số của dòng đã có (null ở nhánh insert, cộng trừ đọc như 0): %s.%s là %s", column.at, column.table, column.column.name, column.column.spelling)
				}
			}
			if aggregateIn(item) != nil {
				return t.fail(entry.Value, "dữ liệu ghi không gộp")
			}
			if item.kind != kindOf(&d) {
				return t.fail(entry.Value, "%s.%s là %s, giá trị ghi là %s", param, name, d.spelling, item.kind)
			}
		case item.op == "literal" && item.literal.IsNull():
			if !field.Nullable {
				return t.fail(entry.Value, "%s.%s không .nullable(): null không ghi được", param, name)
			}
		case item.op == "literal":
			if !literalFits(&d, item.literal) || d.kind == "json" {
				return t.fail(entry.Value, "cột %s là %s, %s không phải giá trị của nó", name, d.spelling, item.literal.ToString().Text())
			}
		}
		t.out.data = append(t.out.data, dataEntry{field: *field, column: &d, value: item, at: t.at(entry.Value)})
	}
	if t.word == "update" {
		return nil
	}
	var missing []string
	for _, f := range table.Fields {
		if !written[f.Name] && required(f) {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		return t.fail(body, "%s thiếu cột bắt buộc của %s: %s", t.word, param, strings.Join(missing, ", "))
	}
	if t.word == "upsert" {
		var targets [][]string
		groups := map[string][]string{}
		var order []string
		for _, f := range table.Fields {
			switch {
			case f.Group != "":
				if groups[f.Group] == nil {
					order = append(order, f.Group)
				}
				groups[f.Group] = append(groups[f.Group], f.Name)
			case (f.Key || f.Unique) && written[f.Name]:
				targets = append(targets, []string{f.Name})
			}
		}
		for _, group := range order {
			whole := true
			for _, name := range groups[group] {
				whole = whole && written[name]
			}
			if whole {
				targets = append(targets, groups[group])
			}
		}
		var names []string
		for _, target := range targets {
			names = append(names, strings.Join(target, "+"))
		}
		switch len(targets) {
		case 0:
			return t.fail(body, "upsert cần một đích so trùng trong dữ liệu: khoá, một cột .unique() hay đủ cột của một nhóm .unique(\"tên\")")
		case 1:
			t.out.target = targets[0]
		default:
			return t.fail(body, "upsert có %d đích so trùng (%s): dữ liệu nêu đủ đúng một", len(targets), strings.Join(names, ", "))
		}
		for _, entry := range t.out.data {
			if entry.field.Readonly && !contains(t.out.target, entry.field.Name) {
				return fmt.Errorf("%s: %s.%s là .readonly(): upsert sửa dòng đã có, nên chỉ ghi nó khi nó là đích so trùng", entry.at, param, entry.field.Name)
			}
		}
	}
	return nil
}

// required reports a column an insert names: not null, no default, nothing the engine fills.
func required(f schema.TableField) bool {
	return !f.Nullable && !f.HasDefault && f.Kind != "id" && !f.Created && !f.Updated
}

// nonNumberColumn is a column a value reads that is not a number, nil when none.
func nonNumberColumn(n *node) *node {
	if n == nil {
		return nil
	}
	if n.op == "column" && n.kind != "number" {
		return n
	}
	for _, child := range append([]*node{n.left, n.right, n.third}, n.items...) {
		if column := nonNumberColumn(child); column != nil {
			return column
		}
	}
	return nil
}

// shape translates a select's lambda: the table itself, or an object of values, spreads (...orders), a table's
// row (customer: users) and the many rows of a table ([orders]).
func (t *translator) shape(body compiler.Expression) error {
	if identifier, ok := body.(*compiler.Identifier); ok && t.tables[identifier.Value] != nil {
		t.out.whole = identifier.Value
		return nil
	}
	object, ok := body.(*compiler.ObjectLiteral)
	if !ok {
		return t.fail(body, "select trả một object, hay chính bảng: db.select((orders) => ({ item: orders.item }))")
	}
	for _, entry := range object.Entries {
		if entry.IsSpread {
			spread := entry.Value
			if s, ok := spread.(*compiler.SpreadExpression); ok {
				spread = s.Value
			}
			identifier, ok := spread.(*compiler.Identifier)
			if !ok || t.tables[identifier.Value] == nil {
				return t.fail(spread, "spread trong select là ...bảng: ({ ...orders, customer: users.name })")
			}
			t.out.reads[identifier.Value] = !t.narrowed[identifier.Value] || t.out.reads[identifier.Value]
			t.out.shape = append(t.out.shape, shapeField{kind: "spread", table: identifier.Value, at: t.at(spread)})
			continue
		}
		name := ""
		switch key := entry.Key.(type) {
		case *compiler.Identifier:
			name = key.Value
		case *compiler.Literal:
			if key.Value.IsString() {
				name = key.Value.Text()
			}
		}
		if name == "" {
			return t.fail(entry.Key, "tên trong select là một chữ: ({ total: … })")
		}
		field := shapeField{name: name, at: t.at(entry.Value)}
		switch v := entry.Value.(type) {
		case *compiler.Identifier:
			if t.tables[v.Value] != nil {
				field.kind, field.table = "one", v.Value
				t.out.shape = append(t.out.shape, field)
				continue
			}
		case *compiler.ArrayLiteral:
			if len(v.Elements) == 1 {
				if identifier, ok := v.Elements[0].(*compiler.Identifier); ok && t.tables[identifier.Value] != nil {
					field.kind, field.table = "many", identifier.Value
					t.out.shape = append(t.out.shape, field)
					continue
				}
			}
			if t.mentionsTable(v) {
				return t.fail(v, "[x] trong select gom các dòng của một bảng: [orders]; %s không phải bảng", v.String())
			}
		}
		if isCondition(entry.Value) {
			return t.fail(entry.Value, "điều kiện đứng chỗ một giá trị: %s", entry.Value.String())
		}
		item, err := t.value(entry.Value)
		if err != nil {
			return err
		}
		field.kind, field.value = "value", item
		t.out.shape = append(t.out.shape, field)
	}
	return nil
}

// mentions reports a value made of a table's columns, not only of values: anything but a literal and a value from
// outside (a computation is made only when it reads a table).
func mentions(n *node) bool { return n.op != "literal" && n.op != "hole" }

// tablesOf adds the tables a value's columns are of, outside aggregates, to tables.
func tablesOf(n *node, tables []string) []string {
	if n == nil {
		return tables
	}
	if n.op == "column" && !contains(tables, n.table) {
		tables = append(tables, n.table)
	}
	for _, child := range []*node{n.left, n.right, n.third} {
		tables = tablesOf(child, tables)
	}
	return tables
}

func contains(items []string, item string) bool {
	for _, each := range items {
		if each == item {
			return true
		}
	}
	return false
}

// describe names a value in a message: orders.amount, or what it is written as.
func describe(n *node) string {
	switch n.op {
	case "column":
		return n.table + "." + n.column.name
	case "aggregate":
		return n.operator + "(…)"
	case "lower":
		return describe(n.left) + ".toLowerCase()"
	case "math":
		return "phép " + n.operator
	case "literal":
		return n.literal.ToString().Text()
	}
	return "giá trị"
}

// spelling is a value's type as a message says it: the column's struct spelling, else its kind.
func spelling(n *node) string {
	if n.op == "column" {
		return n.column.spelling
	}
	if n.kind == "" {
		return "giá trị từ ngoài"
	}
	return n.kind
}

// literalKind is a literal's kind as JavaScript reads it.
func literalKind(item value.Value) string {
	switch {
	case item.IsNumber():
		return "number"
	case item.IsString():
		return "string"
	case item.IsBoolean():
		return "boolean"
	}
	return ""
}

// holePath is a name and the members read off it: user.id, user?.id.
func holePath(expression compiler.Expression) ([]string, bool) {
	switch e := expression.(type) {
	case *compiler.Identifier:
		return []string{e.Value}, true
	case *compiler.MemberExpression:
		base, ok := holePath(e.Object)
		if !ok || e.Property == nil {
			return nil, false
		}
		return append(base, e.Property.Value), true
	case *compiler.ChainExpression:
		return holePath(e.Expression)
	}
	return nil, false
}

// mentionsTable reports an expression reading a table of the lambda.
func (t *translator) mentionsTable(expression any) bool {
	found := false
	walk(expression, func(n any) {
		if identifier, ok := n.(*compiler.Identifier); ok && t.tables[identifier.Value] != nil {
			found = true
		}
	})
	return found
}

func columnNames(table *Table) string {
	names := make([]string, 0, len(table.Fields))
	for _, f := range table.Fields {
		names = append(names, f.Name)
	}
	return strings.Join(names, ", ")
}

// at is where an expression is written: its first token's place.
func (t *translator) at(expression compiler.Expression) string {
	if token, ok := firstToken(expression); ok {
		return t.reader.at(token)
	}
	return t.reader.file
}

func firstToken(expression any) (compiler.Token, bool) {
	v := reflect.ValueOf(expression)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return compiler.Token{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return compiler.Token{}, false
	}
	if field := v.FieldByName("Token"); field.IsValid() {
		if token, ok := field.Interface().(compiler.Token); ok {
			return token, true
		}
	}
	return compiler.Token{}, false
}

// literalFits reports a literal the column holds: a number of a number column, a string of a string one, a boolean
// of a boolean one.
func literalFits(column *declared, literal value.Value) bool {
	switch column.kind {
	case "integer", "float", "decimal":
		return literal.IsNumber()
	case "bool":
		return literal.IsBoolean()
	case "datetime":
		return literal.IsNumber() || literal.IsString()
	case "json":
		return false
	}
	return literal.IsString()
}

// walk visits every node of the compiler's tree under root, root first.
var compilerPackage = reflect.TypeOf(compiler.Program{}).PkgPath()

func walk(root any, visit func(any)) {
	var step func(v reflect.Value)
	step = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				step(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() || v.Elem().Kind() != reflect.Struct || v.Elem().Type().PkgPath() != compilerPackage {
				return
			}
			if v.Elem().Type() == reflect.TypeOf(compiler.Token{}) {
				return
			}
			visit(v.Interface())
			step(v.Elem())
		case reflect.Struct:
			if v.Type() == reflect.TypeOf(compiler.Token{}) {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					step(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				step(v.Index(i))
			}
		}
	}
	step(reflect.ValueOf(root))
}
