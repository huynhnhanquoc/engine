package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// errorsAre fails for each content whose drawing does not end on an error beginning as said.
func errorsAre(t *testing.T, cases []struct{ content, error string }) {
	t.Helper()
	for _, c := range cases {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
}

// The tabs: the host is the tablist, seeded with the tab it starts on; a tab is a button whose click is
// select('<name>'), with its id, role, value and the panels it controls, aria-selected and tabindex bound and
// painted; a panel is an element anywhere whose show compares the alias's selected with a tab's name, tied to its
// tab by ids, hidden unless its tab is the one it starts on, a panel before its tablist too.
func TestTheTabsWireTheirTabsAndPanels(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><box show="$files.selected == 'page'">p</box>` +
			`<tabs as="$files" alt="Files"><button click="select('router')">r</button><button click="select('page')">p</button></tabs>` +
			`<box show="$files.selected == 'router'">r</box></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column" id="kitwork-tabpanel-1-2" role="tabpanel" aria-labelledby="kitwork-tab-1-2" tabindex="0" hidden data-kitwork-bind="show: $files.selected == &#39;page&#39;">p</div>`,
		`<div class="flex-column" data-kitwork-component="tabs: $files" role="tablist" data-kitwork-scope="selected: &#39;router&#39;" aria-label="Files">`,
		`<button class="flex-row background-ink/6 hover:background-ink/10 pressed:background-ink/14 disabled:background-ink/4" id="kitwork-tab-1-1" type="button" role="tab" aria-controls="kitwork-tabpanel-1-1" value="router" aria-selected="true" tabindex="0" data-kitwork-bind="aria-selected: selected == &#39;router&#39;; tabindex: selected == &#39;router&#39; ? 0 : -1" data-kitwork-event="click: select(&#39;router&#39;)">r</button>`,
		`id="kitwork-tab-1-2" type="button" role="tab" aria-controls="kitwork-tabpanel-1-2" value="page" aria-selected="false" tabindex="-1"`,
		`<div class="flex-column" id="kitwork-tabpanel-1-1" role="tabpanel" aria-labelledby="kitwork-tab-1-1" tabindex="0" data-kitwork-bind="show: $files.selected == &#39;router&#39;">r</div>`)
}

