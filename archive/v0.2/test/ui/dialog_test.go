package ui_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// dialogPage is a page that asks before acting (docs/components/dialog.md): a dialog drawn once in overlay.ui, a
// button of the content that opens it in one line, its confirm opening a second dialog that tells what was done.
var dialogPage = map[string]string{
	"view.ui": `<view><content /><overlay /></view>`,
	"content.ui": `<content><button click="$dialog.open({ title: 'Delete order 7?', changes: { name: 1, price: 2 }, confirm: () => $note.open({ title: 'Deleted' }) })">Delete</button>` +
		`<box height="100dvh">tall</box><box height="100dvh">taller</box></content>`,
	"overlay.ui": `<overlay><dialog as="$dialog" keydown:escape="close()"><box place="fill" background="black/50" click="close()" />` +
		`<card place="center" gap="4" padding="6"><text heading="2">{{ value.title }}</text><text>Changed: {{ Object.keys(value.changes).join(', ') }}</text>` +
		`<box flex="row"><button click="close()">No</button><button click="confirm()">Yes</button></box></card></dialog>` +
		`<dialog as="$note" keydown:escape="close()"><card place="bottom"><text heading="3">{{ value.title }}</text><button click="close()">OK</button></card></dialog></overlay>`,
}

// A dialog is the browser's <dialog>, drawn closed (16/11/2026, round 5 of the 13/11 review, dialog + catalogue,
// slice 2; its contract settled 26/10/2026): the kernel's component on it with its alias, its first heading its name
// (aria-labelledby, an id the engine gives it), the empty box that fills it hidden from a screen reader (the scrim),
// no direction or anchor class on it (it is the screen), and nothing written of what reads value, which open(x)
// gives in the browser (Object.keys(value.changes) of a closed dialog would be the TYPE error). The page loads the
// dialog's file. It was "thẻ <dialog> không có trong từ vựng". CONTROL: value evaluated on the server, red (the
// page does not draw).
func TestADialogIsDrawnClosed(t *testing.T) {
	document, warnings := drawWarned(t, dialogPage, value.Value{}, ui.Head{})
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	id := dialogTitleID.FindStringSubmatch(document)
	if id == nil {
		t.Fatalf("no aria-labelledby on the dialog:\n%s", document)
	}
	holds(t, document,
		`<dialog data-kitwork-component="dialog: $dialog" aria-labelledby="`+id[1]+`" data-kitwork-event="keydown.escape: close()">`,
		`<div class="flex-column place-fill background-black/50" aria-hidden="true" data-kitwork-event="click: close()"></div>`,
		`<h2 id="`+id[1]+`" data-kitwork-bind="text: value.title"></h2>`,
		`<p data-kitwork-bind="text: [&#39;Changed: &#39;, (Object.keys(value.changes).join(&#39;, &#39;))].join(&#39;&#39;)">Changed: </p>`,
		`<dialog data-kitwork-component="dialog: $note"`,
		`data-kitwork-event="click: $dialog.open({ title: &#39;Delete order 7?&#39;, changes: { name: 1, price: 2 }, confirm: () =&gt; $note.open({ title: &#39;Deleted&#39; }) })"`,
		`/_kitwork/components/dialog.`,
	)
}

