package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// renderError is the error of drawing the route "" of a site made of files, nil when it draws.
func renderError(t *testing.T, files map[string]string) error {
	t.Helper()
	site := memorySite(files)
	page, err := site.Page(route(""))
	if err != nil {
		return err
	}
	_, _, err = site.Document(page, value.Value{}, ui.Head{}, kitDesign(t))
	return err
}

// The header of every page of kitwork.io: an appearance and a dropdown, written in the grouped form the kernel
// reads (KITWORK_UI mục 11). The host says its component and alias; an event is a pair of data-kitwork-event,
// its modifiers after a dot; what follows the component's state is a pair of data-kitwork-bind (the value the
// server painted first beside it) or, for a class, a pair of data-kitwork-class for each value it can be, whose
// rules the page's stylesheet holds. The page loads the kernel, then each component's file.
func TestTheHeaderComponentsAreWrittenForTheKernel(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/content.ui": `<content>` +
			`<appearance click="toggle()" padding="2"><icon value="{{ dark ? 'sun' : 'moon' }}" alt="Switch" /></appearance>` +
			`<dropdown as="$menu" click-outside="close()" keydown:escape="close()">` +
			`<button click="toggle()" expanded="{{ opened }}"><icon value="menu-2" alt="Menu" /></button>` +
			`<popover place="below-right"><text>m</text></popover></dropdown></content>`,
	}, value.Value{})
	for _, want := range []string{
		`<button class="flex-row padding-2" data-kitwork-component="appearance" type="button" data-kitwork-event="click: toggle()">`,
		`<i class="icon-moon" role="img" aria-label="Switch" data-kitwork-class="icon-moon: (dark ? &#39;sun&#39; : &#39;moon&#39;) == &#39;moon&#39;; icon-sun: (dark ? &#39;sun&#39; : &#39;moon&#39;) == &#39;sun&#39;"></i>`,
		`<div class="flex-column place-anchor" data-kitwork-component="dropdown: $menu" data-kitwork-event="click-outside: close(); keydown.escape: close()">`,
		`<button class="flex-row background-ink/6 hover:background-ink/10 pressed:background-ink/14 disabled:background-ink/4" type="button" aria-expanded="false" data-kitwork-bind="aria-expanded: opened" data-kitwork-event="click: toggle()">`,
		`<div class="popover flex-column place-below-right" hidden data-kitwork-bind="show: opened"><p>m</p></div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold\n%s", body, want)
		}
	}
	if !strings.Contains(document, ".icon-sun{") {
		t.Errorf("the page's stylesheet lacks the mask of the icon the kernel turns on")
	}
	scripts := []string{kernel.Kernel().Path, component.File("appearance").Path, component.File("dropdown").Path}
	at := 0
	for _, path := range scripts {
		tag := `<script data-kitwork-generated="`
		i := strings.Index(document[at:], `src="`+path+`" defer></script>`)
		if i < 0 || !strings.Contains(document, tag) {
			t.Fatalf("the page does not load %s after what comes before it:\n%s", path, document[:strings.Index(document, "<body")])
		}
		at += i
	}
	if i, head := strings.Index(document, kernel.Kernel().Path), strings.Index(document, "</head>"); i > head {
		t.Errorf("the scripts are not in the head")
	}
}

// The prepaint and the kernel decide the look by one rule: the kernel's kitwork.appearance reads the look shown
// from the class dark on <html>, so the prepaint writes it, and both read the choice kept under "appearance", then
// under "theme". Without it a dark page's first toggle stays dark (the kernel thinks it light).
func TestThePrepaintWritesWhatTheKernelReadsTheLookFrom(t *testing.T) {
	document, _ := render(t, map[string]string{"/app/site/content.ui": `<content>c</content>`}, value.Value{})
	head := document[:strings.Index(document, "</head>")]
	kernelFile := string(kernel.Kernel().Body)
	for _, pair := range [][2]string{
		{`classList.toggle("dark",m==="dark")`, `classList.contains("dark")`},
		{`getItem("appearance"))||v(s.getItem("theme"))`, `var appearanceKeys = ["appearance", "theme"];`},
	} {
		if !strings.Contains(head, pair[0]) || !strings.Contains(kernelFile, pair[1]) {
			t.Errorf("the prepaint (%s) and the kernel (%s) do not decide the look by one rule", pair[0], pair[1])
		}
	}
}

