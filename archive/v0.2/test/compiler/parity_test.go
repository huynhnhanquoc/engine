package compiler_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The compiler and the runtime held to node case by case, for what they were made to do as JavaScript
// does (03/11/2026): == and != loose, a string's and a template's escapes, the spellings of a number, the
// name after a dot, the value of an assignment, the array an array method hands its callback. Each case
// runs as the corpus runs one, `const result = (src);` in a strict function, and both sides describe the
// result alike (describe, corpus_test.go); a rejection at compile or run time is "throws" on both.
//
// In a case's text @ stands for a backslash, so this file holds no escape sequence of its own: a tool
// that writes the file would turn one into its character (a lone surrogate then breaks the file).
var parityCases = []string{
	// == and != are loose; ===, !==, switch, ?? and a default stay strict
	`null == undefined`,
	`undefined != null`,
	`null == 0`,
	`undefined == 0`,
	`null == false`,
	`'' == 0`,
	`'0' == false`,
	`'1' == true`,
	`true == 'true'`,
	`1 == '1'`,
	`1 != '1'`,
	`1 === '1'`,
	`1 !== '1'`,
	`'1e3' == 1000`,
	`' 1 ' == 1`,
	`0 == -0`,
	`NaN == NaN`,
	`NaN != NaN`,
	`[1] == 1`,
	`[1, 2] == '1,2'`,
	`({}) == '[object Object]'`,
	`[] == ![]`,
	`[] == []`,
	`null === undefined`,
	`!0 == 1`,
	`(() => { const o = {}; return [o.missing == null, null != o.missing, o.missing === null] })()`,
	`(() => { switch ('1') { case 1: return 'loose'; default: return 'strict' } })()`,
	`[0 ?? 5, '' ?? 5, false ?? 5, null ?? 5]`,
	`(() => { const { a = 5 } = { a: 0 }; return a })()`,
	`(() => { const o = { a: 0 }; return o.a?.toFixed(1) })()`,

	// escapes of a string
	`'@x41@x42'`,
	`'@x4'`,
	`'@x4g'`,
	`'@xG1'`,
	`'@u0041'`,
	`'@u004'`,
	`'@u004G'`,
	`'@u{41}'`,
	`'@u{1F600}'`,
	`'@u{1F600}'.length`,
	`'@u{10FFFF}'.length`,
	`'@u{110000}'`,
	`'@u{}'`,
	`'@u{41'`,
	`'@u{0000000041}'`,
	`'@ud83d@ude00'`,
	`'@ud83d@ude00' === '😀'`,
	`'@ud83d@ude00'.length`,
	`'@ud83d@u{de00}' === '😀'`,
	`'@ud800'.length`,
	`'@ud800' === '😀'.slice(0, 1)`,
	`'@ude00@ud83d'.length`,
	`'@ude00@ud83d'.charCodeAt(0)`,
	`'a@ud83d'.length`,
	`'@0'.length`,
	`'@0' === String.fromCharCode(0)`,
	`'@0a'.length`,
	`'@01'`,
	`'@00'`,
	`'@1'`,
	`'@7'`,
	`'@8'`,
	`'@9'`,
	`'@b@f@n@r@t@v' === String.fromCharCode(8, 12, 10, 13, 9, 11)`,
	`'@a@c@q@z'`,
	`'@'@"'`,
	`"@'@""`,
	`'@@'.length`,
	`'@é'`,
	`'@😀'.length`,
	"'a@\nb'",
	"'a@\r\nb'",
	"'a@\rb'",
	"'a@" + string(rune(0x2028)) + "b'",
	"'a@" + string(rune(0x2029)) + "b'",
	"'a\nb'",
	"'a\rb'",
	"'a" + string(rune(0x2028)) + "b'.length",
	`'abc`,

	// escapes of a template, and its pieces
	"`a@x41`",
	"`a@u{42}c`",
	"`@ud83d@ude00`.length",
	"`@x4`",
	"`@01`",
	"`@0`.length",
	"`a@${1}b`",
	"`a$@{1}b`",
	"`a@`b`",
	"`${'}'}x`",
	"`${\"`\"}x`",
	"`${`in${2}ner`}!`",
	"`${'@n'}`.length",
	"`${1}${2}`",
	"`${1 + 1} ${'x'.toUpperCase()}`",
	"`${1 2}`",
	"`a\r\nb` === 'a@nb'",
	"`a\rb` === 'a@nb'",
	"`a@\nb`",
	"`$`",
	"`${`",
	"`a",

	// numbers
	`0x1f`,
	`0X1F`,
	`0o17`,
	`0O17`,
	`0b101`,
	`0B101`,
	`0x10 + 0b10 + 0o10`,
	`1_000`,
	`1_000.5_5`,
	`0x1_F`,
	`0b1_0`,
	`1e1_0`,
	`.5`,
	`5.`,
	`.5e1`,
	`5.e2`,
	`5..toFixed(1)`,
	`1.5.toFixed(1)`,
	`0.5`,
	`0e5`,
	`1e400`,
	`-1e400`,
	`1e-400`,
	`0xFFFFFFFFFFFFFFFFFF`,
	`0x20000000000001`,
	`0b11111111111111111111111111111111111111111111111111111`,
	`1__000`,
	`1_`,
	`0x_1`,
	`1._5`,
	`1_.5`,
	`1e_5`,
	`0_1`,
	`010`,
	`08`,
	`09.5`,
	`00`,
	`1e`,
	`1e+`,
	`0x`,
	`0b12`,
	`0o8`,
	`0xg`,
	`3in`,
	`5.toFixed(1)`,

	// the name after a dot is a name, a keyword included
	`({ a: 1 }).)`,
	`({ a: 1 }).'a'`,
	`({ a: 1 }).+`,
	`({ a: 1 }).1`,
	`({ if: 1 }).if`,
	`({ true: 2 }).true`,
	`({ null: 3 }).null`,
	`({ undefined: 4 }).undefined`,
	`({ typeof: 5 }).typeof`,
	`({ new: 6 }).new`,
	`({ function: 7 }).function`,

	// an assignment gives the value it assigned
	`(() => { const obj = {}; return obj.a = 2 })()`,
	`(() => { const l = [1, 2]; return l[0] = 9 })()`,
	`(() => { const o = {}; return (o['k'] = 'v') + '!' })()`,
	`(() => { let a; let b; a = b = 3; return [a, b] })()`,
	`(() => { const o = {}; let x; x = o.a = 2; return [x, o.a] })()`,
	`(() => { const o = {}; const p = {}; o.a = p.b = 7; return [o.a, p.b] })()`,
	`(() => { let x; return [(x = 4), x] })()`,
	`(() => { const o = { n: 1 }; return [++o.n, o.n] })()`,
	`(() => { const o = { n: 1 }; return [o.n++, o.n] })()`,
	`(() => { const o = { n: '5' }; return [++o.n, o.n] })()`,
	`(() => { const o = { n: 1 }; return [o.n += 5, o.n] })()`,
	`(() => { const l = [1]; return [l[0] += 5, l] })()`,
	`(() => { const l = [1]; let i = 0; return [++l[i++], l, i] })()`,
	`(() => { let a; let b; const r = ({ a, b } = { a: 1, b: 2 }); return [a, b, r.a] })()`,
	`(() => { const o = {}; o.a = 1; o.b = 2; return o })()`,
	`(() => { const vs = []; const fs = []; for (let i = 0; i < 2; i++) { let v; fs.push(() => v); vs.push(v = i * 10) } return [vs, fs.map((f) => f())] })()`,
	`(() => { const fs = []; for (const x of [1, 2]) { let n = x; fs.push(() => n); n += 10 } return fs.map((f) => f()) })()`,

	// an array method hands its callback the item, its index and the array
	`[5].map((x, i, a) => a)`,
	`[1, 2].map((x, i, a) => a === undefined)`,
	`[1, 2].filter((x, i, a) => a.length == 2)`,
	`[1, 2].find((x, i, a) => a[i] == 2)`,
	`(() => { let seen; [7].forEach((x, i, a) => { seen = a }); return seen })()`,
	`[1, 2].some((x, i, a) => a === undefined)`,
	`[1, 2].every((x, i, a) => Array.isArray(a))`,
	`[1, 2].findIndex((x, i, a) => a[1] == x)`,
	`[1, 2, 3].reduce((acc, x, i, a) => acc + a.length, 0)`,
	`[1, 2, 3].reduce((acc, x, i, a) => acc + i + a[i])`,
	`(() => { const l = [1, 2]; return l.map((x, i, a) => a === l) })()`,
	// sort calls its comparator on the items that are not undefined, which go last, and makes its result
	// a number
	`[3, undefined, 1, undefined, 2].sort((a, b) => a - b)`,
	`(() => { let calls = 0; [undefined, 2, undefined, 1].sort((a, b) => { calls++; return a - b }); return calls })()`,
	`[3, 1, 2].sort((a, b) => String(a - b))`,
	`[3, 1, 2].sort((a, b) => (a > b ? '1' : '-1'))`,
	`[10, 9, 1, 100].sort()`,
	`[{ k: 1, n: 'a' }, { k: 0, n: 'b' }, { k: 1, n: 'c' }].sort((a, b) => a.k - b.k).map((x) => x.n)`,
	// a write past an array's end leaves holes, which read undefined
	`(() => { const a = [1]; a[3] = 4; return [a.length, typeof a[1], a[2] === undefined, a[2] === null, JSON.stringify(a)] })()`,
	`(() => { const a = [1]; a[2] = 3; const seen = []; for (const x of a) { seen.push(typeof x) } return seen })()`,
	`(() => { const a = [1]; a[2] = 3; return [a.join('-'), a[1] ?? 'none'] })()`,
	// a replacer that writes an index key into the object it is walking: the walk keeps the keys it began with
	`(() => { const o = { k0: 0, k1: 1 }; const seen = []; const text = JSON.stringify(o, (k, v) => { seen.push(k); if (k === 'k0') { o['0'] = 'z' } return v }); return [text, seen] })()`,
	`(() => { const o = { k0: 0, k1: 1, k2: 2 }; const seen = []; JSON.stringify(o, (k, v) => { seen.push(k); if (k === 'k0') { o['5'] = 'z' } return v }); return seen })()`,
	`(() => { const inner = { k0: 0, k1: 1 }; return JSON.stringify({ inner }, (k, v) => { if (k === 'k0') { inner['7'] = 'q' } return v }) })()`,
	// Object.freeze: shallow, cannot be undone; a write to a frozen object throws in strict code, as
	// Kitwork's always is; a copy by spread is an object of its own, writable
	`(() => { const o = Object.freeze({ a: 1 }); return [Object.isFrozen(o), o.a, Object.isFrozen({}), Object.isFrozen(1), Object.isFrozen('s'), Object.isFrozen(null), Object.isFrozen(() => 1)] })()`,
	`(() => { const o = Object.freeze({ a: 1 }); o.a = 2; return o.a })()`,
	`(() => { const o = Object.freeze({ a: 1 }); o.b = 2; return o.b })()`,
	`Object.assign(Object.freeze({ a: 1 }), { b: 2 })`,
	`Object.assign({ a: 1 }, Object.freeze({ b: 2 }))`,
	`(() => { const inner = { n: 1 }; const o = Object.freeze({ inner }); o.inner.n = 2; return [o.inner.n, Object.isFrozen(o.inner)] })()`,
	`(() => { const o = { a: 1 }; return [Object.freeze(o) === o, Object.freeze(Object.freeze(o)) === o, Object.freeze(5), Object.freeze('x'), Object.freeze()] })()`,
	`(() => { const o = Object.freeze({ a: 1, b: { c: 2 } }); const c = { ...o }; c.a = 3; return [o.a, c.a, Object.isFrozen(c), JSON.stringify(o), Object.keys(o)] })()`,
	`(() => { const o = Object.freeze({ a: 1 }); return [o.a + 1, o.hasOwnProperty('a'), JSON.stringify(o)] })()`,
	// typeof and unary minus, each one instruction now
	`[typeof undeclaredName, typeof 1, typeof 's', typeof null, typeof {}, typeof [], typeof (() => 1), typeof NaN, typeof typeof 1]`,
	`(() => { const o = { typeof: 1 }; return [typeof o.typeof, typeof o.missing, typeof o?.a?.b] })()`,
	`[-'5', -'', -'x', -null, -undefined, -true, -false, -[3], -[], -[1, 2]]`,
	`[-0 === 0, 1 / -0, 1 / -(-0), -(-3), - -3, -NaN, -Infinity]`,
	`(() => { const o = { valueOf: () => 4 }; const n = 5; return [-o, -n, -(n + 1), typeof -n] })()`,

	// a parameter the call did not pass is undefined, never a variable of the same name around it; an
	// argument past the parameters is dropped
	`(() => { let x = 5; const f = (x) => x; return typeof f() })()`,
	`(() => { const x = 5; const f = (x) => x; return [typeof f(), f(7)] })()`,
	`(() => { let b = 3; const f = (a, b) => [a, typeof b]; return f(1) })()`,
	`(() => { const x = 5; const f = (x) => () => typeof x; return f()() })()`,
	`(() => { const x = 5; const f = (x) => { x = x ?? 1; return x }; return [f(), f(2)] })()`,
	`(() => { const d = 9; return [1, 2].map((a, b, c, d) => typeof d) })()`,
	`(() => { const d = 9; let seen; [1].forEach((a, b, c, d) => { seen = typeof d }); return seen })()`,
	`(() => { const i = 9; return [1, 2, 3].reduce((acc, x, i, a, e) => acc + typeof e, '') })()`,
	`(() => { const b = 2; const o = { f: (a, b) => typeof b }; return o.f(1) })()`,
	`(() => { const f = (a) => a; return f(1, 2, 3) })()`,
	`(() => { const f = (a, b) => [a, b]; return f(1, 2, 3) })()`,
	`(() => { const f = () => 1; return f(1, 2) })()`,
	`[1, 2].map((x) => x * 2)`,
	// a destructured parameter, its defaults, and the default written in the body that Kitwork's
	// compile error for (x = 3) => … asks for
	`(() => { const a = 1; const f = ({ a }) => typeof a; return f({}) })()`,
	`(() => { const f = ({ a = 5 }) => a; return [f({}), f({ a: 0 }), f({ a: undefined })] })()`,
	`(() => { const b = 7; const f = ({ a = 5 }, b) => [a, typeof b]; return f({}) })()`,
	`(() => { const f = ({ a }) => a; return f() })()`,
	`(() => { let x = 4; const f = (x) => { const y = x ?? 3; return y }; return [f(), f(0), f(null)] })()`,

	// docs/language.md mục 8.1 (18/11/2026): a function declared in a case is made as the switch's block begins
	`(() => { const pick = (n) => { switch (n) { case 1: return g(); case 2: function g() { return 'g' } return 'two' } }; return [pick(1), pick(2)] })()`,
	`(() => { switch (2) { case f(): return 'f'; default: function f() { return 2 } } })()`,
	`(() => { switch (1) { case 1: function g() { return 1 } case 2: function g() { return 2 } } })()`,
	// a named function expression's name is the function inside its body, and its name
	`(() => { const f = function g() { return typeof g }; return [f(), f.name] })()`,
	`(() => { const f = function g(n) { return n == 0 ? 0 : g(n - 1) + 1 }; return f(3) })()`,
	`(() => { const g = 5; const f = function g() { return 1 }; return [typeof g, f.name, f()] })()`,
	`(() => { const f = function g() { g = 1; return typeof g }; return f() })()`,
	// a default reads the parameters before its own, never the body's names; length stops at the first default
	`(() => { const f = (a, b = 1) => [a, b]; return [f(1), f.length] })()`,
	`(() => { function f(a, b = 1, ...c) { return 0 } return f.length })()`,
	`(() => { const f = ({ a } = {}, [b] = [], c) => 0; return f.length })()`,
	`(() => { const f = (a = b, b = 1) => a; return f() })()`,
	`(() => { const f = (a = a) => a; return f() })()`,
	`(() => { const x = 1; const f = (a = () => x) => { const x = 5; return a() }; return f() })()`,
	`(() => { const f = (a = () => b, b = 2) => a(); return f() })()`,
	`(() => { const f = ({ a, b = a }) => b; return f({ a: 3 }) })()`,
	`(() => { const f = (a = 1, { b = a } = {}) => b; return f() })()`,
	// an array pattern reads what the value's iteration gives
	`(() => { const [a] = '😀'; return a })()`,
	`(() => { const [a, ...r] = '😀xy'; return [a, r] })()`,
	`(() => { const [a, [b, c]] = [1, '😀é']; return [a, b, c] })()`,
	`(() => { const [a] = 5; return a })()`,
	`(() => { const [a] = { 0: 'z', length: 1 }; return a })()`,
	`(() => { const [a] = null; return a })()`,
	`(() => { let a = 0; const r = ([a] = 'xy'); return [r, a] })()`,
	`(() => { const f = ([x, y] = [1, 2]) => [x, y]; return [f(), f('ab')] })()`,
	// delete of a key, as strict JavaScript has it
	`(() => { const o = { a: 1, b: 2 }; const r = delete o.a; return [r, Object.keys(o)] })()`,
	`(() => { const o = { a: 1 }; return [delete o.x, delete o['a'], o] })()`,
	`(() => { const o = { a: { b: 1 } }; return [delete o.a.b, o] })()`,
	`(() => { const o = Object.freeze({ a: 1 }); return delete o.a })()`,
	`(() => { const o = Object.freeze({ a: 1 }); return delete o.b })()`,
	`(() => { return delete 'abc'[0] })()`,
	`(() => { return [delete 'abc'[5], delete 'abc'.x] })()`,
	`(() => { return delete [1].length })()`,
	`(() => { const n = null; return delete n.a })()`,
	`(() => { const a = [1, 2]; return [delete a[5], delete a.foo, a] })()`,
	// a let or a const is not used before it is made: a compile error where the compiler sees it, the failure where
	// a function made before its line runs before it
	`(() => { let x = x; return x })()`,
	`(() => { const g = () => { const read = () => y; const r = read(); const y = 2; return r }; return g() })()`,
	`(() => { const read = () => y; const y = 2; return read() })()`,
	`(() => { function read() { return y } const y = 3; return read() })()`,
	`(() => { const h = (n) => { switch (n) { case 1: let y = 1; return y; case 2: return y } }; return h(2) })()`,
	`(() => { const w = () => { y = 5 }; let y = 2; w(); return y })()`,
	`(() => { const w = () => { y = 5 }; w(); let y = 2; return y })()`,
	`(() => { const r = (() => typeof y)(); const y = 1; return r })()`,

	// docs/language.md mục 8.2 (18/11/2026): a function owns its length and name
	`(() => { const f = (a, b) => a; return [Object.getOwnPropertyNames(f), Object.hasOwn(f, 'length'), Object.hasOwn(f, 'name'), Object.hasOwn(f, 'x'), Object.keys(f), f.hasOwnProperty('name')] })()`,
	`Object.getOwnPropertyNames(Math.max)`,
	// JSON.stringify calls an object's toJSON with its key, before the replacer
	`JSON.stringify({ toJSON: () => 5 })`,
	`JSON.stringify({ a: { toJSON: (k) => k + '!' } })`,
	`JSON.stringify([{ toJSON: () => undefined }, { toJSON: (k) => k }])`,
	`JSON.stringify({ a: { toJSON: () => undefined }, b: 1 })`,
	`JSON.stringify({ toJSON: () => ({ toJSON: () => 1, x: 2 }) })`,
	`JSON.stringify({ a: { toJSON: (k) => k } }, (k, v) => typeof v === 'string' ? v.toUpperCase() : v)`,
	`JSON.stringify({ a: { toJSON: () => 1 }, b: 2 }, ['a'])`,
	`JSON.stringify({ a: [{ toJSON: () => 'x' }] }, null, 2)`,
	`(() => { const o = { a: 1, b: { toJSON: () => { delete o.c; return 2 } }, c: 3 }; return JSON.stringify(o) })()`,
	`JSON.stringify({ toJSON: 5, a: 1 })`,
	`(() => { const o = { a: 1 }; o.self = o; o.toJSON = () => 'cut'; return JSON.stringify(o) })()`,
	`(() => { const o = { a: 1 }; o.self = o; return JSON.stringify(o) })()`,
	`(() => { const o = { a: { toJSON: () => { o.z = 9; return 1 } }, b: 2 }; return [JSON.stringify(o), Object.keys(o)] })()`,
	// a method runs on the value it is called on; read off it and called apart it has none
	`(() => { const up = 'abc'.toUpperCase; return up() })()`,
	`'abc'['toUpperCase']()`,
	`(() => { const l = [1, 2]; return (l.at)(-1) })()`,
	`(() => { const a = [1]; a?.push?.(2); return a.join() })()`,
	`(() => { const s = 'ab'; return [s.at?.(-1), s['at'](0), [1].map === [2].map, typeof s.at] })()`,
	`['a', 'b'].map((x) => x.toUpperCase())`,
	// a /* … */ comment, as JavaScript reads it (19/11/2026): it ends at its first */, and one holding a line break
	// is one
	`1 /* a */ + 2`,
	`/**/ 7`,
	`4 /* a */ /* b */ * 2`,
	`'/* not a comment */'`,
	`6 /*/ 2 */`,
	`(() => { return /* c
 */ 1 })()`,
	`(() => { const a = 1 /* c
 */ const b = 2; return a + b })()`,
	`(() => { const a = 1 /* c */ const b = 2; return a + b })()`,
	`1 /* a /* b */ c */`,
	// Object.groupBy (19/11/2026): keys made as JavaScript makes them, in an object's order, items walked as for…of walks
	`Object.groupBy([1, 2, 3, 4], (x) => x % 2 ? 'odd' : 'even')`,
	`Object.groupBy('aAbB', (c, i) => i % 2)`,
	`Object.groupBy('a😀', (c) => c.length)`,
	`Object.groupBy([3, 1, 2, 'x'], (x) => x)`,
	`Object.groupBy([], (x) => x)`,
	`Object.groupBy([1.5, 2, -0, NaN], (x) => x)`,
	`Object.groupBy([{ t: 'a', n: 1 }, { t: 'b', n: 2 }, { t: 'a', n: 3 }], (o) => o.t)`,
	`Object.groupBy(['x', 'y'], (x) => x === 'x' ? null : undefined)`,
	`Object.groupBy(['x'], (x) => [1, [2, 3]])`,
	`Object.groupBy(['x'], (x) => ({ toString: () => 'k' }))`,
	`Object.groupBy(['x'], (x) => '__proto__')`,
	`Object.groupBy([1, 2], (x, i) => i)`,
	`Object.keys(Object.groupBy(['b', 'a', 'b'], (x) => x))`,
	`JSON.stringify(Object.groupBy([1, 2, 3], (x) => x > 1))`,
	`(() => { const a = [1, 2]; return Object.groupBy(a, (x) => { if (a.length < 4) a.push(x + 10); return 'k' }) })()`,
	`(() => { const g = Object.groupBy([1], (x) => 'a'); g.a.push(2); g.b = 1; return g })()`,
	`Object.groupBy({ a: 1 }, (x) => x)`,
	`Object.groupBy(null, (x) => x)`,
	`Object.groupBy([1], 5)`,
	`Object.groupBy([1], (x) => ({ toString: () => ({}), valueOf: () => ({}) }))`,
	// Array(n) (19/11/2026): JavaScript's n holes, which read as undefined; what passes over a hole is refused
	`Array(3).fill(0)`,
	`new Array(2).fill('x')`,
	`Array(3).fill(1).map((x) => x * 2)`,
	`[...Array(3)].map((_, i) => i)`,
	`Array.from(Array(2), (_, i) => i)`,
	`[Array(3).length, Array(3).join('-'), JSON.stringify(Array(2)), Array(2)[0], Array(3).includes(undefined)]`,
	`[Array(3).find((x) => true), Array(3).findIndex((x) => x === undefined), Array(0).map((x) => 1), Array('a'), Array(2, 3)]`,
	`(() => { const a = Array(2); a[0] = 1; return [a.at(0), a.at(1), a.length, String(a)] })()`,
	`[Array(3).toReversed(), Array(2).with(0, 1), Array(2).toSorted(), Array(2).toSpliced(0, 1)]`,
	`(() => { const a = Array(3).fill(0, 1); return [a.length, a[0], a[1]] })()`,
	`Array(-1)`,
	`Array(1.5)`,
	// a text built by + and templates (19/11/2026, value/join.go): written in place after the text a run builds, and
	// every text made on the way, and a second join of one of them, keeps what JavaScript gives
	`(() => { let s = 'x'.repeat(70); const seen = []; for (let i = 0; i < 8; i++) { s = s + i; seen.push(s) } const a = s + 'A'; const b = s + 'B'; return [seen.map((t) => t.length), seen.map((t) => t.slice(-3)), a.slice(-3), b.slice(-3), s.length] })()`,
	`(() => { let s = 'y'.repeat(80); s = s + '1'; s = s + '2'; const keep = s; s = s + '3'; const other = keep + '4'; s = s + '5'; return [keep.slice(-3), s.slice(-4), other.slice(-4), keep.length, other.length] })()`,
	`(() => { const parts = ['', '', '', '', '', '']; for (let i = 0; i < 40; i++) { for (let k = 0; k < 6; k++) { parts[k] = parts[k] + k + i } } return parts.map((p) => [p.length, p.slice(0, 6), p.slice(-6)]) })()`,
	`(() => { let s = ''; for (let i = 0; i < 300; i++) { s += '<li>' + i + '</li>' } return [s.length, s.slice(0, 20), s.slice(-20)] })()`,
	"(() => { let s = ''; for (let i = 0; i < 60; i++) { s = `${s}<${i}>` } const t = `${s}!`; return [s.length, s.slice(0, 9), s.slice(-9), t.slice(-3)] })()",
	`(() => { let s = 'a'.repeat(70) + '@uD83D@uDE00'.slice(0, 1); s = s + 'b'.repeat(10); const t = 'c'.repeat(70) + '@uD83D@uDE00'.slice(0, 1); const u = t + '@uD83D@uDE00'.slice(1); return [s.length, u.length, u.codePointAt(70), u.slice(-2) === '@uD83D@uDE00'] })()`,
	`(() => { let s = 'z'.repeat(64); const list = []; for (let i = 0; i < 5; i++) { list.push(s + i); s = s + '-' } return [list.map((t) => t.slice(-2)), s.slice(-6), list[0].length] })()`,
}

