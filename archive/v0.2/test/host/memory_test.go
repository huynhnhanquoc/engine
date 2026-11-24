package host_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kitwork/engine2/internal/loopback"
)

// What a request makes is bounded, as what it spends (core/runtime, memory.go, 12/11/2026): 64 MiB a request, its
// guards and its door together. Past it the request is a 500 whose log line says the memory limit, and the site goes on
// answering: one request cannot take the host's memory and every tenant's with it.
func TestARequestStopsAtWhatItMakes(t *testing.T) {
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/big/router.work":   routerImport + `router.get((context) => { const t = "y".repeat(70000000); return context.text("big"); });` + "\n",
		"apps/shop/shop.test/many/router.work":  routerImport + `router.get((context) => { const a = []; for (let i = 0; i < 100; i++) { a.push("y".repeat(1000000) + i); } return context.text("many"); });` + "\n",
		"apps/shop/shop.test/small/router.work": routerImport + `router.get((context) => context.text("small"));` + "\n",
	}), "application.work"))
	for _, path := range []string{"/big", "/many", "/small", "/many", "/small"} {
		got := app.get(t, "shop.test", path)
		want := 500
		if path == "/small" {
			want = 200
		}
		if got.status != want {
			t.Errorf("GET %s: %d %q, want %d", path, got.status, got.body, want)
		}
	}
	contains(t, "the log", app.log.String(), "GET /big (/big/router.work): 500:", "GET /many (/many/router.work): 500:", "Memory Limit Exceeded")
}

// A request's guards and its door make one request's bytes together: two runs of 40 MB each pass alone, both are over.
func TestAGuardAndItsDoorShareTheBytes(t *testing.T) {
	heavy := `const t = "y".repeat(40000000);`
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/guard/router.work": routerImport + "router.guard((context) => { " + heavy + " return context.next(); }).get((context) => context.text(\"guard\"));\n",
		"apps/shop/shop.test/door/router.work":  routerImport + "router.get((context) => { " + heavy + " return context.text(\"door\"); });\n",
		"apps/shop/shop.test/both/router.work":  routerImport + "router.guard((context) => { " + heavy + " return context.next(); }).get((context) => { " + heavy + " return context.text(\"both\"); });\n",
	}), "application.work"))
	for path, status := range map[string]int{"/guard": 200, "/door": 200, "/both": 500} {
		if got := app.get(t, "shop.test", path); got.status != status {
			t.Errorf("%s: %d, want %d", path, got.status, status)
		}
	}
	contains(t, "the log", app.log.String(), "GET /both (/both/router.work): 500:", "Memory Limit Exceeded")
}

// What a module gives is counted whole, everything inside it: a JSON file of 20,000 texts read thirty times holds some
// 80 MB though each read is one call, and is over; read once, it is not.
func TestWhatAModuleGivesIsCountedWhole(t *testing.T) {
	items := make([]string, 20000)
	for index := range items {
		items[index] = strings.Repeat("x", 100)
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	app, _ := filesApp(t, map[string]string{
		".database/files/big.json":     string(data),
		"shop.test/once/router.work":   fileImport + `router.get((context) => context.text("" + file.json("big.json")?.length));` + "\n",
		"shop.test/thirty/router.work": fileImport + `router.get((context) => { const held = []; for (let i = 0; i < 30; i++) { held.push(file.json("big.json")); } return context.text("" + held.length); });` + "\n",
	}, longWork) // what is read costs its energy (core/runtime, work.go): thirty parses of 2 MB pass the default first
	if got := app.get(t, "shop.test", "/once"); got.status != 200 || got.body != "20000" {
		t.Errorf("GET /once: %d %q, want 200 20000", got.status, got.body)
	}
	if got := app.get(t, "shop.test", "/thirty"); got.status != 500 {
		t.Errorf("GET /thirty: %d %q, want 500", got.status, got.body)
	}
	contains(t, "the log", app.log.String(), "GET /thirty (/thirty/router.work): 500:", "Memory Limit Exceeded")
}

// A file of a site makes no more than a request as the site boots: the app is closed, saying where, and the host
// boots on.
func TestABootThatMakesTooMuchClosesItsApp(t *testing.T) {
	failed := bootError(t, map[string]string{
		"apps/shop/shop.test/router.work": routerImport + `const t = "y".repeat(70000000);` + "\n" + `router.get((context) => context.text("never"));` + "\n",
	})
	contains(t, "the boot", failed, "router.work", "Memory Limit Exceeded")
}

// A module writes what a run hands it within that run's ceiling (value.Budget, 14/11/2026): context.json, text, html
// and svg, an HTTP body as JSON, a JSON column. A site of 2 MB answered context.json of 10 MB, an array holding one
// text of 10 KB a thousand times: what a module made was counted by no run (the answer is a struct, the text inside
// it), and the most it made was 64 MiB, whatever the site's ceiling. Each is now the run's memory limit, a 500, and
// nothing was sent or written; the site answers the next request.
func TestAModuleWritesWithinTheRunsCeiling(t *testing.T) {
	t.Cleanup(loopback.Open()) // the upstream is this machine's
	var sent atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		sent.Add(n)
		_, _ = w.Write([]byte("ok"))
	}))
	defer up.Close()
	shared := `const s = "x".repeat(10000); const a = []; for (let i = 0; i < 1000; i++) { a.push(s); } `
	page := func(body string) string {
		return routineImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => { ` + shared + body + ` });
