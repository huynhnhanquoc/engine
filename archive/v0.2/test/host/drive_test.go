package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// A site moves between its pages without a reload unless its root says router.reload(true) (KITWORK_UI mục 15):
// every page loads Drive after the kernel, and its frame gets the bar of a move first and the reader of the new page
// last when it writes neither; a link of the header says the page viewed, and how Drive marks it again, while a
// link of the content says only the first. router.reload(true) loads no Drive and adds nothing.
func TestASiteMovesWithoutAReloadUnlessItsRootSaysSo(t *testing.T) {
	site := "apps/shop/shop.test/"
	files := map[string]string{
		"application.work":           manifest,
		site + "view.ui":             `<view><header /><content /></view>`,
		site + "header.ui":           `<header><link value="/">Home</link><link value="/menu">Menu</link></header>`,
		site + "content.ui":          `<content><link value="/menu">Menu</link></content>`,
		site + "menu/tea/content.ui": `<content><text>Tea</text></content>`,
	}
	b := boot(t, filepath.Join(write(t, files), "application.work"))
	home, tea := b.get(t, "shop.test", "/"), b.get(t, "shop.test", "/menu/tea")
	contains(t, "the home page", home.body,
		`<script data-kitwork-generated="runtime" src="/_kitwork/kernel.`, `<script data-kitwork-generated="drive" src="/_kitwork/drive.`,
		`<body class="flex-column" data-kitwork-layout="view.ui"><div class="progressbar" data-kitwork-component="progressbar"`,
		`<div class="reader" data-kitwork-component="reader" data-kitwork-bind="text: text" aria-live="polite" aria-atomic="true"></div></body>`,
		`<a href="/" aria-current="page" data-kitwork-current="">Home</a><a href="/menu" data-kitwork-current="">Menu</a>`,
		`<main class="flex-column"><a href="/menu">Menu</a></main>`)
	contains(t, "a page in /menu", tea.body, `<a href="/" data-kitwork-current="">Home</a><a href="/menu" aria-current="true" data-kitwork-current="">Menu</a>`)
	if i, j := strings.Index(home.body, "/_kitwork/kernel."), strings.Index(home.body, "/_kitwork/drive."); i < 0 || j < i {
		t.Errorf("Drive is not loaded after the kernel")
	}

	files[site+"config.work"] = routerImport + "router.reload(true);\n"
	whole := boot(t, filepath.Join(write(t, files), "application.work")).get(t, "shop.test", "/")
	for _, unwanted := range []string{"/_kitwork/drive.", `class="progressbar"`, `data-kitwork-component="reader"`, "/_kitwork/kernel."} {
		if strings.Contains(whole.body, unwanted) {
			t.Errorf("a site that reloads each page holds %s:\n%s", unwanted, whole.body)
		}
	}
}

// router.reload is the site's one switch: true or false, at the site's root only, once.
func TestRouterReloadIsTheSitesOneSwitch(t *testing.T) {
	site := "apps/shop/shop.test/"
	for _, c := range []struct{ file, source, want string }{
		{site + "menu/config.work", "router.reload(true);", "router.reload(…) is the site's one switch, written at the site's root"},
		{site + "config.work", `router.reload("false");`, "router.reload(…) takes true or false, written out"},
		{site + "config.work", "router.reload(false);\nrouter.reload(true);", "router.reload(…) is declared twice: the first is at shop.test/config.work:2:"},
	} {
		got := bootError(t, map[string]string{site + "content.ui": "<content />", c.file: routerImport + c.source + "\n"})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s %q:\n %s\nwant it to say %q", c.file, c.source, got, c.want)
		}
	}
}
