package storage

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The ceilings of the storage of one app (mục 9; the numbers are open, mục 10.2): the first engine's.
const (
	// valueLimit is the largest value, and the largest data of a message, in encoded bytes (mục 5).
	valueLimit = 1 << 20
	// keyLimit is the longest key, its name and segments together, in bytes.
	keyLimit = 4096
	// memoryKeys and memoryBytes bound memory(); past them the key used least recently is pushed out.
	memoryKeys  = 10_000
	memoryBytes = 64 << 20
	// diskKeys and diskBytes bound disk(), datastoreKeys and datastoreBytes datastore(): past them a write fails.
	diskKeys       = 100_000
	diskBytes      = 1 << 30
	datastoreKeys  = 100_000
	datastoreBytes = 256 << 20
)

// place is one of the three places of one app.
type place interface {
	get(key string) ([]byte, bool, error)
	expiry(key string) (time.Time, bool, error)
	set(key string, data []byte, expireAt time.Time) error
	remove(key string) (bool, error)
	// clear drops the values of key and of the keys under it (an encoded key begins with its branch's: each part is
	// written with its length, so a prefix of whole parts is one branch), and says how many lived
	clear(branch string) (int, error)
	close()
}

// App is the storage of one app, which its sites share: memory() in its RAM, disk() and datastore() in its
// .database/, the single flight of get(() => …) of its three places.
type App struct {
	// Zone is the app's (module.timezone of its module.work): expire("nextday 03:00") is in it
	Zone *time.Location

	places  map[Place]place
	flights flights
	// hub carries send(…) to observe(…) (hub.go)
	hub hub
	// changed is told each key a write changed, the branch of a clear: the tabs that read it ask for their page
	// again (host, tab.go); set once at boot
	changed func(key string, branch bool)
}

// OnChange tells changed each key a set or a delete changed (branch false) and each branch a clear emptied (true):
// a key the encoding of its place, name and segments (Key.Encoded), a branch the prefix of every key under it. Set
// once, as the app boots.
func (a *App) OnChange(changed func(key string, branch bool)) { a.changed = changed }

// ReadsKey is what the context.Context of a run gives its Reads under: a run that draws a page notes each key it
// reads (Handle.Attend), for the tab that shows the page to be told when one is written.
type ReadsKey struct{}

// Reads is what notes the keys a run reads.
type Reads interface {
	Read(key string)
}

// NewApp is the storage of the app at root, its datastore() opened by open (the file's one writer of the process).
func NewApp(root string, open Opener) *App {
	return &App{
		places: map[Place]place{
			Memory:    newMemory(memoryKeys, memoryBytes),
			Disk:      &diskPlace{app: root, maxKeys: diskKeys, maxBytes: diskBytes},
			Datastore: &datastorePlace{app: root, open: open, maxKeys: datastoreKeys, maxBytes: datastoreBytes},
		},
		flights: flights{running: map[string]*flight{}, waiting: map[*runtime.VM]*flight{}},
	}
}

// Close lets go of what the app's places hold open (the writer of datastore()'s file), and shuts every observer.
func (a *App) Close() {
	a.hub.close()
	for _, kept := range a.places {
		kept.close()
	}
}

// Module is what a file imports: memory, disk, datastore, each making a handle.
type Module struct {
	App *App
	// Open says the file is still running as the site boots: a handle is made then, nothing is read or written
	Open func() bool
}

// Freeze: a Module is never written once made.
func (m *Module) Freeze() {}

func (m *Module) Memory(arguments ...value.Value) value.Value { return m.handle(Memory, arguments) }
func (m *Module) Disk(arguments ...value.Value) value.Value   { return m.handle(Disk, arguments) }
func (m *Module) Datastore(arguments ...value.Value) value.Value {
	return m.handle(Datastore, arguments)
}

// MissingMember is a word kitwork/storage does not have.
func (m *Module) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "kitwork/storage has no "+name+": memory(name), disk(name), datastore(name) make a handle, which reads and writes (memory(\"rates\").get())")
}

