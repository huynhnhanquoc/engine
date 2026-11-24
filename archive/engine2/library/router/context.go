package router

import (
	"net/http"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/file"
)

// Context is what a handler receives, context (docs/KITWORK_ROUTER.md mục 5): context.request and
// context.response are two real structs, and context.json(…), context.status(…), context.params(…) are
// shortcuts to them, the same implementation (a word is never on one way only). It is made for one
// request and dropped after it.
type Context struct {
	request  *Request
	response *Response
	// guarding is true while a guard runs (rules.go): context.next() is its, and context.error(…) refuses with 403.
	guarding bool
	// events are what the request observes, which end with it (stream.go)
	events events
	// attributes are the inputs of a component's tag, nil outside a component's run (bind.go)
	attributes *value.Value
	// held are the vars the run declared, sent what the browser held when the run is a call's (bridge.go)
	held []Held
	sent map[string]value.Value
	// Files are the files of the request's app, what context.file("…") serves a path of (docs/KITWORK_FILE.md mục 9):
	// the host sets them; nil serves none.
	Files *file.File
}

// NewContext is the context of r, with the values of its route's parameters.
func NewContext(r *http.Request, params map[string]value.Value) *Context {
	c := &Context{request: NewRequest(r, params), response: newResponse(r.Host)}
	c.response.owner, c.response.method, c.response.secure = c, r.Method, r.TLS != nil
	return c
}

// Answer is the response the handler built when result, what the handler returned, is it with an
// answer given: a handler answers by returning its answer, return context.json(…).
func (c *Context) Answer(result value.Value) (*Response, bool) {
	if response, ok := result.Host().(*Response); ok && response == c.response && response.Answered() {
		return response, true
	}
	return nil, false
}

// BodyTooLarge reports that the handler read a body past BodyLimit.
func (c *Context) BodyTooLarge() bool { return c.request.TooLarge() }

// RequestBound: the context belongs to its request; a routine holds none (runtime Snapshot).
func (c *Context) RequestBound() {}

// Guarding marks the run of a guard, on or off: context.next() is a guard's, and context.error(…) in a guard
// refuses with 403 (docs/router.md mục 5).
func (c *Context) Guarding(on bool) {
	c.guarding = on
	c.response.guarding = on
}

// Viewing marks the run of router.view's function, which draws its page (docs/view.md mục 2): context.sse in it is a
// failure, a stream being router.get's (Response.Sse).
func (c *Context) Viewing() { c.response.viewing = true }

// LetOn reports that result, what a guard returned, is context.next(): the request goes on. A guard's context is its
// request's alone, and so is what its next() gives (a routine holds neither, runtime Snapshot).
func (c *Context) LetOn(result value.Value) bool {
	_, ok := result.Host().(*letOn)
	return ok
}

// letOn is what context.next() gives: the guard that returns it lets the request on to the next rule or its door.
type letOn struct{ _ byte }

// RequestBound: it belongs to its request, as its context does.
func (*letOn) RequestBound() {}

// Next is context.next(), a guard's: return context.next() lets the request on.
func (c *Context) Next(arguments ...value.Value) value.Value {
	if !c.guarding {
		return value.Fail(value.TypeCode, "context.next() is a guard's, return context.next() in the function of router.guard(…): a door's function answers")
	}
	if len(arguments) > 0 {
		return value.Fail(value.InvalidArgumentCode, "context.next() takes nothing: it lets the request on as it is")
	}
	return value.Struct(&letOn{})
}

func (c *Context) Request() value.Value  { return value.Struct(c.request) }
func (c *Context) Response() value.Value { return value.Struct(c.response) }

// The request's shortcuts.

func (c *Context) Params(arguments ...value.Value) value.Value { return c.request.Params(arguments...) }
func (c *Context) Query(arguments ...value.Value) value.Value  { return c.request.Query(arguments...) }

// The response's edits and answers.

func (c *Context) Status(arguments ...value.Value) value.Value {
	return c.response.Status(arguments...)
}
func (c *Context) Type(arguments ...value.Value) value.Value { return c.response.Type(arguments...) }
func (c *Context) Header(arguments ...value.Value) value.Value {
	return c.response.Header(arguments...)
}
func (c *Context) Headers(arguments ...value.Value) value.Value {
	return c.response.Headers(arguments...)
}

// Cookie is context.cookie(name), a cookie the browser sent ("" when none), and context.cookie(name, value,
// options?), one set with the answer (16/11/2026, the owner: "không cần context.request.cookie(), nhanh bằng
// context.cookie luôn"); one word for the cookie, its number of arguments saying read or set.
func (c *Context) Cookie(arguments ...value.Value) value.Value {
	if len(arguments) == 1 {
		return c.request.cookie(arguments...)
	}
	return c.response.Cookie(arguments...)
}
func (c *Context) Title(arguments ...value.Value) value.Value { return c.response.Title(arguments...) }
func (c *Context) Description(arguments ...value.Value) value.Value {
	return c.response.Description(arguments...)
}
func (c *Context) Image(arguments ...value.Value) value.Value { return c.response.Image(arguments...) }
func (c *Context) Canonical(arguments ...value.Value) value.Value {
	return c.response.Canonical(arguments...)
}
func (c *Context) Robots(arguments ...value.Value) value.Value {
	return c.response.Robots(arguments...)
}
func (c *Context) Meta(arguments ...value.Value) value.Value { return c.response.Meta(arguments...) }

// text, html, svg and json are given the budget of the run that answers (value.Budget): an array's text and the
// JSON are made within it
func (c *Context) Text(budget value.Budget, arguments ...value.Value) value.Value {
	return c.response.Text(budget, arguments...)
}
func (c *Context) Html(budget value.Budget, arguments ...value.Value) value.Value {
	return c.response.Html(budget, arguments...)
}
func (c *Context) Svg(budget value.Budget, arguments ...value.Value) value.Value {
	return c.response.Svg(budget, arguments...)
}
func (c *Context) Json(budget value.Budget, arguments ...value.Value) value.Value {
	return c.response.Json(budget, arguments...)
}
func (c *Context) Redirect(arguments ...value.Value) value.Value {
	return c.response.Redirect(arguments...)
}
func (c *Context) NotFound(arguments ...value.Value) value.Value {
	return c.response.NotFound(arguments...)
}
func (c *Context) Error(arguments ...value.Value) value.Value { return c.response.Error(arguments...) }
func (c *Context) View(arguments ...value.Value) value.Value  { return c.response.View(arguments...) }
func (c *Context) File(arguments ...value.Value) value.Value  { return c.response.File(arguments...) }
func (c *Context) Sse() value.Value                           { return c.response.Sse() }
func (c *Context) Bind(arguments ...value.Value) value.Value  { return c.response.Bind(arguments...) }
func (c *Context) Sync(arguments ...value.Value) value.Value  { return c.response.Sync(arguments...) }
