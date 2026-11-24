package host_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The test app answers each kind of route the slice serves, over HTTP: a text, JSON, a parameter of each
// kind, a redirect, a page it cannot draw yet (501), no route (404), a method not declared (405), HEAD,
// OPTIONS, a write from another site (403), a body past its limit (413).
func TestASiteAnswersItsRoutes(t *testing.T) {
	app := boot(t, testApp)
	for _, c := range []struct {
		method, path, body string
		headers            []string
		status             int
		mediaType          string
		want               string // the body, or what it holds when it starts with "~"
		header             string // name=value the answer carries
	}{
		{method: "GET", path: "/", status: 200, mediaType: "text/plain; charset=utf-8", want: "Xin chào from alpha.test"},
		{method: "GET", path: "/api?q=kitwork", status: 200, mediaType: "application/json; charset=utf-8",
			want: `{"site":"alpha.test","shared":"site","number":42,"zip":"0123","host":null,"system":null,"query":"kitwork"}`},
		{method: "GET", path: "/users/42", status: 200, want: `{"id":42,"kind":"number"}`},
		{method: "GET", path: "/users/42/", status: 200, want: `{"id":42,"kind":"number"}`},
		{method: "GET", path: "//users//42", status: 200, want: `{"id":42,"kind":"number"}`},
		{method: "GET", path: "/users/me", status: 200, want: "me"},
		{method: "GET", path: "/users/abc", status: 200, want: `{"name":"abc","kind":"string"}`},
		{method: "GET", path: "/files/a/b/c.txt", status: 200, want: "a/b/c.txt"},
		{method: "GET", path: "/old", status: 301, header: "Location=/"},
		{method: "GET", path: "/nope", status: 404, want: "Not Found\n"},
		{method: "GET", path: "/users", status: 404, want: "Not Found\n"},
		{method: "GET", path: "/_drafts", status: 404, want: "Not Found\n"},
		{method: "GET", path: "/view", status: 200, mediaType: "text/html; charset=utf-8", want: "~<h1>Hello alpha</h1>"},
		{method: "GET", path: "/page", status: 200, mediaType: "text/html; charset=utf-8", want: "~A page of .ui, drawn by engine2"},
		{method: "POST", path: "/users/42", status: 405, header: "Allow=GET, HEAD, OPTIONS", want: "Method Not Allowed\n"},
		{method: "OPTIONS", path: "/api", status: 204, header: "Allow=GET, HEAD, POST, OPTIONS"},
		{method: "HEAD", path: "/", status: 200, mediaType: "text/plain; charset=utf-8", want: ""},
		{method: "POST", path: "/api", body: `{"item":"cà phê","quantity":2}`, headers: []string{"Content-Type", "application/json"},
			status: 201, want: `{"received":{"item":"cà phê","quantity":2},"method":"POST"}`},
		{method: "POST", path: "/api", body: `{}`, headers: []string{"Sec-Fetch-Site", "cross-site"}, status: 403, want: "Forbidden\n"},
		{method: "POST", path: "/api", body: `{}`, headers: []string{"Origin", "https://evil.example"}, status: 403, want: "Forbidden\n"},
		{method: "POST", path: "/api", body: `"` + strings.Repeat("x", 1<<20) + `"`, status: 413, want: "Request Entity Too Large\n"},
	} {
		name := c.method + " " + c.path
		var body *strings.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		}
		var got reply
		if body != nil {
			got = app.request(t, c.method, "alpha.test", c.path, body, c.headers...)
		} else {
			got = app.request(t, c.method, "alpha.test", c.path, nil, c.headers...)
		}
		if got.status != c.status {
			t.Errorf("%s: status %d, want %d (body %q)", name, got.status, c.status, got.body)
		}
		if c.mediaType != "" && got.header.Get("Content-Type") != c.mediaType {
			t.Errorf("%s: Content-Type %q, want %q", name, got.header.Get("Content-Type"), c.mediaType)
		}
		if want, ok := strings.CutPrefix(c.want, "~"); ok {
			contains(t, name, got.body, want)
		} else if c.want != "" || c.method == "HEAD" {
			if got.body != c.want {
				t.Errorf("%s: body %q, want %q", name, got.body, c.want)
			}
		}
		if headerName, headerValue, ok := strings.Cut(c.header, "="); ok && got.header.Get(headerName) != headerValue {
			t.Errorf("%s: %s %q, want %q", name, headerName, got.header.Get(headerName), headerValue)
		}
	}
}

