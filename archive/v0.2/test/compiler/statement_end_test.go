package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// Where a statement ends (docs/language.md mục 3.4, mục 8). Each case is a file's code, compiled as a file is.

// runFileResult compiles source as a file and runs it with the builtins; the result is the compile error, the run's
// failure, or the top-level const result.
func runFileResult(t *testing.T, source string) (value.Value, error) {
	t.Helper()
	bytecode, err := compileFile(t, source)
	if err != nil {
		return value.Value{}, err
	}
	globals := map[string]value.Value{}
	builtins.Install(globals)
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 1_000_000
	vm.FastReset(bytecode.Program, globals)
	if result := vm.Run(); result.IsInvalid() {
		return result, nil
	}
	return vm.Vars["result"], nil
}

// A return with no value ends at a line break, as JavaScript inserts the semicolon there (18/11/2026): return⏎5
// returned 5. And it leaves the } after it to close its block: return } consumed the brace, so the rest of the file
// silently became the function's body, and the line after it never ran.
//
// CONTROL (18/11/2026): with the line break not ending the return, return⏎5 gave "number" for "undefined" and the
// switch's case 1: return⏎default: failed at default; with the return taking the } after it, { return } and
// if (x) { return } left their function's block open, which the check of the next test then said.
func TestReturnEndsAtALineBreakAndLeavesTheBrace(t *testing.T) {
	for source, want := range map[string]string{
		"const f = () => {\n  return\n    5\n}\nconst result = typeof f()":                                                          "undefined",
		"const g = () => { return }\nconst result = \"after\"":                                                                      "after",
		"const h = (x) => { if (x) { return } return 2 }\nconst result = String(h(true)) + h(false)":                                "undefined2",
		"const s = (n) => {\n  switch (n) {\n    case 1: return\n    default: return 3\n  }\n}\nconst result = String(s(1)) + s(2)": "undefined3",
		"const m = () => { return; }\nconst result = typeof m() + 7":                                                                "undefined7",
		"const r = (a) => { return a +\n  1 }\nconst result = String(r(1))":                                                         "2",
		"const q = () => {\n  return (\n    5\n  )\n}\nconst result = String(q())":                                                  "5",
	} {
		got, err := runFileResult(t, source)
		if err != nil {
			t.Errorf("%q: %v", source, err)
			continue
		}
		if got.IsInvalid() || !got.IsString() || got.Text() != want {
			t.Errorf("%q: %s, want %q", source, shownResult(got), want)
		}
	}
}

