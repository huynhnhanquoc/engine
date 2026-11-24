package ui_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// faqPage is a group of questions that opens one at a time, closed all by its alias, and an accordion that stands
// alone (docs/components/accordions.md, accordion.md).
var faqPage = map[string]string{"content.ui": `<content><accordions as="$faq" gap="2"><text heading="2">Questions</text>` +
	`<accordion for="faq of [{ id: 'a', title: 'Delivery' }, { id: 'b', title: 'Returns' }]" opened="{{ faq.id == 'a' }}">` +
	`<box click="toggle()" flex="row" space="between"><text heading="3">{{ faq.title }}</text><icon value="chevron-down" opened:rotate="180" /></box>` +
	`<text>Answer {{ faq.id }}</text></accordion></accordions><button click="$faq.close()">Close all</button>` +
	`<accordion opened="true"><text click="toggle()">Advanced</text><box>inside</box></accordion></content>`}

// groupName finds the name a group gives its accordions.
var groupName = regexp.MustCompile(`<details class="flex-column" name="(kitwork-accordions-[a-z0-9]+)" open>`)

// An accordion is the browser's <details>, its first child, the one with click="toggle()", its <summary> itself with
// its layout, toggle() the browser's (no event written), opened="true" or a {{ }} true the open attribute; a group of
// them gives each one name, so opening one closes the others, and its first text names it (role="group",
// aria-labelledby); with an alias it loads its file, whose close() a button calls; one that stands alone takes no
// name (16/11/2026, round 5 of the 13/11 review, dialog + catalogue, slice 3). It was a bare <details>: no summary,
// so the browser wrote its own "Details" and hid all of it. CONTROLS: the header drawn as written, red; the group's
// name left out, red.
func TestAnAccordionIsTheBrowsersDetails(t *testing.T) {
	document, warnings := drawWarned(t, faqPage, value.Value{}, ui.Head{})
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	name := groupName.FindStringSubmatch(document)
	if name == nil {
		t.Fatalf("no named accordion open:\n%s", document)
	}
	holds(t, document,
		`<div class="flex-column gap-2" data-kitwork-component="accordions: $faq" role="group" aria-labelledby="`+name[1]+`-title"><h2 id="`+name[1]+`-title">Questions</h2>`,
		`<details class="flex-column" name="`+name[1]+`" open><summary class="flex-row space-between"><h3>Delivery</h3><i class="opened:rotate-180 icon-chevron-down" aria-hidden="true"></i></summary><p>Answer a</p></details>`,
		`<details class="flex-column" name="`+name[1]+`"><summary class="flex-row space-between"><h3>Returns</h3>`,
		`data-kitwork-event="click: $faq.close()"`,
		`<details class="flex-column" open><summary>Advanced</summary><div class="flex-column">inside</div></details>`,
		`/_kitwork/components/accordions.`,
	)
	if strings.Contains(document, "toggle()") {
		t.Error("toggle() is written for the kernel; it is the browser's")
	}
	quiet, _ := drawWarned(t, map[string]string{"content.ui": `<content><accordions><accordion><text click="toggle()">a</text><text>b</text></accordion></accordions></content>`},
		value.Value{}, ui.Head{})
	body := quiet[strings.Index(quiet, "<body"):]
	if strings.Contains(quiet, "/_kitwork/components/accordions.") || strings.Contains(body, `role="group"`) {
		t.Errorf("a group without an alias or a title loads a file or says it is a named group: %s", body)
	}
}

