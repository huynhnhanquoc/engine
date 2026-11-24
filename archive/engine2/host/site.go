package host

import (
	"context"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/file"
	httpmodule "github.com/kitwork/engine2/library/http"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/routine"
	"github.com/kitwork/engine2/library/storage"
	"github.com/kitwork/engine2/library/ui"
)

// Site is one site folder, apps/<identity>/<domain>/, opened at boot: its route tree, and what every run
// of its code reads, built once. Nothing in it is written after Boot, so requests read it without a lock.
type Site struct {
	// Hostname is the folder's name in lower case: a request whose Host is it reaches the site.
	Hostname string
	// Folder is the site's folder, AppRoot the folder of its app (what app/… names).
	Folder  string
	AppRoot string
	// Timezone is the app's module.timezone, UTC when none: the zone of the site's Date.
	Timezone *time.Location
	root     *Folder
	// app is the scope of the app root's config.work, nil when it has none: its chains of rules are every site's of
	// the app, met before the site's own
	app *router.Scope
	// static is the files the site serves as they are, tried before its routes (static.go)
	static *Static
	// ui is the site's pages of .ui (library/ui): a route's content.ui in its frame, drawn for context.view and
	// for a folder with a page and no door (page.go)
	ui *ui.Site
	// unreadSaid are the pages whose words the .ui engine does not draw yet were said in the log, once each
	unreadSaid sync.Map
	// answers are what kitwork/http keeps of the site's requests out (.cache)
	answers *httpmodule.Cache
	// routines are what kitwork/routine runs on the site (routine.go); stopRoutines ends the life of those of go()
	routines     *routine.Site
	stopRoutines context.CancelFunc
	// storage is kitwork/storage of the site's app, which every site of the app shares
	storage *storage.App
	// background is the queue of the site's app, which every site of the app shares (background.go)
	background *background
	// streams are the open streams of context.sse of the site's app, which every site of the app shares (stream.go)
	streams *appStreams
	// files are kitwork/file of the site's app, what context.file("…") serves a path of
	files *file.File
	// components are the app's components, which the site's pages draw (sitecomponent.go)
	components *component.Definitions
	// drive says the site moves between its pages without a reload (router.reload, false when not written):
	// its pages load Drive, and a reader and a progressbar when their frame writes none
	drive bool
	// closed is why the site's app could not boot (Boot): the site answers 503, its app closed, every other app
	// serving
	closed error
	// slots are the shared frozen builtins by slot with the site's own console and Date laid over them,
	// slot 0 empty, built once at boot (builtinsOf): every run of the site reads these values, a boot run
	// through a copy with its file's kitwork in slot 0. globals holds the same values by name, for a
	// function that assigns a builtin's name; requests read it, never write it.
	slots   []value.Value
	globals map[string]value.Value
	// builtins is what a request's VM reads by slot: slots with the request's kitwork in slot 0 (a
	// handler reads no declaration, those are boot's). Built once; requests read it, never write it.
	builtins []value.Value
	// lead begins every line of the site in the log, "[alpha.test] ".
	lead string
	// gate bounds the requests the site runs at once (gate.go); pool is the host's, what every run makes together
	// (runtime.Pool), nil for none
	gate *gate
	pool *runtime.Pool
	// made is what one run of the site makes at most (app.limits; Host.RunBytes, or 64 MiB), energy what it spends
	// (app.limits; Host.RunEnergy, or 10,000,000 units)
	made   uint64
	energy uint64
	log    *Log
	// machines are the site's VMs between requests, each a worker with its deadline: a VM never carries a
	// site's values to another site.
	machines sync.Pool
	// calls are the locks of the calls of the site's pages, one call of a page at a time (bridge.go)
	calls sync.Map
	// tabs are the tabs of the app's pages that hold live names, which every site of the app shares (tab.go)
	tabs *appTabs
}

// energy is what one run may spend: the boot of one file, or one request.
var energy = runtime.Limits().DefaultMaxEnergy

// madeBytes is what one run may make, as energy: the boot of one file, one request, a job (core/runtime, memory.go).
var madeBytes = runtime.Limits().DefaultMaxBytes

