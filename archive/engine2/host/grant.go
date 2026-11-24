package host

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/database"
)

// A database of the host's catalogue, app.databases({ jobs: { sqlite: "…", apps: ["demo.example"] } }), is opened only
// by the apps its apps name (11/11/2026): each by a hostname of one of its sites or by its folder, or every app for
// apps: "*". None by default: on a host that serves apps of several owners, the catalogue is the host's, and one app
// never reaches another's data unless the manifest says so. An app that declares tables on a name not given to it
// (connection("jobs") in its .shared/schema/) does not boot: its sites answer 503, the others serve; kitwork migrate
// refuses it the same way. connection("…") is written only in .shared/schema/, which every boot reads first, so the
// check of the declarations is the check of every statement.

// grantedDatabases says each name of the host's catalogue the app at appRoot declares is given to it.
func (h *Host) grantedDatabases(appRoot string, databases *database.Catalog) error {
	if h.oneApp {
		return nil // the only app opens every database of its manifest, no apps to name (alone.go)
	}
	for _, entry := range databases.Entries() {
		if entry.Name == "" || h.Manifest == nil {
			continue // the app's own database
		}
		declared := h.Manifest.Databases[entry.Name]
		if declared == nil || h.givenTo(declared.Apps, appRoot) {
			continue // a name the catalogue lacks is library/database's to say
		}
		example := filepath.Base(appRoot)
		if sites := h.siteNames(appRoot); len(sites) > 0 {
			example = sites[0]
		}
		return fmt.Errorf("%s: connection(%q): database %s của host chưa cấp cho app %s; application.work cấp bằng app.databases({ %s: { …, apps: [%s] } }), hay apps: \"*\" cho mọi app",
			entry.Where, entry.Name, entry.Name, filepath.Base(appRoot), entry.Name, strconv.Quote(example))
	}
	return nil
}

// givenTo says apps gives a database to the app at appRoot: "*", its folder, or a hostname of one of its sites.
func (h *Host) givenTo(apps []string, appRoot string) bool {
	if slices.Contains(apps, "*") || slices.Contains(apps, strings.ToLower(filepath.Base(appRoot))) {
		return true
	}
	for _, site := range h.siteNames(appRoot) {
		if slices.Contains(apps, site) {
			return true
		}
	}
	return false
}

// siteNames are the hostnames of the sites of the app at appRoot, its folders that are no dot folder, sorted; on a host
// of a website, only those the table of domains gives the app (domains.go): a folder named after another's hostname is
// no name of this app, and a database given to that hostname is not given to it.
func (h *Host) siteNames(appRoot string) []string {
	return h.siteNamesBy(h.domains.Load(), filepath.Base(appRoot), appRoot)
}

// grantUnknown says each app a database's apps name that the host serves not, neither as a site nor as an app's
// folder: a name mistyped gives the database to no one.
func (h *Host) grantUnknown(sites map[string]*Site) {
	if h.Manifest == nil || h.oneApp {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(h.Manifest.Databases)) {
		for _, app := range h.Manifest.Databases[name].Apps {
			if app == "*" || sites[app] != nil || h.apps[app] != nil {
				continue
			}
			h.log.Line("app.databases " + name + " gives apps " + app + ", which no app serves: the database is given to no app of that name")
		}
	}
}
