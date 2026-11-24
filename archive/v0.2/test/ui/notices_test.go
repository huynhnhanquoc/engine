package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// renderHead draws the route "" of a site made of files with head, and gives the document and its body.
func renderHead(t *testing.T, files map[string]string, data value.Value, head ui.Head) (string, string) {
	t.Helper()
	site := memorySite(files)
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := site.Document(page, data, head, kitDesign(t))
	if err != nil {
		t.Fatal(err)
	}
	return document, document[strings.Index(document, "<body"):]
}

// headError is the error of drawing the route "" of a site made of files with head, nil when it draws.
func headError(t *testing.T, files map[string]string, head ui.Head) error {
	t.Helper()
	site := memorySite(files)
	page, err := site.Page(route(""))
	if err != nil {
		return err
	}
	_, _, err = site.Document(page, value.Value{}, head, kitDesign(t))
	return err
}

// holds fails for each of wants body does not hold.
func holds(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold\n%s", body, want)
		}
	}
}

// The progressbar is the move between pages Drive reports: fixed at the top edge, hidden at first, its line of the
// colour and thickness written, its name the page's language's, the delay Drive waits written only when it is not
// its own 120ms. It stands first in the frame and holds nothing; its words are fixed. A reader says what the page
// says aloud, with an alias when written.
func TestTheProgressbarAndTheReader(t *testing.T) {
	_, body := renderHead(t, map[string]string{
		"/app/site/view.ui":    `<view><progressbar color="success" thickness="3" delay="0" /><content /><reader as="$reader" /></view>`,
		"/app/site/content.ui": `<content>c</content>`,
	}, value.Value{}, ui.Head{Language: "en", Drive: true})
	holds(t, body,
		`<div class="progressbar" data-kitwork-component="progressbar" data-kitwork-bind="show: active; aria-valuenow: value" hidden role="progressbar" data-kitwork-delay="0" aria-label="Loading the next page" aria-valuemin="0" aria-valuemax="100" data-kitwork-style="top: top"><div class="thickness-3 background-success" data-kitwork-style="width: value + &#39;%&#39;"></div></div>`,
		`<div class="reader" data-kitwork-component="reader: $reader" data-kitwork-bind="text: text" aria-live="polite" aria-atomic="true"></div></body>`)
	if strings.Count(body, `class="progressbar"`) != 1 || strings.Count(body, `class="reader"`) != 1 {
		t.Errorf("a frame that writes its bar and its reader gets another:\n%s", body)
	}
	_, body = renderHead(t, map[string]string{
		"/app/site/view.ui":    `<view><progressbar alt="Đang tải" delay="120" /><content /></view>`,
		"/app/site/content.ui": `<content>c</content>`,
	}, value.Value{}, ui.Head{Language: "vi"})
	if !strings.Contains(body, `role="progressbar" aria-label="Đang tải"`) || strings.Contains(body, "data-kitwork-delay") || strings.Contains(body, `class="reader"`) {
		t.Errorf("a bar with alt and Drive's own delay, on a site that reloads:\n%s", body)
	}
	_, body = renderHead(t, map[string]string{
		"/app/site/view.ui":    `<view><content /></view>`,
		"/app/site/content.ui": `<content>c</content>`,
	}, value.Value{}, ui.Head{Language: "vi", Drive: true})
	holds(t, body, `aria-label="Đang chuyển trang"`, `<div class="thickness-1 background-brand"`)
	for _, c := range []struct{ view, error string }{
		{`<view><content /><box><progressbar /></box></view>`, "view.ui:1:23: progressbar đặt trong view.ui, ở đầu <view>"},
		{`<view><progressbar><text>x</text></progressbar><content /></view>`, "view.ui:1:7: progressbar không có nội dung con"},
		{`<view><progressbar thickness="thin" /><content /></view>`, "view.ui:1:20: thickness của progressbar là số px từ 1 đến 8"},
		{`<view><progressbar color="{{ tone }}" /><content /></view>`, "view.ui:1:20: color của progressbar là chữ cố định"},
		{`<view><progressbar size="2" /><content /></view>`, "view.ui:1:20: progressbar chỉ nhận color"},
		{`<view><content /><reader say="x" /></view>`, "view.ui:1:26: reader chỉ nhận as"},
		{`<view><content /><reader><text>x</text></reader></view>`, "view.ui:1:18: reader không có nội dung con"},
	} {
		err := headError(t, map[string]string{"/app/site/view.ui": c.view, "/app/site/content.ui": `<content>c</content>`}, ui.Head{})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.view, err, c.error)
		}
	}
}

