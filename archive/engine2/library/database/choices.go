package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/huynhnhanquoc/sqlitewire/gateway"
)

// What kitwork migrate asks (docs/KITWORK_DATABASE.md mục 6, "Database là gốc"; the old engine's, 06/11/2026, as
// drizzle-kit asks): a declaration does not say whether a column or a table it names is new or renamed, nor whether
// a number's new scale rewrites the values the file holds, so kitwork migrate asks, numbered:
//   - a column the file lacks while its table has one of the same storage (and scale, for a number) no struct
//     declares: rename it (its values kept), add the new one keeping the old as the database's own, or, with one such
//     column in no index, add the new one and drop the old (its values gone). Dropping is only here: a column the code
//     no longer declares stays the database's own;
//   - a table the file lacks while it has one no struct declares holding every declared column: rename it (its rows
//     and kinds along) or make the new one;
//   - a number column (a decimal, an integer: both the value × 10^scale) whose declared scale differs from the
//     file's: rewrite its values to the declared scale, multiplied, or divided rounding half away from zero as
//     PostgreSQL's numeric rounds, nothing rewritten when a value would pass the precision; or not, the struct to
//     follow the database.
//
// The plan reads the file as the choices leave it, without writing (compareTable), so the statements it shows are
// those that run: the choices' first, then what it adds.

// choices are what kitwork migrate was told besides the declaration.
type choices struct {
	tables  map[string]string            // declared table → the file's table it renames
	columns map[string]map[string]string // declared table → declared column → the file's column it renames
	drops   map[string][]string          // declared table → the file's columns dropped
	rescale map[string]bool              // "table.column" → its values rewritten to the declared scale
	decided map[string]bool              // what was asked and answered
}

func newChoices() choices {
	return choices{tables: map[string]string{}, columns: map[string]map[string]string{}, drops: map[string][]string{},
		rescale: map[string]bool{}, decided: map[string]bool{}}
}

// sourceOf is the file's table a declared table is read from.
func (c *choices) sourceOf(table string) string {
	if c != nil {
		if file, renamed := c.tables[table]; renamed {
			return file
		}
	}
	return table
}

// renamesTable says a choice renames the file's table to a declared one.
func (c *choices) renamesTable(file string) bool {
	if c == nil {
		return false
	}
	for _, renamed := range c.tables {
		if renamed == file {
			return true
		}
	}
	return false
}

// renamed is the declared column the file's column takes, "" for none.
func (c *choices) renamed(table, file string) string {
	if c == nil {
		return ""
	}
	for declared, from := range c.columns[table] {
		if from == file {
			return declared
		}
	}
	return ""
}

func (c *choices) dropped(table, file string) bool {
	if c == nil {
		return false
	}
	for _, column := range c.drops[table] {
		if column == file {
			return true
		}
	}
	return false
}

func (c *choices) rescales(table, column string) bool { return c != nil && c.rescale[table+"."+column] }

// viewColumns are the file's columns of a declared table as the choices leave them.
func (c *choices) viewColumns(table string, columns map[string]liveColumn) map[string]liveColumn {
	if c == nil || len(columns) == 0 {
		return columns
	}
	out := make(map[string]liveColumn, len(columns))
	for name, column := range columns {
		switch {
		case c.dropped(table, name):
		case c.renamed(table, name) != "":
			out[c.renamed(table, name)] = column
		default:
			out[name] = column
		}
	}
	return out
}

// viewKinds are kitwork_columns's rows of a declared table as the choices leave them.
func (c *choices) viewKinds(table string, stored map[string]storedKind) map[string]storedKind {
	if c == nil || len(stored) == 0 {
		return stored
	}
	out := make(map[string]storedKind, len(stored))
	for name, row := range stored {
		switch {
		case c.dropped(table, name):
		case c.renamed(table, name) != "":
			out[c.renamed(table, name)] = row
		default:
			out[name] = row
		}
	}
	return out
}

// viewIndexes are a declared table's indexes as the choices leave them: a column renamed in each.
func (c *choices) viewIndexes(table string, live []liveIndex) []liveIndex {
	if c == nil || len(c.columns[table]) == 0 {
		return live
	}
	out := make([]liveIndex, len(live))
	for i, index := range live {
		out[i] = liveIndex{unique: index.unique, partial: index.partial, columns: make([]string, len(index.columns))}
		for j, column := range index.columns {
			if renamed := c.renamed(table, column); renamed != "" {
				column = renamed
			}
			out[i].columns[j] = column
		}
	}
	return out
}

// Question is what kitwork migrate asks before a change the declaration alone cannot tell.
type Question struct {
	Text    string
	Options []string
	answer  func(int)
}

// Answer takes option, 0 the first, and plans again with it.
func (q *Question) Answer(option int) error {
	if option < 0 || option >= len(q.Options) {
		return fmt.Errorf("chọn từ 1 tới %d", len(q.Options))
	}
	q.answer(option)
	return nil
}

