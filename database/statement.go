package database

import (
	"context"
	"fmt"
	"maps"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Một câu lệnh (docs/KITWORK_DATABASE.md mục 2): các mô tả (where, ascending, descending) không chạy gì, mỗi chữ trả
// một câu mới (câu cũ không đổi); hành động (list, first, count) chạy câu trên một bản chụp đọc của file. Nhiều where
// là AND; thứ tự là các bước theo thứ tự viết, rồi khoá của bảng.

const (
	// maxList là số dòng list(n) đọc nhiều nhất; defaultList là số dòng list() không số đọc được, quá là lỗi.
	maxList     = 10_000
	defaultList = 1_000
	// rowUnits là năng lượng một dòng đọc ra tốn (đọc, đổi kiểu, dựng object): bộ đếm byte tính phần bộ nhớ của nó.
	rowUnits = 20
	// timestampLayout là cách engine2 lưu timestamp(): chữ UTC tới mili giây.
	timestampLayout = "2006-01-02T15:04:05.000Z"
)

// Query là một câu lệnh: Handle, bảng (tên tham số của hàm đầu tiên quyết), các điều kiện và các bước sắp xếp.
type Query struct {
	handle *Handle
	table  *Table
	where  []bound
	order  []ordered
	// one là chỗ của điều kiện khi câu có một (phần lớn các câu): where trỏ vào nó, câu là một lần cấp phát
	one [1]bound
}

type bound struct {
	condition *condition
	closure   *runtime.Closure
}

type ordered struct {
	column     *column
	descending bool
}

func (q *Query) Key(name string) value.Value {
	for _, word := range statementWords {
		if word == name {
			return javascript.NewNative(name, 1, func(run javascript.Run, arguments []value.Value) value.Value {
				return q.Word(run, name, arguments)
			})
		}
	}
	return noWord(name)
}

// noWord là lỗi của một chữ câu lệnh không có. Word gọi nó chứ không gọi Key: Key giữ q trong một hàm, nên q của Word
// (câu rỗng của Handle.Word) sẽ phải nằm trên heap.
func noWord(name string) value.Value {
	hint := ""
	switch name {
	case "orderBy", "sort", "asc", "desc":
		hint = ": order with .ascending((t) => t.column) or .descending(…)"
	case "limit", "take", "all", "find":
		hint = ": the number of rows is list(n)"
	case "filter":
		hint = ": write .where((t) => …)"
	case "insert", "update", "delete", "upsert", "transaction", "join", "select", "group":
		hint = ": " + name + " comes in a later slice of engine3"
	}
	return value.Fail(value.TypeCode, "a statement has no word %q%s; the words are %s", name, hint, strings.Join(statementWords, ", "))
}

// Word là một chữ của câu lệnh.
func (q *Query) Word(run javascript.Run, name string, arguments []value.Value) value.Value {
	argument := value.Undefined()
	if len(arguments) > 0 {
		argument = arguments[0]
	}
	switch name {
	case "where":
		next, failure := q.with(argument, "where")
		if failure.IsInvalid() {
			return failure
		}
		return value.Struct(next)
	case "ascending", "descending":
		closure, failure := closureOf(argument, name)
		if failure.IsInvalid() {
			return failure
		}
		o, err := translateOrder(closure.Function(), q.handle)
		if err != nil {
			return refused(err)
		}
		if q.table != nil && o.table != q.table {
			return value.Fail(value.TypeCode, ".%s((%s) => …): this statement reads %s; one statement reads one table", name, o.table.name, q.table.name)
		}
		next := q.copy()
		next.table = o.table
		next.order = append(next.order, ordered{column: o.column, descending: name == "descending"})
		return value.Struct(next)
	case "list":
		target, limit := q, defaultList+1
		switch {
		case argument.IsUndefined():
		case argument.IsFunction():
			next, failure := q.with(argument, "list")
			if failure.IsInvalid() {
				return failure
			}
			target = next
		case argument.IsNumber() && argument.Number() >= 1 && argument.Number() <= maxList && argument.Number() == float64(int(argument.Number())):
			limit = int(argument.Number())
		default:
			return value.Fail(value.TypeCode, "list(n) takes a whole number from 1 to %d, or a condition (table) => …, got %s", maxList, describe(argument))
		}
		return target.read(run, "list", limit, !argument.IsNumber())
	case "first", "count":
		target := q
		if !argument.IsUndefined() {
			next, failure := q.with(argument, name)
			if failure.IsInvalid() {
				return failure
			}
			target = next
		}
		return target.read(run, name, 1, false)
	}
	return noWord(name)
}

// with là q và một điều kiện nữa: fn là (table) => …, bảng của nó là bảng của câu.
func (q *Query) with(fn value.Value, word string) (*Query, value.Value) {
	closure, failure := closureOf(fn, word)
	if failure.IsInvalid() {
		return nil, failure
	}
	c, err := translateCondition(closure.Function(), q.handle)
	if err != nil {
		return nil, refused(err)
	}
	if q.table != nil && c.table != q.table {
		return nil, value.Fail(value.TypeCode, ".%s((%s) => …): this statement reads %s; one statement reads one table (join comes in a later slice)",
			word, c.table.name, q.table.name)
	}
	next := q.copy()
	next.table = c.table
	if len(next.where) == 0 {
		next.one[0] = bound{condition: c, closure: closure}
		next.where = next.one[:1:1]
	} else {
		next.where = append(next.where, bound{condition: c, closure: closure})
	}
	return next, value.Value{}
}

func (q *Query) copy() *Query {
	return &Query{handle: q.handle, table: q.table, where: append([]bound(nil), q.where...), order: append([]ordered(nil), q.order...)}
}

// closureOf là hàm của chương trình fn là; một hàm có sẵn không đọc được thân, nên không dịch được.
func closureOf(fn value.Value, word string) (*runtime.Closure, value.Value) {
	if closure, ok := fn.Function().(*runtime.Closure); ok && fn.IsFunction() {
		return closure, value.Value{}
	}
	return nil, value.Fail(value.TypeCode, ".%s(…) takes a function written in the program, like (orders) => orders.status === \"moi\", got %s", word, describe(fn))
}

// read chạy câu: list (tới limit dòng; bounded là list() không số, nhiều hơn defaultList dòng là lỗi), first, count.
func (q *Query) read(run javascript.Run, action string, limit int, bounded bool) value.Value {
	table := q.table
	if table == nil {
		if len(q.handle.names) != 1 {
			return value.Fail(value.TypeCode, "%s() of which table? write a condition naming it, like .%s((%s) => …)", action, action, q.handle.names[0])
		}
		table = q.handle.tables[q.handle.names[0]]
	}
	sql, arguments, failure := q.statement(table, action, limit)
	if failure.IsInvalid() {
		return failure
	}
	ctx := contextOf(run)
	database, err := q.handle.open(ctx)
	if err != nil {
		return value.Fail(value.DatabaseCode, "%v", err)
	}
	meter, _ := run.(javascript.Meter)
	// Query của sqlitewire: kết quả được giữ và đưa lại tới khi một commit đổi bảng câu đọc (cache truy vấn,
	// KITWORK_APPLICATION.md mục 12). Các dòng của nó dùng chung giữa các request, chỉ đọc: mỗi lượt chạy dựng giá trị
	// của riêng nó và trả năng lượng, byte cho từng dòng như khi đọc thẳng.
	result, err := database.Query(ctx, sql, arguments...)
	if err != nil {
		return failed(err, q.handle, table)
	}
	if action == "count" {
		if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
			return value.Fail(value.DatabaseCode, "%s: COUNT(*) gave %d rows", q.handle.shown, len(result.Rows))
		}
		count, ok := result.Rows[0][0].(int64)
		if !ok {
			return value.Fail(value.DatabaseCode, "%s: COUNT(*) gave %T", q.handle.shown, result.Rows[0][0])
		}
		return value.Number(float64(count))
	}
	if action == "first" { // LIMIT 1: một dòng, hay không có
		if len(result.Rows) == 0 {
			return value.Null()
		}
		row, failure := q.decode(meter, table, result.Rows[0])
		if failure.IsInvalid() {
			return failure
		}
		return row
	}
	var rows []value.Value
	if meter != nil { // các dòng đã dựng: bộ đếm thấy chúng khi đếm lại cái lượt chạy giữ
		defer meter.LetGo(meter.Keep(javascript.Kept{Items: &rows}))
	}
	for _, raw := range result.Rows {
		row, failure := q.decode(meter, table, raw)
		if failure.IsInvalid() {
			return failure
		}
		rows = append(rows, row)
	}
	if bounded && len(rows) > defaultList {
		return value.Fail(value.RangeCode, "list() reads %d rows at most and more of %s match: write list(n), up to %d", defaultList, table.name, maxList)
	}
	return value.ArrayOf(rows)
}

