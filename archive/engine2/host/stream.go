package host

import (
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/storage"
)

// A request whose handler returns context.sse stays open as its stream (engine2/docs/sse.md): it holds its VM, where
// the function of each observe(…) it began runs, one message at a time, each run with an energy and a time of its own.
// A function that fails closes the stream, in the log as the handler's failure; nothing goes to the browser, whose
// answer has begun. The stream closes when the browser goes, stream.close(), a function fails, an observer's queue
// fills (a reader too slow), a write is not read in time (a reader that stopped), or the host closes; every observer
// of the request closes with it, and the browser connects again after its reconnect.

// The numbers of streams, by default (Host's fields set others; sse.md mục 4, the first engine's).
const (
	// defaultStreamLimit is how many streams one app holds open at once: one more is 503.
	defaultStreamLimit = 1_000
	// defaultStreamHeartbeat is how often an open stream writes a comment, so a proxy or the browser keeps it.
	defaultStreamHeartbeat = 15 * time.Second
	// defaultStreamWriteTimeout is how long one write of a stream may wait for the browser to read.
	defaultStreamWriteTimeout = 15 * time.Second
)

func (h *Host) streamLimit() int {
	if h.StreamLimit > 0 {
		return h.StreamLimit
	}
	return defaultStreamLimit
}

func (h *Host) streamHeartbeat() time.Duration {
	if h.StreamHeartbeat > 0 {
		return h.StreamHeartbeat
	}
	return defaultStreamHeartbeat
}

func (h *Host) streamWriteTimeout() time.Duration {
	if h.StreamWriteTimeout > 0 {
		return h.StreamWriteTimeout
	}
	return defaultStreamWriteTimeout
}

// appStreams are the open streams of one app, which its sites share: counted against its ceiling, closed with the
// host.
type appStreams struct {
	host    *Host
	mu      sync.Mutex
	open    map[*router.Stream]struct{}
	closing bool
}

// enter counts stream as open, unless the app is at its ceiling or closing.
func (a *appStreams) enter(stream *router.Stream) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing || len(a.open) >= a.host.streamLimit() {
		return false
	}
	if a.open == nil {
		a.open = map[*router.Stream]struct{}{}
	}
	a.open[stream] = struct{}{}
	return true
}

func (a *appStreams) leave(stream *router.Stream) {
	a.mu.Lock()
	delete(a.open, stream)
	a.mu.Unlock()
}

// close closes every open stream of the app and refuses new ones: the host closes. A browser connects again, to the
// next host.
func (a *appStreams) close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closing = true
	streams := make([]*router.Stream, 0, len(a.open))
	for stream := range a.open {
		streams = append(streams, stream)
	}
	a.mu.Unlock()
	for _, stream := range streams {
		stream.Shut()
	}
}

// stream answers the request with the stream its handler returned, until it closes: on the request's goroutine,
// holding its VM.
func (c *call) stream(stream *router.Stream) {
	if c.r.Method == http.MethodHead {
		stream.Head(c.w) // holding nothing
		return
	}
	streams := c.site.streams
	if !streams.enter(stream) {
		c.w.Header().Set("Retry-After", "5")
		answer(c.w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	defer streams.leave(stream)
	// a stream counts against the streams' ceiling, not against the requests the site runs at once (gate.go)
	c.leaveGate()
	host := streams.host
	if !stream.Open(c.w, host.streamWriteTimeout()) {
		return
	}
	heartbeat := time.NewTicker(host.streamHeartbeat())
	defer heartbeat.Stop()
	gone := c.r.Context().Done()
	wake := c.context.Waking()
	for {
		select {
		case <-gone:
			return
		case <-stream.Done():
			return
		case <-wake:
			if !c.deliver(stream) {
				return
			}
		case <-heartbeat.C:
			if !stream.Beat() {
				return
			}
		}
	}
}

// deliver hands each observer of the request what it holds, oldest first: a function runs on the request's VM, a
// subscribe's message goes out as it was sent. It reports whether the stream stays open.
func (c *call) deliver(stream *router.Stream) bool {
	pending, overflowed := c.context.Pending()
	for _, each := range pending {
		for _, message := range each.Messages {
			if stream.IsClosed() {
				return false
			}
			if each.Callback == nil {
				if !stream.Forward(message) {
					return false
				}
				continue
			}
			if result := c.observed(each.Callback, message.Value()); result.IsInvalid() {
				c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+c.where()+", observe): the stream closed: "+result.Failure())
				return false
			}
		}
	}
	if overflowed {
		c.site.log.Led(c.site.lead, visitorText(c.r.Method)+" "+visitorText(c.r.URL.Path)+" ("+c.where()+"): the stream closed: an observer held "+
			strconv.Itoa(storage.QueueLimit())+" messages it had not sent, the browser reads too slowly")
		return false
	}
	return !stream.IsClosed()
}

// observed runs an observer's function for one message on the request's VM, with an energy and a time of its own: a
// run of its own, as the handler's was.
func (c *call) observed(fn *value.Lambda, message value.Value) value.Value {
	program, ok := runtime.ProgramFromRef(fn.Program)
	if !ok {
		return value.Fail(value.TypeCode, "the function of observe(…) has no program")
	}
	machine := c.work.machine
	c.work.deadline.start(c.r.Context(), c.timeout)
	machine.FastReset(program, c.site.globals)
	machine.Builtins = c.site.builtins
	machine.MaxEnergy = c.site.energy
	machine.MaxBytes = c.site.made
	machine.Pool = c.site.pool
	machine.Context = &c.work.deadline
	return machine.ExecuteLambda(fn, []value.Value{message})
}

// where is the file of the request's door, for the log.
func (c *call) where() string { return path.Join(c.folder.Path, "router.work") }
