package host

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/huynhnhanquoc/sqlitewire/analytics"
	"github.com/huynhnhanquoc/sqlitewire/catalog"
	"github.com/huynhnhanquoc/sqlitewire/gateway"
	"github.com/huynhnhanquoc/sqlitewire/pgwire"
	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/library/database"
)

// The host's database port (docs/KITWORK_KITDB.md câu 13.5; app.database of application.work): the PostgreSQL wire on
// SQLite files, KitDB's own packages (gateway, pgwire, catalog) as the first engine serves them. A client names its
// database as a cluster's catalogue does: a folder of the host's .database/ ("shopping"), or <domain>/<folder> of an
// app ("shop.test/application": the folder application/ of the .database/ of the app whose site has that domain).
// The database's schemas are the .db files of its folder, kitwork.db the public one. Each .database/ is one cluster,
// its accounts, roles and grants in .catalog/kitwork.db, managed by SQL over the port; app.database's user and password
// are the admin of every cluster. SCRAM-SHA-256; the loopback only, until TLS. A write goes through the file's one
// writer in the process, the one the apps' statements write through (library/database, writers.go), so the history
// keeps it and a site reads it at once.

// databasePort is the port while the host runs.
type databasePort struct {
	host     *Host
	folder   string // the host's .database/, a cluster of its own
	admin    catalog.Admin
	options  gateway.Options
	listener net.Listener
	cancel   context.CancelFunc
	done     chan struct{}
	// search holds the full-text indexes MATCH reads (port_search.go)
	search *portSearch

	mutex    sync.Mutex
	catalogs map[string]*catalog.Catalog // by the cluster's folder
	closed   bool
}

// DatabasePort starts the port app.database declares, once, and gives where it listens; nil when the manifest
// declares none.
func (h *Host) DatabasePort() (net.Addr, error) {
	config := h.Manifest.Database
	if config == nil {
		return nil, nil
	}
	h.portOnce.Do(func() { h.port, h.portErr = h.startPort() })
	if h.portErr != nil {
		return nil, h.portErr
	}
	return h.port.listener.Addr(), nil
}

func (h *Host) startPort() (*databasePort, error) {
	config := h.Manifest.Database
	if info, err := os.Stat(config.Path); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("app.database %q: the path is a file; the port serves the host's .database/ folder", config.Path)
	}
	if err := os.MkdirAll(config.Path, 0o700); err != nil {
		return nil, fmt.Errorf("app.database %q: %w", config.Path, err)
	}
	port := &databasePort{
		host: h, folder: config.Path,
		admin: catalog.Admin{User: config.User, Password: config.Password},
		options: gateway.Options{MaxRows: 10_000, MaxBytes: 10 << 20, Analytics: analytics.NewRegistry(),
			Writers: portWriters{}},
		catalogs: map[string]*catalog.Catalog{},
		search:   &portSearch{releases: map[string]func(){}},
	}
	secure, certificates, err := h.DatabaseTLS()
	if err != nil {
		_ = port.closeCatalogs()
		return nil, err
	}
	var clients *x509.CertPool
	if config.ClientAuthority != "" {
		if clients, err = pgwire.ClientAuthorityFile(config.ClientAuthority); err != nil {
			_ = port.closeCatalogs()
			return nil, fmt.Errorf("app.database({ clientAuthority }): %w", err)
		}
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		_ = port.closeCatalogs()
		return nil, fmt.Errorf("app.database: listen on %s: %w", address, err)
	}
	if secure == nil {
		if err := pgwire.ValidateCleartextListener(listener); err != nil {
			_ = listener.Close()
			_ = port.closeCatalogs()
			return nil, fmt.Errorf("app.database: %w (without TLS the host is 127.0.0.1 or ::1)", err)
		}
	}
	server := pgwire.Server{Authenticator: port, TLS: secure, RequireChannelBinding: config.RequireChannelBinding,
		ClientAuthorities: clients}
	if config.Concurrency > 0 {
		server.MaxConcurrentQueries = config.Concurrency
		server.MaxConcurrentQueriesPerKey = config.Concurrency
		server.MaxConnections = max(16, config.Concurrency*4)
	}
	port.openDeclared()
	ctx, cancel := context.WithCancel(context.Background())
	port.listener, port.cancel, port.done = listener, cancel, make(chan struct{})
	go func() {
		defer close(port.done)
		if err := server.Serve(ctx, listener); err != nil && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
			h.log.Line("[database] the port stopped: " + err.Error())
		}
	}()
	line := "[database] the port serves " + config.Path + " and every app's .database/ on " + listener.Addr().String() + " (PostgreSQL wire, SCRAM-SHA-256"
	if secure != nil {
		line += ", TLS: " + certificates
	}
	if server.RequireChannelBinding {
		line += ", SCRAM-SHA-256-PLUS required of a client of another machine"
	}
	if server.ClientAuthorities != nil {
		line += ", a client of another machine presents a certificate of " + config.ClientAuthority
	}
	h.log.Line(line + ")")
	return port, nil
}

