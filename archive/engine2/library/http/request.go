package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

const (
	// defaultTimeout is how long the whole request may take when .timeout() does not say; maxTimeout the most it may
	// say (http.md mục 2.2).
	defaultTimeout = 10 * time.Second
	maxTimeout     = 60 * time.Second
	// maxRetry is the most .retry() may say.
	maxRetry = 5
	// defaultMaxBytes caps the body read when .maxBytes() does not say (10 MiB); hostMaxBytes is the most it may be.
	defaultMaxBytes int64 = 10 << 20
	hostMaxBytes    int64 = 64 << 20
)

// Request is the description of one request: http.get(url) makes it, each step returns a changed copy, and .send()
// sends it, every time it is called. A copy per step keeps a description a program reuses as it was:
//
//	const feed = http.get(url).timeout("5s");
//	const first = feed.send();
//	const again = feed.header("Accept", "application/json").send();
type Request struct {
	module   *Module
	method   string
	target   string // an http(s) URL
	header   http.Header
	query    []parameter // in the order written, a later name replacing an earlier one
	body     []byte
	bodyType string        // the Content-Type of .json(), .form(), .text(); "" for no body
	timeout  time.Duration // the whole request's, its retries included
	retry    int           // runs after the first
	maxBytes int64
	cacheTTL time.Duration // 0 keeps no answer in memory
	diskTTL  time.Duration // 0 keeps no answer on disk
	// waits is the context of the run that calls send() (Attend): the request waits no longer than the run
	waits context.Context
}

type parameter struct{ name, text string }

// Freeze: a Request is never written once made (each step copies it).
func (r *Request) Freeze() {}

// Attend is the request for one call by a run whose context is ctx (value.Attender): send() and its retries end when
// the run does, cancelled or past its time, a routine's .timeout() and a job's with it (15/11/2026: a call out waited
// its own .timeout(), 60 seconds by default, past the request's 30 and past a client gone).
func (r *Request) Attend(ctx context.Context) value.Value {
	attended := *r
	attended.waits = ctx
	return value.Struct(&attended)
}

func (r *Request) clone() *Request {
	next := *r
	next.header = r.header.Clone()
	next.query = append([]parameter(nil), r.query...)
	next.waits = nil // a step's request waits on the run that sends it, given as send() is called
	return &next
}

func (r *Request) word() string { return "http." + strings.ToLower(r.method) + "(…)" }

// ---- steps ----

// Header sets one header; a later value replaces an earlier one of the same name, whatever its case.
func (r *Request) Header(arguments ...value.Value) value.Value {
	if len(arguments) != 2 {
		return value.Fail(value.InvalidArgumentCode, ".header(…) takes a name and a value: .header(\"Accept\", \"application/json\")")
	}
	next := r.clone()
	if failure := next.setHeader("header", arguments[0], arguments[1]); failure.IsInvalid() {
		return failure
	}
	return value.Struct(next)
}

// Headers sets several headers, over those set before.
func (r *Request) Headers(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsObject() {
		return value.Fail(value.InvalidArgumentCode, ".headers(…) takes an object: .headers({ Accept: \"application/json\" })")
	}
	next := r.clone()
	fields := arguments[0]
	for _, name := range fields.Keys() { // in the order written: of two spellings of one name, the later wins
		if failure := next.setHeader("headers", value.String(name), fields.Get(name)); failure.IsInvalid() {
			return failure
		}
	}
	return value.Struct(next)
}

