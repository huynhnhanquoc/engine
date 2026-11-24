package router

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/file"
)

// Response is the answer a handler builds (docs/KITWORK_ROUTER.md mục 6): edits first, then one answer,
// which the handler returns. context.json(…) and response.json(…) are this one implementation.
type Response struct {
	status int
	header http.Header
	// mediaType is what .type(…) set; "" leaves it to the answer.
	mediaType string
	body      []byte
	// answer is the answer given ("json", "text", …), "" before one: an edit or a second answer after it
	// is a failure.
	answer string
	// page is the first page word written (title, description…), which only a page's answers read.
	page string
	// title and description are context.title(…) and context.description(…): the head of the page a view
	// draws (Page).
	title, description string
	// data is what context.view(…) gives its page: the host draws the page with it (library/ui).
	data value.Value
	// host is the request's Host, the one place a redirect may lead without saying so.
	host string
	// guarding is true while a guard runs: its error(…) refuses with 403, or the 4xx .status(…) set.
	guarding bool
	// viewing is true while router.view's function runs: context.sse in it is a failure (stream.go).
	viewing bool
	// owner is the request's context, method its method: context.sse is GET's, and its stream the request's
	owner  *Context
	method string
	// stream is context.sse once read (stream.go)
	stream *Stream
	// synced is context.sync({ … }) before bind (bind.go); cookies are context.cookie(…), secure that the request
	// came over HTTPS, where a cookie is Secure unless it says otherwise
	synced  value.Value
	cookies []*http.Cookie
	secure  bool
	// served is the file context.file(…) answers with, served from the disk as the answer is written
	served *file.Handle
}

// newResponse is the empty answer of a request to host, whose context then owns it.
func newResponse(host string) *Response {
	return &Response{header: http.Header{}, host: host}
}

// RequestBound: the response belongs to its request; a routine holds none (runtime Snapshot).
func (r *Response) RequestBound() {}

// Answered reports that the handler gave an answer.
func (r *Response) Answered() bool { return r.answer != "" }

// Write sends the answer to request: the status, the headers, the body unless it is a HEAD (which runs as GET without
// it); a file of context.file(…) as the system serves it, with its Range, ETag and 304.
func (r *Response) Write(w http.ResponseWriter, request *http.Request) {
	head := request.Method == http.MethodHead
	header := w.Header()
	for name, values := range r.header {
		header[name] = values
	}
	if r.mediaType != "" {
		header.Set("Content-Type", r.mediaType)
	}
	r.writeCookies(header)
	if r.served != nil {
		r.served.Serve(w, request, r.mediaType)
		return
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}
	header.Set("Content-Length", strconv.Itoa(len(r.body)))
	w.WriteHeader(status)
	if !head {
		w.Write(r.body)
	}
}

func (r *Response) self() value.Value { return value.Struct(r) }

// editing is the failure of an edit after the answer, or undefined.
func (r *Response) editing(word string) value.Value {
	if r.answer != "" {
		return value.Fail(value.TypeCode, word+"(…) after "+r.answer+"(…): every edit of an answer stands before it, context."+word+"(…)."+r.answer+"(…)")
	}
	return value.Value{}
}

// ---- edits ----

// Status is .status(code): a whole number from 200 to 599.
func (r *Response) Status(arguments ...value.Value) value.Value {
	if failure := r.editing("status"); failure.IsInvalid() {
		return failure
	}
	code := argument(arguments, 0)
	number := code.Number()
	if !code.IsNumber() || number != float64(int(number)) || number < 200 || number > 599 {
		return value.Fail(value.RangeCode, "status("+code.ToString().Text()+"): a status is a whole number from 200 to 599")
	}
	r.status = int(number)
	return r.self()
}

// Type is .type(mediaType): the Content-Type, which an answer's own then does not replace.
func (r *Response) Type(arguments ...value.Value) value.Value {
	if failure := r.editing("type"); failure.IsInvalid() {
		return failure
	}
	media := argument(arguments, 0)
	if !media.IsString() || media.Text() == "" || strings.ContainsAny(media.Text(), "\r\n\x00") {
		return value.Fail(value.InvalidArgumentCode, "type(…) takes a media type, \"image/svg+xml\"")
	}
	r.mediaType = media.Text()
	return r.self()
}

// headerNames the engine writes itself: a handler sets them through the words that mean them.
var headerNames = map[string]string{
	"Content-Type":      "context.type(…)",
	"Content-Length":    "the engine, from the body",
	"Transfer-Encoding": "the engine",
	"Set-Cookie":        "context.cookie(…)",
}

