package host_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/library/database"
)

const structureSchema = `import { struct, id, string, decimal } from "kitwork/schema";
import { connection } from "kitwork/database";

export const items = struct({
  id: id(),
  code: string().transform((value) => value.trim().toUpperCase()).unique(),
  price: decimal(2).min(0)
});

export const db = connection().tables({ items });
`

const structureRoute = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const made = db.insert((items) => ({ code: " ab ", price: 12.5 }));
  return context.json({ made, found: db.first((items) => items.code === "ab") });
});
`

// A folder holding a database's structure and not the file, a clone of the repository, gets the file made from it as
// the boot reads it (docs/KITWORK_DATABASE.md, "Database là gốc"; docs/KITWORK_KITDB.md: kitwork.db.sql vào git): the
// tables, the indexes, the kinds and rules of kitwork_columns, no row. The statements then run on it: a decimal reads as
// its number, a transform holds.
//
// CONTROL (07/11/2026): with checkEntry not making the file, the statement failed "database .database/application/
// kitwork.db chưa có: chạy kitwork migrate".
func TestADatabaseIsMadeFromItsStructure(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": structureSchema,
		"apps/shop/shop.test/router.work": structureRoute,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	// a clone: the folder keeps the structure, nothing else
	databaseFolder := filepath.Dir(databaseFile(folder))
	entries, err := os.ReadDir(databaseFolder)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "kitwork.db.sql" {
			if err := os.RemoveAll(filepath.Join(databaseFolder, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	if log := app.log.String(); !strings.Contains(log, "[database] .database/application/kitwork.db tạo từ kitwork.db.sql") || strings.Contains(log, "chưa có") {
		t.Errorf("the boot's log: %s", log)
	}
	read := answer(t, app, "/")
	expect(t, read, map[string]string{"found": `{"id": 1, "code": "AB", "price": 12.5}`})
}

// A database of the host's .database/ that holds only its structure is made from it when a session of the port opens
// it.
//
// CONTROL (07/11/2026): with portSchemas not making it, the session was refused: database "shelf": no kitwork.db.
func TestThePortMakesADatabaseFromItsStructure(t *testing.T) {
	port := freePort(t)
	manifest := strings.Replace(manifest, "app.run();", `app.database(".database", { port: `+strconv.Itoa(port)+`, user: "admin", password: "pw" });
