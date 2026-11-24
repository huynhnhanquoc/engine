package ui_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// renderLook draws the route "" of a site made of files with head, in the Kit Design look changed by change, and
// gives the document and its body.
func renderLook(t *testing.T, files map[string]string, head ui.Head, change func(look *lookChange)) (string, string) {
	t.Helper()
	look := *kitDesign(t) // a copy: Kit Design's look is every site's
	change(&lookChange{transition: &look.Transition, appearance: &look.Appearance, palette: &look.Palette})
	site := memorySite(files)
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := site.Document(page, value.Value{}, head, &look)
	if err != nil {
		t.Fatal(err)
	}
	return document, document[strings.Index(document, "<body"):]
}

// lookChange is what a test changes of a look.
type lookChange struct {
	transition, appearance *string
	palette                **stylesheet.Palette
}

// withDark gives a look a palette with a dark look, as a site whose design.colors writes a dark value has.
func withDark(t *testing.T) func(*lookChange) {
	return func(look *lookChange) {
		palette, err := stylesheet.NewPalette(false, map[string]stylesheet.Color{"brand": {Light: "#f5225f", Dark: "#ff6b6b"}})
		if err != nil {
			t.Fatal(err)
		}
		*look.palette = palette
	}
}

// pngOf is a PNG of width by height.
func pngOf(t *testing.T, width, height int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// webpOf is the head of an extended WebP (VP8X) of width by height.
func webpOf(width, height int) string {
	b := make([]byte, 30)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 22)
	copy(b[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(b[16:], 10)
	w, h := uint32(width-1), uint32(height-1)
	b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
	b[27], b[28], b[29] = byte(h), byte(h>>8), byte(h>>16)
	return string(b)
}

// An image of the site written as its path carries the size its file says: an SVG's width and height (px or no
// unit), else its viewBox; a PNG's header; a WebP's. A path from data, a web address, a path out of the site or a
// file that is no image has no size. Every image loads lazily, decoded off the main thread.
func TestAnImageOfTheSiteCarriesItsSize(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><image value="/assets/logo.svg" alt="" /><image value="/assets/box.svg" alt="b" />` +
			`<image value="/assets/photo.png" alt="p" /><image value="/assets/photo.webp" alt="w" /><image value="{{ picture }}" alt="d" />` +
			`<image value="https://cdn.example/x.png" alt="c" /><image value="/../site/assets/photo.png" alt="o" />` +
			`<image value="/assets/notes.svg" alt="n" /><image value="/assets/{{ name }}.png" alt="t" /></content>`,
		"/app/site/assets/logo.svg":   `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="28px" height="27.6" viewBox="0 0 100 100"></svg>`,
		"/app/site/assets/box.svg":    `<svg viewBox="0 0 640 480.4" xmlns="http://www.w3.org/2000/svg"><rect /></svg>`,
		"/app/site/assets/photo.png":  pngOf(t, 3, 2),
		"/app/site/assets/photo.webp": webpOf(1200, 800),
		"/app/site/assets/notes.svg":  `no svg here`,
		// a file named as the path is written: a path from data is never read for a size, whatever is on disk
		"/app/site/assets/{{ name }}.png": pngOf(t, 9, 9),
	}, object("picture", value.String("/assets/photo.png"), "name", value.String("photo")))
	holds(t, body,
		`<img src="/assets/logo.svg" alt="" width="28" height="28" loading="lazy" decoding="async">`,
		`<img src="/assets/box.svg" alt="b" width="640" height="480" loading="lazy" decoding="async">`,
		`<img src="/assets/photo.png" alt="p" width="3" height="2" loading="lazy" decoding="async">`,
		`<img src="/assets/photo.webp" alt="w" width="1200" height="800" loading="lazy" decoding="async">`,
		`<img src="/assets/photo.png" alt="d" loading="lazy" decoding="async">`,
		`<img src="https://cdn.example/x.png" alt="c" loading="lazy" decoding="async">`,
		`<img src="/../site/assets/photo.png" alt="o" loading="lazy" decoding="async">`,
		`<img src="/assets/notes.svg" alt="n" loading="lazy" decoding="async">`,
		`<img src="/assets/photo.png" alt="t" loading="lazy" decoding="async">`)
}

// On disk a site keeps what it read of its files for its boot (10/11/2026): an image's size and a .ui file are read
// once, the same site draws them as read after the files change, and the next boot, a new site, reads them again (until
// then an image's size was read again on the same site when its file changed). app.hotReload boots the app again
// when its files change, the test of the host shows it served (test/host/reload_test.go).
func TestASiteKeepsItsFilesForItsBoot(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string, at time.Time) {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Add(-time.Hour)
	write("content.ui", `<content><image value="/assets/pic.png" alt="p" /></content>`, start)
	write("assets/pic.png", pngOf(t, 3, 2), start)
	draw := func(site *ui.Site) string {
		page, err := site.Page(filepath.Join(root, ""))
		if err != nil {
			t.Fatal(err)
		}
		document, _, err := site.Document(page, value.Value{}, ui.Head{}, kitDesign(t))
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	booted := &ui.Site{Root: root, App: filepath.Dir(root)}
	holds(t, draw(booted), `width="3" height="2"`, `alt="p"`)
	write("assets/pic.png", pngOf(t, 5, 4), start.Add(time.Minute))
	write("content.ui", `<content><image value="/assets/pic.png" alt="q" /></content>`, start.Add(time.Minute))
	holds(t, draw(booted), `width="3" height="2"`, `alt="p"`)
	holds(t, draw(&ui.Site{Root: root, App: filepath.Dir(root)}), `width="5" height="4"`, `alt="q"`)
}

// A link opened in a new tab (new-tab="true") says target="_blank" and rel="noopener", rel="external noopener"
// when it leaves the site, and a screen reader hears it, in the page's language; target is the web's word.
func TestALinkOpensInANewTab(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><link value="/docs" new-tab="true">Docs</link><link value="https://example.com" new-tab="true">Out</link>` +
			`<link value="/blog" new-tab="false">Blog</link></content>`,
	}
	_, body := renderHead(t, files, value.Value{}, ui.Head{Language: "en", Origin: "https://kitwork.io"})
	note := `<span style="position:absolute;width:1px;height:1px;margin:-1px;padding:0;border:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap">`
	holds(t, body,
		`<a href="/docs" target="_blank" rel="noopener">Docs`+note+` (opens in a new tab)</span></a>`,
		`<a href="https://example.com" target="_blank" rel="external noopener">Out`+note+` (opens in a new tab)</span></a>`,
		`<a href="/blog">Blog</a>`)
	_, body = renderHead(t, files, value.Value{}, ui.Head{Language: "vi"})
	holds(t, body, note+` (mở trong tab mới)</span></a>`)
	errorsAre(t, []struct{ content, error string }{
		{`<content><link value="/docs" target="_blank">Docs</link></content>`, `content.ui:1:30: target là chữ của web; mở ở tab mới viết new-tab="true"`},
		{`<content><link value="/docs" new-tab="yes">Docs</link></content>`, `content.ui:1:30: new-tab của link chỉ nhận "true" hoặc "false"`},
	})
}

// A view whose background paints a gradient paints it on a layer fixed to the screen, the first element of
// <body>, which keeps none of the view's backgrounds; the browser's bar takes the gradient's first stop in each
// mode, in the look the site shows when it fixes one. A view of a colour keeps it on <body>, with no bar colour.
// A background from data beside a gradient is an error.
func TestAGradientViewPaintsABackdrop(t *testing.T) {
	files := map[string]string{
		"/app/site/view.ui":    `<view background="cirrus" medium:background="dusk" color="ink"><content /></view>`,
		"/app/site/content.ui": `<content><text>x</text></content>`,
	}
	document, body := renderLook(t, files, ui.Head{}, withDark(t))
	holds(t, body, `<body class="flex-column color-ink" data-kitwork-layout="view.ui"><div class="page-backdrop background-cirrus medium:background-dusk" aria-hidden="true" data-kitwork-backdrop></div>`)
	if !strings.Contains(document, `<meta name="theme-color" media="(prefers-color-scheme: light)" content="#`) ||
		!strings.Contains(document, `<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#`) ||
		!strings.Contains(document, ` data-kitwork-light="#`) || !strings.Contains(document, "page-backdrop") {
		t.Errorf("a backdrop page has no colour of the browser's bar for each mode:\n%s", document[:strings.Index(document, "<body")])
	}
	document, _ = renderLook(t, files, ui.Head{}, func(look *lookChange) { withDark(t)(look); *look.appearance = "dark" })
	if strings.Count(document, `<meta name="theme-color" content="#`) != 1 || strings.Contains(document, `<meta name="theme-color" media=`) {
		t.Errorf("a site that shows the dark look first has not one bar colour:\n%s", document[:strings.Index(document, "<body")])
	}
	// the first stop of cirrus, the background written without a width, never medium:'s dusk
	document, _ = renderLook(t, files, ui.Head{}, func(*lookChange) {})
	if !strings.Contains(document, "<meta name=\"theme-color\" content=\"#849cdd\">\n") || strings.Count(document, "theme-color") != 1 {
		t.Errorf("a site with no dark look has not cirrus's first stop for the bar:\n%s", document[:strings.Index(document, "<body")])
	}
	files["/app/site/view.ui"] = `<view background="paper"><content /></view>`
	document, body = renderLook(t, files, ui.Head{}, func(*lookChange) {})
	holds(t, body, `<body class="flex-column background-paper" data-kitwork-layout="view.ui">`)
	if strings.Contains(document, "theme-color") || strings.Contains(body, "data-kitwork-backdrop") {
		t.Errorf("a view of a colour has a backdrop:\n%s", document)
	}
	for view, want := range map[string]string{
		`<view background="cirrus" medium:background="{{ 1 > 0 ? 'dusk' : 'paper' }}"><content /></view>`: `view.ui:1:27: medium:background="{{ 1 > 0 ? 'dusk' : 'paper' }}": <view> có nền gradient`,
		`<view background="{{ 1 > 0 ? 'cirrus' : 'paper' }}"><content /></view>`:                          `view.ui:1:7: background="{{ 1 > 0 ? 'cirrus' : 'paper' }}": <view> có nền gradient`,
	} {
		err := renderError(t, map[string]string{"/app/site/view.ui": view, "/app/site/content.ui": `<content><text>x</text></content>`})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("a backdrop from data: %v\nwant %s", err, want)
		}
	}
}

