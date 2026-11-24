package host_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/internal/loopback"
)

// httpImport is the two imports of a router.work that sends.
const httpImport = "import * as http from \"kitwork/http\";\n" + routerImport

// upstream is the other side of a request out, on loopback, which the engine's own tests open (internal/loopback):
// /json answers JSON with a header, /status?code=N that status, /echo what it was sent, /big 4096 bytes, /slow?ms=N
// after N ms, /empty a 204, /badjson a body that is not the JSON it says, /bin bytes, /redirect?n=N N redirects then
// "landed", /count/<name> counts its calls (503 for fail…, 429 for busy…, 404 for missing…, 500 for broken…).
func upstream(t *testing.T) (*httptest.Server, func(name string) int) {
	t.Helper()
	var mutex sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Up", "yes")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"a":1,"word":"cà phê"}`))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		w.WriteHeader(code)
		w.Write([]byte("status " + strconv.Itoa(code)))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "query": r.URL.RawQuery, "type": r.Header.Get("Content-Type"),
			"one": r.Header.Values("X-One"), "auth": r.Header.Get("Authorization"), "body": string(body)})
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", 4096))) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		w.Write([]byte("late"))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/badjson", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte("{not json"))
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain words"))
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n == 0 {
			w.Write([]byte("landed"))
			return
		}
		http.Redirect(w, r, "/redirect?n="+strconv.Itoa(n-1), http.StatusFound)
	})
	mux.HandleFunc("/count/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/count/")
		mutex.Lock()
		counts[name]++
		n := counts[name]
		mutex.Unlock()
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		switch {
		case strings.HasPrefix(name, "fail"):
			w.WriteHeader(http.StatusServiceUnavailable)
		case strings.HasPrefix(name, "busy"):
			w.WriteHeader(http.StatusTooManyRequests)
		case strings.HasPrefix(name, "missing"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(name, "broken"):
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write([]byte("count-" + strconv.Itoa(n)))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	t.Cleanup(loopback.Open())
	return server, func(name string) int {
		mutex.Lock()
		defer mutex.Unlock()
		return counts[name]
	}
}

// httpFiles are the folders of a site that sends to up.
func httpFiles(up string) map[string]string {
	site := "apps/shop/shop.test/"
	source := func(text string) string { return httpImport + strings.ReplaceAll(text, "U/", up+"/") }
	return map[string]string{
		"application.work": manifest,
		site + "out/router.work": source(`router.get((context) => {
  const a = http.get("U/json").header("X-Token", "t").send();
  const posted = http.post("U/echo").headers({ "x-one": "1", "X-One": "2" }).query({ b: 2, a: "x y", none: null, gone: undefined })
    .query({ c: true }).json({ item: "cà phê" }).send();
  const formed = http.put("U/echo").form({ name: "An", age: 30, none: null }).send();
  const typed = http.patch("U/echo").text("plain").send();
  const bearer = http.get("U/echo").bearer("tok").send();
  const basic = http.get("U/echo").basic("an", "pw").send();
  const missing = http.get("U/status").query({ code: 404 }).send();
  const server = http.get("U/status").query({ code: 500 }).send();
  const empty = http.delete("U/empty").send();
  const head = http.head("U/json").send();
  const text = http.get("U/bin").send();
  return context.json({ a: [a.ok, a.status, a.code, a.error, a.data, a.text, a.headers["x-up"], a.headers["content-type"]],
    keys: Object.keys(a), posted: posted.data, formed: formed.data, typed: typed.data, bearer: bearer.data.auth, basic: basic.data.auth,
    missing: [missing.ok, missing.status, missing.code, missing.error, missing.data],
    server: [server.ok, server.code], empty: [empty.ok, empty.status, empty.data], head: [head.ok, head.status, head.data],
    text: text.data });
});
`),
		site + "errors/router.work": source(`router.get((context) => {
  const network = http.get("http://nowhere.invalid/").send();
  const denied = http.get("http://169.254.169.254/latest/meta-data/").send();
  const timeout = http.get("U/slow?ms=300").timeout("100ms").send();
  const large = http.get("U/big").maxBytes("1kb").send();
  const badJson = http.get("U/badjson").send();
  const tooMany = http.get("U/redirect?n=6").send();
  const five = http.get("U/redirect?n=5").send();
  return context.json({ network: [network.ok, network.status, network.code, network.data, network.text], denied: denied.code,
    timeout: [timeout.code, timeout.status], large: [large.ok, large.code, large.status, large.data], badJson: [badJson.ok, badJson.code, badJson.data, badJson.text],
    badJsonSays: badJson.error, tooMany: tooMany.code, tooManySays: tooMany.error, five: five.data });
});
`),
		site + "retry/router.work": source(`router.get((context) => {
  const failing = http.get("U/count/fail").retry(2).send();
  const busy = http.get("U/count/busy").retry(1).send();
  const missing = http.get("U/count/missing").retry(3).send();
  const broken = http.get("U/count/broken").retry(3).send();
  const kept = http.post("U/count/failpost").header("Idempotency-Key", "k").retry(1).send();
  const started = Date.now();
  const bounded = http.get("U/count/failslow?ms=150").retry(5).timeout("400ms").send();
  const quick = http.get("U/count/failquick").retry(5).timeout("300ms").send();
  return context.json({ failing: [failing.code, failing.data], busy: busy.code, missing: missing.code, broken: broken.code, kept: kept.status,
    bounded: bounded.ok, boundedMs: Date.now() - started, quick: quick.code });
});
`),
		site + "rules/router.work": source(`const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
router.get((context) => context.json({
  number: code(() => http.get(1)),
  ftp: code(() => http.get("ftp://x/")),
  path: code(() => http.get("/echo")),
  noHost: code(() => http.get("http:///x")),
  extra: code(() => http.post("U/echo", { a: 1 })),
  jsonOnGet: code(() => http.get("U/json").json({ a: 1 })),
  formOnHead: code(() => http.head("U/json").form({ a: 1 })),
  sendData: code(() => http.post("U/echo").send({ a: 1 })),
  numberTimeout: code(() => http.get("U/json").timeout(100)),
  longTimeout: code(() => http.get("U/json").timeout("61s")),
  negativeRetry: code(() => http.get("U/json").retry(-1)),
  sixRetries: code(() => http.get("U/json").retry(6)),
  halfRetry: code(() => http.get("U/json").retry(1.5)),
  retryPost: code(() => http.post("U/echo").retry(1).send()),
  cachePost: code(() => http.post("U/echo").cache("1m")),
  persistPost: code(() => http.post("U/echo").persist("1h")),
  badHeader: code(() => http.get("U/json").header("X A", "a")),
  lineHeader: code(() => http.get("U/json").header("X-A", "a\nb")),
  arrayQuery: code(() => http.get("U/json").query({ a: [1] })),
  emptyBearer: code(() => http.get("U/json").bearer("")),
  colonUser: code(() => http.get("U/json").basic("a:b", "x")),
  functionJson: code(() => http.post("U/echo").json(() => 1)),
  textNumber: code(() => http.post("U/echo").text(1)),
  pastCap: code(() => http.get("U/json").maxBytes("1gb")),
  options: code(() => http.options("U/json")),
  unsentRead: code(() => http.get("U/json").ok),
  fine: code(() => http.get("U/json").send()),
  unsentSays: safe(() => http.get("U/json").data).error?.message,
  sendDataSays: safe(() => http.post("U/echo").send({ a: 1 })).error?.message
}));
`),
		site + "kept/router.work": source(`router.get((context) => {
  const feed = http.get("U/count/fresh").cache("1m");
  const first = feed.send();
  const second = feed.send();
  const other = feed.header("X-Token", "b").send();
  const plain = http.get("U/count/fresh").send();
  const failed = http.get("U/count/failcache").cache("1m");
  failed.send();
  failed.send();
  return context.json({ first: first.text, second: second.text, other: other.text, plain: plain.text });
});
`),
		site + "disk/router.work": source(`router.get((context) => context.json({ text: http.get("U/count/disk").persist("1h").send().text }));
`),
	}
}

// httpSite boots the site of httpFiles against up.
func httpSite(t *testing.T, up string) (*booted, string) {
	t.Helper()
	folder := write(t, httpFiles(up))
	return boot(t, filepath.Join(folder, "application.work")), folder
}

// fetchJSON is the JSON a page of the site answers, failing the test on another status.
func fetchJSON(t *testing.T, b *booted, path string) map[string]any {
	t.Helper()
	got := b.get(t, "shop.test", path)
	if got.status != http.StatusOK {
		t.Fatalf("GET %s: %d %s\n%s", path, got.status, got.body, b.log.String())
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.body), &out); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, got.body)
	}
	return out
}