// DatabaseTLS is the TLS app.database's port answers with (docs/KITWORK_KITDB.md câu 13.11, 08/11/2026), and what it
// is: the files of tls: { certificate, key }; else, when the port listens beyond the loopback, the website's
// certificates, out of its AutoSSL store (.certificates/), a site's by the name the client connects with; nil on the
// loopback without files, the port as before. Beyond the loopback with no files and no website of AutoSSL, an error:
// a client of another machine comes in by TLS only.
func (h *Host) DatabaseTLS() (*tls.Config, string, error) {
	config := h.Manifest.Database
	if config == nil {
		return nil, "", nil
	}
	if config.TLS != nil {
		certificate, err := pgwire.CertificateFiles(config.TLS.Certificate, config.TLS.Key)
		if err != nil {
			return nil, "", fmt.Errorf("app.database({ tls }): %w", err)
		}
		if config.RequireChannelBinding {
			if pair, err := certificate(&tls.ClientHelloInfo{}); err != nil || !pgwire.BindsChannel(pair) {
				return nil, "", fmt.Errorf("app.database({ channelBinding: \"require\" }): the certificate %s is signed without a hash "+
					"(Ed25519): no client could bind to it; use an RSA or ECDSA certificate", config.TLS.Certificate)
			}
		}
		return pgwire.TLSConfig(certificate), config.TLS.Certificate, nil
	}
	if loopbackHost(config.Host) {
		return nil, "", nil
	}
	website := h.Manifest.Website
	if website == nil || !website.TLS.Automatic {
		return nil, "", fmt.Errorf("app.database listens on %s: a client of another machine comes in by TLS, and the manifest "+
			"declares no website of AutoSSL whose certificates the port could use; write tls: { certificate, key }", config.Host)
	}
	store := filepath.Join(h.Folder, certificatesFolder)
	certificates := pgwire.CachedCertificates(store, func(name string) bool {
		_, found := h.appRootOf(name)
		return found
	})
	certificate := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		found, err := certificates(hello)
		if err != nil {
			h.log.Line("[database] TLS: " + err.Error())
		}
		return found, err
	}
	return pgwire.TLSConfig(certificate), "the website's certificates in " + store, nil
}

// loopbackHost reports a host the port listens on that only this machine reaches.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// close stops the port: the listener, the sessions, the catalogs.
func (p *databasePort) close() error {
	if p == nil {
		return nil
	}
	p.cancel()
	err := p.listener.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	<-p.done
	p.search.close()
	return errors.Join(err, p.closeCatalogs())
}

func (p *databasePort) closeCatalogs() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.closed = true
	var failures []error
	for _, opened := range p.catalogs {
		failures = append(failures, opened.Close())
	}
	p.catalogs = nil
	if p.options.Analytics != nil {
		failures = append(failures, p.options.Analytics.Close())
	}
	return errors.Join(failures...)
}

