package host

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/module"
	"github.com/kitwork/engine2/library/queue"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/schedule"
	"github.com/kitwork/engine2/library/storage"
)

// The background work of an app (docs/queue.md, docs/schedule.md): its jobs and its schedules, run by workers of the app
// once the host serves. Each is declared at the top level of a plain .work file of the app, wherever it is (17/11/2026,
// slice 2 of root docs/KITWORK_MODULE_WORK.md; until then only in .shared/queue/ and .schedule/). The host's pass that
// loads the app's work runs each such file once as the app boots, after its module.work (whose module.timezone is the
// schedules' zone), the pass that declares its databases and its config.work, before its sites: what each file exports
// with queue.job(…) or schedule.<rule>(…).run(…) is a job, a schedule of the app, its name the export's. A site's file
// that imports one runs the declaration again and gets a handle that sends, reads, runs now; the workers run the
// functions the pass kept, on a VM of the app, with the app's builtins (its console, its zone's Date).

// loadWork is the pass a file's kitwork runs in when it loads the app's work (Kitwork.loading).
const loadWork = "work"

// background is one app's: its queue, its schedules, and what a run of its work reads (the app's builtins, a pool of
// VMs).
type background struct {
	root      string
	queue     *queue.App
	schedules *schedule.App
	// globals and slots are the app's builtins, set once its zone is known (prepare); builtins are slots with the
	// kitwork of a run of the app in slot 0
	globals  map[string]value.Value
	slots    []value.Value
	builtins []value.Value
	machines sync.Pool
	log      func(string)
	// pool is the host's, what every run makes together (runtime.Pool), nil for none; made what one job makes at most,
	// energy what it spends
	pool   *runtime.Pool
	made   uint64
	energy uint64
}

// newBackground is the background of the app at appRoot, saying its lines in the log led by the app's identity.
func newBackground(h *Host, appRoot, identity string) *background {
	lead := "[" + identity + "] "
	say := func(line string) { h.log.Led(lead, line) }
	_, made, energy := h.tenantLimits(identity) // a job and a schedule run within the tenant's limits, as its sites do
	back := &background{root: appRoot, queue: queue.NewApp(appRoot, database.Acquire, say), schedules: schedule.NewApp(appRoot, database.Acquire, say), log: say, pool: h.pool, made: made, energy: energy}
	// queue.database(db), schedule.database(db): the file of a database of the app's .shared/schema/
	back.queue.Locate, back.schedules.Locate = database.FileOf, database.FileOf
	return back
}

// prepare builds the app's builtins in its zone, what the passes over .shared/queue/ and .schedule/ and every run of
// their work read, and gives the schedules the app's zone (declared: the app writes module.timezone).
func (b *background) prepare(h *Host, appRoot, identity string, zone *time.Location, declared bool, kept *storage.App) {
	b.schedules.Zone, b.schedules.ZoneDeclared = zone, declared
	b.globals, b.slots = builtinsOf(h.log, "["+identity+"] ", zone)
	b.builtins = withKitwork(b.slots, &Kitwork{folder: appRoot, storage: kept, background: b})
}

// run runs fn with argument on a VM of the app until ctx ends: a run of a job.
func (b *background) run(ctx context.Context, fn *value.Lambda, argument value.Value) value.Value {
	program, ok := runtime.ProgramFromRef(fn.Program)
	if !ok {
		return value.Fail(value.TypeCode, "a job's function has no program")
	}
	machine, _ := b.machines.Get().(*runtime.VM)
	if machine == nil {
		machine = runtime.New(program)
	}
	machine.FastReset(program, b.globals)
	machine.Builtins = b.builtins
	machine.MaxEnergy = b.energy
	machine.MaxBytes = b.made
	machine.Pool = b.pool
	machine.Context = ctx
	result := machine.ExecuteLambda(fn, []value.Value{argument})
	machine.ResetForPool()
	b.machines.Put(machine)
	return result
}

// start runs the app's work: the worker of its queue, the clock of its schedules.
func (b *background) start() {
	if err := b.queue.Start(b.run); err != nil {
		b.log("[queue] worker không chạy: " + err.Error())
	}
	if err := b.schedules.Start(b.run); err != nil {
		b.log("[schedule] đồng hồ không chạy: " + err.Error())
	}
}

// pause stops the app's work, waiting for what runs, and keeps its files: what a request of the boot still sends or
// asks now() of is stored, for the worker of the app's next boot (a reload, reload.go).
func (b *background) pause() {
	b.schedules.Stop()
	b.queue.Stop()
}

// closeFiles lets go of the files paused work kept, not of the host's catalogue's claims, which the app's next boot
// holds.
func (b *background) closeFiles() {
	b.schedules.Close()
	b.queue.Close()
}

// wake tells the app's worker and clock to read their runs now: runs a boot put aside stored, which they were not told
// of.
func (b *background) wake() {
	b.queue.Wake()
	b.schedules.Wake()
}

// stop stops the app's work, waiting for what runs (queue.StopGrace at most for each), and gives back the files of the
// host's catalogue the app kept its runs in; again, it does nothing.
func (b *background) stop() {
	b.schedules.Close()
	b.queue.Close()
	queue.ReleaseFiles(b.root)
}

// StartBackground starts the background work of every app that booted, one with no site too: the worker of its queue
// and the clock of its schedules, which claim the runs that are due, those a process before left too. Serve starts
// it, after Boot; a check, which boots and serves nothing, does not. Each starts once, however often this is called
// (queue.App Start); Close stops them.
func (h *Host) StartBackground() {
	h.reloading.Lock()
	defer h.reloading.Unlock()
	h.serving = true // an app a reload boots starts its own (reload.go)
	for _, run := range h.apps {
		if run.back != nil {
			run.back.start()
		}
	}
}

