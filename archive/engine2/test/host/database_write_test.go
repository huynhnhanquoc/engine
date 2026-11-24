package host_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeSchema is the database the writes are tested on: users with two unique columns, a default, a secret and the
// times the engine fills; orders keyed by a UUID, their user readonly; notes keyed by a KitID; lines deleted with
// their order; clickDays counted by a unique group.
const writeSchema = `import { struct, id, string, decimal, timestamp, foreign, integer, choice, json, date } from "kitwork/schema";
import { connection } from "kitwork/database";

export const users = struct({
  id: id(),
  email: string().max(254).unique(),
  username: string().unique(),
  name: string().min(1),
  role: choice("member", "admin").default("member"),
  passwordHash: string().secret(),
  createdAt: timestamp().created(),
  updatedAt: timestamp().updated()
});
export const orders = struct({
  id: id("uuid"),
  userId: foreign(users.id).readonly(),
  item: string(),
  amount: decimal(2).min(0),
  note: string().nullable(),
  meta: json().nullable()
});
export const notes = struct({ id: id("kitwork"), text: string() });
export const lines = struct({ id: id(), orderId: foreign(orders.id).cascade(), qty: integer() });
export const clickDays = struct({ id: id(), linkId: integer().unique("linkDay"), day: date().unique("linkDay"), count: integer() });

export const db = connection().tables({ users, orders, notes, lines, clickDays });
`

func writeApp(t *testing.T, files map[string]string) *booted {
	t.Helper()
	files["application.work"] = manifest
	files["apps/shop/core/schema/shop.work"] = writeSchema
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	return boot(t, filepath.Join(folder, "application.work"))
}

