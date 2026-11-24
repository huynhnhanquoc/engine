package runtime_test

import "testing"

// A closure costs one allocation, its Lambda: it shares the parameters of the function literal it is
// made from and the one runner of its VM. (Each closure copied the parameters and made a runner of its
// own, two allocations more.)
func TestAClosureIsOneAllocation(t *testing.T) {
	for _, c := range []struct {
		name, source string
	}{
		{"() => 1", `g = 0; for (let i = 0; i < 100; i++) { g = () => 1; }`},
		{"(a, b) => a", `g = 0; for (let i = 0; i < 100; i++) { g = (a, b) => a; }`},
		{"a callback made per call", `const rows = [1]; const f = (x) => rows.map((r) => r + x); t = 0; for (let i = 0; i < 100; i++) { t = f(i); }`},
	} {
		program := load(t, c.source)
		// the run's own allocations are a handful, and the callback case makes an array per call too;
		// two or three per closure would be two or three hundred more
		limit := 130.0
		if c.name == "a callback made per call" {
			// per call four: the closure, the result's items and its array, and f's slots made again since
			// the closure holds the old ones (bytecode 7; f's map made again was two, a header and a
			// group); seven with the closure's three
			limit = 420
		}
		if allocations := program.allocationsPerRun(t); allocations > limit {
			t.Errorf("%s: %.0f allocations for 100 closures, want at most %.0f", c.name, allocations, limit)
		} else {
			t.Logf("%s: %.0f allocations for 100 closures", c.name, allocations)
		}
	}
}

// Closures that share their parameters are still functions of their own: each keeps the scope it was
// made in, reads its own arguments, and a parameter it leaves out is undefined.
func TestClosuresSharingParametersKeepTheirOwnScope(t *testing.T) {
	program := load(t, `
const make = (x) => (y, z) => [x, y, typeof z];
const one = make(1);
const two = make(2);
r = JSON.stringify([one(10), two(20, 30), one(11)]) + " " + (one === two) + " " + one.length;
`)
	program.run(t)
	if got, want := program.variable("r").Text(), `[[1,10,"undefined"],[2,20,"number"],[1,11,"undefined"]] false 2`; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
}
