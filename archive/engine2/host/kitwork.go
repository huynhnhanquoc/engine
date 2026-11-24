package host

import (
	"path"
	"strconv"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/application"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/file"
	httpmodule "github.com/kitwork/engine2/library/http"
	"github.com/kitwork/engine2/library/module"
	"github.com/kitwork/engine2/library/napas"
	"github.com/kitwork/engine2/library/queue"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/routine"
	"github.com/kitwork/engine2/library/schedule"
	"github.com/kitwork/engine2/library/schema"
	"github.com/kitwork/engine2/library/storage"
)

// Kitwork is `kitwork` as one file sees it, BUILTIN 0 of its run: import * as router from
// "kitwork/router" compiles to const router = kitwork.router, so each module is a property (a method of
// no parameter, which a read calls) giving a new module value for that import, built for the file that
// runs: the manifest, a config.work or router.work at boot, or a request.
type Kitwork struct {
	// manifest is what application.work declares; nil in every other file.
	manifest *application.Manifest
	// folder is what file.env reads in: the manifest's folder, or the app's root.
	folder string
	// entry is the folder of the file the VM runs, relative to folder, "/"-separated ("" is folder).
	entry string
	// vm runs the file at boot, which tells where a call is written; nil at a request.
	vm *runtime.VM
	// scope is the folder a config.work or router.work declares into, file which of the three runs; look is
	// what a design.work declares into.
	scope *router.Scope
	look  *design.Scope
	file  string
	// notice prints one boot line; requirements records what env.require misses.
	notice       func(text string)
	requirements *Requirements
	// running is true while the file runs at boot: the modules it imports declare and read only then.
	running bool
	// databases is the app's catalog of databases; declaring is the host's pass over the app's plain files, the one
	// run that declares them (docs/schema.md mục 2)
	databases *database.Catalog
	declaring bool
	// site is the site the file runs for: what kitwork/http sends a path of its own to; nil for a file of the app
	site *Site
	// storage is kitwork/storage of the app, for a file of the app root (a site's file reads its site's)
	storage *storage.App
	// components are the app's, which component.define declares into at the pass that loads the app's work (any plain
	// .work file, docs/component.md mục 2); a site's file finds them through its site
	components *component.Definitions
	// background is the app's queue and schedules, for a file of the app root (a site's file reads its site's);
	// loading the host's pass that loads the app's work, loadWork, "" for any other run (background.go)
	background *background
	loading    string
	// declared is what module.work declares, the app's root and its values; nil in every other file (library/module)
	declared *module.Declared
}

// backgroundOf is the app's background the file runs for: its own, or its site's.
func (k *Kitwork) backgroundOf() *background {
	if k.background != nil {
		return k.background
	}
	if k.site != nil {
		return k.site.background
	}
	return nil
}

func (k *Kitwork) open() bool { return k.running }

// where is the place of the call the file makes now, "alpha.test/users/router.work:3:1", from the
// folder: a module the file imports names its own file.
func (k *Kitwork) where() string {
	if k.vm == nil {
		return k.entry
	}
	location := k.vm.CallSite()
	written := path.Join(k.entry, location.File)
	if location.Line <= 0 {
		return written
	}
	return written + ":" + strconv.Itoa(int(location.Line)) + ":" + strconv.Itoa(int(location.Column))
}

// here is the folder of the file writing the call in progress, relative to folder: "./…" of file.env.
func (k *Kitwork) here() string {
	if k.vm == nil {
		return k.entry
	}
	written := path.Dir(path.Join(k.entry, k.vm.CallSite().File))
	if written == "." {
		return ""
	}
	return written
}

func (k *Kitwork) require(key string) bool {
	if k.requirements == nil {
		return false
	}
	return k.requirements.Report(key, k.where())
}