// handle is memory(name), disk(name), datastore(name): a handle of the root name, touching nothing yet, so a file may
// make one at its top level (const flags = datastore("flags")).
func (m *Module) handle(where Place, arguments []value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsString() || arguments[0].Text() == "" {
		return value.Fail(value.InvalidArgumentCode, string(where)+"(name) takes a root name, a string not empty: "+string(where)+"(\"rates\"); the rest of the key is segment(…)")
	}
	name := arguments[0].Text()
	if len(name) > keyLimit {
		return value.Fail(value.InvalidArgumentCode, string(where)+"(name): the name is "+strconv.Itoa(len(name))+" bytes, past the ceiling of a key, "+strconv.Itoa(keyLimit))
	}
	return value.Struct(&Handle{app: m.App, open: m.Open, key: Key{Place: where, Name: name}})
}

// Handle is one key of one place, and the expiry its next write takes. It never changes: segment() and expire()
// make another.
type Handle struct {
	app        *App
	open       func() bool
	key        Key
	expire     expiry // nil: the value does not expire
	expireText string
	// reads notes the keys this handle reads, for the run that calls it (Attend); nil for a handle of no such run
	reads Reads
}

// Freeze: a handle is never written once made.
func (h *Handle) Freeze() {}

// Attend is the handle for one call of a run whose context notes its reads (value.Attender): a copy that notes them;
// the handle itself for any other run.
func (h *Handle) Attend(context context.Context) value.Value {
	reads, ok := context.Value(ReadsKey{}).(Reads)
	if !ok || reads == nil {
		return value.Struct(h)
	}
	noted := *h
	noted.reads = reads
	return value.Struct(&noted)
}

// noteRead tells the run's reads the key, when the run notes them.
func (h *Handle) noteRead(key string) {
	if h.reads != nil {
		h.reads.Read(key)
	}
}

// noteWrite tells the app a write changed key, or emptied the branch under it.
func (h *Handle) noteWrite(key string, branch bool) {
	if h.app != nil && h.app.changed != nil {
		h.app.changed(key, branch)
	}
}

// MissingMember is a word a handle does not have.
func (h *Handle) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, h.spelling()+" has no "+name+": a handle has segment, expire, name, key, keys, get, set, exists, ttl, delete, clear, send, observe (engine2/docs/storage.md)")
}

// spelling is the handle as a program writes it, for a message: memory("users").segment(…).
func (h *Handle) spelling() string {
	spelled := string(h.key.Place) + "(" + strconv.Quote(h.key.Name) + ")"
	if len(h.key.Segments) > 0 {
		spelled += ".segment(…)"
	}
	return spelled
}

// acting is the failure of an action at the top level of a file, which only declares (mục 9).
func (h *Handle) acting(word string) (value.Value, bool) {
	if h.open != nil && h.open() {
		return value.Fail(value.UnsupportedCode, h.spelling()+"."+word+"(…) at the top level of a file: a file declares as the site boots; a handle is made there (const flags = "+
			string(h.key.Place)+"("+strconv.Quote(h.key.Name)+")), and "+word+" runs in a handler, a job, a schedule or a function"), true
	}
	if h.app == nil {
		return value.Fail(value.UnsupportedCode, h.spelling()+"."+word+"(…): this file runs for no app, which keeps the values"), true
	}
	return value.Value{}, false
}

// Segment is the handle of the key with segments added: each a non-empty string or a number, which is written as
// text (segment(123) is "123", mục 4).
func (h *Handle) Segment(arguments ...value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, "segment(…) takes a segment at least: users.segment(\"123\"), users.segment(id, \"profile\")")
	}
	segments := make([]string, len(arguments))
	size := len(h.key.Human())
	for index, argument := range arguments {
		switch {
		case argument.IsString() && argument.Text() != "":
			segments[index] = argument.Text()
		case argument.IsNumber() && argument.Number() == math.Trunc(argument.Number()) && !math.IsInf(argument.Number(), 0):
			segments[index] = argument.ToString().Text()
		default:
			kind := argument.Kind()
			switch {
			case argument.IsString():
				kind = "an empty string"
			case argument.IsNumber():
				kind = "the number " + argument.ToString().Text() + ", not a whole one"
			}
			return value.Fail(value.InvalidArgumentCode, "segment(…) takes strings not empty or whole numbers; segment "+strconv.Itoa(index+1)+" is "+kind+": build the key segment by segment, users.segment(id, \"profile\")")
		}
		size += 1 + len(segments[index])
	}
	if size > keyLimit {
		return value.Fail(value.InvalidArgumentCode, "segment(…): the key is "+strconv.Itoa(size)+" bytes, past the ceiling of a key, "+strconv.Itoa(keyLimit))
	}
	next := *h
	next.key = h.key.With(segments...)
	return value.Struct(&next)
}

