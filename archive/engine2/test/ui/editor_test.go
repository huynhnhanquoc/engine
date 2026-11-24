package ui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui"
)

// editorPage is a form holding a field with a label, an editor and a hint and an error, the editor drawn as
// kitwork.io draws its own: a toolbar with groups, a switch of format, a format, a dropdown of smaller headings
// called through the alias, an alignment, the link, undo; the link's popover; the writing area.
const editorPage = `<content><form method="post"><field><label>Post</label><editor as="$body">` +
	`<box alt="Formatting"><group alt="Write in"><button click="format = 'rich'">R</button><button click="format = 'markdown'">M</button></group>` +
	`<group alt="Text"><button click="toggle('bold')">B</button><dropdown><button click="toggle()">more</button>` +
	`<popover><button click="$body.toggle('heading', 4)">H4</button></popover></dropdown>` +
	`<button click="align('center')">C</button><button click="link()">L</button><button click="undo()">U</button></group></box>` +
	`<popover><input value="{{ address }}" alt="Link address" /><button click="link(address)">Link</button>` +
	`<button click="close()">Cancel</button><error>Bad address.</error></popover>` +
	`<textarea name="body" value="{{ post }}" placeholder="Write…" rows="6" required="true" alt="Body" padding="4" /></editor>` +
	`<hint>{{ $body.words }} words</hint><error>Too long.</error></field></form></content>`

// The editor's host carries its first state; each toolbar is a toolbar that controls the area, each group a
// group, its buttons one roving Tab stop; a switch of format is pressed as the format it writes, a format and an
// alignment are not pressed yet, bold says its keys; the dropdown's trigger is a stop of the toolbar controlling the
// dropdown's popover, whose button names its format and is no stop; link() opens the popover, disabled until there
// is a selection; undo is disabled until there is something to undo.
func TestTheEditorWiresItsToolbar(t *testing.T) {
	_, body := render(t, map[string]string{"/app/site/content.ui": editorPage}, object("post", value.String("<p>Hello <b>world</b></p><script>x</script>")))
	holds(t, body,
		`<div class="flex-column place-anchor editor" data-kitwork-component="editor: $body" data-kitwork-scope="empty: false; words: 2; characters: 11; unsaved: false; opened: false; address: &#39;&#39;; format: &#39;rich&#39;; dropped: []">`,
		`<div class="flex-column editor-toolbar" aria-label="Formatting" role="toolbar" aria-controls="kitwork-editor-1-area">`,
		`<div class="flex-column editor-group" aria-label="Write in" role="group">`,
		`type="button" tabindex="0" data-kitwork-command="format" data-kitwork-format-value="rich" aria-pressed="true" data-kitwork-bind="aria-pressed: format == &#39;rich&#39;" data-kitwork-event="click: format = &#39;rich&#39;">R</button>`,
		`type="button" tabindex="-1" data-kitwork-command="format" data-kitwork-format-value="markdown" aria-pressed="false"`,
		`type="button" tabindex="-1" data-kitwork-format="bold" aria-pressed="false" aria-keyshortcuts="Control+B" data-kitwork-event="click: toggle(&#39;bold&#39;)">B</button>`,
		`type="button" tabindex="-1" aria-controls="kitwork-editor-1-menu-1" aria-expanded="false" data-kitwork-bind="aria-expanded: opened" data-kitwork-event="click: toggle()">more</button>`,
		`<div class="popover flex-column" id="kitwork-editor-1-menu-1" hidden data-kitwork-bind="show: opened">`,
		`disabled:background-ink/4" type="button" data-kitwork-format="heading-4" aria-pressed="false" data-kitwork-event="click: $body.toggle(&#39;heading&#39;, 4)">H4</button>`,
		`type="button" tabindex="-1" data-kitwork-command="align-center" aria-pressed="false" data-kitwork-event="click: align(&#39;center&#39;)">C</button>`,
		`type="button" tabindex="-1" data-kitwork-command="link" aria-keyshortcuts="Control+K" aria-controls="kitwork-editor-1-popover" aria-haspopup="dialog" aria-expanded="false" aria-disabled="true" data-kitwork-bind="aria-expanded: opened" data-kitwork-event="click: link()">L</button>`,
		`type="button" tabindex="-1" data-kitwork-command="undo" aria-keyshortcuts="Control+Z" aria-disabled="true" data-kitwork-event="click: undo()">U</button>`)
	if regexp.MustCompile(`tabindex="[^"]*" data-kitwork-format="heading-4"`).MatchString(body) {
		t.Errorf("a button of a toolbar's dropdown is a Tab stop of the toolbar:\n%s", body)
	}
}

