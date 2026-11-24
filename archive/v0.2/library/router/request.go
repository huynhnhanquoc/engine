package router

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/file"
)

// BodyLimit is the most bytes a read of the body takes (docs/KITWORK_ROUTER.md mục 5): past it the read
// is a TOO_LARGE failure and the answer is 413, nothing more is read.
const BodyLimit = 1 << 20

// UploadLimit is the most bytes of a multipart body request.file(…) reads (the first engine's MaxBodyBytes, 32 MiB):
// past it the read is a TOO_LARGE failure and the answer is 413. The route's own ceilings of uploads (size, count,
// media types) are still open (docs/KITWORK_ROUTER.md mục 5).
var UploadLimit int64 = 32 << 20

// uploadMemory is how much of a multipart body's files the parse keeps in memory: past it each file goes to a file
// of the system, removed as the request ends.
const uploadMemory = 64 << 10

// Request is the asking side of a request, context.request (docs/KITWORK_ROUTER.md mục 5).
type Request struct {
	request *http.Request
	params  map[string]value.Value
	// body is the body once read, so a second read gives the same; read says it was.
	body     []byte
	read     bool
	tooLarge bool
	// query is the URL's query once parsed: query(…) reads it at each call, and parsed it again at each
	// (05/11/2026, 8% of a request that read two parameters)
	query url.Values
	// form is the body's form once read (formRead): its fields, and their names in the order they first came;
	// none for a body that is no form
	formRead bool
	form     url.Values
	names    []string
	// uploads are the body's multipart form request.file(…) parsed once (uploadsRead): nil for a body of another
	// media type, or one not well formed; uploadsFailure a failure of the host, told at every read. streamed says the
	// parse read the body itself, past BodyLimit, so the body is gone for text(), json() and bytes().
	uploadsRead    bool
	uploads        *multipart.Form
	uploadsFailure value.Value
	streamed       bool
}

// NewRequest is the request r, with the values of its folders' parameters ({id[number]} is a number).
func NewRequest(r *http.Request, params map[string]value.Value) *Request {
	return &Request{request: r, params: params}
}

// TooLarge reports that a read of the body went past BodyLimit: the answer is then 413.
func (r *Request) TooLarge() bool { return r.tooLarge }

// ---- properties ----

// RequestBound: the request belongs to its request; a routine holds none (runtime Snapshot).
func (r *Request) RequestBound() {}

func (r *Request) Method() value.Value { return value.String(r.request.Method) }
func (r *Request) Path() value.Value   { return value.String(r.request.URL.Path) }
func (r *Request) Host() value.Value   { return value.String(r.request.Host) }

// Hostname is the host without its port (KITWORK_ROUTER.md mục 5): "shop.example" of "shop.example:8443",
// "::1" of "[::1]:8080".
func (r *Request) Hostname() value.Value {
	host := r.request.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return value.String(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
}

// Url is the full URL: scheme, host, path and query.
func (r *Request) Url() value.Value {
	return value.String(r.scheme() + "://" + r.request.Host + r.request.URL.RequestURI())
}

// Scheme is "https" when the connection is TLS. A proxy's X-Forwarded-Proto is not read: the host does
// not run behind a trusted proxy yet.
func (r *Request) Scheme() value.Value { return value.String(r.scheme()) }
func (r *Request) Secure() value.Value { return value.Boolean(r.request.TLS != nil) }

func (r *Request) scheme() string {
	if r.request.TLS != nil {
		return "https"
	}
	return "http"
}

// Ip is the address of the connection, without its port; X-Forwarded-For is not read (as Scheme).
func (r *Request) Ip() value.Value {
	host, _, err := net.SplitHostPort(r.request.RemoteAddr)
	if err != nil {
		return value.String(r.request.RemoteAddr)
	}
	return value.String(host)
}

func (r *Request) UserAgent() value.Value { return value.String(r.request.UserAgent()) }
func (r *Request) Referer() value.Value   { return value.String(r.request.Referer()) }

// Headers is every header, by its name in lower case, with its first value.
func (r *Request) Headers() value.Value {
	headers := value.Object()
	for name, values := range r.request.Header {
		if len(values) > 0 {
			headers.Set(strings.ToLower(name), value.String(values[0]))
		}
	}
	return headers
}

// Cookies is every cookie the client sent, by name: data the client wrote, not trusted.
func (r *Request) Cookies() value.Value {
	cookies := value.Object()
	for _, cookie := range r.request.Cookies() {
		if !cookies.Has(cookie.Name) {
			cookies.Set(cookie.Name, value.String(cookie.Value))
		}
	}
	return cookies
}

// ---- methods ----

// text is the first argument as text, or the failure of making it text (an object whose own toString
// fails, or one a site's boot made), which a method gives as its own (05/11/2026: read as "").
func text(arguments []value.Value) (string, value.Value) { return argument(arguments, 0).ToText() }

// Params is params("id"): the value of a folder's parameter, a number for {id[number]}; undefined for a
// name no folder of the route has.
func (r *Request) Params(arguments ...value.Value) value.Value {
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return failure
	}
	return r.params[name]
}

