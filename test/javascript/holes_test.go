package javascript_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Lỗ của Array(n): đọc ra như JavaScript (đối chiếu với node), và thứ bỏ qua lỗ là lỗi cho tới khi mảng được điền kín.

func TestHolesReadAsJavaScripts(t *testing.T) {
	compare(t, []string{
		`Array(3).fill(0)`, `Array(3).fill(7, 1)`, `Array(3).fill(7, 0, 3)`, `Array(2).fill(1, -1)`, `Array(2).fill(undefined)`,
		`Array(2).join("-")`, `Array(3).slice(1)`, `Array(2).concat([1], Array(1))`, `Array(3).includes(undefined)`,
		`Array(3).find(identity)`, `Array(3).findLast(identity)`, `Array(3).findIndex(identity)`, `Array(3).at(0)`,
		`Array(2).with(0, 1)`, `Array(2).toSorted()`, `Array(2).toReversed()`, `Array(3).toSpliced(0, 1)`, `Array(3).reverse()`,
		`Array(3).push(1)`, `Array(2).pop()`, `Array(2).shift()`, `Array(2).unshift(1)`, `Array(2).sort()`, `Array(3).sort(ascending)`,
		`Array(3).splice(1, 1)`, `Array(3).copyWithin(0, 1)`, `Array(2).toString()`, `Array(0).map(double)`, `Array(0).every(isEven)`,
		`Array(1.5).fill(0)`, `Array(-1).fill(0)`, `Array(4294967296).fill(0)`, `Array("2").map(double)`, `Array(1, 2).map(double)`,
		`JSON.stringify(Array(2))`, `JSON.stringify([1, Array(1)])`, `Array.from(Array(2))`, `Array.from(Array(2), index)`,
		`Array.isArray(Array(2))`, `Math.max.apply(null, Array(2))`, `Array.call(null, 3)`, `[Array(2)].flat(0)`,
		`String.call(null, Array(3))`, `Array.of(Array(1))`,
	})
}

