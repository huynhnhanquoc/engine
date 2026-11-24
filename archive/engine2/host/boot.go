package host

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/file"
	httpmodule "github.com/kitwork/engine2/library/http"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/storage"
	"github.com/kitwork/engine2/library/ui"
)

// Boot opens every site of every app under the root, all before the first request (docs/KITWORK_BOOT.md
// mục 4): apps/<identity>/ is an app, whose module.work runs first and says where its sites are (docs/module.md,
// 17/11/2026): each hostname the host gives the app is a site, the folder module.routes gives it, or the app's root
// when module.work writes no module.routes; each site's folders run their config.work then their router.work, parent
// first.
//
// An app is what fails alone (06/11/2026): its sites share its config.work and its .env, so an error anywhere in
// it (its config.work, a site, a key env.require misses, a site folder that is no hostname, a hostname another
// app already serves) closes that app. Its sites answer 503 and the error goes to the log, once; every other app
// boots and serves. Before, one app's error stopped the host: a tenant's broken file took every other tenant
// down. What is the host's own still stops it: the manifest, the folder of the apps. Failed gives the errors of
// the apps it closed, for a check.
func (h *Host) Boot() error {
	if err := h.Check(); err != nil {
		return err
	}
	if err := h.openLog(); err != nil { // app.logger's files: the boot's lines are in them
		return err
	}
	if h.HostBytes > 0 && h.pool == nil {
		h.pool = runtime.NewPool(h.HostBytes) // what every run of the host makes together (runtime/pool.go)
	}
	if h.Manifest.Website == nil {
		none := map[string]*Site{}
		h.live.Store(&none)
		return nil // a desktop, a mobile app: no site of this host to open
	}
	if root := h.root(); !h.oneApp && filepath.Base(root) == "app" {
		return fmt.Errorf("app.root: %s is the layout of one app of 22/10/2026, app/<domain>/, which engine2 does not serve; one app is the folder of its module.work, alone or with application.work beside it", root)
	}
	apps, err := h.appFolders()
	if err != nil {
		return fmt.Errorf("app.root: %w", err)
	}
	// which app serves each hostname, read before any app (domains.go); the layout of one app reads no table (alone.go)
	if !h.oneApp {
		if err := h.loadDomains(); err != nil {
			return err
		}
	}
	if err := h.loadTenants(); err != nil { // each tenant's own limits, from the same file (tenants.go)
		return err
	}
	sites := map[string]*Site{}
	h.failures = nil
	h.reloading.Lock()
	defer h.reloading.Unlock()
	for _, each := range apps {
		run := &appRun{identity: each.identity, root: each.root}
		if h.Manifest.HotReload {
			run.marks = marksOf(each.root) // read before the boot: a change while it boots is a change
		}
		h.apps[each.identity] = run
		opened, err := h.bootWholeApp(run, sites)
		if err != nil {
			h.closeApp(each.root, each.identity, err, sites, run)
			continue
		}
		for hostname, site := range opened {
			sites[hostname] = site
			run.hostnames = append(run.hostnames, hostname)
			if site.closed == nil {
				run.sites = append(run.sites, site) // a name module.routes gave no folder answers 503 (route)
			}
		}
	}
	h.live.Store(&sites) // requests read it until a reload publishes another (Host.current, reload.go)
	h.prewarmUnknown(sites)
	for _, line := range h.tenants.Load().said(h.apps) {
		h.log.Line("[app] " + line)
	}
	h.grantUnknown(sites)
	return nil
}

// Failed is every error of the apps Boot closed, joined; nil when every app booted.
func (h *Host) Failed() error { return errors.Join(h.failures...) }

