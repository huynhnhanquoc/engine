package host_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/host"
	"github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// Database của engine3 (package database, 24/11/2026, lát 3 của "engine3 trả lời request thật"): thư viện "schema" khai
// bảng, "database" mở chúng, câu lệnh đọc bằng điều kiện JavaScript dịch sang SQL, trên sqlitewire. Các file database
// được tạo trong thư mục tạm bằng chính sqlitewire (một người ghi): câu lệnh của engine không bao giờ tạo file hay bảng.

// seed tạo file database path (sqlitewire), chạy các câu tạo bảng, rồi ghi các dòng, và đóng file.
func seed(t testing.TB, path string, tables []string, rows func(tx *sqlite.Transaction) error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx, "test", tables...); err != nil {
		t.Fatal(err)
	}
	if rows != nil {
		if _, err := db.Write(ctx, "test", rows); err != nil {
			t.Fatal(err)
		}
	}
}

// exec chạy các câu ghi một lượt, dừng ở lỗi đầu.
func exec(tx *sqlite.Transaction, statements ...string) error {
	for _, s := range statements {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

// blueprintFolder là một bản chép của testdata/blueprint trong thư mục tạm, có hai file database: dữ liệu của host
// (.database/application/system.db: hai tenant, ba tên miền, một IP bị chặn) và của tenant 03dt (orders, ba đơn; như
// engine2 lưu struct của apps/03dt/core/shop.work: decimal(2) là số nguyên × 100, timestamp là chữ UTC tới mili giây).
func blueprintFolder(t testing.TB) string {
	t.Helper()
	from, to := filepath.Join("testdata", "blueprint"), t.TempDir()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	seed(t, filepath.Join(to, ".database", "application", "system.db"), []string{
		`CREATE TABLE domains (hostname TEXT NOT NULL, identity TEXT NOT NULL, status TEXT NOT NULL)`,
		`CREATE TABLE tenants (identity TEXT NOT NULL, status TEXT NOT NULL, memory TEXT, energy INTEGER)`,
		`CREATE TABLE banned (ip TEXT NOT NULL, reason TEXT NOT NULL)`,
	}, func(tx *sqlite.Transaction) error {
		return exec(tx,
			`INSERT INTO tenants VALUES ('03dt', 'active', '128mb', NULL), ('cu', 'paused', NULL, NULL)`,
			`INSERT INTO domains VALUES ('shop.vn', '03dt', 'confirmed'), ('cu.vn', 'cu', 'confirmed'), ('moi.vn', '03dt', 'pending')`,
			`INSERT INTO banned VALUES ('198.51.100.9', 'spam')`)
	})
	seed(t, filepath.Join(to, "apps", "03dt", ".database", "application", "kitwork.db"), []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, email TEXT NOT NULL, status TEXT NOT NULL, amount INTEGER NOT NULL,
		  createdAt TEXT NOT NULL)`,
	}, func(tx *sqlite.Transaction) error {
		return exec(tx, `INSERT INTO orders VALUES
		  (1, 'a@shop.vn', 'moi', 12550, '2026-11-20T08:00:00.000Z'),
		  (2, 'b@shop.vn', 'xong', 9900, '2026-11-21T08:00:00.000Z'),
		  (3, 'c@shop.vn', 'moi', 200000, '2026-11-22T09:30:00.000Z')`)
	})
	return to
}

// milliseconds là thời điểm UTC tính bằng mili giây, như timestamp() đọc ra.
func milliseconds(year int, month time.Month, day, hour, minute int) float64 {
	return float64(time.Date(year, month, day, hour, minute, 0, 0, time.UTC).UnixMilli())
}

// Mốc nghiệm thu của lát 3: trang don-hang của shop.vn đọc orders của tenant 03dt qua KitDB, sau năm guard của host
// (cũng đọc KitDB): các đơn "moi", mới nhất trước, decimal(2) là số, timestamp là mili giây, các khoá theo thứ tự struct.
func TestTheBlueprintReadsKitDB(t *testing.T) {
	w, console, log := blueprintDoor(t)
	rec := serve(w, "https://shop.vn/don-hang", "User-Agent", "k6")
	if rec.Code != 200 {
		t.Fatalf("%d %q, log %q", rec.Code, rec.Body.String(), log.String())
	}
	var orders []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	want := []map[string]any{
		{"id": 3.0, "email": "c@shop.vn", "status": "moi", "amount": 2000.0, "createdAt": milliseconds(2026, 11, 22, 9, 30)},
		{"id": 1.0, "email": "a@shop.vn", "status": "moi", "amount": 125.5, "createdAt": milliseconds(2026, 11, 20, 8, 0)},
	}
	if fmt.Sprint(orders) != fmt.Sprint(want) {
		t.Errorf("orders %v\nwant   %v", orders, want)
	}
	if !strings.HasPrefix(rec.Body.String(), `[{"id":3,"email":"c@shop.vn","status":"moi","amount":2000,"createdAt":`) {
		t.Errorf("the keys in the struct's order: %s", rec.Body.String())
	}
	if got := console.take(); got != "[console.log] GET shop.vn/don-hang 200\n" {
		t.Errorf("printed %q", got)
	}
}

// dataWork là apps/a/core/data.work của databaseSite: db (orders, customers), và ba handle mà file thiếu một thứ.
const dataWork = `import { struct, id, string, decimal, timestamp, boolean } from "schema";
import { connection } from "database";

const orders = struct({
  id: id(),
  email: string(),
  status: string(),
  amount: decimal(2),
  paid: boolean(),
  note: string().nullable(),
  createdAt: timestamp(),
});
const customers = struct({ id: id(), name: string() });
export const db = connection().tables({ orders, customers });

export const missing = connection().schema("billing").tables({ orders });
const ghosts = struct({ id: id() });
export const ghost = connection().tables({ ghosts });
const wide = struct({ id: id(), color: string() });
export const wrong = connection().tables({ orders: wide });
`

// databaseSite là một host không guard, routes tới apps/a/site.vn (chain thêm vào chuỗi của website, như .energy(…)),
// hàm error in mã lỗi; mỗi khoá của routes là một thư mục của site, router.work của nó trả context.json của thân hàm
// (dòng 5 của file). kitwork.db của apps/a có orders với rows dòng: dòng i có status "moi" khi i lẻ, amount i × 1,25,
// paid khi i chẵn, note null khi i chia hết cho 3, createdAt 1/11/2026 cộng i giờ; và customers một dòng.
func databaseSite(t *testing.T, rows int, chain string, routes map[string]string) (*host.Website, *lines, *lines) {
	t.Helper()
	folder := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("apps/a/core/data.work", dataWork)
	write("apps/a/site.vn/router.work", "import { router } from \"router\";\nreturn router();\n")
	for name, body := range routes {
		write("apps/a/site.vn/"+name+"/router.work", `import { router } from "router";
import { db, missing, ghost, wrong } from "app/core/data.work";
const route = router();
route.get((context) => {
`+body+`
});
return route;
`)
	}
	seed(t, filepath.Join(folder, "apps", "a", ".database", "application", "kitwork.db"), []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, email TEXT NOT NULL, status TEXT NOT NULL, amount INTEGER NOT NULL,
		  paid INTEGER NOT NULL, note TEXT, createdAt TEXT NOT NULL)`,
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
	}, func(tx *sqlite.Transaction) error {
		for i := 1; i <= rows; i++ {
			status, paid, note := "xong", 0, any(fmt.Sprintf("ghi chú %d", i))
			if i%2 == 1 {
				status = "moi"
			} else {
				paid = 1
			}
			if i%3 == 0 {
				note = nil
			}
			created := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000Z")
			if _, err := tx.Exec(`INSERT INTO orders VALUES (?, ?, ?, ?, ?, ?, ?)`, i, fmt.Sprintf("k%d@a.vn", i), status, i*125, paid, note, created); err != nil {
				return err
			}
		}
		return exec(tx, `INSERT INTO customers VALUES (1, 'Lan')`)
	})
	return door(t, folder, `import { application } from "application";
const app = application();
app.website()
  .routes((context) => "apps/a/site.vn")`+chain+`
  .error((context) => console.log(context.response.error.code))
  .listen(8080);
return app.run();
`)
}

// Các câu đọc: điều kiện (=== là IS, so được null; && || !; cột boolean đứng một mình), biến bắt được và đường thuộc tính
// của nó, giá trị đổi theo cột (decimal × 100, timestamp sang chữ UTC), thứ tự, list/first/count, câu không đổi khi viết
// tiếp, và một hàm điều kiện giữ trong biến.
func TestStatementsRead(t *testing.T) {
	since := int64(milliseconds(2026, 11, 1, 5, 0))
	cases := []struct{ name, body, want string }{
		{"where", `return context.json(db.where((orders) => orders.status === "moi").list(3).map((o) => o.id));`, `[1,3,5]`},
		{"captured", `const wanted = { status: "xong" };
  return context.json(db.where((orders) => orders.status === wanted.status && orders.id < 7).list().map((o) => o.id));`, `[2,4,6]`},
		{"decimal", `const least = 10;
  return context.json(db.where((orders) => orders.amount >= least).list().map((o) => o.amount));`, `[10,11.25,12.5]`},
		{"decimal-equal", `return context.json(db.where((orders) => orders.amount === 2.5).first().id);`, `2`},
		{"timestamp", `const since = ` + fmt.Sprint(since) + `;
  return context.json(db.where((orders) => orders.createdAt > since).count());`, `5`},
		{"boolean", `return context.json(db.where((orders) => orders.paid && !(orders.id > 4)).list().map((o) => o.id));`, `[2,4]`},
		{"boolean-value", `return context.json(db.where((orders) => orders.id === 2).first().paid);`, `true`},
		{"null", `return context.json(db.where((orders) => orders.note === null).list().map((o) => o.id));`, `[3,6,9]`},
		{"not-null", `return context.json(db.count((orders) => orders.note !== null));`, `7`},
		{"null-value", `return context.json(db.where((orders) => orders.id === 3).first().note);`, `null`},
		{"or", `return context.json(db.list((orders) => orders.id === 1 || orders.id === 10).map((o) => o.id));`, `[1,10]`},
		{"first-none", `return context.json(db.where((orders) => orders.id === 999).first());`, `null`},
		{"count", `return context.json(db.count((orders) => orders.status === "moi"));`, `5`},
		{"order", `return context.json(db.where((orders) => orders.id <= 4).ascending((orders) => orders.paid)
    .descending((orders) => orders.amount).list().map((o) => o.id));`, `[3,1,4,2]`},
		{"one-table", `return context.json(db.where((customers) => customers.id === 1).first());`, `{"id":1,"name":"Lan"}`},
		{"unchanged", `const base = db.where((orders) => orders.status === "moi");
  const fewer = base.where((orders) => orders.id > 5);
  return context.json([base.count(), fewer.count(), base.count()]);`, `[5,2,5]`},
		{"held", `const isNew = (orders) => orders.status === "moi";
  return context.json([db.count(isNew), db.where(isNew).first().id]);`, `[5,1]`},
	}
	routes := map[string]string{}
	for _, c := range cases {
		routes[c.name] = c.body
	}
	w, _, log := databaseSite(t, 10, "", routes)
	for _, c := range cases {
		rec := serve(w, "https://site.vn/"+c.name)
		if rec.Code != 200 || rec.Body.String() != c.want {
			t.Errorf("%s: %d %s, want %s; log %q", c.name, rec.Code, rec.Body.String(), c.want, log.take())
		}
	}
}

// SQL của một câu được nhớ theo dạng của nó (25/11/2026: bảng, hành động, số dòng, các điều kiện, các bước sắp xếp và
// chiều của chúng): cùng một hàm điều kiện với first, count, list(2), list(3), hai chiều sắp xếp, một điều kiện nữa
// trước hay sau nó đều là câu khác. Thiếu một phần trong khoá là hai câu chung một SQL, và một ca ở đây sai. Quá số dạng
// một Handle nhớ (256), câu vẫn đúng: list(n) với n từ 1 tới 300.
func TestStatementsOfOneShape(t *testing.T) {
	w, _, log := databaseSite(t, 10, "", map[string]string{
		"shapes": `const isNew = (orders) => orders.status === "moi";
  const byAmount = (orders) => orders.amount;
  const late = (orders) => orders.id > 5;
  const base = db.where(isNew);
  return context.json([base.first().id, base.count(), base.list(2).length, base.list(3).length,
    base.ascending(byAmount).list(5).map((o) => o.id), base.descending(byAmount).list(5).map((o) => o.id),
    base.where(late).list().map((o) => o.id), base.list().length, db.where(late).where(isNew).list().map((o) => o.id),
    db.where(late).count()]);`,
		"many": `let total = 0;
  for (let n = 1; n <= 300; n++) { total = total + db.where((orders) => orders.id > 0).list(n).length; }
  return context.json(total);`,
	})
	for range 2 { // lần hai: mọi SQL đã nhớ
		if rec := serve(w, "https://site.vn/shapes"); rec.Code != 200 ||
			rec.Body.String() != `[1,5,2,3,[1,3,5,7,9],[9,7,5,3,1],[7,9],5,[7,9],5]` {
			t.Errorf("shapes: %d %s, log %q", rec.Code, rec.Body.String(), log.take())
		}
	}
	if rec := serve(w, "https://site.vn/many"); rec.Code != 200 || rec.Body.String() != "2955" {
		t.Errorf("300 shapes: %d %s, log %q", rec.Code, rec.Body.String(), log.take())
	}
}

// Một câu viết sai hay chạy trên một file thiếu thứ struct khai: 500, lời báo có chỗ (file:dòng:cột của hàm điều kiện,
// hay đường dẫn của file database) và mã của nó.
func TestStatementsRefuse(t *testing.T) {
	const file = "site.vn/%s/router.work"
	cases := []struct{ name, body, code, message string }{
		{"unknown-column", `return context.json(db.where((orders) => orders.total > 1).list());`, "TYPE",
			"5:49: orders has no column total: id, email, status, amount, paid, note, createdAt"},
		{"unknown-table", `return context.json(db.where((users) => users.id === 1).list());`, "TYPE",
			"5:31: apps/a/.database/application/kitwork.db has no table users: the parameter names the table, one of orders, customers"},
		{"nullable-order", `return context.json(db.where((orders) => orders.note > "a").list());`, "TYPE",
			"orders.note may be null and > orders it"},
		{"computed", `return context.json(db.where((orders) => orders.id === 1 + 1).list());`, "TYPE",
			"a value computed in the condition comes in a later slice of engine3"},
		{"block", `return context.json(db.where((orders) => { return orders.id === 1; }).list());`, "TYPE",
			"write the condition as an expression, (orders) => …, not a block with return"},
		{"not-a-variable", `return context.json(db.where((orders) => orders.amount > Math.PI).list());`, "TYPE",
			"Math is not a variable the condition can read"},
		{"wrong-type", `const id = "1";
  return context.json(db.where((orders) => orders.id === id).list());`, "TYPE", "id is a string, compared with column id (id): write a number"},
		{"two-tables", `return context.json(db.where((orders) => orders.id === 1).where((customers) => customers.id === 1).list());`, "TYPE",
			"this statement reads orders; one statement reads one table"},
		{"which-table", `return context.json(db.list());`, "TYPE", "list() of which table?"},
		{"list-zero", `return context.json(db.list(0));`, "TYPE", "list(n) takes a whole number from 1 to 10000"},
		{"list-many", `return context.json(db.where((orders) => orders.id > 0).list(10001));`, "TYPE", "list(n) takes a whole number from 1 to 10000"},
		{"native", `return context.json(db.where(JSON.parse).list());`, "TYPE", "takes a function written in the program"},
		{"unknown-word", `return context.json(db.orderBy((orders) => orders.id));`, "TYPE",
			`a statement has no word "orderBy": order with .ascending((t) => t.column)`},
		{"no-file", `return context.json(missing.list((orders) => orders.id === 1));`, "DATABASE",
			"the database apps/a/.database/application/billing.db does not exist: migrate makes it from the structs"},
		{"no-table", `return context.json(ghost.list((ghosts) => ghosts.id === 1));`, "DATABASE",
			"apps/a/.database/application/kitwork.db lacks what struct ghosts declares (no such table: ghosts): migrate adds it"},
		{"no-column", `return context.json(wrong.list((orders) => orders.id === 1));`, "DATABASE",
			"apps/a/.database/application/kitwork.db lacks what struct orders declares (no such column: color): migrate adds it"},
	}
	routes := map[string]string{}
	for _, c := range cases {
		routes[c.name] = c.body
	}
	w, console, log := databaseSite(t, 3, "", routes)
	for _, c := range cases {
		rec := serve(w, "https://site.vn/"+c.name)
		logged, printed := log.take(), console.take()
		if rec.Code != 500 || !strings.Contains(logged, c.message) || printed != "[console.log] "+c.code+"\n" {
			t.Errorf("%s: %d, printed %q, log %q", c.name, rec.Code, printed, logged)
		}
		if strings.Contains(c.message, ":") && c.message[0] >= '0' && c.message[0] <= '9' &&
			!strings.Contains(logged, fmt.Sprintf(file, c.name)+":"+c.message) {
			t.Errorf("%s: the place is not %s:%s: %q", c.name, fmt.Sprintf(file, c.name), c.message, logged)
		}
	}
}

// list() không số đọc 1000 dòng nhiều nhất: nhiều hơn là lỗi nói viết list(n); list(n) đọc đúng n.
func TestListBounds(t *testing.T) {
	w, _, log := databaseSite(t, 1001, "", map[string]string{
		"all":     `return context.json(db.list((orders) => orders.id > 0).length);`,
		"fewer":   `return context.json(db.list((orders) => orders.id > 1).length);`,
		"counted": `return context.json(db.where((orders) => orders.id > 0).list(1001).length);`,
		"some":    `return context.json(db.where((orders) => orders.id > 0).list(7).length);`,
	})
	if rec := serve(w, "https://site.vn/all"); rec.Code != 500 ||
		!strings.Contains(log.take(), "list() reads 1000 rows at most and more of orders match: write list(n), up to 10000") {
		t.Errorf("1001 rows: %d", rec.Code)
	}
	for path, want := range map[string]string{"fewer": "1000", "counted": "1001", "some": "7"} {
		if rec := serve(w, "https://site.vn/"+path); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s: %d %s, log %q", path, rec.Code, rec.Body.String(), log.take())
		}
	}
}

// Các dòng một câu đọc tốn năng lượng và bộ nhớ của lượt chạy: trần nhỏ dừng câu ở dòng vượt trần.
func TestStatementsAreMetered(t *testing.T) {
	routes := map[string]string{"all": `return context.json(db.where((orders) => orders.id > 0).list(1000).length);`}
	w, console, log := databaseSite(t, 1000, "\n  .energy((context) => 15000)", routes)
	if rec := serve(w, "https://site.vn/all"); rec.Code != 500 || console.take() != "[console.log] ENERGY_LIMIT\n" {
		t.Errorf("energy: %d, log %q", rec.Code, log.take())
	}
	w, console, log = databaseSite(t, 1000, "\n  .memory((context) => \"128kb\")", routes)
	if rec := serve(w, "https://site.vn/all"); rec.Code != 500 || console.take() != "[console.log] MEMORY_LIMIT\n" {
		t.Errorf("memory: %d, log %q", rec.Code, log.take())
	}
	w, _, log = databaseSite(t, 1000, "", routes)
	if rec := serve(w, "https://site.vn/all"); rec.Code != 200 || rec.Body.String() != "1000" {
		t.Errorf("the default ceilings: %d %s, log %q", rec.Code, rec.Body.String(), log.take())
	}
}

// Nhiều request cùng lúc đọc cùng một file qua cùng các câu (go test -race): mỗi request đúng câu trả lời của nó.
func TestStatementsAtOnce(t *testing.T) {
	w, _, log := databaseSite(t, 50, "", map[string]string{
		"page": `const least = 20;
  return context.json(db.where((orders) => orders.amount > least).ascending((orders) => orders.id).list(3).map((o) => o.id));`,
	})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if rec := serve(w, "https://site.vn/page"); rec.Code != 200 || rec.Body.String() != "[17,18,19]" {
					t.Errorf("%d %s, log %q", rec.Code, rec.Body.String(), log.String())
					return
				}
			}
		}()
	}
	wg.Wait()
}