// Query is query("page"): the first value of a parameter of the URL, "" when there is none.
func (r *Request) Query(arguments ...value.Value) value.Value {
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return failure
	}
	if r.query == nil {
		r.query = r.request.URL.Query()
	}
	return value.String(r.query.Get(name))
}

// Header is header("Accept"): a header's first value, "" when there is none.
func (r *Request) Header(arguments ...value.Value) value.Value {
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return failure
	}
	return value.String(r.request.Header.Get(name))
}

// Cookie was request.cookie("session") until 16/11/2026: a cookie is read with context.cookie(name), the word that
// sets one, and this says so.
func (r *Request) Cookie(arguments ...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "context.request.cookie(name) is context.cookie(name) now: one word for a cookie, one argument reads the one the browser sent, two set one with the answer")
}

// cookie is a cookie's value, "" when there is none (context.cookie(name)).
func (r *Request) cookie(arguments ...value.Value) value.Value {
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return failure
	}
	cookie, err := r.request.Cookie(name)
	if err != nil {
		return value.String("")
	}
	return value.String(cookie.Value)
}

// readBody reads the body once, at most BodyLimit bytes.
func (r *Request) readBody() value.Value {
	if r.streamed {
		return value.Fail(value.InvalidInputCode, "the request's body was read by request.file(…), as its uploads: its fields are request.form(…), its files request.file(…)")
	}
	if !r.read {
		r.read = true
		if r.request.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.request.Body, BodyLimit+1))
			switch {
			case len(body) > BodyLimit:
				r.tooLarge = true
			case err != nil:
				return value.Fail(value.InvalidInputCode, "the request's body could not be read")
			default:
				r.body = body
			}
		}
	}
	if r.tooLarge {
		return value.Fail(value.TooLargeCode, "the request's body is past the "+strconv.Itoa(BodyLimit)+" bytes a read takes")
	}
	return value.Value{}
}

// Text is text(): the body as text.
func (r *Request) Text(...value.Value) value.Value {
	if failure := r.readBody(); failure.IsInvalid() {
		return failure
	}
	return value.String(string(r.body))
}

// Bytes is bytes(): the body as it came.
func (r *Request) Bytes(...value.Value) value.Value {
	if failure := r.readBody(); failure.IsInvalid() {
		return failure
	}
	return value.Bytes(r.body)
}

