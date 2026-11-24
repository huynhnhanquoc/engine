package host_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const renameSchema = `import { struct, id, string, decimal, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const items = struct({ id: id(), name: string(), price: decimal(2) });

export const db = connection().tables({ items });
`

// renameFolder is an app whose database kitwork migrate made from schema, with rows written in it.
func renameFolder(t *testing.T, schema string, rows ...string) string {
	t.Helper()
	folder := migrateFolder(t, schema)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("the first run: %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, row := range rows {
		if _, err := db.Exec(row); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// query is one column of every row a statement reads of the file, as text.
func query(t *testing.T, folder, text string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(text)
	if err != nil {
		return []string{"error: " + err.Error()}
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		out = append(out, value.String)
	}
	return out
}

// A column a struct names that the file lacks, while the table has a column of its storage no struct declares, is
// asked (docs/KITWORK_DATABASE.md mục 6): renamed, its values kept, its row of kitwork_columns along; added keeping the
// old as the database's own; added dropping the old. The plan shows the statements that run as the answer leaves the
// file: a rename adds no column.
//
// CONTROL (07/11/2026): with choiceStatements renaming nothing, the answer 1 left name and no title; with the
// view not reading the file as the choices leave it (viewColumns), the plan after the answer 1 still showed ADD COLUMN
// title.
func TestMigrateAsksWhetherAColumnWasRenamed(t *testing.T) {
	renamed := strings.Replace(renameSchema, "name: string()", "title: string()", 1)
	for _, c := range []struct {
		answer                string
		statement, notShown   string
		columns, titles, kept string
	}{
		{"1", "ALTER TABLE items RENAME COLUMN name TO title;", "ADD COLUMN title", "id,title,price", "a,b", "title"},
		{"2", "ALTER TABLE items ADD COLUMN title text;", "RENAME COLUMN", "id,name,price,title", ",", "name,title"},
		{"3", "ALTER TABLE items DROP COLUMN name;", "RENAME COLUMN", "id,price,title", ",", "title"},
	} {
		t.Run("answer "+c.answer, func(t *testing.T) {
			folder := renameFolder(t, renameSchema, `INSERT INTO items (name, price) VALUES ('a', 1250), ('b', 99)`)
			writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": renamed})
			code, said := runMigrate(t, folder, false, c.answer+"\ny\n")
			for _, want := range []string{
				"bảng items, cột title string() mới; database có cột name cùng kiểu mà struct không khai",
				"1) đổi tên name → title (giữ 2 giá trị)", "2) thêm cột title mới, giữ name (cột riêng của database)",
				"3) thêm cột title mới, bỏ name (mất 2 giá trị)", c.statement, "kitwork migrate: .database/application/kitwork.db xong",
			} {
				if !strings.Contains(said, want) {
					t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
				}
			}
			if strings.Contains(said, c.notShown) || code != 0 {
				t.Errorf("exit %d, the plan shows %q:\n%s", code, c.notShown, said)
			}
			if got := strings.Join(query(t, folder, `SELECT name FROM pragma_table_info('items')`), ","); got != c.columns {
				t.Errorf("columns: %s, want %s", got, c.columns)
			}
			if got := strings.Join(query(t, folder, `SELECT title FROM items ORDER BY id`), ","); got != c.titles {
				t.Errorf("titles: %s, want %s", got, c.titles)
			}
			if got := strings.Join(query(t, folder, `SELECT column_name FROM kitwork_columns WHERE table_name = 'items' AND kind = 'text' ORDER BY column_name`), ","); got != c.kept {
				t.Errorf("kitwork_columns' text columns: %s, want %s", got, c.kept)
			}
		})
	}
}

// A table a struct names that the file lacks, while the file has one no struct declares holding all its columns, is
// asked: renamed, its rows and the rows of kitwork_columns along, or made new beside it.
//
// CONTROL (07/11/2026): with applyChoices not moving kitwork_columns, its rows stayed under posts, a table no more,
// and a second run did not find the file as the struct says.
func TestMigrateAsksWhetherATableWasRenamed(t *testing.T) {
	posts := `import { struct, id, string, decimal } from "kitwork/schema";
import { connection } from "kitwork/database";

export const posts = struct({ id: id(), title: string(), price: decimal(2) });

export const db = connection().tables({ posts });
`
	folder := renameFolder(t, posts, `INSERT INTO posts (title, price) VALUES ('một', 100), ('hai', 250)`)
	writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": strings.ReplaceAll(posts, "posts", "articles")})
	code, said := runMigrate(t, folder, false, "1\ny\n")
	for _, want := range []string{
		"bảng articles mới; database có bảng posts mà struct không khai, có đủ các cột của nó",
		"1) đổi tên bảng posts → articles (giữ 2 dòng)", "2) tạo bảng articles mới (posts ở lại, là bảng riêng của database)",
		"ALTER TABLE posts RENAME TO articles;",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
		}
	}
	if code != 0 || strings.Contains(said, "CREATE TABLE") {
		t.Errorf("exit %d:\n%s", code, said)
	}
	if got := strings.Join(query(t, folder, `SELECT title FROM articles ORDER BY id`), ","); got != "một,hai" {
		t.Errorf("articles: %s", got)
	}
	if got := strings.Join(query(t, folder, `SELECT DISTINCT table_name FROM kitwork_columns ORDER BY 1`), ","); got != "articles" {
		t.Errorf("kitwork_columns' tables: %s, want articles", got)
	}
	if code, said := runMigrate(t, folder, true, ""); code != 0 || !strings.Contains(said, "có mọi thứ code khai") {
		t.Errorf("a second run: exit %d\n%s", code, said)
	}
}

// A number column whose declared scale differs from the file's is asked: its values rewritten to the new scale
// (multiplied; divided rounding half away from zero, as PostgreSQL's numeric rounds), its kind written; or not, and
// it stays as the database keeps it, said. A rewrite that would carry a value past the precision changes nothing.
//
// CONTROL (07/11/2026): with applyChoices not running a rescale's guard, the decimal(14) rewrite wrote 1250 × 10^12.
func TestMigrateAsksWhetherANumbersValuesAreRewritten(t *testing.T) {
	rows := `INSERT INTO items (name, price) VALUES ('a', 1250), ('b', 1249), ('c', -1250), ('d', 5)`
	for _, c := range []struct {
		label, declared, answer string
		code                    int
		says                    []string
		values, kind            string
	}{
		{"up", "decimal(4)", "1", 0, []string{"cột items.price, database ghi decimal(2), struct khai decimal(4)", "1) viết lại 4 giá trị của items.price: nhân 100, giữ nguyên giá trị",
			"ALTER TABLE items ALTER COLUMN price TYPE numeric(15, 4);"}, "125000,124900,-125000,500", "decimal 4"},
		{"down to an integer", "integer()", "1", 0, []string{"cột items.price, database ghi decimal(2), struct khai integer()", "1) viết lại 4 giá trị của items.price: chia 100, làm tròn (mất số lẻ)",
			"ALTER TABLE items ALTER COLUMN price TYPE bigint;"}, "13,12,-13,0", "integer 0"},
		{"not", "decimal(3)", "2", 0, []string{"khác database, kitwork migrate không đổi (sửa struct theo database):", "cột items.price: database ghi decimal(2) (kitwork_columns), code khai decimal(3)"},
			"1250,1249,-1250,5", "decimal 2"},
		{"past the precision", "decimal(14)", "1", 1, []string{"3 giá trị vượt độ chính xác của số lẻ mới; không đổi gì"}, "1250,1249,-1250,5", "decimal 2"},
	} {
		t.Run(c.label, func(t *testing.T) {
			folder := renameFolder(t, renameSchema, rows)
			writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": strings.Replace(renameSchema, "price: decimal(2)", "price: "+c.declared, 1)})
			code, said := runMigrate(t, folder, false, c.answer+"\ny\n")
			for _, want := range c.says {
				if !strings.Contains(said, want) {
					t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
				}
			}
			if code != c.code {
				t.Errorf("exit %d, want %d:\n%s", code, c.code, said)
			}
			if got := strings.Join(query(t, folder, `SELECT price FROM items ORDER BY id`), ","); got != c.values {
				t.Errorf("values: %s, want %s", got, c.values)
			}
			if got := strings.Join(query(t, folder, `SELECT kind || ' ' || scale FROM kitwork_columns WHERE table_name = 'items' AND column_name = 'price'`), ","); got != c.kind {
				t.Errorf("kind: %s, want %s", got, c.kind)
			}
		})
	}
}

// With --yes nothing is answered for the runner: a database with a question is left as it is, said, and the exit is 1.
func TestMigrateWithYesLeavesADatabaseWithAQuestion(t *testing.T) {
	folder := renameFolder(t, renameSchema, `INSERT INTO items (name, price) VALUES ('a', 1)`)
	writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": strings.Replace(renameSchema, "name: string()", "title: string()", 1)})
	code, said := runMigrate(t, folder, true, "")
	if code != 1 || !strings.Contains(said, "cần trả lời (chạy kitwork migrate không --yes): .database/application/kitwork.db: bảng items, cột title string() mới") {
		t.Errorf("exit %d:\n%s", code, said)
	}
	if got := strings.Join(query(t, folder, `SELECT name FROM pragma_table_info('items')`), ","); got != "id,name,price" {
		t.Errorf("columns: %s", got)
	}
}