// portTarget is one database as a client named it.
type portTarget struct {
	name    string // as the catalog names it: lower case
	cluster string // its .database/
	folder  string // its own folder in the cluster
}

// portPart is one part of a database's name: a domain or a folder, never a path out of where it is looked for.
var portPart = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// resolve finds the database a client names, or why there is none.
func (p *databasePort) resolve(name string) (portTarget, error) {
	return p.host.resolveDatabase(p.folder, name)
}

// resolveDatabase finds the database named as the port names it: a folder of the host's .database/ (hostFolder), or
// <domain>/<folder> of an app; a command's context.database(name) names it the same way.
func (h *Host) resolveDatabase(hostFolder, name string) (portTarget, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	domain, folder, ofApp := strings.Cut(name, "/")
	if !ofApp {
		folder, domain = domain, ""
	}
	if !portPart.MatchString(folder) || strings.Contains(folder, "..") || ofApp && (!portPart.MatchString(domain) || strings.Contains(domain, "..")) {
		return portTarget{}, fmt.Errorf("database %q does not exist: a database is <folder> of the host's .database/ or <domain>/<folder> of an app", name)
	}
	cluster := hostFolder
	if ofApp {
		appRoot, found := h.appRootOf(domain)
		if !found {
			return portTarget{}, fmt.Errorf("database of %q does not exist: no app has that domain", domain)
		}
		cluster = filepath.Join(appRoot, ".database")
	}
	path := filepath.Join(cluster, folder)
	file, err := os.Stat(filepath.Join(path, "kitwork.db"))
	if err != nil || !file.Mode().IsRegular() {
		// a database of only its structure is one: an authenticated session has it made (portSchemas)
		if structure, err := os.Stat(filepath.Join(path, "kitwork.db.sql")); err != nil || !structure.Mode().IsRegular() {
			return portTarget{}, fmt.Errorf("database %q does not exist: no %s", name, filepath.ToSlash(filepath.Join(folder, "kitwork.db")))
		}
	}
	return portTarget{name: name, cluster: cluster, folder: path}, nil
}

// appRootOf is the folder of the app whose site has domain: the open site's once Boot ran, or, when no site is open
// (kitwork database, ServeDatabase), the app under app.root the host gives that name (siteNamesBy: the table of domains,
// a local folder of an app with module.routes), as Boot would find it (17/11/2026: a folder of that name, so a folder of
// code, core/, could be a domain). A host without a website has no app's database.
func (h *Host) appRootOf(domain string) (string, bool) {
	if sites := h.Sites(); sites != nil {
		site := sites[domain]
		if site == nil {
			return "", false
		}
		return site.AppRoot, true
	}
	if h.Manifest.Website == nil {
		return "", false
	}
	if root := h.root(); !h.oneApp && filepath.Base(root) == "app" {
		return "", false // the layout of one app of 22/10/2026, which engine2 does not serve (Boot)
	}
	apps, err := h.appFolders()
	if err != nil {
		return "", false
	}
	if !h.oneApp && h.loadDomains() != nil {
		return "", false // a table the host cannot read gives no name to anyone
	}
	for _, each := range apps {
		if slices.Contains(h.siteNamesBy(h.domains.Load(), each.identity, each.root), domain) {
			return each.root, true
		}
	}
	return "", false
}

// catalogOf is the catalog of a cluster, opened once.
func (p *databasePort) catalogOf(ctx context.Context, cluster string) (*catalog.Catalog, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.closed {
		return nil, fmt.Errorf("the database port is closing")
	}
	if opened := p.catalogs[cluster]; opened != nil {
		return opened, nil
	}
	opened, err := catalog.Open(ctx, cluster, p.admin)
	if err != nil {
		return nil, err
	}
	p.catalogs[cluster] = opened
	return opened, nil
}

// Authenticate is the cleartext exchange, which the port does not offer: it authenticates with SCRAM-SHA-256.
func (p *databasePort) Authenticate(context.Context, pgwire.Startup, string) (pgwire.Session, error) {
	return nil, pgwire.NewError("28000", "this server authenticates with SCRAM-SHA-256")
}