// bootWholeApp opens one app, run, and every site of it, or none: the sites it opens, by hostname. taken are the
// hostnames of the other apps. What the boot makes goes in run: the catalog of the app's databases, its queue and
// schedules (when it opens); what run holds already, a reload's (reload.go), it keeps: its storage, streams, tabs.
func (h *Host) bootWholeApp(run *appRun, taken map[string]*Site) (opened map[string]*Site, err error) {
	appRoot, identity := run.root, run.identity
	var back *background
	defer func() {
		// a panic of the engine as the app boots closes the app, as an error of its boot does (panic.go)
		if recovered := recover(); recovered != nil {
			opened, err = nil, h.panicked("["+identity+"] ", "boot", recovered)
		}
		switch {
		case back == nil:
		case err != nil:
			back.stop() // a closed app runs nothing: what the passes opened is let go
		default:
			run.back = back
		}
	}()
	requirements := &Requirements{}
	databases := database.NewCatalog(appRoot)
	run.catalog = databases
	if run.kept == nil {
		// kitwork/storage of the app, which its sites share; a calendar expiry reads its zone, its root's
		run.kept = storage.NewApp(appRoot, database.Acquire)
		run.streams = &appStreams{host: h}
		run.tabs = newAppTabs(h) // the tabs of the app's live pages, told what its storage's writes change (tab.go)
		run.kept.OnChange(run.tabs.changed)
	}
	kept, streams, tabs := run.kept, run.streams, run.tabs
	// module.work says where the app's root is and what the whole app keeps, read before any other file of it
	declared, err := h.bootModule(appRoot, identity, requirements)
	if err != nil {
		return nil, err
	}
	run.declared = declared
	if !h.oneApp && isFile(filepath.Join(appRoot, "application.work")) {
		// an app moved from a machine where it ran alone keeps its manifest, which this host's replaces (alone.go)
		h.log.Led("["+identity+"] ", "application.work at the app's root is its own when it runs alone; under this host it is not read: "+filepath.Base(h.File)+" says what the app has")
	}
	// the old places of the app's code are gone (slice 2b): an app that still keeps code there says where it goes
	if err := oldFolders(appRoot, identity); err != nil {
		return nil, err
	}
	if err := rulesInConfig(appRoot, identity); err != nil {
		return nil, err
	}
	// the app's zone, asked up the tiers (slice 3): its module.timezone, the host's app.timezone, UTC
	zone, _ := h.zoneOf(declared)
	back = newBackground(h, appRoot, identity) // the app's queue and schedules (background.go)
	look, app, err := h.bootApp(appRoot, identity, declared.Routes != nil, requirements, databases, kept, back)
	if err != nil {
		return nil, err
	}
	kept.Zone = zone
	back.prepare(h, appRoot, identity, zone, declared.Timezone != nil || h.Manifest.Timezone != nil, kept)
	// the app's components, which its plain .work files declare as the pass loads its work (slice 7)
	components := component.NewDefinitions()
	run.components = components
	if err := h.loadBackground(appRoot, identity, requirements, databases, kept, back, components); err != nil {
		return nil, err
	}
	if declared.Routes != nil && !h.oneApp {
		h.saidFolders(appRoot, identity) // a folder of a public name the table gives the app not (domains.go)
	}
	names := h.siteNamesBy(h.domains.Load(), identity, appRoot)
	if len(names) == 0 {
		h.log.Led("["+identity+"] ", noName(declared.Routes != nil))
	}
	opened = map[string]*Site{}
	for _, hostname := range names {
		if other, ok := taken[hostname]; ok {
			return nil, fmt.Errorf("the site %s is in two apps, %s and %s: one hostname, one app", hostname, other.AppRoot, appRoot)
		}
		folder := "" // the app's root: an app without module.routes is one site
		if declared.Routes != nil {
			folder, err = h.route(declared, appRoot, identity, hostname, zone)
			if err != nil {
				// the name's own error (KITWORK_MODULE_WORK mục 4, luật 3): its site answers 503, the app's others serve
				h.failures = append(h.failures, err)
				h.log.Error("["+identity+"] ", "the site "+hostname+" answers 503: "+err.Error())
				opened[hostname] = &Site{Hostname: hostname, Folder: appRoot, AppRoot: appRoot, closed: err}
				continue
			}
		}
		site, err := h.bootSite(appRoot, folder, hostname, zone, look, app, requirements, databases, kept, back, components)
		if err != nil {
			return nil, err
		}
		site.streams = streams
		site.tabs = tabs
		opened[hostname] = site
	}
	if err := requirements.Close(); err != nil {
		return nil, fmt.Errorf("%s: %w", identity, err)
	}
	// app.prewarm(…): the sites it names read and kept before they serve, at a start and at each reload (prewarm.go)
	for _, hostname := range slices.Sorted(maps.Keys(opened)) {
		if opened[hostname].closed == nil && h.prewarms(hostname) {
			h.prewarm(opened[hostname])
		}
	}
	return opened, nil
}

