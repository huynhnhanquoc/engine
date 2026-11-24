package host_test

import (
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// limitsManifest is manifest with app.limits(…) lines before app.run().
func limitsManifest(lines string) string {
	return strings.Replace(manifest, "app.run(", lines+"app.run(", 1)
}

// tenantRow is a row of the host's table tenant: identity, status, memory, energy, requests; "" writes NULL.
type tenantRow [5]string

// writeTenants makes the host's system database of the manifest in folder, at its default place,
// .database/system/kitwork.db: the table domain, empty, and the table tenant with rows (17/11/2026, slice 4).
func writeTenants(t *testing.T, folder string, rows ...tenantRow) {
	t.Helper()
	file := filepath.Join(folder, ".database", "system", "kitwork.db")
	writeDomains(t, file)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tenant (identity TEXT PRIMARY KEY, status TEXT DEFAULT 'active', memory TEXT, energy INTEGER, requests INTEGER)`); err != nil {
		t.Fatal(err)
	}
	null := func(text string) any {
		if text == "" {
			return nil
		}
		return text
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO tenant (identity, status, memory, energy, requests) VALUES (?, ?, ?, ?, ?)`,
			row[0], null(row[1]), null(row[2]), null(row[3]), null(row[4])); err != nil {
			t.Fatal(err)
		}
	}
}

// app.limits({ memory, energy, requests, host }) sets every tenant's limits (12/11/2026, the fourth slice; energy
// 15/11/2026): the host reads them into RunBytes, RunEnergy, SiteRequests and HostBytes.
func TestAppLimitsAreRead(t *testing.T) {
	folder := write(t, map[string]string{"application.work": limitsManifest(`app.limits({ memory: "32mb", energy: 50_000_000, requests: 4, host: "1gb" });
`)})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if read.RunBytes != 32<<20 || read.RunEnergy != 50_000_000 || read.SiteRequests != 4 || read.HostBytes != 1<<30 {
		t.Errorf("the host's limits: memory %d, energy %d, requests %d, host %d", read.RunBytes, read.RunEnergy, read.SiteRequests, read.HostBytes)
	}
}

// Each wrong way of writing app.limits(…) stops the manifest's read at its line, saying how it is written; a site's
// own, app.limits("site", { … }) until 17/11/2026, names the table tenant.
func TestAppLimitsWrittenWrong(t *testing.T) {
	for line, want := range map[string]string{
		`app.limits({ memory: "100kb" });`:                             `memory is what one run may make, from "1mb"`,
		`app.limits({ requests: 0 });`:                                 "requests is how many requests a site runs at once",
		`app.limits({ requests: 2.5 });`:                               "requests is how many requests a site runs at once",
		`app.limits({ energy: 99999 });`:                               "energy is what one run may spend, a whole number of units from 100_000",
		`app.limits({ energy: 2_000_000_000_000 });`:                   "energy is what one run may spend",
		`app.limits({ energy: 1500000.5 });`:                           "energy is what one run may spend",
		`app.limits({ energy: "10ms" });`:                              "energy is what one run may spend",
		`app.limits({ cpu: 2 });`:                                      `app.limits(…) has no "cpu": it takes memory, energy, requests, host`,
		`app.limits({});`:                                              "names no limit",
		`app.limits({ memory: "64mb", host: "32mb" });`:                "host 32mb is less than one run's memory 64mb",
		`app.limits({ memory: "64mb" }); app.limits({ requests: 4 });`: "app.limits({ … }) is declared twice",
		`app.limits("kho.test", { requests: 2 });`:                     `app.limits("kho.test", { … }) is gone (17/11/2026): a tenant's own limits are its row in the host's table tenant`,
		`app.limits(4);`:                                               `app.limits({ memory: "64mb", energy: 50_000_000, requests: 32, host: "2gb" })`,
	} {
		folder := write(t, map[string]string{"application.work": limitsManifest(line + "\n")})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "application.work:") {
			t.Errorf("%s: %v, want an error at its line saying %q", line, err, want)
		}
	}
}

