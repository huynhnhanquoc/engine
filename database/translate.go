package database

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Dịch một hàm điều kiện ((orders) => orders.status === status) sang SQL, từ cây cú pháp của nó (compiler.Function.Syntax),
// một lần cho mỗi hàm đã biên dịch (khác engine2: không đọc lại nguồn, không tìm hàm theo vị trí, nên một hàm giữ trong
// biến vẫn dùng được). Tên tham số là bảng. Một tên bắt được (status, user.id) là một tham số ? của SQL, đọc lúc câu
// chạy. Phần JavaScript dịch được (docs/KITWORK_DATABASE.md mục 2, lát này): && || !, === !== < <= > >=, null, chữ, số,
// true/false, một cột boolean đứng một mình. Thứ khác là lỗi nêu file:dòng:cột và nói nó tới sau.

// condition là một hàm điều kiện đã dịch: bảng, SQL, và nơi lấy giá trị của mỗi dấu ? theo thứ tự.
type condition struct {
	table  *Table
	sql    string
	params []param
}

// param là giá trị của một dấu ?: một giá trị viết thẳng, hay một tên bắt được và đường thuộc tính sau nó (user.id).
type param struct {
	literal *syntax.Literal
	path    []string
	column  *column // cột nó được so với: giá trị đổi theo cách cột lưu (decimal, timestamp…)
}

// orderColumn là một bước ascending((orders) => orders.createdAt): bảng và cột.
type orderColumn struct {
	table  *Table
	column *column
}

// Mỗi Handle nhớ bản dịch của mỗi hàm nó gặp (Handle.conditions, Handle.orders; một hàm dùng với hai Handle là hai bản
// dịch): một Handle dựng trong một request đi cùng các bản dịch của nó khi request xong. Khoá là hàm đã biên dịch, một
// con trỏ: tra không cấp phát.

// translateCondition là bản dịch điều kiện của f với h, nhớ sau lần đầu.
func translateCondition(f *compiler.Function, h *Handle) (*condition, error) {
	translations, key := &h.conditions, f
	if done, found := translations.Load(key); found {
		if err, failed := done.(error); failed {
			return nil, err
		}
		return done.(*condition), nil
	}
	c, err := (&translator{function: f, handle: h}).condition()
	if err != nil {
		translations.Store(key, err)
		return nil, err
	}
	translations.Store(key, c)
	return c, nil
}

// translateOrder là bản dịch của một hàm sắp xếp ((orders) => orders.createdAt).
func translateOrder(f *compiler.Function, h *Handle) (*orderColumn, error) {
	translations, key := &h.orders, f
	if done, found := translations.Load(key); found {
		if err, failed := done.(error); failed {
			return nil, err
		}
		return done.(*orderColumn), nil
	}
	t := &translator{function: f, handle: h}
	o, err := t.order()
	if err != nil {
		translations.Store(key, err)
		return nil, err
	}
	translations.Store(key, o)
	return o, nil
}

type translator struct {
	function *compiler.Function
	handle   *Handle
	table    *Table
	param    string // tên tham số: tên bảng
	params   []param
}

func (t *translator) fail(at syntax.Pos, format string, arguments ...any) error {
	return &placed{at: fmt.Sprintf("%s:%d:%d", t.function.File, at.Line, at.Column), message: fmt.Sprintf(format, arguments...)}
}

// placed là lỗi của một bản dịch, ở chỗ của nó trong hàm điều kiện: lỗi của câu lệnh mang chỗ ấy (refused), không phải
// chỗ của lời gọi .where(…).
type placed struct{ at, message string }

func (p *placed) Error() string { return p.at + ": " + p.message }

// refused là lỗi err của một bản dịch thành lỗi của câu lệnh, ở chỗ của nó.
func refused(err error) value.Value {
	var p *placed
	if errors.As(err, &p) {
		return value.Fail(value.TypeCode, "%s", p.message).Within(p.at)
	}
	return value.Fail(value.TypeCode, "%v", err)
}

