package javascript_test

import (
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Object.freeze và mảng, object đã đóng băng, đối chiếu với node: push, pop, shift, unshift, splice luôn ném (chúng ghi
// length); reverse, sort, fill, copyWithin ném khi ghi một phần tử, kể cả khi thứ tự không đổi; đọc và bản chép thì
// được.
func TestFreezeIsJavaScripts(t *testing.T) {
	compare(t, []string{
		`Object.freeze([]).push()`, `Object.freeze([1]).push()`, `Object.freeze([1]).push(2)`, `Object.freeze([]).pop()`,
		`Object.freeze([1]).pop()`, `Object.freeze([]).shift()`, `Object.freeze([1]).shift()`, `Object.freeze([]).unshift()`,
		`Object.freeze([1]).unshift(0)`, `Object.freeze([]).splice()`, `Object.freeze([1]).splice()`, `Object.freeze([1]).splice(0, 0)`,
		`Object.freeze([1]).splice(5)`, `Object.freeze([1, 2]).splice(0, 1, 9)`,
		`Object.freeze([]).reverse()`, `Object.freeze([1]).reverse()`, `Object.freeze([1, 1]).reverse()`, `Object.freeze([]).sort()`,
		`Object.freeze([1]).sort()`, `Object.freeze([1, 2]).sort()`, `Object.freeze([2, 1]).sort(ascending)`, `Object.freeze([1]).sort(boom)`,
		`Object.freeze([1]).sort(5)`, `Object.freeze([]).fill(0)`, `Object.freeze([1]).fill(1)`, `Object.freeze([1]).fill(0, 1)`,
		`Object.freeze([1, 2]).copyWithin(0, 0)`, `Object.freeze([1, 2]).copyWithin(0, 2)`, `Object.freeze([1, 2]).copyWithin(0, 1)`,
		`Object.freeze([3, 1]).toSorted()`, `Object.freeze([3, 1]).toReversed()`, `Object.freeze([3, 1]).toSpliced(0, 1)`,
		`Object.freeze([3, 1]).with(0, 2)`, `Object.freeze([3, 1]).slice(1)`, `Object.freeze([3, 1]).concat([3])`,
		`Object.freeze([3, 1]).map(double)`, `Object.freeze([3, 1]).filter(isBig)`, `Object.freeze([3, 1]).join("-")`,
		`Object.freeze([3, 1]).at(-1)`, `Object.freeze([3, 1]).includes(1)`, `Object.freeze([3, [1]]).flat()`,
		`Object.freeze(Array(2)).fill(0)`, `Object.freeze(Array(2)).join("-")`, `Object.freeze([1]).toString()`,
		`Object.freeze({a: 1})`, `Object.freeze([1, 2])`, `Object.freeze(5)`, `Object.freeze("x")`, `Object.freeze(null)`, `Object.freeze()`,
		`Object.freeze(date(0))`, `Object.freeze(double)`,
		`Object.isFrozen({})`, `Object.isFrozen([])`, `Object.isFrozen(Object.freeze({}))`, `Object.isFrozen(Object.freeze([1]))`,
		`Object.isFrozen(1)`, `Object.isFrozen("x")`, `Object.isFrozen(null)`, `Object.isFrozen()`, `Object.isFrozen(true)`,
		`Object.isFrozen(Object.freeze(Array(2)))`, `Object.isFrozen(Object.assign({}, Object.freeze({a: 1})))`,
		`Object.assign(Object.freeze({a: 1}))`, `Object.assign(Object.freeze({a: 1}), {})`, `Object.assign(Object.freeze({a: 1}), {a: 1})`,
		`Object.assign(Object.freeze({a: 1}), {b: 2})`, `Object.assign(Object.freeze({a: 1}), null)`, `Object.assign({}, Object.freeze({a: 1}))`,
		`Object.keys(Object.freeze({b: 1, a: 2, 1: 3}))`, `JSON.stringify(Object.freeze({a: [1]}))`,
		`[{}, Object.freeze({})].map(Object.isFrozen)`, `[[1]].map(Object.freeze)`,
	})
}

// Chỗ Kitwork khác và thứ node không thử được: đóng băng nông, lời báo, Object.isFrozen của thứ Kitwork không ghi khoá
// lên, bytes.
func TestFreezeIsKitworks(t *testing.T) {
	object, _ := javascript.Global("Object")
	math, _ := javascript.Global("Math")
	text := func(v value.Value) string {
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	call := func(name string, arguments ...value.Value) string {
		return text(javascript.Invoke(run{}, object, name, arguments))
	}
	inner := value.Object()
	outer := value.Object().Set("inner", inner).Freeze()
	frozenArray := value.Array(value.Number(1)).Freeze()
	cases := []struct{ name, got, want string }{
		{"shallow: what is inside stays open", text(inner.Set("a", value.Number(1))), `{"a":n:3ff0000000000000}`},
		{"the frozen holder refuses", text(outer.Set("inner", value.Null())), `error: cannot assign to read only property "inner" of a frozen object`},
		{"assign onto a frozen object", call("assign", outer, value.Object().Set("b", value.Number(2))),
			`error: cannot add property "b", object is not extensible`},
		{"push onto a frozen array", text(javascript.Invoke(nil, frozenArray, "push", []value.Value{value.Number(2)})),
			`error: cannot add property "1", object is not extensible`},
		{"pop of a frozen array", text(javascript.Invoke(nil, value.Array().Freeze(), "pop", nil)),
			`error: cannot assign to read only property "length" of a frozen array`},
		{"sort of a frozen array", text(javascript.Invoke(nil, value.Array(value.Number(2), value.Number(1)).Freeze(), "sort", nil)),
			`error: cannot assign to read only property "0" of a frozen array`},
		{"fill of a frozen array", text(javascript.Invoke(nil, frozenArray, "fill", []value.Value{value.Number(0)})),
			`error: cannot assign to read only property "0" of a frozen array`},
		{"Object.freeze(Math) changes nothing", call("freeze", math), "?object"},
		{"bytes with elements", call("freeze", value.Bytes([]byte{1})), "error: cannot freeze array buffer views with elements"},
		{"empty bytes", call("freeze", value.Bytes(nil)), "?object"},
		{"a time", call("isFrozen", value.Time(time.UnixMilli(0))),
			"error: Object.isFrozen: Kitwork never writes keys on a time and JavaScript leaves it open, so neither answer is true of both: ask it of an object or an array"},
		{"a duration", call("isFrozen", value.Duration(time.Second)),
			"error: Object.isFrozen: Kitwork never writes keys on a duration and JavaScript leaves it open, so neither answer is true of both: ask it of an object or an array"},
		{"bytes", call("isFrozen", value.Bytes(nil)),
			"error: Object.isFrozen: Kitwork never writes keys on bytes and JavaScript leaves it open, so neither answer is true of both: ask it of an object or an array"},
		{"a function", call("isFrozen", value.Function(callbacks["double"])),
			"error: Object.isFrozen: Kitwork never writes keys on a function and JavaScript leaves it open, so neither answer is true of both: ask it of an object or an array"},
		{"Math", call("isFrozen", math), "error: Object.isFrozen: Math is a library, not data"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, c.got, c.want)
		}
	}
	// bản chép của một mảng đã đóng băng là mảng mở, như JavaScript
	for _, method := range []string{"slice", "concat", "toSorted", "toReversed", "map"} {
		var arguments []value.Value
		if method == "map" {
			arguments = []value.Value{value.Function(callbacks["double"])}
		}
		if made := javascript.Invoke(run{}, frozenArray, method, arguments); made.IsFrozen() {
			t.Errorf("%s of a frozen array must be open", method)
		}
	}
	if len := frozenArray.Length(); len != 1 {
		t.Errorf("a refused push must leave the array as it was: %d items", len)
	}
}

// Dữ liệu dùng chung (thứ boot dựng, value.FreezeShared): đọc và bản chép như mọi khi, phương thức ghi là lỗi nói cách
// viết thay, Object.isFrozen là lỗi vì Kitwork đóng nó còn JavaScript để mở, Object.freeze không đổi gì.
func TestSharedDataIsReadOnly(t *testing.T) {
	object, _ := javascript.Global("Object")
	json, _ := javascript.Global("JSON")
	list := value.Array(value.Number(3), value.Number(1))
	config := value.Object().Set("name", value.String("shop")).Set("list", list)
	config.FreezeShared()
	text := func(v value.Value) string {
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	const array = "error: cannot change the array: it is shared by every run (made as the site booted) and only read; change a copy, [...a]"
	cases := []struct{ name, got, want string }{
		{"push", text(javascript.Invoke(nil, list, "push", []value.Value{value.Number(2)})), array},
		{"sort", text(javascript.Invoke(nil, list, "sort", nil)), array},
		{"toSorted is a copy", text(javascript.Invoke(nil, list, "toSorted", nil)), "[n:3ff0000000000000,n:4008000000000000]"},
		{"assign onto it", text(javascript.Invoke(nil, object, "assign", []value.Value{config, value.Object().Set("name", value.Null())})),
			`error: cannot write property "name": the object is shared by every run (made as the site booted) and only read; write to a copy, { ...o }`},
		{"assign from it", text(javascript.Invoke(nil, object, "assign", []value.Value{value.Object(), config})),
			`{"name":s:"shop","list":[n:4008000000000000,n:3ff0000000000000]}`},
		{"isFrozen", text(javascript.Invoke(nil, object, "isFrozen", []value.Value{config})),
			"error: Object.isFrozen: Kitwork froze this object because every run shares it (it was made as the site booted), where JavaScript leaves it open, so neither answer is true of both"},
		{"isFrozen of an array", text(javascript.Invoke(nil, object, "isFrozen", []value.Value{list})),
			"error: Object.isFrozen: Kitwork froze this array because every run shares it (it was made as the site booted), where JavaScript leaves it open, so neither answer is true of both"},
		{"freeze changes nothing", text(javascript.Invoke(nil, object, "freeze", []value.Value{list})), "[n:4008000000000000,n:3ff0000000000000]"},
		{"keys", text(javascript.Invoke(nil, object, "keys", []value.Value{config})), `[s:"name",s:"list"]`},
		{"JSON", text(javascript.Invoke(nil, json, "stringify", []value.Value{config})), `s:"{\"name\":\"shop\",\"list\":[3,1]}"`},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, c.got, c.want)
		}
	}
	if copied := javascript.Invoke(nil, object, "assign", []value.Value{value.Object(), config}); copied.IsFrozen() ||
		copied.Set("name", value.Null()).IsInvalid() {
		t.Error("a copy of shared data is open")
	}
}