// answer writes one of the engine's own answers: text, never stored, never sniffed.
func answer(w http.ResponseWriter, status int, text string) {
	header := w.Header()
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	w.Write([]byte(text + "\n"))
}

// protection refuses a write from another site's page (docs/KITWORK_ROUTER.md mục 5, "Nguồn của
// request"): a request that is not GET, HEAD or OPTIONS from a page of another origin, same-site included.
var protection = http.NewCrossOriginProtection()

// ServeHTTP answers one request of the site: the folder its path leads to, the door of its method, the
// handler run on a VM of the site, the answer the handler returns. Its run is bounded by the default
// time (Host.RequestTimeout).
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, defaultRequestTimeout)
}

// serve is ServeHTTP with the time the handler's run may take.
func (s *Site) serve(w http.ResponseWriter, r *http.Request, timeout time.Duration) {
	if s.closed != nil {
		answer(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	// what the engine serves to a page (/_kitwork/), then a file of assets/, of the catalogue or of
	// .well-known/, go out as they are, with no VM; any other path, and a file that is not there, is the
	// routes'
	if r.URL.Path == changesPath {
		s.serveChanges(w, r) // a tab's channel (tab.go), before the files under /_kitwork/
		return
	}
	if engineFile(w, r) {
		return
	}
	if s.static != nil && s.static.Serve(w, r) {
		return
	}
	// a place among the requests the site runs at once (gate.go), given back as the request ends, or as its stream
	// opens (call.leaveGate)
	if !s.gate.enter(r.Context().Done()) {
		s.busy(w)
		return
	}
	var request *call
	defer func() {
		if request == nil || !request.leftGate {
			s.gate.leave()
		}
	}()
	if name, refresh, ok := takeCall(r); ok {
		s.serveCall(w, r, name, refresh, timeout)
		return
	}
	params := map[string]value.Value{}
	parts := segments(r.URL.Path)
	folder := s.root.find(parts, params)
	method := strings.ToLower(r.Method)
	if method == "options" {
		if folder == nil {
			s.notFound(w, r)
			return
		}
		w.Header().Set("Allow", folder.allow())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// the rules of the folders the request goes through (docs/router.md mục 4): a path no folder answers meets those
	// of the folders it reaches, and a method no door takes those of its folder, before the 404 or the 405, so a
	// guard of a branch guards every path of it and may answer for them
	var door *router.Door
	reached := folder
	if folder != nil {
		door, _ = folder.door(method)
	} else {
		reached = s.root.reach(parts, params) // find left params as they were, every parameter it tried taken back
	}
	steps := s.steps(reached, method, door)
	if door == nil && len(steps) == 0 {
		s.unanswered(w, r, folder)
		return
	}
	if err := protection.Check(r); err != nil {
		answer(w, http.StatusForbidden, "Forbidden")
		return
	}
	request = &call{site: s, w: w, r: r, folder: folder, door: door, parts: parts, timeout: timeout}
	request.context = s.newContext(r, params)
	defer request.release()
	if door != nil && door.View && door.Handler != nil {
		request.reads = &tabReads{} // a page of router.view notes what it reads, for its tab (tab.go)
	}
	if !request.ruled(steps) {
		return
	}
	switch {
	case door == nil:
		s.unanswered(w, r, folder)
	case door.View && door.Handler == nil:
		s.drawPage(w, r, folder)
	default:
		request.handle()
	}
}

// busy is the answer of a request that found no place among those the site runs at once, waiting as long as the gate
// waits: 503, to be asked again in a second, and a line in the log at most once a second.
func (s *Site) busy(w http.ResponseWriter) {
	if line := s.gate.refusal(time.Now()); line != "" {
		s.log.Led(s.lead, line)
	}
	w.Header().Set("Retry-After", "1")
	answer(w, http.StatusServiceUnavailable, "Service Unavailable")
}

// unanswered is the answer of a request no door takes: 404 for a path no folder answers, 405 with the folder's
// methods for a method its folder has no door for.
// newContext is the context of a request to the site, with the values of its route's parameters and the files of
// its app, which context.file serves.
func (s *Site) newContext(r *http.Request, params map[string]value.Value) *router.Context {
	made := router.NewContext(r, params)
	made.Files = s.files
	return made
}

func (s *Site) unanswered(w http.ResponseWriter, r *http.Request, folder *Folder) {
	if folder == nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Allow", folder.allow())
	answer(w, http.StatusMethodNotAllowed, "Method Not Allowed")
}

// step is one rule a request meets; door says it is written before the door, whose limits count each path apart.
type step struct {
	rule router.Rule
	door bool
}

// steps are the rules a request of method meets, in the order they run (docs/router.md mục 4): the app root's, the
// site root's, each folder's down to reached, its own (config.work's before router.work's, each chain in the order
// written), for the methods each chain names, then those written before its door. None is nil.
func (s *Site) steps(reached *Folder, method string, door *router.Door) []step {
	steps := branchSteps(nil, s.app, method)
	steps = folderSteps(steps, reached, method)
	if door != nil {
		for _, rule := range door.Rules {
			steps = append(steps, step{rule: rule, door: true})
		}
	}
	return steps
}

// folderSteps are steps with the rules of the folders from the site's root down to folder, the root's first.
func folderSteps(steps []step, folder *Folder, method string) []step {
	if folder == nil {
		return steps
	}
	return branchSteps(folderSteps(steps, folder.parent, method), folder.scope, method)
}

// branchSteps are steps with the rules of scope's chains for method.
func branchSteps(steps []step, scope *router.Scope, method string) []step {
	if scope == nil {
		return steps
	}
	for _, branch := range scope.Branches {
		if branch.Applies(method) {
			for _, rule := range branch.Rules {
				steps = append(steps, step{rule: rule})
			}
		}
	}
	return steps
}

// call is one request on its way through its rules to its door: the VM its guards and its handler run on, taken
// once, and the energy they spent and the bytes they made together.
type call struct {
	site    *Site
	w       http.ResponseWriter
	r       *http.Request
	folder  *Folder
	door    *router.Door
	parts   []string
	timeout time.Duration
	context *router.Context
	work    *worker
	spent   uint64
	made    uint64
	// leftGate is set once the request gave its place back before it ended: a stream, which has a ceiling of its own
	leftGate bool
	// reads notes the keys of kitwork/storage the request's runs read, for the tab its page is drawn for (tab.go);
	// nil for a request that draws no page of router.view
	reads *tabReads
}

// ruled runs the request's rules in order and reports whether it goes on to its door: a limit over its count answers
// 429, a guard that does not let it on has answered it.
func (c *call) ruled(steps []step) bool {
	for _, step := range steps {
		if limit := step.rule.Limit; limit != nil {
			scope := ""
			if step.door {
				scope = "/" + strings.Join(c.parts, "/") // the path as routed: /a/ and /a//b count as /a and /a/b
			}
			if within, wait := limit.Take(c.r, scope, time.Now()); !within {
				c.w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds())))) // a wait is above 0: 1 at least
				answer(c.w, http.StatusTooManyRequests, "Too Many Requests")
				return false
			}
			continue
		}
		if !c.guard(step.rule) {
			return false
		}
	}
	return true
}

