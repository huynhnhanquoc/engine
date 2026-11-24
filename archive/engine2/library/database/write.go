package database

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/internal/id"
	"github.com/kitwork/engine2/library/schema"
)

// The writes (docs/KITWORK_DATABASE.md mục 2; slice 4 of the root's docs/KITWORK_SCHEMA.md mục 8): insert, update,
// delete, upsert, each one statement through KitDB's writer, so each is one revision of kitwork_history and the full
// text folds it in. A value from outside is held to its column's rules before it is written (schema.TableField.Check,
// the rules .parse() holds), null only into a .nullable() column, undefined a column not written (an insert's default,
// an update's value kept). The engine fills a key it makes (id() the rowid, id("uuid") a UUIDv7, id("kitwork") a
// KitID) unless an insert brings one, .created() at an insert, .updated() at an insert and every update. A row
// already there by a unique column, a foreign key pointing to no row, a row deleted that other rows point to are
// CONFLICT, naming the columns. insert and upsert give the row written, update and delete the rows they changed.

// timeLayout is how a timestamp() is kept: UTC to the millisecond, every one as long as the others, so the text
// sorts as the time does.
const timeLayout = "2006-01-02T15:04:05.000Z"

// errFailed carries a failure out of KitDB's writer, which rolls the request back.
var errFailed = errors.New("kitwork: the write failed")

// insert, upsert and update are given the budget of the run that writes (value.Budget): a JSON column's text is
// written within it.
func (h *Handle) Insert(budget value.Budget, arguments ...value.Value) value.Value {
	return h.query().terminalWithin("insert", arguments, budget)
}
func (h *Handle) Upsert(budget value.Budget, arguments ...value.Value) value.Value {
	return h.query().terminalWithin("upsert", arguments, budget)
}
func (h *Handle) Delete(arguments ...value.Value) value.Value {
	return h.query().terminal("delete", arguments)
}
func (h *Handle) Update(budget value.Budget, arguments ...value.Value) value.Value {
	return h.query().terminalWithin("update", arguments, budget)
}

func (q *Query) Update(budget value.Budget, arguments ...value.Value) value.Value {
	return q.terminalWithin("update", arguments, budget)
}
func (q *Query) Delete(arguments ...value.Value) value.Value { return q.terminal("delete", arguments) }
func (q *Query) Insert(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "insert đứng một mình: db.insert((orders) => ({ … }))")
}
func (q *Query) Upsert(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "upsert đứng một mình: db.upsert((orders) => ({ … }))")
}

// write runs a write in one request of KitDB's writer, or in the handle's transaction.
func (q *Query) write(p *plan, database *kitdb.Database, budget value.Budget) value.Value {
	hooked := q.handle.hooked(p)
	statement := func(state *transaction) (value.Value, value.Value) {
		w := &writer{q: q, p: p, tx: state.tx, state: state, now: time.Now().UTC().Format(timeLayout), budget: budget}
		switch {
		case p.action == "insert" && hooked:
			return w.insertHooked()
		case p.action == "insert":
			return w.insert()
		case p.action == "upsert" && hooked:
			return w.upsertHooked()
		case p.action == "upsert":
			return w.upsert()
		case p.action == "update" && hooked:
			return w.updateHooked()
		case p.action == "update":
			return w.update()
		case hooked:
			return w.deleteHooked()
		}
		return w.delete()
	}
	settle := func(result, failure value.Value) value.Value {
		if failure.IsInvalid() {
			return failure
		}
		return result
	}
	if tx := q.handle.tx; tx != nil {
		return settle(statement(tx))
	}
	if q.handle.catalog.writesOutside() {
		owner := goroutineID()
		if open := q.handle.catalog.transactionOf(owner); open != nil && open.hooking > 0 && open.entry == q.handle.entry {
			// a write a hook makes through the database's handle joins the write it runs in
			return settle(statement(open))
		}
		if refused := q.handle.catalog.outsideTransaction(owner, q.handle.entry, p.action); refused.IsInvalid() {
			return refused
		}
	}
	if hooked {
		// the hooks run on this goroutine, in a request of the writer held for them
		return q.handle.within(func(state *transaction) value.Value { return settle(statement(state)) })
	}
	var result, failure value.Value
	ctx, cancel := value.WaitContext(q.handle.waits, time.Time{})
	defer cancel()
	_, err := database.Write(ctx, "", func(tx *kitdb.Transaction) error {
		result, failure = statement(&transaction{tx: tx, entry: q.handle.entry})
		if failure.IsInvalid() {
			return errFailed
		}
		return nil
	})
	switch {
	case failure.IsInvalid():
		return failure
	case err != nil:
		return writeFailure(p.tables[0].name, p.action, err, nil)
	}
	return result
}