// closeApp keeps why an app could not boot, says it once in the log, and gives each of its sites a hostname
// that answers 503 (closedSites), which run holds.
func (h *Host) closeApp(appRoot, identity string, err error, sites map[string]*Site, run *appRun) {
	h.failures = append(h.failures, err)
	h.log.Error("[app] ", identity+" is closed, its sites answer 503: "+err.Error())
	run.hostnames = h.closedSites(appRoot, identity, err, sites)
	run.failed = err
}

// closedSites gives each hostname the host gives the app identity at appRoot (siteNamesBy) a site in sites that answers
// 503, and names them: a visitor of a closed app is told it is unavailable, never why, and never finds another app
// there. A hostname another app holds keeps that app: a closed app with a folder of another's local hostname read
// before that app took it, and closed it too ("in two apps").
func (h *Host) closedSites(appRoot, identity string, err error, sites map[string]*Site) []string {
	var hostnames []string
	for _, hostname := range h.siteNamesBy(h.domains.Load(), identity, appRoot) {
		if _, ok := sites[hostname]; ok {
			continue
		}
		sites[hostname] = &Site{Hostname: hostname, Folder: appRoot, AppRoot: appRoot, closed: err}
		hostnames = append(hostnames, hostname)
	}
	return hostnames
}

// hostnameProblem is what keeps name, a site's folder, from being a hostname, "" when it is one: labels of
// 1 to 63 ASCII letters, digits, hyphens and underscores between single dots, 253 bytes at most. The folder
// is the site's hostname (docs/KITWORK_UI.md mục 4: "tên thư mục là hostname"), which a request's Host is
// looked up by and which leads every line the site writes to the host's log, "[name] ". A name holding a
// line break (U+2028, U+0085), a bidirectional override, a space or a bracket booted, and its lead forged a
// line led by another site for a reader of the log (05/11/2026); a request's Host never named it anyway.
func hostnameProblem(name string) string {
	if len(name) > 253 {
		return "longer than 253 bytes"
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return "an empty label: a dot at an end or two dots together"
		}
		if len(label) > 63 {
			return "a label longer than 63 bytes"
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
				continue
			}
			return "a hostname is ASCII letters, digits, hyphens and underscores between dots; an international name is written in its xn-- form"
		}
	}
	return ""
}

// hidden is a folder that is no app, no site and no route: .shared, .database, _drafts. .well-known is a
// route.
func hidden(name string) bool {
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") && name != ".well-known"
}

func isFile(file string) bool {
	info, err := os.Stat(file)
	return err == nil && info.Mode().IsRegular()
}

// builtinsOf are the builtins every run of one site (or one app root, or the manifest) reads, built once:
// the shared frozen set with console writing to the host's log led by lead and, when zone is not nil,
// Date in zone laid over it. A program reads a builtin from its slot (BUILTIN n, vm.Builtins: the
// compiler binds the name since 04/11/2026, and a host's global of that name is not what it reads), so
// slots is what lays them over; slot 0 is left empty, for the kitwork of each run (withKitwork). globals
// holds the same values by name for the one place a program still reads a builtin's name so: a function
// that assigns the name (var JSON = …) and the functions inside it.
func builtinsOf(log *Log, lead string, zone *time.Location) (globals map[string]value.Value, slots []value.Value) {
	globals = map[string]value.Value{}
	builtins.Install(globals)
	slots = builtins.Slots()
	lay := func(name string, item value.Value) {
		globals[name] = item
		if slot, ok := builtins.Slot(name); ok {
			slots[slot] = item
		}
	}
	lay("console", builtins.ConsoleTo(log, lead))
	if zone != nil {
		lay("Date", builtins.DateIn(zone))
	}
	return globals, slots
}