// A tenant's own limits, its row of the host's table tenant, apply to every site of its app, app.limits({ … }) to
// every other: the app kho may make 2 MB a run, the app shop the host's 32 MB, so a request making 3 MB stops on
// kho.test alone; kho.test runs one request at once, so a second waits and is a 503 while shop.test, of the host's
// four, answers.
func TestATenantsOwnLimitsApplyToIt(t *testing.T) {
	up := newHeldUp(t)
	http := "import * as http from \"kitwork/http\";\n"
	make3 := routerImport + `router.get((context) => { const held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); } return context.text("" + held.length); });` + "\n"
	folder := write(t, map[string]string{
		"application.work":                      limitsManifest(`app.limits({ memory: "32mb", requests: 4 });` + "\n"),
		"apps/shop/shop.test/make/router.work":  make3,
		"apps/kho/kho.test/make/router.work":    make3,
		"apps/kho/kho.test/held/router.work":    http + routerImport + `router.get((context) => context.text(http.get("` + up.server.URL + `/hold").send().text));` + "\n",
		"apps/kho/kho.test/quick/router.work":   routerImport + `router.get((context) => context.text("quick"));` + "\n",
		"apps/shop/shop.test/quick/router.work": routerImport + `router.get((context) => context.text("quick"));` + "\n",
	})
	writeTenants(t, folder, tenantRow{"kho", "active", "2mb", "", "1"})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	read.SiteWait = 100 * time.Millisecond
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	b := serve(t, read, log)
	if got := b.get(t, "shop.test", "/make"); got.status != 200 || got.body != "3000" {
		t.Errorf("3 MB on shop.test, of the host's 32: %d %q, want 200", got.status, got.body)
	}
	if got := b.get(t, "kho.test", "/make"); got.status != 500 {
		t.Errorf("3 MB on kho.test, of its tenant's 2: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", b.log.String(), "[kho.test] GET /make (/make/router.work): 500:", "Memory Limit Exceeded")

	held := make(chan int, 1)
	go func() { held <- b.get(t, "kho.test", "/held").status }()
	deadline := time.Now().Add(5 * time.Second)
	for up.at.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := b.get(t, "kho.test", "/quick"); got.status != 503 {
		t.Errorf("a second request on kho.test, of one at once: %d, want 503", got.status)
	}
	if got := b.get(t, "shop.test", "/quick"); got.status != 200 {
		t.Errorf("shop.test while kho.test is full: %d, want 200", got.status)
	}
	close(up.release)
	<-held
}

// A site's file at boot makes no more than its tenant's runs: kho.test's router.work making 3 MB at its top level does
// not boot under its tenant's 2 MB, and its app is closed saying so.
func TestASitesFileBootsWithinItsTenantsLimit(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/kho/kho.test/router.work": routerImport + `const held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); }
router.get((context) => context.text("" + held.length));
`,
	})
	writeTenants(t, folder, tenantRow{"kho", "", "2mb", "", ""})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	failed := read.Failed()
	if failed == nil {
		t.Fatal("kho booted under 2 MB")
	}
	contains(t, "the boot", failed.Error(), "router.work", "Memory Limit Exceeded")
}

// A row that applies nothing is said at boot, and so is a row of an identity no app has: a status other than active,
// a limit written wrong; the tenant runs on the defaults.
func TestATenantRowThatAppliesNothingIsSaid(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("shop"));` + "\n",
		"apps/kho/kho.test/router.work":   routerImport + `router.get((context) => context.text("kho"));` + "\n",
		"apps/bad/bad.test/router.work":   routerImport + `router.get((context) => context.text("bad"));` + "\n",
	})
	writeTenants(t, folder,
		tenantRow{"nowhere", "active", "", "", "2"},
		tenantRow{"kho", "suspended", "2mb", "", ""},
		tenantRow{"bad", "active", "100kb", "", ""},
	)
	b := boot(t, filepath.Join(folder, "application.work"))
	contains(t, "the boot", b.log.String(),
		"[app] tenant nowhere: no app of that identity is under the apps: its limits are no app's",
		`[app] tenant kho: its status is "suspended", not "active": its row applies nothing, the app runs on the defaults`,
		`[app] tenant bad: memory "100kb" is no size from 1mb to 16gb: its row applies nothing`)
	for _, hostname := range []string{"shop.test", "kho.test", "bad.test"} {
		if got := b.get(t, hostname, "/"); got.status != 200 {
			t.Errorf("%s: %d, want it to serve on the defaults", hostname, got.status)
		}
	}
}

// A tenant's own energy is what each run of its app spends (15/11/2026, its row since 17/11/2026): under the host's
// million units, a request of some three million units of additions stops at its energy on shop.test, and answers on
// kho.test, whose tenant has twenty million; the log says the energy.
func TestATenantsOwnEnergyAppliesToIt(t *testing.T) {
	add := routerImport + `router.get((context) => { let x = 0; for (let i = 0; i < 300000; i++) { x = x + i; } return context.text("" + x); });` + "\n"
	folder := write(t, map[string]string{
		"application.work":                    limitsManifest(`app.limits({ energy: 1_000_000 });` + "\n"),
		"apps/shop/shop.test/add/router.work": add,
		"apps/kho/kho.test/add/router.work":   add,
	})
	writeTenants(t, folder, tenantRow{"kho", "active", "", "20000000", ""})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/add"); got.status != 500 {
		t.Errorf("three million units on shop.test, of the host's million: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", app.log.String(), "[shop.test] GET /add (/add/router.work): 500:", "Energy Limit Exceeded")
	if got := app.get(t, "kho.test", "/add"); got.status != 200 || got.body != "44999850000" {
		t.Errorf("three million units on kho.test, of its tenant's twenty million: %d %q, want 200", got.status, got.body)
	}
}

// A site's file at boot spends no more than its tenant's runs: kho.test's router.work adding three hundred thousand
// numbers at its top level does not boot within the least energy, 100,000 units, and its app is closed saying so.
func TestASitesFileBootsWithinItsTenantsEnergy(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/kho/kho.test/router.work": routerImport + `const x = (() => { let s = 0; for (let i = 0; i < 300000; i++) { s = s + i; } return s; })();
router.get((context) => context.text("" + x));
`,
	})
	writeTenants(t, folder, tenantRow{"kho", "active", "", "100000", ""})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	failed := read.Failed()
	if failed == nil {
		t.Fatal("kho booted within 100,000 units")
	}
	contains(t, "the boot", failed.Error(), "router.work", "Energy Limit Exceeded")
}