// writer is one write in progress, inside the writer's transaction.
type writer struct {
	q     *Query
	p     *plan
	state *transaction
	tx    *kitdb.Transaction
	now   string
	// foreign are the foreign keys the write gives a value, so a row they point to that is not there names its
	// column when it is the one (SQLite's message names none)
	foreign []string
	// budget is the run's that writes (value.Budget): a JSON column is written within it
	budget value.Budget
}

// failed is what a statement's error becomes, the transaction rolled back.
func (w *writer) failed(err error) value.Value {
	return writeFailure(w.p.tables[0].name, w.p.action, err, w.foreign)
}

// writeFailure names what the database refused: a unique column, a foreign key, a column that needs a value; with
// the columns as error.fields, each { path, rule, message }, so a form shows it under the input (docs/schema.md mục
// 6). foreign are the foreign keys the write gave a value: a row pointed to that is not there is the column's when
// there is one.
func writeFailure(table, action string, err error, foreign []string) value.Value {
	text := err.Error()
	columns := func(prefix string) []string {
		_, after, _ := strings.Cut(text, prefix)
		var names []string
		for _, name := range strings.Split(after, ",") {
			name, _, _ = strings.Cut(name, " (") // SQLite's extended code: "users.email (2067)"
			name = strings.TrimSpace(name)
			if _, column, ok := strings.Cut(name, "."); ok {
				name = column
			}
			if name != "" {
				names = append(names, strings.Trim(name, `"`))
			}
		}
		return names
	}
	fields := func(names []string, rule, message string) value.Value {
		entries := make([]value.Value, len(names))
		for i, name := range names {
			entries[i] = schema.FieldProblem([]value.Value{value.String(name)}, rule, message)
		}
		return value.Array(entries)
	}
	switch {
	case strings.Contains(text, "UNIQUE constraint failed:"):
		names := columns("UNIQUE constraint failed:")
		return value.Fail(value.ConflictCode, strings.Join(names, ", ")+" đã có: "+table+" không có hai dòng trùng ở đó").
			WithFields(fields(names, schema.RuleUnique, "đã có"))
	case strings.Contains(text, "FOREIGN KEY constraint failed") && action == "delete":
		return value.Fail(value.ConflictCode, "khoá ngoại tới "+table+": còn dòng khác trỏ tới dòng bị xoá")
	case strings.Contains(text, "FOREIGN KEY constraint failed") && len(foreign) == 1:
		return value.Fail(value.ConflictCode, "khoá ngoại "+foreign[0]+" của "+table+": dòng được trỏ tới không có").
			WithFields(fields(foreign, schema.RuleForeign, "dòng được trỏ tới không có"))
	case strings.Contains(text, "FOREIGN KEY constraint failed"):
		return value.Fail(value.ConflictCode, "khoá ngoại của "+table+": dòng được trỏ tới không có")
	case strings.Contains(text, "NOT NULL constraint failed:"):
		names := columns("NOT NULL constraint failed:")
		return value.Fail(value.InvalidInputCode, "dữ liệu không hợp lệ: "+strings.Join(names, ", ")+": cần giá trị").
			WithFields(fields(names, schema.RuleRequired, "cần giá trị"))
	case strings.Contains(text, "cannot store REAL value in INTEGER column"):
		names := columns("INTEGER column")
		return value.Fail(value.InvalidInputCode, "dữ liệu không hợp lệ: "+strings.Join(names, ", ")+": cần số nguyên").
			WithFields(fields(names, schema.RuleType, "cần số nguyên"))
	case strings.Contains(text, "CHECK constraint failed"):
		return value.Fail(value.InvalidInputCode, "dữ liệu không hợp lệ: database từ chối giá trị của "+table)
	}
	return value.Fail(value.DatabaseCode, table+": "+text)
}

// invalidColumn is a value a column refused, by rule: an INVALID_INPUT failure naming the column, with it as
// error.fields, as .parse() names a field.
func invalidColumn(name, rule, problem string) value.Value {
	return value.Fail(value.InvalidInputCode, "dữ liệu không hợp lệ: "+name+": "+problem).
		WithFields(value.Array([]value.Value{schema.FieldProblem([]value.Value{value.String(name)}, rule, problem)}))
}

