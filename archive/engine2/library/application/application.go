// Package application is "kitwork/application": the words of application.work, the host's manifest.
//
//	import * as app from "kitwork/application";
//	app.root("apps");
//	app.website({ hostname: "kitwork.io", port: 8080 });
//	app.run();
//
// Each app.<word>(…) is a method of Application that fills the one Manifest the host reads after the run,
// and gives the same Application back, so a chain goes on. The manifest declares; it does not act: no
// word opens a port, a database or a window while the file runs.
package application

import (
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// Application is what a manifest writes as app: the host makes one for each import of
// "kitwork/application", each writing into the same Manifest.
type Application struct {
	manifest *Manifest
	// folder is the manifest's folder, which app.root is read from.
	folder string
	// notice prints one line at boot: what the engine does not serve, a word it does not have.
	notice func(text string)
	// Where is the place of the call in progress, application.work:5:4, set by the host; nil for none.
	Where func() string
}

// New is the app a manifest in folder imports: its words fill manifest, its lines go to notice.
func New(manifest *Manifest, folder string, notice func(text string)) *Application {
	return &Application{manifest: manifest, folder: folder, notice: notice}
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// Root is app.root(path): the folder of the apps, apps/<identity>/<domain>/, from the manifest's folder.
func (a *Application) Root(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("root(…)")
	}
	if a.manifest.Root != "" {
		return value.Fail(value.InvalidArgumentCode, "app.root(…) is declared twice: one manifest, one root")
	}
	path := argument(arguments, 0)
	if !path.IsString() || strings.TrimSpace(path.Text()) == "" {
		return value.Fail(value.InvalidArgumentCode, "app.root(…) takes the folder of the apps, a path: app.root(\"apps\")")
	}
	root := filepath.FromSlash(path.Text())
	if !filepath.IsAbs(root) {
		root = filepath.Join(a.folder, root)
	}
	a.manifest.Root = filepath.Clean(root)
	return value.Struct(a)
}

// websiteServed are app.website's options the engine reads; websiteIgnored those it does not serve yet,
// each named at boot. Any other key is a failure: a misspelt option never passes as one left unread.
var (
	websiteServed  = []string{"hostname", "port", "allowLocal", "tls", "rateLimit", "domains"}
	websiteIgnored = []string{"redirects", "trustProxy", "maxBody"}
)

// Website is app.website({ hostname, port, allowLocal, … }), or app.website(hostname) for a manifest that opens
// nothing: the host serves its apps' sites over HTTP, on port.
func (a *Application) Website(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("website(…)")
	}
	if a.manifest.Website != nil {
		return value.Fail(value.InvalidArgumentCode, "app.website(…) is declared twice: one manifest, one website")
	}
	declared := argument(arguments, 0)
	website := &Website{TLS: TLS{Automatic: true}}
	switch {
	case declared.IsString():
		website.Hostname = declared.Text()
	case declared.IsObject():
		for _, key := range declared.Keys() {
			option := declared.Get(key)
			switch {
			case key == "hostname":
				if !option.IsString() {
					return value.Fail(value.InvalidArgumentCode, "app.website({ hostname }): the hostname is a text, \"kitwork.io\"")
				}
				website.Hostname = option.Text()
			case key == "port":
				number := option.Number()
				if !option.IsNumber() || number != float64(int(number)) || number < 1 || number > 65535 {
					return value.Fail(value.InvalidArgumentCode, "app.website({ port }): a whole number from 1 to 65535, got "+option.ToString().Text())
				}
				website.Port = int(number)
			case key == "allowLocal":
				if !option.IsBoolean() {
					return value.Fail(value.InvalidArgumentCode, "app.website({ allowLocal }): true or false, got "+option.Kind())
				}
				website.AllowLocal = option.Boolean()
			case key == "tls":
				tls, failure := websiteTLS(option)
				if failure.IsInvalid() {
					return failure
				}
				website.TLS = tls
			case key == "rateLimit":
				limit, failure := readRateLimit(option)
				if failure.IsInvalid() {
					return failure
				}
				website.RateLimit = limit
			case key == "domains":
				if !option.IsString() || strings.TrimSpace(option.Text()) == "" {
					return value.Fail(value.InvalidArgumentCode, `app.website({ domains }) takes the SQLite file of the host's table of domains, a path: domains: "./.database/system/kitwork.db"`)
				}
				file := filepath.FromSlash(option.Text())
				if !filepath.IsAbs(file) {
					file = filepath.Join(a.folder, file)
				}
				website.Domains = filepath.Clean(file)
			case key == "canonical":
				// the first engine's www ↔ apex for every site of the host; the www rule replaces it (15/11/2026)
				return value.Fail(value.InvalidArgumentCode, "app.website({ canonical }) is gone: a site's www. name and its bare one go to the site with no word, the name of its folder served and the other sent to it; a row of the table of domains sends a name elsewhere")
			case slices.Contains(websiteIgnored, key):
				a.ignore("website({ " + key + " })")
			default:
				return value.Fail(value.InvalidArgumentCode, "app.website({ "+key+" }): not an option of app.website; its options are "+strings.Join(append(slices.Clone(websiteServed), websiteIgnored...), ", "))
			}
		}
	default:
		return value.Fail(value.InvalidArgumentCode, "app.website(…) takes { hostname, port, … }, app.website({ hostname: \"kitwork.io\", port: 8080 }), or the hostname alone for a manifest that opens nothing")
	}
	hostname := strings.ToLower(strings.TrimSpace(website.Hostname))
	if hostname == "" || strings.ContainsAny(hostname, " /:\\") {
		return value.Fail(value.InvalidArgumentCode, "app.website(…): "+strconv.Quote(website.Hostname)+" is not a hostname: a name with no scheme, port or path, \"kitwork.io\"")
	}
	website.Hostname = hostname
	a.manifest.Website = website
	return value.Struct(a)
}

