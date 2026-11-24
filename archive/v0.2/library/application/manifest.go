package application

import "time"

// Manifest is what application.work declared: each app.<word>(…) the engine serves is a field of the same
// name (docs/KITWORK_BOOT.md mục 3), filled once, since a word called twice stops the boot. A word the
// engine does not serve yet leaves its name in Ignored, an unknown word its name in Unknown.
type Manifest struct {
	// Root is the folder of the apps, app.root(…) read from the manifest's folder; "" when the manifest
	// does not say, which is apps/ beside it.
	Root string
	// HotReload is app.hotReload(true): while the host serves, an app whose files change is booted again and served
	// from its new boot (host/reload.go). hotReloadSet says the manifest wrote it, true or false: twice is an error.
	HotReload    bool
	hotReloadSet bool
	// Prewarm is app.prewarm(…): the sites read at boot and kept for it, by hostname, or "*" for every site (host
	// prewarm.go). Their pages are assembled before the host serves and their small files are kept in memory, so no
	// request reads the disk; nil, the default and app.prewarm(false), warms none: a page is read at its first request,
	// a file from the disk at each. prewarmSet says the manifest wrote it.
	Prewarm    []string
	prewarmSet bool
	// Limits is app.limits(…): what a run may make, how many requests a site runs at once, what every run of the host
	// makes together, for every site and for one (ceilings.go); nil for the defaults.
	Limits *Limits
	// Website is app.website(…); nil when the manifest declares none.
	Website *Website
	// Logger is app.logger({ … }): where the host's lines go besides standard output (logger.go); nil for none.
	Logger *Logger
	// Database is app.database(path, { … }): the host's database port; nil when the manifest declares none.
	Database *DatabasePort
	// History is app.history({ days }): how long the history of the databases is kept; nil for the default.
	History *History
	// Timezone is app.timezone(name): the zone of every app whose module.work declares none (17/11/2026, slice 3 of
	// root docs/KITWORK_MODULE_WORK.md); nil for none, UTC. TimezoneAt is where it is written, application.work:5:4.
	Timezone   *time.Location
	TimezoneAt string
	// Databases is app.databases({ … }): the host's catalogue, each name an app reads with connection("name"); nil
	// when the manifest declares none
	Databases map[string]*HostDatabase
	// Commands is app.commands({ … }): what `kitwork <name>` runs, by name (commands.go); nil when the manifest
	// declares none
	Commands map[string]*Command
	// Run is app.run(): the switch on the manifest's last line (docs/KITWORK_BOOT.md mục 3, luật 5); without it the
	// manifest is read and checked, and nothing opens. The website's port is Website.Port, app.website({ port }).
	Run bool
	// Surfaces are the surfaces the manifest declares that the engine does not serve yet (desktop,
	// mobile, database): the app is one of them, so it declares a surface, though nothing opens it.
	Surfaces []string
	// Ignored are the words the engine does not serve yet, each named once at boot, in written order.
	Ignored []string
}

// DatabasePort is app.database(path, { host, port, user, password, concurrency, tls, channelBinding, clientAuthority }): the PostgreSQL wire on the
// SQLite files of the host's .database/ folder (Path) and of every app's (docs/KITWORK_KITDB.md câu 13.5). User and
// Password are the admin of every cluster; Concurrency bounds the statements running at once (0: the server's own
// bound). Host is where it listens: beyond the loopback a client comes in by TLS, the files of TLS or else the
// website's certificates (câu 13.11, 08/11/2026).
type DatabasePort struct {
	Path        string
	Host        string
	Port        int
	User        string
	Password    string
	Concurrency int
	// TLS is tls: { certificate, key }, nil for none
	TLS *DatabaseTLS
	// RequireChannelBinding is channelBinding: "require": a client of another machine authenticates with
	// SCRAM-SHA-256-PLUS only, its password's proof bound to the port's certificate ("prefer", the default: offered)
	RequireChannelBinding bool
	// ClientAuthority is clientAuthority, the PEM file of the authorities whose certificate a client of another machine
	// presents, its common name its user, as well as its password ("" for none), read from the manifest's folder
	ClientAuthority string
	// Search are the full-text indexes MATCH reads on the port that search declares, for a table of the host's
	// .database/ (an app's table has its own from its .searchable() columns), in a fixed order: databases, then
	// tables, by name
	Search []SearchIndex
}

// DatabaseTLS is tls: { certificate, key } of app.database: a PEM certificate file (its chain after it) and its key,
// as PostgreSQL's ssl_cert_file and ssl_key_file, read from the manifest's folder.
type DatabaseTLS struct {
	Certificate, Key string
}

// SearchIndex is one table's full-text index search declares: database → table ("name" or "schema.name") → columns,
// a list (weight 1) or an object of weights.
type SearchIndex struct {
	Database, Schema, Table string
	Columns                 []SearchColumn
}

// SearchColumn is one indexed column and how much a match in it weighs.
type SearchColumn struct {
	Name  string
	Boost float64
}

// HostDatabase is one name of app.databases: its connector (sqlite, postgresql, mysql) and where it is. A sqlite's
// Path is read from the manifest's folder; a URL is kept for the connector that takes it, never said.
type HostDatabase struct {
	Connector string
	Path      string
	URL       string
	// Apps are the apps that may open it, apps: [...]: each a hostname of one of the app's sites or the app's folder
	// (its identity); ["*"] for every app. nil, the default, gives it to none: an app that declares tables on it
	// does not boot (11/11/2026)
	Apps []string
}

// History is app.history({ days }): the days at() reaches back; what no moment of them needs goes (older anchors,
// older commits) (docs/KITWORK_KITDB.md câu 13.6).
type History struct {
	Days int
}