// Question is the plan's next question; nil when none is left.
func (p *Plan) Question() *Question {
	if p.Absent {
		return nil
	}
	for _, gap := range p.gaps {
		name := gap.table.Name
		if !gap.missing || len(gap.tables) == 0 || p.choices.decided[name] {
			continue
		}
		candidates := gap.tables
		question := &Question{Text: fmt.Sprintf("%s: bảng %s mới; database có bảng %s mà struct không khai, có đủ các cột của nó",
			p.Database, name, strings.Join(candidates, ", "))}
		for _, candidate := range candidates {
			question.Options = append(question.Options, fmt.Sprintf("đổi tên bảng %s → %s (giữ %d dòng)", candidate, name, p.count(candidate, "*")))
		}
		question.Options = append(question.Options, fmt.Sprintf("tạo bảng %s mới (%s ở lại, là bảng riêng của database)", name, strings.Join(candidates, ", ")))
		question.answer = func(option int) {
			p.choose(func(c *choices) {
				if option < len(candidates) {
					c.tables[name] = candidates[option]
				}
				c.decided[name] = true
			})
		}
		return question
	}
	for _, gap := range p.gaps {
		name := gap.table.Name
		if gap.missing {
			continue
		}
		for _, d := range gap.absent {
			key := name + "." + d.name
			candidates := gap.candidates[d.name]
			if len(candidates) == 0 || p.choices.decided[key] {
				continue
			}
			question := &Question{Text: fmt.Sprintf("%s: bảng %s, cột %s %s mới; database có cột %s cùng kiểu mà struct không khai",
				p.Database, name, d.name, d.spelling, strings.Join(candidates, ", "))}
			for _, candidate := range candidates {
				question.Options = append(question.Options, fmt.Sprintf("đổi tên %s → %s (giữ %d giá trị)", candidate, d.name, p.count(gap.source, candidate)))
			}
			question.Options = append(question.Options, fmt.Sprintf("thêm cột %s mới, giữ %s (cột riêng của database)", d.name, strings.Join(candidates, ", ")))
			droppable := len(candidates) == 1 && !gap.indexed[candidates[0]]
			if droppable {
				question.Options = append(question.Options, fmt.Sprintf("thêm cột %s mới, bỏ %s (mất %d giá trị)", d.name, candidates[0], p.count(gap.source, candidates[0])))
			}
			column := d.name
			question.answer = func(option int) {
				p.choose(func(c *choices) {
					switch {
					case option < len(candidates):
						if c.columns[name] == nil {
							c.columns[name] = map[string]string{}
						}
						c.columns[name][column] = candidates[option]
					case droppable && option == len(candidates)+1:
						c.drops[name] = append(c.drops[name], candidates[0])
					}
					c.decided[key] = true
				})
			}
			return question
		}
		for _, found := range gap.rescalable {
			key := name + "." + found.column
			if p.choices.decided["~"+key] {
				continue
			}
			values := p.count(gap.source, found.column)
			rewrite := fmt.Sprintf("viết lại %d giá trị của %s: nhân %d, giữ nguyên giá trị", values, key, gateway.RescalePower(found.from, found.to))
			if found.to < found.from {
				rewrite = fmt.Sprintf("viết lại %d giá trị của %s: chia %d, làm tròn (mất số lẻ)", values, key, gateway.RescalePower(found.to, found.from))
			}
			question := &Question{Text: fmt.Sprintf("%s: cột %s, database ghi %s, struct khai %s", p.Database, key, found.file, found.declaredType),
				Options: []string{rewrite, "không: sửa struct theo database"}}
			question.answer = func(option int) {
				p.choose(func(c *choices) {
					if option == 0 {
						c.rescale[key] = true
					}
					c.decided["~"+key] = true
				})
			}
			return question
		}
	}
	return nil
}

// choose changes the choices and plans again, reading the file as they leave it.
func (p *Plan) choose(change func(*choices)) {
	change(&p.choices)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p.Path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		p.Differ = append(p.Differ, "không mở được file: "+err.Error())
		return
	}
	defer db.Close()
	gaps, err := compareEntry(db, p.entry, &p.choices)
	if err != nil {
		p.Differ = append(p.Differ, "không đọc được file: "+err.Error())
		return
	}
	p.fill(gaps)
}

// count is how many rows a table of the file has ("*"), or values a column holds (not null), for a question's words.
func (p *Plan) count(table, column string) int {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p.Path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return 0
	}
	defer db.Close()
	counted := "*"
	if column != "*" {
		counted = quote(column)
	}
	var n int
	_ = db.QueryRow("SELECT count(" + counted + ") FROM " + quote(table)).Scan(&n)
	return n
}

