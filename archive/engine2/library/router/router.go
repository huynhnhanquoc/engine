// Package router is "kitwork/router": the words of router.work and config.work, and the context a
// handler receives (docs/KITWORK_ROUTER.md).
//
//	import * as router from "kitwork/router";
//	router.get((context) => context.text("xin chào"));
//
// A word declares into the folder's Scope while the file runs at boot; a request runs only the handlers.
// The engine serves part of the vocabulary today: what it does not serve says so at boot, as a line when
// leaving it out changes nothing a visitor may rely on (colors, cache), as an error when it would (a guard
// left out opens what it closes).
package router

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Router is "kitwork/router" as one file sees it: the folder's scope and which of its two files runs.
// A Router with no scope is the one a request reaches: declarations are boot's, so every word fails.
type Router struct {
	Scope *Scope
	// File is "config.work" or "router.work".
	File string
	// Open says the file is running at boot. A handler that kept the file's router and calls a word at a
	// request is refused: it would change the folder's doors while other requests read them.
	Open func() bool
	// chain is set on the router a rule gives (router.guard(…), router.limit(…), router.cache(…),
	// router.persist(…)): what stands before a door, or before .methods(…), which only those or another rule
	// may follow (rules.go). nil on the file's router.
	chain *chain
}

const (
	configFile = "config.work"
	routerFile = "router.work"
)

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// unready is a word used where nothing declares: in a request, or by a value read off another file. A
// word that is no door, no rule and no .methods(…) while a chain waits for its end is refused too.
func (r *Router) unready(word string) (value.Value, bool) {
	if r.Scope == nil || r.Open == nil || !r.Open() {
		return value.Fail(value.UnsupportedCode, "router."+word+"(…) declares at the top level of router.work or config.work, while the site boots; a request only runs handlers"), true
	}
	if r.Scope.before != "" && !doorWords[word] {
		return value.Fail(value.InvalidArgumentCode, r.Scope.Unended()), true
	}
	return value.Value{}, false
}

// doorWords are the words a chain may go on with: a door, a door the engine makes (KITWORK_ROUTER.md mục
// 3, "cache, persist như mọi cửa"), the rules themselves, and .methods(…), which ends a chain of rules.
var doorWords = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true, "view": true,
	"sitemap": true, "rss": true, "robots": true, "llms": true, "llmsFull": true, "manifest": true,
	"proxy": true, "favicon": true, "cache": true, "persist": true, "guard": true, "limit": true,
	"methods": true,
}

// ended is a door word, a rule or .methods(…) while a chain waits: on the router that chain gave, a door or
// .methods(…) ends it and a rule goes on with it; on the plain router the chain written before is still
// without its end, which is the error.
func (r *Router) ended(word string) (value.Value, bool) {
	if r.Scope.before == "" {
		return value.Value{}, false
	}
	if r.chain == nil {
		return value.Fail(value.InvalidArgumentCode, r.Scope.Unended()), true
	}
	if !chainWords[word] {
		r.Scope.before, r.Scope.beforeWord = "", ""
	}
	return value.Value{}, false
}

// ---- doors ----

func (r *Router) Get(arguments ...value.Value) value.Value  { return r.door("get", arguments, false) }
func (r *Router) Post(arguments ...value.Value) value.Value { return r.door("post", arguments, false) }
func (r *Router) Put(arguments ...value.Value) value.Value  { return r.door("put", arguments, false) }
func (r *Router) Patch(arguments ...value.Value) value.Value {
	return r.door("patch", arguments, false)
}
func (r *Router) Delete(arguments ...value.Value) value.Value {
	return r.door("delete", arguments, false)
}

// View is router.view(fn): the GET door of a page of .ui, drawn by the function's context.bind({ … }) (docs/view.md).
func (r *Router) View(arguments ...value.Value) value.Value { return r.door("get", arguments, true) }