// The link's popover is a dialog named as its address input; the input is a web address typed (no type="url",
// which would stop the form on /page), bound two-way to address, and is never the field's control; the error is
// hidden until link(address) refuses an address. The writing area is a textbox holding the value cleaned, named by
// the field's label and described by its hint and its error; the field the form posts holds the same HTML escaped,
// with the field's id, the textarea's name and words.
func TestTheEditorWiresItsPopoverAndArea(t *testing.T) {
	_, body := render(t, map[string]string{"/app/site/content.ui": editorPage}, object("post", value.String("<p>Hello <b>world</b></p><script>x</script>")))
	holds(t, body,
		`<div class="popover flex-column editor-popover" id="kitwork-editor-1-popover" role="dialog" aria-label="Link address" hidden data-kitwork-bind="show: opened">`,
		`<input class="editor-address" value="" aria-label="Link address" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" data-kitwork-bind="value: address">`,
		`type="button" data-kitwork-command="link" data-kitwork-event="click: link(address)">Link</button>`,
		`type="button" data-kitwork-command="close" data-kitwork-event="click: close()">Cancel</button>`,
		`<p class="error editor-address-error" id="kitwork-editor-1-address-error" hidden>Bad address.</p>`,
		`<label for="kitwork-field-1" id="kitwork-field-1-label">Post</label>`,
		`<div class="richtext editor-area padding-4 editor-rows-6" id="kitwork-editor-1-area" role="textbox" aria-multiline="true" aria-labelledby="kitwork-field-1-label" aria-describedby="kitwork-field-1-hint kitwork-field-1-error" aria-placeholder="Write…" aria-required="true" aria-readonly="true" translate="no" spellcheck="true" data-kitwork-name="body"><p>Hello <strong>world</strong></p></div>`,
		`<textarea class="editor-field padding-4" id="kitwork-field-1" name="body" required rows="6" placeholder="Write…" aria-describedby="kitwork-field-1-hint kitwork-field-1-error">&lt;p&gt;Hello &lt;strong&gt;world&lt;/strong&gt;&lt;/p&gt;</textarea>`,
		`<p class="hint" id="kitwork-field-1-hint" data-kitwork-bind="text: [($body.words), &#39; words&#39;].join(&#39;&#39;)">2 words</p>`,
		`<p class="error" id="kitwork-field-1-error">Too long.</p>`)
	if strings.Contains(body, "<script>x") || strings.Contains(body, "&lt;script") {
		t.Errorf("the editor writes what the cleaner drops:\n%s", body)
	}
	if regexp.MustCompile(`<input class="editor-address"[^>]* id=`).MatchString(body) {
		t.Errorf("the address input took the field's id:\n%s", body)
	}
}