// choiceStatements are the changes kitwork migrate was told to make, before what it adds: a table renamed, then each
// table's columns renamed and dropped, then a number column's values rewritten to its new scale.
func choiceStatements(c *choices, gaps []*tableGap) []Statement {
	var out []Statement
	for _, gap := range gaps {
		name := gap.table.Name
		if file, renamed := c.tables[name]; renamed {
			out = append(out, Statement{Table: name, Action: "rename-table",
				Postgres: "ALTER TABLE " + gateway.WrittenName(file) + " RENAME TO " + gateway.WrittenName(name),
				SQLite:   []string{"ALTER TABLE " + quote(file) + " RENAME TO " + quote(name)}, from: file})
		}
	}
	for _, gap := range gaps {
		name := gap.table.Name
		renames := c.columns[name]
		declared := make([]string, 0, len(renames))
		for column := range renames {
			declared = append(declared, column)
		}
		sort.Strings(declared)
		for _, column := range declared {
			out = append(out, Statement{Table: name, Action: "rename-column",
				Postgres: "ALTER TABLE " + gateway.WrittenName(name) + " RENAME COLUMN " + gateway.WrittenName(renames[column]) + " TO " + gateway.WrittenName(column),
				SQLite:   []string{"ALTER TABLE " + quote(name) + " RENAME COLUMN " + quote(renames[column]) + " TO " + quote(column)}, from: renames[column], to: column})
		}
		for _, column := range c.drops[name] {
			out = append(out, Statement{Table: name, Action: "drop-column",
				Postgres: "ALTER TABLE " + gateway.WrittenName(name) + " DROP COLUMN " + gateway.WrittenName(column),
				SQLite:   []string{"ALTER TABLE " + quote(name) + " DROP COLUMN " + quote(column)}, from: column})
		}
		for _, found := range gap.rescaled {
			kind := "bigint"
			if found.decimal {
				kind = "numeric(" + strconv.Itoa(found.precision) + ", " + strconv.Itoa(found.to) + ")"
			}
			rewrite := gateway.RescaleStatements(name, found.column, found.precision, found.from, found.to)
			out = append(out, Statement{Table: name, Action: "rescale",
				Postgres: "ALTER TABLE " + gateway.WrittenName(name) + " ALTER COLUMN " + gateway.WrittenName(found.column) + " TYPE " + kind,
				SQLite:   []string{rewrite.Update}, guard: rewrite.Guard, from: found.column, rescaled: &found})
		}
	}
	return out
}

// applyChoices runs the choices' statements on transaction, before what the plan adds: kitwork_columns follows a
// table or a column renamed, a rescale whose values would pass the precision stops the plan.
func applyChoices(transaction *sql.Tx, c *choices, gaps []*tableGap) error {
	statements := choiceStatements(c, gaps)
	if len(statements) == 0 {
		return nil
	}
	var kinds int
	_ = transaction.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'kitwork_columns'`).Scan(&kinds)
	for _, statement := range statements {
		if statement.guard != "" {
			var past int
			if err := transaction.QueryRow(statement.guard).Scan(&past); err != nil {
				return fmt.Errorf("%s: %w", statement.Postgres, err)
			}
			if past > 0 {
				return fmt.Errorf("%s: %d giá trị vượt độ chính xác của số lẻ mới; không đổi gì", statement.Postgres, past)
			}
		}
		for _, text := range statement.SQLite {
			if _, err := transaction.Exec(text); err != nil {
				return fmt.Errorf("%s: %w", statement.Postgres, err)
			}
		}
		if kinds > 0 {
			var err error
			switch statement.Action {
			case "rename-table":
				_, err = transaction.Exec(`UPDATE kitwork_columns SET table_name = ? WHERE table_name = ?`, statement.Table, statement.from)
			case "rename-column":
				_, err = transaction.Exec(`UPDATE kitwork_columns SET column_name = ? WHERE table_name = ? AND column_name = ?`, statement.to, statement.Table, statement.from)
			case "drop-column":
				_, err = transaction.Exec(`DELETE FROM kitwork_columns WHERE table_name = ? AND column_name = ?`, statement.Table, statement.from)
			case "rescale":
				// the values are at the new scale: so is the kind (a column of no row was an integer, scale 0)
				kind, precision := "integer", 0
				if statement.rescaled.decimal {
					kind, precision = "decimal", statement.rescaled.precision
				}
				_, err = transaction.Exec(`INSERT INTO kitwork_columns (table_name, column_name, kind, precision, scale) VALUES (?, ?, ?, ?, ?)
					ON CONFLICT (table_name, column_name) DO UPDATE SET kind = excluded.kind, precision = excluded.precision, scale = excluded.scale`,
					statement.Table, statement.from, kind, precision, statement.rescaled.to)
			}
			if err != nil {
				return fmt.Errorf("kitwork_columns sau %s: %w", statement.Postgres, err)
			}
		}
		if _, err := transaction.Exec(`INSERT INTO "_kitwork_migrations" (id, table_name, action, statement, applied_at) VALUES (?, ?, ?, ?, ?)`,
			ledgerID(), statement.Table, statement.Action, statement.Shown(), time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}
