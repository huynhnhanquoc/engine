package database

import (
	"fmt"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/schema"
)

// before and after (docs/KITWORK_DATABASE.md mục 1, "Quyền, trước và sau khi ghi"):
//
//	export const db = connection()
//	  .before("insert", (users) => ({ ...users, email: users.email.trim().toLowerCase() }))
//	  .after("insert", (orders) => db.insert((audits) => ({ what: "insert", orderId: orders.id })))
//	  .tables({ users, orders, audits });
//
// before takes the row about to be written and gives the row to write, or null for none (the statement writes 0 rows,
// no error); it runs as the program's own code on the real row, every column. after takes the row written (the row
// deleted, for a delete) and runs in the same transaction as the write: a failure out of it rolls the write back, and
// a write it makes through the database's handle joins that transaction. The event is insert, update or delete; an
// upsert is an insert or an update by what it finds; the rows a .cascade() deletes have their table's after("delete").
// One before and one after per event and table, the table named by the lambda's parameter.
//
// A write with a hook runs as a transaction does (transaction.go): KitDB's writer holds its request while the hooks
// run on the request's own VM, each row through them in turn. The hooks are the handle's, made by the same program as
// the handle (the site's run of the schema file), and handed to the calling VM with it at every call (value.Keeper).

var hookEvents = map[string]bool{"insert": true, "update": true, "delete": true}

// hook is one before or after of a declaration.
type hook struct {
	when  string // before, after
	event string // insert, update, delete
	table string
	fn    value.Value
	at    string
}

func (c *Connection) Before(arguments ...value.Value) value.Value { return c.hook("before", arguments) }
func (c *Connection) After(arguments ...value.Value) value.Value  { return c.hook("after", arguments) }

func (c *Connection) hook(when string, arguments []value.Value) value.Value {
	event, fn := argument(arguments, 0), argument(arguments, 1)
	if len(arguments) != 2 || !event.IsString() || !hookEvents[event.Text()] || !fn.IsFunction() {
		return value.Fail(value.InvalidArgumentCode, "."+when+" nhận sự kiện và hàm: ."+when+"(\"insert\", (orders) => …); sự kiện là insert, update, delete")
	}
	lambda := fn.Lambda()
	if lambda == nil || len(lambda.Params) != 1 {
		return value.Fail(value.InvalidArgumentCode, "hàm của ."+when+" nhận một tham số là bảng: ."+when+"(\""+event.Text()+"\", (orders) => …)")
	}
	next := *c
	next.hooks = append(append([]hook(nil), c.hooks...), hook{when: when, event: event.Text(), table: lambda.Params[0], fn: fn, at: c.database.Where()})
	return value.Struct(&next)
}

// attach gives the handle a declaration made its hooks, each on a table of it, one per event and table.
func attach(handle value.Value, hooks []hook) value.Value {
	if len(hooks) == 0 || handle.IsInvalid() {
		return handle
	}
	h, ok := handle.Host().(*Handle)
	if !ok {
		return handle
	}
	seen := map[string]bool{}
	for _, k := range hooks {
		if h.entry.table(k.table) == nil {
			return value.Fail(value.InvalidArgumentCode, k.at+": ."+k.when+"(\""+k.event+"\", ("+k.table+") => …): "+k.table+" không phải bảng của tables({ … }); có: "+h.entry.tableNames())
		}
		key := k.when + " " + k.event + " " + k.table
		if seen[key] {
			return value.Fail(value.InvalidArgumentCode, k.at+": "+k.table+" có hai ."+k.when+"(\""+k.event+"\"): một bảng một "+k.when+" cho mỗi sự kiện")
		}
		seen[key] = true
	}
	next := *h
	next.hooks = hooks
	return value.Struct(&next)
}

// HandOver is the handle one call of its method runs on (value.Keeper): its hooks run on the calling VM.
func (h *Handle) HandOver(hand func(value.Value) value.Value) value.Value {
	return value.Struct(h.handOver(hand))
}

func (h *Handle) handOver(hand func(value.Value) value.Value) *Handle {
	if len(h.hooks) == 0 {
		return h
	}
	next := *h
	next.hooks = make([]hook, len(h.hooks))
	for i, k := range h.hooks {
		k.fn = hand(k.fn)
		next.hooks[i] = k
	}
	return &next
}

// HandOver is the statement one call of its method runs on: its handle's hooks run on the calling VM.
func (q *Query) HandOver(hand func(value.Value) value.Value) value.Value {
	if len(q.handle.hooks) == 0 {
		return value.Struct(q)
	}
	next := *q
	next.handle = q.handle.handOver(hand)
	return value.Struct(&next)
}

