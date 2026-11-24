package queue

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// App is the queue of one app, which its sites share: the jobs its .shared/queue/ declares, the file of their runs,
// the worker that runs them once the host serves (docs/KITWORK_QUEUE.md).
type App struct {
	say   func(line string)
	root  string
	open  Opener
	file  *File
	store *Store
	// Locate gives the file of a database of the app's .shared/schema/, for queue.database(db) (library/database FileOf)
	Locate func(v value.Value) (path, name string, ok bool)
	// place is where the runs are, as the worker's line names it: the app's own file, or a database of the catalogue
	place string

	mu sync.Mutex
	// declared are the jobs the file the loader runs now evaluates, until Loaded binds them
	declared []*Job
	// names are the jobs' names by the place of their queue.job(…), handlers the jobs by name
	names    map[string]string
	handlers map[string]*Handler
	parallel int
	keep     time.Duration
	// calls are where each word of .shared/queue/config.work was called
	calls  map[string]string
	worker *Worker
	// stopped says the worker stopped for good (Stop), the file kept; closed that the file is let go too (Close)
	stopped bool
	closed  bool
}

// NewApp is the queue of the app at root, its runs in the app's database opened by open, its lines said by say.
func NewApp(root string, open Opener, say func(string)) *App {
	file := NewFile(root, open, "kitwork/queue")
	return &App{say: say, root: root, open: open, file: file, store: NewStore(file, KindQueue), place: ".database/application/kitwork.db",
		names: map[string]string{}, handlers: map[string]*Handler{}, parallel: DefaultParallel, keep: DefaultKeep, calls: map[string]string{}}
}

// Place puts the runs of kind in the database at path (named name, its place where the call is written), one of the
// host's catalogue, as queue.database(db) and schedule.database(db) do (docs/KITWORK_QUEUE.md mục 2): the app's own
// file is the default said again; a file another app keeps its runs in is said, and the runs stay in the app's own.
// The runs of before stay where they are, and the runs the app's own database still holds unended are said. It gives
// the file the runs are in, and where the worker's line says they are.
func Place(root string, open Opener, kind, path, name, where string, say func(string)) (*File, string) {
	own := NewFile(root, open, "kitwork/"+kind)
	if sameFile(path, own.Path()) {
		return own, ".database/application/kitwork.db"
	}
	label, call, noun := "[queue] ", "queue.database(…)", "job"
	if kind == KindSchedule {
		label, call, noun = "[schedule] ", "schedule.database(…)", "lịch"
	}
	if holder := ClaimFile(path, root); holder != "" {
		say(fmt.Sprintf("%s%s: %s đã giữ các lần chạy của app %s: kitwork_runs không ghi app nào, nên mỗi database của danh mục giữ các lần chạy của một app; host khai một tên khác cho app này. Các lần chạy nằm trong .database/application/kitwork.db",
			label, where, name, filepath.Base(holder)))
		return own, ".database/application/kitwork.db"
	}
	switch count, err := Unended(root, open, kind); {
	case err != nil:
		say(fmt.Sprintf("%s.database/application/kitwork.db: không đọc được các lần chạy còn ở đó: %v", label, err))
	case count > 0:
		say(fmt.Sprintf("%s.database/application/kitwork.db còn %d lần chạy chưa xong của %s (pending, running, cancelling): chúng ở lại đó và không chạy, vì %s đặt các lần chạy ở %s; đổi database không dời lần chạy",
			label, count, noun, call, name))
	}
	return NewFileAt(path, open, "kitwork/"+kind), name
}

// declare records a job the loader's run of a file of .shared/queue/ evaluates.
func (a *App) declare(job *Job) {
	a.mu.Lock()
	a.declared = append(a.declared, job)
	a.mu.Unlock()
}

