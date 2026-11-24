package host_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2"
)

// migrateSchema declares what kitwork migrate makes: every kind, a key of each way, a foreign key, a unique group, an
// index, a search index, a default.
const migrateSchema = `import { struct, id, string, integer, decimal, boolean, date, timestamp, json, foreign } from "kitwork/schema";
import { connection } from "kitwork/database";

export const users = struct({
  id: id(),
  email: string().unique(),
  name: string().searchable(),
  active: boolean().default(true),
  createdAt: timestamp().created()
});

export const orders = struct({
  id: id("uuid"),
  userId: foreign(users.id),
  day: date().unique("userDay"),
  amount: decimal(2).index(),
  meta: json().nullable(),
  linkId: integer().unique("userDay")
});

export const db = connection().tables({ users, orders });
`

func migrateFolder(t *testing.T, schema string) string {
	t.Helper()
	return write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": schema,
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
	})
}

func runMigrate(t *testing.T, folder string, yes bool, answer string) (int, string) {
	t.Helper()
	var output bytes.Buffer
	code := engine2.Migrate(filepath.Join(folder, "application.work"), yes, strings.NewReader(answer), &output)
	return code, output.String()
}

func databaseFile(folder string) string {
	return filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db")
}

// kitwork migrate on engine2 makes the file the structs declare (docs/schema.md mục 8, slice 2): it says what it adds
// and the statements, PostgreSQL's, asks, then makes the file through KitDB's writer: STRICT tables, the unique group
// and the index, each column's kind and the search weight in kitwork_columns, its ledger, and the structure file
// beside it. A boot then says nothing of the file, and a second run has nothing to do.
//
// CONTROL (06/11/2026): with writeKinds not writing kitwork_columns, the file had none and the boot after it said
// "khác database: cột orders.amount: database ghi số nguyên"; with statementsOf leaving out the unique groups, it said
// "chưa có: ràng buộc unique (day, linkId)".
func TestMigrateMakesTheFileTheStructsDeclare(t *testing.T) {
	folder := migrateFolder(t, migrateSchema)

	code, said := runMigrate(t, folder, false, "n\n")
	for _, want := range []string{
		"kitwork migrate: .database/application/kitwork.db (app shop)",
		"database chưa có, kitwork migrate thêm:",
		"bảng users (id id(), email string(), name string(), active boolean(), createdAt timestamp())",
		"CREATE TABLE users (\n      id bigserial PRIMARY KEY NOT NULL,\n      email text NOT NULL UNIQUE,",
		"active boolean NOT NULL DEFAULT true",
		`"userId" bigint NOT NULL REFERENCES users (id)`,
		"amount numeric(15, 2) NOT NULL",
		`CREATE UNIQUE INDEX "orders_userDay" ON orders (day, "linkId");`,
		"CREATE INDEX orders_amount ON orders (amount);",
		"CREATE INDEX users_search ON users USING search (name);",
		"Thêm vào database? [y/N] kitwork migrate: không đổi gì",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
		}
	}
	if _, err := os.Stat(databaseFile(folder)); code != 0 || !os.IsNotExist(err) {
		t.Fatalf("answered n: exit %d, the file %v", code, err)
	}

	if code, said = runMigrate(t, folder, true, ""); code != 0 || !strings.Contains(said, "kitwork migrate: .database/application/kitwork.db xong") {
		t.Fatalf("--yes: exit %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name = 'users'`).Scan(&definition); err != nil || !strings.Contains(definition, `"id" INTEGER PRIMARY KEY`) || !strings.HasSuffix(definition, "STRICT") {
		t.Errorf("users: %q %v", definition, err)
	}
	var kind string
	var precision, scale, weight int
	if err := db.QueryRow(`SELECT kind, precision, scale FROM kitwork_columns WHERE table_name = 'orders' AND column_name = 'amount'`).Scan(&kind, &precision, &scale); err != nil || kind != "decimal" || precision != 15 || scale != 2 {
		t.Errorf("orders.amount in kitwork_columns: %s %d %d %v", kind, precision, scale, err)
	}
	if err := db.QueryRow(`SELECT search_weight FROM kitwork_columns WHERE table_name = 'users' AND column_name = 'name'`).Scan(&weight); err != nil || weight != 1 {
		t.Errorf("users.name's search weight: %d %v", weight, err)
	}
	var ledger int
	if err := db.QueryRow(`SELECT count(*) FROM "_kitwork_migrations"`).Scan(&ledger); err != nil || ledger < 5 {
		t.Errorf("the ledger: %d %v", ledger, err)
	}
	if structure, err := os.ReadFile(databaseFile(folder) + ".sql"); err != nil || !strings.Contains(string(structure), "CREATE TABLE users") {
		t.Errorf("the structure file: %v\n%s", err, structure)
	}
	db.Close()

	if got := boot(t, filepath.Join(folder, "application.work")).log.String(); strings.Contains(got, "[database]") {
		t.Errorf("after kitwork migrate, the boot said\n%s", got)
	}
	if code, said = runMigrate(t, folder, true, ""); code != 0 || !strings.Contains(said, "có mọi thứ code khai") || !strings.Contains(said, "không đổi gì") {
		t.Errorf("a second run: exit %d\n%s", code, said)
	}
}

