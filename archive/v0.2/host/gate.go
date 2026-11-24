package host

import (
	"strconv"
	"sync/atomic"
	"time"
)

// gate bounds the requests a site runs at once (12/11/2026, the second slice of the tenants' limits, docs/KITWORK_BOOT.md):
// a request that runs the site's code takes a place as it begins and gives it back as it ends; past the site's places
// it waits a while for one, then is answered 503. One site flooded, or one whose requests are slow, holds its own
// places and no more, so the processors go on serving every other site. The files of assets/, the engine's own files
// and a tab's channel take no place; a stream gives its place back as it opens (streams have a ceiling of their own,
// stream.go).
//
// The common case is two atomic additions, one as the request begins and one as it ends: a place is counted, and the
// count taken back when it was full. A request that waits sleeps on wake, which a request ending fills while some wait;
// it holds as many tokens as places, so none is lost, and a waiter that wakes to a full gate waits again.
type gate struct {
	// running is every request's to change, alone on its line of the cache: the fields read by every request beside
	// it (limit, waiting) would move between the processors with each change
	running atomic.Int64
	_       [56]byte
	limit   int64
	wait    time.Duration
	waiting atomic.Int64
	wake    chan struct{}
	// refused counts the requests answered 503 since the last line of the log, said is when that line was written:
	// at most one line a second, however many are refused
	refused atomic.Int64
	said    atomic.Int64
}

// defaultSiteWait is how long a request waits for a place when Host.SiteWait says nothing.
const defaultSiteWait = time.Second

func newGate(limit int, wait time.Duration) *gate {
	return &gate{limit: int64(limit), wait: wait, wake: make(chan struct{}, limit)}
}

// enter takes a place, waiting up to the gate's wait while the request is not over: whether it got one.
func (g *gate) enter(done <-chan struct{}) bool {
	if g.running.Add(1) <= g.limit {
		return true
	}
	g.running.Add(-1)
	g.waiting.Add(1)
	defer g.waiting.Add(-1)
	timer := time.NewTimer(g.wait)
	defer timer.Stop()
	for {
		if g.running.Add(1) <= g.limit {
			return true
		}
		g.running.Add(-1)
		select {
		case <-g.wake:
		case <-timer.C:
			return false
		case <-done:
			return false
		}
	}
}

// leave gives a place back, and wakes a request that waits for one.
func (g *gate) leave() {
	g.running.Add(-1)
	if g.waiting.Load() > 0 {
		select {
		case g.wake <- struct{}{}:
		default:
		}
	}
}

// refusal is the line the log says for a request refused, "" when one was said less than a second ago: how many
// were refused since, and how many the site runs at once.
func (g *gate) refusal(now time.Time) string {
	count := g.refused.Add(1)
	last := g.said.Load()
	if now.UnixNano()-last < int64(time.Second) || !g.said.CompareAndSwap(last, now.UnixNano()) {
		return ""
	}
	g.refused.Add(-count)
	return "busy: " + strconv.FormatInt(count, 10) + " request(s) answered 503 after waiting " + g.wait.String() + " for one of the " +
		strconv.FormatInt(g.limit, 10) + " the site runs at once"
}
