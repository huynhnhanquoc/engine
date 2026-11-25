package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// Files là các file database một tiến trình đang mở: mỗi file đúng một sqlite.Database (sqlitewire cho mỗi file một
// người ghi; mở hai lần là hai người ghi). Mở ở câu lệnh đầu đọc nó, đóng khi host dừng (Close).
type Files struct {
	mu   sync.Mutex
	open map[string]*sqlite.Database
}

// NewFiles là một sổ rỗng.
func NewFiles() *Files { return &Files{open: map[string]*sqlite.Database{}} }

// get là database của file path (tuyệt đối), mở lần đầu. File không có là lỗi: câu lệnh không bao giờ tạo database
// (docs/KITWORK_DATABASE.md, "Database là gốc"), migrate tạo.
func (f *Files) get(ctx context.Context, path, shown string) (*sqlite.Database, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if database, found := f.open[path]; found {
		return database, nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("the database %s does not exist: migrate makes it from the structs", shown)
	}
	database, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		return nil, fmt.Errorf("the database %s: %w", shown, err)
	}
	f.open[path] = database
	return database, nil
}

// Close đóng mọi file đã mở.
func (f *Files) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var problems []error
	for path, database := range f.open {
		if err := database.Close(); err != nil {
			problems = append(problems, err)
		}
		delete(f.open, path)
	}
	return errors.Join(problems...)
}

// Connection là connection() của một gốc (thư mục có .database/): gốc app cho code của site, thư mục manifest cho các
// file của host. schema "" là kitwork.db, một schema là file của nó cạnh đó (docs/KITWORK_DATABASE.md: billing.db).
type Connection struct {
	files  *Files
	root   string // thư mục trên đĩa
	label  string // gốc như lời báo nêu: "apps/03dt"
	schema string
}

// Library là thư viện "database" của một gốc: connection().
func Library(files *Files, root, label string) map[string]value.Value {
	return map[string]value.Value{
		"connection": javascript.NewNative("connection", 1, func(_ javascript.Run, arguments []value.Value) value.Value {
			if len(arguments) > 0 && !arguments[0].IsUndefined() {
				return value.Fail(value.TypeCode, `connection(%s): a database of the host's catalogue comes in a later slice of engine3; `+
					`connection() is the app's own`, describe(arguments[0]))
			}
			return value.Struct(&Connection{files: files, root: root, label: label})
		}),
	}
}

