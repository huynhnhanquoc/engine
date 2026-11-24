package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// TestAFrozenObjectRefusesEveryWrite: Freeze is Object.freeze. Set of a key the object has, or of a
// new one, is a TYPE failure saying which; Delete removes nothing; reads, keys and JSON are as before.
func TestAFrozenObjectRefusesEveryWrite(t *testing.T) {
	o := value.Object().Set("a", value.Number(1)).Set("b", value.String("x"))
	if o.IsFrozen() {
		t.Fatal("an object is frozen before Freeze")
	}
	if got := o.Freeze(); !got.Equal(o) || !o.IsFrozen() {
		t.Fatal("Freeze returns the object itself, frozen")
	}
	if got := o.Set("a", value.Number(2)); got.Code() != value.TypeCode || !strings.Contains(got.Message(), "read only property 'a'") {
		t.Errorf("writing a: %s", got.Failure())
	}
	if got := o.Set("c", value.Number(3)); got.Code() != value.TypeCode || !strings.Contains(got.Message(), "cannot add property c") {
		t.Errorf("adding c: %s", got.Failure())
	}
	if o.Delete("a") {
		t.Error("Delete removed a key of a frozen object")
	}
	if o.Get("a").Number() != 1 || o.Get("c").IsInvalid() || !o.Get("c").IsUndefined() || len(o.Keys()) != 2 {
		t.Errorf("a frozen object reads as before: %s", value.Stringify(o, "").Text())
	}
	if got := value.Stringify(o, "").Text(); got != `{"a":1,"b":"x"}` {
		t.Errorf("JSON of a frozen object: %s", got)
	}
	if again := o.Freeze(); !again.Equal(o) || !o.IsFrozen() {
		t.Error("freezing twice is freezing once")
	}
}

// TestFreezeIsShallow: what a frozen object holds stays as writable as it was, as in JavaScript.
func TestFreezeIsShallow(t *testing.T) {
	inner := value.Object().Set("n", value.Number(1))
	outer := value.Object().Set("inner", inner).Freeze()
	if got := outer.Get("inner").Set("n", value.Number(2)); got.IsInvalid() {
		t.Fatalf("the object a frozen one holds is writable: %s", got.Failure())
	}
	if inner.Get("n").Number() != 2 || inner.IsFrozen() {
		t.Error("Freeze reached into what the object holds")
	}
}

// TestAFrozenFunctionRefusesKeys: a function frozen before it ever held a key refuses one too.
func TestAFrozenFunctionRefusesKeys(t *testing.T) {
	plain := value.Function(func(...value.Value) value.Value { return value.Number(7) }).Freeze()
	if !plain.IsFrozen() {
		t.Fatal("a function is frozen after Freeze")
	}
	if got := plain.Set("x", value.Number(1)); got.Code() != value.TypeCode {
		t.Errorf("a key on a frozen function: %s", got.Failure())
	}
	if got := plain.Call(nil); got.Number() != 7 {
		t.Errorf("a frozen function still runs: %s", got.ToString().Text())
	}
	named := value.Function(func(...value.Value) value.Value { return value.Undefined() }).Set("length", value.Number(2)).Freeze()
	if named.Get("length").Number() != 2 || named.Set("length", value.Number(3)).Code() != value.TypeCode {
		t.Error("a frozen function keeps the keys it had and refuses new values for them")
	}
}

// TestIsFrozenOfEveryKind follows Object.isFrozen: a value with no keys to write is frozen; an object,
// a function or an array that can still be written is not, and is once frozen. (Until 04/11/2026 an
// array's items had no place for the mark, and freezing one was an UNSUPPORTED failure.)
func TestIsFrozenOfEveryKind(t *testing.T) {
	for _, v := range []value.Value{
		value.Undefined(), value.Null(), value.Number(1), value.Boolean(false), value.String("s"),
	} {
		if !v.IsFrozen() {
			t.Errorf("%s is frozen, as Object.isFrozen says", v.Typeof().Text())
		}
		if got := v.Freeze(); !got.Equal(v) && !(v.IsUndefined() && got.IsUndefined()) {
			t.Errorf("Freeze of %s returns it", v.Typeof().Text())
		}
	}
	for _, v := range []value.Value{value.Object(), value.Array(nil), value.Function(func(...value.Value) value.Value { return value.Undefined() })} {
		if v.IsFrozen() {
			t.Errorf("a new value of kind %s is not frozen", v.Kind())
		}
		if got := v.Freeze(); !got.Equal(v) || !v.IsFrozen() {
			t.Errorf("Freeze of a value of kind %s returns it, frozen: %s", v.Kind(), got.Failure())
		}
	}
	failure := value.Fail(value.TypeCode, "x")
	if !failure.Freeze().IsInvalid() {
		t.Error("Freeze passes a failure through")
	}
}

// numbers is an array of the given numbers.
func numbers(items ...float64) value.Value {
	values := make([]value.Value, len(items))
	for i, item := range items {
		values[i] = value.Number(item)
	}
	return value.Array(values)
}