// withKitwork is slots with kitwork in slot 0, a table of its own: every other slot is the value of
// slots, so nothing is built again.
func withKitwork(slots []value.Value, kitwork *Kitwork) []value.Value {
	run := slices.Clone(slots)
	run[0] = value.Struct(kitwork)
	return run
}

// bootApp runs the app root's design.work and config.work, the parents of every site of an app whose module.work
// writes module.routes (routed), and gives its look (its design.work over Kit Design's) and the scope its config.work
// declared, which every site's root starts from (its language, its icon); nil when it has none. An app without
// module.routes is one site, its root: those two files are its site root's, which bootSite runs.
func (h *Host) bootApp(appRoot, identity string, routed bool, requirements *Requirements, databases *database.Catalog, kept *storage.App, back *background) (*design.Look, *router.Scope, error) {
	look, err := design.KitDesign()
	if err != nil {
		return nil, nil, err
	}
	for _, folder := range []string{".shared", ".schedule", ".database"} {
		if isFile(filepath.Join(appRoot, folder, "design.work")) {
			return nil, nil, fmt.Errorf("%s/%s/design.work: design.work has no role in %s/: a look is declared at the app root, a site's root or a branch (docs/KITWORK_DESIGN.md mục 1)", identity, folder, folder)
		}
	}
	lead := "[" + identity + "] "
	notice := func(text string) { h.log.Led(lead, text) }
	globals, slots := builtinsOf(h.log, lead, time.UTC)
	if err := h.declareAppDatabases(appRoot, globals, slots, notice, requirements, databases, back); err != nil {
		return nil, nil, err
	}
	if !routed {
		return look, nil, nil
	}
	if file := filepath.Join(appRoot, "design.work"); isFile(file) {
		declared := &design.Scope{Notice: notice}
		kitwork := &Kitwork{folder: appRoot, scope: router.NewScope(router.LevelApp, nil, notice), look: declared, file: "design.work", notice: notice, requirements: requirements, background: back}
		if err := h.bootFile(appRoot, file, globals, slots, kitwork); err != nil {
			return nil, nil, err
		}
		if look, err = look.Under(declared); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", writtenIn(appRoot, file), err)
		}
	}
	file := filepath.Join(appRoot, "config.work")
	if !isFile(file) {
		return look, nil, nil
	}
	scope := router.NewScope(router.LevelApp, nil, notice)
	kitwork := &Kitwork{folder: appRoot, scope: scope, file: "config.work", notice: notice, requirements: requirements, databases: databases, storage: kept, background: back}
	if err := h.bootFile(appRoot, file, globals, slots, kitwork); err != nil {
		return nil, nil, err
	}
	return look, scope, nil
}

