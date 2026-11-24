package value

import (
	"context"
	"time"
)

// WaitContext is the context a module waits within for a run whose context is run, as an Attender is given it: it
// ends when the run is cancelled, at the run's deadline, or at at when at is set and comes first; with no run, at alone,
// or never for a zero at. Its cancel is called once the wait is over.
//
// A run's context ends at its deadline only as it is asked: the host's request reads its time on each Err, it sets no
// timer (host/deadline.go), and its Done closes only when the request is cancelled. What waits on Done (a call out, a
// database's read or write, routine.wait) waits on this one, whose Done closes at the deadline too (15/11/2026: a call
// out waited its own 60 seconds, a read of the database and routine.wait as long as they took, past the request's 30
// and past the client gone, and a routine's .timeout() cut none of them).
func WaitContext(run context.Context, at time.Time) (context.Context, context.CancelFunc) {
	if run == nil {
		if at.IsZero() {
			return context.Background(), func() {} // a call of Go's with no run and no time: nothing to time
		}
		run = context.Background()
	}
	if deadline, ok := run.Deadline(); ok && (at.IsZero() || !at.Before(deadline)) {
		// the run's deadline comes first: a run that times its own (a routine's task, a job, context.WithTimeout's)
		// ends this one as its own timer fires, one clock for both, so what waits sees the run ended; a clocked run's
		// is timed here
		if _, clocked := run.(Clocked); !clocked {
			return context.WithCancel(run)
		}
		at = deadline
	}
	if at.IsZero() {
		return context.WithCancel(run)
	}
	// a deadline no later than the run's own: context.WithDeadline times it, as it would not one past the run's
	return context.WithDeadline(run, at)
}

// Clocked is a run's context whose deadline is read from the clock each time Err is asked and never timed, its Done
// closing only when it is cancelled: the host's request (host/deadline.go), which sets no timer a request. WaitContext
// times such a deadline; once its timer fired, the clock says the run is past it too.
type Clocked interface {
	Clocked()
}
