package host_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

// viewImport is the imports of a router.work of pages.
const viewImport = routerImport

// componentImport is the import of a file that defines components.
const componentImport = "import * as component from \"kitwork/component\";\n"

// viewFiles are an app whose site draws pages with router.view and components with component.define (docs/view.md,
// component.md), served while developing so its warnings are said.
func viewFiles() map[string]string {
	site := "apps/shop/shop.test/"
	files := map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		"apps/shop/core/components.work": componentImport + `component.define("shop-cart", (context) => {
  const { shippingFee = 15000 } = context.attributes();
  var items = [];
  const checkout = () => { items = []; };
  return context.sync({ checkout }).bind({ items, fee: shippingFee });
});
component.define("deep-box", (context) => context.bind({ n: 1 }));
`,
		site + "components.work": componentImport + `component.define("account-menu", (context) => {
  var opened = false;
  const name = context.cookie("demo-user") === "quoc" ? "Quốc" : "";
  const toggle = () => { opened = !opened; };
  return context.bind({ opened, name, toggle });
});
component.define("app-badge", (context) => context.bind({ from: "site" }));
component.define("json-box", (context) => context.json({}));
component.define("title-box", (context) => context.title("x").bind({}));
component.define("wrong-root", (context) => context.bind({}));
component.define("shadow-box", (context) => context.bind({ query: "component's" }));
component.define("edit-status", (context) => context.status(201).bind({}));
component.define("edit-header", (context) => context.header("X-A", "1").bind({}));
component.define("edit-cookie", (context) => context.cookie("a", "1").bind({}));
component.define("edit-description", (context) => context.description("x").bind({}));
component.define("edit-robots", (context) => context.robots("noindex").bind({}));
component.define("args-box", (context) => { context.attributes(1); return context.bind({}); });
component.define("late-box", (context) => { component.define("late-def", (c) => c.bind({})); return context.bind({}); });
const counting = () => { const seen = []; return (context) => { seen.push(1); return context.bind({ count: seen.length }); }; };
component.define("seen-box", counting());
component.define("synced-title", (context) => context.sync({ a: 1 }).title("x").bind({}));
component.define("busy-box", (context) => { const s = (() => { let n = 0; for (let i = 0; i < 250000; i++) { n = n + 1; } return n; })(); return context.bind({ s }); });
component.define("keys-box", (context) => context.bind({ keys: Object.keys(context.attributes()).join(",") }));
`,
		site + "view.ui":         "<view><account-menu /><content /></view>",
		site + "account-menu.ui": "<account-menu flex=\"row\" gap=\"2\"><text show=\"name == ''\">Đăng nhập thử</text><text show=\"name != ''\">{{ name }}</text></account-menu>",
		site + "wrong-root.ui":   "<box><text>x</text></box>",
		site + "deep-box.ui":     "<deep-box><deep-box /></deep-box>",
		site + "site-footer.ui":  "<site-footer gap=\"2\"><text>Chân trang</text></site-footer>",
		site + "content.ui":      "<content><text>Trang chủ</text></content>",
		site + "products/router.work": viewImport + `router.view((context) => {
  const products = [{ id: 1, name: "Trà" }, { id: 2, name: "Cà phê" }];
  var query = "";
  const startEdit = (id) => { query = "" + id; };
  return context.title("Sản phẩm").cookie("seen", "1").sync({ products }).bind({ query, startEdit, cartShown: false });
});
router.post((context) => context.sync({ products: [{ id: 3, name: "Bánh" }] }).bind({ query: "posted", startEdit: null, cartShown: true }));
`,
		site + "products/content.ui": `<content>
<text for="product of products">{{ product.name }}</text>
<text>[{{ query }}]</text>
<shop-cart shippingFee="{{ 20000 }}" gap="2"><text>phí {{ fee }}, {{ items.length }} món</text></shop-cart>
<shop-cart><text>phí mặc định {{ fee }}</text></shop-cart>
<shop-cart show="cartShown"><text>giỏ ẩn</text></shop-cart>
<app-badge><text>của {{ from }}</text></app-badge>
<site-footer />
<shadow-box><text>{{ query }}</text></shadow-box>
</content>`,
		site + "answers/json/router.work":   viewImport + "router.view((context) => context.json({}));\n",
		site + "answers/json/content.ui":    "<content />",
		site + "answers/view/router.work":   viewImport + "router.view((context) => context.view({}));\n",
		site + "answers/view/content.ui":    "<content />",
		site + "answers/get/router.work":    viewImport + "router.get((context) => context.bind({}));\n",
		site + "answers/synced/router.work": viewImport + "router.get((context) => context.sync({ a: 1 }).json({}));\n",
		site + "answers/plain/content.ui":   "<content><text>plain</text></content>",
		site + "answers/plain/router.work":  viewImport + "router.post((context) => context.bind({}));\n",
		site + "answers/words/router.work": viewImport + `router.get((context) => {
  const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
  return error("said " + JSON.stringify({
    syncArray: code(() => context.sync([1])), bindNothing: code(() => context.bind()),
    both: code(() => context.sync({ a: 1 }).bind({ a: 2 })), syncTwice: code(() => context.sync({ b: 2 })),
    attributes: code(() => context.attributes()),
    cookieRead: safe(() => context.request.cookie("x")).error?.message, cookieValue: code(() => context.cookie("x", "a b")),
    cookieName: code(() => context.cookie("a b", "1")), cookieObject: code(() => context.cookie("x", [1])),
    cookieKey: code(() => context.cookie("x", "1", { age: "1d" })), cookieAge: code(() => context.cookie("x", "1", { maxAge: 5 })),
    cookiePath: code(() => context.cookie("x", "1", { path: "a" })), cookieSameSite: code(() => context.cookie("x", "1", { sameSite: "none" })),
    cookieSecure: code(() => context.cookie("x", "1", { secure: "yes" })), cookieDomain: code(() => context.cookie("x", "1", { domain: 1 })),
    cookieOptions: code(() => context.cookie("x", "1", 3)), cookieSameSiteWord: code(() => context.cookie("x", "1", { sameSite: "loose" })),
    cookieFour: code(() => context.cookie("x", "1", {}, 1)), cookieHttpOnly: code(() => context.cookie("x", "1", { httpOnly: 1 })),
    cookieAfter: code(() => { context.notFound(); return context.cookie("y", "1"); })
  }));
});
`,
		site + "cookies/router.work": viewImport + `router.get((context) => context
  .cookie("a", "1", { maxAge: "1d", path: "/cookies", httpOnly: false, sameSite: "strict", domain: "shop.test" })
  .cookie("b", null)
  .cookie("c", 2, { sameSite: "none", secure: true })
  .cookie("c", 3, { sameSite: "none", secure: true })
  .cookie("d", null, { maxAge: "1d" })
  .text("ok"));
`,
		site + "components/json/router.work":      viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/json/content.ui":       "<content><json-box /></content>",
		site + "components/title/router.work":     viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/title/content.ui":      "<content><title-box /></content>",
		site + "components/root/router.work":      viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/root/content.ui":       "<content><wrong-root /></content>",
		site + "components/deep/router.work":      viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/deep/content.ui":       "<content><deep-box /></content>",
		site + "components/source/router.work":    viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/source/content.ui":     "<content><shop-cart source=\"x\" /></content>",
		site + "components/missing/router.work":   viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/missing/content.ui":    "<content><app-badge /></content>",
		site + "components/input/router.work":     viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/input/content.ui":      "<content><shop-cart ship-fee=\"1\"><text>x</text></shop-cart></content>",
		site + "components/attribute/router.work": viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/attribute/content.ui":  "<content><account-menu gap=\"3\" /></content>",
		site + "components/several/router.work":   viewImport + "router.view((context) => context.bind({}));\n",
		site + "components/several/content.ui":    "<content><busy-box><text>{{ s }}</text></busy-box><busy-box><text>{{ s }}</text></busy-box><keys-box as=\"$k\" a=\"1\" gap=\"2\"><text>[{{ keys }}]</text></keys-box></content>",
	}
	for _, name := range []string{"edit-status", "edit-header", "edit-cookie", "edit-description", "edit-robots", "args-box", "late-box", "seen-box", "synced-title"} {
		files[site+"components/"+name+"/router.work"] = viewImport + "router.view((context) => context.bind({}));\n"
		files[site+"components/"+name+"/content.ui"] = "<content><" + name + "><text>x</text></" + name + "></content>"
	}
	return files
}

