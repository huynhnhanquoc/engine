package compiler_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// scalarKey is how a test tells two scalar constants apart: its kind and its payload, a number by its
// bits.
func scalarKey(constant value.Value) (string, bool) {
	switch {
	case constant.IsUndefined(), constant.IsNull():
		return constant.Kind(), true
	case constant.IsBoolean():
		return "boolean " + strconv.FormatBool(constant.Boolean()), true
	case constant.IsNumber():
		return "number " + strconv.FormatUint(math.Float64bits(constant.Number()), 16), true
	case constant.IsString():
		return "string " + constant.Text(), true
	}
	return "", false
}

// requireEachScalarOnce fails when a program's constant table holds a scalar twice.
func requireEachScalarOnce(t *testing.T, program *runtime.Program) {
	t.Helper()
	seen := map[string]int{}
	for index, constant := range program.Constants() {
		key, scalar := scalarKey(constant)
		if !scalar {
			continue
		}
		if first, ok := seen[key]; ok {
			t.Errorf("constants %d and %d are both %s", first, index, key)
			return
		}
		seen[key] = index
	}
}

// The constant table holds each scalar once, however often the program uses it: a variable's name read
// and written, a key, a number, a string literal, true, null and undefined.
func TestEachScalarConstantIsHeldOnce(t *testing.T) {
	source := `
const f = (o) => { let total = 0; for (let i = 0; i < 3; i++) { total = total + o.n * 2 + i; } return total; };
const g = (o) => [o.n, o.n, "n", "n", 2, 2, true, true, null, null, undefined, 0, -0];
r = f({ n: 2 }) + g({ n: 2 }).length;
`
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatal(err)
	}
	requireEachScalarOnce(t, bytecode.Program)

	vm := runtime.New(bytecode.Program)
	globals := map[string]value.Value{}
	builtins.Install(globals)
	vm.FastReset(bytecode.Program, globals)
	if result := vm.Run(); result.IsInvalid() {
		t.Fatal(result.Message())
	}
	if got := vm.Vars["r"].Number(); got != 28 {
		t.Errorf("r = %v, want 28", got)
	}
}

// A program past 65,535 bytes of code is written again with wide jumps; the constants of the first pass
// are cut off, and what the table knew of them with them, so the second pass holds each scalar once and
// every index it writes is one of its own.
func TestConstantsOfAWideProgramAreHeldOnce(t *testing.T) {
	var source strings.Builder
	source.WriteString("total = 0;\n")
	for i := 0; i < 4000; i++ {
		source.WriteString("if (total >= 0) { total = total + " + strconv.Itoa(i%7) + "; }\n")
	}
	bytecode, err := compiler.CompileSource(source.String())
	if err != nil {
		t.Fatal(err)
	}
	if size := len(bytecode.Program.Instructions()); size <= runtime.MaxNarrowOperand {
		t.Fatalf("the program is %d bytes, want more than %d so that it is written wide", size, runtime.MaxNarrowOperand)
	}
	requireEachScalarOnce(t, bytecode.Program)
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 10_000_000
	vm.FastReset(bytecode.Program, map[string]value.Value{})
	if result := vm.Run(); result.IsInvalid() {
		t.Fatal(result.Message())
	}
	want := 0
	for i := 0; i < 4000; i++ {
		want += i % 7
	}
	if got := vm.Vars["total"].Number(); got != float64(want) {
		t.Errorf("total = %v, want %d", got, want)
	}
}
