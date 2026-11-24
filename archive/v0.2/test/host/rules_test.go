package host_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// trail is a library of the app whose guards mark the trail a request leaves in its memory, which a handler reads
// and clears: the rules a request met, in the order it met them.
const trail = `import { memory } from "kitwork/storage";
export const mark = (name) => (context) => {
  const trail = memory("trail");
  trail.set((trail.get() ?? "") + name + " ");
  return context.next();
};
export const said = () => {
  const trail = memory("trail");
  const text = trail.get() ?? "";
  trail.delete();
  return text;
};
`

const trailImport = routerImport + "import { mark, said } from \"app/core/trail.work\";\n"

// A request meets the rules from the app root down to its folder, each folder's config.work before its
// router.work, each chain in the order written, for the methods it names, then the chain before its door, then the
// door (docs/router.md mục 4); a folder below meets every chain of the folders above.
func TestRulesRunFromTheAppRootDownToTheDoor(t *testing.T) {
	site := "apps/shop/shop.test/"
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":          manifest,
		"apps/shop/core/trail.work": trail,
		"apps/shop/config.work":     trailImport + "router.guard(mark(\"app\")).methods(\"*\");\n",
		site + "config.work":        trailImport + "router.guard(mark(\"site\")).methods(\"*\");\n",
		site + "router.work":        trailImport + "router.get((context) => context.text(said()));\n",
		site + "admin/config.work":  trailImport + "router.guard(mark(\"config\")).methods(\"*\");\nrouter.guard(mark(\"writes\")).methods(\"post\", \"delete\");\n",
		site + "admin/router.work": trailImport + `router.guard(mark("router")).methods("get");
router.guard(mark("door")).guard(mark("again")).get((context) => context.text(said() + "handler"));
router.post((context) => context.text(said() + "post"));
`,
		site + "admin/users/router.work": trailImport + "router.get((context) => context.text(said() + \"users\"));\n",
	}), "application.work"))
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/admin", "app site config router door again handler"},
		{"POST", "/admin", "app site config writes post"},
		{"GET", "/admin/users", "app site config router users"},
		{"GET", "/", "app site "},
	} {
		if got := app.request(t, c.method, "shop.test", c.path, nil); got.status != 200 || got.body != c.want {
			t.Errorf("%s %s: %d %q, want %q", c.method, c.path, got.status, got.body, c.want)
		}
	}
	// a write from another site's page is refused before any rule runs
	if got := app.request(t, "POST", "shop.test", "/admin", nil, "Sec-Fetch-Site", "cross-site"); got.status != 403 {
		t.Errorf("a POST of another site: %d, want 403", got.status)
	}
	if got := app.get(t, "shop.test", "/"); got.body != "app site " {
		t.Errorf("the rules a refused write met: %q, want none", strings.TrimSuffix(got.body, "app site "))
	}
}