// A block the file ends inside is a compile error at its {, as JavaScript's SyntaxError (18/11/2026): it was
// accepted, the rest of the file read as the block's.
//
// CONTROL (18/11/2026): with the end of the file not checked, every case compiled.
func TestABlockTheFileEndsInsideIsACompileError(t *testing.T) {
	for source, want := range map[string]string{
		"const k = () => { const a = 1\nconst result = 1":   "router.work:1:17: khối mở bằng { ở đây chưa đóng",
		"if (true) { const a = 1":                           "router.work:1:11: khối mở bằng { ở đây chưa đóng",
		"for (const x of [1]) {\n  const y = x":             "router.work:1:22: khối mở bằng { ở đây chưa đóng",
		"function f() {\n  return 1":                        "router.work:1:14: khối mở bằng { ở đây chưa đóng",
		"const f = () => {\n  if (true) {\n    return 1\n}": "router.work:1:17: khối mở bằng { ở đây chưa đóng",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
}

func shownResult(v value.Value) string {
	if v.IsInvalid() {
		return "failure " + v.Headline() // its place, then its message
	}
	if v.IsUndefined() {
		return "undefined"
	}
	return v.ToString().Text()
}

// Two statements on one line are held apart by ;, as JavaScript inserts a ; only before a line break, a } or the end
// (18/11/2026): const result = 1 2 read result = 1, then 2. A statement that ends with a block of its own needs
// nothing after it (if (c) {} f(), function f() {} f()), and an if's body that is no block takes its ; before else,
// as JavaScript does: if (c) x = 1; else x = 2 (the ; was an error, and if (c) x = 1 else x = 2 was read). break
// ends at a line break, as continue does (break⏎r = x was the labeled-break error).
//
// CONTROL (18/11/2026): with the end of a statement not checked, every refused case compiled; with the ; before else not taken, if (true) x = 1; else x = 2 and the else if chain failed at
// else; with a line break not ending break, the loop failed with the labeled-break error.
func TestTwoStatementsOnALineAreHeldApartBySemicolons(t *testing.T) {
	for source, want := range map[string]string{
		"const result = 1 2":                                    `router.work:1:18: cần ; hay xuống dòng trước "2"`,
		"const a = 1 const b = 2":                               `router.work:1:13: cần ; hay xuống dòng trước "const"`,
		"let x = 0\nif (true) x = 1 else x = 2":                 `router.work:2:17: cần ; hay xuống dòng trước "else"`,
		"const g = () => {} g()":                                `router.work:1:20: cần ; hay xuống dòng trước "g"`,
		"let r = 0\nswitch (1) { case 1: r = 1 case 2: r = 2 }": `router.work:2:28: cần ; hay xuống dòng trước "case"`,
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const a = 1; const result = String(a + 1)":                                                                                                 "2",
		"const run = () => {\nlet x = 0\nif (true) x = 1; else x = 2\nreturn String(x)\n}\nconst result = run()":                                    "1",
		"const run = () => {\nlet x = 0\nif (false) x = 1\nelse x = 2\nreturn String(x)\n}\nconst result = run()":                                   "2",
		"const run = () => {\nlet x = 0\nif (false) x = 1; else if (true) x = 5; else x = 9\nreturn String(x)\n}\nconst result = run()":             "5",
		"function f() { return 3 } const result = String(f())":                                                                                      "3",
		"const run = () => {\nlet n = 0\nif (true) { n = 4 } return String(n)\n}\nconst result = run()":                                             "4",
		"const run = () => {\nlet r = 0\nfor (const x of [1, 2, 3]) {\n  if (x === 2) break\n  r = x\n}\nreturn String(r)\n}\nconst result = run()": "1",
		"const run = () => {\nlet r = 0\nswitch (1) {\n  case 1: r = 1; break\n  case 2: r = 2\n}\nreturn String(r)\n}\nconst result = run()":       "1",
	} {
		got, err := runFileResult(t, source)
		if err != nil {
			t.Errorf("%q: %v", source, err)
			continue
		}
		if got.IsInvalid() || !got.IsString() || got.Text() != want {
			t.Errorf("%q: %s, want %q", source, shownResult(got), want)
		}
	}
}

// A template right after an expression, on its line or the next, is a tagged template in JavaScript, which calls the
// expression; Kitwork has none, so it is a compile error at the template (18/11/2026): f`x` read f, and the template
// as a statement of its own, the tag never called. A template as an argument, in an array, or alone after a ; is a
// template.
//
// CONTROL (18/11/2026): with the tag not refused, f`a` and o.t`a${1}b` failed only as two statements on one line,
// and f⏎`a` compiled, the tag dropped.
func TestATaggedTemplateIsACompileError(t *testing.T) {
	refused := "Kitwork chưa có chuỗi mẫu có thẻ (tag`…`)"
	for source, at := range map[string]string{
		"const f = (x) => x\nconst result = f`a`":               "router.work:2:17",
		"const f = (x) => x\nconst result = f\n`a`":             "router.work:3:1",
		"const o = { t: (x) => x }\nconst result = o.t`a${1}b`": "router.work:2:19",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), at+": "+refused) {
			t.Errorf("%q: %v, want the compile error %q at %s", source, err, refused, at)
		}
	}
	for source, want := range map[string]string{
		"const name = \"An\"\nconst result = `xin chào ${name}`": "xin chào An",
		"const f = (x) => x\nconst result = f(`a`)":              "a",
		"const result = [`a`, `b`].join(\"\")":                   "ab",
		"const a = \"1\"\n;`x`\nconst result = a":                "1",
	} {
		got, err := runFileResult(t, source)
		if err != nil {
			t.Errorf("%q: %v", source, err)
			continue
		}
		if !got.IsString() || got.Text() != want {
			t.Errorf("%q: %s, want %q", source, shownResult(got), want)
		}
	}
}

