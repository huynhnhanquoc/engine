// Command minify writes engine2's JavaScript minified, for a site in production to load (library/ui/kernel/minified.go,
// decided 07/11/2026). It is a module of its own so that engine2 takes no library beyond Go's own: the minifier,
// tdewolff/minify, is the one the first engine ran on every file as it served it, with the same settings.
//
// It builds every file the way engine2 serves it, the kernel, Drive, the bridge (call.js), the observer of intersect-*
// (intersect.js), the scrolled flag, each component and each part a component loads, with the minifier at hand, so a
// component names the minified parts it loads, and writes each in library/ui/kernel/minified/ beside the SHA-256 of
// the source it was made from (sources.txt). Run it after changing a script of the kernel or a component; the test
// of library/ui/kernel says when one is out of date.
//
//	cd tools/minify && go run .
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/js"

	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/kernel"
)

func main() {
	out := flag.String("out", filepath.Join("..", "..", "library", "ui", "kernel", "minified"), "the folder the minified files go in")
	flag.Parse()
	m := minify.New()
	m.AddFuncRegexp(regexp.MustCompile(`^(application|text)/(x-)?(java|ecma)script$`), js.Minify)
	kernel.Minifier = func(source string) (string, error) { return m.String("application/javascript", source) }

	// every file, built as engine2 builds it: the parts first, which the components name
	built := []*kernel.File{kernel.KernelMin(), kernel.DriveMin(), kernel.CallMin(), kernel.IntersectMin(), ui.ScrolledMin()}
	for _, name := range component.PartNames() {
		built = append(built, component.MinPart(name))
	}
	for _, name := range component.Names() {
		built = append(built, component.MinFile(name))
	}
	for _, file := range built {
		if file == nil {
			fail("a file did not minify: the minifier refused its source")
		}
	}

	if err := os.RemoveAll(*out); err != nil {
		fail(err.Error())
	}
	made := kernel.Made()
	names := make([]string, 0, len(made))
	for name := range made {
		names = append(names, name)
	}
	sort.Strings(names)
	var list strings.Builder
	readable, small := 0, 0
	for _, name := range names {
		entry := made[name]
		path := filepath.Join(*out, filepath.FromSlash(name)+".js")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail(err.Error())
		}
		if err := os.WriteFile(path, []byte(entry[1]), 0o644); err != nil {
			fail(err.Error())
		}
		list.WriteString(name + " " + entry[0] + "\n")
		small += len(entry[1])
	}
	if err := os.WriteFile(filepath.Join(*out, "sources.txt"), []byte(list.String()), 0o644); err != nil {
		fail(err.Error())
	}
	for _, file := range []*kernel.File{kernel.Kernel(), kernel.Drive(), kernel.Call(), kernel.Intersect(), ui.Scrolled()} {
		readable += len(file.Body)
	}
	for _, name := range component.PartNames() {
		readable += len(component.Part(name).Body)
	}
	for _, name := range component.Names() {
		readable += len(component.File(name).Body)
	}
	fmt.Printf("%d files minified in %s: %d bytes, readable %d\n", len(names), *out, small, readable)
}

func fail(why string) {
	fmt.Fprintln(os.Stderr, "minify:", why)
	os.Exit(1)
}
