package ui_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// districtPage is a form that asks for a district: an autocomplete with two options written, one from a list the
// server holds, an empty, the third chosen as the page is built.
var districtPage = map[string]string{"content.ui": `<content><form method="post"><field><label>Quận</label>` +
	`<autocomplete name="district" value="q3" required="true"><input placeholder="Chọn quận" />` +
	`<popover place="below-left"><option value="q1">Quận 1</option><option value="q3">Quận 3</option>` +
	`<option for="d of [{ id: 'bt', name: 'Bình Thạnh' }]" value="{{ d.id }}"><text>{{ d.name }}</text></option>` +
	`<empty>Không có quận nào khớp</empty></popover></autocomplete></field><button type="submit">Gửi</button></form></content>`}

// listID finds the id of an autocomplete's list.
var listID = regexp.MustCompile(`aria-controls="(kitwork-autocomplete-[a-z0-9]+)"`)

// An autocomplete is its input drawn as a combobox showing the chosen option's words, a hidden input after it that
// the form sends (the chosen value, under the autocomplete's name), its popover a listbox of options (role option,
// data-value, the chosen one aria-selected, ids by the place each is written), its empty a hidden status; its input
// is the field's control (16/11/2026, round 5 of the 13/11 review, dialog + catalogue, slice 5). It was "thẻ
// <autocomplete> không có trong từ vựng". An input with an input event of the page's is not filtered. CONTROLS: the
// hidden input left out, red; the chosen option's words left out of the input, red.
func TestAnAutocompleteIsACombobox(t *testing.T) {
	document, warnings := drawWarned(t, districtPage, value.Value{}, ui.Head{})
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	id := listID.FindStringSubmatch(document)
	if id == nil {
		t.Fatalf("no combobox:\n%s", document)
	}
	holds(t, document,
		`<label for="kitwork-field-1">Quận</label><div class="flex-column place-anchor" data-kitwork-component="autocomplete" data-kitwork-scope="filter: true">`,
		`<input role="combobox" aria-autocomplete="list" aria-controls="`+id[1]+`" autocomplete="off" value="Quận 3" required placeholder="Chọn quận" id="kitwork-field-1" aria-expanded="false" data-kitwork-bind="aria-expanded: opened"><input type="hidden" name="district" value="q3">`,
		`<div class="popover flex-column place-below-left" id="`+id[1]+`" role="listbox" hidden data-kitwork-bind="show: opened">`,
		`aria-selected="false" data-value="q1">Quận 1</div>`, `aria-selected="true" data-value="q3">Quận 3</div>`,
		`aria-selected="false" data-value="bt"><span>Bình Thạnh</span></div>`,
		`<div class="flex-column" hidden role="status">Không có quận nào khớp</div>`,
		`/_kitwork/components/autocomplete.`,
	)
	searched, _ := drawWarned(t, map[string]string{"content.ui": `<content><autocomplete name="x"><input input="close()" />` +
		`<popover><option value="a">A</option></popover></autocomplete></content>`}, value.Value{}, ui.Head{})
	holds(t, searched, `data-kitwork-scope="filter: false"`)
}

