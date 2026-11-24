// Package routine is "kitwork/routine": work run apart from its caller, in memory, not durable (engine2/docs/routine.md).
// go() starts a function on a VM of the site and gives its task at once; wait() waits for tasks and gives each one's
// result as { ok, data, error }, never a failure that rises; cancel() asks tasks to stop.
//
//	import * as routine from "kitwork/routine";
//	const orders = routine.go(() => db.list((orders) => orders.userId === id));
//	const rates = routine.timeout("3s").retry(1).go(() => http.get(url).send());
//	const [ordersResult, ratesResult] = routine.wait(orders, rates);
//
// Each function runs from a copy of what it holds made as go() starts it (runtime Snapshot): what it writes is its
// copy's, the caller sees none, and the request is no value it holds. A task lives past the request and stops with
// the site.
package routine

import (
	"context"
	"fmt"
	"math"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

const (
	// maxRunning is how many tasks of a site run at once; maxWaiting how many more wait for a place, in the order
	// go() started them. Past both a task fails DENIED at once (routine.md mục 4).
	maxRunning = 1024
	maxWaiting = 4096
	// defaultTimeout is how long one run of a task may take when .timeout() says nothing (routine.md mục 3.1).
	defaultTimeout = 30 * time.Second
)

// Site is what the host gives the module: how a task runs, where it lives, where a failure nobody waits for goes.
type Site struct {
	// Run runs a snapshot on a VM of the site with ctx, and gives what it returned, and whether it ended with a stop
	// nothing catches other than ctx ending (runtime RunSnapshot)
	Run func(ctx context.Context, snapshot value.Value) (value.Value, bool)
	// Lifetime is the site's: a task stops when it ends
	Lifetime context.Context
	// Log says one line in the site's log
	Log func(line string)
	// Running is how many tasks of the site run at once, maxRunning when 0: the host gives as many as the requests
	// the site runs at once, so one site's tasks cannot crowd out the others'
	Running int

	once  sync.Once
	slots chan struct{} // a place for each task running
	// admitted counts the tasks running and waiting for a place
	admitted atomic.Int64
	// finished is the tasks not ended, which the site waits for as it stops
	finished sync.WaitGroup
}

func (s *Site) places() chan struct{} {
	s.once.Do(func() { s.slots = make(chan struct{}, s.running()) })
	return s.slots
}

// run is Run, with a panic of the engine as it runs made the run's failure nothing catches, NATIVE_PANIC, and said in
// the log with its stack (13/11/2026): one task ends, never the host. It ran on a goroutine of its own, where nothing
// recovered, so one task the interpreter panicked on ended the process and every app on it. Another run would meet the
// same fault, so none follows.
func (s *Site) run(ctx context.Context, snapshot value.Value) (result value.Value, stopped bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if s.Log != nil {
				s.Log(fmt.Sprintf("routine.go(…): panic: %v\n%s", recovered, debug.Stack()))
			}
			result, stopped = runtime.NativePanic(nil, "routine.go(…)", recovered), true
		}
	}()
	return s.Run(ctx, snapshot)
}

// running is how many tasks of the site run at once: Running, or maxRunning.
func (s *Site) running() int {
	if s.Running > 0 {
		return s.Running
	}
	return maxRunning
}

// Wait waits for the tasks the site still runs, up to grace: a task stuck in a call of the host that does not hear
// the stop runs on, and the site does not wait longer for it.
func (s *Site) Wait(grace time.Duration) bool {
	done := make(chan struct{})
	go func() { s.finished.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(grace):
		return false
	}
}

// Routine is what a file imports, with the rules the modifiers before go() set.
type Routine struct {
	Site *Site
	// Open says the file is still running as the site boots: nothing is started then
	Open    func() bool
	retry   int
	timeout time.Duration
	// waits is the context of the run that calls wait() (Attend): it waits no longer than the run
	waits context.Context
}

