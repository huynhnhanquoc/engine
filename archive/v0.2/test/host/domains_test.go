package host_test

import (
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// domainsManifest serves the apps of its folder, the table of domains in system/domains.db.
const domainsManifest = `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "own.example", port: 8080, domains: "./system/domains.db" });
app.run();
`

// domainRow is a row of the table: hostname, identity, status, redirect_to, deleted_at; "" writes NULL.
type domainRow [5]string

// writeDomains makes the table of domains at file, the first engine's columns, with rows, in one transaction: a host
// reading it as it is made reads it whole or not at all.
func writeDomains(t *testing.T, file string, rows ...domainRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?_pragma=busy_timeout(5000)") // the host may be reading it
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE domain (hostname TEXT PRIMARY KEY, identity TEXT NOT NULL, status TEXT DEFAULT 'unverified',
		redirect_to TEXT, created_by TEXT, updated_by TEXT, deleted_by TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	null := func(text string) any {
		if text == "" {
			return nil
		}
		return text
	}
	for _, row := range rows {
		if _, err := tx.Exec(`INSERT INTO domain (hostname, identity, status, redirect_to, deleted_at) VALUES (?, ?, ?, ?, ?)`,
			row[0], row[1], row[2], null(row[3]), null(row[4])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// says is a site answering its name.
func says(name string) string {
	return routerImport + "router.get((context) => context.text(\"" + name + "\"));\n"
}

// The table of domains says which app serves a hostname (14/11/2026), as the first engine's system database did: a
// site folder opens when its row gives the hostname to its app, confirmed, not deleted, not sent elsewhere, whatever
// the order of the folders. aaa reads before bbb and has a folder of bbb's shop.example: bbb serves it, aaa's own site
// serves too, no app is closed. A row unverified, of an empty status, deleted, of no row: not served. A row sending
// its hostname elsewhere answers 301 there, the scheme, path and query kept, an http(s) address as it is; one of
// status redirect needs no folder; one unverified or with a redirect_to that is no address sends nowhere. A test name
// (site.test) needs no row. Each refusal is a line of the log.
//
// CONTROL: with the folders deciding as before (refuses always ""), aaa served shop.example, read first, and bbb was
// closed whole for holding a name another app had; with redirected not called, moved.example and old.example
// answered 404; with status not read, new.example and blank.example served. Each red.
func TestTheTableOfDomainsSaysWhichAppServesAHostname(t *testing.T) {
	files := map[string]string{"application.work": domainsManifest}
	for _, site := range []struct{ app, hostname string }{
		{"aaa", "shop.example"}, {"aaa", "own.example"},
		{"bbb", "shop.example"}, {"bbb", "new.example"}, {"bbb", "gone.example"}, {"bbb", "blank.example"},
		{"bbb", "nothing.example"}, {"bbb", "moved.example"}, {"bbb", "bad.example"}, {"bbb", "site.test"},
	} {
		files["apps/"+site.app+"/"+site.hostname+"/router.work"] = says(site.app + " " + site.hostname)
	}
	folder := write(t, files)
	writeDomains(t, filepath.Join(folder, "system", "domains.db"),
		domainRow{"shop.example", "bbb", "confirmed", "", ""},
		domainRow{"OWN.example", "aaa", "confirmed", "", ""},
		domainRow{"new.example", "bbb", "unverified", "", ""},
		domainRow{"gone.example", "bbb", "confirmed", "", "2026-11-01 10:00:00+07"},
		domainRow{"blank.example", "bbb", "", "", ""},
		domainRow{"moved.example", "bbb", "confirmed", "shop.example", ""},
		domainRow{"old.example", "bbb", "redirect", "https://shop.example/landing/", ""},
		domainRow{"parked.example", "bbb", "redirect", "", ""},
		domainRow{"hold.example", "bbb", "unverified", "shop.example", ""},
		domainRow{"bad.example", "bbb", "confirmed", "javascript:alert(1)", ""},
		domainRow{"", "bbb", "confirmed", "", ""},
	)
	b := boot(t, filepath.Join(folder, "application.work")) // fails the test when an app is closed

	for hostname, want := range map[string]string{"shop.example": "bbb shop.example", "own.example": "aaa own.example", "site.test": "bbb site.test"} {
		if got := b.get(t, hostname, "/"); got.status != http.StatusOK || got.body != want {
			t.Errorf("%s: %d %q, want %q", hostname, got.status, got.body, want)
		}
	}
	for _, hostname := range []string{"new.example", "gone.example", "blank.example", "nothing.example", "parked.example", "hold.example", "bad.example"} {
		if got := b.get(t, hostname, "/"); got.status != http.StatusNotFound {
			t.Errorf("%s: %d %q, want 404", hostname, got.status, got.body)
		}
	}
	for hostname, want := range map[string]string{
		"moved.example": "http://shop.example/a/b%20c?q=1&r=%C4%91",
		"old.example":   "https://shop.example/landing/a/b%20c?q=1&r=%C4%91",
	} {
		got := b.get(t, hostname, "/a/b%20c?q=1&r=%C4%91")
		if got.status != http.StatusMovedPermanently || got.header.Get("Location") != want {
			t.Errorf("%s: %d to %q, want 301 to %q", hostname, got.status, got.header.Get("Location"), want)
		}
	}
	contains(t, "the boot", b.log.String(),
		"[aaa] the site shop.example is not opened: its row gives it to the app bbb",
		`[bbb] the site new.example is not opened: its status is "unverified", not "confirmed"`,
		`[bbb] the site blank.example is not opened: its status is "", not "confirmed"`,
		"[bbb] the site gone.example is not opened: its row is deleted",
		"[bbb] the site nothing.example is not opened: the table of domains has no row for it",
		"[bbb] the site moved.example is not opened: its row sends it to shop.example",
		`[bbb] the site bad.example is not opened: its redirect_to "javascript:alert(1)" is no http:// or https:// address, nor a hostname`,
		"domains: parked.example: status redirect with no redirect_to: neither served nor sent elsewhere",
		`domains: bad.example: its redirect_to "javascript:alert(1)" is no http:// or https:// address, nor a hostname: neither served nor sent elsewhere`,
		"domains: rows with an empty hostname, which give no name to anyone: 1",
		"domains: 10 hostnames in ", ", 2 sent elsewhere")
	if strings.Contains(b.log.String(), "is closed") {
		t.Errorf("an app was closed:\n%s", b.log)
	}
}

// A database the catalogue gives by hostname goes to the app the table gives the hostname to: aaa, with a folder of
// bbb's shop.example, is not given it, at boot and in kitwork migrate (14/11/2026: the folder was enough).
//
// CONTROL: with siteNames reading the folders as before, aaa was given bbb's warehouse at boot and by kitwork migrate.
func TestADatabaseGivenByHostnameGoesToTheAppOfTheTable(t *testing.T) {
	files := grantFiles(`app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.example"] } });`)
	files["application.work"] = strings.Replace(domainsManifest, "app.run();", `app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.example"] } });`+"\napp.run();", 1)
	for name, text := range files {
		switch {
		case strings.HasPrefix(name, "apps/shop/"):
			delete(files, name)
			files["apps/bbb/"+strings.Replace(strings.TrimPrefix(name, "apps/shop/"), "shop.test", "shop.example", 1)] = text
		case strings.HasPrefix(name, "apps/kho/"):
			delete(files, name)
			files["apps/aaa/"+strings.Replace(strings.TrimPrefix(name, "apps/kho/"), "kho.test", "shop.example", 1)] = text
		}
	}
	folder := write(t, files)
	writeDomains(t, filepath.Join(folder, "system", "domains.db"), domainRow{"shop.example", "bbb", "confirmed", "", ""})
	code, said := runMigrate(t, folder, true, "")
	if code == 0 || !strings.Contains(said, `connection("warehouse"): database warehouse của host chưa cấp cho app aaa`) {
		t.Errorf("kitwork migrate of aaa, with a folder of bbb's hostname: %d\n%s", code, said)
	}
	got := bootFolder(t, folder)
	if !strings.Contains(got, `database warehouse của host chưa cấp cho app aaa`) || strings.Contains(got, "app bbb") {
		t.Errorf("the boot: %s", got)
	}
}

// bootFolder reads and boots the manifest of folder, and gives the error the boot ends with, or the error of the apps
// it closed ("" when none).
func bootFolder(t testing.TB, folder string) string {
	t.Helper()
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		return err.Error()
	}
	defer read.Close()
	if err := read.Boot(); err != nil {
		return err.Error()
	}
	if err := read.Failed(); err != nil {
		return err.Error()
	}
	return ""
}

// A host that declares its table of domains and cannot read it serves nothing, its error saying the table it reads:
// a file not there, a file without the table.
func TestATableOfDomainsTheHostCannotReadIsItsError(t *testing.T) {
	folder := write(t, map[string]string{"application.work": domainsManifest, "apps/aaa/own.example/router.work": says("aaa")})
	if got := bootFolder(t, folder); !strings.Contains(got, "app.website({ domains }): ") || !strings.Contains(got, "is not there: the host's table of domains is a SQLite file holding CREATE TABLE domain") {
		t.Errorf("no file: %s", got)
	}
	empty := filepath.Join(folder, "system", "domains.db")
	if err := os.MkdirAll(filepath.Dir(empty), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other (a TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if got := bootFolder(t, folder); !strings.Contains(got, "has no table domain as the host reads it: CREATE TABLE domain") {
		t.Errorf("no table: %s", got)
	}
	for written, want := range map[string]string{
		`domains: 5`:  `app.website({ domains }) takes the SQLite file of the host's table of domains, a path`,
		`domains: ""`: `app.website({ domains }) takes the SQLite file`,
	} {
		manifest := strings.Replace(domainsManifest, `domains: "./system/domains.db"`, written, 1)
		if got := bootError(t, map[string]string{"application.work": manifest, "apps/aaa/own.example/router.work": says("aaa")}); !strings.Contains(got, want) {
			t.Errorf("%s: %s, want %q", written, got, want)
		}
	}
}

// The apps of apps/ are tenants (the first engine's multi-tenant layout, the owner 14/11/2026): with no domains written,
// the table is the host's .database/system/kitwork.db, read when there; not there while a site under apps/ has a public
// name, the boot stops saying so, and the folders never decide a public name of tenants; a host whose sites all have
// local names (a test's, the machine's, a private address) needs none.
//
// CONTROL: with the default not read, aaa's shop.example was served; with a missing default let pass, the host booted,
// the folders deciding shop.example. Each red.
func TestTheTenantsTableIsTheHostsByDefault(t *testing.T) {
	plain := strings.Replace(domainsManifest, `, domains: "./system/domains.db"`, "", 1)
	files := map[string]string{
		"application.work":                  plain,
		"apps/aaa/shop.example/router.work": says("aaa shop.example"),
		"apps/bbb/shop.example/router.work": says("bbb shop.example"),
	}
	folder := write(t, files)
	if got := bootFolder(t, folder); !strings.Contains(got, "the apps of apps are tenants, each public hostname served by the host's table of domains, and ") ||
		!strings.Contains(got, "is not there (aaa/shop.example is a public hostname): make it, CREATE TABLE domain") ||
		!strings.Contains(got, `or say where it is, app.website({ domains: "…" })`) {
		t.Errorf("no table, public names: %s", got)
	}
	writeDomains(t, filepath.Join(folder, ".database", "system", "kitwork.db"), domainRow{"shop.example", "bbb", "confirmed", "", ""})
	b := boot(t, filepath.Join(folder, "application.work"))
	if got := b.get(t, "shop.example", "/"); got.status != http.StatusOK || got.body != "bbb shop.example" {
		t.Errorf("shop.example by the default table: %d %q", got.status, got.body)
	}
	contains(t, "the boot", b.log.String(), "[aaa] the site shop.example is not opened: its row gives it to the app bbb")
	if got := bootError(t, map[string]string{
		"application.work":               plain,
		"apps/aaa/shop.test/router.work": says("aaa"),
		"apps/bbb/127.0.0.1/router.work": says("bbb"),
		"apps/bbb/10.1.2.3/router.work":  says("bbb"),
	}); got != "" {
		t.Errorf("local names only, no table: %s", got)
	}
}

// walWriter is a connection held to the SQLite file in WAL mode, as KitDB's writer holds its file while the host serves
// (the host's database port): a write waits in the -wal, the file itself untouched until a checkpoint, never made here.
func walWriter(t *testing.T, file string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?_pragma=busy_timeout(5000)") // the host may be reading it
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, pragma := range []string{`PRAGMA journal_mode = WAL`, `PRAGMA wal_autocheckpoint = 0`} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// writeAll runs statements on db in one transaction.
func writeAll(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// The table of domains is read again when it changes while the host serves, without app.hotReload (14/11/2026), as the
// first engine asked its system database for each tenant: a write in WAL mode, the file held open as KitDB's writer
// holds it, is read at the next beat. shop.example moves from bbb to aaa, which reads first: both boot again in one
// publish and neither is closed; new.example is confirmed and served; old.example is sent elsewhere; ccc, whose sites
// the change touches not, does not boot again. A table renamed away cannot be read: said, and the host serves by the
// table it read last; renamed back, no row changed and nothing boots.
//
// CONTROL: with the -wal not looked at, shop.example stayed bbb's; with each old boot's sites leaving the map only as
// its own app boots, aaa was closed, shop.example "in two apps"; with a table that cannot be read dropped, old.example
// answered 404. Each red.
func TestTheTableOfDomainsIsReadAgainWhenItChanges(t *testing.T) {
	files := map[string]string{"application.work": domainsManifest}
	for _, site := range []struct{ app, hostname string }{
		{"aaa", "shop.example"}, {"aaa", "own.example"}, {"bbb", "shop.example"}, {"bbb", "new.example"},
		{"bbb", "keep.example"}, {"ccc", "ccc.example"},
	} {
		files["apps/"+site.app+"/"+site.hostname+"/router.work"] = says(site.app + " " + site.hostname)
	}
	folder := write(t, files)
	table := filepath.Join(folder, "system", "domains.db")
	writeDomains(t, table,
		domainRow{"shop.example", "bbb", "confirmed", "", ""},
		domainRow{"own.example", "aaa", "confirmed", "", ""},
		domainRow{"new.example", "bbb", "unverified", "", ""},
		domainRow{"keep.example", "bbb", "confirmed", "", ""},
		domainRow{"ccc.example", "ccc", "confirmed", "", ""},
	)
	admin := walWriter(t, table)
	b := boot(t, filepath.Join(folder, "application.work"))
	b.host.ReloadEvery = 20 * time.Millisecond
	b.host.StartWatching()
	if got := b.get(t, "shop.example", "/"); got.body != "bbb shop.example" {
		t.Fatalf("before: %d %q", got.status, got.body)
	}

	writeAll(t, admin,
		`UPDATE domain SET identity = 'aaa' WHERE hostname = 'shop.example'`,
		`UPDATE domain SET status = 'confirmed' WHERE hostname = 'new.example'`,
		`INSERT INTO domain (hostname, identity, status, redirect_to) VALUES ('old.example', 'bbb', 'redirect', 'keep.example')`)
	eventually(t, "the table read again", func() bool { return b.get(t, "shop.example", "/").body == "aaa shop.example" })
	served := func(when string) {
		t.Helper()
		for hostname, want := range map[string]string{
			"shop.example": "aaa shop.example", "own.example": "aaa own.example", "new.example": "bbb new.example",
			"keep.example": "bbb keep.example", "ccc.example": "ccc ccc.example",
		} {
			if got := b.get(t, hostname, "/"); got.status != http.StatusOK || got.body != want {
				t.Errorf("%s, %s: %d %q, want %q", when, hostname, got.status, got.body, want)
			}
		}
		if got := b.get(t, "old.example", "/a?b=1"); got.status != http.StatusMovedPermanently || got.header.Get("Location") != "http://keep.example/a?b=1" {
			t.Errorf("%s, old.example: %d to %q", when, got.status, got.header.Get("Location"))
		}
	}
	served("read again")
	said := b.log.String()
	contains(t, "the table read again", said,
		"domains: the table changed (new.example, old.example, shop.example): read again",
		"domains: 6 hostnames in ",
		"[aaa] reloaded after the table of domains changed",
		"[bbb] reloaded after the table of domains changed",
		"[bbb] the site shop.example is not opened: its row gives it to the app aaa")
	if strings.Contains(said, "[ccc] reloaded") || strings.Contains(said, "is closed") {
		t.Errorf("an app untouched booted again, or one was closed:\n%s", said)
	}

	writeAll(t, admin, `ALTER TABLE domain RENAME TO kept`)
	eventually(t, "a table not read said", func() bool {
		return strings.Contains(b.log.String(), "the table of domains changed and is not read: ")
	})
	contains(t, "a table not read", b.log.String(), "has no table domain as the host reads it", "the sites stay as the table read last gives them")
	served("a table not read")
	reloads, changes := strings.Count(b.log.String(), "reloaded after"), strings.Count(b.log.String(), "the table changed")
	writeAll(t, admin, `ALTER TABLE kept RENAME TO domain`)
	time.Sleep(300 * time.Millisecond) // fifteen beats
	if said := b.log.String(); strings.Count(said, "reloaded after") != reloads || strings.Count(said, "the table changed") != changes {
		t.Errorf("the table back as it was read booted apps again:\n%s", said)
	}
	served("the table back")
}

// An app closed keeps no hostname the table gives another app (14/11/2026): aaa, read first and closed by its own error,
// has a folder of bbb's shop.example; bbb serves it, aaa's own site answers 503.
//
// CONTROL: with closedSites reading the folders alone, aaa's 503 held shop.example and bbb was closed too, "in two apps".
func TestAnAppClosedTakesNoHostnameOfAnother(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                  domainsManifest,
		"apps/aaa/own.example/router.work":  routerImport + "router.get(1);\n",
		"apps/aaa/shop.example/router.work": says("aaa shop.example"),
		"apps/bbb/shop.example/router.work": says("bbb shop.example"),
	})
	writeDomains(t, filepath.Join(folder, "system", "domains.db"),
		domainRow{"own.example", "aaa", "confirmed", "", ""}, domainRow{"shop.example", "bbb", "confirmed", "", ""})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	b := serve(t, read, log)
	if got := b.get(t, "shop.example", "/"); got.status != http.StatusOK || got.body != "bbb shop.example" {
		t.Errorf("shop.example: %d %q\n%s", got.status, got.body, log)
	}
	if got := b.get(t, "own.example", "/"); got.status != http.StatusServiceUnavailable {
		t.Errorf("own.example of aaa, closed: %d %q", got.status, got.body)
	}
	if failed := read.Failed(); failed == nil || strings.Contains(failed.Error(), "two apps") || strings.Contains(failed.Error(), "bbb") {
		t.Errorf("the apps closed: %v", failed)
	}
}

// A host whose sites all had local names boots with no table (14/11/2026); a public name a reload finds is no one's
// until the table is made, read at the next beat: aaa's shop.example, added while the host serves, is not opened, said
// in the log; the table made, giving it to aaa, it is served.
//
// CONTROL: with no table letting a public name open, shop.example was served by its folder; with a table not there at
// the boot never looked for, it stayed unopened. Each red.
func TestATableMadeAfterTheBootIsRead(t *testing.T) {
	b, folder := reloading(t, map[string]string{"apps/aaa/shop.test/router.work": says("aaa shop.test")})
	rewrite(t, folder, "apps/aaa/shop.example/router.work", says("aaa shop.example"))
	eventually(t, "the reload", func() bool { return strings.Contains(b.log.String(), "[aaa] reloaded after") })
	contains(t, "the reload", b.log.String(), "[aaa] the site shop.example is not opened: the host's table of domains, ",
		"is not there, and only it gives a public hostname to an app")
	if got := b.get(t, "shop.example", "/"); got.status != http.StatusNotFound {
		t.Errorf("shop.example with no table: %d %q", got.status, got.body)
	}
	writeDomains(t, filepath.Join(folder, ".database", "system", "kitwork.db"), domainRow{"shop.example", "aaa", "confirmed", "", ""})
	eventually(t, "the table made", func() bool { return b.get(t, "shop.example", "/").body == "aaa shop.example" })
	contains(t, "the table made", b.log.String(), "domains: the table changed (made: 1 hostnames): read again",
		"[aaa] reloaded after the table of domains changed")
	if got := b.get(t, "shop.test", "/"); got.body != "aaa shop.test" {
		t.Errorf("shop.test: %d %q", got.status, got.body)
	}
}

// The www rule (15/11/2026, the owner): a site is reached by its www. name or its bare one with no row and no word
// for it. www.shop.example goes to shop.example, a site, and blog.example to www.blog.example, a site, 301, the
// scheme, path and query kept; both.example and www.both.example, both sites, each serve itself. The table decides a
// name it has a row for (www.held.example, unverified, sends nowhere), not one whose row is deleted (www.deals.example
// goes to deals.example); a name whose pair is no site is no one's, and a local name is never sent (www.site.test).
func TestAWwwNameGoesToItsSite(t *testing.T) {
	files := map[string]string{"application.work": domainsManifest}
	for _, hostname := range []string{"shop.example", "www.blog.example", "both.example", "www.both.example", "held.example", "deals.example", "site.test"} {
		files["apps/aaa/"+hostname+"/router.work"] = says(hostname)
	}
	folder := write(t, files)
	writeDomains(t, filepath.Join(folder, "system", "domains.db"),
		domainRow{"shop.example", "aaa", "confirmed", "", ""},
		domainRow{"www.blog.example", "aaa", "confirmed", "", ""},
		domainRow{"both.example", "aaa", "confirmed", "", ""},
		domainRow{"www.both.example", "aaa", "confirmed", "", ""},
		domainRow{"held.example", "aaa", "confirmed", "", ""},
		domainRow{"www.held.example", "aaa", "unverified", "", ""},
		domainRow{"deals.example", "aaa", "confirmed", "", ""},
		domainRow{"www.deals.example", "aaa", "confirmed", "", "2026-11-01 10:00:00+07"},
	)
	b := boot(t, filepath.Join(folder, "application.work"))
	for hostname, want := range map[string]string{
		"www.shop.example":  "http://shop.example/a/b%20c?q=1&r=%C4%91",
		"WWW.Shop.Example":  "http://shop.example/a/b%20c?q=1&r=%C4%91",
		"blog.example":      "http://www.blog.example/a/b%20c?q=1&r=%C4%91",
		"www.deals.example": "http://deals.example/a/b%20c?q=1&r=%C4%91",
	} {
		got := b.get(t, hostname, "/a/b%20c?q=1&r=%C4%91")
		if got.status != http.StatusMovedPermanently || got.header.Get("Location") != want {
			t.Errorf("%s: %d to %q, want 301 to %q", hostname, got.status, got.header.Get("Location"), want)
		}
	}
	for _, hostname := range []string{"both.example", "www.both.example", "shop.example", "www.blog.example"} {
		if got := b.get(t, hostname, "/"); got.status != http.StatusOK || got.body != hostname {
			t.Errorf("%s: %d %q, want its own site", hostname, got.status, got.body)
		}
	}
	for _, hostname := range []string{"www.held.example", "www.site.test", "www.nobody.example", "nobody.example", "www.www.shop.example"} {
		if got := b.get(t, hostname, "/"); got.status != http.StatusNotFound {
			t.Errorf("%s: %d to %q, want 404", hostname, got.status, got.header.Get("Location"))
		}
	}
}