// A field ties its control to its label by an id and describes it by its hint, the page's fields counted in order;
// its error is given an id only when the field holds an editor.
func TestAFieldWiresItsControl(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><field><label>Name</label><input name="name" /><hint>Your name.</hint><error>e</error></field>` +
			`<field><label>Bio</label><textarea name="bio" /><hint>h</hint></field>` +
			`<field><label>Size</label><select name="size"><option value="s">S</option></select></field></content>`,
	}, value.Value{})
	holds(t, body,
		`<label for="kitwork-field-1">Name</label>`,
		`<input name="name" id="kitwork-field-1" aria-describedby="kitwork-field-1-hint">`,
		`<p class="hint" id="kitwork-field-1-hint">Your name.</p>`,
		`<p class="error">e</p>`,
		`<label for="kitwork-field-2">Bio</label>`,
		`<textarea name="bio" id="kitwork-field-2"></textarea>`,
		`<select class="flex-column" name="size" id="kitwork-field-3">`)
}

// The editor starts from its value cleaned (empty, words, characters), in the format its host writes, and on a
// page that answers a save (a POST) what it holds is not saved yet.
func TestTheEditorStartsFromItsValue(t *testing.T) {
	page := strings.Replace(editorPage, `<editor as="$body">`, `<editor as="$body" format="markdown">`, 1)
	page = strings.Replace(page, `</form>`, `</form><text>{{ $body.format }}</text>`, 1)
	_, body := renderHead(t, map[string]string{"/app/site/content.ui": page}, object("post", value.String("")), ui.Head{Method: "POST"})
	holds(t, body,
		`data-kitwork-scope="empty: true; words: 0; characters: 0; unsaved: true; opened: false; address: &#39;&#39;; format: &#39;markdown&#39;; dropped: []"`,
		`data-kitwork-format-value="rich" aria-pressed="false"`,
		`data-kitwork-format-value="markdown" aria-pressed="true"`,
		`<p class="hint" id="kitwork-field-1-hint" data-kitwork-bind="text: [($body.words), &#39; words&#39;].join(&#39;&#39;)">0 words</p>`,
		`<p data-kitwork-bind="text: $body.format">markdown</p>`)
}

// The editor's file names each part it loads by the part's name of today, and so does a part that loads another;
// only that name is served, never an older hash or the bare name.
func TestTheEditorsPartsAreServedByTheirNames(t *testing.T) {
	editor := component.File("editor")
	if editor == nil {
		t.Fatal("no editor file")
	}
	hashed := regexp.MustCompile(`"/_kitwork/parts/(editor-[a-z]+)\.[0-9a-f]{16}\.js"`)
	named := map[string]bool{}
	for _, m := range hashed.FindAllStringSubmatch(string(editor.Body), -1) {
		named[m[1]] = true
	}
	for _, part := range []string{"editor-paste", "editor-place", "editor-format"} {
		if !named[part] {
			t.Errorf("the editor's file does not name %s by its hashed name", part)
		}
	}
	if strings.Contains(string(editor.Body), `"/_kitwork/parts/editor-paste.js"`) {
		t.Errorf("the editor's file names a part without its hash")
	}
	format := component.Part("editor-format")
	markdown := component.Part("editor-markdown")
	if format == nil || markdown == nil || !strings.Contains(string(format.Body), `"`+markdown.Path+`"`) {
		t.Fatalf("editor-format does not name editor-markdown by its hashed name")
	}
	if file, exact := ui.FileAt(markdown.Path); file != markdown || !exact {
		t.Errorf("FileAt(%s) = %v, %v; want the part, exact", markdown.Path, file, exact)
	}
	for _, path := range []string{"/_kitwork/parts/editor-markdown.0123456789abcdef.js", "/_kitwork/parts/editor-markdown.js", "/_kitwork/parts/nothing.0123456789abcdef.js"} {
		if file, _ := ui.FileAt(path); file != nil {
			t.Errorf("FileAt(%s) serves a part; only its name of today is", path)
		}
	}
	if component.Part("../editor") != nil || component.Part("Editor-format") != nil {
		t.Errorf("a part's name reaches outside parts/")
	}
}

// Where an editor and what it holds are written wrong, the error says so at its place.
func TestTheEditorSaysWhereItIsWrittenWrong(t *testing.T) {
	form := func(inside string) string {
		return `<content><form method="post"><field><label>Post</label>` + inside + `</field></form></content>`
	}
	area := `<textarea name="body" />`
	errorsAre(t, []struct{ content, error string }{
		{`<content><editor>` + area + `</editor></content>`, `content.ui:1:10: editor phải nằm trong <form method="post">`},
		{`<content><form><editor>` + area + `</editor></form></content>`, `content.ui:1:16: editor phải nằm trong <form method="post">: form GET`},
		{`<content><form method="post"><box for="x of [1]"><editor alt="a">` + area + `</editor></box></form></content>`, `content.ui:1:35: editor không lặp bằng for`},
		{form(`<editor>` + area + `<editor>` + area + `</editor></editor>`), `content.ui:1:88: editor không lồng trong editor (editor ngoài ở content.ui:1:56)`},
		{form(`<editor><box alt="F"><button click="rule()">r</button></box><dropdown><button click="toggle()">t</button><popover><editor><text>x</text></editor></popover></dropdown>` + area + `</editor>`), `content.ui:1:170: editor không lồng trong editor (editor ngoài ở content.ui:1:56)`},
		{form(`<text><editor>` + area + `</editor></text>`), `content.ui:1:62: editor là một khối: không đặt trong <text>, <link>, <button>, <label> (nó đang nằm trong <text> ở content.ui:1:56)`},
		{form(`<editor />`), `content.ui:1:56: editor rỗng`},
		{form(`<editor><text>x</text></editor>`), `content.ui:1:56: editor cần một vùng viết bên trong`},
		{form(`<editor>` + area + area + `</editor>`), `content.ui:1:88: editor có một vùng viết: <textarea> ở content.ui:1:64 đã là của nó`},
		{form(`<editor><textarea /></editor>`), `content.ui:1:64: <textarea> của editor cần name`},
		{form(`<editor><textarea name="b" value="hi" /></editor>`), `content.ui:1:83: value của vùng viết là bài đã lưu`},
		{form(`<editor><textarea name="b" rows="0" /></editor>`), `content.ui:1:83: rows là số dòng thấp nhất`},
		{form(`<editor><textarea name="b" show="true" /></editor>`), `content.ui:1:83: vùng viết của editor luôn có mặt: bỏ show`},
		{form(`<editor><textarea name="b" maxlength="9" /></editor>`), `content.ui:1:83: maxlength không có trên vùng viết`},
		{form(`<editor format="md">` + area + `</editor>`), `content.ui:1:64: format="md": md là chữ viết tắt`},
		{form(`<editor placeholder="x">` + area + `</editor>`), `content.ui:1:64: placeholder không có trên <editor>`},
		{form(`<editor><box><button click="toggle('bold')">b</button></box>` + area + `</editor>`), `content.ui:1:64: thanh công cụ của editor cần alt`},
		{form(`<editor><button click="toggle('bold')">b</button>` + area + `</editor>`), `content.ui:1:72: nút của editor (click="toggle('bold')") nằm chung khung với vùng viết`},
		{form(`<editor><box alt="F"><button click="toggle('h1')">b</button></box>` + area + `</editor>`), `content.ui:1:85: toggle('h1'): heading 1 là tiêu đề của trang`},
		{form(`<editor><box alt="F"><button click="toggle('heading', 1)">b</button></box>` + area + `</editor>`), `content.ui:1:85: toggle('heading', 1): heading 1 là tiêu đề của trang`},
		{form(`<editor><box alt="F"><button click="toggle('strong')">b</button></box>` + area + `</editor>`), `content.ui:1:85: toggle('strong'): định dạng này tên là bold`},
		{form(`<editor><box alt="F"><button click="align('justify')">b</button></box>` + area + `</editor>`), `content.ui:1:85: align('justify'): editor chưa có căn đều`},
		{form(`<editor><box alt="F"><button click="format = 'md'">b</button></box>` + area + `</editor>`), `content.ui:1:85: format = 'md': md là chữ viết tắt`},
		{form(`<editor><box alt="F"><button click="link(address)">b</button></box>` + area + `</editor>`), `content.ui:1:85: link(address) gắn chữ trong ô địa chỉ`},
		{form(`<editor><box alt="F"><button click="link()">b</button></box>` + area + `</editor>`), `content.ui:1:85: link() mở ô địa chỉ mà editor chưa có`},
		{form(`<editor><box alt="F"><button click="rule()">b</button></box><popover><button click="rule()">r</button><input value="{{ address }}" alt="a" /></popover>` + area + `</editor>`), `content.ui:1:133: rule() là nút của thanh công cụ`},
		{form(`<editor><box alt="F"><button click="link()">b</button></box><popover><input value="{{ address }}" alt="a" type="url" /></popover>` + area + `</editor>`), `content.ui:1:162: ô địa chỉ không viết type`},
		{form(`<editor><box alt="F"><button click="link()">b</button></box><popover><text>x</text></popover>` + area + `</editor>`), `content.ui:1:116: popover của editor cần đúng một <input>`},
		{form(`<editor as="$b"><box alt="F"><dropdown><button click="toggle()">t</button><popover><button click="rule()">r</button></popover></dropdown></box>` + area + `</editor>`), `content.ui:1:147: rule() trong <dropdown>: tên trần bên trong <dropdown> là của nó`},
		{form(`<editor as="$b"><box alt="F"><dropdown><button click="$b.rule()">t</button><popover>p</popover></dropdown></box>` + area + `</editor>`), `content.ui:1:103: $b.rule() ở ngoài <popover> của <dropdown>`},
		{form(`<editor as="$b"><box alt="F"><button click="rule()">r</button><group alt="g"><group alt="h"><button click="undo()">u</button></group></group></box>` + area + `</editor>`), `content.ui:1:133: <group> không lồng trong <group> (nhóm ngoài ở content.ui:1:118)`},
		{`<content><form method="post"><editor>` + area + `</editor></form></content>`, `content.ui:1:38: vùng viết chưa có tên đọc được`},
		{`<content><form method="post"><field><label if="false">x</label><editor>` + area + `</editor></field></form></content>`, `content.ui:1:44: if này có thể bỏ <label>`},
		{form(`<editor input="x()">` + area + `</editor>`), `content.ui:1:64: input trên <editor> chưa có ở bản này`},
		{form(`<editor><select value="{{ format }}" alt="W"><option value="markdown">M</option></select>` + area + `</editor>`), `content.ui:1:64: <select value="{{ format }}"> thiếu <option value="rich">`},
		{form(`<editor><select value="{{ format }}" alt="W" name="f"><option value="rich">R</option></select>` + area + `</editor>`), `content.ui:1:101: <select value="{{ format }}"> không có name`},
	})
}

// A <select value="{{ format }}"> in an editor switches its format: the component's command, bound to format both
// ways; inside the field around the editor it is not the field's control, the writing area is.
func TestTheEditorsSelectOfFormat(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><form method="post"><field><label>Post</label><editor><box alt="F"><button click="toggle('bold')">B</button></box>` +
			`<select value="{{ format }}" alt="Write in"><option value="rich">Rich</option><option value="markdown">Markdown</option></select>` +
			`<textarea name="body" /></editor></field></form></content>`,
	}, value.Value{})
	holds(t, body, `<select class="flex-column" aria-label="Write in" data-kitwork-command="format" data-kitwork-bind="value: format">`)
	if regexp.MustCompile(`<select[^>]* id=`).MatchString(body) {
		t.Errorf("the editor's select of format took the field's id:\n%s", body)
	}
}

// Words around a {{ }} that follows a component's state are bound as the page shows them: each run of white space
// one space, none at the ends; a hint is the field's only when it is the field's own child.
func TestJoinedTextIsTheWordsAPageShows(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": "<content><dropdown><text>\n    {{ opened }}\n      and   more\n  </text><popover>p</popover></dropdown>" +
			`<field><label>X</label><input name="x" /><box><hint>deep</hint></box></field></content>`,
	}, value.Value{})
	holds(t, body, `data-kitwork-bind="text: [(opened), &#39; and more&#39;].join(&#39;&#39;)"`, `<input name="x" id="kitwork-field-1">`, `<p class="hint">deep</p>`)
}
