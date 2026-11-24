package router

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// The rules of a request (docs/router.md mục 3–5): router.guard(fn) and router.limit({ rate, per, by }) stand
// before a door, router.guard(staff).post(fn), and are that door's; ended by .methods(…) they are the folder's and
// every folder's below it, for the methods named, router.guard(staff).methods("*"). A request meets them from the app
// root down to its folder, config.work's before router.work's, each chain in the order written, then the chain before
// its door, then the door's function.

// Rule is one rule of a chain: a guard or a limit, with where it is written.
type Rule struct {
	// Guard is a function of the program run before the door with the request's context: it lets the request on
	// with return context.next(), or answers, and the request ends there.
	Guard *value.Lambda
	// Limit is a ceiling of requests for each client.
	Limit *Limit
	// At is where the rule is written, "alpha.test/admin/config.work:3:1".
	At string
}

// Branch is a chain .methods(…) ended: its rules, for the methods it names, in its folder and every folder below.
type Branch struct {
	Rules []Rule
	// methods are the methods named, "get", "post"…; nil is "*", every method.
	methods map[string]bool
}

// Applies reports that the branch's rules are a request's of method (lower case): HEAD runs as GET, so it meets
// the rules of GET.
func (b *Branch) Applies(method string) bool {
	if b.methods == nil {
		return true
	}
	if method == "head" {
		method = "get"
	}
	return b.methods[method]
}

// chain is what the words written before hold, on the router a rule gives: the rules, and the first cache or
// persist in it, which belongs to the answer of a GET door and so never ends at .methods(…).
type chain struct {
	rules  []Rule
	cached string
}

// chainWords are the words a chain goes on with, before its door or its .methods(…).
var chainWords = map[string]bool{"guard": true, "limit": true, "cache": true, "persist": true}

// begin is the router a chain word gives: a chain of its own, the one written before with rule added (a cache or a
// persist adds none). The scope keeps the first word of a chain until a door or .methods(…) ends it: a file that ends
// first is the error Unended says.
func (r *Router) begin(word string, rule *Rule) value.Value {
	next := &chain{}
	if r.chain != nil {
		next.rules = append(next.rules, r.chain.rules...)
		next.cached = r.chain.cached
	} else {
		r.Scope.before, r.Scope.beforeWord = "router."+word+"(…) at "+r.Scope.where(), word
	}
	if rule != nil {
		next.rules = append(next.rules, *rule)
	} else if next.cached == "" {
		next.cached = word
	}
	return value.Struct(&Router{Scope: r.Scope, File: r.File, Open: r.Open, chain: next})
}

// Guard is router.guard(fn): fn runs before the door with the request's context, one function of the program; a
// chain of guards runs in the order written.
func (r *Router) Guard(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("guard"); ok {
		return failure
	}
	if failure, ok := r.ended("guard"); ok {
		return failure
	}
	guard := argument(arguments, 0)
	if len(arguments) != 1 || guard.Lambda() == nil {
		text := "router.guard(…) takes one function of the program, (context) => …: return context.next() lets the request on, an answer (context.redirect(\"/login\"), context.error(\"…\")) ends it"
		switch {
		case len(arguments) == 1 && guard.IsArray():
			text += "; two guards are two words, router.guard(a).guard(b)"
		case len(arguments) == 1 && guard.IsNullish():
			text += "; it was handed " + guard.Kind() + ", a name with no value"
		}
		return value.Fail(value.InvalidArgumentCode, text)
	}
	if problem := takenApart(guard.Lambda(), "guard"); problem != "" {
		return value.Fail(value.InvalidArgumentCode, problem)
	}
	return r.begin("guard", &Rule{Guard: guard.Lambda(), At: r.Scope.where()})
}

// Limit is router.limit({ rate, per, by }): rate requests in each span per for each client, the address of its
// connection ("ip", the default) or everyone together ("global"). Before a door it counts that door by the request's
// path; before .methods(…), the folder and every folder below it together.
func (r *Router) Limit(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("limit"); ok {
		return failure
	}
	if failure, ok := r.ended("limit"); ok {
		return failure
	}
	limit, failure := readLimit(arguments)
	if failure.IsInvalid() {
		return failure
	}
	return r.begin("limit", &Rule{Limit: limit, At: r.Scope.where()})
}