// Application is "kitwork/application", the manifest's words: only application.work has it.
func (k *Kitwork) Application() value.Value {
	if k.manifest == nil {
		return value.Fail(value.UnsupportedCode, "kitwork/application is the host's manifest's: only application.work imports it")
	}
	app := application.New(k.manifest, k.folder, k.notice)
	app.Where = k.where // app.timezone says where it is written, which kitwork check's map prints
	return value.Struct(app)
}

// File is "kitwork/file": file.env, from the manifest's folder with the system's variables on top, or
// from the app's root, never the system's. A request has no file.env: the environment is read at boot.
func (k *Kitwork) File() value.Value {
	if failure, ok := k.designOnly("file"); ok {
		return failure
	}
	if k.vm == nil {
		return value.Struct(&file.File{})
	}
	return value.Struct(&file.File{
		Root:     k.folder,
		Here:     k.here,
		Manifest: k.manifest != nil,
		Require:  k.require,
		Notice:   k.notice,
		Booting:  k.open,
	})
}

// Router is "kitwork/router": the words of the folder that runs, only in config.work and router.work.
func (k *Kitwork) Router() value.Value {
	if failure, ok := k.designOnly("router"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/router is a site's: router.work and config.work import it, application.work does not")
	}
	if k.declaring {
		return value.Fail(value.UnsupportedCode, "kitwork/router is imported by "+k.file+", a plain file of the app the boot reads for its tables, jobs and schedules: only router.work and config.work import it, a door is declared in router.work")
	}
	if k.components != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/router is imported by "+k.file+", which defines components: a door is declared in router.work")
	}
	if k.scope == nil {
		return value.Struct(&router.Router{})
	}
	// a library the file imports may not import it (KITWORK_ROUTER.md mục 3 and 11): its words would
	// declare the doors of whichever folder imports the library (05/11/2026)
	if k.vm != nil {
		if written := k.vm.CallSite().File; written != "" && written != k.file {
			return value.Fail(value.UnsupportedCode, "kitwork/router is imported by "+written+": only router.work and config.work import it; a library gives the handler, and router.work declares the door")
		}
	}
	return value.Struct(&router.Router{Scope: k.scope, File: k.file, Open: k.open})
}

// Napas is "kitwork/napas", a VietQR payload. The import is frozen from the start, so every chain from it
// begins on a copy of its own, as a JavaScript namespace import is the same however often a chain starts
// from it: a file's `const shop = napas.bank(…).receiver(…)` and `const other = napas.bank(…)` are two
// chains. Until 05/11/2026 the import was a builder until the boot froze it, so each chain the file made
// wrote into the import itself: shop's QR paid into other's account, and every request's chain carried
// shop's receiver and note.
func (k *Kitwork) Napas() value.Value {
	if failure, ok := k.designOnly("napas"); ok {
		return failure
	}
	imported := &napas.Napas{}
	imported.Freeze()
	return value.Struct(imported)
}

// Schema is "kitwork/schema": struct({ … }) and the kinds of its fields, declared only in the app's
// .shared/schema/ and imported from there by any file (docs/schema.md mục 2): one place where the shapes of the
// app's data are, which kitwork/database reads its tables from. The import is frozen from the start, as napas's:
// every builder returns a new value, so two structs a file makes from one field are each their own.
func (k *Kitwork) Schema() value.Value {
	if failure, ok := k.designOnly("schema"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/schema is an app's: its structs are declared in a plain .work file of the app, not in application.work")
	}
	if written := k.written(); written != "" {
		if declared := path.Join(k.entry, written); module.RoleFile(declared) {
			return value.Fail(value.UnsupportedCode, "kitwork/schema is imported by "+declared+", a file with a role: a struct is declared in a plain .work file of the app and imported from there, import { customers } from \"app/core/shop.work\"")
		}
	}
	imported := &schema.Schema{}
	if k.declaring && k.databases != nil {
		// the declaring pass tells the catalog of each struct, which refuses one written with a word of a
		// table that no tables({ … }) takes (Catalog.Close)
		catalog := k.databases
		imported.Made = func(made *schema.Struct) { catalog.Made(made, k.where()) }
	}
	imported.Freeze()
	return value.Struct(imported)
}