// kitwork.io's network notice: the host floats at the corner its place names (no transition class for the slide a
// placed host makes), is a status region since it draws a notice, and seeds the instance with its two times in
// milliseconds; what follows connected and open is bound, painted as the browser starts (connected, closed). In
// the page's flow it has no place of its own.
func TestTheNetworksNotice(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><network as="$network" place="bottom-left" transition="slide" delay="1s" duration="4s">` +
			`<card show="opened"><icon show="!connected" value="wifi-off" alt="Offline" />` +
			`<text>{{ connected ? 'Back online' : 'You are offline' }}</text><button click="close()">x</button></card></network>` +
			`<network offline="$network.close()" /></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column network network-bottom-left" data-kitwork-component="network: $network" role="status" data-kitwork-scope="delay: 1000; duration: 4000">`,
		`<div class="card flex-column" hidden data-kitwork-bind="show: opened">`,
		`<i class="icon-wifi-off" hidden role="img" aria-label="Offline" data-kitwork-bind="show: !connected"></i>`,
		`<p data-kitwork-bind="text: connected ? &#39;Back online&#39; : &#39;You are offline&#39;">Back online</p>`,
		`data-kitwork-event="click: close()"`,
		`<div class="flex-column" data-kitwork-component="network" data-kitwork-event="offline: $network.close()"></div>`)
	for _, c := range []struct{ content, error string }{
		{`<content><network delay="300" /></content>`, `content.ui:1:19: delay="300" thiếu đơn vị: viết delay="300ms"`},
		{`<content><network delay="20s" /></content>`, `content.ui:1:19: delay="20s" dài hơn 10 giây`},
		{`<content><network timeout="4s" /></content>`, "content.ui:1:19: network không có timeout"},
		{`<content><network place="bottom-end"><text>x</text></network></content>`, `content.ui:1:19: place="bottom-end": start và end nay là left và right, viết place="bottom-right"`},
		{`<content><network transition="slide"><text>x</text></network></content>`, `content.ui:1:19: transition="slide" trượt vào từ mép màn hình mà network không có place`},
		{`<content><network place="bottom" transition="all 0.3s"><text>x</text></network></content>`, `content.ui:1:34: transition="all 0.3s" là cách viết của CSS`},
		{`<content><box offline="x()">b</box></content>`, `content.ui:1:15: offline là sự kiện của component network, chỉ thẻ của nó nghe được: <network offline="…" />`},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
}