// websiteTLS reads app.website's tls (tls.go): true or false, or { email }, which is AutoSSL writing to that
// address. Anything else is a failure, a misspelt key among them, so no option passes as one left unread.
func websiteTLS(option value.Value) (TLS, value.Value) {
	switch {
	case option.IsBoolean():
		return TLS{Automatic: option.Boolean()}, value.Undefined()
	case option.IsObject():
		tls := TLS{Automatic: true}
		for _, key := range option.Keys() {
			if key != "email" {
				return TLS{}, value.Fail(value.InvalidArgumentCode, "app.website({ tls: { "+key+" } }): not an option of tls; its option is email, the address Let's Encrypt writes to")
			}
			email := option.Get(key)
			if !email.IsString() || strings.Count(email.Text(), "@") != 1 || strings.ContainsAny(email.Text(), " \t\r\n") {
				return TLS{}, value.Fail(value.InvalidArgumentCode, "app.website({ tls: { email } }): an address, \"admin@kitwork.io\"")
			}
			tls.Email = email.Text()
		}
		return tls, value.Undefined()
	}
	return TLS{}, value.Fail(value.InvalidArgumentCode, "app.website({ tls }): false, which serves no HTTPS, or { email }; AutoSSL needs nothing written, got "+option.Kind())
}

// Run is app.run(): the switch on the manifest's last line (docs/KITWORK_BOOT.md mục 3, luật 5; Quốc, 05/11/2026). The
// lines above declare what the app is; it opens what they declare that the command reading the manifest can open: go
// run . the website on app.website({ port }) and the database's port, kitwork database the port alone. It opens
// nothing while the manifest is read. It takes nothing: the port it took until 12/11/2026 is the website's, and a word
// written after it is the boot's failure (afterRun), so the file reads in the order it runs.
func (a *Application) Run(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return value.Fail(value.InvalidArgumentCode, "app.run() is declared twice: one manifest, one switch")
	}
	if len(arguments) == 1 && arguments[0].IsNumber() {
		port := arguments[0].ToString().Text()
		return value.Fail(value.InvalidArgumentCode, "app.run("+port+"): the port is the website's, app.website({ hostname: \"kitwork.io\", port: "+port+" }), and app.run() takes nothing: it is the switch on the manifest's last line")
	}
	if len(arguments) > 0 {
		return value.Fail(value.InvalidArgumentCode, "app.run() takes nothing: it is the switch on the manifest's last line; the website's port is app.website({ hostname: \"kitwork.io\", port: 8080 })")
	}
	a.manifest.Run = true
	return value.Struct(a)
}

