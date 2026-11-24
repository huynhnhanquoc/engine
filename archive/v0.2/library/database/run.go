package database

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	sqlite "modernc.org/sqlite"
)

// The read statements (docs/database.md mục 3; slice 3 of the root's docs/KITWORK_SCHEMA.md mục 8): list, first,
// detail, count, exists, sum, avg, min, max, after where, join, select, group, ascending and descending, put
// together by assemble (plan.go). A statement runs on the database's file through KitDB (opened at the first
// statement), reading one snapshot; a row reads as the struct declares it: a decimal its number, a boolean true or
// false, a timestamp its milliseconds, json its value; a .secret() column only when a select names it. A database
// the boot found lacking fails every statement, saying what it lacks.

// NoRow begins the failure of a detail that found no row, which a route answers 404 (host/site.go): a failure's
// code and text are all the runtime carries of it, as it writes where it was raised.
const NoRow = "không có dòng nào của "

// defaultLimit is the rows a list without a limit reads; maxLimit the most a limit asks.
const (
	defaultLimit = 1000
	maxLimit     = 10000
)

// kitwork_lower is toLowerCase in a statement: Unicode's lower case as the engine's own toLowerCase gives it
// (value.ToLowerCase), where SQLite's lower changes ASCII letters only.
func init() {
	sqlite.MustRegisterFunction("kitwork_lower", &sqlite.FunctionImpl{NArgs: 1, Deterministic: true,
		Scalar: func(_ *sqlite.FunctionContext, arguments []driver.Value) (driver.Value, error) {
			switch text := arguments[0].(type) {
			case string:
				return strings.ToLower(text), nil
			case []byte:
				return strings.ToLower(string(text)), nil
			}
			return arguments[0], nil
		}})
}

// reading runs read in one snapshot of the file, or in the handle's transaction.
func (q *Query) reading(database *kitdb.Database, read func(querier) error) error {
	if tx := q.handle.tx; tx != nil {
		return read(tx.tx)
	}
	ctx, cancel := value.WaitContext(q.handle.waits, time.Time{})
	defer cancel()
	return database.Read(ctx, func(snapshot *kitdb.Snapshot) error { return read(snapshot) })
}

// Query is a statement being written: db.where(…).descending(…), until its action.
type Query struct {
	handle *Handle
	steps  []step
	moment *time.Time // at(…)'s: the statement reads the table as it was then (history.go)
}

// Freeze is what value.Freeze calls on a statement a site's file made as it booted: no step writes into one.
func (q *Query) Freeze() {}

func (h *Handle) query() *Query { return &Query{handle: h} }

// The statements of the handle: each starts a Query.

func (h *Handle) List(arguments ...value.Value) value.Value {
	return h.query().terminal("list", arguments)
}
func (h *Handle) First(arguments ...value.Value) value.Value {
	return h.query().terminal("first", arguments)
}
func (h *Handle) Detail(arguments ...value.Value) value.Value {
	return h.query().terminal("detail", arguments)
}
func (h *Handle) Count(arguments ...value.Value) value.Value {
	return h.query().terminal("count", arguments)
}
func (h *Handle) Exists(arguments ...value.Value) value.Value {
	return h.query().terminal("exists", arguments)
}
func (h *Handle) Sum(arguments ...value.Value) value.Value {
	return h.query().terminal("sum", arguments)
}
func (h *Handle) Avg(arguments ...value.Value) value.Value {
	return h.query().terminal("avg", arguments)
}
func (h *Handle) Min(arguments ...value.Value) value.Value {
	return h.query().terminal("min", arguments)
}
func (h *Handle) Max(arguments ...value.Value) value.Value {
	return h.query().terminal("max", arguments)
}
func (h *Handle) Where(arguments ...value.Value) value.Value  { return h.query().Where(arguments...) }
func (h *Handle) Join(arguments ...value.Value) value.Value   { return h.query().Join(arguments...) }
func (h *Handle) Select(arguments ...value.Value) value.Value { return h.query().Select(arguments...) }
func (h *Handle) Group(arguments ...value.Value) value.Value  { return h.query().Group(arguments...) }
func (h *Handle) Ascending(arguments ...value.Value) value.Value {
	return h.query().Ascending(arguments...)
}
func (h *Handle) Descending(arguments ...value.Value) value.Value {
	return h.query().Descending(arguments...)
}

