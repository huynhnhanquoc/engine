package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// compileFile writes source as a file of its own and compiles it as the host compiles a file.
func compileFile(t *testing.T, source string) (*compiler.Bytecode, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "router.work")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return compiler.CompileFile(file)
}

// A file's names are declared (15/11/2026, round 3 of the 13/11 review): a name read that nothing declares, no
// builtin is and no import gives, or a name assigned that nothing declares, is the file's compile error at the
// name, as JavaScript throws ReferenceError there; it read undefined and made a variable, so a misspelt name ran on
// as nothing. typeof of such a name is "undefined", as JavaScript gives it; a builtin, kitwork, an import, a name of
// the file's own (declared later at the top level too), a parameter, a var of a view are declared.
func TestAFilesNamesAreDeclared(t *testing.T) {
	const router = "import * as router from \"kitwork/router\";\n"
	for source, want := range map[string]string{
		`const total = 1; const result = totl + 1;`: "router.work:1:33: totl chưa được khai",
		`count = 3;`:                           "router.work:1:1: count = … gán vào một tên chưa khai: khai nó ở chỗ nó bắt đầu, let count = …",
		`const f = () => { reslt = 1; }; f();`: "router.work:1:19: reslt = … gán vào một tên chưa khai",
		`const f = () => { const inner = 1; return inner; }; const x = inner;`: "router.work:1:63: inner chưa được khai",
		`JSON = null;`: "router.work:1:1: JSON = … gán vào một tên chưa khai",
		router + `router.get((context) => context.text(contxt.query("q")));`: "router.work:2:38: contxt chưa được khai",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		`const result = typeof nothing;`,
		`const result = [Math.max(1, 2), JSON.stringify({}), typeof console, safe(() => 1).ok, kitwork === kitwork];`,
		`const f = () => later(); const later = () => 1; const result = f();`,
		`const f = (a, { b }) => a + b; const result = f(1, { b: 2 });`,
		`const sum = () => { let n = 0; for (let i = 0; i < 3; i++) { n = n + i; } return n; }; const result = sum();`,
		router + `router.view((context) => { var q = ""; return context.bind({ q }); });`,
	} {
		if _, err := compileFile(t, source); err != nil {
			t.Errorf("%s: %v, want it compiled", source, err)
		}
	}
	bytecode, err := compileFile(t, `const result = typeof nothing;`)
	if err != nil {
		t.Fatal(err)
	}
	globals := map[string]value.Value{}
	builtins.Install(globals)
	vm := runtime.New(bytecode.Program)
	vm.FastReset(bytecode.Program, globals)
	if result := vm.Run(); result.IsInvalid() || vm.Vars["result"].Text() != "undefined" {
		t.Errorf("typeof nothing: %v %v, want \"undefined\"", result, vm.Vars["result"])
	}
}

// A source compiled alone, a test's snippet, may still assign a name to read back, and an expression of a .ui file
// reads its page's names undeclared: neither is a file.
func TestASnippetAndAnExpressionReadUndeclaredNames(t *testing.T) {
	if _, err := compiler.CompileSource(`r = 1;`); err != nil {
		t.Errorf("a snippet: %v", err)
	}
	if _, names, err := compiler.CompileExpression(`count + 1`); err != nil || len(names) != 1 || names[0] != "count" {
		t.Errorf("an expression: %v %v, want it reading count", names, err)
	}
}
