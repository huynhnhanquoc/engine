package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// The meaning of code in a file, held to JavaScript's or refused at compile time (docs/language.md mục 8.1). Each
// case is a file's code, compiled as a file is (runFileResult, statement_end_test.go); parity_test.go holds the same
// against node as snippets.

// A named function expression's name is the function itself inside its body, and the function's name (18/11/2026):
// the name was skipped, so in a file the body's call by it was a compile error (an undeclared name) and f.name was
// the name of the const that held it.
//
// CONTROL (18/11/2026): with the name skipped, the first case failed to compile ("g chưa được khai") and the second
// gave undefined,f; in the parity with node, assigning the name inside the body compiled.
func TestANamedFunctionExpressionIsItselfByItsName(t *testing.T) {
	for source, want := range map[string]string{
		"const f = function g(n) { return n == 0 ? 0 : g(n - 1) + 1 }\nconst result = f(3)": "3",
		"const f = function g() { return typeof g }\nconst result = [f(), f.name]":          "function,g",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
	if _, err := compileFile(t, "const f = function g() { g = 1 }"); err == nil || !strings.Contains(err.Error(), "g") {
		t.Errorf("an assignment to a named function expression's name: %v, want a compile error", err)
	}
}

// A parameter's default reads the parameters before its own, in the parameters' scope, as JavaScript has it
// (18/11/2026): the defaults were the body's first statements, so (a = b, b = 1) => a read undefined, (a = a) => a
// too, and a function in a default read the body's variables (x was 5 where it is the 1 around).
//
// CONTROL (18/11/2026): with the defaults resolved as the body's statements, the refused cases compiled (the call
// of the body's function too) and x read 5.
func TestADefaultReadsOnlyTheParametersBeforeIt(t *testing.T) {
	for source, want := range map[string]string{
		"const f = (a = b, b = 1) => a":              "router.work:1:16: mặc định của tham số đọc b, một tham số chưa có lúc ấy",
		"const f = (a = a) => a":                     "router.work:1:16: mặc định của tham số đọc a",
		"const f = ({ a = c }, c) => a":              "router.work:1:18: mặc định của tham số đọc c",
		"const b = 9\nfunction f(a = b, b) {}":       "router.work:2:16: mặc định của tham số đọc b",
		"const f = (a = g()) => { function g() {} }": "router.work:1:16: g chưa được khai",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const x = 1\nconst f = (a = () => x) => { const x = 5; return a() }\nconst result = f()": "1",
		"const f = (a = () => b, b = 2) => a()\nconst result = f()":                               "2",
		"const f = (a, b = a, { c = b } = {}) => [a, b, c]\nconst result = f(1)":                  "1,1,1",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
}

// A function's length counts its parameters before the first with a default and before the rest, as JavaScript's
// does (18/11/2026: it counted every parameter but the rest), and a program keeps it through its binary envelope.
//
// CONTROL (18/11/2026): with the length counting every parameter but the rest, the lengths were 2,2,3.
func TestAFunctionsLengthStopsAtItsFirstDefault(t *testing.T) {
	source := "const f = (a, b = 1) => 0\nfunction g(a, b = 1, ...c) { return 0 }\nconst h = ({ a } = {}, [b] = [], c) => 0\nconst result = [f.length, g.length, h.length]"
	got, err := runFileResult(t, source)
	if err != nil || shownResult(got) != "1,1,0" {
		t.Fatalf("the lengths: %v %s, want 1,1,0", err, shownResult(got))
	}
	bytecode, err := compileFile(t, source)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bytecode.Program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := runtime.UnmarshalProgram(encoded)
	if err != nil {
		t.Fatalf("the envelope of a program with defaults: %v", err)
	}
	if decoded.Checksum() != bytecode.Program.Checksum() {
		t.Errorf("the decoded program's checksum differs")
	}
	defaulted := 0
	for _, constant := range decoded.Constants() {
		if lambda := constant.Lambda(); lambda != nil {
			defaulted += lambda.Defaulted
		}
	}
	if defaulted != 1+1+3 {
		t.Errorf("the decoded functions hold %d defaulted parameters, want 5", defaulted)
	}
}

// An array pattern reads what the value's iteration gives, as JavaScript's does (18/11/2026, ITEMS, bytecode 10): it
// read the value by index, so a string gave half of a character outside the basic plane, 5 gave undefined and an
// object with a length its key "0". What is not iterable is the TYPE failure, said at the pattern.
//
// CONTROL (18/11/2026): with the pattern reading by index, the string gave its first half, 5 gave undefined and
// the object "z".
func TestAnArrayPatternReadsWhatItsIterationGives(t *testing.T) {
	for source, want := range map[string]string{
		"const [a, ...r] = \"😀xy\"\nconst result = [a, r.length]":            "😀,2",
		"const f = ([x, y]) => x + y\nconst result = [f([1, 2]), f(\"ab\")]": "3,ab",
		"const [a] = 5\nconst result = a":                                    "failure router.work:1:7: mẫu mảng [ … ] tách theo phép lặp: number không lặp được",
		"const [a] = { 0: \"z\", length: 1 }\nconst result = a":              "failure router.work:1:7: mẫu mảng [ … ] tách theo phép lặp: object không lặp được",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !strings.HasPrefix(shownResult(got), want) {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
}

// A let stands where a file's code has one (18/11/2026, docs/language.md mục 2.1, decided 27/10/2026): at a file's top
// level, an imported file's too, a name that changes lives through every request, so it is const there, as var is
// refused there; at the top level of a router.view(…) or component.define(…) function the screen's state is var. A
// let in a function, a block or a loop stays.
//
// CONTROL (18/11/2026): with letPlace giving nil, every refused case compiled.
func TestALetStandsWhereAFileHasOne(t *testing.T) {
	router := "import * as router from \"kitwork/router\"\n"
	for source, want := range map[string]string{
		"let x = 1\nconst result = x": "router.work:1:1: let x ở cấp cao nhất của file: tên đổi được ở cấp file sống qua mọi request",
		"let { a } = { a: 1 }":        "router.work:1:1: let a ở cấp cao nhất của file",
		router + "router.view((context) => { let a = 1; return context.bind({ a }) })": "router.work:2:28: let a ở cấp cao nhất của hàm router.view(…) hay component.define(…): trạng thái của màn hình viết var",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		"const f = () => { let y = 1; y = y + 1; return y }\nconst result = f()",
		"if (true) { let z = 1 }\nfor (let i = 0; i < 2; i++) { let w = i }",
		router + "router.view((context) => { var q = \"\"; const f = () => { let n = 0; return n }; return context.bind({ q, f }) })",
	} {
		if _, err := compileFile(t, source); err != nil {
			t.Errorf("%q: %v", source, err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.work"), []byte("let count = 0\nexport const read = () => count\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "router.work")
	if err := os.WriteFile(entry, []byte("import { read } from \"./lib.work\"\nconst result = read()"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.CompileFile(entry); err == nil || !strings.Contains(err.Error(), "lib.work:1:1: let count ở cấp cao nhất của file") {
		t.Errorf("an imported file's top-level let: %v, want its compile error", err)
	}
}

// error(…) standing alone in a function, not returned, is a compile error (18/11/2026, docs/language.md mục 5.1,
// decided 27/10/2026): it stops the call as return error(…) does, which the line does not show. A file's top level has
// no return, so it stands alone there and stops the boot; a program's own error is no builtin.
//
// CONTROL (18/11/2026): with errorAlone giving nil, both refused cases compiled.
func TestErrorStandsReturnedInAFunction(t *testing.T) {
	for source, want := range map[string]string{
		"const f = () => { error(\"x\") }":                      "router.work:1:19: error(…) đứng riêng trong một hàm: viết return error(…)",
		"const f = (n) => { if (n) { error(\"x\") } return 1 }": "router.work:1:29: error(…) đứng riêng trong một hàm",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		"const f = (n) => { if (n) { return error(\"x\") } return 1 }\nconst g = () => error(\"y\")",
		"const error = (m) => m\nconst f = () => { error(\"x\"); return 1 }",
		"const ok = true\nif (!ok) { error(\"boot\") }",
	} {
		if _, err := compileFile(t, source); err != nil {
			t.Errorf("%q: %v", source, err)
		}
	}
}

// delete o.k and delete o[k] are strict JavaScript's (18/11/2026, DELETE, bytecode 10, decided 27/10/2026; it was the
// compile error "Kitwork chưa có delete"): true once the key is gone; a frozen object's own key, a string's index, an
// array's length, null are the TYPE failure strict mode throws; an array's item is UNSUPPORTED, as a Kitwork array has
// no hole; delete of a name, of what is no key, or through ?. is a compile error.
//
// CONTROL (18/11/2026): with delete the old compile error, every case but const delete was "Kitwork chưa có delete".
func TestDeleteRemovesAKeyAsStrictJavaScript(t *testing.T) {
	for source, want := range map[string]string{
		"const f = () => { const o = { a: 1, b: 2 }; return [delete o.a, delete o[\"x\"], Object.keys(o).join()] }\nconst result = f()": "true,true,b",
		"const o = Object.freeze({ a: 1 })\nconst result = delete o.a":                                                                  "failure router.work:2:16: cannot delete property 'a' of #<Object>",
		"const a = [1, 2]\nconst result = delete a[0]":                                                                                  "failure router.work:2:16: delete a[0]: mảng của Kitwork không có chỗ trống",
		"const result = delete \"abc\"[0]":                                                                                              "failure router.work:1:16: cannot delete property '0' of string",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !strings.HasPrefix(shownResult(got), want) {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
	for source, want := range map[string]string{
		"const x = 1\ndelete x":               "router.work:2:1: delete x: strict JavaScript không xoá một tên",
		"const o = {}\nconst r = delete o?.a": "router.work:2:11: Kitwork chưa có delete qua ?.",
		"const r = delete 5":                  "router.work:1:11: delete xoá một khoá của một object",
		"const delete = 1":                    "router.work:1:7: delete là từ khoá của ngôn ngữ",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
}

// A let or a const is not used before it is made, as JavaScript's temporal dead zone has it (18/11/2026, decided
// 27/10/2026). Where the compiler sees the use it is a compile error: a read in its own declaration (let x = x), a read
// in a later case of a switch, directly or by a function made there. Where a function made before the line may run
// before it, the scope marks the variable as it begins (UNSET_SLOT, UNSET_NAME), and the read or the write is the
// failure NOT_INITIALIZED, which safe catches; all read undefined.
//
// CONTROL (18/11/2026): with no hole marked (assignHoles listing none), safe caught nothing in the runtime cases (its
// error was null) and the read not caught gave undefined; with the declaration's names not marked initializing,
// let x = x and const x = x + 1 compiled; with the cases' places not kept, the reads in a later case compiled.
func TestALetOrConstIsNotUsedBeforeItIsMade(t *testing.T) {
	for source, want := range map[string]string{
		"const f = () => { let x = x; return x }":                                                                  "router.work:1:27: x được đọc trong chính dòng khai nó",
		"const f = () => { const x = x + 1; return x }":                                                            "router.work:1:29: x được đọc trong chính dòng khai nó",
		"const h = (n) => { switch (n) { case 1: let y = 1; return y; case 2: return y } }":                        "router.work:1:77: y khai trong một case trước (router.work:1:45)",
		"const h = (n) => { switch (n) { case 1: const y = 1; return y; case 2: const f = () => y; return f() } }": "router.work:1:88: y khai trong một case trước",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const f = () => x\nconst r = safe(f).error?.code\nconst x = 1\nconst result = r":                                            "NOT_INITIALIZED",
		"const g = () => { const read = () => y; const r = safe(read).error?.code; const y = 2; return r }\nconst result = g()":      "NOT_INITIALIZED",
		"const g = () => { const w = () => { y = 5 }; const r = safe(w).error?.code; let y = 2; return [r, y] }\nconst result = g()": "NOT_INITIALIZED,2",
		"const g = () => { function read() { return y } const y = 3; return read() }\nconst result = g()":                            "3",
		"const g = () => { const read = () => y; const y = 2; return read() }\nconst result = g()":                                   "2",
		"const f = () => { const { a, b = a } = { a: 4 }; return b }\nconst result = f()":                                            "4",
		"const f = () => { const g = () => g; return g() === g }\nconst result = f()":                                                "true",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
	got, _ := runFileResult(t, "const f = () => x\nconst r = f()\nconst x = 1")
	if want := "failure router.work:1:17: x chưa có: một hàm dùng nó trước khi dòng khai nó chạy"; !strings.HasPrefix(shownResult(got), want) {
		t.Errorf("a read before its line, not caught: %s, want %s", shownResult(got), want)
	}
	if _, err := compileFile(t, "const out = []\nfor (let i = 0; i < 2; i++) { const f = () => y; out.push(f); const y = i }"); err == nil ||
		!strings.Contains(err.Error(), "router.work:2:69: y của một lượt vòng lặp được một hàm tạo trước dòng khai nó dùng") {
		t.Errorf("an iteration's variable a function made before its line uses: %v, want the compile error", err)
	}
}

// Date(…) called without new, the builtin's, is a compile error (18/11/2026, docs/language.md mục 8.2): JavaScript's
// Date() leaves its arguments and gives the text of the time now, which code almost never means; Kitwork made the time
// of its arguments, as new Date(…) does. new Date(…) and new Date stay, and a Date a program declares is its own.
//
// CONTROL (18/11/2026): with dateWithoutNew giving nil, both refused cases compiled.
func TestDateWithoutNewIsAnError(t *testing.T) {
	for _, source := range []string{"const t = Date(0)", "const f = () => Date()"} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), "Date(…) không new: JavaScript bỏ các đối số") {
			t.Errorf("%q: %v, want the compile error", source, err)
		}
	}
	for source, want := range map[string]string{
		"const result = [new Date(0).getTime(), typeof new Date, new Date(Date.UTC(2026, 0, 2)).toISOString()]": "0,object,2026-01-02T00:00:00.000Z",
		"const Date = (x) => x + 1\nconst result = Date(1)":                                                     "2",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
}

// Two times are not compared with ===, !==, == or != (18/11/2026, docs/language.md mục 8.2): JavaScript asks whether two
// Date objects are one object, which a Kitwork time, a value, cannot say, so the comparison is the TYPE error that says
// to compare their instants; it compared them, and new Date(0) === new Date(0) was true where JavaScript gives false.
// An order, a comparison with null and getTime() stay.
//
// CONTROL (18/11/2026): with the check out of compareRead, the four comparisons gave true, true, true and "same".
func TestTwoTimesAreNotComparedForEquality(t *testing.T) {
	for _, source := range []string{
		"const result = new Date(0) === new Date(0)",
		"const a = new Date(0)\nconst result = a !== new Date(1)",
		"const a = new Date(0)\nconst result = a == new Date(0)",
		"const f = (t) => { switch (t) { case new Date(0): return \"same\" } return \"other\" }\nconst result = f(new Date(0))",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !strings.Contains(shownResult(got), "hai thời gian không so bằng") {
			t.Errorf("%q: %v %s, want the TYPE error", source, err, shownResult(got))
		}
	}
	got, err := runFileResult(t, "const a = new Date(0)\nconst b = new Date(5)\nconst result = [a < b, a == null, a.getTime() === new Date(0).getTime(), b > a]")
	if err != nil || shownResult(got) != "true,false,true,true" {
		t.Errorf("an order, null and getTime: %v %s", err, shownResult(got))
	}
}

// constructor and __proto__ written by name are a compile error (18/11/2026, docs/language.md mục 8.2): JavaScript reads
// them off the prototype chain and __proto__: p sets it, which Kitwork has not, so o.constructor was undefined where
// JavaScript gives Object, and __proto__: p a plain key. A shorthand { __proto__ } and a computed key are plain keys in
// JavaScript too, and a key read through a name (o[k]) reads the object's own.
//
// CONTROL (18/11/2026): with prototypeKey giving nil, every refused case compiled.
func TestConstructorAndProtoByNameAreErrors(t *testing.T) {
	for source, want := range map[string]string{
		"const o = {}\nconst c = o.constructor":      "router.work:2:13: Kitwork không có prototype nên không có constructor",
		"const o = {}\nconst c = o[\"constructor\"]": "router.work:2:13: Kitwork không có prototype nên không có constructor",
		"const o = {}\no.__proto__ = {}":             "router.work:2:3: Kitwork không có prototype nên __proto__",
		"const o = { __proto__: { a: 1 } }":          "router.work:1:13: Kitwork không có prototype nên __proto__",
		"const o = { \"__proto__\": 1 }":             "router.work:1:13: Kitwork không có prototype nên __proto__",
		"const { constructor } = {}":                 "router.work:1:9: Kitwork không có prototype nên không có constructor",
		"const f = ({ a: { __proto__: p } }) => p":   "Kitwork không có prototype nên __proto__",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	got, err := runFileResult(t, "const __proto__ = 1\nconst k = \"constructor\"\nconst o = { __proto__, [\"x\"]: 2, constructor: 3 }\nconst result = [Object.keys(o).join(), o[k]]")
	if err != nil || shownResult(got) != "__proto__,x,constructor,3" {
		t.Errorf("plain keys: %v %s", err, shownResult(got))
	}
}

// A function has no text (18/11/2026, docs/language.md mục 8.2): JavaScript writes its source, which Kitwork does not
// keep, so String(f), a template, f + "", f.toString(), [f].join() and a text a builtin reads of an argument are the
// TYPE error; they were "function", a text other than JavaScript's ('my function'.includes(f) was true where
// JavaScript gives false). A replacer of replace and replaceAll is called back, as before; f == null, f === f and
// typeof f stay.
//
// CONTROL (18/11/2026): with a function's text "function" again, every refused case gave a text or a boolean.
func TestAFunctionHasNoText(t *testing.T) {
	for _, expression := range []string{"String(f)", "`${f}`", "f + \"\"", "f.toString()", "[f].join()",
		"\"my function\".includes(f)", "parseInt(f)", "f == \"x\""} {
		source := "const f = () => 1\nconst result = safe(() => " + expression + ").error?.message"
		got, err := runFileResult(t, source)
		if err != nil || !strings.Contains(shownResult(got), "một hàm không đổi thành chữ") {
			t.Errorf("%s: %v %s, want the TYPE error", expression, err, shownResult(got))
		}
	}
	got, err := runFileResult(t, "const f = () => 1\nconst result = [f == null, f === f, typeof f, \"abc\".replace(\"b\", (m) => m.toUpperCase()), \"abc\".replaceAll(\"b\", () => \"x\")]")
	if err != nil || shownResult(got) != "false,true,function,aBc,axc" {
		t.Errorf("what stays: %v %s", err, shownResult(got))
	}
}

// A time has no text (18/11/2026, docs/language.md mục 8.2): JavaScript writes a Date in the zone of the machine that
// runs it, so String(t), a template, t + "", t.toString(), [t].join(), a key o[t], a text a builtin reads of an
// argument and t == "x" are the TYPE error; they were RFC 3339 in UTC, a text other than JavaScript's. What reads a
// time as its number (t < u, +t, "abc".at(t), Math.max) stays, as do its own words and JSON.stringify.
//
// CONTROL (18/11/2026): with a time's text RFC 3339 again, every refused case gave a text or a boolean; with the time
// out of ConvertsWithCode alone, "abc".includes(t) was true, parseInt(t) NaN, t == "x" and t == 0 false.
func TestATimeHasNoText(t *testing.T) {
	for _, expression := range []string{"String(t)", "`${t}`", "t + \"\"", "\"at \" + t", "t + 1", "t.toString()", "[t].join()",
		"({})[t]", "\"abc\".includes(t)", "parseInt(t)", "t == \"x\"", "t == 0", "[t, u].sort()"} {
		source := "const t = new Date(0)\nconst u = new Date(1000)\nconst result = safe(() => " + expression + ").error?.message"
		got, err := runFileResult(t, source)
		if err != nil || !strings.Contains(shownResult(got), "một thời gian không đổi thành chữ") {
			t.Errorf("%s: %v %s, want the TYPE error", expression, err, shownResult(got))
		}
	}
	got, err := runFileResult(t, "const t = new Date(0)\nconst u = new Date(1000)\nconst result = [t < u, +u, u.getTime(), JSON.stringify(t), "+
		"t.toISOString(), t == null, \"abc\".at(t), Math.max(u, 1), typeof t, t.format(\"2006\")]")
	if want := "true,1000,1000,\"1970-01-01T00:00:00.000Z\",1970-01-01T00:00:00.000Z,false,a,1000,object,1970"; err != nil || shownResult(got) != want {
		t.Errorf("what stays: %v %s, want %s", err, shownResult(got), want)
	}
}
