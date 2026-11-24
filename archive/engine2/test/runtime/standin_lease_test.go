package runtime_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A stand-in runs on the VM that made it only while that VM serves the request that made it. A host that
// keeps what a request handed it, a frozen file's function or an object the request built around one
// ({ toString: label } as a literal, with a SET, with a spread), and converts or calls it once the VM went
// back to the pool, gets a TYPE failure, and nothing runs on the VM another request now holds (05/11/2026:
// it read "VM has no verified program", ran on the next request's VM, and from another goroutine panicked
// and gave that request wrong answers, where an object a request built before the stand-ins converted to a
// clean TYPE failure).
func TestAStandInRunsNothingOnceItsRequestEnded(t *testing.T) {
	boot := booted(t, `
const label = () => "from label";
const methods = { toString: label };
const viaLiteral = (keep) => { keep({ toString: label }); return 1; };
const viaSet = (keep) => { const o = {}; o.toString = label; keep(o); return 1; };
const viaSpread = (keep) => { keep({ ...methods }); return 1; };
const viaFunction = (keep) => { keep(label); return 1; };
const spin = (n) => { let s = 0; for (let i = 0; i < n; i++) { s = s + i; } return s; };
`)
	boot.vm.Freeze()
	convert := func(kept value.Value) value.Value {
		if kept.IsFunction() {
			return kept.Call(nil)
		}
		return kept.ToString()
	}
	ended := func(got value.Value) bool {
		return got.IsInvalid() && got.Code() == value.TypeCode && strings.Contains(got.Message(), "a request that has ended")
	}
	for _, shape := range []string{"viaLiteral", "viaSet", "viaSpread", "viaFunction"} {
		machine := boot.request(nil)
		var kept value.Value
		keep := value.Function(func(arguments ...value.Value) value.Value {
			kept = arguments[0]
			return value.Undefined()
		})
		if got := machine.ExecuteLambda(boot.variable(shape).Lambda(), []value.Value{keep}); got.IsInvalid() {
			t.Fatalf("%s: %s", shape, got.Failure())
		}
		if got := convert(kept); got.IsInvalid() || got.Text() != "from label" {
			t.Errorf("%s, converted while its request holds the VM: %s, want from label", shape, describe(got))
		}
		machine.ResetForPool()
		if got := convert(kept); !ended(got) {
			t.Errorf("%s, converted once the VM went back to the pool: %s, want the TYPE failure of an ended request", shape, describe(got))
		}
		machine.FastReset(boot.program, boot.globals)
		if got := convert(kept); !ended(got) {
			t.Errorf("%s, converted while the VM serves another request: %s, want the TYPE failure of an ended request", shape, describe(got))
		}
		// the next request runs on the VM while another goroutine converts what the host kept
		for round := 0; round < 5; round++ {
			var group sync.WaitGroup
			var answer value.Value
			var mutex sync.Mutex
			var wrong []string
			group.Add(2)
			go func() {
				defer group.Done()
				defer func() {
					if recovered := recover(); recovered != nil {
						answer = value.Fail(value.FailCode, "panic")
					}
				}()
				machine.FastReset(boot.program, boot.globals)
				answer = machine.ExecuteLambda(boot.variable("spin").Lambda(), []value.Value{value.Number(3000)})
			}()
			go func() {
				defer group.Done()
				defer func() {
					if recovered := recover(); recovered != nil {
						mutex.Lock()
						wrong = append(wrong, "panic")
						mutex.Unlock()
					}
				}()
				for i := 0; i < 50; i++ {
					if got := convert(kept); !ended(got) {
						mutex.Lock()
						wrong = append(wrong, describe(got))
						mutex.Unlock()
					}
				}
			}()
			group.Wait()
			if answer.IsInvalid() || answer.Number() != 4498500 {
				t.Errorf("%s: the next request's answer %s, want 4498500", shape, describe(answer))
			}
			if len(wrong) > 0 {
				t.Errorf("%s: converted beside the next request: %s, want the TYPE failure of an ended request", shape, wrong[0])
			}
			machine.ResetForPool()
		}
	}
}