// A tabs that persists keeps its choice under the set of its tab names, the name it hands its instance, and the page
// holds the pre-paint of that name right after the last tablist or panel of it; selected="…" chooses the tab it
// starts on.
func TestTheTabsPersistAndStart(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><tabs as="$shell" persist="true" selected="powershell"><button click="select('powershell')">p</button>` +
			`<button click="select('bash')">b</button></tabs><box show="$shell.selected == 'bash'">b</box>` +
			`<box show="$shell.selected == 'powershell'">p</box><text>after</text></content>`,
	}, value.Value{})
	holds(t, body,
		`data-kitwork-scope="persist: &#39;bash+powershell&#39;; selected: &#39;powershell&#39;"`,
		`<div class="flex-column" id="kitwork-tabpanel-1-1" role="tabpanel" aria-labelledby="kitwork-tab-1-1" tabindex="0" data-kitwork-bind="show: $shell.selected == &#39;powershell&#39;">p</div><script data-kitwork-generated="tabs" data-kitwork-persist="bash+powershell" data-kitwork-tabs="kitwork-tab-1-1 kitwork-tab-1-2">(function(s){`,
		`id="kitwork-tabpanel-1-2" role="tabpanel" aria-labelledby="kitwork-tab-1-2" tabindex="0" hidden`)
	errorsAre(t, []struct{ content, error string }{
		{`<content><tabs as="$t"><text>x</text></tabs></content>`, "content.ui:1:10: tabs cần ít nhất một tab bên trong <tabs>"},
		{`<content><tabs as="$t"><button click="select('a')">a</button><button click="select('a')">b</button></tabs></content>`, "content.ui:1:70: tab a đã có ở dòng 1"},
		{`<content><tabs as="$t"><button for="x of xs" click="select('a')">a</button></tabs></content>`, "content.ui:1:46: tab a nằm trong for"},
		{`<content><tabs as="$t"><button click="select('a')" value="a">a</button></tabs></content>`, "content.ui:1:52: value của một tab là tên của nó"},
		{`<content><tabs as="$t"><button click="select('a')">a</button></tabs><code show="$t.selected == 'a'">x</code></content>`, "content.ui:1:75: <code> không làm panel của tab a được"},
		{`<content><tabs as="$t" persist="yes"><button click="select('a')">a</button></tabs></content>`, `content.ui:1:24: persist="yes" không phải một giá trị của persist`},
		{`<content><tabs as="$t" remember="shell"><button click="select('a')">a</button></tabs></content>`, `content.ui:1:24: remember đã đổi thành persist (16/10/2026): viết persist="true" để mọi tabs có cùng các tab trên site đi theo một lựa chọn, hay persist="shell"`},
		{`<content><tabs as="$t" selected="b"><button click="select('a')">a</button></tabs></content>`, `content.ui:1:24: selected="b" không phải tên của tab nào (a)`},
	})
}

// The carousel: a region a screen reader names; its track the one <list> inside, scrolling and live while it does
// not play; each written slide named "1 / 3", aria-current on the one it starts on; a dot (go of a place the page
// knows) holds its place as its value and runs go(Number($this.value)); go of the component's own names is a
// button like next(). Slides from a for make count the page's data's, handed to the instance.
func TestTheCarouselWiresItsTrackSlidesAndDots(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><carousel as="$hero" alt="Menu" duration="5s" current="1" transition="fade">` +
			`<list><item>a</item><item>b</item><item>c</item></list>` +
			`<button for="x, i of [1, 2, 3]" click="go(i)">.</button><button click="go(current + 1)">+</button></carousel></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column carousel carousel-fade" data-kitwork-component="carousel: $hero" role="region" aria-roledescription="carousel" aria-label="Menu" data-kitwork-scope="duration: 5000; current: 1">`,
		`<ul class="flex-column carousel-track" id="kitwork-carousel-1" tabindex="0" aria-live="off" role="list" data-kitwork-bind="aria-live: playing ? &#39;off&#39; : &#39;polite&#39;">`,
		`<li class="flex-column" aria-roledescription="slide" aria-label="1 / 3" aria-current="false">a</li>`,
		`<li class="flex-column" aria-roledescription="slide" aria-label="2 / 3" aria-current="true">b</li>`,
		`aria-controls="kitwork-carousel-1" value="1" aria-current="true" type="button" data-kitwork-event="click: go(Number($this.value))">.</button>`,
		`type="button" data-kitwork-event="click: go(current + 1)">+</button>`)
	_, body = renderHead(t, map[string]string{
		"/app/site/content.ui": `<content><carousel><list><item>a</item><item for="x of xs">{{ x }}</item></list></carousel></content>`,
	}, object("xs", value.Array([]value.Value{value.String("b"), value.String("c")})), ui.Head{Language: "vi"})
	holds(t, body, `data-kitwork-scope="count: 3"`, `aria-label="Trình chiếu"`, `<li class="flex-column" aria-roledescription="trang chiếu">a</li>`)
	errorsAre(t, []struct{ content, error string }{
		{`<content><carousel><box>a</box></carousel></content>`, "content.ui:1:10: carousel cần một <list> ngay bên trong"},
		{`<content><carousel><list><item>a</item></list><list><item>b</item></list></carousel></content>`, "content.ui:1:47: carousel có một dải trang"},
		{`<content><carousel><list flex="row"><item>a</item></list></carousel></content>`, "content.ui:1:26: dải trang của carousel là một hàng engine đặt"},
		{`<content><carousel><list></list></carousel></content>`, "content.ui:1:20: dải trang của carousel chưa có trang nào"},
		{`<content><carousel current="3"><list><item>a</item></list></carousel></content>`, `content.ui:1:20: current="3" quá số trang`},
		{`<content><carousel autoplay="true"><list><item>a</item></list></carousel></content>`, "content.ui:1:20: carousel không có autoplay"},
		{`<content><carousel duration="90s"><list><item>a</item></list></carousel></content>`, `content.ui:1:20: duration="90s" dài hơn 60 giây`},
		{`<content><carousel transition="slide-left"><list><item>a</item></list></carousel></content>`, `content.ui:1:20: transition="slide-left": trang chiếu trượt theo dòng chữ`},
	})
}

// The countdown: its time left a timer with a datetime bound to its parts, the text of more than one part joined as
// the server prints it, the instance seeded with its duration; the fields written in its <time> are segments the
// engine writes and binds none of, the <time> then a group named by its alt.
func TestTheCountdownDrawsItsTimeAndSegments(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><countdown as="$tea" duration="10s" finish="$tea.stop()"><text>Left: <time>{{ minutes }}:{{ String(seconds).padStart(2, '0') }}</time></text>` +
			`<button click="toggle()">{{ playing ? 'Pause' : 'Play' }}</button></countdown>` +
			`<countdown duration="1m"><time alt="Time"><input value="{{ minutes }}" alt="Minutes" />:<input value="{{ seconds }}" /></time></countdown></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column" data-kitwork-component="countdown: $tea" data-kitwork-scope="duration: 10000" data-kitwork-event="finish: $tea.stop()">`,
		`<time class="countdown-time" role="timer" datetime="PT0H0M10S" data-kitwork-bind="datetime: &#39;PT&#39; + hours + &#39;H&#39; + minutes + &#39;M&#39; + seconds + &#39;S&#39;; text: [(minutes), &#39;:&#39;, (String(seconds).padStart(2, &#39;0&#39;))].join(&#39;&#39;)">0:10</time>`,
		`<time class="countdown-time" role="group" aria-label="Time" datetime="PT0H1M0S"`,
		`<input role="spinbutton" inputmode="numeric" autocomplete="off" spellcheck="false" size="2" placeholder="00" aria-valuemin="0" aria-valuemax="59" aria-valuenow="1" aria-valuetext="1 minute" data-part="minutes" value="01" aria-label="Minutes">`,
		`<input aria-label="Seconds" role="spinbutton" inputmode="numeric" autocomplete="off" spellcheck="false" size="2" placeholder="00" aria-valuemin="0" aria-valuemax="59" aria-valuenow="0" aria-valuetext="0 seconds" data-part="seconds" value="00">`)
	if strings.Contains(body, "value: minutes") || strings.Contains(body, "value: seconds") {
		t.Errorf("a segment is bound:\n%s", body)
	}
	errorsAre(t, []struct{ content, error string }{
		{`<content><countdown><time>{{ seconds }}</time></countdown></content>`, `content.ui:1:10: countdown cần duration`},
		{`<content><countdown duration="5s"></countdown></content>`, "content.ui:1:10: countdown rỗng"},
		{`<content><countdown duration="5s"><text>x</text></countdown></content>`, "content.ui:1:10: countdown cần một <time>"},
		{`<content><countdown duration="5s"><time>{{ seconds }}</time><time>{{ seconds }}</time></countdown></content>`, "content.ui:1:61: countdown có một thời gian còn lại"},
		{`<content><countdown duration="5s"><time>x</time></countdown></content>`, "content.ui:1:35: <time> của countdown không đọc thời gian còn lại"},
		{`<content><countdown duration="5m"><time>{{ seconds }}</time></countdown></content>`, `content.ui:1:35: duration="5m" dài hơn một phút mà <time> chỉ đọc seconds`},
		{`<content><countdown duration="300"><time>{{ remaining }}</time></countdown></content>`, `content.ui:1:21: duration="300" thiếu đơn vị: viết duration="300s" (300 giây) hay duration="5m"`},
		{`<content><countdown duration="1.5s"><time>{{ remaining }}</time></countdown></content>`, `content.ui:1:21: duration="1.5s": countdown đếm từng giây`},
		{`<content><countdown duration="5s" autoplay="true"><time>{{ remaining }}</time></countdown></content>`, `content.ui:1:35: countdown không có autoplay`},
		{`<content><countdown duration="5s"><time><input value="{{ seconds }}" type="number" /></time></countdown></content>`, `content.ui:1:70: một phần của thời gian không có type="number"`},
		{`<content><countdown duration="5s"><time><input value="{{ seconds }}" />:<input value="{{ minutes }}" /></time></countdown></content>`, "content.ui:1:80: ô minutes đứng sau ô seconds"},
		{`<content><countdown duration="5s"><time><input value="{{ hours }}" /></time></countdown></content>`, "content.ui:1:35: <time> của countdown có ô hours mà không có ô minutes, seconds"},
	})
}