// SCRAMVerifier is the verifier of the startup's user in the cluster of the database it names. For a database that
// does not exist only the host's admin is known (told so once authenticated); anyone else fails as a wrong password.
func (p *databasePort) SCRAMVerifier(ctx context.Context, startup pgwire.Startup) (pgwire.SCRAMVerifier, bool, error) {
	cluster := p.folder
	if target, err := p.resolve(startup.Database()); err == nil {
		cluster = target.cluster
	} else if startup.User() != p.admin.User {
		return pgwire.SCRAMVerifier{}, false, nil
	}
	accounts, err := p.catalogOf(ctx, cluster)
	if err != nil {
		p.host.log.Line("[database] the catalog of " + cluster + " is unavailable: " + err.Error())
		return pgwire.SCRAMVerifier{}, false, pgwire.NewError("58000", "the catalog of this database is unavailable")
	}
	return accounts.Verifier(ctx, startup.User())
}

// OpenSession opens the session of an authenticated user on the database it names.
func (p *databasePort) OpenSession(ctx context.Context, startup pgwire.Startup) (pgwire.Session, error) {
	target, err := p.resolve(startup.Database())
	if err != nil {
		return nil, pgwire.NewError("3D000", err.Error())
	}
	accounts, err := p.catalogOf(ctx, target.cluster)
	if err != nil {
		return nil, pgwire.NewError("58000", "the catalog of this database is unavailable")
	}
	user := startup.User()
	access := &portAccess{catalog: accounts, user: user, database: target.name, admin: accounts.IsAdmin(user)}
	if !access.admin {
		allowed, err := accounts.CanConnect(ctx, user, target.name)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, pgwire.NewError("42501", fmt.Sprintf("permission denied for database %q", target.name))
		}
	}
	if access.stamp, err = accounts.Stamp(ctx, user); err != nil {
		return nil, err
	}
	schemas, err := portSchemas(target.folder)
	if err != nil {
		return nil, pgwire.NewError("3D000", fmt.Sprintf("database %q: %v", target.name, err))
	}
	columns, err := readPortColumns(ctx, schemas)
	if err != nil {
		return nil, pgwire.NewError("58000", fmt.Sprintf("database %q: %v", target.name, err))
	}
	options := p.options
	options.User = user
	options.Search = portSearchProvider{database: target.name, schemas: schemas}
	options.Log = func(statement string, err error) {
		text := []rune(strings.Join(strings.Fields(statement), " "))
		if len(text) > 2000 {
			text = append(text[:2000], []rune(" …")...)
		}
		p.host.log.Line(fmt.Sprintf("[database] %s (%s): %v — %s", target.name, user, err, string(text)))
	}
	session, err := gateway.Open(ctx, gateway.Target{Name: target.name, Schemas: schemas}, access, columns, options)
	if err != nil {
		return nil, err
	}
	p.discover(target.name, schemas, columns) // the tables whose columns ask for an index get one (port_search.go)
	return session, nil
}

// portSchemaName is a schema file's name: kitwork.db is public, <name>.db the schema <name>.
var portSchemaName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// portSchemas are the schemas of a database: each .db file of its folder. A structure, <name>.db.sql, whose file is
// not there (a clone of the repository) gets the file made from it first (library/database, writers.go).
func portSchemas(folder string) (map[string]string, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	made := false
	for _, entry := range entries {
		if name := entry.Name(); entry.Type().IsRegular() && strings.HasSuffix(name, ".db.sql") {
			did, err := database.MakeFromStructure(filepath.Join(folder, strings.TrimSuffix(name, ".sql")))
			if err != nil {
				return nil, err
			}
			made = made || did
		}
	}
	if made {
		if entries, err = os.ReadDir(folder); err != nil {
			return nil, err
		}
	}
	schemas := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".db") {
			continue
		}
		schema := strings.TrimSuffix(name, ".db")
		if name == "kitwork.db" {
			schema = "public"
		} else if !portSchemaName.MatchString(schema) || schema == "public" || schema == "main" || schema == "temp" {
			continue
		}
		schemas[schema] = filepath.Join(folder, name)
	}
	if schemas["public"] == "" {
		return nil, fmt.Errorf("no kitwork.db")
	}
	return schemas, nil
}