// Freeze: a Routine is never written once made (each modifier copies it).
func (r *Routine) Freeze() {}

// Attend is the routine for one call by a run whose context is ctx (value.Attender): wait() ends when the run does,
// cancelled or past its time (15/11/2026: it waited for its tasks however long they took, past the request's time and
// past a client gone).
func (r *Routine) Attend(ctx context.Context) value.Value {
	attended := *r
	attended.waits = ctx
	return value.Struct(&attended)
}

// Retry is how many runs follow the first when a run fails (return error(…), its .timeout()): .retry(2) is three
// in all. A failure the function caught with safe and answered is no failure.
func (r *Routine) Retry(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsNumber() || arguments[0].Number() < 0 || arguments[0].Number() != math.Trunc(arguments[0].Number()) || arguments[0].Number() > 100 {
		return value.Fail(value.InvalidArgumentCode, "routine.retry(…) takes how many runs follow the first, a whole number from 0: routine.retry(2).go(fn) is three in all")
	}
	next := *r
	next.retry = int(arguments[0].Number())
	return value.Struct(&next)
}

// Timeout is how long each run may take, a duration written as text ("500ms", "5s", "2m"); 30 s when not said.
func (r *Routine) Timeout(arguments ...value.Value) value.Value {
	if len(arguments) == 1 && arguments[0].IsString() {
		if d, err := time.ParseDuration(strings.TrimSpace(arguments[0].Text())); err == nil && d > 0 {
			next := *r
			next.timeout = d
			return value.Struct(&next)
		}
	}
	return value.Fail(value.InvalidArgumentCode, "routine.timeout(…) takes a duration written as text: routine.timeout(\"5s\").go(fn)")
}

// Go starts the function on a VM of the site and gives its task at once (routine.md mục 3.1). A task that finds no
// place, past the running and the waiting, has failed DENIED already; wait() gives it.
func (r *Routine) Go(arguments ...value.Value) value.Value {
	if failure, ok := r.refused("go"); ok {
		return failure
	}
	if len(arguments) != 1 || !arguments[0].IsFunction() {
		return value.Fail(value.InvalidArgumentCode, "routine.go(…) takes one function: routine.go(() => work()); several are several go(), then routine.wait(a, b)")
	}
	snapshot, failure := runtime.Snapshot(arguments[0])
	if failure.IsInvalid() {
		return failure
	}
	// what started it: a failure nobody waits for is logged once it has ended, so a handler that waits for its task
	// further down logs nothing (read now, as the call runs: a request's context is the site's again after)
	var callerDone <-chan struct{}
	if caller := callerOf(arguments); caller != nil && caller.Context != nil {
		callerDone = caller.Context.Done()
	}
	ctx, cancel := context.WithCancel(r.Site.Lifetime)
	task := &Handle{done: make(chan struct{}), cancel: cancel}
	if r.Site.admitted.Add(1) > int64(r.Site.running()+maxWaiting) {
		r.Site.admitted.Add(-1)
		cancel()
		task.finish(value.Fail(value.DeniedCode, "the site runs "+strconv.Itoa(r.Site.running())+" tasks and "+strconv.Itoa(maxWaiting)+
			" wait for a place already: the task did not start, try when some have ended"), false)
		return value.Struct(task)
	}
	r.Site.finished.Add(1)
	go func() {
		defer r.Site.finished.Done()
		defer r.Site.admitted.Add(-1)
		result, ran := value.Value{}, false
		select {
		case r.Site.places() <- struct{}{}: // a place, in the order go() started them
			result, ran = r.attempts(ctx, task, snapshot)
			<-r.Site.slots
		case <-ctx.Done():
			result = r.stopped(task, "before it ran")
		}
		closed := ctx.Err() != nil && r.Site.Lifetime.Err() != nil
		task.finish(result, ran)
		cancel()
		switch {
		case !result.IsInvalid() || task.waited.Load() || r.Site.Log == nil:
			return
		case closed:
			r.Site.Log("routine.go(…) stopped unfinished as the site closed: " + result.Failure())
			return
		}
		if callerDone != nil {
			select {
			case <-callerDone:
			case <-r.Site.Lifetime.Done():
			}
		}
		if !task.waited.Load() {
			// nobody waited for it: the failure is never lost
			r.Site.Log("routine.go(…) failed and nothing waited for it: " + task.result.Failure())
		}
	}()
	return value.Struct(task)
}

