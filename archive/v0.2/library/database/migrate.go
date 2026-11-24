package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/huynhnhanquoc/sqlitewire/gateway"
	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
)

// kitwork migrate (docs/schema.md mục 8, slice 2): the one way the structs reach the database, the runner agreeing.
// It adds what a file lacks of the declaration (the file itself, a table, a column, a unique constraint, an index,
// a column's kind in kitwork_columns, the search index) and changes nothing the file has: what differs is the
// struct's to follow, and a column the file has and no struct declares stays the database's own. It writes in
// PostgreSQL's words through the port's translator (gateway.TranslateSchema), so a table it makes is the STRICT table
// the port makes and the old engine's kitwork migrate made, and the plan shows statements one can run anywhere; it
// applies on KitDB's writer (Maintain: one transaction, a Barrier in the history), which writes the structure file
// beside the database (kitwork.db.sql). A file another process writes (a server running) is changed through that
// server's database port when application.work declares one (ThroughPort): the plan's PostgreSQL statements, sent as
// its admin, as TablePlus would send them; else it is refused. What the declaration cannot tell, a rename, a drop, a
// number's new scale, it asks (choices.go).

// by is who a migration's commit names in the history.
const by = "kitwork migrate"

// Plan is what kitwork migrate does to one database file of an app.
type Plan struct {
	App        string // the app's folder
	Database   string // the file as the app names it: .database/application/kitwork.db
	Path       string // the file
	Absent     bool   // no file yet
	Add        []string
	Write      []string
	Differ     []string
	Statements []Statement
	entry      *Entry
	pending    bool
	// gaps are the file as the plan read it, as the choices leave it; choices what kitwork migrate was told
	gaps    []*tableGap
	choices choices
	// Through says, in words, the port a plan of a file a server holds goes through (ThroughPort), "" for none; Left
	// is what applying through it could not write, which kitwork migrate writes with the server stopped (a kind of a
	// column already there)
	Through string
	Left    []string
	Sent    bool // the plan went through the port
	send    func(ctx context.Context, statements []string) error
}

// ThroughPort makes the plan apply through a database port when another process holds the file: send runs the
// plan's PostgreSQL statements there, as its admin; through names the port and the database in words.
func (p *Plan) ThroughPort(through string, send func(ctx context.Context, statements []string) error) {
	p.Through, p.send = through, send
}

