package runtime_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A site's file runs once on a VM of its own as the site boots, and its functions then run at every
// request, many at once, each on a request's VM (host/boot.go). Freeze is what the host calls
// once the file has run (05/11/2026): before, a handler that assigned a top-level let wrote the boot
// VM's map from every request at once, which ends the Go process, and one that pushed into a top-level
// array lost items.

// booted runs source once on a VM of its own, as a site's file runs at boot, and gives it with the
// program, before Freeze.
func booted(t *testing.T, source string) *loaded {
	t.Helper()
	boot := load(t, source)
	boot.run(t)
	return boot
}

// request is a VM of the boot's program for one request: its own globals over the builtins.
func (l *loaded) request(globals map[string]value.Value) *runtime.VM {
	merged := map[string]value.Value{}
	for name, item := range l.globals {
		merged[name] = item
	}
	for name, item := range globals {
		merged[name] = item
	}
	machine := runtime.New(l.program)
	machine.FastReset(l.program, merged)
	machine.MaxEnergy = 10_000_000
	return machine
}

// call runs the boot's function name on machine.
func (l *loaded) call(machine *runtime.VM, name string) value.Value {
	return machine.ExecuteLambda(l.variable(name).Lambda(), nil)
}

// TestAFrozenRunsVariablesAreReadNotWritten: once a run is frozen, another VM running its functions
// reads every variable they hold and writes none: a top-level let, a let of a top-level block, a let of a
// function around a closure, a top-level array and an object deep in another, and an array only a
// function the host kept holds (Freeze's roots). Each write is a TYPE failure naming what it writes, and
// nothing changes.
func TestAFrozenRunsVariablesAreReadNotWritten(t *testing.T) {
	boot := booted(t, `
let count = 0;
const seen = [];
const settings = { deep: { list: [] } };
let step;
{ let n = 0; step = () => { n = n + 1; return n }; }
const make = () => { let k = 0; return () => { k = k + 1; return k }; };
const counter = make();
const makeHidden = () => { const box = []; return () => box.push(1); };
const bump = () => { count = count + 1; return count; };
const push = () => seen.push(1);
const deeper = () => settings.deep.list.push(1);
const rename = () => { settings.deep = 1; return 1; };
const read = () => [count, seen.length, settings.deep.list.length, typeof step].join(",");
`)
	hidden := boot.vm.ExecuteLambda(boot.variable("makeHidden").Lambda(), nil)
	boot.vm.Freeze(hidden)
	if !boot.vm.Frozen() {
		t.Fatal("Freeze marks the VM frozen")
	}
	machine := boot.request(nil)
	for _, c := range []struct{ name, message string }{
		{"bump", "cannot assign to count"},
		{"step", "cannot assign to n"},
		{"counter", "cannot assign to k"},
		{"push", "cannot add property 0, object is not extensible"},
		{"deeper", "cannot add property 0, object is not extensible"},
		{"rename", "cannot assign to read only property 'deep'"},
	} {
		got := boot.call(machine, c.name)
		if !got.IsInvalid() || !strings.Contains(got.Message(), c.message) {
			t.Errorf("%s on a frozen run's variables: %s, want a failure saying %q", c.name, describe(got), c.message)
		}
	}
	if got := machine.ExecuteLambda(hidden.Lambda(), nil); !got.IsInvalid() || !strings.Contains(got.Message(), "not extensible") {
		t.Errorf("an array only a function the host kept holds is frozen with it: %s", describe(got))
	}
	if got := boot.call(machine, "read"); got.Text() != "0,0,0,function" {
		t.Errorf("after the refused writes the variables read %s, want 0,0,0,function", describe(got))
	}
}

