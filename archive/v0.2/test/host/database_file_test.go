package host_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// fileSchema is a declaration the tests of the database file hold against a file they write.
const fileSchema = `import { struct, id, string, decimal, boolean, timestamp } from "kitwork/schema";
import { connection } from "kitwork/database";

export const users = struct({
  id: id(),
  email: string().unique(),
  name: string().index(),
  price: decimal(2),
  active: boolean(),
  createdAt: timestamp().created()
});

export const db = connection().tables({ users });
`

// writeFile makes the app's database file with statements, as the old engine or kitwork migrate wrote it.
func writeFile(t *testing.T, folder string, statements ...string) {
	t.Helper()
	path := filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

const kindsTable = `CREATE TABLE kitwork_columns (table_name TEXT NOT NULL, column_name TEXT NOT NULL, kind TEXT NOT NULL, precision INTEGER NOT NULL, scale INTEGER NOT NULL, search_weight INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (table_name, column_name))`

func fileApp(t *testing.T, statements ...string) *booted {
	t.Helper()
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": fileSchema,
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
	})
	if len(statements) > 0 {
		writeFile(t, folder, statements...)
	}
	return boot(t, filepath.Join(folder, "application.work"))
}

// As the app boots, each database it declares is read, read only, and held against its structs (docs/schema.md
// mục 2, the database is the root): a file that holds the declaration says nothing; one that is not there says
// kitwork migrate makes it; one that lacks or differs says each thing, what the file has and no struct declares
// staying the database's own. The app boots either way.
//
// CONTROL (06/11/2026): with checkTableFile not comparing storage, "cột users.email: database lưu INTEGER" was not
// said; with uniqueHeld always true, the unique constraint of email was not said.
func TestTheBootHoldsTheStructsAgainstTheFile(t *testing.T) {
	if got := fileApp(t).log.String(); !strings.Contains(got, "[database] .database/application/kitwork.db chưa có: kitwork migrate tạo nó") {
		t.Errorf("no file: the boot said\n%s", got)
	}

	held := fileApp(t,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, name TEXT NOT NULL, price INTEGER NOT NULL, active INTEGER NOT NULL, createdAt TEXT NOT NULL, legacy TEXT) STRICT`,
		`CREATE UNIQUE INDEX users_email_unique ON users (email)`,
		`CREATE INDEX users_name ON users (name)`,
		kindsTable,
		`INSERT INTO kitwork_columns VALUES ('users','id','integer',0,0,0), ('users','email','text',0,0,0), ('users','name','text',0,0,0),
		 ('users','price','decimal',15,2,0), ('users','active','bool',0,0,0), ('users','createdAt','datetime',0,0,0)`)
	if got := held.log.String(); strings.Contains(got, "[database]") {
		t.Errorf("a file holding the declaration: the boot said\n%s", got)
	}

	lacking := fileApp(t,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email INTEGER NOT NULL, price INTEGER NOT NULL, active INTEGER NOT NULL, createdAt TEXT NOT NULL, legacy TEXT) STRICT`,
		kindsTable,
		`INSERT INTO kitwork_columns VALUES ('users','id','integer',0,0,0), ('users','price','decimal',15,3,0), ('users','active','bool',0,0,0), ('users','createdAt','datetime',0,0,0)`)
	got := lacking.log.String()
	for _, want := range []string{
		"[database] .database/application/kitwork.db chưa có: cột name string() của bảng users",
		"cột users.email: database lưu INTEGER, code khai string() (lưu TEXT)",
		"cột users.price: database ghi decimal(3) (kitwork_columns), code khai decimal(2)",
		"câu lệnh trên database này lỗi tới khi kitwork migrate thêm",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a file that lacks: the boot said\n%s\nwithout %q", got, want)
		}
	}
	// a column no struct declares is the database's own: never said to differ, only named as what name may have been
	// renamed from, which kitwork migrate asks (choices.go)
	if strings.Contains(got, "users.legacy") || !strings.Contains(got, "có phải đổi tên: name từ legacy? kitwork migrate hỏi") {
		t.Errorf("a column no struct declares: %s", got)
	}

	noUnique := fileApp(t,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, name TEXT NOT NULL, price INTEGER NOT NULL, active INTEGER NOT NULL, createdAt TEXT NOT NULL) STRICT`)
	got = noUnique.log.String()
	for _, want := range []string{
		"chưa có: ràng buộc unique (email) của bảng users",
		"câu lệnh vẫn chạy; kitwork migrate sẽ ghi thêm chỉ mục (name) của bảng users; kiểu của cột users.id, users.email",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a file without the unique constraint nor kitwork_columns: the boot said\n%s\nwithout %q", got, want)
		}
	}
}
