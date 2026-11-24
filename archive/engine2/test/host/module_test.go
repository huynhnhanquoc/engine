package host_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

// The tests of module.work (docs/module.md; root docs/KITWORK_MODULE_WORK.md, slice 1, 17/11/2026): an app's root is
// the folder of its module.work, which runs first, imports kitwork/module alone and declares the app's zone and, for an
// owner of several sites, the folder of each hostname the host gives the app.

const moduleImport = "import * as module from \"kitwork/module\";\n"

// bootSome boots the manifest of folder and serves it, the apps it closed or the names it closed the error it gives.
func bootSome(t *testing.T, folder string) (*booted, error) {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\nlog:\n%s", err, log)
	}
	return serve(t, read, log), read.Failed()
}

// An app is the folder of its module.work: a folder under apps/ without one is closed, saying what to write, and the
// other apps serve.
func TestAnAppIsTheFolderOfItsModuleWork(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/router.work": says("shop"),
		"apps/bare/bare.test/router.work": says("bare"),
	})
	if err := os.Remove(filepath.Join(folder, "apps", "bare", "module.work")); err != nil { // write gave it one
		t.Fatal(err)
	}
	b, failed := bootSome(t, folder)
	if failed == nil || !strings.Contains(failed.Error(), `bare has no module.work: an app's root is the folder of its module.work, import * as module from "kitwork/module"`) {
		t.Fatalf("the app without module.work: %v", failed)
	}
	if got := b.get(t, "shop.test", "/"); got.body != "shop" {
		t.Errorf("the other app: %d %q", got.status, got.body)
	}
	// a folder without module.work says no hostname of its own: none is served, rather than a guess
	if got := b.get(t, "bare.test", "/"); got.status != http.StatusNotFound {
		t.Errorf("bare.test: %d %q, want 404", got.status, got.body)
	}
}

