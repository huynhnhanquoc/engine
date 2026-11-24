package ui_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// drawWarned draws the route "" of a site made of files (by path under the site) with data and head, and gives the
// document and the page's warnings, one line each (file:line:col: message).
func drawWarned(t *testing.T, files map[string]string, data value.Value, head ui.Head) (string, []string) {
	t.Helper()
	site := memorySite(underSite(files))
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	document, notes, err := site.Document(page, data, head, kitDesign(t))
	if err != nil {
		t.Fatalf("a warning lets the page draw; got the error:\n%v", err)
	}
	lines := make([]string, 0, len(notes.Warnings))
	for _, w := range notes.Warnings {
		lines = append(lines, w.String())
	}
	return document, lines
}

// underSite puts each path of files under the site's folder of memorySite.
func underSite(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for path, text := range files {
		out["/app/site/"+path] = text
	}
	return out
}

// Each warning of the first engine, moved (06/11/2026): a page that writes what works but is probably not what its
// author wants draws as written, and says so in the first engine's words, at the same place, in the same order,
// each once however many times its element is drawn (a for). The cases were drawn by both engines side by side
// (warnings_cases_test.go). CONTROL: a clean page, a button with a name, a lowercase id warn of nothing.
func TestThePageWarnsAsTheFirstEngineDid(t *testing.T) {
	for _, c := range warningCases {
		t.Run(c.name, func(t *testing.T) {
			_, got := drawWarned(t, c.files, value.Value{}, ui.Head{Language: "vi", Drive: c.drive, Path: "/"})
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("warnings:\n got  %q\n want %q", got, c.want)
			}
		})
	}
}

// warningsLink finds the warnings file a page links while developing.
var warningsLink = regexp.MustCompile(`<script data-kitwork-generated="warnings" src="(/_kitwork/warnings\.[0-9a-f]{16}\.js)" defer></script>\n</head>`)

// While developing, a page with warnings links /_kitwork/warnings.<hash>.js, last in its head: one console.warn a
// warning, "cảnh báo: file:line:col: message", a JavaScript string in which nothing ends the script. The file is
// served by its own name only. CONTROL: the same page in production links nothing, its warnings still told to the
// host; a page with nothing to tell links nothing while developing.
func TestAPageLinksItsWarningsWhileDeveloping(t *testing.T) {
	files := map[string]string{"content.ui": `<content><image value="/a.png" /><box background="{{ tone }}"><text>a</text></box></content>`}
	data := object("tone", value.String("</script><b>"))
	document, warned := drawWarned(t, files, data, ui.Head{Language: "vi", Development: true})
	if len(warned) != 2 {
		t.Fatalf("want the image's warning and the value from data's, got %q", warned)
	}
	link := warningsLink.FindStringSubmatch(document)
	if link == nil {
		t.Fatalf("a page with warnings links its warnings file last in its head while developing:\n%s", document[:strings.Index(document, "<body")])
	}
	if strings.Contains(document, "console.warn") {
		t.Error("the warnings are a file the page links, never a script inside it")
	}
	file, exact := ui.FileAt(link[1])
	if file == nil || !exact || file.Role != "warnings" {
		t.Fatalf("FileAt(%s) = %v, %v: want the page's warnings file, by its own name", link[1], file, exact)
	}
	script := string(file.Body)
	if strings.Count(script, "console.warn(") != 2 || strings.Contains(script, "</script>") {
		t.Errorf("one console.warn a warning, nothing that ends a script:\n%s", script)
	}
	for _, w := range warned {
		quoted, _ := json.Marshal("cảnh báo: " + w)
		if !strings.Contains(script, "console.warn("+string(quoted)+");\n") {
			t.Errorf("the console says %s\ngot:\n%s", quoted, script)
		}
	}
	if other, _ := ui.FileAt("/_kitwork/warnings.0123456789abcdef.js"); other != nil {
		t.Error("a warnings name the engine did not write finds a file")
	}

	production, told := drawWarned(t, files, data, ui.Head{Language: "vi"})
	if strings.Contains(production, "warnings") || len(told) != 2 {
		t.Errorf("production links no warnings file, and still tells the host its %d warnings (got %d)", 2, len(told))
	}
	quiet, none := drawWarned(t, map[string]string{"content.ui": `<content><image value="/a.png" alt="" /></content>`}, value.Value{}, ui.Head{Development: true})
	if len(none) != 0 || strings.Contains(quiet, "warnings") {
		t.Errorf("a page with nothing to tell links no warnings file: %q", none)
	}
}

