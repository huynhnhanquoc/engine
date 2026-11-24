package database

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/schema"
)

// Catalog is one app's databases, declared by its .shared/schema/ as it boots. The boot is one goroutine per
// app, and every request reads the catalog after it: nothing guards it.
type Catalog struct {
	// hosted is the host's catalogue (app.databases), by name
	hosted  map[string]Hosted
	root    string // the app's folder
	entries []*Entry
	// made are the structs the declaring pass made, with where; placed the table each struct is
	made   []madeStruct
	placed map[*schema.Struct]string
	// handles are, by schema file (from the app's root), the names it exports a database by (Exports);
	// statements every statement's lambda the boot translated, by its place (Prepare). Both are written as
	// the app boots and read by its requests.
	handles    map[string]map[string]*Entry
	statements map[lambdaKey]*compiled
	// open are the files the statements opened, plans the statements put together by their lambdas, searches
	// the releases of the full-text files matches holds (search.go), all guarded by mutex
	mutex    sync.Mutex
	open     map[*Entry]*kitdb.Database
	releases map[*Entry]func() // the shared writers the files were acquired from (writers.go)
	plans    map[string]*plan
	searches map[string]func()
	// transactions are the transactions open, by the goroutine running each one's callback (transaction.go)
	transactions map[uint64]*transaction
}

type madeStruct struct {
	made  *schema.Struct
	where string
}

// Entry is one database and schema the app declares: connection(Name).schema(Schema).tables({ … }).
type Entry struct {
	Name   string // "" for connection(), the app's own
	Schema string // "" for public
	Where  string // the place of .tables(…)
	// Path is the file the entry's tables are in: the app's .database/application/kitwork.db (or <schema>.db), or the
	// sqlite file of the host's catalogue a name is (app.databases)
	Path   string
	Tables []Table
	// Report is what the file said of the declaration when the app booted (Catalog.Check)
	Report Report
}

// Table is one table of an entry: its name, the struct it is, and its fields as kitwork/database reads them.
type Table struct {
	Name   string
	Struct *schema.Struct
	Fields []schema.TableField
	// Transforms are the steps of each column's transform, by column (transform.go)
	Transforms map[string][]string
}

// NewCatalog is the empty catalog of the app at root.
func NewCatalog(root string) *Catalog {
	return &Catalog{root: root, hosted: hostedCatalogue(), placed: map[*schema.Struct]string{}, handles: map[string]map[string]*Entry{},
		statements: map[lambdaKey]*compiled{}, open: map[*Entry]*kitdb.Database{}, releases: map[*Entry]func(){}, plans: map[string]*plan{}, searches: map[string]func(){}, transactions: map[uint64]*transaction{}}
}

// Entries are the databases declared, in the order the boot met them.
func (c *Catalog) Entries() []*Entry { return c.entries }

// Made records a struct the declaring pass made, at where (schema.Schema.Made).
func (c *Catalog) Made(made *schema.Struct, where string) {
	c.made = append(c.made, madeStruct{made: made, where: where})
}

// Declared ends the declaring pass: a struct written with a word of a table that no tables({ … }) takes is an
// error (docs/schema.md mục 2, the second rule): .unique() on a shape nobody stores is a rule nobody keeps.
func (c *Catalog) Declared() error {
	// a struct made at the place of one a table holds is that declaration run again, by a file that imports it
	placedAt := map[string]bool{}
	for _, m := range c.made {
		if _, table := c.placed[m.made]; table {
			placedAt[m.where] = true
		}
	}
	for _, m := range c.made {
		if _, table := c.placed[m.made]; table || placedAt[m.where] {
			continue
		}
		if word := m.made.TableWord(); word != "" {
			return fmt.Errorf("%s: struct có %s mà không là bảng: chữ của bảng chỉ có nghĩa khi struct nằm trong connection().tables({ … }); bỏ nó, hay đặt struct vào tables", m.where, word)
		}
	}
	c.made = nil
	return c.translateTransforms()
}

// label is a database as a person reads it: connection(), connection("warehouse").schema("billing").
func label(name, schemaName string) string {
	out := "connection()"
	if name != "" {
		out = "connection(" + strconv.Quote(name) + ")"
	}
	if schemaName != "" {
		out += ".schema(" + strconv.Quote(schemaName) + ")"
	}
	return out
}

// Hosted is a name of the host's catalogue, app.databases (docs/KITWORK_DATABASE.md mục 1, lát 8): its connector and,
// for sqlite, its file.
type Hosted struct {
	Connector string
	Path      string
}

// hostedNames is the host's catalogue of the process, set as the manifest is read (SetHosted): one host a process, as
// app.history's days.
var hostedNames atomic.Pointer[map[string]Hosted]