// bootSite opens the site hostname, the folder name of the app at appRoot ("/"-separated from its root, "" for the root
// itself): its globals, then its folders, the root's look under the app's and its head under what the app's
// config.work declares.
func (h *Host) bootSite(appRoot, name, hostname string, zone *time.Location, look *design.Look, app *router.Scope, requirements *Requirements, databases *database.Catalog, kept *storage.App, back *background, components *component.Definitions) (*Site, error) {
	folder := filepath.Join(appRoot, filepath.FromSlash(name))
	if name != "" {
		// the app's root holds the app's queue and schedules; a folder below it, a site's, does not
		if problem := backgroundInSite(folder); problem != "" {
			return nil, fmt.Errorf("%s: %s", writtenIn(appRoot, folder), problem)
		}
	}
	site := &Site{
		Hostname: hostname,
		Folder:   folder,
		AppRoot:  appRoot,
		Timezone: zone,
		lead:     "[" + hostname + "] ",
		log:      h.log,
		pool:     h.pool,
		storage:  kept,
		app:      app,
		// the app's queue, which a site's file sends to (background.go)
		background: back,
		// the app's components, which its pages draw and its files find by their place (sitecomponent.go)
		components: components,
		// the app's files, which context.file serves (docs/KITWORK_FILE.md mục 9)
		files: &file.File{Root: appRoot},
	}
	// its tenant's limits (the table tenant, over app.limits): its requests at once (gate.go), what one of its runs makes
	// and what it spends
	requests, memory, energy := h.tenantLimits(filepath.Base(appRoot))
	site.gate, site.made, site.energy = newGate(requests, h.siteWait()), memory, energy
	site.static = &Static{folder: site.Folder, local: h.Manifest.Website != nil && h.Manifest.Website.AllowLocal}
	// the page's expressions take from the host's pool, within the site's runs' own ceiling (app.limits)
	site.ui = &ui.Site{Root: site.Folder, App: appRoot, Pool: h.pool, MaxBytes: memory}
	site.globals, site.slots = builtinsOf(h.log, site.lead, zone)
	site.answers = httpmodule.NewCache()
	site.builtins = withKitwork(site.slots, &Kitwork{folder: appRoot, entry: name, site: site})
	root, err := h.walk(site, site.Folder, path.Base("/"+name), "/", true, look, head{}.under(app, "/"), requirements, databases)
	if err != nil {
		return nil, err
	}
	site.root = root
	// /favicon.ico answers with the icon the site's root links (KITWORK_UI mục 5): one source, so a favicon.ico
	// at the root beside a router.favicon(…) that names another file is the boot's error, naming both
	if icon := root.head.favicon; icon != "" && icon != "/favicon.ico" && !strings.Contains(icon, "://") {
		if isFile(filepath.Join(site.Folder, "favicon.ico")) {
			at := root.scope.FaviconAt()
			if at == "" && app != nil {
				at = app.FaviconAt()
			}
			return nil, fmt.Errorf("%s: /favicon.ico has two sources, this file and router.favicon(%q) at %s: one icon, one source; remove the file or the call",
				writtenIn(appRoot, filepath.Join(site.Folder, "favicon.ico")), strings.TrimPrefix(icon, "/"), at)
		}
		site.static.favicon = strings.TrimPrefix(icon, "/")
	}
	// router.reload(true) at the site's root loads each page whole; anything else moves without a reload
	site.drive = root.scope.Reload != "true"
	return site, nil
}

// walk opens one folder of a site and those below it, parent first: its segment, its design.work, its
// config.work then its router.work, its children in the order they are tried. parent is the look of the
// folder above, above its head (head.go).
func (h *Host) walk(site *Site, dir, name, urlPath string, isRoot bool, parent *design.Look, above head, requirements *Requirements, databases *database.Catalog) (*Folder, error) {
	folder := &Folder{Name: name, Path: urlPath}
	entry, err := filepath.Rel(site.AppRoot, dir)
	if err != nil {
		return nil, err
	}
	entry = filepath.ToSlash(entry)
	if entry == "." {
		entry = "" // the app's root, the one site of an app without module.routes
	}
	if !isRoot {
		if err := folder.parseSegment(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry, err)
		}
	}
	level := router.LevelBranch
	if isRoot {
		level = router.LevelSite
	}
	notice := func(text string) { h.log.Led(site.lead, text) }
	folder.scope = router.NewScope(level, nil, notice)
	folder.page = isFile(filepath.Join(dir, "content.ui"))
	folder.look = parent
	if file := filepath.Join(dir, "design.work"); isFile(file) {
		if name == "assets" {
			return nil, fmt.Errorf("%s: design.work has no role in assets/, the site's files: a look is declared at the app root, a site's root or a branch (docs/KITWORK_DESIGN.md mục 1)", writtenIn(site.AppRoot, file))
		}
		declared := &design.Scope{Notice: notice}
		kitwork := &Kitwork{folder: site.AppRoot, entry: entry, scope: router.NewScope(level, nil, notice), look: declared, file: "design.work", notice: notice, requirements: requirements}
		if err := h.bootFile(site.AppRoot, file, site.globals, site.slots, kitwork); err != nil {
			return nil, err
		}
		if folder.look, err = parent.Under(declared); err != nil {
			return nil, fmt.Errorf("%s: %w", writtenIn(site.AppRoot, file), err)
		}
	}
	for _, file := range []string{"config.work", "router.work"} {
		if !isFile(filepath.Join(dir, file)) {
			continue
		}
		kitwork := &Kitwork{folder: site.AppRoot, entry: entry, scope: folder.scope, file: file, notice: notice, requirements: requirements, databases: databases, site: site}
		if err := h.bootFile(site.AppRoot, filepath.Join(dir, file), site.globals, site.slots, kitwork); err != nil {
			return nil, err
		}
	}
	if door, ok := folder.scope.Doors["get"]; ok && door.View && !folder.page {
		return nil, fmt.Errorf("%s: router.view(…) at %s draws the folder's content.ui, which the folder does not have (docs/view.md mục 2)", entry, door.At)
	}
	if !folder.page && len(folder.scope.Doors) > 0 {
		if at := viewWithoutPage(filepath.Join(dir, "router.work")); at != "" {
			return nil, fmt.Errorf("%s: context.view(…) at %s draws the folder's content.ui, which the folder does not have: add a content.ui to the folder, or answer with context.json(…) (docs/KITWORK_ROUTER.md mục 2)", entry, path.Join(entry, at))
		}
	}
	folder.head = above.under(folder.scope, urlPath)
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", entry, err)
	}
	for _, child := range children {
		if !child.IsDir() || child.Type()&fs.ModeSymlink != 0 || hidden(child.Name()) {
			continue
		}
		below, err := h.walk(site, filepath.Join(dir, child.Name()), child.Name(), path.Join(urlPath, child.Name()), false, folder.look, folder.head, requirements, databases)
		if err != nil {
			return nil, err
		}
		below.parent = folder
		folder.children = append(folder.children, below)
	}
	folder.sortChildren()
	return folder, nil
}

