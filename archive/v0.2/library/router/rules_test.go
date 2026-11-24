package router_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kitwork/engine2/library/router"
)

// A limit is a bucket of rate requests for each client, filled again at rate each span per: a client over it waits
// what one request takes to come back, another client has its own bucket, and "global" is one bucket for everyone.
func TestALimitFillsAgainAtItsRate(t *testing.T) {
	at := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	limit := &router.Limit{Rate: 2, Per: time.Minute, By: "ip"}
	one := httptest.NewRequest("GET", "/", nil)
	one.RemoteAddr = "10.0.0.1:5000"
	other := httptest.NewRequest("GET", "/", nil)
	other.RemoteAddr = "10.0.0.2:5000"
	for i := 0; i < 2; i++ {
		if within, _ := limit.Take(one, "", at); !within {
			t.Fatalf("request %d of 2 a minute refused", i+1)
		}
	}
	within, wait := limit.Take(one, "", at)
	if within || wait < 29*time.Second || wait > 31*time.Second {
		t.Errorf("a third request of 2 a minute: %v, wait %v; want refused, about 30s", within, wait)
	}
	if within, _ := limit.Take(other, "", at); !within {
		t.Error("another client shares the first's bucket")
	}
	port := httptest.NewRequest("GET", "/", nil)
	port.RemoteAddr = "10.0.0.1:6000"
	if within, _ := limit.Take(port, "", at); within {
		t.Error("a client's new connection, another port, starts a bucket of its own")
	}
	if within, _ := limit.Take(one, "/a", at); !within {
		t.Error("a path of a door's limit shares the branch's count")
	}
	if within, _ := limit.Take(one, "", at.Add(31*time.Second)); !within {
		t.Error("a request after half a minute of 2 a minute is refused")
	}
	if within, _ := limit.Take(one, "", at.Add(32*time.Second)); within {
		t.Error("a request a second after the one that came back is let through")
	}
	// a bucket full again holds its rate, however long it rested (within a minute: the sweep would let it go)
	short := &router.Limit{Rate: 2, Per: 10 * time.Second, By: "ip"}
	short.Take(one, "", at)
	short.Take(one, "", at)
	for i := 0; i < 2; i++ {
		short.Take(one, "", at.Add(50*time.Second))
	}
	if within, _ := short.Take(one, "", at.Add(50*time.Second)); within {
		t.Error("a bucket rested five spans holds more than its rate")
	}
	everyone := &router.Limit{Rate: 1, Per: time.Hour, By: "global"}
	everyone.Take(one, "", at)
	if within, _ := everyone.Take(other, "", at); within {
		t.Error("a global limit counts each client apart")
	}
}

// A limit lets go of the buckets of clients that have not come back for a whole span, once a minute: an address seen
// once is not kept for the site's life.
func TestALimitLetsGoOfClientsGone(t *testing.T) {
	at := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	limit := &router.Limit{Rate: 5, Per: 10 * time.Second, By: "ip"}
	for i := 0; i < 100; i++ {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = "10.0.1." + string(rune('0'+i%10)) + string(rune('0'+i/10)) + ":1"
		limit.Take(request, "", at)
	}
	if kept := limit.Kept(); kept != 100 {
		t.Fatalf("100 clients: %d kept", kept)
	}
	late := httptest.NewRequest("GET", "/", nil)
	late.RemoteAddr = "10.0.2.1:1"
	limit.Take(late, "", at.Add(30*time.Second))
	if kept := limit.Kept(); kept != 101 {
		t.Errorf("before a minute: %d kept, want 101", kept)
	}
	limit.Take(late, "", at.Add(2*time.Minute))
	if kept := limit.Kept(); kept != 1 {
		t.Errorf("after a minute, every bucket full again let go: %d kept, want 1", kept)
	}
	// a bucket not full again is kept: its client is still held back
	hourly := &router.Limit{Rate: 1, Per: time.Hour, By: "ip"}
	hourly.Take(late, "", at)
	if within, _ := hourly.Take(late, "", at.Add(2*time.Minute)); within {
		t.Error("a sweep let go of a client still held back: 1 an hour, two in two minutes")
	}
}
