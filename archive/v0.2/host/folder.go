package host

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/router"
)

// The kinds of a folder's segment, in the order siblings are tried (docs/KITWORK_ROUTER.md mục 2): the
// exact word, a typed parameter, a regex, a plain parameter, the rest of the path.
const (
	segmentExact = iota
	segmentTyped
	segmentPattern
	segmentPlain
	segmentRest
)

// Folder is one folder of a site's route tree: the segment of the URL its name matches, its children,
// and its router scope, what its config.work and router.work declared.
type Folder struct {
	// Name is the folder's name on disk; Path is the folder from the site's root, "/" for the root.
	Name string
	Path string
	kind int
	// parameter is the name a parameter folder gives its segment, {id[number]} → id.
	parameter string
	number    bool
	pattern   *regexp.Regexp
	children  []*Folder
	// parent is the folder above, nil for the site's root.
	parent *Folder
	scope  *router.Scope
	// page: the folder has a content.ui, a page GET answers with when no door is declared.
	page bool
	// look is what the folder's pages are drawn with: its design.work over its parent's look.
	look *design.Look
	// head is what its pages' <head> starts with: language, icon and router.meta down the folders (head.go)
	head head
}

// parseSegment reads what a folder's name matches: id[number], slug[string], code(^re$), name, ...rest
// between braces, or the name itself.
func (f *Folder) parseSegment() error {
	name := f.Name
	if !strings.HasPrefix(name, "{") || !strings.HasSuffix(name, "}") {
		f.kind = segmentExact
		return nil
	}
	inner := name[1 : len(name)-1]
	switch {
	case strings.HasPrefix(inner, "..."):
		f.kind, f.parameter = segmentRest, inner[3:]
	case strings.HasSuffix(inner, "]") && strings.Contains(inner, "["):
		open := strings.Index(inner, "[")
		f.kind, f.parameter = segmentTyped, inner[:open]
		switch inner[open+1 : len(inner)-1] {
		case "number":
			f.number = true
		case "string":
		default:
			return fmt.Errorf("folder %s: [%s] is no type of a segment; the two are [number] and [string]", name, inner[open+1:len(inner)-1])
		}
	case strings.HasSuffix(inner, ")") && strings.Contains(inner, "("):
		open := strings.Index(inner, "(")
		f.kind, f.parameter = segmentPattern, inner[:open]
		source := inner[open+1 : len(inner)-1]
		pattern, err := regexp.Compile("^(?:" + strings.TrimSuffix(strings.TrimPrefix(source, "^"), "$") + ")$")
		if err != nil {
			return fmt.Errorf("folder %s: the regex does not compile: %v", name, err)
		}
		f.pattern = pattern
	default:
		f.kind, f.parameter = segmentPlain, inner
	}
	if !isParameterName(f.parameter) {
		return fmt.Errorf("folder %s: %q is no parameter name: letters, digits and _, not first a digit", name, f.parameter)
	}
	return nil
}

func isParameterName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// sortChildren puts the children in the order they are tried: by kind, then by name.
func (f *Folder) sortChildren() {
	sort.SliceStable(f.children, func(i, j int) bool {
		a, b := f.children[i], f.children[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.Name < b.Name
	})
}

// match is the value a segment gives this folder's parameter, and whether the folder matches it. A value
// of the wrong type is no error: the folder does not match, and the next sibling is tried.
func (f *Folder) match(segment string) (value.Value, bool) {
	switch f.kind {
	case segmentExact:
		return value.Value{}, segment == f.Name
	case segmentTyped:
		if !f.number {
			return value.String(segment), true
		}
		for i := 0; i < len(segment); i++ {
			if segment[i] < '0' || segment[i] > '9' {
				return value.Value{}, false
			}
		}
		number, err := strconv.ParseFloat(segment, 64)
		return value.Number(number), err == nil
	case segmentPattern:
		return value.String(segment), f.pattern.MatchString(segment)
	}
	return value.String(segment), true
}

// routable reports a folder a request can end in: one with a door or a page.
func (f *Folder) routable() bool {
	return len(f.scope.Doors) > 0 || f.page
}

// find is the folder segments lead to from f, depth first in each folder's order, the first that takes
// every segment and can answer; params receives the parameters on the way.
func (f *Folder) find(segments []string, params map[string]value.Value) *Folder {
	if len(segments) == 0 {
		if f.routable() {
			return f
		}
		return nil
	}
	for _, child := range f.children {
		if child.kind == segmentRest {
			if child.routable() {
				params[child.parameter] = value.String(strings.Join(segments, "/"))
				return child
			}
			continue
		}
		matched, ok := child.match(segments[0])
		if !ok {
			continue
		}
		if child.parameter != "" {
			params[child.parameter] = matched
		}
		if found := child.find(segments[1:], params); found != nil {
			return found
		}
		if child.parameter != "" {
			delete(params, child.parameter)
		}
	}
	return nil
}

// reach is the deepest folder a path no folder answers goes through from f: at each segment the first child that
// takes it, in the order children are tried, until none does; params receives their parameters. A request of such a
// path meets the rules of the folders down to it before its 404 (docs/router.md mục 4).
func (f *Folder) reach(segments []string, params map[string]value.Value) *Folder {
	at := f
	for index := 0; index < len(segments); index++ {
		var next *Folder
		for _, child := range at.children {
			if child.kind == segmentRest {
				params[child.parameter] = value.String(strings.Join(segments[index:], "/"))
				return child
			}
			if matched, ok := child.match(segments[index]); ok {
				if child.parameter != "" {
					params[child.parameter] = matched
				}
				next = child
				break
			}
		}
		if next == nil {
			return at
		}
		at = next
	}
	return at
}

// door is the folder's door for method, HEAD answering as GET; a folder with a page and no GET door
// answers GET with the page.
func (f *Folder) door(method string) (*router.Door, bool) {
	if method == "head" {
		method = "get"
	}
	if door, ok := f.scope.Doors[method]; ok {
		return door, true
	}
	if method == "get" && f.page {
		return &router.Door{View: true}, true
	}
	return nil, false
}

// viewing reports a folder whose GET door is router.view(fn), which declares with a function: its other doors may
// answer with its page.
func (f *Folder) viewing() bool {
	door, ok := f.scope.Doors["get"]
	return ok && door.View
}

// allow is the Allow header of the folder: its methods, HEAD with GET, and OPTIONS.
func (f *Folder) allow() string {
	var methods []string
	for _, method := range []string{"get", "post", "put", "patch", "delete"} {
		if _, ok := f.door(method); ok {
			methods = append(methods, strings.ToUpper(method))
			if method == "get" {
				methods = append(methods, "HEAD")
			}
		}
	}
	return strings.Join(append(methods, "OPTIONS"), ", ")
}
