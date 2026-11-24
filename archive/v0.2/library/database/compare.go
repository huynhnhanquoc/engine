package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/schema"
	_ "modernc.org/sqlite" // KitDB's SQLite, Go only: the one library beyond Go's own (docs/engine.md mục 1)
)

// The database is the root (docs/schema.md mục 2, the root's docs/KITWORK_DATABASE.md "Database là gốc"): as the app
// boots, each database it declares is read, read only, and held against the structs. What the file lacks (a table,
// a column, a unique constraint) and what differs from it (a column's storage, a decimal's scale, the key) is said;
// the statements on that database fail until kitwork migrate (migrate.go), or a schema statement through the port,
// adds it. What the statements run without (an index, a column's kind in kitwork_columns, the search index) is said as
// a note. A column the file has and no struct declares is the database's own. engine2 reads and writes the files the
// old engine wrote, in the same layout (kitwork_columns, a decimal as an integer times 10^scale, a timestamp as its
// ISO text): changing engines moves no data.

// Report is what one database's file says of its declaration.
type Report struct {
	Database string // the file as the app names it: .database/application/kitwork.db
	Absent   bool   // no file yet: kitwork migrate makes it
	Made     bool   // the file was made from its .sql as the boot read it (writers.go)
	from     string // the structure file it was made from
	Skipped  string // why the file is not read (a name of the host's catalogue)
	Lacking  []string
	Differ   []string
	Notes    []string
}

// Blocking is a report the statements on the database fail by.
func (r Report) Blocking() bool { return r.Absent || len(r.Lacking) > 0 || len(r.Differ) > 0 }

// Lines are the report as the boot's log says it, one line each; none when the file holds the declaration.
func (r Report) Lines() []string {
	switch {
	case r.Skipped != "":
		return []string{"[database] " + r.Database + ": " + r.Skipped}
	case r.Absent:
		return []string{"[database] " + r.Database + " chưa có: kitwork migrate tạo nó từ khai báo của app; câu lệnh trên database này lỗi tới khi đó"}
	}
	var lines []string
	if r.Made {
		lines = append(lines, "[database] "+r.Database+" tạo từ "+r.from+" (database là gốc: bảng và chỉ mục, không có dòng nào)")
	}
	if len(r.Lacking) > 0 || len(r.Differ) > 0 {
		line := "[database] " + r.Database
		if len(r.Lacking) > 0 {
			line += " chưa có: " + strings.Join(r.Lacking, "; ")
		}
		if len(r.Differ) > 0 {
			if len(r.Lacking) > 0 {
				line += ";"
			}
			line += " khác database: " + strings.Join(r.Differ, "; ")
		}
		lines = append(lines, line+"; câu lệnh trên database này lỗi tới khi kitwork migrate thêm, hay struct sửa theo database")
	}
	if len(r.Notes) > 0 {
		lines = append(lines, "[database] "+r.Database+": câu lệnh vẫn chạy; kitwork migrate sẽ ghi thêm "+strings.Join(r.Notes, "; "))
	}
	return lines
}

// File is the entry's file as a report names it: from the app's root for connection() (.database/application/, a
// schema its own file beside kitwork.db), connection("warehouse") for a name of the host's catalogue, whose file is
// the host's (Path).
func (e *Entry) File() string {
	if e.Name != "" {
		return "connection(" + strconv.Quote(e.Name) + ")"
	}
	file := "kitwork.db"
	if e.Schema != "" {
		file = e.Schema + ".db"
	}
	return ".database/application/" + file
}

// Check reads each database the app declares, read only, and says what its file lacks of the declaration. Each
// entry keeps its report, which the statements read.
func (c *Catalog) Check(appRoot string) []Report {
	reports := make([]Report, 0, len(c.entries))
	for _, entry := range c.entries {
		report, _ := checkEntry(appRoot, entry)
		entry.Report = report
		reports = append(reports, report)
	}
	return reports
}