// setHeader keeps a header once checked: a name of token characters, a value with no line break or NUL, so data
// cannot add a header of its own.
func (r *Request) setHeader(step string, name, given value.Value) value.Value {
	if !name.IsString() || !validHeaderName(name.Text()) {
		shown, _ := name.ToText()
		return value.Fail(value.InvalidArgumentCode, "."+step+"(…): "+strconv.Quote(shown)+" is no header name: letters, digits and ! # $ % & ' * + - . ^ _ ` | ~")
	}
	if !given.IsString() && !given.IsNumber() && !given.IsBoolean() {
		return value.Fail(value.InvalidArgumentCode, "."+step+"(…): the header "+name.Text()+" takes a text, a number or a boolean, not "+given.Kind())
	}
	text, failure := given.ToText()
	if failure.IsInvalid() {
		return failure
	}
	if strings.ContainsAny(text, "\r\n\x00") {
		return value.Fail(value.InvalidArgumentCode, "."+step+"(…): the header "+name.Text()+" holds a line break or NUL, which could add a header of its own")
	}
	r.header.Set(name.Text(), text)
	return value.Value{}
}

// validHeaderName says name is a token of HTTP.
func validHeaderName(name string) bool {
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

// Bearer sets Authorization: Bearer token.
func (r *Request) Bearer(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsString() || arguments[0].Text() == "" {
		return value.Fail(value.InvalidArgumentCode, ".bearer(…) takes the token, a text: .bearer(token)")
	}
	next := r.clone()
	if failure := next.setHeader("bearer", value.String("Authorization"), value.String("Bearer "+arguments[0].Text())); failure.IsInvalid() {
		return failure
	}
	return value.Struct(next)
}

// Basic sets Authorization: Basic of the user and the password.
func (r *Request) Basic(arguments ...value.Value) value.Value {
	if len(arguments) != 2 || !arguments[0].IsString() || !arguments[1].IsString() || strings.Contains(arguments[0].Text(), ":") {
		return value.Fail(value.InvalidArgumentCode, ".basic(…) takes a user (no :) and a password, two texts: .basic(user, password)")
	}
	next := r.clone()
	pair := base64.StdEncoding.EncodeToString([]byte(arguments[0].Text() + ":" + arguments[1].Text()))
	next.header.Set("Authorization", "Basic "+pair)
	return value.Struct(next)
}

// Query adds parameters to the URL, encoded as it is sent; a key whose value is undefined or null is left out
// (http.md mục 2.2), a later name replaces an earlier one.
func (r *Request) Query(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsObject() {
		return value.Fail(value.InvalidArgumentCode, ".query(…) takes an object: .query({ page: 1, search: \"kitwork\" })")
	}
	next := r.clone()
	fields := arguments[0]
	for _, name := range fields.Keys() {
		text, keep, failure := field(".query(…)", name, fields.Get(name))
		if failure.IsInvalid() {
			return failure
		}
		if keep {
			next.query = setParameter(next.query, name, text)
		}
	}
	return value.Struct(next)
}

// field is a parameter's text: a text, a number or a boolean; undefined and null are left out.
func field(step, name string, item value.Value) (string, bool, value.Value) {
	if item.IsNullish() {
		return "", false, value.Value{}
	}
	if !item.IsString() && !item.IsNumber() && !item.IsBoolean() {
		return "", false, value.Fail(value.InvalidArgumentCode, step+": "+name+" is "+item.Kind()+"; a parameter is a text, a number or a boolean")
	}
	text, failure := item.ToText()
	return text, true, failure
}

func setParameter(parameters []parameter, name, text string) []parameter {
	for i, each := range parameters {
		if each.name == name {
			parameters[i].text = text
			return parameters
		}
	}
	return append(parameters, parameter{name, text})
}

// encode writes parameters as a query or a form does, in their order.
func encode(parameters []parameter) string {
	var out strings.Builder
	for i, each := range parameters {
		if i > 0 {
			out.WriteByte('&')
		}
		out.WriteString(neturl.QueryEscape(each.name) + "=" + neturl.QueryEscape(each.text))
	}
	return out.String()
}

// Json sets the body: value written as JSON, Content-Type: application/json, within the budget of the run that
// writes it (value.Budget).
func (r *Request) Json(budget value.Budget, arguments ...value.Value) value.Value {
	if failure, ok := r.bodied("json"); ok {
		return failure
	}
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, ".json(…) takes the value to send: .json({ id: 1 })")
	}
	encoded := value.StringifyWithin(arguments[0], "", budget)
	if encoded.IsInvalid() && encoded.Code() != value.TypeCode {
		return encoded // the run's memory failure, or a text past the longest
	}
	if !encoded.IsString() {
		return value.Fail(value.InvalidArgumentCode, ".json(…): "+arguments[0].Kind()+" is no JSON")
	}
	next := r.clone()
	next.body, next.bodyType = []byte(encoded.Text()), "application/json"
	return value.Struct(next)
}

