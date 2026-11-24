package queue

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The worker of one kind of an app's runs (docs/KITWORK_QUEUE.md mục 6, 11): one poller claims what is due, as much as
// parallel leaves room for, each claim a lease with a token; the attempt runs on a VM of the app under its timeout;
// the outcome is written by the token's holder only. An attempt past its time is failed (TIMEOUT) the moment its time
// is up, before it has stopped: its token stops holding the run then, so a late success of it changes nothing
// (fencing). A heartbeat moves the leases of the attempts running here forward and stops an attempt whose run was
// cancelled. A lease that ran out with no attempt here behind it (a process that stopped) gives the run back. The
// poller runs no program itself.
//
// The schedules of an app run on a worker of their own of kind schedule (docs/KITWORK_SCHEDULE.md mục 9): its Dispatch
// stores the run of each due time first, then it claims and runs as the queue's does.

// The kinds of kitwork_runs.
const (
	KindQueue    = "queue"
	KindSchedule = "schedule"
)

// The defaults of .shared/queue/config.work and .schedule/config.work.
const (
	DefaultParallel = 4
	DefaultKeep     = 30 * 24 * time.Hour
)

// LeaseTTL is how long a claim holds its run without a heartbeat; PollInterval how often the poller looks at its
// clock (a send wakes it at once); PruneEvery how often the ended runs past keep are deleted; StopGrace how long a
// worker that stops waits for its attempts, which are told nothing and finish within their timeout. Variables, so a
// test shortens them.
var (
	LeaseTTL     = 30 * time.Second
	PollInterval = 250 * time.Millisecond
	PruneEvery   = time.Minute
	StopGrace    = 30 * time.Second
)

// Backoff is how long a run waits after its attempt-th attempt failed: 1s, 2s, 4s… up to 30 minutes, each give or take
// a fifth (jitter), so runs that failed together do not come back together. A variable, so a test shortens it.
var Backoff = func(attempt int) time.Duration {
	attempt = min(max(attempt, 1), 12)
	wait := min(time.Second<<uint(attempt-1), 30*time.Minute)
	return time.Duration(float64(wait) * (0.8 + 0.4*rand.Float64()))
}

// never is a time the poller never reaches: nothing to look for.
var never = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// errCancelled is the cause an attempt is stopped with when its run is cancelled.
var errCancelled = errors.New("lần chạy bị huỷ")

// Handler is one job (or schedule) a worker runs: its name, where it is declared, its function, its rules.
type Handler struct {
	Name string
	// Source is the file from the app's folder (.shared/queue/camOn.work), Place where it is declared in it.
	Source   string
	Place    string
	Function *value.Lambda
	// Retry is the attempts after the first (.retry(n)); Timeout bounds each attempt, 0 for none.
	Retry       int
	Timeout     time.Duration
	TimeoutText string
	ContentHash string
}

// Runner runs fn with argument on a VM of the app until ctx ends: what it returned, a failure when it failed.
type Runner func(ctx context.Context, fn *value.Lambda, argument value.Value) value.Value

// Worker runs the runs of one kind of one app.
type Worker struct {
	kind string
	// place is where the runs are, as the worker's line names it
	place string
	store *Store
	run   Runner
	// contextOf is the context a claimed run's handler gets: a job's, a schedule's
	contextOf func(claim Claim, log *Log) value.Value
	say       func(line string)
	// dispatch, when set, stores the runs that came due, before each claim: the schedules' clock
	dispatch func()

	mu        sync.Mutex
	handlers  map[string]*Handler
	parallel  int
	keep      time.Duration
	next      time.Time // when the poller looks for due runs again: a send, a replay, a retry bring it forward
	reclaimAt time.Time // when the poller gives back lost leases again
	pruneAt   time.Time
	// attempts are the attempts this process runs now, each until its goroutine ends. Two can be of one run: an
	// attempt past its timeout is settled at once but runs on until its VM stops, while the retry of the same run
	// already runs. Each is its own entry, so the one that ends never takes the other's out.
	attempts map[*attempt]struct{}
	wake     chan struct{}
	stop     chan struct{}
	loops    sync.WaitGroup // the poller and the heartbeat
	running  sync.WaitGroup // the attempts
	closed   bool
}

// attempt is one attempt running here: its run, the lease it holds and how to stop it.
type attempt struct {
	id    string
	token string
	stop  context.CancelCauseFunc
}