// Loaded binds the jobs the loader's run of file (".shared/queue/camOn.work", its source) declared to the names they
// are exported under, and keeps each as a job of the app: a job not exported, a name two files declare, a job whose
// rules are wrong are its errors. A job of a file this one imports is loaded with its own file.
func (a *App) Loaded(file, source string) error {
	a.mu.Lock()
	declared := a.declared
	a.declared = nil
	a.mu.Unlock()
	if !declares(declared, file) {
		return nil // a file of the app that declares no job: what it exports is no job's
	}
	places, err := ExportedChains(file, source, func(word string) bool { return word == "job" })
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(source))
	hash := hex.EncodeToString(sum[:8])
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, job := range declared {
		if !strings.HasPrefix(job.place, file+":") {
			continue
		}
		name, ok := places[job.place]
		if !ok {
			return fmt.Errorf("%s: queue.job không export: export const camOn = queue.job(…) ở cấp file, tên job là tên export", job.place)
		}
		if first, taken := a.handlers[name]; taken {
			return fmt.Errorf("%s: job %s đã khai ở %s; tên job duy nhất trong app", job.place, name, first.Place)
		}
		a.names[job.place] = name
		a.handlers[name] = &Handler{Name: name, Source: file, Place: job.place, Function: job.function, Retry: job.retry,
			Timeout: job.timeout, TimeoutText: job.timeoutText, ContentHash: hash}
	}
	return nil
}

// declares says one of jobs is declared in file: its place is in it.
func declares(jobs []*Job, file string) bool {
	for _, job := range jobs {
		if strings.HasPrefix(job.place, file+":") {
			return true
		}
	}
	return false
}

// handler is the job of name the app declares, nil when it declares none.
func (a *App) handler(name string) *Handler {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handlers[name]
}

// jobAt is the job declared at place, nil when no file of .shared/queue/ exports one there.
func (a *App) jobAt(place string) *Handler {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handlers[a.names[place]]
}

// Handlers are the jobs of the app, by name.
func (a *App) Handlers() []*Handler {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*Handler, 0, len(a.handlers))
	for _, handler := range a.handlers {
		out = append(out, handler)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// configure is a word of .shared/queue/config.work called at place: the error when it was called before.
func (a *App) configure(word, place string, set func()) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if first, called := a.calls[word]; called {
		if first == place {
			return nil // the same call, run again by a file that imports the one that writes it
		}
		return fmt.Errorf("queue.%s gọi hai lần, ở %s và %s: mỗi chữ một lần trong app", word, first, place)
	}
	a.calls[word] = place
	set()
	return nil
}

// Start runs the app's jobs with run, the host's: the worker, which writes their definitions and claims what is due.
// An app that declares no job starts nothing; one closed or started already, nothing more.
func (a *App) Start(run Runner) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.stopped || a.worker != nil || len(a.handlers) == 0 {
		return nil
	}
	handlers := make([]*Handler, 0, len(a.handlers))
	for _, handler := range a.handlers {
		handlers = append(handlers, handler)
	}
	worker := NewWorker(KindQueue, a.place, a.store, run, jobContext, a.say)
	worker.Configure(handlers, a.parallel, a.keep)
	if err := worker.Start(); err != nil {
		return err
	}
	a.worker = worker
	return nil
}

// running is the app's worker, nil before Start.
func (a *App) running() *Worker {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.worker
}

// Stop stops the worker, waiting for its attempts, and keeps the runs' file: a send still stores its run, which the
// next worker of the app's runs claims (a reload of the app, host/reload.go). Start does not start it again.
func (a *App) Stop() {
	a.mu.Lock()
	a.stopped = true
	worker := a.worker
	a.mu.Unlock()
	if worker != nil {
		worker.Stop()
	}
}

// Close stops the worker, waiting for its attempts, and lets go of the runs' file.
func (a *App) Close() {
	a.Stop()
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.file.Close()
}

// Wake tells the worker to read the runs now: runs another App of the same runs stored, which it was not told of (the
// boot of the app a reload put aside, host/reload.go).
func (a *App) Wake() {
	if worker := a.running(); worker != nil {
		worker.Due(time.Now())
	}
}

// Declared are the jobs the app declares, by name: what kitwork check's map says of them (host Map).
func (a *App) Declared() []Handler {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Handler, 0, len(a.handlers))
	for _, handler := range a.handlers {
		out = append(out, *handler)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Rules are the rules of the queue the app writes, queue.parallel, keep, database: each word, where it is called.
func (a *App) Rules() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]string, len(a.calls))
	for word, place := range a.calls {
		out[word] = place
	}
	return out
}