func (q *Query) List(arguments ...value.Value) value.Value   { return q.terminal("list", arguments) }
func (q *Query) First(arguments ...value.Value) value.Value  { return q.terminal("first", arguments) }
func (q *Query) Detail(arguments ...value.Value) value.Value { return q.terminal("detail", arguments) }
func (q *Query) Count(arguments ...value.Value) value.Value  { return q.terminal("count", arguments) }
func (q *Query) Exists(arguments ...value.Value) value.Value { return q.terminal("exists", arguments) }
func (q *Query) Sum(arguments ...value.Value) value.Value    { return q.terminal("sum", arguments) }
func (q *Query) Avg(arguments ...value.Value) value.Value    { return q.terminal("avg", arguments) }
func (q *Query) Min(arguments ...value.Value) value.Value    { return q.terminal("min", arguments) }
func (q *Query) Max(arguments ...value.Value) value.Value    { return q.terminal("max", arguments) }

// Where is where((orders) => condition): the rows the statement reads; after group, the groups (HAVING).
func (q *Query) Where(arguments ...value.Value) value.Value { return q.description("where", arguments) }

// Join is join((orders, leads) => orders.email === leads.email): how a table joins the one before it.
func (q *Query) Join(arguments ...value.Value) value.Value { return q.description("join", arguments) }

// Select is select((orders, users) => ({ … })): the shape of a row.
func (q *Query) Select(arguments ...value.Value) value.Value {
	return q.description("select", arguments)
}

// Group is group((orders) => orders.day): a row for each value of the key.
func (q *Query) Group(arguments ...value.Value) value.Value { return q.description("group", arguments) }

// Ascending is ascending((orders) => orders.day): ordered by that key, smallest first.
func (q *Query) Ascending(arguments ...value.Value) value.Value {
	return q.description("ascending", arguments)
}

// Descending is descending((orders) => orders.day): ordered by that key, largest first.
func (q *Query) Descending(arguments ...value.Value) value.Value {
	return q.description("descending", arguments)
}

// alive refuses a statement on a transaction's handle once its transaction has ended.
func (q *Query) alive() value.Value {
	if tx := q.handle.tx; tx != nil && tx.closed {
		return value.Fail(value.InvalidArgumentCode, "handle của giao dịch hết hiệu lực khi giao dịch xong: giữ nó lại để dùng sau là lỗi")
	}
	return value.Value{}
}

func (q *Query) description(word string, arguments []value.Value) value.Value {
	if failure := q.alive(); failure.IsInvalid() {
		return failure
	}
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, word+" nhận một lambda: db."+word+"((orders) => …)")
	}
	next, failure := q.with(arguments[0], word)
	if failure.IsInvalid() {
		return failure
	}
	return value.Struct(next)
}

// MissingMember is a statement word of a later slice, or none.
func (q *Query) MissingMember(name string) value.Value { return q.handle.MissingMember(name) }