// body là thân của hàm: một biểu thức, tham số là một bảng của Handle.
func (t *translator) body(word string) (syntax.Expression, error) {
	arrow := t.function.Syntax
	if arrow == nil {
		return nil, fmt.Errorf("%s takes a function (table) => …", word)
	}
	if len(arrow.Parameters) != 1 || arrow.Parameters[0].Default != nil {
		return nil, t.fail(arrow.Pos, "%s takes a function of one table, like (orders) => …", word)
	}
	t.param = arrow.Parameters[0].Name
	t.table = t.handle.tables[t.param]
	if t.table == nil {
		return nil, t.fail(arrow.Parameters[0].Pos, "%s has no table %s: the parameter names the table, one of %s", t.handle.shown,
			t.param, strings.Join(t.handle.names, ", "))
	}
	expression, ok := arrow.Body.(syntax.Expression)
	if !ok {
		return nil, t.fail(arrow.Pos, "write the %s as an expression, (%s) => …, not a block with return", word, t.param)
	}
	return expression, nil
}

func (t *translator) condition() (*condition, error) {
	expression, err := t.body("condition")
	if err != nil {
		return nil, err
	}
	sql, err := t.boolean(expression)
	if err != nil {
		return nil, err
	}
	return &condition{table: t.table, sql: sql, params: t.params}, nil
}

func (t *translator) order() (*orderColumn, error) {
	expression, err := t.body("order")
	if err != nil {
		return nil, err
	}
	c, isColumn, err := t.columnOf(expression)
	if err != nil {
		return nil, err
	}
	if !isColumn {
		return nil, t.fail(expression.At(), "an order is one column, (%s) => %s.column", t.param, t.param)
	}
	return &orderColumn{table: t.table, column: c}, nil
}

// boolean dịch một biểu thức đúng hay sai.
func (t *translator) boolean(e syntax.Expression) (string, error) {
	switch e := e.(type) {
	case *syntax.Logical:
		if e.Op != "&&" && e.Op != "||" {
			return "", t.fail(e.Pos, "%s in a condition comes in a later slice of engine3", e.Op)
		}
		left, err := t.boolean(e.Left)
		if err != nil {
			return "", err
		}
		right, err := t.boolean(e.Right)
		if err != nil {
			return "", err
		}
		op := " AND "
		if e.Op == "||" {
			op = " OR "
		}
		return "(" + left + op + right + ")", nil
	case *syntax.Unary:
		if e.Op != "!" {
			return "", t.fail(e.Pos, "%s in a condition comes in a later slice of engine3", e.Op)
		}
		inner, err := t.boolean(e.Operand)
		if err != nil {
			return "", err
		}
		return "NOT (" + inner + ")", nil
	case *syntax.Binary:
		return t.compare(e)
	case *syntax.Member:
		c, isColumn, err := t.columnOf(e)
		if err != nil {
			return "", err
		}
		if isColumn && c.kind == "boolean" {
			return quote(c.name) + " = 1", nil
		}
		if isColumn {
			return "", t.fail(e.Pos, "%s.%s is a %s, not true or false: compare it, like %s.%s === …", t.param, c.name, c.kind, t.param, c.name)
		}
	case *syntax.Literal:
		if e.Kind == syntax.BooleanLiteral {
			if e.Truth {
				return "1", nil
			}
			return "0", nil
		}
	}
	return "", t.fail(e.At(), "this condition comes in a later slice of engine3: a condition is comparisons of columns (===, !==, <, <=, >, >=) joined by &&, ||, !")
}

// comparisons là các phép so, và phép SQL của chúng. === là IS, không phải =: so cả null như JavaScript (null === null).
var comparisons = map[string]string{"===": "IS", "!==": "IS NOT", "<": "<", "<=": "<=", ">": ">", ">=": ">="}

// compare dịch một phép so: ít nhất một vế là một cột; vế kia là một cột, một giá trị viết thẳng, hay một tên bắt được.
func (t *translator) compare(e *syntax.Binary) (string, error) {
	op, ok := comparisons[e.Op]
	if !ok { // == và != thì parser đã từ chối
		return "", t.fail(e.Pos, "%s in a condition comes in a later slice of engine3", e.Op)
	}
	left, leftColumn, leftParam, err := t.operand(e.Left)
	if err != nil {
		return "", err
	}
	right, rightColumn, rightParam, err := t.operand(e.Right)
	if err != nil {
		return "", err
	}
	if leftColumn == nil && rightColumn == nil {
		return "", t.fail(e.Pos, "this comparison has no column of %s: write it outside the condition", t.param)
	}
	ordering := op != "IS" && op != "IS NOT"
	for _, c := range []*column{leftColumn, rightColumn} {
		if ordering && c != nil && c.nullable {
			return "", t.fail(e.Pos, "%s.%s may be null and %s orders it: comparing a nullable column comes in a later slice of engine3", t.param, c.name, e.Op)
		}
		if ordering && c != nil && (c.kind == "boolean" || c.kind == "choice") {
			return "", t.fail(e.Pos, "%s.%s is a %s: %s does not order it", t.param, c.name, c.kind, e.Op)
		}
	}
	// một giá trị so với một cột đổi theo cách cột ấy lưu
	if leftParam >= 0 {
		t.params[leftParam].column = rightColumn
	}
	if rightParam >= 0 {
		t.params[rightParam].column = leftColumn
	}
	return left + " " + op + " " + right, nil
}

