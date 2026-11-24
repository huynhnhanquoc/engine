package host_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// What a site's file made as it booted is shared by every request of the site, many at once, so a
// request reads it and writes none of it (05/11/2026, runtime Freeze): a top-level object's key a handler
// assigns is a TYPE failure, as a push into a top-level array is. Until then a top-level let's assignment
// ended the host process ("concurrent map writes") and the push lost items; since 18/11/2026 a top-level let is
// a compile error (docs/language.md mục 2.1), so the counter is an object's key.
func TestAFileLevelVariableIsReadNotWrittenByARequest(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `const counter = { count: 0 };
const seen = [];
router.get((context) => { counter.count = counter.count + 1; return context.text("" + counter.count); });
`,
		"apps/shop/shop.test/push/router.work": routerImport + `const seen = [];
router.get((context) => { seen.push(1); return context.text("" + seen.length); });
`,
		"apps/shop/shop.test/read/router.work": routerImport + `const prices = { tea: 3 };
const order = (a, b) => a - b;
router.get((context) => context.text([prices.tea, [3, 1, 2].sort(order).join("")].join(" ")));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	var group sync.WaitGroup
	for g := 0; g < 8; g++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 25; i++ {
				for path, want := range map[string]int{"/": 500, "/push": 500, "/read": 200} {
					if got := app.get(t, "shop.test", path); got.status != want {
						t.Errorf("GET %s: %d %q, want %d", path, got.status, got.body, want)
						return
					}
				}
			}
		}()
	}
	group.Wait()
	contains(t, "the log", app.log.String(),
		"[shop.test] GET / (/router.work): 500: ", "cannot assign to read only property 'count'",
		"[shop.test] GET /push (/push/router.work): 500: ", "cannot add property 0, object is not extensible")
	if got := app.get(t, "shop.test", "/read"); got.body != "3 123" {
		t.Errorf("GET /read: %q, want what the file made, read: 3 123", got.body)
	}
}

// A site's file imports only its app's files (KITWORK_UI.md mục 4, KITWORK_ENVIRONMENT.md mục 1): a
// relative path out of the app, an absolute one, or a _core/ found by walking up out of it is a boot error
// (05/11/2026; until then alpha's router.work read beta's code and compiled the host's .env as its own,
// quoting its secrets in the boot error). Since 18/11/2026 a _core/ specifier is no import form at all.
func TestAnAppImportsOnlyItsOwnFiles(t *testing.T) {
	for name, source := range map[string]string{
		"another app's file":       `import { token } from "../../beta/core/keys.work";`,
		"the host's .env":          `import "../../../host.env";`,
		"the host's library":       `import { host } from "../../../hostlib.work";`,
		"a _core above the app":    `import { core } from "_core/shared.work";`,
		"an absolute path":         `import { token } from "` + filepath.ToSlash(filepath.Join(t.TempDir(), "x.work")) + `";`,
		"a root-relative absolute": `import { token } from "/keys.work";`,
	} {
		got := bootError(t, map[string]string{
			"host.env":                        "JWT_SECRET=eyJhbGci.SECRETPART.sig\n",
			"hostlib.work":                    "export const host = \"host-only\";\n",
			"apps/_core/shared.work":          "export const core = \"core\";\n",
			"apps/beta/core/keys.work":        "export const token = \"beta-only-token\";\n",
			"apps/beta/beta.test/router.work": routerImport + "router.get((context) => context.text(\"beta\"));\n",
			"apps/shop/shop.test/router.work": source + "\n" + routerImport + "router.get((context) => context.text(\"x\"));\n",
		})
		// the parser refuses some spellings before the bundler sees them; either way the boot stops
		if got == "" || !strings.Contains(got, "ra ngoài app") && !strings.Contains(got, "chỉ import được") || strings.Contains(got, "SECRETPART") {
			t.Errorf("%s: the boot ends with %q, want the error of an import out of the app", name, got)
		}
	}
	if got := bootError(t, map[string]string{
		"apps/shop/core/keys.work":        "export const token = \"shop\";\n",
		"apps/shop/shop.test/router.work": `import { token } from "../core/keys.work";` + "\n" + routerImport + "router.get((context) => context.text(token));\n",
	}); got != "" {
		t.Errorf("a relative import inside the app: the boot ends with %q", got)
	}
}

// A request runs for at most its time (Host.RequestTimeout, 30 seconds by default), whatever
// energy it has left: one instruction over a large array costs one unit of energy, so a request within its
// energy ran for minutes (05/11/2026).
func TestARequestStopsAtItsTime(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => {
  const items = "x".repeat(1000000).split("");
  for (let i = 0; i < 20000; i++) { items.reverse(); }
  return context.text("done");
});
`,
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	read.RequestTimeout = 300 * time.Millisecond
	read.RunEnergy = longEnergy // a reverse costs its items (core/runtime, work.go): the default ends the loop first
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "shop.test"
	read.ServeHTTP(recorder, request)
	if took := time.Since(started); recorder.Code != 500 || took > 5*time.Second {
		t.Errorf("a request past its time: %d after %v, want 500 soon after 300ms", recorder.Code, took)
	}
	contains(t, "the log", log.String(), "500: ", "deadline exceeded")
}