// What an accordion and a group refuse, each where it is written (docs/components/accordion.md, accordions.md,
// "Compiler kiểm"), and open, its word until 26/10/2026. CONTROL: a header without toggle() let through, red.
func TestWhatAnAccordionRefuses(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><accordion><text>a</text><text>b</text></accordion></content>`,
			`content.ui:1:21: con đầu tiên của accordion là phần đầu, bấm vào thì mở hay đóng: viết click="toggle()" trên nó`},
		{`<content><accordion></accordion></content>`, "content.ui:1:10: accordion cần phần đầu"},
		{`<content><accordion><box click="toggle()"><icon value="x" /></box><text>b</text></accordion></content>`,
			"content.ui:1:21: phần đầu của accordion là một nút, cần chữ làm tên của nó"},
		{`<content><accordion><box click="toggle()"><text>a</text><button click="save()">s</button></box><text>b</text></accordion></content>`,
			"content.ui:1:57: <button> trong phần đầu của accordion: phần đầu đã là một nút (nút trong nút)"},
		{`<content><accordion><box click="toggle()"><text>a</text></box><icon value="x" click="toggle()" /></accordion></content>`,
			"content.ui:1:63: toggle() của accordion chỉ ở phần đầu"},
		{`<content><accordion open><text click="toggle()">a</text></accordion></content>`,
			"content.ui:1:21: open nay là opened (26/10/2026"},
		{`<content><accordion name="x"><text click="toggle()">a</text></accordion></content>`,
			"content.ui:1:21: name trên accordion: bọc các mục trong <accordions>"},
		{`<content><accordion opened="yes"><text click="toggle()">a</text></accordion></content>`,
			`content.ui:1:21: opened chỉ nhận "true", "false" hoặc {{ }}`},
		{`<content><accordions><accordion opened="true"><text click="toggle()">a</text></accordion><accordion opened="true"><text click="toggle()">b</text></accordion></accordions></content>`,
			`content.ui:1:101: hai mục cùng opened="true" trong accordions`},
		{`<content><accordions><box>x</box></accordions></content>`,
			"content.ui:1:22: <box> trong accordions: con của accordions là các accordion"},
		{`<content><accordions multiple="maybe"><accordion><text click="toggle()">a</text></accordion></accordions></content>`,
			`content.ui:1:22: multiple của accordions chỉ nhận "true" hoặc "false"`},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
	both := `<content><accordions multiple="true"><accordion opened="true"><text click="toggle()">a</text></accordion>` +
		`<accordion opened="true"><text click="toggle()">b</text></accordion></accordions></content>`
	if err := renderError(t, map[string]string{"/app/site/content.ui": both}); err != nil {
		t.Errorf("multiple accordions both opened: %v", err)
	}
	_, warnings := drawWarned(t, map[string]string{"content.ui": `<content><accordion><text click="toggle()">a</text><accordion><text click="toggle()">b</text></accordion></accordion></content>`},
		value.Value{}, ui.Head{})
	if !strings.Contains(strings.Join(warnings, "\n"), "accordion nằm thẳng trong accordion") {
		t.Errorf("warnings %q, want one for an accordion right inside another", warnings)
	}
}

// In a browser, opening one accordion of a group closes the one open (the browser's rule, by the name they share),
// the opened: icon turns with it, and $faq.close() closes them all; the one standing alone is left as it is.
// CONTROLS: the name left out, red (both open); close() doing nothing, red.
func TestBrowserAGroupOpensOneAtATime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	document, _ := drawWarned(t, faqPage, value.Value{}, ui.Head{})
	// the turn's transition is held still: a headless browser under virtual time may not finish one it draws no frame of
	document = strings.Replace(document, "</head>", "<style>*{transition:none!important}</style></head>", 1)
	page := strings.Replace(document, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var items = document.querySelectorAll("details"), seen = [];
    var look = function () {
      seen.push(Array.prototype.map.call(items, function (d) { return d.open ? "open" : "shut"; }).join(" ") + " " +
        getComputedStyle(items[0].querySelector("i")).rotate);
    };
    look();
    items[1].querySelector("summary").click();
    setTimeout(function () {
      look();
      document.querySelector("main > button").click();
      setTimeout(function () {
        look();
        fetch("/report", { method: "POST", body: JSON.stringify(seen) });
      }, 100);
    }, 100);
  });
});
</script></body>`, 1)
	var seen []string
	if err := json.Unmarshal(runInBrowser(t, browser, page), &seen); err != nil {
		t.Fatal(err)
	}
	if want := "open shut open 180deg|shut open open none|shut shut open none"; strings.Join(seen, "|") != want {
		t.Errorf("the group went %q, want %s", seen, want)
	}
}