// A guard lets the request on with return context.next(), or answers it: any answer but a page, context.error(…)
// refusing with 403 or the 4xx set before it. Anything else it returns refuses with 403, the reason in the log; a
// failure it does not catch is the request's, as a handler's (docs/router.md mục 5).
func TestAGuardLetsOnOrAnswers(t *testing.T) {
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/form.work": "import { struct, string } from \"kitwork/schema\";\nexport const form = struct({ name: string().min(1) });\n",
		"apps/shop/shop.test/answers/{kind}/router.work": routerImport + `import { form } from "app/core/schema/form.work";
// what a guard returns through a name or a call, which the boot cannot see: refused when it runs
const values = { yes: true, no: false, none: undefined };
const draw = (context) => context.view({});
router.guard((context) => {
  const kind = context.params("kind");
  if (kind === "redirect") { return context.redirect("/login"); }
  if (kind === "error") { return context.error("closed"); }
  if (kind === "unauthorized") { return context.status(401).error(); }
  if (kind === "json") { return context.status(202).json({ by: "guard" }); }
  if (kind === "missing") { return context.notFound(); }
  if (kind === "server") { return context.status(500).error("down"); }
  if (kind === "true") { return values.yes; }
  if (kind === "false") { return values.no; }
  if (kind === "nothing") { return values.none; }
  if (kind === "page") { return draw(context); }
  if (kind === "failure") { return error("the guard broke"); }
  if (kind === "input") { return form.parse(context.request.json()); }
  if (kind === "body") { safe(() => context.request.text()); return context.next(); }
  if (kind === "next") { return context.next(1); }
  return context.next();
}).methods("*");
router.get((context) => context.text("handler " + context.params("kind")));
router.post((context) => context.text("handler"));
`,
		"apps/shop/shop.test/door/router.work": routerImport + "const pass = (context) => context.next();\nrouter.guard((context) => context.next()).get((context) => pass(context));\n",
		"apps/shop/shop.test/big/config.work":  routerImport + "router.guard((context) => { safe(() => context.request.text()); return context.next(); }).methods(\"*\");\n",
		"apps/shop/shop.test/big/router.work":  routerImport + "router.get((context) => context.text(\"big\"));\n",
	}), "application.work"))
	for _, c := range []struct {
		kind, method string
		status       int
		body, log    string
	}{
		{"pass", "GET", 200, "handler pass", ""},
		{"redirect", "GET", 302, "", ""},
		{"error", "GET", 403, "closed", ""},
		{"unauthorized", "GET", 401, "Unauthorized", ""},
		{"json", "GET", 202, `{"by":"guard"}`, ""},
		{"missing", "GET", 404, "Not Found", ""},
		{"server", "GET", 500, "Internal Server Error\n", "a guard refuses with 403"},
		{"true", "GET", 403, "Forbidden\n", "403: the guard returned a boolean"},
		{"false", "GET", 403, "Forbidden\n", "the guard returned a boolean"},
		{"nothing", "GET", 403, "Forbidden\n", "the guard returned nothing"},
		{"page", "GET", 403, "Forbidden\n", "the guard drew a page"},
		{"failure", "GET", 500, "Internal Server Error\n", "the guard broke"},
		{"input", "POST", 400, "Bad Request\n", "answers/{kind}/router.work:6:7, router.guard): 400: INVALID_INPUT"},
		{"next", "GET", 500, "Internal Server Error\n", "context.next() takes nothing"},
	} {
		body := strings.NewReader("{")
		got := app.request(t, c.method, "shop.test", "/answers/"+c.kind, body)
		if got.status != c.status || c.body != "" && got.body != c.body {
			t.Errorf("a guard of %s: %d %q, want %d %q", c.kind, got.status, got.body, c.status, c.body)
		}
		if c.log != "" {
			contains(t, "the log of "+c.kind, app.log.String(), c.log)
		}
	}
	if got := app.get(t, "shop.test", "/answers/redirect"); got.header.Get("Location") != "/login" {
		t.Errorf("a guard's redirect leads to %q", got.header.Get("Location"))
	}
	// a body past the limit a guard read is 413, whether the guard caught its failure or not
	for _, path := range []string{"/answers/body", "/big"} {
		if got := app.request(t, "POST", "shop.test", path, strings.NewReader(strings.Repeat("x", 2<<20))); got.status != 413 {
			t.Errorf("POST %s, a guard that read a body too large: %d, want 413", path, got.status)
		}
	}
	// context.next() is a guard's
	if got := app.get(t, "shop.test", "/door"); got.status != 500 {
		t.Errorf("context.next() in a door: %d, want 500", got.status)
	}
	contains(t, "the log", app.log.String(), "context.next() is a guard's")
}

// A branch's chain guards every path of the branch: one no folder answers meets it before its 404, a method no door
// takes before its 405, so a guard that answers is the door of the whole branch for the methods it names, and one
// that lets the request on leaves the 404 and the 405 as they were. OPTIONS meets no rule.
func TestABranchsRulesGuardEveryPathOfIt(t *testing.T) {
	site := "apps/shop/shop.test/"
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                               manifest,
		"apps/shop/core/trail.work":                      trail,
		site + "locked/config.work":                      routerImport + "router.guard((context) => context.status(401).error(\"sign in\")).methods(\"*\");\n",
		site + "locked/router.work":                      routerImport + "router.get((context) => context.text(\"locked\"));\n",
		site + "locked/page/router.work":                 routerImport + "router.get((context) => context.text(\"page\"));\n",
		site + "open/router.work":                        trailImport + "router.guard(mark(\"open\")).methods(\"*\");\nrouter.get((context) => context.text(said()));\n",
		site + "delivery/router.work":                    routerImport + "router.guard((context) => context.json({ method: context.request.method, path: context.request.path, id: context.params(\"id\") ?? null })).methods(\"get\", \"post\");\n",
		site + "delivery/{id[number]}/notes/router.work": routerImport + "router.put((context) => context.text(\"put\"));\n",
		site + "reads/config.work":                       routerImport + "router.guard((context) => context.status(401).error(\"sign in\")).methods(\"get\");\n",
		site + "reads/router.work":                       routerImport + "router.get((context) => context.text(\"read\"));\nrouter.post((context) => context.text(\"written\"));\n",
		site + "files/{...rest}/router.work":             routerImport + "router.guard((context) => context.text(\"rest \" + context.params(\"rest\"))).methods(\"get\");\n",
	}), "application.work"))
	for _, c := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/locked", 401, "sign in"},
		{"GET", "/locked/page", 401, "sign in"},
		{"GET", "/locked/none/deeper", 401, "sign in"},
		{"DELETE", "/locked", 401, "sign in"},
		{"OPTIONS", "/locked", 204, ""},
		{"GET", "/lockedx", 404, "Not Found\n"},
		{"GET", "/delivery", 200, `{"method":"GET","path":"/delivery","id":null}`},
		{"POST", "/delivery/7/x", 200, `{"method":"POST","path":"/delivery/7/x","id":7}`},
		{"PUT", "/delivery/7/notes", 200, "put"},
		{"PUT", "/delivery", 404, "Not Found\n"},
		{"POST", "/open", 405, "Method Not Allowed\n"},
		{"GET", "/open/none", 404, "Not Found\n"},
		{"GET", "/open", 200, "open open open "},
		{"HEAD", "/reads", 401, ""},
		{"POST", "/reads", 200, "written"},
		{"GET", "/files/a/b", 200, "rest a/b"},
	} {
		if got := app.request(t, c.method, "shop.test", c.path, nil); got.status != c.status || got.body != c.body {
			t.Errorf("%s %s: %d %q, want %d %q", c.method, c.path, got.status, got.body, c.status, c.body)
		}
	}
}

