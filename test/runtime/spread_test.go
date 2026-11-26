package runtime_test

import (
	goruntime "runtime"
	"testing"

	"github.com/huynhnhanquoc/engine/core/runtime"
)

// runBytes là số byte Go cấp cho một lần chạy f, trung bình của runs lần, sau một lần chạy không đo.
func runBytes(t *testing.T, runs int, f func()) uint64 {
	t.Helper()
	f()
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	for range runs {
		f()
	}
	goruntime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

// [...a], a.concat(…) và flatMap chép các phần tử của mảng nguồn đúng một lần (26/11/2026: đọc bằng ReadItems rồi
// append; trước đó Items chép một lần, append chép lần nữa). Đọc không chép không được làm kết quả dính vào nguồn.

// Mảng kết quả là của riêng nó: ghi vào nó không đổi nguồn, nguồn lớn thêm không đổi nó. Kết quả là của node.
func TestASpreadArrayIsItsOwn(t *testing.T) {
	main := compileSource(t, `const a = [1, 2, 3];
const b = [...a, 4];
const c = a.concat(a, [5]);
const d = [a, a].flatMap((x) => x);
const e = [...a];
const f = a.concat();
b[0] = 10; c[1] = 20; d[2] = 30; e[0] = 40; f[1] = 50;
a.push(9);
return JSON.stringify([a, b, c, d, e, f]);`)
	const want = `[[1,2,3,9],[10,2,3,4],[1,20,3,1,2,3,5],[1,2,30,1,2,3],[40,2,3],[1,50,3]]`
	if r := runtime.New(runtime.Options{}).Run(main); r.String() != want {
		t.Errorf("got %q %v, want %q", r.String(), r.Error(), want)
	}
}

// Số byte một lượt chạy làm ra khi trải, nối, và flatMap một mảng 100 phần tử 200 lần: một bản chép mỗi lần, không
// phải hai. Mỗi ca có trần giữa số đo được (26/11/2026) và số của cách chép hai lần.
func TestASpreadCopiesItsItemsOnce(t *testing.T) {
	for _, c := range []struct {
		name, source string
		want         float64
		bytes        uint64 // trần byte một lượt chạy
	}{
		{"[...a, i]", `const a = Array.from({ length: 100 }, (_, i) => i);
let n = 0;
for (let i = 0; i < 200; i++) { const b = [...a, i]; n = n + b.length; }
return n;`, 20200, spreadBytes},
		{"a.concat(a)", `const a = Array.from({ length: 100 }, (_, i) => i);
let n = 0;
for (let i = 0; i < 200; i++) { const b = a.concat(a); n = n + b.length; }
return n;`, 40000, concatBytes},
		{"flatMap", `const a = Array.from({ length: 100 }, (_, i) => i);
const pair = [a, a];
let n = 0;
for (let i = 0; i < 200; i++) { const b = pair.flatMap((x) => x); n = n + b.length; }
return n;`, 40000, flatMapBytes},
	} {
		main := compileSource(t, c.source)
		got := runBytes(t, 20, func() {
			if r := runtime.New(runtime.Options{}).Run(main); r.Number() != c.want {
				t.Fatal(c.name, r, r.Error())
			}
		})
		t.Logf("%s: %d bytes a run", c.name, got)
		if got > c.bytes {
			t.Errorf("%s: %d bytes a run, the ceiling is %d: its items are copied more than once", c.name, got, c.bytes)
		}
	}
}

// Trần của ba ca trên. Đo 26/11/2026, một bản chép rồi hai bản chép: [...a, i] 0,73 và 1,43 MB; a.concat(a) 1,33 và
// 2,79 MB (chỉ mảng đối số chép hai lần: 1,97 MB); flatMap 2,13 và 3,52 MB.
const (
	spreadBytes  = 1 << 20
	concatBytes  = 1_700_000
	flatMapBytes = 2_800_000
)