// with is the query with one more lambda: the one the boot translated at its place.
func (q *Query) with(fn value.Value, word string) (*Query, value.Value) {
	lambda := fn.Lambda()
	if lambda == nil {
		return nil, value.Fail(value.InvalidArgumentCode, word+" nhận một lambda viết ngay trong lời gọi: db."+word+"((orders) => …)")
	}
	// a closure holds its program through the VM's origin: the program itself is the key the boot wrote
	program, _ := runtime.ProgramFromRef(lambda.Program)
	compiled := q.handle.catalog.statements[lambdaKey{program: program, source: lambda.SourceFile, line: lambda.SourceLine, column: lambda.SourceColumn}]
	if compiled == nil {
		return nil, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("lambda ở %s:%d:%d không phải lambda của một câu viết trên database: lambda viết ngay trong lời gọi, db.list((orders) => …), không giữ trong một biến", lambda.SourceFile, lambda.SourceLine, lambda.SourceColumn))
	}
	if compiled.entry != q.handle.entry {
		return nil, value.Fail(value.InvalidArgumentCode, compiled.at+": lambda của câu trên một database khác")
	}
	if compiled.word != word {
		return nil, value.Fail(value.InvalidArgumentCode, compiled.at+": lambda này là của "+compiled.word+", không phải của "+word)
	}
	next := &Query{handle: q.handle, moment: q.moment, steps: append(append([]step(nil), q.steps...), step{compiled: compiled, lambda: lambda, descending: word == "descending"})}
	return next, value.Value{}
}

// plan is the statement put together, kept by its lambdas: a statement written in one place assembles once.
func (q *Query) plan(action string) (*plan, value.Value) {
	var key strings.Builder
	key.WriteString(action)
	for _, s := range q.steps {
		fmt.Fprintf(&key, "|%p", s.compiled)
	}
	catalog := q.handle.catalog
	catalog.mutex.Lock()
	p := catalog.plans[key.String()]
	catalog.mutex.Unlock()
	if p != nil {
		return p, value.Value{}
	}
	p, err := assemble(q.steps, action)
	if err != nil {
		return nil, value.Fail(value.InvalidArgumentCode, err.Error())
	}
	catalog.mutex.Lock()
	catalog.plans[key.String()] = p
	catalog.mutex.Unlock()
	return p, value.Value{}
}

// terminal runs the statement: its arguments are a lambda (a condition's; a column's for sum, avg, min, max), and
// a list's limit, list(20), or page, list({ page: 2, limit: 20 }).
func (q *Query) terminal(action string, arguments []value.Value) value.Value {
	return q.terminalWithin(action, arguments, nil)
}

// terminalWithin is terminal by a run whose budget is budget, which a write carries to what it writes as JSON (the
// plan is the catalogue's, shared by every run: the budget goes beside it).
func (q *Query) terminalWithin(action string, arguments []value.Value, budget value.Budget) value.Value {
	if failure := q.alive(); failure.IsInvalid() {
		return failure
	}
	limit, offset := defaultLimit, 0
	// a list that names no count reads defaultLimit rows and one more, to say when there are more (unnamed)
	unnamed := action == "list"
	for _, argument := range arguments {
		switch {
		case argument.IsFunction():
			next, failure := q.with(argument, action)
			if failure.IsInvalid() {
				return failure
			}
			q = next
		case action == "list" && argument.IsNumber():
			n, failure := whole(argument, "list(n): n", 1, maxLimit)
			if failure.IsInvalid() {
				return failure
			}
			limit, unnamed = n, false
		case action == "list" && argument.IsObject():
			page, size, failure := pageOf(argument)
			if failure.IsInvalid() {
				return failure
			}
			limit, offset, unnamed = size, (page-1)*size, false
		default:
			return value.Fail(value.InvalidArgumentCode, action+" nhận một lambda: db."+action+"((orders) => …)")
		}
	}
	if len(arguments) > 1 && action != "list" {
		return value.Fail(value.InvalidArgumentCode, action+" nhận một lambda")
	}
	p, failure := q.plan(action)
	if failure.IsInvalid() {
		return failure
	}
	entry := q.handle.entry
	if report := entry.Report; report.Blocking() {
		return value.Fail(value.DatabaseCode, blockingMessage(report))
	}
	database, failure := q.handle.catalog.database(entry)
	if failure.IsInvalid() {
		return failure
	}
	if action == "first" || action == "detail" {
		limit, offset = 1, 0
	}
	if unnamed {
		limit = defaultLimit + 1
	}
	if q.moment != nil {
		switch {
		case writeWords[action] || action == "history":
			return value.Fail(value.InvalidArgumentCode, "at() đứng trước một câu đọc: db.at(lúc).list(…); "+action+" không đọc quá khứ")
		case p.matches != nil:
			return value.Fail(value.UnsupportedCode, "at() cùng matches chưa có: chỉ mục tìm kiếm là của bây giờ")
		case len(p.tables) > 1 || p.many != nil:
			return value.Fail(value.UnsupportedCode, "at() trên nhiều bảng chưa có trong engine2: đọc từng bảng như lúc đó")
		}
		if first := action == "first" || action == "detail"; first {
			limit, offset = 1, 0
		}
		r := &renderer{plan: p, steps: q.steps}
		return listed(q.past(p, database, r, func() (string, value.Value) { return r.statement(limit, offset) }), unnamed)
	}
	if action == "history" {
		return q.history(p, database)
	}
	if writeWords[action] {
		return q.write(p, database, budget)
	}
	if p.matches != nil {
		return listed(q.searched(p, database, limit, offset), unnamed)
	}
	r := &renderer{plan: p, steps: q.steps}
	text, failure := r.statement(limit, offset)
	if failure.IsInvalid() {
		return failure
	}
	var result value.Value
	err := q.reading(database, func(read querier) error {
		var err error
		result, err = run(read, p, r, text)
		return err
	})
	if err != nil {
		return value.Fail(value.DatabaseCode, entry.File()+": "+err.Error())
	}
	return listed(result, unnamed)
}

