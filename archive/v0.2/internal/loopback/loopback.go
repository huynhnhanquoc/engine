// Package loopback opens the SSRF guard of kitwork/http to loopback for engine2's own tests, which serve the other
// side on 127.0.0.1 (httptest). It is internal: no app, no configuration and no program reaches it, only engine2.
package loopback

import "sync/atomic"

var opened atomic.Int32

// Open lets a request reach loopback until the returned function closes it again.
func Open() func() {
	opened.Add(1)
	return func() { opened.Add(-1) }
}

// Opened says loopback is open.
func Opened() bool { return opened.Load() > 0 }
