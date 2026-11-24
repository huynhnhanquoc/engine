package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// hookSchema declares before and after on its tables: a user's email made plain, an order's item in capitals unless
// it is locked, every order's insert and delete written to audits through db, a user named "giữ" never deleted.
const hookSchema = `import { struct, id, string, integer, foreign } from "kitwork/schema";
import { connection } from "kitwork/database";

const nothing = null;

export const users = struct({ id: id(), email: string().unique(), name: string() });
export const orders = struct({ id: id(), userId: foreign(users.id).cascade(), item: string(), amount: integer() });
export const audits = struct({ id: id(), what: string(), orderId: integer() });

export const db = connection()
  .before("insert", (users) => ({ ...users, email: users.email.trim().toLowerCase() }))
  .before("update", (orders) => orders.item === "khoá" ? null : { ...orders, item: orders.item.toUpperCase() })
  .after("insert", (orders) => db.insert((audits) => ({ what: "insert", orderId: orders.id })))
  .after("delete", (orders) => orders.item === "LỖI" ? nothing.x : db.insert((audits) => ({ what: "delete", orderId: orders.id })))
  .before("delete", (users) => users.name === "giữ" ? null : users)
  .tables({ users, orders, audits });
`

const hookRoute = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const an = db.insert((users) => ({ email: "  AN@X.VN ", name: "An" }));
  db.insert((users) => ({ email: "giu@x.vn", name: "giữ" }));
  const first = db.insert((orders) => ({ userId: an.id, item: "cà phê", amount: 1 }));
  db.insert((orders) => ({ userId: an.id, item: "khoá", amount: 2 }));
  const changed = db.where((orders) => orders.userId === an.id).update((orders) => ({ amount: orders.amount + 10 }));
  const orders = db.list((orders) => orders).map((order) => order.item + ":" + order.amount);
  const kept = db.delete((users) => users.name === "giữ");
  db.where((orders) => orders.id === first.id).update((orders) => ({ item: "lỗi" }));
  const broken = safe(() => db.delete((users) => users.id === an.id));
  const afterBroken = db.count((users) => users) + "/" + db.count((orders) => orders) + "/" + db.count((audits) => audits);
  db.where((orders) => orders.id === first.id).update((orders) => ({ item: "ok" }));
  const deleted = db.delete((users) => users.id === an.id);
  const upserted = db.upsert((users) => ({ email: "  NEW@X.VN ", name: "Mới" }));
  return context.json({
    an, changed, orders, kept, broken: broken.error, afterBroken, deleted, upserted,
    audits: db.list((audits) => audits).map((audit) => audit.what + ":" + audit.orderId),
    users: db.list((users) => users).map((user) => user.email)
  });
});
`

// before and after (docs/KITWORK_DATABASE.md mục 1): before gives the row to write or null for none, on each row of
// an update and a delete; after runs in the same transaction, a write of its through db joining it, a failure of
// its rolling the whole statement back; the rows a .cascade() deletes take their table's after("delete"); an upsert
// is an insert or an update by what it finds.
//
// CONTROL (06/11/2026): without before, an's email was kept "  AN@X.VN " and the locked order changed too; with a
// hook's write through db refused instead of joining, the first insert of an order was INVALID_ARGUMENT; with the
// after of the rows a .cascade() takes not run, audits held no delete and broken deleted an.
func TestBeforeAndAfterRunOnEveryRow(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": hookSchema,
		"apps/shop/shop.test/router.work": hookRoute,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	read := answer(t, app, "/")
	if an, _ := read["an"].(map[string]any); an["email"] != "an@x.vn" {
		t.Errorf("an = %v, want its email made plain by before(\"insert\")", read["an"])
	}
	expect(t, read, map[string]string{
		"changed":     "1",
		"orders":      `["CÀ PHÊ:11","khoá:2"]`,
		"kept":        "0",
		"afterBroken": `"2/2/2"`,
		"deleted":     "1",
		"audits":      `["insert:1","insert:2","delete:1","delete:2"]`,
		"users":       `["giu@x.vn","new@x.vn"]`,
	})
	if broken, _ := read["broken"].(map[string]any); broken == nil || broken["code"] != "TYPE" {
		t.Errorf("broken = %v, want the after's TYPE failure", read["broken"])
	}
	if upserted, _ := read["upserted"].(map[string]any); upserted["email"] != "new@x.vn" || upserted["name"] != "Mới" {
		t.Errorf("upserted = %v, want the row inserted through before(\"insert\")", read["upserted"])
	}
}

// A hook declared wrong stops the boot where it is written: an event not insert, update or delete; a table not of
// the declaration; two of one kind on one table; a schema after the hooks.
//
// CONTROL (06/11/2026): with attach not checking the hooks' tables, the boot passed .before("insert", (posts) => …).
func TestAHookDeclaredWrongStopsTheBoot(t *testing.T) {
	declare := func(chain string) string {
		return strings.Replace(hookSchema, `.before("insert", (users) => ({ ...users, email: users.email.trim().toLowerCase() }))`, chain, 1)
	}
	for _, c := range []struct{ name, schema, want string }{
		{"an event", declare(`.before("save", (users) => users)`), `.before nhận sự kiện và hàm: .before("insert", (orders) => …); sự kiện là insert, update, delete`},
		{"a table", declare(`.before("insert", (posts) => posts)`), `posts không phải bảng của tables({ … }); có: users, orders, audits`},
		{"two", declare(`.before("delete", (users) => users)`), `users có hai .before("delete"): một bảng một before cho mỗi sự kiện`},
		{"a schema after", declare(`.before("insert", (users) => users).schema("billing")`), `.schema(…) viết một lần, ngay sau connection(…), trước before, after`},
	} {
		got := bootError(t, map[string]string{"apps/shop/core/schema/shop.work": c.schema})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
