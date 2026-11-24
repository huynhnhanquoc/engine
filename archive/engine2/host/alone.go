package host

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/application"
	"github.com/kitwork/engine2/library/database"
)

// The layout of one app (slice 6 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026; docs/module.md mục 7): a folder
// with module.work and no apps/ under a host is an app that runs alone, `cd shop && kitwork`. With no application.work
// beside its module.work it runs on the engine's defaults: the website on port 8080 of this machine alone
// (127.0.0.1), no AutoSSL, the engine's limits, no host database. An application.work beside it is that app's real
// configuration, read as a host's is, except that its folder is the app: app.root has no meaning there. Either way the
// app is the only one, so no table of the host is read (domain, tenant): every hostname is the app's, each folder at its
// root named as one when its module.work writes module.routes, and every database of app.databases is open to it.

// aloneWebsitePort is the port of an app run with no application.work, the engine's default (KITWORK_MODULE_WORK
// câu hỏi 3).
const aloneWebsitePort = 8080

// alone is the host of the app at h.Folder run with no application.work: its manifest the engine's defaults, a website
// on port 8080 of this machine alone, and app.run(), as `cd shop && kitwork` serves it.
func (h *Host) alone() *Host {
	h.withoutManifest = true
	h.Manifest = &application.Manifest{
		Website: &application.Website{Port: aloneWebsitePort, AllowLocal: true},
		Run:     true,
	}
	database.SetHistoryDays(0)
	database.SetHosted(map[string]database.Hosted{})
	database.SetAlone(true) // a connection("name") of the host's says where to declare it (library/database)
	h.log.Line(fmt.Sprintf("[app] no %s in %s: the app of its module.work runs alone, on the engine's defaults: port %d of this machine alone, no AutoSSL; an application.work beside module.work says otherwise",
		filepath.Base(h.File), h.Folder, aloneWebsitePort))
	return h
}

// oneAppCheck is what Check says of the layout of one app: the manifest's folder is the app, so a folder of apps is
// none of its words.
func (h *Host) oneAppCheck() error {
	if h.Manifest.Root != "" {
		return fmt.Errorf("%s: app.root(…): module.work beside %s makes its folder the app, the only one, so it has no folder of apps; take app.root away, or module.work, for apps/<identity>/",
			filepath.Base(h.File), filepath.Base(h.File))
	}
	if website := h.Manifest.Website; website != nil && website.Domains != "" {
		return fmt.Errorf("%s: app.website({ domains }): module.work beside %s makes its folder the app, the only one, so no table gives it its hostnames: every name is its own; take domains away",
			filepath.Base(h.File), filepath.Base(h.File))
	}
	return nil
}

// appFolder is an app the host serves: its identity, the name of its folder, and that folder, its root.
type appFolder struct {
	identity, root string
}

// appFolders are the apps the host serves: each folder of app.root (apps/ beside the manifest) that is no dot or
// underscore folder, or, in the layout of one app, the manifest's folder alone.
func (h *Host) appFolders() ([]appFolder, error) {
	if h.oneApp {
		return []appFolder{{identity: filepath.Base(h.Folder), root: h.Folder}}, nil
	}
	root := h.root()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var apps []appFolder
	for _, entry := range entries {
		if entry.IsDir() && !hidden(entry.Name()) {
			apps = append(apps, appFolder{identity: entry.Name(), root: filepath.Join(root, entry.Name())})
		}
	}
	return apps, nil
}

// oneAppNames are the hostnames of the app of the layout of one app, sorted: with module.routes, each folder at its root
// named as a hostname, public or local, no table to give them; without, its one site is app.website's hostname, or
// localhost when no manifest writes one.
func (h *Host) oneAppNames(appRoot string) []string {
	if !routed(appRoot) {
		if website := h.Manifest.Website; website != nil && website.Hostname != "" {
			return []string{strings.ToLower(website.Hostname)}
		}
		return []string{"localhost"}
	}
	var names []string
	if entries, err := os.ReadDir(appRoot); err == nil {
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			// a folder of one label (core/) is code, not a site; localhost is the machine's own
			if entry.IsDir() && !hidden(entry.Name()) && (strings.Contains(name, ".") || localName(name)) && hostnameProblem(name) == "" {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// onlySite is the one site of sites, for localhost in the layout of one app: the app run alone answers the machine
// itself with its site when it has one. nil when there are several, each then reached by <name>.localhost.
func (h *Host) onlySite(sites map[string]*Site) *Site {
	if !h.oneApp || len(sites) != 1 {
		return nil
	}
	for _, site := range sites {
		return site
	}
	return nil
}

// aloneAddresses is the line Serve adds for an app run alone: where to open each of its sites on this machine.
func (h *Host) aloneAddresses(sites map[string]*Site) string {
	port := strconv.Itoa(h.Manifest.Website.Port)
	if only := h.onlySite(sites); only != nil {
		return "[app] open http://localhost:" + port + "/ (" + only.Hostname + ")"
	}
	var addresses []string
	for _, hostname := range slices.Sorted(maps.Keys(sites)) {
		address := hostname + ".localhost"
		if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
			address = hostname
		}
		addresses = append(addresses, "http://"+address+":"+port+"/")
	}
	return "[app] open " + strings.Join(addresses, ", ")
}
