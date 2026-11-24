package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
)

// history() and at() (docs/KITWORK_DATABASE.md mục 2, "Thời điểm và lịch sử"; KITWORK_KITDB.md mục 5): what KitDB's
// writer records with every commit, in kitwork_history.
//
// db.history((orders) => condition), or db.where(…).history(), is the changes of the rows the condition keeps, oldest
// first, each { at, before, after, by }: at the commit's moment in milliseconds, before and after the row as it was
// and as the commit left it (null for an insert's before and a delete's after), by who wrote it, null for none. A
// change is kept when the row before it or after it meets the condition, so a row since deleted has its history; the
// condition runs as SQL on the images, as the statement's own SQL (the images in a WITH named as the table), so it
// means what it means in list. A .secret() column is never in an image read.
//
// db.at(moment).list(…) reads a table as it was at the moment: an ISO text with its zone, milliseconds, or a Date.
// The engine goes back from the live file (kitdb.PastRelation: the rows the commits after it changed put back as they
// were), or, when the history after it cannot be undone that way, reads a copy of the file restored as it was then.
// It reads one table, and only reads.
//
// Both read a table the history keeps (KITWORK_KITDB.md câu 13.11): its history is the database's to declare, WITH
// (history = on) or ALTER TABLE … SET (history = on), off for a table made since 07/11/2026 and on for every table of
// a file of before. A table without is the statement's failure, saying how to turn it on. A table whose history was
// turned on after rows changed has it from then (kitdb.TableHistory): history() gives the changes since, and at() a
// moment before is the statement's failure, as is one a restore could reach only across that start.

// historyLimit bounds the changes one history() gives.
const historyLimit = 10_000

// snapshotContext lets KitDB's readers of the history read through a snapshot, which takes no context.
type snapshotContext struct{ read querier }

func (s snapshotContext) QueryContext(_ context.Context, query string, arguments ...any) (*sql.Rows, error) {
	return s.read.Query(query, arguments...)
}

// History is db.history((orders) => …): the changes of the rows the condition keeps.
func (h *Handle) History(arguments ...value.Value) value.Value {
	return h.query().terminal("history", arguments)
}
func (q *Query) History(arguments ...value.Value) value.Value {
	return q.terminal("history", arguments)
}