// SetHosted is app.databases of the host's manifest, set as it is read; every catalog made after reads it.
func SetHosted(catalogue map[string]Hosted) { hostedNames.Store(&catalogue) }

// aloneProcess says the process runs one app alone, with no application.work (slice 6 of root
// docs/KITWORK_MODULE_WORK.md, 17/11/2026): no manifest declares the host's catalogue, so a name of it is one the app's
// runner has not declared yet, and its error says where (câu hỏi 8).
var aloneProcess atomic.Bool

// SetAlone says the process runs one app alone, with no application.work; a manifest read says it does not.
func SetAlone(alone bool) { aloneProcess.Store(alone) }

func hostedCatalogue() map[string]Hosted {
	if catalogue := hostedNames.Load(); catalogue != nil {
		return *catalogue
	}
	return nil
}

// place is where an entry's file is: the app's own, or the host's sqlite file its name is. A name the catalogue has not
// is an error naming those it has; one on postgresql or mysql is not served (the database's tables and statements run on
// KitDB and SQLite); a schema of a name is not either.
func (c *Catalog) place(entry *Entry) value.Value {
	if entry.Name == "" {
		entry.Path = filepath.Join(c.root, filepath.FromSlash(entry.File()))
		return value.Value{}
	}
	hosted, ok := c.hosted[entry.Name]
	if !ok {
		names := make([]string, 0, len(c.hosted))
		for name := range c.hosted {
			names = append(names, name)
		}
		sort.Strings(names)
		if aloneProcess.Load() {
			// the app runs alone: the host's databases are its runner's, declared by an application.work beside it
			return value.Fail(value.InvalidArgumentCode, "connection("+strconv.Quote(entry.Name)+") là database của host: chạy một mình thì không có. Thêm application.work cạnh module.work: app.databases({ "+entry.Name+": { sqlite: \"./"+entry.Name+".db\" } })")
		}
		have := "app.databases của application.work không khai tên nào"
		if len(names) > 0 {
			have = "có: " + strings.Join(names, ", ")
		}
		return value.Fail(value.InvalidArgumentCode, "connection("+strconv.Quote(entry.Name)+"): host không có database tên đó; "+have)
	}
	if hosted.Connector != "sqlite" {
		return value.Fail(value.UnsupportedCode, "connection("+strconv.Quote(entry.Name)+") là "+hosted.Connector+": bảng, câu lệnh và kitwork migrate của connection() hôm nay chạy trên KitDB và SQLite")
	}
	if entry.Schema != "" {
		return value.Fail(value.UnsupportedCode, label(entry.Name, entry.Schema)+": schema của một tên của host chưa có; khai bảng trên connection("+strconv.Quote(entry.Name)+")")
	}
	entry.Path = hosted.Path
	return value.Value{}
}

// find is the entry the declaring pass made of this .tables(…), by its place: the same file run again because
// a site imports it. The run's own structs get their lazy foreign keys resolved, as the declaration's did, so
// their parse knows each field's kind.
func (c *Catalog) find(name, schemaName string, written value.Value, where string) value.Value {
	for _, entry := range c.entries {
		if entry.Name == name && entry.Schema == schemaName && entry.Where == where {
			for _, key := range written.Keys() {
				if made, ok := written.Get(key).Host().(*schema.Struct); ok {
					fields := made.TableFields()
					for i := range fields {
						if failure := fields[i].Resolve(); failure.IsInvalid() {
							return failure
						}
					}
				}
			}
			return value.Struct(&Handle{entry: entry, catalog: c})
		}
	}
	return value.Fail(value.UnsupportedCode, label(name, schemaName)+".tables(…) ở "+where+" không được khai lúc app boot: database khai ở cấp file của một file .work mà boot đọc, không trong thư mục chấm hay gạch dưới")
}

