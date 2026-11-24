package host_test

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const designImport = "import * as design from \"kitwork/design\";\n"

// designSite is an app whose look is declared at three levels: the app root names the typefaces, the site its
// brand and its ink, a branch only its brand; another branch declares nothing.
func designSite(t *testing.T) *booted {
	t.Helper()
	site := "apps/shop/shop.test/"
	folder := write(t, map[string]string{
		"application.work":         manifest,
		"apps/shop/design.work":    designImport + "design.fonts({ body: \"Outfit\", code: \"Fira Code\" });\n",
		site + "design.work":       designImport + "design.colors({ brand: \"#f5225f\", ink: { light: \"#242442\", dark: \"#c0caf5\" } });\ndesign.appearance(\"dark\");\n",
		site + "content.ui":        "<content><text color=\"brand\">Shop</text><icon value=\"shield-check\" /><icon value=\"{{ name }}\" /></content>",
		site + "router.work":       routerImport + "router.get((context) => context.view({ name: \"no-such-icon\" }));\n",
		site + "admin/design.work": designImport + "design.colors({ brand: \"#2563eb\" });\n",
		site + "admin/content.ui":  "<content><text color=\"brand\">Admin</text></content>",
		site + "about/content.ui":  "<content><text>About</text></content>",
	})
	return boot(t, filepath.Join(folder, "application.work"))
}

// brandOf is the light value the page's CSS gives --brand.
func brandOf(t *testing.T, page string) string {
	t.Helper()
	found := regexp.MustCompile(`--brand:([^;}]+)`).FindStringSubmatch(page)
	if found == nil {
		t.Fatalf("the page gives no --brand:\n%s", page)
	}
	return found[1]
}

// A folder's pages are drawn in its look: Kit Design's, under the app root's design.work, the site's and each
// branch's down to the folder, the nearest winning per token; a branch that declares nothing has its parent's.
func TestAPageIsDrawnInTheLookOfItsFolder(t *testing.T) {
	b := designSite(t)
	home := b.get(t, "shop.test", "/")
	admin := b.get(t, "shop.test", "/admin")
	about := b.get(t, "shop.test", "/about")
	for _, page := range []reply{home, admin, about} {
		if page.status != http.StatusOK {
			t.Fatalf("a page answered %d:\n%s\nlog:\n%s", page.status, page.body, b.log)
		}
	}
	if got := brandOf(t, home.body); !strings.Contains(strings.ToLower(got), "f5225f") {
		t.Errorf("the site's brand is %s, want the site's #f5225f", got)
	}
	if got := brandOf(t, admin.body); !strings.Contains(strings.ToLower(got), "2563eb") {
		t.Errorf("the branch's brand is %s, want the branch's #2563eb", got)
	}
	if brandOf(t, about.body) != brandOf(t, home.body) {
		t.Errorf("a branch with no design.work has brand %s, want its parent's %s", brandOf(t, about.body), brandOf(t, home.body))
	}
	// the app root's typefaces reach every page; a face the engine carries is served by it
	for _, page := range []reply{home, admin} {
		contains(t, "the page", page.body, "--font-body:Outfit, ", "--font-code:'Fira Code', ", "@font-face{font-family:'Outfit';font-style:normal;font-weight:100 900;font-display:swap;src:url(/_kitwork/fonts/outfit-100-900-latin.")
	}
	// the look the site shows first is written on <html> for the prepaint, which runs before the stylesheet
	contains(t, "the page", home.body, `<html lang="vi" data-kitwork-appearance="dark">`, `<script data-kitwork-generated="appearance">`)
	if strings.Index(home.body, "data-kitwork-generated=\"appearance\"") > strings.Index(home.body, "<style") {
		t.Errorf("the prepaint comes after the stylesheet")
	}
}

