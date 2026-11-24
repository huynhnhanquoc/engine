// Package design is "kitwork/design": the words of design.work, where a folder declares the look of its pages
// (docs/KITWORK_DESIGN.md), and the look each folder's pages are drawn with.
//
//	import * as design from "kitwork/design";
//	design.colors({ brand: "#f82244", ink: { light: "#18181b", dark: "#fafafa" } });
//	design.fonts({ body: "Outfit", code: "Fira Code" });
//
// design.work stands where config.work does, at the app root, a site's root or a branch; it runs first, once,
// at boot. A folder writes only what it wants other than its parent: its look is its parent's with its own
// words over it, and the app root's is over Kit Design's (Look).
package design

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// Scope is what one folder's design.work declares as it runs.
type Scope struct {
	colors    map[string]stylesheet.Color
	gradients map[string]stylesheet.Gradient
	highlight map[string]stylesheet.HighlightColour
	body      string
	code      string
	// the words the nearest folder decides, each with where it is declared, for the error of a second
	appearance, contrast, transition       string
	appearanceAt, contrastAt, transitionAt string
	// Where is the place of the call the file makes now, "kitwork.io/design.work:3:1".
	Where func() string
	// Notice prints one boot line of the site.
	Notice func(text string)
}

func (s *Scope) where() string {
	if s.Where == nil {
		return "design.work"
	}
	return s.Where()
}

// Empty says the folder declares nothing: its look is its parent's.
func (s *Scope) Empty() bool {
	return len(s.colors) == 0 && len(s.gradients) == 0 && len(s.highlight) == 0 && s.body == "" && s.code == "" &&
		s.appearance == "" && s.contrast == "" && s.transition == ""
}

