package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// headSite is an app whose config.work says a language and an icon, whose site's root says its name and its
// description and a language of its own, with a branch that titles its pages and links an icon of its own, a branch
// that says nothing, and a page that says its own title.
func headSite(t *testing.T) *booted {
	t.Helper()
	site := "apps/shop/shop.test/"
	folder := write(t, map[string]string{
		"application.work":                 manifest,
		"apps/shop/config.work":            routerImport + "router.language(\"fr\");\nrouter.favicon(\"/assets/app.svg\");\n",
		site + "config.work":               routerImport + "router.language(\"en\");\nrouter.meta({ site: \"Shop & Co\", description: \"Tea <and> coffee\" });\n",
		site + "content.ui":                "<content><text>Home</text></content>\n",
		site + "assets/app.svg":            "<svg xmlns=\"http://www.w3.org/2000/svg\"/>",
		site + "about/config.work":         routerImport + "router.meta({ title: \"About us\" });\nrouter.favicon(\"icon.png\");\n",
		site + "about/content.ui":          "<content><text>About</text></content>\n",
		site + "menu/config.work":          routerImport + "router.favicon(\"https://cdn.example.com/i.png\");\n",
		site + "menu/content.ui":           "<content><text>Menu</text></content>\n",
		site + "tea/router.work":           routerImport + "router.get((context) => context.title(\"Tea\").view());\n",
		site + "tea/content.ui":            "<content><text>Tea</text></content>\n",
		site + "notfound.ui":               "<content><text>Nothing here</text></content>\n",
		"apps/shop/plain.test/config.work": routerImport + "router.meta({ language: \"de\" });\n",
		"apps/shop/plain.test/content.ui":  "<content><text>Plain</text></content>\n",
		"apps/shop/third.test/content.ui":  "<content><text>Third</text></content>\n",
		"apps/other/bare.test/content.ui":  "<content><text>Bare</text></content>\n",
		"apps/other/bare.test/favicon.ico": "ICO",
	})
	return boot(t, filepath.Join(folder, "application.work"))
}

// headOf is the <html> line and the <head> of a page.
func headOf(page string) string {
	if end := strings.Index(page, "</head>"); end >= 0 {
		return page[:end]
	}
	return page
}

// A page's head is what its folders say (KITWORK_ROUTER.md mục 4, moved from the first engine 07/11/2026): the
// language of the nearest folder that says one, the app's under the site's; router.meta merged key by key down the
// folders, its title else the site's name for <title>, its description; the icon of the nearest folder, a path
// with / the site's and another the folder's, with its type. A page's own title is written over the folders'. The
// not-found page has the root's. With no word anywhere a page is Vietnamese, as the first engine's, with no title,
// no description, no icon. Each is escaped.
func TestAPageHeadIsWhatItsFoldersSay(t *testing.T) {
	b := headSite(t)
	for _, c := range []struct {
		host, path string
		status     int
		want       []string
		absent     []string
	}{
		{"shop.test", "/", http.StatusOK, []string{`<html lang="en"`, "<title>Shop &amp; Co</title>", `<meta name="description" content="Tea &lt;and&gt; coffee">`,
			`<link rel="icon" href="/assets/app.svg" type="image/svg+xml">`}, nil},
		{"shop.test", "/about", http.StatusOK, []string{`<html lang="en"`, "<title>About us</title>", `<meta name="description" content="Tea &lt;and&gt; coffee">`,
			`<link rel="icon" href="/about/icon.png" type="image/png">`}, nil},
		// an icon on another host is linked as written
		{"shop.test", "/menu", http.StatusOK, []string{"<title>Shop &amp; Co</title>", `<link rel="icon" href="https://cdn.example.com/i.png" type="image/png">`}, nil},
		{"shop.test", "/tea", http.StatusOK, []string{"<title>Tea</title>", `<meta name="description" content="Tea &lt;and&gt; coffee">`,
			`<link rel="icon" href="/assets/app.svg" type="image/svg+xml">`}, nil},
		{"shop.test", "/nowhere", http.StatusNotFound, []string{`<html lang="en"`, "<title>Shop &amp; Co</title>", `<link rel="icon" href="/assets/app.svg"`}, nil},
		// the app's icon reaches every site of it; router.meta is a site's own, its language over the app's
		{"plain.test", "/", http.StatusOK, []string{`<html lang="de"`, `<link rel="icon" href="/assets/app.svg" type="image/svg+xml">`}, []string{"<title>", `name="description"`}},
		{"third.test", "/", http.StatusOK, []string{`<html lang="fr"`, `<link rel="icon" href="/assets/app.svg" type="image/svg+xml">`}, nil},
		{"bare.test", "/", http.StatusOK, []string{`<html lang="vi"`}, []string{"<title>", `name="description"`, `rel="icon"`}},
	} {
		got := b.get(t, c.host, c.path)
		head := headOf(got.body)
		if got.status != c.status {
			t.Errorf("GET %s%s: %d, want %d\n%s", c.host, c.path, got.status, c.status, b.log.String())
		}
		contains(t, c.host+c.path, head, c.want...)
		for _, absent := range c.absent {
			if strings.Contains(head, absent) {
				t.Errorf("%s%s holds %q:\n%s", c.host, c.path, absent, head)
			}
		}
		if strings.Count(head, "<title>") > 1 || strings.Count(head, `rel="icon"`) > 1 {
			t.Errorf("%s%s has two titles or two icons:\n%s", c.host, c.path, head)
		}
	}
	// /favicon.ico answers with the icon the site's root links, the app's here; a branch's icon is its pages' only.
	// CONTROL: a site that names none serves its own favicon.ico as it is
	icon := b.get(t, "shop.test", "/favicon.ico")
	if icon.status != http.StatusOK || icon.header.Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(icon.body, "<svg") {
		t.Errorf("GET shop.test/favicon.ico: %d %q %q, want the app's svg", icon.status, icon.header.Get("Content-Type"), icon.body)
	}
	if own := b.get(t, "bare.test", "/favicon.ico"); own.status != http.StatusOK || own.body != "ICO" {
		t.Errorf("GET bare.test/favicon.ico: %d %q, want its own file", own.status, own.body)
	}
}