// decode là một dòng đọc ra thành object, trả năng lượng và byte của nó cho meter.
func (q *Query) decode(meter javascript.Meter, table *Table, raw []any) (value.Value, value.Value) {
	row, bytes, err := decodeRow(table, raw)
	if err != nil {
		return value.Value{}, failed(err, q.handle, table)
	}
	if meter != nil {
		if failure := meter.Spend(rowUnits); failure.IsInvalid() {
			return value.Value{}, failure
		}
		if failure := meter.Made(bytes); failure.IsInvalid() {
			return value.Value{}, failure
		}
	}
	return row, value.Value{}
}

// statementKey là dạng của một câu: bảng, hành động, số dòng, các điều kiện và các bước sắp xếp (tới keyedParts mỗi
// loại; câu dài hơn dựng SQL mỗi lần chạy). Hai câu cùng dạng có cùng SQL, chỉ đối số khác.
type statementKey struct {
	table  *Table
	action string
	limit  int
	where  [keyedParts]*condition
	order  [keyedParts]ordered
}

const (
	keyedParts = 4
	// maxStatements là số dạng câu một Handle nhớ SQL: quá thì câu dạng mới dựng SQL mỗi lần chạy (list(n) với n đổi
	// theo request làm ra nhiều dạng).
	maxStatements = 256
)

