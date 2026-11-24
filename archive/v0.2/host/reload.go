package host

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/module"
	"github.com/kitwork/engine2/library/storage"
)

// Hot reload (app.hotReload(true), docs/KITWORK_BOOT.md mục 4): while the host serves, it reads the files of every app
// every ReloadEvery, and an app whose files changed is booted again, whole, as a start of the host boots it. The new
// boot is published once it is made, one pointer stored (Host.live): a request reads the map of sites once, so it runs
// on the old boot or the new, never half of each, and the requests the old boot answers end as they began. A boot
// that fails closes the app, its sites answering 503 with the error in the log, as at a start; the next change boots
// it again. An app is what reloads, not a file or a site: its sites share its config.work, its .shared/, its .env.
//
// What an app keeps across its boots is what a start of the process would not lose either way within one process:
// its storage (memory(), the writer of datastore()), whose hub the open streams of context.sse listen on, and the
// tabs of its live pages. What its code makes is made again: the routes, the components, the catalog of its
// databases, the jobs and schedules, whose worker stops before the next boot's starts. The old boot's routines and
// catalog are let go once its last request has had its time (RequestTimeout).
//
// The manifest and the host's .env are read once, as the host starts: a change says so in the log. The table of
// domains is read again at each change, with hot reload or without (domains.go): the apps it opens other sites of boot
// again here, in one publish.

// defaultReloadEvery is how often the files are read when ReloadEvery says nothing. A change boots its app once two
// reads in a row agree on it: half a second to a second after a save.
const defaultReloadEvery = 500 * time.Millisecond

// appRun is one app as it runs: what its boot made, what a reload carries to the next, and the files it was read from.
type appRun struct {
	identity, root string
	// hostnames are the sites the boot put in the map: opened, or answering 503 when the boot failed
	hostnames []string
	sites     []*Site
	// what the boot made: the catalog of its databases, its queue and schedules (nil when it failed)
	catalog *database.Catalog
	back    *background
	// declared is what its module.work declares (module.go), which kitwork check's map says (map.go); components are
	// the components its files declare (slice 7)
	declared   *module.Declared
	components *component.Definitions
	// failed is why the boot closed the app, nil when it booted
	failed error
	// what a reload carries
	kept    *storage.App
	streams *appStreams
	tabs    *appTabs
	// marks are its files as they were read before the boot
	marks map[string]mark
	// ended lets the boot's routines and catalog go, once
	ended sync.Once
}

// mark is what a read of a file notes: a change of either is a change.
type mark struct {
	size     int64
	modified int64
}

// skipped are the folders a reload never reads: what the app writes as it runs (.database/), and what no app runs
// from.
var skipped = map[string]bool{".database": true, ".git": true, "node_modules": true}

// marksOf is every file of the app at root, by its path from root.
func marksOf(root string) map[string]mark {
	marks := map[string]mark{}
	filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // a file gone between the listing and its read is read at the next turn
		}
		if entry.IsDir() {
			if file != root && skipped[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relative, _ := filepath.Rel(root, file)
		marks[filepath.ToSlash(relative)] = mark{size: info.Size(), modified: info.ModTime().UnixNano()}
		return nil
	})
	return marks
}

