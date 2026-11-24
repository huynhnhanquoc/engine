package compiler_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// A source the compiler cannot read is that source's error, never the compiler's panic: a panic at boot was the end
// of the host and of every app on it (13/11/2026). The fuzzers below hold it for any text; go test runs their seeds,
// go test -fuzz=FuzzCompileSource -run=^$ ./test/compiler/ looks for more (engine/compiler/compiler_fuzz_test.go of
// the first engine had them; the rewrite had none).

// fuzzStatements are seeds of the statements a program is made of, besides the corpus's expressions.
var fuzzStatements = []string{
	`const x = 10;`,
	`let f = (a, b) => a + b; const r = f(1, 2);`,
	`import * as router from "kitwork/router"; router.get((context) => context.text("ok"));`,
	`const o = { name: "test", count: 123, ...{ a: 1 } }; const { name, count: n = 2 } = o;`,
	`let result = ""; switch (2) { case 1: case 2: result = result + "a"; case 3: result = result + "b"; break; default: result = "c"; }`,
	`let total = 0; for (let i = 0; i < 10; i++) { if (i == 5) { continue; } total += i; }`,
	`let total = 0; for (const item of [1, 2, 3]) { if (item == 2) { break; } total = total + item; }`,
	`const [a, , b = 3] = [1, 2]; const t = ` + "`${a}-${b}-${`in ${a}`}`" + `;`,
	`const nested = { value: { count: 3 } }; const r = nested?.value?.["count"] ?? 0;`,
	`const add = (a, b = 1) => { defer (() => a)(); return a + b; }; const r = add(2);`,
	`let x = 1; x++; ++x; x--; --x; x **= 2; x ??= 3; x ||= 4; x &&= 5;`,
	`const r = typeof x === "undefined" ? [1, 2, 3].map((v) => v * 2).filter((v) => v > 2) : null;`,
	`export const a = 1; export default (b) => b;`,
	`if (true) { const a = 1; } else if (false) { let b = 2; } else { var c = 3; }`,
	`const r = JSON.parse('{"a":[1,2,{"b":null}]}').a[2].b;`,
	`const r = /a+b/.test("aab") && "x".repeat(3).split("").join("-");`,
	"..",
	"((((",
	"{{{{",
	"`${`${",
	"--x",
	"let x = 1; const r = --------x;",
	"let x = 1; const r = ++++++x;",
	"let x = 1; const r = --(--x)++;",
	"let o = {}; o.a----;",
}

// fuzzSeeds adds the statements and every expression of the parity corpus, as the corpus runs it, to f.
func fuzzSeeds(f *testing.F) {
	for _, seed := range fuzzStatements {
		f.Add(seed)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "parity_corpus.json"))
	if err != nil {
		f.Fatal(err)
	}
	var corpus struct {
		Cases []corpusCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		f.Fatal(err)
	}
	for _, c := range corpus.Cases {
		f.Add("const result = (" + strings.TrimSpace(c.Src) + ");")
	}
}

// internalFault fails t when err is the compiler's own fault, with the stack it was met at.
func internalFault(t *testing.T, source string, err error) {
	t.Helper()
	var internal *compiler.InternalError
	if errors.As(err, &internal) {
		t.Fatalf("the compiler's own fault on %q: %v\n%s", source, internal, internal.Stack)
	}
}

func FuzzCompileSource(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, source string) {
		_, err := compiler.CompileSource(source)
		internalFault(t, source, err)
	})
}

// FuzzCompileRun is the whole way of a source no one checked: what compiles is a verified Program, and its run ends,
// within the energy and bytes it is given, in a value or a failure that says why, never in a panic.
func FuzzCompileRun(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 32*1024 {
			t.Skip()
		}
		bytecode, err := compiler.CompileSource(source)
		internalFault(t, source, err)
		if err != nil {
			return
		}
		if bytecode.Program == nil {
			t.Fatalf("%q compiled without a Program", source)
		}
		vm := runtime.New(bytecode.Program)
		vm.MaxEnergy = 20_000
		vm.MaxBytes = 16 << 20
		builtins.Install(vm.Globals)
		if result := vm.Run(); result.IsInvalid() && result.Message() == "" {
			t.Fatalf("%q failed without saying why", source)
		}
	})
}

// FuzzCompileExpression is what a .ui page gives the compiler: the expression of a {{ … }} or an attribute, and the
// handler of an event.
func FuzzCompileExpression(f *testing.F) {
	for _, seed := range []string{
		"a", "a.b?.c ?? 'x'", "dark ? 'sun' : 'moon'", "items.filter((i) => i.done).length", "count + 1",
		"toggle()", "format = 'rich'", "minutes--", "--------x", "(", "a ? 'x' : (b ? 'y' : 'z')", "`${a}`",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _, _, err := compiler.CompileExpressionBuiltins(source)
		internalFault(t, source, err)
		_, err = compiler.CompileHandler(source)
		internalFault(t, source, err)
		compiler.ExpressionStrings(source)
	})
}

// ++ or -- of an update of an update is the error of the inner one, as ----x is: --------x was a nil dereference in
// the parser, the update of nothing ----x left read as a name (13/11/2026), which ended the host at boot.
func TestAnUpdateOfAnUpdateIsTheSourcesError(t *testing.T) {
	for _, source := range []string{
		"let x = 1; const r = --------x;",
		"let x = 1; const r = ++++++x;",
		"let x = 1; const r = --(--x)++;",
		"let x = 1; const r = ------x--;",
		"let o = { a: 1 }; const r = ++++++o.a;",
	} {
		_, err := compiler.CompileSource(source)
		internalFault(t, source, err)
		if err == nil || !strings.Contains(err.Error(), "++ và -- chỉ dùng trên một tên, o.x hay o[k]") {
			t.Errorf("%q: %v, want the error of the inner update", source, err)
		}
	}
}