// shown is a value as JSON, for comparing.
func shown(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}

// .send() gives { ok, status, code, error, data, text, headers } whatever the server answered (engine2/docs/http.md
// mục 3): data the body read by its type, JSON as a value, text as a text; a 4xx, a 5xx is HTTP_4XX, HTTP_5XX with its
// body still read. The body is .json(…), .form(…) or .text(…), each with its type; .query() leaves out undefined and
// null, in the order written; .bearer(), .basic() set Authorization; .headers() merges whatever the case.
func TestARequestOutGivesItsResult(t *testing.T) {
	up, _ := upstream(t)
	b, _ := httpSite(t, up.URL)
	got := fetchJSON(t, b, "/out")
	if shown(got["a"]) != `[true,200,null,null,{"a":1,"word":"cà phê"},"{\"a\":1,\"word\":\"cà phê\"}","yes","application/json"]` {
		t.Errorf("a JSON answer: %s", shown(got["a"]))
	}
	if shown(got["keys"]) != `["ok","status","code","error","data","text","headers"]` {
		t.Errorf("the result's keys: %s", shown(got["keys"]))
	}
	posted := got["posted"].(map[string]any)
	if posted["method"] != "POST" || posted["query"] != "b=2&a=x+y&c=true" || posted["type"] != "application/json" ||
		posted["body"] != `{"item":"cà phê"}` || shown(posted["one"]) != `["2"]` {
		t.Errorf("a POST of JSON: %s", shown(posted))
	}
	formed := got["formed"].(map[string]any)
	if formed["type"] != "application/x-www-form-urlencoded" || formed["body"] != "name=An&age=30" {
		t.Errorf("a form: %s", shown(formed))
	}
	typed := got["typed"].(map[string]any)
	if typed["method"] != "PATCH" || typed["type"] != "text/plain; charset=utf-8" || typed["body"] != "plain" {
		t.Errorf("a text: %s", shown(typed))
	}
	if got["bearer"] != "Bearer tok" || got["basic"] != "Basic YW46cHc=" {
		t.Errorf("authorization: %v, %v", got["bearer"], got["basic"])
	}
	if shown(got["missing"]) != `[false,404,"HTTP_4XX","the server answered 404 Not Found","status 404"]` || shown(got["server"]) != `[false,"HTTP_5XX"]` {
		t.Errorf("a 404: %s, a 500: %s", shown(got["missing"]), shown(got["server"]))
	}
	if shown(got["empty"]) != `[true,204,null]` || shown(got["head"]) != `[true,200,null]` || got["text"] != "plain words" {
		t.Errorf("no body: %s, a HEAD: %s, a text: %v", shown(got["empty"]), shown(got["head"]), got["text"])
	}
}

