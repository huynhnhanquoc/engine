package host

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kitwork/engine2/library/application"
)

// The host's ceilings let go, each minute, of the buckets full again: a client seen once is not kept for the host's
// life, and one that comes back starts full, as it would have.
func TestTheHostsCeilingsLetGoOfFullBuckets(t *testing.T) {
	limit := newHostLimit(&application.RateLimit{IP: 2, Browser: 5, Period: time.Hour})
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	request := func(address string) bool {
		r := httptest.NewRequest("GET", "http://shop.test/", nil)
		r.RemoteAddr = address + ":1"
		ok, _ := limit.take(r, at)
		return ok
	}
	for _, address := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		request(address)
	}
	if kept := limit.kept(); kept != 6 { // three addresses, a browser on each
		t.Fatalf("kept %d buckets", kept)
	}
	// a minute on, none is full again (an hour fills them): all kept, and the new address's two
	at = at.Add(61 * time.Second)
	request("203.0.113.9")
	if kept := limit.kept(); kept != 8 {
		t.Errorf("a minute on, kept %d buckets, want 8: a bucket not full again was let go", kept)
	}
	// two hours on, every one is full again: let go, but the new address's and its browser's
	at = at.Add(2 * time.Hour)
	request("203.0.113.10")
	if kept := limit.kept(); kept != 2 {
		t.Errorf("two hours on, kept %d buckets, want 2", kept)
	}
	if newHostLimit(nil) != nil {
		t.Error("no rateLimit made buckets")
	}
}