// TestAFunctionAnotherVMMadeRunsOnTheVMThatHandsItOver: a function one VM made, handed to a host call by
// another VM, runs on the VM that handed it, the one waiting in the host call on this goroutine. Before
// 05/11/2026 it ran on whichever VM the shared registry of callers named, the deepest of those that had
// been handed it: here the second VM, which holds the same function in a host call of its own, on its
// own goroutine. The comparator reads which VM runs it from that VM's globals.
func TestAFunctionAnotherVMMadeRunsOnTheVMThatHandsItOver(t *testing.T) {
	boot := booted(t, `
const order = (a, b) => (who === "first" ? a - b : b - a);
const sorted = () => [3, 1, 2].sort(order).join(",");
const deeper = () => hold(order);
const deep = () => deeper();
`)
	held, release := make(chan struct{}), make(chan struct{})
	second := boot.request(map[string]value.Value{
		"who": value.String("second"),
		"hold": value.Function(func(arguments ...value.Value) value.Value {
			close(held)
			<-release
			return arguments[0].Call([]value.Value{value.Number(2), value.Number(1)})
		}),
	})
	done := make(chan value.Value)
	go func() { done <- boot.call(second, "deep") }()
	<-held
	first := boot.request(map[string]value.Value{"who": value.String("first")})
	got := boot.call(first, "sorted")
	close(release)
	if heldResult := <-done; heldResult.Number() != -1 {
		t.Errorf("the second VM's own call of the function ran elsewhere: %s, want -1", describe(heldResult))
	}
	if got.Text() != "1,2,3" {
		t.Errorf("sort with a function another VM made gives %s, want 1,2,3 (the comparator ran on the VM that handed it over)", describe(got))
	}
}

// TestAFrozenRunsObjectDoesNotRunItsValueOfOnTheFrozenVM: value converts an object through its own
// valueOf by itself, without a call of the program handing the function over, so the VM running cannot
// be told; a frozen VM runs nothing more, so the conversion is a TYPE failure, and the frozen VM spends
// nothing. Before 05/11/2026 it ran on the boot VM, from every request at once.
func TestAFrozenRunsObjectDoesNotRunItsValueOfOnTheFrozenVM(t *testing.T) {
	boot := booted(t, `
const price = { valueOf: () => 42 };
const add = () => price + 1;
const made = () => ({ valueOf: () => 42 }) + 1;
`)
	boot.vm.Freeze()
	spent := boot.vm.Energy
	machine := boot.request(nil)
	if got := boot.call(machine, "add"); !got.IsInvalid() || !strings.Contains(got.Message(), "valueOf") {
		t.Errorf("an object of the frozen run converted by its own valueOf: %s, want the TYPE failure naming valueOf", describe(got))
	}
	if boot.vm.Energy != spent {
		t.Errorf("the frozen VM spent %d energy after Freeze", boot.vm.Energy-spent)
	}
	if got := boot.call(machine, "made"); got.Number() != 43 {
		t.Errorf("an object the request makes converts as before: %s, want 43", describe(got))
	}
}

// TestAStandInIsTheFunctionItStandsFor: what a host is handed for a function another VM made is that
// function to ===, to its name and to its length (value.RunningOn).
func TestAStandInIsTheFunctionItStandsFor(t *testing.T) {
	boot := booted(t, `
const twice = (x, y) => x * 2;
const same = () => [back(twice) === twice, back(twice).name, back(twice).length, back(twice)(4)].join(",");
`)
	boot.vm.Freeze()
	machine := boot.request(map[string]value.Value{
		"back": value.Function(func(arguments ...value.Value) value.Value { return arguments[0] }),
	})
	if got := boot.call(machine, "same"); got.Text() != "true,twice,2,8" {
		t.Errorf("the stand-in reads %s, want true,twice,2,8", describe(got))
	}
}

// TestRequestsRunAFrozenRunsFunctionsAtOnce: many VMs run the frozen run's functions at once, read its
// variables, hand its comparator to sort and try to write; each gets its own answer, and under -race
// nothing is written twice at once.
func TestRequestsRunAFrozenRunsFunctionsAtOnce(t *testing.T) {
	boot := booted(t, `
let count = 0;
const seen = [];
const order = (a, b) => a - b;
const work = () => [[5, 3, 4, 1, 2].sort(order).findLast((x) => x < 5), safe(() => { count = count + 1 }).error === null, safe(() => seen.push(1)).error === null, count, seen.length].join(",");
`)
	boot.vm.Freeze()
	var group sync.WaitGroup
	failures := make(chan string, 8)
	for g := 0; g < 8; g++ {
		group.Add(1)
		go func() {
			defer group.Done()
			machine := boot.request(nil)
			for i := 0; i < 200; i++ {
				machine.FastReset(boot.program, boot.globals)
				machine.MaxEnergy = 10_000_000
				if got := boot.call(machine, "work"); got.Text() != "4,false,false,0,0" {
					failures <- describe(got)
					return
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("a request of the frozen run gives %s, want 4,false,false,0,0", failure)
	}
}

func describe(v value.Value) string {
	if v.IsInvalid() {
		return v.Failure()
	}
	return v.Kind() + " " + value.Stringify(v, "").Text()
}