// A face is served under /_kitwork/fonts/ by the name the page writes, a year in a cache; a name the engine
// does not have is a 404 there, never a page of the site.
func TestTheFacesAreServedByTheEngine(t *testing.T) {
	b := designSite(t)
	home := b.get(t, "shop.test", "/")
	preload := regexp.MustCompile(`<link rel="preload" href="(/_kitwork/fonts/outfit-100-900-latin\.[0-9a-f]{16}\.woff2)" as="font" type="font/woff2" crossorigin>`).FindStringSubmatch(home.body)
	if preload == nil {
		t.Fatalf("the page preloads no face of its body:\n%s", home.body[:min(len(home.body), 1500)])
	}
	face := b.get(t, "shop.test", preload[1])
	if face.status != http.StatusOK || face.header.Get("Content-Type") != "font/woff2" || !strings.Contains(face.header.Get("Cache-Control"), "immutable") || !strings.HasPrefix(face.body, "wOF2") {
		t.Errorf("GET %s: %d %s %q", preload[1], face.status, face.header.Get("Content-Type"), face.header.Get("Cache-Control"))
	}
	for _, path := range []string{"/_kitwork/fonts/outfit-100-900-latin.0000000000000000.woff2", "/_kitwork/kernel.js"} {
		if missing := b.get(t, "shop.test", path); missing.status != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, missing.status)
		}
	}
}

// An icon a page shows has its mask in the page's stylesheet, once, and no other icon has; an icon the page's
// data names that the set lacks draws an empty icon.
func TestAnIconsRuleIsInThePageThatShowsIt(t *testing.T) {
	b := designSite(t)
	home := b.get(t, "shop.test", "/")
	contains(t, "the page", home.body, `<i class="icon-shield-check" aria-hidden="true"></i>`, ":where(.icon-shield-check){display:inline-block;", `.icon-shield-check{--i:url("data:image/svg+xml,`)
	if strings.Contains(home.body, "no-such-icon") {
		t.Errorf("an icon the set lacks is written:\n%s", home.body)
	}
	if masks := strings.Count(home.body, "{--i:url("); masks != 1 {
		t.Errorf("the page has %d icon masks, want the one icon it shows", masks)
	}
	about := b.get(t, "shop.test", "/about")
	if strings.Contains(about.body, "{--i:url(") || strings.Contains(about.body, ".icon-") {
		t.Errorf("a page that shows no icon carries an icon's rule")
	}
}

