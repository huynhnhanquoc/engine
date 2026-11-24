package host_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// joinSchema is the database of the statements on several tables: orders point to users, payments to orders,
// messages twice to users; leads to no one.
const joinSchema = `import { struct, id, string, decimal, boolean, foreign } from "kitwork/schema";
import { connection } from "kitwork/database";

export const users = struct({
  id: id(),
  email: string(),
  name: string(),
  active: boolean(),
  role: string(),
  passwordHash: string().secret()
});
export const orders = struct({
  id: id(),
  userId: foreign(users.id),
  item: string(),
  day: string(),
  amount: decimal(2),
  note: string().nullable()
});
export const payments = struct({ id: id(), orderId: foreign(orders.id), status: string(), paid: decimal(2) });
export const leads = struct({ id: id(), email: string(), source: string() });
export const messages = struct({ id: id(), senderId: foreign(users.id), receiverId: foreign(users.id), text: string() });

export const db = connection().tables({ users, orders, payments, leads, messages });
`

var joinRows = []string{
	`INSERT INTO users (email, name, active, role, passwordHash) VALUES ('a@x.vn', 'An', 1, 'vip', 'h1'), ('b@x.vn', 'Bình', 1, 'thường', 'h2'), ('c@x.vn', 'Chi', 0, 'thường', 'h3'), ('d@x.vn', 'Dũng', 1, 'thường', 'h4')`,
	`INSERT INTO orders (userId, item, day, amount, note) VALUES (1, 'Cà phê sữa', '2026-11-05', 3500050, NULL), (1, 'Trà đào', '2026-11-06', 4500000, 'giao gấp'), (2, 'Cà phê đen', '2026-11-06', 2500000, 'Ít đá'), (3, 'BÁNH MÌ', '2026-11-06', 2000000, NULL)`,
	`INSERT INTO payments (orderId, status, paid) VALUES (1, 'ok', 3500050), (2, 'chờ', 0), (3, 'ok', 2500000)`,
	`INSERT INTO leads (email, source) VALUES ('a@x.vn', 'ads'), ('z@x.vn', 'seo')`,
	`INSERT INTO messages (senderId, receiverId, text) VALUES (1, 2, 'chào'), (2, 1, 'ừ')`,
}

// joinApp boots an app whose database is joinSchema's, migrated and holding joinRows, with a route for each of
// routes: its path, the statements it answers as JSON.
func joinApp(t *testing.T, routes map[string]string) *booted {
	t.Helper()
	files := map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": joinSchema,
	}
	for path, body := range routes {
		files["apps/shop/shop.test/"+path+"router.work"] = routerImport + `import { db } from "app/core/schema/shop.work";
import { sum, count, max } from "kitwork/database";
router.get((context) => {
` + body + `
});
`
	}
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder)))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range joinRows {
		if _, err := db.Exec(row); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	return boot(t, filepath.Join(folder, "application.work"))
}

// answer reads a route's JSON.
func answer(t *testing.T, app *booted, path string) map[string]any {
	t.Helper()
	got := app.get(t, "shop.test", path)
	if got.status != 200 {
		t.Fatalf("GET %s: %d %s; log: %s", path, got.status, got.body, app.log.String())
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(got.body), &read); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, got.body)
	}
	return read
}

// expect compares each key of read with want, as JSON.
func expect(t *testing.T, read map[string]any, want map[string]string) {
	t.Helper()
	for key, text := range want {
		var wanted any
		if err := json.Unmarshal([]byte(text), &wanted); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if jsonText(read[key]) != jsonText(wanted) {
			t.Errorf("%s = %s, want %s", key, jsonText(read[key]), jsonText(wanted))
		}
	}
}

// ids are the ids of a list of rows, or of one table's rows in a list of nested rows.
func ids(read any, table string) []float64 {
	out := []float64{}
	rows, _ := read.([]any)
	for _, row := range rows {
		item, _ := row.(map[string]any)
		if table != "" {
			item, _ = item[table].(map[string]any)
		}
		if item == nil {
			out = append(out, -1)
			continue
		}
		id, _ := item["id"].(float64)
		out = append(out, id)
	}
	return out
}