// What an autocomplete refuses, each where it is written (docs/components/autocomplete.md, "Compiler kiểm"), and what
// it warns of; options in a list or a group are not yet. CONTROL: name on the inner input let through, red.
func TestWhatAnAutocompleteRefuses(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><autocomplete><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:10: autocomplete cần một <input> (chỗ gõ) và một <popover>"},
		{`<content><autocomplete><input /><input /><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:33: autocomplete có đúng một <input>"},
		{`<content><autocomplete><input /><popover><option value="a">A</option></popover><popover><option value="b">B</option></popover></autocomplete></content>`,
			"content.ui:1:80: autocomplete có đúng một <popover>"},
		{`<content><autocomplete><input name="x" /><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:31: name đặt trên <autocomplete>, không trên input bên trong"},
		{`<content><autocomplete><input required="true" /><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:31: required đặt trên <autocomplete>"},
		{`<content><autocomplete><input /><popover><option value="a">A</option><option value="a">B</option></popover></autocomplete></content>`,
			`content.ui:1:78: hai option cùng value="a"`},
		{`<content><autocomplete><input /><popover><option value="a">A <link value="/a">x</link></option></popover></autocomplete></content>`,
			"content.ui:1:62: <link> trong option"},
		{`<content><autocomplete><input /><popover><option value="a">A</option><box>x</box></popover></autocomplete></content>`,
			"content.ui:1:70: <box> trong popover của autocomplete"},
		{`<content><autocomplete><input /><popover><list><option value="a">A</option></list></popover></autocomplete></content>`,
			"content.ui:1:42: <list> trong popover của autocomplete: option gom trong list hay group chưa có"},
		{`<content><autocomplete><input /><popover><empty>a</empty><empty>b</empty></popover></autocomplete></content>`,
			"content.ui:1:58: popover của autocomplete có một <empty>"},
		{`<content><autocomplete as="$x"><input /><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:24: autocomplete không có as"},
		{`<content><autocomplete><label>x</label><input /><popover><option value="a">A</option></popover></autocomplete></content>`,
			"content.ui:1:24: <label> trong autocomplete"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
	_, warnings := drawWarned(t, map[string]string{"content.ui": `<content><autocomplete><input /><popover><option>Quận 1</option></popover></autocomplete></content>`},
		value.Value{}, ui.Head{})
	if !strings.Contains(strings.Join(warnings, "\n"), "option không có value: chữ của nó là value") {
		t.Errorf("warnings %q, want one for an option without a value", warnings)
	}
}

// In a browser an autocomplete is chosen from as typed (docs/components/autocomplete.md): typing "binh" opens the
// list with Bình Thạnh only (no accents, no case) and its typed words undo the choice the page was built with; Down
// lights it, Enter chooses it (the hidden input, the words, aria-selected, change with $event.value and $event.label,
// the form's data, the list whole again behind the words chosen); "xyz" matches nothing, so the empty shows and the field, typed over, is invalid; Escape closes,
// Escape again clears it and the choice. CONTROLS: the filter keeping every option, red; Enter choosing nothing, red.
func TestBrowserAnAutocompleteChoosesAsTyped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	document, _ := drawWarned(t, districtPage, value.Value{}, ui.Head{})
	page := strings.Replace(document, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var input = document.querySelector("[role=combobox]"), hidden = input.nextElementSibling;
    var list = document.querySelector("[role=listbox]"), empty = list.querySelector("[role=status]");
    var form = document.querySelector("form"), changes = [], seen = [];
    document.addEventListener("change", function (e) { if (e.target === input.parentNode) changes.push(e.value + "/" + e.label); });
    var look = function (label) {
      var visible = Array.prototype.filter.call(list.querySelectorAll("[role=option]"), function (o) { return !o.hidden; })
        .map(function (o) { return o.textContent; }).join(",");
      seen.push(label + ": " + [list.hidden, visible, empty.hidden, input.getAttribute("aria-expanded"), input.value, hidden.value,
        input.checkValidity(), new FormData(form).get("district")].join(" | "));
    };
    var key = function (k) { input.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true })); };
    var type = function (text) { input.value = text; input.dispatchEvent(new Event("input", { bubbles: true })); };
    look("built");
    input.focus();
    type("binh");
    setTimeout(function () {
      look("typed");
      key("ArrowDown");
      setTimeout(function () {
        var lit = document.getElementById(input.getAttribute("aria-activedescendant") || "");
        seen.push("lit: " + !!(lit && lit.hasAttribute("data-active")));
        key("Enter");
        setTimeout(function () {
          look("chosen");
          type("xyz");
          setTimeout(function () {
            look("nothing");
            key("Escape");
            setTimeout(function () {
              look("closed");
              key("Escape");
              setTimeout(function () {
                look("cleared");
                fetch("/report", { method: "POST", body: JSON.stringify({ seen: seen, changes: changes }) });
              }, 50);
            }, 50);
          }, 50);
        }, 50);
      }, 50);
    }, 50);
  });
});
</script></body>`, 1)
	var got struct{ Seen, Changes []string }
	if err := json.Unmarshal(runInBrowser(t, browser, page), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"built: true | Quận 1,Quận 3,Bình Thạnh | true | false | Quận 3 | q3 | true | q3",
		"typed: false | Bình Thạnh | true | true | binh |  | false | ",
		"lit: true",
		"chosen: true | Quận 1,Quận 3,Bình Thạnh | true | false | Bình Thạnh | bt | true | bt",
		"nothing: false |  | false | true | xyz |  | false | ",
		"closed: true |  | false | false | xyz |  | false | ",
		"cleared: true |  | false | false |  |  | false | ",
	}
	if strings.Join(got.Seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("the autocomplete went\n%s\nwant\n%s", strings.Join(got.Seen, "\n"), strings.Join(want, "\n"))
	}
	if want := "/|bt/Bình Thạnh|/"; strings.Join(got.Changes, "|") != want {
		t.Errorf("change sent %q, want %s", got.Changes, want)
	}
}