const limitExample = `router.limit({ rate: 60, per: "1m", by: "ip" })`

// readLimit reads { rate, per, by } strictly: rate a whole number above 0, per a span written as text, by "ip" when
// left out. Any other key, value or spelling is the call's failure, never a rule read another way.
func readLimit(arguments []value.Value) (*Limit, value.Value) {
	shape := argument(arguments, 0)
	if len(arguments) != 1 || !shape.IsObject() {
		return nil, value.Fail(value.InvalidArgumentCode, "router.limit(…) takes one object, { rate, per, by }: "+limitExample)
	}
	for _, key := range shape.Keys() {
		switch key {
		case "rate", "per", "by":
			continue
		}
		hint := ""
		switch strings.ToLower(key) {
		case "max", "count", "requests":
			hint = " (rate?)"
		case "period", "window", "every":
			hint = " (per?)"
		case "type", "key":
			hint = " (by?)"
		}
		return nil, value.Fail(value.InvalidArgumentCode, "router.limit({ "+key+": … }): no key "+key+hint+"; the keys are rate, per and by: "+limitExample)
	}
	rate := shape.Get("rate")
	if !rate.IsNumber() || rate.Number() < 1 || rate.Number() != math.Trunc(rate.Number()) || rate.Number() > math.MaxInt32 {
		return nil, value.Fail(value.InvalidArgumentCode, "router.limit({ rate }): rate is a whole number above 0, the requests each span per lets through: "+limitExample)
	}
	per := shape.Get("per")
	span, ok := time.Duration(0), false
	if per.IsString() {
		span, ok = readSpan(per.Text())
	}
	if !ok {
		return nil, value.Fail(value.InvalidArgumentCode, "router.limit({ per }): per is a span written as text, \"30s\", \"1m\", \"1h\", \"1d\": "+limitExample)
	}
	limit := &Limit{Rate: int(rate.Number()), Per: span, By: "ip"}
	if by := shape.Get("by"); !by.IsUndefined() {
		switch {
		case !by.IsString():
			return nil, value.Fail(value.InvalidArgumentCode, "router.limit({ by }): by is \"ip\" (the default) or \"global\": "+limitExample)
		case by.Text() == "ip" || by.Text() == "global":
			limit.By = by.Text()
		case by.Text() == "user":
			return nil, value.Fail(value.UnsupportedCode, "router.limit({ by: \"user\" }) is not served by engine2 yet: it counts the user of a checked session, which engine2 has no word for yet; the site does not boot, since a limit left out would let through what it holds back")
		default:
			hint := ""
			if near := strings.ToLower(strings.TrimSpace(by.Text())); near == "ip" || near == "global" {
				hint = " (\"" + near + "\"?)"
			}
			return nil, value.Fail(value.InvalidArgumentCode, "router.limit({ by: "+strconv.Quote(by.Text())+" }): by is \"ip\" (the default) or \"global\""+hint+"; nothing a client sends unchecked, a header or a cookie, is counted, since a client changing it would escape the limit")
		}
	}
	return limit, value.Value{}
}

// readSpan reads Go's spans ("30s", "1m", "1h30m") and a whole number of days ("1d").
func readSpan(text string) (time.Duration, bool) {
	if span, err := time.ParseDuration(text); err == nil {
		return span, span > 0
	}
	days, ok := strings.CutSuffix(text, "d")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(days)
	if err != nil || n <= 0 || n > 3650 {
		return 0, false
	}
	return time.Duration(n) * 24 * time.Hour, true
}

