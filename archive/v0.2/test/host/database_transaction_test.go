package host_test

import (
	"strings"
	"testing"
)

const transactionRoute = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const ok = db.transaction((database) => {
    const an = database.insert((users) => ({ email: "an@x.vn", username: "an", name: "An", passwordHash: "h" }));
    database.insert((orders) => ({ userId: an.id, item: "Cà phê", amount: 1 }));
    const seen = database.count((orders) => orders.userId === an.id);
    const outside = db.count((orders) => orders);
    return { id: an.id, seen, outside };
  });
  const failed = safe(() => db.transaction((database) => {
    database.insert((users) => ({ email: "binh@x.vn", username: "binh", name: "B", passwordHash: "h" }));
    database.insert((users) => ({ email: "an@x.vn", username: "x", name: "X", passwordHash: "h" }));
    return 1;
  }));
  const binh = db.count((users) => users.email === "binh@x.vn");
  const kept = db.transaction((database) => database);
  const stale = safe(() => kept.count((users) => users));
  const outer = safe(() => db.transaction((database) => db.insert((notes) => ({ text: "x" }))));
  const nested = safe(() => db.transaction((database) => database.transaction((inner) => 1)));
  return context.json({
    ok, failed: failed.error, binh, stale: stale.error, outer: outer.error, nested: nested.error,
    notes: db.count((notes) => notes), users: db.count((users) => users)
  });
});
`

// A transaction (docs/KITWORK_DATABASE.md mục 3): its statements run in one request of KitDB's writer, each seeing
// the ones before it, a read through db outside it in a snapshot of its own; its value comes back once it commits;
// a failure out of the callback rolls every write back and goes on out; the handle is dead once it ends; a write
// through db inside it is refused, where it would wait for itself; a transaction inside one is not written yet.
//
// CONTROL (06/11/2026): with a failure out of the callback committed, binh was 1; with the transaction's reads in
// a snapshot of their own, seen was 0; without outsideTransaction's refusal, GET / never answered (the test's
// deadline ended it).
func TestATransactionCommitsOrRollsBackAsOne(t *testing.T) {
	app := writeApp(t, map[string]string{"apps/shop/shop.test/router.work": transactionRoute})
	read := answer(t, app, "/")
	expect(t, read, map[string]string{"ok": `{"id":1,"seen":1,"outside":0}`, "binh": "0", "notes": "0", "users": "1"})
	failure := func(key, code, says string) {
		t.Helper()
		got, _ := read[key].(map[string]any)
		if got == nil || got["code"] != code || !strings.Contains(got["message"].(string), says) {
			t.Errorf("%s = %v, want %s saying %q", key, read[key], code, says)
		}
	}
	failure("failed", "CONFLICT", "email đã có")
	failure("stale", "INVALID_ARGUMENT", "handle của giao dịch hết hiệu lực")
	failure("outer", "INVALID_ARGUMENT", "insert qua db trong giao dịch: ghi qua handle của callback")
	failure("nested", "UNSUPPORTED", "lồng trong giao dịch chưa có")
}