// A GET of a folder with router.view(fn) runs fn and draws the folder's content.ui in its frame with the names its
// context.bind({ … }) binds and the sync({ … }) before it (docs/view.md mục 2), the head and cookies written before:
// never kept by a shared cache. A router.post of the folder answers with the same page. Each component on the page
// (component.define) runs with the inputs written on its tag, read where it stands, and draws its interface as one
// box with its names before those of the page: the tag's content, or the file whose root is the tag; the site's
// definition over the app's. A layout whose root is its own tag is a box, never a call of itself.
func TestRouterViewDrawsItsPageAndItsComponents(t *testing.T) {
	b := boot(t, filepath.Join(write(t, viewFiles()), "application.work"))
	got := b.get(t, "shop.test", "/products")
	if got.status != http.StatusOK {
		t.Fatalf("GET /products: %d\n%s\n%s", got.status, got.body, b.log.String())
	}
	// the page binds a work and a var: it runs in the browser, each value it reads kept by the kernel (docs/view.md mục 4)
	contains(t, "the page", got.body, "<title>Sản phẩm</title>", `data-kitwork-key="0" data-kitwork-bind="text: product.name">Trà</p>`,
		`data-kitwork-key="1" data-kitwork-bind="text: product.name">Cà phê</p>`, `data-kitwork-bind="text: [&#39;[&#39;, (query), &#39;]&#39;].join(&#39;&#39;)">[]</p>`,
		"phí 20000, 0 món", "phí mặc định 15000", "của site", "Chân trang", "Đăng nhập thử")
	if got.header.Get("Cache-Control") != "no-store" || !strings.Contains(got.header.Get("Set-Cookie"), "seen=1; Path=/; HttpOnly; SameSite=Lax") {
		t.Errorf("headers: Cache-Control %q, Set-Cookie %q", got.header.Get("Cache-Control"), got.header.Get("Set-Cookie"))
	}
	// show on the tag is read where it stands (the page's cartShown, false), never the component's names
	contains(t, "a component the page hides", got.body, `<div class="flex-column show-false" data-kitwork-component="shop-cart" data-kitwork-at="`, `"><p>giỏ ẩn</p></div>`)
	// the interface used again is the file whose root is the tag, drawn as one box with the root's look
	// it binds a var and works: an instance the browser runs, its names the kernel's (bridge_test.go)
	contains(t, "the account menu", got.body, `<div class="flex-row gap-2" data-kitwork-component="account-menu" data-kitwork-at="`,
		`"><p data-kitwork-bind="show: name == &#39;&#39;">Đăng nhập thử</p><p hidden data-kitwork-bind="show: name != &#39;&#39;; text: name"></p></div>`)
	contains(t, "the log", b.log.String(), "query của <shadow-box> che tên query ở chỗ gọi")
	signedIn := b.request(t, http.MethodGet, "shop.test", "/products", nil, "Cookie", "demo-user=quoc")
	contains(t, "the page of a visitor signed in", signedIn.body, `data-kitwork-bind="show: name != &#39;&#39;; text: name">Quốc</p>`)
	posted := b.request(t, http.MethodPost, "shop.test", "/products", strings.NewReader(url.Values{"x": {"1"}}.Encode()), "Content-Type", "application/x-www-form-urlencoded")
	if posted.status != http.StatusOK {
		t.Fatalf("POST /products: %d\n%s", posted.status, b.log.String())
	}
	// its sync holds a live name: the page runs in the browser, for a tab (bridge_test.go)
	contains(t, "the page a post answers with", posted.body, `data-kitwork-key="0" data-kitwork-bind="text: product.name">Bánh</p>`, `>[posted]</p>`, `,"tab":"`)
}

