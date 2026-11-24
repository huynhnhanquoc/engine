package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/ui"
)

// kitDesign is the look of a site with no design.work.
func kitDesign(t *testing.T) *design.Look {
	t.Helper()
	look, err := design.KitDesign()
	if err != nil {
		t.Fatal(err)
	}
	return look
}

// object is an object of the page's data from pairs of name and value.
func object(pairs ...any) value.Value {
	o := value.Object()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1].(value.Value))
	}
	return o
}

// render draws the route "" of a site made of files, with data, and gives the document and its body.
func render(t *testing.T, files map[string]string, data value.Value) (string, string) {
	t.Helper()
	site := memorySite(files)
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	// the pages of these tests are written in English: the words the engine writes are English there
	document, _, err := site.Document(page, data, ui.Head{Title: "Shop <1>", Description: "A \"shop\"", Language: "en"}, kitDesign(t))
	if err != nil {
		t.Fatal(err)
	}
	body := document[strings.Index(document, "<body"):]
	return document, body
}

// What a page writes in {{ }} is escaped, and undefined and null write nothing (KITWORK_LANG mục 3); a chain
// of a null reads as nothing, never a failure.
func TestAPageWritesItsDataEscapedAndNothingForNull(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><text>{{ user.name }}|{{ note }}|{{ user.owner.name }}|&lt;</text></content>`,
	}, object("user", object("name", value.String("<b>An</b>"), "owner", value.Null()), "note", value.Null()))
	if want := `<p>&lt;b&gt;An&lt;/b&gt;|||&amp;lt;</p>`; !strings.Contains(body, want) {
		t.Errorf("body\n%s\nwant it to hold %s", body, want)
	}
}

// for draws an element once per item (with its index), if draws it or not, an unprefixed show with a
// condition hides it with hidden, and a prefixed show is a class of the stylesheet.
func TestForIfAndShow(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content>` +
			`<text for="item, i of items; key: item.id">{{ i }}:{{ item.name }}</text>` +
			`<text if="admin">admin</text><text if="!admin">guest</text>` +
			`<text show="items.length > 5">many</text>` +
			`<text show="false" medium:show="true">wide</text></content>`,
	}, object("items", value.Array([]value.Value{object("id", value.Number(1), "name", value.String("a")), object("id", value.Number(2), "name", value.String("b"))}), "admin", value.Boolean(false)))
	for _, want := range []string{"<p>0:a</p>", "<p>1:b</p>", "<p>guest</p>", "<p hidden>many</p>", `<p class="show-false medium:show-true">wide</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
	if strings.Contains(body, ">admin<") {
		t.Errorf("if=\"admin\" with admin false drew it:\n%s", body)
	}
}

// A presentation attribute is one class named in the page's words, and the page's CSS holds its rule; the
// document's head has the title and the description escaped, and the ground and the palette's colours.
func TestAPresentationAttributeIsAClassWithItsRuleInThePage(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/view.ui":    `<view background="canvas"><content /></view>`,
		"/app/site/content.ui": `<content gap="4" medium:padding="6"><box hover:background="brand">x</box></content>`,
	}, value.Value{})
	for _, want := range []string{`<body class="flex-column background-canvas" data-kitwork-layout="view.ui">`, `<main class="flex-column gap-4 medium:padding-6">`, `class="flex-column hover:background-brand"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
	for _, want := range []string{".gap-4{", "<title>Shop &lt;1&gt;</title>", `<meta name="description" content="A &#34;shop&#34;">`, "--brand"} {
		if !strings.Contains(document, want) {
			t.Errorf("the document does not hold %s", want)
		}
	}
}

