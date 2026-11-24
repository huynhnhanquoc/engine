package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// The doors of a folder are read from its files' words (docs/KITWORK_ROUTER.md mục 3 and 11): the router is
// imported on one line, import * as router from "kitwork/router", and named only at the head of a statement
// of the file's top level. Every other spelling is a boot error at its line (05/11/2026; each of these booted
// and served, and helper(router) let a .shared library declare the folder's doors).
func TestTheRoutersSpellingIsReadAtBoot(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"the router handed to a library": {map[string]string{
			"apps/shop/core/doors.work":       "export const declare = (r) => { r.get((context) => context.text(\"library\")); };\n",
			"apps/shop/shop.test/router.work": routerImport + "import { declare } from \"app/core/doors.work\";\ndeclare(router);\n",
		}, "router.work:3:9: router is read only as router.<word>(…)"},
		"const r = router": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "const r = router;\nr.get((context) => context.text(\"r\"));\n",
		}, "router.work:2:11: router is read only as router.<word>(…)"},
		"router[\"get\"]": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router[\"get\"]((context) => context.text(\"bracket\"));\n",
		}, "router.work:2:1: router is read only as router.<word>(…)"},
		"a door in an if": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "if (1 > 0) { router.get((context) => context.text(\"if\")); }\n",
		}, "router.work:2:20: router.get(…) is written at the top level of the file, not in an if"},
		"a door in a loop": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "for (const m of [1]) { router.get((context) => context.text(\"loop\")); }\n",
		}, "router.get(…) is written at the top level of the file"},
		"a door in a callback": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "[1].forEach((m) => { router.get((context) => context.text(\"callback\")); });\n",
		}, "router.get(…) is written at the top level of the file"},
		"a door in a function's body": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "const open = () => { router.get((context) => context.text(\"function\")); };\nopen();\n",
		}, "router.get(…) is written at the top level of the file"},
		"a door in a handler": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => { router.post((other) => other.text(\"x\")); return context.text(\"a\"); });\n",
		}, "router.post(…) is written at the top level of the file"},
		"import * as r": {map[string]string{
			"apps/shop/shop.test/router.work": "import * as r from \"kitwork/router\";\nr.get((context) => context.text(\"r\"));\n",
		}, "router.work:1:1: the router is imported on one line, import * as router from \"kitwork/router\""},
		"import { get }": {map[string]string{
			"apps/shop/shop.test/router.work": "import { get } from \"kitwork/router\";\nget((context) => context.text(\"get\"));\n",
		}, "router.work:1:1: the router is imported on one line"},
		"import router": {map[string]string{
			"apps/shop/shop.test/router.work": "import router from \"kitwork/router\";\nrouter.get((context) => context.text(\"default\"));\n",
		}, "router.work:1:1: the router is imported on one line"},
		"import { get as g }": {map[string]string{
			"apps/shop/shop.test/router.work": "import { get as g } from \"kitwork/router\";\ng((context) => context.text(\"g\"));\n",
		}, "router.work:1:1: the router is imported on one line"},
		"a file-level const get": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "const get = 1;\nrouter.get((context) => context.text(\"get \" + get));\n",
		}, "router.work:2:7: const get at the top level of router.work looks like a door"},
		"a chain kept in a const": {map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "const cached = router.cache(\"5m\");\ncached.get((context) => context.text(\"cached\"));\n",
		}, "router.work:2:22: router.cache(…) is a statement of its own"},
		"in config.work too": {map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "const r = router;\n",
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		}, "shop/shop.test/config.work: config.work:2:11: router is read only as router.<word>(…)"},
	} {
		got := bootError(t, c.files)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: boot error %q, want one saying %q", name, got, c.want)
		}
	}
}

// What the spelling allows boots as before: a rule before a door, a variable router in a door's handler
// under another name of the handler's own, a const of another name, and a door's handler kept in a const.
func TestTheRoutersOneSpellingBoots(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `const list = (context) => context.text("list");
const getter = 1;
router.cache("5m").get(list);
router.post((context) => { const router = context.request; return context.text("" + typeof router); });
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 200 || got.body != "list" {
		t.Errorf("GET /: %d %q", got.status, got.body)
	}
}
