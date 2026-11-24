package host

import (
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/kitwork/engine2/library/application"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/schedule"
)

// Map is kitwork check's map of every app the boot read (slice 2c of root docs/KITWORK_MODULE_WORK.md, 17/11/2026):
// for each app, by identity, what its module.work declares, its sites (each hostname and the folder it serves), each
// site's pages with their doors, then its tables, jobs, schedules and the rules of its queue and schedules, each with
// the place that declares it, file:line:column from the app's root, and its components (slice 7, the same day). It is
// read from what the boot made, never kept by hand: the code says where everything is, the map only prints it. An app
// or a site the boot closed says why.
func (h *Host) Map() string {
	var out strings.Builder
	sites := h.current()
	for _, identity := range slices.Sorted(maps.Keys(h.apps)) {
		run := h.apps[identity]
		lines := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
		line := func(cells ...string) { lines.Write([]byte(strings.Join(cells, "\t") + "\n")) }
		line("app", identity, "")
		if run.failed != nil || run.declared == nil {
			for _, hostname := range slices.Sorted(slices.Values(run.hostnames)) {
				line("site", hostname, "503")
			}
			why := "the app did not boot"
			if run.failed != nil {
				why = run.failed.Error()
			}
			line("đóng", "", why)
			lines.Flush()
			out.WriteString("\n")
			continue
		}
		// the app's zone, asked up the tiers (slice 3): where it comes from, module.work, application.work or none
		zone, from := h.zoneOf(run.declared)
		line("múi giờ", zone.String(), from)
		if run.declared.Routes != nil {
			line("routes", "module.routes", run.declared.RoutesAt)
		} else {
			line("routes", "(gốc app là site)", "module.work")
		}
		// the tenant's limits and where each comes from (slice 4): its row of the table tenant, app.limits, the engine
		requests, memory, energy := h.tenantLimits(identity)
		memoryFrom, energyFrom, requestsFrom := h.limitSource(identity)
		line("trần", "memory "+application.SizeText(memory), memoryFrom)
		line("trần", "energy "+strconv.FormatUint(energy, 10), energyFrom)
		line("trần", "requests "+strconv.Itoa(requests), requestsFrom)
		for _, hostname := range slices.Sorted(slices.Values(run.hostnames)) {
			site := sites[hostname]
			switch {
			case site == nil:
			case site.closed != nil:
				line("site", hostname, "503: "+site.closed.Error())
			default:
				line("site", hostname, siteFolder(site))
			}
		}
		for _, site := range sortedSites(run.sites) {
			pages(site.root, func(folder *Folder) {
				methods := slices.Sorted(maps.Keys(folder.scope.Doors))
				for _, method := range methods {
					door := folder.scope.Doors[method]
					word := strings.ToUpper(method)
					if door.View {
						word = "VIEW"
					}
					line("trang", site.Hostname+" "+folder.Path, word+" "+door.At)
				}
				if len(methods) == 0 && folder.page {
					line("trang", site.Hostname+" "+folder.Path, "GET "+path.Join(siteFolder(site), strings.TrimPrefix(folder.Path, "/"), "content.ui"))
				}
			})
		}
		if run.catalog != nil {
			for _, entry := range run.catalog.Entries() {
				for _, table := range entry.Tables {
					line("bảng", table.Name, entryLabel(entry)+" "+entry.Where)
				}
			}
		}
		if run.back != nil {
			for _, job := range run.back.queue.Declared() {
				line("job", job.Name, job.Place)
			}
			for _, each := range run.back.schedules.Declared() {
				zoneFrom := map[string]string{schedule.ZoneOwn: "(.timezone của lịch)", schedule.ZoneInstant: "(giờ của on(…))"}[each.ZoneFrom]
				if each.ZoneFrom == schedule.ZoneApp {
					zoneFrom = "(của app: " + strings.TrimSpace(from) + ")"
				}
				line("lịch", each.Name, each.Mode+" "+each.Rule+" "+each.Zone+" "+each.Place+" "+zoneFrom)
			}
			queueRules, scheduleRules := run.back.queue.Rules(), run.back.schedules.Rules()
			for _, word := range slices.Sorted(maps.Keys(queueRules)) {
				line("luật", "queue."+word, queueRules[word])
			}
			for _, word := range slices.Sorted(maps.Keys(scheduleRules)) {
				line("luật", "schedule."+word, scheduleRules[word])
			}
		}
		// the app's components, each where it is declared (slice 7)
		for _, definition := range run.components.List() {
			line("component", definition.Name, definition.At)
		}
		lines.Flush()
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

// siteFolder is the folder a site serves, from its app's root, "/"-ended; "./" for the root itself.
func siteFolder(site *Site) string {
	folder, err := filepath.Rel(site.AppRoot, site.Folder)
	if err != nil || folder == "." {
		return "./"
	}
	return filepath.ToSlash(folder) + "/"
}

// sortedSites are sites by hostname.
func sortedSites(sites []*Site) []*Site {
	out := slices.Clone(sites)
	slices.SortFunc(out, func(a, b *Site) int { return strings.Compare(a.Hostname, b.Hostname) })
	return out
}

// pages calls each with every folder of the tree from root, parent first, in the order their requests try them.
func pages(root *Folder, each func(*Folder)) {
	if root == nil {
		return
	}
	each(root)
	for _, child := range root.children {
		pages(child, each)
	}
}

// entryLabel is a database as the map names it: connection(), connection("jobs").schema("billing").
func entryLabel(entry *database.Entry) string {
	out := "connection()"
	if entry.Name != "" {
		out = `connection("` + entry.Name + `")`
	}
	if entry.Schema != "" {
		out += `.schema("` + entry.Schema + `")`
	}
	return out
}