// router.limit({ rate, per, by }) counts each client, the address of its connection, or everyone ("global"): before
// .methods(…) the branch's paths together, before a door each path of the door apart, as routed. Over it is 429 with
// Retry-After, and a rule written after it in its chain does not run.
func TestALimitCountsItsClients(t *testing.T) {
	site := "apps/shop/shop.test/"
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                   manifest,
		"apps/shop/core/trail.work":          trail,
		site + "api/config.work":             routerImport + "router.limit({ rate: 2, per: \"1m\" }).methods(\"*\");\n",
		site + "api/a/router.work":           routerImport + "router.get((context) => context.text(\"a\"));\n",
		site + "api/b/router.work":           routerImport + "router.get((context) => context.text(\"b\"));\n",
		site + "items/{id}/router.work":      routerImport + "router.limit({ rate: 1, per: \"1m\", by: \"ip\" }).get((context) => context.text(context.params(\"id\")));\n",
		site + "daily/router.work":           routerImport + "router.limit({ rate: 1, per: \"1d\" }).get((context) => context.text(\"day\"));\n",
		site + "everyone/{name}/router.work": routerImport + "router.limit({ rate: 1, per: \"1h\", by: \"global\" }).get((context) => context.text(\"one\"));\n",
		site + "first/router.work":           trailImport + "router.limit({ rate: 1, per: \"1m\" }).guard(mark(\"guard\")).get((context) => context.text(said()));\n",
	}), "application.work"))
	from := func(address, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host, request.RemoteAddr = "shop.test", address+":4000"
		app.host.ServeHTTP(recorder, request)
		return recorder
	}
	for _, c := range []struct {
		address, path string
		status        int
	}{
		{"10.0.0.1", "/api/a", 200}, {"10.0.0.1", "/api/b", 200}, {"10.0.0.1", "/api/a", 429}, {"10.0.0.2", "/api/b", 200},
		{"10.0.0.1", "/items/1", 200}, {"10.0.0.1", "/items//1/", 429}, {"10.0.0.1", "/items/2", 200}, {"10.0.0.2", "/items/1", 200},
		{"10.0.0.1", "/everyone/a", 200}, {"10.0.0.2", "/everyone/a", 429}, {"10.0.0.2", "/everyone/b", 200},
		{"10.0.0.1", "/first", 200}, {"10.0.0.1", "/first", 429},
	} {
		got := from(c.address, c.path)
		if got.Code != c.status {
			t.Errorf("%s from %s: %d, want %d", c.path, c.address, got.Code, c.status)
		}
		if c.status == 429 && got.Body.String() != "Too Many Requests\n" {
			t.Errorf("%s from %s over its limit went on: %q", c.path, c.address, got.Body.String())
		}
		if c.status == 429 {
			wait, err := strconv.Atoi(got.Header().Get("Retry-After"))
			if err != nil || wait < 1 || wait > 86400 {
				t.Errorf("%s from %s: Retry-After %q", c.path, c.address, got.Header().Get("Retry-After"))
			}
		}
	}
	from("10.0.0.1", "/daily")
	if wait := from("10.0.0.1", "/daily").Header().Get("Retry-After"); wait != "86400" {
		t.Errorf("1 a day: Retry-After %q, want 86400", wait)
	}
	if wait := from("10.0.0.1", "/api/a").Header().Get("Retry-After"); wait != "30" {
		t.Errorf("2 a minute: Retry-After %q, want the 30 seconds one request takes to come back", wait)
	}
	if got := from("10.0.0.3", "/first"); got.Body.String() != "guard " {
		t.Errorf("the guard after a limit ran for a request over it: %q", got.Body.String())
	}
}