// Expire is the handle whose next write expires (mục 6): a span ("30s", "5m", "1d"), a number of milliseconds, or a
// time of the calendar ("nextday 03:00").
func (h *Handle) Expire(arguments ...value.Value) value.Value {
	usage := `expire(…) takes a span ("30s", "5m", "1d"), a number of milliseconds, or a time of the calendar ("nextday 03:00")`
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, usage)
	}
	read := readExpiry(arguments[0], h.zone)
	if read == nil {
		return value.Fail(value.InvalidArgumentCode, "expire("+arguments[0].ToString().Text()+") reads as no expiry: "+usage+", a number from 1 to about 292 years")
	}
	next := *h
	next.expire, next.expireText = read, arguments[0].ToString().Text()
	return value.Struct(&next)
}

func (h *Handle) zone() *time.Location {
	if h.app != nil && h.app.Zone != nil {
		return h.app.Zone
	}
	return time.UTC
}

// Name is the root name, Key the key a person reads ("users:123:profile"), Keys the segments, a new array each
// time: none touches the place.
func (h *Handle) Name(...value.Value) value.Value { return value.String(h.key.Name) }
func (h *Handle) Key(...value.Value) value.Value  { return value.String(h.key.Human()) }
func (h *Handle) Keys(...value.Value) value.Value {
	segments := make([]value.Value, len(h.key.Segments))
	for index, segment := range h.key.Segments {
		segments[index] = value.String(segment)
	}
	return value.Array(segments)
}

// place is the handle's place in its app.
func (h *Handle) place() place { return h.app.places[h.key.Place] }

// Get is get() (the value, or null when there is none or it expired) and get(() => …): read, or run the function
// once, keep its result with the handle's expiry, and give it (mục 6).
func (h *Handle) Get(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("get"); ok {
		return failure
	}
	if len(arguments) > 1 || (len(arguments) == 1 && !arguments[0].IsFunction()) {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".get takes nothing or a function: get() reads, get(() => …) reads or computes; the expiry is set before, expire(\"5m\")")
	}
	key := h.key.Encoded()
	h.noteRead(key)
	if len(arguments) == 0 {
		return h.read(key)
	}
	return h.readOrCompute(key, arguments[0])
}

// read is the value kept under key, decoded as a value of its own, or null.
func (h *Handle) read(key string) value.Value {
	data, found, err := h.place().get(key)
	if err != nil {
		return h.failure(err)
	}
	if !found {
		return value.Null()
	}
	decoded, err := Decode(data)
	if err != nil {
		return h.failure(err)
	}
	return decoded
}

// readOrCompute is get(fn) on a miss: one run of fn for every caller that misses the key at once (the app's
// flights). fn runs on the caller's VM, with its budget. A waiter reads what the run kept; when the run failed and
// kept nothing, the next waiter runs fn itself. A call of the same key from inside fn would wait for itself: it fails
// instead. So does a wait that closes a circle of two keys or more (fn of A waits for B while fn of B, in another
// request, waits for A): the caller that would close it fails CONFLICT, and the other goes on.
func (h *Handle) readOrCompute(key string, fn value.Value) value.Value {
	caller := runtime.LambdaCaller(fn.Lambda())
	for {
		data, found, err := h.place().get(key)
		if err != nil {
			return h.failure(err)
		}
		if found {
			if decoded, err := Decode(data); err == nil {
				return decoded
			}
		}
		running, leading := h.app.flights.begin(key, caller)
		if leading {
			return h.lead(key, running, fn)
		}
		if failure, again := h.waitFor(caller, running); !again {
			return failure
		}
	}
}

// lead runs fn as the leader of key's flight, and ends the flight however the run ends: a flight left in the air
// would hold every later get(fn) of the key.
func (h *Handle) lead(key string, running *flight, fn value.Value) value.Value {
	defer h.app.flights.end(key, running)
	// another caller may have kept the value between the read and the flight
	if data, found, err := h.place().get(key); err == nil && found {
		if decoded, err := Decode(data); err == nil {
			return decoded
		}
	}
	result := fn.Call(nil)
	if result.IsInvalid() || result.IsNullish() {
		return result // a failure rises; null is no value to keep, so the next read runs the function again
	}
	if failure := h.write(key, result); failure.IsInvalid() {
		return failure
	}
	return result
}