// The server reads a request, its body included, for at most Host.ReadTimeout (30 seconds by
// default): a body announced and never sent held the handler, its goroutine and a VM of the site for as
// long as the client liked (05/11/2026).
func TestABodyThatNeverComesEndsAtTheReadTimeout(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/router.work": routerImport + "router.post((context) => context.text(context.request.text()));\n",
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	read.ReadTimeout = 500 * time.Millisecond
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	server, err := read.Server()
	if err != nil {
		t.Fatal(err)
	}
	if server.ReadTimeout != 500*time.Millisecond || server.ReadHeaderTimeout > server.ReadTimeout || server.IdleTimeout == 0 {
		t.Errorf("the server reads for %v (headers %v, idle %v)", server.ReadTimeout, server.ReadHeaderTimeout, server.IdleTimeout)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	fmt.Fprint(connection, "POST / HTTP/1.1\r\nHost: shop.test\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nab")
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatalf("no answer 5 seconds after a body that never comes (the server reads for 500ms): %v", err)
	}
	if strings.Contains(status, "200") {
		t.Errorf("a body that never came is answered %q", status)
	}
}

// The manifest names every key env.require misses in one error, as a site's boot does
// (docs/KITWORK_ENVIRONMENT.md mục 3); it stopped at its first (05/11/2026).
func TestTheManifestNamesEveryMissingKeyAtOnce(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
import * as file from "kitwork/file";
const env = file.env("./.env");
const one = env.require("FIX_MISSING_ONE");
const two = env.require("FIX_MISSING_TWO");
app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`,
	})
	_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "FIX_MISSING_ONE (application.work:4:") || !strings.Contains(err.Error(), "FIX_MISSING_TWO (application.work:5:") {
		t.Errorf("the manifest's read ends with %v, want one error naming both keys with their places", err)
	}
}

// A site's napas import is made once, as the site boots, and every request chains from it: each request's
// payload is its own (05/11/2026; a step wrote into the import, so a request without an amount or a note
// got the ones of the request before it).
func TestEachRequestsNapasChainIsItsOwn(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": "import * as napas from \"kitwork/napas\";\n" + routerImport + `router.get((context) => {
  let chain = napas.bank("970415", "1234567890");
  if (context.query("amount") !== "") { chain = chain.amount(context.query("amount")); }
  if (context.query("note") !== "") { chain = chain.info(context.query("note")); }
  return context.text(chain.payload());
});
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	first := app.get(t, "shop.test", "/?amount=150000&note=ORDER-ALICE")
	second := app.get(t, "shop.test", "/")
	if first.status != 200 || !strings.Contains(first.body, "ORDER-ALICE") {
		t.Fatalf("GET with an amount and a note: %d %q", first.status, first.body)
	}
	if second.status != 200 || strings.Contains(second.body, "ORDER-ALICE") || strings.Contains(second.body, "150000") {
		t.Errorf("GET without them carries the request before's: %d %q", second.status, second.body)
	}
}