// A request's guards and its door spend one request's energy together: two runs within it each are over it both.
func TestAGuardAndItsDoorShareTheEnergy(t *testing.T) {
	loop := "let s = 0; for (let i = 0; i < 250000; i++) { s = s + 1; }"
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/guard/router.work": routerImport + "router.guard((context) => { " + loop + " return context.next(); }).get((context) => context.text(\"guard\"));\n",
		"apps/shop/shop.test/door/router.work":  routerImport + "router.get((context) => { " + loop + " return context.text(\"door\"); });\n",
		"apps/shop/shop.test/both/router.work":  routerImport + "router.guard((context) => { " + loop + " return context.next(); }).get((context) => { " + loop + " return context.text(\"both\"); });\n",
	}), "application.work"))
	for path, status := range map[string]int{"/guard": 200, "/door": 200, "/both": 500} {
		if got := app.get(t, "shop.test", path); got.status != status {
			t.Errorf("%s: %d, want %d", path, got.status, status)
		}
	}
	contains(t, "the log", app.log.String(), "GET /both (/both/router.work): 500:", "Energy Limit Exceeded")
}

// A guard is shared by every request as a handler is: what it holds is frozen, so a request writing it fails instead
// of racing the others (runtime Freeze).
func TestAGuardsValuesAreFrozen(t *testing.T) {
	app := boot(t, filepath.Join(write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/config.work": routerImport + `const make = () => {
  const seen = [];
  return (context) => { seen.push(1); return context.next(); };
};
router.guard(make()).methods("*");
`,
		"apps/shop/shop.test/router.work": routerImport + `const make = () => {
  const seen = [];
  return (context) => { seen.push(1); return context.next(); };
};
router.guard(make()).post((context) => context.text("post"));
router.get((context) => context.text("get"));
`,
	}), "application.work"))
	for _, method := range []string{"GET", "POST"} {
		if got := app.request(t, method, "shop.test", "/", nil); got.status != 500 {
			t.Errorf("%s through a guard writing what it holds: %d, want 500", method, got.status)
		}
	}
	contains(t, "the log", app.log.String(), "TYPE")
}

// Rules are read strictly, at boot: what a guard, a limit or .methods(…) is handed, where each stands, and a chain
// with no end.
func TestRulesAreReadStrictly(t *testing.T) {
	site := "apps/shop/shop.test/"
	for _, c := range []struct{ name, file, source, want string }{
		{"a guard of nothing", "config.work", "router.guard().methods(\"*\");", "router.guard(…) takes one function of the program"},
		{"two guards in one", "config.work", "router.guard([(context) => context.next()]).methods(\"*\");", "two guards are two words, router.guard(a).guard(b)"},
		{"a guard of null", "config.work", "router.guard(null).methods(\"*\");", "it was handed null, a name with no value"},
		{"two guards as two arguments", "config.work", "router.guard((context) => context.next(), (context) => context.next()).methods(\"*\");", "router.guard(…) takes one function of the program"},
		{"a guard of a text", "config.work", "router.guard(\"staff\").methods(\"*\");", "router.guard(…) takes one function of the program"},
		{"a guard taking context apart", "config.work", "router.guard(({ next }) => next()).methods(\"*\");", "takes next out of context"},
		{"a guard alone", "config.work", "router.guard((context) => context.next());", "stands alone: a rule stands before a door, router.guard(…).get(…) for that door, or before .methods(…), router.guard(…).methods(\"*\")"},
		{"a limit alone", "router.work", "router.limit({ rate: 1, per: \"1m\" });", "router.limit(…) at shop.test/router.work:2:7 stands alone"},
		{"a chain then a door on the next line", "router.work", "router.guard((context) => context.next());\nrouter.get((context) => context.text(\"a\"));", "stands alone"},
		{"a chain then another word", "config.work", "router.guard((context) => context.next());\nrouter.meta({ robots: \"noindex\" });", "stands alone"},
		{"a limit of nothing", "config.work", "router.limit().methods(\"*\");", "router.limit(…) takes one object, { rate, per, by }"},
		{"a limit's other key", "config.work", "router.limit({ max: 5, per: \"1m\" }).methods(\"*\");", "no key max (rate?)"},
		{"a limit's rate of half", "config.work", "router.limit({ rate: 1.5, per: \"1m\" }).methods(\"*\");", "rate is a whole number above 0"},
		{"a limit's rate of 0", "config.work", "router.limit({ rate: 0, per: \"1m\" }).methods(\"*\");", "rate is a whole number above 0"},
		{"a limit's rate past a count", "config.work", "router.limit({ rate: 1e10, per: \"1m\" }).methods(\"*\");", "rate is a whole number above 0"},
		{"a limit's per of 0", "config.work", "router.limit({ rate: 1, per: \"0s\" }).methods(\"*\");", "per is a span written as text"},
		{"a limit's per of 0 days", "config.work", "router.limit({ rate: 1, per: \"0d\" }).methods(\"*\");", "per is a span written as text"},
		{"a limit's per of ten thousand days", "config.work", "router.limit({ rate: 1, per: \"10000d\" }).methods(\"*\");", "per is a span written as text"},
		{"a limit's rate of none", "config.work", "router.limit({ per: \"1m\" }).methods(\"*\");", "rate is a whole number above 0"},
		{"a limit's per of a number", "config.work", "router.limit({ rate: 5, per: 60 }).methods(\"*\");", "per is a span written as text"},
		{"a limit's per of words", "config.work", "router.limit({ rate: 5, per: \"1 minute\" }).methods(\"*\");", "per is a span written as text"},
		{"a limit by an upper case", "config.work", "router.limit({ rate: 5, per: \"1m\", by: \"IP\" }).methods(\"*\");", "by is \"ip\" (the default) or \"global\" (\"ip\"?)"},
		{"a limit by a header", "config.work", "router.limit({ rate: 5, per: \"1m\", by: \"x-client\" }).methods(\"*\");", "nothing a client sends unchecked"},
		{"a limit by a user", "config.work", "router.limit({ rate: 5, per: \"1m\", by: \"user\" }).methods(\"*\");", "UNSUPPORTED"},
		{"a limit by a number", "config.work", "router.limit({ rate: 5, per: \"1m\", by: 1 }).methods(\"*\");", "router.limit({ by }): by is \"ip\" (the default) or \"global\""},
		{"methods alone", "config.work", "router.methods(\"*\");", "router.methods(…) ends a chain of router.guard(…) or router.limit(…)"},
		{"methods of nothing", "config.work", "router.guard((context) => context.next()).methods();", ".methods(…) names the methods its chain is for"},
		{"methods of every one and one", "config.work", "router.guard((context) => context.next()).methods(\"*\", \"get\");", "\"*\" is every method, alone"},
		{"methods twice", "config.work", "router.guard((context) => context.next()).methods(\"get\", \"get\");", "names \"get\" twice"},
		{"methods in upper case", "config.work", "router.guard((context) => context.next()).methods(\"GET\");", "in lower case, or \"*\" (\"get\"?)"},
		{"methods of a view", "config.work", "router.guard((context) => context.next()).methods(\"view\");", "a page of router.view answers GET"},
		{"methods of head", "config.work", "router.guard((context) => context.next()).methods(\"head\");", "HEAD runs as GET and meets its rules"},
		{"methods of a function", "config.work", "router.guard((context) => context.next()).methods((context) => context.next());", "takes the names of methods, not function"},
		{"a cache before methods", "router.work", "router.cache(\"5m\").methods(\"*\");", "router.cache(…) before .methods(…): cache belongs to the answer of a GET door"},
		{"a guard and a cache before methods", "router.work", "router.cache(\"5m\").guard((context) => context.next()).methods(\"*\");", "only guard and limit stand before .methods(…)"},
		{"a door in config.work after a guard", "config.work", "router.guard((context) => context.next()).get((context) => context.text(\"a\"));", "a chain of them ended by .methods(…)"},
		{"a rule after a door", "router.work", "router.get((context) => context.text(\"a\")).guard((context) => context.next());", "router.get(…).guard(…): a rule stands before its door, router.guard(…).get(…)"},
		{"a word after methods", "config.work", "router.guard((context) => context.next()).methods(\"*\").limit({ rate: 1, per: \"1m\" });", "router.methods(…).limit(…): a rule stands before its door"},
		{"a word after a folder's word", "router.work", "router.reload(false).guard((context) => context.next());", "router.reload(…).guard(…): a rule stands before its door"},
		{"a word after a door", "router.work", "router.get((context) => context.text(\"a\")).meta({});", "nothing follows router.get(…); router.meta(…) on a line of its own"},
	} {
		got := bootError(t, map[string]string{site + c.file: routerImport + c.source + "\n"})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