// waitFor waits for another caller's run of fn: again once it ended (read again, or lead); otherwise the failure is
// why the caller does not wait.
func (h *Handle) waitFor(caller *runtime.VM, running *flight) (value.Value, bool) {
	if caller != nil && (running.owner == caller || caller.RunsInside(running.owner)) {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".get(fn): the function computing the key "+strconv.Quote(h.key.Human())+
			" reads that key again with get(fn), and would wait for itself: read it with get(), or split the key"), false
	}
	if !h.app.flights.wait(caller, running) {
		return value.Fail(value.ConflictCode, h.spelling()+".get(fn): the key "+strconv.Quote(h.key.Human())+
			" is computed by another request whose function waits for a key this one computes: two get(fn) nested in opposite orders; this one stops so the other goes on (it may be tried again), or nest the keys in one order"), false
	}
	defer h.app.flights.waited(caller)
	ctx, stop := context.Background(), context.CancelFunc(func() {})
	if caller != nil && caller.Context != nil {
		// a request's deadline is read off the clock, which its channel does not tell
		if at, ok := caller.Context.Deadline(); ok {
			ctx, stop = context.WithDeadline(caller.Context, at)
		} else {
			ctx, stop = context.WithCancel(caller.Context)
		}
	}
	defer stop()
	select {
	case <-running.done:
		return value.Value{}, true
	case <-ctx.Done():
		return value.Fail(value.CancelledCode, h.spelling()+".get(fn): the request ended while waiting for another to compute the key "+strconv.Quote(h.key.Human())), false
	}
}

// Set writes the value with the handle's expiry, and gives nothing (mục 5).
func (h *Handle) Set(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("set"); ok {
		return failure
	}
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".set takes one value; the expiry is set before: "+string(h.key.Place)+"("+strconv.Quote(h.key.Name)+").expire(\"5m\").set(value)")
	}
	if arguments[0].IsNullish() {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".set("+arguments[0].Kind()+"): null is no value to keep; "+h.spelling()+".delete() removes the key (engine2/docs/storage.md mục 2)")
	}
	key := h.key.Encoded()
	if failure := h.write(key, arguments[0]); failure.IsInvalid() {
		return failure
	}
	h.noteWrite(key, false)
	return value.Undefined()
}

// write encodes item and keeps it under key with the handle's expiry, from now.
func (h *Handle) write(key string, item value.Value) value.Value {
	data, err := Encode(item, valueLimit)
	if err != nil {
		return valueFailure(h.spelling()+".set", err)
	}
	var expireAt time.Time
	if h.expire != nil {
		now := clock()
		if span := h.expire(now); span > 0 {
			expireAt = now.Add(span)
		}
	}
	if err := h.place().set(key, data, expireAt); err != nil {
		return h.failure(err)
	}
	return value.Value{}
}

// Exists tells "no value" from "null kept" (mục 5).
func (h *Handle) Exists(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("exists"); ok {
		return failure
	}
	if failure, ok := h.noArguments("exists", arguments); ok {
		return failure
	}
	h.noteRead(h.key.Encoded())
	_, found, err := h.place().expiry(h.key.Encoded())
	if err != nil {
		return h.failure(err)
	}
	return value.Boolean(found)
}

// Ttl is how long the value has left, in milliseconds; null when it has no expiry or there is none.
func (h *Handle) Ttl(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("ttl"); ok {
		return failure
	}
	if failure, ok := h.noArguments("ttl", arguments); ok {
		return failure
	}
	h.noteRead(h.key.Encoded())
	expireAt, found, err := h.place().expiry(h.key.Encoded())
	if err != nil {
		return h.failure(err)
	}
	if !found || expireAt.IsZero() {
		return value.Null()
	}
	return value.Number(math.Ceil(float64(expireAt.Sub(clock())) / float64(time.Millisecond)))
}

// Delete drops the value; true when there was one (mục 5).
func (h *Handle) Delete(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("delete"); ok {
		return failure
	}
	if failure, ok := h.noArguments("delete", arguments); ok {
		return failure
	}
	removed, err := h.place().remove(h.key.Encoded())
	if err != nil {
		return h.failure(err)
	}
	if removed {
		h.noteWrite(h.key.Encoded(), false)
	}
	return value.Boolean(removed)
}

