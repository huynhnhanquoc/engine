package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/ui"
)

// The bridge on the server (docs/view.md mục 4, docs/tunnel.md mục 1, slice 2): a work of the page, a function its
// router.view binds, called from the browser as a POST to the page's own URL with the work's name in the query.
//
//	POST /todo?kitwork-call=remove
//	{ "seq": 4, "args": [3], "page": { "title": "" } }
//
// The name is taken out of the URL before anything reads it: the rules and the view's function read the page's own.
// The call meets the origin check, then the rules of the folders down to the page that a GET or a POST meets and those
// written before router.view, its limit counting calls with the page. Then router.view's function runs with the vars
// the page held (library/router, bridge.go), the work runs on what it bound, each var is read back, and the function
// runs again with them, past the guards again, with the cookies the work set as the browser sends them next. The
// answer is the names whose value the second run changed, never the page:
//
//	{ "seq": 4, "page": { "left": 1, "todos": [ … ] } }
//
// A work of a site component the page draws is called the same way, named by the component and the work,
// ?kitwork-call=account-menu.signIn, with the place its instance is called from and the vars it holds:
//
//	{ "seq": 5, "at": "c3fq8w", "page": { … }, "c3fq8w": { "opened": true } }
//
// Its inputs are never the browser's: the page is drawn again on the server, and the instance drawn at that place
// runs with the vars sent, its work on what it bound, then the page again; the answer is what changed of the page
// and of the instance, each under its place: { "seq": 5, "page": { … }, "c3fq8w": { "opened": false } }.
//
// A work that sends the page away answers { seq, redirect }; a call refused or failed answers
// { seq, error: { code, message } } with code as its status, the message public: the status's own text, or the one an
// answer of the site wrote, context.error("…"); the reason of a failure goes to the log. A work's return value is
// dropped. One call of a page runs at a time, from the run before the work to the one after it: two calls that both
// read before either wrote would keep one write of the two.

// callParameter names the work of a call in the page's URL: the URL, not a header, because the shell of a native app
// passes a request's method, URL and body as a browser does, and not always its headers (docs/KITWORK_BRIDGE.md mục 9).
const callParameter = "kitwork-call"

// callLimit is the most a call's body may hold: the arguments and the page's vars.
const callLimit = 1 << 20

// takeCall takes out of r's URL the work its query names, or its ask for the page again (kitwork-refresh, tab.go),
// before anything reads the URL; ok is false when it names neither.
func takeCall(r *http.Request) (name string, refresh, ok bool) {
	if !strings.Contains(r.URL.RawQuery, "kitwork-") {
		return "", false, false
	}
	var kept []string
	for _, part := range strings.Split(r.URL.RawQuery, "&") {
		key, written, _ := strings.Cut(part, "=")
		unescaped, err := url.QueryUnescape(key)
		switch {
		case err == nil && unescaped == callParameter:
			name, _ = url.QueryUnescape(written)
			ok = true
		case err == nil && unescaped == refreshParameter:
			refresh = true
		default:
			kept = append(kept, part)
			continue
		}
	}
	if ok || refresh {
		r.URL.RawQuery = strings.Join(kept, "&")
		r.RequestURI = r.URL.RequestURI()
	}
	return name, refresh && !ok, ok || refresh
}

// componentWork reads name as a work of a site component, account-menu.signIn: the tag and the work; ok is false for
// a name with no dot.
func componentWork(name string) (tag, work string, ok bool) {
	tag, work, ok = strings.Cut(name, ".")
	return tag, work, ok
}

