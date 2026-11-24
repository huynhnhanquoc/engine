package application

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// RateLimit is app.website({ rateLimit: { rate, ip, browser, period } }): the host's ceilings of requests, met before
// any site is looked for (host/ratelimit.go). Each is how many requests a span of Period lets through: Rate all of
// them together, IP those of one address, Browser those of one browser; 0 leaves that one out.
type RateLimit struct {
	Rate    int
	IP      int
	Browser int
	// Period is the span the ceilings count in: period, "1s" when the manifest says none.
	Period time.Duration
}

const rateLimitExample = `rateLimit: { rate: 2000, ip: 120, browser: 240, period: "1s" }`

// readRateLimit reads app.website's rateLimit: an object of the four keys, each ceiling a whole number from 0, at least
// one above 0, period a span written as text. user, a ceiling of each signed-in user, is not served: a manifest that
// writes it does not read, since a ceiling left out would let through what it holds back.
func readRateLimit(option value.Value) (*RateLimit, value.Value) {
	if !option.IsObject() || option.IsArray() {
		return nil, value.Fail(value.InvalidArgumentCode, "app.website({ rateLimit }) takes an object: "+rateLimitExample)
	}
	limit := &RateLimit{Period: time.Second}
	for _, key := range option.Keys() {
		item := option.Get(key)
		switch key {
		case "rate", "ip", "browser":
			n := item.Number()
			if !item.IsNumber() || n != math.Trunc(n) || n < 0 || n > 1_000_000_000 {
				return nil, value.Fail(value.InvalidArgumentCode, "app.website({ rateLimit: { "+key+" } }): a whole number of requests from 0 (0 counts none): "+rateLimitExample)
			}
			switch key {
			case "rate":
				limit.Rate = int(n)
			case "ip":
				limit.IP = int(n)
			default:
				limit.Browser = int(n)
			}
		case "period":
			span, ok := readSpan(item)
			if !ok {
				return nil, value.Fail(value.InvalidArgumentCode, "app.website({ rateLimit: { period } }): a span written as text, \"1s\", \"1m\": "+rateLimitExample)
			}
			limit.Period = span
		case "user":
			return nil, value.Fail(value.UnsupportedCode, "app.website({ rateLimit: { user } }) is not served by engine2 yet: it counts the user of a checked session, which engine2 has no word for yet; the manifest does not read, since a ceiling left out would let through what it holds back")
		default:
			return nil, value.Fail(value.InvalidArgumentCode, "app.website({ rateLimit: { "+key+" } }): no key "+key+"; the keys are rate, ip, browser, period: "+rateLimitExample)
		}
	}
	if limit.Rate == 0 && limit.IP == 0 && limit.Browser == 0 {
		return nil, value.Fail(value.InvalidArgumentCode, "app.website({ rateLimit }) counts nothing: rate, ip or browser above 0: "+rateLimitExample)
	}
	return limit, value.Value{}
}

// readSpan reads a span written as text: Go's ("500ms", "1s", "1m30s") or a whole number of days ("1d").
func readSpan(item value.Value) (time.Duration, bool) {
	if !item.IsString() {
		return 0, false
	}
	text := strings.TrimSpace(item.Text())
	if span, err := time.ParseDuration(text); err == nil {
		return span, span > 0
	}
	days, ok := strings.CutSuffix(text, "d")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(days)
	if err != nil || n <= 0 || n > 3650 {
		return 0, false
	}
	return time.Duration(n) * 24 * time.Hour, true
}
