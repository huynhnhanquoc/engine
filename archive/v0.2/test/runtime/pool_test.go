package runtime_test

import (
	"strings"
	"testing"
)

// A VM back in the pool keeps an energy ceiling: ResetForPool sets the default one, not zero, which is
// no ceiling at all (05/11/2026). A host that takes the VM and forgets MaxEnergy runs a loop that never
// ends into the energy limit, not forever.
func TestAPooledVMRunsWithinTheDefaultEnergy(t *testing.T) {
	program := load(t, `let total = 0; for (let i = 0; i < 30000000; i++) { total = total + i; }`)
	program.vm.ResetForPool()
	program.vm.FastReset(program.program, program.globals)
	result := program.vm.Run()
	if !result.IsInvalid() || !strings.Contains(result.Message(), "Energy Limit Exceeded") {
		t.Fatalf("a pooled VM run without a MaxEnergy of its own ends with %s, want the energy limit", describe(result))
	}
}

// ResetForPool resets the frames the run used, as FastReset does, and no more: until 05/11/2026 it reset
// every frame of the call stack again, 63 of them, which cost a small request's run more than the run.
func TestResetForPoolCostsWhatTheRunUsed(t *testing.T) {
	program := load(t, `const f = (x) => x + 1; let y = f(1);`)
	pooled := func(rounds int) {
		for i := 0; i < rounds; i++ {
			program.vm.FastReset(program.program, program.globals)
			program.vm.Run()
			program.vm.ResetForPool()
		}
	}
	reset := func(rounds int) {
		for i := 0; i < rounds; i++ {
			program.vm.FastReset(program.program, program.globals)
			program.vm.Run()
			program.vm.FastReset(program.program, program.globals)
		}
	}
	requireCost(t, "a run given back with ResetForPool", pooled, reset, 1.7)
}