// siteTag says tag can be a site component's: lower case words and digits joined by single dashes, one at least.
func siteTag(tag string) bool {
	if len(tag) > 64 || tag == "" || tag[0] < 'a' || tag[0] > 'z' || !strings.Contains(tag, "-") || strings.HasSuffix(tag, "-") || strings.Contains(tag, "--") {
		return false
	}
	for i := 0; i < len(tag); i++ {
		if c := tag[i]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// placeName says at can be the place of an instance as a page writes it (library/ui bridge.go): c, a hash in digits
// and lower case letters, then -2, -3… for a second instance of one place.
func placeName(at string) bool {
	if len(at) < 2 || len(at) > 24 || at[0] != 'c' {
		return false
	}
	hash, count, two := strings.Cut(at[1:], "-")
	if hash == "" || two && count == "" {
		return false
	}
	for i := 0; i < len(hash); i++ {
		if c := hash[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	for i := 0; i < len(count); i++ {
		if c := count[i]; c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// workName says name can be a work's: a name the page reads, letters, digits, _ and $, at most 64.
func workName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// bridgeCall is one call as the browser sent it, none of it trusted: the work, the call's number, which its answer
// carries back, the arguments, and the vars the page held.
type bridgeCall struct {
	name string
	seq  float64
	args []value.Value
	page value.Value
	// tag, work, at and held are a call of a work of a site component: its tag, the work, the place of the
	// instance, the vars it held
	tag, work, at string
	held          value.Value
	// tab is the tab the page was drawn for, "" for none (tab.go); places are the vars each instance held, by place,
	// when the tab asks for its page again
	tab    string
	places map[string]value.Value
}

// readCall reads the call of r's body, or the ask for the page again when refresh; problem says why it cannot be
// read, the request's own fault (400).
func readCall(r *http.Request, name string, refresh bool) (call *bridgeCall, problem string) {
	tag, work, instance := componentWork(name)
	if !refresh && (instance && (!siteTag(tag) || !workName(work)) || !instance && !workName(name)) {
		return nil, callParameter + "=" + strconv.Quote(name) + " names no work: a work is a name of the page, or a component's, account-menu.signIn"
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, callLimit+1))
	if err != nil {
		return nil, "the call's body could not be read"
	}
	if len(body) > callLimit {
		return nil, "the call's body is over " + strconv.Itoa(callLimit) + " bytes"
	}
	call = &bridgeCall{name: name}
	if instance {
		call.tag, call.work = tag, work
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if refresh {
			return nil, "an ask for the page again names its tab"
		}
		if instance {
			return nil, "a work of " + tag + " is called on an instance: at names its place"
		}
		return call, ""
	}
	sent := value.ParseJSON(string(body))
	if !sent.IsObject() {
		return nil, "the call's body is no JSON object { seq, args, page }"
	}
	if seq := sent.Get("seq"); !seq.IsUndefined() {
		number := seq.Number()
		if !seq.IsNumber() || number < 0 || number != float64(int64(number)) || number > 1<<53 {
			return nil, "seq is the call's number, a whole number from 0"
		}
		call.seq = number
	}
	switch args := sent.Get("args"); {
	case args.IsUndefined():
	case !args.IsArray():
		return nil, "args are the work's arguments, an array"
	default:
		call.args = args.Items()
	}
	switch page := sent.Get("page"); {
	case page.IsUndefined():
	case !page.IsObject():
		return nil, "page holds the vars of the page by name, an object"
	default:
		call.page = page
	}
	switch tab := sent.Get("tab"); {
	case tab.IsUndefined() && !refresh:
	case !tab.IsString() || !tabID(tab.Text()):
		return nil, "tab is the id of the tab the page was drawn for"
	default:
		call.tab = tab.Text()
	}
	if refresh {
		// the vars of each instance, by place
		call.places = map[string]value.Value{}
		for _, key := range sent.Keys() {
			if !placeName(key) {
				continue
			}
			if held := sent.Get(key); held.IsObject() {
				call.places[key] = held
			} else {
				return nil, key + " holds the vars of the instance by name, an object"
			}
		}
		return call, ""
	}
	if instance {
		at := sent.Get("at")
		if !at.IsString() || !placeName(at.Text()) {
			return nil, "a work of " + tag + " is called on an instance: at names its place, as the page writes it (c3fq8w)"
		}
		call.at = at.Text()
		switch held := sent.Get(call.at); {
		case held.IsUndefined():
		case !held.IsObject():
			return nil, call.at + " holds the vars of the instance by name, an object"
		default:
			call.held = held
		}
	}
	return call, ""
}

// callLock is the lock of a page's calls, one call of the page at a time.
func (s *Site) callLock(folder *Folder) *sync.Mutex {
	lock, _ := s.calls.LoadOrStore(folder, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// callSteps are the rules a call of folder's page meets: those of the folders from the app's root down to it that a
// GET or a POST meets, each once in the order written, then those written before router.view.
func (s *Site) callSteps(folder *Folder, door *router.Door) []step {
	var steps []step
	add := func(scope *router.Scope) {
		if scope == nil {
			return
		}
		for _, branch := range scope.Branches {
			if branch.Applies("get") || branch.Applies("post") {
				for _, rule := range branch.Rules {
					steps = append(steps, step{rule: rule})
				}
			}
		}
	}
	add(s.app)
	var down func(f *Folder)
	down = func(f *Folder) {
		if f != nil {
			down(f.parent)
			add(f.scope)
		}
	}
	down(folder)
	for _, rule := range door.Rules {
		steps = append(steps, step{rule: rule, door: true})
	}
	return steps
}

// guardsOf are the guards of steps, its limits aside: what a call meets again before the page is run after the work.
func guardsOf(steps []step) []step {
	var guards []step
	for _, step := range steps {
		if step.rule.Limit == nil {
			guards = append(guards, step)
		}
	}
	return guards
}

// serveCall answers a call of a work of a page (the file's head says how).
func (s *Site) serveCall(w http.ResponseWriter, r *http.Request, name string, refresh bool, timeout time.Duration) {
	reply := &callReply{w: w}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		reply.error(http.StatusMethodNotAllowed, "")
		return
	}
	params := map[string]value.Value{}
	parts := segments(r.URL.Path)
	folder := s.root.find(parts, params)
	_, _, instance := componentWork(name)
	var door *router.Door
	if folder != nil {
		door, _ = folder.door("get")
	}
	// a work of the page is router.view's; a component's, and an ask for the page again, any page's, router.view's
	// or a content.ui with no door
	if door == nil || !door.View || !instance && !refresh && door.Handler == nil {
		reply.error(http.StatusNotFound, "")
		return
	}
	if err := protection.Check(r); err != nil {
		reply.error(http.StatusForbidden, "")
		return
	}
	sent, problem := readCall(r, name, refresh)
	if problem != "" {
		parameter := callParameter
		if refresh {
			parameter = refreshParameter
		}
		s.log.Led(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+parameter+"): 400: "+problem)
		reply.error(http.StatusBadRequest, "")
		return
	}
	reply.seq = sent.seq
	lock := s.callLock(folder)
	lock.Lock()
	defer lock.Unlock()
	held := &captured{header: http.Header{}}
	request := &call{site: s, w: held, r: r, folder: folder, door: door, parts: parts, timeout: timeout}
	request.context = s.newContext(r, params)
	request.reads = &tabReads{}
	defer request.release()
	steps := s.callSteps(folder, door)
	if refresh {
		// asked because the data changed, not by the person: the guards, never a limit
		steps = guardsOf(steps)
	}
	if !request.ruled(steps) {
		reply.captured(held)
		return
	}
	if refresh {
		request.refreshPage(sent, reply, held)
		return
	}
	if sent.tag != "" {
		request.instanceWork(sent, reply, held, params, guardsOf(steps))
		return
	}
	request.callWork(sent, reply, held, params, guardsOf(steps))
}

// instanceWork runs the work of a site component's instance the page draws: the page drawn again on the server,
// the instance at the place sent run with the vars it held, its work on what it bound, each var read back, then the
// page and the instance again, past the guards again with the cookies set; the answer is what changed of each.
func (c *call) instanceWork(sent *bridgeCall, reply *callReply, held *captured, params map[string]value.Value, guards []step) {
	first := c.context
	first.Calling(sent.page)
	defer first.End()
	viewed := c.door.Handler != nil
	where := c.where() + ", <" + sent.tag + "> at " + sent.at
	var before value.Value
	if viewed {
		names, _, ok := c.bound(first, c.where()+", router.view", reply, held)
		if !ok {
			return
		}
		before = names
	}
	components := c.site.pageComponents(c.r)
	if components == nil || !components.Defines(sent.tag) {
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 404: the site defines no component "+sent.tag)
		reply.cookies(first)
		reply.error(http.StatusNotFound, "")
		return
	}
	defer components.release()
	components.call = &instanceCall{tag: sent.tag, place: sent.at, vars: sent.held}
	if !c.drawn(components, before, first, where, reply) {
		return
	}
	instance := components.call
	work := instance.names.Get(sent.work)
	if !work.IsFunction() || work.Lambda() == nil {
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 404: the instance binds no work "+sent.work)
		reply.cookies(first)
		reply.error(http.StatusNotFound, "")
		return
	}
	own := instance.context
	own.Reopen()
	result, _ := components.run(work.Lambda(), sent.args)
	where += ", work " + sent.work
	reply.cookies(first)
	switch response, answered := own.Answer(result); {
	case result.IsInvalid():
		c.callFailed(reply, own, where, result)
		return
	case answered && response.Word() == "redirect":
		reply.cookies(own)
		reply.redirect(response)
		return
	case answered && (response.Word() == "error" || response.Word() == "notFound"):
		reply.cookies(own)
		reply.answered(response)
		return
	case answered:
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 500: the work answered with context."+
			response.Word()+"(…): a work changes the instance's data and vars, and answers nothing, or a redirect, notFound or error (docs/view.md mục 4)")
		reply.cookies(own)
		reply.error(http.StatusInternalServerError, "")
		return
	}
	vars := value.Object()
	for _, each := range own.Held() {
		read, _ := components.run(each.Read, nil)
		if read.IsInvalid() {
			c.callFailed(reply, own, where, read)
			return
		}
		vars.Set(each.Name, read)
	}
	reply.cookies(own)
	sentNext(c.r, append(append([]*http.Cookie{}, first.Cookies()...), own.Cookies()...))
	next := c.site.newContext(c.r, params)
	defer next.End()
	next.Calling(sent.page)
	c.context = next
	if !c.ruled(guards) {
		reply.captured(held)
		return
	}
	var after value.Value
	draw := &pageDraw{tab: sent.tab, reads: c.reads, places: map[string]livePlace{}}
	if viewed {
		names, live, ok := c.bound(next, c.where()+", router.view after the work "+sent.name, reply, held)
		if !ok {
			return
		}
		after = names
		draw.note("page", names, live)
		reply.cookies(next)
	}
	was := instance.names
	components.call = &instanceCall{tag: sent.tag, place: sent.at, vars: vars}
	components.draw = draw // the instances as the work left them: what the tab holds once answered
	if !c.drawn(components, after, next, where, reply) {
		return
	}
	tracked := sent.tab != ""
	pagePart := part{place: "page", before: before, after: after, live: draw.places["page"].live, tracked: tracked}
	instancePart := part{place: sent.at, before: was, after: components.call.names, live: draw.places[sent.at].live, tracked: tracked}
	if tracked {
		pagePart.held, instancePart.held = c.site.tabs.held(sent.tab, "page"), c.site.tabs.held(sent.tab, sent.at)
		c.site.tabs.keep(sent.tab, c.reads.list(), draw.places)
	}
	reply.parts([]part{pagePart, instancePart})
}

// drawn draws the page with data for a call of a component's work, its components run by components, and says
// whether it drew the instance the call names; when not, or when the page could not be drawn, the call is answered:
// 404 for an instance the page no longer draws there (the tab opens the page again), 500 for a page that fails. What
// it draws is not sent: the instance's names are what the call needs of it.
func (c *call) drawn(components *pageComponents, data value.Value, context *router.Context, where string, reply *callReply) bool {
	route := filepath.Join(c.site.Folder, filepath.FromSlash(c.folder.Path))
	assembled, err := c.site.ui.Assembled(route)
	if err == nil {
		head := c.site.pageHead(c.r, c.folder, "", "", nil)
		head.Components = components
		_, _, err = c.site.ui.Document(assembled, data, head, c.folder.look)
	}
	switch {
	case components.call.failure.IsInvalid():
		c.callFailed(reply, context, where, components.call.failure) // the instance's own run: what the call sent
		return false
	case err != nil:
		c.drawFailed(where, err)
		reply.cookies(context)
		reply.error(drawStatus(reply, err), "")
		return false
	case !components.call.found:
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 404: the page draws no <"+components.call.tag+"> at "+components.call.place)
		reply.cookies(context)
		reply.error(http.StatusNotFound, "")
		return false
	}
	return true
}

// callWork runs the work of sent on what router.view's function binds with the vars the page held, then the function
// again, and answers what changed.
func (c *call) callWork(sent *bridgeCall, reply *callReply, held *captured, params map[string]value.Value, guards []step) {
	first := c.context
	first.Calling(sent.page)
	where := c.where() + ", router.view"
	before, _, ok := c.bound(first, where, reply, held)
	if !ok {
		return
	}
	work := before.Get(sent.name)
	if !work.IsFunction() || work.Lambda() == nil {
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 404: the page binds no work "+sent.name)
		reply.cookies(first)
		reply.error(http.StatusNotFound, "")
		return
	}
	first.Reopen()
	result, _ := c.run(work.Lambda(), sent.args)
	where = c.where() + ", work " + sent.name
	switch response, answered := first.Answer(result); {
	case result.IsInvalid():
		c.callFailed(reply, first, where, result)
		return
	case answered && response.Word() == "redirect":
		reply.cookies(first)
		reply.redirect(response)
		return
	case answered && (response.Word() == "error" || response.Word() == "notFound"):
		reply.cookies(first)
		reply.answered(response)
		return
	case answered:
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 500: the work answered with context."+
			response.Word()+"(…): a work changes the page's data and vars, and answers nothing, or a redirect, notFound or error (docs/view.md mục 4)")
		reply.cookies(first)
		reply.error(http.StatusInternalServerError, "")
		return
	}
	// the vars the work left: the page is run again with them
	vars := value.Object()
	for _, each := range first.Held() {
		read, _ := c.run(each.Read, nil)
		if read.IsInvalid() {
			c.callFailed(reply, first, where, read)
			return
		}
		vars.Set(each.Name, read)
	}
	reply.cookies(first)
	// the page the work left, as the browser asks for it next: with the cookies the work set, past the guards again
	sentNext(c.r, first.Cookies())
	next := c.site.newContext(c.r, params)
	defer next.End()
	next.Calling(vars)
	c.context = next
	if !c.ruled(guards) {
		reply.captured(held)
		return
	}
	after, live, ok := c.bound(next, c.where()+", router.view after the work "+sent.name, reply, held)
	if !ok {
		return
	}
	answer := part{place: "page", before: before, after: after, live: live, tracked: sent.tab != ""}
	if answer.tracked {
		// the tab holds the page as the work left it, once answered: what it missed comes too
		answer.held = c.site.tabs.held(sent.tab, "page")
		c.site.tabs.keep(sent.tab, c.reads.list(), map[string]livePlace{"page": {names: after, live: live}})
	}
	reply.cookies(next)
	reply.parts([]part{answer})
}

// refreshPage answers a tab that asks for its page again (tab.go): the page's function run with the vars it holds,
// the page drawn for the instances with theirs; each live name that differs from what the record says the tab holds,
// by instance; the record then says what the answer gave, and the keys these runs read.
func (c *call) refreshPage(sent *bridgeCall, reply *callReply, held *captured) {
	first := c.context
	first.Calling(sent.page)
	draw := &pageDraw{tab: sent.tab, reads: c.reads, places: map[string]livePlace{}}
	var names value.Value
	if c.door.Handler != nil {
		bound, live, ok := c.bound(first, c.where()+", router.view, asked again", reply, held)
		if !ok {
			return
		}
		names = bound
		draw.note("page", bound, live)
	}
	if components := c.site.pageComponents(c.r); components != nil {
		defer components.release()
		components.draw, components.vars = draw, sent.places
		route := filepath.Join(c.site.Folder, filepath.FromSlash(c.folder.Path))
		assembled, err := c.site.ui.Assembled(route)
		if err == nil {
			head := c.site.pageHead(c.r, c.folder, "", "", nil)
			head.Components = components
			_, _, err = c.site.ui.Document(assembled, names, head, c.folder.look)
		}
		if err != nil {
			c.drawFailed(c.where()+", asked again", err)
			reply.cookies(first)
			reply.error(drawStatus(reply, err), "")
			return
		}
	}
	places := make([]string, 0, len(draw.places))
	for place := range draw.places {
		places = append(places, place)
	}
	sort.Slice(places, func(i, j int) bool { // the page first, then the instances by place
		return places[i] == "page" || places[j] != "page" && places[i] < places[j]
	})
	var fields []string
	for _, place := range places {
		if changed := liveChanges(c.site.tabs.held(sent.tab, place), draw.places[place]); changed != "" {
			fields = append(fields, place, changed)
		}
	}
	c.site.tabs.keep(sent.tab, c.reads.list(), draw.places)
	reply.cookies(first)
	reply.send(http.StatusOK, fields...)
}

// liveChanges is the live names of place whose value differs from what held says the tab holds, as a JSON object; ""
// when none does. A tab the record does not know holds nothing.
func liveChanges(held map[string]uint64, place livePlace) string {
	var parts []string
	for _, name := range place.live {
		item := place.names.Get(name)
		if hash, ok := held[name]; ok && hash == hashOf(item) {
			continue
		}
		text := "null"
		if written := value.Stringify(item, ""); written.IsString() {
			text = written.Text()
		}
		parts = append(parts, jsonText(name)+":"+text)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// bound runs router.view's function with context and gives the names its bind binds; false when it answered anything
// else, which the call has then answered.
func (c *call) bound(context *router.Context, where string, reply *callReply, held *captured) (value.Value, []string, bool) {
	c.context = context
	context.Viewing()
	result, _ := c.run(c.door.Handler, []value.Value{value.Struct(context)})
	response, answered := context.Answer(result)
	switch {
	case result.IsInvalid():
		c.callFailed(reply, context, where, result)
	case answered && response.Word() == "bind":
		names, _, _, _ := response.Bound()
		return names, response.Live(), true
	case answered && response.Word() == "redirect":
		reply.cookies(context)
		reply.redirect(response)
	case answered && (response.Word() == "error" || response.Word() == "notFound"):
		reply.cookies(context)
		reply.answered(response)
	default:
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): 500: router.view returned "+describe(result)+
			", not its page: it draws with return context.bind({ … }) (docs/view.md mục 2)")
		reply.cookies(context)
		reply.error(http.StatusInternalServerError, "")
	}
	return value.Value{}, nil, false
}

// callFailed answers a failure the page's function or its work did not catch: data from outside that could not be
// read 400 (a var of another kind), any other 500; the reason in the log, never in the answer.
func (c *call) callFailed(reply *callReply, context *router.Context, where string, result value.Value) {
	status := http.StatusInternalServerError
	switch {
	case result.Code() == value.InvalidInputCode:
		status = http.StatusBadRequest
	case runtime.HostBusy(result):
		// the host's runs hold all it gives (runtime.Pool): busy, not wrong
		status = http.StatusServiceUnavailable
		reply.w.Header().Set("Retry-After", "1")
	}
	c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): "+strconv.Itoa(status)+": "+result.Failure())
	reply.cookies(context)
	reply.error(status, "")
}

// drawFailed logs a page the bridge could not draw again: 503 for a host that could not give to an expression of it
// (ui.ErrBusy), 500 else.
func (c *call) drawFailed(where string, err error) {
	status := "500"
	if errors.Is(err, ui.ErrBusy) {
		status = "503"
	}
	c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+where+"): "+status+": the page could not be drawn: "+err.Error())
}

// drawStatus is the status of a page the bridge could not draw again: 503, to be asked again in a second, for a busy
// host; 500 else.
func drawStatus(reply *callReply, err error) int {
	if errors.Is(err, ui.ErrBusy) {
		reply.w.Header().Set("Retry-After", "1")
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// sentNext writes into r's Cookie header the cookies an answer set or removed, as the browser sends them with its
// next request: what the run after a work reads.
func sentNext(r *http.Request, set []*http.Cookie) {
	if len(set) == 0 {
		return
	}
	values, order := map[string]string{}, []string{}
	for _, cookie := range r.Cookies() {
		if _, seen := values[cookie.Name]; !seen {
			order = append(order, cookie.Name)
		}
		values[cookie.Name] = cookie.Value
	}
	for _, cookie := range set {
		if cookie.MaxAge < 0 {
			delete(values, cookie.Name)
			continue
		}
		if _, seen := values[cookie.Name]; !seen {
			order = append(order, cookie.Name)
		}
		values[cookie.Name] = cookie.Value
	}
	var line strings.Builder
	for _, name := range order {
		kept, ok := values[name]
		if !ok {
			continue
		}
		if line.Len() > 0 {
			line.WriteString("; ")
		}
		line.WriteString((&http.Cookie{Name: name, Value: kept}).String())
	}
	r.Header.Set("Cookie", line.String())
}

// captured is what a rule answered a call with, for the call to send as its own answer.
type captured struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captured) Header() http.Header { return c.header }
func (c *captured) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}
func (c *captured) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len() < 4096 {
		c.body.Write(b[:min(len(b), 4096-c.body.Len())])
	}
	return len(b), nil
}

// callReply is the answer of a call: JSON, never stored, with the cookies of every run of it.
type callReply struct {
	w   http.ResponseWriter
	seq float64
}

// cookies adds the cookies context's answer set or removed.
func (r *callReply) cookies(context *router.Context) { context.WriteCookies(r.w.Header()) }

// send writes the answer: seq, then fields, each a name and its JSON.
func (r *callReply) send(status int, fields ...string) {
	var b strings.Builder
	b.WriteString(`{"seq":`)
	b.WriteString(strconv.FormatFloat(r.seq, 'f', -1, 64))
	for i := 0; i+1 < len(fields); i += 2 {
		b.WriteString(",")
		b.WriteString(jsonText(fields[i]))
		b.WriteString(":")
		b.WriteString(fields[i+1])
	}
	b.WriteString("}")
	header := r.w.Header()
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	r.w.WriteHeader(status)
	io.WriteString(r.w, b.String())
}

// error answers { seq, error: { code, message } } with status as its status, the status's text when message is "".
func (r *callReply) error(status int, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	r.send(status, "error", `{"code":`+strconv.Itoa(status)+`,"message":`+jsonText(message)+`}`)
}

// redirect answers { seq, redirect }: the page goes where response leads.
func (r *callReply) redirect(response *router.Response) {
	r.send(http.StatusOK, "redirect", jsonText(response.Location()))
}

// answered answers an error or a notFound of the site: its status, and the text it wrote, which is for the visitor.
func (r *callReply) answered(response *router.Response) {
	status, text := response.Answer()
	r.error(status, text)
}

// captured answers what a rule answered: a redirect is the page's to follow, anything else an error with its status,
// the text of a text answer as its message, the cookies and the wait of a limit with it.
func (r *callReply) captured(held *captured) {
	header := r.w.Header()
	for _, line := range held.header.Values("Set-Cookie") {
		header.Add("Set-Cookie", line)
	}
	if wait := held.header.Get("Retry-After"); wait != "" {
		header.Set("Retry-After", wait)
	}
	status := held.status
	if location := held.header.Get("Location"); status >= 300 && status < 400 && location != "" {
		r.send(http.StatusOK, "redirect", jsonText(location))
		return
	}
	if status < 400 {
		status = http.StatusForbidden
	}
	message := ""
	if strings.HasPrefix(held.header.Get("Content-Type"), "text/plain") {
		message = strings.TrimSpace(held.body.String())
	}
	r.error(status, message)
}

// part is an instance of a call's answer: the page or a component's, by its place, its names before the work and
// after it.
type part struct {
	place         string
	before, after value.Value
	// live are its live names when the call names its tab, held what the record says the tab holds of them: those
	// are answered against held, not before, for a change the tab missed comes too (tab.go)
	live    []string
	held    map[string]uint64
	tracked bool
}

// parts answers each instance's names whose value differs from before to after, under its place: each name with its
// new value, null for one after has not; an instance where nothing changed is left out. A function is a work, never
// a value.
func (r *callReply) parts(instances []part) {
	var fields []string
	for _, each := range instances {
		if changed := changedNames(each); changed != "" {
			fields = append(fields, each.place, changed)
		}
	}
	r.send(http.StatusOK, fields...)
}

// changedNames is the names of an instance whose value the tab does not hold after the work, as a JSON object, ""
// when it holds every one: those whose value differs from before to after; a live name of a tab the record follows
// whose value differs from what the record says the tab holds (a tab the record does not know holds none).
func changedNames(each part) string {
	was, now := texts(each.before), texts(each.after)
	live := map[string]bool{}
	if each.tracked {
		for _, name := range each.live {
			live[name] = true
		}
	}
	var keys []string
	for key, text := range now {
		if live[key] {
			if hash, ok := each.held[key]; !ok || hash != hashOf(each.after.Get(key)) {
				keys = append(keys, key)
			}
			continue
		}
		if was[key] != text {
			keys = append(keys, key)
		}
	}
	for key := range was {
		if _, kept := now[key]; !kept {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var page strings.Builder
	page.WriteString("{")
	for i, key := range keys {
		if i > 0 {
			page.WriteString(",")
		}
		text, kept := now[key]
		if !kept {
			text = "null"
		}
		page.WriteString(jsonText(key) + ":" + text)
	}
	page.WriteString("}")
	return page.String()
}

// texts are the names of a bind as JSON; a work is null in each run, never a change.
func texts(names value.Value) map[string]string {
	out := map[string]string{}
	if !names.IsObject() {
		return out
	}
	for _, key := range names.Keys() {
		item := names.Get(key)
		if written := value.Stringify(item, ""); written.IsString() {
			out[key] = written.Text()
		} else {
			out[key] = "null"
		}
	}
	return out
}

// bridgeOf is what a page of router.view runs in the browser with: the names it binds that are works, those that are
// vars its function declared, in the order bound, and live, the data names of its sync; nil when it has none of the
// three, a page the server prints.
func bridgeOf(names value.Value, held []router.Held, live []string) *ui.Bridge {
	if !names.IsObject() {
		return nil
	}
	declared := map[string]bool{}
	for _, each := range held {
		declared[each.Name] = true
	}
	bridge := &ui.Bridge{}
	for _, key := range names.Keys() {
		switch {
		case names.Get(key).IsFunction():
			bridge.Works = append(bridge.Works, key)
		case declared[key]:
			bridge.Vars = append(bridge.Vars, key)
		}
	}
	bridge.Live = live
	if len(bridge.Works)+len(bridge.Vars)+len(bridge.Live) == 0 {
		return nil
	}
	return bridge
}

// jsonText is s as a JSON string.
func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