// afterRun is the failure of a word written after app.run(): the switch is the manifest's last line, so what the file
// declares is all above what opens it.
func (a *Application) afterRun(word string) value.Value {
	return value.Fail(value.InvalidArgumentCode, "app."+word+" is written after app.run(): app.run() is the manifest's last line, the switch that opens what the lines above it declare")
}

// HotReload is app.hotReload(true | false): true boots an app again when one of its files changes, while the host
// serves (host/reload.go); false, as writing nothing, reads the apps once.
func (a *Application) HotReload(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("hotReload(…)")
	}
	if a.manifest.hotReloadSet {
		return value.Fail(value.InvalidArgumentCode, "app.hotReload(…) is declared twice: one manifest, one switch")
	}
	if len(arguments) != 1 || !arguments[0].IsBoolean() {
		return value.Fail(value.InvalidArgumentCode, "app.hotReload(…) takes true or false: app.hotReload(true), app.hotReload(env.HOT_RELOAD ?? false)")
	}
	a.manifest.HotReload, a.manifest.hotReloadSet = arguments[0].Boolean(), true
	return value.Struct(a)
}

// prewarmHow is how app.prewarm(…) is written.
const prewarmHow = `app.prewarm(…) names the sites it warms: app.prewarm("kitwork.io"), app.prewarm("a.test", "b.test"), app.prewarm("*") for every site, app.prewarm(false) for none`

// Prewarm is app.prewarm("kitwork.io", …) | app.prewarm("*") | app.prewarm(false): the sites whose pages and small
// files the boot reads and keeps (host prewarm.go). None by default (11/11/2026): keeping costs memory, which the
// host chooses to spend site by site.
func (a *Application) Prewarm(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("prewarm(…)")
	}
	if a.manifest.prewarmSet {
		return value.Fail(value.InvalidArgumentCode, "app.prewarm(…) is declared twice: one manifest, one list of sites")
	}
	if len(arguments) == 1 && arguments[0].IsBoolean() && !arguments[0].Boolean() {
		a.manifest.prewarmSet = true
		return value.Struct(a)
	}
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, prewarmHow)
	}
	var sites []string
	for _, argument := range arguments {
		if !argument.IsString() {
			return value.Fail(value.InvalidArgumentCode, prewarmHow)
		}
		site := strings.ToLower(strings.TrimSpace(argument.Text()))
		switch {
		case site == "*" && len(arguments) > 1:
			return value.Fail(value.InvalidArgumentCode, `app.prewarm("*") warms every site: alone, with no name beside it`)
		case site == "" || strings.ContainsAny(site, "/: \t"):
			return value.Fail(value.InvalidArgumentCode, "app.prewarm(…): "+strconv.Quote(argument.Text())+" is no hostname; "+prewarmHow)
		case slices.Contains(sites, site):
			return value.Fail(value.InvalidArgumentCode, "app.prewarm(…) names "+site+" twice")
		}
		sites = append(sites, site)
	}
	a.manifest.Prewarm, a.manifest.prewarmSet = sites, true
	return value.Struct(a)
}

// ignore names a word the engine does not serve yet, once, and lets the chain go on.
func (a *Application) ignore(word string) value.Value {
	if a.manifest.Run {
		return a.afterRun(word)
	}
	if !slices.Contains(a.manifest.Ignored, word) {
		a.manifest.Ignored = append(a.manifest.Ignored, word)
		a.notice("app." + word + " is not served by engine2 yet — ignored")
	}
	return value.Struct(a)
}

