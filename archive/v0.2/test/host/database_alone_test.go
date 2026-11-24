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

	"github.com/kitwork/engine2/host"
)

// The database runs alone, as other SQL databases run (Quốc, 07/11/2026): a manifest of app.database and the switch,
// app.run(), no website, serves the port and nothing else, until it is closed. A client makes a database of its
// .database/ by its structure (an empty kitwork.db.sql), its tables in SQL, writes and reads them; closing the host
// lets go of the files. The manifest without app.run() is only read and checked; app.run() with nothing it opens is
// an error.
//
// CONTROL (07/11/2026): with Serve asking for a website first, it ended at once: "the manifest declares no website".
func TestTheDatabaseRunsAlone(t *testing.T) {
	port := freePort(t)
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.database(".database", { port: ` + strconv.Itoa(port) + `, user: "admin", password: "pw" });
app.run();
`,
		".database/shop/kitwork.db.sql": "",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Check(); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v", err)
	}
	served := make(chan error, 1)
	go func() { served <- read.Serve() }()
	address := "127.0.0.1:" + strconv.Itoa(port)
	var client *pgClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		if client, err = pgConnect(address, "admin", "pw", "shop"); err == nil || time.Now().After(deadline) {
			break
		}
		select {
		case err := <-served:
			t.Fatalf("Serve ended: %v\n%s", err, log)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, log)
	}
	for _, statement := range []string{
		`CREATE TABLE notes (id bigserial PRIMARY KEY, text text NOT NULL CHECK (length(text) >= 1))`,
		`INSERT INTO notes (text) VALUES ('một'), ('hai')`,
	} {
		if _, _, err := client.query(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	rows, _, err := client.query(`SELECT text FROM notes ORDER BY id`)
	if err != nil || jsonText(rows) != `[["một"],["hai"]]` {
		t.Errorf("the rows: %s, %v", jsonText(rows), err)
	}
	client.Close()
	if !strings.Contains(log.String(), "[app] the database runs alone on 127.0.0.1:"+strconv.Itoa(port)+", no website") {
		t.Errorf("the log: %s", log)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve after Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not end after Close")
	}
	if _, err := os.Stat(filepath.Join(folder, ".database", "shop", "kitwork.db")); err != nil {
		t.Errorf("the database's file: %v", err)
	}

	for _, c := range []struct{ manifest, says string }{
		{`app.database(".database", { port: 5999, password: "pw" });`, "the manifest has no app.run(): it is read and checked, and nothing opens"},
		{`app.desktop({ name: "x" }); app.run();`, "runs nothing engine2 opens: app.run() opens app.website(…) and app.database(…)"},
		{`app.database(".database", { port: 5999, password: "pw" }); app.run(); app.run();`, "app.run() is declared twice"},
		{`app.database(".database", { port: 5999, password: "pw" }); app.run(1, 2);`, "app.run() takes nothing: it is the switch on the manifest's last line"},
	} {
		folder := write(t, map[string]string{"application.work": "import * as app from \"kitwork/application\";\n" + c.manifest + "\n"})
		read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil {
			err = read.Check()
		}
		if err == nil {
			err = read.Boot()
		}
		if err == nil {
			err = read.Serve()
		}
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v, want %q", c.manifest, err, c.says)
		}
	}
}

// aloneManifest declares a website and app.database's port on port.
func aloneManifest(port int) string {
	return `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
app.database(".database", { port: ` + strconv.Itoa(port) + `, user: "admin", password: "mật khẩu" });
app.run();
`
}

// writeSQLite makes the SQLite file at path with statements run in it.
func writeSQLite(t *testing.T, path, statements string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Exec(statements); err != nil {
		t.Fatal(err)
	}
}

// kitwork database (Host.ServeDatabase, Quốc 08/11/2026) on a manifest that declares a website as well: the port opens
// with no site booted (the site's router.work does not even compile) and the website's port not opened; it reads and
// writes an app's database by <domain>/<folder>, the app found by the folders under app.root as Boot would find it,
// and a folder of the host's .database/; a domain no app's folder has is 3D000; closing the host ends it.
//
// CONTROL (08/11/2026): with appRootOf looking only at the open sites, "connect to shop.test/application: 3D000:
// database of \"shop.test\" does not exist: no app has that domain".
func TestTheDatabaseAloneFindsAnAppsDatabaseByItsFolders(t *testing.T) {
	port := freePort(t)
	folder := write(t, map[string]string{
		"application.work":                aloneManifest(port),
		"apps/shop/shop.test/router.work": routerImport + "router.nothing(",
	})
	writeSQLite(t, filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db"),
		`CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT NOT NULL); INSERT INTO notes (text) VALUES ('một')`)
	writeSQLite(t, filepath.Join(folder, ".database", "plain", "kitwork.db"),
		`CREATE TABLE things (name TEXT); INSERT INTO things VALUES ('hai')`)

	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- read.ServeDatabase() }()
	address := "127.0.0.1:" + strconv.Itoa(port)
	var other *pgClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		if other, err = pgConnect(address, "admin", "mật khẩu", "plain"); err == nil || time.Now().After(deadline) {
			break
		}
		select {
		case err := <-served:
			t.Fatalf("ServeDatabase ended: %v\n%s", err, log)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatalf("connect to plain: %v\n%s", err, log)
	}
	if rows, _, err := other.query(`SELECT name FROM things`); err != nil || jsonText(rows) != `[["hai"]]` {
		t.Errorf("the host's own database: %s, %v", jsonText(rows), err)
	}
	other.Close()
	client, err := pgConnect(address, "admin", "mật khẩu", "shop.test/application")
	if err != nil {
		t.Fatalf("connect to shop.test/application: %v", err)
	}
	if _, tag, err := client.query(`INSERT INTO notes (text) VALUES ('từ cổng')`); err != nil || tag != "INSERT 0 1" {
		t.Fatalf("INSERT through the port: %q, %v", tag, err)
	}
	if rows, _, err := client.query(`SELECT text FROM notes ORDER BY id`); err != nil || jsonText(rows) != `[["một"],["từ cổng"]]` {
		t.Errorf("SELECT through the port: %s, %v", jsonText(rows), err)
	}
	client.Close()
	var wire *pgError
	nobody, err := pgConnect(address, "admin", "mật khẩu", "nobody.test/application")
	if err == nil {
		nobody.Close()
	}
	if !errors.As(err, &wire) || wire.code != "3D000" {
		t.Errorf("a database no app's folder has: %v, want 3D000", err)
	}
	if len(read.Sites()) != 0 {
		t.Errorf("sites open: %d, want none", len(read.Sites()))
	}
	if !strings.Contains(log.String(), "[app] the database runs alone on "+address+", the website not opened") {
		t.Errorf("the log: %s", log)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("ServeDatabase after Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeDatabase did not end after Close")
	}
}

// kitwork database refuses before anything listens: a manifest without app.database has no port to open, and one
// without app.run() is read and checked only.
func TestTheDatabaseAloneRefuses(t *testing.T) {
	for name, test := range map[string]struct{ manifest, want string }{
		"no app.database": {manifest, "declares no app.database"},
		"no app.run()":    {strings.Replace(aloneManifest(freePort(t)), "app.run();\n", "", 1), "has no app.run()"},
	} {
		folder := write(t, map[string]string{"application.work": test.manifest, "apps/shop/shop.test/router.work": routerImport})
		read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := read.ServeDatabase(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: ServeDatabase = %v, want %q", name, err, test.want)
		}
	}
}