// A handler that fails answers 500 with no word of the failure: where it is written and why go to the
// log, led by the site, never to the visitor.
func TestAFailingHandlerAnswers500AndLogsWhy(t *testing.T) {
	app := boot(t, testApp)
	got := app.get(t, "alpha.test", "/broken")
	if got.status != 500 || got.body != "Internal Server Error\n" {
		t.Fatalf("GET /broken: %d %q, want 500 \"Internal Server Error\\n\"", got.status, got.body)
	}
	for _, secret := range []string{"null", "router.work", "missing", "TYPE"} {
		if strings.Contains(got.body, secret) {
			t.Errorf("the answer tells the visitor %q: %q", secret, got.body)
		}
	}
	contains(t, "the log", app.log.String(),
		"[alpha.test] GET /broken (/broken/router.work): 500: TYPE: router.work:7:",
		"null")
}

// Two sites of two apps keep their own: each console leads its lines with its own domain, each Date reads
// its app's zone (alpha's Asia/Ho_Chi_Minh, beta's UTC), each sees its own environment and nothing of the
// other's, of the host's, or of the system's.
func TestTwoSitesKeepTheirOwnConsoleClockAndEnvironment(t *testing.T) {
	t.Setenv("KITWORK_SLICE_SYSTEM", "the system's")
	app := boot(t, testApp)

	alpha := app.get(t, "alpha.test", "/")
	beta := app.get(t, "beta.test", "/")
	if alpha.status != 200 || beta.status != 200 {
		t.Fatalf("alpha %d %q, beta %d %q", alpha.status, alpha.body, beta.status, beta.body)
	}
	log := app.log.String()
	contains(t, "the log", log,
		"[alpha.test] [console.log] router.work of alpha.test is read\n",
		"[alpha.test] [console.log] hello from alpha.test\n",
		"[beta.test] [console.log] hello from beta.test\n")
	for _, crossed := range []string{"[alpha.test] [console.log] hello from beta.test", "[beta.test] [console.log] hello from alpha.test"} {
		if strings.Contains(log, crossed) {
			t.Errorf("a site's console wrote under the other's name: %q", crossed)
		}
	}

	var clock map[string]any
	decode(t, app.get(t, "alpha.test", "/clock").body, &clock)
	if clock["newYear"] != "2025-12-31T17:00:00.000Z" || clock["parsed"] != float64(1767200400000) || clock["now"] != "number" {
		t.Errorf("alpha's clock: %v, want new year at 2025-12-31T17:00:00.000Z (UTC+7) and parsed 1767200400000", clock)
	}
	var other map[string]any
	decode(t, beta.body, &other)
	want := map[string]any{"site": "beta.test", "greeting": "Hello", "shared": "beta-app", "alpha": nil, "newYear": "2026-01-01T00:00:00.000Z"}
	if !reflect.DeepEqual(other, want) {
		t.Errorf("beta: %v, want %v", other, want)
	}

	var environment map[string]any
	decode(t, app.get(t, "alpha.test", "/api").body, &environment)
	if environment["shared"] != "site" || environment["number"] != float64(42) || environment["zip"] != "0123" ||
		environment["host"] != nil || environment["system"] != nil {
		t.Errorf("alpha's environment: %v; want the site's SHARED over the app's, 42 a number, 0123 a text, no host key, no system variable", environment)
	}
}

func decode(t testing.TB, body string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("not JSON: %q: %v", body, err)
	}
}

// console and Date of a site are built once, at boot: a request reads the values the file read when it ran
// at boot, frozen.
func TestASitesConsoleAndDateAreBuiltOnce(t *testing.T) {
	app := boot(t, testApp)
	for range 3 {
		if got := app.get(t, "alpha.test", "/same"); got.body != `{"console":true,"date":true,"frozen":true}` {
			t.Fatalf("GET /same: %d %q", got.status, got.body)
		}
	}
}

