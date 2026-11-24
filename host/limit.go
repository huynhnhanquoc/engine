package host

import (
	"sync"
	"time"
)

// Giới hạn tần suất của route.limit({ rate, per }) (24/11/2026; docs/KITWORK_ROUTER.md "Giới hạn tần suất"): mỗi luật
// một hạn mức cho mỗi client; luật của all(…) và methods(…) đếm theo client, luật đứng trước một cửa đếm theo client và
// đường dẫn đã chuẩn hoá. Cửa sổ cố định: rate request trong mỗi khoảng per tính từ request đầu của cửa sổ; quá thì 429
// kèm Retry-After tới hết cửa sổ.

// maxLimitKeys là số client một luật nhớ: quá thì quên các cửa sổ đã hết; vẫn quá thì quên hết (bộ nhớ của một luật có
// trần, đổi lại một lúc không đếm được ai).
const maxLimitKeys = 100_000

// limiter là các cửa sổ đếm của một luật limit, dùng chung bởi mọi request của site (có khoá).
type limiter struct {
	rate int
	per  time.Duration

	mu      sync.Mutex
	windows map[string]window
}

type window struct {
	start time.Time
	count int
}

func newLimiter(rate int, per time.Duration) *limiter {
	return &limiter{rate: rate, per: per, windows: map[string]window{}}
}

// allow đếm một request của key lúc now: true khi còn trong hạn mức; false và thời gian tới khi cửa sổ hết khi quá.
func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, found := l.windows[key]
	if !found || now.Sub(w.start) >= l.per {
		if !found && len(l.windows) >= maxLimitKeys {
			l.sweep(now)
		}
		w = window{start: now}
	}
	w.count++
	l.windows[key] = w
	if w.count > l.rate {
		return false, w.start.Add(l.per).Sub(now)
	}
	return true, 0
}

// sweep quên các cửa sổ đã hết; còn đầy thì quên hết.
func (l *limiter) sweep(now time.Time) {
	for key, w := range l.windows {
		if now.Sub(w.start) >= l.per {
			delete(l.windows, key)
		}
	}
	if len(l.windows) >= maxLimitKeys {
		clear(l.windows)
	}
}
