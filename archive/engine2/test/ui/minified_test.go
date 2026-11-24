package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// Every file the engine gives a page has a twin minified at build time from its source of today
// (library/ui/kernel/minified, tools/minify): smaller, named by its own bytes, served at its own name. A component
// names the minified parts it loads. A source changed since tools/minify ran has no twin: this test is red until
// it runs again, and the page loads the readable file meanwhile.
func TestEveryScriptHasItsMinifiedTwin(t *testing.T) {
	type pair struct {
		name               string
		readable, minified *kernel.File
	}
	pairs := []pair{
		{"kernel", kernel.Kernel(), kernel.KernelMin()},
		{"drive", kernel.Drive(), kernel.DriveMin()},
		{"call", kernel.Call(), kernel.CallMin()},
		{"directives/intersect", kernel.Intersect(), kernel.IntersectMin()},
		{"scrolled", ui.Scrolled(), ui.ScrolledMin()},
	}
	for _, name := range component.Names() {
		pairs = append(pairs, pair{"components/" + name, component.File(name), component.MinFile(name)})
	}
	for _, name := range component.PartNames() {
		pairs = append(pairs, pair{"parts/" + name, component.Part(name), component.MinPart(name)})
	}
	if len(pairs) < 19 {
		t.Errorf("%d files, want the kernel, Drive, the bridge, the observer of intersect-*, the scrolled flag, every component and every part", len(pairs))
	}
	for _, p := range pairs {
		if p.minified == nil {
			t.Errorf("%s has no minified twin made from its source of today: run tools/minify (cd tools/minify && go run .)", p.name)
			continue
		}
		if len(p.minified.Body) >= len(p.readable.Body) || p.minified.Path == p.readable.Path || p.minified.Role != p.readable.Role {
			t.Errorf("%s: minified %d bytes at %s (%s), readable %d at %s (%s)", p.name, len(p.minified.Body), p.minified.Path, p.minified.Role,
				len(p.readable.Body), p.readable.Path, p.readable.Role)
		}
		for _, file := range []*kernel.File{p.readable, p.minified} {
			if served, exact := ui.FileAt(file.Path); served != file || !exact {
				t.Errorf("%s: FileAt(%s) is not the file by its own name", p.name, file.Path)
			}
		}
	}
	// the editor's component and its Markdown part name the parts they load, minified as they are
	for _, c := range []struct{ minified *kernel.File }{{component.MinFile("editor")}, {component.MinPart("editor-format")}} {
		if c.minified == nil {
			continue
		}
		body := string(c.minified.Body)
		for _, part := range component.PartNames() {
			if readable := component.Part(part).Path; strings.Contains(body, readable) {
				t.Errorf("%s names the readable %s", c.minified.Path, readable)
			}
		}
	}
	if editor := component.MinFile("editor"); editor != nil && !strings.Contains(string(editor.Body), component.MinPart("editor-paste").Path) {
		t.Errorf("the minified editor does not name the minified paste part %s", component.MinPart("editor-paste").Path)
	}
	// a source that is not the one the twin was made from has none
	if stale := kernel.Min("runtime", "kernel", string(kernel.Kernel().Body)+"\n"); stale != nil {
		t.Error("a changed source got the twin of another")
	}
}

// A page drawn for production (Head.Minify) loads the minified kernel, Drive and components; drawn while developing,
// the readable ones.
func TestAPageInProductionLoadsTheMinifiedScripts(t *testing.T) {
	files := map[string]string{"content.ui": `<content><dropdown><button click="toggle()">Mở</button><popover><text>a</text></popover></dropdown></content>`}
	production, _ := drawWarned(t, files, value.Value{}, ui.Head{Drive: true, Minify: true})
	developing, _ := drawWarned(t, files, value.Value{}, ui.Head{Drive: true})
	for _, c := range []struct{ readable, minified *kernel.File }{
		{kernel.Kernel(), kernel.KernelMin()},
		{kernel.Drive(), kernel.DriveMin()},
		{component.File("dropdown"), component.MinFile("dropdown")},
	} {
		if !strings.Contains(production, `src="`+c.minified.Path+`"`) || strings.Contains(production, c.readable.Path) {
			t.Errorf("production loads %s, not %s", c.minified.Path, c.readable.Path)
		}
		if !strings.Contains(developing, `src="`+c.readable.Path+`"`) || strings.Contains(developing, c.minified.Path) {
			t.Errorf("developing loads %s, not %s", c.readable.Path, c.minified.Path)
		}
	}
}