// guard runs one guard (docs/router.md mục 5) and reports whether it let the request on: return context.next() does;
// an answer is sent, but a page, which a guard does not draw; anything else it returns refuses with 403, the reason in
// the log; a failure it does not catch is the request's, as a handler's.
func (c *call) guard(rule router.Rule) bool {
	where := rule.At + ", router.guard"
	c.context.Guarding(true)
	result, ok := c.execute(rule.Guard)
	c.context.Guarding(false)
	if !ok {
		c.site.fail(c.w, c.r, where, "the guard has no program")
		return false
	}
	if c.context.LetOn(result) && !c.context.BodyTooLarge() {
		return true
	}
	switch response, answered := c.context.Answer(result); {
	case c.context.BodyTooLarge():
		answer(c.w, http.StatusRequestEntityTooLarge, "Request Entity Too Large")
	case answered && (isView(response) || response.Word() == "bind"):
		// the boot refuses a page it sees the guard draw (core/compiler router_guard.go); one it cannot see, a
		// function's that draws it, is refused here, as the page of context.view(…) is
		c.site.forbid(c.w, c.r, where, "the guard drew a page, context."+response.Word()+"(…): a guard lets the request on with return context.next() or answers it, never with a page")
	case answered:
		response.Write(c.w, c.r)
	case result.IsInvalid():
		c.site.failed(c.w, c.r, where, result)
	default:
		c.site.forbid(c.w, c.r, where, "the guard returned "+describe(result)+": a guard lets the request on with return context.next() and refuses with return context.error(…) or another answer")
	}
	return false
}