// applyThroughPort sends the plan's statements to the port, then holds the file against the declaration again,
// reading only: what it still lacks is an error, what it notes (a kind, a rule) is Left.
func (p *Plan) applyThroughPort(ctx context.Context) error {
	statements := make([]string, len(p.Statements))
	for i, statement := range p.Statements {
		statements[i] = statement.Postgres
	}
	p.Sent = true
	if err := p.send(ctx, statements); err != nil {
		return fmt.Errorf("%s, qua %s: %w", p.Database, p.Through, err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p.Path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	gaps, err := compareEntry(db, p.entry, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", p.Database, err)
	}
	var lacking []string
	p.Left = nil
	for _, gap := range gaps {
		lacking = append(lacking, gap.lacking...)
		p.Left = append(p.Left, gap.notes...)
	}
	if len(lacking) > 0 {
		return fmt.Errorf("%s: sau các câu qua %s, database vẫn chưa có: %s", p.Database, p.Through, strings.Join(lacking, "; "))
	}
	return nil
}

// Statement is one change a plan makes: as PostgreSQL writes it, and the SQLite statements that run on the file
// (none for the search index, which kitwork_columns holds).
type Statement struct {
	Table, Action string
	Postgres      string
	SQLite        []string
	// from and to are what a choice renames, drops or rescales (choices.go); guard counts the values a rescale would
	// carry past the precision, rescaled is the rescale
	from, to, guard string
	rescaled        *rescale
}

// Shown is the statement as the plan shows it.
func (s Statement) Shown() string { return s.Postgres + ";" }

// Pending reports a plan that changes the file.
func (p *Plan) Pending() bool { return p.pending }

// Plans are kitwork migrate's plans of the app's databases at appRoot, read only: one per file connection() and
// its schemas name (a name of the host's catalogue waits for app.databases).
func (c *Catalog) Plans(appRoot, app string) ([]*Plan, error) {
	var plans []*Plan
	for _, entry := range c.entries {
		file := entry.File()
		report, gaps := checkEntry(appRoot, entry)
		plan := &Plan{App: app, Database: file, Path: entry.Path, Absent: report.Absent, entry: entry, choices: newChoices()}
		if report.Absent {
			gaps = missingGaps(entry)
		} else if gaps == nil {
			return nil, fmt.Errorf("%s: %s", file, strings.Join(report.Differ, "; "))
		}
		plan.fill(gaps)
		plans = append(plans, plan)
	}
	return plans, nil
}

// missingGaps are the gaps of a file not there: every table missing.
func missingGaps(entry *Entry) []*tableGap {
	gaps := make([]*tableGap, 0, len(entry.Tables))
	for _, table := range entry.Tables {
		gap := &tableGap{table: table, missing: true, differs: map[string]bool{}}
		for _, f := range table.Fields {
			gap.fields = append(gap.fields, declare(f))
		}
		gap.absent = gap.fields
		listed := make([]string, len(gap.fields))
		for i, d := range gap.fields {
			listed[i] = d.name + " " + d.spelling
		}
		gap.lacking = []string{"bảng " + table.Name + " (" + strings.Join(listed, ", ") + ")"}
		gaps = append(gaps, gap)
	}
	return gaps
}

// fill writes the plan's lines and statements from the gaps.
func (p *Plan) fill(gaps []*tableGap) {
	p.Add, p.Write, p.Differ, p.gaps = nil, nil, nil, gaps
	p.Statements = choiceStatements(&p.choices, gaps)
	p.pending = p.Absent || len(p.Statements) > 0
	for _, gap := range gaps {
		p.Add = append(p.Add, gap.lacking...)
		p.Write = append(p.Write, gap.notes...)
		p.Differ = append(p.Differ, gap.differ...)
		if gap.missing || len(gap.absent) > 0 || len(gap.uniques) > 0 || len(gap.indexes) > 0 || gap.kinds || gap.search {
			p.pending = true
		}
		p.Statements = append(p.Statements, statementsOf(p.entry, gap)...)
	}
}

// statementsOf are the changes that make one table of the file what its declaration asks, adding only.
func statementsOf(entry *Entry, gap *tableGap) []Statement {
	name := gap.table.Name
	var out []Statement
	if gap.missing {
		definitions := make([]string, len(gap.fields))
		for i, d := range gap.fields {
			definitions[i] = columnDefinition(entry, d, false)
		}
		out = append(out, translated(name, "create-table", "CREATE TABLE "+gateway.WrittenName(name)+" (\n  "+strings.Join(definitions, ",\n  ")+"\n)"))
		for _, unique := range declaredUniques(gap.table) {
			if len(unique.columns) > 1 { // a column's own .unique() is in CREATE TABLE
				out = append(out, translated(name, "add-unique", indexStatement(name, unique)))
			}
		}
		for _, wanted := range declaredIndexes(gap.table) {
			out = append(out, translated(name, "add-index", indexStatement(name, wanted)))
		}
	} else {
		for _, d := range gap.absent {
			out = append(out, translated(name, "add-column", "ALTER TABLE "+gateway.WrittenName(name)+" ADD COLUMN "+columnDefinition(entry, d, true)))
		}
		for _, unique := range gap.uniques {
			out = append(out, translated(name, "add-unique", indexStatement(name, unique)))
		}
		for _, wanted := range gap.indexes {
			out = append(out, translated(name, "add-index", indexStatement(name, wanted)))
		}
	}
	if search := declaredSearch(gap.table); len(search) > 0 && (gap.missing || gap.search) {
		written := make([]string, len(search))
		for i, column := range search {
			written[i] = gateway.WrittenName(column)
		}
		// the port's search index: kitwork_columns's weights, written after with the kinds
		out = append(out, Statement{Table: name, Action: "search-index",
			Postgres: "CREATE INDEX " + gateway.WrittenName(name+"_search") + " ON " + gateway.WrittenName(name) + " USING search (" + strings.Join(written, ", ") + ")"})
	}
	return out
}

// translated is a change as PostgreSQL writes it, with what the port's translator makes of it for the file.
func translated(table, action, postgres string) Statement {
	statement := Statement{Table: table, Action: action, Postgres: postgres}
	statement.SQLite, _ = gateway.TranslateSchema(postgres) // a failure is said when the plan applies (apply)
	return statement
}

// columnDefinition is a column as CREATE TABLE writes it, or ADD COLUMN (adding: its type and DEFAULT alone, as
// SQLite adds a column to rows already there). A struct's rules (.min(), .max(), choice(…), transform, .readonly())
// are the engine's, held on its own statements: the database is not bound to the struct (Quốc, 07/11/2026), and a
// CHECK of its own is written in SQL by whoever owns it.
func columnDefinition(entry *Entry, d declared, adding bool) string {
	parts := []string{gateway.WrittenName(d.name), d.postgres}
	f := d.field
	if !adding {
		if f.Key {
			parts = append(parts, "PRIMARY KEY")
		}
		if !f.Nullable || f.Key {
			parts = append(parts, "NOT NULL")
		}
		if f.Unique && !f.Key {
			parts = append(parts, "UNIQUE")
		}
	}
	if f.HasDefault {
		parts = append(parts, "DEFAULT "+literal(f.Default))
	}
	if !adding && f.ForeignTarget != nil {
		for _, target := range entry.Tables {
			if target.Struct == f.ForeignTarget {
				parts = append(parts, "REFERENCES "+gateway.WrittenName(target.Name)+" ("+gateway.WrittenName(f.ForeignField)+")")
			}
		}
	}
	return strings.Join(parts, " ")
}

// literal is a .default(…) as PostgreSQL writes it.
func literal(item value.Value) string {
	switch {
	case item.IsNull():
		return "NULL"
	case item.IsBoolean():
		return strconv.FormatBool(item.Boolean())
	case item.IsNumber():
		return strconv.FormatFloat(item.Number(), 'f', -1, 64)
	}
	return "'" + strings.ReplaceAll(item.Text(), "'", "''") + "'"
}

// indexStatement is an index's CREATE INDEX as PostgreSQL writes it.
func indexStatement(table string, wanted index) string {
	columns := make([]string, len(wanted.columns))
	for i, column := range wanted.columns {
		columns[i] = gateway.WrittenName(column)
	}
	text := "CREATE INDEX "
	if wanted.unique {
		text = "CREATE UNIQUE INDEX "
	}
	return text + gateway.WrittenName(wanted.name) + " ON " + gateway.WrittenName(table) + " (" + strings.Join(columns, ", ") + ")"
}

// ErrHeld is a file another process writes: a server running it.
var ErrHeld = errors.New("server đang giữ file")

// Apply makes the file what the plan says, on KitDB's writer: the gaps read again in its transaction, so what
// another hand changed since the plan was read is not added twice; the statements, kitwork migrate's ledger
// (_kitwork_migrations), then each declared column's kind and search weight in kitwork_columns, the columns that
// differ aside. One Barrier in the history; the writer writes the structure file.
func (p *Plan) Apply(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return err
	}
	database, err := kitdb.Open(ctx, p.Path, kitdb.Options{Structure: structureWriter(p.Path)})
	if errors.Is(err, kitdb.ErrLocked) && p.send != nil {
		return p.applyThroughPort(ctx) // the server's writer changes it, as it would for TablePlus
	}
	if errors.Is(err, kitdb.ErrLocked) {
		return fmt.Errorf("%s: %w; dừng server rồi chạy lại kitwork migrate (application.work không có cổng database nào phục vụ file này)", p.Database, ErrHeld)
	}
	if err != nil {
		return err
	}
	defer database.Close()
	_, err = database.Maintain(ctx, by, func(conn *sql.Conn) error {
		transaction, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := transaction.Exec(ledger); err != nil {
			_ = transaction.Rollback()
			return err
		}
		// what it was told first, then what the file lacks as they left it, read again in the transaction
		if err := applyChoices(transaction, &p.choices, p.gaps); err != nil {
			_ = transaction.Rollback()
			return err
		}
		if err := apply(transaction, p.entry); err != nil {
			_ = transaction.Rollback()
			return err
		}
		return transaction.Commit()
	})
	return err
}

const (
	ledger        = `CREATE TABLE IF NOT EXISTS "_kitwork_migrations" (id TEXT PRIMARY KEY, table_name TEXT, action TEXT, statement TEXT, applied_at TEXT)`
	columnsTable  = `CREATE TABLE IF NOT EXISTS kitwork_columns (table_name TEXT NOT NULL, column_name TEXT NOT NULL, kind TEXT NOT NULL, precision INTEGER NOT NULL, scale INTEGER NOT NULL, search_weight INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (table_name, column_name))`
	columnsSearch = `ALTER TABLE kitwork_columns ADD COLUMN search_weight INTEGER NOT NULL DEFAULT 0`
)

// apply runs one entry's gaps on transaction.
func apply(transaction *sql.Tx, entry *Entry) error {
	gaps, err := compareEntry(transaction, entry, nil)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(ledger); err != nil {
		return err
	}
	for _, gap := range gaps {
		for _, statement := range statementsOf(entry, gap) {
			if statement.Action != "search-index" {
				texts, err := gateway.TranslateSchema(statement.Postgres)
				if err != nil {
					return fmt.Errorf("%s: %w", statement.Postgres, err)
				}
				for _, text := range texts {
					if _, err := transaction.Exec(text); err != nil {
						return fmt.Errorf("%s: %w", statement.Postgres, err)
					}
				}
			}
			if _, err := transaction.Exec(`INSERT INTO "_kitwork_migrations" (id, table_name, action, statement, applied_at) VALUES (?, ?, ?, ?, ?)`,
				ledgerID(), statement.Table, statement.Action, statement.Shown(), time.Now().UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if err := writeKinds(transaction, gap); err != nil {
			return fmt.Errorf("kitwork_columns của bảng %s: %w", gap.table.Name, err)
		}
	}
	return nil
}

// writeKinds writes each declared column's row of kitwork_columns (its kind, a decimal's precision and scale, a
// .searchable() column's weight), the columns that differ from the file aside; a row of a column the table no
// longer has goes.
func writeKinds(transaction *sql.Tx, gap *tableGap) error {
	if _, err := transaction.Exec(columnsTable); err != nil {
		return err
	}
	if _, err := transaction.Exec(columnsSearch); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	if _, err := transaction.Exec(`DELETE FROM kitwork_columns WHERE table_name = ? AND column_name NOT IN (SELECT name FROM pragma_table_xinfo(table_name))`, gap.table.Name); err != nil {
		return err
	}
	for _, d := range gap.fields {
		if gap.differs[d.name] {
			continue
		}
		weight := 0
		if d.field.Searchable {
			weight = 1
		}
		if _, err := transaction.Exec(`INSERT OR REPLACE INTO kitwork_columns (table_name, column_name, kind, precision, scale, search_weight) VALUES (?, ?, ?, ?, ?, ?)`,
			gap.table.Name, d.name, d.kind, d.precision, d.scale, weight); err != nil {
			return err
		}
	}
	return nil
}

func ledgerID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// loadStructure makes, in a new file database writes, the tables and indexes its structure file says, and the kinds
// of their columns (writers.go), as the old engine's writer does.
func loadStructure(database *kitdb.Database, structure string) error {
	return gateway.LoadStructure(context.Background(), database, "kitwork", structure)
}

// structureWriter writes the database's structure file beside it, path + ".sql", whenever the writer gives it the
// file's structure, only when it changed (git sees no new bytes, no new time), as the old engine's writer does.
func structureWriter(path string) func(context.Context, *sql.Conn) {
	return func(ctx context.Context, conn *sql.Conn) {
		structure, err := gateway.Structure(ctx, conn)
		if err != nil || structure == "" {
			return
		}
		target := path + ".sql"
		if previous, err := os.ReadFile(target); err == nil && string(previous) == structure {
			return
		}
		temporary := target + ".tmp"
		if err := os.WriteFile(temporary, []byte(structure), 0o644); err != nil {
			return
		}
		if err := os.Rename(temporary, target); err != nil {
			_ = os.Remove(temporary)
		}
	}
}
