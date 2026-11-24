package host_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

const routerImport = "import * as router from \"kitwork/router\";\n"

// What the slice cannot serve, or what is written wrong, closes its app with the line that says it: a site
// is never served half open, and a rule left out never opens what it closes.
func TestABootStopsWhereASiteIsWrittenWrong(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a door twice", map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\nrouter.get((context) => context.text(\"b\"));\n",
		}, "router.get(…) declares GET again: the folder's GET door is at shop.test/router.work:2:"},
		{"a door in config.work and router.work", map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "router.post((context) => context.text(\"a\"));\n",
		}, "router.post(…) in config.work: a door is declared in router.work"},
		{"router.view beside router.get", map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.view((context) => context.bind({}));\nrouter.get((context) => context.text(\"b\"));\n",
		}, "router.get(…) declares GET again"},
		{"a guard alone", map[string]string{
			"apps/shop/shop.test/admin/config.work": routerImport + "router.guard((context) => context.next());\n",
		}, "shop/shop.test/admin/config.work: router.guard(…) at shop.test/admin/config.work:2:"},
		{"a rule after its door", map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\")).limit({ rate: 1, per: \"1m\" });\n",
		}, "router.get(…).limit(…): a rule stands before its door, router.limit(…).get(…)"},
		{"a misspelt word", map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "router.faviconn(\"/assets/icon.svg\");\n",
		}, "router has no word faviconn: did you mean router.favicon?"},
		{"the look written in config.work", map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "router.colors({ brand: \"red\" });\n",
		}, "config.work:2:7: router.colors is an old word: write design.colors(…) in the folder's design.work"},
		{"an old word", map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "router.ratelimit(10);\n",
		}, "router.ratelimit is an old word: write router.limit({ rate, per, by })"},
		{"a timezone of a site", map[string]string{
			"apps/shop/shop.test/config.work": routerImport + "router.timezone(\"Asia/Ho_Chi_Minh\");\n",
		}, "router.timezone is an old word: write module.timezone(\"Asia/Ho_Chi_Minh\") in module.work, the app's root"},
		{"a timezone that is no zone", map[string]string{
			"apps/shop/module.work": zonedModule("Asia/Saigon City"),
		}, "module.timezone(\"Asia/Saigon City\"): not an IANA zone name"},
		{"the server's own zone", map[string]string{
			"apps/shop/module.work": zonedModule("Local"),
		}, "the server's own clock (\"Local\") is no app's zone"},
		{"a module not served", map[string]string{
			"apps/shop/shop.test/router.work": "import * as tunnel from \"kitwork/tunnel\";\n" + routerImport,
		}, "router.work:1:1: kitwork/tunnel is not served by engine2 yet"},
		{"the manifest's module in a site", map[string]string{
			"apps/shop/shop.test/router.work": "import * as app from \"kitwork/application\";\n",
		}, "kitwork/application is the host's manifest's"},
		{"a handler that is no function", map[string]string{
			"apps/shop/shop.test/router.work": routerImport + "router.get(\"hello\");\n",
		}, "router.get(…) takes the handler, a function of the program"},
		{"a segment of no type", map[string]string{
			"apps/shop/shop.test/{id[int]}/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		}, "folder {id[int]}: [int] is no type of a segment; the two are [number] and [string]"},
		{"a regex that does not compile", map[string]string{
			"apps/shop/shop.test/{code([A-Z)}/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		}, "the regex does not compile"},
		{"a site in two apps", map[string]string{
			"apps/one/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
			"apps/two/Shop.Test/router.work": routerImport + "router.get((context) => context.text(\"b\"));\n",
		}, "the site shop.test is in two apps"},
		{"a .env out of the app", map[string]string{
			"apps/shop/shop.test/router.work": "import * as file from \"kitwork/file\";\nconst env = file.env(\"app/../../secret.env\");\n",
		}, "file.env(\"app/../../secret.env\"): the path is outside the app's root"},
		{"a .env written without its place", map[string]string{
			"apps/shop/shop.test/router.work": "import * as file from \"kitwork/file\";\nconst env = file.env(\".env\");\n",
		}, "a path starts with ./ (this file's folder) or app/ (the app's root)"},
		{"the layout of one app", map[string]string{
			"application.work":          "import * as app from \"kitwork/application\";\napp.root(\"app\");\napp.website({ hostname: \"shop.test\", port: 8080 });\n",
			"app/shop.test/router.work": routerImport,
		}, "app/<domain>/, which engine2 does not serve; one app is the folder of its module.work"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}

// Every key env.require misses, in every file of every site, is one boot error naming each key with where
// it is required, never a value; a key that is set is its value.
func TestEveryMissingRequiredKeyIsOneBootError(t *testing.T) {
	got := bootError(t, map[string]string{
		"apps/shop/app.env":                    "SET=yes\n",
		"apps/shop/shop.test/router.work":      "import * as file from \"kitwork/file\";\nconst env = file.env(\"app/app.env\");\nconst set = env.require(\"SET\");\nconst stripe = env.require(\"STRIPE_SECRET\");\n",
		"apps/shop/shop.test/mail/router.work": "import * as file from \"kitwork/file\";\nconst env = file.env(\"app/app.env\");\nconst smtp = env.require(\"SMTP_URL\");\n",
	})
	want := "env.require: no file sets STRIPE_SECRET (shop.test/router.work:4:"
	if !strings.Contains(got, want) || !strings.Contains(got, "SMTP_URL (shop.test/mail/router.work:3:") || strings.Contains(got, "SET (") {
		t.Errorf("the boot ends with %q, want one error naming STRIPE_SECRET and SMTP_URL with their places, and not SET", got)
	}
}

// file.env's "./" is the folder of the file that writes the call: a library of the app reads the .env
// beside it, wherever the router.work importing it is.
func TestTheEnvironmentOfALibraryIsReadFromItsOwnFolder(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                  manifest,
		"apps/shop/core/mail/mail.env":      "SMTP=smtp://mail\n",
		"apps/shop/core/mail/settings.work": "import * as file from \"kitwork/file\";\nconst env = file.env(\"./mail.env\");\nexport const smtp = env.SMTP;\n",
		"apps/shop/shop.test/router.work": routerImport +
			"import { smtp } from \"app/core/mail/settings.work\";\nrouter.get((context) => context.text(smtp));\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.body != "smtp://mail" {
		t.Errorf("GET /: %d %q, want the library's own mail.env read", got.status, got.body)
	}
}

// A handler that answers nothing, answers twice, or edits after its answer is a 500 with the reason in the
// log; a page word before a text is refused, as KITWORK_ROUTER.md mục 6 says. (A door declared inside a
// handler is a boot error since 05/11/2026, as mục 3 and 11 say: TestTheRoutersSpellingIsReadAtBoot.)
func TestAHandlerMustReturnOneAnswer(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                     manifest,
		"apps/shop/shop.test/none/router.work": routerImport + "router.get((context) => { context.text(\"lost\"); });\n",
		"apps/shop/shop.test/twice/router.work": routerImport +
			"router.get((context) => context.text(\"a\").json({}));\n",
		"apps/shop/shop.test/after/router.work": routerImport + "router.get((context) => context.text(\"a\").status(201));\n",
		"apps/shop/shop.test/page/router.work":  routerImport + "router.get((context) => context.title(\"Shop\").text(\"a\"));\n",
		"apps/shop/shop.test/typed/router.work": routerImport + "router.get((context) => context.type(\"text/csv\").json([1]));\n",
		"apps/shop/shop.test/away/router.work":  routerImport + "router.get((context) => context.redirect(\"https://evil.example/\"));\n",
		"apps/shop/shop.test/error/router.work": routerImport + "router.get((context) => context.status(410).error(\"Gone for good\"));\n",
		"apps/shop/shop.test/missing/router.work": routerImport +
			"router.get((context) => context.notFound());\n",
		"apps/shop/shop.test/reread/router.work": "import * as file from \"kitwork/file\";\n" + routerImport +
			"router.get((context) => context.json(file.env(\"app/app.env\")));\n",
		"apps/shop/shop.test/required/router.work": "import * as file from \"kitwork/file\";\n" + routerImport +
			"const env = file.env(\"app/app.env\");\nrouter.get((context) => context.text(env.require(\"LATE\")));\n",
		"apps/shop/shop.test/spin/router.work": routerImport +
			"router.get((context) => { let n = 0; for (let i = 0; i < 1000000000; i++) { n = n + i; } return context.text(\"\" + n); });\n",
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	for path, want := range map[string]string{
		"/none":     "the handler returned nothing, not an answer",
		"/twice":    "json(…) after text(…): one answer per response",
		"/after":    "status(…) after text(…)",
		"/page":     "context.title(…) before text(…): a page word means something only to bind, view, notFound and error",
		"/typed":    "type(…) before json(…)",
		"/away":     "redirect(\"https://evil.example/\"): a redirect leads to a path of this site",
		"/reread":   "file.env(…) reads the environment once, at the top of a file",
		"/required": "NOT_FOUND: router.work:4:",
		"/spin":     "Energy Limit Exceeded",
	} {
		got := app.get(t, "shop.test", path)
		if got.status != 500 || got.body != "Internal Server Error\n" {
			t.Errorf("GET %s: %d %q, want 500", path, got.status, got.body)
		}
		contains(t, "the log of "+path, app.log.String(), "[shop.test] GET "+path+" ("+path+"/router.work): 500: ", want)
	}
	if got := app.get(t, "shop.test", "/error"); got.status != 410 || got.body != "Gone for good" || got.header.Get("Cache-Control") != "no-store" {
		t.Errorf("GET /error: %d %q %v, want 410 with the message, not stored", got.status, got.body, got.header)
	}
	if got := app.get(t, "shop.test", "/missing"); got.status != 404 || got.body != "Not Found" {
		t.Errorf("GET /missing: %d %q, want 404", got.status, got.body)
	}
}

// Read on a manifest that does not compile says where.
func TestAManifestThatDoesNotCompileSaysWhere(t *testing.T) {
	folder := write(t, map[string]string{"application.work": "import * as app from \"kitwork/application\";\nwhile (true) {}\n"})
	_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "application.work") {
		t.Errorf("read: %v, want a compile error naming application.work", err)
	}
}