// No answer is a result too, never a failure that rises (mục 3, 4): NETWORK (status null), DENIED by the SSRF guard,
// TIMEOUT, TOO_LARGE past the body's cap, INVALID_RESPONSE for a body that is not the JSON its type says, NETWORK past
// five redirects; five are followed.
func TestNoAnswerIsAResultToo(t *testing.T) {
	up, _ := upstream(t)
	b, _ := httpSite(t, up.URL)
	got := fetchJSON(t, b, "/errors")
	if shown(got["network"]) != `[false,null,"NETWORK",null,""]` || got["denied"] != "DENIED" || shown(got["timeout"]) != `["TIMEOUT",null]` {
		t.Errorf("no answer: %s, %v, %s", shown(got["network"]), got["denied"], shown(got["timeout"]))
	}
	if shown(got["large"]) != `[false,"TOO_LARGE",200,null]` || shown(got["badJson"]) != `[false,"INVALID_RESPONSE",null,"{not json"]` {
		t.Errorf("a body past its cap: %s, not JSON: %s", shown(got["large"]), shown(got["badJson"]))
	}
	contains(t, "not JSON", got["badJsonSays"].(string), "not the JSON its Content-Type says")
	if got["tooMany"] != "NETWORK" || got["five"] != "landed" {
		t.Errorf("redirects: %v after six, %v after five", got["tooMany"], got["five"])
	}
	contains(t, "redirects", got["tooManySays"].(string), "stopped after 5 redirects")
}