// Clear drops the value of the key and of every key under it, its branch (engine2/docs/storage.md mục 2):
// datastore("carts").clear() empties every cart. It gives how many values it dropped.
func (h *Handle) Clear(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("clear"); ok {
		return failure
	}
	if failure, ok := h.noArguments("clear", arguments); ok {
		return failure
	}
	cleared, err := h.place().clear(h.key.Encoded())
	if err != nil {
		return h.failure(err)
	}
	if cleared > 0 {
		h.noteWrite(h.key.Encoded(), true)
	}
	return value.Number(float64(cleared))
}

// Send carries { event, id, data } to every observer of this key in this process, now: nothing is kept, so a message
// no one observes is gone, and nothing is given back (engine2/docs/storage.md mục 3).
func (h *Handle) Send(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("send"); ok {
		return failure
	}
	message, failure := Envelope("send", arguments)
	if failure.IsInvalid() {
		return failure
	}
	h.app.hub.send(h.key.Encoded(), message)
	return value.Undefined()
}

// Observe is observe((message) => …): an observer of this key for the request that calls it, which runs the function
// for each message while it answers with its stream (context.sse), and shuts it as it ends (engine2/docs/sse.md mục
// 4). It gives the observer, whose close() ends it early. Outside a request (a job, a schedule, a routine) there is no
// stream to send to: a failure.
func (h *Handle) Observe(arguments ...value.Value) value.Value {
	if failure, ok := h.acting("observe"); ok {
		return failure
	}
	if len(arguments) != 1 || arguments[0].Lambda() == nil {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".observe takes a function, run for each message: observe((message) => stream.send(message))")
	}
	callback := arguments[0].Lambda()
	listener := listenerOf(runtime.LambdaCaller(callback))
	if listener == nil {
		return value.Fail(value.UnsupportedCode, h.spelling()+".observe(…) runs in a route that answers with its stream, router.get((context) => { const stream = context.sse; …observe((message) => stream.send(message)); return stream; }): a job, a schedule, a routine has no stream (engine2/docs/sse.md mục 4)")
	}
	return h.listen(listener, callback, "observe")
}

// Forward is subscribe(source) of a stream, Go's: an observer of this key for listener, whose stream sends each
// message as it is. It is the failure of the observer not begun, or nothing. A stream is a request's, so the handle is
// read in a request of an app.
func (h *Handle) Forward(listener Listener) value.Value {
	if failure := h.listen(listener, nil, "subscribe"); failure.IsInvalid() {
		return failure
	}
	return value.Value{}
}

// listen begins an observer of the key and gives it to listener, with callback (nil: sent as it is).
func (h *Handle) listen(listener Listener, callback *value.Lambda, word string) value.Value {
	observer, failure := h.app.hub.observe(h.key.Encoded(), h.spelling())
	if failure.IsInvalid() {
		return failure
	}
	if !listener.Listen(observer, callback) {
		return value.Fail(value.CancelledCode, h.spelling()+"."+word+"(…): the request has ended")
	}
	return value.Struct(observer)
}

// listenerOf is the request the run of vm answers, nil when it answers none (a job, a schedule, a routine).
func listenerOf(vm *runtime.VM) Listener {
	if vm == nil || vm.Context == nil {
		return nil
	}
	listener, _ := vm.Context.Value(ListenerKey{}).(Listener)
	return listener
}

func (h *Handle) noArguments(word string, arguments []value.Value) (value.Value, bool) {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+"."+word+"() takes no argument"), true
	}
	return value.Value{}, false
}