// Header is .header(name, value): one header, replacing one of the same name.
func (r *Response) Header(arguments ...value.Value) value.Value {
	if failure := r.editing("header"); failure.IsInvalid() {
		return failure
	}
	return r.setHeader(argument(arguments, 0), argument(arguments, 1))
}

func (r *Response) setHeader(name, item value.Value) value.Value {
	if !name.IsString() || !isToken(name.Text()) {
		return value.Fail(value.InvalidArgumentCode, "header(name, value): "+strconv.Quote(name.ToString().Text())+" is not a header name")
	}
	canonical := http.CanonicalHeaderKey(name.Text())
	if by, ok := headerNames[canonical]; ok {
		return value.Fail(value.InvalidArgumentCode, "header("+strconv.Quote(canonical)+", …): this header is written by "+by)
	}
	text, failure := item.ToText()
	if failure.IsInvalid() {
		return failure // a value whose own toString fails, or a site's boot made, sets no header
	}
	if strings.ContainsAny(text, "\r\n\x00") {
		return value.Fail(value.InvalidArgumentCode, "header("+strconv.Quote(canonical)+", …): a value holds no line break")
	}
	r.header.Set(canonical, text)
	return r.self()
}

// Headers is .headers({ name: value, … }): each a header.
func (r *Response) Headers(arguments ...value.Value) value.Value {
	if failure := r.editing("headers"); failure.IsInvalid() {
		return failure
	}
	all := argument(arguments, 0)
	if !all.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "headers(…) takes an object of headers, { \"Cache-Control\": \"no-store\" }")
	}
	for _, name := range all.Keys() {
		if failure := r.setHeader(value.String(name), all.Get(name)); failure.IsInvalid() {
			return failure
		}
	}
	return r.self()
}

// isToken reports a header name as HTTP writes one: one or more token characters.
func isToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0) {
			return false
		}
	}
	return true
}

// pageWord is a word of a page (title, description, image, canonical, robots, meta): it means something
// only to an answer that draws a page, so it is kept until then.
func (r *Response) pageWord(word string) value.Value {
	if failure := r.editing(word); failure.IsInvalid() {
		return failure
	}
	if r.page == "" {
		r.page = word
	}
	return r.self()
}

func (r *Response) Title(arguments ...value.Value) value.Value {
	return r.pageText("title", &r.title, arguments)
}
func (r *Response) Description(arguments ...value.Value) value.Value {
	return r.pageText("description", &r.description, arguments)
}

// pageText is a page word that keeps its text for the page's head: what cannot be made text is the
// conversion's failure, as every answer's text is.
func (r *Response) pageText(word string, into *string, arguments []value.Value) value.Value {
	if failure := r.pageWord(word); failure.IsInvalid() {
		return failure
	}
	text, failure := argument(arguments, 0).ToText()
	if failure.IsInvalid() {
		return failure
	}
	*into = text
	return r.self()
}
func (r *Response) Image(...value.Value) value.Value     { return r.pageWord("image") }
func (r *Response) Canonical(...value.Value) value.Value { return r.pageWord("canonical") }
func (r *Response) Robots(...value.Value) value.Value    { return r.pageWord("robots") }
func (r *Response) Meta(...value.Value) value.Value      { return r.pageWord("meta") }

// ---- answers ----

// answering checks that word may answer now: no answer yet, and no page word unless a page answers.
func (r *Response) answering(word string, page bool) value.Value {
	if r.answer != "" {
		return value.Fail(value.TypeCode, word+"(…) after "+r.answer+"(…): one answer per response")
	}
	if r.page != "" && !page {
		return value.Fail(value.TypeCode, "context."+r.page+"(…) before "+word+"(…): a page word means something only to bind, view, notFound and error")
	}
	if r.synced.IsObject() && !page {
		return value.Fail(value.TypeCode, "context.sync(…) before "+word+"(…): sync names what the page of bind keeps, context.sync({ … }).bind({ … })")
	}
	return value.Value{}
}

// give records the answer word with its body and the media type it has unless .type(…) said another.
func (r *Response) give(word, mediaType string, body []byte) value.Value {
	r.answer = word
	if r.mediaType == "" {
		r.mediaType = mediaType
	}
	r.body = body
	return r.self()
}

const (
	textType = "text/plain; charset=utf-8"
	jsonType = "application/json; charset=utf-8"
	htmlType = "text/html; charset=utf-8"
	svgType  = "image/svg+xml; charset=utf-8"
)