// .retry(n) runs again when the network failed or the server answered 502, 503, 504 or 429, never on another status
// (mục 2.2); a POST runs again only with an Idempotency-Key; .timeout() bounds the whole request, its runs included.
func TestARequestRunsAgainOnlyWhenItMayHelp(t *testing.T) {
	up, count := upstream(t)
	b, _ := httpSite(t, up.URL)
	got := fetchJSON(t, b, "/retry")
	if shown(got["failing"]) != `["HTTP_5XX","count-3"]` || count("fail") != 3 {
		t.Errorf("retry(2) of a 503: %s after %d runs", shown(got["failing"]), count("fail"))
	}
	if got["busy"] != "HTTP_4XX" || count("busy") != 2 {
		t.Errorf("retry(1) of a 429: %v after %d runs", got["busy"], count("busy"))
	}
	if count("missing") != 1 || count("broken") != 1 {
		t.Errorf("a 404 ran %d times, a 500 %d: neither is run again", count("missing"), count("broken"))
	}
	if got["kept"] != float64(503) || count("failpost") != 2 {
		t.Errorf("a POST with its key: %v after %d runs", got["kept"], count("failpost"))
	}
	if got["bounded"] != false || got["boundedMs"].(float64) > 1000 || count("failslow") >= 6 {
		t.Errorf("retry(5) within 400ms: %v after %v ms and %d runs", got["bounded"], got["boundedMs"], count("failslow"))
	}
	// no pause runs past the request's time: the last answer is the result, not a TIMEOUT of a run started too late
	if got["quick"] != "HTTP_5XX" || count("failquick") < 2 || count("failquick") > 5 {
		t.Errorf("retry(5) of a quick 503 within 300ms: %v after %d runs", got["quick"], count("failquick"))
	}
}

// A program's own mistake rises (mục 2): a URL that is not http(s) with a host (a path of the site neither), a body on
// a GET or a HEAD, .send() given a body, a timeout past 60 s, retries past 5, a word a request does not have.
func TestARequestsMistakesRise(t *testing.T) {
	up, _ := upstream(t)
	b, _ := httpSite(t, up.URL)
	got := fetchJSON(t, b, "/rules")
	contains(t, "a read of a request not sent", got["unsentSays"].(string), "is not sent, so it has no data", ".send()")
	contains(t, "send(data)", got["sendDataSays"].(string), ".send() takes nothing", ".json(value)")
	delete(got, "unsentSays")
	delete(got, "sendDataSays")
	want := map[string]any{
		"number": "INVALID_ARGUMENT", "ftp": "INVALID_ARGUMENT", "path": "INVALID_ARGUMENT", "noHost": "INVALID_ARGUMENT", "extra": "INVALID_ARGUMENT",
		"jsonOnGet": "INVALID_ARGUMENT", "formOnHead": "INVALID_ARGUMENT", "sendData": "INVALID_ARGUMENT", "numberTimeout": "INVALID_ARGUMENT",
		"longTimeout": "INVALID_ARGUMENT", "negativeRetry": "INVALID_ARGUMENT", "sixRetries": "INVALID_ARGUMENT", "halfRetry": "INVALID_ARGUMENT",
		"retryPost": "INVALID_ARGUMENT", "cachePost": "INVALID_ARGUMENT", "persistPost": "INVALID_ARGUMENT", "badHeader": "INVALID_ARGUMENT",
		"lineHeader": "INVALID_ARGUMENT", "arrayQuery": "INVALID_ARGUMENT", "emptyBearer": "INVALID_ARGUMENT", "colonUser": "INVALID_ARGUMENT",
		"functionJson": "INVALID_ARGUMENT", "textNumber": "INVALID_ARGUMENT", "pastCap": "INVALID_ARGUMENT", "options": "TYPE",
		"unsentRead": "TYPE", "fine": nil,
	}
	if shown(got) != shown(want) {
		t.Errorf("rules: %s\nwant   %s", shown(got), shown(want))
	}
}

// .cache() keeps a GET's result that succeeded in the site's memory, by URL and headers; one that failed is not kept
// (mục 2.2). .persist() keeps it on the disk of the app's machine, across a restart.
func TestAResultIsKept(t *testing.T) {
	up, count := upstream(t)
	b, folder := httpSite(t, up.URL)
	got := fetchJSON(t, b, "/kept")
	if shown(got) != `{"first":"count-1","other":"count-2","plain":"count-3","second":"count-1"}` || count("failcache") != 2 {
		t.Errorf("cache: %s, a failure ran %d times", shown(got), count("failcache"))
	}
	if first := fetchJSON(t, b, "/disk"); first["text"] != "count-1" {
		t.Errorf("persist, first: %v", first["text"])
	}
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	again := boot(t, filepath.Join(folder, "application.work"))
	if second := fetchJSON(t, again, "/disk"); second["text"] != "count-1" || count("disk") != 1 {
		t.Errorf("persist after a restart: %v, %d calls", second["text"], count("disk"))
	}
}

// A file declares as the site boots: .send() at its top level is a boot error.
func TestARequestIsNotSentAsTheSiteBoots(t *testing.T) {
	said := bootError(t, map[string]string{
		"apps/shop/shop.test/router.work": httpImport + "const r = http.get(\"https://example.com/\").send();\nrouter.get((context) => context.text(\"x\"));\n",
	})
	contains(t, "the boot", said, ".send() at the top level of a file")
}