// statements là SQL của các dạng câu một Handle đã chạy (25/11/2026: dựng lại ba câu SQL mỗi request là một phần tám
// thời gian của chuỗi host). Đọc không khoá: bản đồ không đổi sau khi đưa ra; thêm một dạng là chép nó, chỉ trong
// những lần chạy đầu.
type statements struct {
	mu   sync.Mutex
	done atomic.Pointer[map[statementKey]string]
}

func (s *statements) get(key statementKey) (string, bool) {
	if done := s.done.Load(); done != nil {
		sql, found := (*done)[key]
		return sql, found
	}
	return "", false
}

func (s *statements) put(key statementKey, sql string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.done.Load()
	if old != nil && len(*old) >= maxStatements {
		return
	}
	next := map[statementKey]string{key: sql}
	if old != nil {
		maps.Copy(next, *old)
	}
	s.done.Store(&next)
}

// shape là dạng của câu; keyed là false khi câu dài hơn một statementKey giữ được.
func (q *Query) shape(table *Table, action string, limit int) (key statementKey, keyed bool) {
	if len(q.where) > keyedParts || len(q.order) > keyedParts {
		return key, false
	}
	key.table, key.action, key.limit = table, action, limit
	for i, w := range q.where {
		key.where[i] = w.condition
	}
	copy(key.order[:], q.order)
	return key, true
}

// statement là SQL của câu, nhớ theo dạng của nó trên Handle, và các đối số của nó.
func (q *Query) statement(table *Table, action string, limit int) (string, []any, value.Value) {
	key, keyed := q.shape(table, action, limit)
	sql, found := "", false
	if keyed {
		sql, found = q.handle.statements.get(key)
	}
	if !found {
		sql = q.sql(table, action, limit)
		if keyed {
			q.handle.statements.put(key, sql)
		}
	}
	count := 0
	for _, w := range q.where {
		count += len(w.condition.params)
	}
	if count == 0 {
		return sql, nil, value.Value{}
	}
	arguments := make([]any, 0, count)
	for _, w := range q.where {
		for _, p := range w.condition.params {
			argument, failure := p.value(w.closure)
			if failure.IsInvalid() {
				return "", nil, failure
			}
			arguments = append(arguments, argument)
		}
	}
	return sql, arguments, value.Value{}
}