// A Go function a request hands a host (safe, Array.from, an array's map), kept and called once the request
// ended, hands the functions it is called with to the VM no more: they run in the lease the Go function was
// handed over in, which has ended, so each is the TYPE failure of an ended request and runs nothing, after
// the pool and while the VM serves another request (05/11/2026: they ran on the VM as it was by then, "VM
// has no verified program" after the pool and the boot's function run on the next request's VM). Making
// those stand-ins writes nothing of the VM, so a host calling one from its own goroutine while the VM serves
// requests is no data race (-race: vm.runHere was written from the host's goroutine).
func TestAGoFunctionAHostKeptHandsNothingOnceItsRequestEnded(t *testing.T) {
	boot := booted(t, `
const greet = () => "greeted";
const handOver = (keep, which) => {
  if (which === "safe") { keep(safe); }
  if (which === "from") { keep(Array.from); }
  if (which === "map") { keep([5].map); }
  if (which === "isFrozen") { keep(Object.isFrozen); }
  return 1;
};
const byNumber = (a, b) => a - b;
const standIns = (n) => { let c = 0; for (let i = 0; i < n; i++) { const o = { toString: greet }; c = c + (("" + o) === "greeted" ? 1 : 0) + [2, 1].sort(byNumber)[0] - 1; } return c; };
`)
	boot.vm.Freeze()
	greet := boot.variable("greet")
	call := func(which string, kept value.Value) string {
		got := kept.Call([]value.Value{greet})
		if which == "from" {
			got = kept.Call([]value.Value{value.Array([]value.Value{value.Number(1)}), greet})
		}
		return describe(got)
	}
	hand := func(machine *runtime.VM, which string) value.Value {
		var kept value.Value
		keep := value.Function(func(arguments ...value.Value) value.Value {
			kept = arguments[0]
			return value.Undefined()
		})
		if got := machine.ExecuteLambda(boot.variable("handOver").Lambda(), []value.Value{keep, value.String(which)}); got.IsInvalid() {
			t.Fatalf("%s: %s", which, got.Failure())
		}
		return kept
	}
	// [5].map read off its array was a function bound to it until 18/11/2026; a method read off its value has none to
	// work on since (docs/language.md mục 4.2), so no such function is made to keep
	for _, which := range []string{"safe", "from"} {
		machine := boot.request(nil)
		kept := hand(machine, which)
		if got := call(which, kept); !strings.Contains(got, "greeted") {
			t.Errorf("%s, called while its request holds the VM: %s, want greet's answer", which, got)
		}
		machine.ResetForPool()
		if got := call(which, kept); !strings.Contains(got, "a request that has ended") || strings.Contains(got, "greeted") {
			t.Errorf("%s, called once the VM went back to the pool: %s, want the TYPE failure of an ended request", which, got)
		}
		machine.FastReset(boot.program, boot.globals)
		if got := call(which, kept); !strings.Contains(got, "a request that has ended") || strings.Contains(got, "greeted") {
			t.Errorf("%s, called while the VM serves another request: %s, want the TYPE failure of an ended request", which, got)
		}
	}

	// the host calls a kept Object.isFrozen with greet from its own goroutine while the VM serves requests
	// that make stand-ins of their own
	machine := boot.request(nil)
	kept := hand(machine, "isFrozen")
	machine.ResetForPool()
	var stop atomic.Bool
	var host sync.WaitGroup
	host.Add(1)
	go func() {
		defer host.Done()
		for !stop.Load() {
			kept.Call([]value.Value{greet})
		}
	}()
	for round := 0; round < 200; round++ {
		machine.FastReset(boot.program, boot.globals)
		if got := machine.ExecuteLambda(boot.variable("standIns").Lambda(), []value.Value{value.Number(5)}); got.IsInvalid() || got.Number() != 5 {
			t.Errorf("request %d beside the host's calls: %s, want 5", round, describe(got))
			break
		}
		machine.ResetForPool()
	}
	stop.Store(true)
	host.Wait()
}
