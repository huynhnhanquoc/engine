package host

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Context của một request ở cửa website (docs/host/request.go, decision.go; docs/KITWORK_APPLICATION.md mục 5), 24/11/2026.
// Guard nhận context có request mà chưa có response, và nói một trong ba điều: context.next(…), context.error(lý do),
// context.redirect(url), có thể sau context.status(n). Các hàm sau khi gửi (success, error, finally) nhận cả request lẫn
// response, và không quyết định gì. Ba struct dưới trả lời khoá và chữ của chúng thẳng (runtime.Direct), không qua
// reflect: một request đọc chúng hàng chục lần.

// requestKeys là các khoá có sẵn của request: context.next không đặt lại được khoá nào trong số này.
var requestKeys = []string{"method", "url", "path", "search", "query", "header", "cookies", "ip", "hostname", "secure"}

// request là request như code của host đọc. Body không có: lúc guard chạy body chưa đọc.
type request struct {
	http     *http.Request
	hostname string // tên trong Host, chữ thường, không cổng
	ip       string // IP của máy gửi, không cổng; IPv4 viết như IPv4
	secure   bool   // đến qua TLS
	set      []entry
	cookies  value.Value // dựng lần đầu đọc
}

// entry là một khoá context.next({ … }) đặt, và giá trị đã chép, đã khoá.
type entry struct {
	key  string
	item value.Value
}

func newRequest(r *http.Request) *request {
	hostname := r.Host
	if host, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = host
	} else {
		hostname = strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")
	}
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if address, err := netip.ParseAddr(ip); err == nil && address.Is4In6() {
		ip = address.Unmap().String()
	}
	return &request{http: r, hostname: strings.ToLower(hostname), ip: ip, secure: r.TLS != nil}
}

// Key là request.name.
func (q *request) Key(name string) value.Value {
	switch name {
	case "method":
		return value.String(q.http.Method)
	case "url":
		scheme := "http://"
		if q.secure {
			scheme = "https://"
		}
		return value.String(scheme + q.http.Host + q.http.URL.RequestURI())
	case "path":
		return value.String(q.http.URL.Path)
	case "search":
		if q.http.URL.RawQuery == "" {
			return value.String("")
		}
		return value.String("?" + q.http.URL.RawQuery)
	case "hostname":
		return value.String(q.hostname)
	case "ip":
		return value.String(q.ip)
	case "secure":
		return value.Boolean(q.secure)
	case "cookies":
		if q.cookies.IsUndefined() {
			cookies := value.Object()
			for _, c := range q.http.Cookies() {
				if _, found := cookies.Get(c.Name); !found {
					cookies.Set(c.Name, value.String(c.Value))
				}
			}
			q.cookies = cookies.Freeze()
		}
		return q.cookies
	case "header", "query":
		return javascript.NewNative(name, 1, func(run javascript.Run, arguments []value.Value) value.Value {
			return q.Word(run, name, arguments)
		})
	}
	for _, e := range q.set {
		if e.key == name {
			return e.item
		}
	}
	return value.Fail(value.TypeCode, "request has no key %q: %s; a guard adds a key with context.next({ %s })",
		name, strings.Join(q.keys(), ", "), name)
}

// keys là các khoá của request: các khoá có sẵn, rồi các khoá guard đã đặt.
func (q *request) keys() []string {
	keys := append([]string(nil), requestKeys...)
	for _, e := range q.set {
		keys = append(keys, e.key)
	}
	return keys
}

// has là request đã có khoá name.
func (q *request) has(name string) bool {
	for _, key := range requestKeys {
		if key == name {
			return true
		}
	}
	for _, e := range q.set {
		if e.key == name {
			return true
		}
	}
	return false
}

// Word là request.name(arguments…): header(tên) là giá trị đầu của header ấy, null khi không có; query(tên) là giá trị
// đầu của tham số ấy, "" khi không có (như guard của site hôm nay).
func (q *request) Word(run javascript.Run, name string, arguments []value.Value) value.Value {
	switch name {
	case "header", "query":
		argument := value.Undefined()
		if len(arguments) > 0 {
			argument = arguments[0]
		}
		if !argument.IsString() {
			return value.Fail(value.TypeCode, "request.%s(name) takes the name as a string, got %s", name, argument.Typeof())
		}
		if name == "query" {
			return value.String(q.http.URL.Query().Get(argument.String()))
		}
		if strings.EqualFold(argument.String(), "host") { // Go đưa Host ra khỏi các header
			return value.String(q.http.Host)
		}
		if values := q.http.Header.Values(argument.String()); len(values) > 0 {
			return value.String(values[0])
		}
		return value.Null()
	}
	if item := q.Key(name); item.IsInvalid() {
		return item
	}
	return value.Fail(value.TypeCode, "request.%s is not a function", name)
}