// Database is "kitwork/database": connection(…).tables({ … }) declares the app's databases in its
// .shared/schema/, where the host's pass runs each file once as the app boots; a site's file importing one of
// them runs it again and finds that declaration in the app's catalog (library/database). The aggregates
// (sum, count, …) are imported by any file, for the lambdas of its statements.
func (k *Kitwork) Database() value.Value {
	if failure, ok := k.designOnly("database"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/database is an app's: the host's databases are app.databases(…) of application.work")
	}
	imported := &database.Database{Catalog: k.databases, Declaring: k.declaring, Where: k.where, Written: k.declaredIn}
	imported.Freeze()
	return value.Struct(imported)
}

// declaredIn is the file writing the call in progress, from the app's root (".shared/schema/shop.work"); ""
// at a request.
func (k *Kitwork) declaredIn() string {
	if written := k.written(); written != "" {
		return path.Join(k.entry, written)
	}
	return ""
}

// Http is "kitwork/http": a request out of the app, described then sent (library/http) to an http:// or https:// URL;
// the SSRF guard refuses loopback and private space, so a site never calls itself. The import keeps nothing a program
// writes: every step returns a new value.
func (k *Kitwork) Http() value.Value {
	if failure, ok := k.designOnly("http"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/http is an app's: application.work declares the host and sends nothing")
	}
	imported := &httpmodule.Module{Open: k.open}
	if k.site != nil {
		imported.Cache = k.site.answers
		if k.site.storage != nil {
			imported.Persist = k.site.storage.Answers() // .persist() keeps on the app's disk (kitwork/storage)
		}
	}
	return value.Struct(imported)
}

// Routine is "kitwork/routine": work run apart from its caller, on VMs of the site (library/routine). A file of no
// site imports it and starts nothing.
func (k *Kitwork) Routine() value.Value {
	if failure, ok := k.designOnly("routine"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/routine is an app's: application.work declares the host and runs nothing")
	}
	imported := &routine.Routine{Open: k.open}
	if k.site != nil {
		imported.Site = k.site.routinesOf()
	}
	return value.Struct(imported)
}

// Storage is "kitwork/storage": values by key in the memory, the disk and the datastore of the app (library/storage),
// which its sites share.
func (k *Kitwork) Storage() value.Value {
	if failure, ok := k.designOnly("storage"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/storage is an app's: application.work declares the host and keeps nothing")
	}
	imported := &storage.Module{Open: k.open, App: k.storage}
	if imported.App == nil && k.site != nil {
		imported.App = k.site.storage
	}
	return value.Struct(imported)
}

// unserved is a module of docs/KITWORK_ENGINE.md mục 3 engine2 does not serve yet: the import itself
// fails, so the boot stops where it is written, saying which.
func unserved(module string) value.Value {
	return value.Fail(value.UnsupportedCode, "kitwork/"+module+" is not served by engine2 yet")
}

// Design is "kitwork/design": the look of the folder, only in design.work (docs/KITWORK_DESIGN.md mục 1).
func (k *Kitwork) Design() value.Value {
	if k.look == nil {
		return value.Fail(value.UnsupportedCode, "kitwork/design is design.work's: a folder's look is declared in its design.work, beside its config.work and router.work")
	}
	if written := k.written(); written != "" && written != k.file {
		return value.Fail(value.UnsupportedCode, "kitwork/design is imported by "+written+": only design.work imports it; a library gives a value, design.work declares the look")
	}
	return value.Struct(&design.Design{Scope: k.look, Open: k.open})
}