// bindWrite is a value from outside written into a column: held to the column's rules, as the file keeps it. omit is
// an undefined value: the column is not written.
func (w *writer) bindWrite(entry dataEntry, item value.Value) (bound any, omit bool, failure value.Value) {
	name := entry.field.Name
	switch {
	case item.IsUndefined():
		return nil, true, value.Value{}
	case item.IsNull():
		if !entry.field.Nullable {
			return nil, false, invalidColumn(name, schema.RuleRequired, "cần giá trị (cột không .nullable())")
		}
		return nil, false, value.Value{}
	}
	if entry.field.ForeignTarget != nil && !contains(w.foreign, name) {
		w.foreign = append(w.foreign, name)
	}
	// the table's transform first, then the rules: what the column's CHECK sees (docs/KITWORK_SCHEMA.md mục 5)
	item = w.p.entry.transformed(w.p.tables[0].name, name, item)
	kept, rule, problem := entry.field.Check(item)
	if problem != "" {
		return nil, false, invalidColumn(name, rule, problem)
	}
	if entry.column.kind == "json" {
		text := value.StringifyWithin(kept, "", w.budget) // within the budget of the run that writes
		if text.IsInvalid() {
			return nil, false, text
		}
		return text.Text(), false, value.Value{}
	}
	bound, failure = bind(entry.column, kept, entry.at)
	return bound, false, failure
}

// values are the columns an insert writes and their SQL: the data's, then what the engine fills.
func (w *writer) values(r *renderer, data *compiled) ([]string, []string, value.Value) {
	var names, marks []string
	written := map[string]bool{}
	lambda := w.q.steps[w.p.data.step].lambda
	for _, entry := range data.data {
		n := entry.value
		if mentions(n) {
			text, failure := computed(r, entry, lambda)
			if failure.IsInvalid() {
				return nil, nil, failure
			}
			names, marks = append(names, quote(entry.field.Name)), append(marks, text)
			written[entry.field.Name] = true
			continue
		}
		item := n.literal
		if n.op == "hole" {
			var failure value.Value
			if item, failure = holeValue(n, lambda); failure.IsInvalid() {
				return nil, nil, failure
			}
		}
		bound, omit, failure := w.bindWrite(entry, item)
		if failure.IsInvalid() {
			return nil, nil, failure
		}
		if omit {
			if required(entry.field) {
				return nil, nil, invalidColumn(entry.field.Name, schema.RuleRequired, "cần giá trị (undefined là cột không ghi)")
			}
			continue
		}
		r.args = append(r.args, bound)
		names, marks = append(names, quote(entry.field.Name)), append(marks, "?")
		written[entry.field.Name] = true
	}
	w.fill(w.p.tables[0].table, written, &names, &marks, r)
	return names, marks, value.Value{}
}

// fill adds to an insert what the engine fills and it does not write: a key id("uuid") or id("kitwork") makes, the
// times of .created() and .updated().
func (w *writer) fill(table *Table, written map[string]bool, names, marks *[]string, r *renderer) {
	for _, f := range table.Fields {
		if written[f.Name] {
			continue
		}
		var filled any
		switch {
		case f.Kind == "id" && f.Generate == "uuid":
			filled = uuidV7()
		case f.Kind == "id" && f.Generate == "kitwork":
			filled = id.Entity()
		case f.Created || f.Updated:
			filled = w.now
		default:
			continue
		}
		r.args = append(r.args, filled)
		*names, *marks = append(*names, quote(f.Name)), append(*marks, "?")
	}
}

