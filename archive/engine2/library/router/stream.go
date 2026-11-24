package router

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/storage"
)

// context.sse (engine2/docs/sse.md): a GET handler that reads it answers with a stream of events, and returns it.
//
//	router.get((context) => {
//	  const stream = context.sse.reconnect("3s");
//	  memory("events").segment(user.id).observe((message) => stream.send(message));
//	  return stream;
//	});
//	router.get((context) => context.sse.subscribe(memory("rooms").segment("lobby")));
//
// A message is the envelope { event, id, data }, data written as JSON; what is sent before the handler returns is
// kept and goes out as the stream opens. The request holds its VM while the stream is open, and runs there the
// function of each observe(…) it began, one message at a time (the host's stream.go). Every write of the stream
// happens on the request's goroutine; Shut, from anywhere, ends a write that waits on a browser that does not read.

// The ceilings of what a handler sends before it returns (sse.md mục 3): a backlog sent on connecting fits, a loop
// without end does not.
const (
	bufferLimit = 10_000
	bufferBytes = 4 << 20
)

// heartbeat is the comment an open stream writes now and then, so a proxy or the browser keeps it.
var heartbeat = []byte(": heartbeat\n\n")

// Stream is context.sse, the answer of its request.
type Stream struct {
	response *Response
	// listener is the request, which holds the observers subscribe(…) begins
	listener *Context
	done     chan struct{}

	mu        sync.Mutex
	writer    http.ResponseWriter
	control   *http.ResponseController
	timeout   time.Duration
	opened    bool
	closed    bool
	writing   bool // a write is on the connection now
	buffer    [][]byte
	buffered  int // the bytes of buffer
	reconnect time.Duration
}

// RequestBound: a stream belongs to its request; a routine holds none (runtime Snapshot).
func (*Stream) RequestBound() {}

// Sse is context.sse, read without a call (sse.md mục 2): the stream this request answers with, the same one however
// often it is read. Only a door of GET answers with one, never a guard nor router.view, and only with the edits a
// stream has (header, headers, cookie) before it: a status not 200, a type, a page word, another answer are failures.
func (r *Response) Sse() value.Value {
	if r.stream != nil {
		return value.Struct(r.stream)
	}
	switch {
	case r.guarding:
		return value.Fail(value.TypeCode, "context.sse in a guard: a stream is a door's answer, router.get((context) => context.sse…); a guard lets the request on with return context.next() or refuses")
	case r.viewing:
		return value.Fail(value.TypeCode, "context.sse in router.view(…): router.view draws its page, return context.bind({ … }); a stream is router.get's, router.get((context) => context.sse…) in a folder without router.view (engine2/docs/sse.md mục 2)")
	case r.method != http.MethodGet && r.method != http.MethodHead:
		method := strings.ToLower(r.method)
		return value.Fail(value.TypeCode, "context.sse in router."+method+"(…): a stream is router.get's, the method a browser's EventSource sends; router."+method+"(…) sends into it, memory(…).segment(…).send({ event, id, data }) (engine2/docs/sse.md mục 2)")
	}
	if failure := r.answering("sse", false); failure.IsInvalid() {
		return failure
	}
	if r.mediaType != "" {
		return value.Fail(value.TypeCode, "type(…) before context.sse: a stream is text/event-stream; its edits are header and headers")
	}
	if r.status != 0 && r.status != http.StatusOK {
		return value.Fail(value.RangeCode, "status("+strconv.Itoa(r.status)+") before context.sse: a stream answers 200")
	}
	r.stream = &Stream{response: r, listener: r.owner, done: make(chan struct{})}
	r.answer = "sse"
	return value.Struct(r.stream)
}

// MissingMember is a word a stream does not have; the first engine's sse.connect and sse.publish say what replaced
// them.
func (s *Stream) MissingMember(name string) value.Value {
	switch name {
	case "connect":
		return value.Fail(value.TypeCode, `stream.connect(…) is gone: context.sse is the stream; it observes a key with context.sse.subscribe(memory("rooms").segment(1)) (engine2/docs/sse.md mục 4)`)
	case "publish":
		return value.Fail(value.TypeCode, `stream.publish(…) is gone: memory("rooms").segment(1).send({ event, id, data }) reaches every stream that observes the key (engine2/docs/storage.md mục 3)`)
	}
	return value.Fail(value.TypeCode, "a stream has no "+name+": it has send, event, id, data, reconnect, subscribe, close and closed (engine2/docs/sse.md mục 3)")
}

