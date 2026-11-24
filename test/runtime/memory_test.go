package runtime_test

import (
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Trần byte của một lượt chạy (core/runtime/memory.go, 23/11/2026). Mỗi luật một ca: tắt luật thì ca ấy đỏ.

const mib = 1 << 20

// metered biên dịch source và trả machine với trần memory cùng chương trình đã biên dịch. Năng lượng rộng: các test
// này thử bộ nhớ, không thử năng lượng (việc của engine tốn năng lượng, work_test.go).
func metered(t *testing.T, source string, memory int64) (*runtime.Machine, *compiler.Function) {
	t.Helper()
	program, err := syntax.Parse("test.work", source)
	if err != nil {
		t.Fatal(err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return runtime.New(runtime.Options{Memory: memory, Energy: 1 << 40}), main
}

// runMetered chạy source với trần memory.
func runMetered(t *testing.T, source string, memory int64) value.Value {
	t.Helper()
	m, main := metered(t, source, memory)
	return m.Run(main)
}

// wantMemoryLimit là result là MEMORY_LIMIT.
func wantMemoryLimit(t *testing.T, result value.Value) {
	t.Helper()
	if result.Code() != value.MemoryCode {
		t.Errorf("want MEMORY_LIMIT, got %v %v", result, result.Error())
	}
}

func TestMemoryDefault(t *testing.T) {
	if got := runtime.New(runtime.Options{}).Memory(); got != 64*mib {
		t.Errorf("default memory %d", got)
	}
}

// Giữ lại quá nửa trần là MEMORY_LIMIT, ở lệnh làm ra, với lời báo nêu trần; trần lớn hơn thì chạy xong.
func TestMemoryHolding(t *testing.T) {
	source := `const kept = [];
for (let i = 0; i < 100; i++) { kept.push("y".repeat(1000000) + i); }
return kept.length;`
	result := runMetered(t, source, 16*mib)
	wantMemoryLimit(t, result)
	if result.Message() != "the run used all its memory (16mb)" || !strings.HasPrefix(result.At(), "test.work:2:") {
		t.Errorf("message %q at %q", result.Message(), result.At())
	}
	if r := runMetered(t, source, 256*mib); r.Number() != 100 {
		t.Errorf("under 256mb: %v %v", r, r.Error())
	}
}

// Làm ra nhiều mà giữ ít thì chạy xong: lần đếm lại thấy cái còn giữ, không phải cái đã làm ra.
func TestMemoryChurn(t *testing.T) {
	r := runMetered(t, `let s = "";
for (let i = 0; i < 500; i++) { s = "y".repeat(100000) + i; }
return s.length;`, 4*mib)
	if r.Number() != 100003 {
		t.Errorf("got %v %v", r, r.Error())
	}
}

// [...big] trăm nghìn lần làm ra hàng chục GB mà giữ ít: mỗi byte làm ra tốn năng lượng khi lượt chạy được đếm lại,
// nên nó dừng sớm ở ENERGY_LIMIT, không chạy hàng giây (một spread không tốn gì khác ngoài lệnh của nó).
func TestMemoryMadeSpendsEnergy(t *testing.T) {
	program, _ := syntax.Parse("test.work", `const big = Array.from({ length: 10000 }, (_, i) => i);
let copy = [];
for (let i = 0; i < 100000; i++) { copy = [...big]; }
return copy.length;`)
	main, _ := compiler.Compile(program, compiler.Options{})
	start := time.Now()
	r := runtime.New(runtime.Options{}).Run(main) // năng lượng mặc định
	if r.Code() != value.EnergyCode {
		t.Errorf("want ENERGY_LIMIT, got %v %v", r, r.Error())
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v", elapsed)
	}
}

// Một hàm có sẵn làm ra nhiều từ ít hỏi chỗ trước khi cấp phát: lỗi đến trước khi Go cấp hàng trăm MB.
func TestMemoryRefusedBeforeMade(t *testing.T) {
	for _, source := range []string{
		`return "y".repeat(500000000);`,
		`return "a".padStart(500000000);`,
		`return Array(10000000);`,
		`return Array.from({ length: 10000000 });`,
		`return "x".repeat(10000000).split("");`,
		`return [..."x".repeat(10000000)];`,
		`return JSON.parse("[" + "0,".repeat(5000000) + "0]");`,
		`return JSON.stringify(Array(1000).fill("y".repeat(300000)));`,
		`return Array(1000).fill(Array(10000).fill(0)).flat();`,
		`return Array(1000).fill("y".repeat(300000)).join("");`,
	} {
		var before, after goruntime.MemStats
		goruntime.GC()
		goruntime.ReadMemStats(&before)
		result := runMetered(t, source, 16*mib)
		goruntime.ReadMemStats(&after)
		if result.Code() != value.MemoryCode {
			t.Errorf("%s: want MEMORY_LIMIT, got %v", source, result.Error())
		}
		// TotalAlloc đếm cả các bản chép khi một slice lớn dần (Go nhân 1,25 lần): mỗi ca không dừng thì cấp 320 MB trở lên
		if made := after.TotalAlloc - before.TotalAlloc; made > 128*mib {
			t.Errorf("%s: Go allocated %d MB before the run stopped", source, made/mib)
		}
	}
}

// Cái một hàm có sẵn đang dựng trong khi chương trình nó gọi lại chạy (các phần tử map đã làm) được thấy khi đếm lại.
func TestMemoryKeptByBuiltins(t *testing.T) {
	for _, source := range []string{
		`return Array.from({ length: 300 }, () => 0).map(() => "y".repeat(100000) + "").length;`,
		`return Array.from({ length: 300 }, () => "y".repeat(100000) + "").length;`,
		`return Array.from({ length: 300 }, () => 0).flatMap(() => ["y".repeat(100000) + ""]).length;`,
		`return Object.keys(Object.groupBy(Array.from({ length: 300 }, (_, i) => i), (i) => "y".repeat(100000) + i)).length;`,
	} {
		wantMemoryLimit(t, runMetered(t, source, 8*mib))
	}
}

// Một biến một closure bắt nằm trong một ô của khung, không trên ngăn xếp: đếm lại thấy nó dù closure đã đi. kept chỉ
// lên ngăn xếp khi ghi kept[i] (và phần ghi thêm được đếm sau khi nó đã xuống), nên không lần đếm lại nào thấy nó ở đó.
func TestMemoryCellsOfFrames(t *testing.T) {
	wantMemoryLimit(t, runMetered(t, `const kept = [];
[1].forEach(() => kept);
for (let i = 0; i < 100; i++) { const s = "y".repeat(1000000) + i; kept[i] = s; }
return kept.length;`, 16*mib))
}

// Closure của một khung đang chạy nằm trên ngăn xếp của khung ấy: biến nó bắt (kept, ô của một khung đã trả) chỉ với
// tới qua nó.
func TestMemoryClosureOfAFrame(t *testing.T) {
	wantMemoryLimit(t, runMetered(t, `const make = () => {
  const kept = [];
  return () => {
    for (let i = 0; i < 100; i++) { const s = "y".repeat(1000000) + i; kept[i] = s; }
    return kept.length;
  };
};
return make()();`, 16*mib))
}

// this và đối số của một hàm có sẵn vẫn trên ngăn xếp khi nó gọi lại chương trình: make() chỉ còn là this của map.
func TestMemoryArgumentsOfBuiltins(t *testing.T) {
	wantMemoryLimit(t, runMetered(t, `const make = () => {
  const a = [];
  for (let i = 0; i < 10; i++) { a.push("y".repeat(600000) + i); }
  return a;
};
return make().map(() => "z".repeat(3000000).length + "w".repeat(500000)).length;`, 16*mib))
}

// Lượt chạy nhớ vài chuỗi dài nó vừa đọc (core/javascript/cursor.go), và bộ đếm byte không đếm chúng: một lần đếm lại
// quên chúng, nên chúng không giữ chuỗi chương trình đã bỏ quá lần đếm ấy. Đo trên heap của Go: bốn chuỗi 3 MB đọc
// .length rồi bỏ, rồi một vòng làm ra đủ để đếm lại: sau đó chúng không còn sống.
func TestMemoryForgetsRememberedTexts(t *testing.T) {
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	m, main := metered(t, `for (let i = 0; i < 4; i++) { const s = "y".repeat(3000000) + i; s.length; }
let x = "";
for (let j = 0; j < 40; j++) { x = "z".repeat(1000000) + j; }
return x.length;`, 16*mib)
	r := m.Run(main)
	goruntime.GC()
	goruntime.ReadMemStats(&after)
	goruntime.KeepAlive(m)
	if r.Number() != 1000002 {
		t.Errorf("got %v %v", r, r.Error())
	}
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 6*mib {
		t.Errorf("the run still holds %d MB after it let its texts go", grown/mib)
	}
}

// Một chuỗi lớn nhiều chỗ giữ đếm một lần: một nghìn phần tử cùng là một chuỗi 2 MB giữ 2 MB.
func TestMemoryCountsAStringOnce(t *testing.T) {
	r := runMetered(t, `const big = "y".repeat(2000000);
const many = Array(1000).fill(big);
let s = "";
for (let i = 0; i < 100; i++) { s = "z".repeat(200000) + i; }
return many.length;`, 16*mib)
	if r.Number() != 1000 {
		t.Errorf("got %v %v", r, r.Error())
	}
}

// Dữ liệu dùng chung từ lúc boot (FreezeShared) là của mọi lượt chạy: lượt chạy đọc nó không bị đếm nó.
func TestMemorySharedIsNotCounted(t *testing.T) {
	m, main := metered(t, `return (data) => {
  let s = "";
  for (let i = 0; i < 200; i++) { s = "y".repeat(100000) + i; }
  return data.length;
};`, 16*mib)
	f := m.Run(main)
	items := make([]value.Value, 1000000) // 32 MB của Value
	data := value.Array(items...).FreezeShared()
	if r := m.Call(f, data); r.Number() != 1000000 {
		t.Errorf("got %v %v", r, r.Error())
	}
}

// MEMORY_LIMIT một lần là mãi: lời gọi sau trên cùng machine cũng dừng.
func TestMemoryStopStays(t *testing.T) {
	m, main := metered(t, `return [() => "y".repeat(50000000), () => 1 + 1];`, 16*mib)
	pair := m.Run(main)
	big, small := pair.Index(0), pair.Index(1)
	wantMemoryLimit(t, m.Call(big))
	wantMemoryLimit(t, m.Call(small))
}

// Mỗi chỗ làm ra byte được đếm: mỗi ca chỉ làm ra theo một cách, nên tắt cách đếm ấy thì lượt chạy giữ hàng chục MB
// mà không lần đếm lại nào xảy ra.
func TestMemoryCountedWhereMade(t *testing.T) {
	for _, c := range []struct{ name, source string }{
		{"the result of a builtin", `const kept = [];
for (let i = 0; i < 100; i++) { kept.push("y".repeat(1000000)); }`},
		{"the result of a function", `const s = "y".repeat(1000000);
const kept = [];
for (let i = 0; i < 100; i++) { kept.push(JSON.stringify(s)); }`},
		{"what push adds", `const kept = [];
for (let i = 0; i < 1000000; i++) { kept.push(i); }`},
		{"what an index write adds", `const kept = [];
for (let i = 0; i < 1000000; i++) { kept[i] = i; }`},
		{"an array literal", `const kept = [];
for (let i = 0; i < 100000; i++) { kept.push([1, 2, 3, 4, 5, 6, 7, 8]); }`},
		{"an object literal", `const kept = [];
for (let i = 0; i < 100000; i++) { kept.push({ a: 1, b: 2, c: 3, d: 4, e: 5, f: 6, g: 7, h: 8 }); }`},
		{"a closure", `const kept = [];
for (let i = 0; i < 300000; i++) { kept.push(() => 1); }`},
		{"a template", "const s = \"y\".repeat(100000);\nconst kept = [];\nfor (let i = 0; i < 200; i++) { kept.push(`${s}${i}`); }"},
		{"a spread", `const big = Array.from({ length: 1000 }, (_, i) => i);
const kept = [];
for (let i = 0; i < 1000; i++) { kept.push([...big]); }`},
		{"two texts added", `const s = "y".repeat(100000);
const kept = [];
for (let i = 0; i < 200; i++) { kept.push(s + "x"); }`},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantMemoryLimit(t, runMetered(t, c.source+"\nreturn 1;", 16*mib))
		})
	}
}