// stopped is the failure of a task that ended by a stop, when: CANCELLED by cancel(), else the site's end.
func (r *Routine) stopped(task *Handle, when string) value.Value {
	if task.cancelled.Load() {
		return value.Fail(value.RoutineCancelledCode, "the task was cancelled (cancel) "+when)
	}
	return value.Fail(value.UnavailableCode, "the site stopped "+when)
}

// Wait waits for every task given, or for an array of them, and gives their results in the order given, each
// { ok, data, error } (routine.md mục 3.2): data what the function returned, a copy of its own for each who waits;
// error { code, message } when it failed after its retries, or was cancelled. Nothing it waits for rises.
func (r *Routine) Wait(arguments ...value.Value) value.Value {
	if failure, ok := r.refused("wait"); ok {
		return failure
	}
	if r.retry != 0 || r.timeout != 0 {
		return value.Fail(value.InvalidArgumentCode, "routine.retry(…) and routine.timeout(…) go before go(): a task runs with the rules its routine.go(…) gave it, routine.wait(task)")
	}
	tasks, failure := tasksOf("wait", arguments)
	if failure.IsInvalid() {
		return failure
	}
	results := make([]value.Value, len(tasks))
	ctx, cancel := value.WaitContext(r.waits, time.Time{})
	defer cancel()
	for i, task := range tasks {
		task.waited.Store(true)
		select {
		case <-task.done: // a task ends: its .timeout(), its energy, the site's life bound it
		case <-ctx.Done():
			// the run that waits ended: its failure, which ends it as its cancellation; the tasks go on
			return value.Fail(value.CancelledCode, "routine.wait(…) waited past the end of its run: "+ctx.Err().Error())
		}
		results[i] = outcome(task)
	}
	return value.Array(results)
}

// outcome is a task's result as wait() gives it: { ok: true, data, error: null } or { ok: false, data: null, error:
// { code, message, at } } (value.ErrorObject, as safe's).
func outcome(task *Handle) value.Value {
	result := task.result
	if !result.IsInvalid() {
		result = runtime.Result(result) // each who waits gets a copy of its own; a function is no data
	}
	if result.IsInvalid() {
		return value.Object().Set("ok", value.Boolean(false)).Set("data", value.Null()).Set("error", result.ErrorObject())
	}
	return value.Object().Set("ok", value.Boolean(true)).Set("data", result).Set("error", value.Null())
}

// Cancel asks each task given, or an array of them, to stop; one that has ended keeps its result.
func (r *Routine) Cancel(arguments ...value.Value) value.Value {
	tasks, failure := tasksOf("cancel", arguments)
	if failure.IsInvalid() {
		return failure
	}
	for _, task := range tasks {
		task.stop()
	}
	return value.Undefined()
}

// tasksOf reads the tasks wait() or cancel() is given: tasks, or one array of them.
func tasksOf(word string, arguments []value.Value) ([]*Handle, value.Value) {
	if len(arguments) == 1 && arguments[0].IsArray() {
		arguments = arguments[0].Items()
	} else if len(arguments) == 0 {
		return nil, value.Fail(value.InvalidArgumentCode, "routine."+word+"(…) takes tasks of routine.go(…): routine."+word+"(task), routine."+word+"(a, b), routine."+word+"([a, b])")
	}
	tasks := make([]*Handle, len(arguments))
	for i, argument := range arguments {
		task, ok := argument.Host().(*Handle)
		if !argument.IsStruct() || !ok {
			if argument.IsFunction() {
				return nil, value.Fail(value.InvalidArgumentCode, "routine."+word+"(…) takes tasks, not functions: const task = routine.go(() => …); routine."+word+"(task)")
			}
			return nil, value.Fail(value.InvalidArgumentCode, "routine."+word+"(…) takes tasks of routine.go(…), not "+argument.Kind()+": const task = routine.go(() => …); routine."+word+"(task)")
		}
		tasks[i] = task
	}
	return tasks, value.Value{}
}

