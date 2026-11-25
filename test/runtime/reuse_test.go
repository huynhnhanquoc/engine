package runtime_test

import (
	goruntime "runtime"
	"runtime/debug"
	"testing"
	"weak"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Máy dùng lại (25/11/2026): runtime.Take lấy một máy Release đã trả về kho, để mỗi request không cấp lại ngăn xếp
// 8 KB của nó. Máy lấy lại không mang gì của lượt chạy trước, và kho không giữ giá trị nào của lượt ấy. Lời gọi từ Go
// (m.Call) không cấp mảng đối số.

// oneProcessor chạy f với một P và GC tắt: Release rồi Take trên cùng goroutine lấy lại đúng máy vừa trả (kho của P),
// nên ca nào cũng chạy trên một máy dùng lại.
func oneProcessor(t *testing.T, f func()) {
	t.Helper()
	defer goruntime.GOMAXPROCS(goruntime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	f()
}

// Một máy dừng vì hết năng lượng, một máy dừng vì MEMORY_LIMIT (một khi đã có là mãi, memory_test.go), trả về kho rồi
// lấy lại: chạy như máy mới, với trần của lần lấy.
func TestATakenMachineCarriesNothing(t *testing.T) {
	endless := compileSource(t, `let n = 0;
for (let i = 0; i < 1000000; i++) { n = n + i; }
return n;`)
	greedy := compileSource(t, `const parts = [];
for (let i = 0; i < 100000; i++) { parts.push("part " + i); }
return parts.length;`)
	small := compileSource(t, `let n = 0;
for (let i = 0; i < 10; i++) { n = n + i; }
return n;`)
	oneProcessor(t, func() {
		reused := 0
		for range 10 {
			m := runtime.Take(runtime.Options{Energy: 1000})
			if r := m.Run(endless); r.Code() != value.EnergyCode {
				t.Fatalf("the endless loop: %v", r.Error())
			}
			m.Release()
			n := runtime.Take(runtime.Options{Memory: 64 << 10, Energy: 1 << 40})
			if n == m {
				reused++
			}
			if r := n.Run(greedy); r.Code() != value.MemoryCode {
				t.Fatalf("the greedy loop: %v", r.Error())
			}
			n.Release()
			o := runtime.Take(runtime.Options{Memory: 64 << 10, Energy: 5000})
			if o.Energy() != 5000 {
				t.Errorf("a taken machine has %d energy, taken with 5000", o.Energy())
			}
			if r := o.Run(small); r.Number() != 45 {
				t.Errorf("a machine taken after an ENERGY_LIMIT and a MEMORY_LIMIT: %v %v", r, r.Error())
			}
			o.Release()
		}
		if reused == 0 {
			t.Error("Take never gave back the machine Release returned")
		}
	})
}

// big là một thứ lớn một lượt chạy giữ trên ngăn xếp (một đối số): sau Release, kho không giữ nó, GC lấy lại được.
type big struct{ data [1 << 20]byte }

//go:noinline
func runWith(t *testing.T, f value.Value) weak.Pointer[big] {
	p := &big{}
	m := runtime.Take(runtime.Options{})
	if r := m.Call(f, value.Struct(p)); r.Number() != 1 {
		t.Fatal(r, r.Error())
	}
	m.Release()
	return weak.Make(p)
}

func TestAReleasedMachineHoldsNothingOfItsRun(t *testing.T) {
	f := runtime.New(runtime.Options{}).Run(compileSource(t, `return (x) => 1;`))
	oneProcessor(t, func() {
		held := runWith(t, f)
		goruntime.GC() // kho chuyển máy sang victim: máy còn sống, ngăn xếp của nó cũng vậy
		if held.Value() != nil {
			t.Error("the machine back in the pool still holds the argument of its last run")
		}
		m := runtime.Take(runtime.Options{}) // cho máy ấy một việc nữa: nó không bị GC lấy đi trước khi kiểm
		m.Release()
	})
}

// m.Call(f, x) từ Go: đối số không cấp phát (một closure chép nó lên ngăn xếp khi vào khung; một hàm có sẵn nhận bản
// chép trên ngăn xếp). Trước 25/11: một mảng mỗi lời gọi, chín lần mỗi request qua chuỗi của host.
func TestACallFromGoAllocatesNoArguments(t *testing.T) {
	f := runtime.New(runtime.Options{}).Run(compileSource(t, `return (x) => x;`))
	m := runtime.New(runtime.Options{Energy: 1 << 40})
	allocs := testing.AllocsPerRun(100, func() {
		if r := m.Call(f, value.Number(7)); r.Number() != 7 {
			t.Fatal(r, r.Error())
		}
	})
	if allocs != 0 {
		t.Errorf("m.Call(f, x) allocates %.0f times, want 0", allocs)
	}
}

// Một hàm có sẵn gọi từ Go nhận các đối số của nó trên ngăn xếp: một lời gọi lồng vào chương trình (đệ quy sâu, ngăn
// xếp lớn quá chỗ đầu và được cấp lại) không đổi chúng, và ngăn xếp về như cũ sau lời gọi.
func TestANativeCalledFromGoKeepsItsArguments(t *testing.T) {
	m := runtime.New(runtime.Options{Energy: 1 << 40})
	deep := m.Run(compileSource(t, `const down = (n) => n === 0 ? 0 : 1 + down(n - 1);
return down;`))
	probe := javascript.NewNative("probe", 2, func(run javascript.Run, arguments []value.Value) value.Value {
		first, second := arguments[0].Number(), arguments[1].Number()
		depth := run.Call(deep, value.Number(400))
		if depth.IsInvalid() {
			return depth
		}
		if arguments[0].Number() != first || arguments[1].Number() != second {
			return value.Fail(value.TypeCode, "the arguments changed: %v %v", arguments[0].Number(), arguments[1].Number())
		}
		return value.Number(first*1000 + second + depth.Number())
	})
	for range 3 {
		if r := m.Call(probe, value.Number(1), value.Number(2)); r.Number() != 1402 {
			t.Fatalf("probe: %v %v", r, r.Error())
		}
	}
}

// Một lượt chạy đệ quy sâu làm ngăn xếp lớn quá 4096 ô (keptStack): Release không đưa máy ấy về kho, nên một request
// như vậy không để lại một mảng lớn cho mọi request sau. Máy của một lượt chạy nông thì về kho (ca đầu của file).
func TestAMachineWithALargeStackIsNotKept(t *testing.T) {
	deep := compileSource(t, `const down = (n, a, b, c) => n === 0 ? 0 : 1 + down(n - 1, n, a, b);
return down(900, 0, 0, 0);`)
	shallow := compileSource(t, `return 1 + 1;`)
	oneProcessor(t, func() {
		kept := 0
		for range 10 {
			m := runtime.Take(runtime.Options{Energy: 1 << 30})
			if r := m.Run(deep); r.Number() != 900 {
				t.Fatalf("the deep call: %v %v", r, r.Error())
			}
			m.Release()
			n := runtime.Take(runtime.Options{})
			if n == m {
				t.Fatal("the pool gave back a machine whose run went 900 calls deep: its stack stays as large as that run made it")
			}
			if r := n.Run(shallow); r.Number() != 2 {
				t.Fatalf("the shallow run: %v %v", r, r.Error())
			}
			n.Release()
			if o := runtime.Take(runtime.Options{}); o == n {
				kept++
			} // o không trả về kho: vòng sau bắt đầu với kho rỗng
		}
		if kept == 0 { // dưới -race kho bỏ ngẫu nhiên một phần thứ nó nhận: không đòi lần nào cũng giữ
			t.Error("the pool never gave back the machine of a shallow run: this case would pass with no pool at all")
		}
	})
}