// A site's console and Date are what its program reads: from their slots, where the compiler binds a builtin's
// name (04/11/2026). A file no longer reads one by name: a function that assigned the name read it so, and a name
// a file assigns undeclared is its compile error (15/11/2026), as JavaScript throws there. The app's zone is
// UTC+14, which no machine running the tests is in, so the shared Date (the machine's zone) cannot pass for the
// site's.
func TestASitesConsoleAndDateAreReadBySlot(t *testing.T) {
	answer := "context.text(new Date(2026, 0, 1).toISOString())"
	folder := write(t, map[string]string{
		"application.work":      manifest,
		"apps/shop/module.work": zonedModule("Pacific/Kiritimati"),
		"apps/shop/shop.test/router.work": routerImport +
			"router.get((context) => { console.log(\"by slot\"); return " + answer + "; });\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 200 || got.body != "2025-12-31T10:00:00.000Z" {
		t.Errorf("GET /: %d %q, want the new year of UTC+14, 2025-12-31T10:00:00.000Z", got.status, got.body)
	}
	contains(t, "the log", app.log.String(), "[shop.test] [console.log] by slot\n")
	failed := bootError(t, map[string]string{
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => { console = null; return context.text(\"x\"); });\n",
	})
	contains(t, "the boot", failed, "router.work:2:27: console = … gán vào một tên chưa khai")
}

// A request reaches the site its Host names, its port and case aside; with allowLocal (host.env), the
// machine itself reaches the website's hostname and <domain>.localhost reaches <domain>. Another host is 404.
func TestARequestReachesTheSiteItsHostNames(t *testing.T) {
	app := boot(t, testApp)
	for host, want := range map[string]string{
		"alpha.test":                 "Xin chào from alpha.test",
		"ALPHA.test:8199":            "Xin chào from alpha.test",
		"alpha.test.":                "Xin chào from alpha.test",
		"alpha.test.localhost:8199":  "Xin chào from alpha.test",
		"localhost:8199":             "Xin chào from alpha.test",
		"127.0.0.1":                  "Xin chào from alpha.test",
		"[::1]:8199":                 "Xin chào from alpha.test",
		"beta.test.localhost":        `"site":"beta.test"`,
		"nobody.test":                "Not Found",
		"nobody.test.localhost:8199": "Not Found",
	} {
		got := app.get(t, host, "/")
		if !strings.Contains(got.body, want) {
			t.Errorf("Host %s: %d %q, want %q", host, got.status, got.body, want)
		}
	}
}

// Requests of two sites at once each get their own answer, and the race detector sees nothing shared
// written: a handler's file-level values are read by every request, never written.
func TestRequestsOfTwoSitesRunAtOnce(t *testing.T) {
	app := boot(t, testApp)
	var group sync.WaitGroup
	failures := make(chan string, 256)
	for worker := range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := range 8 {
				id := worker*100 + round
				checks := []struct{ host, path, want string }{
					{"alpha.test", "/", "Xin chào from alpha.test"},
					{"alpha.test", fmt.Sprintf("/users/%d", id), fmt.Sprintf(`{"id":%d,"kind":"number"}`, id)},
					{"beta.test", "/", `"greeting":"Hello"`},
					{"alpha.test", "/clock", `"newYear":"2025-12-31T17:00:00.000Z"`},
				}
				for _, check := range checks {
					request, _ := http.NewRequest(http.MethodGet, app.server.URL+check.path, nil)
					request.Host = check.host
					response, err := app.server.Client().Do(request)
					if err != nil {
						failures <- err.Error()
						continue
					}
					var body strings.Builder
					_, _ = fmt.Fprint(&body, readAll(response))
					if !strings.Contains(body.String(), check.want) {
						failures <- fmt.Sprintf("%s%s: %q, want %q", check.host, check.path, body.String(), check.want)
					}
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func readAll(response *http.Response) string {
	defer response.Body.Close()
	var body strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, err := response.Body.Read(buffer)
		body.Write(buffer[:n])
		if err != nil {
			return body.String()
		}
	}
}
