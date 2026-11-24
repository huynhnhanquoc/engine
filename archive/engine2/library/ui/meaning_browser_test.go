package ui

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// One meaning everywhere (round 4 of the 13/11 review, 15/11/2026): an expression of a .ui file means in the browser
// what it means on the server, which is JavaScript's with .ui's rules over it. The browser runs what the compiler
// writes for it as JavaScript (core/compiler javascript.go); the server runs the same source on the VM. In a headless
// browser each case runs three ways: as JavaScript itself (the source in a with over the scope), as the JavaScript
// the compiler wrote (called with the scope as this, the kernel loaded for kitwork.internal), and on the server's
// evaluator. What the compiler wrote is the server's value in every case, and JavaScript's but where .ui reads a
// chain as ?. (o.missing.x is undefined, where JavaScript throws). What an event runs, which the server never runs,
// is JavaScript's value and leaves the scope as JavaScript leaves it.
//
// Measured before (the kernel's own language, kernel.js lex/parse/walk): 18 of 95 cases another value, among them
// "abc" + null "abc", missing + 1 1, a call of what is no function undefined, a name nobody gave 0, and a template,
// typeof, a spread, { n } and an arrow with a body refused.

// meaningScope is the scope every case reads, fresh for each.
const meaningScope = `{"n": 3, "s": "abc", "z": null, "o": {"a": 1, "b": {"c": 2}}, "items": [{"id": 1, "done": true, "t": "x"}, {"id": 2, "done": false, "t": "y"}], "list": [3, 1, 2], "e": "", "f": 0, "t": true, "value": 9}`

// meaningExpressions are read by a binding: the server runs each too.
var meaningExpressions = []string{
	"n + 1", "n / 0", "0 / 0", "-n", "'3' * '4'", "'3' + 4", "s + n", "n + z", "s + z", "z + 1",
	"o.missing + 1", "s + o.missing", "[1] + [2]", "+'12'", "+z", "+o.missing", "0.1 + 0.2", "1 / 3", "1e21", "-5 % 3",
	"n > 2", "s < 'b'", "z == null", "o.missing == null", "o.missing === null", "'1' == 1", "0 == ''", "null >= 0", "z < 1",
	"z ?? 'd'", "f || 'd'", "f ?? 'd'", "t && s", "!z", "!!s", "!e",
	"n > 2 ? 'big' : 'small'", "value * 2",
	"o.a", "o.b.c", "o['a']", "items[0].t", "items.length", "s.length", "o.missing", "o.missing.x", "z.x", "o.missing?.x",
	"z?.x", "items[5]", "items[5].t", "o['b']['c']", "items[n - 2].t",
	"s.toUpperCase()", "list.indexOf(2)", "items.filter((i) => i.done).length", "items.map((i) => i.t).join(',')",
	"Math.max(1, n)", "Math.round(2.5)", "Number('12')", "String(n)", "parseInt('08')", "JSON.stringify(o)", "Object.keys(o)",
	"o.a()", "s.nope()", "n.toFixed(1)", "list.includes(3)", "items.find((i) => i.id === 2).t", "items.some((i) => i.done)",
	"Math.min()", "String(z)", "String(o.missing)", "o['a']()",
	"`n=${n}`", "`${s}-${z}-${o.missing}`", "typeof n", "typeof o.missing", "typeof nope", "({ ...o })", "({ a: 1 }).a", "({ n })",
	"({ [s]: n })", "items.map((i) => { return i.id; })", "items.map((i) => { const d = i.id * 2; return d; })",
	"items.map((i) => { if (i.done) { return 'yes'; } return 'no'; })", "Array.isArray(list)",
	"undefined", "NaN", "Infinity", "n === 3 && s", "list.length > 0 ? list[0] : 'none'",
	"items.filter((i) => !i.done).map((i) => i.t)", "s.split('').reverse().join('')", "s[0]", "s.charAt(1)",
	"list.reduce((a, b) => a + b, 0)", "Math.floor(n / 2)", "(n + 1) * 2", "o.constructor", "s.constructor", "o['__proto__']",
	"({ '__proto__': 1 }).__proto__", "new Date(0).getTime()", "Date.UTC(2020, 0, 1)", "[...list]",
	"[...list, n]", "Math.max(...list)", "[...s]", "[0, ...items.map((i) => i.id)]", "[...'a😀']", "String(...[n])",
	"list.concat(...[[9], [8]])", "[...list, ...list].length",
	"((...xs) => xs.length)(1, 2, 3)", "items.map(({ id, ...rest }) => rest)", "(([a, ...r]) => r)(list)",
	"items.map(({ id }) => id)", "items.map(({ t: name }) => name + '!')",
	"((x = 2, y = x * 3) => y)()", "items.map((i, n = 10) => n)", "(({ t = 'z' } = {}) => t)()", "((a = 1) => a)(null)",
	"(({ a = 1, b = 2 }) => [a, b])({ a: null })",
	"[[1, 2], [3, 4]].map(([a, b]) => a * b)", "Object.entries(o.b).map(([k, v]) => k + '=' + v).join('&')",
	"items.map((i) => { const out = []; for (const [k] of Object.entries(i)) { out.push(k); } return out; })",
	"items.map((i) => { let n = 0; for (const { id } of items) { n = n + id; } return n; })",
	"[1].map(() => { const out = []; for (const c of 'a😀ệ') { out.push(c); } return out; })",
	"((a, ...r) => [a, r])(...list)", "items.map((i) => { const { t, ...more } = i; return more; })",
}