// Text is .text(source): the text as it is.
func (r *Response) Text(budget value.Budget, arguments ...value.Value) value.Value {
	if failure := r.answering("text", false); failure.IsInvalid() {
		return failure
	}
	return r.giveText("text", textType, argument(arguments, 0), budget)
}

// giveText answers word with the text of source, or is the failure of making it text: an object whose
// own toString fails, or one a site's boot made, whose toString a request cannot run (05/11/2026: read as
// "", so context.text(page) answered 200 with an empty body). An array's text is made within the budget of
// the run that answers (value.Budget, 14/11/2026).
func (r *Response) giveText(word, mediaType string, source value.Value, budget value.Budget) value.Value {
	text := source.TextWithin(budget)
	if text.IsInvalid() {
		return text
	}
	return r.give(word, mediaType, []byte(text.Text()))
}

// Html is .html(source): HTML the handler made, sent as it is (not cleaned: KITWORK_ROUTER.md mục 6).
func (r *Response) Html(budget value.Budget, arguments ...value.Value) value.Value {
	if failure := r.answering("html", false); failure.IsInvalid() {
		return failure
	}
	return r.giveText("html", htmlType, argument(arguments, 0), budget)
}

// Svg is .svg(source): SVG markup the handler has, sent as it is.
func (r *Response) Svg(budget value.Budget, arguments ...value.Value) value.Value {
	if failure := r.answering("svg", false); failure.IsInvalid() {
		return failure
	}
	return r.giveText("svg", svgType, argument(arguments, 0), budget)
}

// Json is .json(data): data written as JSON. Its media type is fixed: .type(…) before it is a failure. The
// JSON is written within the budget of the run that answers (value.Budget, 14/11/2026): it was 64 MiB, the
// most a module writes where no run counts it, whatever the site's ceiling.
func (r *Response) Json(budget value.Budget, arguments ...value.Value) value.Value {
	if failure := r.answering("json", false); failure.IsInvalid() {
		return failure
	}
	if r.mediaType != "" {
		return value.Fail(value.TypeCode, "type(…) before json(…): json is application/json; a text of another type is type(…).text(…)")
	}
	written := value.StringifyWithin(argument(arguments, 0), "", budget)
	if written.IsInvalid() {
		return written
	}
	if !written.IsString() {
		return value.Fail(value.InvalidArgumentCode, "json(…) of "+argument(arguments, 0).Kind()+": JSON has nothing to write for it")
	}
	return r.give("json", jsonType, []byte(written.Text()))
}

// Redirect is .redirect(target): 302, or the 3xx .status(…) set, to a path of this site or a URL of this
// host. A URL of another host is a failure until the way to say it is chosen (KITWORK_ROUTER.md mục 14):
// a target read from the request would be an open redirect.
func (r *Response) Redirect(arguments ...value.Value) value.Value {
	if failure := r.answering("redirect", false); failure.IsInvalid() {
		return failure
	}
	if r.status != 0 && (r.status < 300 || r.status > 399) {
		return value.Fail(value.RangeCode, "status("+strconv.Itoa(r.status)+") before redirect(…): a redirect is 3xx")
	}
	target := argument(arguments, 0)
	if !target.IsString() || !r.sameHost(target.Text()) {
		return value.Fail(value.DeniedCode, "redirect("+strconv.Quote(target.ToString().Text())+"): a redirect leads to a path of this site, \"/login\"; another host is not written yet")
	}
	if r.status == 0 {
		r.status = http.StatusFound
	}
	r.header.Set("Location", target.Text())
	return r.give("redirect", "", nil)
}

func (r *Response) sameHost(target string) bool {
	if strings.ContainsAny(target, "\r\n\x00\\") {
		return false
	}
	if strings.HasPrefix(target, "/") {
		return !strings.HasPrefix(target, "//")
	}
	parsed, err := url.Parse(target)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(parsed.Host, r.host)
}

// NotFound is .notFound(): 404. The nearest notfound.ui draws it once .ui pages are served; until then
// it answers in text.
func (r *Response) NotFound(...value.Value) value.Value {
	if failure := r.answering("notFound", true); failure.IsInvalid() {
		return failure
	}
	r.status = http.StatusNotFound
	r.noStore()
	return r.give("notFound", textType, []byte("Not Found"))
}