// door declares the folder's door for method: once per folder, in router.work, at a site, with a function
// of the program.
func (r *Router) door(method string, arguments []value.Value, view bool) value.Value {
	word := method
	if view {
		word = "view"
	}
	if failure, ok := r.unready(word); ok {
		return failure
	}
	if failure, ok := r.ended(word); ok {
		return failure
	}
	if r.File == configFile {
		return value.Fail(value.InvalidArgumentCode, "router."+word+"(…) in config.work: a door is declared in router.work; config.work holds the folder's rules, a chain of them ended by .methods(…)")
	}
	if r.Scope.Level == LevelApp {
		return value.Fail(value.InvalidArgumentCode, "router."+word+"(…) at the app root: the app root has no doors; a site's folder has")
	}
	handler := argument(arguments, 0).Lambda()
	if handler == nil {
		return value.Fail(value.InvalidArgumentCode, "router."+word+"(…) takes the handler, a function of the program: router."+word+"((context) => context.text(\"…\"))")
	}
	if problem := takenApart(handler, word); problem != "" {
		return value.Fail(value.InvalidArgumentCode, problem)
	}
	at := r.Scope.where()
	if first, ok := r.Scope.Doors[method]; ok {
		return value.Fail(value.InvalidArgumentCode, "router."+word+"(…) declares "+strings.ToUpper(method)+" again: the folder's "+strings.ToUpper(method)+" door is at "+first.At+"; one door per method per folder")
	}
	door := &Door{Handler: handler, At: at, View: view}
	if r.chain != nil {
		door.Rules = r.chain.rules // the rules written before the door are the door's (rules.go)
	}
	r.Scope.Doors[method] = door
	return value.Undefined()
}

// Reload is router.reload(true | false) of the site's root (KITWORK_ROUTER.md mục 4, KITWORK_UI mục 15): the one
// switch of the whole site, false to move between its pages without a reload (Drive, what a site does when it
// writes nothing), true to load each page whole. A branch has no switch of its own: a branch that needs whole
// loads is reached by links written reload="true".
func (r *Router) Reload(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("reload"); ok {
		return failure
	}
	if failure, ok := r.ended("reload"); ok {
		return failure
	}
	if r.Scope.Level != LevelSite {
		return value.Fail(value.InvalidArgumentCode, "router.reload(…) is the site's one switch, written at the site's root (its config.work or router.work), not in a branch: a link into a branch that needs whole loads writes reload=\"true\"")
	}
	if len(arguments) != 1 || !arguments[0].IsBoolean() {
		return value.Fail(value.InvalidArgumentCode, "router.reload(…) takes true or false, written out: false moves between pages without a reload, true loads each page whole")
	}
	if r.Scope.Reload != "" {
		return value.Fail(value.InvalidArgumentCode, "router.reload(…) is declared twice: the first is at "+r.Scope.reloadAt)
	}
	r.Scope.Reload = strconv.FormatBool(arguments[0].ToBoolean())
	r.Scope.reloadAt = r.Scope.where()
	return value.Undefined()
}

// ---- words the engine does not serve yet ----

// refused is a word whose absence would change what a visitor may do: a site that writes it does not boot.
func (r *Router) refused(word, why string) value.Value {
	if failure, ok := r.unready(word); ok {
		return failure
	}
	return value.Fail(value.UnsupportedCode, "router."+word+"(…) is not served by engine2 yet: "+why)
}

func (r *Router) Cors(...value.Value) value.Value {
	return r.refused("cors", "its rules are still open (KITWORK_ROUTER.md mục 14)")
}

// ignored is a word the engine leaves out with one boot line: a look, a cache, a generated door.
func (r *Router) ignored(word string) value.Value {
	if failure, ok := r.unready(word); ok {
		return failure
	}
	if failure, ok := r.ended(word); ok {
		return failure
	}
	if r.Scope.Notice != nil {
		r.Scope.Notice(r.Scope.where() + ": router." + word + "(…) is not served by engine2 yet — ignored")
	}
	return value.Undefined()
}

// Cache and Persist stand before a door (router.cache("5m").get(fn)): left out, the door still declares,
// and every request runs the handler.
func (r *Router) Cache(...value.Value) value.Value   { return r.chained("cache") }
func (r *Router) Persist(...value.Value) value.Value { return r.chained("persist") }

// chained gives the router a door follows: a door on it ends the chain; anything else, or the file's end
// first, is the error KITWORK_ROUTER.md mục 3 says (05/11/2026: router.cache("5m"); alone booted).
func (r *Router) chained(word string) value.Value {
	if failure := r.ignored(word); failure.IsInvalid() {
		return failure
	}
	return r.begin(word, nil)
}

// takenApart is what is wrong with a handler whose first parameter takes context apart, "" when nothing
// is (KITWORK_ROUTER.md mục 5, "Chỉ tách object, không tách hàm"): request and response are objects and
// may be taken out, with a name of their own; a function of context (json, text, view) loses there what
// it answers through, as a function taken out of a JavaScript object loses its this, and a part taken
// apart further (request: { body }) takes out its functions. The compiler names such a parameter by its
// pattern, "{ request, response }".
func takenApart(handler *value.Lambda, word string) string {
	if len(handler.Params) == 0 || !strings.HasPrefix(handler.Params[0], "{") {
		return ""
	}
	pattern := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(handler.Params[0], "{"), "}"))
	for _, part := range topLevelParts(pattern) {
		name, rest, renamed := strings.Cut(part, ":")
		name, rest = strings.TrimSpace(name), strings.TrimSpace(rest)
		if (name == "request" || name == "response") && (!renamed || !strings.ContainsAny(rest, "{[")) {
			continue
		}
		advice := "write context." + name + "(…), or take out only request and response, ({ request, response })"
		if name == "request" || name == "response" {
			advice = "take " + name + " out whole, ({ " + name + " }), and call its functions on it"
		}
		return "router." + word + "(…): the handler takes " + part + " out of context, and a function taken out of context loses the request it answers: " + advice
	}
	return ""
}