// What router.view, context.bind, sync, cookie and a component's run refuse (docs/view.md mục 2, component.md mục
// 3, router.md mục 6): each a 500, the reason in the log, or a failure the route catches.
func TestViewsAndComponentsRefuseWhatTheyDoNotTake(t *testing.T) {
	b := boot(t, filepath.Join(write(t, viewFiles()), "application.work"))
	for path, says := range map[string]string{
		"/answers/json":                "router.view answered with context.json(…)",
		"/answers/view":                "router.view answered with context.view(…)",
		"/answers/get":                 "context.bind(…) draws the page of router.view",
		"/components/json":             "a component answers with return context.bind",
		"/components/title":            "wrote context.title(…)",
		"/components/root":             "giao diện dùng lại của component wrong-root có gốc là chính thẻ <wrong-root>",
		"/components/deep":             "<deep-box /> gọi deep-box.ui bên trong chính nó",
		"/components/source":           "source trên <shop-cart>",
		"/components/missing":          "<app-badge /> không có nội dung và không tìm thấy app-badge.ui",
		"/components/input":            "đầu vào ship-fee của <shop-cart> không phải một tên",
		"/components/attribute":        "gap viết cả ở chỗ gọi <account-menu /> và ở gốc của account-menu.ui",
		"/answers/synced":              "context.sync(…) before json(…)",
		"/components/edit-status":      "wrote context.status(…)",
		"/components/edit-header":      "wrote context.header(…)",
		"/components/edit-cookie":      "wrote context.cookie(…)",
		"/components/edit-description": "wrote context.description(…)",
		"/components/edit-robots":      "wrote context.robots(…)",
		"/components/args-box":         "context.attributes() takes nothing",
		"/components/late-box":         "component.define(…) khai ở cấp file của một file .work thường của app",
		"/components/seen-box":         "<seen-box>: TYPE",
		"/components/synced-title":     "<synced-title>: component.define(\"synced-title\", …) at shop.test/components.work:22:10 wrote context.title(…)",
	} {
		if got := b.get(t, "shop.test", path); got.status != http.StatusInternalServerError {
			t.Errorf("%s: %d, want 500", path, got.status)
		}
		contains(t, path+": the log", b.log.String(), says)
	}
	b.get(t, "shop.test", "/answers/words") // a sync tried leaves the page's answer: the words are said in the log
	contains(t, "the words", b.log.String(), `said {"syncArray":"INVALID_ARGUMENT","bindNothing":"INVALID_ARGUMENT",`+
		`"both":"INVALID_ARGUMENT","syncTwice":"TYPE","attributes":"TYPE","cookieRead":"context.request.cookie(name) is context.cookie(name) now: one word for a cookie, one argument reads the one the browser sent, two set one with the answer",`+
		`"cookieValue":"INVALID_ARGUMENT","cookieName":"INVALID_ARGUMENT","cookieObject":"INVALID_ARGUMENT","cookieKey":"INVALID_ARGUMENT",`+
		`"cookieAge":"INVALID_ARGUMENT","cookiePath":"INVALID_ARGUMENT","cookieSameSite":"INVALID_ARGUMENT","cookieSecure":"INVALID_ARGUMENT",`+
		`"cookieDomain":"INVALID_ARGUMENT","cookieOptions":"INVALID_ARGUMENT","cookieSameSiteWord":"INVALID_ARGUMENT",`+
		`"cookieFour":"INVALID_ARGUMENT","cookieHttpOnly":"INVALID_ARGUMENT","cookieAfter":"TYPE"}`)
	if got := b.request(t, http.MethodPost, "shop.test", "/answers/plain", nil); got.status != http.StatusInternalServerError {
		t.Errorf("POST bind in a folder with a page and no router.view: %d, want 500", got.status)
	}
	contains(t, "the log", b.log.String(), "router.post(…) answers with it only in a folder that has router.view")
	// each component runs with a request's energy of its own; its inputs are what its tag writes but as and its look
	contains(t, "two busy components", b.get(t, "shop.test", "/components/several").body, "<p>250000</p><", "<p>[a]</p>")
	cookies := b.get(t, "shop.test", "/cookies").header.Values("Set-Cookie")
	if strings.Join(cookies, " | ") != "a=1; Path=/cookies; Domain=shop.test; Max-Age=86400; SameSite=Strict | b=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax | "+
		"c=3; Path=/; HttpOnly; Secure; SameSite=None | d=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" {
		t.Errorf("cookies: %q", cookies)
	}
}