// Form sets the body: the fields as application/x-www-form-urlencoded, a field of undefined or null left out.
func (r *Request) Form(arguments ...value.Value) value.Value {
	if failure, ok := r.bodied("form"); ok {
		return failure
	}
	if len(arguments) != 1 || !arguments[0].IsObject() {
		return value.Fail(value.InvalidArgumentCode, ".form(…) takes an object: .form({ name: \"An\", age: 30 })")
	}
	var fields []parameter
	for _, name := range arguments[0].Keys() {
		text, keep, failure := field(".form(…)", name, arguments[0].Get(name))
		if failure.IsInvalid() {
			return failure
		}
		if keep {
			fields = setParameter(fields, name, text)
		}
	}
	next := r.clone()
	next.body, next.bodyType = []byte(encode(fields)), "application/x-www-form-urlencoded"
	return value.Struct(next)
}

// Text sets the body: a text as it is, Content-Type: text/plain; charset=utf-8.
func (r *Request) Text(arguments ...value.Value) value.Value {
	if failure, ok := r.bodied("text"); ok {
		return failure
	}
	if len(arguments) != 1 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, ".text(…) takes a text: .text(\"hello\")")
	}
	next := r.clone()
	next.body, next.bodyType = []byte(arguments[0].Text()), "text/plain; charset=utf-8"
	return value.Struct(next)
}

// bodied refuses a body on a request that carries none.
func (r *Request) bodied(step string) (value.Value, bool) {
	if bodyless(r.method) {
		return value.Fail(value.InvalidArgumentCode, r.word()+" carries no body, so no ."+step+"(…): the parameters of the URL are .query({ … })"), true
	}
	return value.Value{}, false
}

// Timeout is how long the whole request may take, its retries included: a duration written as text ("500ms",
// "5s"), 60 s at most.
func (r *Request) Timeout(arguments ...value.Value) value.Value {
	if len(arguments) == 1 && arguments[0].IsString() {
		if d, ok := readDuration(arguments[0].Text()); ok {
			if d > maxTimeout {
				return value.Fail(value.InvalidArgumentCode, ".timeout("+strconv.Quote(arguments[0].Text())+"): 60s at most")
			}
			next := r.clone()
			next.timeout = d
			return value.Struct(next)
		}
	}
	return value.Fail(value.InvalidArgumentCode, ".timeout(…) takes a duration written as text, 60s at most: .timeout(\"5s\"), .timeout(\"500ms\")")
}

// Retry is how many runs follow the first when the network failed or the server answered 502, 503, 504 or 429: from
// 0 to 5.
func (r *Request) Retry(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsNumber() || arguments[0].Number() < 0 || arguments[0].Number() != math.Trunc(arguments[0].Number()) || arguments[0].Number() > maxRetry {
		return value.Fail(value.InvalidArgumentCode, ".retry(…) takes how many runs follow the first, a whole number from 0 to 5: .retry(2) is three in all")
	}
	next := r.clone()
	next.retry = int(arguments[0].Number())
	return value.Struct(next)
}

// MaxBytes caps the body read: a size ("512kb", "50mb") or a number of bytes, up to the host's 64 MiB.
func (r *Request) MaxBytes(arguments ...value.Value) value.Value {
	size, ok := int64(0), false
	if len(arguments) == 1 {
		switch {
		case arguments[0].IsNumber() && arguments[0].Number() > 0 && arguments[0].Number() == math.Trunc(arguments[0].Number()):
			size, ok = int64(arguments[0].Number()), true
		case arguments[0].IsString():
			size, ok = readSize(arguments[0].Text())
		}
	}
	if !ok {
		return value.Fail(value.InvalidArgumentCode, ".maxBytes(…) takes a size: .maxBytes(\"512kb\"), .maxBytes(\"50mb\"), or a number of bytes")
	}
	if size > hostMaxBytes {
		return value.Fail(value.InvalidArgumentCode, ".maxBytes(…): past the host's cap, "+sizeText(hostMaxBytes))
	}
	next := r.clone()
	next.maxBytes = size
	return value.Struct(next)
}