// checkEntry reads one entry's file, read only, and gives its report and each table's gap (nil when the file
// was not read).
func checkEntry(appRoot string, entry *Entry) (Report, []*tableGap) {
	report := Report{Database: entry.File()}
	path := entry.Path
	// a folder holding the file's structure and not the file (a clone of the repository) has it made from it
	made, err := MakeFromStructure(path)
	if err != nil {
		report.Differ = append(report.Differ, err.Error())
		return report, nil
	}
	report.Made, report.from = made, filepath.Base(path)+".sql"
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		report.Absent = true
		return report, nil
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		report.Differ = append(report.Differ, "không mở được file: "+err.Error())
		return report, nil
	}
	defer db.Close()
	gaps, err := compareEntry(db, entry, nil)
	if err != nil {
		report.Differ = append(report.Differ, "không đọc được file: "+err.Error())
		return report, nil
	}
	for _, gap := range gaps {
		report.Lacking = append(report.Lacking, gap.lacking...)
		report.Differ = append(report.Differ, gap.differ...)
		report.Notes = append(report.Notes, gap.notes...)
	}
	return report, gaps
}

// querier reads a file's structure: the read-only pool at boot, kitwork migrate's transaction.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// compareEntry holds each table of the entry against the file q reads, as kitwork migrate's choices leave it (nil:
// as it is). A table the file lacks lists the file's tables no struct declares that hold all its columns: maybe it
// was renamed, which kitwork migrate asks.
func compareEntry(q querier, entry *Entry, c *choices) ([]*tableGap, error) {
	gaps := make([]*tableGap, 0, len(entry.Tables))
	var others []string // the file's tables no struct of the entry declares
	for _, table := range entry.Tables {
		gap, err := compareTable(q, entry, table, c)
		if err != nil {
			return nil, fmt.Errorf("bảng %s: %w", table.Name, err)
		}
		if gap.missing {
			if others == nil {
				if others, err = undeclaredTables(q, entry, c); err != nil {
					return nil, err
				}
			}
			for _, other := range others {
				columns, err := readColumns(q, other)
				if err != nil {
					return nil, err
				}
				holds := true
				for _, d := range gap.fields {
					if _, has := columns[d.name]; !has {
						holds = false
						break
					}
				}
				if holds {
					gap.tables = append(gap.tables, other)
				}
			}
		}
		gaps = append(gaps, gap)
	}
	return gaps, nil
}

