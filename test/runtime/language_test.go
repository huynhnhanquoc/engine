package runtime_test

import (
	"strings"
	"testing"
)

// Lát hai của runtime (23/11/2026): vòng lặp, ++ và --, giải cấu trúc, spread, đủ cho application.work mẫu của
// docs/host/blueprint. Mỗi ca chạy từ đầu tới return, so bằng chữ JSON.

func TestLanguage(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		// --- vòng lặp ---
		{"counting for", `let sum = 0; for (let i = 0; i < 5; i++) { sum += i; } return sum;`, `10`},
		{"counting down", `const seen = []; for (let i = 3; i > 0; i -= 1) seen.push(i); return seen;`, `[3,2,1]`},
		{"for of", `let text = ""; for (const word of ["a", "b", "c"]) { text = text + word; } return text;`, `"abc"`},
		{"for of a string, by character", "const seen = []; for (const c of \"a😀b\") seen.push(c); return seen.length;", `3`},
		{"for of with a pattern", `let total = 0; for (const { price, count = 1 } of [{ price: 2, count: 3 }, { price: 5 }]) total += price * count; return total;`, `11`},
		{"break and continue", `const seen = []; for (let i = 0; i < 10; i++) { if (i === 2) continue; if (i === 5) break; seen.push(i); } return seen;`, `[0,1,3,4]`},
		{"break in for of", `let last = null; for (const x of [1, 2, 3, 4]) { last = x; if (x === 2) break; } return last;`, `2`},
		{"nested loops", `let n = 0; for (let i = 0; i < 3; i++) { for (const x of [1, 2]) { if (x === 2) continue; n++; } } return n;`, `3`},
		{"a closure per turn of for of", `const fs = []; for (const x of [1, 2, 3]) fs.push(() => x); return fs.map((f) => f());`, `[1,2,3]`},
		{"a closure per turn of a counting for", `const fs = []; for (let i = 0; i < 3; i++) fs.push(() => i); return fs.map((f) => f());`, `[0,1,2]`},
		{"a closure per turn of a block", `const fs = []; for (let i = 0; i < 2; i++) { const k = i * 10; fs.push(() => k); } return fs.map((f) => f());`, `[0,10]`},
		{"the list grows while it is read", `const l = [1]; for (const x of l) { if (x < 3) l.push(x + 1); } return l;`, `[1,2,3]`},
		// --- ++ và -- ---
		{"postfix and prefix", `let a = 1; const b = a++; const c = ++a; const d = a--; return [a, b, c, d];`, `[2,1,3,3]`},
		{"++ makes a number", `let s = "5"; s++; let n = null; n--; return [s, n];`, `[6,-1]`},
		{"++ on a member and an index", `const o = { n: 1 }; const l = [5]; const old = o.n++; ++l[0]; l[0]--; return [o.n, old, l[0], ++o.n];`, `[2,1,5,3]`},
		// --- giải cấu trúc ---
		{"pattern", `const { a, b: c, d = 4, e = 5 } = { a: 1, b: 2, e: null }; return [a, c, d, e];`, `[1,2,4,null]`},
		{"nested pattern", `const { request: { hostname, path = "/" } } = { request: { hostname: "shop.vn" } }; return [hostname, path];`, `["shop.vn","/"]`},
		{"a default reads the names before it", `const { a, b = a + 1 } = { a: 1 }; return b;`, `2`},
		{"let pattern", `let { x } = { x: 1 }; x = x + 1; return x;`, `2`},
		{"a key written as text", `const { "content-type": type } = { "content-type": "text/html" }; return type;`, `"text/html"`},
		// --- spread ---
		{"array spread", `const a = [2, 3]; return [1, ...a, 4, ..."ab"];`, `[1,2,3,4,"a","b"]`},
		{"object spread, the later key wins", `const o = { a: 1, b: 2 }; return { ...o, b: 3, ...null, c: 4 };`, `{"a":1,"b":3,"c":4}`},
		{"a copy of a frozen object is free", `const o = Object.freeze({ a: 1 }); const copy = { ...o }; copy.a = 2; return [o.a, copy.a];`, `[1,2]`},
		{"spread of a map", `const tenants = [{ id: "a" }, { id: "b" }]; return [".schedules", ...tenants.map((t) => "apps/" + t.id)];`, `[".schedules","apps/a","apps/b"]`},
		// --- object literal mượn khoá của một Shape (23/11/2026): kết quả như node ---
		{"a literal's objects are each their own", `const make = (i) => ({ id: i, name: "n" + i });
const a = make(1), b = make(2); a.extra = true; b.name = "changed";
return [JSON.stringify([a, b, make(3)]), Object.keys(a).join(), Object.keys(b).join()];`,
			`["[{\"id\":1,\"name\":\"n1\",\"extra\":true},{\"id\":2,\"name\":\"changed\"},{\"id\":3,\"name\":\"n3\"}]","id,name,extra","id,name"]`},
		{"a key written twice", `return JSON.stringify({ x: 1, y: 2, x: 3 });`, `"{\"x\":3,\"y\":2}"`},
		{"index keys come first", `const q = { 2: "two", b: "bee", 0: "zero" }; return [JSON.stringify(q), Object.keys(q).join()];`,
			`["{\"0\":\"zero\",\"2\":\"two\",\"b\":\"bee\"}","0,2,b"]`},
		{"a literal of ten keys", `const r = (i) => ({ k1: i, k2: i, k3: i, k4: i, k5: i, k6: i, k7: i, k8: i, k9: i, k10: i });
const s = r(1), t = r(2); s.k11 = 11; t.k5 = 50;
return [JSON.stringify(s), JSON.stringify(t), s.k9, s.k11, t.k3, t.k11];`,
			`["{\"k1\":1,\"k2\":1,\"k3\":1,\"k4\":1,\"k5\":1,\"k6\":1,\"k7\":1,\"k8\":1,\"k9\":1,\"k10\":1,\"k11\":11}","{\"k1\":2,\"k2\":2,\"k3\":2,\"k4\":2,\"k5\":50,\"k6\":2,\"k7\":2,\"k8\":2,\"k9\":2,\"k10\":2}",1,11,2,null]`},
		{"toJSON changes the object being written", `const o = { a: 1, b: { toJSON: () => { o.d = 4; o.c = 30; return "B"; } }, c: 3 };
return JSON.stringify(o);`, `"{\"a\":1,\"b\":\"B\",\"c\":30}"`},
		// --- { ...o } mượn khoá của o, dựng theo thứ tự viết (24/11/2026): kết quả như node ---
		{"a spread is copied where it is written", `const o = { x: 1 }; const set = () => { o.x = 5; return 2; };
const c = { ...o, a: set(), y: o.x }; return [c, o.x];`, `[{"x":1,"a":2,"y":5},5]`},
		{"keys written before a spread", `const o = { a: 9, b: 2 }; return { a: 1, ...o, c: 3 };`, `{"a":9,"b":2,"c":3}`},
		{"two spreads", `const o = { a: 1, b: 2 }; const p = { b: 3, c: 4 }; return { ...o, ...p, d: 5 };`, `{"a":1,"b":3,"c":4,"d":5}`},
		{"a key written twice after a spread", `const o = { a: 1 }; return { ...o, b: 1, b: 2, a: 3 };`, `{"a":3,"b":2}`},
		{"index keys of a spread come first", `const o = { b: 1, 2: "x" }; return [{ ...o, 1: "y" }, Object.keys({ ...o })];`,
			`[{"1":"y","2":"x","b":1},["2","b"]]`},
		{"spread of arrays, strings and the rest", `return { ...[7, 8, 9], ..."hi", ...null, ...undefined, ...5, ...true };`,
			`{"0":"h","1":"i","2":9}`},
		{"a copy and its source are each their own", `const o = { a: 1 }; const c = { ...o }; c.b = 2; o.c = 3; c.a = 10; return [o, c];`,
			`[{"a":1,"c":3},{"a":10,"b":2}]`},
		{"a copy of an object built key by key", `const o = {}; o.a = 1; o.b = 2; const c = { ...o }; o.d = 4; c.e = 5;
const d = { ...o }; return [o, c, d, Object.keys(c).join()];`,
			`[{"a":1,"b":2,"d":4},{"a":1,"b":2,"e":5},{"a":1,"b":2,"d":4},"a,b,e"]`},
		{"a copy of a wide object", `const o = {}; for (let i = 0; i < 12; i++) { o["k" + i] = i; }
const c = { ...o, k3: 30, extra: 1 }; const d = Object.assign({}, o); d.k0 = -1;
return [c.k11, c.k3, o.k3, o.extra, Object.keys(c).length, Object.keys(o).length, d.k0, o.k0, JSON.stringify(c)];`,
			`[11,30,3,null,13,12,-1,0,"{\"k0\":0,\"k1\":1,\"k2\":2,\"k3\":30,\"k4\":4,\"k5\":5,\"k6\":6,\"k7\":7,\"k8\":8,\"k9\":9,\"k10\":10,\"k11\":11,\"extra\":1}"]`},
		{"a spread in a function its calls share", `const tag = (row) => ({ ...row, seen: true });
const a = tag({ id: 1 }), b = tag({ id: 2, x: 0 }); a.more = 1; return [a, b, tag({})];`,
			`[{"id":1,"seen":true,"more":1},{"id":2,"x":0,"seen":true},{"seen":true}]`},
		{"a state updated copy by copy", `let s = { n: 0, last: null }; const first = s;
for (let i = 0; i < 3; i++) { s = { ...s, n: s.n + 1, last: i }; } return [first, s];`, `[{"n":0,"last":null},{"n":3,"last":2}]`},
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

func TestLanguageErrors(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		{"an endless for", "for (;;) {}", "test.work:1:6: loops are for (let i = 0; i < n; i++) and for (const x of list)"},
		{"a for without a condition", "for (let i = 0; ; i++) {}", "test.work:1:17: a for takes a condition, i < n: Kitwork has no endless loop"},
		{"an update of another name", "let j = 0; for (let i = 0; i < 3; j++) {}", "test.work:1:36: the update of a counting for changes its counter: i++, i--, i += n or i -= n"},
		{"a const counter", "for (const i = 0; i < 3; i++) {}", "test.work:1:6: a counting for changes its counter: for (let i = 0; i < n; i++)"},
		{"let in for of", "for (let x of [1]) {}", "test.work:1:6: write for (const x of …): the name is new at each turn"},
		{"break outside a loop", "break;", "test.work:1:1: break is written inside a loop"},
		{"break in a function inside a loop", "for (const x of [1]) { const f = () => { break; }; }", "test.work:1:42: break is written inside a loop"},
		{"++ on a const", "const a = 1;\na++;", "test.work:2:1: cannot assign to a: it is a const"},
		{"++ on a call", "const f = () => 1;\nf()++;", "test.work:2:4: ++ takes a name, a.b or a[b]"},
		{"a pattern without a value", "const { a };", "test.work:1:7: a pattern takes a value: const { … } = …"},
		{"a name twice in a pattern", "const { a, b: a } = {};", "test.work:1:15: a is declared twice: first at 1:9"},
		{"for of something not iterable", "for (const x of 5) {}", "TYPE test.work:1:1: number is not iterable: for…of takes an array or a string"},
		{"spread of a number", "return [...5];", "TYPE test.work:1:8: number is not iterable: ... spreads an array or a string"},
		{"spread of a library", "return { ...Math };", "TYPE test.work:1:10: Math is a library, not data"},
		{"spread of a library after a key", "return { a: 1, ...Math };", "TYPE test.work:1:16: Math is a library, not data"},
		{"export in the program", "export const a = 1;", "test.work:1:14: this file exports nothing: it is the program, it returns its blueprint"},
		{"spread in a call", "const f = () => 1; f(...[1]);", "test.work:1:22: spread in a call, f(...list), comes in a later slice of the engine3 runtime"},
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
