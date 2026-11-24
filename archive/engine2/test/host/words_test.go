package host_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/file"
	"github.com/kitwork/engine2/library/router"
)

// Four words of KITWORK_ROUTER.md served as it says (05/11/2026; each booted or read more loosely):
// a handler that takes a function out of context is a boot error (mục 5, "Chỉ tách object, không tách
// hàm"); router.cache(…) and router.persist(…) stand only before a door (mục 3); kitwork/router is imported
// only by router.work and config.work (mục 3 and 11); request.hostname is the host without its port.
func TestTheRoutersWordsAreServedAsTheSpecSays(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"a function taken out of context": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get(({ text }) => text(\"x\"));\n",
		}, "takes text out of context"},
		"a function of request taken out": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get(({ request: { json } }) => json());\n",
		}, "takes request: { json } out of context"},
		"a cache with no door": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.cache(\"5m\");\nrouter.get((context) => context.text(\"x\"));\n",
		}, "router.cache(…) at shop.test/router.work:2:"},
		"a cache that ends the file": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"x\"));\nrouter.persist(\"1h\");\n",
		}, "stands before a door and none follows it"},
		"a cache before a word that is no door": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.cache(\"5m\").language(\"en\");\n",
		}, "stands before a door"},
		"the router imported by a library": {map[string]string{
			"apps/shop/core/doors.work":       routerImport + "router.get((context) => context.text(\"from the library\"));\nexport const ready = true;\n",
			"apps/shop/shop.test/router.work": "import { ready } from \"app/core/doors.work\";\n",
		}, "kitwork/router is imported by "},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", name, got, c.want)
		}
	}
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport +
			"router.cache(\"5m\").persist(\"1h\").get(({ request, response }) => response.text(request.hostname + \" \" + request.host));\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test:8080", "/"); got.status != 200 || got.body != "shop.test shop.test:8080" {
		t.Errorf("GET / of shop.test:8080: %d %q, want shop.test shop.test:8080", got.status, got.body)
	}
}

// env.KEY is coerced once, as its file is read, and a read allocates nothing; context.query(…) parses
// the URL's query once a request; a Host with no port, most requests', allocates no error (05/11/2026:
// the three were a sixth of a request that read three keys and two parameters).
func TestARequestsReadsAllocateOnce(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "app.env"), []byte("SMTP=smtp://mail\nPORT=8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := (&file.File{Root: folder, Here: func() string { return "" }, Booting: func() bool { return true }}).Env(value.String("./app.env"))
	if env.IsInvalid() {
		t.Fatal(env.Failure())
	}
	if got := testing.AllocsPerRun(100, func() { env.Get("SMTP"); env.Get("PORT") }); got != 0 {
		t.Errorf("reading two keys of env allocates %.0f times, want none", got)
	}
	request := router.NewRequest(httptest.NewRequest(http.MethodGet, "/?q=kitwork&page=2&sort=new", nil), nil)
	query := []value.Value{value.String("q")}
	request.Query(query...)
	if got := testing.AllocsPerRun(100, func() { request.Query(query...) }); got > 1 {
		t.Errorf("a second query(…) of a request allocates %.0f times, want at most its text", got)
	}
	app := boot(t, testApp)
	served := func(host string) func() {
		request := httptest.NewRequest(http.MethodGet, "/users/me", nil)
		request.Host = host
		return func() { app.host.ServeHTTP(httptest.NewRecorder(), request) }
	}
	bare, ported := testing.AllocsPerRun(50, served("alpha.test")), testing.AllocsPerRun(50, served("alpha.test:8080"))
	if !underRace && bare > ported {
		t.Errorf("a request to alpha.test allocates %.0f times, one to alpha.test:8080 %.0f: a Host with no port costs an error", bare, ported)
	}
	// the run's deadline is kept with the site's VM (host/deadline.go): context.WithTimeout cost a
	// request three allocations and a timer; GET /users/me allocated 23 times before both (a recorder
	// included, the request made once)
	if !underRace && bare > 22 {
		t.Errorf("GET /users/me allocates %.0f times, want at most 22", bare)
	}
}
