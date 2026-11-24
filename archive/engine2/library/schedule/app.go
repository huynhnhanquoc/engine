package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/queue"
)

// The app's clock (docs/KITWORK_SCHEDULE.md mục 5, 9, 13): the schedules of an app run on a worker of kind schedule,
// the queue's machinery (library/queue): the same kitwork_runs, lease, fencing, retry, timeout and backoff. What the
// clock adds is the step before the claim: at each due time it stores one run of the schedule (queue.Store Dispatch),
// keyed by the due time, so a due time is stored once whatever restarts, and moves the schedule's last due time
// handled (kitwork_schedules). It reads the rules only when a due time may have come (dispatchAt): an idle app's tick
// reads its clock and nothing else.
//
// The rules of a due time mục 12 leaves open, as the first engine ran them (mục 13):
//   - every() counts from midnight of the schedule's zone and starts again at each midnight, as hourly(n) (12.1).
//   - due times missed while the app was down run once, the last of them, as soon as the app is back, its
//     context.job.at the time it was due (12.3). A schedule declared for the first time, or with another rule, counts
//     from when it was synced: nothing before it is missed.
//   - a due time that comes while a run of the schedule has not ended (pending, running) is passed over: one run of a
//     schedule at a time (12.4). A once() and now() are never passed over.
//   - a schedule without .timezone(…) counts in the app's zone, module.timezone(…) of the module.work at the app's
//     root; in UTC when the app declares none, said for each.

// Now is the clock the schedules read the due times from: a test's way to reach a due time without waiting for it.
// A run's lease, its retry and its timeout stay on the system's.
var Now = time.Now

// never is a time the clock never reaches.
var never = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// What started a run of a schedule: its rule (a due time) or now().
const (
	triggerSchedule = "schedule"
	triggerNow      = "now"
)

// App is the schedules of one app: what its .schedule/ declares, the file of their runs, the clock that runs them
// once the host serves.
type App struct {
	say   func(line string)
	root  string
	open  queue.Opener
	file  *queue.File
	store *queue.Store
	// Locate gives the file of a database of the app's .shared/schema/, for schedule.database(db) (library/database
	// FileOf); place is where the runs are, as the clock's line names it
	Locate func(v value.Value) (path, name string, ok bool)
	place  string
	// Zone is the app's module.timezone, ZoneDeclared whether the app declares one: a schedule without .timezone(…)
	// counts in it, in UTC when it declares none (set as the app boots, before .schedule/ is read)
	Zone         *time.Location
	ZoneDeclared bool

	// life orders Start and Close: a clock started is a clock Close stops
	life sync.Mutex
	mu   sync.Mutex
	// declared are the schedules the file the loader runs now evaluates, until Loaded binds them
	declared []*declaration
	names    map[string]string // the schedules' names by the place of their rule
	entries  map[string]*entry // the schedules by name
	keep     time.Duration
	calls    map[string]string // where each word of .schedule/config.work was called
	worker   *queue.Worker
	// dispatchAt is when, on the clock, the next due time may have come: the rules are read only then
	dispatchAt time.Time
	// stopped says the clock stopped for good (Stop), the file kept; closed that the file is let go too (Close)
	stopped bool
	closed  bool
}

// entry is one schedule the clock runs.
type entry struct {
	name    string
	source  string
	place   string
	rule    *Rule
	handler *queue.Handler
	// location is the zone the rule counts in, zoneFrom which tier gave it; state the rule as kitwork_schedules keeps it
	// (with its zone)
	location *time.Location
	zoneFrom string
	state    string
	// declaredAt is when the rule was first synced, onceAt the due time a once() chose, lastAt the last due time
	// handled: read from kitwork_schedules at start, moved by the dispatch
	declaredAt time.Time
	onceAt     time.Time
	lastAt     time.Time
}

