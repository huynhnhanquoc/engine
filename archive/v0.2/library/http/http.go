// Package http is "kitwork/http": a request out of the app, described then sent (engine2/docs/http.md).
//
//	import * as http from "kitwork/http";
//	const response = http.post("https://api.example.com/orders").bearer(token).timeout("5s").retry(2)
//	  .header("Idempotency-Key", key).json(order).send();
//	if (!response.ok) return context.status(502).json({ code: response.code });
//	return context.json(response.data);
//
// A method describes, each step returns a changed copy, .send() alone goes out, each time it is called. .send() never
// fails for the network or for the status the server answered: its result is { ok, status, code, error, data, text,
// headers } whatever happened, ok on a 2xx whose body was read. A program's own mistake (a word of no meaning, a body
// on a GET) is a failure that rises.
package http

import (
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// Keeper keeps the answers of .persist() on the disk of the app's machine (kitwork/storage's disk(), the host gives
// it): Load an answer kept under key while it lasts, Save one for ttl.
type Keeper interface {
	Load(key string) ([]byte, bool)
	Save(key string, data []byte, ttl time.Duration)
}

// Module is what a file imports.
type Module struct {
	// Open says the file is still running as the site boots: a request is sent while a request runs
	Open func() bool
	// Cache keeps the site's answers of .cache(); nil keeps none
	Cache *Cache
	// Persist keeps the answers of .persist() on disk; nil keeps none
	Persist Keeper
}

// Freeze is what value.Freeze calls on what a file made as the site boots: a Module keeps nothing a program writes.
func (m *Module) Freeze() {}

func (m *Module) Get(arguments ...value.Value) value.Value    { return m.describe("GET", arguments) }
func (m *Module) Post(arguments ...value.Value) value.Value   { return m.describe("POST", arguments) }
func (m *Module) Put(arguments ...value.Value) value.Value    { return m.describe("PUT", arguments) }
func (m *Module) Patch(arguments ...value.Value) value.Value  { return m.describe("PATCH", arguments) }
func (m *Module) Delete(arguments ...value.Value) value.Value { return m.describe("DELETE", arguments) }
func (m *Module) Head(arguments ...value.Value) value.Value   { return m.describe("HEAD", arguments) }

// describe is a request of method to the URL written, an http:// or https:// URL with a host (http.md mục 2.1).
func (m *Module) describe(method string, arguments []value.Value) value.Value {
	name := strings.ToLower(method)
	if len(arguments) == 0 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, "http."+name+"(…) takes a URL: http."+name+"(\"https://…\")")
	}
	if len(arguments) > 1 {
		return value.Fail(value.InvalidArgumentCode, "http."+name+"(…) takes the URL only: its parameters are .query({ … }), its headers .header(name, value), its body .json(…), .form(…) or .text(…)")
	}
	target := strings.TrimSpace(arguments[0].Text())
	if parsed, err := neturl.Parse(target); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return value.Fail(value.InvalidArgumentCode, "http."+name+"(\""+shownURL(target)+"\"): an http:// or https:// URL with a host")
	}
	return value.Struct(&Request{module: m, method: method, target: target, header: http.Header{}, timeout: defaultTimeout, maxBytes: defaultMaxBytes})
}

// MissingMember is a word http does not have: a step written before the method, a way to send other than .send().
func (m *Module) MissingMember(name string) value.Value {
	switch strings.ToLower(name) {
	case "cache", "persist", "retry", "timeout", "header", "headers", "maxbytes", "query", "json", "form", "text", "bearer", "basic":
		return value.Fail(value.TypeCode, "http."+name+" is no word of http: a step follows the method, http.get(url)."+name+"(…).send()")
	case "send", "fetch", "request", "execute":
		return value.Fail(value.TypeCode, "http."+name+" is no word of http: the method describes, .send() sends, http.get(url).send()")
	}
	return value.Fail(value.TypeCode, "http has no word "+name+": http.get, post, put, patch, delete, head(url)")
}

// bodyless says a request of method carries no body.
func bodyless(method string) bool { return method == "GET" || method == "HEAD" }
