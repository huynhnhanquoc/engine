package ui_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// pageProgram finds the program a page loads.
var pageProgram = regexp.MustCompile(`<script data-kitwork-generated="page" src="(/_kitwork/page\.[0-9a-f]{16}\.js)" defer></script>`)

// A page's program (round 4 of the 13/11 review, slice 2, 15/11/2026): every source the kernel runs on the page is
// written as JavaScript into /_kitwork/page.<hash>.js, which the page loads after the kernel and before its
// components; a page the kernel runs nothing on loads none. The same sources are the same file; a source that is a
// name alone is named for a field to write back. CONTROL: no program given (Scripts), red.
func TestAPageLoadsItsSourcesAsJavaScript(t *testing.T) {
	files := map[string]string{"content.ui": `<content><dropdown as="$menu" keydown:escape="close()">` +
		"<button click=\"toggle()\">b</button><text>{{ `opened: ${opened}` }}</text><popover>p</popover></dropdown></content>"}
	document, _ := drawWarned(t, files, value.Value{}, ui.Head{})
	found := pageProgram.FindStringSubmatch(document)
	if found == nil {
		t.Fatalf("the page loads no program:\n%s", document[strings.LastIndex(document, "</style>"):])
	}
	kernelAt, programAt, dropdownAt := strings.Index(document, kernel.Kernel().Path), strings.Index(document, found[1]), strings.Index(document, "/_kitwork/components/dropdown.")
	if !(kernelAt < programAt && programAt < dropdownAt) {
		t.Errorf("the kernel at %d, the program at %d, the dropdown at %d: want them in that order", kernelAt, programAt, dropdownAt)
	}
	file, exact := ui.FileAt(found[1])
	if file == nil || !exact {
		t.Fatalf("FileAt(%s) is not the program", found[1])
	}
	body := string(file.Body)
	for _, want := range []string{
		`"close()": function () { return this.close(); }`,
		`"toggle()": function () { return this.toggle(); }`,
		"\"`opened: ${opened}`\": function () { return `${\"opened: \"}${this.opened}`; }",
		`"opened": function () { return this.opened; }`,
		`}, {"opened": "opened"});`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the program does not hold %s:\n%s", want, body)
		}
	}
	again, _ := drawWarned(t, files, value.Value{}, ui.Head{})
	if found2 := pageProgram.FindStringSubmatch(again); found2 == nil || found2[1] != found[1] {
		t.Errorf("the same page loads another program: %v", found2)
	}
	still, _ := drawWarned(t, map[string]string{"content.ui": `<content><text>no kernel</text></content>`}, value.Value{}, ui.Head{})
	if strings.Contains(still, "/_kitwork/page.") {
		t.Error("a page the kernel runs nothing on loads a program")
	}
}

// In a browser, the kernel runs a page's sources as the program wrote them: a template literal, which the kernel's
// own language refused (the text stood at the server's first paint), follows the dropdown's state once it is clicked,
// and typeof, which it refused too, is JavaScript's. CONTROL: no program given (Scripts), red: the texts stay
// "opened: false" and "".
func TestBrowserAPageRunsItsProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	files := map[string]string{"content.ui": `<content><dropdown>` +
		"<button click=\"toggle()\">b</button><text>{{ `opened: ${opened}` }}</text><text>{{ typeof opened }}</text><popover>p</popover></dropdown></content>"}
	document, _ := drawWarned(t, files, value.Value{}, ui.Head{})
	page := strings.Replace(document, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var texts = function () { return Array.prototype.map.call(document.querySelectorAll("main p"), function (p) { return p.textContent; }); };
    var before = texts();
    document.querySelector("main button").click();
    setTimeout(function () {
      fetch("/report", { method: "POST", body: JSON.stringify({ before: before, after: texts() }) });
    }, 100);
  });
});
</script></body>`, 1)
	var seen struct{ Before, After []string }
	if err := json.Unmarshal(runInBrowser(t, browser, page), &seen); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen.Before, "|") != "opened: false|boolean" || strings.Join(seen.After, "|") != "opened: true|boolean" {
		t.Errorf("before the click %q, after %q; want [opened: false boolean] then [opened: true boolean]", seen.Before, seen.After)
	}
}