// history reads the changes in one snapshot: the history and the rows as they are now, which an update kept as its
// changed columns alone is walked back from (kitdb.WholeImages).
func (q *Query) history(p *plan, database *kitdb.Database) value.Value {
	table := p.tables[0]
	var out []value.Value
	var failure value.Value
	err := q.reading(database, func(read querier) error {
		ctx, cancel := value.WaitContext(q.handle.waits, time.Time{})
		defer cancel()
		on, from, err := kitdb.TableHistory(ctx, snapshotContext{read}, table.name)
		if err != nil {
			return err
		}
		if !on {
			failure = noHistory("history()", table.name)
			return nil
		}
		var changes []kitdb.Change
		var moments []int64
		// the changes from where the history keeps every one of the table's: one before, rows changed with no trace
		if err := kitdb.ReadChanges(ctx, snapshotContext{read}, from-1, func(commit kitdb.Commit) error {
			for _, change := range commit.Changes {
				switch change.Operation {
				case kitdb.Insert, kitdb.Update, kitdb.Delete:
					if strings.EqualFold(change.Table, table.name) {
						changes = append(changes, change)
						moments = append(moments, commit.At.UnixMilli())
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if err := kitdb.WholeImages(changes, table.name, liveRows(read)); err != nil {
			return err
		}
		images := make([][2]map[string]any, len(changes))
		for i, change := range changes {
			if change.Operation != kitdb.Insert {
				images[i][0] = namedImage(change.Columns, change.Before)
			}
			if change.Operation != kitdb.Delete {
				images[i][1] = namedImage(change.Columns, change.After)
			}
		}
		kept, f, err := q.keptChanges(p, read, images)
		if err != nil || f.IsInvalid() {
			failure = f
			return err
		}
		if len(kept) > historyLimit {
			failure = value.Fail(value.InvalidArgumentCode, fmt.Sprintf("history() của %s có %d thay đổi, quá %d: thu hẹp điều kiện", table.name, len(kept), historyLimit))
			return nil
		}
		var columns []declared
		for _, f := range table.table.Fields {
			if !f.Secret {
				columns = append(columns, declare(f))
			}
		}
		for _, i := range kept {
			entry := value.Object().Set("at", value.Number(float64(moments[i]))).Set("before", value.Null()).Set("after", value.Null()).Set("by", value.Null())
			for side, name := range []string{"before", "after"} {
				if image := images[i][side]; image != nil {
					row := value.Object()
					for _, column := range columns {
						row = row.Set(column.name, decode(column, image[column.name]))
					}
					entry = entry.Set(name, row)
				}
			}
			if by := changes[i].By; by != "" {
				entry = entry.Set("by", value.String(by))
			}
			out = append(out, entry)
		}
		return nil
	})
	if failure.IsInvalid() {
		return failure
	}
	if err != nil {
		return value.Fail(value.DatabaseCode, "history() của "+table.name+": "+err.Error())
	}
	if out == nil {
		out = []value.Value{}
	}
	return value.Array(out)
}

// keptChanges are the changes whose image before or after meets the statement's conditions, in order: the images
// in batches, each a WITH named as the table, read by the conditions' own SQL.
func (q *Query) keptChanges(p *plan, read querier, images [][2]map[string]any) ([]int, value.Value, error) {
	if len(p.where) == 0 {
		all := make([]int, len(images))
		for i := range all {
			all[i] = i
		}
		return all, value.Value{}, nil
	}
	table := p.tables[0]
	var names, quoted []string
	for _, f := range table.table.Fields {
		names = append(names, f.Name)
		quoted = append(quoted, quote(f.Name))
	}
	type image struct {
		change int
		values map[string]any
	}
	var all []image
	for i, sides := range images {
		for _, side := range sides {
			if side != nil {
				all = append(all, image{change: i, values: side})
			}
		}
	}
	kept := map[int]bool{}
	batch := max(1, 30_000/(len(names)+1)-1)
	for start := 0; start < len(all); start += batch {
		part := all[start:min(start+batch, len(all))]
		var args []any
		rows := make([]string, len(part))
		for i, each := range part {
			marks := []string{"?"}
			args = append(args, each.change)
			for _, name := range names {
				args = append(args, each.values[name])
				marks = append(marks, "?")
			}
			rows[i] = "(" + strings.Join(marks, ", ") + ")"
		}
		r := &renderer{plan: p, steps: q.steps}
		var conditions []string
		for _, each := range p.where {
			text, failure := r.condition(each.node, r.lambda(each))
			if failure.IsInvalid() {
				return nil, failure, nil
			}
			conditions = append(conditions, text)
		}
		text := "WITH " + quote(table.name) + " (kitwork_change, " + strings.Join(quoted, ", ") + ") AS (VALUES " + strings.Join(rows, ", ") +
			") SELECT DISTINCT kitwork_change FROM " + quote(table.name) + " WHERE " + strings.Join(conditions, " AND ")
		result, err := read.Query(text, append(args, r.args...)...)
		if err != nil {
			return nil, value.Value{}, err
		}
		for result.Next() {
			var i int
			if err := result.Scan(&i); err != nil {
				result.Close()
				return nil, value.Value{}, err
			}
			kept[i] = true
		}
		err = result.Err()
		result.Close()
		if err != nil {
			return nil, value.Value{}, err
		}
	}
	order := make([]int, 0, len(kept))
	for i := range kept {
		order = append(order, i)
	}
	sort.Ints(order)
	return order, value.Value{}, nil
}

// namedImage is a row image by its columns' names; nil when the change does not name them.
func namedImage(columns []string, values []any) map[string]any {
	if columns == nil || len(columns) != len(values) {
		return nil
	}
	image := make(map[string]any, len(columns))
	for i, column := range columns {
		image[column] = values[i]
	}
	return image
}

// liveRows reads rows of a table as they are now, by rowid, five hundred at a time: what kitdb.WholeImages walks a
// row back from.
func liveRows(read querier) kitdb.LiveRows {
	return func(table string, rowIDs []int64) ([]string, map[int64][]any, error) {
		var columns []string
		found := make(map[int64][]any, len(rowIDs))
		for start := 0; start < len(rowIDs); start += 500 {
			part := rowIDs[start:min(start+500, len(rowIDs))]
			args := make([]any, len(part))
			for i, rowID := range part {
				args[i] = rowID
			}
			rows, err := read.Query("SELECT _rowid_, * FROM main."+quote(table)+" WHERE _rowid_ IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(part)), ", ")+")", args...)
			if err != nil {
				return nil, nil, err
			}
			names, err := rows.Columns()
			if err != nil {
				rows.Close()
				return nil, nil, err
			}
			columns = names[1:]
			for rows.Next() {
				values := make([]any, len(names))
				pointers := make([]any, len(names))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					rows.Close()
					return nil, nil, err
				}
				rowID, _ := values[0].(int64)
				found[rowID] = values[1:]
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
		}
		return columns, found, nil
	}
}

// At is db.at(moment): the statement reads the table as it was at the moment.
func (h *Handle) At(arguments ...value.Value) value.Value { return h.query().At(arguments...) }

func (q *Query) At(arguments ...value.Value) value.Value {
	if failure := q.alive(); failure.IsInvalid() {
		return failure
	}
	if q.handle.tx != nil {
		return value.Fail(value.UnsupportedCode, "at() trong giao dịch chưa có: giao dịch đọc chính nó, lúc này")
	}
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, "at nhận một thời điểm: db.at(\"2026-10-16T23:00:00+07:00\").list(…)")
	}
	moment, failure := momentOf(arguments[0])
	if failure.IsInvalid() {
		return failure
	}
	if q.moment != nil {
		return value.Fail(value.InvalidArgumentCode, "at() hai lần: một câu đọc một thời điểm")
	}
	next := &Query{handle: q.handle, steps: q.steps, moment: &moment}
	return value.Struct(next)
}

// momentOf reads a moment: an ISO text with its zone, milliseconds since 1970, a Date. A moment is a millisecond, as
// the times of Kitwork are (timestamp(), Date, history()'s at): the table is read as it was at the end of it, every
// commit within it there. A commit is timed to the nanosecond and history() gives its at in milliseconds, cut down, so
// at(change.at) read the table before a change of the same millisecond, and at(Date.now()) taken just after two writes
// could see the first and not the second (12/11/2026, TestTheHistoryAndThePastOfATable failing 3 times in 40).
func momentOf(item value.Value) (time.Time, value.Value) {
	var moment time.Time
	switch {
	case item.IsNumber():
		moment = time.UnixMilli(int64(item.Number()))
	case item.IsTime():
		moment = item.Time()
	case item.IsString():
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, item.Text()); err == nil {
				moment = parsed
				break
			}
		}
	}
	if moment.IsZero() {
		return time.Time{}, value.Fail(value.InvalidArgumentCode, "at nhận chữ ISO có múi giờ (\"2026-10-16T23:00:00+07:00\"), số mili giây hay Date")
	}
	return time.UnixMilli(moment.UnixMilli()).Add(time.Millisecond - time.Nanosecond).UTC(), value.Value{}
}

// atBackRows bounds the rows going back changes in one read; past it, the file is restored.
const atBackRows = 100_000

// noHistory is the failure of history() or at() on a table the history does not keep.
func noHistory(word, table string) value.Value {
	return value.Fail(value.DatabaseCode, fmt.Sprintf("%s của %s: bảng chưa bật lịch sử; bật bằng ALTER TABLE %s SET (history = on) qua cổng database, "+
		"hoặc WITH (history = on) sau bảng trong file .sql", word, table, table))
}

// past runs a read statement on the table as it was at q.moment.
func (q *Query) past(p *plan, database *kitdb.Database, r *renderer, text func() (string, value.Value)) value.Value {
	table := p.tables[0].name
	ctx, cancel := value.WaitContext(q.handle.waits, time.Time{})
	defer cancel()
	moment := *q.moment
	word := "at(" + moment.Format(time.RFC3339) + ")"
	var on bool
	var from uint64
	var since int64 // the moment of the revision the table's history starts from, in nanoseconds
	if err := database.Read(ctx, func(snapshot *kitdb.Snapshot) error {
		var err error
		if on, from, err = kitdb.TableHistory(ctx, snapshotContext{snapshot}, table); err != nil || !on || from <= 1 {
			return err
		}
		rows, err := snapshot.Query(`SELECT at FROM kitwork_history WHERE rev = ?`, int64(from))
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			err = rows.Scan(&since)
		}
		return errors.Join(err, rows.Err())
	}); err != nil {
		return value.Fail(value.DatabaseCode, word+": "+err.Error())
	}
	if !on {
		return noHistory(word, table)
	}
	revision, err := database.RevisionAt(ctx, moment)
	if errors.Is(err, kitdb.ErrPruned) {
		// the history kept no longer reaches the moment: say from when it does, and how long it keeps
		if kept, _, keptErr := database.KeptFrom(ctx); keptErr == nil {
			return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("at(%s): lịch sử của database chỉ còn từ %s; engine giữ %d ngày (app.history({ days }) trong application.work, mặc định %d)",
				moment.Format(time.RFC3339), kept.UTC().Format(time.RFC3339), HistoryDays(), HistoryDaysDefault))
		}
	}
	if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("at(%s): database chưa giữ lịch sử từ lúc đó: %v", moment.Format(time.RFC3339), err))
	}
	if from > 1 && revision < from {
		// the table as it was then is not in the history: rows changed with no trace before its history began
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s: lịch sử của %s chỉ có từ %s, lúc bảng bật lịch sử", word, table,
			time.Unix(0, since).UTC().Format(time.RFC3339)))
	}
	path := q.handle.entry.Path
	read := func(file string, building func(*sql.Conn) (string, error)) value.Value {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
		if err != nil {
			return value.Fail(value.DatabaseCode, err.Error())
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		conn, err := db.Conn(ctx)
		if err != nil {
			return value.Fail(value.DatabaseCode, err.Error())
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			return value.Fail(value.DatabaseCode, err.Error())
		}
		// the rollback ends the read and takes the temporary tables and view with it
		defer conn.ExecContext(context.Background(), "ROLLBACK")
		relation := table
		if building != nil {
			if relation, err = building(conn); err != nil {
				return value.Fail(value.DatabaseCode, err.Error())
			}
		}
		r.relations = map[string]string{table: relation}
		statement, failure := text()
		if failure.IsInvalid() {
			return failure
		}
		result, err := run(connQuerier{conn, ctx}, p, r, statement)
		if err != nil {
			return value.Fail(value.DatabaseCode, fmt.Sprintf("at(%s): %v", moment.Format(time.RFC3339), err))
		}
		return result
	}
	var unreachable error // why the history after the moment cannot be undone going back
	result := read(path, func(conn *sql.Conn) (string, error) {
		relation, err := kitdb.PastRelation(ctx, conn, table, revision, atBackRows)
		if errors.Is(err, kitdb.ErrPastUnreachable) || errors.Is(err, kitdb.ErrCorruptHistory) {
			unreachable = err
		}
		return relation, err
	})
	if unreachable == nil {
		return result
	}
	// the history after the moment cannot be undone going back: a copy of the file restored as it was then
	copy := filepath.Join(os.TempDir(), "kitwork-at-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".db")
	defer func() {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(copy + suffix)
		}
	}()
	if _, err := database.RestoreTable(ctx, moment, copy, table); errors.Is(err, kitdb.ErrPastUnreachable) {
		// the newest anchor before the moment is older than where the table's history begins, and the commits after the
		// moment cannot be undone going back
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s: %s lúc đó không đọc lại được: không lùi được từ bây giờ (%v), và mốc sao lưu gần nhất "+
			"trước lúc đó có trước khi bảng bật lịch sử", word, table, unreachable))
	} else if errors.Is(err, kitdb.ErrNoAnchor) {
		// no anchor is as old as the moment (the first of a table may still be written in the background, or the anchors
		// are gone): say from when one is (09/11/2026; it was KitDB's own English)
		oldest := "chưa có mốc nào: mốc đầu được chụp ở nền khi bảng bật lịch sử, rồi mỗi ngày một lần"
		if at, found, oldestErr := database.OldestAnchor(); oldestErr == nil && found {
			oldest = "mốc cũ nhất là lúc " + at.Format(time.RFC3339) + ", đọc lại được từ lúc đó"
		}
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s: %s lúc đó không đọc lại được: không lùi được từ bây giờ (%v), và không có mốc "+
			"sao lưu nào trước lúc đó; %s", word, table, unreachable, oldest))
	} else if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("at(%s): %v", moment.Format(time.RFC3339), err))
	}
	r.args = nil
	return read(copy, nil)
}

// connQuerier reads on one connection, within the run's context.
type connQuerier struct {
	conn *sql.Conn
	ctx  context.Context
}

func (c connQuerier) Query(query string, arguments ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(c.ctx, query, arguments...)
}
