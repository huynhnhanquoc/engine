package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

// The layout of one app (slice 6 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026; docs/module.md mục 7): a folder with
// module.work runs alone, `cd shop && kitwork`, with no application.work.

// aloneModule is the module.work of an app run alone, of one site at its root.
const aloneModule = `import * as module from "kitwork/module";
`

// readAlone reads the app of folder as kitwork in that folder does: application.work beside module.work, which may not be
// there.
func readAlone(t *testing.T, folder string) (*host.Host, *lines) {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatalf("read the app alone: %v\n%s", err, log)
	}
	return read, log
}

// An app with no application.work runs on the engine's defaults: the website on port 8080 of this machine alone
// (127.0.0.1), no AutoSSL, its one site at its root answering localhost and 127.0.0.1; the log says so, and kitwork
// check's map has its site. No table of the host is read: its folder has none, and none is asked for.
//
// CONTROL (17/11/2026): with host.ReadWithLog compiling the absent application.work (alone() not called), the read
// failed "open …application.work: The system cannot find the file specified"; with Server listening on every address,
// the address was ":8080".
func TestAnAppRunsAloneWithoutApplicationWork(t *testing.T) {
	folder := write(t, map[string]string{
		"module.work": aloneModule,
		"router.work": routerImport + "router.get((context) => context.text(\"alone\"));\n",
		".env":        "SECRET=1\n",
	})
	read, log := readAlone(t, folder)
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	if err := read.Failed(); err != nil {
		t.Fatalf("the app closed: %v\n%s", err, log)
	}
	b := serve(t, read, log)
	for _, name := range []string{"localhost", "127.0.0.1", "localhost:8080"} {
		if got := b.get(t, name, "/"); got.status != http.StatusOK || got.body != "alone" {
			t.Errorf("%s: %d %q", name, got.status, got.body)
		}
	}
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusNotFound {
		t.Errorf("a name the app has not: %d %q", got.status, got.body)
	}
	contains(t, "the log", log.String(), "no application.work in "+folder+": the app of its module.work runs alone, on the engine's defaults: port 8080 of this machine alone, no AutoSSL")
	server, err := read.Server()
	if err != nil || server.Addr != "127.0.0.1:8080" {
		t.Errorf("the server of an app alone: %v, %v; want 127.0.0.1:8080, this machine alone", server, err)
	}
	identity := filepath.Base(folder)
	contains(t, "the map", spaced(read.Map()), "app "+identity, "site localhost ./", "trần memory 64mb mặc định của engine")
	// the app's root is its site: its manifest, its .env and its code are never files it serves
	for _, path := range []string{"/.env", "/module.work", "/router.work", "/application.work"} {
		if got := b.get(t, "localhost", path); got.status != http.StatusNotFound {
			t.Errorf("%s of the app alone: %d %q", path, got.status, got.body)
		}
	}
}

