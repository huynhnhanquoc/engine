package host_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const portSearchSchema = `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";

export const notes = struct({ id: id(), text: string().searchable() });

export const db = connection().tables({ notes });
`

// MATCH on the port (docs/KITWORK_KITDB.md câu 13.5): a table of the host's .database/ that search of app.database
// declares, and a table of an app whose .searchable() columns are its index in the database, both answer
// `WHERE t MATCH 'words'`, without the accents, with _score_ and _snippet_, from the index <file>.search the old
// engine's full text builds; a table of no index is 42704, a column not in the index 42703.
//
// CONTROL (07/11/2026): with the session given no search provider, every MATCH failed (0A000: MATCH: this port has no
// full-text index); with discover not opening the app's index, the app's MATCH was 42704.
func TestTheDatabasePortAnswersMatch(t *testing.T) {
	port := freePort(t)
	manifest := strings.Replace(manifest, "app.run();", `app.database(".database", { port: `+strconv.Itoa(port)+`, user: "admin", password: "pw",
  search: { catalog: { posts: { title: 5, body: 1 } } } });
app.run();`, 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": portSearchSchema,
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	catalog := filepath.Join(folder, ".database", "catalog")
	if err := os.MkdirAll(catalog, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(catalog, "kitwork.db")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Exec(`CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL, body TEXT NOT NULL);
INSERT INTO posts (title, body) VALUES ('Bàn phím cơ', 'gõ êm'), ('Chuột không dây', 'pin lâu'), ('Màn hình', 'có bàn phím ảo')`); err != nil {
		t.Fatal(err)
	}
	file.Close()

	app := boot(t, filepath.Join(folder, "application.work"))
	address, err := app.host.DatabasePort()
	if err != nil {
		t.Fatal(err)
	}
	// an index is built in the background: a MATCH before then answers how far the build is (55000)
	match := func(database, text string) ([][]any, error) {
		t.Helper()
		client, err := pgConnect(address.String(), "admin", "pw", database)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		deadline := time.Now().Add(10 * time.Second)
		for {
			rows, _, err := client.query(text)
			var wire *pgError
			if errors.As(err, &wire) && wire.code == "55000" && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return rows, err
		}
	}
	rows, err := match("catalog", `SELECT title FROM posts WHERE posts MATCH 'ban phim' ORDER BY _score_ DESC`)
	if err != nil || jsonText(rows) != `[["Bàn phím cơ"],["Màn hình"]]` {
		t.Errorf("MATCH on a declared table: %s, %v (a match in the title weighs 5, in the body 1)", jsonText(rows), err)
	}
	rows, err = match("catalog", `SELECT title, _snippet_ FROM posts WHERE body MATCH 'pin'`)
	if err != nil || len(rows) != 1 || rows[0][0] != "Chuột không dây" || !strings.Contains(rows[0][1].(string), "<b>pin</b>") {
		t.Errorf("MATCH on one column: %s, %v", jsonText(rows), err)
	}

	client, err := pgConnect(address.String(), "admin", "pw", "shop.test/application")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.query(`INSERT INTO notes (text) VALUES ('Cà phê sữa đá'), ('Trà đào')`); err != nil {
		t.Fatalf("an insert: %v", err)
	}
	client.Close()
	rows, err = match("shop.test/application", `SELECT text FROM notes WHERE notes MATCH 'ca phe'`)
	if err != nil || jsonText(rows) != `[["Cà phê sữa đá"]]` {
		t.Errorf("MATCH on an app's .searchable() table: %s, %v", jsonText(rows), err)
	}
	var wire *pgError
	if _, err := match("shop.test/application", `SELECT id FROM notes WHERE id MATCH 'x'`); !errors.As(err, &wire) || wire.code != "42703" {
		t.Errorf("MATCH on a column outside the index: %v, want 42703", err)
	}
	if _, err := match("catalog", `SELECT name FROM sqlite_schema WHERE sqlite_schema MATCH 'x'`); err == nil {
		t.Errorf("MATCH on a table of no index answered")
	}
}