// An object's key is what JavaScript reads there (18/11/2026, docs/language.md mục 3.3): a name, a string, a number,
// or one expression in brackets; a shorthand is a name alone. Any expression was read as a computed key ({ 1 + 1: v },
// { o.b: v } keyed by o.b's value, the shorthand { k() } called k for the key and the value), and { [a, b]: v } took
// the text "a,b": each is a compile error at the key, as JavaScript's SyntaxError.
//
// CONTROL (18/11/2026): with the key not held, every refused case compiled.
func TestAnObjectKeyIsANameAStringANumberOrOneBracketedExpression(t *testing.T) {
	for source, want := range map[string]string{
		"const result = { 1 + 1: 3 }":                       "router.work:1:18: khoá của object là một tên, một chuỗi, một số hay một biểu thức trong ngoặc vuông",
		"const o = { b: \"k\" }\nconst result = { o.b: 3 }": "router.work:2:18: khoá của object là một tên",
		"const result = { -1: 3 }":                          "router.work:1:18: khoá của object là một tên",
		"const result = { `t`: 3 }":                         "router.work:1:18: khoá của object là một tên",
		"const x = 1\nconst result = { x + 1 }":             "router.work:2:18: viết tắt { x } trong object chỉ dùng với một tên",
		"const result = { 1 }":                              "router.work:1:18: viết tắt { x } trong object chỉ dùng với một tên",
		"const a = 1\nconst result = { [a, a]: 3 }":         "router.work:2:18: khoá tính của object là đúng một biểu thức trong ngoặc vuông",
		"const result = { []: 3 }":                          "router.work:1:18: khoá tính của object là đúng một biểu thức trong ngoặc vuông",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	source := "const a = 1\nconst k = null\nconst result = Object.keys({ a, b: 2, \"c-d\": 3, 4: 5, 1.5: 6, 0x10: 7, [a + 1]: 8, default: 9, [k]: 0, ...{ e: 10 }, }).join(\" \")"
	got, err := runFileResult(t, source)
	if want := "2 4 16 a b c-d 1.5 default null e"; err != nil || !got.IsString() || got.Text() != want {
		t.Errorf("the keys JavaScript reads: %v %s, want %q", err, shownResult(got), want)
	}
}

// A pattern's default in an object read before it is a pattern stays allowed, JavaScript's cover grammar (found by the
// node parity test while holding object keys, 18/11/2026): ({ a = 5 }) => a; left an object it is the resolver's
// error.
//
// CONTROL (18/11/2026): without the allowance, ({ a = 5 }) => a failed with the shorthand error, as did two cases of
// TestParityWithNode.
func TestAPatternsDefaultReadAsAnObjectFirstStaysAllowed(t *testing.T) {
	got, err := runFileResult(t, "const f = ({ a = 5, b: { c = 6 } = {} }) => a + c\nconst result = String(f({}))")
	if err != nil || !got.IsString() || got.Text() != "11" {
		t.Errorf("({ a = 5 }) => a: %v %s, want 11", err, shownResult(got))
	}
	if _, err := compileFile(t, "const x = 1\nconst result = { x = 2 }"); err == nil || !strings.Contains(err.Error(), "{ x = … } chỉ là giá trị mặc định khi tách") {
		t.Errorf("{ x = 2 } as an object: %v", err)
	}
}

// Where JavaScript reads an arrow function, an if and a pair of parentheses (18/11/2026, docs/language.md mục 3.3,
// 3.5): an arrow is a whole expression, never the operand of an operator (1 + a => 2 read "1function"), and its
// parameters end on the line of its =>; if is a statement, never a value (const r = if (c) 1 else 2 made bytecode the
// verifier refused, () => if (c) {1} returned undefined); () is only the parameters of () => … (const r = () made
// bytecode the verifier refused).
//
// CONTROL (18/11/2026): with the arrow not held, 1 + a => 2, !a => a and (a)⏎=> a compiled; with if a value again,
// const r = if (true) 1 else 2 failed only at its else and () => if (true) { 1 } compiled; with () a value,
// const r = () failed in the bytecode verifier (STACK_UNDERFLOW), not as an error of what was written.
func TestArrowsIfsAndParenthesesStandWhereJavaScriptReadsThem(t *testing.T) {
	for source, want := range map[string]string{
		"const a = 1\nconst result = 1 + a => 2": "router.work:2:22: hàm mũi tên làm toán hạng của một toán tử thì đặt trong ngoặc",
		"const result = !a => a":                 "router.work:1:19: hàm mũi tên làm toán hạng của một toán tử thì đặt trong ngoặc",
		"const f = (a)\n  => a":                  "router.work:2:3: => đứng cùng dòng với tham số của nó",
		"const result = if (true) 1 else 2":      "router.work:1:16: if là một câu lệnh, không có giá trị",
		"const f = () => if (true) { 1 }":        "router.work:1:17: if là một câu lệnh, không có giá trị",
		"const result = ()":                      "router.work:1:16: () rỗng chỉ là tham số của hàm mũi tên",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const c = true\nconst f = c ? (a) => a + 1 : (b) => b\nconst result = String(f(1))":                            "2",
		"const c = false\nconst f = c ? 1 : b => b * 3\nconst result = String(f(2))":                                    "6",
		"const run = () => {\nlet g = null\ng = (x) => x\nreturn String(g(5))\n}\nconst result = run()":                 "5",
		"const t = true\nconst result = String(t && (() => 4)())":                                                       "4",
		"const f = () => 3\nconst result = String(f())":                                                                 "3",
		"const run = () => {\nlet x = 0\nif (true) { x = 1 } else { x = 2 }\nreturn String(x)\n}\nconst result = run()": "1",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !got.IsString() || got.Text() != want {
			t.Errorf("%q: %v %s, want %q", source, err, shownResult(got), want)
		}
	}
}

// new is what JavaScript makes with it, for what Kitwork can make (18/11/2026, docs/language.md mục 3.3): new Date,
// new Array and new Object are their call, and new X with no parentheses calls X with none (it was X itself: typeof
// new Date was "function"). new on anything else (a function of the program, a member, String, Number, Boolean, whose
// new makes a wrapper object Kitwork has none of) is a compile error: it was a plain call, new String(5) the string
// "5". new Date.now is new (Date.now) in JavaScript, so it is an error too.
//
// CONTROL (18/11/2026): with new a prefix that means nothing again, typeof new Date gave "function" and every refused
// case compiled.
func TestNewIsWhatJavaScriptMakesForDateArrayAndObject(t *testing.T) {
	for source, want := range map[string]string{
		"const result = typeof new Date":                      "object",
		"const result = new Date(0).toISOString()":            "1970-01-01T00:00:00.000Z",
		"const result = String(new Array(3).length)":          "3",
		"const result = JSON.stringify(new Object())":         "{}",
		"const p = [0]\nconst result = typeof new Date(...p)": "object",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !got.IsString() || got.Text() != want {
			t.Errorf("%q: %v %s, want %q", source, err, shownResult(got), want)
		}
	}
	for source, want := range map[string]string{
		"const result = new String(5)":                       "router.work:1:20: Kitwork không có object bọc: new String(x) viết String(x)",
		"const result = new Boolean(false)":                  "router.work:1:20: Kitwork không có object bọc",
		"const f = () => 1\nconst result = new f()":          "router.work:2:20: Kitwork không có hàm dựng của chương trình",
		"const o = { b: () => 1 }\nconst result = new o.b()": "router.work:2:20: Kitwork không có hàm dựng của chương trình",
		"const result = new Date.now()":                      "router.work:1:24: new Date không ngoặc rồi một thành viên",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
}
