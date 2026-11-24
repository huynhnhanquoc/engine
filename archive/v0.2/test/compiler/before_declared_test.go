package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A const or a let used before its line in the function that declares it is the file's compile error (15/11/2026,
// round 3 of the 13/11 review): JavaScript throws ReferenceError there, its temporal dead zone, and Kitwork read
// undefined, or in a loop the value of the turn before (typeof k was "undefined", then "number"). A function made
// before the line reads the name when it is called, as JavaScript allows.
func TestANameUsedBeforeItsLineIsACompileError(t *testing.T) {
	for source, want := range map[string]string{
		`const a = later; const later = 1;`:                                "router.work:1:11: later được dùng trước dòng khai nó (router.work:1:24)",
		`const f = () => { const x = y; const y = 1; return x; }; f();`:    "router.work:1:29: y được dùng trước dòng khai nó",
		`{ const seen = x; } let x = 1;`:                                   "router.work:1:16: x được dùng trước dòng khai nó",
		`count = 1; let count = 0;`:                                        "router.work:1:1: count được dùng trước dòng khai nó",
		`for (let i = 0; i < 2; i++) { const t = typeof k; const k = i; }`: "router.work:1:48: k được dùng trước dòng khai nó",
		`const result = f(); const f = () => 1;`:                           "router.work:1:16: f được dùng trước dòng khai nó",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want the compile error %q", source, err, want)
		}
	}
}

// A function made before the line of a name it reads reads it when called, and function f() {…} is made as its block
// begins, so a call before its line calls it, as JavaScript hoists it (15/11/2026: "f không phải hàm (là undefined)").
func TestAFunctionIsMadeAsItsBlockBegins(t *testing.T) {
	for source, want := range map[string]float64{
		`const f = () => later; const later = 1; const result = f();`:                                1,
		`const result = f(); function f() { return 2; }`:                                             2,
		`function f() { return k; } const k = 3; const result = f();`:                                3,
		`const result = (() => { const r = g(); function g() { return 4; } return r; })();`:          4,
		`const result = (() => { let r = 0; { r = h(); function h() { return 5; } } return r; })();`: 5,
	} {
		bytecode, err := compileFile(t, source)
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		globals := map[string]value.Value{}
		builtins.Install(globals)
		vm := runtime.New(bytecode.Program)
		vm.MaxEnergy = 1_000_000
		vm.FastReset(bytecode.Program, globals)
		if result := vm.Run(); result.IsInvalid() {
			t.Errorf("%s: %s", source, result.Message())
			continue
		}
		if got := vm.Vars["result"]; !got.IsNumber() || got.Number() != want {
			t.Errorf("%s: %v, want %v", source, got, want)
		}
	}
}