// operand dịch một vế của phép so: SQL của nó, cột nếu nó là một cột, và chỗ của tham số ? nó thêm (-1 khi không).
func (t *translator) operand(e syntax.Expression) (string, *column, int, error) {
	switch e := e.(type) {
	case *syntax.Literal:
		switch e.Kind {
		case syntax.NullLiteral:
			return "NULL", nil, -1, nil
		case syntax.UndefinedLiteral:
			return "", nil, -1, t.fail(e.Pos, "a column is never undefined: compare it with null")
		}
		t.params = append(t.params, param{literal: e})
		return "?", nil, len(t.params) - 1, nil
	case *syntax.Member, *syntax.Identifier:
		c, isColumn, err := t.columnOf(e)
		if err != nil {
			return "", nil, -1, err
		}
		if isColumn {
			return quote(c.name), c, -1, nil
		}
		path, err := t.path(e)
		if err != nil {
			return "", nil, -1, err
		}
		t.params = append(t.params, param{path: path})
		return "?", nil, len(t.params) - 1, nil
	}
	return "", nil, -1, t.fail(e.At(), "a value computed in the condition comes in a later slice of engine3: put it in a const before the statement")
}

// columnOf là cột của e khi e là bảng.cột; false (không lỗi) khi e là thứ khác.
func (t *translator) columnOf(e syntax.Expression) (*column, bool, error) {
	member, ok := e.(*syntax.Member)
	if !ok {
		if id, isName := e.(*syntax.Identifier); isName && id.Name == t.param {
			return nil, false, t.fail(id.Pos, "%s is the whole table: name a column, %s.column", t.param, t.param)
		}
		return nil, false, nil
	}
	root, ok := member.Object.(*syntax.Identifier)
	if !ok || root.Name != t.param {
		if inner, nested := member.Object.(*syntax.Member); nested {
			if id, isName := inner.Object.(*syntax.Identifier); isName && id.Name == t.param {
				return nil, false, t.fail(member.Pos, "%s.%s.%s: a column has no keys here", t.param, inner.Name, member.Name)
			}
		}
		return nil, false, nil
	}
	c := t.table.column(member.Name)
	if c == nil {
		return nil, false, t.fail(member.Pos, "%s has no column %s: %s", t.param, member.Name, t.table.names())
	}
	return c, true, nil
}

// path là một tên bắt được và các khoá sau nó: status, user.id. Tên phải là một biến hàm bắt (của hàm bao nó, hay của
// cấp file).
func (t *translator) path(e syntax.Expression) ([]string, error) {
	switch e := e.(type) {
	case *syntax.Identifier:
		if !slices.ContainsFunc(t.function.Captures, func(c compiler.Capture) bool { return c.Name == e.Name }) {
			return nil, t.fail(e.Pos, "%s is not a variable the condition can read: a condition reads its table, values written in it, and variables around it", e.Name)
		}
		return []string{e.Name}, nil
	case *syntax.Member:
		head, err := t.path(e.Object)
		if err != nil {
			return nil, err
		}
		return append(head, e.Name), nil
	}
	return nil, t.fail(e.At(), "a value computed in the condition comes in a later slice of engine3: put it in a const before the statement")
}

// quote là tên một bảng hay cột trong SQL, trong dấu huyền: SQLite luôn đọc nó là một tên. Trong ngoặc kép, một tên
// file không có (một cột struct khai mà migrate chưa thêm) thành một chuỗi, và câu chạy không lỗi trên chữ ấy (DQS của
// SQLite). Tên đã qua tableName, struct: không có dấu huyền.
func quote(name string) string { return "`" + name + "`" }
