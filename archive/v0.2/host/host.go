// Package host is what runs Kitwork's apps: Read runs application.work and gives the Host it declares,
// Boot opens every site of its apps, Serve answers their requests (engine2/run.go). Each site runs its
// own code on its own VMs with its own console and clock; nothing is global. The language is core/, and
// each "kitwork/<word>" a file imports is library/<word>.
package host

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	// the zones of module.timezone travel in the binary: a host without a zone database (a bare Windows,
	// a scratch container) reads "Asia/Ho_Chi_Minh" as every other host does
	_ "time/tzdata"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/application"
	"github.com/kitwork/engine2/library/database"
)

// Host is what runs the apps: the manifest it read and the sites Boot opened, by hostname. Sites is built
// once by Boot and never written after it, so requests read it without a lock; a reload (reload.go) publishes a
// map of its own, never written after either, which requests read from then on.
type Host struct {
	// File is the manifest, Folder its folder: app.root and file.env read from there.
	File   string
	Folder string
	// Manifest is what application.work declared.
	Manifest *application.Manifest
	// RequestTimeout bounds the run of one request, ReadTimeout the reading of one (its headers and its
	// body); zero is the default of each, 30 seconds (serve.go).
	RequestTimeout time.Duration
	ReadTimeout    time.Duration
	// StreamLimit is how many streams of context.sse one app holds open, StreamHeartbeat how often an open one
	// writes a comment, StreamWriteTimeout how long one write may wait for the browser to read; zero is the
	// default of each, 1,000, 15 seconds, 15 seconds (stream.go).
	StreamLimit        int
	StreamHeartbeat    time.Duration
	StreamWriteTimeout time.Duration
	// CertificateAuthority is the ACME directory AutoSSL asks for certificates (certificates.go): Let's
	// Encrypt's when "". Its staging directory for a trial, a server of the test's for a test.
	CertificateAuthority string
	// ReloadEvery is how often app.hotReload(true) reads the apps' files; zero is the default, half a second
	// (reload.go).
	ReloadEvery time.Duration
	// StaticCheck is how often a file a prewarmed site holds in memory is checked against the disk; zero is the
	// default, a second (prewarm.go).
	StaticCheck time.Duration
	// SiteRequests is how many requests one site runs at once, SiteWait how long a request past them waits for a place
	// before its 503 (gate.go); zero is the default of each, twice the processors (at least 8), a second. A site runs
	// as many routines of go() at once.
	SiteRequests int
	SiteWait     time.Duration
	// HostBytes is what every run of the host makes together at most (runtime.Pool), counted as each run's own
	// ceiling counts; zero is none. A run the host cannot give to stops, and its request is answered 503. The runs of
	// a file at boot are not in it, each within its own ceiling.
	HostBytes uint64
	pool      *runtime.Pool
	// RunBytes is what one run makes at most: a request, its guards and its door together, a job, a routine, a file
	// at boot (core/runtime, memory.go); zero is the default, 64 MiB. app.limits(…) sets it, SiteRequests and HostBytes
	// for every site, and a site's own over them (Manifest.Limits).
	RunBytes uint64
	// RunEnergy is what one run spends at most, as RunBytes is what it makes (core/runtime, work.go): its instructions
	// and what the engine's code does for it, counted by its work; zero is the default, 10,000,000 units, some 44 ms of
	// the engine's work on the machine measured (15/11/2026). app.limits({ energy }) sets it for every site, a site's own
	// over it (Manifest.Limits); a job and a schedule, which are an app's, spend the host's.
	RunEnergy uint64
	// failures are the errors of the apps Boot closed (Failed).
	failures []error
	log      *Log
	// live is the map of sites requests read: Boot's, then each reload's (reload.go).
	live atomic.Pointer[map[string]*Site]
	// domains is the website's table of domains as last read, nil when there is none (domains.go); domainsAt is the file
	// it is read from, declared or the default, "" for a host of no website; domainsMarks are that file and its -wal as
	// they were before the last read, which each beat of the watcher compares (reloading guards them once Boot ends)
	domains      atomic.Pointer[domainTable]
	domainsAt    string
	domainsMarks map[string]mark
	// tenants is the host's table tenant as last read from the same file, each tenant's own limits, nil for none
	// (tenants.go)
	tenants atomic.Pointer[tenantTable]
	// apps are the apps as they run, by identity: what each boot made (the catalog of its databases, its queue and
	// schedules, which Serve starts and Close stops) and what a reload carries; retired the boots a reload put
	// aside, let go after their last request. reloading guards them, and serving, closing, watchStop, watchDone.
	apps      map[string]*appRun
	retired   []*appRun
	pending   map[string]map[string]mark
	reloading sync.Mutex
	serving   bool
	closing   bool
	watchStop chan struct{}
	watchDone chan struct{}
	// limit is the host's ceilings of requests, app.website's rateLimit (ratelimit.go); nil for none
	limit *hostLimit
	// oneApp is the layout of one app (slice 6 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026): module.work beside the
	// manifest, so the manifest's folder is the app, the only one, and no table of the host is read (alone.go).
	// withoutManifest is that app run with no application.work at all, on the engine's defaults.
	oneApp, withoutManifest bool
	// logOnce opens the files of app.logger once (openLog), logErr is why it could not
	logOnce sync.Once
	logErr  error
	// port is app.database's port once started (port.go)
	port     *databasePort
	portErr  error
	portOnce sync.Once
	// StopGrace is how long the requests going on get once Stop is called; zero is the default, 10 seconds. stop is
	// closed by Stop, made once by stopInit, closed once by stopOnce (shutdown.go).
	StopGrace time.Duration
	stop      chan struct{}
	stopInit  sync.Once
	stopOnce  sync.Once
}

