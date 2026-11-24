package host_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A file a server holds is changed through the server's database port when application.work declares one
// (docs/KITWORK_DATABASE.md, "Database là gốc", B2): kitwork migrate sends the plan's PostgreSQL statements as the
// port's admin, on the database the port names the file by (<domain>/application), as TablePlus would; the server's
// writer runs them, so its history keeps them and it writes kitwork.db.sql. A rename and a rescale it was told go
// too.
//
// CONTROL (07/11/2026): with Apply refusing a held file whatever the manifest says, the run said "server đang giữ
// file; dừng server rồi chạy lại kitwork migrate" and the file kept name.
func TestMigrateGoesThroughThePortOfAServerHoldingTheFile(t *testing.T) {
	port := freePort(t)
	manifest := strings.Replace(manifest, "app.run();", `app.database(".database", { port: `+strconv.Itoa(port)+`, user: "admin", password: "pw" });
app.run();`, 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": renameSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => context.json({ made: db.insert((items) => ({ name: "cà phê", price: 12.5 })) }));
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("the first run: %d\n%s", code, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	if _, err := app.host.DatabasePort(); err != nil {
		t.Fatal(err)
	}
	answer(t, app, "/") // the site's writer holds the file from its first statement

	grown := strings.Replace(renameSchema, "name: string(), price: decimal(2)",
		"title: string(), price: decimal(3), code: string().transform((value) => value.trim()), note: string().nullable()", 1)
	writeFiles(t, folder, map[string]string{"apps/shop/core/schema/shop.work": grown})
	code, said := runMigrate(t, folder, false, "1\n1\ny\n")
	for _, want := range []string{
		"1) đổi tên name → title (giữ 1 giá trị)",
		"1) viết lại 1 giá trị của items.price: nhân 10, giữ nguyên giá trị",
		"ALTER TABLE items RENAME COLUMN name TO title;",
		"ALTER TABLE items ALTER COLUMN price TYPE numeric(15, 3);",
		"ALTER TABLE items ADD COLUMN code text;",
		"kitwork migrate: .database/application/kitwork.db xong, qua cổng database 127.0.0.1:" + strconv.Itoa(port) + " (database shop.test/application)",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("kitwork migrate said:\n%s\nwithout %q", said, want)
		}
	}
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	if got := strings.Join(query(t, folder, `SELECT name FROM pragma_table_info('items')`), ","); got != "id,title,price,code,note" {
		t.Errorf("columns: %s", got)
	}
	if got := strings.Join(query(t, folder, `SELECT title || ' ' || price FROM items`), ","); got != "cà phê 12500" {
		t.Errorf("the row, renamed and rescaled by the server's writer: %s", got)
	}
	if got := strings.Join(query(t, folder, `SELECT column_name || ' ' || kind || ' ' || scale FROM kitwork_columns WHERE table_name = 'items' ORDER BY column_name`), ","); !strings.Contains(got, "price decimal 3") || !strings.Contains(got, "title text 0") {
		t.Errorf("kitwork_columns: %s", got)
	}
	structure, err := os.ReadFile(databaseFile(folder) + ".sql")
	if err != nil || !strings.Contains(string(structure), "title text") || !strings.Contains(string(structure), "code text") {
		t.Errorf("the server's writer wrote kitwork.db.sql again: %v\n%s", err, structure)
	}
}
