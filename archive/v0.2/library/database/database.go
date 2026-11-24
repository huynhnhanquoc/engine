// Package database is "kitwork/database": connection(…).schema(…).tables({ … }), which makes the app's structs
// its tables (docs/schema.md mục 5, docs/database.md), and the statements on the handle it gives.
//
//	// app/.shared/schema/shop.work
//	import { struct, id, string, foreign } from "kitwork/schema";
//	import { connection } from "kitwork/database";
//
//	export const users = struct({ id: id(), email: string().max(254).unique() });
//	export const orders = struct({ id: id(), userId: foreign(users.id).readonly() });
//
//	export const db = connection().tables({ users, orders });
//
// The app's databases are declared once, as the app boots: the host runs every file of its .shared/schema/
// in turn (Declaring), and the Catalog keeps each database and its tables, the struct each table is and the
// place .tables(…) is written. A file importing a schema file runs it again as it boots (each import is
// bundled into its program), and that run finds the declaration by its place: one database, one catalog
// entry, however many files import it. Declaring opens nothing: a database is opened by its first statement.
//
// The read statements are translated as each file boots and run on the file (statement.go, plan.go, run.go);
// the writes (insert, update, …) are the next slices of docs/schema.md mục 8, and each is a failure where it is
// called until then, as policy, before, after and connect are.
package database

import (
	"context"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/module"
)

// Database is what the program imports.
type Database struct {
	Catalog   *Catalog
	Declaring bool          // the run is the host's pass over the app's files, which declares
	Where     func() string // the place of the call in progress: ".shared/schema/shop.work:12:3"
	// Written is the file writing the call in progress, from the app's root ("" at a request)
	Written func() string
}

// Freeze is what value.Freeze calls on a struct a site's file made as it booted: a Database keeps nothing a
// run changes, the Catalog is the app's.
func (d *Database) Freeze() {}

// Connection is connection() for the app's own database, connection("name") for a name of the host's
// app.databases; then .schema("name") picks a schema, and .tables({ … }) declares its tables.
func (d *Database) Connection(arguments ...value.Value) value.Value {
	if written := d.written(); written != "" && module.RoleFile(written) {
		return value.Fail(value.UnsupportedCode, "connection() is written in "+written+", a file with a role: a database is declared at the top level of a plain .work file of the app, and imported from there: import { db } from \"app/core/database.work\"")
	}
	name := ""
	switch len(arguments) {
	case 0:
	case 1:
		if !arguments[0].IsString() || arguments[0].Text() == "" {
			return value.Fail(value.InvalidArgumentCode, "connection nhận tên database viết bằng chuỗi: connection(\"warehouse\"); database của app là connection()")
		}
		name = arguments[0].Text()
		if problem := reservedConnection(name); problem != "" {
			return value.Fail(value.InvalidArgumentCode, problem)
		}
	default:
		return value.Fail(value.InvalidArgumentCode, "connection nhận một tên")
	}
	return value.Struct(&Connection{database: d, name: name})
}

// reservedConnection is why name is no name of connection("…"), "" when it may be one (docs/database.md).
func reservedConnection(name string) string {
	switch name {
	case "application":
		return "connection(\"application\") không có: database của app là connection() không tên"
	case "storage":
		return "\"storage\" là tên dành cho datastore() của kitwork/storage"
	case "files":
		return "\"files\" là tên dành cho nơi lưu file dùng chung"
	}
	return ""
}

func (d *Database) written() string {
	if d.Written == nil {
		return ""
	}
	return d.Written()
}

// Connect is connect({ postgresql: … }), a database outside the app, with query() only: not served yet.
func (d *Database) Connect(...value.Value) value.Value {
	return value.Fail(value.UnsupportedCode, "connect({ … }) chưa có trong engine2: database ngoài app đi sau các lát của database của app")
}

// The aggregates sum, count, avg, min and max mean something only inside a statement's lambda, which the
// statements' translation reads; they are never called.

func (d *Database) Sum(...value.Value) value.Value   { return aggregate("sum") }
func (d *Database) Count(...value.Value) value.Value { return aggregate("count") }
func (d *Database) Avg(...value.Value) value.Value   { return aggregate("avg") }
func (d *Database) Min(...value.Value) value.Value   { return aggregate("min") }
func (d *Database) Max(...value.Value) value.Value   { return aggregate("max") }

func aggregate(word string) value.Value {
	return value.Fail(value.UnsupportedCode, word+"(…) chỉ có nghĩa trong lambda của một câu gom nhóm: db.group(…).select((bảng) => ({ n: "+word+"(…) })); câu lệnh chưa có trong engine2")
}

// Connection is a database being declared: connection(name), then .schema(name).
type Connection struct {
	database *Database
	name     string
	schema   string // "" is public
	hooks    []hook // .before(…) and .after(…), in the order written (hooks.go)
}

// Freeze is what value.Freeze calls on a struct a site's file made as it booted: no step writes into one.
func (c *Connection) Freeze() {}

