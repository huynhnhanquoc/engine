package runtime_test

import (
	"strconv"
	"strings"
	"testing"
)

// The top-level map a closure holds is made again on the next run for as many names as the last run
// held, so it does not grow through every size on the way: 100 top-level names and one closure cost a
// few allocations a run. (Made with no size, it grew from 8 slots to 128 each run, nine allocations
// and more.)
func TestARunWithATopLevelClosureMakesItsMapOnce(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 100; i++ {
		source.WriteString("const v" + strconv.Itoa(i) + " = " + strconv.Itoa(i) + "; ")
	}
	source.WriteString("const f = (x) => x + 1; t = f(v99);")
	program := load(t, source.String())
	program.run(t)
	if got := program.variable("t").Number(); got != 100 {
		t.Fatalf("t = %v, want 100", got)
	}
	if allocations := program.allocationsPerRun(t); allocations > 6 {
		t.Errorf("%.0f allocations a run, want at most 6", allocations)
	} else {
		t.Logf("%.0f allocations a run", allocations)
	}
}

// A function whose frame a closure holds gets new slots for its next call (bytecode 7; it was a new map
// made for as many names as the last held): a function of 40 variables that makes a closure costs two
// allocations a call.
func TestAFrameAClosureHoldsIsMadeAgainOnce(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 40; i++ {
		body.WriteString("const v" + strconv.Itoa(i) + " = x + " + strconv.Itoa(i) + "; ")
	}
	program := load(t, "const f = (x) => { "+body.String()+"const g = () => v39; return g(); }; t = 0; for (let i = 0; i < 100; i++) { t = f(i); }")
	program.run(t)
	if got := program.variable("t").Number(); got != 138 {
		t.Fatalf("t = %v, want 138", got)
	}
	// two a call: the closure and the frame's 41 slots in one slice; a map of 40 names was five, its
	// header, directory, table and groups, and grown step by step ten
	if allocations := program.allocationsPerRun(t); allocations > 250 {
		t.Errorf("%.0f allocations for 100 calls, want at most 250", allocations)
	} else {
		t.Logf("%.0f allocations for 100 calls", allocations)
	}
}
