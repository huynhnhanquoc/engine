package host_test

import (
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const portSchema = `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";

export const notes = struct({ id: id(), text: string() });

export const db = connection().tables({ notes });
`

// freePort is a port of the loopback nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// The host's database port (docs/KITWORK_KITDB.md câu 13.5): app.database(path, { port, user, password }) serves the
// PostgreSQL wire with SCRAM-SHA-256 on an app's database by <domain>/<folder> and on a folder of the host's
// .database/; a write through it goes through the file's one writer in the process, the site's, so the site reads it
// at once; the history of a table turned on through it (ALTER TABLE … SET (history = on), KITWORK_KITDB.md câu 13.11)
// keeps the writes from then, the site's before it not; a wrong password, a database no app has, are the wire's errors.
//
// CONTROL (06/11/2026): with Acquire opening a writer of its own each time, the port's INSERT failed, the site holding
// the file's lock.
func TestTheDatabasePortServesTheApps(t *testing.T) {
	port := freePort(t)
	manifest := strings.Replace(manifest, "app.run();", `app.database(".database", { port: `+strconv.Itoa(port)+`, user: "admin", password: "mật khẩu" });
app.run();`, 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": portSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  db.insert((notes) => ({ text: "từ site" }));
  return context.json({ ok: true });
});
`,
		"apps/shop/shop.test/read/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => context.json({
  texts: db.list((notes) => notes).map((note) => note.text),
  changes: db.history((notes) => notes).length
}));
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	// a database of the host's own .database/, a plain SQLite file
	plain := filepath.Join(folder, ".database", "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(plain, "kitwork.db")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Exec(`CREATE TABLE things (name TEXT); INSERT INTO things VALUES ('một')`); err != nil {
		t.Fatal(err)
	}
	file.Close()

	app := boot(t, filepath.Join(folder, "application.work"))
	address, err := app.host.DatabasePort()
	if err != nil || address == nil {
		t.Fatalf("the port: %v, %v", address, err)
	}
	answer(t, app, "/") // the site writes first: it holds the file's writer

	client, err := pgConnect(address.String(), "admin", "mật khẩu", "shop.test/application")
	if err != nil {
		t.Fatalf("connect to shop.test/application: %v", err)
	}
	defer client.Close()
	if _, tag, err := client.query(`ALTER TABLE notes SET (history = on)`); err != nil || tag != "ALTER TABLE" {
		t.Fatalf("ALTER TABLE notes SET (history = on) through the port: %q, %v", tag, err)
	}
	if _, tag, err := client.query(`INSERT INTO notes (text) VALUES ('từ cổng')`); err != nil || tag != "INSERT 0 1" {
		t.Fatalf("INSERT through the port: %q, %v", tag, err)
	}
	rows, _, err := client.query(`SELECT text FROM notes ORDER BY id`)
	if err != nil || jsonText(rows) != `[["từ site"],["từ cổng"]]` {
		t.Errorf("SELECT through the port: %s, %v", jsonText(rows), err)
	}
	expect(t, answer(t, app, "/read"), map[string]string{"texts": `["từ site","từ cổng"]`, "changes": "1"})

	other, err := pgConnect(address.String(), "admin", "mật khẩu", "plain")
	if err != nil {
		t.Fatalf("connect to plain: %v", err)
	}
	defer other.Close()
	if rows, _, err := other.query(`SELECT name FROM things`); err != nil || jsonText(rows) != `[["một"]]` {
		t.Errorf("the host's own database: %s, %v", jsonText(rows), err)
	}

	var wire *pgError
	if _, err := pgConnect(address.String(), "admin", "sai", "shop.test/application"); !errors.As(err, &wire) || wire.code != "28P01" {
		t.Errorf("a wrong password: %v, want 28P01", err)
	}
	nobody, err := pgConnect(address.String(), "admin", "mật khẩu", "nobody.test/application")
	if err == nil {
		defer nobody.Close()
	}
	if !errors.As(err, &wire) || wire.code != "3D000" {
		t.Errorf("a database no app has: %v, want 3D000", err)
	}
}