// Close closes every database file the apps' statements opened: KitDB's writer of each, so another process
// (kitwork migrate, another server) may write it.
func (h *Host) Close() error {
	var failures []error
	h.stopWatching() // no reload begins while the host closes; one in progress ends first
	h.reloading.Lock()
	defer h.reloading.Unlock()
	h.closing = true
	// the queues and the schedules first: an attempt that ends as they stop writes its outcome while the files are open
	for _, run := range h.apps {
		if run.back != nil {
			run.back.stop()
		}
	}
	// the boots a reload put aside let their routines and catalog go now: when Close returns, no file is held
	for _, run := range h.retired {
		run.end()
	}
	// what each app keeps across its boots: the streams of context.sse close, the browser connects again (stream.go)
	for _, run := range h.apps {
		run.streams.close()
	}
	for _, site := range h.current() {
		site.closeRoutines() // the routines of go() stop with the host (routine.go)
	}
	for _, run := range h.apps {
		if run.kept != nil {
			run.kept.Close() // the writer of its datastore() lets the file go, an app that did not boot's too
		}
	}
	if h.port != nil {
		failures = append(failures, h.port.close())
	}
	for _, run := range h.apps {
		if run.catalog == nil {
			continue
		}
		if err := run.catalog.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	failures = append(failures, h.log.close()) // the log's files last: a line of the closing is in them
	return errors.Join(failures...)
}

// openLog opens the files of app.logger, once, as the host starts: Boot (a server, a check), ServeDatabase. A read of the
// manifest alone (a test, a migrate's question) writes no file.
func (h *Host) openLog() error {
	h.logOnce.Do(func() {
		if err := h.log.open(h.Manifest.Logger); err != nil {
			h.logErr = fmt.Errorf("%s: %w", filepath.Base(h.File), err)
		}
	})
	return h.logErr
}

// Read runs the manifest at file and gives the Host it declares; its lines go to standard output.
func Read(file string) (*Host, error) {
	return ReadWithLog(file, os.Stdout)
}

// ReadWithLog is Read writing every line of the host (the boot's, each site's console, the failures of
// requests) to log.
func ReadWithLog(file string, log io.Writer) (*Host, error) {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	h := &Host{
		File:     absolute,
		Folder:   filepath.Dir(absolute),
		Manifest: &application.Manifest{},
		log:      newLog(log),
		apps:     map[string]*appRun{},
		pending:  map[string]map[string]mark{},
	}
	// module.work beside the manifest: the layout of one app; and with no manifest, that app alone (alone.go)
	h.oneApp = isFile(filepath.Join(h.Folder, "module.work"))
	database.SetAlone(false)
	if h.oneApp && !isFile(absolute) {
		return h.alone(), nil
	}
	bytecode, err := compiler.CompileFile(absolute)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(absolute), err)
	}
	notice := func(text string) { h.log.Led("[app] ", text) }
	// the manifest's console writes to the host's log; its Date is the shared one, in the host's zone
	globals, slots := builtinsOf(h.log, "[app] ", nil)
	machine := runtime.New(bytecode.Program)
	// env.require of a key no file sets is recorded, and the manifest's read fails at its end naming every
	// such key at once, as a site's boot does (docs/KITWORK_ENVIRONMENT.md mục 3; 05/11/2026: the
	// manifest stopped at its first)
	requirements := &Requirements{}
	kitwork := &Kitwork{manifest: h.Manifest, folder: h.Folder, vm: machine, notice: notice, requirements: requirements}
	// the failure's text begins with application.work:line:column, where it stopped; a key missing is
	// named first, since a run that went on without it may have stopped on its null
	ran := runFile(machine, bytecode.Program, globals, slots, kitwork, energy)
	if err := requirements.Close(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(absolute), err)
	}
	if ran != nil {
		return nil, ran
	}
	// app.website's rateLimit: the host's ceilings of requests (the files of app.logger open as the host starts, openLog)
	if h.Manifest.Website != nil {
		h.limit = newHostLimit(h.Manifest.Website.RateLimit)
	}
	// app.limits({ memory, energy, requests, host }): the tenants' limits for every site (a site's own are read as it
	// boots)
	if limits := h.Manifest.Limits; limits != nil {
		h.RunBytes, h.RunEnergy, h.SiteRequests, h.HostBytes = limits.Memory, limits.Energy, limits.Requests, limits.Host
	}
	// app.history({ days }) is how much history every writer of the process keeps (library/database, writers.go)
	days := 0
	if h.Manifest.History != nil {
		days = h.Manifest.History.Days
	}
	database.SetHistoryDays(days)
	// app.databases is the catalogue every app's connection("name") reads (library/database, catalog.go)
	hosted := map[string]database.Hosted{}
	for name, entry := range h.Manifest.Databases {
		hosted[name] = database.Hosted{Connector: entry.Connector, Path: entry.Path}
	}
	database.SetHosted(hosted)
	return h, nil
}