// designOnly refuses a library design.work itself imports: its vocabulary is closed, kitwork/design and
// values of the app's files (a colour from the environment comes through a .shared file with file.env). module.work's
// is closed the same way, to kitwork/module alone (17/11/2026).
func (k *Kitwork) designOnly(library string) (value.Value, bool) {
	if k.written() != k.file || k.written() == "" {
		return value.Value{}, false
	}
	switch {
	case k.look != nil:
		return value.Fail(value.UnsupportedCode, "design.work imports only kitwork/design, not kitwork/"+library+": its words are the look's (a value of another file comes by import { x } from \"app/core/…\")"), true
	case k.declared != nil && library != "module":
		return value.Fail(value.UnsupportedCode, "module.work imports only kitwork/module, not kitwork/"+library+": it says where the app's root is and what the whole app keeps; the app's code is in its other files"), true
	}
	return value.Value{}, false
}

// Module is "kitwork/module": the words of module.work, the app's root (library/module), which no other file imports.
func (k *Kitwork) Module() value.Value {
	if k.declared == nil {
		return value.Fail(value.UnsupportedCode, "kitwork/module is module.work's: the app's root and the values of the whole app are declared in the module.work at its root")
	}
	if written := k.written(); written != "" && written != k.file {
		return value.Fail(value.UnsupportedCode, "kitwork/module is imported by "+written+": only module.work imports it")
	}
	return value.Struct(&module.Module{Declared: k.declared, Where: k.where, Open: k.open})
}

// written is the file writing the call in progress at boot, as the VM names it; "" at a request.
func (k *Kitwork) written() string {
	if k.vm == nil {
		return ""
	}
	return k.vm.CallSite().File
}

// Component is "kitwork/component" (docs/component.md): component.define declares at the top level of a plain .work file
// of the app, kept by the host's pass that loads the app's work; a file that imports it finds the declaration by its
// place (library/component).
func (k *Kitwork) Component() value.Value {
	if failure, ok := k.designOnly("component"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/component is an app's: application.work declares the host and draws no page")
	}
	app := k.components
	if app == nil && k.site != nil {
		app = k.site.components
	}
	return value.Struct(&component.Module{App: app, Loading: k.loading == loadWork, Declaring: k.declaring, Where: k.where, Written: k.declaredIn, Open: k.open})
}

// Handle is "kitwork/handle", which is no library (the owner, 19/11/2026, docs/language.md mục 5): error(…) and safe(…)
// are builtins, written without an import. A CLAUDE.md of 27–28/10/2026 moved them there; the decision of 05/11/2026
// kept them builtins, and the owner chose it.
func (k *Kitwork) Handle() value.Value {
	return value.Fail(value.UnsupportedCode, "kitwork/handle is no library: error(…) and safe(…) are builtins, written without an import: return error(\"…\"), safe(() => …) (docs/language.md mục 5)")
}

// Queue is "kitwork/queue": a job declared with queue.job(…) at the top level of a plain .work file of the app, kept by
// the host's pass that loads the app's work, sent from anywhere in the app through the handle a file imports
// (library/queue).
func (k *Kitwork) Queue() value.Value {
	if failure, ok := k.designOnly("queue"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/queue is an app's: application.work declares the host and runs no job")
	}
	imported := &queue.Module{Place: k.where, Written: k.declaredIn, Open: k.open, Loading: k.loading == loadWork}
	if back := k.backgroundOf(); back != nil {
		imported.App = back.queue
	}
	return value.Struct(imported)
}

// Schedule is "kitwork/schedule": a schedule declared with schedule.<rule>(…).run(…) at the top level of a plain .work
// file of the app, kept by the host's pass that loads the app's work, read and run now from anywhere in the app through
// the handle a file imports (library/schedule).
func (k *Kitwork) Schedule() value.Value {
	if failure, ok := k.designOnly("schedule"); ok {
		return failure
	}
	if k.manifest != nil {
		return value.Fail(value.UnsupportedCode, "kitwork/schedule is an app's: application.work declares the host and runs no schedule")
	}
	imported := &schedule.Module{Place: k.where, Written: k.declaredIn, Open: k.open, Loading: k.loading == loadWork}
	if back := k.backgroundOf(); back != nil {
		imported.App = back.schedules
	}
	return value.Struct(imported)
}

func (k *Kitwork) Tunnel() value.Value { return unserved("tunnel") }