// listed is what list() gives: rows, or for a list that named no count and found more than defaultLimit rows, the
// failure saying how to write one (15/11/2026, the owner, round 3 of the 13/11 review). It read the first 1000 and
// left the rest in silence, so a page listing every order showed a thousand and looked whole.
func listed(rows value.Value, unnamed bool) value.Value {
	if unnamed && rows.IsArray() && len(rows.Items()) > defaultLimit {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("list() không nêu số dòng đọc tối đa %d dòng, mà còn hơn %d dòng khớp: viết list(n) với n tới %d, list({ page, limit }), hay thu hẹp điều kiện",
			defaultLimit, defaultLimit, maxLimit))
	}
	return rows
}

// whole is an integer from min to max.
func whole(item value.Value, what string, min, max int) (int, value.Value) {
	n := item.Number()
	if !item.IsNumber() || n != math.Trunc(n) || n < float64(min) || n > float64(max) {
		return 0, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s là số nguyên từ %d tới %d", what, min, max))
	}
	return int(n), value.Value{}
}

// pageOf reads list({ page, limit }): page from 1, limit from 1 to maxLimit, 1000 when not given.
func pageOf(item value.Value) (int, int, value.Value) {
	page, limit := 0, defaultLimit
	for _, key := range item.Keys() {
		switch key {
		case "page":
			n, failure := whole(item.Get(key), "page", 1, math.MaxInt32)
			if failure.IsInvalid() {
				return 0, 0, failure
			}
			page = n
		case "limit":
			n, failure := whole(item.Get(key), "limit", 1, maxLimit)
			if failure.IsInvalid() {
				return 0, 0, failure
			}
			limit = n
		default:
			return 0, 0, value.Fail(value.InvalidArgumentCode, "list({ page, limit }) không có "+key)
		}
	}
	if page == 0 {
		return 0, 0, value.Fail(value.InvalidArgumentCode, "list({ page, limit }) cần page: list({ page: 2, limit: 20 })")
	}
	return page, limit, value.Value{}
}

