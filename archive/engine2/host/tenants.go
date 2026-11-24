package host

import (
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/application"
)

// The host's table tenant (17/11/2026, slice 4 of root docs/KITWORK_MODULE_WORK.md mục 6, 9): a tenant's own limits,
// one row by its identity, in the host's system database beside the table domain (the website's domains file, by
// default .database/system/kitwork.db). The host writes it; a tenant never does, nor declares its own limits: the
// limits are a ceiling only the host sets. A row gives the app of its identity what one of its runs may make (memory),
// spend (energy) and how many requests each of its sites runs at once (requests), its sites, jobs and schedules alike;
// a column left empty keeps app.limits({ … }) of the manifest, every tenant's default, and those the engine's. Until
// then app.limits("site", { … }) set one site's in the manifest. A row only applies when its status is active; a row
// written wrong, or of another status, is said once and applies nothing: the tenant runs on the defaults. A file or a
// table not there is no row: every tenant on the defaults. The table is read with the table domain, at boot and on
// each beat its file changes (rereadDomains), each app whose limits change booted again.

// tenantTableShape is the table the host reads.
const tenantTableShape = `CREATE TABLE tenant (identity TEXT PRIMARY KEY, status TEXT DEFAULT 'active', memory TEXT, energy INTEGER, requests INTEGER)`

// tenantRow is a row of the table: the limits it gives, and what keeps it from applying ("" when it applies).
type tenantRow struct {
	limit application.TenantLimit
	why   string
}

// tenantTable is the table as last read, by identity; nil when there is none.
type tenantTable struct {
	rows map[string]tenantRow
}

// readTenants reads the table tenant of file: nil and no error when the file or the table is not there.
func readTenants(file string) (*tenantTable, error) {
	if file == "" {
		return nil, nil
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return nil, nil
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("the table tenant: %s: %w", file, err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT COALESCE(identity, ''), COALESCE(status, ''), COALESCE(CAST(memory AS TEXT), ''), COALESCE(energy, 0), COALESCE(requests, 0) FROM tenant`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, fmt.Errorf("the table tenant: %s holds no table tenant as the host reads it, %s (%v)", file, tenantTableShape, err)
	}
	defer rows.Close()
	table := &tenantTable{rows: map[string]tenantRow{}}
	for rows.Next() {
		var identity, status, memory string
		var energy, requests int64
		if err := rows.Scan(&identity, &status, &memory, &energy, &requests); err != nil {
			return nil, fmt.Errorf("the table tenant: %s: %w", file, err)
		}
		identity = strings.TrimSpace(identity)
		if identity == "" {
			continue
		}
		row := tenantRow{}
		row.limit, row.why = application.TenantLimits(memory, energy, requests)
		if row.why == "" {
			if status = strings.ToLower(strings.TrimSpace(status)); status != "active" && status != "" {
				row.why = "its status is " + strconv.Quote(status) + ", not \"active\""
			}
		}
		table.rows[identity] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("the table tenant: %s: %w", file, err)
	}
	return table, nil
}

// limitOf is the row of identity that applies, and whether there is one.
func (t *tenantTable) limitOf(identity string) (application.TenantLimit, bool) {
	if t == nil {
		return application.TenantLimit{}, false
	}
	row, ok := t.rows[identity]
	return row.limit, ok && row.why == ""
}

// said is a line for each row that applies nothing, and for each row of an identity no app under the apps has.
func (t *tenantTable) said(apps map[string]*appRun) []string {
	if t == nil {
		return nil
	}
	var lines []string
	for _, identity := range slices.Sorted(maps.Keys(t.rows)) {
		row := t.rows[identity]
		switch {
		case row.why != "":
			lines = append(lines, "tenant "+identity+": "+row.why+": its row applies nothing, the app runs on the defaults")
		case apps != nil && apps[identity] == nil:
			lines = append(lines, "tenant "+identity+": no app of that identity is under the apps: its limits are no app's")
		}
	}
	return lines
}

// loadTenants reads the table tenant from the website's domains file, as loadDomains reads the table domain there.
func (h *Host) loadTenants() error {
	table, err := readTenants(h.domainsAt)
	if err != nil {
		return err
	}
	h.tenants.Store(table)
	return nil
}

// tenantLimits are the limits of the app identity: its row of the table tenant, over app.limits({ … }), over the
// engine's defaults: how many requests one of its sites runs at once, what one of its runs makes and what one spends.
func (h *Host) tenantLimits(identity string) (int, uint64, uint64) {
	requests, memory, energy := h.siteRequests(), h.runBytes(), h.runEnergy()
	if own, ok := h.tenants.Load().limitOf(identity); ok {
		if own.Requests > 0 {
			requests = own.Requests
		}
		if own.Memory > 0 {
			memory = own.Memory
		}
		if own.Energy > 0 {
			energy = own.Energy
		}
	}
	return requests, memory, energy
}

// limitSource is where each of the app identity's limits comes from, as kitwork check's map says it: its row of the
// table tenant, the manifest's app.limits, or the engine.
func (h *Host) limitSource(identity string) (memory, energy, requests string) {
	own, ok := h.tenants.Load().limitOf(identity)
	source := func(mine bool, manifest bool) string {
		switch {
		case ok && mine:
			return "bảng tenant"
		case manifest:
			return "app.limits, mặc định của host"
		}
		return "mặc định của engine"
	}
	limits := h.Manifest.Limits
	return source(own.Memory > 0, limits != nil && limits.Memory > 0 || h.RunBytes > 0),
		source(own.Energy > 0, limits != nil && limits.Energy > 0 || h.RunEnergy > 0),
		source(own.Requests > 0, limits != nil && limits.Requests > 0 || h.SiteRequests > 0)
}

// tenantsChanged are the identities whose limits table gives otherwise than old: a row added, gone, or other.
func (t *tenantTable) changedFrom(old *tenantTable) []string {
	changed := map[string]bool{}
	var mine, before map[string]tenantRow
	if t != nil {
		mine = t.rows
	}
	if old != nil {
		before = old.rows
	}
	for identity, row := range mine {
		if was, ok := before[identity]; !ok || was != row {
			changed[identity] = true
		}
	}
	for identity := range before {
		if _, ok := mine[identity]; !ok {
			changed[identity] = true
		}
	}
	return slices.Sorted(maps.Keys(changed))
}