// TestAFrozenArrayRefusesEveryWrite: Object.freeze([2, 1]) refuses each writer of an array with a TYPE
// failure worded as JavaScript words it for an object (a write to an item it has, an item it would add,
// an item it would remove, or the length alone), and keeps its items; reads are as before.
func TestAFrozenArrayRefusesEveryWrite(t *testing.T) {
	a := numbers(2, 1)
	if got := a.Freeze(); !got.Equal(a) || !a.IsFrozen() {
		t.Fatal("Freeze returns the array itself, frozen")
	}
	compare := value.Function(func(arguments ...value.Value) value.Value {
		return arguments[0].Subtract(arguments[1])
	})
	for _, c := range []struct {
		write   string
		got     value.Value
		message string
	}{
		{"push(3)", a.Push(value.Number(3)), "cannot add property 2, object is not extensible"},
		{"push()", a.Push(), "cannot assign to read only property 'length' of object '[object Array]'"},
		{"pop()", a.Pop(), "cannot delete property '1' of [object Array]"},
		{"shift()", a.Shift(), "cannot assign to read only property '0' of object '[object Array]'"},
		{"unshift(0)", a.Unshift(value.Number(0)), "cannot add property 2, object is not extensible"},
		{"splice(0, 1)", a.Splice(value.Number(0), value.Number(1)), "cannot assign to read only property '0' of object '[object Array]'"},
		{"splice(0, 0)", a.Splice(value.Number(0), value.Number(0)), "cannot assign to read only property 'length' of object '[object Array]'"},
		{"fill(0)", a.Fill(value.Number(0)), "cannot assign to read only property '0' of object '[object Array]'"},
		{"sort()", a.Sort(), "cannot assign to read only property '0' of object '[object Array]'"},
		{"sort(compare)", a.Invoke("sort", []value.Value{compare}), "cannot assign to read only property '0' of object '[object Array]'"},
		{"reverse()", a.Reverse(), "cannot assign to read only property '0' of object '[object Array]'"},
		{"copyWithin(0, 1)", a.CopyWithin(value.Number(0), value.Number(1)), "cannot assign to read only property '0' of object '[object Array]'"},
		{"a[0] = 9", a.SetIndex(0, value.Number(9)), "cannot assign to read only property '0' of object '[object Array]'"},
		{"a[5] = 9", a.SetIndex(5, value.Number(9)), "cannot add property 5, object is not extensible"},
	} {
		if c.got.Code() != value.TypeCode || c.got.Message() != c.message {
			t.Errorf("%s on a frozen array: %s, want TYPE: %s", c.write, c.got.Failure(), c.message)
		}
	}
	if got := value.Stringify(a, "").Text(); got != "[2,1]" || a.Get("length").Number() != 2 || a.Index(0).Number() != 2 {
		t.Errorf("a frozen array keeps its items and reads as before: %s", got)
	}
	if got := a.Map(compare); !got.IsArray() || got.IsFrozen() {
		t.Errorf("map of a frozen array is a new array, not frozen: %s", got.Failure())
	}
	if !a.Slice().IsArray() || a.Slice().IsFrozen() || !a.ToSorted().IsArray() || a.ToSorted().IsFrozen() {
		t.Error("what makes a new array from a frozen one makes it writable")
	}
}

// TestAFrozenArrayAllowsWhatWritesNothing: as in JavaScript, a writer that would write no item is not
// refused (fill past the end; sort, reverse and copyWithin of an empty array or one item), and Freeze is
// shallow: an array a frozen one holds stays writable.
func TestAFrozenArrayAllowsWhatWritesNothing(t *testing.T) {
	a := numbers(2, 1).Freeze()
	if got := a.Fill(value.Number(0), value.Number(5)); !got.Equal(a) {
		t.Errorf("fill(0, 5) writes nothing: %s", got.Failure())
	}
	for _, frozen := range []value.Value{numbers().Freeze(), numbers(7).Freeze()} {
		for name, got := range map[string]value.Value{
			"sort": frozen.Sort(), "reverse": frozen.Reverse(), "fill(1, 1)": frozen.Fill(value.Number(1), value.Number(1)),
			"copyWithin(0, 1)": frozen.CopyWithin(value.Number(0), value.Number(1)),
		} {
			if got.IsInvalid() {
				t.Errorf("%s of a frozen array of %d writes nothing: %s", name, len(frozen.Items()), got.Failure())
			}
		}
	}
	empty := numbers().Freeze()
	if got := empty.Pop(); got.Message() != "cannot assign to read only property 'length' of object '[object Array]'" {
		t.Errorf("pop of a frozen empty array sets its length: %s", got.Failure())
	}
	// each as node words it (checked against node 22 for 300 writer calls on frozen arrays of 0 to 3 items)
	three := numbers(0, 1, 2).Freeze()
	for _, c := range []struct {
		write   string
		got     value.Value
		message string
	}{
		{"[].unshift(9, 9)", empty.Unshift(value.Number(9), value.Number(9)), "cannot add property 0, object is not extensible"},
		{"[0, 1, 2].unshift(9, 9)", three.Unshift(value.Number(9), value.Number(9)), "cannot add property 4, object is not extensible"},
		{"[0, 1, 2].splice(1, 2)", three.Splice(value.Number(1), value.Number(2)), "cannot delete property '2' of [object Array]"},
		{"[0, 1, 2].splice(0, 1, 9, 9)", three.Splice(value.Number(0), value.Number(1), value.Number(9), value.Number(9)), "cannot add property 3, object is not extensible"},
		{"[0, 1, 2].splice(3, 0, 9)", three.Splice(value.Number(3), value.Number(0), value.Number(9)), "cannot add property 3, object is not extensible"},
		{"[0].shift()", numbers(0).Freeze().Shift(), "cannot delete property '0' of [object Array]"},
	} {
		if c.got.Message() != c.message {
			t.Errorf("%s on a frozen array: %s, want %q", c.write, c.got.Failure(), c.message)
		}
	}
	inner := numbers(1)
	outer := value.Array([]value.Value{inner}).Freeze()
	if got := outer.Index(0).Push(value.Number(2)); got.IsInvalid() || len(inner.Items()) != 2 || inner.IsFrozen() {
		t.Errorf("an array a frozen one holds is writable: %s", got.Failure())
	}
}