// surface is a surface the engine does not serve yet: the manifest declares it all the same.
func (a *Application) surface(word string) value.Value {
	if a.manifest.Run {
		return a.afterRun(word + "(…)")
	}
	if !slices.Contains(a.manifest.Surfaces, word) {
		a.manifest.Surfaces = append(a.manifest.Surfaces, word)
	}
	return a.ignore(word + "()")
}

// The words of the manifest the engine does not serve yet (docs/KITWORK_BOOT.md mục 3). Each prints one
// line at boot and gives the app back, so the words after it still apply.

func (a *Application) Desktop(...value.Value) value.Value    { return a.surface("desktop") }
func (a *Application) Mobile(...value.Value) value.Value     { return a.surface("mobile") }
func (a *Application) Postgresql(...value.Value) value.Value { return a.ignore("postgresql()") }
func (a *Application) Mysql(...value.Value) value.Value      { return a.ignore("mysql()") }
func (a *Application) Sqlite(...value.Value) value.Value     { return a.ignore("sqlite()") }
func (a *Application) Redis(...value.Value) value.Value      { return a.ignore("redis()") }
func (a *Application) Connect(...value.Value) value.Value    { return a.ignore("connect()") }
func (a *Application) Policy(...value.Value) value.Value     { return a.ignore("policy()") }
func (a *Application) Gateway(...value.Value) value.Value    { return a.ignore("gateway()") }
func (a *Application) Proxy(...value.Value) value.Value      { return a.ignore("proxy()") }

// ShareEnvironment is app.shareEnvironment([…]): not served yet, so no key of the host reaches an app,
// which is where the rule starts anyway (docs/KITWORK_ENVIRONMENT.md mục 2).
func (a *Application) ShareEnvironment(...value.Value) value.Value {
	return a.ignore("shareEnvironment()")
}

// MissingMember is a word this engine's manifest does not have (a typo, a word of a newer engine, the old app.web):
// the manifest's error at its line, saying the nearest word (15/11/2026, the owner, round 3 of the 13/11 review). It
// was named once at boot and passed over, the chain going on, so app.websit({ … }) booted a host with no website
// and said so in one line; a misspelt router or design word already stopped the boot where it was written. A word
// of the manifest's vocabulary engine2 does not serve yet (app.desktop, app.gateway, …) is still one line (ignore).
func (a *Application) MissingMember(name string) value.Value {
	if a.manifest.Run {
		return a.afterRun(name + "(…)")
	}
	text := "app has no word " + name
	if nearest := value.NearestName(name, value.Expose[Application]().Names()); nearest != "" {
		text += ": did you mean app." + nearest + "?"
	}
	return value.Fail(value.TypeCode, text)
}

// databaseTLS reads tls: { certificate, key } of app.database, both paths from the manifest's folder.
func (a *Application) databaseTLS(option value.Value) (*DatabaseTLS, value.Value) {
	refused := value.Fail(value.InvalidArgumentCode, "app.database({ tls }): { certificate, key }, the port's certificate file and its key")
	if !option.IsObject() {
		return nil, refused
	}
	files := &DatabaseTLS{}
	for _, key := range option.Keys() {
		item := option.Get(key)
		if key != "certificate" && key != "key" {
			return nil, value.Fail(value.InvalidArgumentCode, "app.database({ tls: { "+key+" } }): tls takes certificate and key")
		}
		if !item.IsString() || strings.TrimSpace(item.Text()) == "" {
			return nil, value.Fail(value.InvalidArgumentCode, "app.database({ tls: { "+key+" } }): the path of a file")
		}
		path := filepath.FromSlash(strings.TrimSpace(item.Text()))
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.folder, path)
		}
		if key == "certificate" {
			files.Certificate = filepath.Clean(path)
		} else {
			files.Key = filepath.Clean(path)
		}
	}
	if files.Certificate == "" || files.Key == "" {
		return nil, refused
	}
	return files, value.Undefined()
}