// container on <view> is the width of each region directly inside it, never of the <body>, whose background
// spans the screen (KITWORK_UI mục 7): a region writing its own container or max-width keeps it, the overlay
// floats over the screen uncapped, and what is not a region, or stands deeper, keeps the whole width. Its value
// is a fixed word.
func TestTheContainerOfTheViewIsItsRegionsWidth(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/view.ui":    `<view background="canvas" container="wide" huge:container="huge"><header /><box><sidebar>s</sidebar></box><content /><footer /><overlay /></view>`,
		"/app/site/header.ui":  `<header padding="4">h</header>`,
		"/app/site/content.ui": `<content gap="4">c</content>`,
		"/app/site/footer.ui":  `<footer medium:max-width="large">f</footer>`,
		"/app/site/overlay.ui": `<overlay>o</overlay>`,
	}, value.Value{})
	for _, want := range []string{
		`<body class="flex-column background-canvas" data-kitwork-layout="view.ui">`,
		`<header class="flex-column padding-4 container-wide huge:container-huge">h</header>`,
		`<div class="flex-column"><aside class="flex-column">s</aside></div>`,
		`<main class="flex-column gap-4 container-wide huge:container-huge">c</main>`,
		`<footer class="flex-column medium:max-width-large">f</footer>`,
		`<div class="flex-column">o</div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
	site := memorySite(map[string]string{
		"/app/site/view.ui":    `<view container="{{ width }}"><content /></view>`,
		"/app/site/content.ui": `<content>c</content>`,
	})
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = site.Document(page, object("width", value.String("wide")), ui.Head{}, kitDesign(t))
	if want := "view.ui:1:7: container của <view> là một chữ cố định"; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("container from data on <view>: %v, want an error beginning %q", err, want)
	}
}

// A piece of the site is called by its tag: its file is found from the calling file's folder up, each
// attribute of the call is an input it reads (a {{ }} filling the value passes the value itself), and its root
// renders as it is, once per item of the call's for.
func TestAPieceReadsWhatItsCallPasses(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui":      `<content><product-card for="p of products" product="{{ p }}" label="Mua {{ p.price }}" /></content>`,
		"/app/site/product-card.ui": `<card><text>{{ product.name }}: {{ label }}</text></card>`,
	}, object("products", value.Array([]value.Value{object("name", value.String("Trà"), "price", value.Number(20)), object("name", value.String("Cà phê"), "price", value.Number(25))})))
	for _, want := range []string{`<div class="card flex-column"><p>Trà: Mua 20</p></div>`, `<div class="card flex-column"><p>Cà phê: Mua 25</p></div>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
}

// A component's names are its own inside its tag, as they are before anything happened, and the page reads
// them anywhere through its alias; a popover is drawn closed. The server paints that first frame, and what
// follows the component's state is written for the kernel to keep: the host, its alias, each value's pair.
func TestAComponentsStateAndItsAlias(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><dropdown as="$menu"><button>{{ opened ? 'close' : 'open' }}</button><popover><text>menu</text></popover></dropdown>` +
			`<text>{{ $menu.opened }}</text></content>`,
	}, value.Value{})
	for _, want := range []string{
		`<div class="flex-column place-anchor" data-kitwork-component="dropdown: $menu">`,
		`<button class="flex-row background-ink/6 hover:background-ink/10 pressed:background-ink/14 disabled:background-ink/4" type="button" data-kitwork-bind="text: opened ? &#39;close&#39; : &#39;open&#39;">open</button>`,
		`<div class="popover flex-column" hidden data-kitwork-bind="show: opened"><p>menu</p></div>`,
		`<p data-kitwork-bind="text: $menu.opened">false</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
}

// An element has the engine's words before its attributes' classes: the tag's word where its HTML element is
// shared (card), then how it lays out what it holds when flex is not written (a container a column, a grid its
// grid, a button, a badge and a link holding an element a row); a link of text alone has none.
func TestAnElementHasTheEnginesWordsFirst(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><card padding="4">c</card><grid columns="2">g</grid><box flex="row" gap="2">r</box>` +
			`<badge>b</badge><link value="/a"><icon value="mesh" /></link><link value="/b">text</link><section>s</section></content>`,
	}, value.Value{})
	for _, want := range []string{
		`<div class="card flex-column padding-4">c</div>`, `<div class="grid columns-2">g</div>`, `<div class="flex-row gap-2">r</div>`,
		`<span class="badge flex-row">b</span>`, `<a class="flex-row" href="/a">`, `<a href="/b">text</a>`, `<section class="flex-column">s</section>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
}

