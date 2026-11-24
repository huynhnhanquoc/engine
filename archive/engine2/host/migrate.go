package host

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/library/database"
)

// Migrations are kitwork migrate's plans of every app's databases (docs/schema.md mục 8, slice 2), read only: each
// app's .shared/schema/ declared as its boot declares it (declareDatabases), and each file it names held against it.
// Nothing of the sites runs, and nothing is written.
func (h *Host) Migrations() ([]*database.Plan, error) {
	if h.Manifest.Website == nil {
		return nil, nil
	}
	if !h.oneApp { // a database given by hostname is given by the table's (domains.go); one app reads none (alone.go)
		if err := h.loadDomains(); err != nil {
			return nil, err
		}
	}
	apps, err := h.appFolders()
	if err != nil {
		return nil, err
	}
	var plans []*database.Plan
	for _, each := range apps {
		appRoot, identity := each.root, each.identity
		lead := "[" + identity + "] "
		notice := func(text string) { h.log.Led(lead, text) }
		globals, slots := builtinsOf(h.log, lead, time.UTC)
		catalog := database.NewCatalog(appRoot)
		if err := oldFolders(appRoot, identity); err != nil {
			return nil, err // a table of .shared/schema/ would be left out of the plan, unread
		}
		if err := h.declareDatabases(appRoot, globals, slots, notice, &Requirements{}, catalog); err != nil {
			return nil, err
		}
		planned, err := catalog.Plans(appRoot, identity)
		if err != nil {
			return nil, err
		}
		for _, plan := range planned {
			h.routeThroughPort(plan, appRoot)
		}
		plans = append(plans, planned...)
	}
	return plans, nil
}

// routeThroughPort gives a plan the way to the port application.work declares (app.database), for when a server holds
// the file: the port names a folder of the host's .database/ by its name (a name of app.databases there), an app's
// database <domain>/<folder>, a domain of the app. A file the port does not serve (a name of app.databases elsewhere),
// and a schema's own file (billing.db), whose statements would name billing.<table>, which the plan does not write,
// are not routed.
func (h *Host) routeThroughPort(plan *database.Plan, appRoot string) {
	config := h.Manifest.Database
	if config == nil || filepath.Base(plan.Path) != "kitwork.db" {
		return
	}
	same := func(left, right string) bool {
		a, errA := filepath.Abs(left)
		b, errB := filepath.Abs(right)
		return errA == nil && errB == nil && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	folder := filepath.Dir(plan.Path)
	name := ""
	switch cluster := filepath.Dir(folder); {
	case same(cluster, config.Path):
		name = strings.ToLower(filepath.Base(folder))
	case same(cluster, filepath.Join(appRoot, ".database")):
		// the port names an app's database by a hostname of the app, the first the host gives it (17/11/2026: the first
		// folder named like a hostname, so core/ was one)
		if names := h.siteNamesBy(h.domains.Load(), filepath.Base(appRoot), appRoot); len(names) > 0 {
			name = names[0] + "/" + strings.ToLower(filepath.Base(folder))
		}
	}
	if name == "" {
		return
	}
	host := config.Host
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	address := net.JoinHostPort(host, strconv.Itoa(config.Port))
	user, password := config.User, config.Password
	plan.ThroughPort(fmt.Sprintf("cổng database %s (database %s)", address, name), func(ctx context.Context, statements []string) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		client, err := dialPort(ctx, address, user, password, name)
		if err != nil {
			return err
		}
		defer client.close()
		// a schema statement through the port commits by itself: a failure leaves the statements before it done
		for _, statement := range statements {
			if err := client.exec(statement); err != nil {
				first, _, _ := strings.Cut(statement, "\n")
				return fmt.Errorf("%s: %w", first, err)
			}
		}
		return nil
	})
}
