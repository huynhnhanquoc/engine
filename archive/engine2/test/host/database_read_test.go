package host_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// readSchema is the database the read statements are tested on.
const readSchema = `import { struct, id, string, uuid, decimal, boolean, timestamp, json, choice } from "kitwork/schema";
import { connection } from "kitwork/database";

export const orders = struct({
  id: id(),
  email: string(),
  status: string().index(),
  amount: decimal(2),
  paid: boolean(),
  gift: boolean().nullable(),
  note: string().nullable(),
  ref: uuid().nullable(),
  meta: json().nullable(),
  secretCode: string().secret().nullable(),
  createdAt: timestamp().created()
});

export const db = connection().tables({ orders });
`

// readRoutes write every read statement, each answering its value as JSON.
const readRoutes = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const status = context.request.query("status");
  const least = Number(context.request.query("least") ?? 0);
  const ids = [1, 3];
  const who = { email: "b@x.vn" };
  const missing = who.nothing;
  return context.json({
    all: db.list((orders) => orders),
    moi: db.list((orders) => orders.status === status),
    big: db.where((orders) => orders.amount >= least && orders.paid).descending((orders) => orders.amount).list(2),
    some: db.list((orders) => ids.includes(orders.id)),
    literal: db.count((orders) => [2, 3].includes(orders.id)),
    noNote: db.count((orders) => orders.note === null),
    notPaid: db.count((orders) => !orders.paid),
    either: db.count((orders) => orders.status === "giao" || orders.amount < 100),
    cheap: db.count((orders) => orders.amount < 100),
    noGift: db.count((orders) => !orders.gift),
    notes: db.count((orders) => ["gấp", null].includes(orders.note)),
    notX: db.count((orders) => !["x"].includes(orders.note)),
    notUrgent: db.count((orders) => orders.note !== "gấp"),
    narrowed: db.count((orders) => orders.note !== null && orders.note < "y"),
    orNull: db.count((orders) => orders.note === null || orders.note > "h"),
    noneUndefined: db.count((orders) => orders.note === missing),
    allUndefined: db.count((orders) => orders.note !== missing),
    first: db.first((orders) => orders.email === who.email),
    none: db.first((orders) => orders.email === "nobody"),
    count: db.count((orders) => orders),
    exists: db.exists((orders) => orders.status === "huy"),
    newest: db.descending((orders) => orders.createdAt).first((orders) => orders)
  });
});
`

func readApp(t *testing.T) *booted {
	t.Helper()
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": readSchema,
		"apps/shop/shop.test/router.work": readRoutes,
		"apps/shop/shop.test/detail/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const id = Number(context.request.query("id"));
  const { value, error } = safe(() => db.detail((orders) => orders.id === id));
  return context.json(error ? { code: error.code } : value);
});
`,
		"apps/shop/shop.test/wrong/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const status = 5;
  const { error } = safe(() => db.list((orders) => orders.status === status));
  return context.json({ code: error?.code, message: error?.message });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder)))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		`INSERT INTO orders (email, status, amount, paid, gift, note, ref, meta, secretCode, createdAt) VALUES ('a@x.vn', 'moi', 3500050, 1, NULL, NULL, '0192f3a4-5b6c-7d8e-9fa0-b1c2d3e4f5a6', '{"tags":["x"]}', 's1', '2026-11-06T01:00:00Z')`,
		`INSERT INTO orders (email, status, amount, paid, gift, note, ref, meta, secretCode, createdAt) VALUES ('b@x.vn', 'giao', 9900, 0, 1, 'gấp', NULL, NULL, 's2', '2026-11-06T02:00:00Z')`,
		`INSERT INTO orders (email, status, amount, paid, gift, note, ref, meta, secretCode, createdAt) VALUES ('c@x.vn', 'moi', 12000000, 1, 0, 'x', NULL, NULL, NULL, '2026-11-06T03:00:00Z')`,
	} {
		if _, err := db.Exec(row); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	return boot(t, filepath.Join(folder, "application.work"))
}