var (
	schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	tableName  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

func (c *Connection) Key(name string) value.Value {
	if name == "schema" || name == "tables" {
		return javascript.NewNative(name, 1, func(run javascript.Run, arguments []value.Value) value.Value {
			return c.Word(run, name, arguments)
		})
	}
	return value.Fail(value.TypeCode, "connection() has no word %q: schema, tables", name)
}

// Word là .schema("tên") hay .tables({ … }).
func (c *Connection) Word(_ javascript.Run, name string, arguments []value.Value) value.Value {
	argument := value.Undefined()
	if len(arguments) > 0 {
		argument = arguments[0]
	}
	switch name {
	case "schema":
		if c.schema != "" {
			return value.Fail(value.TypeCode, ".schema(…) is written once, right after connection()")
		}
		if !argument.IsString() || !schemaName.MatchString(argument.String()) || argument.String() == "kitwork" {
			return value.Fail(value.TypeCode, `.schema(name) takes a name in lower case, like "billing", got %s`, describe(argument))
		}
		copied := *c
		copied.schema = argument.String()
		return value.Struct(&copied)
	case "tables":
		return c.tables(argument)
	}
	return c.Key(name)
}

// tables là .tables({ orders, … }): mỗi khoá là tên một bảng, mỗi giá trị một struct.
func (c *Connection) tables(object value.Value) value.Value {
	if !object.IsObject() {
		return value.Fail(value.TypeCode, ".tables({ orders }) takes an object of structs, got %s", describe(object))
	}
	file := "kitwork.db"
	if c.schema != "" {
		file = c.schema + ".db"
	}
	h := &Handle{files: c.files, path: filepath.Join(c.root, ".database", "application", file),
		shown: strings.TrimPrefix(c.label+"/.database/application/"+file, "./"), tables: map[string]*Table{}}
	for _, name := range object.Keys() {
		item, _ := object.Get(name)
		s, ok := value.As[Struct](item)
		if !ok {
			return value.Fail(value.TypeCode, ".tables({ %s }): a table is a struct({ … }), got %s", name, describe(item))
		}
		if !tableName.MatchString(name) {
			return value.Fail(value.TypeCode, ".tables({ %s }): a table's name is letters, digits and _", name)
		}
		h.tables[name] = newTable(name, s)
		h.names = append(h.names, name)
	}
	if len(h.names) == 0 {
		return value.Fail(value.TypeCode, ".tables({ … }) names one table or more")
	}
	return value.Struct(h)
}

// Table là một bảng: các cột theo thứ tự struct viết, và Shape của các dòng nó đọc ra (mọi dòng mượn chung các khoá).
type Table struct {
	name    string
	columns []column
	shape   *value.Shape
	key     string // cột id(), "" khi bảng không có (thứ tự dự phòng là rowid)
}

type column struct {
	name string
	*Field
}

func newTable(name string, s *Struct) *Table {
	t := &Table{name: name}
	for i, field := range s.fields {
		t.columns = append(t.columns, column{s.names[i], field})
		if field.kind == "id" && t.key == "" {
			t.key = s.names[i]
		}
	}
	t.shape = value.NewShape(slices.Clone(s.names))
	return t
}

// column là cột tên name của t, hay nil.
func (t *Table) column(name string) *column {
	for i := range t.columns {
		if t.columns[i].name == name {
			return &t.columns[i]
		}
	}
	return nil
}

func (t *Table) names() string {
	names := make([]string, len(t.columns))
	for i, c := range t.columns {
		names[i] = c.name
	}
	return strings.Join(names, ", ")
}

// Handle là thứ .tables(…) trả: một file database và các bảng của nó. Bảng của một câu lệnh là tên tham số của hàm điều
// kiện: db.where((orders) => …) đọc bảng orders.
type Handle struct {
	files  *Files
	path   string // file trên đĩa
	shown  string // "apps/03dt/.database/application/kitwork.db"
	tables map[string]*Table
	names  []string
	// conditions, orders là bản dịch của mỗi hàm điều kiện, sắp xếp đã gặp (translate.go): *compiler.Function →
	// *condition, *orderColumn hay error
	conditions, orders sync.Map
	// statements là SQL của mỗi dạng câu đã chạy (statement.go)
	statements statements
	// database là file đã mở, sau câu đầu: các câu sau không qua khoá chung của Files
	database atomic.Pointer[sqlite.Database]
}

// statementWords là các chữ của một câu lệnh: mô tả (where, ascending, descending) và hành động (list, first, count).
var statementWords = []string{"where", "ascending", "descending", "list", "first", "count"}

func (h *Handle) Key(name string) value.Value { return (&Query{handle: h}).Key(name) }

// Word là chữ đầu của một câu (system.where(…), system.first(…)): câu rỗng của h nằm trên ngăn xếp, chỉ câu chữ ấy
// dựng ra được cấp phát.
func (h *Handle) Word(run javascript.Run, name string, arguments []value.Value) value.Value {
	q := Query{handle: h}
	return q.Word(run, name, arguments)
}

// open là file database của h, mở ở câu đầu (Files.get) rồi giữ trên h.
func (h *Handle) open(ctx context.Context) (*sqlite.Database, error) {
	if database := h.database.Load(); database != nil {
		return database, nil
	}
	database, err := h.files.get(ctx, h.path, h.shown)
	if err != nil {
		return nil, err
	}
	h.database.Store(database)
	return database, nil
}
