package router

import (
	"net/http"

	"github.com/kitwork/engine2/core/value"
)

// The context's side of the bridge (docs/view.md mục 4, docs/component.md, slice 2): a var at the top of the
// function of router.view(…) or component.define(…) is a let the run's context fills (core/compiler, var.go):
//
//	var title = "";   →   let title = context["var#"]("title", "", () => title);
//
// When the page is drawn the context gives the value written. When the browser calls a work of the page it sends
// the vars it holds, and the context of that run gives each var what the browser held (Calling), the value of its
// kind or null, never anything else. Each var's function is kept (Held), so that after the work the host reads the
// value the work left, the vars the page is run with again.

// Held is one var of a run: its name and the function that reads it.
type Held struct {
	Name string
	Read *value.Lambda
}

// Internal answers the methods the compiler writes: "var#" fills a var (core/value, method.go).
func (c *Context) Internal(name string, arguments []value.Value) value.Value {
	if name != "var#" || len(arguments) != 3 || !arguments[0].IsString() || arguments[2].Lambda() == nil {
		return value.Fail(value.TypeCode, "context has no method "+name) // what a type with no such method answers
	}
	key, initial := arguments[0].Text(), arguments[1]
	c.held = append(c.held, Held{Name: key, Read: arguments[2].Lambda()})
	sent, ok := c.sent[key]
	if !ok {
		return initial
	}
	if !initial.IsNullish() && !sent.IsNull() && sent.Kind() != initial.Kind() {
		return value.Fail(value.InvalidInputCode, "var "+key+" is a "+initial.Kind()+" and the call sent a "+sent.Kind()+
			": a var the browser sends back is of the kind it was written with, or null")
	}
	return sent
}

// Calling gives the run the vars the browser held, an object by name; nothing outside a call.
func (c *Context) Calling(vars value.Value) {
	c.sent = map[string]value.Value{}
	if !vars.IsObject() {
		return
	}
	for _, key := range vars.Keys() {
		c.sent[key] = vars.Get(key)
	}
}

// Held is the vars the run declared, in the order it did.
func (c *Context) Held() []Held { return c.held }

// Reopen sets aside the answer of the run, for a work called on what it bound to answer its own: the work runs with
// the context its function closes over, and answers a redirect, a notFound, an error or nothing. The cookies set
// before stay, and go with whatever is answered.
func (c *Context) Reopen() {
	was := c.response
	c.response = newResponse(was.host)
	c.response.owner, c.response.method, c.response.secure, c.response.cookies = c, was.method, was.secure, was.cookies
}

// Live is the names of the sync before the answer that are data, not works: what the server keeps live for the tab
// that shows them (engine2/docs/view.md mục 2.3), in the order written.
func (r *Response) Live() []string {
	if !r.synced.IsObject() {
		return nil
	}
	var live []string
	for _, key := range r.synced.Keys() {
		if !r.synced.Get(key).IsFunction() {
			live = append(live, key)
		}
	}
	return live
}

// Location is where a redirect leads.
func (r *Response) Location() string { return r.header.Get("Location") }

// Answer is the status of an answer and the text it wrote: an error's message, for the visitor.
func (r *Response) Answer() (int, string) { return r.status, string(r.body) }

// Cookies are the cookies the answer sets or removes, in the order written.
func (c *Context) Cookies() []*http.Cookie { return c.response.cookies }

// WriteCookies adds them to header.
func (c *Context) WriteCookies(header http.Header) { c.response.writeCookies(header) }