// The read statements run on the database's file (docs/schema.md mục 8, slice 3): a condition's ===, !==, <, >=, &&,
// ||, !, a boolean column alone, === null, values.includes(column), a value from outside the lambda read off its
// closure (a name, a member of one), never run; where, descending, a list's limit, the key last as the order; first
// and its null, count, exists, detail and its NOT_FOUND. A row reads as the struct declares it: a decimal its
// number, a boolean, a timestamp its milliseconds, json its value, null; a .secret() column never.
//
// CONTROL (06/11/2026): with bind not scaling a decimal, cheap (amount < 100, the file holding 9900) was 0; with
// decode not reading a timestamp's text, createdAt was "2026-11-06T01:00:00Z"; with a boolean column alone as = 1,
// noGift was 1 (NOT of null is null); without IS NOT NULL before IN on a nullable column, notX was 1; with an undefined
// from outside as null, noneUndefined was 1; without narrowing, the boot stopped at narrowed.
func TestTheReadStatementsRunOnTheFile(t *testing.T) {
	app := readApp(t)
	got := app.get(t, "shop.test", "/?status=moi&least=35000.5")
	if got.status != 200 {
		t.Fatalf("GET /: %d %s", got.status, got.body)
	}
	var read map[string]any
	if err := json.Unmarshal([]byte(got.body), &read); err != nil {
		t.Fatalf("%v: %s", err, got.body)
	}
	all, _ := read["all"].([]any)
	if len(all) != 3 {
		t.Fatalf("all: %v", read["all"])
	}
	first := all[0].(map[string]any)
	want := map[string]any{"id": 1.0, "email": "a@x.vn", "status": "moi", "amount": 35000.5, "paid": true, "note": nil,
		"ref": "0192f3a4-5b6c-7d8e-9fa0-b1c2d3e4f5a6", "createdAt": 1793926800000.0}
	for key, value := range want {
		if first[key] != value {
			t.Errorf("all[0].%s = %#v, want %#v", key, first[key], value)
		}
	}
	if meta, _ := first["meta"].(map[string]any); meta == nil || meta["tags"] == nil {
		t.Errorf("all[0].meta = %#v, want the JSON's value", first["meta"])
	}
	if _, secret := first["secretCode"]; secret {
		t.Errorf("a .secret() column was read: %v", first)
	}
	ids := func(key string) []float64 {
		var out []float64
		rows, _ := read[key].([]any)
		for _, row := range rows {
			out = append(out, row.(map[string]any)["id"].(float64))
		}
		return out
	}
	check := func(key string, got, want any) {
		t.Helper()
		if strings.TrimSpace(strings.ReplaceAll(jsonText(got), " ", "")) != jsonText(want) {
			t.Errorf("%s = %s, want %s", key, jsonText(got), jsonText(want))
		}
	}
	check("moi", ids("moi"), []float64{1, 3})
	check("big", ids("big"), []float64{3, 1})
	check("some", ids("some"), []float64{1, 3})
	check("literal", read["literal"], 2.0)
	check("noNote", read["noNote"], 1.0)
	check("notPaid", read["notPaid"], 1.0)
	check("either", read["either"], 1.0)
	check("cheap", read["cheap"], 1.0)
	// null as JavaScript (docs/KITWORK_DATABASE.md, "null theo JavaScript"): the notes are null, "gấp", "x"; the
	// gifts null, true, false
	check("noGift", read["noGift"], 2.0)
	check("notes", read["notes"], 2.0)
	check("notX", read["notX"], 2.0)
	check("notUrgent", read["notUrgent"], 2.0)
	check("narrowed", read["narrowed"], 2.0)
	check("orNull", read["orNull"], 2.0)
	check("noneUndefined", read["noneUndefined"], 0.0)
	check("allUndefined", read["allUndefined"], 3.0)
	check("count", read["count"], 3.0)
	check("exists", read["exists"], false)
	check("none", read["none"], nil)
	if who, _ := read["first"].(map[string]any); who == nil || who["id"] != 2.0 {
		t.Errorf("first = %v, want order 2", read["first"])
	}
	if newest, _ := read["newest"].(map[string]any); newest == nil || newest["id"] != 3.0 {
		t.Errorf("newest = %v, want order 3", read["newest"])
	}

	if got := app.get(t, "shop.test", "/detail?id=2"); !strings.Contains(got.body, `"email":"b@x.vn"`) {
		t.Errorf("detail of 2: %s", got.body)
	}
	if got := app.get(t, "shop.test", "/detail?id=9"); !strings.Contains(got.body, `"code":"NOT_FOUND"`) {
		t.Errorf("detail of 9: %s", got.body)
	}
	if got := app.get(t, "shop.test", "/wrong"); !strings.Contains(got.body, `"code":"INVALID_ARGUMENT"`) || !strings.Contains(got.body, "cột status là string(): giá trị ở router.work:5:66 không phải chữ") {
		t.Errorf("a value of another kind: %s", got.body)
	}
}