// declareDatabases is the pass that declares the app's databases (docs/schema.md mục 2): every plain .work file of the
// app (moduleFiles; 17/11/2026, until then only those of its .shared/schema/) run once, in the order of its path,
// declaring the tables its top level writes into the app's catalog before any other file of the app runs, so a
// config.work, a job or a site importing a file of tables finds them. A file that imports another runs that one's
// declaration too, found by its place when it runs again. A struct written with a word of a table that no tables({ … })
// takes ends the pass. A file nobody imports is read too: the app's files, not the imports, say what its data is.
func (h *Host) declareDatabases(appRoot string, globals map[string]value.Value, slots []value.Value, notice func(string), requirements *Requirements, databases *database.Catalog) error {
	return h.declareAppDatabases(appRoot, globals, slots, notice, requirements, databases, nil)
}

// declareAppDatabases is declareDatabases for an app that boots, its queue back: a schema file that imports a job (a
// hook that sends one) gets the app's handle (background.go).
func (h *Host) declareAppDatabases(appRoot string, globals map[string]value.Value, slots []value.Value, notice func(string), requirements *Requirements, databases *database.Catalog, back *background) error {
	files, err := moduleFiles(appRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(appRoot), err)
	}
	for _, relative := range files {
		file := filepath.Join(appRoot, filepath.FromSlash(relative))
		entry := path.Dir(relative)
		if entry == "." {
			entry = ""
		}
		kitwork := &Kitwork{folder: appRoot, entry: entry, scope: router.NewScope(router.LevelApp, nil, notice),
			file: path.Base(relative), notice: notice, requirements: requirements, databases: databases, declaring: true, background: back}
		if err := h.bootFile(appRoot, file, globals, slots, kitwork); err != nil {
			return err
		}
		// the names the file exports its databases by, which a site's file imports and writes statements on
		if text, err := os.ReadFile(file); err == nil {
			databases.Exports(relative, string(text))
		}
	}
	if err := databases.Declared(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(appRoot), err)
	}
	// a database of the host's catalogue, only for the apps its apps name (grant.go)
	if err := h.grantedDatabases(appRoot, databases); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(appRoot), err)
	}
	// the database is the root: each file is read, read only, and what it lacks of the structs is said once
	for _, report := range databases.Check(appRoot) {
		for _, line := range report.Lines() {
			notice(line)
		}
	}
	return nil
}

