package runtime_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Một ô của khung chưa được ghi là undefined (25/11/2026): một đối số thiếu, một biến let chưa gán. Khung nằm trên ngăn
// xếp chung, nơi một lời gọi sâu trước đó đã để lại giá trị của nó ở trên đỉnh; execute xoá các ô của khung khi vào
// (interpreter.go, vòng "len(m.stack) < base+f.Locals"). Không xoá thì đối số thiếu đọc ra giá trị của lời gọi trước:
// sai JavaScript trong một lượt chạy, và là hàng chắn thứ hai, sau Release, giữa hai lượt chạy dùng chung một máy.
func TestAMissingArgumentIsUndefinedAfterADeepCall(t *testing.T) {
	deep := compileSource(t, `const down = (n, a, b, c) => n === 0 ? 0 : 1 + down(n - 1, "left behind " + n, a, b);
return down(300, "x", "y", "z");`)
	reads := compileSource(t, `const seen = (v) => v === undefined ? 0 : 1;
const three = (a, b, c) => seen(b) + seen(c);
const locals = () => { let x; let y; let z; return seen(x) + seen(y) + seen(z); };
const eight = (a, b, c, d, e, f, g, h) => seen(b) + seen(e) + seen(h);
return three(1) + locals() + eight(1);`)
	fromGo := compileSource(t, `return (a, b, c, d) => (b === undefined ? 0 : 1) + (c === undefined ? 0 : 1) + (d === undefined ? 0 : 1);`)
	check := func(m *runtime.Machine, when string) {
		t.Helper()
		if r := m.Run(reads); r.IsInvalid() || r.Number() != 0 {
			t.Errorf("%s: a program reads %v values it never wrote (%v)", when, r.Number(), r.Error())
		}
		f := m.Run(fromGo)
		if r := m.Call(f, value.Number(1)); r.IsInvalid() || r.Number() != 0 {
			t.Errorf("%s: a function called from Go with one argument of four reads %v values (%v)", when, r.Number(), r.Error())
		}
	}
	oneProcessor(t, func() {
		reused := 0
		for range 5 {
			m := runtime.Take(runtime.Options{Energy: 1 << 30})
			if r := m.Run(deep); r.Number() != 300 {
				t.Fatalf("the deep call: %v %v", r, r.Error())
			}
			check(m, "after a deep call on the same machine")
			if r := m.Run(deep); r.Number() != 300 {
				t.Fatalf("the deep call: %v %v", r, r.Error())
			}
			m.Release()
			n := runtime.Take(runtime.Options{Energy: 1 << 30})
			if n == m {
				reused++
			}
			check(n, "on a machine taken after a deep call")
			n.Release()
		}
		if reused == 0 {
			t.Error("Take never gave back the machine Release returned")
		}
	})
}