// meaningUIChains are where .ui reads a chain as ?. and JavaScript throws, and where it reads a member that leads to
// code (constructor, __proto__) that the value does not hold itself, which JavaScript finds on a prototype: the
// server's value, undefined, is the one. A key __proto__ written in an object is a key on the server, which has no
// prototypes, where JavaScript sets the prototype: the browser is written to keep it a key.
var meaningUIChains = map[string]bool{"o.missing.x": true, "z.x": true, "items[5].t": true, "o.constructor": true,
	"s.constructor": true, "o['__proto__']": true, "({ '__proto__': 1 }).__proto__": true}

// meaningHandlers are run by an event: only the browser runs them; each gives its value and the scope after it.
var meaningHandlers = []string{
	"n = 5", "n++", "++n", "n--", "n += 2", "s += '!'", "o.a = 2", "o.b.c += 1", "items.push({ id: 3 })", "value = value + 1",
	"t = !t", "z = o.missing ?? 'none'", "list = list.filter((x) => x > 1)", "f = n > 2 ? 1 : 2",
	"list = [...list, 4]", "list.push(...[7, 8])",
}

func TestBrowserAnExpressionMeansWhatItMeansOnTheServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := meaningBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	scope := map[string]value.Value{}
	parsed := value.ParseJSON(meaningScope)
	for _, key := range parsed.Keys() {
		scope[key] = parsed.Get(key)
	}
	type run struct {
		Source  string `json:"source"`
		Written string `json:"written"`
		Handler bool   `json:"handler"`
	}
	var runs []run
	server := map[string]string{}
	for _, source := range meaningExpressions {
		written, err := compiler.ExpressionJavaScript(source)
		server[source] = meaningOnServer(source, scope)
		if err != nil {
			if server[source] != "refused" {
				t.Errorf("%s: the server reads it (%s), the browser's JavaScript is not written: %v", source, server[source], err)
			}
			continue
		}
		runs = append(runs, run{Source: source, Written: written})
	}
	for _, source := range meaningHandlers {
		written, err := compiler.HandlerJavaScript(source)
		if err != nil {
			t.Errorf("event %s: not written: %v", source, err)
			continue
		}
		runs = append(runs, run{Source: source, Written: written, Handler: true})
	}
	payload, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	report := meaningInBrowser(t, browser, payload)
	var got [][2]string
	if err := json.Unmarshal(report, &got); err != nil || len(got) != len(runs) {
		t.Fatalf("report %.300s: %v (%d of %d)", report, err, len(got), len(runs))
	}
	for i, r := range runs {
		javascript, written := got[i][0], got[i][1]
		switch {
		case r.Handler:
			if written != javascript {
				t.Errorf("event %s (%s): the browser %s, JavaScript %s", r.Source, r.Written, written, javascript)
			}
		case server[r.Source] == "refused":
			// a name the page is not given: the server's drawing refuses it, the browser never gets it
		case written != server[r.Source]:
			t.Errorf("%s (%s): the browser %s, the server %s", r.Source, r.Written, written, server[r.Source])
		case written != javascript && !meaningUIChains[r.Source]:
			t.Errorf("%s (%s): the browser and the server %s, JavaScript %s", r.Source, r.Written, written, javascript)
		}
	}
	t.Logf("%d expressions and %d events in %s", len(runs)-len(meaningHandlers), len(meaningHandlers), filepath.Base(browser))
}

