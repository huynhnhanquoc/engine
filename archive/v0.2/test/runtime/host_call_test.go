package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// Tally is a host struct for the tests: its exposed method tally.add(…) counts its arguments.
type Tally struct{ seen int }

func (tally *Tally) Add(arguments ...value.Value) value.Value {
	tally.seen += len(arguments)
	return value.Number(float64(tally.seen))
}

// A call into the host takes its arguments as a window on the stack, so it allocates nothing per call:
// a native function called (parseInt), one an object or a function holds (Math.max, Number.isInteger),
// a struct's method. (Each made an argument slice, one allocation a call.)
func TestAHostCallAllocatesNoArgumentSlice(t *testing.T) {
	for _, c := range []struct {
		name, source, check, want string
	}{
		{"parseInt(s)", `t = 0; for (let i = 0; i < 100; i++) { t = parseInt("42"); }`, "t", "42"},
		{"Math.max(a, b)", `t = 0; for (let i = 0; i < 100; i++) { t = Math.max(t, i); }`, "t", "99"},
		{"Number.isInteger(i)", `t = false; for (let i = 0; i < 100; i++) { t = Number.isInteger(i); }`, "t", "true"},
		{"tally.add(a, b)", `t = 0; for (let i = 0; i < 100; i++) { t = tally.add(i, 1); }`, "t", "200"},
	} {
		program := load(t, c.source)
		tally := &Tally{}
		program.globals["tally"] = value.Struct(tally)
		program.run(t)
		if got := program.variable(c.check).ToString().Text(); got != c.want {
			t.Errorf("%s: %s = %q, want %q", c.name, c.check, got, c.want)
		}
		if allocations := program.allocationsPerRun(t); allocations > 20 {
			t.Errorf("%s: %.0f allocations for 100 calls, want at most 20", c.name, allocations)
		}
	}
}

// The window is the host's to read while it runs, nothing more: a host that appends to it writes a
// copy, a lambda it runs pushes above it, and what it is handed is what the program passed, a failure
// among them coming back as the call's failure.
func TestAHostReadsItsArgumentsAndLetsGo(t *testing.T) {
	program := load(t, `r = [grow(1, 2), 7, [3].map((x) => grow(x, x)), safe(() => grow(5, 6)).value, safe(() => grow(error("no"))).error === null].join(",");`)
	program.globals["grow"] = value.Function(func(arguments ...value.Value) value.Value {
		arguments = append(arguments, value.Number(100))
		total := 0.0
		for _, argument := range arguments {
			total += argument.Number()
		}
		return value.Number(total)
	})
	program.run(t)
	if got, want := program.variable("r").Text(), "103,7,106,111,false"; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
}

// Meter is a host struct with a property and a method, for what a program reads of each (04/11/2026):
// meter.total, a method of no parameter, is read without (); meter.add, read without a call, is the
// method bound to the meter, called later.
type Meter struct{ total int }

func (meter *Meter) Total() value.Value { return value.Number(float64(meter.total)) }

func (meter *Meter) Add(arguments ...value.Value) value.Value {
	for _, argument := range arguments {
		meter.total += int(argument.Number())
	}
	return value.Number(float64(meter.total))
}

// A program reads a struct's method as a function and calls it when it says so: `const add =
// meter.add` runs nothing (it ran add() until 04/11/2026), and the property is not a function.
func TestAProgramReadsAStructMethodWithoutCallingIt(t *testing.T) {
	program := load(t, `const add = meter.add; before = meter.total; r = add(2, 3); after = meter.total;
same = meter.add === meter.add; kind = typeof meter.add; notCalled = safe(() => meter.total()).error.code;`)
	meter := &Meter{}
	program.globals["meter"] = value.Struct(meter)
	program.run(t)
	for name, want := range map[string]string{
		"before": "0", "r": "5", "after": "5", "same": "true", "kind": "function", "notCalled": "TYPE",
	} {
		if got := program.variable(name).ToString().Text(); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// A write to a struct is the TYPE failure a write through a value that holds no property is (05/11/2026):
// until then it was dropped and the run went on as if it had written, so meter.total read the struct's
// own total after meter.total = 5, and safe(() => { meter.total = 7 }) was ok. JavaScript throws there
// for a getter without a setter; a struct holds no key a program sets.
func TestAWriteToAStructIsATypeFailure(t *testing.T) {
	program := load(t, `const write = (name) => safe(() => { meter[name] = 5; }).error;
total = write("total"); add = write("add"); extra = write("extra");
after = [meter.total, typeof meter.add, meter.extra].join(",");`)
	program.globals["meter"] = value.Struct(&Meter{total: 42})
	program.run(t)
	for _, name := range []string{"total", "add", "extra"} {
		failure := program.variable(name)
		if !failure.IsObject() || failure.Get("code").Text() != "TYPE" {
			t.Errorf("meter.%s = 5 gives %s, want a TYPE failure", name, value.Stringify(failure, "").Text())
		}
	}
	if got := program.variable("after").Text(); got != "42,function," {
		t.Errorf("after the refused writes meter reads %q, want 42,function,", got)
	}
}