// A statement on one table takes what JavaScript writes on its columns (docs/KITWORK_DATABASE.md mục 2): includes,
// startsWith, endsWith on a string, letter for letter; toLowerCase as Unicode lowers (the engine's, not SQLite's,
// which lowers ASCII only); + - * / between a column and a number, a decimal as its number, / never integer
// division; c ? a : b, a branch narrowed from null; sum, avg, min, max of a column, 0 and null with no row; a list's
// page; null last in either order.
//
// CONTROL (06/11/2026): with lower as SQLite's, lowered was 0 ("BÁNH MÌ" kept its Á); with / as SQLite's on two
// integer columns, half was 0; without NULLS LAST, notes were [1 4 2 3], the nulls first.
func TestAStatementComputesAsJavaScript(t *testing.T) {
	app := joinApp(t, map[string]string{
		"": `  const text = "bánh";
  return context.json({
    coffee: db.list((orders) => orders.item.includes("Cà phê")),
    starts: db.count((orders) => orders.item.startsWith("Cà")),
    empty: db.count((orders) => orders.item.startsWith("")),
    ends: db.count((orders) => orders.item.endsWith("sữa")),
    lowered: db.count((orders) => orders.item.toLowerCase().includes(text)),
    urgent: db.count((orders) => orders.note !== null && orders.note.includes("gấp")),
    doubled: db.list((orders) => orders.amount * 2 > 60000),
    quarter: db.count((orders) => orders.amount / 8 === 3125),
    half: db.count((orders) => orders.userId / orders.id === 0.5),
    less: db.count((orders) => orders.amount - 20000 < 1),
    negative: db.count((orders) => -orders.amount < -40000),
    either: db.count((orders) => orders.note === null ? orders.amount > 30000 : orders.item.includes("Trà")),
    ice: db.count((orders) => orders.note === null ? false : orders.note.includes("đá")),
    total: db.sum((orders) => orders.amount),
    today: db.where((orders) => orders.day === "2026-11-06").sum((orders) => orders.amount),
    average: db.avg((orders) => orders.amount),
    least: db.min((orders) => orders.item),
    most: db.max((orders) => orders.amount),
    none: db.where((orders) => orders.day === "x").sum((orders) => orders.amount),
    noAverage: db.where((orders) => orders.day === "x").avg((orders) => orders.amount),
    page: db.ascending((orders) => orders.id).list({ page: 2, limit: 3 }),
    notes: db.ascending((orders) => orders.note).list(),
    notesDown: db.descending((orders) => orders.note).list()
  });`,
	})
	read := answer(t, app, "/")
	expect(t, read, map[string]string{
		"starts": "2", "empty": "4", "ends": "1", "lowered": "1", "urgent": "1", "quarter": "1", "less": "1",
		"negative": "1", "half": "1", "either": "2", "ice": "1", "total": "125000.5", "today": "90000", "average": "31250.125",
		"least": `"BÁNH MÌ"`, "most": "45000", "none": "0", "noAverage": "null",
	})
	for key, want := range map[string][]float64{"coffee": {1, 3}, "doubled": {1, 2}, "page": {4}, "notes": {2, 3, 1, 4}, "notesDown": {3, 2, 1, 4}} {
		if got := ids(read[key], ""); jsonText(got) != jsonText(want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// select says a row's shape, group makes a row of each key (docs/KITWORK_DATABASE.md mục 2): a value made of
// columns, a value from outside read off the lambda; sum and count of each group, a where after group as HAVING, an
// order by an aggregate, the groups by their key when no order says; a select of aggregates only one row; a count
// of groups.
//
// CONTROL (06/11/2026): with a where after group put in WHERE, the boot stopped at count() in where.
func TestSelectAndGroupShapeTheRows(t *testing.T) {
	app := joinApp(t, map[string]string{
		"": `  const label = "đơn";
  return context.json({
    one: db.select((orders) => ({ item: orders.item, doubled: orders.amount * 2, size: orders.amount > 30000 ? "lớn" : "nhỏ", tag: label }))
      .where((orders) => orders.id === 1).first(),
    days: db.group((orders) => orders.day).select((orders) => ({ day: orders.day, total: sum(orders.amount), n: count() })).list(),
    busy: db.group((orders) => orders.day).select((orders) => ({ day: orders.day })).where((orders) => count() > 1).list(),
    ranked: db.group((orders) => orders.day).select((orders) => ({ day: orders.day })).descending((orders) => sum(orders.amount)).list(),
    all: db.select((orders) => ({ total: sum(orders.amount), most: max(orders.amount), n: count() })).first(),
    groups: db.group((orders) => orders.day).count()
  });`,
	})
	read := answer(t, app, "/")
	expect(t, read, map[string]string{
		"one":    `{"item":"Cà phê sữa","doubled":70001,"size":"lớn","tag":"đơn"}`,
		"days":   `[{"day":"2026-11-05","total":35000.5,"n":1},{"day":"2026-11-06","total":90000,"n":3}]`,
		"busy":   `[{"day":"2026-11-06"}]`,
		"ranked": `[{"day":"2026-11-06"},{"day":"2026-11-05"}]`,
		"all":    `{"total":125000.5,"most":45000,"n":4}`,
		"groups": "2",
	})
}

// Tables join by their foreign key, each to the one before it, or by what join says (docs/KITWORK_DATABASE.md mục
// 2, "Nhiều bảng"): without select a row nests each table; a spread, a column of another table, a table's row; a
// .secret() column only when a select names it; two keys to one table, join picks; a table compared with null is a
// LEFT JOIN, its row null, a condition in join kept in ON; [orders] the many side by a second query, [] for none.
//
// CONTROL (06/11/2026): with the many side's rows given to no row, people were An:[] Bình:[] Dũng:[]; with a LEFT
// JOIN written as JOIN, paid lost order 4, okOnly orders 2 and 4, and missing was [].
func TestTablesJoinByTheirForeignKey(t *testing.T) {
	app := joinApp(t, map[string]string{
		"": `  return context.json({
    vip: db.list((orders, users) => users.role === "vip"),
    named: db.select((orders, users) => ({ ...orders, customer: users.name }))
      .where((orders, users) => users.active === true && orders.day === "2026-11-06").list(),
    child: db.select((orders, users) => ({ item: orders.item, user: users })).where((orders) => orders.id === 3).first(),
    hash: db.select((users) => ({ hash: users.passwordHash })).first((users) => users.id === 1),
    leads: db.join((users, leads) => users.email === leads.email).list(),
    said: db.join((messages, users) => messages.senderId === users.id).select((messages, users) => ({ text: messages.text, from: users.name })).list(),
    paid: db.select((orders, payments) => ({ id: orders.id, paid: payments === null ? 0 : payments.paid })).list(),
    okOnly: db.join((orders, payments) => payments.status === "ok").select((orders, payments) => ({ id: orders.id, paid: payments === null ? -1 : payments.paid })).list(),
    missing: db.list((orders, payments) => payments === null),
    people: db.select((users, orders) => ({ name: users.name, orders: [orders] })).where((users) => users.active === true).list()
  });`,
	})
	read := answer(t, app, "/")
	if got := ids(read["vip"], "orders"); jsonText(got) != "[1,2]" {
		t.Errorf("vip orders = %v, want [1,2]", got)
	}
	if rows, _ := read["vip"].([]any); len(rows) > 0 {
		user, _ := rows[0].(map[string]any)["users"].(map[string]any)
		if user["name"] != "An" || user["passwordHash"] != nil {
			t.Errorf("vip's user = %v, want An without its passwordHash", user)
		}
	}
	expect(t, read, map[string]string{
		"named":   `[{"id":2,"userId":1,"item":"Trà đào","day":"2026-11-06","amount":45000,"note":"giao gấp","customer":"An"},{"id":3,"userId":2,"item":"Cà phê đen","day":"2026-11-06","amount":25000,"note":"Ít đá","customer":"Bình"}]`,
		"child":   `{"item":"Cà phê đen","user":{"id":2,"email":"b@x.vn","name":"Bình","active":true,"role":"thường"}}`,
		"hash":    `{"hash":"h1"}`,
		"leads":   `[{"users":{"id":1,"email":"a@x.vn","name":"An","active":true,"role":"vip"},"leads":{"id":1,"email":"a@x.vn","source":"ads"}}]`,
		"said":    `[{"text":"chào","from":"An"},{"text":"ừ","from":"Bình"}]`,
		"paid":    `[{"id":1,"paid":35000.5},{"id":2,"paid":0},{"id":3,"paid":25000},{"id":4,"paid":0}]`,
		"okOnly":  `[{"id":1,"paid":35000.5},{"id":2,"paid":-1},{"id":3,"paid":25000},{"id":4,"paid":-1}]`,
		"missing": `[{"orders":{"id":4,"userId":3,"item":"BÁNH MÌ","day":"2026-11-06","amount":20000,"note":null},"payments":null}]`,
	})
	people, _ := read["people"].([]any)
	var shape []string
	for _, person := range people {
		item := person.(map[string]any)
		shape = append(shape, item["name"].(string)+":"+jsonText(ids(item["orders"], "")))
	}
	if got := strings.Join(shape, " "); got != "An:[1,2] Bình:[3] Dũng:[]" {
		t.Errorf("people = %s, want An:[1,2] Bình:[3] Dũng:[]", got)
	}
}

// A detail with no row is the route's 404 (docs/KITWORK_DATABASE.md mục 5): uncaught, its NOT_FOUND answers Not
// Found, not a 500.
//
// CONTROL (06/11/2026): without NOT_FOUND's case in the site's answer, the route answered 500.
func TestADetailWithNoRowIsNotFound(t *testing.T) {
	app := joinApp(t, map[string]string{
		"": `  const id = Number(context.request.query("id"));
  return context.json(db.detail((orders) => orders.id === id));`,
	})
	if got := app.get(t, "shop.test", "/?id=2"); got.status != 200 || !strings.Contains(got.body, `"item":"Trà đào"`) {
		t.Errorf("detail of 2: %d %s", got.status, got.body)
	}
	if got := app.get(t, "shop.test", "/?id=9"); got.status != 404 {
		t.Errorf("detail of 9: %d %s, want 404", got.status, got.body)
	}
}

// A statement whose lambdas do not make one statement stops the boot where it is written (docs/KITWORK_DATABASE.md
// mục 5): a statement without its action; a table only a later lambda names; two tables without a foreign key, or
// with two, and no join; a LEFT JOIN's column read without narrowing; the main table compared with null; an object
// for the many side, [x] for the one side; a column neither key nor aggregated; an aggregate in where before group;
// select twice; a condition given to sum; a method on a nullable column not narrowed; a method not of a statement.
//
// CONTROL (06/11/2026): with checkChains not putting the whole statements together, the boot passed the table named
// late, the tables without a foreign key, the two foreign keys and the LEFT JOIN's read, among the rest.
func TestAStatementThatDoesNotHoldTogetherStopsTheBoot(t *testing.T) {
	for _, c := range []struct{ name, statement, want string }{
		{"no action", `db.where((orders) => orders.day === "x");`, "câu chưa kết thúc: cần list, first, detail, count, exists, sum"},
		{"a table named late", `return db.where((orders) => orders.id > 1).list((orders, users) => users.active);`, "users chưa có trong câu: lambda đầu tiên của câu nêu mọi bảng của nó"},
		{"no foreign key", `return db.list((users, leads) => leads.source === "ads");`, "users và leads không có khoá ngoại: viết điều kiện nối trong join"},
		{"two foreign keys", `return db.list((messages, users) => users.active);`, "messages và users có 2 khoá ngoại (messages.senderId, messages.receiverId): join chọn khoá"},
		{"a LEFT JOIN read", `return db.select((orders, payments) => ({ id: orders.id, paid: payments.paid })).where((orders, payments) => payments === null).list();`, "payments có thể không có dòng (LEFT JOIN vì so với null)"},
		{"the main table null", `return db.list((orders, payments) => orders === null);`, "orders là bảng chính của câu"},
		{"an object for many", `return db.select((users, orders) => ({ ...users, order: orders })).list();`, "orders là phía nhiều của users: gom các dòng bằng order: [orders]"},
		{"[x] for one", `return db.select((orders, users) => ({ ...orders, users: [users] })).list();`, "users là phía một của orders: viết users: users, không ngoặc vuông"},
		{"not grouped", `return db.group((orders) => orders.day).select((orders) => ({ item: orders.item, n: count() })).list();`, "orders.item không phải khoá của group"},
		{"aggregated alone", `return db.select((orders) => ({ item: orders.item, total: sum(orders.amount) })).list();`, "orders.item không gộp cạnh hàm gộp: thêm group((orders) => orders.item)"},
		{"where before group", `return db.where((orders) => sum(orders.amount) > 5).list();`, "sum(…) trong where trước group"},
		{"select twice", `return db.select((orders) => ({ a: orders.id })).select((orders) => ({ b: orders.id })).list();`, "select hai lần"},
		{"a condition to sum", `return db.sum((orders) => orders.amount > 5);`, "sum cần một cột, điều kiện ở where"},
		{"a nullable's method", `return db.list((orders) => orders.note.includes("x"));`, "orders.note là .nullable(): gọi includes sau khi thu hẹp khỏi null, orders.note !== null && orders.note.includes(…)"},
		{"a method not of a statement", `return db.list((orders) => orders.item.trim() === "x");`, "không dịch được orders.item.trim()"},
		{"search", `return db.list((orders) => orders.item.search("x") > 0);`, "search của JavaScript trả một vị trí: tìm toàn văn là matches"},
	} {
		got := bootError(t, map[string]string{
			"apps/shop/core/schema/shop.work": joinSchema,
			"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
import { sum, count } from "kitwork/database";
router.get((context) => {
  ` + c.statement + `
  return context.text("x");
});
`,
		})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