// The site's design.transition is written on <html> for Drive, unless it is none.
func TestTheTransitionIsWrittenOnTheDocument(t *testing.T) {
	files := map[string]string{"/app/site/content.ui": `<content><text>x</text></content>`}
	document, _ := renderLook(t, files, ui.Head{Language: "en"}, func(look *lookChange) { *look.transition = "fade" })
	if !strings.Contains(document, `<html lang="en" data-kitwork-transition="fade">`) {
		t.Errorf("no transition on <html>:\n%s", document[:200])
	}
	document, _ = renderLook(t, files, ui.Head{Language: "en"}, func(look *lookChange) { *look.transition = "none" })
	if strings.Contains(document, "data-kitwork-transition") {
		t.Errorf("transition none is written:\n%s", document[:200])
	}
}

// A breadcrumb is a <nav> named in the page's language (or by its alt) holding an <ol class="breadcrumb"> of its
// items, each a row whose words stand in a line; a link in an item is a step, a <text> with no element inside is
// the page viewed; after the <nav>, the trail as BreadcrumbList data, a path written with the site's origin.
func TestABreadcrumbIsATrailAndItsData(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><breadcrumb gap="2"><item gap="2"><link value="/docs">The <text weight="bold">docs</text></link><icon value="chevron-right" /></item>` +
			`<item><text>Getting started</text></item></breadcrumb></content>`,
	}
	_, body := renderHead(t, files, value.Value{}, ui.Head{Language: "en", Path: "/docs/start", Origin: "https://kitwork.io"})
	holds(t, body,
		`<nav aria-label="Breadcrumb"><ol class="breadcrumb flex-row gap-2" role="list"><li class="flex-row gap-2"><a class="flex-row" href="/docs" aria-current="true">The <span class="weight-bold">docs</span></a><i class="icon-chevron-right" aria-hidden="true"></i></li>`,
		`<li class="flex-row"><span aria-current="page">Getting started</span></li></ol></nav>`,
		`<script type="application/ld+json">{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[`+
			`{"@type":"ListItem","position":1,"name":"The docs","item":"https://kitwork.io/docs"},`+
			`{"@type":"ListItem","position":2,"name":"Getting started","item":"https://kitwork.io/docs/start"}]}</script>`)
	_, body = renderHead(t, files, value.Value{}, ui.Head{Language: "vi"})
	holds(t, body, `<nav aria-label="Vị trí trang">`)
	files["/app/site/content.ui"] = `<content><breadcrumb alt="Trail"><item><text>{{ '</script><b>' }} & co</text></item></breadcrumb></content>`
	_, body = renderHead(t, files, value.Value{}, ui.Head{Language: "en"})
	holds(t, body, `<nav aria-label="Trail">`, `"name":"\u003c/script\u003e\u003cb\u003e \u0026 co"`)
	errorsAre(t, []struct{ content, error string }{
		{`<content><breadcrumb>a<item><text>x</text></item></breadcrumb></content>`, `content.ui:1:22: chữ không đặt thẳng trong breadcrumb`},
		{`<content><breadcrumb><box>a</box></breadcrumb></content>`, `content.ui:1:22: con của breadcrumb chỉ là item`},
		{`<content><breadcrumb current="x"><item><text>x</text></item></breadcrumb></content>`, `content.ui:1:22: thuộc tính current không có trên <breadcrumb>`},
	})
}