// hookOf is the handle's hook, undefined when it has none.
func (h *Handle) hookOf(when, event, table string) value.Value {
	for _, k := range h.hooks {
		if k.when == when && k.event == event && k.table == table {
			return k.fn
		}
	}
	return value.Undefined()
}

// hooked reports a write that takes hooks: some hook of its table and events, or of a table a delete cascades to.
func (h *Handle) hooked(p *plan) bool {
	if len(h.hooks) == 0 {
		return false
	}
	events := map[string][]string{"insert": {"insert"}, "update": {"update"}, "upsert": {"insert", "update"}, "delete": {"delete"}}[p.action]
	for _, k := range h.hooks {
		for _, event := range events {
			if k.event == event && (k.table == p.tables[0].name || event == "delete") {
				return true
			}
		}
	}
	return false
}

// call runs a hook on a row, counting the hooks running so a write through db joins the transaction.
func (w *writer) call(fn value.Value, row value.Value) value.Value {
	w.state.hooking++
	defer func() { w.state.hooking-- }()
	return fn.Call([]value.Value{row})
}

// before runs the table's before of the event on row: the row to write, or null for none.
func (w *writer) before(event, table string, row value.Value) (value.Value, value.Value) {
	fn := w.q.handle.hookOf("before", event, table)
	if fn.IsUndefined() {
		return row, value.Value{}
	}
	out := w.call(fn, row)
	switch {
	case out.IsInvalid():
		return value.Value{}, out
	case out.IsNull():
		return out, value.Value{}
	case !out.IsObject():
		return value.Value{}, value.Fail(value.TypeCode, "before(\""+event+"\") của "+table+" trả dòng sẽ ghi, hay null để không ghi")
	}
	return out, value.Value{}
}

// after runs the table's after of the event on the row written.
func (w *writer) after(event, table string, row value.Value) value.Value {
	fn := w.q.handle.hookOf("after", event, table)
	if fn.IsUndefined() {
		return value.Value{}
	}
	if out := w.call(fn, row); out.IsInvalid() {
		return out
	}
	return value.Value{}
}