// Error is .error(message): 500, or the 4xx or 5xx .status(…) set, with the message, which is for the
// visitor; in a guard 403, or the 4xx .status(…) set (docs/router.md mục 5). The nearest error.ui draws it once
// .ui pages are served.
func (r *Response) Error(arguments ...value.Value) value.Value {
	if failure := r.answering("error", true); failure.IsInvalid() {
		return failure
	}
	status := r.status
	if status == 0 {
		status = http.StatusInternalServerError
		if r.guarding {
			status = http.StatusForbidden
		}
	}
	if status < 400 {
		return value.Fail(value.RangeCode, "status("+strconv.Itoa(status)+") before error(…): an error is 4xx or 5xx")
	}
	if r.guarding && status >= 500 {
		return value.Fail(value.RangeCode, "status("+strconv.Itoa(status)+") before error(…) in a guard: a guard refuses with 403, or the 4xx context.status(401) sets; a 5xx is a failure of the site")
	}
	message := argument(arguments, 0)
	text := http.StatusText(status)
	if !message.IsNullish() {
		var failure value.Value
		if text, failure = message.ToText(); failure.IsInvalid() {
			// as giveText, and the response as it was: an answer that fails edits nothing, so the handler's
			// next answer is its own (05/11/2026: the 500 was written first and stayed, so a text after a
			// failed error went out as a 500, and a redirect after it failed)
			return failure
		}
	}
	r.status = status
	r.noStore()
	return r.give("error", textType, []byte(text))
}

func (r *Response) noStore() {
	r.header.Set("Cache-Control", "no-store")
	r.header.Set("X-Content-Type-Options", "nosniff")
}

// unserved is an answer the engine cannot give yet: 501 with a text that says which, never a panic and
// never a half answer.
func (r *Response) unserved(word, what string) value.Value {
	if failure := r.answering(word, true); failure.IsInvalid() {
		return failure
	}
	r.status = http.StatusNotImplemented
	r.mediaType = textType
	r.noStore()
	return r.give(word, textType, []byte("501 Not Implemented: context."+word+"(…) "+what+", which engine2 does not serve yet.\n"))
}

// View is context.view(data): the page of the route's folder, its content.ui in its frame, which the host
// draws (library/ui) with data, an object whose keys are the names the page reads; nothing for a page given
// nothing. Its head is context.title(…) and context.description(…) written before it.
func (r *Response) View(arguments ...value.Value) value.Value {
	if failure := r.answering("view", true); failure.IsInvalid() {
		return failure
	}
	data := argument(arguments, 0)
	if !data.IsNullish() && !data.IsObject() {
		return value.Fail(value.TypeCode, "context.view(…) takes an object, the names its page reads: context.view({ user, orders })")
	}
	r.data = data
	return r.give("view", htmlType, nil)
}

// Page is what a view answer gives the host to draw: its data and its head. ok is false for any other answer.
func (r *Response) Page() (data value.Value, title, description string, ok bool) {
	return r.data, r.title, r.description, r.answer == "view"
}

// Drawn is the page the host drew for a view answer: its body, which Write sends with the answer's status and
// headers.
func (r *Response) Drawn(page []byte) { r.body = page }

// File is .file(source) (docs/KITWORK_FILE.md mục 9): the content of a file. A path, written as every read of
// kitwork/file writes one ("exports/a.csv" the app's data, "app/…" its code), or a handle of kitwork/file: served
// from the disk as the answer is written, never through the VM, 404 for what is not there or never reached. Bytes
// the program has: sent as they are, of the media type .type(…) set, else application/octet-stream. A string is
// always a path; text is .text(…).
func (r *Response) File(arguments ...value.Value) value.Value {
	if failure := r.answering("file", false); failure.IsInvalid() {
		return failure
	}
	source := argument(arguments, 0)
	switch {
	case source.IsBytes():
		return r.give("file", "application/octet-stream", source.Bytes())
	case source.IsString():
		if r.owner == nil || r.owner.Files == nil {
			return value.Fail(value.UnsupportedCode, "context.file(\""+source.Text()+"\"): this request belongs to no app, so it has no file to serve")
		}
		opened := r.owner.Files.Open(source)
		if opened.IsInvalid() {
			return opened
		}
		r.served = opened.Host().(*file.Handle)
	default:
		handle, ok := source.Host().(*file.Handle)
		if !source.IsStruct() || !ok {
			return value.Fail(value.InvalidArgumentCode, "file(…) takes a path, a handle of kitwork/file or bytes, not "+source.Kind()+": context.file(\"exports/a.csv\"); text is context.text(…)")
		}
		r.served = handle
	}
	r.answer = "file"
	return r.self()
}