// What a dialog refuses, each where it is written: a dialog in a for (one dialog for the list, the row given to
// open), one without an alias (nothing opens it), its show, a place outside the screen, an if or a for reading its
// value (nothing drawn to show when it opens), a confirm that is no arrow written there or one with a parameter,
// and the browser's confirm() and alert() and the dropped dialog.confirm. What it warns of: no heading, no
// keydown:escape. CONTROL: the confirm's parameters let through, red.
func TestWhatADialogRefuses(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><box for="o of [1, 2]"><dialog as="$d" keydown:escape="close()"><text heading="2">x</text></dialog></box></content>`,
			"content.ui:1:33: dialog trong for: mỗi dòng sẽ là một dialog"},
		{`<content><dialog keydown:escape="close()"><text heading="2">x</text></dialog></content>`,
			"content.ui:1:10: dialog không có as: không gì mở được nó"},
		{`<content><dialog as="$d" show="true"><text heading="2">x</text></dialog></content>`,
			"content.ui:1:26: dialog mở bằng open() và đóng bằng close(), không bằng show"},
		{`<content><dialog as="$d"><card place="below-right"><text heading="2">x</text></card></dialog></content>`,
			`content.ui:1:32: place="below-right" trong dialog: khung của dialog là cả màn hình`},
		{`<content><dialog as="$d"><text heading="2" if="value.title">x</text></dialog></content>`,
			`content.ui:1:44: if đọc value của dialog, thứ chỉ có ở trình duyệt khi open(x) chạy, nên server không vẽ gì để hiện: viết show="value.title"`},
		{`<content><dialog as="$d"><text heading="2">x</text><text for="line of value.lines">{{ line }}</text></dialog></content>`,
			"content.ui:1:58: for đọc value của dialog"},
		{`<content><dialog as="$d"><text heading="2">x</text></dialog><button click="$d.open({ title: 'x', confirm: (id) => $d.close() })">b</button></content>`,
			"content.ui:1:69: confirm nhận tham số (dialog gọi nó không đối số)"},
		{`<content><dialog as="$d"><text heading="2">x</text></dialog><button click="$d.open({ title: 'x', confirm: $d })">b</button></content>`,
			"content.ui:1:69: confirm của $d.open là việc chạy khi người dùng đồng ý, một hàm viết tại chỗ"},
		{`<content><button click="confirm('Delete?')">b</button></content>`,
			`content.ui:1:18: confirm("…") của trình duyệt không có trong Kitwork`},
		{`<content><button click="alert('Done')">b</button></content>`,
			`content.ui:1:18: alert("…") của trình duyệt không có trong Kitwork`},
		{`<content><button click="dialog.confirm({ title: 'x' })">b</button></content>`,
			"content.ui:1:18: dialog.confirm và dialog.alert đã bỏ (26/10/2026)"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
	_, warnings := drawWarned(t, map[string]string{"content.ui": `<content><dialog as="$d"><card place="center"><text>x</text></card></dialog></content>`},
		value.Value{}, ui.Head{})
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "dialog không có tiêu đề") || !strings.Contains(joined, `dialog không có keydown:escape="close()"`) {
		t.Errorf("warnings %q, want one for the heading and one for Escape", warnings)
	}
}

// In a browser a dialog asks before acting (docs/components/dialog.md): closed, it is not shown and nothing inside
// it is painted (Object.keys(value.changes) of null would throw each paint); $dialog.open({ title, changes, confirm })
// shows it modal (:modal, the page
// under it not scrolling) with the title; Escape runs the author's close(), and focus goes back to the button that
// opened it; opened again, its confirm() runs the arrow the call gave (which opens the note) and closes it. The page
// writes no error. Found by running it twelve times: the close event of a closing came after the dialog had opened
// again and closed it. CONTROLS: the kernel painting inside a closed dialog, red (an error each paint); the
// dialog's setter not calling showModal(), red; the close event read while the dialog is open, red in some runs.
func TestBrowserADialogAsksBeforeActing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	document, _ := drawWarned(t, dialogPage, value.Value{}, ui.Head{})
	page := strings.Replace(document, "<head>", `<head><script>window.errors = []; var e = console.error; console.error = function () { window.errors.push(String(arguments[0])); e.apply(console, arguments); };</script>`, 1)
	page = strings.Replace(page, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var ask = document.querySelectorAll("dialog")[0], note = document.querySelectorAll("dialog")[1];
    var trigger = document.querySelector("main button");
    var seen = [];
    var look = function (label) {
      seen.push(label + ": " + [ask.open, ask.matches(":modal"), ask.querySelector("h2").textContent, ask.querySelector("p").textContent,
        getComputedStyle(document.documentElement).overflow, note.open, note.querySelector("h3").textContent,
        document.activeElement === trigger].join(" "));
    };
    look("closed");
    trigger.focus();
    trigger.click();
    setTimeout(function () {
      look("opened");
      document.activeElement.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      setTimeout(function () {
        look("escaped");
        trigger.click();
        setTimeout(function () {
          ask.querySelectorAll("button")[1].click();
          setTimeout(function () {
            look("confirmed");
            fetch("/report", { method: "POST", body: JSON.stringify({ seen: seen, errors: window.errors }) });
          }, 100);
        }, 100);
      }, 100);
    }, 100);
  });
});
</script></body>`, 1)
	var got struct {
		Seen   []string
		Errors []string
	}
	if err := json.Unmarshal(runInBrowser(t, browser, page), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"closed: false false  Changed:  visible false  false",
		"opened: true true Delete order 7? Changed: name, price hidden false  false",
		"escaped: false false Delete order 7? Changed: name, price visible false  true",
		"confirmed: false false Delete order 7? Changed: name, price hidden true Deleted false",
	}
	if strings.Join(got.Seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("the dialog went\n%s\nwant\n%s", strings.Join(got.Seen, "\n"), strings.Join(want, "\n"))
	}
	if len(got.Errors) > 0 {
		t.Errorf("the page wrote errors: %q", got.Errors)
	}
}

// dialogTitleID finds the id a dialog's aria-labelledby names.
var dialogTitleID = regexp.MustCompile(`aria-labelledby="(kitwork-dialog-[a-z0-9]+-title)"`)