// due is the due time to store at now: the last the rule had at or before now, when it is after the last handled (an
// earlier one missed is not stored: the last stands for them).
func (e *entry) due(now time.Time) (time.Time, bool) {
	if e.rule.Once {
		if e.onceAt.IsZero() || e.onceAt.After(now) || !e.lastAt.Before(e.onceAt) {
			return time.Time{}, false
		}
		return e.onceAt, true
	}
	latest, ok := e.rule.Latest(now)
	if !ok || !latest.After(e.lastAt) {
		return time.Time{}, false
	}
	return latest, true
}

// next is the next due time after now, false when there is none (a once() that ran).
func (e *entry) next(now time.Time) (time.Time, bool) {
	if e.rule.Once {
		if e.onceAt.IsZero() {
			// not synced yet: the due time the first sync would choose
			return e.rule.Next(now)
		}
		if !e.lastAt.Before(e.onceAt) {
			return time.Time{}, false
		}
		return e.onceAt, true
	}
	after := now
	if e.lastAt.After(after) {
		after = e.lastAt
	}
	return e.rule.Next(after)
}

// NewApp is the schedules of the app at root, their runs in the app's database opened by open, the lines said by say.
func NewApp(root string, open queue.Opener, say func(string)) *App {
	file := queue.NewFile(root, open, "kitwork/schedule")
	return &App{say: say, root: root, open: open, file: file, store: queue.NewStore(file, queue.KindSchedule), place: ".database/application/kitwork.db", Zone: time.UTC,
		names: map[string]string{}, entries: map[string]*entry{}, keep: queue.DefaultKeep, calls: map[string]string{}, dispatchAt: never}
}

// declare records a schedule the loader's run of a file of .schedule/ evaluates.
func (a *App) declare(d *declaration) {
	a.mu.Lock()
	a.declared = append(a.declared, d)
	a.mu.Unlock()
}

// Loaded binds the schedules the loader's run of file (".schedule/baoCao.work", its source) declared to the names they
// are exported under, and keeps each as a schedule of the app: one not exported, not ended with .run or .once, a name
// two declare, a schedule.on(…) already past when no sync before kept it, are its errors.
func (a *App) Loaded(file, source string) error {
	a.mu.Lock()
	declared := a.declared
	a.declared = nil
	a.mu.Unlock()
	mine := false
	for _, d := range declared {
		mine = mine || strings.HasPrefix(d.place, file+":")
	}
	if !mine {
		return nil // a file of the app that declares no schedule: what it exports is no schedule's
	}
	places, err := queue.ExportedChains(file, source, func(word string) bool { return RuleWords[word] })
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(source))
	hash := hex.EncodeToString(sum[:8])
	var past []*entry
	a.mu.Lock()
	for _, d := range declared {
		if !strings.HasPrefix(d.place, file+":") {
			continue
		}
		name, ok := places[d.place]
		if !ok {
			a.mu.Unlock()
			return fmt.Errorf(`%s: lịch không export: export const baoCaoSang = schedule.daily("09:00").run(fn) ở cấp file, tên lịch là tên export; lịch không tên thì lịch sử lạc mỗi lần sửa`, d.place)
		}
		if d.chain.Ended() == "" {
			a.mu.Unlock()
			return fmt.Errorf("%s: lịch %s chưa kết thúc: nối .run(fn), hay .once(fn) cho đúng một lần", d.place, name)
		}
		if first, taken := a.entries[name]; taken {
			a.mu.Unlock()
			return fmt.Errorf("%s: lịch %s đã khai ở %s; tên lịch duy nhất trong app", d.place, name, first.place)
		}
		rule := d.chain.Rule()
		location, from := rule.Location, ZoneOwn
		switch {
		case location != nil:
		case rule.Kind == On:
			location, from = rule.Instant.Location(), ZoneInstant // an instant carries its own offset: its due time reads as written
		default:
			location, from = a.Zone, ZoneApp
			if !a.ZoneDeclared {
				a.say(fmt.Sprintf(`[schedule] %s: lịch %s không viết .timezone(…), app không khai module.timezone(…), host không khai app.timezone(…): đếm theo UTC; múi giờ của app viết module.timezone("Asia/Ho_Chi_Minh") trong module.work ở gốc app, của mọi app trên host thì app.timezone(…) trong application.work, của riêng lịch thì .timezone(…)`, d.place, name))
			}
		}
		rule.Location = location
		each := &entry{name: name, source: file, place: d.place, rule: rule, location: location, zoneFrom: from,
			state: rule.Mode() + " " + rule.Text() + " " + location.String(),
			handler: &queue.Handler{Name: name, Source: file, Place: d.place, Function: d.function, Retry: rule.Retry,
				Timeout: rule.Timeout, TimeoutText: rule.TimeoutText, ContentHash: hash}}
		a.names[d.place] = name
		a.entries[name] = each
		if rule.Kind == On && !rule.Instant.After(Now()) {
			past = append(past, each)
		}
	}
	a.mu.Unlock()
	return a.refusePast(past)
}

