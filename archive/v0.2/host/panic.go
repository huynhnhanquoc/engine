package host

import (
	"fmt"
	"runtime/debug"
)

// A panic of the engine outside a request (ServeHTTP has its own): as an app boots, at boot or at a reload, and in a
// look of hot reload. It is said in the log with its stack, a fault of the engine to mend, and ends only what it was
// part of: that app's boot, closed as a boot that failed is, or that look. Never the host and every other app with it,
// as it was while nothing recovered on those goroutines (13/11/2026). The goroutines of the modules recover their own:
// a task of routine.go(…), an attempt of a job or a schedule, the worker's poller and heartbeat.

// panicked says recovered, a panic of the engine met as what ran, in the log led by lead with its stack, and is the
// error that says it ended what ran.
func (h *Host) panicked(lead, what string, recovered any) error {
	h.log.Error(lead, fmt.Sprintf("%s: panic: %v\n%s", what, recovered, debug.Stack()))
	return fmt.Errorf("%s: a panic of the engine ended it, not the app's code: %v", what, recovered)
}

// survive, deferred, says a panic of what ran in the log with its stack, and lets it go no further.
func (h *Host) survive(lead, what string) {
	if recovered := recover(); recovered != nil {
		h.panicked(lead, what, recovered)
	}
}