// databaseRemoved are the options of app.database the KitDB port had, each with why it went when the port moved to
// SQLite (30/10/2026), as the first engine says.
var databaseRemoved = map[string]string{
	"alias":  "the port no longer publishes a database to the process under an alias; an app reads its own database with connection() of .shared/schema/",
	"kitsql": "the /_kitsql/v1/query endpoint went with KitDB; read over the PostgreSQL wire",
	"memory": "SQLite keeps its own page cache; the KitDB page cache it sized is gone",
	"warm":   "a SQLite database opens with the session that reads it; nothing is kept warm",
	"cache":  "the KitDB query cache is gone; the port answers each statement from the file",
}

// Databases is app.databases({ warehouse: { sqlite: "./data/warehouse.db" } }): the host's catalogue, each name an
// app reads with connection("warehouse") (docs/KITWORK_DATABASE.md mục 1, lát 8): one connector a name, sqlite with a
// path read from the manifest's folder, postgresql or mysql with a URL. application, storage and files are no names of
// it; a failure never repeats a value written (a URL holds a password).
func (a *Application) Databases(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("databases(…)")
	}
	if a.manifest.Databases != nil {
		return value.Fail(value.InvalidArgumentCode, "app.databases(…) is declared twice: the host has one catalogue")
	}
	catalogue := argument(arguments, 0)
	if len(arguments) != 1 || !catalogue.IsObject() || catalogue.IsArray() {
		return value.Fail(value.InvalidArgumentCode, "app.databases takes one object: app.databases({ warehouse: { sqlite: \"./data/warehouse.db\" } })")
	}
	databases := map[string]*HostDatabase{}
	for _, name := range catalogue.Keys() {
		switch name {
		case "application":
			return value.Fail(value.InvalidArgumentCode, "app.databases: application is the app's own database, connection(), in its folder; it is no name of the catalogue")
		case "storage", "files":
			return value.Fail(value.InvalidArgumentCode, "app.databases: "+name+" is a name kitwork keeps (storage for datastore(), files for the files apps share)")
		}
		if !plainName(name) || strings.Contains(name, ".") {
			return value.Fail(value.InvalidArgumentCode, "app.databases: "+strconv.Quote(name)+" is no name of a database: letters, digits, _ and -")
		}
		entry := catalogue.Get(name)
		if !entry.IsObject() || entry.IsArray() {
			return value.Fail(value.InvalidArgumentCode, "app.databases."+name+" is one connector: { sqlite: path }, { postgresql: url } or { mysql: url }")
		}
		var connectors []string
		for _, key := range entry.Keys() {
			if key != "apps" {
				connectors = append(connectors, key)
			}
		}
		if len(connectors) != 1 {
			return value.Fail(value.InvalidArgumentCode, "app.databases."+name+" is one connector: { sqlite: path }, { postgresql: url } or { mysql: url }")
		}
		apps, failure := databaseApps(name, entry.Get("apps"))
		if failure.IsInvalid() {
			return failure
		}
		connector := connectors[0]
		where := entry.Get(connector)
		if !where.IsString() || strings.TrimSpace(where.Text()) == "" {
			return value.Fail(value.InvalidArgumentCode, "app.databases."+name+"."+connector+" is a text, a path or a URL")
		}
		switch connector {
		case "sqlite":
			path := filepath.FromSlash(strings.TrimSpace(where.Text()))
			if !filepath.IsAbs(path) {
				path = filepath.Join(a.folder, path)
			}
			databases[name] = &HostDatabase{Connector: connector, Path: filepath.Clean(path), Apps: apps}
		case "postgresql", "mysql":
			databases[name] = &HostDatabase{Connector: connector, URL: where.Text(), Apps: apps}
		default:
			return value.Fail(value.InvalidArgumentCode, "app.databases."+name+": "+connector+" is no connector; sqlite, postgresql or mysql")
		}
	}
	a.manifest.Databases = databases
	return value.Struct(a)
}

