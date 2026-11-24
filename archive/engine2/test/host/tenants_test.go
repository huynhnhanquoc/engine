package host_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	engine2 "github.com/kitwork/engine2"
)

// The host's table tenant is read again when its file changes (slice 4 of root docs/KITWORK_MODULE_WORK.md,
// 17/11/2026), as the table domain of the same file is: an app whose limits a row now gives otherwise boots again, and
// its runs spend what the row says from then; a row gone gives the defaults back.
func TestTheTenantTableIsReadAgainWhenItChanges(t *testing.T) {
	add := routerImport + `router.get((context) => { let x = 0; for (let i = 0; i < 300000; i++) { x = x + i; } return context.text("" + x); });` + "\n"
	folder := write(t, map[string]string{
		"application.work":                  manifest,
		"apps/kho/kho.test/add/router.work": add,
	})
	writeTenants(t, folder)
	admin := walWriter(t, filepath.Join(folder, ".database", "system", "kitwork.db"))
	b := boot(t, filepath.Join(folder, "application.work"))
	b.host.ReloadEvery = 20 * time.Millisecond
	b.host.StartWatching()
	if got := b.get(t, "kho.test", "/add"); got.status != 200 {
		t.Fatalf("on the defaults: %d %q", got.status, got.body)
	}
	writeAll(t, admin, `INSERT INTO tenant (identity, status, energy) VALUES ('kho', 'active', 100000)`)
	eventually(t, "the row read", func() bool { return b.get(t, "kho.test", "/add").status == 500 })
	contains(t, "the log", b.log.String(), "[app] tenant: the table changed (kho): read again", "[kho] reloaded after the table tenant changed")
	writeAll(t, admin, `DELETE FROM tenant WHERE identity = 'kho'`)
	eventually(t, "the row gone", func() bool { return b.get(t, "kho.test", "/add").status == 200 })
}

// kitwork check's map says each tenant's limits and where each comes from: its row of the table tenant, the
// manifest's app.limits, or the engine's default.
func TestTheMapSaysEachTenantsLimits(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                limitsManifest(`app.limits({ energy: 50_000_000 });` + "\n"),
		"apps/kho/kho.test/router.work":   says("kho"),
		"apps/shop/shop.test/router.work": says("shop"),
	})
	writeTenants(t, folder, tenantRow{"kho", "active", "128mb", "", "8"})
	shown, err := engine2.CheckMap(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	plain := regexp.MustCompile(` +`).ReplaceAllString(shown, " ")
	kho, shop, _ := strings.Cut(plain, "app shop")
	for _, want := range []string{"trần memory 128mb bảng tenant", "trần energy 50000000 app.limits, mặc định của host", "trần requests 8 bảng tenant"} {
		if !strings.Contains(kho, want) {
			t.Errorf("kho's limits lack %q:\n%s", want, shown)
		}
	}
	for _, want := range []string{"trần memory 64mb mặc định của engine", "trần energy 50000000 app.limits, mặc định của host"} {
		if !strings.Contains(shop, want) {
			t.Errorf("shop's limits lack %q:\n%s", want, shown)
		}
	}
}

// A tenant's row bounds its app's jobs as it does its sites (17/11/2026): a job adding three hundred thousand numbers
// runs on the defaults, and stops at its energy when the app's row gives 100,000 units.
func TestATenantsJobsRunWithinItsLimits(t *testing.T) {
	job := queueImport + `export const sum = queue.job((context) => { let x = 0; for (let i = 0; i < 300000; i++) { x = x + i; } context.job.log("" + x); });` + "\n"
	for _, c := range []struct {
		row  []tenantRow
		want string
	}{{nil, "completed"}, {[]tenantRow{{"shop", "active", "", "100000", ""}}, "failed"}} {
		quickQueue(t)
		folder := write(t, queueApp(map[string]string{"sum.work": job}))
		writeTenants(t, folder, c.row...)
		b := boot(t, filepath.Join(folder, "application.work"))
		b.host.StartBackground()
		receipt := b.send(t, "sum", "")
		if receipt.Error != nil {
			t.Fatalf("send: %s", receipt.Error)
		}
		done := b.settled(t, "sum", receipt.Value.ID, c.want)
		if c.want == "failed" && (done.Error == nil || !strings.Contains(done.Error.Message, "Energy Limit Exceeded")) {
			t.Errorf("the job of a tenant of 100,000 units: %+v", done)
		}
	}
}