// runFile runs a file's program once on machine with the builtins of its site (globals by name, slots
// by slot) and kitwork as BUILTIN 0: a manifest, a config.work, a router.work at boot, within spends of energy (the
// host's RunEnergy; the default for the manifest, read before it). A failure is the error, "CODE:
// file:line:column: text".
func runFile(machine *runtime.VM, program *runtime.Program, globals map[string]value.Value, slots []value.Value, kitwork *Kitwork, spends uint64) error {
	machine.FastReset(program, globals)
	machine.Builtins = withKitwork(slots, kitwork)
	machine.MaxEnergy = spends
	machine.MaxBytes = madeBytes
	if kitwork.site != nil {
		machine.MaxBytes = kitwork.site.made // a site's file: the site's own (app.limits)
	}
	kitwork.running = true
	result := machine.Run()
	kitwork.running = false
	if result.IsInvalid() {
		return fmt.Errorf("%s", result.Failure())
	}
	return nil
}

// root is the folder of the apps: app.root, or apps/ beside the manifest when it says none.
func (h *Host) root() string {
	if h.Manifest.Root != "" {
		return h.Manifest.Root
	}
	return filepath.Join(h.Folder, "apps")
}

// Check says what the manifest declares cannot run, reading only: no network, no site's code. A
// manifest declares at least one surface; a website needs the folder of its apps.
func (h *Host) Check() error {
	manifest := h.Manifest
	if manifest.Website == nil && manifest.Database == nil && len(manifest.Surfaces) == 0 {
		return fmt.Errorf("%s declares no surface: app.website(…), app.desktop(…), app.mobile(…) or app.database(…) says what the app is", filepath.Base(h.File))
	}
	if manifest.Run && manifest.Website == nil && manifest.Database == nil {
		return fmt.Errorf("%s runs nothing engine2 opens: app.run() opens app.website(…) and app.database(…)", filepath.Base(h.File))
	}
	if manifest.Website == nil {
		return nil
	}
	if manifest.Run && manifest.Website.Port == 0 {
		return fmt.Errorf("%s: app.website(…) has no port, which app.run() opens it on: app.website({ hostname: %q, port: 8080 })", filepath.Base(h.File), manifest.Website.Hostname)
	}
	if h.oneApp {
		return h.oneAppCheck() // the manifest's folder is the app: no folder of apps (alone.go)
	}
	info, err := os.Stat(h.root())
	if err != nil || !info.IsDir() {
		return fmt.Errorf("app.root: %s is not a folder; the website serves the sites of apps/<identity>/<domain>/ in it", h.root())
	}
	return nil
}
