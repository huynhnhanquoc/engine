package runtime_test

import (
	"sync"
	"testing"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Lời gọi phương thức (23/11/2026): đối số là khúc ngăn xếp của chúng (không cấp phát), tên của mỗi lệnh INVOKE tra
// một lần cho hàm (core/javascript/names.go, compiler.Function.Resolved), meter đưa thẳng xuống (không ép kiểu).

func compileSource(t *testing.T, source string) *compiler.Function {
	t.Helper()
	program, err := syntax.Parse("calls.work", source)
	if err != nil {
		t.Fatal(err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return main
}

// Một nghìn lời gọi phương thức có đối số không cấp phát mảng đối số nào: số lần cấp phát của cả lượt chạy không lớn
// theo số lời gọi (trước: một mảng mỗi lời gọi).
func TestMethodCallsAllocateNoArguments(t *testing.T) {
	main := compileSource(t, `const pair = [1, 2];
let n = 0;
for (let i = 0; i < 1000; i++) { n = n + "abc".indexOf("c", 1) + Math.max(i, 2) + pair.at(-1); }
return n;`)
	allocs := testing.AllocsPerRun(20, func() {
		if r := runtime.New(runtime.Options{}).Run(main); r.Number() != 503503 { // node cho cùng chương trình
			t.Fatal(r, r.Error())
		}
	})
	// không có gì mỗi lời gọi: chỉ cái của lượt chạy (machine, ngăn xếp, pair)
	if allocs > 50 {
		t.Errorf("%.0f allocations for 3000 method calls", allocs)
	}
}

// Cùng một lệnh INVOKE gặp this của mọi loại: mỗi loại đi bảng của nó (một tên tra sẵn cho mọi loại, không chỉ loại
// gặp đầu tiên).
func TestMethodNamesPerKind(t *testing.T) {
	m := runtime.New(runtime.Options{})
	f := m.Run(compileSource(t, `return (x) => x.toString();`))
	for _, c := range []struct {
		this value.Value
		want string
	}{
		{value.String("abc"), "abc"},
		{value.Number(12.5), "12.5"},
		{value.Boolean(true), "true"},
		{value.Array(value.Number(1), value.Number(2)), "1,2"},
		{value.Object(), "[object Object]"},
		{value.Object().Set("toString", m.Run(compileSource(t, `return () => "own";`))), "own"},
		{value.String("again"), "again"},
	} {
		if r := m.Call(f, c.this); r.String() != c.want {
			t.Errorf("(%v).toString(): got %q %v, want %q", c.this, r.String(), r.Error(), c.want)
		}
	}
	if r := m.Call(f, value.Null()); r.Code() != value.TypeCode {
		t.Errorf("null.toString(): got %v", r)
	}
}

// Một hàm của site được nhiều request chạy cùng lúc: tên của nó tra một lần, đọc chung (go test -race).
func TestMethodNamesShared(t *testing.T) {
	main := compileSource(t, `return (s, i) => s.slice(i).length + s.indexOf("c") + Math.min(i, 9);`)
	f := runtime.New(runtime.Options{}).Run(main) // closure dựng một lần, như lúc boot
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := runtime.New(runtime.Options{})
			for i := range 200 {
				r := m.Call(f, value.String("abcdef"), value.Number(float64((i+g)%6)))
				if want := float64(6-(i+g)%6) + 2 + float64((i+g)%6); r.Number() != want {
					t.Errorf("got %v %v, want %v", r, r.Error(), want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Make (bench_test.go: object, mảng, chuỗi, map, filter, JSON) cấp phát không quá ngân sách của nó. Số lần cấp phát
// là tất định (một bản Go), nên ngân sách sát: mỗi phần của lát 23/11 (khoá không qua strconv, một mảng đối số cho cả
// vòng gọi lại, mảng mới không chép lại, chuỗi không đóng vào interface, chữ của số nhỏ dựng sẵn, và object literal
// mượn khoá của một Shape, JSON đọc khoá ấy không chép) bỏ đi là vượt. Trước 23/11: 5043 lần.
func TestMakeAllocates(t *testing.T) {
	main := compileSource(t, makeSource)
	allocs := testing.AllocsPerRun(20, func() {
		if r := runtime.New(runtime.Options{}).Run(main); r.Number() != 11890 {
			t.Fatal(r, r.Error())
		}
	})
	if allocs > 1500 {
		t.Errorf("%.0f allocations, the budget is 1500 (before 23/11: 5043)", allocs)
	}
}

// { ...o } (bench_test.go: spreadSource) mượn khoá của o, và các khoá viết sau ghi thẳng vào object đang dựng, không
// object tạm (24/11/2026): bỏ phần nào là vượt ngân sách. Trước 24/11: 7842 lần.
func TestSpreadAllocates(t *testing.T) {
	main := compileSource(t, spreadSource)
	allocs := testing.AllocsPerRun(20, func() {
		if r := runtime.New(runtime.Options{}).Run(main); r.Number() != 12050 {
			t.Fatal(r, r.Error())
		}
	})
	if allocs > 1700 {
		t.Errorf("%.0f allocations, the budget is 1700 (before 24/11: 7842)", allocs)
	}
}

// JSON.parse (bench_test.go: parseSource) dựng mỗi object và mỗi mảng một lần đúng kích thước, và các object cùng khoá
// mượn chung khoá (24/11/2026): bỏ phần nào là vượt ngân sách. Trước 24/11: 16593 lần.
func TestParseAllocates(t *testing.T) {
	main := compileSource(t, parseSource)
	allocs := testing.AllocsPerRun(10, func() {
		if r := runtime.New(runtime.Options{}).Run(main); r.Number() != 40465 {
			t.Fatal(r, r.Error())
		}
	})
	if allocs > 11000 {
		t.Errorf("%.0f allocations, the budget is 11000 (before 24/11: 16593)", allocs)
	}
}