// databaseApps reads apps of the host's database name: "*" for every app, or a list of the apps that may open it, each
// a hostname of one of its sites or its folder; nil when the manifest writes none, which gives it to no app.
func databaseApps(name string, apps value.Value) ([]string, value.Value) {
	how := `app.databases.` + name + `.apps names the apps that may open it: apps: ["shop.example"], a hostname of one of the app's sites or the app's folder, or apps: "*" for every app`
	switch {
	case apps.IsUndefined():
		return nil, value.Value{}
	case apps.IsString() && strings.TrimSpace(apps.Text()) == "*":
		return []string{"*"}, value.Value{}
	case !apps.IsArray() || len(apps.Items()) == 0:
		return nil, value.Fail(value.InvalidArgumentCode, how)
	}
	var list []string
	for _, item := range apps.Items() {
		app := strings.ToLower(strings.TrimSpace(item.Text()))
		switch {
		case !item.IsString() || app == "" || strings.ContainsAny(app, "/:\\ "):
			return nil, value.Fail(value.InvalidArgumentCode, how)
		case app == "*":
			return nil, value.Fail(value.InvalidArgumentCode, `app.databases.`+name+`.apps: "*" gives it to every app, written alone: apps: "*"`)
		case slices.Contains(list, app):
			return nil, value.Fail(value.InvalidArgumentCode, "app.databases."+name+".apps names "+app+" twice")
		}
		list = append(list, app)
	}
	return list, value.Value{}
}

// Timezone is app.timezone(name): the zone of every app whose module.work declares none, an IANA zone name (slice 3 of
// root docs/KITWORK_MODULE_WORK.md, 17/11/2026). A zone is asked up the tiers, the nearest winning: a schedule's own
// .timezone(…), then the app's module.timezone(…), then this, then UTC. The server's own clock ("Local") is no zone.
func (a *Application) Timezone(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("timezone(…)")
	}
	if a.manifest.Timezone != nil {
		return value.Fail(value.InvalidArgumentCode, "app.timezone(…) is declared twice: the first is at "+a.manifest.TimezoneAt)
	}
	name := argument(arguments, 0)
	if len(arguments) != 1 || !name.IsString() || name.Text() == "" || name.Text() == "Local" {
		return value.Fail(value.InvalidArgumentCode, "app.timezone(…) takes an IANA zone name, \"Asia/Ho_Chi_Minh\"; the server's own clock (\"Local\") is no app's zone")
	}
	location, err := time.LoadLocation(name.Text())
	if err != nil {
		return value.Fail(value.InvalidArgumentCode, "app.timezone("+strconv.Quote(name.Text())+"): not an IANA zone name")
	}
	a.manifest.Timezone = location
	a.manifest.TimezoneAt = "application.work"
	if a.Where != nil {
		a.manifest.TimezoneAt = a.Where()
	}
	return value.Struct(a)
}

// History is app.history({ days }): how many days the history of the databases keeps, 30 when the manifest says
// nothing; at() reaches that far back (docs/KITWORK_KITDB.md câu 13.6).
func (a *Application) History(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("history(…)")
	}
	if a.manifest.History != nil {
		return value.Fail(value.InvalidArgumentCode, "app.history(…) is declared twice")
	}
	options := argument(arguments, 0)
	if len(arguments) != 1 || !options.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "app.history takes one object: app.history({ days: 90 })")
	}
	history := &History{}
	for _, key := range options.Keys() {
		if key != "days" {
			return value.Fail(value.InvalidArgumentCode, "app.history: "+key+" is not a setting of the history; it has days")
		}
		days := options.Get(key)
		n := days.Number()
		if !days.IsNumber() || n != float64(int(n)) || n < 1 || n > 36500 {
			return value.Fail(value.InvalidArgumentCode, "app.history: days is a whole number of days from 1 to 36500")
		}
		history.Days = int(n)
	}
	if history.Days == 0 {
		return value.Fail(value.InvalidArgumentCode, "app.history needs days: app.history({ days: 90 })")
	}
	a.manifest.History = history
	return value.Struct(a)
}

// maxSearchColumns is the most columns one index of search has.
const maxSearchColumns = 32

