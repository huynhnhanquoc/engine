package router

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// The answer that draws a page of router.view (docs/view.md mục 2) and a component's (docs/component.md mục 3):
//
//	return context.title("Sản phẩm").sync({ products, remove }).bind({ query, startEdit });
//
// sync({ … }) is an edit before the answer: the names the server computes and keeps; bind({ … }) is the answer, the
// names the browser holds. Each is an object of names; a name is one or the other. The page draws with both (slice
// 1: on the server; the browser's half, the works it calls and the data it keeps live, come later).

// Sync is context.sync({ … }): the names the server keeps, before bind({ … }).
func (r *Response) Sync(arguments ...value.Value) value.Value {
	if failure := r.editing("sync"); failure.IsInvalid() {
		return failure
	}
	if r.synced.IsObject() {
		return value.Fail(value.TypeCode, "context.sync(…) twice: one sync({ … }) holds every name the server keeps, before bind({ … })")
	}
	names := argument(arguments, 0)
	if len(arguments) != 1 || !names.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "context.sync(…) takes one object of names, the data and the works the server keeps: context.sync({ products, remove }).bind({ query })")
	}
	r.synced = names
	return r.self()
}

// Bind is context.bind({ … }): the answer that draws the page with the names of bind and of the sync before it.
func (r *Response) Bind(arguments ...value.Value) value.Value {
	if failure := r.answering("bind", true); failure.IsInvalid() {
		return failure
	}
	names := argument(arguments, 0)
	if len(arguments) != 1 || !names.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "context.bind(…) takes one object of the names the page reads: context.bind({ query, startEdit })")
	}
	merged := value.Object()
	if r.synced.IsObject() {
		for _, key := range r.synced.Keys() {
			merged.Set(key, r.synced.Get(key))
		}
	}
	for _, key := range names.Keys() {
		if r.synced.IsObject() && r.synced.Has(key) {
			return value.Fail(value.InvalidArgumentCode, "context.sync({ … }) and bind({ … }) both name "+key+": a name is the server's (sync) or the browser's (bind)")
		}
		merged.Set(key, names.Get(key))
	}
	r.data = merged
	return r.give("bind", htmlType, nil)
}

// Bound is what a bind answer gives the host to draw: the names, the head. ok is false for any other answer.
func (r *Response) Bound() (names value.Value, title, description string, ok bool) {
	return r.data, r.title, r.description, r.answer == "bind"
}

// Extra is the first edit of an answer that a component's run does not carry (a status, a header, a cookie, the
// page's title), "" when it has none: a component's answer is the names it binds.
func (r *Response) Extra() string {
	switch {
	case r.status != 0:
		return "status"
	case len(r.header) > 0:
		return "header"
	case len(r.cookies) > 0:
		return "cookie"
	case r.page != "":
		return r.page // title, description and the page's other words
	}
	return ""
}

// Word is the answer given ("json", "bind", "redirect" …), "" before one.
func (r *Response) Word() string { return r.answer }

// Cookie is context.cookie(name, value, options?): a cookie set with the answer (docs/router.md mục 6), HttpOnly,
// SameSite=Lax, Path=/ and, on HTTPS, Secure unless options say otherwise; a value null removes it. A cookie the
// browser sent is read with context.cookie(name), one argument (Context.Cookie).
func (r *Response) Cookie(arguments ...value.Value) value.Value {
	if failure := r.editing("cookie"); failure.IsInvalid() {
		return failure
	}
	if len(arguments) < 2 || len(arguments) > 3 {
		return value.Fail(value.InvalidArgumentCode, "context.cookie(name) reads a cookie the browser sent, context.cookie(name, value, options?) sets one with the answer")
	}
	name := arguments[0]
	if !name.IsString() || !isToken(name.Text()) {
		return value.Fail(value.InvalidArgumentCode, "context.cookie("+name.ToString().Text()+", …): a cookie's name is letters, digits and !#$%&'*+-.^_`|~")
	}
	cookie := &http.Cookie{Name: name.Text(), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.secure}
	switch item := arguments[1]; {
	case item.IsNull():
		cookie.MaxAge = -1 // removed
	case item.IsString() || item.IsNumber():
		text := item.ToString().Text()
		if !cookieValue(text) {
			return value.Fail(value.InvalidArgumentCode, "context.cookie("+strconv.Quote(cookie.Name)+", …): a cookie's value is ASCII letters, digits and !#$%&'()*+-./:<=>?@[]^_`{|}~, no space, comma, semicolon, quote or backslash: encode it first, encodeURIComponent(value)")
		}
		cookie.Value = text
	default:
		return value.Fail(value.InvalidArgumentCode, "context.cookie("+strconv.Quote(cookie.Name)+", …): the value is a string or a number, or null to remove the cookie; "+item.Kind()+" here")
	}
	if len(arguments) == 3 {
		if failure := cookieOptions(cookie, arguments[2]); failure.IsInvalid() {
			return failure
		}
	}
	if cookie.SameSite == http.SameSiteNoneMode && !cookie.Secure {
		return value.Fail(value.InvalidArgumentCode, "context.cookie("+strconv.Quote(cookie.Name)+", …): sameSite \"none\" is sent by a browser only with secure: true")
	}
	for i, set := range r.cookies {
		if set.Name == cookie.Name && set.Path == cookie.Path && set.Domain == cookie.Domain {
			r.cookies[i] = cookie // the last word of one cookie
			return r.self()
		}
	}
	r.cookies = append(r.cookies, cookie)
	return r.self()
}