// refused is a call made as the site boots, or for no site.
func (r *Routine) refused(word string) (value.Value, bool) {
	if r.Open != nil && r.Open() {
		return value.Fail(value.UnsupportedCode, "routine."+word+"(…) at the top level of a file: a file declares as the site boots; a routine starts in a handler, a job or a schedule"), true
	}
	if r.Site == nil || r.Site.Run == nil {
		return value.Fail(value.UnsupportedCode, "routine."+word+"(…): this file runs for no site, which runs the routines"), true
	}
	return value.Value{}, false
}

// attempts runs a snapshot once and then as often as .retry() says while it fails, each run within its timeout: a
// TIMEOUT past it, CANCELLED when cancel() stopped it. ran says a failure is the run's own, which has its place.
func (r *Routine) attempts(ctx context.Context, task *Handle, snapshot value.Value) (result value.Value, ran bool) {
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	for attempt := 0; attempt <= r.retry; attempt++ {
		run, stop := context.WithTimeout(ctx, timeout)
		var stopped bool
		result, stopped = r.Site.run(run, snapshot)
		timedOut := run.Err() == context.DeadlineExceeded && ctx.Err() == nil
		stop()
		ran = true
		switch {
		case timedOut:
			// past its time the run failed, though an answer came (a call of the host that did not hear the stop)
			result, ran = value.Fail(value.TimeoutCode, "a run of the task took longer than "+timeout.String()+" (routine.timeout)"), false
		case !result.IsInvalid():
			return result, true // done, though a cancel came as it ended
		case ctx.Err() != nil:
			return r.stopped(task, "as it ran"), false
		case stopped:
			return result, true // its energy, its frames: another run would meet them again
		}
	}
	return result, ran
}

// callerOf is the VM that handed the function, the request or job that calls; nil when none is found.
func callerOf(arguments []value.Value) *runtime.VM {
	for _, argument := range arguments {
		if lambda := argument.Lambda(); argument.IsFunction() && lambda != nil {
			if vm := runtime.LambdaCaller(lambda); vm != nil {
				return vm
			}
		}
	}
	return nil
}

// MissingMember is a word routine does not have.
func (r *Routine) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "routine has no word "+name+": go, wait, cancel, retry, timeout")
}

// Handle is a task go() started: what wait() waits for and cancel() stops. Nothing is read from it.
type Handle struct {
	done      chan struct{}
	result    value.Value
	ran       bool // result is the run's own (attempts)
	cancel    context.CancelFunc
	cancelled atomic.Bool
	waited    atomic.Bool
}

// Freeze: a task is shared as it is; it guards itself.
func (h *Handle) Freeze() {}

func (h *Handle) finish(result value.Value, ran bool) {
	h.result, h.ran = result, ran
	close(h.done)
}

func (h *Handle) stop() {
	h.cancelled.Store(true)
	h.cancel()
}

// Cancel is task.cancel(): routine.cancel(task).
func (h *Handle) Cancel(...value.Value) value.Value {
	h.stop()
	return value.Undefined()
}

// MissingMember: a task is read by routine.wait, never by the program.
func (h *Handle) MissingMember(name string) value.Value {
	return value.Fail(value.UnsupportedCode, "a task of routine.go(…) has no "+name+": routine.wait(task) gives its result, task.cancel() stops it")
}