// What design.work writes wrong stops its app at the line: its words are seven and its own, its vocabulary
// closed, its calls one a line at the top level, a word the nearest folder decides once a folder, and it has
// no role in core/ or assets/; the look's words of the router are where they were.
func TestDesignWorkIsWrittenAsTheSpecSays(t *testing.T) {
	site := "apps/shop/shop.test/"
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a misspelt word", map[string]string{site + "design.work": designImport + "design.colours({ brand: \"red\" });\n"},
			"design.work:2:7: design has no word colours: did you mean design.colors?"},
		{"a router's word", map[string]string{site + "design.work": designImport + "design.meta({ site: \"Shop\" });\n"},
			"design.meta is no word of design.work: router.meta(…) is written in router.work or config.work"},
		{"another module", map[string]string{site + "design.work": designImport + routerImport + "design.colors({ brand: \"red\" });\n"},
			"design.work imports only kitwork/design, not kitwork/router"},
		{"design in router.work", map[string]string{site + "router.work": designImport + routerImport + "router.get((context) => context.text(\"a\"));\n"},
			"kitwork/design is design.work's"},
		{"a chain", map[string]string{site + "design.work": designImport + "design.colors({ brand: \"red\" }).fonts({ body: \"Outfit\" });\n"},
			"design.work:2:32: design.colors(…).fonts(…): one call a line, design.fonts(…) on a line of its own"},
		{"in an if", map[string]string{site + "design.work": designImport + "if (true) { design.colors({ brand: \"red\" }); }\n"},
			"design.colors(…) is written at the top level of the file, not in an if"},
		{"another spelling of the import", map[string]string{site + "design.work": "import * as d from \"kitwork/design\";\nd.colors({ brand: \"red\" });\n"},
			`design is imported on one line, import * as design from "kitwork/design"`},
		{"an appearance twice", map[string]string{site + "design.work": designImport + "design.appearance(\"dark\");\ndesign.appearance(\"light\");\n"},
			"design.appearance(…) is declared twice in one folder, at shop.test/design.work:2:7 and at shop.test/design.work:3:7"},
		{"a word of no list", map[string]string{site + "design.work": designImport + "design.transition(\"spin\");\n"},
			`design.transition("spin"): transition is one of "none", "fade", "slide"`},
		{"a kind of code that is none", map[string]string{site + "design.work": designImport + "design.highlight({ keywrod: \"brand\" });\n"},
			"design.highlight({ keywrod: … }): "},
		{"a font role", map[string]string{site + "design.work": designImport + "design.fonts({ heading: \"Outfit\" });\n"},
			`design.fonts(…) names the typeface of two roles, body and code, not "heading"`},
		{"a colour that is none", map[string]string{site + "design.work": designImport + "design.colors({ brand: \"#zzzzzz\" });\n"},
			"shop/shop.test/design.work: "},
		{"in .shared", map[string]string{"apps/shop/.shared/design.work": designImport, site + "router.work": routerImport},
			"shop/.shared/design.work: .shared/ đã bỏ (17/11/2026)"},
		{"in assets", map[string]string{site + "assets/design.work": designImport},
			"shop/shop.test/assets/design.work: design.work has no role in assets/"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}

// A typeface the engine does not carry is said once at boot and named in the page's stack, the system's font
// drawing it; the page still serves.
func TestATypefaceTheEngineDoesNotCarryIsNamed(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/design.work": designImport + "design.fonts({ body: \"Plus Jakarta Sans\" });\n",
		"apps/shop/shop.test/content.ui":  "<content><text>Shop</text></content>",
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	home := b.get(t, "shop.test", "/")
	contains(t, "the page", home.body, "--font-body:'Plus Jakarta Sans', ui-sans-serif")
	if strings.Contains(home.body, "font-family:'Plus Jakarta Sans'") || strings.Contains(home.body, `rel="preload"`) {
		t.Errorf("a face the engine does not carry is declared or preloaded")
	}
	contains(t, "the log", b.log.String(), `shop.test/design.work:2:7: design.fonts({ body: "Plus Jakarta Sans" }): engine2 does not carry Plus Jakarta Sans yet`)
}

// The site's design.highlight colours its code: a kind it names is its colour in the page's rule, a token or a
// pair in each look; a kind it leaves out keeps the engine's.
func TestDesignHighlightColoursTheCode(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/design.work": designImport + "design.colors({ canvas: { light: \"#f6f9fc\", dark: \"#16161e\" } });\n" +
			"design.highlight({ keyword: { light: \"#7847bd\", dark: \"#bb9af7\" }, string: \"success\" });\n",
		"apps/shop/shop.test/content.ui": "<content><code language=\"javascript\">const a = \"x\"; // note</code></content>",
	})
	page := boot(t, filepath.Join(folder, "application.work")).get(t, "shop.test", "/").body
	rule := func(kind string) string {
		found := regexp.MustCompile(`\.code-` + kind + `\{[^}]*\}`).FindString(page)
		if found == "" {
			t.Fatalf("the page has no rule .code-%s:\n%s", kind, page)
		}
		return found
	}
	if keyword := rule("keyword"); !strings.Contains(keyword, "--kitwork-highlight-keyword") {
		t.Errorf("the keyword's rule %s does not read the site's pair", keyword)
	}
	contains(t, "the page", page, "--kitwork-highlight-keyword:#7847bd", "--kitwork-highlight-keyword:#bb9af7")
	if text := rule("string"); !strings.Contains(text, "success") {
		t.Errorf("the string's rule %s is not the site's colour, its success", text)
	}
	if comment := rule("comment"); strings.Contains(comment, "highlight") {
		t.Errorf("a kind the site leaves out reads the site's colours: %s", comment)
	}
}