// stage là lúc một context được dùng: mỗi lúc một bộ chữ (docs/KITWORK_APPLICATION.md mục 10, "mỗi lúc một hình
// context").
type stage int

const (
	hostGuard  stage = iota // guard của host: next, error, redirect (sang được tên khác), status 3xx hay 4xx
	hostChoice              // routes, memory, energy của website: chỉ đọc request
	siteGuard               // guard của site: next, error, redirect trong cùng host, và các câu trả lời
	siteDoor                // cửa của site: các câu trả lời, redirect trong cùng host
	afterSend               // success, error, finally: đọc request và response, không quyết định gì
)

// requestContext là context của một request ở một lúc (stage): request, các chữ lúc ấy được dùng, và response sau khi
// gửi.
type requestContext struct {
	request  *request
	response value.Value
	stage    stage
}

// contextWords là các chữ của context; contextKeys cho lời báo.
var contextWords = []string{"next", "status", "error", "redirect", "text", "json"}

const contextKeys = "request, response, next, status, error, redirect, text, json"

// Key là context.name.
func (c *requestContext) Key(name string) value.Value {
	switch {
	case name == "request":
		return value.Struct(c.request)
	case name == "response":
		if c.stage != afterSend {
			return value.Fail(value.TypeCode, "context.response is read after the response is sent, in success, error and finally: a guard decides it")
		}
		return c.response
	case slices.Contains(contextWords, name):
		return javascript.NewNative(name, 1, func(run javascript.Run, arguments []value.Value) value.Value {
			return c.Word(run, name, arguments)
		})
	}
	return value.Fail(value.TypeCode, "context has no key %q: %s", name, contextKeys)
}

// Word là context.name(arguments…): các quyết định của guard và các câu trả lời, mỗi chữ ở lúc của nó (allows).
func (c *requestContext) Word(run javascript.Run, name string, arguments []value.Value) value.Value {
	if !slices.Contains(contextWords, name) {
		if item := c.Key(name); item.IsInvalid() {
			return item
		}
		return value.Fail(value.TypeCode, "context.%s is not a function", name)
	}
	if failure := c.allows(name); failure.IsInvalid() {
		return failure
	}
	argument := value.Undefined()
	if len(arguments) > 0 {
		argument = arguments[0]
	}
	switch name {
	case "next":
		return c.next(run, argument)
	case "status":
		if !argument.IsNumber() || argument.Number() != float64(int(argument.Number())) {
			return value.Fail(value.TypeCode, "context.status(code) takes a whole number, got %s", argument.Typeof())
		}
		n := int(argument.Number())
		if c.stage == hostGuard && (n < 300 || n > 499) {
			return value.Fail(value.RangeCode, "context.status(%d): a guard answers with 3xx then redirect(url), or 4xx then error(reason)", n)
		}
		if n < 200 || n > 599 {
			return value.Fail(value.RangeCode, "context.status(%d): a status is from 200 to 599", n)
		}
		return value.Struct(&statused{context: c, status: n})
	case "error":
		return refuse(http.StatusForbidden, argument)
	case "redirect":
		return c.redirect(http.StatusFound, argument)
	case "text":
		return answerText(http.StatusOK, argument)
	}
	return answerJSON(run, http.StatusOK, argument)
}

// allows là undefined khi chữ name được dùng lúc này, hay lỗi nói lúc nào nó được dùng.
func (c *requestContext) allows(name string) value.Value {
	switch c.stage {
	case hostGuard:
		if name == "text" || name == "json" {
			return value.Fail(value.TypeCode, "context.%s answers in a site: a guard of the host says next(), error(reason) or redirect(url)", name)
		}
	case hostChoice:
		return value.Fail(value.TypeCode, "context.%s decides a request, in a guard: routes, memory and energy only read the request", name)
	case siteDoor:
		if name == "next" || name == "error" {
			return value.Fail(value.TypeCode, "context.%s is a guard's word: a door answers with text(…), json(…) or redirect(url)", name)
		}
	case afterSend:
		return value.Fail(value.TypeCode, "context.%s decides a request, in a guard: the response is already sent", name)
	}
	return value.Value{}
}

// redirect là quyết định chuyển hướng tới url. Guard của host sang được tên khác; code của site chỉ trong cùng host: một
// đường dẫn "/…" (docs/KITWORK_APPLICATION.md mục 5).
func (c *requestContext) redirect(status int, url value.Value) value.Value {
	if c.stage != hostGuard && url.IsString() {
		u := url.String()
		if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") || strings.HasPrefix(u, "/\\") {
			return value.Fail(value.TypeCode, "context.redirect(%q): a site redirects within its host: write a path like /login", u)
		}
	}
	return redirect(status, url)
}

