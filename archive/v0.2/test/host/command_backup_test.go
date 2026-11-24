package host_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynhnhanquoc/sqlitewire/catalog"
	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/library/database"
)

// commandsWithBackup is a manifest whose backup command copies each database it is given to --to, keeping --keep.
const commandsWithBackup = `import * as app from "kitwork/application";
app.root("apps");
app.commands({
  backup: (context) => {
    for (const name of context.arguments) {
      const database = context.database(name);
      const made = context.options.keep === undefined ? database.backup(context.options.to) : database.backup(context.options.to, { keep: Number(context.options.keep) });
      context.print(JSON.stringify(made));
    }
  },
  bare: (context) => context.database("shopping").backup(),
  odd: (context) => context.database("shopping").backup("copies", { kept: 1 }),
});
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`

// made is what backup(…) gives.
type made struct {
	Folder string `json:"folder"`
	Files  []struct {
		Name     string  `json:"name"`
		Bytes    int64   `json:"bytes"`
		Revision *uint64 `json:"revision"`
	} `json:"files"`
	Bytes   int64    `json:"bytes"`
	Seconds float64  `json:"seconds"`
	Removed []string `json:"removed"`
}

// kitdbFile makes a KitDB file at path with a table notes of rows rows, its history on, and gives it open: its writer.
func kitdbFile(t *testing.T, path string, rows int) *kitdb.Database {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := kitdb.Open(context.Background(), path, kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Migrate(context.Background(), "t", `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	// with its history, each write is a revision: the copy's says where it was taken
	if _, err := database.SetHistory(context.Background(), "t", "notes", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		insertNote(t, database)
	}
	return database
}

func insertNote(t *testing.T, database *kitdb.Database) uint64 {
	revision, err := database.Write(context.Background(), "t", func(transaction *kitdb.Transaction) error {
		_, err := transaction.Exec(`INSERT INTO notes (body) VALUES (?)`, strings.Repeat("n", 500))
		return err
	})
	if err != nil {
		t.Error(err)
	}
	return revision
}

// TestACommandBacksUpADatabase: context.database(name).backup(to, { keep }) (docs/KITWORK_COMMANDS.md mục 6, step 4 of
// mục 9) copies each schema file of a database the port names, the host's (.database/ beside the manifest, or
// app.database's folder) or an app's (<domain>/<folder>), with its structure, into <to>/<name>/<moment>/, while the
// writer that holds it goes on writing; nothing else of the folder (.history, .search, the lock, the WAL). A copy is
// whole or not there; keep removes the older copies, only the folders backup names. A name of no database is
// NOT_FOUND; a database of only its structure, a copy inside the database, and backup written wrong are refused.
//
// CONTROL (13/11/2026), each turned off alone (thirteen of fifteen; the other two in the test below): app.database's
// folder, `backup of app.database's orders: NOT_FOUND: … database "orders" does not exist`; keep from 1, "--keep 0:
// <nil>"; keep the only option, "odd: … keep is how many copies are kept …"; the folder required, "bare: <nil>"; the
// revision said, "the copy of kitwork.db is at revision 0, not past the writer's 203"; only the .db files, "backup of
// … billing.db-shm"; a database of only its structure, "backup empty …: <nil>"; no copy inside the database, "backup
// shopping --to .database/shopping/in: <nil>"; a failed copy cleaned, "a failed copy left [.2026-11-13T09-59-36.partial]";
// the structure copied, "the copy of shopping holds [billing.db kitwork.db]"; only backup's folders removed, "keep 2
// removed [.x.partial 2026-01-03 …]"; the oldest removed, "keep 2 removed [2026-11-13T09-59-50 …]"; keep applied, "keep
// 2 removed []"; the cluster's accounts copied, "the copy of shopping holds [billing.db kitwork.db kitwork.db.sql]";
// accounts that do not copy failing the copy, "backup of orders with accounts that do not copy: … GetFileAttributesEx …".
func TestACommandBacksUpADatabase(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                     commandsWithBackup,
		".database/shopping/kitwork.db.sql":    "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT);\n",
		".database/shopping/kitwork.db.search": "an index",
		".database/empty/kitwork.db.sql":       "CREATE TABLE notes (id INTEGER PRIMARY KEY);\n",
		"apps/shop/shop.test/router.work":      routerImport + `router.get((context) => context.text("ok"));` + "\n",
	})
	shopping := kitdbFile(t, filepath.Join(folder, ".database", "shopping", "kitwork.db"), 200)
	// the accounts of the host's cluster, open as the port holds them: alice made by SQL over the port
	accounts, err := catalog.Open(context.Background(), filepath.Join(folder, ".database"), catalog.Admin{User: "kitdb", Password: "pw-of-the-host"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, _, err := accounts.Command(context.Background(), "kitdb", "shopping", `CREATE USER alice PASSWORD 'secret-of-alice'`); err != nil {
		t.Fatal(err)
	}
	kitdbFile(t, filepath.Join(folder, ".database", "shopping", "billing.db"), 3)
	kitdbFile(t, filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db"), 5)
	if err := os.MkdirAll(filepath.Join(folder, ".database", "shopping", "kitwork.db.history"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a database whose second file is no KitDB file: its copy fails, and leaves nothing
	kitdbFile(t, filepath.Join(folder, ".database", "mixed", "kitwork.db"), 1)
	plain, err := sql.Open("sqlite", filepath.Join(folder, ".database", "mixed", "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Exec(`CREATE TABLE things (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	_ = plain.Close()

	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "copies")
	run := func(words ...string) ([]made, error) {
		t.Helper()
		var out bytes.Buffer
		err := read.RunCommand(context.Background(), "backup", words, &out, &bytes.Buffer{})
		var all []made
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line == "" {
				continue
			}
			var one made
			if err := json.Unmarshal([]byte(line), &one); err != nil {
				t.Fatalf("backup printed %q: %v", line, err)
			}
			all = append(all, one)
		}
		return all, err
	}

	// the writer of shopping goes on while the copy is taken
	base := shopping.Revision()
	stop := make(chan struct{})
	var writing sync.WaitGroup
	writing.Add(1)
	go func() {
		defer writing.Done()
		for {
			select {
			case <-stop:
				return
			default:
				insertNote(t, shopping)
			}
		}
	}()
	copies, err := run("shopping", "shop.test/application", "--to", to)
	close(stop)
	writing.Wait()
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if len(copies) != 2 {
		t.Fatalf("backup printed %d copies, want 2", len(copies))
	}
	first := copies[0]
	if filepath.Dir(filepath.FromSlash(first.Folder)) != filepath.Join(to, "shopping") {
		t.Errorf("the copy of shopping is in %s, want a folder of %s", first.Folder, filepath.Join(to, "shopping"))
	}
	var names []string
	var total int64
	var revision uint64
	for _, file := range first.Files {
		names = append(names, file.Name)
		total += file.Bytes
		if file.Name == "kitwork.db" && file.Revision != nil {
			revision = *file.Revision
		}
	}
	if want := []string{"billing.db", "kitwork.db", "kitwork.db.sql", ".catalog/kitwork.db"}; !slices.Equal(names, want) {
		t.Errorf("the copy of shopping holds %v, want %v", names, want)
	}
	if held := namesIn(t, filepath.FromSlash(first.Folder)); !slices.Equal(held, []string{".catalog", "billing.db", "kitwork.db", "kitwork.db.sql"}) {
		t.Errorf("the copy's folder holds %v", held)
	}
	// the copy's accounts are the cluster's: alice is there
	copiedAccounts, err := catalog.Open(context.Background(), filepath.FromSlash(first.Folder), catalog.Admin{})
	if err != nil {
		t.Fatalf("the copy's accounts: %v", err)
	}
	if _, found, err := copiedAccounts.Verifier(context.Background(), "alice"); err != nil || !found {
		t.Errorf("alice in the copy's accounts: %v, %v", found, err)
	}
	_ = copiedAccounts.Close()
	if total != first.Bytes || first.Bytes == 0 || first.Seconds < 0 || len(first.Removed) != 0 {
		t.Errorf("the copy says %d bytes of %d, %v seconds, removed %v", first.Bytes, total, first.Seconds, first.Removed)
	}
	if revision <= base {
		t.Errorf("the copy of kitwork.db is at revision %d, not past the writer's %d before the copy", revision, base)
	}
	if after := shopping.Revision(); after <= revision {
		t.Errorf("the writer committed nothing after the copy's revision %d: it did not go on", revision)
	}
	copied, err := kitdb.Open(context.Background(), filepath.Join(filepath.FromSlash(first.Folder), "kitwork.db"), kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if copied.Revision() != revision {
		t.Errorf("the copy opens at revision %d, backup said %d", copied.Revision(), revision)
	}
	_ = copied.Close()
	if app := copies[1]; filepath.Dir(filepath.FromSlash(app.Folder)) != filepath.Join(to, "shop.test", "application") || len(app.Files) != 1 || app.Files[0].Name != "kitwork.db" {
		t.Errorf("the copy of shop.test/application: %+v", app)
	}

	// keep: the newest copies BackupFolder named stay, the older go, nothing else there is touched
	for _, name := range []string{"2026-01-01T00-00-00", "2026-01-02T00-00-00", "2026-01-02T00-00-00-1", "notes", "2026-01-03", ".x.partial"} {
		if err := os.MkdirAll(filepath.Join(to, "shopping", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copies, err = run("shopping", "--to", to, "--keep", "2")
	if err != nil || len(copies) != 1 {
		t.Fatalf("backup --keep 2: %v, %d copies", err, len(copies))
	}
	second := copies[0]
	var removed []string
	for _, folder := range second.Removed {
		removed = append(removed, filepath.Base(filepath.FromSlash(folder)))
	}
	if want := []string{"2026-01-01T00-00-00", "2026-01-02T00-00-00", "2026-01-02T00-00-00-1"}; !slices.Equal(removed, want) {
		t.Errorf("keep 2 removed %v, want %v", removed, want)
	}
	left := namesIn(t, filepath.Join(to, "shopping"))
	want := []string{".x.partial", "2026-01-03", filepath.Base(filepath.FromSlash(first.Folder)), filepath.Base(filepath.FromSlash(second.Folder)), "notes"}
	slices.Sort(want)
	if !slices.Equal(left, want) {
		t.Errorf("after keep 2, %s holds %v, want %v", filepath.Join(to, "shopping"), left, want)
	}

	// a copy that fails leaves nothing, and keeps every older one
	if _, err := run("mixed", "--to", to, "--keep", "1"); err == nil || !strings.Contains(err.Error(), "other.db") || !strings.Contains(err.Error(), "not a file KitDB wrote") {
		t.Errorf("backup of a database with a file KitDB did not write: %v", err)
	}
	if held := namesIn(t, filepath.Join(to, "mixed")); len(held) != 0 {
		t.Errorf("a failed copy left %v", held)
	}

	for words, want := range map[string]string{
		"missing --to " + to:                  `NOT_FOUND: application.work:6:31: context.database: database "missing" does not exist`,
		"../shopping --to " + to:              `NOT_FOUND`,
		"empty --to " + to:                    "the database empty has no file yet, only its structure",
		"shopping --to .database/shopping/in": "inside the database itself",
		"shopping --to " + to + " --keep 0":   "keep is how many copies are kept, a whole number from 1",
		"shopping --to " + to + " --keep 1.5": "keep is how many copies are kept, a whole number from 1",
	} {
		if _, err := run(strings.Fields(words)...); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("backup %s: %v, want %q", words, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, ".database", "shopping", "in")); err == nil {
		t.Error("a copy refused for going inside the database made its folder")
	}
	for name, want := range map[string]string{
		"bare": "backup takes the folder the copies go to",
		"odd":  "backup: kept is no option",
	} {
		if err := read.RunCommand(context.Background(), name, nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", name, err, want)
		}
	}

	// with app.database, the host's databases are its folder's
	moved := write(t, map[string]string{
		"application.work": strings.Replace(commandsWithBackup, `app.root("apps");`, `app.root("apps");
app.database("./data", { port: 5999, password: "pw" });`, 1),
		"apps/.keep": "",
	})
	kitdbFile(t, filepath.Join(moved, "data", "orders", "kitwork.db"), 2)
	readMoved, err := host.ReadWithLog(filepath.Join(moved, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := readMoved.RunCommand(context.Background(), "backup", []string{"orders", "--to", to}, &out, &bytes.Buffer{}); err != nil || !strings.Contains(out.String(), "/orders/") {
		t.Errorf("backup of app.database's orders: %v, %q", err, out.String())
	}
	// accounts that do not copy fail the copy of the database, which leaves nothing
	if err := os.MkdirAll(filepath.Join(moved, "data", ".catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "data", ".catalog", "kitwork.db"), []byte(strings.Repeat("no accounts ", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	before := namesIn(t, filepath.Join(to, "orders"))
	if err := readMoved.RunCommand(context.Background(), "backup", []string{"orders", "--to", to}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "the database orders, the accounts of its cluster") {
		t.Errorf("backup of orders with accounts that do not copy: %v", err)
	}
	if after := namesIn(t, filepath.Join(to, "orders")); !slices.Equal(after, before) {
		t.Errorf("a copy whose accounts failed left %v, there was %v", after, before)
	}
}

// namesIn is what folder holds, sorted; nothing when it is not there.
func namesIn(t *testing.T, folder string) []string {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// TestBackupFolderNamesASecondCopyOfTheSameSecond: a copy made within the second of another is <moment>-1, and keep
// orders the copies of one second by that number, -2 before -10.
//
// CONTROL (13/11/2026): with the name not counted on, "backup --keep 2: … rename …partial …" (and here the second copy
// fails); with the numbers ordered as texts, "keep 2 left [2026-11-13T03-00-00-2 2026-11-13T03-00-00-3]".
func TestBackupFolderNamesASecondCopyOfTheSameSecond(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "shopping")
	kitdbFile(t, filepath.Join(folder, "kitwork.db"), 1)
	to := t.TempDir()
	now := time.Date(2026, 11, 13, 3, 0, 0, 0, time.Local)
	first, err := database.BackupFolder(context.Background(), folder, "shopping", to, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.BackupFolder(context.Background(), folder, "shopping", to, 0, now)
	if err != nil {
		t.Fatalf("the second copy of the same second: %v", err)
	}
	if filepath.Base(first.Folder) != "2026-11-13T03-00-00" || filepath.Base(second.Folder) != "2026-11-13T03-00-00-1" {
		t.Errorf("two copies of one second: %s, %s", filepath.Base(first.Folder), filepath.Base(second.Folder))
	}
	for _, name := range []string{"2026-11-13T03-00-00-2", "2026-11-13T03-00-00-10"} {
		if err := os.Mkdir(filepath.Join(to, "shopping", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.BackupFolder(context.Background(), folder, "shopping", to, 2, now); err != nil {
		t.Fatal(err)
	}
	if left := namesIn(t, filepath.Join(to, "shopping")); !slices.Equal(left, []string{"2026-11-13T03-00-00-10", "2026-11-13T03-00-00-3"}) {
		t.Errorf("keep 2 left %v, want the -10 and the -3 just made", left)
	}
}
