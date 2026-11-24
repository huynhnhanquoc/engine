// Package runtime_test holds core/runtime from outside: what a program sees of a call, a closure and a
// host call, and what each of them costs where a fast path keeps it cheap.
package runtime_test

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// loaded is one program on a VM of its own, with the globals core/builtins installs.
type loaded struct {
	program *runtime.Program
	vm      *runtime.VM
	globals map[string]value.Value
}

func load(t testing.TB, source string) *loaded {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	globals := map[string]value.Value{}
	builtins.Install(globals)
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 1_000_000_000
	vm.FastReset(bytecode.Program, globals)
	return &loaded{program: bytecode.Program, vm: vm, globals: globals}
}

// run runs the program once more from the start and fails the test on a failure.
func (l *loaded) run(t testing.TB) value.Value {
	t.Helper()
	l.vm.FastReset(l.program, l.globals)
	result := l.vm.Run()
	if result.IsInvalid() {
		t.Fatalf("run: %s", result.Message())
	}
	return result
}

// variable is a top-level variable of the last run.
func (l *loaded) variable(name string) value.Value {
	return l.vm.Vars[name]
}

// runs is a measured side of costRatio: the program run rounds times.
func (l *loaded) runs(t testing.TB) func(rounds int) {
	return func(rounds int) {
		for i := 0; i < rounds; i++ {
			l.vm.FastReset(l.program, l.globals)
			if result := l.vm.Run(); result.IsInvalid() {
				t.Fatalf("run: %s", result.Message())
			}
		}
	}
}

// allocationsPerRun is how many allocations one run of the program makes, FastReset included.
func (l *loaded) allocationsPerRun(t testing.TB) float64 {
	l.run(t)
	return testing.AllocsPerRun(50, func() {
		l.vm.FastReset(l.program, l.globals)
		l.vm.Run()
	})
}

// costRatio is the best time of measured over the best time of reference, each running its own loop
// of the same number of rounds, the trials alternating so that a loaded machine slows both sides. The
// rounds grow until the quicker side takes a few milliseconds, since a clock (Windows' among them) may
// tick at half a millisecond, or until the slower side takes fifty.
func costRatio(measured, reference func(rounds int)) float64 {
	timed := func(f func(int), rounds int) time.Duration {
		start := time.Now()
		f(rounds)
		return time.Since(start)
	}
	rounds := 1
	for rounds < 1<<30 {
		first, second := timed(measured, rounds), timed(reference, rounds)
		if min(first, second) >= 4*time.Millisecond || max(first, second) >= 50*time.Millisecond {
			break
		}
		rounds *= 2
	}
	measuredBest, referenceBest := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
	for trial := 0; trial < 7; trial++ {
		measuredBest = min(measuredBest, timed(measured, rounds))
		referenceBest = min(referenceBest, timed(reference, rounds))
	}
	return float64(measuredBest) / float64(referenceBest)
}

// requireCost fails when measured costs more than limit times what reference costs in each of five
// measures. One measure was a flake on a busy machine (12/11/2026): go test ./... runs the packages at
// once, and a collection or a neighbour's burst on every trial of one side moved a ratio of 1.2 past 1.4.
// A cost that is there is there every time, so a measure within the limit is enough to say it is not.
func requireCost(t *testing.T, what string, measured, reference func(rounds int), limit float64) {
	t.Helper()
	if testing.CoverMode() != "" {
		t.Skip("costs are not compared under coverage counters")
	}
	if underRace {
		t.Skip("costs are not compared under the race detector")
	}
	var ratios []string
	for attempt := 0; attempt < 5; attempt++ {
		ratio := costRatio(measured, reference)
		ratios = append(ratios, strconv.FormatFloat(ratio, 'f', 2, 64))
		if ratio <= limit {
			t.Logf("%s costs %s times its reference (limit %.2f)", what, strings.Join(ratios, ", then "), limit)
			return
		}
	}
	t.Errorf("%s costs %s times its reference, want at most %.2f", what, strings.Join(ratios, ", "), limit)
}