// portAccess is what one session's user may do, asked of its cluster's catalog.
type portAccess struct {
	catalog  *catalog.Catalog
	user     string
	database string
	admin    bool
	stamp    uint64
}

func (a *portAccess) CanSelect(ctx context.Context, schema, table string) (bool, error) {
	if a.admin {
		return true, nil
	}
	return a.catalog.CanSelect(ctx, a.user, a.database, schema, table)
}

func (a *portAccess) CanModify(ctx context.Context, schema, table, privilege string) (bool, error) {
	if a.admin {
		return true, nil
	}
	return a.catalog.Can(ctx, a.user, a.database, schema, table, privilege)
}

func (a *portAccess) Admin(context.Context) (bool, error) { return a.admin, nil }

func (a *portAccess) Command(ctx context.Context, statement string) (string, bool, error) {
	return a.catalog.Command(ctx, a.user, a.database, statement)
}

// Current is false once the user's password, roles or grants changed since the session opened: it ends.
func (a *portAccess) Current(ctx context.Context) (bool, error) {
	stamp, err := a.catalog.Stamp(ctx, a.user)
	return err == nil && stamp == a.stamp, err
}

// portWriters opens, for a session that writes, the writer of a file the apps' statements write through.
type portWriters struct{}

func (portWriters) Open(path string) (gateway.Writer, error) {
	opened, release, err := database.Acquire(path)
	if err != nil {
		return nil, err
	}
	return portWriter{Database: opened, release: release}, nil
}

type portWriter struct {
	*kitdb.Database
	release func()
}

func (w portWriter) Close() error {
	w.release()
	return nil
}

// portColumns is what kitwork_columns of each schema file says of its columns.
type portColumns map[string]portColumn

type portColumn struct {
	table, name      string // as the file writes them
	kind             string
	precision, scale int
	weight           int // its search weight: the column is in its table's full-text index (port_search.go)
}

func (columns portColumns) Column(schema, table, column string) (string, int, int, bool) {
	found, ok := columns[strings.ToLower(schema+"."+table+"."+column)]
	return found.kind, found.precision, found.scale, ok
}

// readPortColumns reads kitwork_columns of every schema file, read only; a file without it says nothing.
func readPortColumns(ctx context.Context, schemas map[string]string) (portColumns, error) {
	columns := portColumns{}
	names := make([]string, 0, len(schemas))
	for schema := range schemas {
		names = append(names, schema)
	}
	sort.Strings(names)
	for _, schema := range names {
		file, err := sql.Open("sqlite", "file:"+filepath.ToSlash(schemas[schema])+"?mode=ro&_pragma=busy_timeout(5000)")
		if err != nil {
			return nil, err
		}
		err = func() error {
			defer file.Close()
			var held int
			if err := file.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'kitwork_columns'`).Scan(&held); err != nil || held == 0 {
				return err
			}
			// the search weight came on 31/10/2026: a file written before has none
			weight := "0"
			if err := file.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('kitwork_columns') WHERE name = 'search_weight'`).Scan(&held); err != nil {
				return err
			}
			if held > 0 {
				weight = "search_weight"
			}
			rows, err := file.QueryContext(ctx, `SELECT table_name, column_name, kind, precision, scale, `+weight+` FROM kitwork_columns`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var found portColumn
				if err := rows.Scan(&found.table, &found.name, &found.kind, &found.precision, &found.scale, &found.weight); err != nil {
					return err
				}
				columns[strings.ToLower(schema+"."+found.table+"."+found.name)] = found
			}
			return rows.Err()
		}()
		if err != nil {
			return nil, err
		}
	}
	return columns, nil
}