// bootFile compiles a file of the app at appRoot and runs it once on a VM of its own, with the builtins
// of its site (globals, slots) and kitwork: the closures it makes (the handlers) are kept, and run later
// on the site's VMs.
func (h *Host) bootFile(appRoot, file string, globals map[string]value.Value, slots []value.Value, kitwork *Kitwork) error {
	written := writtenIn(appRoot, file)
	bytecode, err := compiler.CompileFileInApp(appRoot, file)
	if err != nil {
		return fmt.Errorf("%s: %w", written, err)
	}
	// every statement the program writes on a database of the app is translated before the file runs: a lambda
	// written wrong is the boot's error, where it is written (library/database, statement.go)
	if kitwork.databases != nil && !kitwork.declaring {
		if err := kitwork.databases.Prepare(bytecode.Program, file, bytecode.Files); err != nil {
			return fmt.Errorf("%s: %w", written, err)
		}
	}
	// the doors are read from the file's words, not only from its run (spelling.go)
	if source, err := os.ReadFile(file); err == nil {
		if problems := routerWords(filepath.Base(file), string(source)); problems != "" {
			return fmt.Errorf("%s: %s", written, problems)
		}
		// a guard that can let a request on but by return context.next(), or a door that says it, is the boot's
		// error where it is written, in the file it is imported from too (compiler.GuardProblems)
		if base := filepath.Base(file); base == "router.work" || base == "config.work" {
			if problems := compiler.GuardProblems(appRoot, file, string(source)); len(problems) > 0 {
				return fmt.Errorf("%s", guardReport(appRoot, problems))
			}
		}
	}
	machine := runtime.New(bytecode.Program)
	kitwork.vm = machine
	kitwork.scope.Where = kitwork.where
	if kitwork.look != nil {
		kitwork.look.Where = kitwork.where
	}
	branches := len(kitwork.scope.Branches)
	defined := 0
	if kitwork.components != nil {
		defined = kitwork.components.Len()
	}
	// a site's file spends what the site's runs do (app.limits), a file of the app the host's
	spends := h.runEnergy()
	if kitwork.site != nil {
		spends = kitwork.site.energy
	}
	if err := runFile(machine, bytecode.Program, globals, slots, kitwork, spends); err != nil {
		return fmt.Errorf("%s: %w", written, err)
	}
	if problem := kitwork.scope.Unended(); problem != "" {
		return fmt.Errorf("%s: %s", written, problem)
	}
	// every request of the site runs the file's handlers, guards and components at once and reads what the file
	// made: it is frozen, and the boot VM runs nothing more (runtime Freeze)
	kept := handlers(kitwork.scope, kitwork.scope.Branches[branches:])
	if kitwork.declared != nil && kitwork.declared.Routes != nil {
		kept = append(kept, value.Closure(kitwork.declared.Routes)) // module.routes's, called for each hostname (route)
	}
	if kitwork.components != nil {
		for _, definition := range kitwork.components.Since(defined) {
			kept = append(kept, value.Closure(definition.Function))
		}
	}
	machine.Freeze(kept...)
	return nil
}

// writtenIn is a file of the app at appRoot as an error names it: from the folder of the apps,
// "<identity>/kitwork.io/router.work".
func writtenIn(appRoot, file string) string {
	written, err := filepath.Rel(filepath.Dir(appRoot), file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(written)
}

// handlers are the functions of a folder's doors and the guards before them, and those of the chains a file
// ended with .methods(…), branches, which the host keeps of its files' runs: what they hold is shared by the
// requests as the file's top level is.
func handlers(scope *router.Scope, branches []*router.Branch) []value.Value {
	kept := make([]value.Value, 0, len(scope.Doors))
	guards := func(rules []router.Rule) {
		for _, rule := range rules {
			if rule.Guard != nil {
				kept = append(kept, value.Closure(rule.Guard))
			}
		}
	}
	for _, door := range scope.Doors {
		if door.Handler != nil {
			kept = append(kept, value.Closure(door.Handler))
		}
		guards(door.Rules)
	}
	for _, branch := range branches {
		guards(branch.Rules)
	}
	return kept
}
