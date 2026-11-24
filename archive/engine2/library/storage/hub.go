package storage

import (
	"strconv"
	"sync"

	"github.com/kitwork/engine2/core/value"
)

// send(…) and observe(…) of one app (engine2/docs/storage.md mục 3, sse.md mục 4): a message sent to a key reaches
// every observer of that key in the same process, now, and is kept nowhere. An observer belongs to the request that
// began it, which runs its function for each message while the request answers with its stream (context.sse), and
// ends with it. A sender never waits: an observer whose queue is full is shut, and its stream closes.

// The ceilings of observers (sse.md mục 4; the numbers are the first engine's).
const (
	// observerLimit is how many observers one app holds at once: one more is TOO_LARGE.
	observerLimit = 10_000
	// queueLimit is how many messages one observer holds that its stream has not sent: one more shuts it.
	queueLimit = 256
)

// Message is one send(…), the envelope { event, id, data } as it was sent, data encoded once by Encode. Every observer
// it reaches shares it and none changes it: each reads data as a value of its own (Value), and JSON is written once for
// every stream that sends it as it is (JSON).
type Message struct {
	Event, ID       string
	HasEvent, HasID bool
	Data            []byte

	once sync.Once
	json []byte
	err  error
}

// NewMessage is the message of word ("send", "stream.send", "stream.data") with data, which must be a value JSON
// writes, at most the size of a kept value; event and id when has says so, strings with no line break, which the
// caller checked.
func NewMessage(word, event, id string, hasEvent, hasID bool, data value.Value) (*Message, value.Value) {
	if data.IsUndefined() {
		return nil, value.Fail(value.InvalidArgumentCode, word+": data is undefined, which JSON does not write; a message of nothing is data: null")
	}
	encoded, err := Encode(data, valueLimit)
	if err != nil {
		return nil, valueFailure(word+" (data)", err)
	}
	return &Message{Event: event, ID: id, HasEvent: hasEvent, HasID: hasID, Data: encoded}, value.Value{}
}

// JSON is data written as JSON, once for every reader of the message.
func (m *Message) JSON() ([]byte, error) {
	m.once.Do(func() {
		decoded, err := Decode(m.Data)
		if err != nil {
			m.err = err
			return
		}
		written := value.Stringify(decoded, "")
		if !written.IsString() {
			m.err = ErrCorrupt
			return
		}
		m.json = []byte(written.Text())
	})
	return m.json, m.err
}

// Value is the message as observe(…) gives it to its function: the envelope as it was sent, data read as a value of
// its own, so no two readers share an object.
func (m *Message) Value() value.Value {
	envelope := value.Object()
	if m.HasEvent {
		envelope = envelope.Set("event", value.String(m.Event))
	}
	if m.HasID {
		envelope = envelope.Set("id", value.String(m.ID))
	}
	data, err := Decode(m.Data)
	if err != nil {
		data = value.Null()
	}
	return envelope.Set("data", data)
}

// Listener is the request that observes, the one that may answer with context.sse: it holds every observer it is
// given until it ends, wakes for their messages, and runs callback for each (nil: its stream sends the message as it
// is, subscribe). Listen is false once the request has ended: the observer is shut.
type Listener interface {
	Listen(observer *Observer, callback *value.Lambda) bool
}

// ListenerKey is what the context.Context of a request's run gives its Listener under: observe(…) finds the request
// of the run that calls it there.
type ListenerKey struct{}

// hub carries the messages of one app from send to its observers, by encoded key.
type hub struct {
	mu     sync.Mutex
	keys   map[string]map[*Observer]struct{}
	count  int
	closed bool
}

// observe begins an observer of key, the handle spelled as the program wrote it for a failure; or the failure of one
// past the app's ceiling, or of an app that is closing.
func (h *hub) observe(key, spelled string) (*Observer, value.Value) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closed:
		return nil, value.Fail(value.UnavailableCode, spelled+".observe(…): the app is closing")
	case h.count >= observerLimit:
		return nil, value.Fail(value.TooLargeCode, spelled+".observe(…): the app holds "+strconv.Itoa(observerLimit)+" observers, its ceiling (engine2/docs/sse.md mục 4)")
	}
	if h.keys == nil {
		h.keys = map[string]map[*Observer]struct{}{}
	}
	observer := &Observer{hub: h, key: key, spelled: spelled}
	observers := h.keys[key]
	if observers == nil {
		observers = map[*Observer]struct{}{}
		h.keys[key] = observers
	}
	observers[observer] = struct{}{}
	h.count++
	return observer, value.Value{}
}