// fullRow is a row of table by rowid as the hooks take it: every column, a .secret() one too, with the raw cells.
func (w *writer) fullRow(table *Table, rowid int64) (value.Value, map[string]any, value.Value) {
	var names []string
	var columns []declared
	for _, f := range table.Fields {
		columns = append(columns, declare(f))
		names = append(names, quote(f.Name))
	}
	rows, err := w.tx.Query("SELECT "+strings.Join(names, ", ")+" FROM "+quote(table.Name)+" WHERE rowid = ?", rowid)
	if err != nil {
		return value.Value{}, nil, w.failed(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return value.Null(), nil, value.Value{}
	}
	cells := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range cells {
		pointers[i] = &cells[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return value.Value{}, nil, w.failed(err)
	}
	row := value.Object()
	raw := map[string]any{}
	for i, column := range columns {
		row = row.Set(column.name, decode(column, cells[i]))
		raw[column.name] = cells[i]
	}
	return row, raw, value.Value{}
}

// dataValue is the value one column of the data gives: a value from outside, or one computed on the row (rowid), or
// in the branch that inserts (inserting).
func (w *writer) dataValue(entry dataEntry, rowid int64, inserting bool) (value.Value, value.Value) {
	lambda := w.q.steps[w.p.data.step].lambda
	n := entry.value
	if !mentions(n) {
		if n.op == "hole" {
			return holeValue(n, lambda)
		}
		return n.literal, value.Value{}
	}
	r := &renderer{plan: w.p, steps: w.q.steps, inserting: inserting}
	text, failure := computed(r, entry, lambda)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	query, args := "SELECT "+text, r.args
	if !inserting {
		query += " FROM " + quote(w.p.tables[0].name) + " WHERE rowid = ?"
		args = append(args, rowid)
	}
	rows, err := w.tx.Query(query, args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	defer rows.Close()
	var cell any
	if rows.Next() {
		if err := rows.Scan(&cell); err != nil {
			return value.Value{}, w.failed(err)
		}
	}
	return decode(*entry.column, cell), value.Value{}
}

// fieldOf is a column of table.
func fieldOf(table *Table, name string) (dataEntry, bool) {
	for _, f := range table.Fields {
		if f.Name == name {
			d := declare(f)
			return dataEntry{field: f, column: &d, at: table.Name + "." + name}, true
		}
	}
	return dataEntry{}, false
}

// insertRow writes a row a before gave, as an insert writes its data: each value through its column's transform and
// rules, what the engine fills filled. It gives the rowid, 0 for none.
func (w *writer) insertRow(table *Table, row value.Value) (int64, value.Value) {
	var names, marks []string
	var args []any
	written := map[string]bool{}
	for _, name := range row.Keys() {
		entry, ok := fieldOf(table, name)
		switch {
		case !ok:
			return 0, value.Fail(value.TypeCode, "before(\"insert\") của "+table.Name+" trả cột "+name+" không có; có: "+columnNames(table))
		case entry.field.Created || entry.field.Updated:
			return 0, value.Fail(value.TypeCode, "before(\"insert\") của "+table.Name+" ghi "+name+", cột engine điền")
		}
		bound, omit, failure := w.bindWrite(entry, row.Get(name))
		if failure.IsInvalid() {
			return 0, failure
		}
		if omit {
			continue
		}
		names, marks, args = append(names, quote(name)), append(marks, "?"), append(args, bound)
		written[name] = true
	}
	for _, f := range table.Fields {
		if !written[f.Name] && required(f) {
			return 0, invalidColumn(f.Name, schema.RuleRequired, "cần giá trị")
		}
	}
	r := &renderer{plan: w.p, steps: w.q.steps}
	w.fill(table, written, &names, &marks, r)
	text := "INSERT INTO " + quote(table.Name) + " DEFAULT VALUES"
	if len(names) > 0 {
		text = "INSERT INTO " + quote(table.Name) + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ")"
	}
	result, err := w.tx.Exec(text, append(args, r.args...)...)
	if err != nil {
		return 0, w.failed(err)
	}
	rowid, err := result.LastInsertId()
	if err != nil {
		return 0, w.failed(err)
	}
	return rowid, value.Value{}
}

// updateRow writes onto the row at rowid what next changes of it (raw, its cells) and every column the data names,
// .updated() with them: the key and a .readonly() column changed are refused.
func (w *writer) updateRow(table *Table, rowid int64, raw map[string]any, next value.Value, named map[string]bool) value.Value {
	var sets []string
	var args []any
	for _, name := range next.Keys() {
		entry, ok := fieldOf(table, name)
		if !ok {
			return value.Fail(value.TypeCode, "before(\"update\") của "+table.Name+" trả cột "+name+" không có; có: "+columnNames(table))
		}
		if entry.field.Created || entry.field.Updated {
			continue // the engine's
		}
		bound, omit, failure := w.bindWrite(entry, next.Get(name))
		if failure.IsInvalid() {
			return failure
		}
		if omit || !named[name] && fmt.Sprint(bound) == fmt.Sprint(raw[name]) {
			continue
		}
		if entry.field.Key || entry.field.Readonly {
			if fmt.Sprint(bound) != fmt.Sprint(raw[name]) {
				return value.Fail(value.TypeCode, table.Name+"."+name+" là khoá hay .readonly(): update không đổi nó")
			}
			continue
		}
		sets, args = append(sets, quote(name)+" = ?"), append(args, bound)
	}
	for _, f := range table.Fields {
		if f.Updated {
			sets, args = append(sets, quote(f.Name)+" = ?"), append(args, w.now)
		}
	}
	if len(sets) == 0 {
		return value.Value{}
	}
	if _, err := w.tx.Exec("UPDATE "+quote(table.Name)+" SET "+strings.Join(sets, ", ")+" WHERE rowid = ?", append(args, rowid)...); err != nil {
		return w.failed(err)
	}
	return value.Value{}
}

// insertHooked is an insert with hooks: the data as a row, through before, written, then after.
func (w *writer) insertHooked() (value.Value, value.Value) {
	table := w.p.tables[0]
	row := value.Object()
	for _, entry := range w.q.steps[w.p.data.step].compiled.data {
		item, failure := w.dataValue(entry, 0, true)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		if !item.IsUndefined() {
			row = row.Set(entry.field.Name, item)
		}
	}
	return w.insertThrough(table, row)
}

// insertThrough writes row through the table's before and after of insert, and gives the row as a read gives it.
func (w *writer) insertThrough(table *joined, row value.Value) (value.Value, value.Value) {
	row, failure := w.before("insert", table.name, row)
	if failure.IsInvalid() || row.IsNull() {
		return row, failure
	}
	rowid, failure := w.insertRow(table.table, row)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	written, _, failure := w.fullRow(table.table, rowid)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	if failure := w.after("insert", table.name, written); failure.IsInvalid() {
		return value.Value{}, failure
	}
	return w.row(table, "rowid = ?", rowid)
}

// updateThrough changes the row at rowid by the data (computed on that row) through the table's before and after of
// update: whether it wrote.
func (w *writer) updateThrough(table *joined, rowid int64) (bool, value.Value) {
	current, raw, failure := w.fullRow(table.table, rowid)
	if failure.IsInvalid() || current.IsNull() {
		return false, failure
	}
	next := current
	named := map[string]bool{}
	for _, entry := range w.q.steps[w.p.data.step].compiled.data {
		item, failure := w.dataValue(entry, rowid, false)
		if failure.IsInvalid() {
			return false, failure
		}
		if !item.IsUndefined() {
			next = next.Set(entry.field.Name, item)
			named[entry.field.Name] = true
		}
	}
	next, failure = w.before("update", table.name, next)
	if failure.IsInvalid() || next.IsNull() {
		return false, failure
	}
	if failure := w.updateRow(table.table, rowid, raw, next, named); failure.IsInvalid() {
		return false, failure
	}
	written, _, failure := w.fullRow(table.table, rowid)
	if failure.IsInvalid() {
		return false, failure
	}
	return true, w.after("update", table.name, written)
}

// updateHooked is an update with hooks: each row the conditions keep, in turn.
func (w *writer) updateHooked() (value.Value, value.Value) {
	table := w.p.tables[0]
	r := &renderer{plan: w.p, steps: w.q.steps}
	where, failure := w.matched(r)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	rowids, err := w.rowids("SELECT "+qualified(table.name, "rowid")+" FROM "+quote(table.name)+where, r.args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	changed := 0
	for _, rowid := range rowids {
		wrote, failure := w.updateThrough(table, rowid)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		if wrote {
			changed++
		}
	}
	return value.Number(float64(changed)), value.Value{}
}

// upsertHooked is an upsert with hooks: an update of the row the target finds, or an insert.
func (w *writer) upsertHooked() (value.Value, value.Value) {
	table := w.p.tables[0]
	data := w.q.steps[w.p.data.step].compiled
	var where []string
	var args []any
	for _, name := range data.target {
		for _, entry := range data.data {
			if entry.field.Name != name {
				continue
			}
			item, failure := w.dataValue(entry, 0, true)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			bound, _, failure := w.bindWrite(entry, item)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			where, args = append(where, quote(name)+" = ?"), append(args, bound)
		}
	}
	found, err := w.rowids("SELECT rowid FROM "+quote(table.name)+" WHERE "+strings.Join(where, " AND "), args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	if len(found) == 0 {
		return w.insertHooked()
	}
	if _, failure := w.updateThrough(table, found[0]); failure.IsInvalid() {
		return value.Value{}, failure
	}
	return w.row(table, "rowid = ?", found[0])
}

// deleteHooked is a delete with hooks: each row the conditions keep through the table's before (null keeps it), the
// rows a .cascade() takes with them, deleted, then each row deleted through its table's after.
func (w *writer) deleteHooked() (value.Value, value.Value) {
	table := w.p.tables[0]
	r := &renderer{plan: w.p, steps: w.q.steps}
	where, failure := w.matched(r)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	matched, err := w.rowids("SELECT "+qualified(table.name, "rowid")+" FROM "+quote(table.name)+where, r.args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	var kept []int64
	for _, rowid := range matched {
		row, _, failure := w.fullRow(table.table, rowid)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		row, failure = w.before("delete", table.name, row)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		if !row.IsNull() {
			kept = append(kept, rowid)
		}
	}
	var doomed []doomedRows
	if failure := w.collect(table.table, kept, map[string]bool{}, &doomed); failure.IsInvalid() {
		return value.Value{}, failure
	}
	type gone struct {
		table string
		row   value.Value
	}
	var deleted []gone
	for _, d := range doomed {
		if w.q.handle.hookOf("after", "delete", d.table.Name).IsUndefined() {
			continue
		}
		for _, rowid := range d.rowids {
			row, _, failure := w.fullRow(d.table, rowid)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			deleted = append(deleted, gone{table: d.table.Name, row: row})
		}
	}
	if failure := w.deleteDoomed(doomed); failure.IsInvalid() {
		return value.Value{}, failure
	}
	for _, each := range deleted {
		if failure := w.after("delete", each.table, each.row); failure.IsInvalid() {
			return value.Value{}, failure
		}
	}
	return value.Number(float64(len(kept))), value.Value{}
}