// An event written outside every component that reads the page's own names waits for the page to run in the
// browser: no pair, and a page with nothing the kernel reads loads no kernel. So does one that reads an alias no tag
// of the page sets. (Until 06/11/2026 also one that read a component whose markup had not moved; the editor was the
// last to move.) An event may write (opened = false), unlike a {{ }}.
func TestWhatTheBrowserDoesNotRunYetLoadsNothing(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><button click="save()">s</button><button click="$nobody.open()">n</button></content>`,
	}, value.Value{})
	if strings.Contains(body, "data-kitwork-") || strings.Contains(document, "<script data-kitwork-generated=\"runtime\"") {
		t.Errorf("a page the browser runs nothing of says it does:\n%s", body)
	}
	_, body = render(t, map[string]string{
		"/app/site/content.ui": `<content><dropdown><button click="opened = false">b</button><popover>p</popover></dropdown></content>`,
	}, value.Value{})
	if want := `data-kitwork-event="click: opened = false"`; !strings.Contains(body, want) {
		t.Errorf("body\n%s\nwant it to hold %s", body, want)
	}
	_, body = render(t, map[string]string{
		"/app/site/content.ui": `<content><dropdown><box background="{{ opened ? 'brand' : 'paper' }}">b</box><popover>p</popover></dropdown></content>`,
	}, value.Value{})
	if want := `<div class="flex-column background-paper" data-kitwork-class="background-brand: (opened ? &#39;brand&#39; : &#39;paper&#39;) == &#39;brand&#39;; background-paper: (opened ? &#39;brand&#39; : &#39;paper&#39;) == &#39;paper&#39;">`; !strings.Contains(body, want) {
		t.Errorf("body\n%s\nwant it to hold %s", body, want)
	}
}

// What the kernel would refuse, or could not run, is an error where it is written: a modifier it does not have,
// an event inside a component calling the page's names, an expression reading a component's names and the page's,
// a class from the state that does not spell out its values, a popover no component opens, an alias that is no
// name, a button's type HTML does not have.
func TestWhatTheKernelWouldRefuseIsAnError(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><dropdown><button click:debounse(300)="close()">b</button><popover>p</popover></dropdown></content>`,
			"content.ui:1:28: click:debounse(300): không có phần đi kèm debounse(300)"},
		{`<content><dropdown><button click="save()">b</button><popover>p</popover></dropdown></content>`,
			"content.ui:1:28: bên trong dropdown sự kiện click chỉ gọi tên của component (close, open, opened, toggle)"},
		{`<content><dropdown><text show="opened && user">b</text><popover>p</popover></dropdown></content>`,
			"content.ui:1:26: bên trong dropdown biểu thức opened && user đọc cả tên của component lẫn user của trang"},
		{`<content><dropdown><box background="{{ opened && 'brand' }}">b</box><popover>p</popover></dropdown></content>`,
			"content.ui:1:25: bên trong dropdown background=\"{{ opened && 'brand' }}\" đổi theo trạng thái, nên phải viết ra mọi giá trị"},
		{`<content><popover>p</popover></content>`, "content.ui:1:10: popover hiện khi component chứa nó mở"},
		{`<content><dropdown as="menu"><popover>p</popover></dropdown></content>`, "content.ui:1:20: as của <dropdown> là $ và một tên"},
		{`<content><button type="sumbit">b</button></content>`, "content.ui:1:18: type của button là button, submit hoặc reset"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
}

// Each file is served by its name under /_kitwork/, the hash of its bytes in it; an older hash of the same file
// still gets it, not to be kept; a name that is no file of the engine gets nothing. A component's file registers
// its blueprint with the kernel, in a function of its own.
func TestTheBrowsersFilesAreServedByTheirNames(t *testing.T) {
	for _, file := range []*kernel.File{kernel.Kernel(), kernel.Drive(), ui.Scrolled(), component.File("dropdown")} {
		got, exact := ui.FileAt(file.Path)
		if got != file || !exact {
			t.Errorf("%s: %v %v, want the file, exact", file.Path, got, exact)
		}
		old := strings.TrimSuffix(file.Path, ".js")
		old = old[:strings.LastIndexByte(old, '.')+1] + "0123456789abcdef.js"
		if got, exact := ui.FileAt(old); got != file || exact {
			t.Errorf("%s: %v %v, want the file, not exact", old, got, exact)
		}
	}
	for _, path := range []string{"/_kitwork/components/nothing.0123456789abcdef.js", "/_kitwork/components/../kernel.0123456789abcdef.js",
		"/_kitwork/kernel.js", "/_kitwork/kernel.0123456789ABCDEF.js", "/_kitwork/fonts/x.0123456789abcdef.js"} {
		if got, _ := ui.FileAt(path); got != nil {
			t.Errorf("%s: %s, want nothing", path, got.Path)
		}
	}
	dropdown := string(component.File("dropdown").Body)
	if !strings.HasPrefix(dropdown, ";(function () {\n") || !strings.Contains(dropdown, `window.kitwork.component("dropdown", dropdownDef);`) {
		t.Errorf("the dropdown's file:\n%s", dropdown)
	}
	if !strings.Contains(string(kernel.Kernel().Body), "kitwork.start") {
		t.Errorf("the kernel's file does not start the kernel")
	}
}
