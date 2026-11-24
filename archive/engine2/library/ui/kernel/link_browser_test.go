package kernel_test

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/kitwork/engine2/library/ui/kernel"
)

// The kernel holds a place to go and an image to the server's rules (links.go, safeLink and safeImage) as it writes
// what follows a component's state, in a real browser: a page of the site, http:, https:, mailto: or tel:; an image
// of the site, http:, https: or data:image/…. 13/11/2026: the server refused href="javascript:…" from data at the
// first paint, and the kernel wrote it at the next; a click ran it.
//
// The browser is the judge of each value: one the kernel refuses has no attribute after the write, and one it keeps
// is read by the browser's own URL parser as a path of the page's host, a web page, an e-mail or a phone number,
// never as javascript: (the values a rule that read a scheme differently from the browser would let through: a
// leading NBSP or BOM, a zero-width space, an entity, a backslash). Then a page as the server draws it: a binding
// refused at its first paint, a link whose state turns from a path to javascript: and is clicked, a form's action,
// a button's formaction, an iframe, an SVG link, an image given javascript: beside one given data:image/…; each
// refusal said in the console. A video's poster and a sound's src are no place to go and are kept as they are.
//
// CONTROL: with writeBinding's check removed, javascript: ran in the page (hits [2 1 "IFRAME src"]: the page's
// iframe, the link clicked after its state turned, the matrix's iframe), the seven refusals of the page were written
// and never said, and every refused value and attribute of the matrix was written; this test failed on each.
func TestBrowserTheKernelWritesOnlyAPlaceToGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := findHeadlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	refused := []string{
		"javascript:top.hit('matrix')", "JAVASCRIPT:top.hit('matrix')", " \t javascript:top.hit('matrix')",
		"java\tscript:top.hit('matrix')", "java\nscript:top.hit('matrix')", "\x00javascript:top.hit('matrix')",
		"\x1fjavascript:top.hit('matrix')", "vbscript:msgbox(1)", "data:text/html,<b>x</b>",
		"data:image/gif;base64,R0lGODlhAQABAAAAACw=", "blob:https://example.test/x", "file:///c:/x",
		"ftp://example.test/", "x-custom:abc",
	}
	kept := []string{
		"/a", "./b", "#c", "?d=1", "https://example.test/x", "HTTP://EXAMPLE.TEST", "mailto:a@b.c", "tel:+84 90",
		"\u00a0javascript:top.hit('matrix')", "\ufeffjavascript:top.hit('matrix')", "javascript\u200b:top.hit('matrix')",
		"jav&#x09;ascript:top.hit('matrix')", "javascript%3Atop.hit('matrix')", "1javascript:top.hit('matrix')",
	}
	refusedImages := []string{
		"javascript:top.hit('image')", " JAVASCRIPT:top.hit('image')", "data:text/html,<b>x</b>",
		"data:text/javascript,top.hit('image')", "data:imagex/png,x", "vbscript:x", "blob:https://example.test/x",
		"mailto:a@b.c",
	}
	keptImages := []string{
		"/a.png", "./b.png", "https://example.test/a.png", "data:image/gif;base64,R0lGODlhAQABAAAAACw=",
		"DATA:IMAGE/PNG;base64,AAAA", " data:image/svg+xml,<svg/>",
	}
	var values []linkValue
	for _, c := range []struct {
		tag, name string
		list      []string
		refuse    bool
	}{{"a", "href", refused, true}, {"a", "href", kept, false}, {"img", "src", refusedImages, true}, {"img", "src", keptImages, false}} {
		for _, v := range c.list {
			values = append(values, linkValue{c.tag, c.name, v, c.refuse})
		}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html><head><meta charset="utf-8"><script>
window.hits = [];
window.hit = function (where) { hits.push(where); };
window.said = [];
var error = console.error;
console.error = function () { said.push(String(arguments[0] && arguments[0].message || arguments[0])); return error.apply(console, arguments); };
</script></head><body>
<main id="app" data-kitwork-scope="url: '/start'; bad: 'javascript:top.hit(1)'; framed: 'javascript:top.hit(2)'; picture: 'javascript:top.hit(3)'; pixel: 'data:image/gif;base64,R0lGODlhAQABAAAAACw='">
  <a id="first" data-kitwork-bind="href: bad">refused at its first paint</a>
  <a id="follows" href="/start" data-kitwork-bind="href: url">a path, then javascript:</a>
  <a id="stays" data-kitwork-bind="href: '/kept'">stays a path</a>
  <form id="form" data-kitwork-bind="action: bad"><button id="submit" type="button" data-kitwork-bind="formaction: bad">x</button></form>
  <iframe id="frame" data-kitwork-bind="src: framed"></iframe>
  <img id="image" alt="" data-kitwork-bind="src: picture">
  <img id="pixel" alt="" data-kitwork-bind="src: pixel">
  <svg><a id="svglink" data-kitwork-bind="href: bad"><text>z</text></a></svg>
  <button id="swap" type="button" data-kitwork-event="click: url = bad">swap</button>
</main>
<div id="matrix" hidden></div>
<script src="/kernel.js"></script>
<script>
window.addEventListener("load", async () => {
  const values = await (await fetch("/values")).json();
  const out = { matrix: [], elements: [] };
  const first = { first: document.getElementById("first").getAttribute("href"), follows: document.getElementById("follows").getAttribute("href") };
  document.getElementById("swap").click();
  document.getElementById("follows").click(); // a link to javascript: runs it as it is clicked
  const read = (id, name) => document.getElementById(id).getAttribute(name);
  out.page = {
    first: first.first, followsBefore: first.follows, followsAfter: read("follows", "href"), stays: read("stays", "href"),
    action: read("form", "action"), formaction: read("submit", "formaction"), frame: read("frame", "src"),
    image: read("image", "src"), pixel: read("pixel", "src"), svg: read("svglink", "href"),
  };
  for (const v of values) {
    const el = document.createElement(v.tag);
    let threw = "";
    try { kitwork.write(el, v.name, v.value); } catch (e) { threw = String(e.message); }
    const href = el.getAttribute(v.name);
    let protocol = "unparsed", own = false;
    if (href !== null) { try { const u = new URL(href, location.href); protocol = u.protocol; own = u.host === location.host; } catch (e) {} }
    out.matrix.push({ href, threw, protocol, own });
  }
  // every attribute that goes somewhere, given javascript:, and the ones that do not, given it too
  const matrix = document.getElementById("matrix");
  const svg = (tag) => document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [make, name, refuse] of [
    [() => document.createElement("a"), "href", true], [() => document.createElement("area"), "href", true],
    [() => document.createElement("form"), "action", true], [() => document.createElement("button"), "formaction", true],
    [() => document.createElement("input"), "formaction", true], [() => document.createElement("iframe"), "src", true],
    [() => document.createElement("embed"), "src", true], [() => document.createElement("object"), "data", true],
    [() => document.createElement("script"), "src", true], [() => svg("a"), "href", true], [() => svg("a"), "xlink:href", true],
    [() => document.createElement("img"), "src", true], [() => document.createElement("video"), "poster", false],
    [() => document.createElement("audio"), "src", false], [() => document.createElement("source"), "src", false],
  ]) {
    const el = make();
    matrix.appendChild(el);
    let threw = "";
    try { kitwork.write(el, name, "javascript:top.hit('" + el.tagName + " " + name + "')"); } catch (e) { threw = String(e.message); }
    out.elements.push({ tag: el.tagName, name, refuse, written: el.getAttribute(name), threw });
  }
  setTimeout(async () => { // what loaded has run (the browser's clock is virtual: a fetch pending stops it)
    out.hits = hits;
    out.said = said;
    await fetch("/report", { method: "POST", body: JSON.stringify(out) });
  }, 500);
});
</script></body></html>`
	// the page's program, as the server writes it (library/ui program.go): the sources the page's attributes hold, each
	// a function the kernel calls with the element's scope as this (15/11/2026: the kernel reads no expression itself)
	var program strings.Builder
	program.WriteString("<script>kitwork.expressions({")
	for i, source := range []string{"bad", "url", "'/kept'", "framed", "picture", "pixel"} {
		written, err := compiler.ExpressionJavaScript(source)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			program.WriteString(", ")
		}
		quoted, _ := json.Marshal(source)
		program.WriteString(string(quoted) + ": function () { return " + written + "; }")
	}
	written, err := compiler.HandlerJavaScript("url = bad")
	if err != nil {
		t.Fatal(err)
	}
	program.WriteString(`, "url = bad": function () { return ` + written + `; }});</script>`)
	page = strings.Replace(page, `<script src="/kernel.js"></script>`, `<script src="/kernel.js"></script>`+program.String(), 1)

	// the kernel a page loads while developing, and the one it loads in production, minified at build time
	readable := kernel.Kernel()
	t.Run("readable", func(t *testing.T) { judgeLinks(t, browser, page, payload, values, readable.Body) })
	t.Run("minified", func(t *testing.T) {
		minified := kernel.Min(readable.Role, "kernel", string(readable.Body))
		if minified == nil {
			t.Fatal("the kernel has no minified twin made from its source of today: run tools/minify (cd tools/minify && go run .)")
		}
		judgeLinks(t, browser, page, payload, values, minified.Body)
	})
}

// linkValue is a value the kernel writes as the attribute name of an element tag, and whether the rule refuses it.
type linkValue struct {
	Tag    string `json:"tag"`
	Name   string `json:"name"`
	Value  string `json:"value"`
	Refuse bool   `json:"refuse"`
}

// judgeLinks serves page with the kernel body and payload, the values, and reads what the browser reports.
func judgeLinks(t *testing.T, browser, page string, payload []byte, values []linkValue, body []byte) {
	var mu sync.Mutex
	var report []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(response, page)
		case "/kernel.js":
			response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = response.Write(body)
		case "/values":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(payload)
		case "/report":
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			report = body
			mu.Unlock()
		default:
			http.NotFound(response, request)
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
	var seen struct {
		Page struct {
			First, FollowsBefore, FollowsAfter, Stays, Action, Formaction, Frame, Image, Pixel, Svg *string
		} `json:"page"`
		Matrix []struct {
			Href     *string `json:"href"`
			Threw    string  `json:"threw"`
			Protocol string  `json:"protocol"`
			Own      bool    `json:"own"`
		} `json:"matrix"`
		Elements []struct {
			Tag, Name, Threw string
			Refuse           bool
			Written          *string
		} `json:"elements"`
		Hits []any    `json:"hits"`
		Said []string `json:"said"`
	}
	if err := json.Unmarshal(report, &seen); err != nil {
		t.Fatalf("report %.300s: %v", report, err)
	}

	// the page as the server draws it
	text := func(s *string) string {
		if s == nil {
			return "(none)"
		}
		return *s
	}
	p := seen.Page
	for _, c := range []struct {
		what string
		got  *string
		want string
	}{
		{"a link refused at its first paint", p.First, "(none)"},
		{"the link before its state turns", p.FollowsBefore, "/start"},
		{"the link after its state turns to javascript:", p.FollowsAfter, "(none)"},
		{"a link to a path", p.Stays, "/kept"},
		{"a form's action", p.Action, "(none)"},
		{"a button's formaction", p.Formaction, "(none)"},
		{"an iframe's src", p.Frame, "(none)"},
		{"an SVG link", p.Svg, "(none)"},
		{"an image given javascript:", p.Image, "(none)"},
		{"an image given data:image/…", p.Pixel, "data:image/gif;base64,R0lGODlhAQABAAAAACw="},
	} {
		if text(c.got) != c.want {
			t.Errorf("%s: the browser reads %q, want %q", c.what, text(c.got), c.want)
		}
	}
	if len(seen.Hits) != 0 {
		t.Errorf("javascript: ran in the page: %v", seen.Hits)
	}
	links, images := 0, 0
	for _, line := range seen.Said {
		switch {
		case strings.HasPrefix(line, "hydrate: ") && strings.Contains(line, " goes to a page of the site (/…, ./…, #…), http:, https:, mailto: or tel:, not \"javascript:"):
			links++
		case line == `hydrate: src is an image of the site (/…, ./…), http:, https: or data:image/…, not "javascript:top.hit(3)"`:
			images++
		}
	}
	if links < 6 || images < 1 { // first, follows, action, formaction, frame, svg; the image
		t.Errorf("the console said %d refusals of a place to go and %d of an image, want the page's six and one:\n%s", links, images, strings.Join(seen.Said, "\n"))
	}

	// each value, judged by the browser
	if len(seen.Matrix) != len(values) {
		t.Fatalf("the browser wrote %d values, want %d", len(seen.Matrix), len(values))
	}
	for i, v := range values {
		got := seen.Matrix[i]
		switch {
		case v.Refuse && (got.Href != nil || got.Threw == ""):
			t.Errorf("%s %s %q: written as %q (said %q), want refused and said", v.Tag, v.Name, v.Value, text(got.Href), got.Threw)
		case !v.Refuse && (got.Href == nil || *got.Href != v.Value || got.Threw != ""):
			t.Errorf("%s %s %q: written as %q (said %q), want kept as it is", v.Tag, v.Name, v.Value, text(got.Href), got.Threw)
		case !v.Refuse && v.Tag == "a" && got.Protocol != "unparsed" && got.Protocol != "http:" && got.Protocol != "https:" &&
			got.Protocol != "mailto:" && got.Protocol != "tel:":
			t.Errorf("%q: kept, and the browser goes to %s with it", v.Value, got.Protocol)
		case !v.Refuse && v.Tag == "a" && strings.Contains(v.Value, "javascript") && !got.Own:
			t.Errorf("%q: kept, and the browser reads it as another host, not a path of the page's", v.Value)
		}
	}
	if len(seen.Elements) != 15 {
		t.Fatalf("the browser wrote %d elements, want 15", len(seen.Elements))
	}
	for _, e := range seen.Elements {
		switch {
		case e.Refuse && (e.Written != nil || e.Threw == ""):
			t.Errorf("%s %s: written as %q (said %q), want refused", e.Tag, e.Name, text(e.Written), e.Threw)
		case !e.Refuse && (e.Written == nil || e.Threw != ""):
			t.Errorf("%s %s: written as %q (said %q), want kept: no place to go", e.Tag, e.Name, text(e.Written), e.Threw)
		}
	}
	t.Logf("%d values and %d attributes judged in %s", len(values), len(seen.Elements), filepath.Base(browser))
}

func findHeadlessBrowser() string {
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