// cookieOptions reads { maxAge, path, domain, secure, httpOnly, sameSite } into cookie.
func cookieOptions(cookie *http.Cookie, options value.Value) value.Value {
	refuse := func(why string) value.Value {
		return value.Fail(value.InvalidArgumentCode, "context.cookie("+strconv.Quote(cookie.Name)+", value, { maxAge, path, domain, secure, httpOnly, sameSite }): "+why)
	}
	if !options.IsObject() {
		return refuse("the options are an object, " + options.Kind() + " here")
	}
	for _, key := range options.Keys() {
		item := options.Get(key)
		switch key {
		case "maxAge":
			span, ok := time.Duration(0), false
			if item.IsString() {
				span, ok = readSpan(item.Text())
			}
			if !ok || span < time.Second {
				return refuse("maxAge is a span of a second at least, \"7d\", \"2h\"")
			}
			if cookie.MaxAge >= 0 {
				cookie.MaxAge = int(span / time.Second)
			}
		case "path":
			if !item.IsString() || !strings.HasPrefix(item.Text(), "/") || !cookieValue(item.Text()) {
				return refuse("path is a path of the site, \"/\", \"/account\"")
			}
			cookie.Path = item.Text()
		case "domain":
			if !item.IsString() || item.Text() == "" || !cookieValue(item.Text()) {
				return refuse("domain is a host name, \"shop.com\"")
			}
			cookie.Domain = item.Text()
		case "secure", "httpOnly":
			if !item.IsBoolean() {
				return refuse(key + " is true or false")
			}
			if key == "secure" {
				cookie.Secure = item.ToBoolean()
			} else {
				cookie.HttpOnly = item.ToBoolean()
			}
		case "sameSite":
			switch item.Text() {
			case "lax":
				cookie.SameSite = http.SameSiteLaxMode
			case "strict":
				cookie.SameSite = http.SameSiteStrictMode
			case "none":
				cookie.SameSite = http.SameSiteNoneMode
			default:
				return refuse("sameSite is \"lax\", \"strict\" or \"none\"")
			}
		default:
			return refuse("the key " + strconv.Quote(key) + " is not one of them")
		}
	}
	return value.Value{}
}

// cookieValue reports a text a cookie carries as it is (RFC 6265 cookie-octet).
func cookieValue(text string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c < 0x21 || c > 0x7e || c == '"' || c == ',' || c == ';' || c == '\\' {
			return false
		}
	}
	return true
}

// writeCookies adds the answer's cookies to header.
func (r *Response) writeCookies(header http.Header) {
	for _, cookie := range r.cookies {
		if line := cookie.String(); line != "" {
			header.Add("Set-Cookie", line)
		}
	}
}

// Attributes is context.attributes(), a component's: the inputs written on its tag, read where it stands
// (docs/component.md mục 3.1).
func (c *Context) Attributes(arguments ...value.Value) value.Value {
	if c.attributes == nil {
		return value.Fail(value.TypeCode, "context.attributes() is a component's, the inputs written on its tag: const { orderId } = context.attributes() in the function of component.define(…)")
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, "context.attributes() takes nothing: const { orderId, compact = false } = context.attributes()")
	}
	return *c.attributes
}

// NewComponentContext is the context of one run of a component's function for request r: the inputs its tag
// writes, its own answer.
func NewComponentContext(r *http.Request, attributes value.Value) *Context {
	c := NewContext(r, nil)
	c.attributes = &attributes
	return c
}

// Component reports a component's context.
func (c *Context) Component() bool { return c.attributes != nil }