// searchIndexes reads search of app.database: database → table ("name" or "schema.name") → columns, a list (weight
// 1) or an object of weights from more than 0 to 1000, as the first engine reads it.
func searchIndexes(option value.Value) ([]SearchIndex, value.Value) {
	if !option.IsObject() || option.IsArray() {
		return nil, value.Fail(value.InvalidArgumentCode, "app.database({ search }) is an object: { database: { table: [columns] } }")
	}
	var indexes []SearchIndex
	for _, database := range sortedKeys(option) {
		if !plainName(database) {
			return nil, value.Fail(value.InvalidArgumentCode, "search: "+strconv.Quote(database)+" is not a database of the host's .database/ folder (a folder name)")
		}
		tables := option.Get(database)
		if !tables.IsObject() || tables.IsArray() {
			return nil, value.Fail(value.InvalidArgumentCode, "search."+database+" is an object: { table: [columns] }")
		}
		for _, key := range sortedKeys(tables) {
			index := SearchIndex{Database: database, Schema: "public", Table: key}
			if schema, table, qualified := strings.Cut(key, "."); qualified {
				index.Schema, index.Table = schema, table
			}
			if !plainName(index.Schema) || !plainName(index.Table) {
				return nil, value.Fail(value.InvalidArgumentCode, "search."+database+": "+strconv.Quote(key)+" is not a table name (table or schema.table)")
			}
			where := "search." + database + "." + key
			columns := tables.Get(key)
			switch {
			case columns.IsArray():
				for _, item := range columns.Items() {
					if !item.IsString() || strings.TrimSpace(item.Text()) == "" {
						return nil, value.Fail(value.InvalidArgumentCode, where+": each column is a name")
					}
					index.Columns = append(index.Columns, SearchColumn{Name: strings.TrimSpace(item.Text()), Boost: 1})
				}
			case columns.IsObject():
				for _, name := range sortedKeys(columns) {
					weight := columns.Get(name)
					if !weight.IsNumber() || weight.Number() <= 0 || weight.Number() > 1000 || strings.TrimSpace(name) == "" {
						return nil, value.Fail(value.InvalidArgumentCode, where+"."+name+": the weight of a column is a number from more than 0 to 1000")
					}
					index.Columns = append(index.Columns, SearchColumn{Name: strings.TrimSpace(name), Boost: weight.Number()})
				}
			default:
				return nil, value.Fail(value.InvalidArgumentCode, where+": write the columns as [\"name\", \"brand\"] or { name: 5, brand: 2 }")
			}
			if len(index.Columns) == 0 || len(index.Columns) > maxSearchColumns {
				return nil, value.Fail(value.InvalidArgumentCode, where+": an index has from 1 to "+strconv.Itoa(maxSearchColumns)+" columns")
			}
			seen := map[string]bool{}
			for _, column := range index.Columns {
				if seen[strings.ToLower(column.Name)] {
					return nil, value.Fail(value.InvalidArgumentCode, where+": column "+strconv.Quote(column.Name)+" is named twice")
				}
				seen[strings.ToLower(column.Name)] = true
			}
			indexes = append(indexes, index)
		}
	}
	return indexes, value.Value{}
}

func sortedKeys(object value.Value) []string {
	keys := append([]string(nil), object.Keys()...)
	sort.Strings(keys)
	return keys
}