// Send is stream.send({ event, id, data }): the message goes out now, or as the stream opens when the handler has
// not returned yet. Once the stream has closed it does nothing: a closed connection is the network's, not the
// program's.
func (s *Stream) Send(arguments ...value.Value) value.Value {
	message, failure := storage.Envelope("stream.send", arguments)
	if failure.IsInvalid() {
		return failure
	}
	return s.put("stream.send", message)
}

// Event and Id begin a message written link by link, stream.event("chat").id("1").data({ text }); Data sends one with
// no event and no id.
func (s *Stream) Event(arguments ...value.Value) value.Value {
	return (&draft{stream: s}).Event(arguments...)
}
func (s *Stream) Id(arguments ...value.Value) value.Value {
	return (&draft{stream: s}).Id(arguments...)
}
func (s *Stream) Data(arguments ...value.Value) value.Value {
	return (&draft{stream: s}).Data(arguments...)
}

// Reconnect is reconnect("3s"): how long the browser waits before it connects again once the stream is cut, written
// once as retry: when the stream opens (sse.md mục 2). It gives the stream.
func (s *Stream) Reconnect(arguments ...value.Value) value.Value {
	usage := `reconnect(span) takes a span written as a string, from 1ms to 24h: reconnect("3s"), reconnect("500ms")`
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, usage)
	}
	// a value not a string reads as "", no span
	span, err := time.ParseDuration(strings.TrimSpace(arguments[0].Text()))
	if err != nil || span < time.Millisecond || span > 24*time.Hour {
		return value.Fail(value.InvalidArgumentCode, "reconnect("+arguments[0].ToString().Text()+"): "+usage)
	}
	s.mu.Lock()
	s.reconnect = span
	opened := s.opened
	s.mu.Unlock()
	if opened {
		s.write(retryFrame(span))
	}
	return value.Struct(s)
}

func retryFrame(span time.Duration) []byte {
	return []byte("retry: " + strconv.FormatInt(span.Milliseconds(), 10) + "\n\n")
}

// Subscribe is subscribe(source): source observed, each message sent as it is, the short form of observe((message)
// => stream.send(message)). It gives the stream, so subscribes chain (sse.md mục 4).
func (s *Stream) Subscribe(arguments ...value.Value) value.Value {
	usage := `stream.subscribe takes one handle of kitwork/storage: context.sse.subscribe(memory("rooms").segment(1)); two sources are two subscribes, .subscribe(a).subscribe(b)`
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, usage)
	}
	source, ok := arguments[0].Host().(*storage.Handle)
	if !ok {
		return value.Fail(value.InvalidArgumentCode, usage+"; "+arguments[0].Kind()+" here")
	}
	if failure := source.Forward(s.listener); failure.IsInvalid() {
		return failure
	}
	return value.Struct(s)
}

// Close is stream.close(): the stream closes now, and every observer of its request with it (sse.md mục 3).
func (s *Stream) Close(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, "stream.close() takes no argument: the stream closes now, and every observer of its request with it")
	}
	s.Shut()
	return value.Undefined()
}

// Closed is stream.closed: whether the stream has closed.
func (s *Stream) Closed() value.Value { return value.Boolean(s.IsClosed()) }

// put writes one message on the open stream, or keeps it until the stream opens.
func (s *Stream) put(word string, message *storage.Message) value.Value {
	frame, err := frameOf(message)
	if err != nil {
		return value.Fail(value.InvalidArgumentCode, word+": the data cannot be written as JSON ("+err.Error()+")")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return value.Undefined()
	}
	if !s.opened {
		if len(s.buffer) >= bufferLimit || s.buffered+len(frame) > bufferBytes {
			held, size := len(s.buffer), s.buffered
			s.mu.Unlock()
			return value.Fail(value.InvalidArgumentCode, word+": what a handler sends before it returns its stream is at most "+strconv.Itoa(bufferLimit)+" messages and "+
				strconv.Itoa(bufferBytes)+" bytes ("+strconv.Itoa(held)+" messages, "+strconv.Itoa(size)+" bytes held): send a backlog in parts, only what came after Last-Event-ID (engine2/docs/sse.md mục 3)")
		}
		s.buffer = append(s.buffer, frame)
		s.buffered += len(frame)
		s.mu.Unlock()
		return value.Undefined()
	}
	s.mu.Unlock()
	s.write(frame)
	return value.Undefined()
}