// undeclaredTables are the file's tables no struct of the entry declares, the engine's own aside (kitwork_*, a table
// a choice renames already).
func undeclaredTables(q querier, entry *Entry, c *choices) ([]string, error) {
	rows, err := q.Query(`SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	others := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "kitwork_") || strings.HasPrefix(name, "_kitwork_") || c.renamesTable(name) {
			continue
		}
		declared := false
		for _, table := range entry.Tables {
			declared = declared || table.Name == name
		}
		if !declared {
			others = append(others, name)
		}
	}
	return others, rows.Err()
}

// declared is one field as the file keeps it: its kind in kitwork_columns, its SQLite storage, its PostgreSQL type.
type declared struct {
	name      string
	kind      string // kitwork_columns' kind: integer, kitid, uuid, text, float, decimal, bool, date, datetime, json
	storage   string // INTEGER, TEXT, REAL
	postgres  string // as PostgreSQL writes it, which the port's translator takes
	scale     int
	precision int
	spelling  string // as the struct writes it
	field     schema.TableField
}

// declare is a field as the file keeps it, in the old engine's layout and the port's words.
func declare(f schema.TableField) declared {
	d := declared{name: f.Name, kind: "text", storage: "TEXT", postgres: "text", spelling: f.Kind + "()", field: f}
	switch f.Kind {
	case "id":
		switch f.Generate {
		case "number":
			d.kind, d.storage, d.postgres, d.spelling = "integer", "INTEGER", "bigserial", "id()"
		case "uuid":
			d.kind, d.postgres, d.spelling = "uuid", "uuid", "id(\"uuid\")"
		default:
			d.kind, d.postgres, d.spelling = "kitid", "kitid", "id(\"kitwork\")"
		}
	case "uuid":
		d.kind, d.postgres = "uuid", "uuid"
	case "integer":
		d.kind, d.storage, d.postgres = "integer", "INTEGER", "bigint"
	case "float":
		d.kind, d.storage, d.postgres = "float", "REAL", "double precision"
	case "decimal":
		d.kind, d.storage, d.scale, d.precision = "decimal", "INTEGER", f.Scale, 15
		d.postgres = "numeric(15, " + strconv.Itoa(f.Scale) + ")"
		d.spelling = "decimal(" + strconv.Itoa(f.Scale) + ")"
	case "boolean":
		d.kind, d.storage, d.postgres = "bool", "INTEGER", "boolean"
	case "date":
		d.kind, d.postgres = "date", "date"
	case "timestamp":
		d.kind, d.postgres = "datetime", "timestamptz" // the old engine's ISO text; read as milliseconds
	case "json":
		d.kind, d.postgres = "json", "json"
	case "choice":
		d.spelling = "choice(…)"
	}
	if f.ForeignTarget != nil {
		d.spelling = "foreign(…)"
	}
	return d
}

// affinity is SQLite's type affinity of a declared column type (sqlite.org/datatype3.html, 3.1).
func affinity(declared string) string {
	upper := strings.ToUpper(declared)
	switch {
	case strings.Contains(upper, "INT"):
		return "INTEGER"
	case strings.Contains(upper, "CHAR"), strings.Contains(upper, "CLOB"), strings.Contains(upper, "TEXT"):
		return "TEXT"
	case upper == "", strings.Contains(upper, "BLOB"):
		return "BLOB"
	case strings.Contains(upper, "REAL"), strings.Contains(upper, "FLOA"), strings.Contains(upper, "DOUB"):
		return "REAL"
	}
	return "NUMERIC"
}

type liveColumn struct {
	storage string
	key     bool
}

type storedKind struct {
	kind             string
	precision, scale int
	weight           int
}

type liveIndex struct {
	unique, partial bool
	columns         []string
}

// index is an index a table declares: a column's .unique() or .index(), or a named group of them.
type index struct {
	name    string
	columns []string
	unique  bool
}

// tableGap is what one table's file lacks of the table's declaration, and what differs.
type tableGap struct {
	table   Table
	fields  []declared
	missing bool       // the file has no such table
	absent  []declared // declared columns the table has not, in declared order
	uniques []index    // unique constraints the table has not
	indexes []index    // indexes the table has not
	differs map[string]bool
	search  bool // the search index differs from kitwork_columns
	kinds   bool // a column's row of kitwork_columns is not the declaration's
	lacking []string
	differ  []string
	notes   []string
	// indexed are the file's columns an index or the key holds, which a drop cannot take
	indexed map[string]bool
	// candidates are, for each declared column the table lacks, the file's columns no struct declares of its storage
	// (and scale, for a number): maybe it was renamed, which kitwork migrate asks
	candidates map[string][]string
	// tables are, for a table the file lacks, the file's tables no struct declares holding all its columns
	tables []string
	// rescalable are the number columns whose scale differs from the file's, rescaled those a choice rewrites
	rescalable, rescaled []rescale
	// source is the file's table the declared table is read from: its own name, or the one a choice renames
	source string
}

// rescale is a number column (a decimal, an integer: both an INTEGER of the value × 10^scale) whose declared scale
// differs from the one the file keeps.
type rescale struct {
	column             string
	from, to           int
	precision          int
	decimal            bool
	file, declaredType string
}

// compareTable holds one table against the file, as the choices leave it: a table renamed is read under its new
// name, a column renamed under its new one, a column dropped is gone, a number rescaled has the declared scale.
func compareTable(q querier, entry *Entry, table Table, c *choices) (*tableGap, error) {
	gap := &tableGap{table: table, differs: map[string]bool{}, candidates: map[string][]string{}, source: c.sourceOf(table.Name)}
	for _, f := range table.Fields {
		gap.fields = append(gap.fields, declare(f))
	}
	columns, err := readColumns(q, gap.source)
	if err != nil {
		return nil, err
	}
	columns = c.viewColumns(table.Name, columns)
	if len(columns) == 0 {
		gap.missing, gap.absent = true, gap.fields
		listed := make([]string, len(gap.fields))
		for i, d := range gap.fields {
			listed[i] = d.name + " " + d.spelling
		}
		gap.lacking = append(gap.lacking, "bảng "+table.Name+" ("+strings.Join(listed, ", ")+")")
		return gap, nil
	}
	stored, err := readKinds(q, gap.source)
	if err != nil {
		return nil, err
	}
	stored = c.viewKinds(table.Name, stored)
	live, err := readIndexes(q, gap.source)
	if err != nil {
		return nil, err
	}
	live = c.viewIndexes(table.Name, live)
	gap.indexed = map[string]bool{}
	for _, held := range live {
		for _, column := range held.columns {
			gap.indexed[column] = true
		}
	}
	for name, column := range columns {
		if column.key {
			gap.indexed[name] = true
		}
	}
	var absent, kinds []string
	for _, d := range gap.fields {
		column, has := columns[d.name]
		if !has {
			gap.absent = append(gap.absent, d)
			absent = append(absent, d.name+" "+d.spelling)
			continue
		}
		at := table.Name + "." + d.name
		if affinity(column.storage) != d.storage {
			gap.differs[d.name] = true
			if d.kind == "decimal" && affinity(column.storage) == "TEXT" {
				gap.differ = append(gap.differ, "cột "+at+" là decimal lưu dạng chữ (cách trước 29/10/2026), nay SQLite lưu decimal là số nguyên nhân 10^số_lẻ")
			} else {
				gap.differ = append(gap.differ, fmt.Sprintf("cột %s: database lưu %s, code khai %s (lưu %s) (đổi cách lưu cần dựng lại bảng, chưa có)", at, column.storage, d.spelling, d.storage))
			}
			continue
		}
		if d.field.Key && !column.key {
			gap.differs[d.name] = true
			gap.differ = append(gap.differ, "khoá của bảng "+table.Name+": code khai "+d.name+", database không lấy nó làm khoá")
		}
		row, recorded := stored[d.name]
		from, fromNumber := storedScale(row, recorded)
		to, toNumber := declaredScale(d)
		switch {
		case fromNumber && toNumber && from != to:
			// a number whose scale differs: its values rewritten, when kitwork migrate was told so
			file := "integer"
			if recorded && row.kind == "decimal" {
				file = fmt.Sprintf("decimal(%d)", row.scale)
			}
			found := rescale{column: d.name, from: from, to: to, precision: max(d.precision, 15), decimal: d.kind == "decimal", file: file, declaredType: d.spelling}
			if c.rescales(table.Name, d.name) {
				gap.rescaled = append(gap.rescaled, found)
				kinds = append(kinds, at)
				continue
			}
			gap.rescalable = append(gap.rescalable, found)
			gap.differs[d.name] = true
			gap.differ = append(gap.differ, fmt.Sprintf("cột %s: database ghi %s (kitwork_columns), code khai %s", at, file, d.spelling))
		case d.kind == "decimal" && recorded && row.kind != "decimal" && row.kind != "integer",
			d.kind != "decimal" && recorded && row.kind == "decimal":
			gap.differs[d.name] = true
			gap.differ = append(gap.differ, fmt.Sprintf("cột %s: database ghi %s (kitwork_columns), code khai %s", at, row.kind, d.spelling))
		case !recorded || row.kind != d.kind || d.kind == "decimal" && row.precision != d.precision:
			kinds = append(kinds, at)
		}
	}
	if len(absent) > 0 {
		var hints []string
		for _, d := range gap.absent {
			candidates := renameCandidates(columns, stored, gap.fields, d)
			if len(candidates) > 0 {
				gap.candidates[d.name] = candidates
				hints = append(hints, d.name+" từ "+strings.Join(candidates, " hay ")+"?")
			}
		}
		line := "cột " + strings.Join(absent, ", ") + " của bảng " + table.Name
		if len(hints) > 0 {
			line += " (database có cột cùng kiểu struct không khai, có phải đổi tên: " + strings.Join(hints, ", ") + " kitwork migrate hỏi)"
		}
		gap.lacking = append(gap.lacking, line)
	}
	for _, unique := range declaredUniques(table) {
		if !uniqueHeld(live, unique.columns) {
			gap.uniques = append(gap.uniques, unique)
			gap.lacking = append(gap.lacking, "ràng buộc unique ("+strings.Join(unique.columns, ", ")+") của bảng "+table.Name)
		}
	}
	for _, wanted := range declaredIndexes(table) {
		if !indexHeld(live, wanted.columns) {
			gap.indexes = append(gap.indexes, wanted)
			gap.notes = append(gap.notes, "chỉ mục ("+strings.Join(wanted.columns, ", ")+") của bảng "+table.Name)
		}
	}
	if len(kinds) > 0 {
		gap.kinds = true
		gap.notes = append(gap.notes, "kiểu của cột "+strings.Join(kinds, ", ")+" trong kitwork_columns")
	}
	if search := declaredSearch(table); len(search) > 0 && !sameSearch(search, stored) {
		gap.search = true
		gap.notes = append(gap.notes, "chỉ mục tìm kiếm "+table.Name+"_search ("+strings.Join(search, ", ")+")")
	}
	return gap, nil
}

// renameCandidates are the file's columns no struct declares that a declared column the table lacks may have been
// renamed from: its storage, and a number's scale (a decimal of the same scale, an integer for an integer).
func renameCandidates(columns map[string]liveColumn, stored map[string]storedKind, fields []declared, d declared) []string {
	var candidates []string
	for name, column := range columns {
		if declaredIn(fields, name) || column.key || affinity(column.storage) != d.storage {
			continue
		}
		row, recorded := stored[name]
		fileDecimal := recorded && row.kind == "decimal"
		if d.kind == "decimal" && (!fileDecimal || row.scale != d.scale) || d.kind != "decimal" && fileDecimal {
			continue
		}
		candidates = append(candidates, name)
	}
	sort.Strings(candidates)
	return candidates
}

// storedScale is the scale a number column's values are kept at: a decimal's, 0 for an integer or a column of no row
// of kitwork_columns; false for any other kind.
func storedScale(row storedKind, recorded bool) (int, bool) {
	switch {
	case !recorded || row.kind == "integer":
		return 0, true
	case row.kind == "decimal":
		return row.scale, true
	}
	return 0, false
}

// declaredScale is the scale a declared number column's values are kept at: a decimal's, 0 for an integer.
func declaredScale(d declared) (int, bool) {
	switch {
	case d.kind == "decimal":
		return d.scale, true
	case d.kind == "integer" && d.field.Kind == "integer":
		return 0, true
	}
	return 0, false
}

func declaredIn(fields []declared, name string) bool {
	for _, d := range fields {
		if d.name == name {
			return true
		}
	}
	return false
}

// readColumns are a table's columns, by name; none when the file has no such table.
func readColumns(q querier, table string) (map[string]liveColumn, error) {
	rows, err := q.Query(`SELECT name, type, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]liveColumn{}
	for rows.Next() {
		var name, kind string
		var key int
		if err := rows.Scan(&name, &kind, &key); err != nil {
			return nil, err
		}
		columns[name] = liveColumn{storage: strings.ToUpper(strings.TrimSpace(kind)), key: key > 0}
	}
	return columns, rows.Err()
}

