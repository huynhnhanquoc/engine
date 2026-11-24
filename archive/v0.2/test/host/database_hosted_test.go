package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hostedManifestLine = `app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.test", "kho.test"] }, crm: { postgresql: "postgres://sales:hush-hush@db.internal/crm", apps: "*" } });
app.run();`

// stockSchema declares the app's own notes and, on the host's warehouse, the stock it reads.
func stockSchema(item string) string {
	return `import { struct, id, string, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const notes = struct({ id: id(), text: string() });
export const stock = struct({ id: id(), ` + item + `: string(), qty: integer().min(0) });

export const db = connection().tables({ notes });
export const warehouse = connection("warehouse").tables({ stock });
`
}

// A name of the host's catalogue, app.databases({ warehouse: { sqlite: "./data/warehouse.db" } }), is a database an
// app declares tables on with connection("warehouse") (docs/KITWORK_DATABASE.md mục 1, lát 8): its file is the host's,
// read from the manifest's folder, and every app that names it shares it (one writer a file for the process); kitwork
// migrate makes it as it makes the app's own, the statements run on it as on the app's own, and a column the file has
// that one app does not declare is the database's (each app declares what it uses).
//
// CONTROL (07/11/2026): with place not finding a name in the catalogue, kitwork migrate stopped: connection("warehouse"):
// host không có database tên đó.
func TestAnAppDeclaresTablesOnAHostsDatabase(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                strings.Replace(manifest, "app.run();", hostedManifestLine, 1),
		"apps/shop/core/schema/shop.work": stockSchema("item"),
		"apps/shop/shop.test/router.work": routerImport + `import { db, warehouse } from "app/core/schema/shop.work";
router.get((context) => {
  db.insert((notes) => ({ text: "của shop" }));
  warehouse.insert((stock) => ({ item: "cà phê", qty: 3 }));
  return context.json({ stock: warehouse.list((stock) => stock), notes: db.count((notes) => notes) });
});
`,
		"apps/kho/core/schema/kho.work": `import { struct, id, string, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const stock = struct({ id: id(), qty: integer().min(0) });

export const warehouse = connection("warehouse").tables({ stock });
`,
		"apps/kho/kho.test/router.work": routerImport + `import { warehouse } from "app/core/schema/kho.work";
router.get((context) => context.json({ total: warehouse.sum((stock) => stock.qty) }));
`,
	})
	code, said := runMigrate(t, folder, true, "")
	if code != 0 || !strings.Contains(said, `connection("warehouse")`) {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	if _, err := os.Stat(filepath.Join(folder, "data", "warehouse.db")); err != nil {
		t.Fatalf("the host's file: %v\n%s", err, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	if log := app.log.String(); strings.Contains(log, "[database]") {
		t.Errorf("the boot said:\n%s", log)
	}
	read := answer(t, app, "/")
	expect(t, read, map[string]string{"stock": `[{"id": 1, "item": "cà phê", "qty": 3}]`, "notes": "1"})
	got := app.get(t, "kho.test", "/")
	if got.status != 200 || !strings.Contains(got.body, `"total":3`) {
		t.Errorf("the other app on the same name: %d %s", got.status, got.body)
	}
}

// A name the catalogue has not is a boot error naming those it has; a name on postgresql or mysql is not served (the
// tables and statements of connection() run on KitDB and SQLite); a schema of a name is not either. The manifest's
// catalogue takes one connector a name, keeps application, storage and files, and a failure never repeats a value
// written (a URL holds a password).
func TestAHostsDatabaseWrittenWrongStopsTheBoot(t *testing.T) {
	for _, c := range []struct{ declared, says string }{
		{`connection("nope").tables({ stock })`, `connection("nope"): host không có database tên đó; có: crm, warehouse`},
		{`connection("crm").tables({ stock })`, `connection("crm") là postgresql: bảng, câu lệnh và kitwork migrate của connection() hôm nay chạy trên KitDB và SQLite`},
		{`connection("warehouse").schema("billing").tables({ stock })`, "schema của một tên của host chưa có"},
	} {
		said := bootError(t, map[string]string{
			"application.work": strings.Replace(manifest, "app.run();", hostedManifestLine, 1),
			"apps/shop/core/schema/shop.work": `import { struct, id, integer } from "kitwork/schema";
import { connection } from "kitwork/database";
export const stock = struct({ id: id(), qty: integer() });
export const warehouse = ` + c.declared + `;
`,
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
		})
		if !strings.Contains(said, c.says) || strings.Contains(said, "hush-hush") {
			t.Errorf("%s: the boot says %q, want %q", c.declared, said, c.says)
		}
	}
	for _, c := range []struct{ line, says string }{
		{`app.databases({ application: { sqlite: "./a.db" } });`, "application is the app's own database"},
		{`app.databases({ storage: { sqlite: "./a.db" } });`, "storage is a name kitwork keeps"},
		{`app.databases({ w: { sqlite: "./a.db", postgresql: "postgres://u:hush-hush@h/d" } });`, "app.databases.w is one connector"},
		{`app.databases({ w: { oracle: "hush-hush" } });`, "app.databases.w: oracle is no connector"},
		{`app.databases({ w: { postgresql: 5 } });`, "app.databases.w.postgresql is a text"},
		{`app.databases({}); app.databases({});`, "app.databases(…) is declared twice"},
	} {
		said := bootError(t, map[string]string{
			"application.work":                strings.Replace(manifest, "app.run();", c.line+"\napp.run();", 1),
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
		})
		if !strings.Contains(said, c.says) || strings.Contains(said, "hush-hush") {
			t.Errorf("%s: the boot says %q, want %q", c.line, said, c.says)
		}
	}
}