// run reads the statement in one snapshot: the main query, then the many side of a select by its foreign key.
func run(read querier, p *plan, r *renderer, text string) (value.Value, error) {
	rows, err := read.Query(text, r.args...)
	if err != nil {
		return value.Value{}, err
	}
	defer rows.Close()
	switch {
	case p.action == "count":
		var n int64
		if rows.Next() {
			if err := rows.Scan(&n); err != nil {
				return value.Value{}, err
			}
		}
		return value.Number(float64(n)), rows.Err()
	case p.action == "exists":
		found := rows.Next()
		return value.Boolean(found), rows.Err()
	case p.column != nil:
		var cell any
		if rows.Next() {
			if err := rows.Scan(&cell); err != nil {
				return value.Value{}, err
			}
		}
		column := p.column.node
		if column.op == "column" && p.action != "avg" {
			return decode(*column.column, cell), rows.Err()
		}
		return decodeKind("number", cell), rows.Err()
	}
	got, _, failure, err := readRows(read, p, r, rows)
	if err != nil || failure.IsInvalid() {
		return failure, err
	}
	return shaped(p, got), nil
}

// readRows reads the rows of a statement's main query, then the many side of its select; rowids are the rows'
// rowids when the renderer asked for them (search.go).
func readRows(source querier, p *plan, r *renderer, rows *sql.Rows) ([]value.Value, []int64, value.Value, error) {
	columns, _ := rows.Columns()
	var read []value.Value
	var parents []any
	var rowids []int64
	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, nil, value.Value{}, err
		}
		row, parent, rowid, failure := r.row(cells)
		if failure.IsInvalid() {
			return nil, nil, failure, nil
		}
		read = append(read, row)
		parents = append(parents, parent)
		rowids = append(rowids, rowid)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, value.Value{}, err
	}
	rows.Close()
	if p.many != nil && len(read) > 0 {
		if err := many(source, p.many, read, parents); err != nil {
			return nil, nil, value.Value{}, err
		}
	}
	return read, rowids, value.Value{}, nil
}

// shaped is the rows read as the action gives them: list all of them, first one or null, detail one or NOT_FOUND.
func shaped(p *plan, read []value.Value) value.Value {
	switch p.action {
	case "list":
		if read == nil {
			read = []value.Value{}
		}
		return value.Array(read)
	case "first":
		if len(read) == 0 {
			return value.Null()
		}
		return read[0]
	}
	if len(read) == 0 {
		return value.Fail(value.NotFoundCode, NoRow+p.tables[0].name+" khớp điều kiện: db.detail")
	}
	return read[0]
}

// row reads one row of cells as the outputs say; parent is the cell a many side's rows point to, rowid the row's
// when the renderer asked for it.
func (r *renderer) row(cells []any) (value.Value, any, int64, value.Value) {
	row := value.Object()
	at := 0
	next := func() any {
		cell := cells[at]
		at++
		return cell
	}
	for _, o := range r.plan.output {
		switch o.kind {
		case "column":
			row = row.Set(o.name, decode(o.column, next()))
		case "value":
			cell := next()
			if o.exact {
				row = row.Set(o.name, decode(o.column, cell))
			} else {
				row = row.Set(o.name, decodeKind(o.part.node.kind, cell))
			}
		case "outside":
			n := o.part.node
			item := n.literal
			if n.op == "hole" {
				var failure value.Value
				if item, failure = holeValue(n, r.lambda(o.part)); failure.IsInvalid() {
					return value.Value{}, nil, 0, failure
				}
			}
			row = row.Set(o.name, item)
		case "one":
			present := true
			if o.left {
				present = decodeKind("boolean", next()).Boolean()
			}
			child := value.Object()
			for _, column := range o.columns {
				child = child.Set(column.name, decode(column, next()))
			}
			if !present {
				child = value.Null()
			}
			row = row.Set(o.name, child)
		case "many":
			row = row.Set(o.name, value.Array([]value.Value{}))
		}
	}
	var parent any
	if r.plan.many != nil {
		parent = next()
	}
	var rowid int64
	if r.rowid {
		rowid, _ = next().(int64)
	}
	return row, parent, rowid, value.Value{}
}