// NewWorker is the worker of kind's runs in store (at place, as its line says), running each handler with run, its
// context made by contextOf, saying its lines with say.
func NewWorker(kind, place string, store *Store, run Runner, contextOf func(Claim, *Log) value.Value, say func(string)) *Worker {
	return &Worker{kind: kind, place: place, store: store, run: run, contextOf: contextOf, say: say, handlers: map[string]*Handler{},
		parallel: DefaultParallel, keep: DefaultKeep, next: never, reclaimAt: never, wake: make(chan struct{}, 1), stop: make(chan struct{})}
}

// noun is what the worker runs, as a line names one.
func (w *Worker) noun() string {
	if w.kind == KindSchedule {
		return "lịch"
	}
	return "job"
}

// label is how the worker's lines begin.
func (w *Worker) label() string {
	if w.kind == KindSchedule {
		return "[schedule] "
	}
	return "[queue] "
}

// Configure sets what runs, how many at once, and how long an ended run is kept: before Start.
func (w *Worker) Configure(handlers []*Handler, parallel int, keep time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers = make(map[string]*Handler, len(handlers))
	for _, handler := range handlers {
		w.handlers[handler.Name] = handler
	}
	w.parallel, w.keep = parallel, keep
}

// OnDispatch sets the step before each claim: the schedules' clock. Before Start.
func (w *Worker) OnDispatch(dispatch func()) { w.dispatch = dispatch }

// Handler is the handler of name, nil when the app declares none of that name.
func (w *Worker) Handler(name string) *Handler {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.handlers[name]
}

// Start writes the definitions into kitwork_definitions and starts the poller and the heartbeat: once, before Stop
// (the app's).
func (w *Worker) Start() error {
	w.mu.Lock()
	definitions := make([]Definition, 0, len(w.handlers))
	names := make([]string, 0, len(w.handlers))
	for _, handler := range w.handlers {
		definitions = append(definitions, Definition{Name: handler.Name, Source: handler.Source, Retry: handler.Retry,
			Timeout: handler.TimeoutText, TimeoutMS: int(handler.Timeout / time.Millisecond), ContentHash: handler.ContentHash})
		names = append(names, handler.Name)
	}
	w.mu.Unlock()
	if err := w.store.Sync(definitions, time.Now()); err != nil {
		return fmt.Errorf("kitwork_definitions: %w", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// the first look, the first reclaim and the first prune are now: what the file holds is not known yet
	w.next, w.reclaimAt, w.pruneAt = time.Time{}, time.Time{}, time.Time{}
	w.loops.Add(2)
	go w.poll()
	go w.heartbeat()
	sort.Strings(names)
	w.say(fmt.Sprintf("%sworker chạy (kitwork_runs của %s): %d %s: %s", w.label(), w.place, len(names), w.noun(), strings.Join(names, ", ")))
	return nil
}

// Stop halts claiming and waits, up to StopGrace, for the attempts running here: an attempt that ends during the wait
// still writes its outcome. One that runs on is told nothing more; its lease runs out and the run comes back to the
// next worker.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.stop)
	w.mu.Unlock()
	w.loops.Wait()
	finished := make(chan struct{})
	go func() {
		w.running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(StopGrace):
		w.say(w.label() + "các lần chạy còn đang chạy khi app đóng: không chờ thêm; lease của chúng hết rồi lần chạy về lại hàng")
	}
}

// Due tells the worker a pending run is due at at (stored by a send, a replay, a now()): it looks for it then, at
// once when it is due now.
func (w *Worker) Due(at time.Time) {
	w.noteDue(at)
	if !time.Now().Before(at) {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
}

// StopAttempt stops the attempts of a cancelled run, if this process runs one.
func (w *Worker) StopAttempt(id string) {
	for _, each := range w.held() {
		if each.id == id {
			each.stop(errCancelled)
		}
	}
}

func (w *Worker) noteDue(at time.Time) {
	w.mu.Lock()
	if at.Before(w.next) {
		w.next = at
	}
	w.mu.Unlock()
}

// takeDue reports whether a run may be due at now, and if so takes the look: until the claim says when the next one
// is due, only a noteDue brings the look forward.
func (w *Worker) takeDue(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Before(w.next) {
		return false
	}
	w.next = never
	return true
}

func (w *Worker) noteReclaim(at time.Time) {
	w.mu.Lock()
	if at.Before(w.reclaimAt) {
		w.reclaimAt = at
	}
	w.mu.Unlock()
}

func (w *Worker) takeReclaim(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Before(w.reclaimAt) {
		return false
	}
	w.reclaimAt = never
	return true
}

// capacity is how many runs this machine could start now: parallel less the attempts running, those past their time
// that have not stopped yet included (each is a VM).
func (w *Worker) capacity() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.parallel - len(w.attempts)
}

// begin registers an attempt: one the poller starts as the worker stops runs, and Stop waits for it as for the others
// (the poller ends before Stop waits for the attempts).
func (w *Worker) begin(each *attempt) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.attempts == nil {
		w.attempts = map[*attempt]struct{}{}
	}
	w.attempts[each] = struct{}{}
	w.running.Add(1)
}