// A link's value is its address, an icon's its class, an image's its source; alt names an element for a
// screen reader, or is an image's own.
func TestTheWordsOfLinkIconAndImage(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><link value="/docs?q={{ q }}" alt="Docs">Docs</link><icon value="route-off" /><image value="/assets/a.png" alt="A" /></content>`,
	}, object("q", value.String("a&b")))
	for _, want := range []string{`<a href="/docs?q=a&amp;b" aria-label="Docs">Docs</a>`, `<i class="icon-route-off" aria-hidden="true"></i>`, `<img src="/assets/a.png" alt="A" loading="lazy" decoding="async">`} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
}

// What a page cannot draw is an error at its place: a name the page is not given, a tag outside the
// vocabulary, a piece with no file, a prefix that is no prefix, a for that is not over a list.
func TestWhatAPageCannotDrawIsAnErrorAtItsPlace(t *testing.T) {
	for _, want := range []struct{ content, error string }{
		{`<content><text>{{ missing }}</text></content>`, "content.ui:1:16: trang không được đưa tên missing: {{ missing }}"},
		{`<content><blink /></content>`, "content.ui:1:10: thẻ <blink> không có trong từ vựng của Kitwork"},
		{`<content><shop-cart /></content>`, "content.ui:1:10: không tìm thấy shop-cart.ui"},
		{`<content><box tiny:gap="2" /></content>`, "content.ui:1:15: tiền tố không tồn tại: tiny:"},
		{`<content><text for="x of name">x</text></content>`, "content.ui:1:16: for đi qua một mảng; name là string"},
		{`<content><icon value="shield-chek" /></content>`, `content.ui:1:16: không có icon "shield-chek" trong bộ icon của Kitwork (Tabler): did you mean shield-check`},
	} {
		site := memorySite(map[string]string{"/app/site/content.ui": want.content})
		page, err := site.Page(route(""))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = site.Document(page, object("name", value.String("x")), ui.Head{}, kitDesign(t))
		if err == nil || !strings.HasPrefix(err.Error(), want.error) {
			t.Errorf("%s: %v, want an error beginning %q", want.content, err, want.error)
		}
	}
}

// A page that writes a function in {{ }} is an error that says so (18/11/2026): a function has no text, and the page
// wrote "function".
func TestAPageDoesNotWriteAFunction(t *testing.T) {
	site := memorySite(map[string]string{"/app/site/content.ui": `<content><text>{{ handler }}</text></content>`})
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	data := object("handler", value.Function(func(...value.Value) value.Value { return value.Undefined() }))
	_, _, err = site.Document(page, data, ui.Head{}, kitDesign(t))
	if err == nil || !strings.Contains(err.Error(), "{{ handler }} là một hàm, không hiện được thành chữ") {
		t.Errorf("the page: %v, want the error of a function written", err)
	}
}

// A page that writes a time in {{ }} is an error that says so (18/11/2026): a time has no text, and the page wrote RFC
// 3339 in UTC.
//
// CONTROL (18/11/2026): without the time's branch, the page drew with no error, its text empty.
func TestAPageDoesNotWriteATime(t *testing.T) {
	site := memorySite(map[string]string{"/app/site/content.ui": `<content><text>{{ order.at }}</text></content>`})
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	data := object("order", object("at", value.Time(time.Unix(0, 0))))
	_, _, err = site.Document(page, data, ui.Head{}, kitDesign(t))
	if err == nil || !strings.Contains(err.Error(), "{{ order.at }} là một thời gian, không hiện được thành chữ") {
		t.Errorf("the page: %v, want the error of a time written", err)
	}
}