// The toast's notice is a blueprint the kernel makes each row of: written once, data-kitwork-for on it and its
// leaving bound, every value of the notice bound and none painted (a link has no address, a text no words), what
// a show of the notice hides hidden. The host is a region named in the page's language that F8 reaches, seeded
// with its place and limit; the two regions a screen reader hears close it.
func TestTheToastsNoticeIsABlueprint(t *testing.T) {
	notice := `<card for="notice of notices; key: notice.id"><icon show="notice.tone == 'danger'" value="alert-circle" alt="Error" />` +
		`<text show="notice.title">{{ notice.title }}</text><badge show="notice.count > 1">{{ 'x' + notice.count }}</badge><link show="notice.action.link" value="{{ notice.action.link }}">{{ notice.action.label }}</link>` +
		`<button click="dismiss(notice.id)"><icon value="x" alt="Dismiss" /></button></card>`
	_, body := render(t, map[string]string{"/app/site/content.ui": `<content><toast as="$toast" gap="2" limit="5" transition="fade">` + notice + `</toast></content>`}, value.Value{})
	holds(t, body,
		`<div class="flex-column gap-2 toast toast-bottom-right transition-fade" data-kitwork-component="toast: $toast" role="region" aria-label="Notifications (F8)" tabindex="-1" data-kitwork-scope="place: &#39;bottom-right&#39;; limit: 5">`,
		`<div class="card flex-column" data-kitwork-for="notice of notices; key: notice.id" data-kitwork-bind="data-leaving: notice.leaving">`,
		`<i class="icon-alert-circle" hidden role="img" aria-label="Error" data-kitwork-bind="show: notice.tone == &#39;danger&#39;"></i>`,
		`<p hidden data-kitwork-bind="show: notice.title; text: notice.title"></p>`,
		`<span class="badge flex-row" hidden data-kitwork-bind="show: notice.count &gt; 1; text: &#39;x&#39; + notice.count"></span>`,
		`<a hidden data-kitwork-bind="show: notice.action.link; href: notice.action.link; text: notice.action.label"></a>`,
		`data-kitwork-event="click: dismiss(notice.id)"`,
		`</div><span class="reader" role="status" aria-atomic="false"></span><span class="reader" role="alert" aria-atomic="false"></span></div>`)
	_, body = renderHead(t, map[string]string{"/app/site/content.ui": `<content><toast place="top">` + notice + `</toast></content>`}, value.Value{}, ui.Head{Language: "vi"})
	holds(t, body, `toast toast-top" data-kitwork-component="toast" role="region" aria-label="Thông báo (F8)"`, "place: &#39;top&#39;; limit: 3")
	for _, c := range []struct{ content, error string }{
		{`<content><toast><text>x</text></toast></content>`, "content.ui:1:10: toast cần một mẫu thông báo bên trong <toast>"},
		{`<content><toast><card for="n of notices">x</card></toast></content>`, "content.ui:1:23: mẫu thông báo cần key: n.id"},
		{`<content><toast><card for="n of items; key: n.id">x</card></toast></content>`, "content.ui:1:23: danh sách của toast là notices (items là tên cũ)"},
		{`<content><toast><card for="n of notices; key: n.id"><text>{{ n.text }}</text><button click="dismiss(n.id)" alt="Đóng">x</button></card></toast></content>`,
			"content.ui:1:59: n không có text: câu là n.message"},
		{`<content><toast><card for="n of notices; key: n.id"><text>{{ n.message }}</text></card></toast></content>`, "content.ui:1:17: mỗi thông báo cần một nút đóng"},
		{`<content><toast><card for="n of notices; key: n.id"><button show="n.title" click="dismiss(n.id)" alt="Đóng">x</button></card></toast></content>`,
			"content.ui:1:61: nút đóng của thông báo luôn hiện: bỏ show ở đây"},
		{`<content><toast limit="11">` + notice + `</toast></content>`, `content.ui:1:17: limit là số thông báo hiện cùng lúc, từ 1 đến 10`},
		{`<content><toast>` + notice + `</toast><toast>` + notice + `</toast></content>`, "content.ui:1:"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
	if err := renderError(t, map[string]string{"/app/site/content.ui": `<content><toast>` + notice + `</toast><toast>` + notice + `</toast></content>`}); err == nil || !strings.Contains(err.Error(), "trang đã có một toast ở") {
		t.Errorf("two toasts: %v", err)
	}
}

// A link says where it goes: the page viewed (aria-current="page"), a folder it stands in ("true", never for "/" or
// an exact link), a web address on another host (rel="external noopener"); outside the content, how Drive marks it
// again. A scheme that is no place to go is an error when written and no address when it comes from data;
// reload="true" loads its page whole.
func TestALinkSaysWhereItGoes(t *testing.T) {
	_, body := renderHead(t, map[string]string{
		"/app/site/view.ui": `<view><header><link value="/menu">Menu</link><link value="/menu" exact="true">Exact</link><link value="/">Home</link></header><content /></view>`,
		"/app/site/content.ui": `<content><link value="/menu/tea">Tea</link><link value="https://shop.test/a">Own</link>` +
			`<link value="https://github.com/kitwork">Source</link><link value="{{ bad }}">Bad</link><link value="/pay" reload="true">Pay</link></content>`,
	}, object("bad", value.String("java\tscript:alert(1)")), ui.Head{Path: "/menu/tea", Origin: "https://shop.test"})
	holds(t, body,
		`<a href="/menu" aria-current="true" data-kitwork-current="">Menu</a>`,
		`<a href="/menu" data-kitwork-current="exact">Exact</a>`,
		`<a href="/" data-kitwork-current="">Home</a>`,
		`<a href="/menu/tea" aria-current="page">Tea</a>`,
		`<a href="https://shop.test/a">Own</a>`,
		`<a href="https://github.com/kitwork" rel="external noopener">Source</a>`,
		`<a>Bad</a>`,
		`<a href="/pay" data-kitwork-reload="true">Pay</a>`)
	for _, c := range []struct{ content, error string }{
		{`<content><link value="javascript:alert(1)">x</link></content>`, "content.ui:1:16: link đi tới đường dẫn trong site"},
		{`<content><link value="/a" exact="yes">x</link></content>`, `content.ui:1:27: exact của link chỉ nhận "true" hoặc "false"`},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
}

// An image comes from the site, the web or data:image/… (KITWORK_UI mục 7, the first engine's safeImage): another
// scheme is an error when written and no src when it comes from data, the alt still saying what it was. 13/11/2026:
// engine2 wrote any src, javascript: and data:text/html among them.
//
// CONTROL: with render.go's image case writing src without safeImage, the two images from data had their src and
// the four written were no error; this test failed on each.
func TestAnImageComesFromAnImageAddress(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><image value="{{ bad }}" alt="Bad" /><image value="{{ other }}" alt="Other" />` +
			`<image value="{{ pixel }}" alt="Pixel" /><image value="https://cdn.test/a.png" alt="Web" /></content>`,
	}, object("bad", value.String(" java\tscript:alert(1)"), "other", value.String("data:text/html,<b>x</b>"),
		"pixel", value.String("data:image/gif;base64,R0lGODlh")))
	holds(t, body,
		`<img alt="Bad" loading="lazy" decoding="async">`,
		`<img alt="Other" loading="lazy" decoding="async">`,
		`<img src="data:image/gif;base64,R0lGODlh" alt="Pixel" loading="lazy" decoding="async">`,
		`<img src="https://cdn.test/a.png" alt="Web" loading="lazy" decoding="async">`)
	for _, written := range []string{"javascript:alert(1)", "data:text/html,x", "vbscript:x", "mailto:a@b.c"} {
		content := `<content><image value="` + written + `" alt="x" /></content>`
		err := renderError(t, map[string]string{"/app/site/content.ui": content})
		if want := "content.ui:1:17: image lấy ảnh từ đường dẫn trong site (/…, ./…), http:, https: hay data:image/…; \"" + written + "\" không phải ảnh"; err == nil || err.Error() != want {
			t.Errorf("%s:\n %v\nwant\n %s", content, err, want)
		}
	}
	if err := renderError(t, map[string]string{"/app/site/content.ui": `<content><image value="data:image/png;base64,AAAA" alt="x" /></content>`}); err != nil {
		t.Errorf("an image written as data:image/…: %v", err)
	}
}

// A button whose author writes nothing of its fill or its edge has the engine's: translucent ink, deeper under the
// pointer and while pressed, fainter while disabled, each a rule of the page; one that writes a background, a
// border or a material, with a prefix or not, is the author's.
func TestANeutralButtonHasTheEnginesFill(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><button>a</button><button border="none">b</button><button hover:background="panel">c</button></content>`,
	}, value.Value{})
	holds(t, body,
		`<button class="flex-row background-ink/6 hover:background-ink/10 pressed:background-ink/14 disabled:background-ink/4" type="button">a</button>`,
		`<button class="flex-row border-none" type="button">b</button>`,
		`<button class="flex-row hover:background-panel" type="button">c</button>`)
	holds(t, document, `.background-ink\/6{`, `.disabled\:background-ink\/4`)
}

// The page's CSS is three blocks Drive trades on a move, each by its bytes: the site's, the icons' masks, the
// rules of its classes.
func TestThePagesStylesheetsAreBlocksDriveTrades(t *testing.T) {
	document, _ := render(t, map[string]string{"/app/site/content.ui": `<content gap="2"><icon value="x" /></content>`}, value.Value{})
	head := document[:strings.Index(document, "</head>")]
	site, icons, css := strings.Index(head, `<style data-kitwork-generated="site">`), strings.Index(head, `<style data-kitwork-generated="icons">`), strings.Index(head, `<style data-kitwork-generated="css">`)
	if site < 0 || icons < site || css < icons || strings.Count(head, "<style") != 3 || strings.Count(head, "</style>") != 3 ||
		strings.Count(head[site:icons], "</style>") != 1 || strings.Count(head[icons:css], "</style>") != 1 {
		t.Fatalf("the head's blocks:\n%s", head)
	}
	if !strings.Contains(head[css:], ".gap-2{") || !strings.Contains(head[icons:css], ".icon-x{") || !strings.Contains(head[site:icons], "@layer reboot") {
		t.Errorf("a block holds what is not its own:\n%s", head)
	}
}