// insert writes one row and gives it back as a read gives it.
func (w *writer) insert() (value.Value, value.Value) {
	table := w.p.tables[0]
	r := &renderer{plan: w.p, steps: w.q.steps}
	names, marks, failure := w.values(r, w.q.steps[w.p.data.step].compiled)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	text := "INSERT INTO " + quote(table.name) + " DEFAULT VALUES"
	if len(names) > 0 {
		text = "INSERT INTO " + quote(table.name) + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ")"
	}
	result, err := w.tx.Exec(text, r.args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	rowid, err := result.LastInsertId()
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	return w.row(table, "rowid = ?", rowid)
}

// row reads one row of table in the transaction, as a read gives it: every column but a .secret() one.
func (w *writer) row(table *joined, where string, arguments ...any) (value.Value, value.Value) {
	var columns []declared
	var names []string
	for _, f := range table.table.Fields {
		if !f.Secret {
			columns = append(columns, declare(f))
			names = append(names, quote(f.Name))
		}
	}
	rows, err := w.tx.Query("SELECT "+strings.Join(names, ", ")+" FROM "+quote(table.name)+" WHERE "+where, arguments...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return value.Null(), value.Value{}
	}
	cells := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range cells {
		pointers[i] = &cells[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return value.Value{}, w.failed(err)
	}
	row := value.Object()
	for i, column := range columns {
		row = row.Set(column.name, decode(column, cells[i]))
	}
	return row, value.Value{}
}

// upsert inserts the row, or changes the one already there by the target: the columns the data writes, a number
// column read as it is there (null in the branch that inserts), the default only when it inserts.
func (w *writer) upsert() (value.Value, value.Value) {
	table := w.p.tables[0]
	data := w.q.steps[w.p.data.step].compiled
	lambda := w.q.steps[w.p.data.step].lambda
	r := &renderer{plan: w.p, steps: w.q.steps, inserting: true}
	names, marks, failure := w.values(r, data)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	r.inserting = false
	var sets []string
	var targets []any
	for _, entry := range data.data {
		n := entry.value
		if contains(data.target, entry.field.Name) {
			item := n.literal
			if n.op == "hole" {
				if item, failure = holeValue(n, lambda); failure.IsInvalid() {
					return value.Value{}, failure
				}
			}
			bound, _, failure := w.bindWrite(entry, item)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			targets = append(targets, bound)
			continue
		}
		if mentions(n) {
			text, failure := computed(r, entry, lambda)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			sets = append(sets, quote(entry.field.Name)+" = "+text)
			continue
		}
		item := n.literal
		if n.op == "hole" {
			if item, failure = holeValue(n, lambda); failure.IsInvalid() {
				return value.Value{}, failure
			}
		}
		bound, omit, failure := w.bindWrite(entry, item)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		if omit {
			continue
		}
		r.args = append(r.args, bound)
		sets = append(sets, quote(entry.field.Name)+" = ?")
	}
	for _, f := range table.table.Fields {
		if f.Updated {
			r.args = append(r.args, w.now)
			sets = append(sets, quote(f.Name)+" = ?")
		}
	}
	var conflict []string
	for _, name := range data.target {
		conflict = append(conflict, quote(name))
	}
	action := " DO NOTHING"
	if len(sets) > 0 {
		action = " DO UPDATE SET " + strings.Join(sets, ", ")
	}
	text := "INSERT INTO " + quote(table.name) + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ") ON CONFLICT (" + strings.Join(conflict, ", ") + ")" + action
	if _, err := w.tx.Exec(text, r.args...); err != nil {
		return value.Value{}, w.failed(err)
	}
	var where []string
	for _, name := range data.target {
		where = append(where, quote(name)+" = ?")
	}
	return w.row(table, strings.Join(where, " AND "), targets...)
}

// computed is a value made of columns, written into a column: a number as JavaScript computes it, back into a
// decimal's stored integer (rounded to its scale, so orders.amount + 1000 stays exact to the cent).
func computed(r *renderer, entry dataEntry, lambda *value.Lambda) (string, value.Value) {
	text, failure := r.value(entry.value, lambda, true, entry.value.kind)
	if failure.IsInvalid() {
		return "", failure
	}
	if entry.column.kind == "decimal" {
		return "CAST(round((" + text + ") * " + strconv.FormatFloat(math.Pow10(entry.column.scale), 'f', -1, 64) + ") AS INTEGER)", value.Value{}
	}
	return text, value.Value{}
}

// matched is the SQL of the rows of the main table a statement's conditions keep: the condition itself on one
// table, the rowids of the joined rows on several.
func (w *writer) matched(r *renderer) (string, value.Value) {
	tail, failure := r.filters()
	if failure.IsInvalid() {
		return "", failure
	}
	main := w.p.tables[0].name
	if len(w.p.tables) == 1 {
		return tail, value.Value{}
	}
	from, failure := r.from()
	if failure.IsInvalid() {
		return "", failure
	}
	return " WHERE " + qualified(main, "rowid") + " IN (SELECT " + qualified(main, "rowid") + from + tail + ")", value.Value{}
}

// update changes the columns the data writes on every row the conditions keep, .updated() with them, and gives
// how many rows it changed.
func (w *writer) update() (value.Value, value.Value) {
	table := w.p.tables[0]
	data := w.q.steps[w.p.data.step].compiled
	lambda := w.q.steps[w.p.data.step].lambda
	r := &renderer{plan: w.p, steps: w.q.steps}
	var sets []string
	for _, entry := range data.data {
		n := entry.value
		if mentions(n) {
			text, failure := computed(r, entry, lambda)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			sets = append(sets, quote(entry.field.Name)+" = "+text)
			continue
		}
		item := n.literal
		if n.op == "hole" {
			var failure value.Value
			if item, failure = holeValue(n, lambda); failure.IsInvalid() {
				return value.Value{}, failure
			}
		}
		bound, omit, failure := w.bindWrite(entry, item)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		if omit {
			continue // undefined: the column keeps its value
		}
		r.args = append(r.args, bound)
		sets = append(sets, quote(entry.field.Name)+" = ?")
	}
	if len(sets) == 0 {
		return value.Number(0), value.Value{}
	}
	for _, f := range table.table.Fields {
		if f.Updated {
			r.args = append(r.args, w.now)
			sets = append(sets, quote(f.Name)+" = ?")
		}
	}
	where, failure := w.matched(r)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	result, err := w.tx.Exec("UPDATE "+quote(table.name)+" SET "+strings.Join(sets, ", ")+where, r.args...)
	if err != nil {
		return value.Value{}, w.failed(err)
	}
	changed, _ := result.RowsAffected()
	return value.Number(float64(changed)), value.Value{}
}

// delete removes every row the conditions keep, and gives how many: a row other rows point to by a foreign key
// without .cascade() is CONFLICT naming them; with .cascade(), those rows go first, row by row, as theirs do.
func (w *writer) delete() (value.Value, value.Value) {
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
	var doomed []doomedRows
	if failure := w.collect(table.table, rowids, map[string]bool{}, &doomed); failure.IsInvalid() {
		return value.Value{}, failure
	}
	if failure := w.deleteDoomed(doomed); failure.IsInvalid() {
		return value.Value{}, failure
	}
	return value.Number(float64(len(rowids))), value.Value{}
}

// deleteDoomed deletes the rows collect found, the rows pointing first: collect put each table after the ones it
// found pointing to it.
func (w *writer) deleteDoomed(doomed []doomedRows) value.Value {
	for i := len(doomed) - 1; i >= 0; i-- {
		if len(doomed[i].rowids) == 0 {
			continue
		}
		ids, err := json.Marshal(doomed[i].rowids)
		if err != nil {
			return value.Fail(value.DatabaseCode, err.Error())
		}
		if _, err := w.tx.Exec("DELETE FROM "+quote(doomed[i].table.Name)+" WHERE rowid IN (SELECT value FROM json_each(?))", string(ids)); err != nil {
			return w.failed(err)
		}
	}
	return value.Value{}
}

// doomedRows are rows of a table a delete removes.
type doomedRows struct {
	table  *Table
	rowids []int64
}

func (w *writer) rowids(text string, arguments ...any) ([]int64, error) {
	rows, err := w.tx.Query(text, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			return nil, err
		}
		out = append(out, rowid)
	}
	return out, rows.Err()
}

// collect finds every row a delete of table's rowids removes, before anything is deleted: the rows pointing to
// them through a .cascade(), theirs in turn; a row pointing without one is CONFLICT, so a delete never stops halfway.
// Each table comes in doomed before the tables pointing to it.
func (w *writer) collect(table *Table, rowids []int64, seen map[string]bool, doomed *[]doomedRows) value.Value {
	if len(rowids) == 0 {
		return value.Value{}
	}
	if seen[table.Name] {
		return value.Fail(value.ConflictCode, "xoá theo .cascade() đi vòng qua "+table.Name)
	}
	seen[table.Name] = true
	defer delete(seen, table.Name)
	*doomed = append(*doomed, doomedRows{table: table, rowids: rowids})
	ids, err := json.Marshal(rowids)
	if err != nil {
		return value.Fail(value.DatabaseCode, err.Error())
	}
	for _, child := range w.p.entry.Tables {
		for _, f := range child.Fields {
			if f.ForeignTarget == nil || f.ForeignTarget != table.Struct {
				continue
			}
			pointing := "SELECT rowid FROM " + quote(child.Name) + " WHERE " + quote(f.Name) + " IN (SELECT " + quote(f.ForeignField) + " FROM " + quote(table.Name) + " WHERE rowid IN (SELECT value FROM json_each(?)))"
			children, err := w.rowids(pointing, string(ids))
			if err != nil {
				return w.failed(err)
			}
			if len(children) == 0 {
				continue
			}
			if !f.Cascade {
				return value.Fail(value.ConflictCode, fmt.Sprintf("còn %d dòng của %s trỏ tới qua %s.%s: xoá chúng trước, hay khai foreign(…).cascade()", len(children), child.Name, child.Name, f.Name))
			}
			c := child
			if failure := w.collect(&c, children, seen, doomed); failure.IsInvalid() {
				return failure
			}
		}
	}
	return value.Value{}
}

// uuidV7 is a UUID of version 7 (RFC 9562): the time in milliseconds first, so its text sorts as it was made.
func uuidV7() string {
	var b [16]byte
	_, _ = rand.Read(b[6:])
	ms := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = 0x70 | b[6]&0x0f
	b[8] = 0x80 | b[8]&0x3f
	text := hex.EncodeToString(b[:])
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:32]
}