`
	}
	files := map[string]string{
		"application.work":                       limitsManifest(`app.limits({ memory: "2mb" });` + "\n"),
		"apps/shop/core/schema/shop.work":        writeSchema,
		"apps/shop/shop.test/json/router.work":   page(`return context.json(a);`),
		"apps/shop/shop.test/text/router.work":   page(`return context.text(a);`),
		"apps/shop/shop.test/html/router.work":   page(`return context.html(a);`),
		"apps/shop/shop.test/svg/router.work":    page(`return context.svg(a);`),
		"apps/shop/shop.test/send/router.work":   page(`http.post("` + up.URL + `/take").json(a).send(); return context.text("sent");`),
		"apps/shop/shop.test/insert/router.work": page(`const u = db.insert((users) => ({ email: "an@x.vn", username: "an", name: "An", passwordHash: "h" })); db.insert((orders) => ({ userId: u.id, item: "x", amount: 1, meta: a })); return context.text("written");`),
		"apps/shop/shop.test/count/router.work":  page(`return context.json(db.count((orders) => true));`),
		"apps/shop/shop.test/small/router.work":  page(`return context.json({ ok: a.length });`),
	}
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	for _, path := range []string{"/json", "/text", "/html", "/svg", "/send", "/insert"} {
		if got := app.get(t, "shop.test", path); got.status != 500 || len(got.body) > 4096 {
			t.Errorf("GET %s: %d with %d bytes, want the 500 of the memory limit", path, got.status, len(got.body))
		}
		line := ""
		for _, each := range strings.Split(app.log.String(), "\n") {
			if strings.Contains(each, "GET "+path+" ("+path+"/router.work): 500:") {
				line = each
			}
		}
		if !strings.Contains(line, "Memory Limit Exceeded") {
			t.Errorf("GET %s: the log says %q, want the memory limit", path, line)
		}
	}
	if n := sent.Load(); n != 0 {
		t.Errorf("the upstream got %d bytes, want nothing sent", n)
	}
	if got := app.get(t, "shop.test", "/count"); got.status != 200 || strings.TrimSpace(got.body) != "0" {
		t.Errorf("GET /count: %d %q, want no order written", got.status, got.body)
	}
	if got := app.get(t, "shop.test", "/small"); got.status != 200 || !strings.Contains(got.body, "1000") {
		t.Errorf("GET /small: %d %q, want the site answering", got.status, got.body)
	}
}

// A module reads JSON within the run's ceiling too (value.Budget, 14/11/2026): the body of a request, the answer of
// an HTTP call, a JSON file. A text of "{}," is some 90 bytes of tree for each 3 of its own, so a body of 1 MiB made a
// tree of 30 MB before what the module gave was counted, then stopped. Each now stops as the tree grows: the same
// 500, a few MB made, Go's own count.
func TestAModuleReadsWithinTheRunsCeiling(t *testing.T) {
	t.Cleanup(loopback.Open()) // the upstream is this machine's
	tree := "[" + strings.TrimSuffix(strings.Repeat("{},", 340_000), ",") + "]"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tree))
	}))
	defer up.Close()
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                        limitsManifest(`app.limits({ memory: "2mb" });` + "\n"),
		"apps/shop/.database/files/tree.json":     tree,
		"apps/shop/shop.test/body/router.work":    routerImport + `router.post((context) => context.json(context.request.json().length));` + "\n",
		"apps/shop/shop.test/fetched/router.work": routineImport + `router.get((context) => context.json(http.get("` + up.URL + `/tree").send().data.length));` + "\n",
		"apps/shop/shop.test/file/router.work":    fileImport + `router.get((context) => context.json(file.json("tree.json")?.length));` + "\n",
	}), "application.work"))
	for _, path := range []string{"/body", "/fetched", "/file"} {
		var body io.Reader
		method := http.MethodGet
		if path == "/body" {
			method, body = http.MethodPost, strings.NewReader(tree)
		}
		goruntime.GC()
		var before, after goruntime.MemStats
		goruntime.ReadMemStats(&before)
		got := app.request(t, method, "shop.test", path, body, "Content-Type", "application/json")
		goruntime.ReadMemStats(&after)
		if got.status != 500 {
			t.Errorf("%s %s: %d %q, want the 500 of the memory limit", method, path, got.status, got.body)
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
			t.Errorf("%s %s: allocated %d MB, want the tree stopped as it grew", method, path, allocated>>20)
		}
	}
	contains(t, "the log", app.log.String(), "Memory Limit Exceeded")
}