// Json is json(): the body read as JSON, null when it is not JSON (KITWORK_ROUTER.md mục 5). The limit
// of json({ limit }) is not served yet.
func (r *Request) Json(budget value.Budget, arguments ...value.Value) value.Value {
	if len(arguments) > 0 {
		return value.Fail(value.UnsupportedCode, "request.json({ limit }) is not served by engine2 yet: a read takes "+strconv.Itoa(BodyLimit)+" bytes")
	}
	if failure := r.readBody(); failure.IsInvalid() {
		return failure
	}
	if len(r.body) == 0 {
		return value.Null()
	}
	// the tree, within the budget of the run that reads it: a body of "[]," is some 80 bytes for each 3 of its own
	parsed := value.ParseJSONWithin(string(r.body), budget)
	if parsed.IsInvalid() {
		if parsed.Code() != value.InvalidInputCode {
			return parsed // the run's memory failure, not a body that is no JSON
		}
		return value.Null()
	}
	return parsed
}

// Form is form("email"): the first value of a field of the body, "" when it has none (docs/KITWORK_ROUTER.md
// mục 5, as the first engine's work/request_form.go). A field is one of an urlencoded body
// (application/x-www-form-urlencoded) or a text field of a multipart one (multipart/form-data), never a value of
// the URL's query, which query(…) reads; a file of a multipart body is no field. A body that is no form (JSON, no
// Content-Type) or a malformed one (a bad %, a multipart body without its boundary or cut short) has no field.
// form() is every field, each its first value, in the order they came: the input a struct parses
// (KITWORK_SCHEMA.md, customers.parse(context.request.form())). The body is read once, under BodyLimit: past it
// the read fails TOO_LARGE and the request answers 413.
func (r *Request) Form(arguments ...value.Value) value.Value {
	if failure := r.readForm(); failure.IsInvalid() {
		return failure
	}
	if len(arguments) == 0 {
		fields := value.Object()
		for _, name := range r.names {
			fields.Set(name, value.String(r.form.Get(name)))
		}
		return fields
	}
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return failure
	}
	return value.String(r.form.Get(name))
}

// readForm reads the body's form once: the fields of the parse request.file(…) made when it made one, whose bytes
// alone count against BodyLimit (a file beside them is an upload's, not the form's).
func (r *Request) readForm() value.Value {
	if r.formRead {
		return value.Value{}
	}
	if r.uploadsRead && r.uploads != nil {
		size := 0
		for name, values := range r.uploads.Value {
			for _, field := range values {
				size += len(name) + len(field)
			}
		}
		if size > BodyLimit {
			r.tooLarge = true
			return value.Fail(value.TooLargeCode, "the fields of the request's form are past the "+strconv.Itoa(BodyLimit)+" bytes a read takes")
		}
		r.formRead = true
		r.form = url.Values(r.uploads.Value)
		for name := range r.form {
			r.names = append(r.names, name)
		}
		return value.Value{}
	}
	if failure := r.readBody(); failure.IsInvalid() {
		return failure
	}
	r.formRead = true
	r.form, r.names = formOf(r.request.Header.Get("Content-Type"), r.body)
	return value.Value{}
}

// formOf reads body as the form its media type names: its fields and their names in the order they first came;
// none for any other media type, and for a form that is not well formed.
func formOf(contentType string, body []byte) (url.Values, []string) {
	if len(body) == 0 {
		return nil, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil
	}
	switch mediaType {
	case "application/x-www-form-urlencoded":
		fields, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, nil
		}
		// the order of the names, read as ParseQuery reads them: a piece of the body between two &, its name
		// before its =
		var names []string
		seen := map[string]bool{}
		for _, piece := range strings.Split(string(body), "&") {
			if piece == "" {
				continue
			}
			key, _, _ := strings.Cut(piece, "=")
			name, err := url.QueryUnescape(key)
			if err != nil || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		return fields, names
	case "multipart/form-data":
		// a body without its boundary has no part: the reader refuses an empty one at the first part
		reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
		fields := url.Values{}
		var names []string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return fields, names
			}
			if err != nil {
				return nil, nil
			}
			name := part.FormName()
			if name == "" || part.FileName() != "" {
				continue // no field: no name, or a file
			}
			text, err := io.ReadAll(part)
			if err != nil {
				return nil, nil
			}
			if _, kept := fields[name]; !kept {
				names = append(names, name)
			}
			fields.Add(name, string(text))
		}
	}
	return nil, nil
}