// handle runs the door's handler and writes what it answers: a handler that read context.sse answers with its stream,
// which it returns (stream.go).
func (c *call) handle() {
	if c.door.View {
		c.context.Viewing()
	}
	result, ok := c.execute(c.door.Handler)
	if !ok {
		c.site.fail(c.w, c.r, c.where(), "the handler has no program")
		return
	}
	stream := c.context.Opened()
	switch response, answered := c.context.Answer(result); {
	case c.context.BodyTooLarge():
		answer(c.w, http.StatusRequestEntityTooLarge, "Request Entity Too Large")
	case stream != nil && result.Host() == any(stream):
		c.stream(stream)
	case stream != nil && !result.IsInvalid():
		c.site.fail(c.w, c.r, c.where(), "the handler read context.sse, which answers with a stream, and returned "+describe(result)+": it returns the stream, return stream (engine2/docs/sse.md mục 2)")
	case answered && c.door.View && !viewAnswers[response.Word()]:
		c.site.fail(c.w, c.r, c.where(), "router.view answered with context."+response.Word()+"(…): it draws its page with return context.bind({ … }), or context.sync({ … }).bind({ … }), or answers redirect, notFound or error (docs/view.md mục 2)")
	case answered && response.Word() == "bind" && !c.door.View && !c.folder.viewing():
		c.site.fail(c.w, c.r, c.where(), "context.bind(…) draws the page of router.view: router."+strings.ToLower(c.r.Method)+"(…) answers with it only in a folder that has router.view (docs/view.md mục 2)")
	case answered && response.Word() == "bind":
		c.site.drawBound(c.w, c.r, c.folder, response, c.context.Held(), c.reads)
	case answered && isView(response):
		c.site.drawView(c.w, c.r, c.folder, response)
	case answered:
		response.Write(c.w, c.r)
	case result.IsInvalid():
		c.site.failed(c.w, c.r, c.where(), result)
	default:
		c.site.fail(c.w, c.r, c.where(), "the handler returned "+describe(result)+", not an answer: return context.text(…), context.json(…) …")
	}
}

// execute runs fn, a guard's or the handler's, with the request's context, on the VM the request took at its first
// run: one request's energy is what its guards and its handler spend together, and its time is one too.
func (c *call) execute(fn *value.Lambda) (value.Value, bool) {
	return c.run(fn, []value.Value{value.Struct(c.context)})
}

// run runs fn with arguments on the request's VM, as execute does: a work of the page and the function that reads a
// var run on it too, with what the request has left (bridge.go).
func (c *call) run(fn *value.Lambda, arguments []value.Value) (value.Value, bool) {
	program, ok := runtime.ProgramFromRef(fn.Program)
	if !ok {
		return value.Value{}, false
	}
	if c.work == nil {
		c.work, _ = c.site.machines.Get().(*worker)
		if c.work == nil {
			c.work = &worker{machine: runtime.New(program)}
		}
		// the run ends with the request or at its time, whichever comes first (deadline.go): the VM reads
		// its context every 64 instructions, so a run of a few long instructions stops too
		c.work.deadline.start(c.r.Context(), c.timeout)
		c.work.deadline.listener = c.context // observe(…) of the run keeps its observer for the request
		if c.reads != nil {
			c.work.deadline.reads = c.reads // the keys the runs read, a tab's (tab.go)
		}
	}
	machine := c.work.machine
	machine.FastReset(program, c.site.globals)
	machine.Builtins = c.site.builtins
	// what the request has left; never 0, which is no ceiling at all
	machine.MaxEnergy = max(c.site.energy-min(c.spent, c.site.energy), 1)
	machine.MaxBytes = max(c.site.made-min(c.made, c.site.made), 1)
	machine.Pool = c.site.pool
	machine.Context = &c.work.deadline
	result := machine.ExecuteLambda(fn, arguments)
	c.spent += machine.Energy
	c.made += machine.Bytes
	return result, true
}