// many reads the rows of a select's [orders] whose foreign key points to a row read, in the order of their key, and
// gives each row its own: a row with none has [].
func many(source querier, m *manyPart, read []value.Value, parents []any) error {
	keys, err := json.Marshal(parents)
	if err != nil {
		return err
	}
	var names []string
	for _, column := range m.columns {
		names = append(names, qualified(m.table.Name, column.name))
	}
	order := ""
	if key := keyOf(m.table); key != "" {
		order = " ORDER BY " + qualified(m.table.Name, key) + " ASC"
	}
	text := "SELECT " + strings.Join(names, ", ") + ", " + qualified(m.table.Name, m.child) + " FROM " + quote(m.table.Name) +
		" WHERE " + qualified(m.table.Name, m.child) + " IN (SELECT value FROM json_each(?))" + order
	rows, err := source.Query(text, string(keys))
	if err != nil {
		return err
	}
	defer rows.Close()
	children := map[string][]value.Value{}
	for rows.Next() {
		cells := make([]any, len(m.columns)+1)
		pointers := make([]any, len(cells))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		child := value.Object()
		for i, column := range m.columns {
			child = child.Set(column.name, decode(column, cells[i]))
		}
		key := fmt.Sprint(cells[len(m.columns)])
		children[key] = append(children[key], child)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i, row := range read {
		mine := children[fmt.Sprint(parents[i])]
		if mine == nil {
			mine = []value.Value{}
		}
		read[i] = row.Set(m.name, value.Array(mine))
	}
	return nil
}

func blockingMessage(report Report) string {
	if report.Absent {
		return "database " + report.Database + " chưa có: chạy kitwork migrate (database là gốc, câu lệnh không tạo bảng)"
	}
	return "database " + report.Database + " chưa khớp struct (database là gốc): " + strings.Join(append(append([]string(nil), report.Lacking...), report.Differ...), "; ") + "; chạy kitwork migrate, hay sửa struct theo database"
}

// holeValue is the value a lambda takes from outside: its name off the lambda's closure, then each member.
func holeValue(n *node, lambda *value.Lambda) (value.Value, value.Value) {
	if lambda == nil {
		return value.Value{}, value.Fail(value.InvalidArgumentCode, n.at+": "+n.hole[0]+" không đọc được từ ngoài lambda")
	}
	item, ok := runtime.FreeValue(lambda, n.hole[0])
	if !ok {
		return value.Value{}, value.Fail(value.InvalidArgumentCode, n.at+": "+n.hole[0]+" không đọc được từ ngoài lambda")
	}
	for _, member := range n.hole[1:] {
		if item.IsNullish() {
			return value.Undefined(), value.Value{}
		}
		item = item.Get(member)
		if item.IsInvalid() {
			return value.Value{}, item
		}
	}
	return item, value.Value{}
}

// bind is a value as the column's file keeps it: a decimal its integer times 10^scale, a boolean 1 or 0, a
// timestamp its ISO text, a UUID lower case. A value of another kind fails, naming the column, never the value.
func bind(column *declared, item value.Value, at string) (any, value.Value) {
	wrong := func(want string) (any, value.Value) {
		return nil, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("cột %s là %s: giá trị ở %s không phải %s", column.name, column.spelling, at, want))
	}
	switch column.kind {
	case "integer":
		n := item.Number()
		if !item.IsNumber() || n != math.Trunc(n) || math.Abs(n) > 1<<53-1 {
			return wrong("số nguyên")
		}
		return int64(n), value.Value{}
	case "float":
		if !item.IsNumber() {
			return wrong("số")
		}
		return item.Number(), value.Value{}
	case "decimal":
		if !item.IsNumber() {
			return wrong("số")
		}
		return int64(math.Round(item.Number() * math.Pow10(column.scale))), value.Value{}
	case "bool":
		if !item.IsBoolean() {
			return wrong("true hay false")
		}
		if item.Boolean() {
			return int64(1), value.Value{}
		}
		return int64(0), value.Value{}
	case "datetime":
		switch {
		case item.IsNumber():
			return time.UnixMilli(int64(item.Number())).UTC().Format(timeLayout), value.Value{}
		case item.IsTime():
			return item.Time().UTC().Format(timeLayout), value.Value{}
		case item.IsString():
			return item.Text(), value.Value{}
		}
		return wrong("thời điểm (số mili giây)")
	case "uuid":
		if !item.IsString() {
			return wrong("chữ")
		}
		return strings.ToLower(item.Text()), value.Value{}
	case "json":
		return wrong("giá trị so được: cột json() không dùng trong điều kiện")
	}
	if !item.IsString() {
		return wrong("chữ")
	}
	return item.Text(), value.Value{}
}

