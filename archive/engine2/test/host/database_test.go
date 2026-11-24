package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// shopTables is the app's database for the tests of kitwork/database: every word of a table, a lazy foreign key
// pointing at its own table.
const shopTables = `import { struct, id, string, uuid, decimal, timestamp, foreign } from "kitwork/schema";
import { connection } from "kitwork/database";

export const users = struct({
  id: id(),
  email: string().max(254).email().unique(),
  name: string().min(1).max(100).index(),
  passwordHash: string().secret(),
  createdAt: timestamp().created()
});

export const orders = struct({
  id: id("uuid"),
  userId: foreign(users.id).readonly(),
  paymentId: uuid().readonly().nullable(),
  amount: decimal(2).min(0).index("byAmount"),
  note: string().searchable().nullable(),
  updatedAt: timestamp().updated()
});

export const employees = struct({
  code: string().key(),
  managerCode: foreign(() => employees.code).nullable().cascade()
});

export const db = connection().tables({ users, orders, employees });
`

// The app's core/schema/ is declared once as the app boots, before any site (docs/schema.md mục 2): every
// word of a table means something on a struct inside tables({ … }), a foreign key takes its target's kind, and
// two sites importing the schema file run it again and find the same declaration. A struct of a table parses
// what comes from outside without its engine-made columns. A statement on a database not made yet says so.
//
// CONTROL (06/11/2026): with Catalog.find not matching a later run to the declaration by its place, the second
// site's boot said "connection().tables(…) ở core/schema/shop.work:… không được khai lúc app boot".
func TestTheAppDeclaresItsTables(t *testing.T) {
	route := routerImport + `import { db, users, employees } from "app/core/schema/shop.work";
router.post((context) => {
  const { value, error } = safe(() => users.parse(context.request.json()));
  return context.json(error ? { message: error.message } : value);
});
`
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": shopTables,
		"apps/shop/shop.test/router.work": route,
		"apps/shop/shop.test/list/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const { error } = safe(() => db.list((users) => users));
  return context.text(error?.message);
});
`,
		"apps/shop/shop.test/boss/router.work": routerImport + `import { employees } from "app/core/schema/shop.work";