// refusePast is the error of a schedule.on(…) whose instant has passed (mục 3: "thời điểm đã qua lúc khai là lỗi
// boot"), unless kitwork_schedules of the database the runs are in holds it with the same rule: declared before its
// instant, it has run, or runs now as a due time missed.
func (a *App) refusePast(past []*entry) error {
	if len(past) == 0 {
		return nil
	}
	states, err := a.store.ScheduleStates()
	if err != nil {
		return fmt.Errorf("kitwork_schedules: %w", err)
	}
	for _, each := range past {
		if state, kept := states[each.name]; kept && state.Rule == each.state {
			continue
		}
		return fmt.Errorf("%s: schedule.on(%q) đã qua lúc khai: thời điểm của once() phải ở tương lai", each.place, each.rule.InstantText)
	}
	return nil
}

// configure is a word of .schedule/config.work called at place: the error when it was called before.
func (a *App) configure(word, place string, set func()) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if first, called := a.calls[word]; called {
		if first == place {
			return nil // the same call, run again by a file that imports the one that writes it
		}
		return fmt.Errorf("schedule.%s gọi hai lần, ở %s và %s: mỗi chữ một lần trong app", word, first, place)
	}
	a.calls[word] = place
	set()
	return nil
}

// nameAt is the name of the schedule declared at place, "" when no file of .schedule/ exports one there.
func (a *App) nameAt(place string) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.names[place]
}

// entryOf is the schedule of name, which the app declares.
func (a *App) entryOf(name string) *entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.entries[name]
}

// Start runs the app's schedules with run, the host's: it syncs kitwork_schedules with what .schedule/ declares and
// starts the clock. An app with no schedule starts nothing.
func (a *App) Start(run queue.Runner) error {
	a.life.Lock()
	defer a.life.Unlock()
	a.mu.Lock()
	if a.closed || a.stopped || a.worker != nil || len(a.entries) == 0 {
		a.mu.Unlock()
		return nil
	}
	handlers := make([]*queue.Handler, 0, len(a.entries))
	for _, each := range a.entries {
		handlers = append(handlers, each.handler)
	}
	a.mu.Unlock()
	if err := a.sync(); err != nil {
		return fmt.Errorf("kitwork_schedules: %w", err)
	}
	worker := queue.NewWorker(queue.KindSchedule, a.place, a.store, run, scheduleContext, a.say)
	worker.Configure(handlers, queue.DefaultParallel, a.keep)
	worker.OnDispatch(a.dispatch)
	if err := worker.Start(); err != nil {
		return err
	}
	a.mu.Lock()
	a.worker = worker
	a.mu.Unlock()
	return nil
}