// sql là câu SQL của q: các cột của bảng (hay COUNT(*)), các điều kiện nối bằng AND, các bước sắp xếp rồi khoá của
// bảng, LIMIT.
func (q *Query) sql(table *Table, action string, limit int) string {
	var b strings.Builder
	if action == "count" {
		b.WriteString("SELECT COUNT(*) FROM ")
	} else {
		b.WriteString("SELECT ")
		for i, c := range table.columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quote(c.name))
		}
		b.WriteString(" FROM ")
	}
	b.WriteString(quote(table.name))
	for i, w := range q.where {
		if i == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		b.WriteString("(" + w.condition.sql + ")")
	}
	if action == "count" {
		return b.String()
	}
	b.WriteString(" ORDER BY ")
	for _, o := range q.order {
		b.WriteString(quote(o.column.name))
		if o.descending {
			b.WriteString(" DESC")
		}
		b.WriteString(", ")
	}
	if table.key != "" {
		b.WriteString(quote(table.key))
	} else {
		b.WriteString("rowid")
	}
	b.WriteString(" LIMIT " + strconv.Itoa(limit))
	return b.String()
}

// value là giá trị của tham số lúc câu chạy, đổi theo cách cột nó được so lưu.
func (p param) value(closure *runtime.Closure) (any, value.Value) {
	var v value.Value
	if p.literal != nil {
		switch p.literal.Kind {
		case syntax.BooleanLiteral:
			v = value.Boolean(p.literal.Truth)
		case syntax.NumberLiteral:
			v = value.Number(p.literal.Num)
		default:
			v = value.String(p.literal.Text)
		}
	} else {
		var found bool
		if v, found = closure.Captured(p.path[0]); !found {
			return nil, value.Fail(value.TypeCode, "%s is not a variable the condition can read", p.path[0])
		}
		for _, key := range p.path[1:] {
			if v = javascript.Get(v, key); v.IsInvalid() {
				return nil, v
			}
		}
	}
	return bind(p.column, v, p.path)
}

// bind là v như cột c lưu: decimal là số nguyên × 10^scale, timestamp là chữ UTC, boolean là 0/1. Một giá trị khác loại
// cột là lỗi (chặt như TypeScript: so một chuỗi với một cột số không bao giờ đúng ở JavaScript).
// path là tên bắt được và đường thuộc tính sau nó (user.id), nil cho một giá trị viết thẳng: chỉ lời báo đọc nó.
func bind(c *column, v value.Value, path []string) (any, value.Value) {
	if v.IsNullish() {
		return nil, value.Value{}
	}
	wrong := func(want string) (any, value.Value) {
		name := strings.Join(path, ".")
		if name == "" {
			name = "the value"
		}
		return nil, value.Fail(value.TypeCode, "%s is a %s, compared with column %s (%s): write a %s", name, v.Typeof(), c.name, c.kind, want)
	}
	switch c.kind {
	case "decimal":
		if !v.IsNumber() {
			return wrong("number")
		}
		return int64(math.Round(v.Number() * math.Pow10(c.scale))), value.Value{}
	case "timestamp":
		switch {
		case v.IsNumber():
			return time.UnixMilli(int64(v.Number())).UTC().Format(timestampLayout), value.Value{}
		case v.IsTime():
			return v.Time().UTC().Format(timestampLayout), value.Value{}
		}
		return wrong("number of milliseconds")
	case "boolean":
		if !v.IsBoolean() {
			return wrong("boolean")
		}
		if v.Boolean() {
			return 1, value.Value{}
		}
		return 0, value.Value{}
	case "integer", "float":
		if !v.IsNumber() {
			return wrong("number")
		}
		return number(v.Number()), value.Value{}
	case "id":
		if c.key == "" {
			if !v.IsNumber() {
				return wrong("number")
			}
			return number(v.Number()), value.Value{}
		}
	}
	if !v.IsString() {
		return wrong("string")
	}
	return v.String(), value.Value{}
}

