package host

import (
	"database/sql"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The host's table of domains, app.website({ domains: "./.database/system/kitwork.db" }) (14/11/2026): which app serves each
// hostname. It is the first engine's table domain of its system database (engine/database/master.go: SELECT identity
// FROM domain WHERE hostname = $1), moved from PostgreSQL to SQLite, its columns kept. A site folder
// apps/<identity>/<hostname>/ opens only when the table gives hostname to that identity, its status confirmed, its row
// not deleted and not sending it elsewhere: the order of the folders decides nothing. Before it, the folders alone
// said it: the app read first held a name two apps had folders of and the other app was closed whole, a hostname's
// grant of a database (app.databases apps) went to any app with a folder of that name, and AutoSSL asked for a
// certificate of any folder. The first engine read status and deleted_at not: an unverified or deleted row served.
//
// redirect_to sends a hostname elsewhere, as the first engine did (engine/domain/redirect.go): 301 to the hostname or
// the http(s):// address it holds, the request's scheme, its path and query kept; not with allowLocal. A name of the
// machine, of a private network or of a test (localhost, *.localhost, *.test, a loopback or private address) needs no
// row: no one owns it, and it never reaches AutoSSL.
//
// The layout says whether the table is needed, as the first engine's did (engine/work/root_layout.go: app/ is one app
// and reads no table, apps/<identity>/ are tenants and read it; the owner, 14/11/2026): apps/ are apps of several
// owners, so a public hostname under it is served by the table, at defaultDomains beside the other databases of the
// host unless app.website({ domains }) says where. A table not there while a public hostname is under apps/ stops the
// boot: the folders never decide a public name of tenants again. A host whose sites all have local names needs none.
//
// The table is read again when it changes while the host serves (14/11/2026), as the first engine asked its system
// database for each tenant it loaded (engine/work/tenant.go) and kept a redirect five minutes (engine/domain/redirect.go):
// on each beat of ReloadEvery, with app.hotReload or without, the watcher looks at the file and at its -wal, where SQLite
// in WAL mode and KitDB's writer (the host's database port) write first. A row added, given to another app, confirmed,
// deleted or sent elsewhere is served from the next beat: a redirect and AutoSSL read the table as it is, and each app
// the table now opens other sites of boots again, whole, as a reload boots it (reload.go), all such apps in one publish:
// a request reads the old boots until it and the new ones after, so a hostname moved from one app to another is served
// by the one or the other, never by both, and neither is closed for holding it. A table that changed and cannot
// be read (gone, broken, half made) is said in the log and the host serves by the table it read last: a tenant is never
// closed because the host's file is between two states. A host that had no table, its sites all local, reads one made
// after its boot; until then a public hostname is no one's, and a site of that name a reload finds is not opened.

// defaultDomains is where the table of domains is when app.website({ domains }) does not say: the host's database
// system, in the folder of the host's databases (app.database), from the manifest's folder.
const defaultDomains = ".database/system/kitwork.db"

// domainTableShape is the table the website's domains reads, as the first engine's system database has it.
const domainTableShape = `CREATE TABLE domain (hostname TEXT PRIMARY KEY, identity TEXT NOT NULL, status TEXT DEFAULT 'unverified', redirect_to TEXT, created_by TEXT, updated_by TEXT, deleted_by TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT)`

// domainRow is a row of the table: the identity of the app it gives hostname to, its status in lower case, where it
// sends it ("" for nowhere), and whether it is deleted. bad says redirect_to is no address.
type domainRow struct {
	identity, status, redirect string
	deleted                    bool
	bad                        string
}

// domainTable is the table as the boot read it, by hostname in lower case; nil when the manifest declares none.
// nameless are its rows whose hostname is empty, which give no name to anyone.
type domainTable struct {
	file     string
	rows     map[string]domainRow
	nameless int
}

// readDomains reads the table of file. A file not there, or one without the table, is the host's error: a host that
// declares its table serves no site it cannot read the owner of.
func readDomains(file string) (*domainTable, error) {
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return nil, fmt.Errorf("app.website({ domains }): %s is not there: the host's table of domains is a SQLite file holding %s", file, domainTableShape)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("app.website({ domains }): %s: %w", file, err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT hostname, COALESCE(identity, ''), COALESCE(status, ''), COALESCE(redirect_to, ''), deleted_at IS NOT NULL FROM domain`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "no such column") {
			return nil, fmt.Errorf("app.website({ domains }): %s has no table domain as the host reads it: %s (%v)", file, domainTableShape, err)
		}
		return nil, fmt.Errorf("app.website({ domains }): %s: %w", file, err)
	}
	defer rows.Close()
	table := &domainTable{file: file, rows: map[string]domainRow{}}
	for rows.Next() {
		var hostname, identity, status, redirect string
		var deleted bool
		if err := rows.Scan(&hostname, &identity, &status, &redirect, &deleted); err != nil {
			return nil, fmt.Errorf("app.website({ domains }): %s: %w", file, err)
		}
		name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
		if name == "" {
			table.nameless++
			continue
		}
		if _, twice := table.rows[name]; twice {
			return nil, fmt.Errorf("app.website({ domains }): %s gives %s in two rows: a hostname is one row", file, name)
		}
		row := domainRow{identity: strings.TrimSpace(identity), status: strings.ToLower(strings.TrimSpace(status)), redirect: strings.TrimSpace(redirect), deleted: deleted}
		if row.redirect != "" {
			if problem := redirectProblem(row.redirect); problem != "" {
				row.bad = "its redirect_to " + strconv.Quote(row.redirect) + " " + problem
			}
		}
		table.rows[name] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("app.website({ domains }): %s: %w", file, err)
	}
	return table, nil
}

// loadDomains reads the website's table of domains once: the boot and kitwork migrate both ask which app a hostname
// is. It is app.website({ domains }), or defaultDomains: there, it is read; not there, it is needed when a site folder
// under the apps has a public name, and the boot stops saying so.
func (h *Host) loadDomains() error {
	website := h.Manifest.Website
	if website == nil || h.domainsAt != "" {
		return nil
	}
	file := website.Domains
	if file == "" {
		file = filepath.Join(h.Folder, filepath.FromSlash(defaultDomains))
	}
	marks := tableMarks(file) // before the read: a write as it reads is read again at the watcher's first beat
	if website.Domains == "" {
		if _, err := os.Stat(file); err != nil {
			public := h.publicSite()
			if public != "" {
				return fmt.Errorf("the apps of %s are tenants, each public hostname served by the host's table of domains, and %s is not there (%s is a public hostname): make it, %s, or say where it is, app.website({ domains: \"…\" })",
					filepath.Base(h.root()), file, public, domainTableShape)
			}
			h.domainsAt, h.domainsMarks = file, marks // every site has a local name: no one to tell apart, until it is made
			return nil
		}
	}
	table, err := readDomains(file)
	if err != nil {
		return err
	}
	for _, line := range table.said() {
		h.log.Line("[app] " + line)
	}
	h.domains.Store(table)
	h.domainsAt, h.domainsMarks = file, marks
	return nil
}

// tableMarks are the marks of the table's file and of its -wal, where a write in WAL mode goes until a checkpoint.
func tableMarks(file string) map[string]mark {
	return marksOfFiles(file, file+"-wal")
}

// rereadDomains reads the table of domains again when its file or its -wal changed since the last read, and boots again
// each app whose sites the table opens otherwise now, all in one publish (reloadApps). A table that cannot be read is
// said, and the one read last stays.
func (h *Host) rereadDomains() {
	h.reloading.Lock()
	defer h.reloading.Unlock()
	defer h.survive("[app] ", "a read of the table of domains") // the next beat reads it again (panic.go)
	if h.closing || h.domainsAt == "" {
		return
	}
	now := tableMarks(h.domainsAt)
	if changed(h.domainsMarks, now) == "" {
		return
	}
	h.domainsMarks = now
	// the table tenant of the same file: each app whose limits it gives otherwise boots again (tenants.go)
	limited := map[string]bool{}
	if tenants, err := readTenants(h.domainsAt); err != nil {
		h.log.Error("[app] ", "the table tenant changed and is not read: "+err.Error()+"; the tenants keep the limits read last")
	} else if changed := tenants.changedFrom(h.tenants.Load()); len(changed) > 0 {
		h.log.Line("[app] tenant: the table changed (" + strings.Join(changed, ", ") + "): read again")
		h.tenants.Store(tenants)
		for _, line := range tenants.said(h.apps) {
			h.log.Line("[app] " + line)
		}
		for _, identity := range changed {
			limited[identity] = true
		}
	}
	table, err := readDomains(h.domainsAt)
	if err != nil {
		h.log.Error("[app] ", "the table of domains changed and is not read: "+err.Error()+"; the sites stay as the table read last gives them")
		table = nil
	}
	if table != nil {
		if what := table.changedFrom(h.domains.Load()); what != "" {
			h.log.Line("[app] domains: the table changed (" + what + "): read again")
			for _, line := range table.said() {
				h.log.Line("[app] " + line)
			}
			h.domains.Store(table) // a redirect and AutoSSL read it from now on
		} else {
			table = nil // a write that changed no row of it: its history, another table of the file
		}
	}
	var reboots []reboot
	for _, identity := range slices.Sorted(maps.Keys(h.apps)) {
		run := h.apps[identity]
		switch {
		case table != nil && !slices.Equal(h.opens(table, run.identity, run.root), slices.Sorted(slices.Values(run.hostnames))):
			reboots = append(reboots, reboot{old: run, identity: identity, root: run.root, marks: run.marks, why: "the table of domains changed"})
		case limited[identity]:
			reboots = append(reboots, reboot{old: run, identity: identity, root: run.root, marks: run.marks, why: "the table tenant changed"})
		}
	}
	if len(reboots) > 0 {
		h.reloadApps(reboots...)
	}
}

// changedFrom says what of t differs from old, the table read before it (nil for none): the hostnames whose row is
// added, gone or other, sorted, five at most; "" when nothing does.
func (t *domainTable) changedFrom(old *domainTable) string {
	if old == nil {
		return "made: " + strconv.Itoa(len(t.rows)) + " hostnames"
	}
	var names []string
	for name, row := range t.rows {
		if was, ok := old.rows[name]; !ok || was != row {
			names = append(names, name)
		}
	}
	for name := range old.rows {
		if _, ok := t.rows[name]; !ok {
			names = append(names, name)
		}
	}
	switch {
	case len(names) > 5:
		slices.Sort(names)
		return strings.Join(names[:5], ", ") + " and " + strconv.Itoa(len(names)-5) + " more"
	case len(names) > 0:
		slices.Sort(names)
		return strings.Join(names, ", ")
	case old.nameless != t.nameless:
		return "its rows with an empty hostname"
	}
	return ""
}

// refuses is why the site folder hostname of the app identity does not open by table, "" when it opens: a local name
// always does, and every name of a host of no website, which has no table; with no table, a public name does not,
// only the table giving it to an app.
func (h *Host) refuses(table *domainTable, identity, hostname string) string {
	switch {
	case localName(hostname), table == nil && h.domainsAt == "":
		return ""
	case table == nil:
		return "the host's table of domains, " + h.domainsAt + ", is not there, and only it gives a public hostname to an app"
	}
	return table.refuses(identity, hostname)
}

// opens are the hostnames of the site folders of the app identity at appRoot that table opens, sorted: what its boot
// would put in the map of sites.
func (h *Host) opens(table *domainTable, identity, appRoot string) []string {
	var opened []string
	for _, name := range h.siteNamesBy(table, identity, appRoot) {
		if hostnameProblem(name) == "" {
			opened = append(opened, name)
		}
	}
	return opened
}

// publicSite is the first folder at the root of an app under the apps, one whose module.work writes module.routes, named
// after a public hostname (a dot in it, not a local name): identity/hostname; "" when there is none. A folder named so
// is the site of that name only by the table (module.go siteNamesBy), so a host without the table serves it under no
// name: the boot says so rather than serving nothing quietly. A folder of code, core/, is no hostname's.
func (h *Host) publicSite() string {
	identities, err := os.ReadDir(h.root())
	if err != nil {
		return ""
	}
	for _, identity := range identities {
		if !identity.IsDir() || hidden(identity.Name()) {
			continue
		}
		appRoot := filepath.Join(h.root(), identity.Name())
		if !routed(appRoot) {
			continue
		}
		sites, err := os.ReadDir(appRoot)
		if err != nil {
			continue
		}
		for _, site := range sites {
			// a folder that is no hostname is its app's error at boot, said there escaped (hostnameProblem)
			if site.IsDir() && !hidden(site.Name()) && strings.Contains(site.Name(), ".") && hostnameProblem(site.Name()) == "" && !localName(strings.ToLower(site.Name())) {
				return identity.Name() + "/" + strings.ToLower(site.Name())
			}
		}
	}
	return ""
}

// redirectProblem is "" when to is where redirect_to may send a request: an http:// or https:// address, or a
// hostname with a port and a path or not; what is wrong otherwise.
func redirectProblem(to string) string {
	if strings.ContainsFunc(to, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return "holds a space or a control character"
	}
	if scheme, _, found := strings.Cut(to, "://"); found {
		if scheme != "http" && scheme != "https" {
			return "is no http:// or https:// address"
		}
		if parsed, err := url.Parse(to); err != nil || parsed.Host == "" {
			return "is no address"
		}
		return ""
	}
	name, _, _ := strings.Cut(to, "/")
	if host, port, found := strings.Cut(name, ":"); found {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "is no http:// or https:// address, nor a hostname"
		}
		name = host
	}
	if problem := hostnameProblem(name); problem != "" {
		return "is no hostname: " + problem
	}
	return ""
}

// paired is where the www rule sends a request to hostname (15/11/2026, the owner): www.X to X when X is a site and
// www.X is not, X to www.X when www.X is a site and X is not, so a site is reached by either name with no row and no
// word for it (the first engine had app.website({ canonical: "apex" }) for every site of the host; engine2 had
// nothing, and www.kitwork.io answered 404). "" when hostname is a site itself (both names a site: each serves
// itself), when the table has a row for it not deleted (the table decides that name: a row unverified sends nowhere),
// when its pair is no site, and for a local name, which no one owns. AutoSSL asks a certificate for such a name too
// (certificates.go): a name whose DNS is not the host's is never asked for, as no request comes.
func (h *Host) paired(hostname string) string {
	if hostname == "" || localName(hostname) {
		return ""
	}
	sites := h.current()
	if _, ok := sites[hostname]; ok || h.domains.Load().holds(hostname) {
		return ""
	}
	pair := "www." + hostname
	if apex, ok := strings.CutPrefix(hostname, "www."); ok {
		pair = apex
	}
	if _, ok := sites[pair]; !ok {
		return ""
	}
	return pair
}

// holds says the table has a row for hostname, not deleted: the name is the table's to decide.
func (t *domainTable) holds(hostname string) bool {
	if t == nil {
		return false
	}
	row, ok := t.rows[hostname]
	return ok && !row.deleted
}

// localName says hostname is the machine's, a private network's or a test's (localhost, *.localhost, *.test, a loopback
// or private address): no one owns it, and it needs no row. A public address does.
func localName(hostname string) bool {
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".test") {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	return false
}

// refuses is why the table opens not the site folder hostname, a public name, of the app identity; "" when it opens it.
func (t *domainTable) refuses(identity, hostname string) string {
	row, ok := t.rows[hostname]
	switch {
	case !ok:
		return "the table of domains has no row for it"
	case row.deleted:
		return "its row is deleted"
	case row.identity != identity:
		return "its row gives it to the app " + row.identity
	case row.status != "confirmed":
		return "its status is " + strconv.Quote(row.status) + ", not \"confirmed\""
	case row.bad != "":
		return row.bad
	case row.redirect != "" && row.redirect != hostname:
		return "its row sends it to " + row.redirect
	}
	return ""
}

// redirect is where a request to hostname goes, "" for nowhere: a row not deleted, confirmed or of status redirect,
// whose redirect_to is an address and not hostname itself.
func (t *domainTable) redirect(hostname string) string {
	if t == nil {
		return ""
	}
	row, ok := t.rows[hostname]
	if !ok || row.deleted || row.bad != "" || row.redirect == "" || row.redirect == hostname ||
		(row.status != "confirmed" && row.status != "redirect") {
		return ""
	}
	return row.redirect
}

// said is a line for each row of the table nothing comes of: a status redirect with no redirect_to, a redirect_to that
// is no address; and how many hostnames it gives and sends elsewhere.
func (t *domainTable) said() []string {
	var lines []string
	redirects := 0
	for _, name := range slices.Sorted(maps.Keys(t.rows)) {
		row := t.rows[name]
		switch {
		case row.deleted:
		case row.bad != "":
			lines = append(lines, "domains: "+name+": "+row.bad+": neither served nor sent elsewhere")
		case row.status == "redirect" && row.redirect == "":
			lines = append(lines, "domains: "+name+": status redirect with no redirect_to: neither served nor sent elsewhere")
		case t.redirect(name) != "":
			redirects++
		}
	}
	if t.nameless > 0 {
		lines = append(lines, fmt.Sprintf("domains: rows with an empty hostname, which give no name to anyone: %d", t.nameless))
	}
	return append(lines, fmt.Sprintf("domains: %d hostnames in %s, %d sent elsewhere", len(t.rows), t.file, redirects))
}

// requestHostname is a request's Host as a hostname: in lower case, without its port, brackets or a final dot.
func requestHostname(host string) string {
	// a Host with no port is most requests' (behind a proxy, on 443): SplitHostPort would make an error
	if strings.Contains(host, ":") {
		if name, _, err := net.SplitHostPort(host); err == nil {
			host = name
		}
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}

// redirected answers 301 for a hostname the table sends elsewhere, as the first engine did, or the www rule sends to
// its site (paired): the request's scheme, its path and query kept; never with allowLocal (the first engine neither).
func (h *Host) redirected(w http.ResponseWriter, r *http.Request) bool {
	if h.Manifest.Website == nil || h.Manifest.Website.AllowLocal {
		return false
	}
	hostname := requestHostname(r.Host)
	to := h.domains.Load().redirect(hostname)
	if to == "" {
		to = h.paired(hostname)
	}
	if to == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	target := scheme + "://" + to
	if strings.HasPrefix(to, "http://") || strings.HasPrefix(to, "https://") {
		target = strings.TrimRight(to, "/")
	}
	target += r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
	return true
}