// frameOf is a message as the stream writes it: event when it has one, id when written, data always as JSON on one
// line, which the browser reads with JSON.parse(event.data) (sse.md mục 3).
func frameOf(message *storage.Message) ([]byte, error) {
	data, err := message.JSON()
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 0, len(message.Event)+len(message.ID)+len(data)+24)
	if message.HasEvent && message.Event != "" {
		frame = append(frame, "event: "...)
		frame = append(frame, message.Event...)
		frame = append(frame, '\n')
	}
	if message.HasID {
		frame = append(frame, "id: "...)
		frame = append(frame, message.ID...)
		frame = append(frame, '\n')
	}
	frame = append(frame, "data: "...)
	frame = append(frame, data...)
	return append(frame, '\n', '\n'), nil
}

// Head writes the stream's headers, those the handler set then the stream's own, and 200: the answer of HEAD, and the
// beginning of GET's.
func (s *Stream) Head(w http.ResponseWriter) {
	header := w.Header()
	for name, values := range s.response.header {
		header[name] = values
	}
	s.response.writeCookies(header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no") // a proxy that buffers would hold every message
	w.WriteHeader(http.StatusOK)
}

// Open makes the stream the answer written to w: its headers, the retry: of reconnect, what was sent before the
// handler returned. Each write may wait timeout for the browser to read. It reports whether the stream is open: a
// stream closed before is not.
func (s *Stream) Open(w http.ResponseWriter, timeout time.Duration) bool {
	s.Head(w)
	s.mu.Lock()
	s.writer, s.control, s.timeout, s.opened = w, http.NewResponseController(w), timeout, true
	frames := s.buffer
	if s.reconnect > 0 {
		frames = append([][]byte{retryFrame(s.reconnect)}, frames...)
	}
	s.buffer, s.buffered = nil, 0
	s.mu.Unlock()
	return s.write(frames...)
}

// Forward writes a message observed by subscribe(…) as it was sent, and reports whether the stream is open.
func (s *Stream) Forward(message *storage.Message) bool {
	frame, err := frameOf(message)
	if err != nil {
		return !s.IsClosed() // a message no stream can write is dropped
	}
	return s.write(frame)
}

// Beat writes the heartbeat, and reports whether the stream is open.
func (s *Stream) Beat() bool { return s.write(heartbeat) }

// write writes frames on the open stream and flushes them, under a deadline for this write, cleared after it (an idle
// stream has none: under HTTP/2 a deadline is a timer that resets the stream when it fires). It reports whether the
// stream is open: false for a write the connection refuses, or one the browser does not read in time, after which
// net/http cancels the request (a run on its VM stops, its loop ends).
func (s *Stream) write(frames ...[]byte) bool {
	s.mu.Lock()
	if s.closed || !s.opened {
		closed := s.closed
		s.mu.Unlock()
		return !closed
	}
	s.writing = true
	_ = s.control.SetWriteDeadline(time.Now().Add(s.timeout))
	s.mu.Unlock()
	failed := false
	for _, frame := range frames {
		if _, err := s.writer.Write(frame); err != nil {
			failed = true
			break
		}
	}
	if !failed && s.control.Flush() != nil {
		failed = true
	}
	s.mu.Lock()
	s.writing = false
	if !failed && !s.closed {
		_ = s.control.SetWriteDeadline(time.Time{})
	}
	s.mu.Unlock()
	if failed {
		return false
	}
	return !s.IsClosed()
}

// Shut closes the stream, at once: a write waiting on a browser that does not read is given a deadline in the past,
// so it returns now. Twice is once.
func (s *Stream) Shut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.buffer, s.buffered = nil, 0
	if s.writing {
		_ = s.control.SetWriteDeadline(time.Now())
	}
	close(s.done)
}

// Done is closed once the stream is.
func (s *Stream) Done() <-chan struct{} { return s.done }

// IsClosed reports whether the stream has closed.
func (s *Stream) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// draft is stream.event(…).id(…) before its .data(…): the edits of one message, which go with it and never stick to
// the next (sse.md mục 3).
type draft struct {
	stream          *Stream
	event, id       string
	hasEvent, hasID bool
}

