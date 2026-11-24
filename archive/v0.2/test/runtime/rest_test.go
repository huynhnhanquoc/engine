package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// A function's rest and a pattern's (15/11/2026, round 5 of the 13/11 review; runtime spread.go REST, value.Lambda
// Rest): (a, ...rest) => …, function f(...all), function (x, ...ys), an array method's callback with one; const
// { a, ...rest } = o, const [first, ...others] = list, a nested pattern after ..., an object's rest of a string,
// assignments ({ b: p, ...q } = o) and [r, ...s] = text (a string's code points, as JavaScript's iterator gives),
// a destructured parameter with a rest. A function's length leaves its rest out. They were compile errors, the rest
// to be read key by key or with arr.slice(n). CONTROLS: a rest parameter left unbound, red; REST leaving out no key,
// red.
func TestARestGathersWhatTheOthersLeave(t *testing.T) {
	program := load(t, `const f = (a, ...rest) => [a, rest];
function g(...all) { return all.length; }
const h = function (x, ...ys) { return ys; };
const o = { a: 1, b: 2, c: 3 };
const { a, ...rest } = o;
const [first, ...others] = [1, 2, 3];
const [x, ...[y, z]] = [7, 8, 9];
const { ...all } = "ab";
let p = 0; let q = null;
({ b: p, ...q } = o);
let r = 0; let s = null;
[r, ...s] = "a😀b";
const more = ({ a, ...others }) => others;
const result = [f(1), f(1, 2, 3), g(), g(1, 2), h(0, 'a'), [1, 2].map((...xs) => xs.length), ((a, ...b) => 0).length,
  f(...[7, 8, 9]), a, rest, first, others, x, y, z, all, p, q, r, s, more(o), [...[]].length];`)
	program.run(t)
	want := `array [[1,[]],[1,[2,3]],0,2,["a"],[3,3],1,[7,[8,9]],1,{"b":2,"c":3},1,[2,3],7,8,9,{"0":"a","1":"b"},2,{"a":1,"c":3},"a",["😀","b"],{"b":2,"c":3},0]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// What has no rest to give is the pattern's TYPE failure, as JavaScript throws: an object's rest of null, an array's of
// what is not iterable (an object, a number), which since 18/11/2026 an array pattern meets as it iterates the value,
// before its rest (ITEMS, bytecode 10), as JavaScript's does.
func TestARestOfNothingIsItsFailure(t *testing.T) {
	program := load(t, `const failed = (f) => { const r = safe(f); return r.error.code + " " + r.error.message; };
const result = [failed(() => { const { ...n } = null; return n; }), failed(() => { const [...n] = { a: 1 }; return n; }),
  failed(() => { const [a, ...n] = 5; return n; })];`)
	program.run(t)
	want := `array ["TYPE ...: không tách được phần còn lại của null","TYPE mẫu mảng [ … ] tách theo phép lặp: object không lặp được; chỉ mảng, chuỗi hay bytes","TYPE mẫu mảng [ … ] tách theo phép lặp: number không lặp được; chỉ mảng, chuỗi hay bytes"]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A rest stands where JavaScript has it, each other place a compile error saying so: the last parameter and the last
// element, a name (or in an array a nested pattern), with no default.
func TestARestStandsLast(t *testing.T) {
	for source, want := range map[string]string{
		`const f = (...a, b) => a;`:           "phần còn lại (...tên) là tham số cuối",
		`function f(...[a, b]) { return a; }`: "chưa có tách trong phần còn lại",
		`const { ...a, b } = {};`:             "phần còn lại (...tên) là phần cuối của mẫu tách",
		`const [...a, b] = [];`:               "phần còn lại (...tên) là phần cuối của mẫu tách",
		`const { ...{ a } } = {};`:            "phần còn lại của mẫu tách object là một tên",
		`const [...a = []] = [];`:             "phần còn lại (...tên) không có giá trị mặc định",
		`let a = 0; [...a, x] = [];`:          "phần còn lại (...tên) là phần cuối của mẫu tách",
		`const x = (...a);`:                   "không đứng trong ngoặc đơn",
	} {
		if _, err := compiler.CompileSource(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", source, err, want)
		}
	}
}

// A rest makes an array or an object, counted by what the run makes: a function gathering 50,000 arguments twice, and
// an object's rest of 50,000 keys, stop at a ceiling of 4 MiB.
func TestARestCountsWhatItMakes(t *testing.T) {
	for _, source := range []string{
		`const a = Array.from({ length: 50000 }, (_, j) => j); const f = (...xs) => xs; const p = [f(...a), f(...a), f(...a)];`,
		`const a = Array.from({ length: 50000 }, (_, j) => j); const { ...o } = a; const { ...p } = a;`,
	} {
		program := load(t, source)
		program.vm.FastReset(program.program, program.globals)
		program.vm.MaxBytes = 4 << 20
		if result := program.vm.Run(); !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit Exceeded") {
			t.Errorf("%s: ended %v, want the run's memory spent", source, result)
		}
	}
}