// Schema is .schema("billing"): the database's schema the tables are in, a file of its own beside kitwork.db
// (docs/database.md mục 1). Lowercase letters, digits and underscores, beginning with a letter; public is
// the default, written by leaving .schema out.
func (c *Connection) Schema(arguments ...value.Value) value.Value {
	if c.schema != "" || len(c.hooks) > 0 {
		return value.Fail(value.InvalidArgumentCode, ".schema(…) viết một lần, ngay sau connection(…), trước before, after")
	}
	if len(arguments) != 1 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, "schema nhận tên viết bằng chuỗi: connection().schema(\"billing\").tables({ … })")
	}
	name := arguments[0].Text()
	if problem := schemaName(name); problem != "" {
		return value.Fail(value.InvalidArgumentCode, problem)
	}
	next := *c
	next.schema = name
	return value.Struct(&next)
}

// schemaName is why name is no schema's name, "" when it is one (the old engine's rule, word for word).
func schemaName(name string) string {
	switch strings.ToLower(name) {
	case "":
		return "schema nhận tên viết bằng chuỗi: connection().schema(\"billing\").tables({ … })"
	case "public":
		return "public là schema mặc định: bỏ .schema(\"public\"), viết connection().tables({ … })"
	case "main", "temp":
		return name + " là tên SQLite giữ cho mình: đặt tên schema khác"
	case "kitwork":
		return "kitwork.db là file của schema public: đặt tên schema khác"
	}
	if len(name) > 63 {
		return "tên schema dài tối đa 63 ký tự, như Postgres"
	}
	for i, c := range name {
		letter := c >= 'a' && c <= 'z'
		if letter || i > 0 && (c >= '0' && c <= '9' || c == '_') {
			continue
		}
		return "tên schema viết chữ thường, số và gạch dưới, bắt đầu bằng chữ: \"billing\", \"sales_2026\""
	}
	return ""
}

// Tables is .tables({ users, orders }): each struct a table, named by its key. The host's pass over
// .shared/schema/ declares the database; any other run finds that declaration by the place it is written.
func (c *Connection) Tables(arguments ...value.Value) value.Value {
	written := argument(arguments, 0)
	if len(arguments) != 1 || !written.IsObject() || len(written.Keys()) == 0 {
		return value.Fail(value.InvalidArgumentCode, "tables nhận một object bảng: .tables({ users, orders })")
	}
	catalog := c.database.Catalog
	if catalog == nil {
		return value.Fail(value.UnsupportedCode, "connection().tables(…) runs as the app boots, at the top level of a plain .work file of the app")
	}
	where := c.database.Where()
	if c.database.Declaring {
		return attach(catalog.declare(c.name, c.schema, written, where), c.hooks)
	}
	return attach(catalog.find(c.name, c.schema, written, where), c.hooks)
}

// The words of a declaration engine2 does not serve yet fail where they are written.

func (c *Connection) Policy(...value.Value) value.Value { return later(".policy(…)") }

func later(word string) value.Value {
	return value.Fail(value.UnsupportedCode, word+" chưa có trong engine2: lát 5 của docs/schema.md mục 8")
}

// Handle is what .tables({ … }) gives: the database the program writes statements on, db.list(…) (run.go).
type Handle struct {
	entry   *Entry
	catalog *Catalog
	tx      *transaction // a transaction's handle, the callback's database (transaction.go)
	hooks   []hook       // the declaration's before and after (hooks.go)
	// waits is the context of the run that calls a statement (Attend): a read, a write, a transaction waits no
	// longer than the run
	waits context.Context
}

// Freeze is what value.Freeze calls on a handle a site's file made as it booted.
func (h *Handle) Freeze() {}

// Attend is the handle for one call by a run whose context is ctx (value.Attender): its statements read, write and
// hold a transaction no longer than the run, cancelled or past its time (15/11/2026: a read of the database, a write
// waiting for the writer, went on with no context, past the request's time and past a client gone). KitDB refuses a
// write whole once its context ends before it commits.
func (h *Handle) Attend(ctx context.Context) value.Value {
	attended := *h
	attended.waits = ctx
	return value.Struct(&attended)
}

// Attend is the statement for one call by a run whose context is ctx: its handle's (Handle.Attend), so a statement
// a site's file wrote as it booted reads within the run that runs it.
func (q *Query) Attend(ctx context.Context) value.Value {
	attended, handle := *q, *q.handle
	handle.waits = ctx
	attended.handle = &handle
	return value.Struct(&attended)
}

// MissingMember is a statement: each fails, saying the slice that brings it, until it is served.
func (h *Handle) MissingMember(name string) value.Value {
	switch name {
	case "limit":
		return value.Fail(value.TypeCode, "database không có limit: số dòng là list(n), db.where(…).list(20)")
	case "orderBy", "asc", "desc":
		return value.Fail(value.TypeCode, "database không có "+name+": thứ tự là ascending, descending, db.descending((orders) => orders.day)")
	case "groupBy":
		return value.Fail(value.TypeCode, "database không có groupBy: gom nhóm là group, db.group((orders) => orders.day)")
	case "matches", "search":
		return value.Fail(value.TypeCode, "db."+name+"(…) không có: tìm toàn văn viết trong điều kiện, db.list((posts) => posts.matches(q))")
	case "query":
		return value.Fail(value.UnsupportedCode, "db.query(…) chưa có trong engine2")
	}
	return value.Fail(value.TypeCode, "database không có "+name+": câu lệnh là list, first, count, insert, update, delete, … (docs/database.md mục 3)")
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}