router.post((context) => context.json(employees.parse(context.request.json())));
`,
		"apps/shop/other.test/router.work": route,
	})
	app := boot(t, filepath.Join(folder, "application.work"))

	status, got, body := postJSON(t, app, "/", `{"id":7,"email":"an@x.vn","name":"An","passwordHash":"h","createdAt":1}`)
	if status != 200 || got["email"] != "an@x.vn" || got["passwordHash"] != "h" {
		t.Fatalf("POST /: %d %s", status, body)
	}
	if _, id := got["id"]; id {
		t.Errorf("parse took the key the engine makes: %s", body)
	}
	if _, created := got["createdAt"]; created {
		t.Errorf("parse took .created(): %s", body)
	}
	if other := app.request(t, "POST", "other.test", "/", strings.NewReader(`{"email":"b@x.vn","name":"B","passwordHash":"h"}`), "Content-Type", "application/json"); other.status != 200 || !strings.Contains(other.body, `"email":"b@x.vn"`) {
		t.Errorf("the second site, which runs the schema file again: %d %s", other.status, other.body)
	}
	if got := app.get(t, "shop.test", "/list"); !strings.Contains(got.body, "database .database/application/kitwork.db chưa có: chạy kitwork migrate") {
		t.Errorf("GET /list: %d %q", got.status, got.body)
	}
	// the lazy foreign key took the kind of employees.code, a string, in the site's own run of the file
	status, got, body = postJSON(t, app, "/boss", `{"code":"A1","managerCode":"B2"}`)
	if status != 200 || got["managerCode"] != "B2" {
		t.Errorf("POST /boss: %d %s", status, body)
	}
}

// A declaration written wrong stops the app's boot, saying where (docs/schema.md mục 2 and 5).
//
// CONTROL (06/11/2026): with checkTable not counting keys, the table without a key booted; with declare not
// keeping the struct each table is, one struct made two tables.
func TestADeclarationWrittenWrongStopsTheBoot(t *testing.T) {
	head := "import { struct, id, string, integer, uuid, json, foreign } from \"kitwork/schema\";\nimport { connection } from \"kitwork/database\";\n"
	schemaFile := func(text string) map[string]string {
		return map[string]string{
			"apps/shop/core/schema/shop.work": head + text,
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		}
	}
	users := "const users = struct({ id: id(), email: string().unique(), name: string() });\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no key", schemaFile("const notes = struct({ body: string() });\nexport const db = connection().tables({ notes });\n"),
			"bảng notes không có khoá: thêm id: id(), hay .key() trên một cột"},
		{"two keys", schemaFile("const notes = struct({ id: id(), code: string().key() });\n"), "struct có hai khoá, id và code"},
		{"a UUID key written with .key()", schemaFile("const notes = struct({ id: uuid().key() });\n"), "khoá là UUID viết id(\"uuid\")"},
		{"a foreign key to a plain column", schemaFile(users + "const orders = struct({ id: id(), who: foreign(users.name) });\n"),
			"khoá ngoại trỏ tới khoá (id(…), .key()) hay một cột .unique()"},
		{"a foreign key out of the declaration", schemaFile(users + "const orders = struct({ id: id(), userId: foreign(users.id) });\nexport const db = connection().tables({ orders });\nexport const other = connection().schema(\"billing\").tables({ users });\n"),
			"orders.userId: khoá ngoại trỏ tới một struct không có trong tables({ … }) này"},
		{"a nested field", schemaFile("const notes = struct({ id: id(), tags: json(), parts: struct({ a: string() }) });\nexport const db = connection().tables({ notes });\n"),
			"notes.parts: struct lồng hay array() trong bảng đi qua json(…)"},
		{"a transform not translated", schemaFile("const notes = struct({ id: id(), body: string().transform((v) => v.replace(\"a\", \"b\")) });\nexport const db = connection().tables({ notes });\n"),
			"notes.body: transform chưa dịch được sang SQL"},
		{"the session table", schemaFile(users + "export const db = connection().tables({ session: users });\n"), "session là tên dành riêng"},
		{"an engine's table", schemaFile(users + "export const db = connection().tables({ kitwork_users: users });\n"), "kitwork_users: tiền tố kitwork_"},
		{"one struct two tables", schemaFile(users + "export const db = connection().tables({ users, people: users });\n"), "một struct một bảng"},
		{"a database twice", map[string]string{
			"apps/shop/core/schema/a.work":    head + users + "export const a = connection().tables({ users });\n",
			"apps/shop/core/schema/b.work":    head + "const notes = struct({ id: id() });\nexport const b = connection().tables({ notes });\n",
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		}, "connection() khai hai lần, ở core/schema/a.work:"},
		{"connection(application)", schemaFile(users + "export const db = connection(\"application\").tables({ users });\n"), "database của app là connection() không tên"},
		{"schema public", schemaFile(users + "export const db = connection().schema(\"public\").tables({ users });\n"), "public là schema mặc định"},
		{"a policy", schemaFile(users + "export const db = connection().policy((users, session) => true).tables({ users });\n"), ".policy(…) chưa có trong engine2: lát 5"},
		{"a nullable column of a group", schemaFile("const days = struct({ id: id(), link: integer().unique(\"linkDay\").nullable(), day: string().unique(\"linkDay\") });\nexport const db = connection().tables({ days });\n"),
			"days.link: cột trong nhóm .unique(\"linkDay\") không được .nullable()"},
		{"cascade without a foreign key", schemaFile("const notes = struct({ id: id(), body: string().cascade() });\n"), ".cascade() chỉ đi sau foreign(…)"},
		{"connection() in router.work", map[string]string{
			"apps/shop/shop.test/router.work": "import { connection } from \"kitwork/database\";\n" + routerImport + "const db = connection();\nrouter.get((context) => context.text(\"a\"));\n",
		}, "a file with a role: a database is declared at the top level of a plain .work file of the app"},
		{"the router in a schema file", schemaFile(routerImport + "router.get((context) => context.text(\"a\"));\n"), "only router.work and config.work import it, a door is declared in router.work"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