// sync makes kitwork_schedules what .schedule/ declares, and the entries what it kept: a schedule with the rule it was
// synced with keeps its last due time handled and the due time its once() chose; one declared for the first time, or
// with another rule, counts from now. The state of a schedule no file declares any more goes.
func (a *App) sync() error {
	states, err := a.store.ScheduleStates()
	if err != nil {
		return err
	}
	now := Now()
	a.mu.Lock()
	var put []queue.ScheduleState
	for _, each := range a.entries {
		state, kept := states[each.name]
		if !kept || state.Rule != each.state {
			state = queue.ScheduleState{Name: each.name, Rule: each.state, DeclaredAt: now, LastAt: now}
			if each.rule.Once {
				state.OnceAt, _ = each.rule.Next(now)
			}
			put = append(put, state)
		}
		each.declaredAt, each.onceAt, each.lastAt = state.DeclaredAt, state.OnceAt, state.LastAt
	}
	var remove []string
	for name := range states {
		if a.entries[name] == nil {
			remove = append(remove, name)
		}
	}
	// the first look is now: a due time missed while the app was down is stored at once
	a.dispatchAt = time.Time{}
	a.mu.Unlock()
	return a.store.SyncSchedules(put, remove, now)
}

// dispatch stores the run of every due time that has come on the clock, and notes when the next one comes. It runs
// on the worker's poller, before the claim.
func (a *App) dispatch() {
	now := Now()
	a.mu.Lock()
	if now.Before(a.dispatchAt) {
		a.mu.Unlock()
		return
	}
	a.dispatchAt = never
	entries := make([]*entry, 0, len(a.entries))
	for _, each := range a.entries {
		entries = append(entries, each)
	}
	worker := a.worker
	a.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	next := never
	stored := false
	for _, each := range entries {
		a.mu.Lock()
		due, isDue := each.due(now)
		a.mu.Unlock()
		if isDue {
			at := Stamp(due, each.location)
			_, outcome, err := a.store.Dispatch(due, queue.NewRun{Name: each.name, Data: runData(at, triggerSchedule), Key: queue.Stamp(due),
				MaxAttempts: each.rule.Retry + 1, Timeout: each.rule.Timeout, Available: time.Now()}, !each.rule.Once, time.Now())
			if err != nil {
				// the due time stays due: looked at again after a poll
				a.say(fmt.Sprintf("[schedule] %s, tới giờ %s: không lưu được lần chạy: %v", each.name, at, err))
				if retry := now.Add(queue.PollInterval); retry.Before(next) {
					next = retry
				}
				continue
			}
			a.mu.Lock()
			each.lastAt = due
			a.mu.Unlock()
			stored = stored || outcome == queue.Dispatched
		}
		a.mu.Lock()
		upcoming, ok := each.next(now)
		a.mu.Unlock()
		if ok && upcoming.Before(next) {
			next = upcoming
		}
	}
	a.mu.Lock()
	if next.Before(a.dispatchAt) {
		a.dispatchAt = next
	}
	a.mu.Unlock()
	if stored && worker != nil {
		worker.Due(time.Now())
	}
}

// Stop stops the clock, waiting for its runs, and keeps the runs' file: a now() still stores its run, which the next
// clock of the app's runs claims (a reload of the app, host/reload.go). Start does not start it again.
func (a *App) Stop() {
	a.life.Lock()
	defer a.life.Unlock()
	a.mu.Lock()
	a.stopped = true
	worker := a.worker
	a.mu.Unlock()
	if worker != nil {
		worker.Stop()
	}
}

// Close stops the clock, waiting for its runs, and lets go of the runs' file.
func (a *App) Close() {
	a.Stop()
	a.life.Lock()
	defer a.life.Unlock()
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.file.Close()
}

// Wake tells the clock to read the runs now: runs another App of the same runs stored (a now() of the boot of the app
// a reload put aside, host/reload.go).
func (a *App) Wake() {
	a.mu.Lock()
	worker := a.worker
	a.mu.Unlock()
	if worker != nil {
		worker.Due(time.Now())
	}
}

