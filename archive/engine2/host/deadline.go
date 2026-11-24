package host

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/kitwork/engine2/library/storage"
)

// deadline is the context a request's run reads: the request's own, which ends when the client goes,
// ended as well at a time (Host.RequestTimeout), since energy counted instructions and one
// instruction over a large array took a millisecond (energy counts that work since 15/11/2026, core/runtime
// work.go; the time still bounds what a run waits for). context.WithTimeout cost each request three
// allocations and a timer, a tenth of a small request's allocations (05/11/2026), so the deadline is
// kept with the site's VM between requests (worker) and reads the clock itself, at every Err, which the
// VM calls every 64 instructions: a run stops within 64 instructions past its time. It read the clock at
// every sixteenth Err, a thousand instructions, which for a loop of sorts over a large array ended a
// 300 ms request after 12 s; reading it at every Err measured no slower. Done is the request's own:
// nothing waits on it, the VM reads Err.
type deadline struct {
	parent context.Context
	at     time.Time
	// passed is set once the time has passed; atomic, since a VM nested in the run's host calls reads
	// the context it shows, on its own goroutine
	passed atomic.Bool
	// listener is the request the run answers, which observe(…) finds under storage.ListenerKey (stream.go)
	listener storage.Listener
	// reads notes the keys of kitwork/storage the run reads, found under storage.ReadsKey (tab.go)
	reads storage.Reads
}

// start makes the deadline the context of a run of parent's request, ending timeout from now.
func (d *deadline) start(parent context.Context, timeout time.Duration) {
	d.parent = parent
	d.at = time.Now().Add(timeout)
	d.passed.Store(false)
}

// stop lets go of the request once its run has ended and its VM let go of the deadline (ResetForPool).
func (d *deadline) stop() { d.parent, d.listener, d.reads = nil, nil, nil }

func (d *deadline) Deadline() (time.Time, bool) {
	if at, ok := d.parent.Deadline(); ok && at.Before(d.at) {
		return at, true
	}
	return d.at, true
}

func (d *deadline) Done() <-chan struct{} { return d.parent.Done() }

// Clocked says its deadline is read from the clock, never timed (value.Clocked): what waits on the run gets a timer of
// its own for it (value.WaitContext).
func (d *deadline) Clocked() {}

func (d *deadline) Value(key any) any {
	if _, ok := key.(storage.ListenerKey); ok && d.listener != nil {
		return d.listener
	}
	if _, ok := key.(storage.ReadsKey); ok && d.reads != nil {
		return d.reads
	}
	return d.parent.Value(key)
}

func (d *deadline) Err() error {
	if err := d.parent.Err(); err != nil {
		return err
	}
	if d.passed.Load() {
		return context.DeadlineExceeded
	}
	if !time.Now().Before(d.at) {
		d.passed.Store(true)
		return context.DeadlineExceeded
	}
	return nil
}