// RequestBound: a draft belongs to its stream's request.
func (*draft) RequestBound() {}

func (d *draft) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "stream.event(…).id(…) has no "+name+": the message is sent by .data(value) (engine2/docs/sse.md mục 3)")
}

func (d *draft) Event(arguments ...value.Value) value.Value { return d.field("event", arguments) }
func (d *draft) Id(arguments ...value.Value) value.Value    { return d.field("id", arguments) }

// field is .event(name) or .id(value): a string with no line break, once a message.
func (d *draft) field(word string, arguments []value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsString() || strings.ContainsAny(arguments[0].Text(), "\r\n") {
		return value.Fail(value.InvalidArgumentCode, "stream."+word+`(…) takes a string with no line break: stream.event("chat").id("msg-123").data({ text })`)
	}
	if (word == "event" && d.hasEvent) || (word == "id" && d.hasID) {
		return value.Fail(value.InvalidArgumentCode, "stream."+word+"(…) twice: one message, one "+word)
	}
	next := *d
	if word == "event" {
		next.event, next.hasEvent = arguments[0].Text(), true
	} else {
		next.id, next.hasID = arguments[0].Text(), true
	}
	return value.Struct(&next)
}

// Data is .data(value): the message is sent, with the event and the id written before it.
func (d *draft) Data(arguments ...value.Value) value.Value {
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, `stream.data(value) takes one value and sends it: stream.event("chat").data({ text })`)
	}
	message, failure := storage.NewMessage("stream.data", d.event, d.id, d.hasEvent, d.hasID, arguments[0])
	if failure.IsInvalid() {
		return failure
	}
	return d.stream.put("stream.data", message)
}

// events are what one request observes, and its stream once context.sse is read: they end with the request (End).
type events struct {
	mu sync.Mutex
	// wake is told, never blocking, that an observer has a message or was shut by its hub
	wake      chan struct{}
	observers []listened
	ended     bool
}

// listened is one observer of the request, and the function it runs for each message (nil: subscribe, the stream
// sends the message as it is).
type listened struct {
	observer *storage.Observer
	callback *value.Lambda
}

// Delivery is what one observer of a request holds for its stream: its function (nil: the stream sends each message
// as it is) and its messages, oldest first.
type Delivery struct {
	Callback *value.Lambda
	Messages []*storage.Message
}

// Listen keeps observer for the request, its function callback; false once the request has ended (storage.Listener).
func (c *Context) Listen(observer *storage.Observer, callback *value.Lambda) bool {
	e := &c.events
	e.mu.Lock()
	if e.ended {
		e.mu.Unlock()
		observer.Shut()
		return false
	}
	if e.wake == nil {
		e.wake = make(chan struct{}, 1)
	}
	e.observers = append(e.observers, listened{observer: observer, callback: callback})
	wake := e.wake
	e.mu.Unlock()
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	observer.Waking(signal)
	signal() // a message sent before the observer was kept waits in its queue
	return true
}

// Waking is told that an observer of the request has a message, or was shut.
func (c *Context) Waking() <-chan struct{} {
	e := &c.events
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.wake == nil {
		e.wake = make(chan struct{}, 1)
	}
	return e.wake
}

// Pending hands over what each observer of the request holds, in the order they began; overflowed is true when one
// was shut because its queue was full, which closes the stream.
func (c *Context) Pending() (pending []Delivery, overflowed bool) {
	e := &c.events
	e.mu.Lock()
	observers := append([]listened(nil), e.observers...)
	e.mu.Unlock()
	for _, each := range observers {
		messages, full := each.observer.Take()
		overflowed = overflowed || full
		if len(messages) > 0 {
			pending = append(pending, Delivery{Callback: each.callback, Messages: messages})
		}
	}
	return pending, overflowed
}

// Opened is the stream the request answers with, nil when it read no context.sse.
func (c *Context) Opened() *Stream { return c.response.stream }

// End ends what the request observes, and its stream: the request is over.
func (c *Context) End() {
	e := &c.events
	e.mu.Lock()
	e.ended = true
	observers := e.observers
	e.observers = nil
	e.mu.Unlock()
	for _, each := range observers {
		each.observer.Shut()
	}
	if c.response.stream != nil {
		c.response.stream.Shut()
	}
}
