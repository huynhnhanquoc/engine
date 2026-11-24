package runtime_test

import "testing"

// A method call on an object looks its own key up once and allocates nothing per call: a builtin of the
// object (hasOwnProperty, toString) through a window on the stack, a lambda the object holds as a frame
// bound from the stack. (A builtin's call built two bound functions the result threw away, 5 allocations
// a call; a held lambda's call made an argument slice and ran nested.)
func TestAMethodCallOnAnObjectAllocatesNothingPerCall(t *testing.T) {
	for _, c := range []struct {
		name, source, check, want string
	}{
		{"o.hasOwnProperty(k)", `const o = { x: 1 }; t = false; for (let i = 0; i < 100; i++) { t = o.hasOwnProperty("x"); }`, "t", "true"},
		{"o.toString()", `const o = { x: 1 }; t = ""; for (let i = 0; i < 100; i++) { t = o.toString(); }`, "t", "[object Object]"},
		{"o.f(i), f a lambda", `const o = { f: (x) => x + 1 }; t = 0; for (let i = 0; i < 100; i++) { t = o.f(i); }`, "t", "100"},
	} {
		program := load(t, c.source)
		program.run(t)
		if got := program.variable(c.check).ToString().Text(); got != c.want {
			t.Errorf("%s: %s = %q, want %q", c.name, c.check, got, c.want)
		}
		// the run's own allocations (the root map a top-level closure keeps, the object) are a handful;
		// one per call would be a hundred more
		if allocations := program.allocationsPerRun(t); allocations > 20 {
			t.Errorf("%s: %.0f allocations for 100 calls, want at most 20", c.name, allocations)
		}
	}
}

// What a held lambda's call does as a frame is what it did nested: its failure, its writes, a parameter
// left out, recursion through the object, and the stack overflow at the same place.
func TestALambdaHeldByAnObjectRunsAsACall(t *testing.T) {
	for _, c := range []struct {
		source, want string
	}{
		{`const o = { f: (x) => x * 2 }; r = o.f(21);`, "42"},
		{`const o = { f: (a, b) => typeof b }; r = o.f(1);`, "undefined"},
		{`const o = { f: (n) => n < 1 ? 0 : 1 + o.f(n - 1) }; r = o.f(50);`, "50"},
		{`const o = { f: (x) => error("boom " + x) }; const e = safe(() => o.f(1)).error; r = e.at + ": " + e.message;`, "<source>:1:23: boom 1"},
		{`const o = { f: () => { o.d = 1; return 2; }, d: 0 }; r = o.f() + o.d;`, "3"},
		{`const o = { f: (x) => ({ g: (y) => x + y }) }; r = o.f(1).g(2);`, "3"},
		{`const o = { m: Math.max }; r = o.m(1, 9, 3);`, "9"},
		{`const o = { a: 1, f: 3 }; const e = safe(() => o.f()).error; let r = e.at + ": " + e.message;`, "<source>:1:49: f is not a function"},
	} {
		program := load(t, c.source)
		program.run(t)
		if got := program.variable("r").ToString().Text(); got != c.want {
			t.Errorf("%s: r = %q, want %q", c.source, got, c.want)
		}
	}
	overflow := load(t, `const o = { f: (n) => 1 + o.f(n + 1) }; r = o.f(0);`)
	overflow.vm.FastReset(overflow.program, overflow.globals)
	if result := overflow.vm.Run(); !result.IsInvalid() || result.Headline() != "<source>:1:28: Stack overflow: Call stack limit exceeded" {
		t.Errorf("unbounded recursion through o.f: %s %q, want the stack overflow at 1:28", result.Kind(), result.Headline())
	}
}