// What the three words take, and where: each is the boot's error at the call, the app closed, saying how to write it.
// CONTROL: a favicon.ico at the root beside router.favicon("/favicon.ico"), the same file, and an icon on another
// host, linked as written and no source of /favicon.ico, boot.
func TestTheHeadWordsAreReadStrictly(t *testing.T) {
	site := "apps/shop/shop.test/"
	page := "<content><text>a</text></content>\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		says  string
	}{
		{"meta at the app root", map[string]string{"apps/shop/config.work": routerImport + "router.meta({ site: \"Shop\" });\n"},
			"router.meta(…) at the app root: the metadata of pages is a site's"},
		{"meta not an object", map[string]string{site + "config.work": routerImport + "router.meta(\"Shop\");\n"},
			`router.meta(…) takes an object: router.meta({ title: "…", description: "…" })`},
		{"meta not text", map[string]string{site + "config.work": routerImport + "router.meta({ title: 1 });\n"},
			`router.meta({ title }): each key of router.meta is a text`},
		{"language empty", map[string]string{site + "config.work": routerImport + "router.language(\" \");\n"},
			`router.language(…) takes a language code: router.language("vi")`},
		{"language twice in a folder", map[string]string{site + "config.work": routerImport + "router.language(\"vi\");\n",
			site + "router.work": routerImport + "router.language(\"en\");\nrouter.get((context) => context.text(\"a\"));\n"},
			"router.language(…) is declared twice: the first is at shop.test/config.work:2:"},
		{"favicon twice in a folder", map[string]string{site + "config.work": routerImport + "router.favicon(\"/assets/a.svg\");\nrouter.favicon(\"/assets/b.svg\");\n"},
			"router.favicon(…) is declared twice: the first is at shop.test/config.work:2:"},
		{"favicon of nothing", map[string]string{site + "config.work": routerImport + "router.favicon(null);\n"},
			`router.favicon(…) takes the path of the icon: router.favicon("/assets/favicon.svg")`},
		{"favicon a dot file", map[string]string{site + "config.work": routerImport + "router.favicon(\"/.env\");\n"},
			`router.favicon("/.env"): a dot file or folder (.env, .database/) is never served`},
		{"favicon a source", map[string]string{site + "config.work": routerImport + "router.favicon(\"router.work\");\n"},
			`router.favicon("router.work"): a source of the site (.work, .ui, .kitwork.*) is never served`},
		{"favicon.ico and router.favicon", map[string]string{site + "config.work": routerImport + "router.favicon(\"/assets/a.svg\");\n", site + "favicon.ico": "ICO"},
			`shop/shop.test/favicon.ico: /favicon.ico has two sources, this file and router.favicon("assets/a.svg") at shop.test/config.work:2:`},
		{"favicon.ico and the app's router.favicon", map[string]string{"apps/shop/config.work": routerImport + "router.favicon(\"/assets/a.svg\");\n", site + "favicon.ico": "ICO"},
			`/favicon.ico has two sources, this file and router.favicon("assets/a.svg") at config.work:2:`},
		{"the same file", map[string]string{site + "config.work": routerImport + "router.favicon(\"/favicon.ico\");\n", site + "favicon.ico": "ICO"}, ""},
		{"another host", map[string]string{site + "config.work": routerImport + "router.favicon(\"https://cdn.example.com/i.png\");\n", site + "favicon.ico": "ICO"}, ""},
	} {
		c.files[site+"content.ui"] = page
		got := bootError(t, c.files)
		switch {
		case c.says == "" && got != "":
			t.Errorf("%s: %s, want it to boot", c.name, got)
		case c.says != "" && !strings.Contains(got, c.says):
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.says)
		}
	}
}