// spaced is text with every run of spaces one space: the map's columns, whatever their width.
func spaced(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// An app alone that writes module.routes has a site for each folder at its root named as a hostname, public or local:
// no table gives the public ones, since the app is the only one. Each is reached as <name>.localhost (a .test name as
// itself); a folder of one label is code, no site. With one site, localhost answers it.
//
// CONTROL (17/11/2026): with siteNamesBy reading the table for the app alone (oneAppNames not called), shop.example
// answered 404, the folder of a public name given by no table; with site not asking onlySite, localhost answered 404 to
// the app of one site.
func TestAnAppAloneServesEachFolderNamedAsAHostname(t *testing.T) {
	folder := write(t, map[string]string{
		"module.work":              routedModule,
		"shop.example/router.work": routerImport + "router.get((context) => context.text(\"shop\"));\n",
		"blog.test/router.work":    routerImport + "router.get((context) => context.text(\"blog\"));\n",
		"core/words.work":          "export const words = 1;\n",
	})
	read, log := readAlone(t, folder)
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	if err := read.Failed(); err != nil {
		t.Fatalf("the app closed: %v\n%s", err, log)
	}
	b := serve(t, read, log)
	for name, want := range map[string]string{"shop.example.localhost": "shop", "shop.example": "shop", "blog.test": "blog", "blog.test.localhost": "blog"} {
		if got := b.get(t, name, "/"); got.status != http.StatusOK || got.body != want {
			t.Errorf("%s: %d %q, want %q", name, got.status, got.body, want)
		}
	}
	for _, name := range []string{"core.localhost", "localhost"} {
		if got := b.get(t, name, "/"); got.status != http.StatusNotFound {
			t.Errorf("%s: %d %q; two sites: the machine answers neither, core/ is no site", name, got.status, got.body)
		}
	}
	contains(t, "the map", spaced(read.Map()), "site blog.test blog.test/", "site shop.example shop.example/")

	one := write(t, map[string]string{
		"module.work":              routedModule,
		"shop.example/router.work": routerImport + "router.get((context) => context.text(\"shop\"));\n",
	})
	read, log = readAlone(t, one)
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	b = serve(t, read, log)
	if got := b.get(t, "localhost", "/"); got.status != http.StatusOK || got.body != "shop" {
		t.Errorf("localhost, the app's one site: %d %q", got.status, got.body)
	}
}

// application.work beside module.work is the app's own configuration, read as a host's: its website, its databases. Its
// folder is the app, the only one: app.website's hostname names its one site with no table of domains (a public name
// that under apps/ needs one), every database of app.databases is open to it with no apps, and app.root is an error.
//
// CONTROL (17/11/2026): with oneAppNames giving the site of one app localhost always, shop.example answered 404; with
// grantedDatabases asking apps of one app, kitwork migrate refused the app, warehouse "chưa cấp cho app"; with
// moduleFiles reading application.work as the app's code, it refused it "kitwork/application is the host's manifest's".
func TestApplicationWorkBesideModuleWorkIsTheAppsOwn(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.website({ hostname: "shop.example", port: 8443, allowLocal: true });
app.databases({ warehouse: { sqlite: "./data/warehouse.db" } });
app.run();
`,
		"module.work": aloneModule,
		"core/stock.work": `import { struct, id, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const stock = struct({ id: id(), qty: integer() });
export const warehouse = connection("warehouse").tables({ stock });
`,
		"router.work": routerImport + `import { warehouse } from "app/core/stock.work";
router.get((context) => context.json({ count: warehouse.count((stock) => stock.qty > 0) }));
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 || !strings.Contains(said, "warehouse") {
		t.Fatalf("kitwork migrate of the app alone: %d\n%s", code, said)
	}
	b := boot(t, filepath.Join(folder, "application.work"))
	for _, name := range []string{"shop.example", "localhost"} {
		if got := b.get(t, name, "/"); got.status != http.StatusOK || got.body != `{"count":0}` {
			t.Errorf("%s: %d %q", name, got.status, got.body)
		}
	}
	if server, err := b.host.Server(); err != nil || server.Addr != ":8443" {
		t.Errorf("the server of the app's own manifest: %v, %v; want :8443, as allowLocal opens it", server, err)
	}
	if strings.Contains(b.log.String(), "no application.work") {
		t.Errorf("an app with its manifest said it runs without one:\n%s", b.log)
	}

	// a folder of apps and a table of domains are a host's: beside module.work, each is an error
	for written, want := range map[string]string{
		`app.root("apps");`: "app.root(…): module.work beside application.work makes its folder the app, the only one",
		`app.website({ hostname: "shop.example", port: 8080, domains: "./domains.db" });`: "app.website({ domains }): module.work beside application.work makes its folder the app, the only one",
	} {
		website := `app.website({ hostname: "shop.example", port: 8080 });`
		if strings.HasPrefix(written, "app.website") {
			website, written = written, ""
		}
		rooted := write(t, map[string]string{
			"application.work": "import * as app from \"kitwork/application\";\n" + written + "\n" + website + "\napp.run();\n",
			"module.work":      aloneModule,
		})
		read, err := host.ReadWithLog(filepath.Join(rooted, "application.work"), &lines{})
		if err != nil {
			t.Fatal(err)
		}
		if err := read.Boot(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s beside module.work: %v", written+website, err)
		}
	}
}

// A database of the host, connection("shopping"), in an app run alone with no application.work: no manifest declares
// it, and the app's boot says where to (KITWORK_MODULE_WORK câu hỏi 8): the code stays as it is, the runner adds an
// application.work beside it. A manifest read after it is a host again, its message the host's.
//
// CONTROL (17/11/2026): with alone() not calling database.SetAlone(true), the boot said "host không có database tên đó;
// app.databases của application.work không khai tên nào".
func TestAHostsDatabaseAloneSaysWhereToDeclareIt(t *testing.T) {
	folder := write(t, map[string]string{
		"module.work": aloneModule,
		"core/shopping.work": `import { struct, id, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const orders = struct({ id: id(), amount: integer() });
export const shopping = connection("shopping").tables({ orders });
`,
	})
	read, log := readAlone(t, folder)
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	want := `connection("shopping") là database của host: chạy một mình thì không có. Thêm application.work cạnh module.work: app.databases({ shopping: { sqlite: "./shopping.db" } })`
	if err := read.Failed(); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("the app alone, a host database: %v\n%s", err, log)
	}
	if got := bootError(t, map[string]string{"apps/shop/core/shopping.work": `import { struct, id, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const orders = struct({ id: id(), amount: integer() });
export const shopping = connection("shopping").tables({ orders });
`}); !strings.Contains(got, "host không có database tên đó") {
		t.Errorf("a host after the app alone: %q, want the host's message", got)
	}
}

// An app that ran alone, moved under apps/ of a host with its application.work, runs there unchanged (KITWORK_MODULE_WORK
// mục 2): its manifest is not its code, and the host's replaces it, as the log says. kitwork database of an app alone
// with no application.work has no port to open, and says what opens one.
//
// CONTROL (17/11/2026): with moduleFiles reading application.work at the app's root as its code, the app closed,
// "kitwork/application is the host's manifest's: only application.work imports it".
func TestAnAppMovedUnderAHostKeepsItsManifestUnread(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/shop/application.work":      "import * as app from \"kitwork/application\";\napp.website({ hostname: \"shop.example\", port: 9090 });\napp.run();\n",
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"moved\"));\n",
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusOK || got.body != "moved" {
		t.Errorf("the app moved under the host: %d %q", got.status, got.body)
	}
	contains(t, "the log", b.log.String(), "[shop] application.work at the app's root is its own when it runs alone; under this host it is not read: application.work says what the app has")

	alone := write(t, map[string]string{"module.work": aloneModule})
	read, _ := readAlone(t, alone)
	if err := read.ServeDatabase(); err == nil || !strings.Contains(err.Error(), "the app runs alone and opens no database port; app.database(…) in an application.work beside module.work opens one") {
		t.Errorf("kitwork database of an app alone: %v", err)
	}
}