app.run();`, 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": structureSchema,
		"apps/shop/shop.test/router.work": structureRoute,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	structure, err := os.ReadFile(databaseFile(folder) + ".sql")
	if err != nil {
		t.Fatal(err)
	}
	shelf := filepath.Join(folder, ".database", "shelf")
	if err := os.MkdirAll(shelf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shelf, "kitwork.db.sql"), structure, 0o644); err != nil {
		t.Fatal(err)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	address, err := app.host.DatabasePort()
	if err != nil {
		t.Fatal(err)
	}
	client, err := pgConnect(address.String(), "admin", "pw", "shelf")
	if err != nil {
		t.Fatalf("a session on a database of only its structure: %v", err)
	}
	defer client.Close()
	if _, _, err := client.query(`INSERT INTO items (code, price) VALUES ('  x ', 1.25)`); err != nil {
		t.Fatalf("an insert: %v", err)
	}
	rows, _, err := client.query(`SELECT code, price FROM items`)
	if err != nil || jsonText(rows) != `[["  x ","1.25"]]` {
		t.Errorf("the table made from the structure, its kinds (a decimal read as its value; a struct's transform is the engine's, not the database's): %s, %v", jsonText(rows), err)
	}
}

// A structure that cannot be made is said at boot, and leaves no file behind: the next boot tries again.
func TestAStructureThatCannotBeMadeLeavesNoFile(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                               manifest,
		"apps/shop/core/schema/shop.work":                structureSchema,
		"apps/shop/shop.test/router.work":                structureRoute,
		"apps/shop/.database/application/kitwork.db.sql": "CREATE TABLE items (id bigserial PRIMARY KEY, code text NOT NULL,\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if log := app.log.String(); !strings.Contains(log, "tạo database từ kitwork.db.sql") {
		t.Errorf("the boot's log: %s", log)
	}
	if _, err := os.Stat(databaseFile(folder)); !os.IsNotExist(err) {
		t.Errorf("the file was left: %v", err)
	}
}

// app.history({ days }) is how long the history of every database keeps (docs/KITWORK_KITDB.md câu 13.6): each writer
// applies it as it opens and every hour after, and what no moment of the days kept needs goes; at() before what is
// kept says from when the history reaches, and the days.
//
// CONTROL (07/11/2026): with the writer not applying the retention (keep), the history kept its three changes. Three
// days on, KitDB anchors the file and keeps the change the anchor stands on: one of three is left.
func TestTheHistoryKeepsTheDaysOfTheManifest(t *testing.T) {
	var ahead atomic.Int64
	every, clock := database.RetainEvery, database.Now
	database.RetainEvery = 20 * time.Millisecond
	database.Now = func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }
	t.Cleanup(func() { database.RetainEvery, database.Now = every, clock })

	manifest := strings.Replace(manifest, "app.run();", "app.history({ days: 1 });\napp.run();", 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": structureSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  if (context.request.query("write") === "yes") {
    db.insert((items) => ({ code: "a", price: 1 }));
    db.insert((items) => ({ code: "b", price: 2 }));
    db.where((items) => items.code === "a").update((items) => ({ price: 3 }));
  }
  return context.json({ changes: db.history((items) => items).length,
    before: safe(() => db.at("2020-01-01T00:00:00Z").list((items) => items)).error });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	declareHistory(t, folder, "items")
	app := boot(t, filepath.Join(folder, "application.work"))
	if database.HistoryDays() != 1 {
		t.Fatalf("HistoryDays = %d, want the manifest's 1", database.HistoryDays())
	}
	if read := answer(t, app, "/?write=yes"); read["changes"] != 3.0 {
		t.Fatalf("the history before: %v", read)
	}
	ahead.Store(int64(3 * 24 * time.Hour)) // three days on: the days kept no longer reach the changes
	deadline := time.Now().Add(10 * time.Second)
	for {
		read := answer(t, app, "/")
		// the anchor holds the file as the last change left it; what came before it goes
		if changes, _ := read["changes"].(float64); changes < 3 {
			failure, _ := read["before"].(map[string]any)
			if message, _ := failure["message"].(string); !strings.Contains(message, "lịch sử của database chỉ còn từ") || !strings.Contains(message, "engine giữ 1 ngày") {
				t.Errorf("at() before the history kept: %v", read["before"])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history kept %v changes three days on, with one day kept", read["changes"])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// app.history takes { days }, a whole number from 1 to 36500, once; search of app.database names a database, its
// tables and their columns.
func TestTheManifestsHistoryAndSearchWrittenWrongStopTheBoot(t *testing.T) {
	for _, c := range []struct{ line, says string }{
		{`app.history({ days: 0 });`, "app.history: days is a whole number of days from 1 to 36500"},
		{`app.history({ weeks: 1 });`, "app.history: weeks is not a setting of the history; it has days"},
		{`app.history({ days: 1 }); app.history({ days: 2 });`, "app.history(…) is declared twice"},
		{`app.database(".database", { port: 5999, password: "x", search: { shop: { posts: [] } } });`, "search.shop.posts: an index has from 1 to 32 columns"},
		{`app.database(".database", { port: 5999, password: "x", search: { shop: { posts: { title: 0 } } } });`, "search.shop.posts.title: the weight of a column is a number from more than 0 to 1000"},
		{`app.database(".database", { port: 5999, password: "x", search: { "../x": { posts: ["title"] } } });`, `search: "../x" is not a database of the host's .database/ folder`},
	} {
		said := bootError(t, map[string]string{
			"application.work":                strings.Replace(manifest, "app.run();", c.line+"\napp.run();", 1),
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
		})
		if !strings.Contains(said, c.says) {
			t.Errorf("%s: the boot says %q, want %q", c.line, said, c.says)
		}
	}
}