// parityText is a case's source: @ made a backslash.
func parityText(source string) string {
	return strings.ReplaceAll(source, "@", string(rune(92)))
}

// nodeDescribe is describe (corpus_test.go) in JavaScript, as generate.js writes it.
const nodeDescribe = `
function __describe(v, depth) {
  depth = depth || 0;
  if (depth > 12) return "<deep>";
  if (v === undefined) return "undefined";
  if (v === null) return "null";
  switch (typeof v) {
    case "boolean": return String(v);
    case "number": return v !== v ? "NaN" : v === 0 && 1 / v < 0 ? "-0" : String(v);
    case "string": return JSON.stringify(v);
  }
  if (typeof v === "function") return "fn:";
  if (Array.isArray(v)) return "[" + v.map(function (item) { return __describe(item, depth + 1); }).join(",") + "]";
  return "{" + Object.keys(v).map(function (k) { return JSON.stringify(k) + ":" + __describe(v[k], depth + 1); }).join(",") + "}";
}
`

func TestParityWithNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	sources := make([]string, len(parityCases))
	for i, source := range parityCases {
		sources[i] = parityText(source)
	}
	script := nodeDescribe + `
const sources = JSON.parse(require("fs").readFileSync(0, "utf8"));
const out = sources.map((src) => {
  try {
    const run = new Function('"use strict"; const result = (' + src.trim() + '); return result;');
    return __describe(run());
  } catch (e) {
    return "throws";
  }
});
process.stdout.write(JSON.stringify(out));`
	encoded, _ := json.Marshal(sources)
	// the sources go through standard input: a command line has a length limit, the input none
	command := exec.Command("node", "-e", script)
	command.Stdin = bytes.NewReader(encoded)
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected []string
	if err := json.Unmarshal(raw, &expected); err != nil || len(expected) != len(sources) {
		t.Fatalf("node output: %v\n%s", err, raw)
	}
	failed := 0
	for i, source := range sources {
		got := runParity(source)
		if got.got != expected[i] {
			failed++
			t.Errorf("%s\n\tnode:    %s\n\tkitwork: %s %s", parityCases[i], expected[i], got.got, got.detail)
		}
	}
	t.Logf("%d of %d cases agree with node", len(sources)-failed, len(sources))
}