// changed is a file whose mark differs between before and now, "" when none does: the first by name, for the log.
func changed(before, now map[string]mark) string {
	var names []string
	for name, was := range before {
		if is, ok := now[name]; !ok || is != was {
			names = append(names, name)
		}
	}
	for name := range now {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	if len(names) == 1 {
		return names[0]
	}
	return names[0] + " and " + strconv.Itoa(len(names)-1) + " more"
}

// current is the map of sites a request reads: the one the last boot or reload published; nil before Boot.
func (h *Host) current() map[string]*Site {
	if sites := h.live.Load(); sites != nil {
		return *sites
	}
	return nil
}

// Sites are the sites the host serves by hostname, as the last boot or reload published them: nil before Boot, none
// for a host of no website. The host keeps no other map of them (15/11/2026: a field kept the first boot's map for as
// long as the host ran, so no site a reload put aside was ever let go, and a database's app was looked up in it).
func (h *Host) Sites() map[string]*Site {
	return h.current()
}

// reloadEvery is ReloadEvery, or its default.
func (h *Host) reloadEvery() time.Duration {
	if h.ReloadEvery > 0 {
		return h.ReloadEvery
	}
	return defaultReloadEvery
}

// StartWatching looks every ReloadEvery until Close, when the manifest declares a website: at the table of domains,
// read again when it changes (domains.go), and with app.hotReload(true) at the apps' files. Serve starts it, once the
// sites listen; a test serving through httptest starts it itself. It starts once, however often it is called.
func (h *Host) StartWatching() {
	if h.Manifest.Website == nil || !h.Manifest.HotReload && h.domainsAt == "" {
		return
	}
	h.reloading.Lock()
	defer h.reloading.Unlock()
	if h.watchStop != nil || h.closing {
		return
	}
	h.watchStop, h.watchDone = make(chan struct{}), make(chan struct{})
	manifest := marksOfFiles(h.File, filepath.Join(h.Folder, ".env"))
	if h.Manifest.HotReload {
		h.log.Line("[app] hot reload: an app whose files change boots again (every " + h.reloadEvery().String() + ")")
	}
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		ticker := time.NewTicker(h.reloadEvery())
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				h.rereadDomains() // a host of no table looks for one made
				if h.Manifest.HotReload {
					h.reloadChanged(&manifest)
				}
			}
		}
	}(h.watchStop, h.watchDone)
}