// meaningOnServer is source's value on the server's evaluator, encoded as the page encodes the browser's.
func meaningOnServer(source string, scope map[string]value.Value) string {
	bytecode, reads, _, err := compiler.CompileExpressionBuiltins(source)
	if err != nil {
		return "refused"
	}
	e := newEvaluator(nil, nil, 0)
	for name, v := range scope {
		e.put(name, v)
	}
	v, err := e.evaluate(&expression{source: source, program: bytecode.Program, reads: reads})
	if err != nil {
		if strings.Contains(err.Error(), "không được đưa tên") {
			return "refused"
		}
		return "throws"
	}
	switch {
	case v.IsUndefined():
		return "undefined"
	case v.IsFunction():
		return "function"
	case v.IsTime():
		return "date"
	case v.IsNumber():
		n := v.Number()
		switch {
		case math.IsNaN(n):
			return "NaN"
		case math.IsInf(n, 1):
			return "Infinity"
		case math.IsInf(n, -1):
			return "-Infinity"
		case n == 0 && math.Signbit(n):
			return "-0"
		}
		return v.ToString().Text()
	}
	return value.Stringify(v, "").Text()
}

// meaningPage runs each case of the payload as JavaScript and as what the compiler wrote, and posts both.
const meaningPage = `<!doctype html><html><head><meta charset="utf-8"><script src="/kernel.js"></script></head><body><script>
function enc(v) {
  if (v === undefined) return "undefined";
  if (typeof v === "function") return "function";
  if (v instanceof Date) return "date";
  if (typeof v === "number") { if (isNaN(v)) return "NaN"; if (!isFinite(v)) return v > 0 ? "Infinity" : "-Infinity"; if (Object.is(v, -0)) return "-0"; return String(v); }
  try { return JSON.stringify(v); } catch (e) { return "unencodable"; }
}
window.addEventListener("load", async () => {
  const runs = await (await fetch("/runs")).json();
  const scope = await (await fetch("/scope")).text();
  const out = [];
  for (const r of runs) {
    const each = (f) => {
      const s = JSON.parse(scope);
      try { const v = f(s); return r.handler ? enc(v) + " " + JSON.stringify(s) : enc(v); } catch (e) { return e instanceof SyntaxError ? "syntax" : "throws"; }
    };
    const javascript = each((s) => new Function("scope", "with (scope) { return (" + r.source + "); }")(s));
    const written = each((s) => new Function("return (" + r.written + ");").call(s));
    out.push([javascript, written]);
  }
  await fetch("/report", { method: "POST", body: JSON.stringify(out) });
});
</script></body></html>`

func meaningInBrowser(t *testing.T, browser string, payload []byte) []byte {
	t.Helper()
	var mu sync.Mutex
	var report []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, meaningPage)
		case "/kernel.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(kernel.Kernel().Body)
		case "/runs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payload)
		case "/scope":
			_, _ = io.WriteString(w, meaningScope)
		case "/report":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			report = body
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--disable-background-networking", "--disable-default-apps", "--disable-extensions", "--disable-sync",
		"--metrics-recording-only", "--no-first-run", "--window-size=800,600",
		"--user-data-dir=" + filepath.Join(t.TempDir(), "profile"), "--virtual-time-budget=20000", "--dump-dom", server.URL + "/"}
	if runtime.GOOS == "windows" {
		args = append([]string{"--disable-background-mode", "--disable-breakpad", "--disable-crashpad-for-testing"}, args...)
	}
	output, _ := exec.CommandContext(ctx, browser, args...).CombinedOutput()
	mu.Lock()
	defer mu.Unlock()
	if report == nil {
		t.Fatalf("the page never reported\n%.700s", output)
	}
	return report
}

// meaningBrowser is Chrome, Chromium or Edge, "" when none is installed or KITWORK_BROWSER_TESTS says not to.
func meaningBrowser() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITWORK_BROWSER_TESTS"))) {
	case "0", "false", "off", "no":
		return ""
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, path := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