// runParity compiles and runs one case as the corpus does, with no scope of its own.
func runParity(source string) outcome {
	program, fault := compileCase(source)
	if fault != "" {
		return outcome{got: "panic", detail: fault}
	}
	if program.err != nil {
		return outcome{got: "throws", detail: program.err.Error()}
	}
	return runCase(program.bytecode, ordered{kind: 'o'}, ordered{kind: 'o'})
}

// TestKitworkCannotBeShadowed holds a departure from JavaScript, which lets a program name a variable
// kitwork: the compiler reads the name as Kitwork itself wherever it is written, so declaring or
// assigning it is a compile error rather than a variable no read reaches.
func TestKitworkCannotBeShadowed(t *testing.T) {
	refused := []string{
		`const kitwork = 5;`,
		`let kitwork;`,
		`let x = 1; kitwork = 5;`,
		`const f = (kitwork) => 1;`,
		`function kitwork() { return 1 }`,
		`const { kitwork } = { kitwork: 1 };`,
		`const [kitwork] = [1];`,
		`for (const kitwork of [1]) { }`,
		`for (let kitwork = 0; kitwork < 1; kitwork++) { }`,
		`kitwork++;`,
		`kitwork += 1;`,
		`let a; ({ kitwork: a, kitwork } = { kitwork: 3 });`,
		`const f = () => { const kitwork = 1; return kitwork };`,
	}
	for _, source := range refused {
		if _, err := compiler.CompileSource(source); err == nil || !strings.Contains(err.Error(), "kitwork") {
			t.Errorf("%s: want the compile error that names kitwork, got %v", source, err)
		}
	}
	allowed := []string{
		`const x = kitwork;`,
		`const t = typeof kitwork;`,
		`const o = { kitwork: 1 }; const k = o.kitwork;`,
		`let a; ({ kitwork: a } = { kitwork: 3 });`,
		`const o = { kitwork };`,
	}
	for _, source := range allowed {
		if _, err := compiler.CompileSource(source); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}

// TestBigIntRefused holds the other departure: Kitwork has no BigInt (its numbers are number), so 1n is a
// compile error where JavaScript gives a bigint.
func TestBigIntRefused(t *testing.T) {
	for _, source := range []string{`const x = 1n;`, `const x = 0x1fn;`, `const x = 10n + 1;`} {
		if _, err := compiler.CompileSource(source); err == nil || !strings.Contains(err.Error(), "BigInt") {
			t.Errorf("%s: want the compile error that names BigInt, got %v", source, err)
		}
	}
}

// TestCompareModes holds the verifier to the comparisons COMPARE runs: the loose two (6 and 7) are
// accepted beside the first six, a mode past them is refused before it runs.
func TestCompareModes(t *testing.T) {
	constants := []value.Value{value.Null(), value.Undefined()}
	program := func(mode byte) []byte {
		return []byte{byte(runtime.PUSH), 0, 0, byte(runtime.PUSH), 0, 1, byte(runtime.COMPARE), mode, byte(runtime.RETURN)}
	}
	for mode := byte(0); mode <= runtime.CompareLooseNotEqual; mode++ {
		if err := runtime.Verify(program(mode), constants); err != nil {
			t.Errorf("COMPARE %d: %v", mode, err)
		}
	}
	if err := runtime.Verify(program(runtime.CompareLooseNotEqual+1), constants); err == nil {
		t.Errorf("COMPARE %d: verified, want INVALID_OPERAND", runtime.CompareLooseNotEqual+1)
	}
	for mode, want := range map[byte]bool{
		runtime.CompareStrictEqual: false, runtime.CompareStrictNotEqual: true,
		runtime.CompareLooseEqual: true, runtime.CompareLooseNotEqual: false,
	} {
		bytecode, err := runtime.NewProgram(program(mode), constants, nil)
		if err != nil {
			t.Fatalf("COMPARE %d: %v", mode, err)
		}
		if got := runtime.New(bytecode).Run(); !got.Equal(value.Boolean(want)) {
			t.Errorf("null COMPARE %d undefined: %v, want %v", mode, got.ToString().Text(), want)
		}
	}
}