// end takes the attempt out, and only it: another attempt of the same run stays.
func (w *Worker) end(each *attempt) {
	w.mu.Lock()
	delete(w.attempts, each)
	w.mu.Unlock()
	w.running.Done()
}

// held is a copy of the attempts running here.
func (w *Worker) held() []*attempt {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*attempt, 0, len(w.attempts))
	for each := range w.attempts {
		out = append(out, each)
	}
	return out
}

// poll looks at its clock every PollInterval and at every wake, and reads kitwork_runs only when a run it knows of is
// due, a lease it knows of ran out, or the prune is due.
func (w *Worker) poll() {
	defer w.loops.Done()
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		w.look(time.Now())
		select {
		case <-w.stop:
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// look is one look of the poller. A panic of the engine in it is said with its stack, and the next tick looks again at
// every run and lease, as the look that panicked may have taken them and noted nothing: one fault ends neither the
// host nor the worker (13/11/2026; on the poller's goroutine nothing recovered).
func (w *Worker) look(now time.Time) {
	defer func() {
		if recovered := recover(); recovered != nil {
			w.say(fmt.Sprintf("%spoll: panic: %v\n%s", w.label(), recovered, debug.Stack()))
			w.noteDue(now.Add(PollInterval))
			w.noteReclaim(now.Add(PollInterval))
		}
	}()
	if w.dispatch != nil {
		w.dispatch() // the run of each due time is stored before anything claims it
	}
	if w.takeReclaim(now) {
		w.reclaim(now)
	}
	w.claim(now)
	w.mu.Lock()
	prune := !now.Before(w.pruneAt)
	if prune {
		w.pruneAt = now.Add(PruneEvery)
	}
	keep := w.keep
	w.mu.Unlock()
	if prune {
		if _, err := w.store.Prune(now.Add(-keep)); err != nil {
			w.say(w.label() + "keep: " + err.Error())
		}
	}
}

// survive, deferred on a goroutine of the worker, says a panic of the engine there with its stack: what panicked is
// lost, never the host (13/11/2026).
func (w *Worker) survive(what string) {
	if recovered := recover(); recovered != nil {
		w.say(fmt.Sprintf("%s%s: panic: %v\n%s", w.label(), what, recovered, debug.Stack()))
	}
}

// reclaim gives back the runs whose lease ran out with no attempt here behind them, and notes when the next lease no
// attempt here holds runs out.
func (w *Worker) reclaim(now time.Time) {
	skip := map[string]bool{}
	for _, each := range w.held() {
		skip[each.id] = true
	}
	count, next, err := w.store.Reclaim(now, skip)
	switch {
	case err != nil:
		w.say(w.label() + "reclaim: " + err.Error())
		w.noteReclaim(now.Add(LeaseTTL))
		return
	case !next.IsZero():
		w.noteReclaim(next)
	}
	if count > 0 {
		w.noteDue(now)
	}
}

// heartbeat moves the leases of the attempts running here forward, and stops an attempt whose run was cancelled from
// a process that does not run it.
func (w *Worker) heartbeat() {
	defer w.loops.Done()
	ticker := time.NewTicker(max(LeaseTTL/3, 100*time.Millisecond))
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.extend()
		}
	}
}

// extend moves the leases of the attempts running here forward and stops those whose run was cancelled.
func (w *Worker) extend() {
	defer w.survive("heartbeat")
	for _, each := range w.held() {
		status, holds, err := w.store.Extend(each.id, each.token, time.Now().Add(LeaseTTL))
		if err == nil && holds && status == Cancelling {
			each.stop(errCancelled)
		}
	}
}

// claim claims only what there is room for, when a run may be due, then starts each claim.
func (w *Worker) claim(now time.Time) {
	room := w.capacity()
	if room < 1 || !w.takeDue(now) {
		return
	}
	claims, next, err := w.store.Claim(now, LeaseTTL, room)
	if err != nil {
		w.say(w.label() + "claim: " + err.Error())
		w.noteDue(now.Add(PollInterval))
		return
	}
	if next.IsZero() {
		next = never
	}
	w.noteDue(next)
	for _, claim := range claims {
		ctx, stop := context.WithCancelCause(context.Background())
		each := &attempt{id: claim.ID, token: claim.Token, stop: stop}
		w.begin(each)
		go func() {
			defer w.end(each)
			defer stop(nil)
			defer w.survive(claim.Name + " " + claim.ID) // its lease, which no heartbeat moves now, gives the run back
			w.attempt(ctx, claim)
		}()
	}
}

