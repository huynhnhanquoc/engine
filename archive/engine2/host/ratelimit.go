package host

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/kitwork/engine2/library/application"
)

// The host's ceilings of requests, app.website({ rateLimit: { rate, ip, browser, period } }) (the first engine's
// core/ratelimit.go): met in ServeHTTP before any site is looked for, so a flood on one site cannot spend what every
// site of the host shares. Each ceiling is a bucket of tokens filled again at its rate each period, a request taking
// one from each: rate's, one for all; ip's, one for each client's address (an IPv4 address, an IPv6 network /64);
// browser's, one for each browser on such an address, its User-Agent and Accept-Language. A request one of them
// refuses takes from none, so a refused request spends no one's room, and it is answered 429 with Retry-After, the
// seconds until it would pass.
//
// The machine itself and private addresses are never counted: a service of the host, a check of its health. A
// browser is counted on its address, never across addresses (14/11/2026): its User-Agent and Accept-Language are what
// millions of browsers send alike, so one bucket for each across the host let a client from a few addresses spend the
// room of every browser that looks like it, everywhere. On an address many people share (an office, a school, a
// carrier's NAT) each browser has its own room under the address's. What a client says of itself it may change, so a
// browser's ceiling holds an honest browser, not a client that changes its User-Agent, which the ceilings of all and
// of the address still hold. The first engine's browser read a cookie __device_id and a header X-Device-ID before
// them, which any client sets as it likes: not read. An IPv6 address is counted by its /64, the network one subscriber
// is given: counted one by one, a client had 2^64 addresses to move between, and its ceiling held nothing.

// hostLimit is the host's buckets for the ceilings the manifest declares.
type hostLimit struct {
	limit *application.RateLimit
	mu    sync.Mutex
	// buckets are by "all", "ip|<client>", "browser|<client>|<hash>", client an address as clientOf counts it
	buckets map[string]*hostBucket
	// swept is when the buckets filled again were last let go
	swept time.Time
}

type hostBucket struct {
	tokens float64
	at     time.Time
	rate   float64
}

// newHostLimit is the buckets of limit, nil for none.
func newHostLimit(limit *application.RateLimit) *hostLimit {
	if limit == nil {
		return nil
	}
	return &hostLimit{limit: limit, buckets: map[string]*hostBucket{}}
}

// take counts r against every ceiling, in the order all, address, browser: whether it passes and, when it does not,
// how long until it would.
func (l *hostLimit) take(r *http.Request, now time.Time) (bool, time.Duration) {
	address := connectionAddress(r)
	if ip := net.ParseIP(address); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return true, 0
	}
	type ceiling struct {
		key  string
		rate int
	}
	ceilings := make([]ceiling, 0, 3)
	if l.limit.Rate > 0 {
		ceilings = append(ceilings, ceiling{"all", l.limit.Rate})
	}
	client := clientOf(address)
	if l.limit.IP > 0 {
		ceilings = append(ceilings, ceiling{"ip|" + client, l.limit.IP})
	}
	if l.limit.Browser > 0 {
		ceilings = append(ceilings, ceiling{"browser|" + client + "|" + browserOf(r), l.limit.Browser})
	}
	period := float64(l.limit.Period)
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) >= time.Minute {
		l.sweep(now)
	}
	taken := make([]*hostBucket, 0, len(ceilings))
	for _, each := range ceilings {
		rate := float64(each.rate)
		bucket, ok := l.buckets[each.key]
		if !ok {
			bucket = &hostBucket{tokens: rate, at: now, rate: rate}
			l.buckets[each.key] = bucket
		}
		bucket.tokens = math.Min(rate, bucket.tokens+float64(now.Sub(bucket.at))*rate/period)
		bucket.at = now
		if bucket.tokens < 1 {
			return false, time.Duration((1 - bucket.tokens) * period / rate)
		}
		taken = append(taken, bucket)
	}
	for _, bucket := range taken {
		bucket.tokens--
	}
	return true, 0
}

// sweep lets go of every bucket full again: a client that comes back starts full, as it would have.
func (l *hostLimit) sweep(now time.Time) {
	l.swept = now
	period := float64(l.limit.Period)
	for key, bucket := range l.buckets {
		if bucket.tokens+float64(now.Sub(bucket.at))*bucket.rate/period >= bucket.rate {
			delete(l.buckets, key)
		}
	}
}

// kept is how many buckets the host keeps now, for a test.
func (l *hostLimit) kept() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// limited answers 429 for a request past one of the host's ceilings and reports it.
func (h *Host) limited(w http.ResponseWriter, r *http.Request) bool {
	if h.limit == nil {
		return false
	}
	ok, wait := h.limit.take(r, time.Now())
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(int(math.Ceil(wait.Seconds())), 1)))
	answer(w, http.StatusTooManyRequests, "Too Many Requests")
	return true
}

// connectionAddress is the address of r's connection without its port; X-Forwarded-For is not read: the host serves at
// the edge.
func connectionAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientOf is the address the ceilings count as one client's: an IPv4 address whole (one written as IPv6, ::ffff:a.b.c.d,
// too), an IPv6 address by its /64; what does not read as an address, as it is.
func clientOf(address string) string {
	ip := net.ParseIP(address)
	switch {
	case ip == nil:
		return address
	case ip.To4() != nil:
		return ip.To4().String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// browserOf is r's browser as the host counts it on its address: a hash of its User-Agent and Accept-Language.
func browserOf(r *http.Request) string {
	sum := sha256.Sum256([]byte(r.Header.Get("User-Agent") + "|" + r.Header.Get("Accept-Language")))
	return hex.EncodeToString(sum[:16])
}
