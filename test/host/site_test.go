package host_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/huynhnhanquoc/engine/host"
)

// Site của một request (host/site.go, blueprint/router; 24/11/2026, lát 2): routes chọn thư mục, router.work của mỗi
// thư mục là một route, luật all(…) đi xuống các thư mục con, methods(…) chỉ ở thư mục của nó, luật đứng trước một cửa
// chỉ của cửa ấy. Mỗi luật một ca: tắt luật thì ca ấy đỏ.

// site dựng một thư mục tạm với các file ấy (đường dẫn dấu /, tính từ thư mục của application.work), boot application.work
// của nó và nạp cửa website.
func site(t *testing.T, files map[string]string) (*host.Website, *lines, *lines) {
	t.Helper()
	folder := t.TempDir()
	for name, source := range files {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return door(t, folder, files["application.work"])
}

// shopApplication là application.work của các ca: guard đặt user từ header X-User, routes là apps/a/shop.vn.
const shopApplication = `import { application } from "application";
return application().website()
  .guard((context) => context.next({ user: context.request.header("X-User") ?? "" }))
  .routes("apps/a/shop.vn")
  .finally((context) => console.log(context.response.status, context.response.error === null ? "" : context.response.error.code))
  .listen(8080).run();`

const session = `export const signedIn = (context) => context.request.user !== "" ? context.next() : context.status(401).error("sign in");
export const staff = (context) => context.request.user === "staff" ? context.next() : context.error("staff only");`

func newRequest(method, target string) *http.Request { return httptest.NewRequest(method, target, nil) }

func record(w http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	w.ServeHTTP(rec, r)
	return rec
}

// call là một request method tới site; user là header X-User ("" là không có), ip là máy gửi.
func call(w *host.Website, method, path, user, ip string) (int, string, map[string]string) {
	r := newRequest(method, "https://shop.vn"+path)
	if user != "" {
		r.Header.Set("X-User", user)
	}
	r.RemoteAddr = ip + ":4000"
	rec := record(w, r)
	return rec.Code, rec.Body.String(), map[string]string{
		"Content-Type": rec.Header().Get("Content-Type"), "Allow": rec.Header().Get("Allow"),
		"Retry-After": rec.Header().Get("Retry-After"), "Location": rec.Header().Get("Location"),
	}
}

func TestTheRulesOfARoute(t *testing.T) {
	w, console, log := site(t, map[string]string{
		"application.work":         shopApplication,
		"apps/a/core/session.work": session,
		"apps/a/shop.vn/router.work": `import { router } from "router";
const route = router({ meta: { title: "Shop" } });
route.get((context) => context.text("home"));
return route;`,
		"apps/a/shop.vn/san-pham/router.work": `import { router } from "router";
import { signedIn, staff } from "app/core/session.work";
const route = router({ meta: { title: "Sản phẩm" } });
route.limit({ rate: 3, per: "1m" }).guard(signedIn).all("*");
route.guard(staff).methods("post", "delete");
route.get((context) => context.json({ items: [1, 2], user: context.request.user }));
route.limit({ rate: 1, per: "1m" }).post((context) => context.status(201).json({ created: true }));
route.delete((context) => context.status(204).text(""));
return route;`,
		"apps/a/shop.vn/san-pham/anh/router.work": `import { router } from "router";
const route = router();
route.get((context) => context.text("images"));
route.post((context) => context.text("uploaded"));
return route;`,
		"apps/a/shop.vn/san-pham/empty/readme.txt": "a folder without router.work",
		"apps/a/shop.vn/.hidden/router.work":       `import { router } from "router"; const route = router(); route.get((c) => c.text("x")); return route;`,
		"apps/a/shop.vn/_partials/router.work":     `import { router } from "router"; const route = router(); route.get((c) => c.text("x")); return route;`,
	})
	for i, c := range []struct {
		method, path, user, ip string
		status                 int
		body                   string
		header, want           string
	}{
		{"GET", "/", "", "198.51.100.1", 200, "home", "Content-Type", "text/plain; charset=utf-8"},
		{"GET", "/san-pham", "", "198.51.100.1", 401, "sign in", "", ""}, // all("*") của chính thư mục
		{"GET", "/san-pham/", "u", "198.51.100.2", 200, `{"items":[1,2],"user":"u"}`, "Content-Type", "application/json; charset=utf-8"},
		{"POST", "/san-pham", "u", "198.51.100.2", 403, "staff only", "", ""},           // methods(…)
		{"POST", "/san-pham", "staff", "198.51.100.3", 201, `{"created":true}`, "", ""}, //
		{"POST", "/san-pham", "staff", "198.51.100.3", 429, "", "Retry-After", "60"},    // limit trước cửa: 1 mỗi phút
		{"DELETE", "/san-pham", "staff", "198.51.100.4", 204, "", "", ""},               //
		{"GET", "/san-pham/anh", "", "198.51.100.5", 401, "sign in", "", ""},            // all("*") đi xuống
		{"POST", "/san-pham/anh", "u", "198.51.100.5", 200, "uploaded", "", ""},         // methods(…) không đi xuống
		{"HEAD", "/san-pham/anh", "", "198.51.100.6", 401, "", "", ""},                  // HEAD đi theo GET
		{"HEAD", "/san-pham/anh", "u", "198.51.100.6", 200, "", "", ""},                 //
		{"PUT", "/san-pham", "u", "198.51.100.7", 405, "PUT is not a door of /san-pham", "Allow", "GET, HEAD, POST, DELETE, OPTIONS"},
		{"OPTIONS", "/san-pham", "", "198.51.100.7", 204, "", "Allow", "GET, HEAD, POST, DELETE, OPTIONS"},
		{"GET", "/san-pham/empty", "u", "198.51.100.8", 404, "no route for /san-pham/empty", "", ""},
		{"GET", "/nope", "", "198.51.100.8", 404, "no route for /nope", "", ""},
		{"GET", "/.hidden", "", "198.51.100.8", 404, "no route for /.hidden", "", ""},
		{"GET", "/_partials", "", "198.51.100.8", 404, "no route for /_partials", "", ""},
	} {
		status, body, headers := call(w, c.method, c.path, c.user, c.ip)
		if status != c.status || (c.body != "" && !strings.HasPrefix(body, c.body)) || (c.header != "" && headers[c.header] != c.want) {
			t.Errorf("%d: %s %s (%q): %d %q, %s %q", i, c.method, c.path, c.user, status, body, c.header, headers[c.header])
		}
		if c.method == "HEAD" && body != "" {
			t.Errorf("%d: HEAD has a body %q", i, body)
		}
	}
	// limit của all("*"): 3 mỗi phút cho mỗi máy, đếm cả nhánh
	for i, want := range []int{200, 200, 200, 429} {
		path := []string{"/san-pham", "/san-pham/anh", "/san-pham", "/san-pham/anh"}[i]
		if status, _, _ := call(w, "GET", path, "u", "203.0.113.50"); status != want {
			t.Errorf("the branch limit, request %d: %d, want %d", i+1, status, want)
		}
	}
	if status, _, _ := call(w, "GET", "/san-pham", "u", "203.0.113.51"); status != 200 {
		t.Errorf("another client has its own count: %d", status)
	}
	if got := log.String(); got != "" {
		t.Errorf("the engine logged %q", got)
	}
	if printed := console.take(); !strings.Contains(printed, "[console.log] 401 REFUSED\n") || !strings.Contains(printed, "[console.log] 429 TOO_MANY_REQUESTS\n") ||
		!strings.Contains(printed, "[console.log] 200 \n") || !strings.Contains(printed, "[console.log] 404 NOT_FOUND\n") {
		t.Errorf("the functions after sending: %q", printed)
	}
}

// Câu trả lời của một cửa và của một guard của site: text, json, status(n).…, redirect trong cùng host; một cửa không
// trả câu trả lời, hay dùng chữ không phải của nó, là 500 nêu chỗ; console.log của site có đầu dòng [tên site].
func TestTheAnswersOfASite(t *testing.T) {
	w, console, log := site(t, map[string]string{
		"application.work": shopApplication,
		"apps/a/shop.vn/router.work": `import { router } from "router";
const route = router();
route.guard((context) => context.request.query("as") === "guard" ? context.status(402).json({ pay: true }) : context.next()).methods("*");
route.get((context) => {
  const as = context.request.query("as");
  console.log("door", as);
  if (as === "redirect") { return context.redirect("/login"); }
  if (as === "away") { return context.redirect("https://la.vn/"); }
  if (as === "status") { return context.status(418).text("tea"); }
  if (as === "nothing") { return; }
  if (as === "next") { return context.next(); }
  if (as === "error") { return context.error("no"); }
  if (as === "undefined") { return context.json(undefined); }
  if (as === "number") { return context.text(5); }
  if (as === "301") { return context.status(301).json({}); }
  return context.json(null);
});
return route;`,
	})
	for _, c := range []struct {
		as, location, body, logged string
		status                     int
	}{
		{"guard", "", `{"pay":true}`, "", 402},
		{"redirect", "/login", "", "", 302},
		{"status", "", "tea", "", 418},
		{"", "", "null", "", 200},
		{"away", "", "Internal Server Error", "a site redirects within its host", 500},
		{"nothing", "", "Internal Server Error", "the get door of shop.vn/router.work returned undefined (no return?)", 500},
		{"next", "", "Internal Server Error", "context.next is a guard's word", 500},
		{"error", "", "Internal Server Error", "context.error is a guard's word", 500},
		{"undefined", "", "Internal Server Error", "context.json(data): undefined has no JSON", 500},
		{"number", "", "Internal Server Error", "context.text(source) takes a string, got number", 500},
		{"301", "", "Internal Server Error", "a 3xx is a redirect", 500},
	} {
		status, body, headers := call(w, "GET", "/?as="+c.as, "", "198.51.100.1")
		logged := log.take()
		if status != c.status || headers["Location"] != c.location || body != c.body || !strings.Contains(logged, c.logged) {
			t.Errorf("as %q: %d %q Location %q\nlogged %q", c.as, status, body, headers["Location"], logged)
		}
		if c.status == 500 && !strings.Contains(logged, "shop.vn/router.work:") {
			t.Errorf("as %q: no place in %q", c.as, logged)
		}
	}
	if printed := console.take(); !strings.Contains(printed, "[shop.vn] [console.log] door redirect\n") {
		t.Errorf("the console of the site: %q", printed)
	}
}

// Các lỗi của một router.work: mỗi ca một site riêng (một site biên dịch một lần), request tới nó là 500 và log nêu lỗi
// với chỗ của nó.
func TestTheErrorsOfARouterWork(t *testing.T) {
	for _, c := range []struct{ name, source, logged string }{
		{"a chain without an end", `route.guard((c) => c.next());`, `chain 1, route.guard(…), has no end: write .all("*"), .methods("*") or a door`},
		{"all without methods", `route.guard((c) => c.next()).all();`, `shop.vn/router.work:3:30: .all() names the methods`},
		{"a star with a name", `route.guard((c) => c.next()).methods("*", "get");`, `.methods("*") is every method already`},
		{"a method in upper case", `route.guard((c) => c.next()).all("GET");`, `.all("GET"): the methods are get, post, put, patch, delete`},
		{"a name twice", `route.guard((c) => c.next()).methods("get", "get");`, `.methods(…) names "get" twice`},
		{"all with no rule", `route.all("*");`, `.all(…) ends a chain of rules: write route.guard(fn).all(…)`},
		{"cache before a post", `route.cache("5m").post((c) => c.text("x"));`, `keeps the answer of a GET: write it before .get(…), not before .post(…)`},
		{"cache before methods", `route.cache("5m").guard((c) => c.next()).methods("*");`, `keeps the answer of one GET door`},
		{"a door twice", `route.get((c) => c.text("a")); route.get((c) => c.text("b"));`, `route.get(…) is written twice in this router.work`},
		{"rules after a door", `route.get((c) => c.text("a")).guard((c) => c.next());`, `chain 2, route.guard(…), has no end`},
		{"a guard that is not a function", `route.guard("staff").all("*");`, `route.guard(fn) takes a function`},
		{"a bad limit", `route.limit({ rate: 3, per: "1 minute" }).all("*");`, `route.limit({ per }): "1 minute" is not a time`},
		{"a limit without per", `route.limit({ rate: 3 }).all("*");`, `rate and per are both written`},
		{"a bad cache time", `route.cache("soon").get((c) => c.text("x"));`, `route.cache(time): "soon" is not a time`},
		{"no return", `route.get((c) => c.text("a")); return 5;`, `shop.vn/router.work: must return router(…), got number`},
		{"an import outside the app", `import { x } from "../../../application.work";`, `the file is outside the app's folder`},
		{"the old library", `import * as old from "kitwork/router";`, `kitwork/router`},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := c.source
			if !strings.HasPrefix(source, "import") {
				source = "import { router } from \"router\";\nconst route = router();\n" + source + "\nreturn route;"
			}
			w, _, log := site(t, map[string]string{"application.work": shopApplication, "apps/a/shop.vn/router.work": source})
			status, body, _ := call(w, "GET", "/", "", "198.51.100.1")
			if logged := log.take(); status != 500 || body != "Internal Server Error" || !strings.Contains(logged, c.logged) {
				t.Errorf("%d %q\nlogged %q\nwant   %q", status, body, logged, c.logged)
			}
		})
	}
}

