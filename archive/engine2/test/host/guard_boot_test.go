package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A guard written so it can let a request on but by return context.next(), or so it draws a page, is the boot's error
// where it is written, in the file it is imported from too (16/11/2026, the owner, docs/KITWORK_ROUTER.md "Hỏng thì
// đóng"; core/compiler router_guard.go): a branch reaching the guard's end without a return (until now each such
// request was a 403 and the boot said nothing), a value written in a return (true, a comparison), return with nothing,
// context.next() anywhere but straight in a return, a parameter named next, context.view(…), context.next() in a
// door. The guard of docs/KITWORK_MODULE.md, imported from auth.work, which falls through for the admin it means to
// let in, is one. CONTROLS: the fall-through left unread, red; the values written in a return let through, red.
func TestAGuardWrittenWrongIsTheBootsError(t *testing.T) {
	site := "apps/shop/shop.test/"
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a branch without a return", map[string]string{
			site + "config.work": routerImport + "router.guard((context) => {\n  if (context.cookie(\"role\") === \"admin\") {\n    return context.next();\n  }\n}).methods(\"*\");\n"},
			"shop.test/config.work:3:3: the guard reaches its end here without a return"},
		{"the guard of KITWORK_MODULE, imported", map[string]string{
			"apps/shop/auth.work":      "export const admin = (context) => {\n  if (context.cookie(\"role\") !== \"admin\") {\n    return context.redirect(\"/admin/login\");\n  }\n};\n",
			site + "admin/router.work": routerImport + "import { admin } from \"app/auth.work\";\n\nrouter.guard(admin).get((context) => context.text(\"admin\"));\n"},
			"shop/auth.work:2:3: the guard reaches its end here without a return"},
		{"true", map[string]string{site + "config.work": routerImport + "router.guard((context) => true).methods(\"*\");\n"},
			"shop.test/config.work:2:27: the guard returns true: a guard lets the request on with return context.next()"},
		{"a comparison", map[string]string{site + "config.work": routerImport + "router.guard((context) => context.cookie(\"role\") === \"admin\").methods(\"*\");\n"},
			"shop.test/config.work:2:27: the guard returns true or false"},
		{"behind ?:", map[string]string{site + "config.work": routerImport + "router.guard((context) => context.cookie(\"a\") ? context.next() : false).methods(\"*\");\n"},
			"the guard returns false"},
		{"return with nothing", map[string]string{site + "config.work": routerImport + "router.guard((context) => {\n  return;\n}).methods(\"*\");\n"},
			"shop.test/config.work:3:3: the guard returns nothing"},
		{"next() outside a return", map[string]string{site + "config.work": routerImport + "router.guard((context) => {\n  context.next();\n  return context.next();\n}).methods(\"*\");\n"},
			"shop.test/config.work:3:3: context.next() stands straight after the guard's return"},
		{"next() held by a name", map[string]string{site + "config.work": routerImport + "router.guard((context) => {\n  const on = context.next();\n  return on;\n}).methods(\"*\");\n"},
			"shop.test/config.work:3:14: context.next() stands straight after the guard's return"},
		{"a next parameter", map[string]string{site + "config.work": routerImport + "router.guard((context, next) => context.next()).methods(\"*\");\n"},
			"shop.test/config.work:2:24: a guard has no next parameter"},
		{"a page", map[string]string{site + "config.work": routerImport + "router.guard((context) => context.title(\"x\").view({})).methods(\"*\");\n"},
			"shop.test/config.work:2:27: the guard draws a page, context.view(…)"},
		{"next() in a door", map[string]string{site + "router.work": routerImport + "router.get((context) => context.next());\n"},
			"shop.test/router.work:2:25: context.next() is a guard's"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with\n%q\nwant it to say %q", c.name, got, c.want)
		}
	}
}

// What a guard writes right boots: ?: between next() and an answer, if and else each returning, a switch whose every
// case returns, a guard of a const, one imported from a file of the app that returns on every branch; and a value the
// boot cannot see (a call) is the run's, refused with 403, a page drawn by a function the guard calls, context.bind(…),
// as context.view(…) is. CONTROL: the guard's run not refusing context.bind(…), /drawn answers 200 empty, red.
func TestAGuardWrittenRightBoots(t *testing.T) {
	site := "apps/shop/shop.test/"
	b := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":            manifest,
		"apps/shop/auth.work":         "export const admin = (context) => {\n  if (context.cookie(\"role\") !== \"admin\") {\n    return context.redirect(\"/login\");\n  }\n  return context.next();\n};\n",
		site + "admin/router.work":    routerImport + "import { admin } from \"app/auth.work\";\n\nrouter.guard(admin).get((context) => context.text(\"admin\"));\n",
		site + "ternary/router.work":  routerImport + "router.guard((context) => context.cookie(\"a\") === \"1\" ? context.next() : context.error(\"no\")).get((context) => context.text(\"t\"));\n",
		site + "branches/router.work": routerImport + "const staff = (context) => {\n  if (context.cookie(\"a\") === \"1\") {\n    return context.next();\n  } else {\n    return context.status(401).error(\"in\");\n  }\n};\nrouter.guard(staff).get((context) => context.text(\"b\"));\n",
		site + "switch/router.work":   routerImport + "router.guard((context) => {\n  switch (context.cookie(\"a\")) {\n    case \"1\":\n    case \"2\":\n      return context.next();\n    default:\n      return context.error(\"no\");\n  }\n}).get((context) => context.text(\"s\"));\n",
		site + "hidden/router.work":   routerImport + "const decide = (context) => null;\nrouter.guard((context) => decide(context)).get((context) => context.text(\"h\"));\n",
		site + "drawn/router.work":    routerImport + "const draw = (context) => context.bind({});\nrouter.guard((context) => draw(context)).get((context) => context.text(\"d\"));\n",
	}), "application.work"))
	for _, c := range []struct {
		path, cookie string
		status       int
	}{
		{"/admin", "role=admin", http.StatusOK}, {"/admin", "", http.StatusFound},
		{"/ternary", "a=1", http.StatusOK}, {"/ternary", "", http.StatusForbidden},
		{"/branches", "a=1", http.StatusOK}, {"/branches", "", http.StatusUnauthorized},
		{"/switch", "a=2", http.StatusOK}, {"/switch", "a=3", http.StatusForbidden},
		{"/hidden", "", http.StatusForbidden}, {"/drawn", "", http.StatusForbidden},
	} {
		var headers []string
		if c.cookie != "" {
			headers = []string{"Cookie", c.cookie}
		}
		if got := b.request(t, http.MethodGet, "shop.test", c.path, nil, headers...); got.status != c.status {
			t.Errorf("GET %s with %q: %d, want %d", c.path, c.cookie, got.status, c.status)
		}
	}
	contains(t, "the log", b.log.String(), "the guard drew a page, context.bind(…): a guard lets the request on with return context.next() or answers it, never with a page")
}
