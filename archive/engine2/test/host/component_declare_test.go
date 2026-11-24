package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Components declare themselves (slice 7 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026; docs/component.md mục 2):
// component.define at the top level of any plain .work file of the app, for every site of it, its interface <name>.ui
// beside the file that declares it.

// componentApp is an app of two sites, shop.test and blog.test, whose component shop-cart is declared in core/cart.work
// with its interface core/shop-cart.ui beside it; shop.test draws its own shop-cart.ui over it. aa/uses.work, read before
// core/ in the order of paths, imports core/cart.work, so its declaration runs first there; shop.test's page imports it
// too.
func componentApp() map[string]string {
	return map[string]string{
		"application.work": manifest,
		"apps/shop/core/cart.work": componentImport + `export const fee = 15000;

component.define("shop-cart", (context) => {
  const { owner = "khách" } = context.attributes();
  return context.bind({ owner, fee });
});
`,
		"apps/shop/core/shop-cart.ui":       `<shop-cart gap="2"><text>giỏ chung của {{ owner }}, phí {{ fee }}</text></shop-cart>`,
		"apps/shop/aa/uses.work":            "import { fee } from \"app/core/cart.work\";\nexport const twice = fee * 2;\n",
		"apps/shop/shop.test/router.work":   routerImport + "import { fee } from \"app/core/cart.work\";\nrouter.get((context) => context.view({ fee }));\n",
		"apps/shop/shop.test/content.ui":    `<content><shop-cart owner="Quốc" /><text>[{{ fee }}]</text></content>`,
		"apps/shop/shop.test/shop-cart.ui":  `<shop-cart><text>giỏ của shop, {{ owner }}</text></shop-cart>`,
		"apps/shop/blog.test/content.ui":    `<content><shop-cart owner="An" /></content>`,
		"apps/shop/blog.test/_draft/x.work": "export const unread = 1;\n",
	}
}

// A component declared in core/ is the app's: both sites draw it, blog.test with the interface beside its declaration,
// shop.test with its own shop-cart.ui, the nearest from the page up to the site's root. A file that imports the
// declaring file, before it in the boot's order or as a page, runs the declaration again and finds it by its place: no
// "again". kitwork check's map has the component and its place.
//
// CONTROL (17/11/2026): with the load pass declaring nothing (Define not adding to App), the app closed, shop.test's
// router.work importing core/cart.work whose declaration it did not find; with Define not finding a declaration by
// its place, it closed, "component.define("shop-cart", …) at core/cart.work:4:10 again"; with the pass that declares
// the tables not passing over a component, it closed at aa/uses.work; with the interface beside the declaration not
// found, blog.test answered 500; with it looked for before the site's own, shop.test did not draw "giỏ của shop"; with
// the map leaving components out, the map had none.
func TestAComponentDeclaresItselfInAnyPlainFile(t *testing.T) {
	b := boot(t, filepath.Join(write(t, componentApp()), "application.work"))
	shop := b.get(t, "shop.test", "/")
	if shop.status != http.StatusOK || !strings.Contains(shop.body, "giỏ của shop, Quốc") || !strings.Contains(shop.body, "[15000]") {
		t.Errorf("shop.test, its own interface: %d %s\n%s", shop.status, shop.body, b.log)
	}
	blog := b.get(t, "blog.test", "/")
	if blog.status != http.StatusOK || !strings.Contains(blog.body, "giỏ chung của An, phí 15000") {
		t.Errorf("blog.test, the interface beside the declaration: %d %s\n%s", blog.status, blog.body, b.log)
	}
	contains(t, "the map", spaced(b.host.Map()), "component shop-cart core/cart.work:4:10")
}

// A component declared in a file the boot does not read, in a folder of underscore (a draft) or a dot folder, is no
// component of the app: a page whose file imports that file stops the boot of the app saying so.
//
// CONTROL (17/11/2026): with Define keeping a declaration of any run, the app booted and drew the draft's component.
func TestAComponentInAFileTheBootDoesNotReadIsNone(t *testing.T) {
	files := componentApp()
	files["apps/shop/blog.test/_draft/x.work"] = componentImport + "component.define(\"draft-box\", (context) => context.bind({}));\nexport const unread = 1;\n"
	files["apps/shop/blog.test/router.work"] = routerImport + "import { unread } from \"./_draft/x.work\";\nrouter.get((context) => context.view({ unread }));\n"
	got := bootError(t, files)
	if !strings.Contains(got, `component.define("draft-box", …) at blog.test/_draft/x.work:2:10`) ||
		!strings.Contains(got, "the boot declares the components of the plain .work files it reads, and not of this one, in a folder of _ or .: move it to a folder it reads, core/") {
		t.Errorf("a component in a draft: %q", got)
	}
}