// Design is "kitwork/design" as design.work sees it.
type Design struct {
	Scope *Scope
	// Open says the file is running at boot: a function the file kept and calls later declares nothing.
	Open func() bool
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

func fail(text string) value.Value { return value.Fail(value.InvalidArgumentCode, text) }

// unready is a word used where nothing declares: at a request, or from a function the file kept.
func (d *Design) unready(word string) (value.Value, bool) {
	if d.Scope == nil || d.Open == nil || !d.Open() {
		return value.Fail(value.UnsupportedCode, "design."+word+"(…) declares at the top level of design.work, while the site boots"), true
	}
	return value.Value{}, false
}

// object is the one object a word that merges by key takes, or its failure.
func (d *Design) object(word, example string, arguments []value.Value) (value.Value, value.Value, bool) {
	if failure, ok := d.unready(word); ok {
		return value.Value{}, failure, false
	}
	declared := argument(arguments, 0)
	if len(arguments) != 1 || !declared.IsObject() {
		return value.Value{}, fail("design." + word + "(…) takes one object: " + example), false
	}
	return declared, value.Value{}, true
}

// pair reads a colour as design.colors writes a token: one colour, or the colour of each look, { light, dark }.
func pair(at string, v value.Value) (stylesheet.Color, string) {
	if v.IsString() {
		color := strings.TrimSpace(v.Text())
		if color == "" {
			return stylesheet.Color{}, at + ": the colour is empty"
		}
		return stylesheet.Color{Light: color}, ""
	}
	if !v.IsObject() {
		return stylesheet.Color{}, at + `: a colour is a string ("#2563eb", "brand+20", "tomato") or the colour of each look, { light: "#2563eb", dark: "#60a5fa" }`
	}
	var c stylesheet.Color
	for _, key := range v.Keys() {
		side := v.Get(key)
		if key != "light" && key != "dark" {
			return c, at + `: a pair takes light and dark, not "` + key + `"`
		}
		if !side.IsString() {
			return c, at + ": " + key + ` of the pair is a string, "#rrggbb" or a named colour`
		}
		if key == "light" {
			c.Light = strings.TrimSpace(side.Text())
		} else {
			c.Dark = strings.TrimSpace(side.Text())
		}
	}
	if c == (stylesheet.Color{}) {
		return c, at + `: the pair is empty, write { light: "#2563eb", dark: "#60a5fa" }`
	}
	return c, ""
}

// Colors is design.colors({ token: colour, … }): the palette's tokens, merged by token over the parent's and
// over the calls before it.
func (d *Design) Colors(arguments ...value.Value) value.Value {
	declared, failure, ok := d.object("colors", `design.colors({ brand: "#2563eb", ink: { light: "#18181b", dark: "#fafafa" } })`, arguments)
	if !ok {
		return failure
	}
	colors := map[string]stylesheet.Color{}
	for _, token := range declared.Keys() {
		c, problem := pair("design.colors({ "+token+": … })", declared.Get(token))
		if problem != "" {
			return fail(problem)
		}
		colors[token] = c
	}
	if d.Scope.colors == nil {
		d.Scope.colors = map[string]stylesheet.Color{}
	}
	for token, c := range colors {
		d.Scope.colors[token] = c
	}
	return value.Undefined()
}

// Gradients is design.gradients({ name: { type, direction or position, stops }, … }): gradients a page names as
// a colour, merged by name.
func (d *Design) Gradients(arguments ...value.Value) value.Value {
	declared, failure, ok := d.object("gradients", `design.gradients({ hero: { type: "linear", direction: "to-bottom-right", stops: [{ color: "brand", at: 0 }, { color: "brand+15", at: 100 }] } })`, arguments)
	if !ok {
		return failure
	}
	gradients := map[string]stylesheet.Gradient{}
	for _, name := range declared.Keys() {
		g, problem := gradient("design.gradients({ "+name+": … })", declared.Get(name))
		if problem != "" {
			return fail(problem)
		}
		if err := stylesheet.CheckGradient(name, g); err != nil {
			return fail(err.Error())
		}
		gradients[name] = g
	}
	if d.Scope.gradients == nil {
		d.Scope.gradients = map[string]stylesheet.Gradient{}
	}
	for name, g := range gradients {
		d.Scope.gradients[name] = g
	}
	return value.Undefined()
}

// gradient reads one gradient: an object of type, direction, position and stops, each stop { color, at }.
func gradient(at string, v value.Value) (stylesheet.Gradient, string) {
	var g stylesheet.Gradient
	if !v.IsObject() {
		return g, at + ": a gradient is an object { type, direction or position, stops }"
	}
	for _, key := range v.Keys() {
		field := v.Get(key)
		switch key {
		case "type", "direction", "position":
			if !field.IsString() {
				return g, at + ": " + key + ` is a string ("linear", "to-bottom", "top"…)`
			}
			word := strings.TrimSpace(field.Text())
			switch key {
			case "type":
				g.Type = word
			case "direction":
				g.Direction = word
			default:
				g.Position = word
			}
		case "stops":
			if !field.IsArray() {
				return g, at + `: stops is an array of stops, [{ color: "brand", at: 0 }, { color: "brand+15", at: 100 }]`
			}
			for i, stop := range field.Items() {
				place := at + ": stop " + strconv.Itoa(i+1)
				if !stop.IsObject() {
					return g, place + ` is an object { color, at }`
				}
				for _, name := range stop.Keys() {
					if name != "color" && name != "at" {
						return g, place + ` takes color and at, not "` + name + `"`
					}
				}
				position := stop.Get("at")
				if !position.IsNumber() {
					return g, place + " has no at, a number from 0 to 100"
				}
				c, problem := pair(place, stop.Get("color"))
				if problem != "" {
					return g, problem
				}
				s := stylesheet.Stop{At: position.Number()}
				if c.Dark == "" {
					s.Color = c.Light
				} else {
					s.Pair = c
				}
				g.Stops = append(g.Stops, s)
			}
		default:
			return g, at + `: a gradient takes type, direction, position and stops, not "` + key + `"`
		}
	}
	return g, ""
}

// Fonts is design.fonts({ body, code }): the typeface of each role, merged by role.
func (d *Design) Fonts(arguments ...value.Value) value.Value {
	declared, failure, ok := d.object("fonts", `design.fonts({ body: "Outfit", code: "Fira Code" })`, arguments)
	if !ok {
		return failure
	}
	roles := map[string]string{}
	for _, role := range declared.Keys() {
		name := declared.Get(role)
		if role != "body" && role != "code" {
			return fail(`design.fonts(…) names the typeface of two roles, body and code, not "` + role + `"`)
		}
		if !name.IsString() || strings.TrimSpace(name.Text()) == "" {
			return fail("design.fonts({ " + role + ": … }): a typeface is its name, a string (\"Outfit\")")
		}
		roles[role] = strings.TrimSpace(name.Text())
	}
	for role, name := range roles {
		if !Carried(name) && d.Scope.Notice != nil {
			d.Scope.Notice(d.Scope.where() + ": design.fonts({ " + role + ": \"" + name + "\" }): engine2 does not carry " + name + " yet; the page names it and the system's font draws it")
		}
		if role == "body" {
			d.Scope.body = name
		} else {
			d.Scope.code = name
		}
	}
	return value.Undefined()
}

// Highlight is design.highlight({ kind: colour, … }): the colour of each kind of code, merged by kind.
func (d *Design) Highlight(arguments ...value.Value) value.Value {
	declared, failure, ok := d.object("highlight", `design.highlight({ keyword: "brand", string: "success", comment: "ink-30" })`, arguments)
	if !ok {
		return failure
	}
	kinds := map[string]stylesheet.HighlightColour{}
	where := source(d.Scope.where())
	for _, key := range declared.Keys() {
		v := declared.Get(key)
		written := "…"
		if v.IsString() {
			written = v.Text()
		}
		kind, problem := stylesheet.CheckHighlightKey(key, written)
		if problem != "" {
			return fail("design.highlight({ " + key + ": … }): " + problem)
		}
		c, problem := pair("design.highlight({ "+stylesheet.HighlightKey(kind)+": … })", v)
		if problem != "" {
			return fail(problem)
		}
		colour := stylesheet.HighlightColour{Source: where}
		if c.Dark == "" && v.IsString() {
			colour.Color = c.Light
		} else {
			colour.Pair = c
		}
		if err := stylesheet.CheckHighlight(kind, colour); err != nil {
			return fail(err.Error())
		}
		kinds[kind] = colour
	}
	if d.Scope.highlight == nil {
		d.Scope.highlight = map[string]stylesheet.HighlightColour{}
	}
	for kind, c := range kinds {
		d.Scope.highlight[kind] = c
	}
	return value.Undefined()
}

// source is a place "file:line:column" as the stylesheet keeps it.
func source(where string) stylesheet.Source {
	parts := strings.Split(where, ":")
	if len(parts) < 3 {
		return stylesheet.Source{File: where}
	}
	line, lineErr := strconv.Atoi(parts[len(parts)-2])
	column, columnErr := strconv.Atoi(parts[len(parts)-1])
	if lineErr != nil || columnErr != nil {
		return stylesheet.Source{File: where}
	}
	return stylesheet.Source{File: strings.Join(parts[:len(parts)-2], ":"), Line: line, Column: column}
}

// Appearance is design.appearance("system" | "light" | "dark"): the look a page shows before its visitor
// chooses one.
func (d *Design) Appearance(arguments ...value.Value) value.Value {
	return d.nearest("appearance", []string{"system", "light", "dark"}, arguments, &d.scope().appearance, &d.scope().appearanceAt)
}

// Contrast is design.contrast("warning" | "strict"): a colour short of contrast is a warning or an error.
func (d *Design) Contrast(arguments ...value.Value) value.Value {
	return d.nearest("contrast", []string{"warning", "strict"}, arguments, &d.scope().contrast, &d.scope().contrastAt)
}

// Transition is design.transition("none" | "fade" | "slide"): how a page changes when Drive moves to it.
func (d *Design) Transition(arguments ...value.Value) value.Value {
	return d.nearest("transition", []string{"none", "fade", "slide"}, arguments, &d.scope().transition, &d.scope().transitionAt)
}

// scope is the scope, or a scope of nothing for a word used where nothing declares (which unready refuses
// before anything is written).
func (d *Design) scope() *Scope {
	if d.Scope == nil {
		return &Scope{}
	}
	return d.Scope
}

// nearest declares a word the nearest folder decides: one of words, once a folder.
func (d *Design) nearest(word string, words []string, arguments []value.Value, into, at *string) value.Value {
	if failure, ok := d.unready(word); ok {
		return failure
	}
	written := argument(arguments, 0)
	quoted := `"` + strings.Join(words, `", "`) + `"`
	if len(arguments) != 1 || !written.IsString() {
		return fail("design." + word + "(…) takes one of " + quoted)
	}
	choice := strings.TrimSpace(written.Text())
	found := false
	for _, each := range words {
		found = found || each == choice
	}
	if !found {
		return fail("design." + word + `("` + choice + `"): ` + word + " is one of " + quoted)
	}
	here := d.Scope.where()
	if *at != "" {
		return fail("design." + word + "(…) is declared twice in one folder, at " + *at + " and at " + here + ": the nearest folder decides it, once")
	}
	*into, *at = choice, here
	return value.Undefined()
}

// routerWords are the words of router.work and config.work, which a design.work does not hold.
var routerWords = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true, "view": true, "guard": true, "limit": true,
	"cors": true, "methods": true, "cache": true, "persist": true, "language": true, "meta": true,
	"jsonld": true, "favicon": true, "bundle": true, "reload": true, "assets": true, "proxy": true, "sitemap": true,
	"rss": true, "robots": true, "llms": true, "llmsFull": true, "manifest": true,
}

// MissingMember is a word design does not have: a router's word says where it is written, any other the
// nearest of the seven. Both are failures, so the boot stops at the line.
func (d *Design) MissingMember(name string) value.Value {
	if name == "timezone" {
		return value.Fail(value.TypeCode, "design.timezone is no word of design.work: the app's zone is module.timezone(…), in module.work at the app's root")
	}
	if routerWords[name] {
		return value.Fail(value.TypeCode, "design."+name+" is no word of design.work: router."+name+"(…) is written in router.work or config.work")
	}
	text := "design has no word " + name
	if nearest := value.NearestName(name, value.Expose[Design]().Names()); nearest != "" {
		text += ": did you mean design." + nearest + "?"
	}
	return value.Fail(value.TypeCode, text)
}