const writeRoute = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const form = { email: "an@x.vn", username: "an", name: "An", hash: "h1" };
  const an = db.insert((users) => ({ email: form.email, username: form.username, name: form.name, passwordHash: form.hash }));
  const duplicate = safe(() => db.insert((users) => ({ email: "an@x.vn", username: "an2", name: "B", passwordHash: "h" })));
  const empty = safe(() => db.insert((users) => ({ email: "b@x.vn", username: "b", name: "", passwordHash: "h" })));
  const nobody = 99;
  const orphan = safe(() => db.insert((orders) => ({ userId: nobody, item: "x", amount: 1 })));
  const negative = safe(() => db.insert((orders) => ({ userId: an.id, item: "x", amount: -1 })));
  const meta = { size: "L" };
  const order = db.insert((orders) => ({ userId: an.id, item: "Cà phê", amount: 35000.5, meta }));
  db.insert((lines) => ({ orderId: order.id, qty: 2 }));
  const note = db.insert((notes) => ({ text: "x" }));
  const changed = db.where((orders) => orders.id === order.id).update((orders) => ({ amount: orders.amount + 1000, note: "gấp" }));
  const nothing = form.missing;
  const kept = db.where((orders) => orders.id === order.id).update((orders) => ({ note: nothing }));
  const updated = db.first((orders) => orders.id === order.id);
  const first = db.upsert((clickDays) => ({ linkId: 7, day: "2026-11-06", count: clickDays.count + 1 }));
  const second = db.upsert((clickDays) => ({ linkId: 7, day: "2026-11-06", count: clickDays.count + 1 }));
  const restricted = safe(() => db.delete((users) => users.id === an.id));
  const deleted = db.delete((orders) => orders.id === order.id);
  const renamed = db.where((users) => users.id === an.id).update((users) => ({ name: "An Nguyễn" }));
  return context.json({
    an, duplicate: duplicate.error, empty: empty.error, orphan: orphan.error, negative: negative.error,
    order, note, changed, kept, updated, first, second, restricted: restricted.error, deleted,
    lines: db.count((lines) => lines), orders: db.count((orders) => orders), renamed,
    renamedUser: db.first((users) => users.id === an.id)
  });
});
`

// The writes go through KitDB's writer (docs/KITWORK_DATABASE.md mục 2, docs/KITWORK_SCHEMA.md mục 4–6): an insert
// gives its row, the key the engine made (the rowid, a UUIDv7, a KitID), the default, the times, never the secret; a
// value is held to its column's rules before it is written; a unique column already there and a foreign key to no
// row are CONFLICT naming them; an update changes what its data writes, a column reading the row, undefined keeping
// a column; an upsert inserts then adds to the row there; a delete of a row others point to is CONFLICT, with
// .cascade() the rows pointing go with it.
//
// CONTROL (06/11/2026): with bindWrite not holding a value to its rules, empty and negative were written (and an
// order more); with remove not deleting the rows of a .cascade() first, the order's delete was CONFLICT; with the
// upsert's branch that inserts reading the column as null, the first upsert was INVALID_INPUT, count: cần giá trị.
func TestTheWritesGoThroughTheWriter(t *testing.T) {
	app := writeApp(t, map[string]string{"apps/shop/shop.test/router.work": writeRoute})
	read := answer(t, app, "/")
	an, _ := read["an"].(map[string]any)
	if an["id"] != 1.0 || an["email"] != "an@x.vn" || an["role"] != "member" || an["passwordHash"] != nil {
		t.Errorf("an = %v, want id 1, its email, role member, no passwordHash", an)
	}
	if created, _ := an["createdAt"].(float64); created < 1.7e12 || an["updatedAt"] != an["createdAt"] {
		t.Errorf("an's times = %v, %v, want now in milliseconds, both the same", an["createdAt"], an["updatedAt"])
	}
	failure := func(key, code, says string) {
		t.Helper()
		got, _ := read[key].(map[string]any)
		if got == nil || got["code"] != code || !strings.Contains(got["message"].(string), says) {
			t.Errorf("%s = %v, want %s saying %q", key, read[key], code, says)
		}
	}
	failure("duplicate", "CONFLICT", "email đã có")
	failure("empty", "INVALID_INPUT", "name: cần ít nhất 1 ký tự")
	failure("orphan", "CONFLICT", "khoá ngoại userId của orders")
	failure("negative", "INVALID_INPUT", "amount: nhỏ nhất là 0")
	failure("restricted", "CONFLICT", "còn 1 dòng của orders trỏ tới qua orders.userId")
	order, _ := read["order"].(map[string]any)
	if id, _ := order["id"].(string); !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Errorf("order's id = %v, want a UUIDv7", order["id"])
	}
	if meta, _ := order["meta"].(map[string]any); meta["size"] != "L" {
		t.Errorf("order's meta = %v, want its JSON", order["meta"])
	}
	if note, _ := read["note"].(map[string]any); len(note["id"].(string)) != 36 {
		t.Errorf("note's id = %v, want a KitID of 36 characters", note["id"])
	}
	expect(t, read, map[string]string{"changed": "1", "kept": "0", "deleted": "1", "lines": "0", "orders": "0", "renamed": "1"})
	if updated, _ := read["updated"].(map[string]any); updated["amount"] != 36000.5 || updated["note"] != "gấp" {
		t.Errorf("updated = %v, want amount 36000.5, note gấp", read["updated"])
	}
	for key, want := range map[string]float64{"first": 1, "second": 2} {
		if row, _ := read[key].(map[string]any); row == nil || row["count"] != want || row["id"] != 1.0 {
			t.Errorf("%s = %v, want the one row, count %v", key, read[key], want)
		}
	}
	if user, _ := read["renamedUser"].(map[string]any); user["name"] != "An Nguyễn" || user["updatedAt"].(float64) < user["createdAt"].(float64) {
		t.Errorf("renamed user = %v, want its name, updatedAt not before createdAt", user)
	}
}

// A row the engine writes is found by matches at once: the writer records it, and the index, built before the write
// (before counts on it), folds it in before the next read (library/database, search.go).
func TestAWrittenRowIsSearchedAtOnce(t *testing.T) {
	app := searchApp(t, map[string]string{
		"": `  const before = db.count((posts) => posts.matches("pho bo"));
  db.insert((posts) => ({ title: "Phở bò tái", body: "Nước dùng trong", published: true, slug: "e" }));
  return context.json({ before, found: db.list((posts) => posts.matches("pho bo")) });`,
	})
	read := answer(t, app, "/")
	if got := ids(read["found"], ""); jsonText(got) != "[5]" || read["before"] != 0.0 {
		t.Errorf("before = %v, found = %v, want 0, then [5], the post just written", read["before"], got)
	}
}

// A write written wrong stops the boot where it is written (docs/KITWORK_DATABASE.md mục 5): a column the engine
// fills, a required column missing, a spread, data not an object, an insert reading a column, a key or a .readonly()
// column in an update, an update without where, an insert after where, an upsert with no target or two, an upsert
// reading a column not a number.
//
// CONTROL (06/11/2026): with data not checking the required columns, the boot passed the insert of an email alone.
func TestAWriteWrittenWrongStopsTheBoot(t *testing.T) {
	for _, c := range []struct{ name, statement, want string }{
		{"a filled column", `db.insert((users) => ({ email: "a", username: "a", name: "a", passwordHash: "h", createdAt: 1 }))`, "users.createdAt là .created(): engine điền lúc insert"},
		{"a required column missing", `db.insert((users) => ({ email: "a" }))`, "insert thiếu cột bắt buộc của users: username, name, passwordHash"},
		{"a spread", `db.insert((users) => ({ ...form }))`, "spread trong dữ liệu ghi chưa có: viết từng cột"},
		{"not an object", `db.insert((users) => form)`, "dữ liệu của insert viết từng cột"},
		{"an insert reading a column", `db.insert((notes) => ({ text: notes.text }))`, "insert không đọc cột: dòng chưa có"},
		{"the key", `db.where((users) => users.id === 1).update((users) => ({ id: 2 }))`, "users.id là khoá: update không sửa khoá"},
		{"readonly", `db.where((orders) => orders.item === "x").update((orders) => ({ userId: 2 }))`, "orders.userId là .readonly(): đặt một lần lúc insert"},
		{"no where", `db.update((orders) => ({ item: "x" }))`, "update cần where"},
		{"an insert after where", `db.where((notes) => notes.text === "x").insert((notes) => ({ text: "y" }))`, "insert đứng một mình"},
		{"no target", `db.upsert((lines) => ({ orderId: "x", qty: 1 }))`, "upsert cần một đích so trùng trong dữ liệu"},
		{"two targets", `db.upsert((users) => ({ email: "a", username: "a", name: "a", passwordHash: "h" }))`, "upsert có 2 đích so trùng (email, username)"},
		{"a text read", `db.upsert((clickDays) => ({ linkId: 7, day: clickDays.day, count: 1 }))`, "upsert chỉ đọc cột số của dòng đã có"},
	} {
		got := bootError(t, map[string]string{
			"apps/shop/core/schema/shop.work": writeSchema,
			"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const form = context.request.json();
  return context.json(` + c.statement + `);
});
`,
		})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