// now stores a run of the schedule name due now, outside its rule: context.job.at is when now() was called. The clock
// runs it as it runs a due time.
func (a *App) now(name string) value.Value {
	each := a.entryOf(name)
	at := Stamp(Now(), each.location)
	now := time.Now()
	run, _, err := a.store.Enqueue(queue.NewRun{Name: name, Data: runData(at, triggerNow), MaxAttempts: each.rule.Retry + 1,
		Timeout: each.rule.Timeout, Available: now}, now)
	if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s.now(): không lưu được lần chạy: %v", name, err))
	}
	a.mu.Lock()
	worker := a.worker
	a.mu.Unlock()
	if worker != nil {
		worker.Due(now)
	}
	receipt := value.Object()
	receipt.Set("id", value.String(run.ID))
	receipt.Set("name", value.String(run.Name))
	receipt.Set("status", value.String(run.Status))
	receipt.Set("at", value.String(at))
	return receipt
}

// list is what list() reads of each schedule, by name: name, rule, mode, retry, timeout, zone, the next due time
// (null for a once() that ran) and its file.
func (a *App) list(limit, offset int) value.Value {
	now := Now()
	a.mu.Lock()
	entries := make([]*entry, 0, len(a.entries))
	for _, each := range a.entries {
		entries = append(entries, each)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	entries = entries[min(offset, len(entries)):]
	entries = entries[:min(limit, len(entries))]
	out := make([]value.Value, len(entries))
	for index, each := range entries {
		item := value.Object()
		item.Set("name", value.String(each.name))
		item.Set("rule", value.String(each.rule.Text()))
		item.Set("mode", value.String(each.rule.Mode()))
		item.Set("retry", value.Number(float64(each.rule.Retry)))
		timeout := value.Null()
		if each.rule.TimeoutText != "" {
			timeout = value.String(each.rule.TimeoutText)
		}
		item.Set("timeout", timeout)
		item.Set("timezone", value.String(each.location.String()))
		next := value.Null()
		if due, ok := each.next(now); ok {
			next = value.String(Stamp(due, each.location))
		}
		item.Set("next", next)
		item.Set("source", value.String(each.source))
		out[index] = item
	}
	a.mu.Unlock()
	return value.Array(out)
}

// runData is what a schedule's run keeps in kitwork_runs' data: the due time as the author reads it (context.job.at),
// and what started it. A schedule carries no data of its own (mục 7). The first engine's, field for field.
func runData(at, trigger string) string {
	encoded, _ := json.Marshal(map[string]string{"at": at, "trigger": trigger})
	return string(encoded)
}

// runOf reads what runData wrote.
func runOf(data string) (at, trigger string) {
	var decoded struct {
		At      string `json:"at"`
		Trigger string `json:"trigger"`
	}
	_ = json.Unmarshal([]byte(data), &decoded)
	return decoded.At, decoded.Trigger
}

// Summary is a schedule as kitwork check's map says it: its name, where it is declared, its rule and mode, the zone it
// counts in and which tier gave it (ZoneOwn, ZoneInstant, ZoneApp).
type Summary struct {
	Name, Place, Rule, Mode, Zone, ZoneFrom string
}

// The tiers a schedule's zone comes from (slice 3 of root docs/KITWORK_MODULE_WORK.md, mục 6): its own .timezone(…),
// the offset of the instant of on(…), or the app's (module.timezone, else app.timezone, else UTC).
const (
	ZoneOwn     = "own"
	ZoneInstant = "instant"
	ZoneApp     = "app"
)

// Declared are the schedules the app declares, by name (host Map).
func (a *App) Declared() []Summary {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Summary, 0, len(a.entries))
	for _, each := range a.entries {
		zone := ""
		if each.location != nil {
			zone = each.location.String()
		}
		out = append(out, Summary{Name: each.name, Place: each.place, Rule: each.rule.Text(), Mode: each.rule.Mode(), Zone: zone, ZoneFrom: each.zoneFrom})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Rules are the rules of the schedules the app writes, schedule.keep, database, session: each word, where it is called.
func (a *App) Rules() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]string, len(a.calls))
	for word, place := range a.calls {
		out[word] = place
	}
	return out
}