// topLevelParts splits a pattern's text at its commas outside braces and brackets.
func topLevelParts(pattern string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(pattern[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(pattern[start:]); last != "" {
		parts = append(parts, last)
	}
	return parts
}

func (r *Router) Bundle(...value.Value) value.Value   { return r.ignored("bundle") }
func (r *Router) Jsonld(...value.Value) value.Value   { return r.ignored("jsonld") }
func (r *Router) Assets(...value.Value) value.Value   { return r.ignored("assets") }
func (r *Router) Proxy(...value.Value) value.Value    { return r.ignored("proxy") }
func (r *Router) Sitemap(...value.Value) value.Value  { return r.ignored("sitemap") }
func (r *Router) Rss(...value.Value) value.Value      { return r.ignored("rss") }
func (r *Router) Robots(...value.Value) value.Value   { return r.ignored("robots") }
func (r *Router) Llms(...value.Value) value.Value     { return r.ignored("llms") }
func (r *Router) LlmsFull(...value.Value) value.Value { return r.ignored("llmsFull") }
func (r *Router) Manifest(...value.Value) value.Value { return r.ignored("manifest") }

// ---- words that are not the router's ----

// oldWords are words the router had and gave up, each with what is written now (KITWORK_ROUTER.md mục 3,
// mục 11): writing one is an error that says the new spelling, not one that only says "no such word".
var oldWords = map[string]string{
	"ratelimit":   "router.limit({ rate, per, by })",
	"title":       "router.meta({ title, description, image })",
	"description": "router.meta({ title, description, image })",
	"image":       "router.meta({ title, description, image })",
	"url":         "router.meta({ … })",
	"type":        "router.meta({ … })",
	"error":       "error.ui, the nearest up the folders",
	"notfound":    "notfound.ui, the nearest up the folders",
	"headers":     "context.header(…) on the answer",
	"redirect":    "a folder whose router.get answers context.status(301).redirect(\"/new\")",
	"success":     "the handler itself",
	"finally":     "the handler itself",
	"then":        "the handler itself",
	"catch":       "the handler itself",
	"middleware":  "router.guard(…).methods(\"*\")",
	// the app's zone is the app root's, declared in its module.work (17/11/2026, docs/module.md)
	"timezone": "module.timezone(\"Asia/Ho_Chi_Minh\") in module.work, the app's root",
	"drive":    "router.reload(false)",
	"theme":    "design.colors(…) and design.fonts(…) in the folder's design.work",
	"css":      "design.colors(…) and design.fonts(…) in the folder's design.work",
	"jitcss":   "design.colors(…) and design.fonts(…) in the folder's design.work",
	// the look moved to design.work (docs/KITWORK_DESIGN.md mục 1, luật 6): no two ways at once
	"colors":     "design.colors(…) in the folder's design.work",
	"fonts":      "design.fonts(…) in the folder's design.work",
	"gradients":  "design.gradients(…) in the folder's design.work",
	"highlight":  "design.highlight(…) in the folder's design.work",
	"appearance": "design.appearance(…) in the folder's design.work",
	"contrast":   "design.contrast(…) in the folder's design.work",
	"transition": "design.transition(…) in the folder's design.work",
	"connection": "connection() at the top level of a plain .work file of the app",
	"connect":    "connection() at the top level of a plain .work file of the app",
}

// MissingMember is a word the router does not have: an old word names what is written now, another
// the nearest word ("router.faviconn": did you mean router.favicon?). Both are failures, so a misspelt word
// stops the boot where it is written instead of reading as undefined.
func (r *Router) MissingMember(name string) value.Value {
	if now, ok := oldWords[strings.ToLower(name)]; ok {
		return value.Fail(value.TypeCode, "router."+name+" is an old word: write "+now)
	}
	text := "router has no word " + name
	if nearest := value.NearestName(name, value.Expose[Router]().Names()); nearest != "" {
		text += ": did you mean router." + nearest + "?"
	}
	return value.Fail(value.TypeCode, text)
}