// decode is a cell as the struct reads it.
func decode(column declared, cell any) value.Value {
	if cell == nil {
		return value.Null()
	}
	switch column.kind {
	case "integer":
		if n, ok := cell.(int64); ok {
			return value.Number(float64(n))
		}
	case "float":
		switch n := cell.(type) {
		case float64:
			return value.Number(n)
		case int64:
			return value.Number(float64(n))
		}
	case "decimal":
		switch n := cell.(type) {
		case int64:
			number, _ := strconv.ParseFloat(strconv.FormatInt(n, 10)+"e-"+strconv.Itoa(column.scale), 64)
			return value.Number(number)
		case float64:
			return value.Number(n / math.Pow10(column.scale))
		}
	case "bool":
		if n, ok := cell.(int64); ok {
			return value.Boolean(n != 0)
		}
	case "datetime":
		return decodeKind("time", cell)
	case "json":
		return decodeKind("json", cell)
	}
	return decodeKind("", cell)
}

// decodeKind is a cell of a computed value as JavaScript reads its kind.
func decodeKind(kind string, cell any) value.Value {
	if bytes, ok := cell.([]byte); ok {
		cell = string(bytes)
	}
	switch c := cell.(type) {
	case nil:
		return value.Null()
	case int64:
		if kind == "boolean" {
			return value.Boolean(c != 0)
		}
		return value.Number(float64(c))
	case float64:
		if kind == "boolean" {
			return value.Boolean(c != 0)
		}
		return value.Number(c)
	case string:
		switch kind {
		case "time":
			if moment, err := time.Parse(time.RFC3339Nano, c); err == nil {
				return value.Number(float64(moment.UnixMilli()))
			}
		case "json":
			return value.ParseJSON(c)
		}
		return value.String(c)
	}
	return value.Null()
}

// database is the entry's file, opened at its first statement: KitDB's writer, the process's for this file.
func (c *Catalog) database(entry *Entry) (*kitdb.Database, value.Value) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if open := c.open[entry]; open != nil {
		return open, value.Value{}
	}
	database, release, err := Acquire(entry.Path)
	if errors.Is(err, kitdb.ErrLocked) {
		return nil, value.Fail(value.UnavailableCode, entry.File()+": một tiến trình khác đang ghi file này (kitwork migrate, hay một server khác)")
	}
	if err != nil {
		return nil, value.Fail(value.UnavailableCode, entry.File()+": "+err.Error())
	}
	c.open[entry] = database
	c.releases[entry] = release
	return database, value.Value{}
}

// Close closes every database file the app opened, the full-text files matches held first.
func (c *Catalog) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for path, release := range c.searches {
		release()
		delete(c.searches, path)
	}
	for entry := range c.open {
		if release := c.releases[entry]; release != nil {
			release() // the writer closes when the last of the process lets it go (writers.go)
		}
		delete(c.open, entry)
		delete(c.releases, entry)
	}
	return nil
}
