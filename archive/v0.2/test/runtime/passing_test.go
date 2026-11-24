package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A host call passed a closure its own VM made costs no entry in the shared registry of callers while
// that closure is the last lambda passed (04/11/2026): the registry's lock and map, and a slice for each
// call, were most of what a call of an array method with a callback cost beside its callbacks.
// LambdaCaller still answers that VM, through the closure's maker.
func TestAHostCallPassedItsOwnClosureAllocatesNothingPerCall(t *testing.T) {
	var asked, found int
	var machine *runtime.VM
	one := []value.Value{value.Number(1)} // made once: the test's own slice is no allocation of the call
	probe := value.Function(func(arguments ...value.Value) value.Value {
		asked++
		if caller := runtime.LambdaCaller(arguments[0].Lambda()); caller != nil && caller == machine {
			found++
		}
		return arguments[0].Call(one)
	})
	allocations := map[int]float64{}
	for _, calls := range []int{10, 100} {
		program := load(t, `const f = (x) => x + 1; t = 0; for (let i = 0; i < `+value.Number(float64(calls)).ToString().Text()+`; i++) { t = t + probe(f); }`)
		program.globals["probe"] = probe
		machine = program.vm
		program.run(t)
		if got, want := program.variable("t").ToString().Text(), value.Number(float64(2*calls)).ToString().Text(); got != want {
			t.Fatalf("%d calls: t = %s, want %s", calls, got, want)
		}
		allocations[calls] = program.allocationsPerRun(t)
	}
	if allocations[100] != allocations[10] {
		t.Errorf("100 host calls passed the VM's own closure make %.0f allocations, 10 make %.0f: a call allocates", allocations[100], allocations[10])
	}
	if found != asked {
		t.Errorf("LambdaCaller answered the calling VM %d times of %d", found, asked)
	}
	if caller := runtime.LambdaCaller(machine.Vars["f"].Lambda()); caller != nil {
		t.Errorf("once no host call is in progress, LambdaCaller answers a VM")
	}
}

// Every lambda a host call in progress was passed is found, whichever came last: a closure of the VM
// that a later lambda covers (two passed at once, or one passed by a call inside the callback) is listed
// as soon as it is covered, and found after the inner call returns too.
func TestEveryLambdaPassedIsFoundWhicheverCameLast(t *testing.T) {
	var machine *runtime.VM
	var missing []string
	check := func(where string, lambdas ...value.Value) {
		for _, lambda := range lambdas {
			if runtime.LambdaCaller(lambda.Lambda()) != machine {
				missing = append(missing, where)
			}
		}
	}
	program := load(t, `
const f = (x) => inner(g);
const g = (x) => 2;
r = [both(f, g), outer(f)].join(",");
`)
	program.globals["both"] = value.Function(func(arguments ...value.Value) value.Value {
		check("both", arguments[0], arguments[1])
		return value.Number(1)
	})
	program.globals["inner"] = value.Function(func(arguments ...value.Value) value.Value {
		check("inner, its own", arguments[0])
		check("inner, the outer call's", machine.Vars["f"])
		return arguments[0].Call(nil)
	})
	program.globals["outer"] = value.Function(func(arguments ...value.Value) value.Value {
		check("outer, before", arguments[0])
		result := arguments[0].Call(nil)
		check("outer, after the inner call", arguments[0])
		return result
	})
	machine = program.vm
	program.run(t)
	if got := program.variable("r").Text(); got != "1,2" {
		t.Errorf("r = %q, want 1,2", got)
	}
	if len(missing) > 0 {
		t.Errorf("LambdaCaller did not answer the calling VM: %v", missing)
	}
	for _, name := range []string{"f", "g"} {
		if runtime.LambdaCaller(machine.Vars[name].Lambda()) != nil {
			t.Errorf("%s: once no host call is in progress, LambdaCaller answers a VM", name)
		}
	}
}
