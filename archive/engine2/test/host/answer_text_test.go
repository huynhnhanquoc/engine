package host_test

import (
	"path/filepath"
	"testing"
)

// An answer made of a value's text is the failure of making that text, as every other conversion is
// (KITWORK_BOOT mục 10, TYPE at every conversion): an object the file made at boot, whose toString a
// request cannot run, or one whose own toString fails, given to text, html, svg, error, a header or the
// request's query is a 500 with the failure in the log (05/11/2026: each read the failure as "", so the
// site answered 200 with an empty body, an empty header, an empty error page, or read the query "").
func TestAnAnswerOfATextThatCannotBeMadeFails(t *testing.T) {
	routes := map[string]string{
		"text":   `context.text(page)`,
		"html":   `context.html(page)`,
		"svg":    `context.svg(page)`,
		"header": `context.header("x-a", page).text("ok")`,
		// headers converts each value as header does (05/11/2026: an empty X-A and 200 before)
		"headers":    `context.headers({ "x-a": page }).text("ok")`,
		"headersOwn": `context.headers({ "x-b": "b", "x-a": { toString: () => error("own failure") } }).text("ok")`,
		"error":      `context.status(404).error(page)`,
		"query":      `context.text("[" + context.query(page) + "]")`,
		"own":        `context.text({ toString: () => error("own failure") })`,
	}
	files := map[string]string{"application.work": manifest}
	for name, answer := range routes {
		files["apps/shop/shop.test/"+name+"/router.work"] = routerImport + `const page = { toString: () => "hello" };
router.get((context) => ` + answer + `);
`
	}
	files["apps/shop/shop.test/made/router.work"] = routerImport + `router.get((context) => context.text({ toString: () => "made here" }));
`
	app := boot(t, filepath.Join(write(t, files), "application.work"))
	for name := range routes {
		if got := app.get(t, "shop.test", "/"+name); got.status != 500 || got.body != "Internal Server Error\n" && got.body != "Internal Server Error" {
			t.Errorf("GET /%s: %d %q (x-a %q), want the 500 of a failure", name, got.status, got.body, got.header.Get("X-A"))
		}
	}
	contains(t, "the log", app.log.String(),
		"GET /text (/text/router.work): 500: TYPE: ", "was made as the site booted",
		"GET /header (/header/router.work): 500: TYPE: ", "GET /query (/query/router.work): 500: TYPE: ",
		"GET /headers (/headers/router.work): 500: TYPE: ", "GET /headersOwn (/headersOwn/router.work): 500: ",
		"GET /own (/own/router.work): 500: ", "own failure")
	if got := app.get(t, "shop.test", "/made"); got.status != 200 || got.body != "made here" {
		t.Errorf("GET /made, an object the request made: %d %q, want 200 made here", got.status, got.body)
	}
}

// An answer word that fails edits nothing (KITWORK_ROUTER.md mục 6, every edit before the answer, the
// answer makes and closes it): error(message) whose message cannot be made text leaves the status as it
// was, so what the handler answers next is its own answer (05/11/2026: error wrote the 500 before it made
// the message text, so text(…) after a failed error inside safe went out as a 500, and a redirect after it
// failed, "status(500) before redirect(…)").
func TestAnErrorThatFailsLeavesTheStatusAsItWas(t *testing.T) {
	routes := map[string]string{
		"/text":     `const r = safe(() => context.error({ toString: () => error("x") })); return context.text("after " + (r.error === null));`,
		"/json":     `const r = safe(() => context.error({ toString: () => error("x") })); return context.json({ ok: r.error === null });`,
		"/booted":   `const r = safe(() => context.error(page)); return context.text("after " + (r.error === null));`,
		"/redirect": `safe(() => context.error({ toString: () => error("x") })); return context.redirect("/login");`,
		"/status":   `const r = safe(() => context.status(404).error(page)); return context.text("after " + (r.error === null));`,
	}
	files := map[string]string{"application.work": manifest}
	for path, body := range routes {
		files["apps/shop/shop.test"+path+"/router.work"] = routerImport + `const page = { toString: () => "hello" };
router.get((context) => { ` + body + ` });
`
	}
	app := boot(t, filepath.Join(write(t, files), "application.work"))
	for path, want := range map[string]struct {
		status int
		body   string
	}{
		"/text": {200, "after false"}, "/json": {200, `{"ok":false}`}, "/booted": {200, "after false"},
		"/redirect": {302, ""}, "/status": {404, "after false"},
	} {
		got := app.get(t, "shop.test", path)
		if got.status != want.status || want.body != "" && got.body != want.body {
			t.Errorf("GET %s: %d %q, want %d %q", path, got.status, got.body, want.status, want.body)
		}
	}
}
