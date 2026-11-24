package runtime_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/host"
)

// Đo VM: fib(20) là ~22 nghìn lời gọi hàm, phép so, phép cộng; cùng chương trình đo trên engine2 để đối chiếu (luật
// "engine mới không chậm hơn"). Boot là biên dịch và chạy một application.work thật.

const fibSource = `const fib = (n) => n < 2 ? n : fib(n - 1) + fib(n - 2); return fib(20);`

func BenchmarkFib(b *testing.B) { benchmark(b, fibSource, 6765) }

// logicSource là một guard kiểu thật, nặng &&, ||, ??: đo cách VM ghép đoản mạch (23/11/2026).
const logicSource = `
const allowed = (request) => (request.secure && request.hostname && request.hostname.length > 3) ||
  (request.tenant.fallback ?? false) || (request.ip !== "" && !request.banned);
const memory = (request) => request.tenant.memory ?? request.default ?? "64mb";
const count = (n, request) => n < 1 ? 0 : (allowed(request) && memory(request) === "64mb" ? 1 : 0) + count(n - 1, request);
return count(500, { secure: false, hostname: "shop.vn", ip: "203.0.113.7", banned: false, default: null,
  tenant: { fallback: null, memory: null } });
`

func BenchmarkLogic(b *testing.B) { benchmark(b, logicSource, 500) }

// makeSource làm ra nhiều: object, mảng, chuỗi, map, filter, JSON: đo bộ đếm byte ở chỗ nó làm việc nhiều nhất.
const makeSource = `
const rows = [];
for (let i = 0; i < 200; i++) {
  rows.push({ id: i, name: "item " + i, tags: ["a", "b", ` + "`t${i}`" + `] });
}
const names = rows.map((row) => row.name.toUpperCase()).filter((name) => name.length > 6);
return JSON.stringify(rows).length + names.join(",").length;
`

func BenchmarkMake(b *testing.B) { benchmark(b, makeSource, 11890) } // 11890: node cho cùng chương trình

// textSource là các phép chuỗi ngắn đi bằng con trỏ (core/javascript/cursor.go): slice, startsWith, charCodeAt, length.
const textSource = `
const words = [];
for (let i = 0; i < 200; i++) { words.push("word-" + i); }
let n = 0;
for (const w of words) { n = n + w.slice(1).length + (w.startsWith("word") ? 1 : 0) + w.charCodeAt(0) + w.length; }
return n;
`

func BenchmarkText(b *testing.B) { benchmark(b, textSource, 26780) } // 26780: node cho cùng chương trình

// callsSource là lời gọi phương thức rẻ, năm cái mỗi vòng: đo giá của chính lời gọi (23/11/2026).
const callsSource = `
const a = [];
let n = 0;
for (let i = 0; i < 1000; i++) {
  a.push(i);
  n = n + "word".slice(1).length + Math.max(i, 3) + "ab".indexOf("b") + a.at(-1);
}
return n;
`

func BenchmarkCalls(b *testing.B) { benchmark(b, callsSource, 1003006) } // 1003006: node cho cùng chương trình

// spreadSource là { ...o } kiểu thật (24/11/2026): object từ một mẫu, thêm một khoá, cập nhật trạng thái.
const spreadSource = `
const base = { id: 0, name: "item", price: 10, stock: 3 };
const rows = [];
for (let i = 0; i < 200; i++) { rows.push({ ...base, id: i }); }
const totals = rows.map((row) => ({ ...row, total: row.price * row.stock + row.id }));
let state = { count: 0, last: 0 };
for (const row of totals) { state = { ...state, count: state.count + 1, last: row.total }; }
return totals.length + state.count + state.last + JSON.stringify(totals).length;
`

func BenchmarkSpread(b *testing.B) { benchmark(b, spreadSource, 12050) } // 12050: node cho cùng chương trình

// wideSource là { ...row } trên các dòng rộng (12 cột, có chỉ mục) mà JSON.parse dựng, như dòng của một database: thêm
// một khoá, và thay một khoá đã có.
const wideSource = `
const rows = JSON.parse(JSON.stringify(Array.from({ length: 200 }, (_, i) => ({ id: i, sku: "s" + i, name: "item",
  price: 10, stock: 3, brand: "b", category: "c", color: "red", size: "m", weight: 1, rating: 4, sold: i }))));
const linked = rows.map((row) => ({ ...row, url: "/p/" + row.id }));
const raised = rows.map((row) => ({ ...row, price: row.price + 1 }));
return linked.length + raised[199].price + linked[199].url.length + Object.keys(linked[0]).length;
`

func BenchmarkSpreadWide(b *testing.B) { benchmark(b, wideSource, 230) } // 230: node cho cùng chương trình

// parseSource là JSON.parse của một kết quả API (24/11/2026): 200 dòng, mỗi dòng hai object con cùng hai khoá bắt đầu
// bằng "id" (hai loại xen nhau) và một mảng; đọc năm lần rồi đọc các trường.
const parseSource = `
const text = JSON.stringify(Array.from({ length: 200 }, (_, i) => ({ id: i, name: "item " + i, price: i % 50,
  owner: { id: i % 7, email: "u" + (i % 7) + "@shop.vn" }, category: { id: i % 3, title: "c" + (i % 3) }, tags: ["a", "b"] })));
let total = 0;
for (let k = 0; k < 5; k++) {
  const rows = JSON.parse(text);
  for (const row of rows) { total += row.price + row.owner.id + row.category.id + row.tags.length + row.owner.email.length; }
}
return total;
`

func BenchmarkParse(b *testing.B) { benchmark(b, parseSource, 40465) } // 40465: node cho cùng chương trình

// benchmark biên dịch source một lần rồi chạy nó mỗi vòng trên một lượt chạy mới, và kiểm kết quả.
func benchmark(b *testing.B, source string, want float64) {
	program, err := syntax.Parse("bench.work", source)
	if err != nil {
		b.Fatal(err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if r := runtime.New(runtime.Options{}).Run(main); r.Number() != want {
			b.Fatal(r, r.Error())
		}
	}
}

const bootSource = `import { website, application, database } from "application";

const seen = {};
const https = (context) => context.request.secure ? context.next() : context.redirect("https://" + context.request.hostname);

const web = website()
  .listen(443)
  .port(80)
  .certificates(".certificates")
  .concurrency(16)
  .guard(https)
  .guard((context) => { seen[context.request.ip] = true; return context.next(); })
  .routes((context) => ` + "`apps/${context.request.tenant.identity}/${context.request.hostname}`" + `)
  .memory((context) => context.request.tenant.memory ?? "64mb")
  .energy(10000000);

const db = database(".database", { listen: "127.0.0.1:5445", password: "secret", timeout: "10s" });

return application({ memory: "2gb", timezone: "Asia/Ho_Chi_Minh" }).run({ web, db });
`

func BenchmarkBoot(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := host.Boot("application.work", bootSource); err != nil {
			b.Fatal(err)
		}
	}
}
