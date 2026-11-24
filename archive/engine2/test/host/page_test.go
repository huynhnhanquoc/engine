package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// pageSite is an app whose site draws pages of .ui: a frame, a route that gives its page data, a folder with a
// page and no door, a notfound.ui, and two routes whose page cannot be drawn.
func pageSite(t *testing.T) *booted {
	t.Helper()
	site := "apps/shop/shop.test/"
	folder := write(t, map[string]string{
		"application.work":          manifest,
		site + "view.ui":            "<view background=\"canvas\"><header /><content /></view>",
		site + "header.ui":          "<header><text>Shop</text></header>",
		site + "content.ui":         "<content gap=\"4\"><text heading=\"1\">{{ user.name }}</text><text for=\"item of items\">{{ item }}</text></content>",
		site + "router.work":        routerImport + "router.get((context) => context.title(\"Trang <1>\").description(\"Mô tả\").view({ user: { name: \"An\" }, items: [\"trà\", \"cà phê\"] }));\n",
		site + "about/content.ui":   "<content><text>About</text></content>",
		site + "notfound.ui":        "<content><text>Không có trang này</text></content>",
		site + "broken/content.ui":  "<content><text>{{ nope }}</text></content>",
		site + "broken/router.work": routerImport + "router.get((context) => context.view({}));\n",
		site + "number/router.work": routerImport + "router.get((context) => context.view(42));\n",
		site + "number/content.ui":  "<content />",
	})
	return boot(t, filepath.Join(folder, "application.work"))
}

// context.view draws the route's content.ui in its frame with what the handler gives it, a whole HTML document
// whose head has context.title and context.description; a folder with a page and no door draws it given
// nothing; a path no folder has draws the nearest notfound.ui, a 404 (KITWORK_UI mục 4).
func TestAViewDrawsTheRoutesPageInItsFrame(t *testing.T) {
	b := pageSite(t)
	home := b.get(t, "shop.test", "/")
	if home.status != http.StatusOK || home.header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET /: %d %s %q", home.status, home.header.Get("Content-Type"), home.body)
	}
	for _, want := range []string{
		"<!doctype html>", "<title>Trang &lt;1&gt;</title>", `<meta name="description" content="Mô tả">`,
		`<body class="flex-column background-canvas" data-kitwork-layout="view.ui">`, `<header class="flex-column"><p>Shop</p></header>`, `<main class="flex-column gap-4"><h1>An</h1><p>trà</p><p>cà phê</p></main>`,
		".gap-4{",
	} {
		if !strings.Contains(home.body, want) {
			t.Errorf("GET / does not hold %s:\n%s", want, home.body[strings.Index(home.body, "<body"):])
		}
	}
	if about := b.get(t, "shop.test", "/about"); about.status != http.StatusOK || !strings.Contains(about.body, `<main class="flex-column"><p>About</p></main>`) {
		t.Errorf("GET /about: %d, want the page given nothing:\n%s", about.status, about.body)
	}
	missing := b.get(t, "shop.test", "/nowhere")
	if missing.status != http.StatusNotFound || !strings.Contains(missing.body, "<p>Không có trang này</p>") || !strings.Contains(missing.body, `<header class="flex-column">`) {
		t.Errorf("GET /nowhere: %d, want notfound.ui in the frame, a 404:\n%s", missing.status, missing.body)
	}
	if head := b.request(t, http.MethodHead, "shop.test", "/", nil); head.status != http.StatusOK || head.body != "" {
		t.Errorf("HEAD /: %d %q, want 200 with no body", head.status, head.body)
	}
}

// A page that cannot be drawn is a 500 whose reason, at its place in the .ui file, goes to the log and never
// to the visitor; context.view of something that is no object is the handler's failure.
func TestAPageThatCannotBeDrawnIs500WithItsPlaceInTheLog(t *testing.T) {
	b := pageSite(t)
	broken := b.get(t, "shop.test", "/broken")
	if broken.status != http.StatusInternalServerError || strings.Contains(broken.body, "nope") {
		t.Errorf("GET /broken: %d %q, want a 500 that names nothing", broken.status, broken.body)
	}
	if log := b.log.String(); !strings.Contains(log, "broken/content.ui:1:16: trang không được đưa tên nope") {
		t.Errorf("the log does not say where /broken failed:\n%s", log)
	}
	number := b.get(t, "shop.test", "/number")
	if number.status != http.StatusInternalServerError {
		t.Errorf("GET /number, context.view(42): %d, want 500", number.status)
	}
	if log := b.log.String(); !strings.Contains(log, "context.view(…) takes an object") {
		t.Errorf("the log does not say what context.view(42) is:\n%s", log)
	}
}
