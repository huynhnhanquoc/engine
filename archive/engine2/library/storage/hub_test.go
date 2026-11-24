package storage

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// message is a message of data, as send(…) makes one.
func message(t *testing.T, data value.Value) *Message {
	t.Helper()
	made, failure := NewMessage("send", "", "", false, false, data)
	if failure.IsInvalid() {
		t.Fatal(failure.Failure())
	}
	return made
}

// A message reaches the observers of its key alone, in the order sent; an observer whose queue is full is shut and
// let go, and says it overflowed; a shut observer gets nothing more; a closed hub shuts every observer, wakes it, and
// begins none.
func TestTheHubCarriesMessagesToTheirKey(t *testing.T) {
	var h hub
	lobby, _ := h.observe("lobby", "memory(\"rooms\")")
	hall, _ := h.observe("hall", "memory(\"rooms\")")
	woken := 0
	lobby.Waking(func() { woken++ })
	h.send("lobby", message(t, value.Number(1)))
	h.send("lobby", message(t, value.Number(2)))
	h.send("nobody", message(t, value.Number(3)))
	got, overflowed := lobby.Take()
	if len(got) != 2 || string(must(got[0].JSON())) != "1" || string(must(got[1].JSON())) != "2" || overflowed || woken != 2 {
		t.Errorf("lobby: %d messages, overflowed %v, woken %d; want 1 then 2, woken twice", len(got), overflowed, woken)
	}
	if got, _ := hall.Take(); len(got) != 0 {
		t.Errorf("hall got %d messages of another key", len(got))
	}
	for range queueLimit {
		h.send("hall", message(t, value.Number(0)))
	}
	if got, overflowed := hall.Take(); len(got) != queueLimit || overflowed {
		t.Errorf("a full queue: %d messages, overflowed %v", len(got), overflowed)
	}
	for range queueLimit + 1 {
		h.send("hall", message(t, value.Number(0)))
	}
	if got, overflowed := hall.Take(); len(got) != 0 || !overflowed || h.count != 1 {
		t.Errorf("one past a full queue: %d messages, overflowed %v, %d observers; want it shut and let go", len(got), overflowed, h.count)
	}
	lobby.Shut()
	h.send("lobby", message(t, value.Number(4)))
	lobby.deliver(message(t, value.Number(4))) // a send that read the hub's observers just before the Shut
	if got, _ := lobby.Take(); len(got) != 0 || h.count != 0 || len(h.keys) != 0 {
		t.Errorf("after Shut: %d messages, %d observers, %d keys", len(got), h.count, len(h.keys))
	}
	last, _ := h.observe("lobby", "memory(\"rooms\")")
	woken = 0
	last.Waking(func() { woken++ })
	h.close()
	h.send("lobby", message(t, value.Number(5)))
	if got, _ := last.Take(); len(got) != 0 || woken != 1 {
		t.Errorf("a closed hub: %d messages, woken %d; want none, woken once", len(got), woken)
	}
	if _, failure := h.observe("lobby", "memory(\"rooms\")"); failure.Code() != value.UnavailableCode {
		t.Errorf("observe on a closed hub: %s, want UNAVAILABLE", failure.Failure())
	}
}

// An app that closes shuts its observers and begins none.
func TestAnAppClosesItsObservers(t *testing.T) {
	app := NewApp(t.TempDir(), nil)
	observer, _ := app.hub.observe("k", "memory(\"k\")")
	app.Close()
	app.hub.send("k", message(t, value.Number(1)))
	if got, _ := observer.Take(); len(got) != 0 || app.Observers() != 0 {
		t.Errorf("after the app closed: %d messages, %d observers", len(got), app.Observers())
	}
	if _, failure := app.hub.observe("k", "memory(\"k\")"); failure.Code() != value.UnavailableCode {
		t.Errorf("observe after the app closed: %s", failure.Failure())
	}
}

// An app holds at most observerLimit observers: one more is TOO_LARGE, one shut frees its place.
func TestAnAppsObserversHaveACeiling(t *testing.T) {
	var h hub
	var first *Observer
	for i := range observerLimit {
		observer, failure := h.observe("k", "memory(\"k\")")
		if failure.IsInvalid() {
			t.Fatalf("observer %d: %s", i+1, failure.Failure())
		}
		if first == nil {
			first = observer
		}
	}
	if _, failure := h.observe("k", "memory(\"k\")"); failure.Code() != value.TooLargeCode || !strings.Contains(failure.Message(), "10000 observers") {
		t.Errorf("one past the ceiling: %s", failure.Failure())
	}
	first.Shut()
	if _, failure := h.observe("k", "memory(\"k\")"); failure.IsInvalid() {
		t.Errorf("after one shut: %s", failure.Failure())
	}
}

// A message is the envelope as sent, data read as a value of its own by each reader; JSON writes a string quoted.
func TestAMessageIsReadAsItWasSent(t *testing.T) {
	made, failure := NewMessage("send", "chat", "", true, true, value.Object().Set("text", value.String("hi")))
	if failure.IsInvalid() {
		t.Fatal(failure.Failure())
	}
	first, second := made.Value(), made.Value()
	first.Get("data").Set("text", value.String("changed"))
	if got := second.Get("data").Get("text").Text(); got != "hi" {
		t.Errorf("a reader's change reached another: %q", got)
	}
	if got := first.Keys(); strings.Join(got, ",") != "event,id,data" || first.Get("id").Text() != "" {
		t.Errorf("the envelope: %v", got)
	}
	plain := message(t, value.String("hi"))
	if keys := plain.Value().Keys(); strings.Join(keys, ",") != "data" || string(must(plain.JSON())) != `"hi"` {
		t.Errorf("a message of data alone: %v, %s", keys, must(plain.JSON()))
	}
	if _, failure := NewMessage("send", "", "", false, false, value.Undefined()); failure.Code() != value.InvalidArgumentCode {
		t.Errorf("undefined data: %s", failure.Failure())
	}
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}
