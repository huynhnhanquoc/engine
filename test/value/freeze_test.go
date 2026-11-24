package value_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Một object, một mảng đã đóng băng từ chối mọi phép ghi, lời báo như TypeError của JavaScript, và không đổi; mọi bản
// chép của Value thấy dấu; bản chép các phần tử là một mảng mới, mở.
func TestFrozenRefusesEveryWrite(t *testing.T) {
	o := value.Object().Set("a", value.Number(1))
	copied := o
	o.Freeze()
	a := value.Array(value.Number(1), value.Number(2)).Freeze()
	failure := func(v value.Value) string {
		if !v.IsInvalid() {
			return "no error"
		}
		return v.Error().Error()
	}
	cases := []struct{ name, got, want string }{
		{"set a key it has", failure(o.Set("a", value.Number(1))), `cannot assign to read only property "a" of a frozen object`},
		{"add a key", failure(o.Set("b", value.Number(2))), `cannot add property "b", object is not extensible`},
		{"delete a key it has", failure(o.Delete("a")), `cannot delete property "a" of a frozen object`},
		{"delete a key it has not", failure(o.Delete("z")), "no error"},
		{"set an index it has", failure(a.SetIndex(0, value.Number(9))), `cannot assign to read only property "0" of a frozen array`},
		{"add an index", failure(a.SetIndex(2, value.Number(9))), `cannot add property "2", object is not extensible`},
		{"an index past the end", failure(a.SetIndex(5, value.Number(9))), "index 5 is outside an array of length 2"},
		{"push", failure(a.Push(value.Number(3))), `cannot add property "2", object is not extensible`},
		{"push nothing", failure(a.Push()), `cannot assign to read only property "length" of a frozen array`},
		{"splice nothing", failure(a.Splice(0, 0)), `cannot assign to read only property "length" of a frozen array`},
		{"splice as many as it puts", failure(a.Splice(0, 2, value.Number(2), value.Number(1))), `cannot assign to read only property "0" of a frozen array`},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
	if !copied.IsFrozen() || !a.IsFrozen() {
		t.Error("every copy sees the mark")
	}
	if item, _ := o.Get("a"); item.Number() != 1 || len(o.Keys()) != 1 || a.Length() != 2 || a.Index(0).Number() != 1 {
		t.Error("a refused write must change nothing")
	}
	if value.Array(a.Items()...).IsFrozen() || o.Freeze().Freeze() != o || !o.IsFrozen() {
		t.Error("a copy of the items is open; freezing twice is freezing once")
	}
	open := value.Array(value.Number(1))
	if removed := open.Splice(0, 1); removed.IsInvalid() || removed.Length() != 1 || open.Length() != 0 {
		t.Errorf("Splice of an open array gives the removed items as an array: %d", removed.Length())
	}
	for _, v := range []value.Value{value.Number(1), value.String("x"), value.Undefined(), value.Function("f")} {
		if v.Freeze().IsFrozen() {
			t.Errorf("%s has nothing to freeze: IsFrozen is about objects and arrays", v.Typeof())
		}
	}
}