// A richtext writes its value cleaned, inside <div class="richtext"> and its attributes' classes: what the cleaner
// does not keep never reaches the page, a link to another host leaves it. Every other way to write HTML from data is
// told to write it.
func TestARichtextWritesItsValueCleaned(t *testing.T) {
	document, body := renderHead(t, map[string]string{
		"/app/site/content.ui": `<content><box><richtext value="{{ post }}" padding="4" /></box></content>`,
	}, object("post", value.String(`<h2>Hours</h2><p onclick="x()">Open <script>alert(1)</script><a href="https://other.test/a">there</a></p>`)), ui.Head{Origin: "https://shop.test"})
	holds(t, body, `<div class="richtext padding-4"><h2>Hours</h2>`, `<a href="https://other.test/a" rel="external noopener">there</a>`)
	if strings.Contains(body, "<script>") || strings.Contains(body, "onclick") {
		t.Errorf("the cleaner let through what it does not keep:\n%s", body)
	}
	holds(t, document, ".richtext")
	errorsAre(t, []struct{ content, error string }{
		{`<content><richtext /></content>`, "content.ui:1:10: richtext cần value"},
		{`<content><richtext html="{{ post }}" /></content>`, "content.ui:1:20: html không có: .ui không ghi HTML từ dữ liệu"},
		{`<content><richtext value="{{ '<p>x</p>' }}" /></content>`, "content.ui:1:20: richtext hiện bài đã lưu, value là {{ }}"},
		{`<content><richtext value="Hello" /></content>`, "content.ui:1:20: richtext hiện bài đã lưu, value là {{ }}"},
		{`<content><dropdown><richtext value="{{ opened }}" /><popover>p</popover></dropdown></content>`, "content.ui:1:30: richtext dựng ở server: value đọc dữ liệu của trang, không đọc opened"},
		{`<content><text><richtext value="{{ post }}" /></text></content>`, "content.ui:1:16: richtext là một khối"},
		{`<content><richtext value="{{ post }}"><text>x</text></richtext></content>`, "content.ui:1:10: richtext không có nội dung bên trong"},
		{`<content><raw>x</raw></content>`, "content.ui:1:10: không có thẻ <raw>"},
	})
}

// What the first engine writes on every page, moved with the components: a list says it is one (Safari drops a
// list's semantics without markers), its empty part is a status, an icon with alt="" is decoration with no name, and
// an icon's class comes after the classes of its attributes.
func TestListsAndIconsAsTheFirstEngineWritesThem(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><list gap="2"><item>a</item><empty>none</empty></list><icon value="x" color="brand" alt="" />` +
			`<box show="true" medium:show="false">b</box></content>`,
	}, value.Value{})
	holds(t, body, `<ul class="flex-column gap-2" role="list"><li class="flex-column">a</li><li class="flex-column" role="status">none</li></ul>`,
		`<i class="color-brand icon-x" aria-hidden="true"></i>`, `<div class="flex-column medium:show-false">b</div>`)
}
