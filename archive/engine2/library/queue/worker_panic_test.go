package queue

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// lines keeps what a worker says.
type lines struct {
	mu   sync.Mutex
	said []string
}

func (l *lines) say(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.said = append(l.said, line)
}

func (l *lines) find(part string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.said {
		if strings.Contains(line, part) {
			return line
		}
	}
	return ""
}

// settled waits for the run id to end, up to 10 s.
func settled(t *testing.T, store *Store, id string) Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		run := status(t, store, id)
		if run.Status == Completed || run.Status == Failed || time.Now().After(deadline) {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A panic of the engine as an attempt runs is that attempt's failure, NATIVE_PANIC, said in the log with its stack;
// the worker runs the next run (13/11/2026: on the attempt's goroutine nothing recovered, and one job ended the host).
// The runner panics as an interpreter's fault would.
func TestAPanicAsAJobRunsIsThatAttemptsFailure(t *testing.T) {
	store := openStore(t, KindQueue)
	var runs atomic.Int64
	run := func(ctx context.Context, fn *value.Lambda, argument value.Value) value.Value {
		if runs.Add(1) == 1 {
			var missing *Worker
			_ = missing.kind // a nil dereference, as a fault of the engine
		}
		return value.Number(1)
	}
	log := &lines{}
	worker := NewWorker(KindQueue, "test", store, run, func(Claim, *Log) value.Value { return value.Undefined() }, log.say)
	worker.Configure([]*Handler{{Name: "camOn", Function: &value.Lambda{}}}, 2, time.Hour)
	first := enqueue(t, store, "camOn", 1, time.Now())
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	failed := settled(t, store, first.ID)
	if failed.Status != Failed || !strings.Contains(failed.ErrorMessage, "Native panic in job camOn") {
		t.Fatalf("the run the engine panicked on is %s: %s %s", failed.Status, failed.ErrorCode, failed.ErrorMessage)
	}
	if line := log.find("camOn " + first.ID + ": panic: runtime error: invalid memory address"); !strings.Contains(line, "goroutine ") {
		t.Errorf("the log says %q, want the panic with its stack", line)
	}
	second := enqueue(t, store, "camOn", 1, time.Now())
	worker.Due(time.Now())
	if done := settled(t, store, second.ID); done.Status != Completed {
		t.Errorf("the next run is %s: %s", done.Status, done.ErrorMessage)
	}
}

// A panic of the engine in a look of the poller (the schedules' clock stores the due runs in it) is said with its
// stack, and the next look claims what is due: the worker goes on.
func TestAPanicInAPollersLookIsSaidAndTheNextLookGoesOn(t *testing.T) {
	store := openStore(t, KindQueue)
	log := &lines{}
	worker := NewWorker(KindQueue, "test", store, func(context.Context, *value.Lambda, value.Value) value.Value { return value.Number(1) },
		func(Claim, *Log) value.Value { return value.Undefined() }, log.say)
	worker.Configure([]*Handler{{Name: "camOn", Function: &value.Lambda{}}}, 2, time.Hour)
	var looks atomic.Int64
	worker.OnDispatch(func() {
		if looks.Add(1) == 1 {
			panic("the clock's fault")
		}
	})
	queued := enqueue(t, store, "camOn", 1, time.Now())
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if done := settled(t, store, queued.ID); done.Status != Completed {
		t.Errorf("the run after the look that panicked is %s: %s", done.Status, done.ErrorMessage)
	}
	if line := log.find("poll: panic: the clock's fault"); !strings.Contains(line, "goroutine ") {
		t.Errorf("the log says %q, want the panic with its stack", line)
	}
}

// survive, deferred, says the panic with its stack and lets it go no further.
func TestSurviveSaysThePanic(t *testing.T) {
	log := &lines{}
	worker := NewWorker(KindSchedule, "test", nil, nil, nil, log.say)
	func() {
		defer worker.survive("heartbeat")
		panic("the store's fault")
	}()
	if line := log.find("[schedule] heartbeat: panic: the store's fault"); !strings.Contains(line, "goroutine ") {
		t.Errorf("the log says %q", line)
	}
}
