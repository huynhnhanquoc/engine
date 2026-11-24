package http

import (
	"encoding/json"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// answer is what a run got: a status, headers, a body read whole.
type answer struct {
	status int
	header http.Header
	body   []byte
}

func (a *answer) ok() bool { return a.status >= 200 && a.status <= 299 }

// result is what .send() gives, whatever happened (http.md mục 3): ok, status (null when no answer came), code (null
// when ok: TIMEOUT, NETWORK, DENIED, TOO_LARGE, INVALID_RESPONSE, HTTP_4XX, HTTP_5XX), error (a short text when not
// ok), data (the body read by its Content-Type: JSON as a value, text as a text), text (the body as it is), headers
// (each name in lower case).
func result(got *answer, failure value.Value, budget value.Budget) value.Value {
	out := value.Object()
	if got == nil {
		return out.Set("ok", value.Boolean(false)).Set("status", value.Null()).Set("code", value.String(failure.Code())).
			Set("error", value.String(failure.Message())).Set("data", value.Null()).Set("text", value.String("")).Set("headers", value.Object())
	}
	status, headers := value.Number(float64(got.status)), headersOf(got.header)
	if failure.IsInvalid() { // an answer whose body is past its cap: read no further
		return out.Set("ok", value.Boolean(false)).Set("status", status).Set("code", value.String(failure.Code())).
			Set("error", value.String(failure.Message())).Set("data", value.Null()).Set("text", value.String("")).Set("headers", headers)
	}
	data, unread := decode(got, budget)
	if data.IsInvalid() {
		return data // the run's memory failure as the body's JSON grew
	}
	code, said := value.Null(), value.Null()
	switch {
	case !got.ok():
		code = value.String("HTTP_" + strconv.Itoa(got.status/100) + "XX")
		said = value.String("the server answered " + strconv.Itoa(got.status) + " " + http.StatusText(got.status))
	case unread != "":
		code, said, data = value.String("INVALID_RESPONSE"), value.String(unread), value.Null()
	}
	return out.Set("ok", value.Boolean(code.IsNull())).Set("status", status).Set("code", code).Set("error", said).
		Set("data", data).Set("text", value.String(string(got.body))).Set("headers", headers)
}

// headersOf is the answer's headers, each name in lower case, the values of one name joined by ", ".
func headersOf(header http.Header) value.Value {
	headers := value.Object()
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		headers.Set(strings.ToLower(name), value.String(strings.Join(header[name], ", ")))
	}
	return headers
}

// decode is the body read by its Content-Type: JSON (application/json, …+json) as a value, any other as its text;
// no body is null. unread says why a JSON body could not be read. The JSON's tree is made within budget, the run
// that sent (value.Budget); its failure is the value.
func decode(got *answer, budget value.Budget) (value.Value, string) {
	if len(got.body) == 0 {
		return value.Null(), ""
	}
	kind, _, _ := mime.ParseMediaType(got.header.Get("Content-Type"))
	if kind == "application/json" || strings.HasSuffix(kind, "+json") {
		parsed := value.ParseJSONWithin(string(got.body), budget)
		if parsed.IsInvalid() {
			if parsed.Code() != value.InvalidInputCode {
				return parsed, ""
			}
			return value.Null(), "the body is not the JSON its Content-Type says: " + parsed.Message()
		}
		return parsed, ""
	}
	return value.String(string(got.body)), ""
}

// kept is an answer as .persist() writes it on disk.
type kept struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header"`
	Body   []byte              `json:"body"`
}

func encodeAnswer(got *answer) []byte {
	data, _ := json.Marshal(kept{Status: got.status, Header: got.header, Body: got.body})
	return data
}

func decodeAnswer(data []byte) (*answer, bool) {
	var read kept
	if json.Unmarshal(data, &read) != nil || read.Status == 0 {
		return nil, false
	}
	return &answer{status: read.Status, header: read.Header, body: read.Body}, true
}
