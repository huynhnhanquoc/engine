package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A closure called back from Go runs on the VM that called the host, not on the VM that made it: a
// function made while a module loaded (VM a) and handed to a host by a request of the same program (VM
// b) runs on b, in b's budget, while a has gone on to run something else. It ran on a, which then
// refused it as a lambda of a different program, or would have run it in the middle of a's own run.
func TestAClosureRunsOnTheVMThatCalledTheHost(t *testing.T) {
	bytecode, err := compiler.CompileSource(`
const double = (x) => x * 2;
const byValue = (left, right) => left - right;
r = typeof shared === "function" ? [Array.from([1, 2, 3], shared).join(","), [3, 1, 2].sort(order).join(","), [1, 2].map(shared).join(",")].join(" ") : "";
`)
	if err != nil {
		t.Fatal(err)
	}
	program := bytecode.Program
	loader := runtime.New(program)
	loaderGlobals := map[string]value.Value{}
	builtins.Install(loaderGlobals)
	loader.FastReset(program, loaderGlobals)
	if result := loader.Run(); result.IsInvalid() {
		t.Fatal(result.Message())
	}
	shared, order := loader.Vars["double"], loader.Vars["byValue"]
	if shared.Lambda() == nil || order.Lambda() == nil {
		t.Fatalf("double is a %s and byValue a %s, want two functions of the program", shared.Kind(), order.Kind())
	}

	// the loader goes on to another program, as a VM back in the pool does
	other, err := compiler.CompileSource(`x = 1;`)
	if err != nil {
		t.Fatal(err)
	}
	loader.FastReset(other.Program, loaderGlobals)
	if result := loader.Run(); result.IsInvalid() {
		t.Fatal(result.Message())
	}
	before := loader.Stats().Instructions

	requestGlobals := map[string]value.Value{}
	builtins.Install(requestGlobals)
	requestGlobals["shared"] = shared
	requestGlobals["order"] = order
	request := runtime.New(program)
	request.MaxEnergy = 1_000_000
	request.FastReset(program, requestGlobals)
	if result := request.Run(); result.IsInvalid() {
		t.Fatalf("the request failed: %s", result.Message())
	}
	if got, want := request.Vars["r"].ToString().Text(), "2,4,6 1,2,3 2,4"; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
	if after := loader.Stats().Instructions; after != before {
		t.Errorf("the loader ran %d instructions of the request's calls", after-before)
	}
}

// group, groupBy, sortBy and unique were Kitwork's own array methods, run by the runtime; LANG dropped
// them (27/10/2026), so an array has no such method, the TYPE failure of any method it does not have.
func TestKitworksOwnArrayMethodsAreGone(t *testing.T) {
	for _, method := range []string{"group", "groupBy", "sortBy", "unique"} {
		program := load(t, `r = safe(() => [1, 2].`+method+`((x) => x)).error.message;`)
		program.run(t)
		if got, want := program.variable("r").Text(), "array has no method "+method; !strings.HasSuffix(got, want) {
			t.Errorf("%s: %q, want it to end with %q", method, got, want)
		}
	}
}