// readKinds are kitwork_columns's rows of a table, by column; none when the file has no kitwork_columns.
func readKinds(q querier, table string) (map[string]storedKind, error) {
	kinds := map[string]storedKind{}
	columns, err := readColumns(q, "kitwork_columns")
	if err != nil || len(columns) == 0 {
		return kinds, err
	}
	weight := "0"
	if _, held := columns["search_weight"]; held { // written before 31/10/2026 without it
		weight = "search_weight"
	}
	rows, err := q.Query(`SELECT column_name, kind, precision, scale, `+weight+` FROM kitwork_columns WHERE table_name = ?`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		var row storedKind
		if err := rows.Scan(&column, &row.kind, &row.precision, &row.scale, &row.weight); err != nil {
			return nil, err
		}
		kinds[column] = row
	}
	return kinds, rows.Err()
}

// readIndexes are a table's indexes, SQLite's own of a UNIQUE or PRIMARY KEY constraint among them.
func readIndexes(q querier, table string) ([]liveIndex, error) {
	rows, err := q.Query(`SELECT name, "unique", partial FROM pragma_index_list(?)`, table)
	if err != nil {
		return nil, err
	}
	type listed struct {
		name            string
		unique, partial int
	}
	var names []listed
	for rows.Next() {
		var l listed
		if err := rows.Scan(&l.name, &l.unique, &l.partial); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, l)
	}
	rows.Close()
	indexes := make([]liveIndex, 0, len(names))
	for _, l := range names {
		columns, err := q.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, l.name)
		if err != nil {
			return nil, err
		}
		found := liveIndex{unique: l.unique == 1, partial: l.partial == 1}
		for columns.Next() {
			var name sql.NullString
			if err := columns.Scan(&name); err != nil {
				columns.Close()
				return nil, err
			}
			found.columns = append(found.columns, name.String)
		}
		columns.Close()
		indexes = append(indexes, found)
	}
	return indexes, nil
}