func jsonText(v any) string {
	text, _ := json.Marshal(v)
	return string(text)
}

// A statement written wrong stops the boot where it is written (docs/schema.md mục 8, slice 3): a table the
// database has not, a column its table has not, == of JavaScript's conversions, a computation on a column, a
// value from outside that is no name, a column of another kind compared, a column alone that is not a boolean, an
// order of something not a column, a table not of the database, an order on a .nullable() column not narrowed (JavaScript
// holds null < 100 true, SQL unknown). A body of one return is the expression it returns.
//
// A remainder is no word of a statement (+ - * / are).
//
// CONTROL (06/11/2026): with Prepare skipping the handles imported from a schema file, every statement written
// wrong booted, and the right ones answered 500.
func TestAStatementWrittenWrongStopsTheBoot(t *testing.T) {
	route := func(statement string) map[string]string {
		return map[string]string{
			"apps/shop/core/schema/shop.work": readSchema,
			"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const email = "a@x.vn";
  return context.json(` + statement + `);
});
`,
		}
	}
	for _, c := range []struct {
		name, statement, want string
	}{
		{"a table not there", `db.list((users) => users)`, "shop/shop.test/router.work: router.work:5:39: users không phải bảng của database này; có: orders"},
		{"a column not there", `db.list((orders) => orders.mail === email)`, "orders không có cột mail; có: id, email"},
		{"==", `db.list((orders) => orders.email == email)`, "== so có đổi kiểu của JavaScript: điều kiện viết ==="},
		{"a remainder", `db.list((orders) => orders.amount % 2 === 0)`, "phép % trong câu chưa có: + - * / giữa cột và số"},
		{"a call outside", `db.list((orders) => orders.email === email.trim())`, "giá trị từ ngoài lambda là một tên, hay thành phần của nó (user.id): đặt nó vào một const trước câu"},
		{"a literal of another kind", `db.list((orders) => orders.paid === "yes")`, "orders.paid là boolean(), yes không phải giá trị của nó"},
		{"a column not boolean alone", `db.list((orders) => orders.email)`, "orders.email là string(), không phải boolean(): so nó với một giá trị"},
		{"an order not a column", `db.descending((orders) => orders.amount * 2).list()`, "khoá là một cột hay một giá trị gộp: (orders) => orders.day"},
		{"a table not of the database", `db.list((orders, users) => orders)`, "users không phải bảng của database này; có: orders"},
		{"an order on a nullable column", `db.list((orders) => orders.note < email)`, "orders.note là .nullable(): so thứ tự sau khi thu hẹp khỏi null, orders.note !== null && orders.note < …"},
		{"narrowed the wrong way", `db.list((orders) => orders.note === null && orders.note < email)`, "orders.note là .nullable()"},
		{"a block", `db.list((orders) => { return orders; })`, ""},
	} {
		got := bootError(t, route(c.statement))
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: the boot ended with %q", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