// A cookie of a page answered over HTTPS is Secure unless it says otherwise.
func TestACookieOverHTTPSIsSecure(t *testing.T) {
	read, err := host.ReadWithLog(filepath.Join(write(t, viewFiles()), "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(read)
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = read.Close() })
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/products", nil)
	request.Host = "shop.test"
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := response.Header.Get("Set-Cookie"); got != "seen=1; Path=/; HttpOnly; Secure; SameSite=Lax" {
		t.Errorf("a cookie over HTTPS: %q", got)
	}
}

// What the boot refuses of components and views (docs/component.md mục 2, view.md mục 2).
func TestComponentsAndViewsAreDeclaredWhereTheSpecSays(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"a component in router.work": {map[string]string{
			"apps/shop/shop.test/router.work": componentImport + "component.define(\"shop-cart\", (context) => context.bind({}));\n",
		}, "component.define(…) khai ở cấp file của một file .work thường của app (không phải router.work"},
		"a name without a dash": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"cart\", (context) => context.bind({}));\n",
		}, "a site's component has a dash in its name"},
		"a name in capitals": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"Shop-Cart\", (context) => context.bind({}));\n",
		}, "lower-case letters, digits and dashes"},
		"a name with two dashes": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"shop--cart\", (context) => context.bind({}));\n",
		}, "joined by single dashes"},
		"a name of a digit": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"1-cart\", (context) => context.bind({}));\n",
		}, "begins with a letter"},
		"no function": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"shop-cart\", 1);\n",
		}, "takes the component's function"},
		"no name": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(1, (context) => context.bind({}));\n",
		}, "takes the tag's name first"},
		"twice": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"shop-cart\", (c) => c.bind({}));\ncomponent.define(\"shop-cart\", (c) => c.bind({}));\n",
		}, "again: it is defined at shop.test/components.work:2:"},
		"one name in two files": {map[string]string{
			"apps/shop/core/a.work":               componentImport + "component.define(\"shop-cart\", (c) => c.bind({}));\n",
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"shop-cart\", (c) => c.bind({}));\n",
		}, "component.define(\"shop-cart\", …) at shop.test/components.work:2:10 again: it is defined at core/a.work:2:10; one definition a name in the app"},
		"a door in components.work": {map[string]string{
			"apps/shop/shop.test/components.work": routerImport + "router.get((context) => context.text(\"x\"));\n",
		}, "kitwork/router is imported by components.work, a plain file"},
		"a view without its page": {map[string]string{
			"apps/shop/shop.test/router.work": viewImport + "router.view((context) => context.bind({}));\n",
		}, "draws the folder's content.ui, which the folder does not have"},
		"an empty name": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.define(\"\", (context) => context.bind({}));\n",
		}, "the name is empty"},
		"a word kitwork/component has not": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "component.definee(\"shop-cart\", (context) => context.bind({}));\n",
		}, "kitwork/component has no definee"},
		"an alias": {map[string]string{
			"apps/shop/shop.test/components.work": componentImport + "const { $toast } = component.alias();\n",
		}, "component.alias() is not served by engine2 yet"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", name, got, c.want)
		}
	}
}