func number(n float64) any {
	if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
		return int64(n)
	}
	return n
}

// decodeRow đọc một dòng của table thành object, mỗi cột như struct nói nó đọc ra: decimal là số, timestamp là số mili
// giây, boolean là true/false, NULL là null. Các dòng mượn chung khoá (Shape của bảng). bytes là cái dòng giữ: phần
// đầu, một Value mỗi cột, và chữ của các chuỗi.
func decodeRow(table *Table, raw []any) (row value.Value, bytes uint64, err error) {
	if len(raw) != len(table.columns) {
		return value.Value{}, 0, fmt.Errorf("%s: the database gave %d columns, the struct has %d", table.name, len(raw), len(table.columns))
	}
	items := make([]value.Value, len(raw))
	for i, c := range table.columns {
		v, err := decode(c, raw[i])
		if err != nil {
			return value.Value{}, 0, fmt.Errorf("%s.%s: %w", table.name, c.name, err)
		}
		if v.IsString() {
			bytes += uint64(len(v.String()))
		}
		items[i] = v
	}
	row = value.ObjectFrom(table.shape, items)
	return row, bytes + row.Shallow(), nil
}

func decode(c column, raw any) (value.Value, error) {
	if raw == nil {
		return value.Null(), nil
	}
	if b, ok := raw.([]byte); ok {
		raw = string(b)
	}
	switch c.kind {
	case "decimal":
		switch n := raw.(type) {
		case int64:
			return value.Number(float64(n) / math.Pow10(c.scale)), nil
		case float64:
			return value.Number(n / math.Pow10(c.scale)), nil
		}
	case "timestamp":
		switch t := raw.(type) {
		case string:
			for _, layout := range []string{timestampLayout, time.RFC3339Nano, "2006-01-02 15:04:05"} {
				if parsed, err := time.Parse(layout, t); err == nil {
					return value.Number(float64(parsed.UnixMilli())), nil
				}
			}
			return value.Value{}, fmt.Errorf("%q is not a time", t)
		case int64:
			return value.Number(float64(t)), nil
		}
	case "boolean":
		if n, ok := raw.(int64); ok {
			return value.Boolean(n != 0), nil
		}
	case "integer", "float", "id":
		switch n := raw.(type) {
		case int64:
			return value.Number(float64(n)), nil
		case float64:
			return value.Number(n), nil
		case string:
			if c.kind == "id" {
				return value.String(n), nil
			}
		}
	default: // string, choice, date
		if s, ok := raw.(string); ok {
			return value.String(s), nil
		}
	}
	return value.Value{}, fmt.Errorf("the database holds %T, the struct says %s", raw, c.kind)
}

// sqliteCode là mã số SQLite viết sau lời báo của nó: "no such table: ghosts (1)".
var sqliteCode = regexp.MustCompile(` \(\d+\)$`)

// failed là lỗi của một câu: file thiếu bảng hay cột struct khai là DATABASE nói migrate; lỗi của bộ đếm giữ mã của nó.
func failed(err error, h *Handle, table *Table) value.Value {
	if failure := value.Error(err); failure.Code() == value.EnergyCode || failure.Code() == value.MemoryCode {
		return failure
	}
	text := sqliteCode.ReplaceAllString(strings.TrimPrefix(err.Error(), "SQL logic error: "), "")
	if strings.Contains(text, "no such table") || strings.Contains(text, "no such column") {
		return value.Fail(value.DatabaseCode, "%s lacks what struct %s declares (%s): migrate adds it", h.shown, table.name, text)
	}
	return value.Fail(value.DatabaseCode, "%s: %v", h.shown, err)
}

// contextOf là context của lượt chạy (request), hay Background khi lượt chạy không có.
func contextOf(run javascript.Run) context.Context {
	if c, ok := run.(interface{ Context() context.Context }); ok {
		if ctx := c.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}