// stopWatching ends the reading of the files and waits for a reload in progress.
func (h *Host) stopWatching() {
	h.reloading.Lock()
	stop, done := h.watchStop, h.watchDone
	h.watchStop = nil
	h.reloading.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

// marksOfFiles is the marks of the files named, by their path.
func marksOfFiles(files ...string) map[string]mark {
	marks := map[string]mark{}
	for _, file := range files {
		if info, err := os.Stat(file); err == nil {
			marks[file] = mark{size: info.Size(), modified: info.ModTime().UnixNano()}
		}
	}
	return marks
}

// reloadChanged reads every app's files once: an app whose files changed boots again, a new app folder boots, an app
// folder gone closes. manifest are the marks of the manifest and its .env, which only the log is told of.
func (h *Host) reloadChanged(manifest *map[string]mark) {
	h.reloading.Lock()
	defer h.reloading.Unlock()
	defer h.survive("[app] ", "hot reload") // the next look reads the files again (panic.go)
	if now := marksOfFiles(h.File, filepath.Join(h.Folder, ".env")); changed(*manifest, now) != "" {
		*manifest = now
		h.log.Line("[app] " + filepath.Base(h.File) + " or its .env changed: the host reads them once, as it starts; start it again to apply them")
	}
	apps, err := h.appFolders()
	if err != nil {
		return
	}
	present := map[string]bool{}
	for _, each := range apps {
		identity, appRoot := each.identity, each.root
		present[identity] = true
		marks := marksOf(appRoot)
		run := h.apps[identity]
		why := "a new app"
		if run != nil {
			file := changed(run.marks, marks)
			if file == "" {
				delete(h.pending, identity)
				continue
			}
			why = file + " changed"
		}
		// the app boots once two reads agree: a file being written is never read half, nor booted twice
		if seen, ok := h.pending[identity]; !ok || changed(seen, marks) != "" {
			h.pending[identity] = marks
			continue
		}
		h.reloadApps(reboot{old: run, identity: identity, root: appRoot, marks: marks, why: why}) // the next read finds no change and forgets the pending
	}
	for identity, run := range h.apps {
		if !present[identity] {
			delete(h.pending, identity)
			h.removeApp(run)
		}
	}
}

// reboot is an app reloadApps boots again: old its boot that runs (nil for an app that was not there), root its folder,
// marks its files as read before the boot, why what the log is told.
type reboot struct {
	old            *appRun
	identity, root string
	marks          map[string]mark
	why            string
}

// reloadApps boots each app of reboots again and publishes the new boots at once: their sites in place of the old
// boots' in the map every request reads. Every old boot's sites leave the map before any app boots, so a hostname one
// app gives up and another takes (the table of domains moved it, domains.go) is never in two apps, whatever their order.
func (h *Host) reloadApps(reboots ...reboot) {
	sites := map[string]*Site{}
	for hostname, site := range h.current() {
		sites[hostname] = site
	}
	for _, each := range reboots {
		if each.old == nil {
			continue
		}
		if each.old.back != nil {
			each.old.back.pause() // its worker ends before the next boot's claims the same runs; a send still stores its run
		}
		for _, hostname := range each.old.hostnames {
			delete(sites, hostname)
		}
	}
	nexts := make([]*appRun, len(reboots))
	for i, each := range reboots {
		began := time.Now()
		next := &appRun{identity: each.identity, root: each.root, marks: each.marks}
		if each.old != nil {
			next.kept, next.streams, next.tabs = each.old.kept, each.old.streams, each.old.tabs
		}
		opened, err := h.bootWholeApp(next, sites)
		lead := "[" + each.identity + "] "
		if err != nil {
			next.hostnames = h.closedSites(each.root, each.identity, err, sites)
			next.failed = err
			h.log.Error(lead, "reload after "+each.why+": the app is closed, its sites answer 503: "+err.Error())
		} else {
			for hostname, site := range opened {
				sites[hostname] = site
				next.hostnames = append(next.hostnames, hostname)
				next.sites = append(next.sites, site)
			}
			if h.serving && next.back != nil {
				next.back.start()
			}
			h.log.Led(lead, "reloaded after "+each.why+" ("+time.Since(began).Round(time.Millisecond).String()+")")
		}
		nexts[i] = next
	}
	h.live.Store(&sites)
	for i, each := range reboots {
		h.apps[each.identity] = nexts[i]
		if each.old != nil {
			h.retire(each.old, nexts[i])
		}
	}
}

// removeApp takes away an app whose folder is gone: its sites leave the map, its work stops, what it kept closes.
func (h *Host) removeApp(run *appRun) {
	sites := map[string]*Site{}
	for hostname, site := range h.current() {
		sites[hostname] = site
	}
	for _, hostname := range run.hostnames {
		delete(sites, hostname)
	}
	h.live.Store(&sites)
	delete(h.apps, run.identity)
	if run.back != nil {
		run.back.stop()
	}
	h.log.Led("["+run.identity+"] ", "the app's folder is gone: its sites are no more")
	run.streams.close()
	if run.kept != nil {
		run.kept.Close()
	}
	h.retire(run, nil)
}

// retire lets the routines, the catalog and the files of the paused work of a boot no request reads any more go, once
// each request it may still answer has had its time (RequestTimeout); Close lets them go at once. Then the host lets
// the boot go, its sites, their programs and their VMs with it (15/11/2026: retired kept every boot a reload put
// aside for as long as the host ran, so each reload held the memory of a whole app), and next, the boot that took its
// place, reads the runs: one a request of the old boot sent as it ended is not lost.
func (h *Host) retire(run, next *appRun) {
	h.retired = append(h.retired, run)
	time.AfterFunc(h.requestTimeout(), func() {
		run.end()
		h.reloading.Lock()
		h.retired = slices.DeleteFunc(h.retired, func(retired *appRun) bool { return retired == run })
		h.reloading.Unlock()
		if next != nil && next.back != nil {
			next.back.wake()
		}
	})
}

// end lets the boot's routines, the catalog of its databases and the files of its paused work go, once.
func (run *appRun) end() {
	run.ended.Do(func() {
		for _, site := range run.sites {
			site.closeRoutines()
		}
		if run.catalog != nil {
			run.catalog.Close()
		}
		if run.back != nil {
			run.back.closeFiles()
		}
	})
}
