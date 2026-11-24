package host_test

import (
	"path/filepath"
	"testing"
)

// A door that answers with context.view(…) in a folder with no content.ui is the boot's error, at the call
// (15/11/2026, the owner, round 3 of the 13/11 review; KITWORK_UI's list of errors, KITWORK_ROUTER.md mục 2): it
// booted, and every request of the door was a 500. The function is read where the door is given it: written there or
// held by a const of the top level, any door, a rule before it or not, a branch as the site's root, whatever its
// parameter is named; a call inside a function of it counts, unless that function names a parameter the same.
func TestAViewWithoutItsPageIsTheBootsError(t *testing.T) {
	site := "apps/shop/shop.test/"
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"a get": {map[string]string{
			site + "router.work": routerImport + "router.get((context) => context.view({ a: 1 }));\n",
		}, "shop.test: context.view(…) at shop.test/router.work:2:25 draws the folder's content.ui, which the folder does not have: add a content.ui to the folder, or answer with context.json(…)"},
		"a const and an if": {map[string]string{
			site + "router.work": routerImport + "const show = (context) => {\n  if (context.query.a) {\n    return context.json({});\n  }\n  return context.view({});\n};\nrouter.get(show);\n",
		}, "context.view(…) at shop.test/router.work:6:10"},
		"a post after a rule, in a branch": {map[string]string{
			site + "orders/router.work": routerImport + "router.guard((context) => context.next()).post((c) => c.view({}));\n",
		}, "shop.test/orders: context.view(…) at shop.test/orders/router.work:2:55"},
		"inside a function of it": {map[string]string{
			site + "router.work": routerImport + "router.get((context) => safe(() => context.view({})));\n",
		}, "context.view(…) at shop.test/router.work:2:36"},
		"the first of two": {map[string]string{
			site + "router.work": routerImport + "router.put((context) => context.view({}));\nrouter.get((context) => context.view({}));\n",
		}, "context.view(…) at shop.test/router.work:2:25"},
	} {
		t.Run(name, func(t *testing.T) {
			got := bootError(t, c.files)
			contains(t, "the boot", got, c.want)
		})
	}
	for name, files := range map[string]map[string]string{
		"its page": {
			site + "router.work": routerImport + "router.get((context) => context.view({ a: 1 }));\n",
			site + "content.ui":  "<content><text>{{ a }}</text></content>",
		},
		"no view": {
			site + "router.work": routerImport + "router.get((context) => context.json({ view: 1 }));\n",
		},
		"a function with a context of its own": {
			site + "router.work": routerImport + "const page = { view: () => 1 };\nrouter.get((context) => {\n  const run = (context) => context.view();\n  return context.json({ n: run(page) });\n});\n",
		},
		"a block with a context of its own": {
			site + "router.work": routerImport + "router.get((context) => {\n  if (true) {\n    const context = { view: () => 2 };\n    return safe(() => context.view());\n  }\n  return context.text(\"x\");\n});\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := bootError(t, files); got != "" {
				t.Errorf("the boot: %s", got)
			}
		})
	}
}

// What a folder with no content.ui answers when its door draws a page only through a function the door calls is
// still the request's error: the boot does not read past the door's own function.
func TestAViewThroughAnotherFunctionIsTheRequestsError(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + "const draw = (context) => context.view({});\n" +
			"router.get((context) => draw(context));\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 500 {
		t.Errorf("GET /: %d %s", got.status, got.body)
	}
}
