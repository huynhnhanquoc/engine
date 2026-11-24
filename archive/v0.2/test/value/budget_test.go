package value_test

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// unlimited is the budget of a run without a ceiling of bytes: room for anything, the longest string the one
// bound left.
type unlimited struct{}

func (unlimited) Room(uint64) bool { return true }
func (unlimited) Exceeded() value.Value {
	return value.Fail(value.RangeCode, "an unlimited budget refused")
}
func (unlimited) Spend(uint64) value.Value { return value.Value{} }

// ceiling is a run's budget as a test sees it: room for bytes in all, its failure past them, and how often it
// was asked.
type ceiling struct {
	bytes uint64
	asked int
}

func (c *ceiling) Room(n uint64) bool { c.asked++; return n <= c.bytes }
func (c *ceiling) Exceeded() value.Value {
	return value.Fail(value.RangeCode, "past the test's ceiling")
}
func (c *ceiling) Spend(uint64) value.Value { return value.Value{} }

// sharing is an array holding the text s count times: little held, much made once written out.
func sharing(s string, count int) value.Value {
	items := make([]value.Value, count)
	for i := range items {
		items[i] = value.String(s)
	}
	return value.Array(items)
}

// Each builder that makes much from little asks the run's budget as what it makes grows, and stops at the
// budget's failure (14/11/2026): an array holding a text of 100 KB a thousand times is 100 MB once joined, made
// from 124 KB held. Under a budget of 1 MiB each stops having allocated a few MB; given room, each makes what
// it made before.
func TestABuilderStopsAtItsRunsBudget(t *testing.T) {
	text := strings.Repeat("x", 100_000)
	shared := sharing(text, 1000)
	nested := value.Array([]value.Value{shared, shared, shared, shared})
	tree := "[" + strings.TrimSuffix(strings.Repeat("[],", 400_000), ",") + "]"
	numbers := make([]value.Value, 10_000)
	for i := range numbers {
		numbers[i] = value.Number(float64(i))
	}
	arrays := make([]value.Value, 1000)
	for i := range arrays {
		arrays[i] = value.Array(numbers)
	}
	many := value.Array(arrays) // ten million items once flat, 240 MB, from 264 KB held
	for name, build := range map[string]func(value.Budget) value.Value{
		"join":               func(b value.Budget) value.Value { return shared.InvokeWithin(b, "join", nil) },
		"a nested join":      func(b value.Budget) value.Value { return nested.InvokeWithin(b, "join", nil) },
		"toString":           func(b value.Budget) value.Value { return shared.InvokeWithin(b, "toString", nil) },
		"an array made text": func(b value.Budget) value.Value { return shared.TextWithin(b) },
		"an argument made text": func(b value.Budget) value.Value {
			return value.String("y").InvokeWithin(b, "concat", []value.Value{shared})
		},
		"JSON.stringify": func(b value.Budget) value.Value { return value.StringifyWithin(shared, "", b) },
		"flat": func(b value.Budget) value.Value {
			return many.InvokeWithin(b, "flat", nil)
		},
		"JSON.parse": func(b value.Budget) value.Value { return value.ParseJSONWithin(tree, b) },
	} {
		t.Run(name, func(t *testing.T) {
			budget := &ceiling{bytes: 1 << 20}
			goruntime.GC()
			var before, after goruntime.MemStats
			goruntime.ReadMemStats(&before)
			got := build(budget)
			goruntime.ReadMemStats(&after)
			if got.Message() != "past the test's ceiling" {
				t.Fatalf("%s, want the budget's failure", got.Failure())
			}
			if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
				t.Errorf("allocated %d MB before it stopped, want it stopped as it grew", grown>>20)
			}
			if budget.asked > 12 {
				t.Errorf("asked the budget %d times, want few: each ask is twice the last", budget.asked)
			}
			if room := build(unlimited{}); room.IsInvalid() {
				t.Errorf("with room: %s", room.Failure())
			}
		})
	}
}

// Near its ceiling a run is asked a few times, not at each item: a limit refused when it asked ahead asks halfway
// between what it needs and what was refused. Each ask past what fits counts the run again by what it holds, so an
// ask at each item made a builder of many small items cost as many counts. Fifty thousand texts of ten bytes joined
// under a budget of 400 KB.
func TestNearItsCeilingARunIsAskedAFewTimes(t *testing.T) {
	small := sharing("0123456789", 50_000)
	budget := &ceiling{bytes: 400 << 10}
	if got := small.InvokeWithin(budget, "join", []value.Value{value.String("")}); got.Message() != "past the test's ceiling" {
		t.Fatalf("%s, want the budget's failure", got.Failure())
	}
	if budget.asked > 48 {
		t.Errorf("asked the budget %d times for 40,000 items past its first refusal, want a few", budget.asked)
	}
}

// A builder no run calls makes at most value.UnbudgetedLength, 64 MiB (a module writing a value as JSON, a
// method read off a value and called later): where JavaScript allows eight times that, one call made 2.6 GB from
// a value a run held as 64 KB. Its failure says so; under it, what it made before. What it allocates is a few
// times 64 MiB, as Go grows a large slice by a quarter, each smaller copy let go.
func TestABuilderNoRunCallsStopsAtSixtyFourMiB(t *testing.T) {
	shared := sharing(strings.Repeat("x", 100_000), 1000)
	for name, build := range map[string]func() value.Value{
		"JSON.stringify": func() value.Value { return value.Stringify(shared, "") },
		"join":           func() value.Value { return shared.Join() },
		"String(array)":  func() value.Value { return shared.ToString() },
	} {
		goruntime.GC()
		var before, after goruntime.MemStats
		goruntime.ReadMemStats(&before)
		got := build()
		goruntime.ReadMemStats(&after)
		if got.Code() != value.RangeCode || !strings.Contains(got.Message(), "64 MiB made where no run counts it") {
			t.Errorf("%s: %s, want RANGE past 64 MiB", name, got.Failure())
		}
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 6*value.UnbudgetedLength {
			t.Errorf("%s: allocated %d MB before it stopped, want a few times 64 MiB", name, grown>>20)
		}
	}
	if got := value.Stringify(sharing(strings.Repeat("x", 100_000), 600), ""); len(got.Text()) != 600*100_002+601 {
		t.Errorf("JSON.stringify of 60 MB: %s of %d bytes", got.Failure(), len(got.Text()))
	}
}