// next là context.next(keys): undefined là đi tiếp không đặt gì; một object là các khoá đặt vào request, mỗi khoá chưa
// có, mỗi giá trị là dữ liệu (không hàm, không struct của engine), được chép rồi khoá, để code đọc nó sau đó không sửa
// được (docs/KITWORK_APPLICATION.md mục 5). Các khoá vào request khi guard trả quyết định này (website.go).
func (c *requestContext) next(run javascript.Run, keys value.Value) value.Value {
	if keys.IsUndefined() {
		return proceed
	}
	if !keys.IsObject() {
		return value.Fail(value.TypeCode, "context.next(keys) takes an object of keys to add to the request, like next({ tenant }), got %s", keys.Typeof())
	}
	d := &decision{kind: next}
	for _, key := range keys.ReadKeys() {
		if c.request.has(key) {
			return value.Fail(value.TypeCode, "context.next({ %s }): the request already has the key %s", key, key)
		}
		item, _ := keys.Get(key)
		copied := plainCopy(run, item, key, 0)
		if copied.IsInvalid() {
			return copied
		}
		d.set = append(d.set, entry{key, copied})
	}
	return value.Struct(d)
}

// maxNextDepth là độ lồng sâu nhất của một giá trị context.next chép: sâu hơn (hay một vòng, o.self = o) là lỗi.
const maxNextDepth = 64

// plainCopy là bản chép đã khoá của v, một giá trị context.next đặt: số, chuỗi, thời điểm… là chính nó; object và mảng
// chép từng tầng rồi đóng băng (object mượn khoá của bản gốc, value.Assign); dữ liệu dùng chung (đã khoá vĩnh viễn) giữ
// nguyên. Hàm và struct của engine là lỗi: code của tenant không bao giờ nhận được hàm của host. path là chỗ của v cho
// lời báo ("tenant.owner"). Bản chép được đếm vào bộ đếm byte của lượt chạy.
func plainCopy(run javascript.Run, v value.Value, path string, depth int) value.Value {
	switch {
	case v.IsFunction():
		return value.Fail(value.TypeCode, "context.next({ %s }): %s is a function: next passes data, the tenant's code never gets a function of the host", rootOf(path), path)
	case v.IsStruct():
		return value.Fail(value.TypeCode, "context.next({ %s }): %s is a struct of the engine, not data", rootOf(path), path)
	case v.IsBytes():
		return value.Bytes(v.Bytes())
	case !v.IsObject() && !v.IsArray(), v.IsShared():
		return v
	case depth >= maxNextDepth:
		return value.Fail(value.RangeCode, "context.next({ %s }): %s nests deeper than %d (a value that holds itself?)", rootOf(path), path, maxNextDepth)
	}
	var copied value.Value
	if v.IsArray() {
		if v.HasHoles() {
			return value.Fail(value.TypeCode, "context.next({ %s }): %s has holes, Array(n) not filled: fill it first", rootOf(path), path)
		}
		items := make([]value.Value, v.Length())
		for i := range items {
			if items[i] = plainCopy(run, v.Index(i), path+"["+strconv.Itoa(i)+"]", depth+1); items[i].IsInvalid() {
				return items[i]
			}
		}
		copied = value.ArrayOf(items)
	} else {
		copied = value.Object().Assign(v, 0)
		for _, key := range copied.ReadKeys() {
			item, _ := copied.Get(key)
			inner := plainCopy(run, item, path+"."+key, depth+1)
			if inner.IsInvalid() {
				return inner
			}
			copied.Set(key, inner) // khoá đã có: chỉ đổi giá trị, khoá vẫn mượn
		}
	}
	if meter, ok := run.(javascript.Meter); ok {
		if failure := meter.Made(copied.Shallow()); failure.IsInvalid() {
			return failure
		}
	}
	return copied.Freeze()
}

// rootOf là khoá đầu của một đường dẫn: "tenant" của "tenant.owner[2]".
func rootOf(path string) string {
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}

// statused là context.status(n): nó chờ error(lý do) với một mã 4xx, redirect(url) với một mã 3xx, hay một câu trả lời
// (text, json) với một mã khác 3xx, mỗi chữ ở lúc của nó.
type statused struct {
	context *requestContext
	status  int
}

func (s *statused) Key(name string) value.Value {
	if name == "error" || name == "redirect" || name == "text" || name == "json" {
		return javascript.NewNative(name, 1, func(run javascript.Run, arguments []value.Value) value.Value {
			return s.Word(run, name, arguments)
		})
	}
	return value.Fail(value.TypeCode, "context.status(%d) has no key %q: error, redirect, text, json", s.status, name)
}

