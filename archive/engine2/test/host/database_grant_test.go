package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

// grantFiles are two apps, shop (shop.test) and kho (kho.test), each declaring tables on the host's warehouse, under
// a manifest whose catalogue line is databases.
func grantFiles(databases string) map[string]string {
	schema := `import { struct, id, string, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

export const stock = struct({ id: id(), item: string(), qty: integer() });

export const warehouse = connection("warehouse").tables({ stock });
`
	route := routerImport + `import { warehouse } from "app/core/schema/stock.work";
router.get((context) => context.json({ count: warehouse.count((stock) => stock.qty > 0) }));
`
	return map[string]string{
		"application.work":                 strings.Replace(manifest, "app.run();", databases+"\napp.run();", 1),
		"apps/shop/core/schema/stock.work": schema,
		"apps/shop/shop.test/router.work":  route,
		"apps/kho/core/schema/stock.work":  schema,
		"apps/kho/kho.test/router.work":    route,
	}
}

// A database of the host's catalogue is opened only by the apps its apps name (11/11/2026): an app it does not name
// that declares tables on it does not boot, its sites 503, its log line saying how the manifest gives it; the app it
// names serves; kitwork migrate refuses the same app the same way. Before, every app opened every name, one app's
// data within reach of every other's.
//
// CONTROL (11/11/2026): with grantedDatabases not called, kho booted and read shop's warehouse.
func TestAHostsDatabaseIsOnlyForTheAppsItNames(t *testing.T) {
	folder := write(t, grantFiles(`app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.test"] } });`))
	code, said := runMigrate(t, folder, true, "")
	if code == 0 || !strings.Contains(said, `connection("warehouse"): database warehouse của host chưa cấp cho app kho`) {
		t.Errorf("kitwork migrate of an app not given the name: %d\n%s", code, said)
	}
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	b := serve(t, read, log)
	contains(t, "the boot", log.String(), `connection("warehouse"): database warehouse của host chưa cấp cho app kho`,
		`app.databases({ warehouse: { …, apps: ["kho.test"] } })`)
	if got := b.get(t, "kho.test", "/"); got.status != http.StatusServiceUnavailable {
		t.Errorf("kho.test, not given the warehouse: %d %s", got.status, got.body)
	}
	if got := b.get(t, "shop.test", "/"); strings.Contains(log.String(), "shop is closed") || got.status == http.StatusServiceUnavailable {
		t.Errorf("shop, given the warehouse, was closed: %d\n%s", got.status, log)
	}
}

// apps names an app by a hostname of one of its sites or by its folder, or every app by "*"; given so, each app boots
// and reads the host's database.
func TestAHostsDatabaseIsGivenByHostnameFolderOrAll(t *testing.T) {
	for _, line := range []string{
		`app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.test", "kho"] } });`,
		`app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: "*" } });`,
	} {
		folder := write(t, grantFiles(line))
		if code, said := runMigrate(t, folder, true, ""); code != 0 {
			t.Fatalf("%s: kitwork migrate: %d\n%s", line, code, said)
		}
		b := boot(t, filepath.Join(folder, "application.work"))
		for _, site := range []string{"shop.test", "kho.test"} {
			if got := b.get(t, site, "/"); got.status != http.StatusOK || got.body != `{"count":0}` {
				t.Errorf("%s: %s: %d %s", line, site, got.status, got.body)
			}
		}
	}
}

// A name in apps no app serves is said at boot; apps written wrong stops the manifest's read at its line.
func TestTheAppsOfAHostsDatabaseAreWrittenRight(t *testing.T) {
	folder := write(t, grantFiles(`app.databases({ warehouse: { sqlite: "./data/warehouse.db", apps: ["shop.test", "kho", "nope.test"] } });`))
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	b := boot(t, filepath.Join(folder, "application.work"))
	contains(t, "the boot", b.log.String(), "app.databases warehouse gives apps nope.test, which no app serves")

	for written, want := range map[string]string{
		`apps: []`:                   "app.databases.warehouse.apps names the apps that may open it",
		`apps: 5`:                    "app.databases.warehouse.apps names the apps that may open it",
		`apps: "shop.test"`:          "app.databases.warehouse.apps names the apps that may open it",
		`apps: ["*", "shop.test"]`:   `"*" gives it to every app, written alone`,
		`apps: ["a.test", "A.test"]`: "app.databases.warehouse.apps names a.test twice",
		`apps: ["https://a.test"]`:   "app.databases.warehouse.apps names the apps that may open it",
	} {
		folder := write(t, map[string]string{
			"application.work": strings.Replace(manifest, "app.run();", `app.databases({ warehouse: { sqlite: "./w.db", `+written+` } });`+"\napp.run();", 1),
			"apps/.keep":       "",
		})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", written, err, want)
		}
	}
}