// routes, memory, energy của website: thư mục ra ngoài, không có gốc app, không có là 500; trần của lượt chạy của site
// là memory, energy (hàm đọc request được), và code của site không ghi được vào thứ mọi request cùng đọc.
func TestTheHostChoosesTheSite(t *testing.T) {
	routes := func(folder string) string {
		return `import { application } from "application";
return application().website().routes(` + folder + `)
  .memory((context) => context.request.query("memory") === "small" ? "1mb" : "64mb")
  .energy((context) => context.request.query("energy") === "small" ? 1000 : 10000000).listen(8080).run();`
	}
	files := map[string]string{
		"apps/a/shop.vn/router.work": `import { router } from "router";
const route = router();
let seen = 0;
route.get((context) => {
  const what = context.request.query("do");
  if (what === "grow") { let s = "x"; for (let i = 0; i < 40; i++) { s = s + s; } return context.text(s); }
  if (what === "loop") { let n = 0; for (let i = 0; i < 100000; i++) { n = n + 1; } return context.text("done " + n); }
  if (what === "count") { seen = seen + 1; }
  if (what === "route") { route.get((c) => c.text("again")); }
  return context.text("ok");
});
return route;`,
	}
	for _, c := range []struct {
		folder, path, logged string
		status               int
	}{
		{`"apps/a/shop.vn"`, "/", "", 200},
		{`"../outside/shop.vn"`, "/", "a folder inside the folder of application.work", 500},
		{`"shop.vn"`, "/", "a site sits in an app's folder", 500},
		{`"apps/a/none.vn"`, "/", "the site folder apps/a/none.vn does not exist", 500},
		{`(context) => 5`, "/", `application.website.routes returned number`, 500},
		{`"apps/a/shop.vn"`, "/?do=grow&memory=small", "the run used all its memory (1mb)", 500},
		{`"apps/a/shop.vn"`, "/?do=loop&energy=small", "the run used all its energy", 500},
		{`"apps/a/shop.vn"`, "/?do=loop", "", 200},
		{`"apps/a/shop.vn"`, "/?do=count", "cannot assign to a variable this function holds", 500},
		{`"apps/a/shop.vn"`, "/?do=route", "the route was loaded by the engine and is read only", 500},
	} {
		files["application.work"] = routes(c.folder)
		w, _, log := site(t, files)
		status, _, _ := call(w, "GET", c.path, "", "198.51.100.1")
		if logged := log.take(); status != c.status || !strings.Contains(logged, c.logged) {
			t.Errorf("%s %s: %d\nlogged %q", c.folder, c.path, status, logged)
		}
	}
}

// Nhiều request cùng tới một site lần đầu (go test -race): site biên dịch một lần, mọi request đúng câu trả lời.
func TestASiteCompilesOnce(t *testing.T) {
	w, console, _ := site(t, map[string]string{
		"application.work": shopApplication,
		"apps/a/shop.vn/router.work": `import { router } from "router";
console.log("compiled");
const route = router();
route.get((context) => context.json({ user: context.request.user }));
return route;`,
	})
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				user := []string{"a", "b", "c"}[g%3]
				if status, body, _ := call(w, "GET", "/", user, "198.51.100.1"); status != 200 || body != `{"user":"`+user+`"}` {
					t.Errorf("%d %q", status, body)
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := strings.Count(console.String(), "[shop.vn] [console.log] compiled\n"); n != 1 {
		t.Errorf("the site compiled %d times", n)
	}
}