// plainName is a folder, schema or table name: letters, digits, '_', '-' and '.', not "." or "..".
func plainName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// Database is app.database(path, { port, user, password }), or app.database({ path, port, user, password }) as the
// first engine reads it: the host's database port, the PostgreSQL wire on the SQLite files of the host's .database/
// folder at path (from the manifest's folder) and of every app's.
func (a *Application) Database(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("database(…)")
	}
	if a.manifest.Database != nil {
		return value.Fail(value.InvalidArgumentCode, "app.database(…) is declared twice: the host has one database port, on one .database/ folder")
	}
	path, options := argument(arguments, 0), argument(arguments, 1)
	if len(arguments) == 1 && path.IsObject() {
		options, path = path, path.Get("path")
		if !path.IsString() {
			return value.Fail(value.InvalidArgumentCode, "app.database({ path }): the host's .database/ folder, a path")
		}
	} else if len(arguments) != 2 {
		path = value.Undefined()
	}
	if !path.IsString() || strings.TrimSpace(path.Text()) == "" || !options.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "app.database(…) takes the host's .database/ folder and the port's options: app.database(\".database\", { port: 5433, user: \"admin\", password: env.DATABASE_PASSWORD })")
	}
	port := &DatabasePort{Host: "127.0.0.1", User: "kitdb"}
	folder := filepath.FromSlash(path.Text())
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(a.folder, folder)
	}
	port.Path = filepath.Clean(folder)
	for _, key := range options.Keys() {
		option := options.Get(key)
		if why, removed := databaseRemoved[key]; removed {
			return value.Fail(value.InvalidArgumentCode, "app.database({ "+key+" }) is gone (30/10/2026, the port runs on SQLite): "+why)
		}
		switch key {
		case "path":
		case "host":
			if !option.IsString() || strings.TrimSpace(option.Text()) == "" {
				return value.Fail(value.InvalidArgumentCode, "app.database({ host }): a host to listen on, \"127.0.0.1\"")
			}
			port.Host = strings.TrimSpace(option.Text())
		case "port":
			n := option.Number()
			if !option.IsNumber() || n != float64(int(n)) || n < 1 || n > 65535 {
				return value.Fail(value.InvalidArgumentCode, "app.database({ port }): a port from 1 to 65535")
			}
			port.Port = int(n)
		case "user":
			if !option.IsString() || strings.TrimSpace(option.Text()) == "" {
				return value.Fail(value.InvalidArgumentCode, "app.database({ user }): the admin's name, a text")
			}
			port.User = strings.TrimSpace(option.Text())
		case "password":
			if !option.IsString() {
				return value.Fail(value.InvalidArgumentCode, "app.database({ password }): the admin's password, a text")
			}
			port.Password = option.Text()
		case "concurrency":
			n := option.Number()
			switch {
			case option.IsString() && strings.EqualFold(strings.TrimSpace(option.Text()), "auto"):
			case option.IsNumber() && n == float64(int(n)) && n >= 1 && n <= 1024:
				port.Concurrency = int(n)
			default:
				return value.Fail(value.InvalidArgumentCode, "app.database({ concurrency }): \"auto\", or from 1 to 1024")
			}
		case "search":
			indexes, failure := searchIndexes(option)
			if failure.IsInvalid() {
				return failure
			}
			port.Search = indexes
		case "tls":
			files, failure := a.databaseTLS(option)
			if failure.IsInvalid() {
				return failure
			}
			port.TLS = files
		case "clientAuthority":
			if !option.IsString() || strings.TrimSpace(option.Text()) == "" {
				return value.Fail(value.InvalidArgumentCode, "app.database({ clientAuthority }): the path of the PEM file of the authorities "+
					"that sign the clients' certificates")
			}
			path := filepath.FromSlash(strings.TrimSpace(option.Text()))
			if !filepath.IsAbs(path) {
				path = filepath.Join(a.folder, path)
			}
			port.ClientAuthority = filepath.Clean(path)
		case "channelBinding":
			switch {
			case option.IsString() && option.Text() == "prefer":
			case option.IsString() && option.Text() == "require":
				port.RequireChannelBinding = true
			default:
				return value.Fail(value.InvalidArgumentCode, "app.database({ channelBinding }): \"prefer\" (the default: SCRAM-SHA-256-PLUS "+
					"offered over TLS) or \"require\" (a client of another machine authenticates with it only)")
			}
		default:
			return value.Fail(value.InvalidArgumentCode, "app.database({ "+key+" }): not an option of app.database; its options are host, port, user, password, concurrency, search, tls, channelBinding, clientAuthority")
		}
	}
	switch {
	case port.Port == 0:
		return value.Fail(value.InvalidArgumentCode, "app.database(…) needs { port }: the port the PostgreSQL wire listens on")
	case port.Password == "":
		return value.Fail(value.InvalidArgumentCode, "app.database(…) needs { password }: user and password are the admin of every cluster on the port")
	}
	a.manifest.Database = port
	return value.Struct(a)
}