// module.work is read on its own words, each a boot error at its line: one import, of kitwork/module alone, written
// import * as module; module.<word>(…) one call a line at the top level; each word once; a word it has not names the
// nearest. No other file imports kitwork/module, and router.timezone is an old word.
func TestModuleWorkIsReadOnItsOwnWords(t *testing.T) {
	routes := "module.routes((site) => site.domain);\n"
	for source, want := range map[string]string{
		moduleImport + "module.timezon(\"Asia/Ho_Chi_Minh\");\n" + routes:                             "module has no word timezon: did you mean module.timezone?",
		moduleImport + "module.timezone(\"Asia/Ho_Chi_Minh\");\nmodule.timezone(\"UTC\");\n" + routes: "module.timezone(…) is declared twice: the first is at module.work:2:",
		moduleImport + "module.timezone(\"Europe/Nowhere\");\n" + routes:                              "module.timezone(\"Europe/Nowhere\"): not an IANA zone name",
		moduleImport + "module.timezone(\"Local\");\n" + routes:                                       "the server's own clock (\"Local\") is no app's zone",
		moduleImport + "module.routes(\"shop.test\");\n":                                              "module.routes(…) takes a function of one site",
		moduleImport + "module.routes((site, other) => site.domain);\n":                               "module.routes(…) takes a function of one site",
		moduleImport + routes + routes:                                                                "module.routes(…) is declared twice: the first is at module.work:2:",
		routerImport + moduleImport + routes:                                                          "module.work imports only kitwork/module, not kitwork/router",
		moduleImport + "import { x } from \"./x.work\";\n" + routes:                                   "module.work imports only kitwork/module, not \"./x.work\"",
		moduleImport + "module.timezone(\"Asia/Ho_Chi_Minh\").routes((site) => site.domain);\n":       "one call a line, module.routes(…) on a line of its own",
		"import { routes } from \"kitwork/module\";\nroutes((site) => site.domain);\n":                "module is imported on one line, import * as module from \"kitwork/module\"",
		moduleImport + "const m = module;\nm.routes((site) => site.domain);\n":                        "module is read only as module.<word>(…)",
		moduleImport + "if (true) { module.routes((site) => site.domain); }\n":                        "is written at the top level of the file",
	} {
		got := bootError(t, map[string]string{
			"apps/shop/module.work":           source,
			"apps/shop/x.work":                "export const x = 1;\n",
			"apps/shop/shop.test/router.work": says("shop"),
		})
		if !strings.Contains(got, want) {
			t.Errorf("module.work %q:\n got %q\nwant %q", source, got, want)
		}
	}
	for name, files := range map[string]map[string]string{
		"kitwork/module in a router.work": {"apps/shop/shop.test/router.work": moduleImport + says("shop")},
		"router.timezone":                 {"apps/shop/config.work": routerImport + "router.timezone(\"Asia/Ho_Chi_Minh\");\n"},
		"design.timezone":                 {"apps/shop/shop.test/design.work": "import * as design from \"kitwork/design\";\ndesign.timezone(\"Asia/Ho_Chi_Minh\");\n"},
	} {
		want := map[string]string{
			"kitwork/module in a router.work": "kitwork/module is module.work's",
			"router.timezone":                 "router.timezone is an old word: write module.timezone(\"Asia/Ho_Chi_Minh\") in module.work, the app's root",
			"design.timezone":                 "design.timezone is no word of design.work: the app's zone is module.timezone(…), in module.work at the app's root",
		}[name]
		if _, ok := files["apps/shop/shop.test/router.work"]; !ok {
			files["apps/shop/shop.test/router.work"] = says("shop")
		}
		if got := bootError(t, files); !strings.Contains(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

// module.routes gives each hostname the host gives the app its folder: the function is called once for each as the
// app boots, with { domain }, and several names may share one folder, below the root or deeper.
func TestModuleRoutesGivesEachHostnameItsFolder(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":               domainsManifest,
		"apps/shop/module.work":          moduleImport + "module.routes((site) => site.domain.endsWith(\".vn\") ? \"sites/vn\" : \"global\");\n",
		"apps/shop/sites/vn/router.work": says("vn"),
		"apps/shop/global/router.work":   says("global"),
		"apps/shop/core/helper.work":     "export const helper = 1;\n",
	})
	writeDomains(t, filepath.Join(folder, "system", "domains.db"),
		domainRow{"shop.vn", "shop", "confirmed", "", ""},
		domainRow{"cua-hang.vn", "shop", "confirmed", "", ""},
		domainRow{"shop.com", "shop", "confirmed", "", ""},
		domainRow{"other.vn", "other", "confirmed", "", ""},
	)
	b := boot(t, filepath.Join(folder, "application.work"))
	for hostname, want := range map[string]string{"shop.vn": "vn", "cua-hang.vn": "vn", "shop.com": "global"} {
		if got := b.get(t, hostname, "/"); got.status != http.StatusOK || got.body != want {
			t.Errorf("%s: %d %q, want %q", hostname, got.status, got.body, want)
		}
	}
	// a name the table gives another app is no name of this one
	if got := b.get(t, "other.vn", "/"); got.status != http.StatusNotFound {
		t.Errorf("other.vn: %d %q, want 404", got.status, got.body)
	}
}

// A hostname module.routes gives no site's folder is that name's error alone (KITWORK_MODULE_WORK mục 4, luật 3): its
// site answers 503, said in the log and in the boot's failures, and the app's other names serve.
func TestANameModuleRoutesGivesNoFolderAnswers503(t *testing.T) {
	route := `module.routes((site) => {
  if (site.domain === "gone.test") { return "nowhere"; }
  if (site.domain === "dot.test") { return ".shared"; }
  if (site.domain === "up.test") { return "../other"; }
  if (site.domain === "number.test") { return 7; }
  if (site.domain === "root.test") { return ""; }
  if (site.domain === "fails.test") { return site.nothing.more; }
  return site.domain;
});
`
	files := map[string]string{
		"application.work":                      manifest,
		"apps/shop/module.work":                 moduleImport + route,
		"apps/shop/core/x.work":                 "export const x = 1;\n",
		"apps/other/module.work":                moduleImport + routes1,
		"apps/other/elsewhere.test/router.work": says("elsewhere"),
	}
	names := []string{"shop.test", "gone.test", "dot.test", "up.test", "number.test", "root.test", "fails.test"}
	for _, name := range names {
		files["apps/shop/"+name+"/router.work"] = says(name)
	}
	b, failed := bootSome(t, write(t, files))
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusOK || got.body != "shop.test" {
		t.Errorf("shop.test: %d %q", got.status, got.body)
	}
	for name, why := range map[string]string{
		"gone.test":   "gives \"nowhere\", a folder the app does not have",
		"dot.test":    "gives \".shared\", which is no site's folder: a dot folder or an underscore one (.shared) is the app's own",
		"up.test":     "gives \"../other\", which is no site's folder: a folder below the app's root, straight down",
		"number.test": "gives number, not a folder",
		"root.test":   "gives \"\", which is no site's folder: the app's root is the one site of an app that writes no module.routes",
		"fails.test":  "module.routes(…) at module.work:2:",
	} {
		if got := b.get(t, name, "/"); got.status != http.StatusServiceUnavailable {
			t.Errorf("%s: %d %q, want 503", name, got.status, got.body)
		}
		if failed == nil || !strings.Contains(failed.Error(), why) {
			t.Errorf("%s: the boot's failures do not say %q:\n%v", name, why, failed)
		}
		contains(t, "the log", b.log.String(), "[shop] the site "+name+" answers 503: module.routes(…) at module.work:2:7, for "+name)
	}
	if got := b.get(t, "elsewhere.test", "/"); got.body != "elsewhere" {
		t.Errorf("the other app: %d %q", got.status, got.body)
	}
}

// routes1 is module.work's module.routes giving each name the folder of its name, without the import.
const routes1 = "module.routes((site) => site.domain);\n"

// An app whose module.work writes no module.routes is one site, its root: its pages are its folders from the root, its
// root's config.work is the site's root's (router.reload is the site's one switch), and the names the table gives it
// serve it; a folder named like a hostname is a page like any other.
func TestAnAppWithoutRoutesIsOneSiteAtItsRoot(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                domainsManifest,
		"apps/shop/module.work":           moduleImport + "module.timezone(\"Asia/Ho_Chi_Minh\");\n",
		"apps/shop/config.work":           routerImport + "router.reload(true);\n",
		"apps/shop/router.work":           says("root"),
		"apps/shop/san-pham/router.work":  says("san-pham"),
		"apps/shop/shop.test/router.work": says("a page"),
		"apps/shop/core/helper.work":      "export const helper = 1;\n",
		"apps/shop/core/queue/send.work":  queueImport + "export const send = queue.job((context) => {});\n",
	})
	writeDomains(t, filepath.Join(folder, "system", "domains.db"),
		domainRow{"shop.example", "shop", "confirmed", "", ""},
		domainRow{"www.shop.example", "shop", "confirmed", "", ""},
	)
	b := boot(t, filepath.Join(folder, "application.work"))
	for _, hostname := range []string{"shop.example", "www.shop.example"} {
		for path, want := range map[string]string{"/": "root", "/san-pham": "san-pham", "/shop.test": "a page"} {
			if got := b.get(t, hostname, path); got.status != http.StatusOK || got.body != want {
				t.Errorf("%s%s: %d %q, want %q", hostname, path, got.status, got.body, want)
			}
		}
		for _, path := range []string{"/module.work", "/config.work", "/core/helper.work"} {
			if got := b.get(t, hostname, path); got.status != http.StatusNotFound {
				t.Errorf("%s%s: %d, want 404: a .work file is never served", hostname, path, got.status)
			}
		}
	}
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusNotFound {
		t.Errorf("shop.test: %d %q, want 404: a folder of an app without module.routes is a page, not a site", got.status, got.body)
	}
	// with no name, the app boots and serves nothing, saying so
	alone := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":      manifest,
		"apps/shop/module.work": moduleImport,
		"apps/shop/router.work": says("root"),
	}), "application.work"))
	contains(t, "the log", alone.log.String(), "[shop] no hostname is the app's: its module.work writes no module.routes")
}