// Methods is .methods(…) at the end of a chain of router.guard(…) and router.limit(…): the chain is the folder's
// and every folder's below it, for the methods named, written in lower case, or "*" for every method.
func (r *Router) Methods(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("methods"); ok {
		return failure
	}
	if r.chain == nil {
		return value.Fail(value.InvalidArgumentCode, "router.methods(…) ends a chain of router.guard(…) or router.limit(…): router.guard(staff).methods(\"*\")")
	}
	if r.chain.cached != "" {
		return value.Fail(value.InvalidArgumentCode, "router."+r.chain.cached+"(…) before .methods(…): "+r.chain.cached+" belongs to the answer of a GET door and stands before that door, router."+r.chain.cached+"(\"5m\").get(…); only guard and limit stand before .methods(…)")
	}
	if failure, ok := r.ended("methods"); ok {
		return failure
	}
	branch := &Branch{Rules: r.chain.rules}
	const example = `.methods("*") for every method, .methods("post", "put", "patch", "delete") for some`
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, ".methods(…) names the methods its chain is for: "+example)
	}
	for _, item := range arguments {
		if !item.IsString() {
			return value.Fail(value.InvalidArgumentCode, ".methods(…) takes the names of methods, not "+item.Kind()+": "+example+"; what a request does is written in a guard")
		}
		name := item.Text()
		switch name {
		case "*":
			if len(arguments) != 1 {
				return value.Fail(value.InvalidArgumentCode, `.methods("*", …): "*" is every method, alone`)
			}
		case "get", "post", "put", "patch", "delete":
			if branch.methods == nil {
				branch.methods = map[string]bool{}
			}
			if branch.methods[name] {
				return value.Fail(value.InvalidArgumentCode, ".methods(…) names "+strconv.Quote(name)+" twice")
			}
			branch.methods[name] = true
		default:
			text := ".methods(" + strconv.Quote(name) + "): the methods are \"get\", \"post\", \"put\", \"patch\", \"delete\", in lower case, or \"*\""
			switch lower := strings.ToLower(name); {
			case lower != name && strings.Contains(" get post put patch delete ", " "+lower+" "):
				text += " (" + strconv.Quote(lower) + "?)"
			case lower == "view":
				text += "; a page of router.view answers GET, \"get\""
			case lower == "head":
				text += "; HEAD runs as GET and meets its rules, \"get\""
			}
			return value.Fail(value.InvalidArgumentCode, text)
		}
	}
	r.Scope.Branches = append(r.Scope.Branches, branch)
	return value.Undefined()
}

// ---- limits ----

// Limit is router.limit({ rate, per, by }) as boot read it, with its count: a bucket of rate requests for each client
// (or one for everyone, "global"), filled again at rate each span per, one request taking one. It is shared by every
// request the rule counts, of every site of the app for a rule of its root.
type Limit struct {
	Rate int
	Per  time.Duration
	// By is "ip" or "global".
	By string

	mu      sync.Mutex
	buckets map[string]*bucket
	// swept is when the buckets filled again were last let go: a client seen once is not kept for the site's life.
	swept time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// sweepEvery is how often a limit lets go of the buckets of clients that have not come back for a whole span.
const sweepEvery = time.Minute

// Take counts one request of r against the limit; scope is "" for a branch's, the request's path for a door's, which
// counts each path of the door apart. It reports whether the request is within the limit and, when it is not, how
// long until it would be.
func (l *Limit) Take(r *http.Request, scope string, now time.Time) (bool, time.Duration) {
	key := scope
	if l.By == "ip" {
		key = clientAddress(r) + "|" + scope
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets, l.swept = map[string]*bucket{}, now
	}
	if now.Sub(l.swept) >= sweepEvery {
		l.sweep(now)
	}
	rate := float64(l.Rate)
	held, ok := l.buckets[key]
	if !ok {
		held = &bucket{tokens: rate, at: now}
		l.buckets[key] = held
	}
	held.tokens = math.Min(rate, held.tokens+float64(now.Sub(held.at))*rate/float64(l.Per))
	held.at = now
	if held.tokens >= 1 {
		held.tokens--
		return true, 0
	}
	return false, time.Duration((1 - held.tokens) * float64(l.Per) / rate)
}

// sweep lets go of every bucket full again: a client that comes back starts full, as it would have.
func (l *Limit) sweep(now time.Time) {
	l.swept = now
	rate := float64(l.Rate)
	for key, held := range l.buckets {
		if held.tokens+float64(now.Sub(held.at))*rate/float64(l.Per) >= rate {
			delete(l.buckets, key)
		}
	}
}

// Kept is how many clients the limit counts now, for a test.
func (l *Limit) Kept() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// clientAddress is the address of the request's connection without its port; X-Forwarded-For is not read
// (context.request.ip says the same).
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
