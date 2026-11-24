package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// An array method that calls back once per item (map, filter, find, forEach, some, every, reduce,
// reduceRight, flatMap, findLast, sort's comparator) makes one argument slice per call of the method and
// overwrites it for each item (04/11/2026): until then each callback took a slice of its own, one
// allocation an item. What a method allocates is now the same for 10 items as for 100.

// count is n numbers from 0.
func count(n int) value.Value {
	items := make([]value.Value, n)
	for i := range items {
		items[i] = value.Number(float64(i))
	}
	return value.Array(items)
}

// lambdaCallback is a program's function as the runtime makes one, its Run binding the arguments
// before the body reads them, as the runtime's frame does; the body gives answer.
func lambdaCallback(answer bool) value.Value {
	lambda := &value.Lambda{Name: "callback", Params: []string{"x", "i", "a"}}
	lambda.Run = func(_ *value.Lambda, arguments []value.Value) value.Value {
		bound := [3]value.Value{}
		copy(bound[:], arguments)
		return value.Boolean(answer && bound[0].IsNumber())
	}
	return value.Closure(lambda)
}

func TestAnArrayCallbackMethodAllocatesNothingPerItem(t *testing.T) {
	compare := value.Function(func(arguments ...value.Value) value.Value {
		return arguments[1].Subtract(arguments[0])
	})
	for _, method := range []string{"map", "filter", "find", "findIndex", "findLast", "findLastIndex", "forEach", "some", "every", "reduce", "reduceRight", "flatMap", "sort"} {
		// every item is called back: every goes on while the answer is true, the others while it is false
		answer := method == "every"
		native := value.Function(func(...value.Value) value.Value { return value.Boolean(answer) })
		for name, fn := range map[string]value.Value{"a Go function": native, "a lambda": lambdaCallback(answer)} {
			if method == "sort" {
				fn = compare
			}
			arguments := []value.Value{fn}
			if strings.HasPrefix(method, "reduce") {
				arguments = append(arguments, value.Number(0))
			}
			allocations := func(n int) float64 {
				a := count(n)
				made := testing.AllocsPerRun(20, func() {
					a.Invoke(method, arguments)
				})
				if method == "flatMap" {
					// flat's output grows as it is filled, whoever calls it: only the callbacks are counted
					mapped := a.Map(fn)
					made -= testing.AllocsPerRun(20, func() { mapped.Flat() })
				}
				return made
			}
			few, many := allocations(10), allocations(100)
			if many > few {
				t.Errorf("%s with %s: %.0f allocations for 10 items, %.0f for 100: the cost grows with the items", method, name, few, many)
			}
		}
	}
}

// The argument slice is overwritten for each item, so it must hold that item's arguments, every one
// of them, whatever the callback did to the slice it was handed the time before.
func TestEachCallbackSeesItsOwnArguments(t *testing.T) {
	a := count(4)
	var seen []string
	scribble := value.Function(func(arguments ...value.Value) value.Value {
		seen = append(seen, value.Stringify(value.Array(append([]value.Value(nil), arguments...)), "").Text())
		for i := range arguments {
			arguments[i] = value.String("scribbled") // a callback that writes its arguments over
		}
		return value.Boolean(false)
	})
	a.ForEach(scribble)
	a.Reduce(scribble, value.Number(7))
	want := `[0,0,[0,1,2,3]] [1,1,[0,1,2,3]] [2,2,[0,1,2,3]] [3,3,[0,1,2,3]] ` +
		`[7,0,0,[0,1,2,3]] [false,1,1,[0,1,2,3]] [false,2,2,[0,1,2,3]] [false,3,3,[0,1,2,3]]`
	if got := strings.Join(seen, " "); got != want {
		t.Errorf("the arguments each callback saw:\n got %s\nwant %s", got, want)
	}
}

// As in JavaScript, a method walks the items the array had when it began: one the callback adds is not
// visited, one it removes is skipped by forEach, map, filter, some, every, reduce and reduceRight, and
// read as undefined by find and findLast; each answers with the item it read, not the one there now.
// (A callback that pushed made forEach run on, and map panic past the end of its output.)
func TestACallbackThatChangesTheArray(t *testing.T) {
	calls := 0
	a := count(3)
	grow := value.Function(func(arguments ...value.Value) value.Value {
		calls++
		return a.Push(value.Number(9))
	})
	if got := a.ForEach(grow); got.IsInvalid() || calls != 3 || len(a.Items()) != 6 {
		t.Errorf("forEach that pushes: %d calls, %d items, %s", calls, len(a.Items()), got.Failure())
	}
	b := count(3)
	mapped := b.Map(value.Function(func(arguments ...value.Value) value.Value {
		b.Push(value.Number(9))
		return arguments[0].Multiply(value.Number(2))
	}))
	if got := value.Stringify(mapped, "").Text(); got != "[0,2,4]" {
		t.Errorf("map that pushes: %s (%s)", got, mapped.Failure())
	}
	shrink := func(target *value.Value) value.Value {
		return value.Function(func(arguments ...value.Value) value.Value {
			calls++
			target.Pop()
			return value.Boolean(false)
		})
	}
	c := count(4)
	calls = 0
	if got := c.ForEach(shrink(&c)); got.IsInvalid() || calls != 2 {
		t.Errorf("forEach that pops runs for the items still there: %d calls", calls)
	}
	d := count(4)
	calls = 0
	if got := d.Find(shrink(&d)); !got.IsUndefined() || calls != 4 {
		t.Errorf("find that pops reads every index it began with: %d calls, %s", calls, got.ToString().Text())
	}
	e := count(3)
	found := e.Find(value.Function(func(arguments ...value.Value) value.Value {
		e.Pop()
		e.Pop()
		e.Pop()
		return value.Boolean(true)
	}))
	if found.Number() != 0 || found.IsInvalid() {
		t.Errorf("find gives the item it read, though the array is empty now: %s", found.Failure())
	}
	f := count(4)
	var visited []string
	right := f.ReduceRight(value.Function(func(arguments ...value.Value) value.Value {
		visited = append(visited, arguments[2].ToString().Text())
		f.Splice(value.Number(0), value.Number(2))
		return arguments[0]
	}), value.Number(0))
	if got := strings.Join(visited, ","); got != "3,1" || right.IsInvalid() {
		t.Errorf("reduceRight skips the items a callback removed: visited %s", got)
	}
}
