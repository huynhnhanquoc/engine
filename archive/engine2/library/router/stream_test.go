package router_test

import (
	"net/http/httptest"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/storage"
)

// A request keeps the observers begun for it, hands over what each holds in the order they began, and as it ends
// shuts them and its stream; one begun after it ended is shut at once.
func TestARequestsObserversEndWithIt(t *testing.T) {
	app := storage.NewApp(t.TempDir(), nil)
	defer app.Close()
	module := &storage.Module{App: app}
	handle := func(name string) *storage.Handle { return module.Memory(value.String(name)).Host().(*storage.Handle) }
	context := router.NewContext(httptest.NewRequest("GET", "/events", nil), nil)
	stream, ok := context.Sse().Host().(*router.Stream)
	if !ok {
		t.Fatalf("context.sse of a GET: %s", context.Sse().Failure())
	}
	for _, name := range []string{"a", "b"} {
		if failure := handle(name).Forward(context); failure.IsInvalid() {
			t.Fatal(failure.Failure())
		}
	}
	handle("b").Send(value.Object().Set("data", value.Number(2)))
	handle("a").Send(value.Object().Set("data", value.Number(1)))
	select {
	case <-context.Waking():
	default:
		t.Error("a message woke nothing")
	}
	pending, overflowed := context.Pending()
	if len(pending) != 2 || overflowed || len(pending[0].Messages) != 1 || pending[0].Messages[0].Value().Get("data").Number() != 1 {
		t.Errorf("pending: %d observers' messages, overflowed %v; want a's first", len(pending), overflowed)
	}
	if app.Observers() != 2 {
		t.Errorf("%d observers, want 2", app.Observers())
	}
	context.End()
	if app.Observers() != 0 || !stream.IsClosed() {
		t.Errorf("after the request ended: %d observers, stream closed %v", app.Observers(), stream.IsClosed())
	}
	if failure := handle("a").Forward(context); failure.Code() != value.CancelledCode || app.Observers() != 0 {
		t.Errorf("an observer begun after the request ended: %s, %d observers", failure.Failure(), app.Observers())
	}
}

// sendingFirst sends a message before it gives the observer to the request: one sent between observe and Listen.
type sendingFirst struct {
	context *router.Context
	send    func()
}

func (s sendingFirst) Listen(observer *storage.Observer, callback *value.Lambda) bool {
	s.send()
	return s.context.Listen(observer, callback)
}

// A message sent to an observer before its request kept it wakes the request as it keeps it.
func TestAMessageBeforeTheRequestKeptItsObserverWakesIt(t *testing.T) {
	app := storage.NewApp(t.TempDir(), nil)
	defer app.Close()
	handle := (&storage.Module{App: app}).Memory(value.String("early")).Host().(*storage.Handle)
	context := router.NewContext(httptest.NewRequest("GET", "/events", nil), nil)
	listener := sendingFirst{context: context, send: func() { handle.Send(value.Object().Set("data", value.Number(1))) }}
	if failure := handle.Forward(listener); failure.IsInvalid() {
		t.Fatal(failure.Failure())
	}
	select {
	case <-context.Waking():
	default:
		t.Error("the message sent before the observer was kept woke nothing")
	}
	defer context.End()
}
