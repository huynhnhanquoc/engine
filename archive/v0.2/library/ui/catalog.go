package ui

import "github.com/kitwork/engine2/core/value"

// tagKind is how a tag holds what is inside it.
type tagKind uint8

const (
	kindContainer tagKind = iota // lays its children out
	kindInline                   // holds a line of text: a <text> inside is a <span>
	kindVoid                     // never holds anything
)

// tag is what one tag of Kitwork becomes on the web (KITWORK_UI mục 7): its element and how it holds what
// is inside it. Everything in markup is a tag (mục 13); a name without a dash is Kitwork's, a name with one the
// site's (a piece, <surface-card />).
type tag struct {
	html string
	kind tagKind
}

// catalog is Kitwork's vocabulary as slice 1 draws it: the core tags and the elements of Kitwork. text, which
// is <p>, <span> or <h1>…<h6> by where and how it is written, is drawn by its own rule (render.go).
var catalog = map[string]tag{
	// the screen and its regions
	"view": {html: "body", kind: kindContainer}, "header": {html: "header", kind: kindContainer},
	"footer": {html: "footer", kind: kindContainer}, "navbar": {html: "nav", kind: kindContainer},
	"content": {html: "main", kind: kindContainer}, "overlay": {html: "div", kind: kindContainer},
	"sidebar": {html: "aside", kind: kindContainer}, "tabbar": {html: "nav", kind: kindContainer},

	// layout and surfaces
	"section": {html: "section", kind: kindContainer}, "box": {html: "div", kind: kindContainer},
	"card": {html: "div", kind: kindContainer}, "grid": {html: "div", kind: kindContainer},
	"list": {html: "ul", kind: kindContainer}, "item": {html: "li", kind: kindContainer},
	"empty": {html: "li", kind: kindContainer}, "group": {html: "div", kind: kindContainer},
	"popover": {html: "div", kind: kindContainer},

	// text and what stands in a line
	"code": {html: "code", kind: kindInline}, "quote": {html: "blockquote", kind: kindContainer},
	"time": {html: "time", kind: kindInline}, "badge": {html: "span", kind: kindInline},
	"link": {html: "a", kind: kindInline}, "button": {html: "button", kind: kindInline},
	"icon": {html: "i", kind: kindVoid}, "image": {html: "img", kind: kindVoid},

	// forms
	"form": {html: "form", kind: kindContainer}, "field": {html: "div", kind: kindContainer},
	"label": {html: "label", kind: kindInline}, "hint": {html: "p", kind: kindInline},
	"error": {html: "p", kind: kindInline}, "input": {html: "input", kind: kindVoid},
	"select": {html: "select", kind: kindContainer}, "option": {html: "option", kind: kindInline},
	"textarea": {html: "textarea", kind: kindInline},

	// elements of Kitwork whose own look is slice 3's: here a box holding what the author wrote
	"accordions": {html: "div", kind: kindContainer}, "accordion": {html: "details", kind: kindContainer},
	"breadcrumb": {html: "nav", kind: kindContainer},
}

// componentState are Kitwork's components (KITWORK_UI mục 13) and the names each declares, as they are before
// anything happened (mục 7): inside the component its names are its own, not the page's (a page that has an
// opened of its own does not open every dropdown), and the page reads them anywhere through the component's
// alias, $menu.opened. A function's name (toggle) is the kernel's to call: on the server it is nothing, and so is
// what the server cannot know (dark: the visitor's choice is kept in their browser).
//
// The server draws each component's host and what it holds, as the first engine did, and the kernel runs it in the
// browser (live.go runs); richtext the server draws whole.
var componentState = map[string]map[string]any{
	"appearance": {"mode": "system", "dark": nil, "toggle": nil, "set": nil, "system": nil},
	"dropdown":   {"opened": false, "open": nil, "close": nil, "toggle": nil},
	"toast": {"notices": []any{}, "waiting": 0.0, "paused": false, "show": nil, "info": nil, "success": nil,
		"warning": nil, "danger": nil, "loading": nil, "update": nil, "dismiss": nil, "clear": nil, "act": nil},
	"progressbar": {"active": false, "value": 0.0, "top": nil},
	"terminal":    {"typing": 0.0, "copied": false, "copy": nil},
	"tabs":        {"selected": "", "persist": "", "select": nil},
	"network":     {"connected": true, "opened": false, "close": nil},
	"carousel": {"current": 0.0, "count": 0.0, "playing": false, "go": nil, "next": nil, "previous": nil,
		"play": nil, "pause": nil, "toggle": nil},
	"countdown": {"duration": 0.0, "remaining": 0.0, "hours": 0.0, "minutes": 0.0, "seconds": 0.0, "playing": false,
		"play": nil, "pause": nil, "toggle": nil, "stop": nil},
	"editor": {"empty": true, "words": 0.0, "characters": 0.0, "unsaved": false, "opened": false, "address": "",
		"format": "rich", "dropped": []any{}, "toggle": nil, "align": nil, "rule": nil, "link": nil, "unlink": nil,
		"undo": nil, "redo": nil, "close": nil},
	"richtext": {},
	"reader":   {"text": "", "say": nil},
	// a dialog's value is what open(x) gives, in the browser (dialog.go): nothing on the server
	"dialog": {"opened": false, "value": nil, "open": nil, "close": nil, "confirm": nil},
	// an autocomplete's list shows while opened; what it chose is its hidden input's (autocomplete.go)
	"autocomplete": {"opened": false, "close": nil},
}

// stateValue is a name of a component's state as a value of the page: nothing for nil.
func stateValue(initial any) value.Value {
	switch v := initial.(type) {
	case bool:
		return value.Boolean(v)
	case float64:
		return value.Number(v)
	case string:
		return value.String(v)
	case []any:
		return value.Array(nil)
	}
	return value.Value{}
}

// stateObject is a component's state as one object, what its alias ($menu) reads.
func stateObject(state map[string]any) value.Value {
	object := value.Object()
	for name, initial := range state {
		object.Set(name, stateValue(initial))
	}
	return object
}