// declaredUniques are a table's unique constraints: each column's .unique() (not the key), and each group, its
// columns in the order written or pinned.
func declaredUniques(table Table) []index {
	var uniques []index
	for _, f := range table.Fields {
		if f.Unique && !f.Key {
			uniques = append(uniques, index{name: table.Name + "_" + f.Name + "_unique", columns: []string{f.Name}, unique: true})
		}
	}
	for _, group := range groups(table, func(f schema.TableField) (string, int) { return f.Group, f.GroupAt }) {
		uniques = append(uniques, index{name: table.Name + "_" + group.name, columns: group.columns, unique: true})
	}
	return uniques
}

// declaredIndexes are a table's indexes: each column's .index(), and each named one.
func declaredIndexes(table Table) []index {
	var indexes []index
	for _, f := range table.Fields {
		if f.Index && f.IndexName == "" {
			indexes = append(indexes, index{name: table.Name + "_" + f.Name, columns: []string{f.Name}})
		}
	}
	for _, group := range groups(table, func(f schema.TableField) (string, int) { return f.IndexName, f.IndexAt }) {
		indexes = append(indexes, index{name: table.Name + "_" + group.name, columns: group.columns})
	}
	return indexes
}

type group struct {
	name    string
	columns []string
}

// groups are the named groups of a table's columns, by name, each its columns by position then the order written.
func groups(table Table, of func(schema.TableField) (string, int)) []group {
	type member struct {
		name     string
		at, seen int
	}
	byName := map[string][]member{}
	var names []string
	for seen, f := range table.Fields {
		name, at := of(f)
		if name == "" {
			continue
		}
		if _, ok := byName[name]; !ok {
			names = append(names, name)
		}
		byName[name] = append(byName[name], member{name: f.Name, at: at, seen: seen})
	}
	out := make([]group, 0, len(names))
	for _, name := range names {
		members := byName[name]
		sort.SliceStable(members, func(i, j int) bool {
			a, b := members[i], members[j]
			switch {
			case a.at != 0 && b.at != 0:
				return a.at < b.at
			case a.at != 0:
				return true
			case b.at != 0:
				return false
			}
			return a.seen < b.seen
		})
		columns := make([]string, len(members))
		for i, m := range members {
			columns[i] = m.name
		}
		out = append(out, group{name: name, columns: columns})
	}
	return out
}