// declare checks and records one .tables({ … }) of the declaring pass.
func (c *Catalog) declare(name, schemaName string, written value.Value, where string) value.Value {
	for _, entry := range c.entries {
		if entry.Name == name && entry.Schema == schemaName {
			if entry.Where == where {
				return c.find(name, schemaName, written, where) // the same declaration, run again by a file that imports it
			}
			return value.Fail(value.InvalidArgumentCode, label(name, schemaName)+" khai hai lần, ở "+entry.Where+" và ở "+where+": mỗi database và schema khai đúng một lần trong app")
		}
	}
	entry := &Entry{Name: name, Schema: schemaName, Where: where}
	if failure := c.place(entry); failure.IsInvalid() {
		return failure
	}
	seen := map[string]string{}
	mine := map[*schema.Struct]string{}
	for _, key := range written.Keys() {
		if problem := tableName(key); problem != "" {
			return value.Fail(value.InvalidArgumentCode, problem)
		}
		if other, ok := seen[strings.ToLower(key)]; ok {
			return value.Fail(value.InvalidArgumentCode, "bảng "+key+" khai hai lần: "+other+" là cùng tên, tên bảng không phân biệt chữ hoa, thường")
		}
		seen[strings.ToLower(key)] = key
		made, ok := written.Get(key).Host().(*schema.Struct)
		if !ok {
			return value.Fail(value.InvalidArgumentCode, "bảng "+key+" là một struct: .tables({ "+key+": struct({ … }) })")
		}
		if other, ok := c.placed[made]; ok {
			return value.Fail(value.InvalidArgumentCode, "bảng "+key+" là struct của bảng "+other+": một struct một bảng; hai bảng giống nhau là hai struct dùng chung trường qua const")
		}
		if other, ok := mine[made]; ok {
			return value.Fail(value.InvalidArgumentCode, "bảng "+key+" là struct của bảng "+other+": một struct một bảng")
		}
		mine[made] = key
		entry.Tables = append(entry.Tables, Table{Name: key, Struct: made, Fields: made.TableFields()})
	}
	for i := range entry.Tables {
		if failure := checkTable(&entry.Tables[i], mine); failure.IsInvalid() {
			return failure
		}
	}
	for made, table := range mine {
		c.placed[made] = label(name, schemaName) + "." + table
	}
	c.entries = append(c.entries, entry)
	return value.Struct(&Handle{entry: entry, catalog: c})
}

// tableName is why key is no table's name, "" when it is one.
func tableName(key string) string {
	lower := strings.ToLower(key)
	switch {
	case key == "":
		return "tên bảng không rỗng"
	case lower == "session":
		return "session là tên dành riêng: tham số session của policy là phiên; bảng lưu phiên là sessions"
	case strings.HasPrefix(lower, "kitwork_") || strings.HasPrefix(lower, "_kitwork_"):
		return key + ": tiền tố kitwork_ hay _kitwork_ (viết hoa hay thường) là của bảng engine"
	case strings.HasPrefix(lower, "sqlite_"):
		return key + ": tiền tố sqlite_ là của SQLite"
	case len(key) > 63:
		return key + ": tên bảng dài tối đa 63 ký tự, như Postgres"
	}
	return ""
}

// checkTable holds one table to the rules of a table (docs/schema.md mục 2, the first rule): one key, a nested
// field through json(…), a foreign key to a key or a unique column of a table of the same declaration. mine
// are the structs of that declaration, by table.
func checkTable(table *Table, mine map[*schema.Struct]string) value.Value {
	keys := 0
	searchable := false
	for i := range table.Fields {
		f := &table.Fields[i]
		at := table.Name + "." + f.Name
		if f.Key {
			keys++
		}
		if f.Searchable {
			searchable = true
		}
		switch {
		case f.Nested:
			return value.Fail(value.InvalidArgumentCode, at+": struct lồng hay array() trong bảng đi qua json(…): json(address), json(array(string()))")
		case f.Group != "" && f.Nullable:
			return value.Fail(value.InvalidArgumentCode, at+": cột trong nhóm .unique(\""+f.Group+"\") không được .nullable(): SQL coi hai null là khác nhau nên để lọt dòng trùng")
		case f.Generate != "" && f.HasDefault:
			return value.Fail(value.InvalidArgumentCode, at+": khoá id(…) do engine sinh, không có .default()")
		}
		if failure := f.Resolve(); failure.IsInvalid() {
			if failure.Code() != value.InvalidArgumentCode {
				return failure // the lazy foreign key's function failed: its own failure
			}
			return value.Fail(value.InvalidArgumentCode, at+": "+failure.Headline())
		}
		if f.ForeignTarget != nil {
			if _, ok := mine[f.ForeignTarget]; !ok {
				return value.Fail(value.InvalidArgumentCode, at+": khoá ngoại trỏ tới một struct không có trong tables({ … }) này; khoá ngoại không đi qua database hay schema khác")
			}
		}
	}
	if keys == 0 {
		return value.Fail(value.InvalidArgumentCode, "bảng "+table.Name+" không có khoá: thêm id: id(), hay .key() trên một cột")
	}
	if searchable {
		for _, f := range table.Fields {
			if f.Name == "score" || f.Name == "snippet" {
				return value.Fail(value.InvalidArgumentCode, table.Name+"."+f.Name+": bảng có cột .searchable() không có cột tên score hay snippet, hai trường matches thêm vào dòng")
			}
		}
	}
	return value.Value{}
}