// Cache keeps the result of a GET that succeeded in the site's memory, for the duration written ("5m", "1h", "7d"),
// by its URL and its headers.
func (r *Request) Cache(arguments ...value.Value) value.Value {
	ttl, failure := r.kept("cache", arguments)
	if failure.IsInvalid() {
		return failure
	}
	next := r.clone()
	next.cacheTTL = ttl
	return value.Struct(next)
}

// Persist keeps the result of a GET that succeeded on the disk of the app's machine (.database/disk/), for the
// duration written, by its URL and its headers.
func (r *Request) Persist(arguments ...value.Value) value.Value {
	ttl, failure := r.kept("persist", arguments)
	if failure.IsInvalid() {
		return failure
	}
	next := r.clone()
	next.diskTTL = ttl
	return value.Struct(next)
}

func (r *Request) kept(step string, arguments []value.Value) (time.Duration, value.Value) {
	if r.method != "GET" {
		return 0, value.Fail(value.InvalidArgumentCode, "."+step+"(…) is a GET's: "+r.word()+" keeps no result")
	}
	if len(arguments) == 1 && arguments[0].IsString() {
		if d, ok := readDuration(arguments[0].Text()); ok {
			return d, value.Value{}
		}
	}
	return 0, value.Fail(value.InvalidArgumentCode, "."+step+"(…) takes a duration written as text: ."+step+"(\"5m\"), ."+step+"(\"24h\")")
}

// MissingMember is a word a request does not have: what is read on its result says to send it first.
func (r *Request) MissingMember(name string) value.Value {
	switch strings.ToLower(name) {
	case "ok", "status", "code", "error", "data", "headers", "body":
		return value.Fail(value.TypeCode, r.word()+" is not sent, so it has no "+name+": the result is .send()'s, const response = "+r.word()+".send(); then response."+name)
	case "then", "catch", "fetch", "execute", "request":
		return value.Fail(value.TypeCode, r.word()+"."+name+" is no word of a request: .send() sends it")
	}
	return value.Fail(value.TypeCode, "a request has no word "+name+": .query, .header, .headers, .bearer, .basic, .json, .form, .text, .timeout, .retry, .maxBytes, .cache, .persist, .send")
}

// ---- send ----

// Send sends the request, once a call, and gives its result whatever happened (http.md mục 3).
func (r *Request) Send(budget value.Budget, arguments ...value.Value) value.Value {
	if r.module != nil && r.module.Open != nil && r.module.Open() {
		return value.Fail(value.UnsupportedCode, r.word()+".send() at the top level of a file: a file declares as the site boots; a request is sent as a request runs, in a handler, a routine, a job or a schedule")
	}
	if len(arguments) > 0 {
		return value.Fail(value.InvalidArgumentCode, ".send() takes nothing: the body is .json(value), .form({ … }) or .text(\"…\") before it")
	}
	if r.retry > 0 && (r.method == "POST" || r.method == "PATCH") && r.header.Get("Idempotency-Key") == "" {
		return value.Fail(value.InvalidArgumentCode, r.method+" run again may write twice: add the header Idempotency-Key, .header(\"Idempotency-Key\", key), or leave .retry() out")
	}
	got, failure := r.run()
	if r.waits != nil && r.waits.Err() != nil {
		// the run ended as the request waited: its failure, which ends the run as its cancellation, not an answer the
		// program reads on
		return value.Fail(value.CancelledCode, r.word()+".send() waited past the end of its run: "+r.waits.Err().Error())
	}
	return result(got, failure, budget)
}