func (r *Request) Body(...value.Value) value.Value {
	return value.Fail(value.UnsupportedCode, "request.body() is not served by engine2 yet: its rule is still open; read text(), json() or bytes()")
}

// File is request.file("avatar") (docs/KITWORK_FILE.md mục 9): the first upload of the field, null when the body has
// none. An upload is { name, size, type }, what the client said; file.open(…).write(upload) copies its bytes to the
// app's data without them entering the VM.
func (r *Request) File(arguments ...value.Value) value.Value {
	uploads, failure := r.uploadsOf(arguments)
	if failure.IsInvalid() {
		return failure
	}
	if len(uploads) == 0 {
		return value.Null()
	}
	return uploads[0]
}

// Files is request.files("photos"): every upload of the field, [] when the body has none.
func (r *Request) Files(arguments ...value.Value) value.Value {
	uploads, failure := r.uploadsOf(arguments)
	if failure.IsInvalid() {
		return failure
	}
	if uploads == nil {
		uploads = []value.Value{}
	}
	return value.Array(uploads)
}

// uploadsOf is the uploads of the field the arguments name.
func (r *Request) uploadsOf(arguments []value.Value) ([]value.Value, value.Value) {
	name, failure := text(arguments)
	if failure.IsInvalid() {
		return nil, failure
	}
	form, failure := r.parseUploads()
	if failure.IsInvalid() || form == nil {
		return nil, failure
	}
	var uploads []value.Value
	for _, header := range form.File[name] {
		uploads = append(uploads, value.Struct(file.NewUpload(name, header)))
	}
	return uploads, value.Value{}
}

// parseUploads parses the body's multipart form once: from the body read already (under BodyLimit, by form(),
// text()…), or else from the body itself, under UploadLimit, its files past uploadMemory to files of the system,
// removed as the request ends. A failure of the host is told, never taken for "no file was sent": a body past the
// ceiling, one of more parts than Go's parse reads (both 413), a file of the system the parse could not write
// (DATABASE). Only a body that is not well-formed multipart, a client gone away midway among them, has no upload.
func (r *Request) parseUploads() (*multipart.Form, value.Value) {
	if r.uploadsRead {
		return r.uploads, r.uploadsFailure
	}
	r.uploadsRead = true
	mediaType, parameters, err := mime.ParseMediaType(r.request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, value.Value{}
	}
	var body io.Reader
	switch {
	case r.read:
		if failure := r.readBody(); failure.IsInvalid() {
			r.uploadsFailure = failure
			return nil, failure
		}
		body = bytes.NewReader(r.body)
	case r.request.Body == nil:
		return nil, value.Value{}
	default:
		r.read, r.streamed = true, true
		body = http.MaxBytesReader(nil, r.request.Body, UploadLimit)
	}
	form, err := multipart.NewReader(body, parameters["boundary"]).ReadForm(uploadMemory)
	if err != nil {
		var past *http.MaxBytesError
		var system *fs.PathError
		switch {
		case errors.As(err, &past):
			r.tooLarge = true
			r.uploadsFailure = value.Fail(value.TooLargeCode, "the request's body is past the "+strconv.FormatInt(UploadLimit, 10)+" bytes of uploads a request takes")
		case errors.Is(err, multipart.ErrMessageTooLarge):
			r.tooLarge = true
			r.uploadsFailure = value.Fail(value.TooLargeCode, "the multipart body is past what its reader takes: too many parts, headers or text fields; send fewer fields, a large file in one part")
		case errors.As(err, &system):
			r.uploadsFailure = value.Fail(value.DatabaseCode, "the uploads could not be kept in a file of the system: "+system.Err.Error())
		}
		return nil, r.uploadsFailure
	}
	// the files the parse wrote go as the request ends, whatever answers it
	context.AfterFunc(r.request.Context(), func() { _ = form.RemoveAll() })
	r.uploads = form
	return form, value.Value{}
}
