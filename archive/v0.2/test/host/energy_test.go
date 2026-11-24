package host_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// A run spends what the host gives it (Host.RunEnergy, core/runtime work.go, 15/11/2026): ten million units by default,
// some 44 ms of the engine's work, and what a host sets. A request of about three million units of additions answers
// under the default and stops at its energy under one million; the log says the energy.
func TestARunSpendsWhatTheHostGivesIt(t *testing.T) {
	files := map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => {
  let x = 0;
  for (let i = 0; i < 300000; i++) { x = x + i; }
  return context.text("" + x);
});
`,
	}
	if got := boot(t, filepath.Join(write(t, files), "application.work")).get(t, "shop.test", "/"); got.status != 200 {
		t.Fatalf("GET / under the default energy: %d %q", got.status, got.body)
	}
	little := boot(t, filepath.Join(write(t, files), "application.work"), func(h *host.Host) { h.RunEnergy = 1_000_000 })
	if got := little.get(t, "shop.test", "/"); got.status != 500 {
		t.Fatalf("GET / under a million units: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", little.log.String(), "GET / (/router.work): 500:", "Energy Limit Exceeded")
}

// What a module's method is handed it reads, and the text costs the run's energy as a function of the engine's does
// (value.HandedText): a loop of context.query of a name of 10 MB hashed the name each time, a millisecond, for the price
// of a call.
func TestWhatAModuleIsHandedCostsEnergy(t *testing.T) {
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => {
  const name = "q".repeat(10000000);
  let found = 0;
  for (let i = 0; i < 1000; i++) { if (context.query(name) !== "") { found++; } }
  return context.text("" + found);
});
`,
	}), "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 500 {
		t.Fatalf("GET /: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", app.log.String(), "GET / (/router.work): 500:", "Energy Limit Exceeded")
}

// What a module reads costs the run's energy before it is read (value.ParseJSONWithin): file.json of a file of 2 MB
// reads for 2 ms and makes a tree of one item, which a loop did a thousand times for the price of its calls.
func TestWhatAModuleReadsCostsEnergy(t *testing.T) {
	app, _ := filesApp(t, map[string]string{
		".database/files/big.json":   strings.Repeat(" ", 2_000_000) + "[0]",
		"shop.test/read/router.work": fileImport + `router.get((context) => { let n = 0; for (let i = 0; i < 1000; i++) { n = n + file.json("big.json")?.length; } return context.text("" + n); });` + "\n",
	}, func(h *host.Host) { h.RequestTimeout = 3 * time.Second })
	started := time.Now()
	if got := app.get(t, "shop.test", "/read"); got.status != 500 {
		t.Fatalf("GET /read: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", app.log.String(), "GET /read (/read/router.work): 500:", "Energy Limit Exceeded")
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("GET /read took %v", took)
	}
}