// run sends the request with its runs, from what is kept when it keeps: the answer of the last run, or the failure of
// none.
func (r *Request) run() (*answer, value.Value) {
	target := r.finalURL()
	key := cacheKey(target, r.header)
	if r.cacheTTL > 0 && r.module != nil && r.module.Cache != nil {
		if kept, ok := r.module.Cache.load(key); ok {
			return kept, value.Value{}
		}
	}
	if r.diskTTL > 0 && r.module != nil && r.module.Persist != nil {
		if data, ok := r.module.Persist.Load(key); ok {
			if kept, ok := decodeAnswer(data); ok {
				return kept, value.Value{}
			}
		}
	}
	// its own time, or the run's when that ends first (value.WaitContext)
	ctx, cancel := value.WaitContext(r.waits, time.Now().Add(r.timeout))
	defer cancel()
	deadline, _ := ctx.Deadline()
	var got *answer
	var failure value.Value
	for attempt := 0; ; attempt++ {
		got, failure = r.attempt(ctx, target)
		if attempt >= r.retry || !retryable(got, failure) {
			break
		}
		pause := backoff(attempt)
		if time.Until(deadline) <= pause {
			break
		}
		wait := time.NewTimer(pause)
		select {
		case <-wait.C:
		case <-ctx.Done():
			wait.Stop()
		}
		if ctx.Err() != nil {
			break
		}
	}
	if !failure.IsInvalid() && got.ok() {
		if r.cacheTTL > 0 && r.module != nil && r.module.Cache != nil {
			r.module.Cache.save(key, got, r.cacheTTL)
		}
		if r.diskTTL > 0 && r.module != nil && r.module.Persist != nil {
			r.module.Persist.Save(key, encodeAnswer(got), r.diskTTL)
		}
	}
	return got, failure
}

// retryable says a run is worth another: the network failed (not the time, which is the whole request's), or the
// server answered 502, 503, 504 or 429.
func retryable(got *answer, failure value.Value) bool {
	if failure.IsInvalid() {
		return failure.Code() == value.NetworkCode
	}
	switch got.status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return true
	}
	return false
}

// backoff is the pause before run n+2: 100 ms doubled each time, in its upper half at random, 6.4 s at most.
func backoff(n int) time.Duration {
	n = min(n, 6)
	step := (100 * time.Millisecond) << uint(n)
	return step/2 + rand.N(step/2)
}

// finalURL is the target with the parameters of .query() after those the URL has, in their order.
func (r *Request) finalURL() string {
	if len(r.query) == 0 {
		return r.target
	}
	base, fragment, hasFragment := strings.Cut(r.target, "#")
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
		if strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&") {
			separator = ""
		}
	}
	target := base + separator + encode(r.query)
	if hasFragment {
		target += "#" + fragment
	}
	return target
}

// attempt is one run: the answer read whole, up to maxBytes, or the failure of none (or of a body past its cap, with
// the answer's status and headers).
func (r *Request) attempt(ctx context.Context, target string) (*answer, value.Value) {
	var reader io.Reader
	if r.body != nil {
		reader = bytes.NewReader(r.body)
	}
	request, err := http.NewRequestWithContext(ctx, r.method, target, reader)
	if err != nil {
		return nil, value.Fail(value.NetworkCode, shownError(err))
	}
	for name, values := range r.header {
		request.Header[name] = append([]string(nil), values...)
	}
	if r.bodyType != "" && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", r.bodyType)
	}
	response, err := guardedClient.Do(request)
	if err != nil {
		return nil, noAnswer(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, r.maxBytes+1))
	if err != nil {
		return nil, noAnswer(err)
	}
	got := &answer{status: response.StatusCode, header: response.Header}
	if int64(len(data)) > r.maxBytes {
		return got, value.Fail(value.TooLargeCode, "the body of the answer is past "+sizeText(r.maxBytes)+" (.maxBytes)")
	}
	got.body = data
	return got, value.Value{}
}