// attempt runs one attempt and settles it.
func (w *Worker) attempt(ctx context.Context, claim Claim) {
	log := &Log{}
	log.text.WriteString(claim.Log)
	// fail records a failed attempt; a run with attempts left is pending again until retryAt, which the worker notes
	fail := func(code, message string, retryAt, now time.Time) {
		code, message = strings.ToValidUTF8(code, string(utf8.RuneError)), strings.ToValidUTF8(message, string(utf8.RuneError))
		status, err := w.store.FailAttempt(claim.ID, claim.Token, code, message, log.String(), retryAt, now)
		if err == nil && status == Pending {
			w.noteDue(retryAt)
		}
		w.settle(claim.ID, err)
	}
	handler := w.Handler(claim.Name)
	if handler == nil {
		// no file declares the job (renamed or removed since the run was sent): no attempt mends that, so the run ends
		// failed, saying so, rather than coming back for ever
		message := fmt.Sprintf("không có job %s trong app: job đã đổi tên hay đã xoá sau khi gửi", claim.Name)
		if w.kind == KindSchedule {
			message = fmt.Sprintf("không có lịch %s trong app: lịch đã đổi tên hay đã xoá sau khi tới giờ", claim.Name)
		}
		log.attemptFailed(claim.Attempt, value.NotFoundCode, message)
		fail(value.NotFoundCode, message, time.Time{}, time.Now())
		return
	}
	if timeout := time.Duration(claim.TimeoutMS) * time.Millisecond; timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	done := make(chan value.Value, 1)
	go func() {
		// a panic of the engine as the attempt runs is the attempt's failure, said with its stack, and the worker goes
		// on: on this goroutine nothing recovered, and one job the interpreter panicked on ended the host (13/11/2026)
		defer func() {
			if recovered := recover(); recovered != nil {
				w.say(fmt.Sprintf("%s%s %s: panic: %v\n%s", w.label(), claim.Name, claim.ID, recovered, debug.Stack()))
				done <- runtime.NativePanic(nil, w.noun()+" "+claim.Name, recovered)
			}
		}()
		done <- w.run(ctx, handler.Function, w.contextOf(claim, log))
	}()
	var result value.Value
	finished := false
	select {
	case result = <-done:
		finished = true
	case <-ctx.Done():
	}
	if !finished {
		// past its time or cancelled: settled now; it stops at its VM's next checkpoint, and whatever it does after,
		// its lease no longer holds the run
		defer func() { <-done }()
	}
	now := time.Now()
	switch {
	case finished && !result.IsInvalid():
		won, err := w.store.Complete(claim.ID, claim.Token, log.String(), now)
		if err == nil && !won {
			w.say(fmt.Sprintf("%s%s %s: lần thử %d chạy xong sau khi mất lease; kết quả bỏ", w.label(), claim.Name, claim.ID, claim.Attempt))
		}
		w.settle(claim.ID, err)
	case errors.Is(context.Cause(ctx), errCancelled):
		log.Write(fmt.Sprintf("lần thử %d: dừng vì lần chạy bị huỷ", claim.Attempt))
		fail("", "", now, now)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		message := fmt.Sprintf("hết giờ: lần thử %d chạy quá %s", claim.Attempt, time.Duration(claim.TimeoutMS)*time.Millisecond)
		if handler.TimeoutText != "" {
			message += fmt.Sprintf(" (.timeout(%q))", handler.TimeoutText)
		}
		log.attemptFailed(claim.Attempt, value.TimeoutCode, message)
		fail(value.TimeoutCode, message, now.Add(Backoff(claim.Attempt)), now)
	default:
		code, message := result.Code(), result.Headline() // the run's record keeps where it failed: it has no column for at
		log.attemptFailed(claim.Attempt, code, message)
		fail(code, message, now.Add(Backoff(claim.Attempt)), now)
	}
}

// settle reports a settle that could not be written, in the log: it leaves the run running under its lease, which no
// heartbeat moves any more, and the worker gives it back when the lease runs out.
func (w *Worker) settle(id string, err error) {
	if err != nil {
		w.say(fmt.Sprintf("%s%s: không ghi được kết quả: %v", w.label(), id, err))
		w.noteReclaim(time.Now().Add(LeaseTTL))
	}
}