// uniqueHeld reports a unique index over every row on exactly columns, in any order.
func uniqueHeld(indexes []liveIndex, columns []string) bool {
	want := append([]string(nil), columns...)
	sort.Strings(want)
	for _, index := range indexes {
		if !index.unique || index.partial || len(index.columns) != len(want) {
			continue
		}
		have := append([]string(nil), index.columns...)
		sort.Strings(have)
		if strings.Join(have, "\x00") == strings.Join(want, "\x00") {
			return true
		}
	}
	return false
}

// indexHeld reports an index over every row on the same columns in the same order, whatever its name (one made
// through the port has its own), a unique one too.
func indexHeld(indexes []liveIndex, columns []string) bool {
	for _, index := range indexes {
		if !index.partial && strings.Join(index.columns, "\x00") == strings.Join(columns, "\x00") {
			return true
		}
	}
	return false
}

// declaredSearch are a table's .searchable() columns, in the order written.
func declaredSearch(table Table) []string {
	var columns []string
	for _, f := range table.Fields {
		if f.Searchable {
			columns = append(columns, f.Name)
		}
	}
	return columns
}

// sameSearch reports kitwork_columns weighting exactly the declared columns.
func sameSearch(declared []string, stored map[string]storedKind) bool {
	held := 0
	for _, row := range stored {
		if row.weight > 0 {
			held++
		}
	}
	if held != len(declared) {
		return false
	}
	for _, column := range declared {
		if stored[column].weight <= 0 {
			return false
		}
	}
	return true
}