// leaveGate gives the request's place among those the site runs at once back before it ends (gate.go): a stream's,
// which stays open long after its handler ran and counts against the streams' own ceiling.
func (c *call) leaveGate() {
	if !c.leftGate {
		c.leftGate = true
		c.site.gate.leave()
	}
}

// release ends what the request observes and its stream, and gives the VM back: it lets go of the request (its
// context, its values) before another request of the site takes it.
func (c *call) release() {
	c.context.End()
	if c.work == nil {
		return
	}
	c.work.machine.ResetForPool()
	c.work.deadline.stop()
	c.site.machines.Put(c.work)
	c.work = nil
}

// failed answers a failure a guard or a handler did not catch: detail's row not found is the route's 404, data from
// outside that could not be read 400, any other 500, the reason in the log.
func (s *Site) failed(w http.ResponseWriter, r *http.Request, where string, result value.Value) {
	switch {
	case result.Code() == value.NotFoundCode && strings.Contains(result.Message(), database.NoRow):
		// detail found no row: the route's 404, the site's own page of it when it has one, the reason in the log
		// (docs/KITWORK_DATABASE.md mục 5); another NOT_FOUND (env.require) is the site's 500
		s.log.Led(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+where+"): 404: "+result.Failure())
		s.notFound(w, r)
	case runtime.HostBusy(result):
		// the host's runs hold all it gives (runtime.Pool): the host is busy, not the site wrong
		s.log.Warn(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+where+"): 503: "+result.Failure())
		w.Header().Set("Retry-After", "1")
		answer(w, http.StatusServiceUnavailable, "Service Unavailable")
	case result.Code() == value.InvalidInputCode:
		// data from outside that could not be read (a body that is not JSON, a struct's parse) is the
		// visitor's to fix, not the site's: 400, the reason in the log as a 500's (06/11/2026)
		s.refuse(w, r, where, result.Failure())
	default:
		s.fail(w, r, where, result.Failure())
	}
}

// fail answers 500 and writes why in the log, never in the answer: a failure's text may name a file, a
// line, a value of the program.
func (s *Site) fail(w http.ResponseWriter, r *http.Request, where, why string) {
	s.log.Error(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+where+"): 500: "+why)
	answer(w, http.StatusInternalServerError, "Internal Server Error")
}

// refuse answers 400 for a failure of data from outside the handler did not catch, and writes why in the log,
// never in the answer: as fail's, the text may name a field or a file.
func (s *Site) refuse(w http.ResponseWriter, r *http.Request, where, why string) {
	s.log.Warn(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+where+"): 400: "+why)
	answer(w, http.StatusBadRequest, "Bad Request")
}

// forbid answers 403 for a guard that neither let the request on nor answered it as a guard answers: closed when
// in doubt (docs/router.md mục 5), the reason in the log.
func (s *Site) forbid(w http.ResponseWriter, r *http.Request, where, why string) {
	s.log.Warn(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+where+"): 403: "+why)
	answer(w, http.StatusForbidden, "Forbidden")
}

// describe names what a handler returned instead of an answer.
func describe(result value.Value) string {
	switch {
	case result.IsUndefined():
		return "nothing"
	case result.IsStruct():
		return "a value of the engine"
	}
	return "a " + result.Kind()
}

// segments are the path's segments, cleaned: /a/, /a//b and /a/./b are /a and /a/b.
func segments(urlPath string) []string {
	var out []string
	for _, segment := range strings.Split(path.Clean("/"+urlPath), "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

// viewAnswers are what router.view answers with (docs/view.md mục 2): its page, or a redirect, a 404, an error.
var viewAnswers = map[string]bool{"bind": true, "redirect": true, "notFound": true, "error": true}

// isView reports an answer of context.view(…), a page the host draws.
func isView(response *router.Response) bool {
	_, _, _, ok := response.Page()
	return ok
}