// loadBackground is the host's pass that loads the app's work (docs/queue.md, docs/schedule.md): every plain .work file
// of the app (moduleFiles) run once, those that write a rule of the queue or the schedules first (queue.database(…) says
// where the runs and the schedules' state are before a schedule is read), then the others in the order of their path;
// what a file exports with queue.job(…), with schedule.<rule>(…).run(…) or .once(…), is a job, a schedule of the app,
// what it writes with component.define(…) a component of the app (slice 7), and its statements on the app's databases
// are translated as any file's.
func (h *Host) loadBackground(appRoot, identity string, requirements *Requirements, databases *database.Catalog, kept *storage.App, back *background, components *component.Definitions) error {
	files, err := moduleFiles(appRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", identity, err)
	}
	for _, relative := range rulesFirst(appRoot, files) {
		file := filepath.Join(appRoot, filepath.FromSlash(relative))
		entry := path.Dir(relative)
		if entry == "." {
			entry = ""
		}
		kitwork := &Kitwork{folder: appRoot, entry: entry, scope: router.NewScope(router.LevelApp, nil, back.log), file: path.Base(relative),
			notice: back.log, requirements: requirements, databases: databases, storage: kept, background: back, loading: loadWork, components: components}
		if err := h.bootFile(appRoot, file, back.globals, back.slots, kitwork); err != nil {
			return err
		}
		source, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("%s: %w", writtenIn(appRoot, file), err)
		}
		if err := back.queue.Loaded(relative, string(source)); err != nil {
			return fmt.Errorf("%s/%w", identity, err)
		}
		if err := back.schedules.Loaded(relative, string(source)); err != nil {
			return fmt.Errorf("%s/%w", identity, err)
		}
	}
	return nil
}

// moduleFiles are the plain .work files of the app at appRoot, "/"-separated from its root, sorted: what the boot reads
// for the tables, the jobs and the schedules the app declares (17/11/2026, slice 2 of root docs/KITWORK_MODULE_WORK.md).
// Every .work file of its folders, those of its sites among them, but a file with a role (module.RoleFile: module.work,
// router.work, config.work, design.work), a link, and the files of a folder of underscore (a draft) or of a dot folder
// (.database/ is data). The app's .shared/ and .schedule/, which the code's places were, are the boot's error
// (oldFolders); so are its components since slice 7, each file declaring its own.
func moduleFiles(appRoot string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(appRoot, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(appRoot, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." || relative == "application.work" {
			return nil // the manifest of the app alone is its runner's, never its code (alone.go)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if hidden(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".work") && !module.RoleFile(relative) {
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

// backgroundRules are the words of the queue and the schedules that rule every job, every schedule of the app.
var backgroundRules = map[string]map[string]bool{
	"queue":    {"parallel": true, "keep": true, "database": true},
	"schedule": {"keep": true, "database": true, "session": true},
}

// rulesFirst is files with those that write a rule of the queue or the schedules at their top level first, each in the
// order of its path: read from their words, the namespace import of "kitwork/queue" or "kitwork/schedule" a file calls.
func rulesFirst(appRoot string, files []string) []string {
	var first, rest []string
	for _, relative := range files {
		if writesRules(filepath.Join(appRoot, filepath.FromSlash(relative))) {
			first = append(first, relative)
		} else {
			rest = append(rest, relative)
		}
	}
	return append(first, rest...)
}

// writesRules says the file at file calls queue.parallel, keep, database or schedule.keep, database, session at the head
// of a statement of its top level.
func writesRules(file string) bool {
	source, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	parser := compiler.NewParser(compiler.NewLexerSource(string(source), filepath.Base(file)))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return false
	}
	imported := map[string]string{} // the name a file imports the library under, by it
	for _, statement := range program.Statements {
		if declaration, ok := statement.(*compiler.VarStatement); ok && len(declaration.Names) == 1 && declaration.DestructMode == compiler.DestructNone {
			for library := range backgroundRules {
				if _, importing := moduleImport(declaration, library); importing {
					imported[declaration.Names[0].Value] = library
				}
			}
		}
	}
	for _, statement := range program.Statements {
		expression, ok := statement.(*compiler.ExpressionStatement)
		if !ok {
			continue
		}
		for name, library := range imported {
			if links, chain := moduleChain(expression.Expression, name); chain && backgroundRules[library][links[len(links)-1].Method.Value] {
				return true
			}
		}
	}
	return false
}

// backgroundInSite is the error of a site's folder that holds .schedule/ or .shared/queue/, the old places of the app's
// work, which no boot reads (a dot folder): "" when it holds neither.
func backgroundInSite(site string) string {
	if info, err := os.Stat(filepath.Join(site, ".schedule")); err == nil && info.IsDir() {
		return ".schedule/ đã bỏ (17/11/2026): lịch tự khai ở cấp file của một file .work thường của app, export const nhip = schedule.every(\"1m\").run(fn), không trong thư mục chấm"
	}
	if info, err := os.Stat(filepath.Join(site, ".shared", "queue")); err == nil && info.IsDir() {
		return ".shared/queue/ đã bỏ (17/11/2026): job tự khai ở cấp file của một file .work thường của app, export const camOn = queue.job(fn), không trong thư mục chấm"
	}
	return ""
}