func (s *statused) Word(run javascript.Run, name string, arguments []value.Value) value.Value {
	if name != "error" && name != "redirect" && name != "text" && name != "json" {
		return s.Key(name)
	}
	if failure := s.context.allows(name); failure.IsInvalid() {
		return failure
	}
	argument := value.Undefined()
	if len(arguments) > 0 {
		argument = arguments[0]
	}
	redirecting := s.status >= 300 && s.status < 400
	switch name {
	case "error":
		if s.status < 400 || s.status > 499 {
			return value.Fail(value.RangeCode, "context.status(%d).error(…): a refusal is a 4xx", s.status)
		}
		return refuse(s.status, argument)
	case "redirect":
		if !redirecting {
			return value.Fail(value.RangeCode, "context.status(%d).redirect(…): a redirect is a 3xx", s.status)
		}
		return s.context.redirect(s.status, argument)
	}
	if redirecting {
		return value.Fail(value.RangeCode, "context.status(%d).%s(…): a 3xx is a redirect, status(%d).redirect(url)", s.status, name, s.status)
	}
	if name == "text" {
		return answerText(s.status, argument)
	}
	return answerJSON(run, s.status, argument)
}

// decisionKind là một trong các điều một guard hay một cửa nói.
type decisionKind int

const (
	next        decisionKind = iota // context.next(…): đi tiếp, có thể đặt khoá vào request
	refusal                         // context.status(4xx).error(lý do): dừng, nói lý do
	redirection                     // context.status(3xx).redirect(url): chuyển hướng
	answered                        // context.text(…), context.json(…): một câu trả lời của site
)

// decision là thứ Go đọc khi một guard hay một cửa return (docs/host/decision.go). Code không đọc được nó: chỉ trả nó.
type decision struct {
	kind        decisionKind
	status      int
	message     string // refusal: lý do; answered: thân
	url         string
	contentType string // answered
	set         []entry
}

func (*decision) Key(name string) value.Value {
	return value.Fail(value.TypeCode, "an answer has no key %q: the engine reads it, return it", name)
}

func (d *decision) Word(_ javascript.Run, name string, _ []value.Value) value.Value {
	return d.Key(name)
}

// proceed là context.next() không khoá: một quyết định dùng chung, không cấp phát mỗi guard (không ai sửa được nó).
var proceed = value.Struct(&decision{kind: next})

// refuse là quyết định từ chối với lý do reason, một chuỗi không rỗng: lý do tới khách và tới response.error.
func refuse(status int, reason value.Value) value.Value {
	if !reason.IsString() || reason.String() == "" {
		return value.Fail(value.TypeCode, "context.error(reason) takes the reason as a string, said to the client, got %s", describeArgument(reason))
	}
	return value.Struct(&decision{kind: refusal, status: status, message: reason.String()})
}

// redirect là quyết định chuyển hướng tới url: một chuỗi không rỗng, không ký tự điều khiển (một dòng mới trong
// Location là chèn header).
func redirect(status int, url value.Value) value.Value {
	if !url.IsString() || url.String() == "" {
		return value.Fail(value.TypeCode, "context.redirect(url) takes the address as a string, got %s", describeArgument(url))
	}
	if strings.ContainsFunc(url.String(), func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return value.Fail(value.TypeCode, "context.redirect(url): the address has a control character (a new line?)")
	}
	return value.Struct(&decision{kind: redirection, status: status, url: url.String()})
}

// answerText là context.text(chữ): một chuỗi, gửi nguyên, text/plain.
func answerText(status int, body value.Value) value.Value {
	if !body.IsString() {
		return value.Fail(value.TypeCode, "context.text(source) takes a string, got %s", body.Typeof())
	}
	return value.Struct(&decision{kind: answered, status: status, message: body.String(), contentType: "text/plain; charset=utf-8"})
}

// answerJSON là context.json(data): data viết thành JSON như JSON.stringify, application/json. Thứ không có JSON
// (undefined, một hàm) là lỗi.
func answerJSON(run javascript.Run, status int, data value.Value) value.Value {
	json, _ := javascript.Global("JSON")
	text := javascript.Invoke(run, json, "stringify", []value.Value{data})
	if text.IsInvalid() {
		return text
	}
	if !text.IsString() {
		return value.Fail(value.TypeCode, "context.json(data): %s has no JSON: write an object, an array, a string, a number, a boolean or null", data.Typeof())
	}
	return value.Struct(&decision{kind: answered, status: status, message: text.String(), contentType: "application/json; charset=utf-8"})
}

// describeArgument là một đối số cho lời báo: "an empty string", hay loại của nó.
func describeArgument(v value.Value) string {
	if v.IsString() {
		return "an empty string"
	}
	return v.Typeof()
}