func TestHolesRefuseWhatPassesOverThem(t *testing.T) {
	global := func(name string) value.Value {
		g, _ := javascript.Global(name)
		return g
	}
	array, object, json := global("Array"), global("Object"), global("JSON")
	holes := func(n int) value.Value {
		return javascript.Invoke(nil, array, "call", []value.Value{value.Null(), value.Number(float64(n))})
	}
	text := func(v value.Value) string {
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	double, sum := value.Function(callbacks["double"]), value.Function(callbacks["sum"])
	const refusal = ": the array Array(n) made still has holes, which JavaScript passes over here and a Kitwork array does not have: " +
		"fill it first, Array(n).fill(x), or write Array.from({ length: n }, (_, i) => …)"

	// mỗi phương thức bỏ qua lỗ: JavaScript cho kết quả khác trên undefined, nên là lỗi
	for name, arguments := range map[string][]value.Value{
		"map": {double}, "forEach": {double}, "filter": {double}, "some": {double}, "every": {double}, "flatMap": {double},
		"reduce": {sum}, "reduceRight": {sum}, "indexOf": {value.Undefined()}, "lastIndexOf": {value.Undefined()}, "flat": nil,
		"hasOwnProperty": {value.String("0")},
	} {
		if got := text(javascript.Invoke(run{}, holes(2), name, arguments)); got != "error: a."+name+"(…)"+refusal {
			t.Errorf("%s: %s", name, got)
		}
	}

	filled := holes(3)
	javascript.Invoke(nil, filled, "fill", []value.Value{value.Number(1)})
	part := holes(3)
	javascript.Invoke(nil, part, "fill", []value.Value{value.Number(1), value.Number(1)})
	written := holes(1)
	written.SetIndex(0, value.Number(5)) // a[0] = 5: dấu còn, như engine2
	nested := value.Array(value.Number(1), holes(2))
	giveHoles := value.Function(&callback{fn: func(javascript.Run, []value.Value) value.Value { return holes(2) }})
	dropped := javascript.Invoke(run{}, json, "parse", []value.Value{value.String("[1,2]"), value.Function(callbacks["dropNumbers"])})
	kept := javascript.Invoke(run{}, json, "parse", []value.Value{value.String("[1,2]"), value.Function(callbacks["doubleNumbers"])})

	cases := []struct {
		name, got, want string
	}{
		{"fill whole, then map", text(javascript.Invoke(run{}, filled, "map", []value.Value{double})), "[n:4000000000000000,n:4000000000000000,n:4000000000000000]"},
		{"fill part, then map", text(javascript.Invoke(run{}, part, "map", []value.Value{double})), "error: a.map(…)" + refusal},
		{"a[0] = 5 keeps the mark", text(javascript.Invoke(run{}, written, "map", []value.Value{double})), "error: a.map(…)" + refusal},
		{"flat of a hole inside", text(javascript.Invoke(nil, nested, "flat", nil)), "error: a.flat(…)" + refusal},
		{"flat 0 keeps it whole", text(javascript.Invoke(nil, nested, "flat", []value.Value{value.Number(0)})), "[n:3ff0000000000000,[undefined,undefined]]"},
		{"flatMap giving holes", text(javascript.Invoke(run{}, value.Array(value.Number(1)), "flatMap", []value.Value{giveHoles})), "error: a.flatMap(…)" + refusal},
		{"Object.keys", text(javascript.Invoke(nil, object, "keys", []value.Value{holes(2)})), "error: Object.keys(a), values, entries, assign" + refusal},
		{"Object.entries", text(javascript.Invoke(nil, object, "entries", []value.Value{holes(2)})), "error: Object.keys(a), values, entries, assign" + refusal},
		{"Object.assign from", text(javascript.Invoke(nil, object, "assign", []value.Value{value.Object(), holes(2)})),
			"error: Object.keys(a), values, entries, assign" + refusal},
		{"Object.hasOwn", text(javascript.Invoke(nil, object, "hasOwn", []value.Value{holes(2), value.String("0")})), "error: Object.hasOwn(a, i)" + refusal},
		{"a reviver's undefined is a hole", text(javascript.Invoke(run{}, dropped, "map", []value.Value{double})), "error: a.map(…)" + refusal},
		{"a reviver that keeps all", text(javascript.Invoke(run{}, kept, "map", []value.Value{double})), "[n:4010000000000000,n:4020000000000000]"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, c.got, c.want)
		}
	}

	// slice, concat, splice giữ lỗ như JavaScript; thứ đọc lỗ ra undefined dựng một mảng liền
	for name, made := range map[string]value.Value{
		"slice":  javascript.Invoke(nil, holes(3), "slice", []value.Value{value.Number(1)}),
		"concat": javascript.Invoke(nil, value.Array(), "concat", []value.Value{holes(1)}),
		"splice": javascript.Invoke(nil, holes(3), "splice", []value.Value{value.Number(0), value.Number(2)}),
	} {
		if !made.HasHoles() {
			t.Errorf("%s must keep the holes", name)
		}
	}
	for name, made := range map[string]value.Value{
		"toSpliced":  javascript.Invoke(nil, holes(3), "toSpliced", []value.Value{value.Number(0), value.Number(1)}),
		"with":       javascript.Invoke(nil, holes(2), "with", []value.Value{value.Number(0), value.Number(1)}),
		"toSorted":   javascript.Invoke(nil, holes(2), "toSorted", nil),
		"toReversed": javascript.Invoke(nil, holes(2), "toReversed", nil),
		"Array.from": javascript.Invoke(nil, array, "from", []value.Value{holes(2)}),
		"empty":      holes(0),
		"of items":   javascript.Invoke(nil, array, "call", []value.Value{value.Null(), value.Number(1), value.Number(2)}),
	} {
		if made.IsInvalid() || made.HasHoles() {
			t.Errorf("%s must be a whole array: %s", name, text(made))
		}
	}
}
