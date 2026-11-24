package routine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// A site runs at most maxRunning tasks at once; maxWaiting more wait for a place, in the order go() started them;
// past both a task fails DENIED at once, which wait() gives (engine2/docs/routine.md mục 4). A task that ends makes
// room again. The tasks here run on a stand-in of the host that holds each until the test lets them go.
func TestASiteRunsItsCeilingAndQueuesTheRest(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	site := &Site{
		Run: func(ctx context.Context, snapshot value.Value) (value.Value, bool) {
			started.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return value.Number(1), false
		},
		Lifetime: context.Background(),
	}
	r := &Routine{Site: site}
	fn := value.Closure(&value.Lambda{})
	const running, waiting = 1024, 4096 // the site's, written out: not read off the code under test
	tasks := make([]value.Value, running+waiting)
	for i := range tasks {
		if tasks[i] = r.Go(fn); tasks[i].IsInvalid() {
			t.Fatalf("go() %d: %s", i, tasks[i].Failure())
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for started.Load() < running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := started.Load(); n != running {
		t.Errorf("%d tasks run at once, want %d: the rest wait", n, running)
	}
	denied := r.Wait(r.Go(fn)).Items()[0]
	if denied.Get("ok").Boolean() || denied.Get("error").Get("code").Text() != value.DeniedCode {
		t.Errorf("a task past the running and the waiting: %s", value.Stringify(denied, "").Text())
	}
	close(release)
	results := r.Wait(value.Array(tasks)).Items()
	if len(results) != len(tasks) || !results[len(results)-1].Get("ok").Boolean() || results[0].Get("data").Number() != 1 {
		t.Errorf("the tasks once let go: %d results, last %s", len(results), value.Stringify(results[len(results)-1], "").Text())
	}
	if !site.Wait(5 * time.Second) {
		t.Fatal("the tasks did not end")
	}
	if n := site.admitted.Load(); n != 0 {
		t.Errorf("%d tasks still counted", n)
	}
	if again := r.Wait(r.Go(fn)).Items()[0]; !again.Get("ok").Boolean() {
		t.Errorf("a task once the others ended: %s", value.Stringify(again, "").Text())
	}
}

// A site the host gives a number of tasks at once (Running, the requests it runs at once, 12/11/2026) runs that many,
// the rest waiting, and is denied past that many and maxWaiting, its message saying the number.
func TestASiteRunsAsManyTasksAsTheHostGivesIt(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	site := &Site{
		Run: func(ctx context.Context, snapshot value.Value) (value.Value, bool) {
			started.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return value.Number(1), false
		},
		Lifetime: context.Background(),
		Running:  3,
	}
	r := &Routine{Site: site}
	fn := value.Closure(&value.Lambda{})
	const running, waiting = 3, 4096
	tasks := make([]value.Value, running+waiting)
	for i := range tasks {
		if tasks[i] = r.Go(fn); tasks[i].IsInvalid() {
			t.Fatalf("go() %d: %s", i, tasks[i].Failure())
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for started.Load() < running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := started.Load(); n != running {
		t.Errorf("%d tasks run at once, want %d", n, running)
	}
	denied := r.Wait(r.Go(fn)).Items()[0]
	if denied.Get("error").Get("code").Text() != value.DeniedCode || !strings.Contains(denied.Get("error").Get("message").Text(), "runs 3 tasks") {
		t.Errorf("a task past the running and the waiting: %s", value.Stringify(denied, "").Text())
	}
	close(release)
	r.Wait(value.Array(tasks))
	if !site.Wait(5 * time.Second) {
		t.Fatal("the tasks did not end")
	}
}

// A run takes 30 s at most when .timeout() says nothing (engine2/docs/routine.md mục 3.1), and what .timeout() says
// otherwise.
func TestARunIsBoundByItsTimeout(t *testing.T) {
	deadlines := make(chan time.Duration, 2)
	site := &Site{
		Run: func(ctx context.Context, snapshot value.Value) (value.Value, bool) {
			at, _ := ctx.Deadline()
			deadlines <- time.Until(at)
			return value.Number(1), false
		},
		Lifetime: context.Background(),
	}
	fn := value.Closure(&value.Lambda{})
	plain := &Routine{Site: site}
	plain.Wait(plain.Go(fn))
	if left := <-deadlines; left < 29*time.Second || left > 30*time.Second {
		t.Errorf("a run with no .timeout() has %v, want 30s", left)
	}
	timed := plain.Timeout(value.String("2s")).Host().(*Routine)
	timed.Wait(timed.Go(fn))
	if left := <-deadlines; left < time.Second || left > 2*time.Second {
		t.Errorf("a run of .timeout(\"2s\") has %v", left)
	}
}

// A panic of the engine as a task runs is that task's failure, NATIVE_PANIC, said in the log with its stack, and run
// once: never the host's end, as it was when nothing recovered on the task's goroutine (13/11/2026). The place it held is
// free again: the next task runs. The stand-in of the host panics as an interpreter's fault would.
func TestAPanicAsATaskRunsIsThatTasksFailure(t *testing.T) {
	var runs atomic.Int64
	var logged atomic.Value
	site := &Site{
		Run: func(ctx context.Context, snapshot value.Value) (value.Value, bool) {
			if runs.Add(1) == 1 {
				var missing *Handle
				missing.cancelled.Load() // a nil dereference, as a fault of the engine
			}
			return value.Number(2), false
		},
		Lifetime: context.Background(),
		Log:      func(line string) { logged.Store(line) },
		Running:  1,
	}
	r := &Routine{Site: site}
	retried := r.Retry(value.Number(3)).Host().(*Routine)
	fn := value.Closure(&value.Lambda{})
	failed := r.Wait(retried.Go(fn)).Items()[0]
	if failed.Get("ok").Boolean() || !strings.Contains(failed.Get("error").Get("message").Text(), "Native panic in routine.go(…)") {
		t.Fatalf("a task the engine panicked on: %s", value.Stringify(failed, "").Text())
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the task ran %d times, want once: another run meets the same fault", n)
	}
	if line, _ := logged.Load().(string); !strings.Contains(line, "routine.go(…): panic: runtime error: invalid memory address") || !strings.Contains(line, "goroutine ") {
		t.Errorf("the log says %q, want the panic with its stack", line)
	}
	next := r.Wait(r.Go(fn)).Items()[0]
	if !next.Get("ok").Boolean() || next.Get("data").Number() != 2 {
		t.Errorf("the next task, on the one place: %s", value.Stringify(next, "").Text())
	}
}
