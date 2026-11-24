// Package component is "kitwork/component" (KITWORK_ENGINE mục 5, KITWORK_COMPONENTS.md). Today it holds the
// browser's half of Kitwork's own components (appearance, dropdown, toast…): each one file registering its
// blueprint with the kernel, window.kitwork.component("dropdown", …), moved whole from the first engine's
// jit/js/components on 06/11/2026, the newest version of each, and the parts a component loads itself when it
// needs them (parts/, the editor's), moved whole from its jit/js/parts. The server half, what each draws and the
// names it declares, is library/ui's. component.define and the components of a site come here later.
package component

import (
	"embed"
	"regexp"
	"strings"
	"sync"

	"github.com/kitwork/engine2/library/ui/kernel"
)

//go:embed *.js parts/*.js
var sources embed.FS

// cache holds each component's file once built, by name; parts each part's; minCache and minParts their files
// minified at build time (kernel.Min).
var cache, parts, minCache, minParts sync.Map

// File is the file of the component named name, a page loads at /_kitwork/components/<name>.<hash>.js; nil for
// a name the catalogue does not have. Its source runs in a function of its own, so its names stay its own, and
// names each part it loads by the part's name of today.
func File(name string) *kernel.File {
	if known, ok := cache.Load(name); ok {
		return known.(*kernel.File)
	}
	if !slug(name) {
		return nil
	}
	source, err := sources.ReadFile(name + ".js")
	if err != nil {
		return nil
	}
	file := kernel.New("component", "components/"+name, wrap(named(string(source), "", Part)))
	known, _ := cache.LoadOrStore(name, file)
	return known.(*kernel.File)
}

// MinFile is File minified at build time, naming the minified parts it loads; nil for a name the catalogue does
// not have, or one whose source changed since tools/minify ran.
func MinFile(name string) *kernel.File {
	if known, ok := minCache.Load(name); ok {
		return known.(*kernel.File)
	}
	if !slug(name) {
		return nil
	}
	source, err := sources.ReadFile(name + ".js")
	if err != nil {
		return nil
	}
	file := kernel.Min("component", "components/"+name, wrap(named(string(source), "", MinPart)))
	if file == nil {
		return nil
	}
	known, _ := minCache.LoadOrStore(name, file)
	return known.(*kernel.File)
}

// Names are the components of the catalogue; PartNames the parts they load.
func Names() []string { return embedded(".") }

func PartNames() []string { return embedded("parts") }

// embedded are the names of the .js files of folder, without their extension.
func embedded(folder string) []string {
	entries, _ := sources.ReadDir(folder)
	var out []string
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), ".js"); ok && !entry.IsDir() {
			out = append(out, name)
		}
	}
	return out
}

// A part is a file a component loads itself when it needs it, never with the page: the editor's reading of a
// paste or a drop (parts/editor-paste.js), what puts its tooltip and its link's popover in their place
// (editor-place.js), its Markdown and HTML (editor-format.js, which loads editor-markdown.js). A source names a
// part without its hash, "/_kitwork/parts/editor-paste.js" in quotes; the file a page gets has the part's name
// of today in that place, so the hash of what names a part follows the part's bytes too. A part naming itself
// keeps the name as written (its hash would be its own bytes).

// partInSource finds a part a source names without its hash.
var partInSource = regexp.MustCompile(`"/_kitwork/parts/([a-z][a-z0-9-]*)\.js"`)

// Part is the part named name, at /_kitwork/parts/<name>.<hash>.js; nil for a name no part has. Only its name
// of today is answered (FileAt): a component of another build would hand a part of this one what it no longer
// expects, so it gets nothing and does without (the editor pastes plain words).
func Part(name string) *kernel.File {
	if known, ok := parts.Load(name); ok {
		return known.(*kernel.File)
	}
	if !slug(name) {
		return nil
	}
	source, err := sources.ReadFile("parts/" + name + ".js")
	if err != nil {
		return nil
	}
	file := kernel.New("part", "parts/"+name, wrap(named(string(source), name, Part)))
	known, _ := parts.LoadOrStore(name, file)
	return known.(*kernel.File)
}

// MinPart is Part minified at build time, naming the minified parts it loads; nil for a name no part has, or one
// whose source changed since tools/minify ran.
func MinPart(name string) *kernel.File {
	if known, ok := minParts.Load(name); ok {
		return known.(*kernel.File)
	}
	if !slug(name) {
		return nil
	}
	source, err := sources.ReadFile("parts/" + name + ".js")
	if err != nil {
		return nil
	}
	file := kernel.Min("part", "parts/"+name, wrap(named(string(source), name, MinPart)))
	if file == nil {
		return nil
	}
	known, _ := minParts.LoadOrStore(name, file)
	return known.(*kernel.File)
}

// named is source with each part it names, but self, under the part's name of today as part builds it (Part, or
// MinPart for the minified files); a name no part has stays as written.
func named(source, self string, part func(string) *kernel.File) string {
	return partInSource.ReplaceAllStringFunc(source, func(quoted string) string {
		name := partInSource.FindStringSubmatch(quoted)[1]
		if name == self {
			return quoted
		}
		if file := part(name); file != nil {
			return `"` + file.Path + `"`
		}
		return quoted
	})
}

// wrap runs source in a function of its own, so its names stay its own.
func wrap(source string) string { return ";(function () {\n" + source + "\n})();" }

// slug says name can be a component's: lower-case letters, digits and dashes, a letter first, so it never reaches
// outside the catalogue.
func slug(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