// send queues m for every observer of key.
func (h *hub) send(key string, m *Message) {
	h.mu.Lock()
	observers := make([]*Observer, 0, len(h.keys[key]))
	for observer := range h.keys[key] {
		observers = append(observers, observer)
	}
	h.mu.Unlock()
	for _, observer := range observers {
		observer.deliver(m)
	}
}

// close shuts every observer and refuses any new one: the app closes.
func (h *hub) close() {
	h.mu.Lock()
	h.closed = true
	var observers []*Observer
	for _, set := range h.keys {
		for observer := range set {
			observers = append(observers, observer)
		}
	}
	h.mu.Unlock()
	for _, observer := range observers {
		observer.Shut()
		observer.signal()
	}
}

// forget takes observer out of the hub: its owner shuts it once (Shut, deliver).
func (h *hub) forget(observer *Observer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	observers := h.keys[observer.key]
	delete(observers, observer)
	if len(observers) == 0 {
		delete(h.keys, observer.key)
	}
	h.count--
}

// Observer is what observe(…) gives: the messages sent to its key since it began, in the order each sender sent them,
// until it is shut. observer.close() shuts it early; otherwise it ends with its request.
type Observer struct {
	hub     *hub
	key     string
	spelled string

	mu         sync.Mutex
	wake       func()
	queue      []*Message
	shut       bool
	overflowed bool
}

// RequestBound: an observer belongs to the request that began it; a routine holds none (runtime Snapshot).
func (*Observer) RequestBound() {}

// MissingMember is a word an observer does not have.
func (o *Observer) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "an observer of "+o.spelled+" has no "+name+": close() ends it early; otherwise it ends with its request (engine2/docs/sse.md mục 4)")
}

// Close is observer.close(): nothing more reaches it.
func (o *Observer) Close(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, "observer.close() takes no argument: it ends the observer now")
	}
	o.Shut()
	return value.Undefined()
}

// Waking makes wake what a message queued, or the observer shut by its hub, calls, never blocking: the request that
// holds the observer comes to Take them.
func (o *Observer) Waking(wake func()) {
	o.mu.Lock()
	o.wake = wake
	o.mu.Unlock()
}

func (o *Observer) signal() {
	o.mu.Lock()
	wake := o.wake
	o.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// deliver queues m, or shuts the observer when its queue is full.
func (o *Observer) deliver(m *Message) {
	o.mu.Lock()
	if o.shut {
		o.mu.Unlock()
		return
	}
	full := len(o.queue) >= queueLimit
	if full {
		o.overflowed, o.shut, o.queue = true, true, nil
	} else {
		o.queue = append(o.queue, m)
	}
	wake := o.wake
	o.mu.Unlock()
	if full {
		o.hub.forget(o)
	}
	if wake != nil {
		wake()
	}
}

// Take hands over the messages queued so far, oldest first; overflowed is true once the observer was shut because its
// queue was full.
func (o *Observer) Take() (messages []*Message, overflowed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	messages, o.queue = o.queue, nil
	return messages, o.overflowed
}

// Shut ends the observer: nothing more reaches it, what it queued is dropped. Twice is once.
func (o *Observer) Shut() {
	o.mu.Lock()
	open := !o.shut
	o.shut, o.queue = true, nil
	o.mu.Unlock()
	if open {
		o.hub.forget(o)
	}
}

// QueueLimit is how many messages an observer holds before it is shut, for a line of the log.
func QueueLimit() int { return queueLimit }

// Observers is how many observers the app holds.
func (a *App) Observers() int {
	a.hub.mu.Lock()
	defer a.hub.mu.Unlock()
	return a.hub.count
}
