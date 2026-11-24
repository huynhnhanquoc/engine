package js

import (
	"regexp"
	"strings"

	hydrate "github.com/kitwork/engine/jit/hydrate"
	"github.com/kitwork/engine/jit/internal/htmlattr"
)

// Directives whose machinery stays out of the kernel. The kernel knows the event's name and runs its
// expression through the event pipeline; the part that watches the browser is a file of its own,
// delivered only to a page that asks for it — like a component, but named by the grammar.
//
//	data-kitwork-event="intersect-enter[.once][.half|.full][.margin(x)]: …; intersect-leave: …"
//	                                       → directives/intersect.js
//	data-kitwork-drag="true" | "false"         → directives/drag.js (a desktop window's own title bar)
//	data-kitwork-bind="html: content"          → directives/html.js (inert, sanitized content)
//
// (data-kit-intersect, the hand-written form of template pages, went on 26/10/2026.)
var directiveMarkers = map[string]string{"intersect": "intersect-", "drag": "data-kitwork-drag", "html": "data-kitwork-bind"}

func directiveSource(name string) string {
	if _, known := directiveMarkers[name]; !known {
		return ""
	}
	key := "directive:" + name
	if v, ok := moduleCache.Load(key); ok {
		return v.(string)
	}
	b, err := jsFS.ReadFile("directives/" + name + ".js")
	s := ""
	if err == nil {
		s = strings.TrimSpace(string(b))
	}
	moduleCache.Store(key, s)
	return s
}

func directiveAsset(name string) *Asset {
	source := directiveSource(name)
	if source == "" {
		return nil
	}
	return cachedAsset("directive:"+name, func() *Asset { return buildAsset("directive", name, source) })
}

// directiveAssets is the directive files a page needs, in a fixed order. The page is read without
// its examples, so a docs page that prints a directive gets no module for that example.
func directiveAssets(html string) []*Asset {
	var out []*Asset
	for _, name := range []string{"intersect", "drag", "html"} {
		var needed bool
		switch name {
		case "intersect":
			needed = groupedIntersect(html)
		case "drag":
			needed = strings.Contains(html, directiveMarkers[name])
		case "html":
			needed = groupedHTML(html)
		}
		if needed {
			if asset := directiveAsset(name); asset != nil {
				out = append(out, asset)
			}
		}
	}
	return out
}

// groupedEventAttrRe finds a data-kitwork-event attribute; its value is read by hydrate.Pairs.
var groupedEventAttrRe = regexp.MustCompile(`data-kitwork-event="([^"]*)"`)

var groupedBindAttrRe = regexp.MustCompile(`data-kitwork-bind="([^"]*)"`)

func groupedHTML(html string) bool {
	for _, match := range groupedBindAttrRe.FindAllStringSubmatch(html, -1) {
		pairs, err := hydrate.Pairs(htmlattr.Decode(match[1]))
		if err != nil {
			continue
		}
		for _, pair := range pairs {
			if pair.Name == "html" {
				return true
			}
		}
	}
	return false
}

// groupedIntersect reports whether a data-kitwork-event on the page has an intersect-enter or
// intersect-leave pair (any modifiers after the name).
func groupedIntersect(html string) bool {
	if !strings.Contains(html, "intersect-") {
		return false
	}
	for _, m := range groupedEventAttrRe.FindAllStringSubmatch(html, -1) {
		pairs, err := hydrate.Pairs(htmlattr.Decode(m[1]))
		if err != nil {
			continue
		}
		for _, pair := range pairs {
			event, _, _ := strings.Cut(pair.Name, ".")
			if event == "intersect-enter" || event == "intersect-leave" {
				return true
			}
		}
	}
	return false
}
