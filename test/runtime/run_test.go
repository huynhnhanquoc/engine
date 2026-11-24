package runtime_test

import (
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Lát runtime đầu (23/11/2026): mỗi ca là một chương trình chạy từ đầu tới return, kết quả so bằng chữ JSON của nó.
// Lỗi so bằng mã và chỗ.

// run biên dịch và chạy source, trả giá trị return (hay lỗi biên dịch, lỗi lúc chạy).
func run(t *testing.T, source string, energy int64) (value.Value, error) {
	t.Helper()
	program, err := syntax.Parse("test.work", source)
	if err != nil {
		return value.Value{}, err
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		return value.Value{}, err
	}
	result := runtime.New(runtime.Options{Energy: energy}).Run(main)
	if result.IsInvalid() {
		return result, nil
	}
	return result, nil
}

// text là chữ của v để so: JSON.stringify qua chính runtime.
func text(t *testing.T, v value.Value) string {
	t.Helper()
	program, _ := syntax.Parse("text.work", "return (x) => JSON.stringify(x);")
	main, _ := compiler.Compile(program, compiler.Options{})
	m := runtime.New(runtime.Options{})
	stringify := m.Run(main)
	out := m.Call(stringify, v)
	if out.IsInvalid() {
		return "!" + out.Error().Error()
	}
	if out.IsUndefined() {
		return "undefined"
	}
	return out.String()
}

func TestPrograms(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		{"numbers", `return 1 + 2 * 3 - 4 / 2 % 3;`, `5`},
		{"power is right to left", `return 2 ** 3 ** 2;`, `512`},
		{"strings", `const a = "kit"; return a + "work" + 1;`, `"kitwork1"`},
		{"template", "const n = 3; return `n=${n} next=${n + 1}`;", `"n=3 next=4"`},
		{"template of an object", "return `${[1, 2]}|${null}|${undefined}`;", `"1,2|null|undefined"`},
		{"comparison", `return [1 < 2, "b" < "a", 2 <= 2, "10" > 9, NaN < 1];`, `[true,false,true,true,false]`},
		{"strict equality", `return [1 === 1, "1" === 1, null === undefined, NaN === NaN];`, `[true,false,false,false]`},
		{"logical keeps the value", `return [0 || "x", 1 && "y", null ?? "z", 0 ?? "w", "" && 1];`, `["x","y","z",0,""]`},
		{"conditional", `const a = 5; return a > 3 ? "big" : "small";`, `"big"`},
		{"unary", `return [!0, -"3", +"4", typeof 1, typeof "a", typeof null, typeof undefined];`, `[true,-3,4,"number","string","object","undefined"]`},
		{"object and member", `const o = { a: 1, "b c": 2, 3: "x" }; return [o.a, o["b c"], o[3], o.missing];`, `[1,2,"x",null]`},
		{"shorthand", `const web = 1; const db = 2; return { web, db };`, `{"web":1,"db":2}`},
		{"assignment", `let a = 1; a = a + 1; a += 3; const o = { n: 1 }; o.n += 5; o.m = 2; const l = [1, 2]; l[0] = 9; l[2] = 3; return [a, o, l];`, `[5,{"n":6,"m":2},[9,2,3]]`},
		{"string index and length", `const s = "héllo"; return [s.length, s[1], s.toUpperCase(), [1, 2, 3].length];`, `[5,"é","HÉLLO",3]`},
		{"closure keeps its variable", `let count = 0; const next = () => { count = count + 1; return count; }; next(); next(); return [next(), count];`, `[3,3]`},
		{"a function calls one declared after it", `const f = () => g() + 1; const g = () => 41; return f();`, `42`},
		{"recursion", `const fib = (n) => n < 2 ? n : fib(n - 1) + fib(n - 2); return fib(15);`, `610`},
		{"default parameters", `const f = (a, b = a * 2) => [a, b]; return [f(1), f(1, 5), f(1, undefined)];`, `[[1,2],[1,5],[1,2]]`},
		{"missing arguments are undefined", `const f = (a, b) => b; return f(1) === undefined;`, `true`},
		{"nested closures", `const make = (a) => (b) => (c) => a + b + c; return make(1)(2)(3);`, `6`},
		{"if and else", `const sign = (n) => { if (n < 0) { return "-"; } else if (n === 0) return "0"; return "+"; }; return [sign(-2), sign(0), sign(5)];`, `["-","0","+"]`},
		{"block scope", `let a = 1; { let a = 2; a = 3; } return a;`, `1`},
		{"methods of JavaScript", `return [Math.max(1, 5, 3), [3, 1, 2].map((x) => x * 2), "a-b".split("-"), JSON.parse("[1]")];`, `[5,[6,2,4],["a","b"],[1]]`},
		{"a method read as a value", `const up = "abc".toUpperCase; return up();`, `"ABC"`},
		{"return without value", `return;`, `undefined`},
		{"asi", "const a = 1\nconst b = 2\nreturn a + b", `3`},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, err := run(t, c.source, 0)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if v.IsInvalid() {
				t.Fatalf("%v", v.Error())
			}
			if got := text(t, v); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		// lỗi biên dịch: chỗ của chúng
		{"not defined", "const a = 1;\nreturn b;", "test.work:2:8: b is not defined: declare it with const or let, or import it"},
		{"assign to const", "const a = 1;\na = 2;", "test.work:2:1: cannot assign to a: it is a const"},
		{"used before its declaration", "const a = b;\nconst b = 1;", "test.work:1:11: b is used before its declaration at 2:7"},
		{"declared twice", "let a = 1;\nlet a = 2;", "test.work:2:5: a is declared twice: first at 1:5"},
		{"trailing comma in an array", "return [1, 2,];", "test.work:1:14: an array takes no comma after its last item"},
		{"while", "while (true) {}", "test.work:1:1: Kitwork has no while: loops are for (let i = 0; i < n; i++) and for (const x of list)"},
		{"loose equality", "return 1 == 1;", "test.work:1:10: write ===: Kitwork compares with === and !==, the loose == comes in a later slice if ever"},
		{"var", "var a = 1;", "test.work:1:1: Kitwork has no var: write const, or let when the name changes"},
		{"function", "function f() {}", "test.work:1:1: Kitwork has arrow functions only: write const f = (…) => …"},
		{"an unknown module", `import { x } from "kitwork/nope";`, `test.work:1:1: there is no module "kitwork/nope": `},
		{"a string never closed", `return "abc;`, "test.work:1:8: a string is never closed: end it with \""},
		// lỗi lúc chạy: mã và chỗ của lệnh
		{"reading undefined", "const o = {};\nreturn o.a.b;", "TYPE test.work:2:12: cannot read properties of undefined (reading \"b\")"},
		{"calling what is not a function", "const a = 1;\nreturn a();", "TYPE test.work:2:8: number is not a function"},
		{"a range error", `return "x".repeat(-1);`, `RANGE test.work:1:12: invalid count value: -1`},
		{"too deep", "const f = (n) => f(n + 1);\nreturn f(0);", "RANGE test.work:1:18: maximum call stack size exceeded"},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, err := run(t, c.source, 0)
			got := ""
			switch {
			case err != nil:
				got = err.Error()
			case v.IsInvalid():
				got = v.Code() + " " + v.Error().Error()
			default:
				got = "no error: " + text(t, v)
			}
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestEnergy(t *testing.T) {
	source := `const fib = (n) => n < 2 ? n : fib(n - 1) + fib(n - 2); return fib(20);`
	v, err := run(t, source, 1000)
	if err != nil || !v.IsInvalid() || v.Code() != value.EnergyCode || !strings.Contains(v.Error().Error(), "the run used all its energy") {
		t.Fatalf("a small energy stops the run: %v %v", err, v)
	}
	if v, _ := run(t, source, 0); v.IsInvalid() || v.Number() != 6765 {
		t.Errorf("the default energy runs it: %v", v)
	}
}