// Envelope reads the one argument of word, send or stream.send, as { event, id, data } (storage.md mục 3, sse.md mục
// 3): data is required and kept as a value is; event and id, when written, are strings with no line break (an SSE
// field ends at one); any other key is refused, so the data of a program never passes for the envelope.
func Envelope(word string, arguments []value.Value) (*Message, value.Value) {
	refuse := func(why string) (*Message, value.Value) {
		return nil, value.Fail(value.InvalidArgumentCode, word+" takes { event, id, data }; the data goes in data: "+why)
	}
	if len(arguments) != 1 {
		return refuse(strconv.Itoa(len(arguments)) + " arguments here")
	}
	message := arguments[0]
	if !message.IsObject() {
		return refuse(message.Kind() + " here, " + word + "({ data: … })")
	}
	for _, key := range message.Keys() {
		item := message.Get(key)
		switch key {
		case "event", "id":
			if !item.IsString() {
				return refuse(key + " is a string, " + item.Kind() + " here")
			}
			if strings.ContainsAny(item.Text(), "\r\n") {
				return refuse(key + " holds no line break")
			}
		case "data":
		default:
			return refuse("the key " + strconv.Quote(key) + " is not one of the envelope")
		}
	}
	if !message.Has("data") {
		return refuse("data is missing")
	}
	event, id := message.Get("event"), message.Get("id")
	return NewMessage(word, event.Text(), id.Text(), message.Has("event"), message.Has("id"), message.Get("data"))
}

// placeError is a failure of a place: one past its ceiling (full), or one that cannot be reached.
type placeError struct {
	full bool
	text string
}

func (e *placeError) Error() string { return e.text }

func unavailable(text string) error { return &placeError{text: text} }
func full(text string) error        { return &placeError{full: true, text: text} }

// failure is the failure of a place: TOO_LARGE past its ceiling, DENIED for a path that leads outside the app,
// UNAVAILABLE when it cannot be reached, DATABASE for a kept value that cannot be read and anything else it reports.
func (h *Handle) failure(err error) value.Value {
	var known *placeError
	switch {
	case errors.As(err, &known) && known.full:
		return value.Fail(value.TooLargeCode, h.spelling()+": "+known.text)
	case errors.As(err, &known):
		return value.Fail(value.UnavailableCode, h.spelling()+": "+known.text)
	case errors.Is(err, errOutside), errors.Is(err, errOutsideStore):
		return value.Fail(value.DeniedCode, h.spelling()+": "+err.Error())
	case errors.Is(err, ErrCorrupt):
		return value.Fail(value.DatabaseCode, h.spelling()+": a kept value cannot be read ("+err.Error()+")")
	}
	return value.Fail(value.DatabaseCode, h.spelling()+": "+err.Error())
}

// valueFailure is the failure of a value that cannot be kept or sent: INVALID_ARGUMENT, with what and where.
func valueFailure(where string, err error) value.Value {
	var refused *ValueError
	if errors.As(err, &refused) {
		return value.Fail(value.InvalidArgumentCode, where+": "+refused.Error()+"; a value kept is one JSON writes (a string, a number, a boolean, null, an array, a plain object) or bytes (KITWORK_STORAGE.md mục 5)")
	}
	return value.Fail(value.InvalidArgumentCode, where+": "+err.Error()+" (KITWORK_STORAGE.md mục 5)")
}

// flight is one run of get(fn) of a key: done closes when it ends; owner is the VM that runs it.
type flight struct {
	done  chan struct{}
	owner *runtime.VM
}

// flights are the runs of get(fn) of an app in progress, and which VM waits for which, to see a circle before it
// closes.
type flights struct {
	mu      sync.Mutex
	running map[string]*flight
	waiting map[*runtime.VM]*flight
}

// begin is the flight of key, and whether the caller leads it (none was running).
func (f *flights) begin(key string, caller *runtime.VM) (*flight, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if running := f.running[key]; running != nil {
		return running, false
	}
	running := &flight{done: make(chan struct{}), owner: caller}
	f.running[key] = running
	return running, true
}

func (f *flights) end(key string, running *flight) {
	f.mu.Lock()
	if f.running[key] == running {
		delete(f.running, key)
	}
	f.mu.Unlock()
	close(running.done)
}

// wait records that caller waits for running, false when that would close a circle: the owner of running waits,
// through the owners of what it waits for, for a flight the caller runs.
func (f *flights) wait(caller *runtime.VM, running *flight) bool {
	if caller == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := running.owner
	for steps := 0; owner != nil && steps <= len(f.waiting); steps++ {
		if owner == caller || owner.RunsInside(caller) {
			return false
		}
		next := f.waiting[owner]
		if next == nil {
			break
		}
		owner = next.owner
	}
	f.waiting[caller] = running
	return true
}

func (f *flights) waited(caller *runtime.VM) {
	if caller == nil {
		return
	}
	f.mu.Lock()
	delete(f.waiting, caller)
	f.mu.Unlock()
}