// What a struct adds to a file that has rows is added and the rows stay: a column (its type and default alone, as
// SQLite adds a column), a unique constraint as an index; a column the file has and no struct declares stays. What
// differs is said and left. A column of the file of a lacking one's storage, and a decimal's new scale, are asked
// (choices.go): answered "add the new one, keep the old" and "not", the file keeps both.
func TestMigrateAddsAndLeavesWhatTheFileHas(t *testing.T) {
	folder := migrateFolder(t, migrateSchema)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("the first run: %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (email, name, active, createdAt) VALUES ('an@x.vn', 'An', 1, '2026-11-06T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN legacy TEXT`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	grown := strings.Replace(migrateSchema, "  name: string().searchable(),", "  name: string().searchable(),\n  phone: string().nullable().unique(),\n  plan: string().default(\"free\"),", 1)
	grown = strings.Replace(grown, "amount: decimal(2).index()", "amount: decimal(3).index()", 1)
	writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": grown})
	code, said := runMigrate(t, folder, false, "2\n2\n2\ny\n")
	for _, want := range []string{
		"bảng users, cột phone string() mới; database có cột legacy cùng kiểu mà struct không khai",
		"1) đổi tên legacy → phone (giữ 0 giá trị)",
		"2) thêm cột phone mới, giữ legacy (cột riêng của database)",
		"3) thêm cột phone mới, bỏ legacy (mất 0 giá trị)",
		"bảng users, cột plan string() mới",
		"cột orders.amount, database ghi decimal(2), struct khai decimal(3)",
		"1) viết lại 0 giá trị của orders.amount: nhân 10, giữ nguyên giá trị",
		"cột phone string(), plan string() của bảng users",
		"ALTER TABLE users ADD COLUMN phone text;",
		"ALTER TABLE users ADD COLUMN plan text DEFAULT 'free';",
		`CREATE UNIQUE INDEX users_phone_unique ON users (phone);`,
		"khác database, kitwork migrate không đổi (sửa struct theo database):",
		"cột orders.amount: database ghi decimal(2) (kitwork_columns), code khai decimal(3)",
		"kitwork migrate: .database/application/kitwork.db xong",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
		}
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	db, err = sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var email, plan string
	var legacy sql.NullString
	if err := db.QueryRow(`SELECT email, plan, legacy FROM users`).Scan(&email, &plan, &legacy); err != nil || email != "an@x.vn" || plan != "free" {
		t.Errorf("the row after: %q %q %v", email, plan, err)
	}
	var amount string
	if err := db.QueryRow(`SELECT kind || ' ' || scale FROM kitwork_columns WHERE table_name = 'orders' AND column_name = 'amount'`).Scan(&amount); err != nil || amount != "decimal 2" {
		t.Errorf("a column that differs keeps its row of kitwork_columns: %q %v", amount, err)
	}
}

// A file a server writes is not changed under it: engine2 has no port to go through yet, so kitwork migrate says to
// stop the server.
func TestMigrateLeavesAFileAServerHolds(t *testing.T) {
	folder := migrateFolder(t, migrateSchema)
	if err := os.MkdirAll(filepath.Dir(databaseFile(folder)), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := kitdb.Open(context.Background(), databaseFile(folder), kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	code, said := runMigrate(t, folder, true, "")
	if code != 1 || !strings.Contains(said, "server đang giữ file; dừng server rồi chạy lại kitwork migrate") {
		t.Errorf("a held file: exit %d\n%s", code, said)
	}
}

// writeFiles writes files into an existing folder of a test.
func writeFiles(t *testing.T, folder string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(folder, filepath.FromSlash(name)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