// A value from data a presentation attribute does not take (a colour no token of the palette nor CSS names) draws no class,
// so data never reaches the CSS, and is a warning naming the values it takes; an icon name from data that no icon
// could have is one too. No value is no class and no warning. CONTROL: a value the page spells out that the
// attribute does not take stays an error, every arm of {{ cond ? 'a' : 'b' }} with it.
func TestAValueFromDataTheAttributeDoesNotTakeIsAWarning(t *testing.T) {
	files := map[string]string{"content.ui": `<content><box background="{{ tone }}"><text>a</text></box><icon value="{{ glyph }}" /></content>`}
	document, warned := drawWarned(t, files, object("tone", value.String("brandd"), "glyph", value.String("Bad Name")), ui.Head{})
	body := document[strings.Index(document, "<body"):]
	if strings.Contains(body, "brandd") || strings.Contains(body, "Bad") {
		t.Errorf("a value the attribute does not take makes no class:\n%s", body)
	}
	// the first engine's words, its values the palette's tokens
	if len(warned) != 2 ||
		warned[0] != `content.ui:1:15: background="{{ tone }}" cho "brandd": không phải giá trị background nhận; không có class. Nhận: brand, canvas, danger, info, ink, line, panel, paper, success, warning` ||
		warned[1] != `content.ui:1:65: value="{{ glyph }}" cho "Bad Name": tên icon chỉ gồm chữ thường, số và dấu gạch nối; không có class` {
		t.Errorf("warnings: %q", warned)
	}
	if _, none := drawWarned(t, files, object("tone", value.String(""), "glyph", value.String("x")), ui.Head{}); len(none) != 0 {
		t.Errorf("no value is no class and no warning; an icon of the set warns of nothing: %q", none)
	}
	for _, written := range []string{`background="brandd"`, `background="{{ 1 > 0 ? 'brandd' : 'brand' }}"`} {
		site := memorySite(underSite(map[string]string{"content.ui": `<content><box ` + written + `><text>a</text></box></content>`}))
		page, err := site.Page(route(""))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := site.Document(page, value.Value{}, ui.Head{}, kitDesign(t)); err == nil {
			t.Errorf("%s: a value the page spells out that the attribute does not take is an error", written)
		}
	}
}

// What a warning says is drawn as written: a true-or-false word written bare reads as "true"; show="true" on a
// popover is left out; a network with nothing inside draws no place, no transition, no duration. Each page is the
// page written the house's way.
func TestAWarnedPageDrawsAsWritten(t *testing.T) {
	pairs := [][2]string{
		{`<content><link value="/" exact>Trang chủ</link></content>`, `<content><link value="/" exact="true">Trang chủ</link></content>`},
		{`<content><link value="https://example.com" new-tab>Ví dụ</link></content>`, `<content><link value="https://example.com" new-tab="true">Ví dụ</link></content>`},
		{`<content><text strikethrough>cũ</text></content>`, `<content><text strikethrough="true">cũ</text></content>`},
		{`<content><text italic>Xin</text></content>`, `<content><text italic="true">Xin</text></content>`},
		{`<content><dropdown><button click="toggle()">Mở</button><popover show="true"><text>a</text></popover></dropdown></content>`,
			`<content><dropdown><button click="toggle()">Mở</button><popover><text>a</text></popover></dropdown></content>`},
		{`<content><network place="bottom" duration="3s" transition="fade" /><text>a</text></content>`, `<content><network /><text>a</text></content>`},
		{`<content><box hover+large:background="paper"><text>a</text></box></content>`, `<content><box large+hover:background="paper"><text>a</text></box></content>`},
	}
	for _, pair := range pairs {
		warned, _ := drawWarned(t, map[string]string{"content.ui": pair[0]}, value.Value{}, ui.Head{})
		house, _ := drawWarned(t, map[string]string{"content.ui": pair[1]}, value.Value{}, ui.Head{})
		if warned != house {
			t.Errorf("%s draws as %s:\n got  %s\n want %s", pair[0], pair[1], body(warned), body(house))
		}
	}
	// CONTROL: a popover's show that would hide what it holds while open is an error, not a warning
	site := memorySite(underSite(map[string]string{"content.ui": `<content><dropdown><button click="toggle()">Mở</button><popover show="false"><text>a</text></popover></dropdown></content>`}))
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := site.Document(page, value.Value{}, ui.Head{}, kitDesign(t)); err == nil || !strings.Contains(err.Error(), `show="false" trên popover chưa được hỗ trợ`) {
		t.Errorf("popover show=\"false\": %v, want the error", err)
	}
}

// A code block too long to colour (past the highlighter's 1 MiB) is shown plain and told, at the code element, in
// the first engine's words. CONTROL: a short block warns of nothing.
func TestACodeBlockTooLongToColourIsAWarning(t *testing.T) {
	source := strings.Repeat("a+", (1<<20)/2+1)
	document, warned := drawWarned(t, map[string]string{"content.ui": `<content><code language="javascript">` + source + `</code></content>`}, value.Value{}, ui.Head{})
	want := `content.ui:1:10: <code language="javascript">: khối mã javascript dài 1048578 byte, quá giới hạn 1048576 byte; khối mã hiện thành chữ thường`
	if len(warned) != 1 || warned[0] != want {
		t.Errorf("warnings: %q, want %q", warned, want)
	}
	if !strings.Contains(document, `<code class="language-javascript">a+a+`) {
		t.Error("the block is shown plain")
	}
	if _, none := drawWarned(t, map[string]string{"content.ui": `<content><code language="javascript">a + b</code></content>`}, value.Value{}, ui.Head{}); len(none) != 0 {
		t.Errorf("a short block warns of nothing: %q", none)
	}
}

// body is a document's <body>.
func body(document string) string { return document[strings.Index(document, "<body"):] }
