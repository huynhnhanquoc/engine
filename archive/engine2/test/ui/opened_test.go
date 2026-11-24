package ui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// What opens and closes is opened, and open() opens it (16/11/2026, round 5 of the 13/11 review; the owner's rule of
// 26/10/2026: a verb is a function, a participle a state, as play() and playing): a dropdown's, a network's and an
// editor's state is opened, the popover shows on it, a dropdown has open() beside close() and toggle(), and the state
// prefix is opened:. They were open, the state, and open:, so a dropdown had no function that opened it. CONTROL: the
// popover shown on open again, red.
func TestWhatOpensIsOpened(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><dropdown as="$menu" keydown:escape="close()"><button click="toggle()" expanded="{{ opened }}">m</button>` +
			`<icon value="chevron-down" opened:rotate="180" /><popover><button click="close()">x</button></popover></dropdown>` +
			`<button click="$menu.open()">open it</button><text>{{ $menu.opened ? 'shown' : 'hidden' }}</text>` +
			`<network><card show="opened"><text>{{ connected ? 'back' : 'gone' }}</text></card></network></content>`,
	}, value.Value{})
	holds(t, body,
		`aria-expanded="false" data-kitwork-bind="aria-expanded: opened" data-kitwork-event="click: toggle()"`,
		`<i class="opened:rotate-180 icon-chevron-down" aria-hidden="true">`,
		`<div class="popover flex-column" hidden data-kitwork-bind="show: opened">`,
		`data-kitwork-event="click: $menu.open()"`,
		`data-kitwork-bind="text: $menu.opened ? &#39;shown&#39; : &#39;hidden&#39;">hidden</p>`,
		`<div class="card flex-column" hidden data-kitwork-bind="show: opened">`,
	)
}

// The spellings of before are each an error saying the one of now, where they are written: open read as a value
// inside a component that has opened (a dropdown's open is its function now, a network's and an editor's nothing), a
// write of it, an alias's .open read as a value, the prefix open:. What calls open() is no error, inside or through
// the alias. CONTROL: open read as a value let through, red (the dropdown painted a function's text).
func TestTheOpenOfBeforeIsAnError(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><dropdown><button click="toggle()" expanded="{{ open }}">m</button><popover>p</popover></dropdown></content>`,
			"content.ui:1:45: bên trong dropdown open dùng open như một giá trị; trạng thái mở của dropdown là opened, open() là hàm mở nó"},
		{`<content><dropdown><button click="open = false">m</button><popover>p</popover></dropdown></content>`,
			"content.ui:1:28: bên trong dropdown open = false dùng open như một giá trị; trạng thái mở của dropdown là opened"},
		{`<content><dropdown><text show="!open">m</text><popover>p</popover></dropdown></content>`,
			"content.ui:1:26: bên trong dropdown !open dùng open như một giá trị; trạng thái mở của dropdown là opened"},
		{`<content><network><card show="open"><text>gone</text></card></network></content>`,
			"content.ui:1:25: bên trong network open dùng open như một giá trị; trạng thái mở của network là opened"},
		{`<content><dropdown as="$menu"><popover>p</popover></dropdown><text>{{ $menu.open }}</text></content>`,
			"content.ui:1:62: ngoài mọi component $menu.open dùng $menu.open như một giá trị; trạng thái mở là $menu.opened"},
		{`<content><icon value="chevron-down" open:rotate="180" /></content>`,
			"tiền tố open: nay là opened:"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.Contains(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error holding\n %s", c.content, err, c.error)
		}
	}
	for _, content := range []string{
		`<content><dropdown><button click="open()">m</button><button click="opened ? close() : open()">t</button><popover>p</popover></dropdown></content>`,
		`<content><dropdown as="$menu"><popover>p</popover></dropdown><button click="$menu.open()">o</button></content>`,
		`<content><dropdown><button click="toggle()" expanded="{{ opened }}">{{ opened ? 'open' : 'shut' }}</button><popover>p</popover></dropdown></content>`,
	} {
		if err := renderError(t, map[string]string{"/app/site/content.ui": content}); err != nil {
			t.Errorf("%s: %v", content, err)
		}
	}
}

// In a browser, a dropdown opens by open() through its alias from outside it and closes by close() inside it, the
// popover shown while it is opened. CONTROL: the dropdown's open() left out of its file, red.
func TestBrowserADropdownOpensByOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	files := map[string]string{"content.ui": `<content><dropdown as="$menu"><button click="toggle()" expanded="{{ opened }}">m</button>` +
		`<popover><button click="close()">x</button></popover></dropdown><button click="$menu.open()">open it</button></content>`}
	document, _ := drawWarned(t, files, value.Value{}, ui.Head{})
	page := strings.Replace(document, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var popover = document.querySelector(".popover"), trigger = document.querySelector("main div > button");
    var seen = function () { return (popover.hidden ? "hidden" : "shown") + " " + trigger.getAttribute("aria-expanded"); };
    var states = [seen()];
    document.querySelector("main > button").click();
    setTimeout(function () {
      states.push(seen());
      popover.querySelector("button").click();
      setTimeout(function () {
        states.push(seen());
        fetch("/report", { method: "POST", body: JSON.stringify(states) });
      }, 100);
    }, 100);
  });
});
</script></body>`, 1)
	var states []string
	if err := json.Unmarshal(runInBrowser(t, browser, page), &states); err != nil {
		t.Fatal(err)
	}
	if want := "hidden false|shown true|hidden false"; strings.Join(states, "|") != want {
		t.Errorf("the dropdown went %q, want %s", states, want)
	}
}
